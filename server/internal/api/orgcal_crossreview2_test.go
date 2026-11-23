package api

import (
	"context"
	"fmt"
	"officerdashboard/internal/orgcal"
	"sort"
	"strings"
	"testing"
	"time"
)

// subteamWithMembers returns a subteam with at least n active officers that have emails.
func subteamWithMembers(t *testing.T, e *testEnv, n int) (string, []string) {
	t.Helper()
	rows, err := e.db.R.Query(`SELECT p.subteam_id, u.id FROM officer_profiles p JOIN users u ON u.id=p.user_id
		WHERE u.active=1 AND u.email LIKE '%@%' AND p.subteam_id IS NOT NULL ORDER BY p.subteam_id, u.id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	by := map[string][]string{}
	var order []string
	for rows.Next() {
		var team, user string
		if err := rows.Scan(&team, &user); err != nil {
			t.Fatal(err)
		}
		if by[team] == nil {
			order = append(order, team)
		}
		by[team] = append(by[team], user)
	}
	for _, team := range order {
		if len(by[team]) >= n {
			return team, by[team]
		}
	}
	t.Skipf("no seeded subteam with %d members", n)
	return "", nil
}

func userEmail(t *testing.T, e *testEnv, id string) string {
	t.Helper()
	var email string
	if err := e.db.R.QueryRow(`SELECT lower(email) FROM users WHERE id=?`, id).Scan(&email); err != nil {
		t.Fatal(err)
	}
	return email
}

func lastPush(t *testing.T, f *orgFake, path string) orgRequest {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.requests) - 1; i >= 0; i-- {
		if r := f.requests[i]; (r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH") && strings.HasPrefix(r.Path, path) {
			return r
		}
	}
	t.Fatal("no push to", path)
	return orgRequest{}
}

func attendeeEmails(r orgRequest) []string {
	var out []string
	for _, a := range r.Body["attendees"].([]any) {
		out = append(out, a.(map[string]any)["email"].(string))
	}
	sort.Strings(out)
	return out
}

// F-3: subteam invitees expand to active members only; sendInvites:false sends sendUpdates=none.
func TestOrgCalendarPushExpandsSubteamAndHonoursSendInvites(t *testing.T) {
	e, f := orgGoogleEnv(t)
	team, members := subteamWithMembers(t, e, 2)
	// Deactivate a member that is not the last owner (a trigger protects that one).
	gone := -1
	for i := len(members) - 1; i >= 0 && gone < 0; i-- {
		if _, err := e.db.W.Exec(`UPDATE users SET active=0 WHERE id=?`, members[i]); err == nil {
			gone = i
		}
	}
	if gone < 0 {
		t.Skip("no member can be deactivated")
	}
	active := members[(gone+1)%len(members)]
	e.mustOK(e.post("/api/orgcal/events", map[string]any{"calendarKey": "external", "title": "Team sync", "startsAt": "2026-11-24T18:00:00Z", "endsAt": "2026-11-24T19:00:00Z", "sendInvites": false,
		"invitees": []any{map[string]any{"kind": "subteam", "ref": team}, map[string]any{"kind": "email", "ref": "Guest@Example.org"}}}, e.officer()))
	e.srv.WaitBackground()
	r := lastPush(t, f, "/calendars/external/events")
	if r.Query != "sendUpdates=none" {
		t.Fatal(r.Query)
	}
	got := strings.Join(attendeeEmails(r), ",")
	if !strings.Contains(got, userEmail(t, e, active)) || strings.Contains(got, userEmail(t, e, members[gone])) || !strings.Contains(got, "guest@example.org") {
		t.Fatal(got)
	}
}

// F-2 and F-6: an RSVP-only change pulled from Google keeps the subteam invitee and the RSVP
// is sent back on the next push.
func TestOrgCalendarPullKeepsGroupInviteesAndRSVPs(t *testing.T) {
	e, f := orgGoogleEnv(t)
	team, members := subteamWithMembers(t, e, 1)
	r := e.mustOK(e.post("/api/orgcal/events", map[string]any{"calendarKey": "internal", "title": "Design review", "startsAt": "2026-11-24T18:00:00Z", "endsAt": "2026-11-24T19:00:00Z",
		"invitees": []any{map[string]any{"kind": "subteam", "ref": team}}}, e.officer()))
	id := r.str("event", "id")
	e.srv.WaitBackground()
	member := userEmail(t, e, members[0])
	f.mu.Lock()
	for i := range f.events["internal"] {
		ev := &f.events["internal"][i]
		ev.ETag = "rsvp-etag"
		for j := range ev.Attendees {
			if strings.EqualFold(ev.Attendees[j].Email, member) {
				ev.Attendees[j].ResponseStatus = "accepted"
			}
		}
	}
	f.mu.Unlock()
	e.clock = e.clock.Add(6 * time.Minute)
	if err := e.srv.syncOrgCalendar(context.Background()); err != nil {
		t.Fatal(err)
	}
	ev, err := e.db.Read().OrgEvent(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if len(ev.Invitees) != 1 || ev.Invitees[0].Kind != "subteam" || ev.Invitees[0].Ref != team {
		t.Fatalf("group invitee rewritten: %+v", ev.Invitees)
	}
	e.mustOK(e.do("PATCH", "/api/orgcal/events/"+id, map[string]any{"title": "Design review (room 2)"}, e.officer()))
	e.srv.WaitBackground()
	push := lastPush(t, f, "/calendars/internal/events")
	kept := false
	for _, a := range push.Body["attendees"].([]any) {
		m := a.(map[string]any)
		if strings.EqualFold(m["email"].(string), member) && m["responseStatus"] == "accepted" {
			kept = true
		}
	}
	if !kept {
		t.Fatal("RSVP reset on push", push.Body["attendees"])
	}
}

// F-2: Google-side attendee edits merge: extras become email rows, a removed member breaks the group.
func TestOrgCalendarPullMergesAttendeeChanges(t *testing.T) {
	e, f := orgGoogleEnv(t)
	team, members := subteamWithMembers(t, e, 1)
	r := e.mustOK(e.post("/api/orgcal/events", map[string]any{"calendarKey": "internal", "title": "Merge", "startsAt": "2026-11-24T18:00:00Z", "endsAt": "2026-11-24T19:00:00Z",
		"invitees": []any{map[string]any{"kind": "subteam", "ref": team}, map[string]any{"kind": "email", "ref": "dropped@example.org"}}}, e.officer()))
	id := r.str("event", "id")
	e.srv.WaitBackground()
	f.mu.Lock()
	ev := &f.events["internal"][0]
	ev.ETag = "edited"
	keep := ev.Attendees[:0]
	for _, a := range ev.Attendees {
		if a.Email != "dropped@example.org" {
			keep = append(keep, a)
		}
	}
	ev.Attendees = append(keep, orgcal.Attendee{Email: "added@example.org"})
	f.mu.Unlock()
	e.clock = e.clock.Add(6 * time.Minute)
	if err := e.srv.syncOrgCalendar(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := e.db.Read().OrgEvent(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	var refs []string
	for _, v := range got.Invitees {
		refs = append(refs, v.Kind+":"+v.Ref)
	}
	sort.Strings(refs)
	want := []string{"email:added@example.org", "subteam:" + team}
	sort.Strings(want)
	if strings.Join(refs, ",") != strings.Join(want, ",") {
		t.Fatal(refs, members)
	}
}

// F-6: External events created in Google import without sending invites from the dashboard.
func TestOrgCalendarImportedExternalDoesNotSendInvites(t *testing.T) {
	e, f := orgGoogleEnv(t)
	f.mu.Lock()
	f.events["external"] = []orgcal.Event{{ID: "g-ext", ETag: "v1", Summary: "Partner call", Status: "confirmed", Start: orgcal.Time{DateTime: "2026-11-24T18:00:00Z"}, End: orgcal.Time{DateTime: "2026-11-24T19:00:00Z"}, Attendees: []orgcal.Attendee{{Email: "partner@example.org"}}}}
	f.mu.Unlock()
	if err := e.srv.syncOrgCalendar(context.Background()); err != nil {
		t.Fatal(err)
	}
	r := e.mustOK(e.get("/api/orgcal/events?from=2026-11-01&to=2026-12-01&cal=external", e.officer()))
	if len(r.list("events")) != 1 || r.at("events", 0, "sendInvites") != false {
		t.Fatal(string(r.Raw))
	}
	e.mustOK(e.do("PATCH", "/api/orgcal/events/"+r.str("events", 0, "id"), map[string]any{"title": "Partner call (typo fixed)"}, e.officer()))
	e.srv.WaitBackground()
	if q := lastPush(t, f, "/calendars/external/events").Query; q != "sendUpdates=none" {
		t.Fatal(q)
	}
}

func outboxState(t *testing.T, e *testEnv) string {
	t.Helper()
	var n, rev int64
	if err := e.db.R.QueryRow(`SELECT count(*), coalesce(sum(revision),0) FROM org_cal_outbox`).Scan(&n, &rev); err != nil {
		t.Fatal(err)
	}
	var revs int64
	if err := e.db.R.QueryRow(`SELECT coalesce(sum(revision),0) FROM org_cal_events`).Scan(&revs); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%d/%d/%d", n, rev, revs)
}

// F-3: locked rows refuse every write path; calendar changes and read tokens are refused.
func TestOrgCalendarLockedRowsRefuseAllWriters(t *testing.T) {
	e := newEnv(t)
	who := e.officer()
	p := e.mustOK(e.post("/api/programs", map[string]any{"trackId": "buildspace", "name": "Locked lab", "weekday": 2, "startTime": "18:00", "durationMin": 60, "startsOn": "2026-11-01", "endsOn": "2026-11-30"}, who))
	programID := p.str("program", "id")
	ev := e.mustOK(e.post("/api/events", map[string]any{"name": "Locked event", "startsAt": "2026-11-20T18:00:00Z", "endsAt": "2026-11-20T20:00:00Z", "status": "published"}, who))
	eventID := ev.str("event", "id")
	list := e.mustOK(e.get("/api/orgcal/events?from=2026-11-01&to=2026-12-01&cal=programs,events", who))
	locked := map[string]string{}
	for i := range list.list("events") {
		locked[list.str("events", i, "sourceKind")+":"+list.str("events", i, "sourceId")] = list.str("events", i, "id")
	}
	rows := []string{locked["program:"+programID], locked["event:"+eventID]}
	if rows[0] == "" || rows[1] == "" {
		t.Fatal(locked, string(list.Raw))
	}
	write := e.mustOK(e.post("/api/admin/tokens", map[string]any{"name": "calendar", "scopes": []string{"calendar:write"}}, e.director())).str("plaintext")
	_, tokens := connectAgent(t, e, "user-priya", "Calendar agent")
	agent := tokens.str("access_token")
	before := outboxState(t, e)
	for _, id := range rows {
		for _, a := range []struct {
			name string
			who  actor
		}{{"session", who}, {"write token", bearer(write)}} {
			if r := e.do("PATCH", "/api/orgcal/events/"+id, map[string]any{"title": "Hijack"}, a.who); r.Status != 403 {
				t.Errorf("%s PATCH %s: %d %s", a.name, id, r.Status, r.Raw)
			}
			if r := e.do("DELETE", "/api/orgcal/events/"+id, nil, a.who); r.Status != 403 {
				t.Errorf("%s DELETE %s: %d %s", a.name, id, r.Status, r.Raw)
			}
		}
		if text, isErr := toolCall(e, agent, "orgcal_update", map[string]any{"id": id, "body": map[string]any{"title": "Hijack"}}); !isErr || !strings.Contains(text, "locked") {
			t.Errorf("MCP update %s: %v %s", id, isErr, text)
		}
		if text, isErr := toolCall(e, agent, "orgcal_delete", map[string]any{"id": id}); !isErr || !strings.Contains(text, "locked") {
			t.Errorf("MCP delete %s: %v %s", id, isErr, text)
		}
	}
	if after := outboxState(t, e); after != before {
		t.Fatalf("outbox changed: %s -> %s", before, after)
	}
	m := e.mustOK(e.post("/api/orgcal/events", map[string]any{"calendarKey": "internal", "title": "Mine", "startsAt": "2026-11-24T18:00:00Z", "endsAt": "2026-11-24T19:00:00Z"}, who))
	mine := m.str("event", "id")
	for _, key := range []string{"external", "events"} {
		if r := e.do("PATCH", "/api/orgcal/events/"+mine, map[string]any{"calendarKey": key}, who); r.Status != 400 {
			t.Errorf("calendarKey %s: %d %s", key, r.Status, r.Raw)
		}
	}
	read := e.mustOK(e.post("/api/admin/tokens", map[string]any{"name": "read", "scopes": []string{"calendar:read"}}, e.director())).str("plaintext")
	if r := e.do("DELETE", "/api/orgcal/events/"+mine, nil, bearer(read)); r.Status != 403 {
		t.Fatal(r.Status, string(r.Raw))
	}
}

// F-8: a revoked org refresh token shows as reconnect-needed; disconnect does not wait for a pass.
func TestOrgGoogleStatusShowsReconnectNeeded(t *testing.T) {
	e, f := orgGoogleEnv(t)
	f.mu.Lock()
	f.revoked = true
	f.mu.Unlock()
	e.clock = e.clock.Add(2 * time.Hour) // access token expired: the pass must refresh
	_ = e.srv.syncOrgCalendar(context.Background())
	r := e.mustOK(e.get("/api/settings/org-google/status", e.as("user-leadership")))
	if r.at("connected") != true || r.at("needsReconnect") != true || !strings.Contains(r.str("lastSyncError"), "reconnect") {
		t.Fatal(string(r.Raw))
	}
}

func TestOrgGoogleDisconnectCancelsRunningPass(t *testing.T) {
	e, f := orgGoogleEnv(t)
	pause, entered := make(chan struct{}), make(chan struct{})
	f.mu.Lock()
	f.pauseWrite, f.enteredWrite = pause, entered
	f.mu.Unlock()
	defer close(pause)
	e.mustOK(e.post("/api/orgcal/events", map[string]any{"calendarKey": "internal", "title": "Slow", "startsAt": "2026-11-24T18:00:00Z", "endsAt": "2026-11-24T19:00:00Z"}, e.officer()))
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("pass did not start")
	}
	done := make(chan reply, 1)
	go func() { done <- e.do("DELETE", "/api/settings/org-google", nil, e.as("user-leadership")) }()
	select {
	case r := <-done:
		if r.Status != 200 || r.at("connected") != false {
			t.Fatal(r.Status, string(r.Raw))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("disconnect waited behind the running pass")
	}
}
