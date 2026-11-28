package api

import (
	"strings"
	"testing"

	"officerdashboard/internal/store"
)

// refused asserts a 403 from the named guard, so an unrelated refusal (for example an
// account that hasn't finished onboarding) can't pass for the rule under test.
func refused(t *testing.T, label string, r reply, message string) {
	t.Helper()
	expectStatus(t, label, r, 403)
	if got := r.str("error", "message"); r.str("error", "code") != "forbidden" || !strings.Contains(got, message) {
		t.Errorf("%s: refused with %q (code %q), want a forbidden %q", label, got, r.str("error", "code"), message)
	}
}

// onboard finishes setup for an account the test created, as the onboarding flow would.
func onboard(e *testEnv, userID string) {
	e.exec(`UPDATE users SET onboarding_version=? WHERE id=?`, store.OnboardingVersion, userID)
	e.exec(`UPDATE invites SET onboarded_at=accepted_at WHERE user_id=? AND onboarded_at IS NULL`, userID)
}

// The access email is the address Cloudflare Access will send a person's sign-in PIN to
// (PR 2 syncs it). Directors type it; officers never set or see it, their own included.

func TestAccessEmailOnAddAccountAndEdit(t *testing.T) {
	e := newEnv(t)
	created := e.mustOK(e.post("/api/admin/users", map[string]any{
		"discordId": "100000000000000901", "name": "Noor Ali", "subteamId": "team-events", "accessEmail": "  Noor.Ali@Example.ORG ",
	}, e.director()))
	id := created.str("user", "id")
	if got := created.str("user", "accessEmail"); got != "noor.ali@example.org" {
		t.Fatalf("created accessEmail = %q, want it trimmed and lower-cased: %s", got, created.Raw)
	}
	if after := e.scalar(`SELECT after_json FROM audit_log WHERE action='user.created' AND entity_id=?`, id).(string); !strings.Contains(after, `"accessEmail":"noor.ali@example.org"`) {
		t.Errorf("user.created audit lacks the access email: %s", after)
	}

	// Set, change and clear it on an existing officer.
	set := e.mustOK(e.do("PATCH", "/api/admin/users/user-priya", map[string]any{"accessEmail": "priya.shah@example.org"}, e.director()))
	if set.str("user", "accessEmail") != "priya.shah@example.org" {
		t.Fatalf("patch: %s", set.Raw)
	}
	audit := e.scalar(`SELECT before_json || '|' || after_json FROM audit_log WHERE action='user.updated' AND entity_id='user-priya' ORDER BY created_at DESC, rowid DESC LIMIT 1`).(string)
	beforeJSON, afterJSON, _ := strings.Cut(audit, "|")
	if !strings.Contains(beforeJSON, `"accessEmail":null`) || !strings.Contains(afterJSON, `"accessEmail":"priya.shah@example.org"`) {
		t.Errorf("user.updated audit doesn't show the change: %s", audit)
	}
	cleared := e.mustOK(e.do("PATCH", "/api/admin/users/user-priya", map[string]any{"accessEmail": nil}, e.director()))
	if cleared.at("user", "accessEmail") != nil {
		t.Fatalf("clear: %s", cleared.Raw)
	}
	// Patching something else leaves it alone, and so does the calendar email: neither fills the other in.
	e.mustOK(e.do("PATCH", "/api/admin/users/user-priya", map[string]any{"accessEmail": "priya.shah@example.org"}, e.director()))
	e.mustOK(e.do("PATCH", "/api/admin/users/user-priya", map[string]any{"title": "VP Events", "email": "p@example.org"}, e.director()))
	if got := e.scalar(`SELECT access_email FROM users WHERE id='user-priya'`); got != "priya.shah@example.org" {
		t.Fatalf("access email after an unrelated edit = %v", got)
	}

	expectStatus(t, "not an email", e.do("PATCH", "/api/admin/users/user-priya", map[string]any{"accessEmail": "priya"}, e.director()), 400)
	refused(t, "officer edits own", e.do("PATCH", "/api/admin/users/user-priya", map[string]any{"accessEmail": "x@example.org"}, e.officer()), "director role")
	expectStatus(t, "officer adds account", e.post("/api/admin/users", map[string]any{
		"discordId": "100000000000000902", "name": "X", "subteamId": "team-events", "accessEmail": "x@example.org",
	}, e.officer()), 403)
}

func TestAccessEmailClashNamesTheOtherPerson(t *testing.T) {
	e := newEnv(t)
	e.mustOK(e.do("PATCH", "/api/admin/users/user-priya", map[string]any{"accessEmail": "priya@example.org"}, e.director()))
	r := e.do("PATCH", "/api/admin/users/user-marcus", map[string]any{"accessEmail": "PRIYA@example.org"}, e.director())
	expectStatus(t, "clash on edit", r, 409)
	if !strings.Contains(string(r.Raw), "Priya Shah") {
		t.Errorf("409 should name who has it: %s", r.Raw)
	}
	r = e.post("/api/admin/users", map[string]any{"discordId": "100000000000000903", "name": "Dup", "subteamId": "team-events", "accessEmail": "priya@example.org"}, e.director())
	expectStatus(t, "clash on add", r, 409)
	if !strings.Contains(string(r.Raw), "Priya Shah") {
		t.Errorf("409 should name who has it: %s", r.Raw)
	}
	// Saving your own current value again is not a clash.
	e.mustOK(e.do("PATCH", "/api/admin/users/user-priya", map[string]any{"accessEmail": "priya@example.org"}, e.director()))
	// An officer probing for the clash learns nothing: refused before the lookup.
	r = e.do("PATCH", "/api/admin/users/user-marcus", map[string]any{"accessEmail": "priya@example.org"}, e.officer())
	refused(t, "officer probe", r, "director role")
	if strings.Contains(string(r.Raw), "Priya") {
		t.Errorf("an officer learned who holds the email: %s", r.Raw)
	}
}

func TestAccessEmailOfDirectorsLeadershipAndOwners(t *testing.T) {
	e := newEnv(t)
	body := map[string]any{"accessEmail": "jordan@example.org"}
	// A director can't change a director's (their own included) or leadership's.
	refused(t, "director edits own", e.do("PATCH", "/api/admin/users/user-director", body, e.director()), "Requires leadership role")
	refused(t, "director edits leadership", e.do("PATCH", "/api/admin/users/user-leadership", map[string]any{"accessEmail": "sam@example.org"}, e.director()), "Requires leadership role")
	e.mustOK(e.do("PATCH", "/api/admin/users/user-director", body, e.as("user-leadership")))
	// Only an owner changes an owner's. user-leadership is the seeded owner.
	lead := e.mustOK(e.post("/api/admin/users", map[string]any{"discordId": "100000000000000904", "name": "Lee Kim", "subteamId": "team-events", "role": "leadership"}, e.as("user-leadership")))
	onboard(e, lead.str("user", "id"))
	other := e.as(lead.str("user", "id"))
	// Non-owner leadership may set leadership's (their own included) but not an owner's.
	e.mustOK(e.do("PATCH", "/api/admin/users/"+lead.str("user", "id"), map[string]any{"accessEmail": "lee@example.org"}, other))
	e.mustOK(e.do("PATCH", "/api/admin/users/user-director", map[string]any{"accessEmail": "jordan.lee@example.org"}, other))
	refused(t, "leadership edits owner", e.do("PATCH", "/api/admin/users/user-leadership", map[string]any{"accessEmail": "sam@example.org"}, other), "Requires dashboard owner")
	e.mustOK(e.do("PATCH", "/api/admin/users/user-leadership", map[string]any{"accessEmail": "sam@example.org"}, e.as("user-leadership")))
	// Adding a director with an access email needs leadership, like adding a director at all.
	refused(t, "director adds a director", e.post("/api/admin/users", map[string]any{"discordId": "100000000000000905", "name": "D", "subteamId": "team-events", "role": "director", "accessEmail": "d@example.org"}, e.director()), "Requires leadership role")
}

func inviteWithEmail(method, email string) map[string]any {
	b := inviteBody(method)
	b["accessEmail"] = email
	return b
}

func TestAccessEmailOnInvitesCarriesOverAtSignIn(t *testing.T) {
	e := discordEnv(t, `{"id":"`+inviteeID+`","username":"alex","global_name":"Alex"}`, nil)
	inv := e.mustOK(e.post("/api/invites", inviteWithEmail("link", " Alex@Example.org"), e.director()))
	id := inv.str("invite", "id")
	if inv.str("invite", "accessEmail") != "alex@example.org" {
		t.Fatalf("invite: %s", inv.Raw)
	}
	if after := e.scalar(`SELECT after_json FROM audit_log WHERE action='invite.created' AND entity_id=?`, id).(string); !strings.Contains(after, `"accessEmail":"alex@example.org"`) {
		t.Errorf("invite.created audit lacks the access email: %s", after)
	}
	listed := e.mustOK(e.get("/api/invites", e.director()))
	if !strings.Contains(string(listed.Raw), `"accessEmail":"alex@example.org"`) {
		t.Errorf("invite list lacks the access email: %s", listed.Raw)
	}
	// Edit details: the email changes and the audit says so; leaving it out keeps it.
	ed := e.mustOK(e.do("PATCH", "/api/invites/"+id, map[string]any{"accessEmail": "alex.chen@example.org"}, e.director()))
	if ed.str("invite", "accessEmail") != "alex.chen@example.org" {
		t.Fatalf("edit: %s", ed.Raw)
	}
	if a := e.scalar(`SELECT before_json || '|' || after_json FROM audit_log WHERE action='invite.updated' AND entity_id=? ORDER BY created_at DESC, rowid DESC LIMIT 1`, id).(string); !strings.Contains(a, "alex.chen@example.org") {
		t.Errorf("invite.updated audit lacks the change: %s", a)
	}
	e.mustOK(e.do("PATCH", "/api/invites/"+id, map[string]any{"name": "Alex Chen"}, e.director()))
	// A re-invite without the field keeps the email; with it, replaces it.
	re := e.mustOK(e.post("/api/invites", inviteBody("link"), e.director()))
	if re.str("invite", "accessEmail") != "alex.chen@example.org" {
		t.Fatalf("re-invite dropped the email: %s", re.Raw)
	}
	// An explicit null is a deliberate clear (the UI sends it only from "Edit invite details").
	cleared := inviteBody("link")
	cleared["accessEmail"] = nil
	if re = e.mustOK(e.post("/api/invites", cleared, e.director())); re.at("invite", "accessEmail") != nil {
		t.Fatalf("re-invite with null kept the email: %s", re.Raw)
	}
	re = e.mustOK(e.post("/api/invites", inviteWithEmail("link", "alex@example.org"), e.director()))
	if re.str("invite", "accessEmail") != "alex@example.org" {
		t.Fatalf("re-invite with a new email: %s", re.Raw)
	}
	// Accepting copies it to the new account in the same sign-in.
	cb := signInAsInvitee(t, e, "invite="+re.str("token")+"&next=/")
	if cb.Header.Get("Location") != "/onboarding" {
		t.Fatalf("invitee → %s", cb.Header.Get("Location"))
	}
	userID := e.scalar(`SELECT id FROM users WHERE discord_id=?`, inviteeID).(string)
	if got := e.scalar(`SELECT access_email FROM users WHERE id=?`, userID); got != "alex@example.org" {
		t.Fatalf("account access email = %v, want the invite's", got)
	}
	if a := e.scalar(`SELECT after_json FROM audit_log WHERE action='user.created' AND entity_id=?`, userID).(string); !strings.Contains(a, `"accessEmail":"alex@example.org"`) {
		t.Errorf("user.created (via invite) audit lacks the access email: %s", a)
	}
	// After acceptance the account holds it: the invite can't change it any more.
	expectStatus(t, "edit accepted invite email", e.do("PATCH", "/api/invites/"+id, map[string]any{"accessEmail": "z@example.org"}, e.director()), 409)
}

func TestAccessEmailInviteClashes(t *testing.T) {
	e := newEnv(t)
	e.mustOK(e.do("PATCH", "/api/admin/users/user-priya", map[string]any{"accessEmail": "priya@example.org"}, e.director()))
	r := e.post("/api/invites", inviteWithEmail("link", "priya@example.org"), e.director())
	expectStatus(t, "invite with an account's email", r, 409)
	if !strings.Contains(string(r.Raw), "Priya Shah") {
		t.Errorf("409 should name the account: %s", r.Raw)
	}
	e.mustOK(e.post("/api/invites", inviteWithEmail("link", "alex@example.org"), e.director()))
	other := map[string]any{"name": "Bo Diaz", "discordId": "100000000000000778", "subteamId": "team-events", "method": "link", "accessEmail": "alex@example.org"}
	r = e.post("/api/invites", other, e.director())
	expectStatus(t, "two invites, one email", r, 409)
	if !strings.Contains(string(r.Raw), "Alex Chen") {
		t.Errorf("409 should name the invite: %s", r.Raw)
	}
	r = e.do("PATCH", "/api/admin/users/user-marcus", map[string]any{"accessEmail": "alex@example.org"}, e.director())
	expectStatus(t, "account takes an invite's email", r, 409)
	if !strings.Contains(string(r.Raw), "Alex Chen") {
		t.Errorf("409 should name the invite: %s", r.Raw)
	}
	// Editing a pending invite onto someone else's email names them too.
	bo := map[string]any{"name": "Bo Diaz", "discordId": "100000000000000778", "subteamId": "team-events", "method": "link", "accessEmail": "bo@example.org"}
	boID := e.mustOK(e.post("/api/invites", bo, e.director())).str("invite", "id")
	for email, holder := range map[string]string{"priya@example.org": "Priya Shah", "ALEX@example.org": "Alex Chen"} {
		r = e.do("PATCH", "/api/invites/"+boID, map[string]any{"accessEmail": email}, e.director())
		expectStatus(t, "invite edit onto "+holder+"'s email", r, 409)
		if !strings.Contains(string(r.Raw), holder) {
			t.Errorf("409 should name %s: %s", holder, r.Raw)
		}
	}
	if got := e.scalar(`SELECT access_email FROM invites WHERE id=?`, boID); got != "bo@example.org" {
		t.Errorf("a refused edit changed the invite's email to %v", got)
	}
	refused(t, "officer invites", e.post("/api/invites", other, e.officer()), "director role")
	expectStatus(t, "not an email", e.post("/api/invites", inviteWithEmail("link", "alex"), e.director()), 400)
}

// TestAccessEmailNeverReachesOfficersOrAgents walks every read surface that carries
// officer or user data. Directors see it on the Officers page; nothing else does.
func TestAccessEmailNeverReachesOfficersOrAgents(t *testing.T) {
	e := newEnv(t)
	const secret = "zz-access-only@example.org" // appears nowhere else in the seed
	e.mustOK(e.do("PATCH", "/api/admin/users/user-priya", map[string]any{"accessEmail": secret}, e.director()))
	e.mustOK(e.do("PATCH", "/api/admin/users/user-marcus", map[string]any{"accessEmail": "zz-marcus@example.org"}, e.director()))

	// Directors in the browser: the Officers list and drawer carry it.
	dir := e.mustOK(e.get("/api/officers", e.director()))
	if !strings.Contains(string(dir.Raw), `"accessEmail":"`+secret+`"`) {
		t.Errorf("director's officer list lacks the access email: %s", dir.Raw)
	}
	if one := e.mustOK(e.get("/api/officers/user-priya", e.director())); one.str("officer", "accessEmail") != secret {
		t.Errorf("director's officer detail: %s", one.Raw)
	}

	leaks := func(label string, raw []byte) {
		t.Helper()
		if strings.Contains(string(raw), "accessEmail") || strings.Contains(string(raw), "zz-") {
			t.Errorf("%s exposes an access email: %s", label, raw)
		}
	}
	// Officers, about others and about themselves (user-priya is the officer actor).
	off := e.officer()
	for _, path := range []string{"/api/officers", "/api/officers/user-priya", "/api/officers/user-marcus", "/api/officers/user-priya/activity",
		"/api/me", "/api/me/profile", "/api/search?q=zz-", "/api/search?q=Priya", "/api/members", "/api/members/export"} {
		r := e.get(path, off)
		leaks("officer GET "+path, r.Raw)
	}
	for _, path := range []string{"/api/admin/users", "/api/invites", "/api/admin/audit"} {
		r := e.get(path, off)
		refused(t, "officer GET "+path, r, "director role")
		leaks("officer GET "+path, r.Raw)
	}
	// The officer's own profile save and an officer-reachable write echo nothing either.
	p := e.do("PUT", "/api/me/profile", map[string]any{"phone": "+1 206 555 0100", "birthday": "01-15", "accessEmail": "mine@example.org"}, off)
	leaks("officer PUT /api/me/profile", p.Raw)
	if got := e.scalar(`SELECT access_email FROM users WHERE id='user-priya'`); got != secret {
		t.Errorf("the profile save changed the access email to %v", got)
	}
	// Search can't be used as an oracle: an access email matches nobody.
	if hits := e.mustOK(e.get("/api/search?q="+secret, e.director())); strings.Contains(string(hits.Raw), "Priya") {
		t.Errorf("search matched on the access email: %s", hits.Raw)
	}

	// Agents never see it, even a director's.
	_, tokens := connectAgent(t, e, "user-director", "Leak check")
	token := tokens.str("access_token")
	for _, call := range []struct {
		name string
		args map[string]any
	}{
		{"list_officers", map[string]any{}},
		{"get_officer", map[string]any{"id": "user-priya"}},
		{"update_officer", map[string]any{"id": "user-priya", "body": map[string]any{"title": "VP Events"}}},
		{"get_officer_activity", map[string]any{"id": "user-priya"}},
		{"search", map[string]any{"q": "Priya"}},
		{"whoami", map[string]any{}},
	} {
		text, isErr := toolCall(e, token, call.name, call.args)
		if isErr {
			t.Errorf("agent %s failed: %s", call.name, text)
		}
		leaks("director's agent "+call.name, []byte(text))
	}
	if tools := toolNames(mcpRPC(e, token, "tools/list", nil)); tools["update_user"] || tools["create_invite"] || tools["list_invites"] {
		t.Errorf("agents gained an access-managing tool: %v", tools)
	}
}

// Adding an account for a Discord ID whose invite expired keeps the email a director typed
// on that invite, unless the request sets one itself (a value, or null for none).
func TestAccessEmailAddAccountOverExpiredInvite(t *testing.T) {
	e := newEnv(t)
	expire := func(discordID, email string) {
		t.Helper()
		e.mustOK(e.post("/api/invites", map[string]any{"name": "Old Invite", "discordId": discordID, "subteamId": "team-events", "method": "link", "accessEmail": email}, e.director()))
		e.exec(`UPDATE invites SET expires_at=1 WHERE discord_id=?`, discordID)
	}
	add := func(discordID string, extra map[string]any) reply {
		t.Helper()
		body := map[string]any{"discordId": discordID, "name": "Kim Lee", "subteamId": "team-events"}
		for k, v := range extra {
			body[k] = v
		}
		return e.mustOK(e.post("/api/admin/users", body, e.director()))
	}

	expire("100000000000000911", "kim@example.org")
	carried := add("100000000000000911", nil)
	if carried.str("user", "accessEmail") != "kim@example.org" {
		t.Fatalf("the expired invite's email wasn't carried over: %s", carried.Raw)
	}
	if a := e.scalar(`SELECT after_json FROM audit_log WHERE action='user.created' AND entity_id=?`, carried.str("user", "id")).(string); !strings.Contains(a, `"accessEmailFrom":"expired invite"`) {
		t.Errorf("user.created doesn't say where the email came from: %s", a)
	}

	expire("100000000000000912", "lee@example.org")
	if got := add("100000000000000912", map[string]any{"accessEmail": "lee.k@example.org"}).str("user", "accessEmail"); got != "lee.k@example.org" {
		t.Errorf("the request's own email lost to the invite's: %q", got)
	}

	expire("100000000000000913", "mo@example.org")
	if got := add("100000000000000913", map[string]any{"accessEmail": nil}).at("user", "accessEmail"); got != nil {
		t.Errorf("an explicit null still carried the invite's email: %v", got)
	}
}
