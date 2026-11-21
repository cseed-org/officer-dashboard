package api

import (
	"context"
	"officerdashboard/internal/orgcal"
	"strings"
	"testing"
)

func TestOrgCalendarTentativeGoogleRoundTripThenFixed(t *testing.T) {
	e, _ := orgGoogleEnv(t)
	who := e.officer()
	r := e.mustOK(e.post("/api/orgcal/events", map[string]any{"calendarKey": "internal", "title": "Planning", "tentativeFrom": "2026-11-24", "tentativeTo": "2026-11-28"}, who))
	id := r.str("event", "id")
	e.srv.WaitBackground()
	r = e.mustOK(e.get("/api/orgcal/events?from=2026-11-01&to=2026-12-01&cal=internal", who))
	if r.str("events", 0, "title") != "Planning" || r.str("events", 0, "tentativeTo") != "2026-11-28" || r.at("events", 0, "startsAt") != nil {
		t.Fatal(string(r.Raw))
	}
	e.mustOK(e.do("PATCH", "/api/orgcal/events/"+id, map[string]any{"startsAt": "2026-11-25T18:00:00Z", "endsAt": "2026-11-25T19:00:00Z"}, who))
	e.srv.WaitBackground()
	r = e.mustOK(e.get("/api/orgcal/events?from=2026-11-01&to=2026-12-01&cal=internal", who))
	if r.str("events", 0, "title") != "Planning" || r.at("events", 0, "tentativeFrom") != nil || r.str("events", 0, "startsAt") != "2026-11-25T18:00:00Z" {
		t.Fatal(string(r.Raw))
	}
}

func TestOrgCalendarAllDayRecurrenceAcrossFallDST(t *testing.T) {
	e := newEnv(t)
	who := e.officer()
	e.mustOK(e.post("/api/orgcal/events", map[string]any{"calendarKey": "internal", "title": "All day", "startsAt": "2026-10-25", "endsAt": "2026-10-26", "allDay": true, "rrule": "RRULE:FREQ=WEEKLY;UNTIL=20261102T235959Z"}, who))
	r := e.mustOK(e.get("/api/orgcal/events?from=2026-11-01&to=2026-11-03&cal=internal", who))
	if len(r.list("events")) != 1 || r.str("events", 0, "endsAt") != "2026-11-02" {
		t.Fatal(string(r.Raw))
	}
}
func TestOrgCalendarImportedRecurrenceCanBeEditedWithoutTimezoneDrift(t *testing.T) {
	e, f := orgGoogleEnv(t)
	f.events["internal"] = []orgcal.Event{{ID: "london", ETag: "v1", Summary: "London", Status: "confirmed", Start: orgcal.Time{DateTime: "2027-02-02T10:00:00Z", TimeZone: "Europe/London"}, End: orgcal.Time{DateTime: "2027-02-02T11:00:00Z", TimeZone: "Europe/London"}, Recurrence: []string{"RRULE:FREQ=WEEKLY;BYDAY=TU;COUNT=10"}}}
	if err := e.srv.syncOrgCalendar(context.Background()); err != nil {
		t.Fatal(err)
	}
	r := e.mustOK(e.get("/api/orgcal/events?from=2027-02-01&to=2027-04-01&cal=internal", e.officer()))
	id := r.str("events", 0, "id")
	e.mustOK(e.do("PATCH", "/api/orgcal/events/"+id, map[string]any{"title": "Updated London", "rrule": "RRULE:FREQ=WEEKLY;BYDAY=TU;COUNT=10"}, e.officer()))
	e.srv.WaitBackground()
	f.mu.Lock()
	defer f.mu.Unlock()
	found := false
	for _, r := range f.requests {
		if r.Method == "PATCH" && r.Path == "/calendars/internal/events/london" {
			found = true
			if r.Body["start"].(map[string]any)["timeZone"] != "Europe/London" {
				t.Fatal(r.Body)
			}
		}
	}
	if !found {
		t.Fatal("no title update")
	}
}
func TestOrgCalendarProgramRepairsOutOfRangeAndContentDrift(t *testing.T) {
	e, f := orgGoogleEnv(t)
	original := orgcal.Time{DateTime: "2026-11-24T10:00:00-08:00", TimeZone: "America/Los_Angeles"}
	f.instances = []orgcal.Event{{ID: "moved-outside", Summary: "Unauthorized title", Description: "Unauthorized description", Start: orgcal.Time{DateTime: "2026-12-05T10:00:00-08:00"}, End: orgcal.Time{DateTime: "2026-12-05T11:00:00-08:00"}, OriginalStartTime: &original, Status: "confirmed"}}
	e.mustOK(e.post("/api/programs", map[string]any{"trackId": "buildspace", "name": "Source title", "description": "Source description", "weekday": 2, "startTime": "10:00", "durationMin": 60, "startsOn": "2026-11-01", "endsOn": "2026-11-30"}, e.officer()))
	e.srv.WaitBackground()
	f.mu.Lock()
	defer f.mu.Unlock()
	fixed := false
	for _, r := range f.requests {
		if strings.HasSuffix(r.Path, "/instances") && (strings.Contains(r.Query, "timeMin") || strings.Contains(r.Query, "timeMax")) {
			t.Fatal("actual-date filter hides moved instances")
		}
		if r.Method == "PATCH" && r.Path == "/calendars/programs/events/moved-outside" {
			fixed = true
			if r.Body["summary"] != "Source title" || r.Body["description"] != "Source description" || r.Body["start"].(map[string]any)["dateTime"] != "2026-11-24T10:00:00-08:00" {
				t.Fatal(r.Body)
			}
		}
	}
	if !fixed {
		t.Fatal("drift not repaired")
	}
}
