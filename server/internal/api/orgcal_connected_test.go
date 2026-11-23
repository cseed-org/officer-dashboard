package api

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// GET /api/orgcal/calendars tells every officer whether Google sync is live, without the
// leadership-only account email or error text (#98 follow-up review N-1).
func orgCalendarFlags(t *testing.T, e *testEnv) (connected, problem any, r reply) {
	t.Helper()
	r = e.mustOK(e.get("/api/orgcal/calendars", e.officer()))
	if strings.Contains(string(r.Raw), "org@example.org") || strings.Contains(string(r.Raw), "lastSyncError") {
		t.Fatalf("calendars leaked leadership status: %s", r.Raw)
	}
	return r.at("googleConnected"), r.at("syncProblem"), r
}

func TestOrgCalendarsGoogleNeverConnected(t *testing.T) {
	e := newEnv(t)
	if c, p, r := orgCalendarFlags(t, e); c != false || p != false {
		t.Fatal(string(r.Raw))
	}
}

func TestOrgCalendarsGoogleConnected(t *testing.T) {
	e, _ := orgGoogleEnv(t)
	if err := e.srv.syncOrgCalendar(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c, p, r := orgCalendarFlags(t, e); c != true || p != false {
		t.Fatal(string(r.Raw))
	}
	// A recorded pass failure (not a revocation) keeps the account connected but flags the problem.
	e.srv.recordOrgSync(errors.New("google: 500 backend error"))
	if c, p, r := orgCalendarFlags(t, e); c != true || p != true {
		t.Fatal(string(r.Raw))
	}
	if r := e.mustOK(e.get("/api/settings/org-google/status", e.as("user-leadership"))); !strings.Contains(r.str("lastSyncError"), "backend error") {
		t.Fatal(string(r.Raw))
	}
}

func TestOrgCalendarsGoogleDisconnectedAfterConnecting(t *testing.T) {
	e, _ := orgGoogleEnv(t)
	if err := e.srv.syncOrgCalendar(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.mustOK(e.do("DELETE", "/api/settings/org-google", nil, e.as("user-leadership")))
	c, p, r := orgCalendarFlags(t, e)
	if c != false || p != false {
		t.Fatal(string(r.Raw))
	}
	// The calendar ids stay (so a reconnect picks them up), which is why they can't signal sync.
	for _, cal := range r.list("calendars") {
		if id, _ := cal.(map[string]any)["googleCalendarId"].(string); id == "" {
			t.Fatalf("calendar id cleared on disconnect: %s", r.Raw)
		}
	}
}

func TestOrgCalendarsGoogleRevoked(t *testing.T) {
	e, f := orgGoogleEnv(t)
	f.mu.Lock()
	f.revoked = true
	f.mu.Unlock()
	e.clock = e.clock.Add(2 * time.Hour) // access token expired: the pass must refresh
	_ = e.srv.syncOrgCalendar(context.Background())
	if c, p, r := orgCalendarFlags(t, e); c != false || p != true {
		t.Fatal(string(r.Raw))
	}
}
