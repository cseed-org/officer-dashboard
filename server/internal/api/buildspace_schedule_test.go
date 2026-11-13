package api

import (
	"strings"
	"testing"
	"time"
)

// testNow is Saturday 2026-10-31 11:00 in the default club zone (America/Los_Angeles),
// inside the default Monday–Sunday week Oct 26 – Nov 1.
func TestBuildspaceScheduleMidWeekChange(t *testing.T) {
	e := newEnv(t)
	audits := func() int64 {
		return e.scalar(`SELECT count(*) FROM audit_log WHERE action='buildspace_launchpad.schedule.updated'`).(int64)
	}
	list := e.mustOK(e.get("/api/buildspace-launchpad", e.officer()))
	if list.str("weekStart") != "2026-10-26" || list.str("weekDue") != "2026-11-01" || list.str("today") != "2026-10-31" || list.at("schedule", "pending") != nil {
		t.Fatal(string(list.Raw))
	}

	wednesday := map[string]any{"dueWeekday": 3, "dueTime": nil}
	expectStatus(t, "officer", e.do("PUT", "/api/buildspace-launchpad/schedule", wednesday, e.officer()), 403)
	expectStatus(t, "director", e.do("PUT", "/api/buildspace-launchpad/schedule", wednesday, e.director()), 403)
	expectStatus(t, "bad time", e.do("PUT", "/api/buildspace-launchpad/schedule", map[string]any{"dueWeekday": 3, "dueTime": "24:00"}, e.as("user-leadership")), 400)
	expectStatus(t, "bad day", e.do("PUT", "/api/buildspace-launchpad/schedule", map[string]any{"dueWeekday": 7}, e.as("user-leadership")), 400)
	if audits() != 0 {
		t.Fatal("refused writes were audited")
	}

	// Mid-week: the current deadline stays Sunday; Wednesday starts after it, with one
	// 10-day transition week (Nov 2–11).
	s := e.mustOK(e.do("PUT", "/api/buildspace-launchpad/schedule", wednesday, e.as("user-leadership")))
	if s.str("week", "due") != "2026-11-01" || s.num("current", "dueWeekday") != 0 ||
		s.str("pending", "effectiveFrom") != "2026-11-02" || s.num("pending", "dueWeekday") != 3 || s.str("pending", "updatedBy") != "user-leadership" ||
		s.str("pendingFirstWeek", "start") != "2026-11-02" || s.str("pendingFirstWeek", "due") != "2026-11-11" {
		t.Fatal(string(s.Raw))
	}
	if audits() != 1 {
		t.Fatal("schedule change not audited")
	}
	e.mustOK(e.do("PUT", "/api/buildspace-launchpad/schedule", wednesday, e.as("user-leadership")))
	if audits() != 1 {
		t.Fatal("unchanged repeat was audited")
	}

	// A builder who checked in this week is still checked in for this week after the change.
	who := e.officer()
	project := map[string]any{"name": "Schedule project", "description": "", "track": "Software", "stage": "Building", "links": []string{}, "mentorIds": []string{}}
	e.mustOK(e.do("PUT", "/api/buildspace-launchpad/projects/schedule-project", project, who))
	e.mustOK(e.do("PUT", "/api/buildspace-launchpad/people/schedule-person", map[string]any{"name": "Schedule builder", "projectId": "schedule-project", "joinedOn": "2026-10-01", "discordId": "123456789012345699"}, who))
	weekly := func(date string, who actor) reply {
		return e.mustOK(e.post("/api/buildspace-launchpad/activity", map[string]any{"projectId": "schedule-project", "personId": "schedule-person", "kind": "weekly", "date": date, "body": "Progress", "status": "on-track"}, who))
	}
	if r := weekly("2026-10-27", who); r.str("week", "start") != "2026-10-26" || r.str("week", "due") != "2026-11-01" {
		t.Fatal(string(r.Raw))
	}

	// Thursday Nov 5: the transition week is current.
	e.clock = time.Date(2026, 11, 5, 18, 0, 0, 0, time.UTC)
	who = e.officer()
	list = e.mustOK(e.get("/api/buildspace-launchpad", who))
	if list.str("weekStart") != "2026-11-02" || list.str("weekDue") != "2026-11-11" || list.num("schedule", "current", "dueWeekday") != 3 || list.at("schedule", "pending") != nil {
		t.Fatal(string(list.Raw))
	}

	// Discord ingest assigns by date under the rules: Nov 10 belongs to the week due Nov 11.
	issued := e.mustOK(e.post("/api/admin/tokens", map[string]any{"name": "bot", "scopes": []string{"ingest:buildspace"}, "expiresInDays": 30}, e.director()))
	bot := bearer(issued.at("plaintext").(string))
	in := e.mustOK(e.post("/api/ingest/buildspace-launchpad", map[string]any{"discordId": "123456789012345699", "externalId": "m-1", "kind": "weekly", "date": "2026-11-10", "body": "From Discord", "status": "on-track"}, bot))
	if in.str("week", "start") != "2026-11-02" || in.str("week", "due") != "2026-11-11" {
		t.Fatal(string(in.Raw))
	}

	// Calendar: due Nov 1 (old Sunday), none on Nov 8, the Nov 11 item suppressed by the
	// ingested check-in, then the normal Wednesday week due Nov 18.
	e.mustOK(e.do("PUT", "/api/me/calendar-widget", map[string]any{"view": "month", "colourBy": "source", "showWeekends": true, "sources": []string{"buildspace-weekly"}}, who))
	feed := e.mustOK(e.get("/api/calendar/feed?start=2026-10-26&end=2026-11-20&sources=buildspace-weekly", who))
	dates := []string{}
	for _, v := range feed.list("items") {
		item := v.(map[string]any)
		if strings.Contains(item["id"].(string), "schedule-person") {
			dates = append(dates, item["date"].(string))
		}
	}
	// Nov 1 is suppressed by the Oct 27 check-in.
	if strings.Join(dates, ",") != "2026-11-18" {
		t.Fatalf("due dates %v: %s", dates, feed.Raw)
	}

	// Changing again during the transition week waits for Nov 12; choosing the current
	// values cancels that pending change.
	sunday := e.mustOK(e.do("PUT", "/api/buildspace-launchpad/schedule", map[string]any{"dueWeekday": 0, "dueTime": "18:00"}, e.as("user-leadership")))
	if sunday.str("pending", "effectiveFrom") != "2026-11-12" || sunday.str("pending", "dueTime") != "18:00" || sunday.str("pendingFirstWeek", "due") != "2026-11-22" {
		t.Fatal(string(sunday.Raw))
	}
	cancel := e.mustOK(e.do("PUT", "/api/buildspace-launchpad/schedule", wednesday, e.as("user-leadership")))
	if cancel.at("pending") != nil || audits() != 3 {
		t.Fatal(string(cancel.Raw))
	}
}

func TestBuildspaceWeekUsesClubTimezone(t *testing.T) {
	e := newEnv(t)
	// 2026-10-31 18:00 UTC is already Sunday Nov 1 03:00 in Tokyo.
	e.mustOK(e.do("PUT", "/api/club/settings", map[string]any{"location": "Tokyo campus", "clubTimezone": "Asia/Tokyo"}, e.as("user-leadership")))
	e.mustOK(e.do("PUT", "/api/buildspace-launchpad/schedule", map[string]any{"dueWeekday": 0, "dueTime": "02:00"}, e.as("user-leadership")))
	list := e.mustOK(e.get("/api/buildspace-launchpad", e.officer()))
	if list.str("today") != "2026-11-01" || list.str("schedule", "timezone") != "Asia/Tokyo" || list.str("weekDue") != "2026-11-01" {
		t.Fatal(string(list.Raw))
	}
	// The default end-of-day Sunday deadline has not passed yet at 03:00.
	if list.at("schedule", "pastDue") != false || list.str("schedule", "dueAt") != "2026-11-01T15:00:00Z" {
		t.Fatal(string(list.Raw))
	}
}
