package api

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"officerdashboard/internal/calendar"
	"officerdashboard/internal/store"
)

// Tests for the second re-review of PR #88 (R-07, R-08, R-09). The first test of each
// group is the reviewer's probe, adopted as is or adapted where the fix changes the setup.

// restartWith points the env at a new server on the same database with cfg changed by
// edit, as a restart with a different environment does.
func restartWith(e *testEnv, edit func(c *Config)) {
	cfg := e.cfg
	edit(&cfg)
	e.srv = New(cfg)
	e.h = e.srv.Handler()
}

// ─── R-07: a missing transport keeps persisted work ───

// A persisted job survives a restart where only SMTP is temporarily absent. Google stays
// configured so the outbox worker still runs. (Reviewer probe.)
func TestRereviewMissingSMTPDoesNotAcknowledgePersistedWork(t *testing.T) {
	for _, kind := range []string{"sync", "status cancel", "delete cancel"} {
		t.Run(kind, func(t *testing.T) {
			m := &flakyMailer{}
			g := newFakeGoogle(t)
			e := newEnv(t, func(c *Config) { c.Mailer = m; g.config(c) })
			e.mustOK(e.post("/api/me/calendar-emails", map[string]any{"email": "priya.cal@example.org"}, e.officer()))
			e.srv.WaitBackground()

			var id string
			switch kind {
			case "sync":
				m.fail.Store(true)
				id = e.newEvent("SMTP configuration gap")
			default:
				id = e.newEvent("SMTP configuration gap")
				e.srv.WaitBackground()
				m.fail.Store(true)
				if kind == "status cancel" {
					e.mustOK(e.do("PATCH", "/api/events/"+id, map[string]any{"status": "cancelled"}, e.officer()))
				} else {
					e.mustOK(e.do("DELETE", "/api/events/"+id, nil, e.officer()))
				}
			}
			e.srv.WaitBackground()
			if n := e.pendingCalendarJobs(); n != 1 {
				t.Fatalf("before restart: %d jobs, want 1", n)
			}

			withoutSMTP := e.cfg
			withoutSMTP.Mailer = nil
			restarted := New(withoutSMTP)
			restarted.ResumeCalendar()
			restarted.WaitBackground()
			if n := e.pendingCalendarJobs(); n != 1 {
				t.Errorf("restart without SMTP acknowledged the job: %d jobs remain", n)
			}

			m.fail.Store(false)
			restored := New(e.cfg)
			restored.ResumeCalendar()
			restored.WaitBackground()
			messages := forUID(m.all(), id)
			want := 1
			if kind != "sync" {
				want = 2
			}
			if len(messages) != want {
				t.Errorf("after SMTP recovery: %d messages, want %d (%+v)", len(messages), want, messages)
			}
			if kind != "sync" && len(messages) == want && messages[1].Method != "CANCEL" {
				t.Errorf("after SMTP recovery: second message %s, want CANCEL", messages[1].Method)
			}
			if n := e.pendingCalendarJobs(); n != 0 {
				t.Errorf("after SMTP recovery: %d jobs left", n)
			}
		})
	}
}

// The same for Google: an event change or a delete made while a restart is missing the
// Google OAuth config stays queued (SMTP keeps the worker running) and reaches the
// account's calendar once Google is configured again.
func TestRereviewMissingGoogleConfigKeepsPersistedWork(t *testing.T) {
	for _, kind := range []string{"sync", "delete cancel"} {
		t.Run(kind, func(t *testing.T) {
			g := newFakeGoogle(t)
			e := newEnv(t, func(c *Config) { c.Mailer = &fakeMailer{}; g.config(c) })
			connectGoogle(t, e, "user-priya", "a")
			id := e.newEvent("Google configuration gap")
			e.srv.WaitBackground()
			cals, _ := g.snapshot()
			grant, err := e.db.Read().GoogleTokenFor(context.Background(), "user-priya")
			if err != nil {
				t.Fatal(err)
			}
			gid := calendar.EventID("event-"+id+"@cseed-dashboard", grant.Destination())
			if cals["at-a"][gid] != "confirmed" {
				t.Fatalf("not on account A: %v", cals["at-a"])
			}
			full := e.cfg
			restartWith(e, func(c *Config) { c.Google = nil })
			if kind == "sync" {
				e.mustOK(e.do("PATCH", "/api/events/"+id, map[string]any{"name": "Renamed during the gap"}, e.officer()))
			} else {
				e.mustOK(e.do("DELETE", "/api/events/"+id, nil, e.officer()))
			}
			e.srv.WaitBackground()
			if n := e.pendingCalendarJobs(); n != 1 {
				t.Fatalf("change during the gap: %d jobs, want 1 kept", n)
			}
			e.srv.ResumeCalendar()
			e.srv.WaitBackground()
			if n := e.pendingCalendarJobs(); n != 1 {
				t.Fatalf("resume without Google acknowledged the job: %d jobs", n)
			}
			_, before := g.snapshot()
			restored := New(full)
			restored.ResumeCalendar()
			restored.WaitBackground()
			cals, calls := g.snapshot()
			want := "PATCH at-a " + gid + " confirmed"
			if kind != "sync" {
				want = "PATCH at-a " + gid + " cancelled"
			}
			if len(calls) != len(before)+1 || calls[len(calls)-1] != want {
				t.Fatalf("after Google is back: calls %v, want one %q", calls[len(before):], want)
			}
			if kind != "sync" && cals["at-a"][gid] != "cancelled" {
				t.Fatalf("event not cancelled on account A: %v", cals["at-a"])
			}
			if n := e.pendingCalendarJobs(); n != 0 {
				t.Fatalf("%d jobs left", n)
			}
		})
	}
}

// The intentional skips stay: on a dashboard that has Google but never had SMTP, an
// officer's .ics address (which never received anything) is not waited for, so events do
// not pile up in the outbox; and a deleted event is not cancelled on a Google account the
// officer has since disconnected.
func TestRereviewNeverConfiguredAndDisconnectedAreSkipped(t *testing.T) {
	t.Run("never configured", func(t *testing.T) {
		g := newFakeGoogle(t)
		e := newEnv(t, func(c *Config) { g.config(c) })
		e.mustOK(e.post("/api/me/calendar-emails", map[string]any{"email": "priya.cal@example.org"}, e.officer()))
		e.srv.WaitBackground()
		e.newEvent("No SMTP here")
		e.srv.WaitBackground()
		if n := e.pendingCalendarJobs(); n != 0 {
			t.Fatalf("%d jobs wait for an SMTP relay this dashboard never had", n)
		}
	})
	t.Run("disconnected", func(t *testing.T) {
		g := newFakeGoogle(t)
		e := newEnv(t, func(c *Config) { c.Mailer = &fakeMailer{}; g.config(c) })
		connectGoogle(t, e, "user-priya", "a")
		id := e.newEvent("Then disconnected")
		e.srv.WaitBackground()
		e.mustOK(e.do("DELETE", "/api/me/calendar/google", nil, e.officer()))
		e.srv.WaitBackground()
		_, before := g.snapshot()
		e.mustOK(e.do("DELETE", "/api/events/"+id, nil, e.officer()))
		e.srv.WaitBackground()
		if _, calls := g.snapshot(); len(calls) != len(before) {
			t.Fatalf("cancelled on a disconnected account: %v", calls[len(before):])
		}
		if n := e.pendingCalendarJobs(); n != 0 {
			t.Fatalf("%d jobs left for a disconnected account", n)
		}
	})
}

// Work that waits for a transport stays one job per event however many edits queue
// behind it: each sync re-reads the event, so a newer one replaces a queued one.
func TestRereviewWaitingSyncIsOneJobPerEvent(t *testing.T) {
	m := &flakyMailer{}
	e := newEnv(t, func(c *Config) { c.Mailer = m })
	e.mustOK(e.post("/api/me/calendar-emails", map[string]any{"email": "priya.cal@example.org"}, e.officer()))
	id := e.newEvent("Edited while the relay is down")
	e.srv.WaitBackground()
	m.fail.Store(true)
	for i := range 5 {
		e.mustOK(e.do("PATCH", "/api/events/"+id, map[string]any{"name": "Edit " + string(rune('A'+i))}, e.officer()))
		e.srv.WaitBackground()
	}
	if n := e.pendingCalendarJobs(); n != 1 {
		t.Fatalf("%d jobs for one event, want 1", n)
	}
	m.fail.Store(false)
	e.srv.ResumeCalendar()
	e.srv.WaitBackground()
	got := forUID(m.all(), id)
	if len(got) != 2 || !strings.Contains(got[1].Subject, "Edit E") || !strings.Contains(got[1].ICS, "SEQUENCE:1") {
		t.Fatalf("after recovery: %+v", got)
	}
}

// ─── R-08: a crash replay reuses the recorded attempt ───

// recordAttemptAndSend plans one .ics delivery (recording its attempt), sends it, and
// stops before its outcome is recorded, as a crash right after the send does.
func recordAttemptAndSend(t *testing.T, e *testEnv, eventID string) {
	t.Helper()
	ctx := context.Background()
	ev, err := e.db.Read().EventByID(ctx, eventID)
	if err != nil {
		t.Fatal(err)
	}
	email := "priya.cal@example.org"
	p, err := e.srv.planDelivery(ctx, ev, recipient{UserID: "user-priya", Mode: "ics", Email: &email})
	if err != nil || p == nil {
		t.Fatalf("plan %v %v", p, err)
	}
	if out := e.srv.deliverOne(ctx, p.r, p.ev, p.prior); out.Status != "sent" {
		t.Fatal(out)
	}
}

// An unresolved send replays the SEQUENCE recorded before it, with the same method and
// subject, rather than turning a crash retry into an update. (Reviewer probe, plus the
// subject check.)
func TestRereviewCrashReplayReusesRecordedSequence(t *testing.T) {
	m := &fakeMailer{}
	e := newEnv(t, func(c *Config) { c.Mailer = m })
	e.mustOK(e.post("/api/me/calendar-emails", map[string]any{"email": "priya.cal@example.org"}, e.officer()))
	e.srv.WaitBackground()
	ctx := context.Background()
	const id = "kimi-sequence-replay"
	e.exec(`INSERT INTO events(id,name,starts_at,status) VALUES(?,?,?,'published')`, id, "Sequence replay", "2026-11-20T18:00:00-08:00")
	if err := e.db.Write(ctx, func(q *store.Queries) error {
		return q.EnqueueCalendar(ctx, store.CalendarJob{Kind: "sync", EventID: id}, e.clock.UnixMilli())
	}); err != nil {
		t.Fatal(err)
	}
	recordAttemptAndSend(t, e, id)
	restarted := New(e.cfg)
	restarted.ResumeCalendar()
	restarted.WaitBackground()
	messages := forUID(m.all(), id)
	if len(messages) != 2 || !strings.Contains(messages[0].ICS, "SEQUENCE:0") || !strings.Contains(messages[1].ICS, "SEQUENCE:0") {
		t.Fatalf("crash replay did not reuse recorded SEQUENCE: %+v", messages)
	}
	for _, msg := range messages {
		if msg.Method != "REQUEST" || msg.Subject != "Invitation: Sequence replay" {
			t.Fatalf("replay is not the same message: %s %q", msg.Method, msg.Subject)
		}
	}
	d, err := e.db.Read().DeliveryFor(ctx, id, "user-priya", "ics:priya.cal@example.org")
	if err != nil || d.Status != "sent" || d.Sequence != 0 || d.Unresolved() || d.AttemptHash != nil {
		t.Fatalf("after replay: %+v %v", d, err)
	}
	if n := e.pendingCalendarJobs(); n != 0 {
		t.Fatalf("%d jobs left", n)
	}
}

// When the event changed between the unresolved send and its retry, the retry is new
// content: it goes out as an update with a SEQUENCE above the attempt, which may have
// arrived. A delete after the replay cancels above that.
func TestRereviewCrashReplayOfChangedEventIsAnUpdate(t *testing.T) {
	m := &fakeMailer{}
	e := newEnv(t, func(c *Config) { c.Mailer = m })
	e.mustOK(e.post("/api/me/calendar-emails", map[string]any{"email": "priya.cal@example.org"}, e.officer()))
	e.srv.WaitBackground()
	ctx := context.Background()
	const id = "rereview-replay-changed"
	e.exec(`INSERT INTO events(id,name,starts_at,status) VALUES(?,?,?,'published')`, id, "Before the crash", "2026-11-20T18:00:00-08:00")
	recordAttemptAndSend(t, e, id)
	e.exec(`UPDATE events SET name='After the crash' WHERE id=?`, id)
	if err := e.db.Write(ctx, func(q *store.Queries) error {
		return q.EnqueueCalendar(ctx, store.CalendarJob{Kind: "sync", EventID: id}, e.clock.UnixMilli())
	}); err != nil {
		t.Fatal(err)
	}
	restartWith(e, func(*Config) {})
	e.srv.ResumeCalendar()
	e.srv.WaitBackground()
	e.mustOK(e.do("DELETE", "/api/events/"+id, nil, e.officer()))
	e.srv.WaitBackground()
	got := forUID(m.all(), id)
	if len(got) != 3 {
		t.Fatalf("%d messages, want REQUEST, update, CANCEL: %+v", len(got), got)
	}
	if !strings.Contains(got[1].ICS, "SEQUENCE:1") || got[1].Subject != "Updated invitation: After the crash" {
		t.Fatalf("replay of changed content: %q %s", got[1].Subject, got[1].ICS)
	}
	if got[2].Method != "CANCEL" || !strings.Contains(got[2].ICS, "SEQUENCE:2") {
		t.Fatalf("delete after replay: %s %s", got[2].Method, got[2].ICS)
	}
}

// ─── R-09: Google destinations are keyed by the account's sub ───

// Two grants without an email claim but with different accounts never share delivery
// state. (Reviewer probe; each grant now carries its ID-token sub, which every grant
// stored since V0.14.1 has: one without is refused, see the next test.)
func TestRereviewGoogleAccountsWithoutEmailDoNotShareDeliveryState(t *testing.T) {
	g := newFakeGoogle(t)
	e := newEnv(t, func(c *Config) { g.config(c) })
	ctx := context.Background()
	connectWithoutEmail := func(account string) {
		t.Helper()
		sealer := calendar.NewSealer("gsecret")
		access := sealer.Seal("at-" + account)
		exp := e.clock.UnixMilli() + 86400000
		sub := "sub-" + account
		if err := e.db.Write(ctx, func(w *store.Queries) error {
			if err := w.SaveGoogleToken(ctx, store.GoogleToken{UserID: "user-priya", AccountSub: &sub, SealedRefresh: sealer.Seal("rt"), SealedAccess: &access, AccessExpires: &exp, ConnectedAt: e.clock.UnixMilli()}); err != nil {
				return err
			}
			return w.SetDeliveryMode(ctx, "user-priya", "google")
		}); err != nil {
			t.Fatal(err)
		}
		e.backfill("user-priya")
		e.srv.WaitBackground()
	}
	connectWithoutEmail("a")
	_ = e.newEvent("Unknown Google account email")
	e.srv.WaitBackground()
	cals, calls := g.snapshot()
	nA := len(cals["at-a"])
	if nA == 0 {
		t.Fatalf("account A did not receive events: %v (%v)", cals, calls)
	}
	connectWithoutEmail("b")
	cals, calls = g.snapshot()
	if len(cals["at-b"]) != nA {
		t.Fatalf("account B reused account A's destination/fingerprint: got %d events, want %d (%v; calls %v)", len(cals["at-b"]), nA, cals, calls)
	}
	before := len(calls)
	connectWithoutEmail("a")
	if cals, calls = g.snapshot(); len(cals["at-a"]) != nA || len(calls) != before {
		t.Fatalf("reconnecting A again changed its calendar: %d events, new calls %v", len(cals["at-a"]), calls[before:])
	}
}

// oauthConnect runs Priya's real connect → consent → callback flow, with Google's token
// endpoint answering for account (access token "at-"+account) with the ID-token claims
// given, and returns where the callback sent her.
func oauthConnect(t *testing.T, e *testEnv, g *fakeGoogle, account, claims string) string {
	t.Helper()
	g.mu.Lock()
	g.grantAccess, g.grantClaims = "at-"+account, claims
	g.mu.Unlock()
	me := e.officer()
	start := e.get("/api/me/calendar/google/connect", me)
	loc, err := url.Parse(start.Header.Get("Location"))
	if start.Status != 302 || err != nil {
		t.Fatalf("connect %d %v", start.Status, err)
	}
	cb := e.get("/auth/google/callback?code=c&state="+loc.Query().Get("state"), actor{cookies: append(setCookies(start), me.cookie)})
	e.srv.WaitBackground()
	return cb.Header.Get("Location")
}

// A token response whose ID token has no sub (missing, malformed, or without the claim)
// is refused: nothing is stored and the officer stays where they were.
func TestRereviewGoogleGrantWithoutSubIsRefused(t *testing.T) {
	for name, claims := range map[string]string{"email only": `{"email":"a@gmail.test"}`, "no claims": `{}`, "not json": `x`} {
		t.Run(name, func(t *testing.T) {
			g := newFakeGoogle(t)
			e := newEnv(t, func(c *Config) { g.config(c) })
			if got := oauthConnect(t, e, g, "a", claims); got != "/settings?google=failed" {
				t.Fatalf("callback → %s", got)
			}
			if _, err := e.db.Read().GoogleTokenFor(context.Background(), "user-priya"); err == nil {
				t.Fatal("grant without an account id was stored")
			}
			if _, calls := g.snapshot(); len(calls) != 0 {
				t.Fatalf("delivered on a refused grant: %v", calls)
			}
		})
	}
	g := newFakeGoogle(t)
	e := newEnv(t, func(c *Config) { g.config(c) })
	if got := oauthConnect(t, e, g, "a", `{"sub":"sub-a"}`); got != "/settings?google=connected" {
		t.Fatalf("grant with a sub and no email → %s", got)
	}
	tok, err := e.db.Read().GoogleTokenFor(context.Background(), "user-priya")
	if err != nil || tok.AccountSub == nil || *tok.AccountSub != "sub-a" || tok.AccountEmail != nil || tok.Destination() != "google:sub:sub-a" {
		t.Fatalf("stored grant %+v %v", tok, err)
	}
}

// upcomingEvents counts the events a backfill delivers.
func upcomingEvents(t *testing.T, e *testEnv) int {
	t.Helper()
	events, err := e.db.Read().Events(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for i := range events {
		if start, _, err := calendar.Window(events[i].StartsAt, events[i].EndsAt); err == nil && start.After(e.srv.now()) && events[i].Status != "cancelled" {
			n++
		}
	}
	return n
}

// legacyGoogleUpgrade sets up an officer upgraded from before V0.14.1: a grant with no
// sub (and the email given, possibly none) that had delivered one event, whose single
// delivery row migration 0027 turns into the grant's legacy destination. It returns the
// event id and its Google event id on account A.
func legacyGoogleUpgrade(t *testing.T, e *testEnv, email *string) (string, string) {
	t.Helper()
	ctx := context.Background()
	sealer := calendar.NewSealer("gsecret")
	access := sealer.Seal("at-a")
	exp := e.clock.Add(24 * time.Hour).UnixMilli()
	if err := e.db.Write(ctx, func(w *store.Queries) error {
		if err := w.SaveGoogleToken(ctx, store.GoogleToken{UserID: "user-priya", AccountEmail: email, SealedRefresh: sealer.Seal("rt"), SealedAccess: &access, AccessExpires: &exp, ConnectedAt: e.clock.UnixMilli()}); err != nil {
			return err
		}
		return w.SetDeliveryMode(ctx, "user-priya", "google")
	}); err != nil {
		t.Fatal(err)
	}
	const id = "legacy-google"
	e.exec(`INSERT INTO events(id,name,starts_at,status) VALUES(?,?,?,'published')`, id, "Legacy Google event", "2026-11-20T18:00:00-08:00")
	ev, _ := e.db.Read().EventByID(ctx, id)
	invite, _ := e.srv.calendarEvent(ev, 0, false)
	out := e.srv.deliverOne(ctx, recipient{UserID: "user-priya", Mode: "google"}, invite, nil)
	if out.Status != "sent" {
		t.Fatal(out)
	}
	r, err := e.srv.recipientFor(ctx, "user-priya")
	if err != nil || r == nil {
		t.Fatalf("recipient %v %v", r, err)
	}
	hash := fingerprint(invite, *r)
	legacyDeliveries(t, e, func() {
		e.exec(`INSERT INTO calendar_deliveries(event_id,user_id,mode,recipient,google_event_id,sequence,status,content_hash,updated_at) VALUES(?,?,'google',NULL,?,0,'sent',?,1)`, id, "user-priya", out.GoogleEventID, hash)
	})
	want := "google:"
	if email != nil {
		want += *email
	}
	rows, err := e.db.Read().DeliveriesForEvent(ctx, id, "user-priya")
	if err != nil || len(rows) != 1 || rows[0].Destination != want {
		t.Fatalf("migrated rows %+v %v", rows, err)
	}
	return id, out.GoogleEventID
}

// An upgraded grant that never had an email: reconnecting the same account updates the
// event it already has (no copy); connecting a different account creates the events
// there and leaves the first account's alone.
func TestRereviewLegacyGrantWithoutEmailReconnects(t *testing.T) {
	t.Run("same account", func(t *testing.T) {
		g := newFakeGoogle(t)
		e := newEnv(t, func(c *Config) { g.config(c) })
		id, gid := legacyGoogleUpgrade(t, e, nil)
		if got := oauthConnect(t, e, g, "a", `{"sub":"sub-a"}`); got != "/settings?google=connected" {
			t.Fatalf("callback → %s", got)
		}
		cals, calls := g.snapshot()
		if n := upcomingEvents(t, e); len(cals["at-a"]) != n || cals["at-a"][gid] != "confirmed" {
			t.Fatalf("account A has %d events, want %d with %s kept (calls %v)", len(cals["at-a"]), n, gid, calls)
		}
		rows, _ := e.db.Read().DeliveriesForEvent(context.Background(), id, "user-priya")
		if len(rows) != 1 || rows[0].Destination != "google:sub:sub-a" || rows[0].GoogleEventID == nil || *rows[0].GoogleEventID != gid {
			t.Fatalf("rows after reconnect %+v", rows)
		}
		e.mustOK(e.do("DELETE", "/api/events/"+id, nil, e.officer()))
		e.srv.WaitBackground()
		if cals, _ := g.snapshot(); cals["at-a"][gid] != "cancelled" {
			t.Fatalf("delete after reconnect: %v", cals["at-a"])
		}
	})
	t.Run("different account", func(t *testing.T) {
		g := newFakeGoogle(t)
		e := newEnv(t, func(c *Config) { g.config(c) })
		_, gid := legacyGoogleUpgrade(t, e, nil)
		if got := oauthConnect(t, e, g, "b", `{"sub":"sub-b"}`); got != "/settings?google=connected" {
			t.Fatalf("callback → %s", got)
		}
		cals, calls := g.snapshot()
		if n := upcomingEvents(t, e); len(cals["at-b"]) != n {
			t.Fatalf("account B has %d events, want %d (calls %v)", len(cals["at-b"]), n, calls)
		}
		if len(cals["at-a"]) != 1 || cals["at-a"][gid] != "confirmed" {
			t.Fatalf("account A changed: %v", cals["at-a"])
		}
	})
}

// An upgraded grant with an email: reconnecting that account (same email, now with its
// sub) moves its deliveries as they are, so nothing is resent.
func TestRereviewLegacyGrantWithEmailReconnectsIdempotently(t *testing.T) {
	g := newFakeGoogle(t)
	e := newEnv(t, func(c *Config) { g.config(c) })
	email := "a@gmail.test"
	id, gid := legacyGoogleUpgrade(t, e, &email)
	// Bring the other upcoming events onto account A under the legacy grant, as before.
	e.backfill("user-priya")
	e.srv.WaitBackground()
	_, before := g.snapshot()
	if got := oauthConnect(t, e, g, "a", `{"sub":"sub-a","email":"A@gmail.test"}`); got != "/settings?google=connected" {
		t.Fatalf("callback → %s", got)
	}
	cals, calls := g.snapshot()
	if len(calls) != len(before) || len(cals["at-a"]) != upcomingEvents(t, e) {
		t.Fatalf("reconnecting the same account resent: new calls %v", calls[len(before):])
	}
	rows, _ := e.db.Read().DeliveriesForEvent(context.Background(), id, "user-priya")
	if len(rows) != 1 || rows[0].Destination != "google:sub:sub-a" || *rows[0].GoogleEventID != gid {
		t.Fatalf("rows after reconnect %+v", rows)
	}
}
