package api

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/seed"
	"officerdashboard/internal/store"
)

// portalLogin creates a portal session row for an account and returns the cookie.
func (e *testEnv) portalLogin(accountID string) actor {
	e.t.Helper()
	token := auth.RandomToken(32)
	err := e.db.Write(context.Background(), func(w *store.Queries) error {
		return w.InsertLaunchpadSession(context.Background(), auth.SHA256Hex(token), accountID, e.clock.Add(24*time.Hour).UnixMilli(), "test", e.clock.UnixMilli())
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return actor{cookie: &http.Cookie{Name: e.srv.portalCookieName(), Value: token}}
}

func (e *testEnv) exec(q string, args ...any) {
	e.t.Helper()
	if _, err := e.db.W.Exec(q, args...); err != nil {
		e.t.Fatalf("%s: %v", q, err)
	}
}

// activate binds a pending invite as if its Discord sign-in had happened.
func (e *testEnv) activate(accountID, discordID string) actor {
	e.exec(`UPDATE buildspace_launchpad_accounts SET status='active', discord_id=?, accepted_at=1 WHERE id=?`, discordID, accountID)
	return e.portalLogin(accountID)
}

func inviteToken(t *testing.T, r reply) string {
	t.Helper()
	u, err := url.Parse(r.str("links", 0, "url"))
	if err != nil || u.Query().Get("token") == "" {
		t.Fatalf("no invite link: %s", r.Raw)
	}
	return u.Query().Get("token")
}

func TestLaunchpadCohortLifecycle(t *testing.T) {
	e := newEnv(t)
	officer, director := e.officer(), e.director()
	list := e.mustOK(e.get("/api/buildspace-launchpad", officer))
	if list.str("cohortId") != "cohort-fall-2026" || len(list.list("cohorts")) != 2 || len(list.list("projects")) != 6 {
		t.Fatalf("default cohort: %s %d %d", list.str("cohortId"), len(list.list("cohorts")), len(list.list("projects")))
	}
	spring := e.mustOK(e.get("/api/buildspace-launchpad?cohort=cohort-spring-2026", officer))
	if len(spring.list("projects")) != 1 || spring.str("projects", 0, "id") != "bs-lumen" {
		t.Fatalf("spring: %s", spring.Raw)
	}
	expectStatus(t, "unknown cohort", e.get("/api/buildspace-launchpad?cohort=nope", officer), 404)
	expectStatus(t, "bad archived flag", e.get("/api/buildspace-launchpad?archived=yes", officer), 400)

	body := map[string]any{"name": "Winter 2027", "startsOn": "2027-01-04", "endsOn": "2027-03-19"}
	expectStatus(t, "officer cannot create cohorts", e.post("/api/buildspace-launchpad/cohorts", body, officer), 403)
	created := e.mustOK(e.post("/api/buildspace-launchpad/cohorts", body, director))
	id := created.str("cohort", "id")
	if created.str("cohort", "status") != "active" || created.num("cohort", "counts", "projects") != 0 {
		t.Fatalf("created %s", created.Raw)
	}
	expectStatus(t, "duplicate name", e.post("/api/buildspace-launchpad/cohorts", with(body, "name", "winter 2027"), director), 409)
	expectStatus(t, "ends before start", e.post("/api/buildspace-launchpad/cohorts", with(body, "name", "X", "endsOn", "2026-12-01"), director), 400)
	if got := e.mustOK(e.get("/api/buildspace-launchpad", officer)).str("cohortId"); got != id {
		t.Fatalf("newest active cohort should be default, got %s", got)
	}
	project := map[string]any{"name": "Winter project", "description": "", "track": "Software", "stage": "Ideating", "links": []string{}, "mentorIds": []string{}, "cohortId": id}
	e.mustOK(e.do("PUT", "/api/buildspace-launchpad/projects/winter-p", project, officer))
	project["cohortId"] = "cohort-fall-2026"
	e.mustOK(e.do("PUT", "/api/buildspace-launchpad/projects/winter-p", project, officer))
	if e.scalar(`SELECT cohort_id FROM buildspace_launchpad_projects WHERE id='winter-p'`) != id {
		t.Fatal("a project's cohort must never change")
	}
	e.mustOK(e.do("PATCH", "/api/buildspace-launchpad/cohorts/"+id, map[string]any{"status": "archived"}, director))
	expectStatus(t, "archived cohort is read-only", e.do("PUT", "/api/buildspace-launchpad/projects/winter-p", project, officer), 409)
	expectStatus(t, "no new projects in archived cohort", e.do("PUT", "/api/buildspace-launchpad/projects/winter-q", with(project, "cohortId", id), officer), 409)
	expectStatus(t, "no invites to archived cohort", e.post("/api/buildspace-launchpad/cohorts/"+id+"/invites", map[string]any{"invites": []any{map[string]any{"email": "a@uw.edu"}}}, officer), 409)
	e.mustOK(e.do("PATCH", "/api/buildspace-launchpad/cohorts/"+id, map[string]any{"status": "active", "name": "Winter 2027 cohort", "endsOn": nil}, director))
	expectStatus(t, "non-empty cohort", e.do("DELETE", "/api/buildspace-launchpad/cohorts/"+id, nil, director), 409)
	e.mustOK(e.do("DELETE", "/api/buildspace-launchpad/projects/winter-p", nil, director))
	e.mustOK(e.do("DELETE", "/api/buildspace-launchpad/cohorts/"+id, nil, director))
	if e.scalar(`SELECT count(*) FROM audit_log WHERE action LIKE 'buildspace_launchpad.cohort.%'`).(int64) < 4 {
		t.Fatal("cohort changes are audited")
	}
}

func TestLaunchpadInvitesEmailLinksAndAccounts(t *testing.T) {
	m := &fakeMailer{}
	e := newEnv(t, func(c *Config) { c.Mailer = m })
	officer, director := e.officer(), e.director()
	path := "/api/buildspace-launchpad/cohorts/cohort-fall-2026/invites"
	expectStatus(t, "invalid email", e.post(path, map[string]any{"invites": []any{map[string]any{"email": "not-an-email"}}}, officer), 400)
	r := e.mustOK(e.post(path, map[string]any{"invites": []any{
		map[string]any{"email": "New.Person@UW.edu", "name": "New Person"},
		map[string]any{"email": "new.person@uw.edu"},
		map[string]any{"email": "alex.kim@uw.edu"},
		map[string]any{"email": "sky.morales@uw.edu"},
	}}, officer))
	if len(r.list("accounts")) != 2 || len(r.list("links")) != 0 || r.at("emailConfigured") != true {
		t.Fatalf("invite result %s", r.Raw)
	}
	reasons := map[string]string{}
	for _, s := range r.list("skipped") {
		v := s.(map[string]any)
		reasons[v["email"].(string)] = v["reason"].(string)
	}
	if reasons["new.person@uw.edu"] != "duplicate" || reasons["alex.kim@uw.edu"] != "already_active" {
		t.Fatalf("skips %v", reasons)
	}
	e.srv.WaitBackground()
	mails := m.all()
	if len(mails) != 2 || !strings.Contains(mails[0].Text+mails[1].Text, "/launchpad/join?token=") || mails[0].ICS != "" {
		t.Fatalf("mails %+v", mails)
	}
	if e.scalar(`SELECT count(*) FROM buildspace_launchpad_accounts WHERE invite_sent_at IS NOT NULL AND cohort_id='cohort-fall-2026' AND status='invited'`).(int64) != 2 {
		t.Fatal("delivery not recorded")
	}
	// The re-issued seeded invite's old token no longer works.
	old := e.mustOK(e.get("/api/portal/invite?token="+seed.SeededInviteToken, anon))
	if old.at("valid") != false || old.str("reason") != "not_found" {
		t.Fatalf("old token %s", old.Raw)
	}
	links := e.mustOK(e.post(path, map[string]any{"invites": []any{map[string]any{"email": "link.only@uw.edu"}}, "method": "link"}, officer))
	token := inviteToken(t, links)
	e.srv.WaitBackground()
	if len(m.all()) != 2 {
		t.Fatal("link method must not email")
	}
	prev := e.mustOK(e.get("/api/portal/invite?token="+token, anon))
	if prev.at("valid") != true || prev.str("kind") != "builder" || prev.str("cohortName") != "Fall 2026" || prev.str("emailHint") != "l•••@uw.edu" {
		t.Fatalf("preview %s", prev.Raw)
	}
	accID := links.str("accounts", 0, "id")
	resent := e.mustOK(e.post("/api/buildspace-launchpad/accounts/"+accID+"/resend", nil, officer))
	if resent.str("link", "url") == "" {
		t.Fatal("resend returns a link")
	}
	if e.mustOK(e.get("/api/portal/invite?token="+token, anon)).at("valid") != false {
		t.Fatal("resend must rotate the token")
	}
	expectStatus(t, "officer cannot disable", e.post("/api/buildspace-launchpad/accounts/lp-account-alex/disable", nil, officer), 403)
	alex := e.portalLogin("lp-account-alex")
	e.mustOK(e.get("/api/portal/me", alex))
	e.mustOK(e.post("/api/buildspace-launchpad/accounts/lp-account-alex/disable", nil, director))
	expectStatus(t, "disabled account signed out", e.get("/api/portal/me", alex), 401)
	expectStatus(t, "revoke an active account", e.do("DELETE", "/api/buildspace-launchpad/accounts/lp-account-alex", nil, officer), 409)
	en := e.mustOK(e.post("/api/buildspace-launchpad/accounts/lp-account-alex/enable", nil, director))
	if en.str("account", "status") != "active" {
		t.Fatal(string(en.Raw))
	}
	e.mustOK(e.do("DELETE", "/api/buildspace-launchpad/accounts/"+accID, nil, officer))
	expectStatus(t, "officer cannot revoke mentor accounts", e.do("DELETE", "/api/buildspace-launchpad/accounts/lp-account-maya", nil, officer), 403)

	// Mentor invites default to the contact's email; directors only.
	expectStatus(t, "officer mentor invite", e.post("/api/buildspace-launchpad/mentors/bs-mentor-jordan/invite", map[string]any{}, officer), 403)
	mi := e.mustOK(e.post("/api/buildspace-launchpad/mentors/bs-mentor-jordan/invite", map[string]any{"method": "both"}, director))
	if mi.str("account", "email") != "jordan.ellis@example.org" || mi.str("account", "kind") != "mentor" || mi.str("link", "url") == "" {
		t.Fatalf("mentor invite %s", mi.Raw)
	}
	expectStatus(t, "active mentor", e.post("/api/buildspace-launchpad/mentors/bs-mentor-maya/invite", map[string]any{}, director), 409)
	list := e.mustOK(e.get("/api/buildspace-launchpad", officer))
	for _, v := range list.list("mentors") {
		mm := v.(map[string]any)
		if mm["id"] == "bs-mentor-jordan" && mm["account"].(map[string]any)["status"] != "invited" {
			t.Fatalf("roster account %v", mm)
		}
	}
}

func TestLaunchpadInvitesWithoutMailerReturnLinks(t *testing.T) {
	e := newEnv(t)
	r := e.mustOK(e.post("/api/buildspace-launchpad/cohorts/cohort-fall-2026/invites", map[string]any{"invites": []any{map[string]any{"email": "a@uw.edu"}}}, e.officer()))
	if r.at("emailConfigured") != false || len(r.list("links")) != 1 {
		t.Fatalf("%s", r.Raw)
	}
}

// oauthPortal runs /auth/login with extra query parameters and the callback; it returns the reply.
func oauthPortal(t *testing.T, e *testEnv, query string, extra ...*http.Cookie) reply {
	t.Helper()
	r := e.get("/auth/login?"+query, anon)
	if r.Status != 302 {
		t.Fatalf("login → %d", r.Status)
	}
	loc, _ := url.Parse(r.Header.Get("Location"))
	cs := append(setCookies(r), extra...)
	return e.get("/auth/callback?code=good-code&state="+loc.Query().Get("state"), actor{cookies: cs})
}

func linkInvite(t *testing.T, e *testEnv, cohort, email string) string {
	t.Helper()
	return inviteToken(t, e.mustOK(e.post("/api/buildspace-launchpad/cohorts/"+cohort+"/invites", map[string]any{"invites": []any{map[string]any{"email": email}}, "method": "link"}, e.officer())))
}

func TestLaunchpadInviteBindsDiscordAndRefusals(t *testing.T) {
	e := discordEnv(t, `{"id":"300000000000000001","username":"newbie"}`, nil)
	token := linkInvite(t, e, "cohort-fall-2026", "newbie@uw.edu")
	cb := oauthPortal(t, e, "launchpad="+token)
	if cb.Status != 302 || cb.Header.Get("Location") != "/launchpad" {
		t.Fatalf("accept → %d %s", cb.Status, cb.Header.Get("Location"))
	}
	ck := cookieNamed(setCookies(cb), e.srv.portalCookieName())
	if ck == nil || cookieNamed(setCookies(cb), e.srv.sessionCookieName()) != nil {
		t.Fatal("an invite gives a portal session, never an officer session")
	}
	me := e.mustOK(e.get("/api/portal/me", actor{cookie: ck}))
	if me.str("account", "kind") != "builder" || me.at("account", "onboarded") != false || me.at("officer") != false {
		t.Fatalf("me %s", me.Raw)
	}
	expectStatus(t, "portal cookie on officer API", e.get("/api/buildspace-launchpad", actor{cookie: ck}), 401)
	expectStatus(t, "portal cookie on /api/me", e.get("/api/me", actor{cookie: ck}), 401)
	// Opening the same link again with the same Discord account just signs in.
	if again := oauthPortal(t, e, "launchpad="+token); again.Header.Get("Location") != "/launchpad" {
		t.Fatalf("reopen → %s", again.Header.Get("Location"))
	}
	// A second invite in an active cohort cannot take the same Discord account.
	second := linkInvite(t, e, "cohort-fall-2026", "newbie.two@uw.edu")
	if r := oauthPortal(t, e, "launchpad="+second); r.Header.Get("Location") != "/launchpad/join?error=discord_taken" {
		t.Fatalf("taken → %s", r.Header.Get("Location"))
	}
	expired := linkInvite(t, e, "cohort-fall-2026", "late@uw.edu")
	e.clock = e.clock.Add(15 * 24 * time.Hour)
	if r := oauthPortal(t, e, "launchpad="+expired); r.Header.Get("Location") != "/launchpad/join?error=expired" {
		t.Fatalf("expired → %s", r.Header.Get("Location"))
	}
	if r := oauthPortal(t, e, "launchpad=nonsense"); r.Header.Get("Location") != "/launchpad/join?error=invalid" {
		t.Fatalf("invalid → %s", r.Header.Get("Location"))
	}
	// Later sign-ins: the portal button, and the officer button falling through.
	if r := oauthPortal(t, e, "portal=1"); r.Header.Get("Location") != "/launchpad" {
		t.Fatalf("portal sign-in → %s", r.Header.Get("Location"))
	}
	if r := oauthPortal(t, e, "next=/"); r.Header.Get("Location") != "/launchpad" || cookieNamed(setCookies(r), e.srv.portalCookieName()) == nil {
		t.Fatalf("officer button fallthrough → %s", r.Header.Get("Location"))
	}
	if e.scalar(`SELECT count(*) FROM audit_log WHERE action='buildspace_launchpad.account.accepted' AND actor_user_id IS NULL`).(int64) != 1 {
		t.Fatal("acceptance audited without an officer actor")
	}
}

func TestLaunchpadReturningBuilderMovesFromArchivedCohort(t *testing.T) {
	e := discordEnv(t, `{"id":"300000000000000002","username":"returning"}`, nil)
	e.exec(`INSERT INTO buildspace_launchpad_accounts(id,kind,email,name,status,cohort_id,person_id,discord_id,created_at,updated_at) VALUES('old','builder','rowan@uw.edu','Rowan','active','cohort-spring-2026','bs-person-13','300000000000000002',1,1)`)
	token := linkInvite(t, e, "cohort-fall-2026", "rowan@uw.edu")
	if r := oauthPortal(t, e, "launchpad="+token); r.Header.Get("Location") != "/launchpad" {
		t.Fatalf("→ %s", r.Header.Get("Location"))
	}
	if e.scalar(`SELECT status FROM buildspace_launchpad_accounts WHERE id='old'`) != "disabled" || e.scalar(`SELECT discord_id FROM buildspace_launchpad_accounts WHERE id='old'`) != nil {
		t.Fatal("old cohort account not detached")
	}
}

// A returning builder's old record (archived cohort) still carries their Discord ID, which is
// unique: onboarding into the new cohort takes it over instead of refusing.
func TestLaunchpadReturningBuilderOnboardsWithSameDiscord(t *testing.T) {
	e := discordEnv(t, `{"id":"300000000000000002","username":"returning"}`, nil)
	e.exec(`UPDATE buildspace_launchpad_people SET discord_id='300000000000000002' WHERE id='bs-person-13'`)
	e.exec(`INSERT INTO buildspace_launchpad_accounts(id,kind,email,name,status,cohort_id,person_id,discord_id,created_at,updated_at) VALUES('old','builder','rowan@uw.edu','Rowan','active','cohort-spring-2026','bs-person-13','300000000000000002',1,1)`)
	token := linkInvite(t, e, "cohort-fall-2026", "rowan@uw.edu")
	cb := oauthPortal(t, e, "launchpad="+token)
	ck := cookieNamed(setCookies(cb), e.srv.portalCookieName())
	if cb.Header.Get("Location") != "/launchpad" || ck == nil {
		t.Fatalf("accept → %s", cb.Header.Get("Location"))
	}
	e.mustOK(e.post("/api/portal/builder/onboarding", map[string]any{"name": "Rowan Diaz", "pronouns": "", "designation": "Founder", "bio": "",
		"project": map[string]any{"mode": "join", "projectId": "bs-sprout"}}, actor{cookie: ck}))
	if e.scalar(`SELECT discord_id FROM buildspace_launchpad_people WHERE id='bs-person-13'`) != nil {
		t.Fatal("old record keeps the Discord ID")
	}
	if e.scalar(`SELECT b.cohort_id||'|'||b.discord_id FROM buildspace_launchpad_people b JOIN buildspace_launchpad_accounts a ON a.person_id=b.id WHERE a.email='rowan@uw.edu' AND a.status='active'`) != "cohort-fall-2026|300000000000000002" {
		t.Fatal("new record not created in the new cohort")
	}
}

// An officer-added builder (this cohort, no portal account) links to the account on onboarding
// when they pick the project their updates are on (M-018: another or a new project is 409, as
// for an officer moving them); an archived one is left for an officer.
func TestLaunchpadOnboardingLinksExistingBuilder(t *testing.T) {
	e := newEnv(t)
	people := func() int64 { return e.scalar(`SELECT count(*) FROM buildspace_launchpad_people`).(int64) }
	before := people()
	sky := e.activate("lp-invite-sky", "100000000000000002") // bs-person-02's Discord ID
	activity := func() string {
		return e.scalar(`SELECT group_concat(id||":"||project_id) FROM (SELECT id, project_id FROM buildspace_launchpad_activity WHERE person_id='bs-person-02' ORDER BY id)`).(string)
	}
	history := activity()
	for name, project := range map[string]map[string]any{
		"another project": {"mode": "join", "projectId": "bs-sprout"},
		"a new project":   {"mode": "new", "name": "Fresh idea", "description": "", "track": "Software", "links": []string{}},
	} {
		expectStatus(t, "onboard with history into "+name, e.post("/api/portal/builder/onboarding", map[string]any{"name": "Sky Morales", "pronouns": "she/her", "designation": "Lead", "bio": "", "project": project}, sky), 409)
	}
	if e.scalar(`SELECT person_id FROM buildspace_launchpad_accounts WHERE id='lp-invite-sky'`) != nil || people() != before || activity() != history ||
		e.scalar(`SELECT project_id FROM buildspace_launchpad_people WHERE id='bs-person-02'`) != "bs-moss" {
		t.Fatal("a refused onboarding changed rows")
	}
	e.mustOK(e.post("/api/portal/builder/onboarding", map[string]any{"name": "Sky Morales", "pronouns": "she/her", "designation": "Lead", "bio": "",
		"project": map[string]any{"mode": "join", "projectId": "bs-moss"}}, sky))
	if e.scalar(`SELECT person_id FROM buildspace_launchpad_accounts WHERE id='lp-invite-sky'`) != "bs-person-02" || people() != before {
		t.Fatal("did not link the existing builder record")
	}
	if e.scalar(`SELECT name||'|'||project_id FROM buildspace_launchpad_people WHERE id='bs-person-02'`) != "Sky Morales|bs-moss" {
		t.Fatal("linked record not updated from onboarding")
	}
	e.exec(`UPDATE buildspace_launchpad_people SET archived_at=1 WHERE id='bs-person-03'`)
	dev := e.activate("lp-invite-dev", "100000000000000003")
	expectStatus(t, "archived record with this Discord ID", e.post("/api/portal/builder/onboarding", map[string]any{"name": "Dev", "pronouns": "", "designation": "", "bio": "",
		"project": map[string]any{"mode": "join", "projectId": "bs-sprout"}}, dev), 409)
}

// Reopening an accepted invite signs in through the same checks as any returning sign-in.
func TestLaunchpadReopenedInviteRefusesArchivedBuilder(t *testing.T) {
	e := discordEnv(t, `{"id":"300000000000000003","username":"paused"}`, nil)
	token := linkInvite(t, e, "cohort-fall-2026", "paused@uw.edu")
	if r := oauthPortal(t, e, "launchpad="+token); r.Header.Get("Location") != "/launchpad" {
		t.Fatalf("accept → %s", r.Header.Get("Location"))
	}
	e.exec(`UPDATE buildspace_launchpad_accounts SET person_id='bs-person-02' WHERE email='paused@uw.edu'`)
	e.exec(`UPDATE buildspace_launchpad_people SET archived_at=1 WHERE id='bs-person-02'`)
	r := oauthPortal(t, e, "launchpad="+token)
	if r.Header.Get("Location") != "/launchpad/login?error=archived" || cookieNamed(setCookies(r), e.srv.portalCookieName()) != nil {
		t.Fatalf("archived reopen → %s", r.Header.Get("Location"))
	}
}

// A portal sign-in that fails before Discord answers returns to the portal page it came from.
func TestLaunchpadOAuthFailureStaysInPortal(t *testing.T) {
	e := discordEnv(t, `{"id":"300000000000000004","username":"x"}`, nil)
	for query, want := range map[string]string{"launchpad=sometoken": "/launchpad/join?error=state", "portal=1": "/launchpad/login?error=state", "next=/": "/login?error=state"} {
		r := e.get("/auth/login?"+query, anon)
		if r.Status != 302 {
			t.Fatalf("%s login → %d", query, r.Status)
		}
		cb := e.get("/auth/callback?code=good-code&state=wrong", actor{cookies: setCookies(r)})
		if cb.Header.Get("Location") != want {
			t.Errorf("%s bad state → %s, want %s", query, cb.Header.Get("Location"), want)
		}
	}
}

func TestLaunchpadOfficerAndPortalSessionsCoexist(t *testing.T) {
	e := discordEnv(t, `{"id":"100000000000000002","username":"priya"}`, nil) // the seeded officer
	if r := oauthPortal(t, e, "portal=1"); r.Header.Get("Location") != "/launchpad/login?error=not_found" {
		t.Fatalf("no portal account → %s", r.Header.Get("Location"))
	}
	token := linkInvite(t, e, "cohort-fall-2026", "priya.builder@uw.edu")
	cb := oauthPortal(t, e, "launchpad="+token)
	portalCk := cookieNamed(setCookies(cb), e.srv.portalCookieName())
	if cb.Header.Get("Location") != "/launchpad" || portalCk == nil {
		t.Fatalf("officer accepting a builder invite → %s", cb.Header.Get("Location"))
	}
	officerCb := oauthPortal(t, e, "next=/events")
	officerCk := cookieNamed(setCookies(officerCb), e.srv.sessionCookieName())
	if officerCb.Header.Get("Location") != "/events" || officerCk == nil {
		t.Fatal("the officer button still signs the officer in")
	}
	both := actor{cookies: []*http.Cookie{portalCk, officerCk}}
	if me := e.mustOK(e.get("/api/portal/me", both)); me.at("officer") != true {
		t.Fatalf("%s", me.Raw)
	}
	expectStatus(t, "officer cookie alone on portal", e.get("/api/portal/me", actor{cookie: officerCk}), 401)
	e.mustOK(e.get("/api/me", both))
}

func TestLaunchpadPortalScopingAndVisibility(t *testing.T) {
	e := newEnv(t)
	officer := e.officer()
	for vis, body := range map[string]string{"officers": "Officer-only note", "mentors": "Mentor-only note", "team": "Team note"} {
		e.mustOK(e.post("/api/buildspace-launchpad/activity", map[string]any{"projectId": "bs-moss", "kind": "request", "date": "2026-10-30", "body": body, "status": "open", "visibility": vis}, officer))
	}
	e.mustOK(e.post("/api/buildspace-launchpad/activity", map[string]any{"projectId": "bs-afterimage", "kind": "request", "date": "2026-10-30", "body": "Other project", "status": "open", "visibility": "team"}, officer))
	builder := e.portalLogin("lp-account-alex")
	b := e.mustOK(e.get("/api/portal/builder", builder))
	raw := string(b.Raw)
	if b.str("project", "id") != "bs-moss" || len(b.list("teammates")) != 1 || !strings.Contains(raw, "Team note") ||
		strings.Contains(raw, "Mentor-only") || strings.Contains(raw, "Officer-only") || strings.Contains(raw, "Other project") || strings.Contains(raw, "purchase") {
		t.Fatalf("builder scope: %s", raw)
	}
	if b.str("mentors", 0, "name") != "Maya Chen" || strings.Contains(raw, "@example.org") {
		t.Fatal("builders see mentor names, not emails")
	}
	expectStatus(t, "builder on mentor API", e.get("/api/portal/mentor", builder), 403)
	expectStatus(t, "builder on officer API", e.get("/api/buildspace-launchpad", builder), 401)
	expectStatus(t, "officer on portal API", e.get("/api/portal/builder", officer), 401)
	expectStatus(t, "anonymous portal", e.get("/api/portal/me", anon), 401)

	mentor := e.portalLogin("lp-account-maya")
	m := e.mustOK(e.get("/api/portal/mentor", mentor))
	ids := []string{}
	for _, p := range m.list("projects") {
		ids = append(ids, p.(map[string]any)["id"].(string))
	}
	mraw := string(m.Raw)
	if strings.Join(ids, ",") != "bs-moss,bs-pantry,bs-sprout" && strings.Join(ids, ",") != "bs-moss,bs-sprout,bs-pantry" {
		t.Fatalf("mentor projects %v", ids)
	}
	if !strings.Contains(mraw, "Mentor-only note") || strings.Contains(mraw, "Officer-only") || strings.Contains(mraw, "Other project") || strings.Contains(mraw, "jordan.ellis@example.org") {
		t.Fatalf("mentor scope: %s", mraw)
	}
	note := map[string]any{"projectId": "bs-afterimage", "date": "2026-10-31", "body": "Hi", "status": "on-track", "visibility": "team"}
	expectStatus(t, "mentor on unassigned project", e.post("/api/portal/mentor/activity", note, mentor), 404)
	note["projectId"], note["personId"] = "bs-moss", "bs-person-03"
	expectStatus(t, "mentor names a builder elsewhere", e.post("/api/portal/mentor/activity", note, mentor), 400)
	note["personId"], note["visibility"] = "bs-person-02", "mentors"
	e.mustOK(e.post("/api/portal/mentor/activity", note, mentor))
	if strings.Contains(string(e.mustOK(e.get("/api/portal/builder", builder)).Raw), `"visibility":"mentors"`) {
		t.Fatal("builders must not see mentor-only notes")
	}
	expectStatus(t, "cross-site portal write", e.do("POST", "/api/portal/mentor/activity", note, actor{cookie: mentor.cookie, headers: map[string]string{"Origin": "https://evil.example"}}), 403)
	// Officers see everything, with the author.
	found := false
	for _, a := range e.mustOK(e.get("/api/buildspace-launchpad", officer)).list("activity") {
		v := a.(map[string]any)
		found = found || (v["visibility"] == "mentors" && v["authorName"] == "Maya Chen")
	}
	if !found {
		t.Fatal("officers see mentor-authored notes with the author")
	}
	e.mustOK(e.post("/api/portal/logout", nil, mentor))
	expectStatus(t, "after logout", e.get("/api/portal/mentor", mentor), 401)
}

func TestLaunchpadOnboardingProfileProjectAndReadOnly(t *testing.T) {
	e := newEnv(t)
	e.exec(`INSERT INTO members_mirror(person_id,first_name,last_name,uw_email,join_date,has_been_officer,record_json) VALUES('member-sky','Sky','Morales','Sky.Morales@uw.edu','2026-09-01',0,'{}')`)
	sky := e.activate("lp-invite-sky", "300000000000000010")
	b := e.mustOK(e.get("/api/portal/builder", sky))
	if b.at("onboarded") != false || len(b.list("joinable")) != 6 || b.at("me") != nil {
		t.Fatalf("pre-onboarding %s", b.Raw)
	}
	expectStatus(t, "write before onboarding", e.post("/api/portal/builder/activity", map[string]any{"kind": "weekly", "date": "2026-10-31", "body": "x", "status": "on-track"}, sky), 409)
	body := map[string]any{"name": "Sky Morales", "pronouns": "she/her", "designation": "Hardware lead", "bio": "Sensors.", "project": map[string]any{"mode": "join", "projectId": "bs-lumen"}}
	expectStatus(t, "join another cohort's project", e.post("/api/portal/builder/onboarding", body, sky), 400)
	body["project"] = map[string]any{"mode": "join", "projectId": "bs-sprout"}
	e.mustOK(e.post("/api/portal/builder/onboarding", body, sky))
	expectStatus(t, "onboard twice", e.post("/api/portal/builder/onboarding", body, sky), 409)
	row := func(col string) any {
		return e.scalar(`SELECT b.` + col + ` FROM buildspace_launchpad_people b JOIN buildspace_launchpad_accounts a ON a.person_id=b.id WHERE a.id='lp-invite-sky'`)
	}
	if row("project_id") != "bs-sprout" || row("member_id") != "member-sky" || row("discord_id") != "300000000000000010" || row("email") != "sky.morales@uw.edu" || row("joined_on") != "2026-10-31" {
		t.Fatalf("person %v %v %v %v %v", row("project_id"), row("member_id"), row("discord_id"), row("email"), row("joined_on"))
	}
	b = e.mustOK(e.get("/api/portal/builder", sky))
	if b.str("me", "designation") != "Hardware lead" || b.str("thisWeek") != "awaiting" || len(b.list("teammates")) != 2 || len(b.list("joinable")) != 0 {
		t.Fatalf("dashboard %s", b.Raw)
	}
	e.mustOK(e.post("/api/portal/builder/activity", map[string]any{"kind": "weekly", "date": "2026-10-31", "body": "Wired the sensor", "status": "on-track"}, sky))
	if e.mustOK(e.get("/api/portal/builder", sky)).str("thisWeek") != "done" {
		t.Fatal("check-in counts for this week")
	}
	e.mustOK(e.do("PATCH", "/api/portal/builder/profile", map[string]any{"name": "Sky M.", "pronouns": "she/her", "designation": "Lead", "bio": ""}, sky))
	pr := e.mustOK(e.do("PATCH", "/api/portal/builder/project", map[string]any{"name": "Sprout sensor v2", "description": "d", "track": "Hardware", "stage": "Building", "links": []string{"https://example.org"}}, sky))
	if pr.str("project", "name") != "Sprout sensor v2" || e.scalar(`SELECT count(*) FROM buildspace_launchpad_mentors WHERE project_id='bs-sprout'`).(int64) != 2 {
		t.Fatal("project edit keeps mentors")
	}
	expectStatus(t, "unsafe link", e.do("PATCH", "/api/portal/builder/project", map[string]any{"name": "x", "description": "", "track": "Hardware", "stage": "Building", "links": []string{"javascript:x"}}, sky), 400)

	// Starting a new project.
	dev := e.activate("lp-invite-dev", "300000000000000011")
	e.mustOK(e.post("/api/portal/builder/onboarding", map[string]any{"name": "Dev", "pronouns": "", "designation": "Founder", "bio": "", "project": map[string]any{"mode": "new", "name": "Brand new", "description": "Idea", "track": "Creative", "links": []string{}}}, dev))
	if e.scalar(`SELECT stage||'|'||cohort_id FROM buildspace_launchpad_projects WHERE name='Brand new'`) != "Ideating|cohort-fall-2026" {
		t.Fatal("new project")
	}
	// An archived cohort makes the portal read-only.
	e.mustOK(e.do("PATCH", "/api/buildspace-launchpad/cohorts/cohort-fall-2026", map[string]any{"status": "archived"}, e.director()))
	if e.mustOK(e.get("/api/portal/builder", sky)).at("readOnly") != true {
		t.Fatal("read-only")
	}
	expectStatus(t, "read-only write", e.post("/api/portal/builder/activity", map[string]any{"kind": "absence", "date": "2026-10-31", "body": "Away", "status": "open"}, sky), 409)
	expectStatus(t, "mentor portal hides archived cohort", e.mustOK(e.get("/api/portal/mentor", e.portalLogin("lp-account-maya"))), 200)
	if n := len(e.mustOK(e.get("/api/portal/mentor", e.portalLogin("lp-account-maya"))).list("projects")); n != 0 {
		t.Fatalf("mentor still sees %d archived-cohort projects", n)
	}
}

func TestLaunchpadAutoAssignEvenCapsAndStale(t *testing.T) {
	e := newEnv(t)
	officer := e.officer()
	for _, id := range []string{"aa-1", "aa-2", "aa-3", "aa-4"} {
		e.mustOK(e.do("PUT", "/api/buildspace-launchpad/projects/"+id, map[string]any{"name": id, "description": "", "track": "Software", "stage": "Building", "links": []string{}, "mentorIds": []string{}}, officer))
	}
	for _, id := range []string{"bs-mentor-maya", "bs-mentor-amara"} {
		e.mustOK(e.do("PUT", "/api/buildspace-launchpad/mentors/"+id+"/settings", map[string]any{"available": true, "maxProjects": nil}, officer))
	}
	path := "/api/buildspace-launchpad/cohorts/cohort-fall-2026/auto-assign"
	plan := e.mustOK(e.post(path+"/preview", nil, officer))
	if len(plan.list("assignments")) != 5 || len(plan.list("unassigned")) != 0 {
		t.Fatalf("plan %s", plan.Raw)
	}
	lo, hi := 99, 0
	for _, m := range plan.list("mentors") {
		after := int(m.(map[string]any)["loadAfter"].(float64))
		lo, hi = min(lo, after), max(hi, after)
	}
	if hi-lo > 1 {
		t.Fatalf("uneven: %s", plan.Raw)
	}
	assignments := plan.at("assignments")
	e.mustOK(e.post(path, map[string]any{"assignments": assignments}, officer))
	expectStatus(t, "stale plan", e.post(path, map[string]any{"assignments": assignments}, officer), 409)
	if again := e.mustOK(e.post(path+"/preview", nil, officer)); len(again.list("assignments")) != 0 {
		t.Fatal("everything is mentored now")
	}
	// Caps: with everyone full, new projects stay unassigned.
	e.mustOK(e.do("PUT", "/api/buildspace-launchpad/projects/aa-5", map[string]any{"name": "aa-5", "description": "", "track": "Software", "stage": "Building", "links": []string{}, "mentorIds": []string{}}, officer))
	for _, id := range []string{"bs-mentor-maya", "bs-mentor-amara", "bs-mentor-jordan"} {
		e.mustOK(e.do("PUT", "/api/buildspace-launchpad/mentors/"+id+"/settings", map[string]any{"available": true, "maxProjects": 1}, officer))
	}
	capped := e.mustOK(e.post(path+"/preview", nil, officer))
	if len(capped.list("assignments")) != 0 || capped.str("unassigned", 0) != "aa-5" {
		t.Fatalf("capped %s", capped.Raw)
	}
	expectStatus(t, "over cap", e.post(path, map[string]any{"assignments": []any{map[string]any{"projectId": "aa-5", "mentorId": "bs-mentor-jordan"}}}, officer), 409)
	e.mustOK(e.do("PUT", "/api/buildspace-launchpad/mentors/bs-mentor-jordan/settings", map[string]any{"available": false, "maxProjects": nil}, officer))
	expectStatus(t, "unavailable mentor", e.post(path, map[string]any{"assignments": []any{map[string]any{"projectId": "aa-5", "mentorId": "bs-mentor-jordan"}}}, officer), 409)
	expectStatus(t, "archived cohort", e.post("/api/buildspace-launchpad/cohorts/cohort-spring-2026/auto-assign/preview", nil, officer), 409)
}

func TestPlanAutoAssignPrefersTopicOnTies(t *testing.T) {
	b := store.Buildspace{
		Cohorts:  []store.LaunchpadCohort{{ID: "c", Status: "active"}},
		Projects: []store.BuildspaceProject{{ID: "hw", Track: "Hardware", Stage: "Building", CohortID: "c", MentorIDs: []string{}}},
		Mentors: []store.BuildspaceMentor{
			{ID: "sw", Type: "mentor", Available: true, Topics: []string{"Product"}},
			{ID: "hwm", Type: "mentor", Available: true, Topics: []string{"Prototyping", "hardware"}},
			{ID: "off", Type: "mentor", Available: false, Topics: []string{"Hardware"}},
		},
	}
	for seedN := 0; seedN < 5; seedN++ {
		a, _, loads := planAutoAssign(&b, "c", func(n int) int { return seedN % n })
		if len(a) != 1 || a[0].MentorID != "hwm" || len(loads) != 2 {
			t.Fatalf("pick %+v %+v", a, loads)
		}
	}
}

func TestLaunchpadArchiveRestoreDeleteAndExclusions(t *testing.T) {
	e := newEnv(t)
	officer, director := e.officer(), e.director()
	alex := e.portalLogin("lp-account-alex")
	e.mustOK(e.post("/api/buildspace-launchpad/projects/bs-moss/archive", nil, officer))
	expectStatus(t, "archived builder signed out", e.get("/api/portal/builder", alex), 401)
	expectStatus(t, "archived builder cannot sign in again", e.get("/api/portal/builder", e.portalLogin("lp-account-alex")), 403)
	list := e.mustOK(e.get("/api/buildspace-launchpad", officer))
	if len(list.list("projects")) != 5 || len(list.list("people")) != 10 {
		t.Fatalf("archived rows hidden: %d %d", len(list.list("projects")), len(list.list("people")))
	}
	if all := e.mustOK(e.get("/api/buildspace-launchpad?archived=1", officer)); len(all.list("projects")) != 6 {
		t.Fatal("archived=1 includes archived rows")
	}
	p := map[string]any{"name": "Moss", "description": "", "track": "Software", "stage": "Building", "links": []string{}, "mentorIds": []string{}}
	expectStatus(t, "edit archived project", e.do("PUT", "/api/buildspace-launchpad/projects/bs-moss", p, officer), 409)
	expectStatus(t, "log on archived project", e.post("/api/buildspace-launchpad/activity", map[string]any{"projectId": "bs-moss", "kind": "request", "date": "2026-10-31", "body": "x", "status": "open"}, officer), 409)
	expectStatus(t, "restore builder before project", e.post("/api/buildspace-launchpad/people/bs-person-01/restore", nil, officer), 409)
	issued := e.mustOK(e.post("/api/admin/tokens", map[string]any{"name": "bot", "scopes": []string{"ingest:buildspace"}, "expiresInDays": 30}, director))
	bot := bearer(issued.at("plaintext").(string))
	expectStatus(t, "ingest for archived builder", e.post("/api/ingest/buildspace-launchpad", map[string]any{"discordId": "100000000000000001", "externalId": "m-arch", "kind": "weekly", "date": "2026-10-31", "body": "x", "status": "on-track"}, bot), 400)
	e.mustOK(e.do("PUT", "/api/me/calendar-widget", map[string]any{"view": "month", "colourBy": "source", "showWeekends": true, "sources": []string{"buildspace-weekly", "buildspace-mentor"}}, officer))
	feed := string(e.mustOK(e.get("/api/calendar/feed?start=2026-10-26&end=2026-11-09&sources=buildspace-weekly", officer)).Raw)
	if strings.Contains(feed, "bs-person-01") || strings.Contains(feed, "Moss") {
		t.Fatal("archived project in calendar")
	}
	if !strings.Contains(feed, "bs-person-04") {
		t.Fatalf("live builders keep their due items: %s", feed)
	}
	e.mustOK(e.post("/api/buildspace-launchpad/projects/bs-moss/restore", nil, officer))
	if e.scalar(`SELECT count(*) FROM buildspace_launchpad_people WHERE project_id='bs-moss' AND archived_at IS NULL`).(int64) != 2 {
		t.Fatal("restore brings its builders back")
	}
	e.mustOK(e.get("/api/portal/builder", e.portalLogin("lp-account-alex")))

	expectStatus(t, "delete project with builders", e.do("DELETE", "/api/buildspace-launchpad/projects/bs-moss", nil, director), 409)
	e.mustOK(e.do("PUT", "/api/buildspace-launchpad/projects/empty", with(p, "name", "Empty"), officer))
	expectStatus(t, "officer cannot delete", e.do("DELETE", "/api/buildspace-launchpad/projects/empty", nil, officer), 403)
	e.mustOK(e.do("DELETE", "/api/buildspace-launchpad/projects/empty", nil, director))
	expectStatus(t, "delete builder with history", e.do("DELETE", "/api/buildspace-launchpad/people/bs-person-02", nil, director), 409)
	e.mustOK(e.do("PUT", "/api/buildspace-launchpad/people/new-b", map[string]any{"name": "New", "projectId": "bs-moss", "joinedOn": "2026-10-31", "designation": "Founder"}, officer))
	e.mustOK(e.post("/api/buildspace-launchpad/people/new-b/archive", nil, officer))
	expectStatus(t, "edit archived builder", e.do("PUT", "/api/buildspace-launchpad/people/new-b", map[string]any{"name": "New", "projectId": "bs-moss", "joinedOn": "2026-10-31"}, officer), 409)
	e.mustOK(e.post("/api/buildspace-launchpad/people/new-b/restore", nil, officer))
	e.mustOK(e.do("PUT", "/api/buildspace-launchpad/people/new-b", map[string]any{"name": "New", "projectId": "bs-moss", "joinedOn": "2026-10-31"}, officer))
	if e.scalar(`SELECT designation FROM buildspace_launchpad_people WHERE id='new-b'`) != "Founder" {
		t.Fatal("unsent profile fields are kept")
	}
	e.mustOK(e.do("DELETE", "/api/buildspace-launchpad/people/new-b", nil, director))
	expectStatus(t, "move across cohorts", e.do("PUT", "/api/buildspace-launchpad/people/bs-person-13", map[string]any{"name": "Rowan", "projectId": "bs-moss", "joinedOn": "2026-03-30"}, officer), 409)
}
