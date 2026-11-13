package api

import (
	"officerdashboard/internal/fiscal"
	"strings"
	"testing"
	"time"
)

func TestTimezonePreferencesAndClubPermissions(t *testing.T) {
	e := newEnv(t)
	initial := e.mustOK(e.get("/api/me/prefs", e.officer()))
	if initial.str("timezoneMode") != "club" || initial.str("effectiveTimezone") != "America/Los_Angeles" {
		t.Fatal(string(initial.Raw))
	}
	club := map[string]any{"location": "Tokyo campus", "clubTimezone": "Asia/Tokyo"}
	for _, who := range []actor{e.officer(), e.director()} {
		expectStatus(t, "club is leadership only", e.do("PUT", "/api/club/settings", club, who), 403)
	}
	e.mustOK(e.do("PUT", "/api/club/settings", club, e.as("user-leadership")))
	if got := e.mustOK(e.get("/api/me/prefs", e.officer())).str("effectiveTimezone"); got != "Asia/Tokyo" {
		t.Fatal(got)
	}
	e.mustOK(e.do("PUT", "/api/me/prefs", map[string]any{"timezoneMode": "manual", "timezone": "Europe/London"}, e.officer()))
	e.mustOK(e.do("PUT", "/api/me/prefs", map[string]any{"widgetLayout": []map[string]any{{"id": "budget", "size": "S"}}}, e.officer()))
	got := e.mustOK(e.get("/api/me/prefs", e.officer()))
	if got.str("effectiveTimezone") != "Europe/London" {
		t.Fatal(string(got.Raw))
	}
	e.mustOK(e.do("PUT", "/api/me/prefs", map[string]any{"timezoneMode": "auto", "timezone": "America/New_York"}, e.officer()))
	got = e.mustOK(e.get("/api/me/prefs", e.officer()))
	if got.str("effectiveTimezone") != "America/New_York" || !strings.Contains(string(got.Raw), `"size":"S"`) {
		t.Fatal(string(got.Raw))
	}
	if got := e.mustOK(e.get("/api/me/prefs", e.director())).str("effectiveTimezone"); got != "Asia/Tokyo" {
		t.Fatal("prefs leaked", got)
	}
	for _, body := range []map[string]any{
		{"timezoneMode": "manual"}, {"timezoneMode": "bad"}, {"timezoneMode": "manual", "timezone": "Local"}, {"timezoneMode": "auto", "timezone": "Mars/Olympus"}, {"timezone": "UTC"},
	} {
		expectStatus(t, "invalid timezone", e.do("PUT", "/api/me/prefs", body, e.officer()), 400)
	}
	e.mustOK(e.do("PUT", "/api/me/prefs", map[string]any{"timezoneMode": "auto", "timezone": ""}, e.officer()))
	if got := e.mustOK(e.get("/api/me/prefs", e.officer())).str("effectiveTimezone"); got != "Asia/Tokyo" {
		t.Fatal("auto fallback", got)
	}
	expectStatus(t, "invalid club", e.do("PUT", "/api/club/settings", map[string]any{"location": "X", "clubTimezone": "Local"}, e.as("user-leadership")), 400)
	var count int
	if err := e.db.R.QueryRow(`SELECT count(*) FROM audit_log WHERE action='club_settings.updated'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("audit", count, err)
	}
}

func TestCSVImportsUseClubZoneAndAccountingStaysPacific(t *testing.T) {
	e := newEnv(t)
	e.mustOK(e.do("PUT", "/api/club/settings", map[string]any{"location": "New York", "clubTimezone": "America/New_York"}, e.as("user-leadership")))
	// The importing officer's personal zone must not override the club zone.
	e.mustOK(e.do("PUT", "/api/me/prefs", map[string]any{"timezoneMode": "manual", "timezone": "Asia/Tokyo"}, e.officer()))
	id := e.mustOK(e.post("/api/events", draftEvent, e.officer())).str("event", "id")
	for kind, csv := range map[string]string{
		"luma_guests": "email,registered at,checked in at\nzone@example.org,2026-10-31 23:30,2026-10-31T23:30:00-08:00",
		"reflections": "email,timestamp\nzone@example.org,2026-10-31 23:30",
	} {
		e.mustOK(e.do("POST", "/api/events/import", formBody{fields: map[string]string{"kind": kind, "eventId": id}, files: []formFile{{"file", "zone.csv", "text/csv", csv}}}, e.officer()))
	}
	naive := time.Date(2026, 11, 1, 3, 30, 0, 0, time.UTC)
	offset := time.Date(2026, 11, 1, 7, 30, 0, 0, time.UTC)
	for query, want := range map[string]int64{
		`SELECT registered_at FROM event_guests WHERE event_id = ?`: naive.UnixMilli(),
		`SELECT checked_in_at FROM event_guests WHERE event_id = ?`: offset.UnixMilli(),
		`SELECT submitted_at FROM reflections WHERE event_id = ?`:   naive.UnixMilli(),
	} {
		if got := e.scalar(query, id); got != want {
			t.Errorf("%s = %v; want %v", query, got, want)
		}
	}
	if got := fiscal.PacificDate(naive); got != "2026-10-31" {
		t.Fatalf("accounting boundary changed: %s", got)
	}
	token := e.mustOK(e.post("/api/admin/tokens", map[string]any{"name": "ingest", "scopes": []string{"ingest:luma"}}, e.director())).str("plaintext")
	expectStatus(t, "JSON ingest requires offset", e.post("/api/ingest/luma", map[string]any{"event": map[string]any{"lumaId": "naive", "name": "Naive", "startsAt": "2026-10-31T23:30:00"}, "guests": []any{}}, bearer(token)), 400)
}
