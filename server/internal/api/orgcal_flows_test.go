package api

import (
	"testing"
)

func TestOrgCalendarTentativeProjectionAndFixedTransition(t *testing.T) {
	e := newEnv(t)
	who := e.officer()
	r := e.mustOK(e.post("/api/events", map[string]any{"name": "Unscheduled", "tentativeFrom": "2026-11-24", "tentativeTo": "2026-11-28"}, who))
	id := r.str("event", "id")
	if r.str("event", "startsAt") != "" || r.str("event", "tentativeFrom") != "2026-11-24" {
		t.Fatal(string(r.Raw))
	}
	r = e.mustOK(e.get("/api/orgcal/events?from=2026-11-01&to=2026-12-01&cal=events", who))
	found := false
	for _, v := range r.list("events") {
		item := v.(map[string]any)
		if item["sourceId"] == id {
			found = true
			if item["startsAt"] != nil || item["tentativeTo"] != "2026-11-28" || item["sourceHref"] != "/events?id="+id {
				t.Fatal(item)
			}
		}
	}
	if !found {
		t.Fatal("no projection")
	}
	e.mustOK(e.do("PATCH", "/api/events/"+id, map[string]any{"startsAt": "2026-11-25T18:00:00Z", "endsAt": "2026-11-25T19:00:00Z"}, who))
	r = e.mustOK(e.get("/api/orgcal/events?from=2026-11-01&to=2026-12-01&cal=events", who))
	for _, v := range r.list("events") {
		item := v.(map[string]any)
		if item["sourceId"] == id && (item["tentativeFrom"] != nil || item["startsAt"] != "2026-11-25T18:00:00Z") {
			t.Fatal(item)
		}
	}
	e.mustOK(e.do("DELETE", "/api/events/"+id, nil, who))
	r = e.mustOK(e.get("/api/orgcal/events?from=2026-11-01&to=2026-12-01&cal=events", who))
	for _, v := range r.list("events") {
		if v.(map[string]any)["sourceId"] == id {
			t.Fatal("deleted source still visible")
		}
	}
}
func TestOrgCalendarProgramsExceptionsAndDST(t *testing.T) {
	e := newEnv(t)
	who := e.officer()
	r := e.mustOK(e.post("/api/programs", map[string]any{"trackId": "buildspace", "name": "Weekly", "weekday": 0, "startTime": "10:00", "durationMin": 60, "startsOn": "2026-10-25", "endsOn": "2026-11-08"}, who))
	id := r.str("program", "id")
	r = e.mustOK(e.get("/api/orgcal/events?from=2026-10-25&to=2026-11-09&cal=programs", who))
	if len(r.list("events")) != 3 {
		t.Fatal(string(r.Raw))
	}
	if r.str("events", 0, "startsAt") != "2026-10-25T10:00:00-07:00" || r.str("events", 1, "startsAt") != "2026-11-01T10:00:00-08:00" {
		t.Fatal(string(r.Raw))
	}
	e.mustOK(e.post("/api/programs/"+id+"/exceptions", map[string]any{"onDate": "2026-11-01", "kind": "skip"}, who))
	e.mustOK(e.post("/api/programs/"+id+"/exceptions", map[string]any{"onDate": "2026-11-08", "kind": "move", "newStartsAt": "2026-11-10T18:00:00Z", "newEndsAt": "2026-11-10T19:00:00Z"}, who))
	r = e.mustOK(e.get("/api/orgcal/events?from=2026-11-01&to=2026-11-12&cal=programs", who))
	if len(r.list("events")) != 1 || r.str("events", 0, "startsAt") != "2026-11-10T18:00:00Z" {
		t.Fatal(string(r.Raw))
	}
	r = e.do("PATCH", "/api/orgcal/events/program-"+id, map[string]any{"title": "Bypass"}, who)
	if r.Status != 403 {
		t.Fatal(r.Status)
	}
	e.mustOK(e.do("DELETE", "/api/programs/"+id+"/exceptions?onDate=2026-11-08", nil, who))
	e.mustOK(e.do("DELETE", "/api/programs/"+id, nil, who))
}
func TestOrgCalendarServiceTokenScopesAndAudit(t *testing.T) {
	e := newEnv(t)
	token := e.mustOK(e.post("/api/admin/tokens", map[string]any{"name": "calendar", "scopes": []string{"calendar:write"}}, e.director())).str("plaintext")
	r := e.mustOK(e.post("/api/orgcal/events", map[string]any{"calendarKey": "external", "title": "Agent meeting", "tentativeFrom": "2026-11-24", "tentativeTo": "2026-11-28"}, bearer(token)))
	if r.str("event", "id") == "" {
		t.Fatal(string(r.Raw))
	}
	e.mustOK(e.get("/api/orgcal/events?from=2026-11-01&to=2026-12-01", bearer(token)))
	read := e.mustOK(e.post("/api/admin/tokens", map[string]any{"name": "read", "scopes": []string{"calendar:read"}}, e.director())).str("plaintext")
	e.mustOK(e.get("/api/orgcal/calendars", bearer(read)))
	r = e.post("/api/orgcal/events", map[string]any{}, bearer(read))
	if r.Status != 403 {
		t.Fatal(r.Status, string(r.Raw))
	}
}
