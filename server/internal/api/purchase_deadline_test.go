package api

import (
	"strings"
	"testing"
	"time"
)

// Deadlines are date-only: "today" is the officer's effective wall-clock date, so the
// same instant allows or rejects a date depending on the time zone in Settings.
func TestPurchaseDeadlineCreateRejectsPastDatesInOfficerTimezone(t *testing.T) {
	e := newEnv(t)
	who := e.officer()
	// 05:00Z on Nov 1 is still Oct 31 in Los Angeles but already Nov 1 in Tokyo.
	e.clock = time.Date(2026, 11, 1, 5, 0, 0, 0, time.UTC)

	bad := e.post("/api/requests", with(purchase, "neededBy", "2026-10-30"), who)
	expectStatus(t, "past deadline", bad, 400)
	if !strings.Contains(string(bad.Raw), "Choose today or a later date") || bad.at("error", "details", "fieldErrors", "neededBy") == nil {
		t.Fatalf("field error: %s", bad.Raw)
	}
	expectStatus(t, "impossible date", e.post("/api/requests", with(purchase, "neededBy", "2026-02-30"), who), 400)
	expectStatus(t, "datetime is not a date", e.post("/api/requests", with(purchase, "neededBy", "2026-12-01T00:00:00Z"), who), 400)

	today := e.mustOK(e.post("/api/requests", with(purchase, "neededBy", "2026-10-31"), who))
	if today.str("request", "neededBy") != "2026-10-31" {
		t.Fatal(string(today.Raw))
	}
	none := e.mustOK(e.post("/api/requests", with(purchase, "neededBy", nil), who))
	if none.at("request", "neededBy") != nil {
		t.Fatal(string(none.Raw))
	}

	e.mustOK(e.do("PUT", "/api/me/prefs", map[string]any{"timezoneMode": "manual", "timezone": "Asia/Tokyo"}, who))
	expectStatus(t, "yesterday in Tokyo", e.post("/api/requests", with(purchase, "neededBy", "2026-10-31"), who), 400)
	e.mustOK(e.post("/api/requests", with(purchase, "neededBy", "2026-11-01"), who))
}

func TestPurchaseDeadlineEditFollowsDraftPermissionsAndIsAudited(t *testing.T) {
	e := newEnv(t)
	who := e.officer()
	id := e.mustOK(e.post("/api/requests", with(purchase, "neededBy", "2026-11-20"), who)).str("request", "id")

	expectStatus(t, "other officer edits", e.do("PATCH", "/api/requests/"+id, map[string]any{"neededBy": "2026-11-21"}, e.as("user-marcus")), 403)
	expectStatus(t, "move into the past", e.do("PATCH", "/api/requests/"+id, map[string]any{"neededBy": "2026-10-01"}, who), 400)

	moved := e.mustOK(e.do("PATCH", "/api/requests/"+id, map[string]any{"neededBy": "2026-11-22"}, who))
	if moved.str("request", "neededBy") != "2026-11-22" {
		t.Fatal(string(moved.Raw))
	}
	audit, _ := e.scalar(`SELECT before_json || '|' || after_json FROM audit_log WHERE action='request.updated' AND entity_id=? ORDER BY created_at DESC, rowid DESC LIMIT 1`, id).(string)
	if !strings.Contains(audit, `"neededBy":"2026-11-20"`) || !strings.Contains(audit, `"neededBy":"2026-11-22"`) {
		t.Fatalf("audit: %s", audit)
	}

	// A deadline that has since passed may stay while other fields change.
	if _, err := e.db.W.Exec(`UPDATE purchase_requests SET needed_by='2026-10-15' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	kept := e.mustOK(e.do("PATCH", "/api/requests/"+id, map[string]any{"title": "Retitled", "neededBy": "2026-10-15"}, who))
	if kept.str("request", "neededBy") != "2026-10-15" || kept.str("request", "title") != "Retitled" {
		t.Fatal(string(kept.Raw))
	}
	cleared := e.mustOK(e.do("PATCH", "/api/requests/"+id, map[string]any{"neededBy": nil}, who))
	if cleared.at("request", "neededBy") != nil {
		t.Fatal(string(cleared.Raw))
	}

	e.mustOK(e.post("/api/requests/"+id+"/submit", map[string]any{}, who))
	expectStatus(t, "submitted request edit", e.do("PATCH", "/api/requests/"+id, map[string]any{"neededBy": "2026-12-01"}, who), 409)
}

func TestPurchaseRequestListSortsByDeadline(t *testing.T) {
	e := newEnv(t)
	who := e.officer()
	for _, d := range []string{"2026-12-05", "2026-11-02", "2026-11-02"} {
		e.clock = e.clock.Add(time.Minute)
		e.mustOK(e.post("/api/requests", with(purchase, "neededBy", d, "title", "sort "+d), who))
	}
	e.clock = e.clock.Add(time.Minute)
	e.mustOK(e.post("/api/requests", with(purchase, "title", "undated"), who))

	newest := e.mustOK(e.get("/api/requests?scope=mine", who))
	if newest.list("items")[0].(map[string]any)["title"] != "undated" {
		t.Fatal("default order changed")
	}

	var dates []string
	cursor := ""
	for page := 0; page < 10; page++ {
		path := "/api/requests?scope=all&sort=deadline&limit=2"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		r := e.mustOK(e.get(path, who))
		for _, v := range r.list("items") {
			dates = append(dates, v.(map[string]any)["neededBy"].(string))
		}
		next, _ := r.at("nextCursor").(string)
		if next == "" {
			break
		}
		cursor = next
	}
	// Three new rows plus the four seeded deadlines; undated rows are excluded.
	want := []string{"2026-09-12", "2026-11-02", "2026-11-02", "2026-11-06", "2026-11-12", "2026-11-20", "2026-12-05"}
	if strings.Join(dates, ",") != strings.Join(want, ",") {
		t.Fatalf("deadline order %v", dates)
	}
	expectStatus(t, "bad deadline cursor", e.get("/api/requests?sort=deadline&cursor=2026-13-01|x", who), 400)
	expectStatus(t, "bad sort", e.get("/api/requests?sort=amount", who), 400)
}

func TestPurchaseDeadlineCalendarSource(t *testing.T) {
	e := newEnv(t)
	priya, marcus, director := e.officer(), e.as("user-marcus"), e.director()
	mk := func(who actor, title, date string, submit bool) string {
		return e.mustOK(e.post("/api/requests", with(purchase, "title", title, "neededBy", date, "submit", submit), who)).str("request", "id")
	}
	mk(priya, "first-day", "2026-11-01", true)
	mk(priya, "last-day", "2026-11-30", true)
	mk(priya, "after-range", "2026-12-01", true) // end is exclusive
	mk(priya, "priya-draft", "2026-11-15", false)
	mk(marcus, "marcus-draft", "2026-11-15", false)
	denied := mk(priya, "denied", "2026-11-16", true)
	e.mustOK(e.post("/api/requests/"+denied+"/deny", map[string]any{"comment": "no"}, director))
	cancelled := mk(priya, "cancelled", "2026-11-17", true)
	e.mustOK(e.post("/api/requests/"+cancelled+"/transition", map[string]any{"to": "cancelled"}, director))
	bought := mk(priya, "bought", "2026-11-18", true)
	e.mustOK(e.post("/api/requests/"+bought+"/approve", map[string]any{}, director))
	e.mustOK(e.post("/api/requests/"+bought+"/transition", map[string]any{"to": "fulfilling"}, director))
	buying := mk(priya, "buying", "2026-11-19", true)
	e.mustOK(e.post("/api/requests/"+buying+"/approve", map[string]any{}, director))
	e.mustOK(e.post("/api/requests/"+buying+"/transition", map[string]any{"to": "fulfilling"}, director))
	e.mustOK(e.post("/api/requests/"+bought+"/transition", map[string]any{"to": "fulfilled", "actualTotalCents": 10000}, director))

	titles := func(who actor, query string) map[string]map[string]any {
		r := e.mustOK(e.get("/api/calendar/feed?start=2026-11-01&end=2026-12-01"+query, who))
		out := map[string]map[string]any{}
		for _, v := range r.list("items") {
			item := v.(map[string]any)
			if item["source"] == "purchase-deadlines" {
				out[item["title"].(string)] = item
			}
		}
		return out
	}
	got := titles(priya, "&sources=purchase-deadlines")
	// "BuildHer workshop snacks" is seeded as approved, due 2026-11-06.
	for _, title := range []string{"first-day", "last-day", "priya-draft", "buying", "BuildHer workshop snacks"} {
		if got[title] == nil {
			t.Fatalf("missing %q in %v", title, got)
		}
	}
	for _, title := range []string{"after-range", "marcus-draft", "denied", "cancelled", "bought"} {
		if got[title] != nil {
			t.Fatalf("unexpected %q", title)
		}
	}
	first := got["first-day"]
	if first["date"] != "2026-11-01" || first["startsAt"] != nil || first["type"] != "submitted" ||
		!strings.HasPrefix(first["href"].(string), "/finance?id=") || !strings.Contains(first["detail"].(string), "$100.00 · pending") {
		t.Fatal(first)
	}
	if got["buying"]["detail"] == nil || !strings.HasSuffix(got["buying"]["detail"].(string), "· buying") {
		t.Fatal(got["buying"])
	}

	// Date-only: the deadline keeps its date in any time zone.
	e.mustOK(e.do("PUT", "/api/me/prefs", map[string]any{"timezoneMode": "manual", "timezone": "Pacific/Kiritimati"}, priya))
	if d := titles(priya, "&sources=purchase-deadlines")["first-day"]; d == nil || d["date"] != "2026-11-01" {
		t.Fatal(d)
	}
	// Single-day window includes only that day.
	one := e.mustOK(e.get("/api/calendar/feed?start=2026-11-30&end=2026-12-01&sources=purchase-deadlines", priya))
	if len(one.list("items")) != 1 || one.list("items")[0].(map[string]any)["title"] != "last-day" {
		t.Fatal(string(one.Raw))
	}

	// Default visibility: on for directors and leadership, off for officers.
	if titles(priya, "")["first-day"] != nil {
		t.Fatal("officer default should exclude purchase deadlines")
	}
	if titles(director, "")["first-day"] == nil || titles(e.as("user-leadership"), "")["first-day"] == nil {
		t.Fatal("director/leadership default should include purchase deadlines")
	}
	for who, want := range map[string]bool{"user-priya": false, "user-director": true, "user-leadership": true} {
		opts := e.mustOK(e.get("/api/me/calendar-widget", e.as(who)))
		has := false
		for _, s := range opts.list("sources") {
			has = has || s == "purchase-deadlines"
		}
		if has != want {
			t.Fatalf("%s default sources %s", who, opts.Raw)
		}
	}
	// Officers can opt in through saved options.
	e.mustOK(e.do("PUT", "/api/me/calendar-widget", map[string]any{"view": "auto", "colourBy": "source", "showWeekends": true, "sources": []string{"purchase-deadlines"}}, priya))
	feed := e.mustOK(e.get("/api/calendar/feed?start=2026-11-01&end=2026-11-02", priya))
	found := false
	for _, s := range feed.list("sources") {
		m := s.(map[string]any)
		if m["id"] == "purchase-deadlines" {
			found = m["icon"] == "wallet" && m["hue"] == "green"
		}
	}
	if !found {
		t.Fatal(string(feed.Raw))
	}
	expectStatus(t, "anonymous feed", e.get("/api/calendar/feed?start=2026-11-01&end=2026-11-02&sources=purchase-deadlines", anon), 401)
}
