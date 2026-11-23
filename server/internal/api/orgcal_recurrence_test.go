package api

import (
	"officerdashboard/internal/store"
	"strings"
	"testing"
)

func TestOrgRecurrenceValidation(t *testing.T) {
	monday := "2027-01-04T18:00:00Z" // Monday 10:00 in America/Los_Angeles
	cases := []struct {
		rule, start string
		allDay      bool
		ok          bool
	}{
		{"RRULE:FREQ=WEEKLY", monday, false, true},
		{"RRULE:FREQ=WEEKLY;UNTIL=20270301T000000Z", monday, false, true},
		{"RRULE:FREQ=WEEKLY;UNTIL=20270301", monday, false, true},
		{"RRULE:FREQ=DAILY", monday, false, true},
		{"RRULE:FREQ=DAILY;INTERVAL=3;COUNT=10", monday, false, true},
		{"RRULE:FREQ=WEEKLY;INTERVAL=2", monday, false, true},
		{"RRULE:FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE;COUNT=12", monday, false, true},
		{"RRULE:FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR", monday, false, true},
		{"RRULE:FREQ=MONTHLY;COUNT=500", monday, false, true},
		{"RRULE:FREQ=MONTHLY;INTERVAL=52;UNTIL=20300101T000000Z", monday, false, true},
		{"RRULE:FREQ=WEEKLY;BYDAY=MO", "2027-01-04", true, true},
		// Rejected shapes.
		{"FREQ=WEEKLY", monday, false, false},
		{"RRULE:FREQ=YEARLY", monday, false, false},
		{"RRULE:FREQ=HOURLY", monday, false, false},
		{"RRULE:INTERVAL=2", monday, false, false},
		{"RRULE:FREQ=WEEKLY;FREQ=DAILY", monday, false, false},
		{"RRULE:FREQ=WEEKLY;INTERVAL=0", monday, false, false},
		{"RRULE:FREQ=WEEKLY;INTERVAL=53", monday, false, false},
		{"RRULE:FREQ=WEEKLY;INTERVAL=x", monday, false, false},
		{"RRULE:FREQ=WEEKLY;COUNT=0", monday, false, false},
		{"RRULE:FREQ=WEEKLY;COUNT=501", monday, false, false},
		{"RRULE:FREQ=WEEKLY;COUNT=3;UNTIL=20270301T000000Z", monday, false, false},
		{"RRULE:FREQ=WEEKLY;UNTIL=2027-03-01", monday, false, false},
		{"RRULE:FREQ=DAILY;BYDAY=MO", monday, false, false},
		{"RRULE:FREQ=MONTHLY;BYDAY=MO", monday, false, false},
		{"RRULE:FREQ=MONTHLY;BYMONTHDAY=15", monday, false, false},
		{"RRULE:FREQ=MONTHLY;BYSETPOS=1", monday, false, false},
		{"RRULE:FREQ=WEEKLY;BYDAY=1MO", monday, false, false},
		{"RRULE:FREQ=WEEKLY;BYDAY=XX", monday, false, false},
		{"RRULE:FREQ=WEEKLY;BYDAY=MO,MO", monday, false, false},
		{"RRULE:FREQ=WEEKLY;BYDAY=", monday, false, false},
		{"RRULE:FREQ=WEEKLY;BYDAY=TU,WE", monday, false, false}, // start must be an occurrence
		{"RRULE:FREQ=WEEKLY;WKST=SU", monday, false, false},
		{"RRULE:FREQ=WEEKLY;", monday, false, false},
		{"RRULE:FREQ=WEEKLY\nEXDATE:20270111T100000", monday, false, false},
		{"RRULE:freq=weekly", monday, false, false},
	}
	for _, c := range cases {
		start := c.start
		e := &store.OrgEvent{StartsAt: &start, AllDay: c.allDay}
		err := validateOrgRecurrence(c.rule, e)
		if (err == nil) != c.ok {
			t.Errorf("%q: err=%v, want ok=%v", c.rule, err, c.ok)
		}
	}
}

func TestOrgCalendarBiweeklyMeetingListsAndPushes(t *testing.T) {
	e, f := orgGoogleEnv(t)
	who := e.officer()
	rule := "RRULE:FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE;COUNT=6"
	r := e.mustOK(e.post("/api/orgcal/events", map[string]any{"calendarKey": "internal", "title": "Biweekly", "startsAt": "2027-03-01T18:00:00Z", "endsAt": "2027-03-01T19:00:00Z", "rrule": rule}, who))
	if r.str("event", "rrule") != rule {
		t.Fatal(string(r.Raw))
	}
	e.srv.WaitBackground()
	r = e.mustOK(e.get("/api/orgcal/events?from=2027-03-01&to=2027-05-01&cal=internal", who))
	got := []string{}
	for i := range r.list("events") {
		got = append(got, r.str("events", i, "startsAt"))
	}
	// Spring DST on 2027-03-14 keeps 10:00 local time.
	want := "2027-03-01T10:00:00-08:00 2027-03-03T10:00:00-08:00 2027-03-15T10:00:00-07:00 2027-03-17T10:00:00-07:00 2027-03-29T10:00:00-07:00 2027-03-31T10:00:00-07:00"
	if strings.Join(got, " ") != want {
		t.Fatalf("got %v", got)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	pushed := false
	for _, req := range f.requests {
		if rec, ok := req.Body["recurrence"].([]any); ok && len(rec) == 1 && rec[0] == rule {
			pushed = true
		}
	}
	if !pushed {
		t.Fatal(f.requests)
	}
}

func TestOrgCalendarRejectsUnsupportedManualRecurrence(t *testing.T) {
	e := newEnv(t)
	r := e.post("/api/orgcal/events", map[string]any{"calendarKey": "internal", "title": "Yearly", "startsAt": "2027-03-01T18:00:00Z", "endsAt": "2027-03-01T19:00:00Z", "rrule": "RRULE:FREQ=YEARLY"}, e.officer())
	if r.Status != 400 || !strings.Contains(string(r.Raw), "DAILY") {
		t.Fatal(r.Status, string(r.Raw))
	}
}

func TestOrgCalendarMovedStartMustMatchByday(t *testing.T) {
	e := newEnv(t)
	who := e.officer()
	r := e.mustOK(e.post("/api/orgcal/events", map[string]any{"calendarKey": "internal", "title": "MW", "startsAt": "2027-03-01T18:00:00Z", "endsAt": "2027-03-01T19:00:00Z", "rrule": "RRULE:FREQ=WEEKLY;BYDAY=MO,WE"}, who))
	id := r.str("event", "id")
	// Tuesday is not in BYDAY.
	if r := e.do("PATCH", "/api/orgcal/events/"+id, map[string]any{"startsAt": "2027-03-02T18:00:00Z", "endsAt": "2027-03-02T19:00:00Z", "moveSeriesStart": true}, who); r.Status != 400 {
		t.Fatal(r.Status, string(r.Raw))
	}
	// Without moveSeriesStart the series keeps its Monday date.
	if r := e.mustOK(e.do("PATCH", "/api/orgcal/events/"+id, map[string]any{"startsAt": "2027-03-02T18:00:00Z", "endsAt": "2027-03-02T19:00:00Z"}, who)); r.str("event", "startsAt") != "2027-03-01T18:00:00Z" {
		t.Fatal(string(r.Raw))
	}
	e.mustOK(e.do("PATCH", "/api/orgcal/events/"+id, map[string]any{"startsAt": "2027-03-03T18:00:00Z", "endsAt": "2027-03-03T19:00:00Z", "moveSeriesStart": true}, who))
}

// createWeeklySeries starts a Monday 10:00 Los Angeles series on 2027-03-01.
func createWeeklySeries(t *testing.T, e *testEnv) string {
	t.Helper()
	r := e.mustOK(e.post("/api/orgcal/events", map[string]any{"calendarKey": "internal", "title": "Weekly", "startsAt": "2027-03-01T18:00:00Z", "endsAt": "2027-03-01T19:00:00Z", "rrule": "RRULE:FREQ=WEEKLY"}, e.officer()))
	return r.str("event", "id")
}

func orgStarts(t *testing.T, e *testEnv) (starts []string, r reply) {
	t.Helper()
	r = e.mustOK(e.get("/api/orgcal/events?from=2027-03-01&to=2027-04-01&cal=internal", e.officer()))
	for i := range r.list("events") {
		starts = append(starts, r.str("events", i, "startsAt"))
	}
	return starts, r
}

func TestOrgCalendarOccurrencesCarrySeriesTimes(t *testing.T) {
	e := newEnv(t)
	createWeeklySeries(t, e)
	e.mustOK(e.post("/api/orgcal/events", map[string]any{"calendarKey": "internal", "title": "Once", "startsAt": "2027-03-02T18:00:00Z", "endsAt": "2027-03-02T19:00:00Z"}, e.officer()))
	_, r := orgStarts(t, e)
	for i := range r.list("events") {
		if r.str("events", i, "title") == "Once" {
			if r.at("events", i, "seriesStartsAt") != nil || r.at("events", i, "seriesEndsAt") != nil {
				t.Fatal(string(r.Raw))
			}
			continue
		}
		if r.str("events", i, "seriesStartsAt") != "2027-03-01T18:00:00Z" || r.str("events", i, "seriesEndsAt") != "2027-03-01T19:00:00Z" {
			t.Fatal(i, string(r.Raw))
		}
	}
}

func TestOrgCalendarPatchFromOccurrenceKeepsSeriesDate(t *testing.T) {
	e := newEnv(t)
	id := createWeeklySeries(t, e)
	// The drawer opened the 2027-03-15 occurrence and moved it to 11:00 (after spring DST).
	r := e.mustOK(e.do("PATCH", "/api/orgcal/events/"+id, map[string]any{"startsAt": "2027-03-15T18:00:00Z", "endsAt": "2027-03-15T19:30:00Z"}, e.officer()))
	if r.str("event", "startsAt") != "2027-03-01T19:00:00Z" || r.str("event", "endsAt") != "2027-03-01T20:30:00Z" || r.str("event", "seriesStartsAt") != "2027-03-01T19:00:00Z" {
		t.Fatal(string(r.Raw))
	}
	starts, _ := orgStarts(t, e)
	want := "2027-03-01T11:00:00-08:00 2027-03-08T11:00:00-08:00 2027-03-15T11:00:00-07:00 2027-03-22T11:00:00-07:00 2027-03-29T11:00:00-07:00"
	if strings.Join(starts, " ") != want {
		t.Fatalf("history lost: %v", starts)
	}
	// moveSeriesStart re-anchors deliberately; the earlier occurrences then end.
	e.mustOK(e.do("PATCH", "/api/orgcal/events/"+id, map[string]any{"startsAt": "2027-03-15T17:00:00Z", "endsAt": "2027-03-15T18:00:00Z", "moveSeriesStart": true}, e.officer()))
	starts, _ = orgStarts(t, e)
	if strings.Join(starts, " ") != "2027-03-15T10:00:00-07:00 2027-03-22T10:00:00-07:00 2027-03-29T10:00:00-07:00" {
		t.Fatalf("move: %v", starts)
	}
	if r := e.do("PATCH", "/api/orgcal/events/"+id, map[string]any{"moveSeriesStart": "yes"}, e.officer()); r.Status != 400 {
		t.Fatal(r.Status, string(r.Raw))
	}
}

func TestOrgCalendarPatchAllDaySeriesKeepsDate(t *testing.T) {
	e := newEnv(t)
	r := e.mustOK(e.post("/api/orgcal/events", map[string]any{"calendarKey": "internal", "title": "Days", "startsAt": "2027-03-01", "endsAt": "2027-03-02", "allDay": true, "rrule": "RRULE:FREQ=WEEKLY"}, e.officer()))
	id := r.str("event", "id")
	r = e.mustOK(e.do("PATCH", "/api/orgcal/events/"+id, map[string]any{"startsAt": "2027-03-15", "endsAt": "2027-03-17"}, e.officer()))
	if r.str("event", "startsAt") != "2027-03-01" || r.str("event", "endsAt") != "2027-03-03" {
		t.Fatal(string(r.Raw))
	}
}

func TestOrgCalendarPatchEndOnlyFromOccurrence(t *testing.T) {
	e := newEnv(t)
	id := createWeeklySeries(t, e)
	r := e.mustOK(e.do("PATCH", "/api/orgcal/events/"+id, map[string]any{"endsAt": "2027-03-15T18:30:00Z"}, e.officer()))
	// 11:30 PDT on the occurrence becomes 11:30 PST on the series date.
	if r.str("event", "endsAt") != "2027-03-01T19:30:00Z" || r.str("event", "seriesStartsAt") != "2027-03-01T18:00:00Z" {
		t.Fatal(string(r.Raw))
	}
}
