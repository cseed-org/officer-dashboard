package api

import "testing"

// Every permission a director or leadership can change applies on the very next request of
// an already signed-in person, and the X-Perm-Version header tells their open tab.

func permOf(t *testing.T, r reply) string {
	t.Helper()
	v := r.Header.Get(permVersionHeader)
	if v == "" {
		t.Fatalf("no %s header on a signed-in response (status %d)", permVersionHeader, r.Status)
	}
	return v
}

func TestRoleChangesApplyOnTheNextRequestWithoutSigningOut(t *testing.T) {
	e := newEnv(t)
	lead := e.as("user-leadership")
	priya := e.officer()
	v0 := permOf(t, e.mustOK(e.get("/api/me", priya)))
	expectStatus(t, "officer reads Access", e.get("/api/admin/users", priya), 403)

	e.mustOK(e.do("PATCH", "/api/admin/users/user-priya", map[string]any{"role": "director"}, lead))
	r := e.get("/api/admin/users", priya)
	expectStatus(t, "promoted, same session", r, 200)
	v1 := permOf(t, r)
	if v1 == v0 {
		t.Error("promotion did not change the permission version")
	}
	if me := e.mustOK(e.get("/api/me", priya)); me.str("user", "role") != "director" {
		t.Errorf("/api/me role = %q", me.str("user", "role"))
	}

	e.mustOK(e.do("PATCH", "/api/admin/users/user-priya", map[string]any{"role": "officer"}, lead))
	r = e.get("/api/admin/users", priya)
	expectStatus(t, "demoted, same session", r, 403)
	if permOf(t, r) != v0 {
		t.Error("demotion back to officer should restore the officer version")
	}
	expectStatus(t, "demoted officer still signed in", e.get("/api/me", priya), 200)
}

func TestDeactivationEndsTheSessionOnTheNextRequest(t *testing.T) {
	e := newEnv(t)
	ava := e.as("user-ava")
	e.mustOK(e.get("/api/me", ava))
	e.mustOK(e.do("PATCH", "/api/admin/users/user-ava", map[string]any{"active": false}, e.director()))
	expectStatus(t, "deactivated", e.get("/api/me", ava), 401)
	expectStatus(t, "deactivated, any page", e.get("/api/events", ava), 401)
}

func TestOwnerAndSubteamChangesBumpThePermissionVersion(t *testing.T) {
	e := newEnv(t)
	owner := e.as("user-leadership")
	e.mustOK(e.do("PATCH", "/api/admin/users/user-ava", map[string]any{"role": "leadership"}, owner))
	ava := e.as("user-ava")
	v0 := permOf(t, e.mustOK(e.get("/api/me", ava)))

	e.mustOK(e.post("/api/admin/owners/user-ava", nil, owner))
	me := e.mustOK(e.get("/api/me", ava))
	if me.at("user", "isOwner") != true {
		t.Error("owner flag not visible on the next request")
	}
	v1 := permOf(t, me)
	if v1 == v0 {
		t.Error("owner flag did not change the permission version")
	}
	e.mustOK(e.do("DELETE", "/api/admin/owners/user-ava", nil, owner))
	if v := permOf(t, e.mustOK(e.get("/api/me", ava))); v != v0 {
		t.Error("removing ownership should restore the earlier version")
	}

	priya := e.officer()
	p0 := permOf(t, e.mustOK(e.get("/api/me", priya)))
	e.mustOK(e.do("PATCH", "/api/officers/user-priya", map[string]any{"subteamId": "team-design"}, e.director()))
	r := e.mustOK(e.get("/api/design-requests", priya))
	if permOf(t, r) == p0 {
		t.Error("joining Design did not change the permission version")
	}
	if r.at("viewer", "isDesigner") != true {
		t.Error("Design membership not applied on the next request")
	}
}

func TestPermissionVersionOnlyForSignedInResponses(t *testing.T) {
	e := newEnv(t)
	if v := e.get("/api/health", anon).Header.Get(permVersionHeader); v != "" {
		t.Errorf("anonymous response carries %s=%q", permVersionHeader, v)
	}
	if permOf(t, e.get("/api/admin/users", e.officer())) == permOf(t, e.get("/api/admin/users", e.director())) {
		t.Error("officer and director share a version")
	}
}
