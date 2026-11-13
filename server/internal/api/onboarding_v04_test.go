package api

import (
	"strings"
	"testing"
)

// newInvitee invites inviteeID as Alex Chen (Events) and signs them in through Discord.
func newInvitee(t *testing.T, dm *fakeDM) (e *testEnv, alex actor, inviteID string) {
	t.Helper()
	e = discordEnv(t, `{"id":"`+inviteeID+`","username":"alex","global_name":"Alex"}`, nil)
	if dm != nil {
		e.cfg.DM = dm
	}
	e.srv = New(e.cfg)
	e.h = e.srv.Handler()
	inv := e.mustOK(e.post("/api/invites", inviteBody("link"), e.director()))
	cb := signInAsInvitee(t, e, "invite="+inv.str("token"))
	if cb.Header.Get("Location") != "/onboarding" {
		t.Fatalf("invitee → %s", cb.Header.Get("Location"))
	}
	return e, actor{cookie: cookieNamed(setCookies(cb), e.srv.sessionCookieName())}, inv.str("invite", "id")
}

var validProfile = map[string]any{"phone": "+1 206 555 0100", "birthday": "04-21"}

func TestOnboardingGateBlocksTheAPIUntilSetupIsComplete(t *testing.T) {
	e, alex, _ := newInvitee(t, nil)
	for _, p := range []string{"/api/events", "/api/officers", "/api/requests", "/api/contacts", "/api/overview", "/api/search?q=a", "/api/lookups"} {
		r := e.get(p, alex)
		expectStatus(t, "before setup "+p, r, 403)
		if r.str("error", "code") != "onboarding_required" {
			t.Errorf("%s code %s", p, r.Raw)
		}
	}
	expectStatus(t, "write before setup", e.post("/api/events", map[string]any{"name": "x"}, alex), 403)
	// Setup itself, and what the onboarding page and app shell need, stay open.
	for _, p := range []string{"/api/me", "/api/me/profile", "/api/me/prefs", "/api/me/calendar", "/api/me/calendar-emails", "/api/club/settings", "/api/health"} {
		expectStatus(t, "during setup "+p, e.get(p, alex), 200)
	}
	e.mustOK(e.do("PUT", "/api/me/onboarding", map[string]any{"step": 3}, alex))
	e.mustOK(e.do("PUT", "/api/me/profile", validProfile, alex))
	e.mustOK(e.post("/api/me/onboarding/complete", nil, alex))
	for _, p := range []string{"/api/events", "/api/officers", "/api/lookups"} {
		expectStatus(t, "after setup "+p, e.get(p, alex), 200)
	}
	// People with no invite (seeded, bootstrap) are never gated.
	expectStatus(t, "seeded officer", e.get("/api/events", e.officer()), 200)
}

func TestAdminCreatedUsersGoThroughOnboarding(t *testing.T) {
	e := newEnv(t)
	expectStatus(t, "no subteam", e.post("/api/admin/users", map[string]any{"discordId": "100000000000000099", "name": "Sam"}, e.director()), 400)
	expectStatus(t, "unknown subteam", e.post("/api/admin/users", map[string]any{"discordId": "100000000000000099", "name": "Sam", "subteamId": "team-nope"}, e.director()), 400)
	created := e.mustOK(e.post("/api/admin/users", map[string]any{"discordId": "100000000000000099", "name": "Sam Lee", "subteamId": "team-build"}, e.director()))
	sam := e.as(created.str("user", "id"))
	me := e.mustOK(e.get("/api/me", sam))
	if me.num("onboarding", "step") != 1 || me.str("onboarding", "name") != "Sam Lee" || me.str("onboarding", "subteam", "id") != "team-build" {
		t.Fatalf("me %s", me.Raw)
	}
	expectStatus(t, "gated", e.get("/api/events", sam), 403)
	if s := e.mustOK(e.get("/api/invites", e.director())); !strings.Contains(string(s.Raw), `"status":"accepted"`) {
		t.Errorf("invite row %s", s.Raw)
	}
	// A Discord ID with a pending invite can't also be created directly.
	e.mustOK(e.post("/api/invites", inviteBody("link"), e.director()))
	pending := e.post("/api/admin/users", map[string]any{"discordId": inviteeID, "name": "Alex", "subteamId": "team-build"}, e.director())
	expectStatus(t, "pending invite", pending, 409)
	if !strings.Contains(pending.str("error", "message"), "pending invite") {
		t.Errorf("message %s", pending.Raw)
	}
}

func TestEditingAnInviteUpdatesTheOnboardingDetails(t *testing.T) {
	dm := &fakeDM{}
	e, alex, id := newInvitee(t, dm)
	expectStatus(t, "officer edits", e.do("PATCH", "/api/invites/"+id, map[string]any{"name": "Alexandra Chen"}, e.officer()), 403)
	expectStatus(t, "bad subteam", e.do("PATCH", "/api/invites/"+id, map[string]any{"subteamId": "team-nope"}, e.director()), 400)
	r := e.mustOK(e.do("PATCH", "/api/invites/"+id, map[string]any{"name": "Alexandra Chen", "subteamId": "team-build"}, e.director()))
	if r.str("invite", "name") != "Alexandra Chen" || r.str("invite", "subteam", "id") != "team-build" {
		t.Fatalf("patch %s", r.Raw)
	}
	me := e.mustOK(e.get("/api/me", alex))
	if me.str("user", "name") != "Alexandra Chen" || me.str("onboarding", "name") != "Alexandra Chen" || me.str("onboarding", "subteam", "id") != "team-build" {
		t.Fatalf("me after edit %s", me.Raw)
	}
	// The profile takes the new subteam, and a later edit moves the saved profile too.
	p := e.mustOK(e.do("PUT", "/api/me/profile", validProfile, alex))
	if p.str("profile", "subteam", "id") != "team-build" {
		t.Fatalf("profile %s", p.Raw)
	}
	e.mustOK(e.do("PATCH", "/api/invites/"+id, map[string]any{"subteamId": "team-design"}, e.director()))
	if me := e.mustOK(e.get("/api/me", alex)); me.str("onboarding", "subteam", "id") != "team-design" {
		t.Fatalf("me %s", me.Raw)
	}
	if p := e.mustOK(e.do("PUT", "/api/me/profile", validProfile, alex)); p.str("profile", "subteam", "id") != "team-design" {
		t.Fatalf("profile after second edit %s", p.Raw)
	}
	if e.scalar(`SELECT count(*) FROM audit_log WHERE action='invite.updated' AND entity_id=?`, id) != int64(2) {
		t.Error("edits not audited")
	}
	// Pending (not yet accepted) invites can be edited too; finished ones can't.
	pend := e.mustOK(e.post("/api/invites", map[string]any{"name": "Kim", "discordId": "100000000000000888", "subteamId": "team-events"}, e.director()))
	e.mustOK(e.do("PATCH", "/api/invites/"+pend.str("invite", "id"), map[string]any{"name": "Kim Park"}, e.director()))
	e.mustOK(e.post("/api/me/onboarding/complete", nil, alex))
	expectStatus(t, "onboarded", e.do("PATCH", "/api/invites/"+id, map[string]any{"name": "X"}, e.director()), 409)
}

// A shared browser: someone else is signed in when the invite link opens. Signing out
// returns to that invite link; any other target falls back to /login.
func TestLogoutCanReturnToAnInviteLink(t *testing.T) {
	e := newEnv(t)
	for next, want := range map[string]string{
		"/login?invite=abc123": "/login?invite=abc123",
		"/finance":             "/login",
		"//evil.example/login": "/login",
		"":                     "/login",
	} {
		r := e.do("POST", "/auth/logout", formBody{fields: map[string]string{"next": next}}, e.officer())
		if r.Status != 303 || r.Header.Get("Location") != want {
			t.Errorf("next %q → %d %s, want %s", next, r.Status, r.Header.Get("Location"), want)
		}
	}
}

// Only leadership edits leadership: an invite PATCH can't rename or move a leadership
// account that is still in setup (the same rule as PATCH /api/officers/:id).
func TestDirectorsCannotEditALeadershipAccountsInvite(t *testing.T) {
	e := newEnv(t)
	created := e.mustOK(e.post("/api/admin/users", map[string]any{"discordId": "100000000000000099", "name": "Lee Boss", "subteamId": "team-build", "role": "leadership"}, e.as("user-leadership")))
	uid := created.str("user", "id")
	invID := e.scalar(`SELECT id FROM invites WHERE user_id=?`, uid).(string)
	expectStatus(t, "director edits leadership invite", e.do("PATCH", "/api/invites/"+invID, map[string]any{"name": "Renamed"}, e.director()), 403)
	if n := e.scalar(`SELECT name FROM users WHERE id=?`, uid); n != "Lee Boss" {
		t.Fatalf("renamed anyway: %v", n)
	}
	e.mustOK(e.do("PATCH", "/api/invites/"+invID, map[string]any{"name": "Lee Boss Jr"}, e.as("user-leadership")))
}
