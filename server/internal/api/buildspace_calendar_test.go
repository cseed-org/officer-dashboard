package api

import (
	"strings"
	"testing"

	"officerdashboard/internal/store"
)

func TestBuildspaceCalendarOptInDatesAndNotes(t *testing.T) {
	e := newEnv(t)
	who := e.officer()
	project := map[string]any{"name": "Calendar prototype", "description": "", "track": "Software", "stage": "Building", "links": []string{}, "mentorIds": []string{"bs-mentor-maya"}}
	e.mustOK(e.do("PUT", "/api/buildspace-launchpad/projects/calendar-project", project, who))
	e.mustOK(e.do("PUT", "/api/buildspace-launchpad/people/calendar-person", map[string]any{"name": "Calendar builder", "projectId": "calendar-project", "joinedOn": "2027-03-01"}, who))
	for _, kind := range []string{"absence", "mentor"} {
		body := map[string]any{"projectId": "calendar-project", "personId": "calendar-person", "kind": kind, "date": "2027-03-14", "body": "Private note must not appear in feed", "status": "on-track"}
		if kind == "mentor" {
			body["mentorId"] = "bs-mentor-maya"
		}
		e.mustOK(e.post("/api/buildspace-launchpad/activity", body, who))
	}
	path := "/api/calendar/feed?start=2027-03-08&end=2027-03-15"
	defaults := e.mustOK(e.get("/api/me/calendar-widget", who))
	if len(defaults.list("sources")) != 2 {
		t.Fatal(string(defaults.Raw))
	}
	feed := e.mustOK(e.get(path, who))
	if len(feed.list("sources")) != len(store.CalendarSources) {
		t.Fatal("missing source metadata")
	}
	for _, v := range feed.list("items") {
		if strings.HasPrefix(v.(map[string]any)["source"].(string), "buildspace-") {
			t.Fatal("Buildspace + Launchpad enabled by default")
		}
	}
	ids := []string{"buildspace-absences", "buildspace-weekly", "buildspace-mentor"}
	e.mustOK(e.do("PUT", "/api/me/calendar-widget", map[string]any{"view": "month", "colourBy": "source", "showWeekends": true, "sources": ids}, who))
	saved := e.mustOK(e.get("/api/me/calendar-widget", who))
	if len(saved.list("sources")) != 3 {
		t.Fatal(string(saved.Raw))
	}
	for _, source := range ids {
		got := e.mustOK(e.get(path+"&sources="+source, who))
		found := false
		for _, v := range got.list("items") {
			item := v.(map[string]any)
			if item["source"] != source || item["date"] != "2027-03-14" || item["startsAt"] != nil || strings.Contains(item["title"].(string), "Private") {
				t.Fatal(item)
			}
			if strings.Contains(item["title"].(string), "Calendar prototype") {
				found = true
				if !strings.HasPrefix(item["href"].(string), "/buildspace-launchpad?") {
					t.Fatal(item)
				}
			}
		}
		if !found {
			t.Fatalf("source %s missing project: %s", source, got.Raw)
		}
	}
	// The exclusive end omits Sunday's due date, including across DST changes.
	before := e.mustOK(e.get("/api/calendar/feed?start=2027-03-08&end=2027-03-14&sources=buildspace-weekly", who))
	if len(before.list("items")) != 0 {
		t.Fatal(string(before.Raw))
	}
	e.mustOK(e.post("/api/buildspace-launchpad/activity", map[string]any{"projectId": "calendar-project", "personId": "calendar-person", "kind": "weekly", "date": "2027-03-08", "body": "Weekly progress", "status": "on-track"}, who))
	got := e.mustOK(e.get(path+"&sources=buildspace-weekly", who))
	for _, v := range got.list("items") {
		if strings.Contains(v.(map[string]any)["id"].(string), "calendar-person") {
			t.Fatal("received weekly check-in still due")
		}
	}
	// The following week still needs an update, until the project ships.
	next := "/api/calendar/feed?start=2027-03-15&end=2027-03-22&sources=buildspace-weekly"
	found := false
	for _, v := range e.mustOK(e.get(next, who)).list("items") {
		if strings.Contains(v.(map[string]any)["id"].(string), "calendar-person") {
			found = true
		}
	}
	if !found {
		t.Fatal("next week's deadline missing")
	}
	project["stage"] = "Shipped"
	e.mustOK(e.do("PUT", "/api/buildspace-launchpad/projects/calendar-project", project, who))
	for _, v := range e.mustOK(e.get(next, who)).list("items") {
		if strings.Contains(v.(map[string]any)["id"].(string), "calendar-person") {
			t.Fatal("shipped project still due")
		}
	}
}

// M-044: weekly check-in items for officer-created builders (36-character UUIDs) keep
// CalendarFeedItem.id within 64 characters, stay stable across reads, and link the real ID;
// a builder ID that already fits keeps the readable form.
func TestBuildspaceWeeklyItemIDsFitTheContract(t *testing.T) {
	e := newEnv(t)
	who := e.officer()
	project := map[string]any{"name": "Long ids", "description": "", "track": "Software", "stage": "Building", "links": []string{}, "mentorIds": []string{}}
	e.mustOK(e.do("PUT", "/api/buildspace-launchpad/projects/long-id-project", project, who))
	uuid := "0b3f8c1e-5a47-4d2b-9c61-2f7e8a9d4b10"                                   // crypto.randomUUID(), as the officer form makes
	boundary := strings.Repeat("b", 64-len("buildspace-weekly-")-len("-2027-03-14")) // item id exactly 64 long
	for _, id := range []string{uuid, boundary} {
		e.mustOK(e.do("PUT", "/api/buildspace-launchpad/people/"+id, map[string]any{"name": "Builder " + id[:4], "projectId": "long-id-project", "joinedOn": "2027-03-01"}, who))
	}
	e.mustOK(e.do("PUT", "/api/me/calendar-widget", map[string]any{"view": "month", "colourBy": "source", "showWeekends": true, "sources": []string{"buildspace-weekly"}}, who))
	read := func() map[string]string {
		out := map[string]string{} // href → id
		for _, v := range e.mustOK(e.get("/api/calendar/feed?start=2027-03-08&end=2027-03-15&sources=buildspace-weekly", who)).list("items") {
			item := v.(map[string]any)
			id := item["id"].(string)
			if len(id) > 64 {
				t.Errorf("item id %q is %d characters", id, len(id))
			}
			out[item["href"].(string)] = id
		}
		return out
	}
	first, second := read(), read()
	uuidItem, ok := first["/buildspace-launchpad?person="+uuid]
	if !ok || strings.Contains(uuidItem, uuid) {
		t.Fatalf("UUID builder item %q (items %v)", uuidItem, first)
	}
	if got := first["/buildspace-launchpad?person="+boundary]; got != "buildspace-weekly-"+boundary+"-2027-03-14" || len(got) != 64 {
		t.Errorf("64-character id changed: %q", got)
	}
	for href, id := range first {
		if second[href] != id {
			t.Errorf("id for %s not stable: %q then %q", href, id, second[href])
		}
	}
}
