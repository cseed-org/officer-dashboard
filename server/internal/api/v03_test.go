package api

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestV03RoleMatrix(t *testing.T) {
	e := newEnv(t)
	for _, tc := range []struct {
		user         string
		invite, role int
	}{{"user-priya", 403, 403}, {"user-director", 200, 403}, {"user-leadership", 200, 200}} {
		t.Run(tc.user, func(t *testing.T) {
			invite := map[string]any{"name": "Invitee", "discordId": fmt.Sprintf("10000000000000%04d", tc.invite+tc.role), "subteamId": "team-events", "method": "link"}
			expectStatus(t, "invite", e.post("/api/invites", invite, e.as(tc.user)), tc.invite)
			expectStatus(t, "grant leadership", e.do("PATCH", "/api/admin/users/user-ava", map[string]any{"role": "leadership"}, e.as(tc.user)), tc.role)
		})
	}
	expectStatus(t, "director cannot create leadership", e.post("/api/admin/users", map[string]any{"name": "Boss", "discordId": "100000000000000099", "role": "leadership"}, e.director()), 403)
	expectStatus(t, "leadership admin", e.get("/api/admin/users", e.as("user-leadership")), 200)
	expectStatus(t, "leadership finance", e.get("/api/finance/reports/trial-balance", e.as("user-leadership")), 200)
	var hash string
	if err := e.db.R.QueryRow(`SELECT token_hash FROM invites LIMIT 1`).Scan(&hash); err != nil || len(hash) != 64 {
		t.Fatalf("stored hash %q: %v", hash, err)
	}
}

func TestV03WidgetPrefsArePrivateValidatedAndAudited(t *testing.T) {
	e := newEnv(t)
	body := map[string]any{"widgetLayout": []map[string]any{{"id": "budget", "size": "S"}}}
	e.mustOK(e.do("PUT", "/api/me/prefs", body, e.officer()))
	got := e.mustOK(e.get("/api/me/prefs", e.officer()))
	if !strings.Contains(string(got.Raw), `"budget"`) {
		t.Fatal(string(got.Raw))
	}
	other := e.mustOK(e.get("/api/me/prefs", e.director()))
	if strings.Contains(string(other.Raw), `"size":"S"`) {
		t.Fatal("leaked preferences")
	}
	expectStatus(t, "invalid size", e.do("PUT", "/api/me/prefs", map[string]any{"widgetLayout": []any{map[string]any{"id": "budget", "size": "XL"}}}, e.officer()), 400)
	expectStatus(t, "duplicate", e.do("PUT", "/api/me/prefs", map[string]any{"widgetLayout": []any{map[string]any{"id": "a", "size": "S"}, map[string]any{"id": "a", "size": "M"}}}, e.officer()), 400)
	e.mustOK(e.do("PUT", "/api/me/prefs", map[string]any{"widgetLayout": []any{}}, e.officer()))
	var count int
	if err := e.db.R.QueryRow(`SELECT count(*) FROM audit_log WHERE action='prefs.updated' AND actor_user_id='user-priya'`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("audit %d %v", count, err)
	}
}

func TestV03CalendarLimitOwnershipAndActiveSelection(t *testing.T) {
	e := newEnv(t)
	ids := []string{}
	for i := 0; i < 5; i++ {
		r := e.mustOK(e.post("/api/me/calendar-emails", map[string]any{"email": fmt.Sprintf("Calendar%d@example.org", i)}, e.officer()))
		ids = append(ids, r.str("email", "id"))
	}
	sixth := e.post("/api/me/calendar-emails", map[string]any{"email": "six@example.org"}, e.officer())
	expectStatus(t, "sixth", sixth, 409)
	if !strings.Contains(sixth.str("error", "message"), "5") {
		t.Fatal("unclear limit error")
	}
	expectStatus(t, "other user update", e.do("PATCH", "/api/me/calendar-emails/"+ids[0], map[string]any{"active": true}, e.director()), 404)
	expectStatus(t, "other user delete", e.do("DELETE", "/api/me/calendar-emails/"+ids[0], nil, e.director()), 404)
	e.mustOK(e.do("PATCH", "/api/me/calendar-emails/"+ids[2], map[string]any{"active": true}, e.officer()))
	e.mustOK(e.do("DELETE", "/api/me/calendar-emails/"+ids[2], nil, e.officer()))
	rows, err := e.db.Read().CalendarEmails(context.Background(), "user-priya")
	if err != nil {
		t.Fatal(err)
	}
	active := 0
	for _, r := range rows {
		if r.Active {
			active++
		}
		if r.Email != strings.ToLower(r.Email) {
			t.Fatal("not normalized")
		}
	}
	if len(rows) != 4 || active != 1 {
		t.Fatalf("rows=%d active=%d", len(rows), active)
	}
}

func TestV03LeadershipCannotBeStrandedAndNoopRolePatch(t *testing.T) {
	e := newEnv(t)
	expectStatus(t, "last leadership demotion", e.do("PATCH", "/api/admin/users/user-leadership", map[string]any{"role": "director"}, e.as("user-leadership")), 409)
	expectStatus(t, "last leadership deactivation", e.do("PATCH", "/api/admin/users/user-leadership", map[string]any{"active": false}, e.as("user-leadership")), 409)
	expectStatus(t, "director echo existing role", e.do("PATCH", "/api/admin/users/user-priya", map[string]any{"role": "officer", "title": "Events"}, e.director()), 200)
	expectStatus(t, "unknown subteam", e.post("/api/invites", map[string]any{"name": "Unknown", "discordId": "100000000000000099", "subteamId": "missing"}, e.director()), 400)
}
