package api

import (
	"encoding/json"
	"officerdashboard/internal/orgcal"
	"sort"
	"strings"
	"testing"
	"time"
)

// F-1: a Google-rejected roster email must not stop pushes or pulls.
func TestOrgCalendarACLFailureDoesNotBlockSync(t *testing.T) {
	e, f := orgGoogleEnv(t)
	f.mu.Lock()
	f.failACLInsert = true
	f.mu.Unlock()
	e.mustOK(e.post("/api/orgcal/events", map[string]any{"calendarKey": "internal", "title": "Still pushed", "startsAt": "2026-11-24T18:00:00Z", "endsAt": "2026-11-24T19:00:00Z"}, e.officer()))
	e.srv.WaitBackground()
	f.mu.Lock()
	defer f.mu.Unlock()
	pushed, pulled := false, false
	aclCalendars := map[string]bool{}
	for _, r := range f.requests {
		if r.Method == "POST" && r.Path == "/calendars/internal/events" && r.Body["summary"] == "Still pushed" {
			pushed = true
		}
		if r.Method == "GET" && r.Path == "/calendars/internal/events" {
			pulled = true
		}
		if r.Method == "POST" && strings.HasSuffix(r.Path, "/acl") {
			aclCalendars[strings.Split(r.Path, "/")[2]] = true
		}
	}
	if !pushed || !pulled {
		t.Fatalf("pushed=%v pulled=%v %v", pushed, pulled, f.requests)
	}
	// A failing address is skipped, not fatal: every calendar's ACL was still attempted.
	if len(aclCalendars) != 4 {
		t.Fatal(aclCalendars)
	}
}

func programStarts(t *testing.T, r reply) []time.Time {
	t.Helper()
	var out []time.Time
	for i := range r.list("events") {
		v, err := time.Parse(time.RFC3339Nano, r.str("events", i, "startsAt"))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	return out
}

// Regression: exceptions match the Los Angeles date of evening sessions (02:00Z the next day).
func TestOrgCalendarEveningProgramSkipAndMove(t *testing.T) {
	e := newEnv(t)
	who := e.officer()
	r := e.mustOK(e.post("/api/programs", map[string]any{"trackId": "buildspace", "name": "Evening lab", "weekday": 2, "startTime": "18:00", "durationMin": 60, "startsOn": "2026-11-01", "endsOn": "2026-11-30"}, who))
	id := r.str("program", "id")
	e.mustOK(e.post("/api/programs/"+id+"/exceptions", map[string]any{"onDate": "2026-11-10", "kind": "skip"}, who))
	e.mustOK(e.post("/api/programs/"+id+"/exceptions", map[string]any{"onDate": "2026-11-17", "kind": "move", "newStartsAt": "2026-11-19T02:00:00Z", "newEndsAt": "2026-11-19T03:00:00Z"}, who))
	r = e.mustOK(e.get("/api/orgcal/events?from=2026-11-01&to=2026-12-01&cal=programs", who))
	la := orgTZ()
	var got []string
	for _, v := range programStarts(t, r) {
		got = append(got, v.In(la).Format("01-02 15:04"))
	}
	sort.Strings(got) // moved sessions are appended after the series
	if strings.Join(got, ",") != "11-03 18:00,11-18 18:00,11-24 18:00" {
		t.Fatal(got)
	}
	for i := range r.list("events") {
		if r.str("events", i, "sourceHref") != "/buildspace-launchpad?tab=programs&program="+id {
			t.Fatal(r.str("events", i, "sourceHref"))
		}
	}
}

// F-3: an instance already at the right time, reported in another offset form, is not re-patched.
func TestOrgCalendarProgramInstanceTimesCompareAsInstants(t *testing.T) {
	e, f := orgGoogleEnv(t)
	who := e.officer()
	original := orgcal.Time{DateTime: "2026-11-03T18:00:00Z", TimeZone: "America/Los_Angeles"}
	f.instances = []orgcal.Event{{ID: "instance-3", Summary: "Program", Status: "confirmed", OriginalStartTime: &original,
		Start: orgcal.Time{DateTime: "2026-11-03T10:00:00-08:00"}, End: orgcal.Time{DateTime: "2026-11-03T11:00:00-08:00"}}}
	r := e.mustOK(e.post("/api/programs", map[string]any{"trackId": "buildspace", "name": "Program", "weekday": 2, "startTime": "10:00", "durationMin": 60, "startsOn": "2026-11-01", "endsOn": "2026-11-30"}, who))
	e.srv.WaitBackground()
	e.mustOK(e.post("/api/programs/"+r.str("program", "id")+"/exceptions", map[string]any{"onDate": "2026-11-10", "kind": "skip"}, who))
	e.srv.WaitBackground()
	f.mu.Lock()
	defer f.mu.Unlock()
	listed := false
	for _, req := range f.requests {
		if strings.HasSuffix(req.Path, "/instances") {
			listed = true
		}
		if req.Method == "PATCH" && strings.HasSuffix(req.Path, "/instance-3") {
			t.Fatal("unchanged instance re-patched", req)
		}
	}
	if !listed {
		t.Fatal("instances were not checked", f.requests)
	}
}

// F-4: a tentative meeting cannot recur.
func TestOrgCalendarTentativeMeetingRejectsRRule(t *testing.T) {
	e := newEnv(t)
	who := e.officer()
	r := e.post("/api/orgcal/events", map[string]any{"calendarKey": "internal", "title": "T", "tentativeFrom": "2026-11-24", "tentativeTo": "2026-11-28", "rrule": "RRULE:FREQ=WEEKLY"}, who)
	if r.Status != 400 {
		t.Fatal(r.Status, string(r.Raw))
	}
	id := createWeeklySeries(t, e)
	r = e.do("PATCH", "/api/orgcal/events/"+id, map[string]any{"startsAt": nil, "tentativeFrom": "2027-03-01", "tentativeTo": "2027-03-05"}, who)
	if r.Status != 400 || !strings.Contains(string(r.Raw), "rrule") {
		t.Fatal(r.Status, string(r.Raw))
	}
	r = e.mustOK(e.do("PATCH", "/api/orgcal/events/"+id, map[string]any{"startsAt": nil, "tentativeFrom": "2027-03-01", "tentativeTo": "2027-03-05", "rrule": nil}, who))
	if r.at("event", "rrule") != nil {
		t.Fatal(string(r.Raw))
	}
}

// D-6: service tokens and MCP agents do not send invites unless they ask to.
func TestOrgCalendarAgentWritesDefaultInvitesOff(t *testing.T) {
	e := newEnv(t)
	token := e.mustOK(e.post("/api/admin/tokens", map[string]any{"name": "calendar", "scopes": []string{"calendar:write"}}, e.director())).str("plaintext")
	body := map[string]any{"calendarKey": "external", "title": "Agent meeting", "startsAt": "2026-11-24T18:00:00Z", "endsAt": "2026-11-24T19:00:00Z"}
	r := e.mustOK(e.post("/api/orgcal/events", body, bearer(token)))
	if r.at("event", "sendInvites") != false {
		t.Fatal(string(r.Raw))
	}
	body["sendInvites"] = true
	r = e.mustOK(e.post("/api/orgcal/events", body, bearer(token)))
	id := r.str("event", "id")
	if r.at("event", "sendInvites") != true {
		t.Fatal(string(r.Raw))
	}
	// An agent edit that does not mention invites stops sending them.
	if r = e.mustOK(e.do("PATCH", "/api/orgcal/events/"+id, map[string]any{"title": "Renamed"}, bearer(token))); r.at("event", "sendInvites") != false {
		t.Fatal(string(r.Raw))
	}
	// Humans keep the External default.
	delete(body, "sendInvites")
	if r = e.mustOK(e.post("/api/orgcal/events", body, e.officer())); r.at("event", "sendInvites") != true {
		t.Fatal(string(r.Raw))
	}
	_, tokens := connectAgent(t, e, "user-priya", "Calendar agent")
	text, isErr := toolCall(e, tokens.str("access_token"), "orgcal_create", map[string]any{"body": map[string]any{"calendarKey": "external", "title": "MCP meeting", "startsAt": "2026-11-25T18:00:00Z", "endsAt": "2026-11-25T19:00:00Z"}})
	var out struct {
		Event struct {
			SendInvites *bool `json:"sendInvites"`
		} `json:"event"`
	}
	if isErr || json.Unmarshal([]byte(text), &out) != nil || out.Event.SendInvites == nil || *out.Event.SendInvites {
		t.Fatal(isErr, text)
	}
}
