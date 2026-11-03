package api

import (
	"context"
	"fmt"
	"officerdashboard/internal/store"
	"testing"
)

func TestCalendarFeedRangesSourcesAndTimezones(t *testing.T) {
	e := newEnv(t)
	officer := e.officer()
	// Fall-back day in LA is 25 hours: both 01:30 occurrences belong to Nov 1.
	for i, stamp := range []string{"2026-11-01T01:30:00-07:00", "2026-11-01T01:30:00-08:00", "2026-11-02T00:00:00-08:00", "2026-10-31T23:59:00-07:00"} {
		e.mustOK(e.post("/api/events", map[string]any{"name": fmt.Sprintf("boundary-%d", i), "startsAt": stamp}, officer))
	}
	e.mustOK(e.post("/api/events", map[string]any{"name": "overnight", "startsAt": "2026-10-31T23:00:00-07:00", "endsAt": "2026-11-01T02:00:00-08:00", "lumaUrl": "https://lu.ma/calendar-test", "location": "Atrium"}, officer))
	e.mustOK(e.post("/api/design-requests", map[string]any{"title": "calendar-deadline", "type": "poster", "brief": "A dated poster request", "dueDate": "2026-11-01"}, officer))
	path := "/api/calendar/feed?start=2026-11-01&end=2026-11-02"
	feed := e.mustOK(e.get(path, officer))
	counts := map[string]int{}
	for _, v := range feed.list("items") {
		item := v.(map[string]any)
		name := item["title"].(string)
		counts[name]++
		if name == "overnight" && (item["date"] != "2026-10-31" || item["lumaUrl"] != "https://lu.ma/calendar-test" || item["location"] != "Atrium" || item["href"] == "") {
			t.Fatal(item)
		}
		if name == "calendar-deadline" && (item["date"] != "2026-11-01" || item["startsAt"] != nil) {
			t.Fatal(item)
		}
	}
	if counts["boundary-0"] != 1 || counts["boundary-1"] != 1 || counts["boundary-2"] != 0 || counts["boundary-3"] != 0 || counts["overnight"] != 1 || counts["calendar-deadline"] != 1 {
		t.Fatal(counts)
	}
	for _, source := range []string{"events", "design"} {
		r := e.mustOK(e.get(path+"&sources="+source, officer))
		for _, v := range r.list("items") {
			if v.(map[string]any)["source"] != source {
				t.Fatal(string(r.Raw))
			}
		}
	}
	if r := e.mustOK(e.get(path+"&sources=none", officer)); len(r.list("items")) != 0 {
		t.Fatal(string(r.Raw))
	}
	e.mustOK(e.do("PUT", "/api/me/prefs", map[string]any{"timezoneMode": "manual", "timezone": "Asia/Tokyo"}, officer))
	tokyo := e.mustOK(e.get(path, officer))
	if tokyo.str("timezone") != "Asia/Tokyo" {
		t.Fatal(string(tokyo.Raw))
	}
	for _, v := range tokyo.list("items") {
		if v.(map[string]any)["date"] != "2026-11-01" {
			t.Fatal(v)
		}
	}
	for _, query := range []string{"start=2026-11-02&end=2026-11-01", "start=2026-11-01&end=2026-11-01", "start=2026-02-30&end=2026-03-02", "start=2026-01-01&end=2027-01-01", "start=2026-11-01&end=2026-11-02&sources=google"} {
		expectStatus(t, "invalid range/source", e.get("/api/calendar/feed?"+query, officer), 400)
	}
	expectStatus(t, "private feed", e.get(path, anon), 401)
}

func TestCalendarFeedSpringForwardAndExclusiveEnd(t *testing.T) {
	e := newEnv(t)
	who := e.officer()
	for i, stamp := range []string{"2027-03-14T00:00:00-08:00", "2027-03-14T23:59:00-07:00", "2027-03-15T00:00:00-07:00"} {
		e.mustOK(e.post("/api/events", map[string]any{"name": fmt.Sprintf("spring-%d", i), "startsAt": stamp}, who))
	}
	e.mustOK(e.post("/api/events", map[string]any{"name": "ends-at-start", "startsAt": "2027-03-13T23:00:00-08:00", "endsAt": "2027-03-14T00:00:00-08:00"}, who))
	got := e.mustOK(e.get("/api/calendar/feed?start=2027-03-14&end=2027-03-15&sources=events", who))
	names := map[string]bool{}
	for _, v := range got.list("items") {
		names[v.(map[string]any)["title"].(string)] = true
	}
	if !names["spring-0"] || !names["spring-1"] || names["spring-2"] || names["ends-at-start"] {
		t.Fatal(string(got.Raw))
	}
}

func TestCalendarWidgetSettingsArePrivateAndValidated(t *testing.T) {
	e := newEnv(t)
	body := map[string]any{"view": "month", "colourBy": "type", "showWeekends": false, "events": false, "design": true}
	e.mustOK(e.do("PUT", "/api/me/calendar-widget", body, e.officer()))
	got := e.mustOK(e.get("/api/me/calendar-widget", e.officer()))
	if got.str("view") != "month" || got.at("showWeekends") != false || len(got.list("sources")) != 1 {
		t.Fatal(string(got.Raw))
	}
	// Directors also get purchase deadlines by default.
	other := e.mustOK(e.get("/api/me/calendar-widget", e.director()))
	if other.str("view") != "auto" || len(other.list("sources")) != 3 {
		t.Fatal(string(other.Raw))
	}
	body["view"] = "bad"
	expectStatus(t, "invalid settings", e.do("PUT", "/api/me/calendar-widget", body, e.officer()), 400)
	expectStatus(t, "anonymous settings", e.get("/api/me/calendar-widget", anon), 401)
}

// A source registered by another feature must participate without modifying the API
// source switch, settings validation, or metadata response.
func TestCalendarSourceRegistryExtension(t *testing.T) {
	e := newEnv(t)
	prior := store.CalendarSources
	defer func() { store.CalendarSources = prior }()
	var calls int
	store.CalendarSources = append(append([]store.CalendarSource{}, prior...), store.CalendarSource{
		ID: "extension", Label: "Extension dates", Icon: "check", Hue: "teal",
		Load: func(ctx context.Context, q *store.Queries, window store.CalendarRange) ([]store.CalendarItem, error) {
			calls++
			if window.UserID != "user-priya" || window.FirstDate != "2026-11-01" || window.EndDate != "2026-11-02" {
				t.Fatalf("loader scope: %+v", window)
			}
			return []store.CalendarItem{{ID: "demo", Title: "Extension date", Date: window.FirstDate, Type: "due", Href: "/buildspace"}}, nil
		},
	})
	result := e.mustOK(e.get("/api/calendar/feed?start=2026-11-01&end=2026-11-02&sources=extension", e.officer()))
	if calls != 1 || len(result.list("items")) != 1 || result.list("items")[0].(map[string]any)["source"] != "extension" || len(result.list("sources")) != len(prior)+1 {
		t.Fatal(string(result.Raw))
	}
	e.mustOK(e.do("PUT", "/api/me/calendar-widget", map[string]any{"view": "auto", "colourBy": "source", "showWeekends": true, "sources": []string{"extension"}}, e.officer()))
	settings := e.mustOK(e.get("/api/me/calendar-widget", e.officer()))
	if settings.list("sources")[0] != "extension" {
		t.Fatal(string(settings.Raw))
	}
	e.mustOK(e.get("/api/calendar/feed?start=2026-11-01&end=2026-11-02&sources=none", e.officer()))
	if calls != 1 {
		t.Fatal("disabled source was queried")
	}
	expectStatus(t, "unknown source preference", e.do("PUT", "/api/me/calendar-widget", map[string]any{"view": "auto", "colourBy": "source", "showWeekends": true, "sources": []string{"missing"}}, e.officer()), 400)
}
