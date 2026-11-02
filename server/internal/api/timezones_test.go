package api

import (
	"strings"
	"testing"
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
