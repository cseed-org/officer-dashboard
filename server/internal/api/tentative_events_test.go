package api

import (
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
)

// tentativeEvent creates a published Event with no fixed date, hosted by Priya.
func tentativeEvent(t *testing.T, e *testEnv) string {
	t.Helper()
	r := e.mustOK(e.post("/api/events", map[string]any{"name": "Tentative mixer", "startsAt": nil, "tentativeFrom": "2026-11-24", "tentativeTo": "2026-11-30", "status": "published", "hostUserId": "user-priya"}, e.officer()))
	return r.str("event", "id")
}

func findByID(r reply, list, id string) map[string]any {
	for _, v := range r.list(list) {
		if m, _ := v.(map[string]any); m != nil && m["id"] == id {
			return m
		}
	}
	return nil
}

func TestTentativeEventInLookups(t *testing.T) {
	e := newEnv(t)
	id := tentativeEvent(t, e)
	r := e.mustOK(e.get("/api/lookups", e.officer()))
	ev := findByID(r, "events", id)
	if ev == nil || ev["startsAt"] != "" || ev["tentativeFrom"] != "2026-11-24" || ev["tentativeTo"] != "2026-11-30" {
		t.Fatal(string(r.Raw))
	}
	for _, v := range r.list("events") {
		m := v.(map[string]any)
		if _, ok := m["tentativeFrom"]; !ok {
			t.Fatal("every lookup event carries tentativeFrom", m)
		}
	}
}

func TestTentativeEventIsSearchable(t *testing.T) {
	e := newEnv(t)
	id := tentativeEvent(t, e)
	r := e.mustOK(e.get("/api/search?q=tentative+mixer", e.officer()))
	ev := findByID(r, "results", id)
	if ev == nil || !strings.Contains(ev["subtitle"].(string), "Tentative") {
		t.Fatal(string(r.Raw))
	}
}

func TestTentativeEventInPersonActivity(t *testing.T) {
	e := newEnv(t)
	before := e.mustOK(e.get("/api/officers/user-priya/activity", e.officer())).num("stats", "eventsHosted")
	id := tentativeEvent(t, e)
	r := e.mustOK(e.get("/api/officers/user-priya/activity", e.officer()))
	ev := findByID(r, "items", id)
	if ev == nil || ev["at"] != "" || ev["status"] != "tentative" || ev["tentativeFrom"] != "2026-11-24" || ev["tentativeTo"] != "2026-11-30" {
		t.Fatal(string(r.Raw))
	}
	for _, v := range r.list("items") {
		if _, ok := v.(map[string]any)["tentativeFrom"]; !ok {
			t.Fatal("every activity item carries tentativeFrom", v)
		}
	}
	// Time-based stats leave tentative events out: it has not been hosted.
	if r.num("stats", "eventsHosted") != before {
		t.Fatal(string(r.Raw))
	}
}

func TestTentativeEventOnContact(t *testing.T) {
	e := newEnv(t)
	id := tentativeEvent(t, e)
	c := e.mustOK(e.post("/api/contacts", map[string]any{"name": "Ada Example", "type": "both", "eventIds": []string{id}}, e.officer()))
	cid := c.str("contact", "id")
	for _, path := range []string{"/api/contacts/" + cid, "/api/contacts"} {
		r := e.mustOK(e.get(path, e.officer()))
		if !strings.Contains(string(r.Raw), `"startsAt":"","tentativeFrom":"2026-11-24","tentativeTo":"2026-11-30"`) {
			t.Fatal(path, string(r.Raw))
		}
	}
}

func TestTentativeEventStatsAndFeed(t *testing.T) {
	e := newEnv(t)
	priya := e.officer()
	before := e.mustOK(e.get("/api/me/stats", priya))
	tentativeEvent(t, e)
	r := e.mustOK(e.get("/api/me/stats", priya))
	if string(r.Raw) != string(before.Raw) {
		t.Fatal(string(r.Raw))
	}
	// Tentative Events appear on the org Events calendar, not the Home/overlay feed.
	r = e.mustOK(e.get("/api/calendar/feed?start=2026-11-01&end=2026-12-31", priya))
	if strings.Contains(string(r.Raw), "Tentative mixer") {
		t.Fatal(string(r.Raw))
	}
}

// Every GET route must survive a tentative Event in the database.
func TestTentativeEventSweepAllGetRoutes(t *testing.T) {
	e := newEnv(t)
	id := tentativeEvent(t, e)
	c := e.mustOK(e.post("/api/contacts", map[string]any{"name": "Ada Example", "type": "both", "eventIds": []string{id}}, e.officer()))
	src, err := os.ReadFile("routes.go")
	if err != nil {
		t.Fatal(err)
	}
	params := map[string]string{"id": id, "contactId": c.str("contact", "id")}
	query := url.Values{"q": {"tentative"}, "from": {"2026-11-01"}, "to": {"2026-12-31"}, "start": {"2026-11-01"}, "end": {"2026-12-31"}}.Encode()
	pattern := regexp.MustCompile(`h\("GET (/api/[^"]+)"`)
	param := regexp.MustCompile(`\{([a-zA-Z]+)\}`)
	n := 0
	for _, m := range pattern.FindAllStringSubmatch(string(src), -1) {
		path := param.ReplaceAllStringFunc(m[1], func(p string) string {
			if v, ok := params[strings.Trim(p, "{}")]; ok {
				return url.PathEscape(v)
			}
			return "missing"
		})
		if strings.Contains(path, "/officers/") {
			path = strings.Replace(path, "/"+id, "/user-priya", 1)
		}
		for _, who := range []actor{e.director(), e.officer()} {
			r := e.get(path+"?"+query, who)
			// 503 is a not-configured integration (Google OAuth), not a failure.
			if r.Status >= 500 && r.Status != 503 {
				t.Errorf("GET %s: %d %s", path, r.Status, r.Raw)
			}
		}
		n++
	}
	if n < 40 {
		t.Fatalf("only %d GET routes found", n)
	}
}
