package api

import "testing"

func TestOrgCalendarMeetingsAndLocks(t *testing.T) {
	e := newEnv(t)
	who := e.officer()
	r := e.mustOK(e.get("/api/orgcal/calendars", who))
	if len(r.list("calendars")) != 4 {
		t.Fatal(string(r.Raw))
	}
	body := map[string]any{"calendarKey": "internal", "title": "Planning", "startsAt": "2026-11-24T18:00:00Z", "endsAt": "2026-11-24T19:00:00Z"}
	r = e.mustOK(e.post("/api/orgcal/events", body, who))
	id := r.str("event", "id")
	if id == "" {
		t.Fatal(string(r.Raw))
	}
	r = e.mustOK(e.get("/api/orgcal/events?from=2026-11-01&to=2026-12-01&cal=internal", who))
	if len(r.list("events")) != 1 {
		t.Fatal(string(r.Raw))
	}
	body["calendarKey"] = "events"
	r = e.post("/api/orgcal/events", body, who)
	if r.Status != 403 {
		t.Fatal(r.Status, string(r.Raw))
	}
	e.mustOK(e.do("PATCH", "/api/orgcal/events/"+id, map[string]any{"title": "Revised"}, e.director()))
	e.mustOK(e.do("DELETE", "/api/orgcal/events/"+id, nil, who))
	r = e.mustOK(e.get("/api/orgcal/events?from=2026-11-01&to=2026-12-01&cal=internal", who))
	if len(r.list("events")) != 0 {
		t.Fatal(string(r.Raw))
	}
}
