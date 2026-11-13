package api

import (
	"officerdashboard/internal/store"
	"strings"
	"testing"
)

// V0.4 Home block grid: W×H sizes round-trip, the default is block-sized, and V0.3 S/M/L
// layouts stay readable and writable so the web client can migrate them.
func TestHomeGridBlockSizes(t *testing.T) {
	e := newEnv(t)
	def := e.mustOK(e.get("/api/me/prefs", e.officer()))
	if !strings.Contains(string(def.Raw), `{"id":"kpi-upcoming","size":"1x1"}`) || strings.Contains(string(def.Raw), `"kpis"`) {
		t.Fatalf("default layout is not block-sized: %s", def.Raw)
	}
	layout := []any{
		map[string]any{"id": "kpi-pending", "size": "2x1"},
		map[string]any{"id": "recent-events", "size": "4x2"},
		map[string]any{"id": "activity", "size": "1x2"},
		map[string]any{"id": "future-widget", "size": "4x1"},
	}
	e.mustOK(e.do("PUT", "/api/me/prefs", map[string]any{"widgetLayout": layout}, e.officer()))
	got := e.mustOK(e.get("/api/me/prefs", e.officer()))
	want := `"widgetLayout":[{"id":"kpi-pending","size":"2x1"},{"id":"recent-events","size":"4x2"},{"id":"activity","size":"1x2"},{"id":"future-widget","size":"4x1"}]`
	if !strings.Contains(string(got.Raw), want) {
		t.Fatalf("order or sizes changed:\n got %s\nwant %s", got.Raw, want)
	}
	// An old open tab still saves V0.3 sizes.
	e.mustOK(e.do("PUT", "/api/me/prefs", map[string]any{"widgetLayout": []any{map[string]any{"id": "kpis", "size": "L"}}}, e.officer()))
	for _, bad := range []string{"3x1", "2x3", "XL", "1X1", ""} {
		expectStatus(t, "size "+bad, e.do("PUT", "/api/me/prefs", map[string]any{"widgetLayout": []any{map[string]any{"id": "budget", "size": bad}}}, e.officer()), 400)
	}
}

func TestDashboardTutorialAndLinks(t *testing.T) {
	e := newEnv(t)
	initial := e.mustOK(e.get("/api/me/prefs", e.officer()))
	if !strings.Contains(string(initial.Raw), `"dashboardTutorialSeen":false`) || !strings.Contains(string(initial.Raw), `"id":"links","size":"2x1"`) {
		t.Fatal(string(initial.Raw))
	}
	e.mustOK(e.do("PUT", "/api/me/prefs", map[string]any{"dashboardTutorialSeen": true}, e.officer()))
	e.mustOK(e.do("PUT", "/api/me/prefs", map[string]any{"timezoneMode": "club"}, e.officer()))
	got := e.mustOK(e.get("/api/me/prefs", e.officer()))
	if !strings.Contains(string(got.Raw), `"dashboardTutorialSeen":true`) || !strings.Contains(string(got.Raw), `"id":"links","size":"2x1"`) {
		t.Fatal(string(got.Raw))
	}
	other := e.mustOK(e.get("/api/me/prefs", e.director()))
	if !strings.Contains(string(other.Raw), `"dashboardTutorialSeen":false`) {
		t.Fatal("tutorial status leaked", string(other.Raw))
	}
	defaults := e.mustOK(e.get("/api/club/settings", e.officer()))
	if defaults.str("websiteUrl") != store.DefaultWebsiteURL || defaults.str("notionUrl") != store.DefaultNotionURL || defaults.str("driveUrl") != store.DefaultDriveURL {
		t.Fatal("missing club defaults", string(defaults.Raw))
	}
	body := map[string]any{"clubTimezone": "UTC", "location": "Club", "websiteUrl": "https://example.org/club", "notionUrl": "", "driveUrl": ""}
	expectStatus(t, "officer cannot change links", e.do("PUT", "/api/club/settings", body, e.officer()), 403)
	e.mustOK(e.do("PUT", "/api/club/settings", body, e.as("user-leadership")))
	e.mustOK(e.do("PUT", "/api/club/settings", map[string]any{"clubTimezone": "UTC", "location": "New club"}, e.as("user-leadership")))
	links := e.mustOK(e.get("/api/club/settings", e.officer()))
	if links.str("websiteUrl") != "https://example.org/club" {
		t.Fatal("timezone update erased links", string(links.Raw))
	}
	if links.str("notionUrl") != store.DefaultNotionURL || links.str("driveUrl") != store.DefaultDriveURL {
		t.Fatal("blank links must use defaults", string(links.Raw))
	}
	body["websiteUrl"] = ""
	reset := e.mustOK(e.do("PUT", "/api/club/settings", body, e.as("user-leadership")))
	if reset.str("websiteUrl") != store.DefaultWebsiteURL {
		t.Fatal("clear must restore default", string(reset.Raw))
	}
	for _, bad := range []string{"javascript:alert(1)", "/relative", "https://user:pass@example.org", "https:///missing-host"} {
		body["websiteUrl"] = bad
		expectStatus(t, "unsafe link", e.do("PUT", "/api/club/settings", body, e.as("user-leadership")), 400)
	}
}

func TestSidebarWidthPref(t *testing.T) {
	e := newEnv(t)
	if got := e.mustOK(e.get("/api/me/prefs", e.officer())); !strings.Contains(string(got.Raw), `"sidebarWidth":0`) {
		t.Fatal("default width", string(got.Raw))
	}
	put := e.mustOK(e.do("PUT", "/api/me/prefs", map[string]any{"sidebarWidth": 280}, e.officer()))
	if !strings.Contains(string(put.Raw), `"sidebarWidth":280`) || !strings.Contains(string(put.Raw), `"id":"links"`) {
		t.Fatal(string(put.Raw))
	}
	// A timezone-only update leaves the width alone; another user is unaffected.
	e.mustOK(e.do("PUT", "/api/me/prefs", map[string]any{"timezoneMode": "club"}, e.officer()))
	if got := e.mustOK(e.get("/api/me/prefs", e.officer())); !strings.Contains(string(got.Raw), `"sidebarWidth":280`) {
		t.Fatal("width lost", string(got.Raw))
	}
	if got := e.mustOK(e.get("/api/me/prefs", e.director())); !strings.Contains(string(got.Raw), `"sidebarWidth":0`) {
		t.Fatal("width leaked", string(got.Raw))
	}
	e.mustOK(e.do("PUT", "/api/me/prefs", map[string]any{"sidebarWidth": 64}, e.officer()))
	e.mustOK(e.do("PUT", "/api/me/prefs", map[string]any{"sidebarWidth": 0}, e.officer()))
	for _, bad := range []any{63, 321, -1, "wide", 100.5} {
		expectStatus(t, "width", e.do("PUT", "/api/me/prefs", map[string]any{"sidebarWidth": bad}, e.officer()), 400)
	}
}
