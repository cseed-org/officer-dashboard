package api

import (
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
	want := `{"widgetLayout":[{"id":"kpi-pending","size":"2x1"},{"id":"recent-events","size":"4x2"},{"id":"activity","size":"1x2"},{"id":"future-widget","size":"4x1"}]}`
	if strings.TrimSpace(string(got.Raw)) != want {
		t.Fatalf("order or sizes changed:\n got %s\nwant %s", got.Raw, want)
	}
	// An old open tab still saves V0.3 sizes.
	e.mustOK(e.do("PUT", "/api/me/prefs", map[string]any{"widgetLayout": []any{map[string]any{"id": "kpis", "size": "L"}}}, e.officer()))
	for _, bad := range []string{"3x1", "2x3", "XL", "1X1", ""} {
		expectStatus(t, "size "+bad, e.do("PUT", "/api/me/prefs", map[string]any{"widgetLayout": []any{map[string]any{"id": "budget", "size": bad}}}, e.officer()), 400)
	}
}
