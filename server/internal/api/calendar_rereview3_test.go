package api

import (
	"context"
	"strings"
	"testing"
)

// Tests for the third re-review of PR #88 (R-10, R-11). The first test of each group is
// the reviewer's probe, extended.

// ─── R-10: a restart with neither transport keeps persisted work ───

// An event delivered by .ics, then a restart with neither SMTP nor Google: an edit, a
// delete or a new event is still queued, as one job, and goes out once SMTP is back.
// (Reviewer probe, plus a second edit, the job count and no resend of the first event.)
func TestRereviewTotalTransportGapKeepsICSChanges(t *testing.T) {
	for _, kind := range []string{"update", "delete", "create"} {
		t.Run(kind, func(t *testing.T) {
			m := &fakeMailer{}
			e := newEnv(t, func(c *Config) { c.Mailer = m })
			e.mustOK(e.post("/api/me/calendar-emails", map[string]any{"email": "priya.cal@example.org"}, e.officer()))
			original := e.newEvent("Delivered before the outage")
			e.srv.WaitBackground()
			full := e.cfg
			restartWith(e, func(c *Config) { c.Mailer = nil; c.Google = nil })

			id := original
			switch kind {
			case "update":
				e.mustOK(e.do("PATCH", "/api/events/"+id, map[string]any{"name": "First edit during the outage"}, e.officer()))
				e.srv.WaitBackground()
				e.mustOK(e.do("PATCH", "/api/events/"+id, map[string]any{"name": "Edited during the outage"}, e.officer()))
			case "delete":
				e.mustOK(e.do("DELETE", "/api/events/"+id, nil, e.officer()))
			case "create":
				id = e.newEvent("Created during the outage")
			}
			e.srv.WaitBackground()
			if n := e.pendingCalendarJobs(); n != 1 {
				t.Fatalf("%s during a total transport gap: %d jobs queued, want 1", kind, n)
			}

			restartWith(e, func(c *Config) { *c = full })
			e.srv.ResumeCalendar()
			e.srv.WaitBackground()
			messages := forUID(m.all(), id)
			switch kind {
			case "update":
				if len(messages) != 2 || !strings.Contains(messages[1].Subject, "Edited during the outage") || !strings.Contains(messages[1].ICS, "SEQUENCE:1") {
					t.Errorf("update was not delivered after recovery: %+v", messages)
				}
			case "delete":
				if len(messages) != 2 || messages[1].Method != "CANCEL" || !strings.Contains(messages[1].ICS, "SEQUENCE:1") {
					t.Errorf("cancellation was not delivered after recovery: %+v", messages)
				}
			case "create":
				if len(messages) != 1 || messages[0].Method != "REQUEST" {
					t.Errorf("new invitation was not delivered after recovery: %+v", messages)
				}
				if got := forUID(m.all(), original); len(got) != 1 {
					t.Errorf("the unchanged first event was resent: %+v", got)
				}
			}
			if n := e.pendingCalendarJobs(); n != 0 {
				t.Errorf("%d jobs left after recovery", n)
			}
		})
	}
}

// The same with Google delivery: the edit, the delete and the new event reach the
// connected account once Google is configured again.
func TestRereviewTotalTransportGapKeepsGoogleChanges(t *testing.T) {
	for _, kind := range []string{"update", "delete", "create"} {
		t.Run(kind, func(t *testing.T) {
			g := newFakeGoogle(t)
			e := newEnv(t, func(c *Config) { g.config(c) })
			connectGoogle(t, e, "user-priya", "a")
			original := e.newEvent("Delivered before the outage")
			e.srv.WaitBackground()
			rows, err := e.db.Read().DeliveriesForEvent(context.Background(), original, "user-priya")
			if err != nil || len(rows) != 1 || rows[0].GoogleEventID == nil {
				t.Fatalf("first delivery %+v %v", rows, err)
			}
			gid := *rows[0].GoogleEventID
			cals, _ := g.snapshot()
			before := len(cals["at-a"])
			full := e.cfg
			restartWith(e, func(c *Config) { c.Mailer = nil; c.Google = nil })

			switch kind {
			case "update":
				e.mustOK(e.do("PATCH", "/api/events/"+original, map[string]any{"name": "Edited during the outage"}, e.officer()))
			case "delete":
				e.mustOK(e.do("DELETE", "/api/events/"+original, nil, e.officer()))
			case "create":
				e.newEvent("Created during the outage")
			}
			e.srv.WaitBackground()
			if n := e.pendingCalendarJobs(); n != 1 {
				t.Fatalf("%s during a total transport gap: %d jobs queued, want 1", kind, n)
			}
			_, callsBefore := g.snapshot()

			restartWith(e, func(c *Config) { *c = full })
			e.srv.ResumeCalendar()
			e.srv.WaitBackground()
			cals, calls := g.snapshot()
			calls = calls[len(callsBefore):]
			switch kind {
			case "update":
				if len(calls) != 1 || calls[0] != "PATCH at-a "+gid+" confirmed" {
					t.Errorf("update was not delivered after recovery: %v", calls)
				}
			case "delete":
				if cals["at-a"][gid] != "cancelled" {
					t.Errorf("event not cancelled after recovery: %v (calls %v)", cals["at-a"], calls)
				}
			case "create":
				if len(cals["at-a"]) != before+1 || len(calls) != 1 || calls[0] != "POST at-a" {
					t.Errorf("new event not created after recovery: %d events, was %d (calls %v)", len(cals["at-a"]), before, calls)
				}
			}
			if n := e.pendingCalendarJobs(); n != 0 {
				t.Errorf("%d jobs left after recovery", n)
			}
		})
	}
}

// A dashboard that never had SMTP or Google: officers can still choose an address or
// hold a grant, but no change leaves work behind, so the outbox stays empty.
func TestRereviewNeverConfiguredDashboardKeepsNoOutbox(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.Mailer = nil; c.Google = nil })
	e.mustOK(e.post("/api/me/calendar-emails", map[string]any{"email": "priya.cal@example.org"}, e.officer()))
	connectGoogle(t, e, "user-marcus", "m")
	e.srv.WaitBackground()
	for i := range 3 {
		id := e.newEvent("Never sent " + string(rune('A'+i)))
		e.srv.WaitBackground()
		e.mustOK(e.do("PATCH", "/api/events/"+id, map[string]any{"name": "Edited"}, e.officer()))
		e.mustOK(e.do("PATCH", "/api/events/"+id, map[string]any{"status": "cancelled"}, e.officer()))
		e.srv.WaitBackground()
		if i > 0 {
			e.mustOK(e.do("DELETE", "/api/events/"+id, nil, e.officer()))
		}
	}
	e.srv.ResumeCalendar()
	e.srv.WaitBackground()
	if n := e.pendingCalendarJobs(); n != 0 {
		t.Fatalf("%d jobs queued on a dashboard with no transport ever", n)
	}
	if rows := e.scalar(`SELECT COUNT(*) FROM calendar_deliveries`); rows != int64(0) {
		t.Fatalf("%v delivery rows written with no transport", rows)
	}
}

// ─── R-11: an email is not proof of the same Google account ───

// A legacy grant keyed by email, then a reconnect whose ID token carries the same email
// (in other case) but a different sub: the new account gets every event, and the old
// account's events are left alone. (Reviewer probe, plus the checks on A and the rows.)
func TestRereviewLegacyEmailAdoptionWithDifferentSub(t *testing.T) {
	g := newFakeGoogle(t)
	e := newEnv(t, func(c *Config) { g.config(c) })
	email := "reassigned@gmail.test"
	id, gid := legacyGoogleUpgrade(t, e, &email)
	e.backfill("user-priya")
	e.srv.WaitBackground()
	cals, _ := g.snapshot()
	n := len(cals["at-a"])
	if n == 0 || n != upcomingEvents(t, e) {
		t.Fatalf("legacy account has %d events, want %d", n, upcomingEvents(t, e))
	}

	if got := oauthConnect(t, e, g, "b", `{"sub":"different-sub","email":"REASSIGNED@gmail.test"}`); got != "/settings?google=connected" {
		t.Fatalf("callback: %s", got)
	}
	cals, calls := g.snapshot()
	if len(cals["at-b"]) != n {
		t.Fatalf("different sub reused the old account's fingerprints: account B has %d events, want %d; calls %v", len(cals["at-b"]), n, calls)
	}
	if len(cals["at-a"]) != n || cals["at-a"][gid] != "confirmed" {
		t.Fatalf("account A changed: %v", cals["at-a"])
	}
	rows, _ := e.db.Read().DeliveriesForEvent(context.Background(), id, "user-priya")
	if len(rows) != 1 || rows[0].Destination != "google:sub:different-sub" || rows[0].GoogleEventID == nil || cals["at-b"][*rows[0].GoogleEventID] != "confirmed" {
		t.Fatalf("rows after reconnect %+v (B %v)", rows, cals["at-b"])
	}
}
