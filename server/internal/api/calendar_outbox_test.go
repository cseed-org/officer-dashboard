package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"officerdashboard/internal/calendar"
	"officerdashboard/internal/store"
)

// flakyMailer is fakeMailer whose sends can be made to fail.
type flakyMailer struct {
	fakeMailer
	fail atomic.Bool
}

func (f *flakyMailer) Send(ctx context.Context, m calendar.Message) error {
	if f.fail.Load() {
		return errors.New("relay down")
	}
	return f.fakeMailer.Send(ctx, m)
}

// forUID returns the messages for one event's invite.
func forUID(msgs []calendar.Message, eventID string) []calendar.Message {
	var out []calendar.Message
	for _, m := range msgs {
		if strings.Contains(m.ICS, "UID:event-"+eventID+"@cseed-dashboard") {
			out = append(out, m)
		}
	}
	return out
}

// fakeGoogle is a Google Calendar API with one calendar per access token.
type fakeGoogle struct {
	mu     sync.Mutex
	next   int
	events map[string]map[string]string // access token → event id → status
	calls  []string                     // "POST at-A" / "PATCH at-A g-1 cancelled"
	srv    *httptest.Server
}

func newFakeGoogle(t *testing.T) *fakeGoogle {
	g := &fakeGoogle{events: map[string]map[string]string{}}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		var body struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		cal := g.events[tok]
		if cal == nil {
			cal = map[string]string{}
			g.events[tok] = cal
		}
		switch {
		case r.Method == "POST" && r.URL.Path == "/calendars/primary/events":
			// Like events.insert: a client-supplied id is used as is, and taking one that
			// already exists on the calendar is a 409.
			id := body.ID
			if id == "" {
				g.next++
				id = fmt.Sprintf("g-%d", g.next)
			} else if _, taken := cal[id]; taken {
				g.calls = append(g.calls, "POST "+tok+" 409")
				w.WriteHeader(409)
				return
			}
			cal[id] = body.Status
			g.calls = append(g.calls, "POST "+tok)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": id})
		case r.Method == "PATCH" && strings.HasPrefix(r.URL.Path, "/calendars/primary/events/"):
			id := strings.TrimPrefix(r.URL.Path, "/calendars/primary/events/")
			g.calls = append(g.calls, "PATCH "+tok+" "+id+" "+body.Status)
			if _, ok := cal[id]; !ok {
				w.WriteHeader(404)
				return
			}
			cal[id] = body.Status
			_ = json.NewEncoder(w).Encode(map[string]string{"id": id})
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(g.srv.Close)
	return g
}

func (g *fakeGoogle) snapshot() (map[string]map[string]string, []string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := map[string]map[string]string{}
	for tok, cal := range g.events {
		out[tok] = map[string]string{}
		for id, st := range cal {
			out[tok][id] = st
		}
	}
	return out, append([]string(nil), g.calls...)
}

func (g *fakeGoogle) config(c *Config) {
	cl := calendar.NewGoogle("gid", "gsecret", "http://localhost:8090/auth/google/callback")
	cl.AuthURL, cl.TokenURL, cl.APIBase, cl.RevokeURL = g.srv.URL+"/auth", g.srv.URL+"/token", g.srv.URL, g.srv.URL+"/revoke"
	c.Google = cl
}

// connectGoogle stores a grant for account (access token "at-"+account, valid for a day)
// and switches the officer to Google delivery, as the OAuth callback does.
func connectGoogle(t *testing.T, e *testEnv, userID, account string) {
	t.Helper()
	ctx := context.Background()
	sealer := calendar.NewSealer("gsecret")
	access := sealer.Seal("at-" + account)
	exp := e.clock.Add(24 * time.Hour).UnixMilli()
	email := account + "@gmail.test"
	if err := e.db.Write(ctx, func(w *store.Queries) error {
		if err := w.SaveGoogleToken(ctx, store.GoogleToken{UserID: userID, AccountEmail: &email, SealedRefresh: sealer.Seal("rt"), SealedAccess: &access, AccessExpires: &exp, ConnectedAt: e.clock.UnixMilli()}); err != nil {
			return err
		}
		return w.SetDeliveryMode(ctx, userID, "google")
	}); err != nil {
		t.Fatal(err)
	}
}

func (e *testEnv) pendingCalendarJobs() int {
	e.t.Helper()
	n, err := e.db.Read().PendingCalendarJobs(context.Background())
	if err != nil {
		e.t.Fatal(err)
	}
	return n
}

// backfill queues and runs a backfill for one officer, as a destination change does.
func (e *testEnv) backfill(userID string) {
	e.t.Helper()
	ctx := context.Background()
	if err := e.db.Write(ctx, func(w *store.Queries) error { return e.srv.queueBackfill(ctx, w, userID) }); err != nil {
		e.t.Fatal(err)
	}
	e.srv.kickCalendar()
}

func (e *testEnv) newEvent(name string) string {
	e.t.Helper()
	return e.mustOK(e.post("/api/events", map[string]any{"name": name, "startsAt": "2026-11-20T18:00:00-08:00", "location": "Room 101"}, e.officer())).str("event", "id")
}

// M-001: a deletion's cancellations are stored in the outbox with the delete itself. When
// they can't go out before the process stops, a restarted server sends them, with the
// event's original UID and the next SEQUENCE.
func TestCalendarCancellationSurvivesRestart(t *testing.T) {
	m := &flakyMailer{}
	e := newEnv(t, func(c *Config) { c.Mailer = m })
	e.mustOK(e.post("/api/me/calendar-emails", map[string]any{"email": "priya.cal@example.org"}, e.officer()))
	id := e.newEvent("Outbox night")
	e.srv.WaitBackground()
	if got := forUID(m.all(), id); len(got) != 1 || !strings.Contains(got[0].ICS, "SEQUENCE:0") {
		t.Fatalf("invite %+v", got)
	}
	m.fail.Store(true) // the relay is unreachable until the restart
	e.mustOK(e.do("DELETE", "/api/events/"+id, nil, e.officer()))
	e.srv.WaitBackground()
	if n := e.pendingCalendarJobs(); n != 1 {
		t.Fatalf("outbox holds %d jobs after a failed cancellation, want 1", n)
	}
	// Restart: a new server on the same database drains the outbox.
	m.fail.Store(false)
	restarted := New(e.cfg)
	restarted.ResumeCalendar()
	restarted.WaitBackground()
	got := forUID(m.all(), id)
	if len(got) != 2 || got[1].Method != "CANCEL" || got[1].To != "priya.cal@example.org" || !strings.Contains(got[1].ICS, "SEQUENCE:1") || !strings.Contains(got[1].ICS, "STATUS:CANCELLED") {
		t.Fatalf("cancellation after restart %+v", got)
	}
	if n := e.pendingCalendarJobs(); n != 0 {
		t.Fatalf("outbox still holds %d jobs", n)
	}
	// Event syncs are in the outbox too: a sync whose send fails stays there (nothing is
	// queued by hand here) and a restarted server delivers it.
	m.fail.Store(true)
	id2 := e.newEvent("Left behind")
	e.srv.WaitBackground()
	if n := e.pendingCalendarJobs(); n != 1 {
		t.Fatalf("outbox holds %d jobs after a failed sync, want 1", n)
	}
	m.fail.Store(false)
	again := New(e.cfg)
	again.ResumeCalendar()
	again.WaitBackground()
	if got := forUID(m.all(), id2); len(got) != 1 || got[0].Method != "REQUEST" {
		t.Fatalf("sync job after restart %+v", got)
	}
	if n := e.pendingCalendarJobs(); n != 0 {
		t.Fatalf("outbox still holds %d jobs", n)
	}
}

// raceDelete holds the next invite send in the worker, deletes the event while that send
// is in flight, then lets the send finish. change makes the edit that triggers the send
// and returns the event id. The synchronisation is explicit: the send signals that it is
// about to go out (holding the event lock), and the DELETE signals that it is about to
// wait for that lock; only then is the send released. Whatever is blocked is released when
// raceDelete returns, also when an assertion fails.
func raceDelete(t *testing.T, e *testEnv, change func() string) string {
	t.Helper()
	entered, release, waiting := make(chan string, 1), make(chan struct{}), make(chan struct{}, 1)
	var first, freed sync.Once
	free := func() { freed.Do(func() { close(release) }) }
	testDeliveryHook = func(eventID, _ string) {
		held := false
		first.Do(func() { held = true })
		if held {
			entered <- eventID
			<-release
		}
	}
	testDeleteHook = func(string) { waiting <- struct{}{} }
	defer func() {
		free()
		e.srv.WaitBackground()
		testDeliveryHook, testDeleteHook = nil, nil
	}()
	id := change()
	select {
	case got := <-entered: // the send has read the event and recorded its attempt
		if got != id {
			t.Fatalf("held a send of %s, want %s", got, id)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no send started")
	}
	deleted := make(chan reply, 1)
	go func() { deleted <- e.do("DELETE", "/api/events/"+id, nil, e.officer()) }()
	<-waiting // the DELETE is about to wait for the event lock, which the send holds
	select {
	case r := <-deleted:
		t.Fatalf("delete finished while a send was in flight: %d", r.Status)
	default:
	}
	free()
	if r := <-deleted; r.Status != 200 {
		t.Fatalf("delete %d %s", r.Status, r.Raw)
	}
	e.srv.WaitBackground()
	if n := e.scalar(`SELECT count(*) FROM calendar_deliveries WHERE event_id = ?`, id); n != int64(0) {
		t.Errorf("%v delivery rows remain for the deleted event", n)
	}
	return id
}

// M-007: deleting an event while its invite is mid-send waits for that send, then cancels
// it on the same destination with a SEQUENCE strictly newer than the one in flight: for
// the first invite, and for an update of an invite already delivered. For Google, the
// cancellation targets the event the in-flight send created or updated rather than
// creating another.
func TestDeleteDuringFirstDeliveryCancelsIt(t *testing.T) {
	icsEnv := func(t *testing.T) (*testEnv, *fakeMailer) {
		m := &fakeMailer{}
		e := newEnv(t, func(c *Config) { c.Mailer = m })
		e.mustOK(e.post("/api/me/calendar-emails", map[string]any{"email": "priya.cal@example.org"}, e.officer()))
		e.srv.WaitBackground()
		return e, m
	}
	googleEnv := func(t *testing.T) (*testEnv, *fakeGoogle) {
		g := newFakeGoogle(t)
		e := newEnv(t, func(c *Config) { g.config(c) })
		connectGoogle(t, e, "user-priya", "priya")
		e.backfill("user-priya")
		e.srv.WaitBackground()
		return e, g
	}
	checkGoogle := func(t *testing.T, g *fakeGoogle, before map[string]map[string]string, id string) {
		cals, calls := g.snapshot()
		if len(cals["at-priya"]) != len(before["at-priya"])+1 {
			t.Fatalf("google events %v (calls %v)", cals, calls)
		}
		for gid, st := range cals["at-priya"] {
			if _, old := before["at-priya"][gid]; !old && st != "cancelled" {
				t.Fatalf("google event %s left %q (calls %v)", gid, st, calls)
			}
		}
	}
	t.Run("ics first send", func(t *testing.T) {
		e, m := icsEnv(t)
		id := raceDelete(t, e, func() string { return e.newEvent("Racing delete") })
		mails := forUID(m.all(), id)
		if len(mails) != 2 || mails[0].Method != "REQUEST" || !strings.Contains(mails[0].ICS, "SEQUENCE:0") ||
			mails[1].Method != "CANCEL" || mails[1].To != mails[0].To || !strings.Contains(mails[1].ICS, "SEQUENCE:1") {
			t.Fatalf("ics %+v", mails)
		}
	})
	t.Run("ics update", func(t *testing.T) {
		e, m := icsEnv(t)
		id := e.newEvent("Racing update")
		e.srv.WaitBackground()
		raceDelete(t, e, func() string {
			e.mustOK(e.do("PATCH", "/api/events/"+id, map[string]any{"location": "Room 202"}, e.officer()))
			return id
		})
		mails := forUID(m.all(), id)
		if len(mails) != 3 || mails[1].Method != "REQUEST" || !strings.Contains(mails[1].ICS, "SEQUENCE:1") ||
			mails[2].Method != "CANCEL" || mails[2].To != mails[1].To || !strings.Contains(mails[2].ICS, "SEQUENCE:2") {
			t.Fatalf("the CANCEL must follow the in-flight update with a higher SEQUENCE: %+v", mails)
		}
	})
	t.Run("google first send", func(t *testing.T) {
		e, g := googleEnv(t)
		before, _ := g.snapshot()
		id := raceDelete(t, e, func() string { return e.newEvent("Racing delete") })
		checkGoogle(t, g, before, id)
	})
	t.Run("google update", func(t *testing.T) {
		e, g := googleEnv(t)
		before, _ := g.snapshot()
		id := e.newEvent("Racing update")
		e.srv.WaitBackground()
		raceDelete(t, e, func() string {
			e.mustOK(e.do("PATCH", "/api/events/"+id, map[string]any{"location": "Room 202"}, e.officer()))
			return id
		})
		checkGoogle(t, g, before, id)
	})
}

// M-034: a failed update keeps the earlier successful delivery, so deleting the event
// later still cancels the original invite; a failed switch to another address does not
// replace where the invite already is.
func TestFailedUpdateStillCancelsOriginalInvite(t *testing.T) {
	m := &flakyMailer{}
	e := newEnv(t, func(c *Config) { c.Mailer = m })
	e.mustOK(e.post("/api/me/calendar-emails", map[string]any{"email": "priya.cal@example.org"}, e.officer()))
	id := e.newEvent("Fragile")
	e.srv.WaitBackground()
	m.fail.Store(true)
	e.mustOK(e.do("PATCH", "/api/events/"+id, map[string]any{"location": "Room 202"}, e.officer()))
	e.srv.WaitBackground()
	// A switch to another address that fails as well.
	e.mustOK(e.post("/api/me/calendar-emails", map[string]any{"email": "priya.new@example.org", "active": true}, e.officer()))
	e.srv.WaitBackground()
	m.fail.Store(false) // the relay recovers
	e.mustOK(e.do("DELETE", "/api/events/"+id, nil, e.officer()))
	e.srv.WaitBackground()
	got := forUID(m.all(), id)
	if len(got) != 2 || got[1].Method != "CANCEL" || got[1].To != "priya.cal@example.org" || !strings.Contains(got[1].ICS, "SEQUENCE:1") {
		t.Fatalf("messages %+v", got)
	}
}

// M-038: cancelling an event (status only) withdraws the invite; a new recipient's backfill
// does not invite them to it; restoring it sends a normal update. Google likewise.
func TestCancelledStatusWithdrawsAndRestoreUpdates(t *testing.T) {
	m := &fakeMailer{}
	g := newFakeGoogle(t)
	e := newEnv(t, func(c *Config) { c.Mailer = m; g.config(c) })
	e.mustOK(e.post("/api/me/calendar-emails", map[string]any{"email": "priya.cal@example.org"}, e.officer()))
	connectGoogle(t, e, "user-director", "dir")
	id := e.newEvent("Maybe")
	e.srv.WaitBackground()
	e.mustOK(e.do("PATCH", "/api/events/"+id, map[string]any{"status": "cancelled"}, e.officer()))
	e.srv.WaitBackground()
	got := forUID(m.all(), id)
	if len(got) != 2 || got[1].Method != "CANCEL" || !strings.Contains(got[1].ICS, "SEQUENCE:1") {
		t.Fatalf("status cancel %+v", got)
	}
	if cals, calls := g.snapshot(); len(cals["at-dir"]) != 1 || !strings.HasSuffix(calls[len(calls)-1], " cancelled") {
		t.Fatalf("google after cancel %v %v", cals, calls)
	}
	// Saving the cancelled event again sends nothing more.
	e.mustOK(e.do("PATCH", "/api/events/"+id, map[string]any{"status": "cancelled"}, e.officer()))
	e.srv.WaitBackground()
	if n := len(forUID(m.all(), id)); n != 2 {
		t.Fatalf("repeated cancel sent %d messages", n)
	}
	// A new recipient's backfill skips the cancelled event.
	e.mustOK(e.post("/api/me/calendar-emails", map[string]any{"email": "ava.cal@example.org"}, e.as("user-ava")))
	e.srv.WaitBackground()
	for _, msg := range forUID(m.all(), id) {
		if msg.To == "ava.cal@example.org" {
			t.Fatalf("backfill invited a new recipient to a cancelled event: %+v", msg)
		}
	}
	// Restoring it is a normal update with a newer SEQUENCE.
	e.mustOK(e.do("PATCH", "/api/events/"+id, map[string]any{"status": "published"}, e.officer()))
	e.srv.WaitBackground()
	var priya []calendar.Message
	for _, msg := range forUID(m.all(), id) {
		if msg.To == "priya.cal@example.org" {
			priya = append(priya, msg)
		}
	}
	if len(priya) != 3 || priya[2].Method != "REQUEST" || !strings.Contains(priya[2].ICS, "SEQUENCE:2") {
		t.Fatalf("restore %+v", priya)
	}
	if cals, calls := g.snapshot(); len(cals["at-dir"]) != 1 || !strings.HasSuffix(calls[len(calls)-1], " confirmed") {
		t.Fatalf("google after restore %v %v", cals, calls)
	}
}

// M-022: switching delivery mode through Disconnect Google or PUT /api/me/profile sends
// upcoming events to the newly selected destination without another write.
func TestDeliveryModeChangesBackfill(t *testing.T) {
	m := &fakeMailer{}
	g := newFakeGoogle(t)
	e := newEnv(t, func(c *Config) { c.Mailer = m; g.config(c) })
	connectGoogle(t, e, "user-priya", "priya")
	e.mustOK(e.post("/api/me/calendar-emails", map[string]any{"email": "priya.cal@example.org"}, e.officer()))
	id := e.newEvent("Both ways")
	e.srv.WaitBackground()
	if n := len(forUID(m.all(), id)); n != 0 {
		t.Fatalf("google-mode officer got %d emails", n)
	}
	// Disconnect Google: the account falls back to .ics, and the event arrives by email.
	e.mustOK(e.do("DELETE", "/api/me/calendar/google", nil, e.officer()))
	e.srv.WaitBackground()
	if got := forUID(m.all(), id); len(got) != 1 || got[0].To != "priya.cal@example.org" || got[0].Method != "REQUEST" {
		t.Fatalf("after disconnect %+v", got)
	}
	// Profile save that switches ics → google: the event reaches Google.
	connectGoogle(t, e, "user-priya", "priya2")
	if err := e.db.Write(context.Background(), func(w *store.Queries) error { return w.SetDeliveryMode(context.Background(), "user-priya", "ics") }); err != nil {
		t.Fatal(err)
	}
	e.mustOK(e.do("PUT", "/api/me/profile", map[string]any{"phone": "+1 206 555 0100", "birthday": "01-15", "calendarDeliveryMode": "google"}, e.officer()))
	e.srv.WaitBackground()
	if cals, calls := g.snapshot(); len(cals["at-priya2"]) == 0 {
		t.Fatalf("profile switch to google sent nothing: %v", calls)
	}
}

// M-070: reconnecting with a different Google account delivers to that account's own
// calendar and never patches the first account's event ids there; reconnecting the first
// account again does not duplicate its events.
func TestNewGoogleAccountGetsItsOwnEvents(t *testing.T) {
	g := newFakeGoogle(t)
	e := newEnv(t, func(c *Config) { g.config(c) })
	connectGoogle(t, e, "user-priya", "a")
	e.backfill("user-priya")
	e.newEvent("Two accounts")
	e.srv.WaitBackground()
	cals, _ := g.snapshot()
	nA := len(cals["at-a"])
	if nA == 0 {
		t.Fatal("nothing on account A")
	}
	connectGoogle(t, e, "user-priya", "b")
	e.backfill("user-priya")
	e.srv.WaitBackground()
	cals, calls := g.snapshot()
	if len(cals["at-b"]) != nA {
		t.Fatalf("account B has %d events, want %d (calls %v)", len(cals["at-b"]), nA, calls)
	}
	for _, c := range calls {
		if strings.HasPrefix(c, "PATCH at-b") {
			t.Fatalf("patched an id on account B: %v", calls)
		}
	}
	before := len(calls)
	connectGoogle(t, e, "user-priya", "a")
	e.backfill("user-priya")
	e.srv.WaitBackground()
	cals, calls = g.snapshot()
	if len(cals["at-a"]) != nA || len(calls) != before {
		t.Fatalf("reconnecting A changed its calendar: %d events, new calls %v", len(cals["at-a"]), calls[before:])
	}
}

// R-01: a sync whose send fails stays in the outbox and is delivered once the relay is back.
func TestReviewFailedSyncIsRetried(t *testing.T) {
	m := &flakyMailer{}
	e := newEnv(t, func(c *Config) { c.Mailer = m })
	e.mustOK(e.post("/api/me/calendar-emails", map[string]any{"email": "priya.cal@example.org"}, e.officer()))
	e.srv.WaitBackground()
	m.fail.Store(true)
	id := e.newEvent("Retry first invite")
	e.srv.WaitBackground()
	if n := e.pendingCalendarJobs(); n == 0 {
		t.Fatal("the failed sync left nothing in the outbox")
	}
	m.fail.Store(false)
	e.srv.ResumeCalendar()
	e.srv.WaitBackground()
	if got := forUID(m.all(), id); len(got) != 1 {
		t.Fatalf("after recovery and resume: %d invites, want 1", len(got))
	}
	if n := e.pendingCalendarJobs(); n != 0 {
		t.Fatalf("outbox still holds %d jobs", n)
	}
}

// R-01: a status cancellation whose CANCEL fails is retried until the invite is withdrawn.
func TestReviewFailedStatusCancellationIsRetried(t *testing.T) {
	m := &flakyMailer{}
	e := newEnv(t, func(c *Config) { c.Mailer = m })
	e.mustOK(e.post("/api/me/calendar-emails", map[string]any{"email": "priya.cal@example.org"}, e.officer()))
	id := e.newEvent("Retry status cancellation")
	e.srv.WaitBackground()
	m.fail.Store(true)
	e.mustOK(e.do("PATCH", "/api/events/"+id, map[string]any{"status": "cancelled"}, e.officer()))
	e.srv.WaitBackground()
	if n := e.pendingCalendarJobs(); n == 0 {
		t.Fatal("the failed cancellation left nothing in the outbox")
	}
	m.fail.Store(false)
	e.srv.ResumeCalendar()
	e.srv.WaitBackground()
	got := forUID(m.all(), id)
	if len(got) != 2 || got[1].Method != "CANCEL" {
		t.Fatalf("after recovery and resume: %+v; want REQUEST and CANCEL", got)
	}
	if n := e.pendingCalendarJobs(); n != 0 {
		t.Fatalf("outbox still holds %d jobs", n)
	}
}

// R-01: a backfill to a new address that fails is retried, and only what failed is sent.
func TestFailedBackfillIsRetried(t *testing.T) {
	m := &flakyMailer{}
	e := newEnv(t, func(c *Config) { c.Mailer = m })
	id := e.newEvent("Before the address")
	e.srv.WaitBackground()
	m.fail.Store(true)
	e.mustOK(e.post("/api/me/calendar-emails", map[string]any{"email": "priya.cal@example.org"}, e.officer()))
	e.srv.WaitBackground()
	if n := e.pendingCalendarJobs(); n == 0 {
		t.Fatal("the failed backfill left nothing in the outbox")
	}
	m.fail.Store(false)
	e.srv.ResumeCalendar()
	e.srv.WaitBackground()
	if got := forUID(m.all(), id); len(got) != 1 || got[0].To != "priya.cal@example.org" {
		t.Fatalf("backfill after recovery %+v", got)
	}
	sent := len(m.all())
	e.srv.ResumeCalendar()
	e.srv.WaitBackground()
	if n := len(m.all()); n != sent || e.pendingCalendarJobs() != 0 {
		t.Fatalf("a finished backfill was repeated (%d -> %d messages, %d jobs)", sent, n, e.pendingCalendarJobs())
	}
}

// R-01: a change does not hammer a failing job: until its backoff passes, only startup
// and the hourly retry (ResumeCalendar) run it again.
func TestFailedJobWaitsForBackoff(t *testing.T) {
	m := &flakyMailer{}
	e := newEnv(t, func(c *Config) { c.Mailer = m })
	e.mustOK(e.post("/api/me/calendar-emails", map[string]any{"email": "priya.cal@example.org"}, e.officer()))
	e.srv.WaitBackground()
	m.fail.Store(true)
	id := e.newEvent("Backing off")
	e.srv.WaitBackground()
	m.fail.Store(false)
	other := e.newEvent("Unrelated change") // kicks the worker; the failed job isn't due yet
	e.srv.WaitBackground()
	if got := forUID(m.all(), id); len(got) != 0 {
		t.Fatalf("retried before its backoff: %+v", got)
	}
	if got := forUID(m.all(), other); len(got) != 1 {
		t.Fatalf("new job not delivered: %+v", got)
	}
	e.clock = e.clock.Add(calendarRetryMin)
	e.newEvent("Later change")
	e.srv.WaitBackground()
	if got := forUID(m.all(), id); len(got) != 1 {
		t.Fatalf("not retried once its backoff passed: %+v", got)
	}
}

// R-02: the worker records each attempt before sending. A crash after the send but before
// its outcome is recorded leaves that record (and the sync job); a DELETE that arrives
// before the job replays cancels the destination anyway, with a newer SEQUENCE, and the
// replay then finds the event gone.
func TestReviewCrashBeforeFirstDeliveryRecordThenDelete(t *testing.T) {
	m := &fakeMailer{}
	e := newEnv(t, func(c *Config) { c.Mailer = m })
	e.mustOK(e.post("/api/me/calendar-emails", map[string]any{"email": "priya.cal@example.org"}, e.officer()))
	e.srv.WaitBackground()
	ctx := context.Background()
	const id = "review-crash-gap"
	e.exec(`INSERT INTO events(id,name,starts_at,status) VALUES(?,?,?,'published')`, id, "Crash gap", "2026-11-20T18:00:00-08:00")
	if err := e.db.Write(ctx, func(q *store.Queries) error {
		return q.EnqueueCalendar(ctx, store.CalendarJob{Kind: "sync", EventID: id}, e.clock.UnixMilli())
	}); err != nil {
		t.Fatal(err)
	}
	ev, err := e.db.Read().EventByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	email := "priya.cal@example.org"
	// The worker's steps up to the crash: record the attempt, send. The outcome is never
	// recorded and the sync job stays in the outbox.
	p, err := e.srv.planDelivery(ctx, ev, recipient{UserID: "user-priya", Mode: "ics", Email: &email})
	if err != nil || p == nil {
		t.Fatalf("plan %v %v", p, err)
	}
	if out := e.srv.deliverOne(ctx, p.r, p.ev, p.prior); out.Status != "sent" {
		t.Fatal(out)
	}
	// Restart: a new server on the same database; a DELETE arrives before the replay.
	e.srv = New(e.cfg)
	e.h = e.srv.Handler()
	e.mustOK(e.do("DELETE", "/api/events/"+id, nil, e.officer()))
	e.srv.ResumeCalendar()
	e.srv.WaitBackground()
	got := forUID(m.all(), id)
	if len(got) != 2 || got[1].Method != "CANCEL" || got[1].To != email || !strings.Contains(got[1].ICS, "SEQUENCE:1") {
		t.Fatalf("outbox=%d, messages=%+v; initial invitation stranded", e.pendingCalendarJobs(), got)
	}
	if n := e.pendingCalendarJobs(); n != 0 {
		t.Fatalf("outbox still holds %d jobs", n)
	}
}

// R-03: Google inserts use a stable id the dashboard chooses. An insert that reached
// Google but whose answer and record were lost is not repeated as a second event on
// replay, and a delete before the replay cancels that same event.
func TestReviewGoogleCrashWindowDoesNotDuplicate(t *testing.T) {
	g := newFakeGoogle(t)
	e := newEnv(t, func(c *Config) { g.config(c) })
	connectGoogle(t, e, "user-priya", "a")
	ctx := context.Background()
	insert := func(id, name string) *store.Event {
		e.exec(`INSERT INTO events(id,name,starts_at,status) VALUES(?,?,?,'published')`, id, name, "2026-11-20T18:00:00-08:00")
		if err := e.db.Write(ctx, func(q *store.Queries) error {
			return q.EnqueueCalendar(ctx, store.CalendarJob{Kind: "sync", EventID: id}, e.clock.UnixMilli())
		}); err != nil {
			t.Fatal(err)
		}
		ev, err := e.db.Read().EventByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return ev
	}
	// Insert accepted, then the process stops before anything is recorded (not even the
	// attempt): the replay's insert finds the id taken and updates that event.
	ev := insert("review-google-crash", "Google crash gap")
	invite, _ := e.srv.calendarEvent(ev, 0, false)
	if out := e.srv.deliverOne(ctx, recipient{UserID: "user-priya", Mode: "google"}, invite, nil); out.Status != "sent" {
		t.Fatal(out)
	}
	restarted := New(e.cfg)
	restarted.ResumeCalendar()
	restarted.WaitBackground()
	cals, calls := g.snapshot()
	if len(cals["at-a"]) != 1 {
		t.Fatalf("after restarting: %d Google events for one invite; calls=%v", len(cals["at-a"]), calls)
	}
	// The worker's attempt is recorded, the insert accepted, then the crash; a DELETE
	// before the replay cancels the event the lost insert created.
	ev2 := insert("review-google-crash-2", "Google crash then delete")
	p, err := e.srv.planDelivery(ctx, ev2, recipient{UserID: "user-priya", Mode: "google"})
	if err != nil || p == nil {
		t.Fatalf("plan %v %v", p, err)
	}
	if out := e.srv.deliverOne(ctx, p.r, p.ev, p.prior); out.Status != "sent" {
		t.Fatal(out)
	}
	e.srv = New(e.cfg)
	e.h = e.srv.Handler()
	e.mustOK(e.do("DELETE", "/api/events/review-google-crash-2", nil, e.officer()))
	e.srv.ResumeCalendar()
	e.srv.WaitBackground()
	cals, calls = g.snapshot()
	if len(cals["at-a"]) != 2 || cals["at-a"][*p.row.GoogleEventID] != "cancelled" {
		t.Fatalf("after delete: %v; calls=%v", cals["at-a"], calls)
	}
	if n := e.pendingCalendarJobs(); n != 0 {
		t.Fatalf("outbox still holds %d jobs", n)
	}
}

// R-04: a cancellation that keeps failing is never dropped: however many attempts fail,
// it goes out once the relay is back.
func TestReviewDeleteAfterManyFailedCancellationAttempts(t *testing.T) {
	m := &flakyMailer{}
	e := newEnv(t, func(c *Config) { c.Mailer = m })
	e.mustOK(e.post("/api/me/calendar-emails", map[string]any{"email": "priya.cal@example.org"}, e.officer()))
	id := e.newEvent("Don't abandon cancellation")
	e.srv.WaitBackground()
	m.fail.Store(true)
	e.mustOK(e.do("DELETE", "/api/events/"+id, nil, e.officer()))
	e.srv.WaitBackground()
	for i := 0; i < 2*calendarAlertAttempts; i++ {
		e.srv.ResumeCalendar()
		e.srv.WaitBackground()
	}
	if n := e.pendingCalendarJobs(); n != 1 {
		t.Fatalf("outbox holds %d jobs after many failures, want the cancellation", n)
	}
	m.fail.Store(false)
	e.srv.ResumeCalendar()
	e.srv.WaitBackground()
	got := forUID(m.all(), id)
	if len(got) != 2 || !strings.Contains(got[1].ICS, "METHOD:CANCEL") {
		t.Fatalf("after relay recovers: outbox=%d, %d messages; cancellation snapshot lost", e.pendingCalendarJobs(), len(got))
	}
}

// legacyDeliveries turns calendar_deliveries back into its pre-0027 shape (one row per
// officer), adds row, and has store.Open apply 0027 again, as an upgrade does. It points
// the env at the upgraded database and a server on it.
func legacyDeliveries(t *testing.T, e *testEnv, row func()) {
	t.Helper()
	e.exec(`CREATE TABLE review_legacy AS SELECT event_id,user_id,mode,recipient,google_event_id,sequence,status,error,content_hash,updated_at FROM calendar_deliveries`)
	e.exec(`DROP TABLE calendar_deliveries`)
	e.exec(`ALTER TABLE review_legacy RENAME TO calendar_deliveries`)
	row()
	e.exec(`DELETE FROM schema_migrations WHERE name='0027_calendar_outbox.sql'`)
	e.exec(`DROP TABLE calendar_outbox`)
	dbPath := e.db.Path
	e.db.Close()
	upgraded, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(upgraded.Close)
	e.cfg.DB, e.db = upgraded, upgraded
	e.srv = New(e.cfg)
	e.h = e.srv.Handler()
}

// R-05: before V0.14.1 a switch Google -> .ics kept the Google event id on the officer's
// single delivery row. Migration 0027 keeps it as that account's Google destination:
// unchanged .ics is not resent, switching back updates the same Google event, and deleting
// the event withdraws both.
func TestReviewMigrationKeepsGoogleIDAcrossModeSwitch(t *testing.T) {
	m := &fakeMailer{}
	g := newFakeGoogle(t)
	e := newEnv(t, func(c *Config) { c.Mailer = m; g.config(c) })
	e.mustOK(e.post("/api/me/calendar-emails", map[string]any{"email": "priya.cal@example.org"}, e.officer()))
	e.srv.WaitBackground()
	connectGoogle(t, e, "user-priya", "a")
	ctx := context.Background()
	const id = "review-legacy-google-id"
	e.exec(`INSERT INTO events(id,name,starts_at,status) VALUES(?,?,?,'published')`, id, "Legacy mode switch", "2026-11-20T18:00:00-08:00")
	ev, _ := e.db.Read().EventByID(ctx, id)
	invite, _ := e.srv.calendarEvent(ev, 0, false)
	out := e.srv.deliverOne(ctx, recipient{UserID: "user-priya", Mode: "google"}, invite, nil)
	if out.Status != "sent" {
		t.Fatal(out)
	}
	email := "priya.cal@example.org"
	hash := fingerprint(invite, recipient{Mode: "ics", Email: &email})
	legacyDeliveries(t, e, func() {
		e.exec(`INSERT INTO calendar_deliveries(event_id,user_id,mode,recipient,google_event_id,sequence,status,content_hash,updated_at) VALUES(?,?,'ics',?,?,1,'sent',?,1)`, id, "user-priya", email, out.GoogleEventID, hash)
		e.exec(`UPDATE officer_profiles SET calendar_delivery_mode='ics' WHERE user_id='user-priya'`)
	})
	before := len(forUID(m.all(), id))
	if err := e.srv.deliverTo(ctx, id, recipient{UserID: "user-priya", Mode: "ics", Email: &email}); err != nil {
		t.Fatal(err)
	}
	if len(forUID(m.all(), id)) != before {
		t.Fatal("migration resent unchanged ICS")
	}
	if err := e.db.Write(ctx, func(q *store.Queries) error { return q.SetDeliveryMode(ctx, "user-priya", "google") }); err != nil {
		t.Fatal(err)
	}
	if err := e.srv.deliverTo(ctx, id, recipient{UserID: "user-priya", Mode: "google", Email: &email}); err != nil {
		t.Fatal(err)
	}
	cals, calls := g.snapshot()
	if len(cals["at-a"]) != 1 {
		t.Fatalf("mode switch after upgrade creates duplicate: %d events, calls=%v", len(cals["at-a"]), calls)
	}
	e.mustOK(e.do("DELETE", "/api/events/"+id, nil, e.officer()))
	e.srv.WaitBackground()
	if cals, calls = g.snapshot(); cals["at-a"][out.GoogleEventID] != "cancelled" {
		t.Fatalf("delete after upgrade left the Google event %v (calls %v)", cals["at-a"], calls)
	}
	if got := forUID(m.all(), id); len(got) != before+1 || got[len(got)-1].Method != "CANCEL" {
		t.Fatalf("delete after upgrade: .ics %+v", got)
	}
}

// R-05: deleting right after the upgrade (no switch back) also withdraws the Google event
// the legacy .ics row remembered.
func TestMigrationLegacyGoogleIDIsCancelledOnDelete(t *testing.T) {
	m := &fakeMailer{}
	g := newFakeGoogle(t)
	e := newEnv(t, func(c *Config) { c.Mailer = m; g.config(c) })
	connectGoogle(t, e, "user-priya", "a")
	ctx := context.Background()
	const id = "legacy-delete"
	e.exec(`INSERT INTO events(id,name,starts_at,status) VALUES(?,?,?,'published')`, id, "Legacy delete", "2026-11-20T18:00:00-08:00")
	ev, _ := e.db.Read().EventByID(ctx, id)
	invite, _ := e.srv.calendarEvent(ev, 0, false)
	out := e.srv.deliverOne(ctx, recipient{UserID: "user-priya", Mode: "google"}, invite, nil)
	if out.Status != "sent" {
		t.Fatal(out)
	}
	email := "priya.cal@example.org"
	legacyDeliveries(t, e, func() {
		e.exec(`INSERT INTO calendar_deliveries(event_id,user_id,mode,recipient,google_event_id,sequence,status,content_hash,updated_at) VALUES(?,?,'ics',?,?,0,'sent','x',1)`, id, "user-priya", email, out.GoogleEventID)
	})
	rows, err := e.db.Read().DeliveriesForEvent(ctx, id, "user-priya")
	if err != nil || len(rows) != 2 || rows[0].Destination != "google:a@gmail.test" || rows[1].Destination != "ics:"+email {
		t.Fatalf("migrated rows %+v %v", rows, err)
	}
	e.mustOK(e.do("DELETE", "/api/events/"+id, nil, e.officer()))
	e.srv.WaitBackground()
	if cals, calls := g.snapshot(); len(cals["at-a"]) != 1 || cals["at-a"][out.GoogleEventID] != "cancelled" {
		t.Fatalf("delete after upgrade left the Google event %v (calls %v)", cals["at-a"], calls)
	}
	if got := forUID(m.all(), id); len(got) != 1 || got[0].Method != "CANCEL" || got[0].To != email {
		t.Fatalf("delete after upgrade: .ics %+v", got)
	}
}

// Backfills are queued in the transaction that changes where an officer's invites go: if
// the outbox write fails, the change is not saved either, so a restart can never leave a
// changed destination without its backfill.
func TestBackfillIsQueuedWithTheDestinationChange(t *testing.T) {
	g := newFakeGoogle(t)
	e := newEnv(t, func(c *Config) { c.Mailer = &fakeMailer{}; g.config(c) })
	connectGoogle(t, e, "user-priya", "a")
	e.exec(`CREATE TRIGGER outbox_down BEFORE INSERT ON calendar_outbox WHEN NEW.kind = 'backfill' BEGIN SELECT RAISE(ABORT, 'outbox unavailable'); END`)
	mode := func() any {
		return e.scalar(`SELECT calendar_delivery_mode FROM officer_profiles WHERE user_id = 'user-priya'`)
	}
	emails := e.scalar(`SELECT count(*) FROM calendar_emails WHERE user_id = 'user-priya'`)
	if r := e.post("/api/me/calendar-emails", map[string]any{"email": "priya.cal@example.org"}, e.officer()); r.Status == 200 {
		t.Error("calendar email saved without its backfill")
	}
	if n := e.scalar(`SELECT count(*) FROM calendar_emails WHERE user_id = 'user-priya'`); n != emails {
		t.Errorf("calendar emails %v -> %v", emails, n)
	}
	if r := e.do("PUT", "/api/me/calendar", map[string]any{"mode": "ics"}, e.officer()); r.Status == 200 {
		t.Error("delivery mode changed without its backfill")
	}
	if got := mode(); got != "google" {
		t.Errorf("mode %v after a refused change", got)
	}
	if r := e.do("DELETE", "/api/me/calendar/google", nil, e.officer()); r.Status == 200 {
		t.Error("Google disconnected (falling back to .ics) without its backfill")
	}
	if got := mode(); got != "google" || e.scalar(`SELECT count(*) FROM google_calendar_tokens WHERE user_id = 'user-priya'`) != int64(1) {
		t.Errorf("disconnect half-applied: mode %v", got)
	}
	if r := e.do("PUT", "/api/me/profile", map[string]any{"phone": "+1 206 555 0100", "birthday": "01-15", "calendarDeliveryMode": "ics"}, e.officer()); r.Status == 200 {
		t.Error("profile switched delivery mode without its backfill")
	}
	if got := mode(); got != "google" {
		t.Errorf("mode %v after a refused profile save", got)
	}
}
