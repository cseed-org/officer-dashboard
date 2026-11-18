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
			g.next++
			id := fmt.Sprintf("g-%d", g.next)
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
	// Event syncs are in the outbox too: a job left by a crash is delivered on restart.
	m.fail.Store(true)
	id2 := e.newEvent("Left behind")
	e.srv.WaitBackground()
	m.fail.Store(false)
	if err := e.db.Write(context.Background(), func(w *store.Queries) error {
		return w.EnqueueCalendar(context.Background(), store.CalendarJob{Kind: "sync", EventID: id2}, e.clock.UnixMilli())
	}); err != nil {
		t.Fatal(err)
	}
	again := New(e.cfg)
	again.ResumeCalendar()
	again.WaitBackground()
	if got := forUID(m.all(), id2); len(got) != 1 || got[0].Method != "REQUEST" {
		t.Fatalf("sync job after restart %+v", got)
	}
}

// M-007: deleting an event while its first invite is mid-send waits for that send, then
// cancels it on the same destination with a newer SEQUENCE. For Google, the cancellation
// targets the event the in-flight send created rather than creating another.
func TestDeleteDuringFirstDeliveryCancelsIt(t *testing.T) {
	race := func(t *testing.T, e *testEnv) string {
		entered, release := make(chan struct{}, 8), make(chan struct{})
		var once sync.Once
		testDeliveryHook = func(string, string) {
			entered <- struct{}{}
			once.Do(func() { <-release })
		}
		defer func() { testDeliveryHook = nil }()
		id := e.newEvent("Racing delete")
		<-entered // the first send has read the event and is about to go out
		deleted := make(chan reply)
		go func() { deleted <- e.do("DELETE", "/api/events/"+id, nil, e.officer()) }()
		select {
		case r := <-deleted:
			t.Fatalf("delete finished while a send was in flight: %d", r.Status)
		case <-time.After(100 * time.Millisecond):
		}
		close(release)
		if r := <-deleted; r.Status != 200 {
			t.Fatalf("delete %d %s", r.Status, r.Raw)
		}
		e.srv.WaitBackground()
		if n := e.scalar(`SELECT count(*) FROM calendar_deliveries WHERE event_id = ?`, id); n != int64(0) {
			t.Errorf("%v delivery rows remain for the deleted event", n)
		}
		return id
	}
	t.Run("ics", func(t *testing.T) {
		m := &fakeMailer{}
		e := newEnv(t, func(c *Config) { c.Mailer = m })
		e.mustOK(e.post("/api/me/calendar-emails", map[string]any{"email": "priya.cal@example.org"}, e.officer()))
		e.srv.WaitBackground()
		id := race(t, e)
		mails := forUID(m.all(), id)
		if len(mails) != 2 || mails[0].Method != "REQUEST" || !strings.Contains(mails[0].ICS, "SEQUENCE:0") ||
			mails[1].Method != "CANCEL" || mails[1].To != mails[0].To || !strings.Contains(mails[1].ICS, "SEQUENCE:1") {
			t.Fatalf("ics %+v", mails)
		}
	})
	t.Run("google", func(t *testing.T) {
		g := newFakeGoogle(t)
		e := newEnv(t, func(c *Config) { g.config(c) })
		connectGoogle(t, e, "user-priya", "priya")
		e.srv.backfillCalendar("user-priya")
		e.srv.WaitBackground()
		before, _ := g.snapshot()
		race(t, e)
		cals, calls := g.snapshot()
		if len(cals["at-priya"]) != len(before["at-priya"])+1 {
			t.Fatalf("google events %v (calls %v)", cals, calls)
		}
		for gid, st := range cals["at-priya"] {
			if _, old := before["at-priya"][gid]; !old && st != "cancelled" {
				t.Fatalf("google event %s left %q (calls %v)", gid, st, calls)
			}
		}
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
	e.srv.backfillCalendar("user-priya")
	e.newEvent("Two accounts")
	e.srv.WaitBackground()
	cals, _ := g.snapshot()
	nA := len(cals["at-a"])
	if nA == 0 {
		t.Fatal("nothing on account A")
	}
	connectGoogle(t, e, "user-priya", "b")
	e.srv.backfillCalendar("user-priya")
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
	e.srv.backfillCalendar("user-priya")
	e.srv.WaitBackground()
	cals, calls = g.snapshot()
	if len(cals["at-a"]) != nA || len(calls) != before {
		t.Fatalf("reconnecting A changed its calendar: %d events, new calls %v", len(cals["at-a"]), calls[before:])
	}
}
