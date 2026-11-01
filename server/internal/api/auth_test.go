package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/store"
)

// fakeDiscord answers the token and profile endpoints for one Discord account.
func fakeDiscord(t *testing.T, profileJSON string, gotVerifier *string) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if gotVerifier != nil {
			*gotVerifier = r.PostForm.Get("code_verifier")
		}
		if r.PostForm.Get("code") != "good-code" {
			http.Error(w, `{"error":"invalid_grant"}`, 400)
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"tok","token_type":"Bearer"}`))
	})
	mux.HandleFunc("GET /me", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(profileJSON)) })
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func discordEnv(t *testing.T, profileJSON string, bootstrap string, verifier *string) *testEnv {
	d := fakeDiscord(t, profileJSON, verifier)
	return newEnv(t, func(c *Config) {
		cl := auth.NewDiscord("cid", "secret", "http://localhost:8090/auth/callback")
		cl.TokenURL, cl.ProfileURL = d.URL+"/token", d.URL+"/me"
		c.Discord = cl
		c.BootstrapDirectorDiscordID = bootstrap
	})
}

func setCookies(r reply) []*http.Cookie {
	return (&http.Response{Header: r.Header}).Cookies()
}

func cookieNamed(cs []*http.Cookie, name string) *http.Cookie {
	for _, c := range cs {
		if c.Name == name && c.MaxAge >= 0 {
			return c
		}
	}
	return nil
}

// startLogin runs /auth/login and returns the cookies to carry plus the state sent to Discord.
func startLogin(t *testing.T, e *testEnv, next string) ([]*http.Cookie, string) {
	r := e.get("/auth/login?next="+url.QueryEscape(next), anon)
	if r.Status != 302 {
		t.Fatalf("login → %d %s", r.Status, r.Raw)
	}
	loc, _ := url.Parse(r.Header.Get("Location"))
	q := loc.Query()
	if loc.Host != "discord.com" || q.Get("code_challenge_method") != "S256" || q.Get("client_id") != "cid" || q.Get("scope") != "identify" {
		t.Errorf("authorize URL %s", loc)
	}
	cs := setCookies(r)
	state, verifier := cookieNamed(cs, oauthStateName), cookieNamed(cs, oauthVerifier)
	if state == nil || verifier == nil || state.Value != q.Get("state") || !state.HttpOnly || !verifier.HttpOnly {
		t.Fatalf("oauth cookies %v", cs)
	}
	if q.Get("code_challenge") != auth.Challenge(verifier.Value) {
		t.Error("challenge does not match the verifier cookie")
	}
	if n := cookieNamed(cs, nextCookieName); n == nil || n.Value != auth.SafeNext(next) {
		t.Errorf("next cookie %v", n)
	}
	return cs, q.Get("state")
}

func TestDiscordSignInForAnAllowlistedUser(t *testing.T) {
	var verifier string
	e := discordEnv(t, `{"id":"100000000000000002","username":"priya","global_name":"Priya S.","avatar":"abc123"}`, "", &verifier)
	cs, state := startLogin(t, e, "/requests/new?event=e1")
	cb := e.get("/auth/callback?code=good-code&state="+state, actor{cookies: cs})
	if cb.Status != 302 || cb.Header.Get("Location") != "/requests/new?event=e1" {
		t.Fatalf("callback → %d %s", cb.Status, cb.Header.Get("Location"))
	}
	if verifier != cookieNamed(cs, oauthVerifier).Value {
		t.Error("the PKCE verifier was not sent to Discord")
	}
	session := cookieNamed(setCookies(cb), e.srv.sessionCookieName())
	if session == nil || !session.HttpOnly || session.SameSite != http.SameSiteLaxMode || len(session.Value) < 40 {
		t.Fatalf("session cookie %+v", session)
	}
	// The temporary cookies are cleared.
	for _, c := range setCookies(cb) {
		if (c.Name == oauthStateName || c.Name == oauthVerifier || c.Name == nextCookieName) && c.MaxAge >= 0 {
			t.Errorf("cookie %s not cleared", c.Name)
		}
	}
	me := e.get("/api/me", actor{cookie: session})
	if me.Status != 200 || me.str("user", "id") != "user-priya" || me.str("user", "role") != "officer" {
		t.Errorf("me %d %s", me.Status, me.Raw)
	}
	if e.scalar(`SELECT avatar_url FROM users WHERE id = 'user-priya'`) != "https://cdn.discordapp.com/avatars/100000000000000002/abc123.png?size=128" {
		t.Error("avatar not refreshed")
	}
	if e.scalar(`SELECT id FROM sessions WHERE id = ?`, auth.SHA256Hex(session.Value)) == nil {
		t.Error("only the sha256 of the token is stored")
	}
	// Logout deletes the session and redirects.
	out := e.do("POST", "/auth/logout", nil, actor{cookie: session})
	if out.Status != 303 || out.Header.Get("Location") != "/login" {
		t.Errorf("logout → %d %s", out.Status, out.Header.Get("Location"))
	}
	expectStatus(t, "after logout", e.get("/api/me", actor{cookie: session}), 401)
}

func TestDiscordSignInRefusals(t *testing.T) {
	e := discordEnv(t, `{"id":"999999999999999999","username":"stranger"}`, "", nil)
	cs, state := startLogin(t, e, "/")
	cb := e.get("/auth/callback?code=good-code&state="+state, actor{cookies: cs})
	if cb.Status != 302 || cb.Header.Get("Location") != "/login?error=not_allowlisted" {
		t.Errorf("stranger → %d %s", cb.Status, cb.Header.Get("Location"))
	}
	if cookieNamed(setCookies(cb), e.srv.sessionCookieName()) != nil {
		t.Error("a refused login must not get a session")
	}
	// A wrong state, a missing verifier and a failed code exchange.
	cs, state = startLogin(t, e, "/")
	if cb := e.get("/auth/callback?code=good-code&state=tampered", actor{cookies: cs}); cb.Header.Get("Location") != "/login?error=state" {
		t.Errorf("tampered state → %s", cb.Header.Get("Location"))
	}
	if cb := e.get("/auth/callback?code=good-code&state="+state, anon); cb.Header.Get("Location") != "/login?error=state" {
		t.Errorf("no cookies → %s", cb.Header.Get("Location"))
	}
	cs, state = startLogin(t, e, "/")
	if cb := e.get("/auth/callback?code=bad-code&state="+state, actor{cookies: cs}); cb.Header.Get("Location") != "/login?error=discord" {
		t.Errorf("bad code → %s", cb.Header.Get("Location"))
	}
	// A deactivated user is refused as such.
	e2 := discordEnv(t, `{"id":"100000000000000004","username":"ava"}`, "", nil)
	if err := e2.db.Write(context.Background(), func(w *store.Queries) error {
		return w.Exec(context.Background(), `UPDATE users SET active = 0 WHERE id = 'user-ava'`)
	}); err != nil {
		t.Fatal(err)
	}
	cs, state = startLogin(t, e2, "/")
	if cb := e2.get("/auth/callback?code=good-code&state="+state, actor{cookies: cs}); cb.Header.Get("Location") != "/login?error=inactive" {
		t.Errorf("inactive → %s", cb.Header.Get("Location"))
	}
}

func TestBootstrapDirectorIsCreatedOnFirstLogin(t *testing.T) {
	e := discordEnv(t, `{"id":"123456789012345678","username":"boss","global_name":null}`, "123456789012345678", nil)
	cs, state := startLogin(t, e, "/admin/users")
	cb := e.get("/auth/callback?code=good-code&state="+state, actor{cookies: cs})
	if cb.Status != 302 || cb.Header.Get("Location") != "/admin/users" {
		t.Fatalf("callback → %d %s", cb.Status, cb.Header.Get("Location"))
	}
	session := cookieNamed(setCookies(cb), e.srv.sessionCookieName())
	me := e.get("/api/me", actor{cookie: session})
	if me.str("user", "role") != "director" || me.str("user", "name") != "boss" {
		t.Errorf("bootstrap director %s", me.Raw)
	}
	if n := e.scalar(`SELECT count(*) FROM audit_log WHERE action = 'user.bootstrap_director'`); n != int64(1) {
		t.Errorf("bootstrap audit rows %v", n)
	}
}

func TestLoginWithoutDiscordConfiguredIs503(t *testing.T) {
	e := newEnv(t)
	expectStatus(t, "login", e.get("/auth/login", anon), 503)
	cb := e.get("/auth/callback?code=x&state=y", anon)
	if cb.Header.Get("Location") != "/login?error=state" {
		t.Errorf("callback → %s", cb.Header.Get("Location"))
	}
}

func TestSecureCookiesAndHostPrefixOverHTTPS(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.AppURL = "https://officers.example" })
	if e.srv.sessionCookieName() != "__Host-cseed_session" {
		t.Errorf("cookie name %s", e.srv.sessionCookieName())
	}
	rec := httptest.NewRecorder()
	e.srv.setCookie(rec, "x", "y", time.Now().Add(time.Hour), 0)
	if c := rec.Result().Cookies()[0]; !c.Secure || !c.HttpOnly {
		t.Errorf("cookie %+v", c)
	}
	if h := e.get("/api/health", anon).Header.Get("Strict-Transport-Security"); h == "" {
		t.Error("HSTS missing over https")
	}
	if e2 := newEnv(t); e2.srv.sessionCookieName() != "cseed_session" {
		t.Errorf("http cookie name %s", e2.srv.sessionCookieName())
	}
}

func TestCrossOriginWritesWithASessionAreRefused(t *testing.T) {
	e := newEnv(t)
	who := e.officer()
	evil := who
	evil.headers = map[string]string{"Origin": "https://evil.example"}
	expectStatus(t, "cross-origin POST", e.post("/api/requests", purchase, evil), 403)
	same := who
	same.headers = map[string]string{"Origin": "http://localhost:8090"}
	expectStatus(t, "same-origin POST", e.post("/api/requests", purchase, same), 200)
	expectStatus(t, "no Origin header", e.post("/api/requests", purchase, who), 200)
	expectStatus(t, "cross-origin GET is a read", e.get("/api/me", evil), 200)
	nul := who
	nul.headers = map[string]string{"Origin": "null"}
	expectStatus(t, "opaque origin", e.post("/api/requests", purchase, nul), 403)
	// Service tokens carry no cookie, so they are not subject to the check.
	tok := e.mustOK(e.post("/api/admin/tokens", map[string]any{"name": "t", "scopes": []string{"agent:jobs"}}, e.director())).str("plaintext")
	agent := bearer(tok)
	agent.headers = map[string]string{"Origin": "https://evil.example"}
	expectStatus(t, "token with foreign Origin", e.get("/api/agent/jobs", agent), 200)
}

func TestSessionsSlideWhenHalfExpired(t *testing.T) {
	e := newEnv(t)
	token := auth.RandomToken(32)
	soon := e.clock.Add(10 * 24 * time.Hour).UnixMilli()
	if err := e.db.Write(context.Background(), func(w *store.Queries) error {
		return w.InsertSession(context.Background(), auth.SHA256Hex(token), "user-priya", soon, "t", e.clock.UnixMilli())
	}); err != nil {
		t.Fatal(err)
	}
	r := e.get("/api/me", actor{cookie: &http.Cookie{Name: e.srv.sessionCookieName(), Value: token}})
	if r.Status != 200 {
		t.Fatal(r.Status)
	}
	renewed := cookieNamed(setCookies(r), e.srv.sessionCookieName())
	if renewed == nil || renewed.Value != token {
		t.Fatalf("expected a refreshed cookie, got %v", setCookies(r))
	}
	exp := e.scalar(`SELECT expires_at FROM sessions WHERE id = ?`, auth.SHA256Hex(token)).(int64)
	if got := time.UnixMilli(exp).Sub(e.clock); got < 29*24*time.Hour {
		t.Errorf("expiry extended to only %s", got)
	}
	// A session with plenty of life left is not rewritten on every request.
	freshToken := auth.RandomToken(32)
	if err := e.db.Write(context.Background(), func(w *store.Queries) error {
		return w.InsertSession(context.Background(), auth.SHA256Hex(freshToken), "user-priya", e.clock.Add(29*24*time.Hour).UnixMilli(), "t", e.clock.UnixMilli())
	}); err != nil {
		t.Fatal(err)
	}
	fresh := actor{cookie: &http.Cookie{Name: e.srv.sessionCookieName(), Value: freshToken}}
	if r := e.get("/api/me", fresh); r.Status != 200 || cookieNamed(setCookies(r), e.srv.sessionCookieName()) != nil {
		t.Errorf("a 29-day session should not be rewritten (status %d)", r.Status)
	}
	// An unknown or expired cookie is cleared.
	r = e.get("/api/me", actor{cookie: &http.Cookie{Name: e.srv.sessionCookieName(), Value: "garbage"}})
	if r.Status != 401 {
		t.Errorf("garbage cookie → %d", r.Status)
	}
	cleared := false
	for _, c := range setCookies(r) {
		if c.Name == e.srv.sessionCookieName() && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("stale cookie should be cleared")
	}
}
