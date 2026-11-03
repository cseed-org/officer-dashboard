package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"officerdashboard/internal/calendar"
	"officerdashboard/internal/notify"
)

type fakeDM struct {
	mu   sync.Mutex
	sent []struct{ to, content string }
	err  error
}

func (f *fakeDM) SendDM(_ context.Context, to, content string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, struct{ to, content string }{to, content})
	return nil
}

type fakeMailer struct {
	mu   sync.Mutex
	msgs []calendar.Message
}

func (f *fakeMailer) From() string { return "dashboard@cseed.test" }
func (f *fakeMailer) Send(_ context.Context, m calendar.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs = append(f.msgs, m)
	return nil
}
func (f *fakeMailer) all() []calendar.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]calendar.Message(nil), f.msgs...)
}

const inviteeID = "100000000000000777"

func inviteBody(method string) map[string]any {
	return map[string]any{"name": "Alex Chen", "discordId": inviteeID, "subteamId": "team-events", "method": method}
}

func TestInviteDMSentFallbackAndFailure(t *testing.T) {
	dm := &fakeDM{}
	e := newEnv(t, func(c *Config) { c.DM = dm })
	r := e.mustOK(e.post("/api/invites", inviteBody("both"), e.director()))
	if r.str("dm", "status") != "sent" || r.str("dm", "sentAt") == "" || r.str("invite", "dmSentAt") == "" {
		t.Fatalf("dm %s", r.Raw)
	}
	link := r.str("link")
	if !strings.HasPrefix(link, "http://localhost:8090/login?invite=") || r.str("invite", "status") != "pending" || r.str("invite", "subteam", "name") != "Events" {
		t.Fatalf("invite %s", r.Raw)
	}
	if len(dm.sent) != 1 || dm.sent[0].to != inviteeID || !strings.Contains(dm.sent[0].content, link) || !strings.Contains(dm.sent[0].content, "Events") {
		t.Fatalf("dm %+v", dm.sent)
	}
	// Inviting the same Discord ID again re-issues the pending invite with a new token.
	again := e.mustOK(e.post("/api/invites", inviteBody("link"), e.director()))
	if again.at("reinvited") != true || again.str("invite", "id") != r.str("invite", "id") || again.str("token") == r.str("token") || again.at("invite", "dmSentAt") != nil {
		t.Fatalf("reinvite %s", again.Raw)
	}
	if n := e.scalar(`SELECT count(*) FROM invites WHERE discord_id=?`, inviteeID); n != int64(1) {
		t.Fatalf("invites %v", n)
	}
	// A Discord refusal is reported, and the link is still returned.
	dm.err = &notify.DMError{Status: 403, Code: 50007, Msg: "closed DMs"}
	failed := e.mustOK(e.post("/api/invites", inviteBody("dm"), e.director()))
	if failed.str("dm", "status") != "failed" || failed.str("dm", "message") != "closed DMs" || failed.str("link") == "" || failed.str("invite", "dmError") != "closed DMs" {
		t.Fatalf("failed %s", failed.Raw)
	}
	list := e.mustOK(e.get("/api/invites", e.director()))
	if len(list.list("invites")) != 1 || list.at("dmConfigured") != true {
		t.Fatalf("list %s", list.Raw)
	}
	if strings.Contains(string(list.Raw), "token") {
		t.Fatal("token leaked in list")
	}
	expectStatus(t, "officer list", e.get("/api/invites", e.officer()), 403)
}

func TestInviteWithoutBotIsLinkOnlyAndExistingAccountsAreRefused(t *testing.T) {
	e := newEnv(t)
	r := e.mustOK(e.post("/api/invites", inviteBody("dm"), e.director()))
	if r.str("dm", "status") != "not_configured" || !strings.Contains(r.str("dm", "message"), "DISCORD_BOT_TOKEN") || r.str("link") == "" {
		t.Fatalf("no bot %s", r.Raw)
	}
	taken := e.post("/api/invites", map[string]any{"name": "Priya", "discordId": "100000000000000002", "subteamId": "team-events"}, e.director())
	expectStatus(t, "existing user", taken, 409)
	if !strings.Contains(taken.str("error", "message"), "already belongs") {
		t.Errorf("message %s", taken.Raw)
	}
	e.mustOK(e.do("PATCH", "/api/admin/users/user-ava", map[string]any{"active": false}, e.director()))
	inactive := e.post("/api/invites", map[string]any{"name": "Ava", "discordId": e.scalar(`SELECT discord_id FROM users WHERE id='user-ava'`), "subteamId": "team-events"}, e.director())
	expectStatus(t, "inactive user", inactive, 409)
	if !strings.Contains(inactive.str("error", "message"), "Reactivate") {
		t.Errorf("message %s", inactive.Raw)
	}
	// Revoke a pending invite; accepted ones cannot be revoked.
	e.mustOK(e.do("DELETE", "/api/invites/"+r.str("invite", "id"), nil, e.director()))
	expectStatus(t, "gone", e.do("DELETE", "/api/invites/"+r.str("invite", "id"), nil, e.director()), 404)
}

// signInAsInvitee runs the Discord callback for the invitee's account.
func signInAsInvitee(t *testing.T, e *testEnv, query string) reply {
	t.Helper()
	r := e.get("/auth/login?"+query, anon)
	if r.Status != 302 {
		t.Fatalf("login %d", r.Status)
	}
	loc, _ := url.Parse(r.Header.Get("Location"))
	return e.get("/auth/callback?code=good-code&state="+loc.Query().Get("state"), actor{cookies: setCookies(r)})
}

func TestInviteeSignInOnboardingAndProfile(t *testing.T) {
	dm := &fakeDM{}
	e := discordEnv(t, `{"id":"`+inviteeID+`","username":"alex","global_name":"Alex"}`, nil)
	e.cfg.DM = dm
	e.srv = New(e.cfg)
	e.h = e.srv.Handler()
	// Without an invite, the account is refused; through an invite link it sees "unavailable".
	if cb := signInAsInvitee(t, e, "invite=x"); cb.Header.Get("Location") != "/onboarding?unavailable=1" {
		t.Fatalf("no invite → %s", cb.Header.Get("Location"))
	}
	if cb := signInAsInvitee(t, e, ""); cb.Header.Get("Location") != "/login?error=not_allowlisted" {
		t.Fatalf("no invite → %s", cb.Header.Get("Location"))
	}
	inv := e.mustOK(e.post("/api/invites", inviteBody("link"), e.director()))
	token := inv.str("token")
	cb := signInAsInvitee(t, e, "invite="+token+"&next=/")
	if cb.Header.Get("Location") != "/onboarding" {
		t.Fatalf("invitee → %s", cb.Header.Get("Location"))
	}
	alex := actor{cookie: cookieNamed(setCookies(cb), e.srv.sessionCookieName())}
	me := e.mustOK(e.get("/api/me", alex))
	if me.str("user", "role") != "officer" || me.str("user", "name") != "Alex Chen" || me.num("onboarding", "step") != 1 || me.str("onboarding", "subteam", "id") != "team-events" || me.str("onboarding", "invitedBy", "id") != "user-director" {
		t.Fatalf("me %s", me.Raw)
	}
	if e.scalar(`SELECT count(*) FROM audit_log WHERE action IN ('user.created','invite.accepted') AND actor_user_id=?`, me.str("user", "id")) != int64(2) {
		t.Error("acceptance not audited")
	}
	// Finish later saves the step; the next sign-in resumes onboarding.
	e.mustOK(e.do("PUT", "/api/me/onboarding", map[string]any{"step": 3}, alex))
	if again := signInAsInvitee(t, e, "next=/finance"); again.Header.Get("Location") != "/onboarding" {
		t.Fatalf("resume → %s", again.Header.Get("Location"))
	}
	expectStatus(t, "finish without profile", e.post("/api/me/onboarding/complete", nil, alex), 409)
	// Corrections go to the inviter over Discord.
	corr := e.mustOK(e.post("/api/me/onboarding/correction", map[string]any{"message": "I'm on Build, not Events"}, alex))
	if corr.str("dm", "status") != "sent" || len(dm.sent) != 1 || !strings.Contains(dm.sent[0].content, "not Events") {
		t.Fatalf("correction %s %+v", corr.Raw, dm.sent)
	}
	// Profile: required fields, month-day birthday, and the invite's subteam.
	bad := e.do("PUT", "/api/me/profile", map[string]any{"phone": "12", "birthday": "2004-02-30", "linkedin": "https://evil.example/in/x", "personalEmail": "nope"}, alex)
	expectStatus(t, "invalid profile", bad, 400)
	for _, f := range []string{"phone", "birthday", "linkedin", "personalEmail"} {
		if !strings.Contains(string(bad.Raw), `"`+f+`"`) {
			t.Errorf("no %s error: %s", f, bad.Raw)
		}
	}
	expectStatus(t, "other subteam", e.do("PUT", "/api/me/profile", map[string]any{"phone": "+1 206 555 0100", "birthday": "02-29", "subteamId": "team-build"}, alex), 403)
	p := e.mustOK(e.do("PUT", "/api/me/profile", map[string]any{"phone": "+1 206 555 0100", "birthday": "02-29", "pronouns": "they/them", "personalEmail": "Alex@Example.org"}, alex))
	if p.str("profile", "subteam", "id") != "team-events" || p.str("profile", "birthday") != "02-29" || p.str("profile", "personalEmail") != "alex@example.org" {
		t.Fatalf("profile %s", p.Raw)
	}
	if strings.Contains(e.scalar(`SELECT after_json FROM audit_log WHERE action='profile.updated' ORDER BY created_at DESC LIMIT 1`).(string), "0100") {
		t.Error("phone in audit log")
	}
	e.mustOK(e.post("/api/me/onboarding/complete", nil, alex))
	e.mustOK(e.post("/api/me/onboarding/complete", nil, alex)) // idempotent
	if me := e.mustOK(e.get("/api/me", alex)); me.at("onboarding") != nil {
		t.Fatalf("still onboarding %s", me.Raw)
	}
	if after := signInAsInvitee(t, e, "next=/finance"); after.Header.Get("Location") != "/finance" {
		t.Fatalf("after onboarding → %s", after.Header.Get("Location"))
	}
	expectStatus(t, "non-invitee complete", e.post("/api/me/onboarding/complete", nil, e.officer()), 409)
}

func TestOfficersDirectoryPhoneVisibilityAndSubteamEdits(t *testing.T) {
	e := newEnv(t)
	officerView := e.mustOK(e.get("/api/officers", e.officer()))
	if strings.Contains(string(officerView.Raw), `"phone"`) || officerView.at("canManage") != false {
		t.Fatalf("officer sees phone: %s", officerView.Raw)
	}
	if !strings.Contains(string(officerView.Raw), `"birthday":"01-15"`) {
		t.Error("birthday should be visible to officers")
	}
	dirView := e.mustOK(e.get("/api/officers", e.director()))
	if !strings.Contains(string(dirView.Raw), `"phone":"+1206555`) || dirView.at("canManage") != true || dirView.at("canChangeRoles") != false {
		t.Fatalf("director view %s", dirView.Raw)
	}
	detail := e.mustOK(e.get("/api/officers/user-priya", e.officer()))
	if detail.at("officer", "phone") != nil || detail.str("officer", "subteam", "id") != "team-events" {
		t.Fatalf("detail %s", detail.Raw)
	}
	expectStatus(t, "officer edit", e.do("PATCH", "/api/officers/user-priya", map[string]any{"subteamId": "team-build"}, e.officer()), 403)
	up := e.mustOK(e.do("PATCH", "/api/officers/user-priya", map[string]any{"subteamId": "team-build", "title": "Build lead"}, e.director()))
	if up.str("officer", "subteam", "id") != "team-build" || up.str("officer", "title") != "Build lead" {
		t.Fatalf("update %s", up.Raw)
	}
	expectStatus(t, "director edits leadership", e.do("PATCH", "/api/officers/user-leadership", map[string]any{"title": "x"}, e.director()), 403)
	expectStatus(t, "unknown subteam", e.do("PATCH", "/api/officers/user-priya", map[string]any{"subteamId": "nope"}, e.director()), 400)
	expectStatus(t, "own subteam change", e.do("PUT", "/api/me/profile", map[string]any{"phone": "+1 206 555 0100", "birthday": "01-15", "subteamId": "team-events"}, e.officer()), 403)
}

func TestICSDeliveryOnEventCreateAndUpdate(t *testing.T) {
	m := &fakeMailer{}
	e := newEnv(t, func(c *Config) { c.Mailer = m })
	e.mustOK(e.do("PUT", "/api/me/prefs", map[string]any{"timezoneMode": "manual", "timezone": "Asia/Tokyo"}, e.officer()))
	st := e.mustOK(e.get("/api/me/calendar", e.officer()))
	if st.at("ics", "configured") != true || st.at("google", "configured") != false || st.str("mode") != "ics" {
		t.Fatalf("settings %s", st.Raw)
	}
	e.mustOK(e.post("/api/me/calendar-emails", map[string]any{"email": "priya.cal@example.org"}, e.officer()))
	e.srv.WaitBackground()
	// Choosing an address backfills the upcoming events they haven't received.
	backfilled := len(m.all())
	if backfilled == 0 {
		t.Fatal("no backfill to the new calendar email")
	}
	ev := e.mustOK(e.post("/api/events", map[string]any{"name": "Hack, Night; 2", "startsAt": "2026-11-20T18:00:00-08:00", "location": "Room 101"}, e.officer()))
	e.srv.WaitBackground()
	msgs := m.all()[backfilled:]
	if len(msgs) != 1 || msgs[0].To != "priya.cal@example.org" || !strings.Contains(msgs[0].ICS, `SUMMARY:Hack\, Night\; 2`) || !strings.Contains(msgs[0].ICS, "SEQUENCE:0") || !strings.Contains(msgs[0].ICS, "DTSTART:20261121T020000Z") {
		t.Fatalf("mail %+v", msgs)
	}
	if !strings.Contains(msgs[0].Text, "11:00 AM JST") || !strings.Contains(msgs[0].Text, "Asia/Tokyo") || !strings.Contains(msgs[0].ICS, "X-CSEED-TIMEZONE:Asia/Tokyo") {
		t.Fatalf("recipient timezone not used: %+v", msgs[0])
	}
	e.mustOK(e.do("PATCH", "/api/events/"+ev.str("event", "id"), map[string]any{"location": "Room 202"}, e.officer()))
	e.srv.WaitBackground()
	msgs = m.all()[backfilled:]
	if len(msgs) != 2 || !strings.Contains(msgs[1].ICS, "SEQUENCE:1") || !strings.HasPrefix(msgs[1].Subject, "Updated") {
		t.Fatalf("update mail %+v", msgs[1:])
	}
	// Saving the same content again sends nothing; switching mode and back resends nothing.
	e.mustOK(e.do("PATCH", "/api/events/"+ev.str("event", "id"), map[string]any{"location": "Room 202"}, e.officer()))
	e.srv.WaitBackground()
	if n := len(m.all()) - backfilled; n != 2 {
		t.Fatalf("unchanged event resent: %d mails", n)
	}
	// A new active address gets the events it hasn't had.
	e.mustOK(e.post("/api/me/calendar-emails", map[string]any{"email": "priya.other@example.org", "active": true}, e.officer()))
	e.srv.WaitBackground()
	last := m.all()[len(m.all())-1]
	if last.To != "priya.other@example.org" {
		t.Fatalf("no backfill to the new address: %+v", last.To)
	}
	// Deleting the event cancels it wherever it was sent.
	before := len(m.all())
	e.mustOK(e.do("DELETE", "/api/events/"+ev.str("event", "id"), nil, e.officer()))
	e.srv.WaitBackground()
	cancels := m.all()[before:]
	if len(cancels) != 1 || cancels[0].Method != "CANCEL" || !strings.Contains(cancels[0].ICS, "STATUS:CANCELLED") || !strings.HasPrefix(cancels[0].Subject, "Cancelled") {
		t.Fatalf("cancellation %+v", cancels)
	}
	test := e.mustOK(e.post("/api/me/calendar/test", nil, e.officer()))
	if test.str("status") != "sent" {
		t.Fatalf("test %s", test.Raw)
	}
	none := newEnv(t)
	if r := none.mustOK(none.post("/api/me/calendar/test", nil, none.officer())); r.str("status") != "not_configured" {
		t.Fatalf("unconfigured %s", r.Raw)
	}
	expectStatus(t, "google not configured", none.do("PUT", "/api/me/calendar", map[string]any{"mode": "google"}, none.officer()), 409)
	expectStatus(t, "connect not configured", none.get("/api/me/calendar/google/connect", none.officer()), 503)
}

func TestGoogleOAuthConnectSyncAndDisconnect(t *testing.T) {
	var mu sync.Mutex
	var created, patched, revoked int
	gs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.URL.Path == "/token":
			_ = r.ParseForm()
			if r.PostForm.Get("code_verifier") == "" {
				http.Error(w, `{"error":"invalid_request"}`, 400)
				return
			}
			claims := `{"email":"priya@gmail.test"}`
			_, _ = io.WriteString(w, `{"access_token":"at","refresh_token":"rt","expires_in":3600,"id_token":"x.`+b64(claims)+`.y"}`)
		case r.URL.Path == "/revoke":
			revoked++
		case r.Method == "POST" && r.URL.Path == "/calendars/primary/events":
			if r.Header.Get("Authorization") != "Bearer at" {
				w.WriteHeader(401)
				return
			}
			created++
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "g-1"})
		case r.Method == "PATCH" && r.URL.Path == "/calendars/primary/events/g-1":
			patched++
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "g-1"})
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(gs.Close)
	e := newEnv(t, func(c *Config) {
		g := calendar.NewGoogle("gid", "gsecret", "http://localhost:8090/auth/google/callback")
		g.AuthURL, g.TokenURL, g.APIBase, g.RevokeURL = gs.URL+"/auth", gs.URL+"/token", gs.URL, gs.URL+"/revoke"
		c.Google = g
	})
	me := e.officer()
	start := e.get("/api/me/calendar/google/connect?next=/onboarding", me)
	if start.Status != 302 || !strings.HasPrefix(start.Header.Get("Location"), gs.URL+"/auth?") {
		t.Fatalf("connect %d %s", start.Status, start.Header.Get("Location"))
	}
	loc, _ := url.Parse(start.Header.Get("Location"))
	if loc.Query().Get("access_type") != "offline" || loc.Query().Get("code_challenge") == "" {
		t.Errorf("consent url %s", loc)
	}
	cookies := append(setCookies(start), me.cookie)
	bad := e.get("/auth/google/callback?code=c&state=wrong", actor{cookies: cookies})
	if bad.Header.Get("Location") != "/onboarding?google=failed" {
		t.Fatalf("bad state → %s", bad.Header.Get("Location"))
	}
	cb := e.get("/auth/google/callback?code=c&state="+loc.Query().Get("state"), actor{cookies: cookies})
	if cb.Header.Get("Location") != "/onboarding?google=connected" {
		t.Fatalf("callback → %s", cb.Header.Get("Location"))
	}
	e.srv.WaitBackground()
	st := e.mustOK(e.get("/api/me/calendar", me))
	if st.str("mode") != "google" || st.at("google", "connected") != true || st.str("google", "accountEmail") != "priya@gmail.test" {
		t.Fatalf("settings %s", st.Raw)
	}
	if v := e.scalar(`SELECT sealed_refresh_token FROM google_calendar_tokens WHERE user_id='user-priya'`).(string); strings.Contains(v, "rt") && len(v) < 10 {
		t.Error("refresh token not sealed")
	}
	mu.Lock()
	backfilled := created
	mu.Unlock()
	if backfilled < 1 {
		t.Error("connecting should backfill upcoming events")
	}
	ev := e.mustOK(e.post("/api/events", map[string]any{"name": "Demo day", "startsAt": "2026-12-01T18:00:00-08:00"}, e.officer()))
	e.srv.WaitBackground()
	e.mustOK(e.do("PATCH", "/api/events/"+ev.str("event", "id"), map[string]any{"name": "Demo day!"}, e.officer()))
	e.srv.WaitBackground()
	mu.Lock()
	c1, p1 := created, patched
	mu.Unlock()
	if c1 != backfilled+1 || p1 != 1 {
		t.Fatalf("created %d patched %d", c1, p1)
	}
	off := e.mustOK(e.do("DELETE", "/api/me/calendar/google", nil, me))
	if off.str("mode") != "ics" || off.at("google", "connected") != false || revoked != 1 {
		t.Fatalf("disconnect %s revoked=%d", off.Raw, revoked)
	}
}

func b64(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }

func TestReissuedInviteLinkReplacesTheOldOneAndCorrectionsAreRateLimited(t *testing.T) {
	dm := &fakeDM{}
	e := discordEnv(t, `{"id":"`+inviteeID+`","username":"alex","global_name":"Alex"}`, nil)
	e.cfg.DM = dm
	e.srv = New(e.cfg)
	e.h = e.srv.Handler()
	old := e.mustOK(e.post("/api/invites", inviteBody("link"), e.director())).str("token")
	fresh := e.mustOK(e.post("/api/invites", inviteBody("link"), e.director())).str("token")
	if cb := signInAsInvitee(t, e, "invite="+old); cb.Header.Get("Location") != "/onboarding?unavailable=1" {
		t.Fatalf("old link → %s", cb.Header.Get("Location"))
	}
	if e.scalar(`SELECT count(*) FROM users WHERE discord_id=?`, inviteeID) != int64(0) {
		t.Fatal("old link created an account")
	}
	cb := signInAsInvitee(t, e, "invite="+fresh)
	if cb.Header.Get("Location") != "/onboarding" {
		t.Fatalf("new link → %s", cb.Header.Get("Location"))
	}
	alex := actor{cookie: cookieNamed(setCookies(cb), e.srv.sessionCookieName())}
	e.mustOK(e.post("/api/me/onboarding/correction", map[string]any{"message": "typo in my name"}, alex))
	expectStatus(t, "second within a minute", e.post("/api/me/onboarding/correction", map[string]any{"message": "again"}, alex), 409)
	for i := 0; i < 4; i++ {
		e.clock = e.clock.Add(2 * time.Minute)
		e.mustOK(e.post("/api/me/onboarding/correction", map[string]any{"message": "more"}, alex))
	}
	e.clock = e.clock.Add(2 * time.Minute)
	expectStatus(t, "sixth", e.post("/api/me/onboarding/correction", map[string]any{"message": "too many"}, alex), 409)
	if len(dm.sent) != 5 {
		t.Fatalf("DMs %d", len(dm.sent))
	}
}

func TestDirectorsDeactivateOfficersOnlyLeadershipDeactivatesDirectors(t *testing.T) {
	e := newEnv(t)
	second := e.mustOK(e.post("/api/admin/users", map[string]any{"name": "Second Director", "discordId": "100000000000000555", "role": "director", "subteamId": "team-events"}, e.as("user-leadership"))).str("user", "id")
	e.mustOK(e.do("PATCH", "/api/admin/users/user-marcus", map[string]any{"active": false}, e.director()))
	e.mustOK(e.do("PATCH", "/api/admin/users/user-marcus", map[string]any{"active": true}, e.director()))
	expectStatus(t, "director deactivates director", e.do("PATCH", "/api/admin/users/"+second, map[string]any{"active": false}, e.director()), 403)
	expectStatus(t, "director deactivates leadership", e.do("PATCH", "/api/admin/users/user-leadership", map[string]any{"active": false}, e.director()), 403)
	e.mustOK(e.do("PATCH", "/api/admin/users/"+second, map[string]any{"active": false}, e.as("user-leadership")))
	expectStatus(t, "director reactivates director", e.do("PATCH", "/api/admin/users/"+second, map[string]any{"active": true}, e.director()), 403)
	// A name-only edit of a director is still fine for directors.
	e.mustOK(e.do("PATCH", "/api/admin/users/user-director", map[string]any{"title": "ED"}, e.director()))
}
