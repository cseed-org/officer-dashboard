package api

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/store"
)

// The `setup.` branch of /auth/callback (setuprelay.go). It shows a code only for a state
// minted with this install's Discord client secret within the last ten minutes, and does
// nothing else. Every test here is one of the requirements in the setup brief or the
// review of #128.

const (
	relaySecret = "secret" // the client secret relayEnv configures
	relayCode   = "Zq9TqBy3mKcJ0dRw7LhVf2NsXe5Ua1"
)

func relayURL(state, code string) string {
	return "/auth/callback?" + url.Values{"code": {code}, "state": {state}}.Encode()
}

// mintState is the state `dashboard setup discord connect` makes, at the test clock.
func mintState(t *testing.T, e *testEnv, secret string) string {
	t.Helper()
	s, err := auth.MintSetupState(secret, e.clock, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// relayEnv is a server with Discord configured (client secret "secret") against a fake
// that counts every request.
func relayEnv(t *testing.T, mutate ...func(*Config)) (*testEnv, *atomic.Int64) {
	var calls atomic.Int64
	d := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"access_token":"tok","token_type":"Bearer","id":"100000000000000002","username":"priya"}`))
	}))
	t.Cleanup(d.Close)
	e := newEnv(t, append([]func(*Config){func(c *Config) {
		cl := auth.NewDiscord("cid", relaySecret, "http://localhost:8090/auth/callback")
		cl.TokenURL, cl.ProfileURL = d.URL+"/token", d.URL+"/me"
		c.Discord = cl
	}}, mutate...)...)
	return e, &calls
}

// dbFingerprint is every row of every table, so a test can prove a request wrote nothing.
func dbFingerprint(t *testing.T, e *testEnv) string {
	t.Helper()
	rows, err := e.db.R.Query(`SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, n)
	}
	rows.Close()
	h := sha256.New()
	for _, table := range tables {
		r, err := e.db.R.Query(`SELECT * FROM "` + table + `"`)
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := r.Columns()
		for r.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := r.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(h, "%s%v\n", table, vals)
		}
		r.Close()
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// renewalDueSession is a session with an hour left, which withAuth renews on any request
// it looks at (a Set-Cookie and a database write).
func renewalDueSession(t *testing.T, e *testEnv) *http.Cookie {
	token := auth.RandomToken(32)
	if err := e.db.Write(context.Background(), func(w *store.Queries) error {
		return w.InsertSession(context.Background(), auth.SHA256Hex(token), "user-priya", e.clock.Add(time.Hour).UnixMilli(), "t", e.clock.UnixMilli())
	}); err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: e.srv.sessionCookieName(), Value: token}
}

// signInCookies are the cookies an officer's browser may carry into the callback: a
// session about to be renewed and a sign-in in progress.
func signInCookies(t *testing.T, e *testEnv) []*http.Cookie {
	return []*http.Cookie{
		renewalDueSession(t, e),
		{Name: oauthStateName, Value: "pending-state"},
		{Name: oauthVerifier, Value: "pending-verifier"},
		{Name: nextCookieName, Value: "/events"},
	}
}

func renewed(e *testEnv, r reply) bool {
	c := cookieNamed(setCookies(r), e.srv.sessionCookieName())
	return c != nil && c.Value != ""
}

func showsRelay(r reply) bool { return strings.Contains(string(r.Raw), "Dashboard setup code") }

func TestSetupRelayShowsTheCodeAndDoesNothingElse(t *testing.T) {
	e, calls := relayEnv(t)
	state := mintState(t, e, relaySecret)
	cookies := signInCookies(t, e)
	before := dbFingerprint(t, e)
	r := e.get(relayURL(state, relayCode), actor{cookies: cookies})
	if r.Status != 200 || !showsRelay(r) {
		t.Fatalf("status %d %s", r.Status, r.Raw)
	}
	if got := r.Header.Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Content-Type %q", got)
	}
	if !strings.Contains(string(r.Raw), state+":"+relayCode) {
		t.Errorf("the page does not show state:code to copy:\n%s", r.Raw)
	}
	// No session, no cookie set or cleared, no redirect.
	if sc := r.Header.Values("Set-Cookie"); len(sc) != 0 {
		t.Errorf("Set-Cookie sent: %q", sc)
	}
	if loc := r.Header.Get("Location"); loc != "" {
		t.Errorf("redirected to %q", loc)
	}
	// No token exchange.
	if n := calls.Load(); n != 0 {
		t.Errorf("%d requests to Discord", n)
	}
	// No database write, not even the session renewal the cookie above is due.
	if after := dbFingerprint(t, e); after != before {
		t.Error("the database changed")
	}
	// The session the browser carried is still there, unrenewed and usable.
	if me := e.get("/api/me", actor{cookie: cookies[0]}); me.Status != 200 {
		t.Errorf("the session stopped working: %d", me.Status)
	}
}

// F-10: the page tells the reader never to pass the code on.
func TestSetupRelayPageWarnsNeverToSendTheCode(t *testing.T) {
	e, _ := relayEnv(t)
	r := e.get(relayURL(mintState(t, e, relaySecret), relayCode), anon)
	page := string(r.Raw)
	for _, want := range []string{"only into the terminal where", "you yourself", "Never send it to anyone", "sign in as you", "tell the dashboard owner"} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(page, "works only together with that terminal") {
		t.Error("the page still says the code is harmless")
	}
}

func TestSetupRelayHeaders(t *testing.T) {
	e, _ := relayEnv(t)
	r := e.get(relayURL(mintState(t, e, relaySecret), relayCode), anon)
	h := r.Header
	for k, want := range map[string]string{
		"Cache-Control":          "no-store",
		"Referrer-Policy":        "no-referrer",
		"X-Frame-Options":        "DENY",
		"X-Content-Type-Options": "nosniff",
	} {
		if got := h.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	csp := h.Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'none'", "frame-ancestors 'none'", "base-uri 'none'", "form-action 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP %q lacks %q", csp, want)
		}
	}
	// No scripts, no inline anything without a hash, no other origin.
	for _, bad := range []string{"script-src", "unsafe-inline", "unsafe-eval", "http:", "https:", "*", "data:"} {
		if strings.Contains(csp, bad) {
			t.Errorf("CSP %q contains %q", csp, bad)
		}
	}
	// The one style block is allowed by its hash, and the page has nothing else to load.
	m := regexp.MustCompile(`(?s)<style>(.*?)</style>`).FindSubmatch(r.Raw)
	if m == nil {
		t.Fatal("no style block")
	}
	sum := sha256.Sum256(m[1])
	if want := "style-src 'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"; !strings.Contains(csp, want) {
		t.Errorf("CSP %q does not allow the style block by hash (%s)", csp, want)
	}
	for _, bad := range []string{"<script", "src=", "href=", "<link", "<img", "<form", "<iframe", "url(", "@import"} {
		if strings.Contains(strings.ToLower(string(r.Raw)), bad) {
			t.Errorf("page contains %q:\n%s", bad, r.Raw)
		}
	}
}

// Anything that is not exactly a fresh, correctly signed setup callback takes the normal
// sign-in path, which refuses it: the same redirect and cookie clearing as any bad state.
func TestSetupRelayRefusesAnythingElseThroughTheNormalErrorPath(t *testing.T) {
	e, calls := relayEnv(t)
	normal := e.get("/auth/callback?code=x&state=tampered", anon)
	if normal.Status != 302 || normal.Header.Get("Location") != "/login?error=state" {
		t.Fatalf("normal bad state → %d %s", normal.Status, normal.Header.Get("Location"))
	}
	good := mintState(t, e, relaySecret)
	tampered := []byte(good)
	tampered[len(tampered)-5] ^= 1 // inside the MAC
	// Minted five minutes in the future: its expiry is fifteen minutes from now.
	early, _ := auth.MintSetupState(relaySecret, e.clock.Add(5*time.Minute), nil)
	long := strings.Repeat("a", 300)
	for name, query := range map[string]string{
		"wrong MAC":                 url.Values{"code": {relayCode}, "state": {string(tampered)}}.Encode(),
		"another secret":            url.Values{"code": {relayCode}, "state": {mintState(t, e, "another-install-secret")}}.Encode(),
		"expiry too far ahead":      url.Values{"code": {relayCode}, "state": {early}}.Encode(),
		"unsigned (old shape)":      url.Values{"code": {relayCode}, "state": {auth.SetupStatePrefix + strings.Repeat("A", 43)}}.Encode(),
		"unsigned, signed length":   url.Values{"code": {relayCode}, "state": {auth.SetupStatePrefix + strings.Repeat("A", 54)}}.Encode(),
		"markup in code":            url.Values{"code": {"abc<script>alert(1)</script>"}, "state": {good}}.Encode(),
		"long code":                 url.Values{"code": {long}, "state": {good}}.Encode(),
		"long state":                url.Values{"code": {relayCode}, "state": {good + long}}.Encode(),
		"no code":                   url.Values{"state": {good}}.Encode(),
		"consent cancelled":         url.Values{"error": {"access_denied"}, "state": {good}}.Encode(),
		"two states":                "code=" + relayCode + "&state=" + good + "&state=" + good,
		"two codes":                 "code=" + relayCode + "&code=" + relayCode + "&state=" + good,
		"prefix in caps":            url.Values{"code": {relayCode}, "state": {"Setup." + good[6:]}}.Encode(),
		"prefix only":               url.Values{"code": {relayCode}, "state": {"setup."}}.Encode(),
		"encoded markup":            "code=abc%3Cb%3Edef123&state=" + good,
		"control characters":        url.Values{"code": {relayCode + "\r\n"}, "state": {good}}.Encode(),
		"spoofed text (review F-4)": url.Values{"code": {"Your-session-expired.Sign-in-again-at-club-login.example.com"}, "state": {auth.SetupStatePrefix + "Paste_this_whole_line_to_the_IT_helpdesk"}}.Encode(),
	} {
		t.Run(name, func(t *testing.T) {
			r := e.get("/auth/callback?"+query, anon)
			if r.Status != normal.Status || r.Header.Get("Location") != normal.Header.Get("Location") {
				t.Errorf("→ %d %q, want the normal error path", r.Status, r.Header.Get("Location"))
			}
			if got, want := strings.Join(r.Header.Values("Set-Cookie"), "\n"), strings.Join(normal.Header.Values("Set-Cookie"), "\n"); got != want {
				t.Errorf("cookies differ from the normal path:\n%s\nwant\n%s", got, want)
			}
			if showsRelay(r) || strings.Contains(string(r.Raw), "club-login") {
				t.Error("showed the relay page")
			}
		})
	}
	// Ten minutes on, the good state has expired too.
	e.clock = e.clock.Add(auth.SetupStateTTL)
	if r := e.get(relayURL(good, relayCode), anon); showsRelay(r) || r.Header.Get("Location") != "/login?error=state" {
		t.Errorf("expired state → %d %q", r.Status, r.Header.Get("Location"))
	}
	if n := calls.Load(); n != 0 {
		t.Errorf("%d requests to Discord", n)
	}
}

// With no Discord client secret configured, there is no relay.
func TestSetupRelayIsOffWithoutAClientSecret(t *testing.T) {
	e := newEnv(t)
	state, _ := auth.MintSetupState(relaySecret, e.clock, nil)
	if r := e.get(relayURL(state, relayCode), anon); showsRelay(r) || r.Header.Get("Location") != "/login?error=state" {
		t.Errorf("no Discord configured → %d %q", r.Status, r.Header.Get("Location"))
	}
	e2 := newEnv(t, func(c *Config) { c.Discord = auth.NewDiscord("cid", "", "http://localhost:8090/auth/callback") })
	if r := e2.get(relayURL(state, relayCode), anon); showsRelay(r) {
		t.Error("an empty client secret verified a state")
	}
}

// Review F-3: only the exact path the router sends to authCallback is the relay; any
// other spelling gets the normal session handling (a due session is renewed) and no page.
func TestSetupRelayMatchesOnlyTheRoutedPath(t *testing.T) {
	e, _ := relayEnv(t)
	q := "?" + url.Values{"code": {relayCode}, "state": {mintState(t, e, relaySecret)}}.Encode()
	for _, p := range []string{"/auth/callback/", "/auth//callback", "/auth/./callback", "/x/../auth/callback", "//auth/callback", "/auth/%63allback", "/auth%2Fcallback", "/AUTH/callback"} {
		r := e.get(p+q, actor{cookie: renewalDueSession(t, e)})
		if showsRelay(r) {
			t.Errorf("%s showed the relay page", p)
		}
		if !renewed(e, r) {
			t.Errorf("%s skipped the session handling (→ %d)", p, r.Status)
		}
	}
}

// Review F-7: the withAuth skip is for one page only. A setup query on any other route
// changes nothing about its authentication.
func TestSetupQueryDoesNotSkipAuthElsewhere(t *testing.T) {
	e, _ := relayEnv(t)
	q := "?" + url.Values{"code": {relayCode}, "state": {mintState(t, e, relaySecret)}}.Encode()
	if r := e.get("/api/me"+q, actor{cookie: renewalDueSession(t, e)}); r.Status != 200 || !renewed(e, r) {
		t.Errorf("/api/me with a setup query → %d, renewed %v", r.Status, renewed(e, r))
	}
	if r := e.get("/api/members"+q, anon); r.Status != 401 {
		t.Errorf("anonymous /api/members with a setup query → %d, want 401 from withAuth", r.Status)
	}
	if r := e.do("POST", "/auth/callback"+q, nil, anon); showsRelay(r) || r.Status == 200 {
		t.Errorf("POST → %d, relay %v", r.Status, showsRelay(r))
	}
	if r := e.do("HEAD", "/auth/callback"+q, nil, anon); r.Status == 200 {
		t.Errorf("HEAD → %d", r.Status)
	}
}

// The normal sign-in is unchanged by the branch: its states never start with the prefix,
// and a full sign-in still works with the relay in place.
func TestSignInStatesNeverReachTheSetupRelay(t *testing.T) {
	e := discordEnv(t, `{"id":"100000000000000002","username":"priya"}`, nil)
	// 20 sign-ins fit the per-IP /auth budget (30 a minute) with the requests below;
	// auth.TestSignInStatesNeverLookLikeSetupStates draws thousands of states.
	for range 20 {
		_, state := startLogin(t, e, "/")
		if strings.HasPrefix(state, auth.SetupStatePrefix) || !regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`).MatchString(state) {
			t.Fatalf("sign-in state %q", state)
		}
	}
	cs, state := startLogin(t, e, "/events")
	cb := e.get("/auth/callback?code=good-code&state="+state, actor{cookies: cs})
	if cb.Status != 302 || cb.Header.Get("Location") != "/events" || cookieNamed(setCookies(cb), e.srv.sessionCookieName()) == nil {
		t.Fatalf("normal sign-in → %d %s", cb.Status, cb.Header.Get("Location"))
	}
	// A real setup state with sign-in cookies is still only the relay page: the cookies are
	// neither used nor cleared.
	cs, _ = startLogin(t, e, "/")
	r := e.get(relayURL(mintState(t, e, "secret"), "good-code-1234"), actor{cookies: cs})
	if r.Status != 200 || len(r.Header.Values("Set-Cookie")) != 0 {
		t.Errorf("relay with sign-in cookies → %d, cookies %q", r.Status, r.Header.Values("Set-Cookie"))
	}
}

// strictDiscord binds each code to the code_challenge and redirect_uri of the
// authorization that made it and to the account that approved it, as an OAuth2 server with
// PKCE does. A code is not bound to the state. (From the review of #128.)
type strictDiscord struct {
	*httptest.Server
	mu     sync.Mutex
	codes  map[string]struct{ challenge, redirect, user string }
	tokens map[string]string
}

func newStrictDiscord(t *testing.T) *strictDiscord {
	d := &strictDiscord{codes: map[string]struct{ challenge, redirect, user string }{}, tokens: map[string]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		id, secret, _ := r.BasicAuth()
		if id != "cid" || secret != relaySecret {
			http.Error(w, `{"error":"invalid_client"}`, 401)
			return
		}
		d.mu.Lock()
		defer d.mu.Unlock()
		g, ok := d.codes[r.PostForm.Get("code")]
		if !ok || g.redirect != r.PostForm.Get("redirect_uri") || auth.Challenge(r.PostForm.Get("code_verifier")) != g.challenge {
			http.Error(w, `{"error":"invalid_grant"}`, 400)
			return
		}
		delete(d.codes, r.PostForm.Get("code"))
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		tok := hex.EncodeToString(b)
		d.tokens[tok] = g.user
		_, _ = w.Write([]byte(`{"access_token":"` + tok + `","token_type":"Bearer"}`))
	})
	mux.HandleFunc("GET /me", func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		user := d.tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		d.mu.Unlock()
		if user == "" {
			http.Error(w, `{"message":"401: Unauthorized"}`, 401)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id": user, "username": "victim"})
	})
	d.Server = httptest.NewServer(mux)
	t.Cleanup(d.Close)
	return d
}

// approve plays a Discord account approving an authorization link: Discord redirects that
// browser to redirect_uri with a fresh code and the link's state, whatever the state is.
func (d *strictDiscord) approve(t *testing.T, link, discordUser string) string {
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	b := make([]byte, 15)
	_, _ = rand.Read(b)
	code := hex.EncodeToString(b)
	d.mu.Lock()
	d.codes[code] = struct{ challenge, redirect, user string }{q.Get("code_challenge"), q.Get("redirect_uri"), discordUser}
	d.mu.Unlock()
	cb, _ := url.Parse(q.Get("redirect_uri"))
	return cb.Path + "?" + url.Values{"code": {code}, "state": {q.Get("state")}}.Encode()
}

// Review F-1, as a regression test: an attacker who can reach /auth/login starts a normal
// sign-in, swaps the state of Discord's link for a setup-shaped one, and lures an officer
// to approve it. The relay must not show the officer the code: the callback takes the
// normal error path, and the attacker's own pending sign-in has nothing to finish with.
func TestSetupRelayCannotBeUsedToStealASignInCode(t *testing.T) {
	d := newStrictDiscord(t)
	e := newEnv(t, func(c *Config) {
		cl := auth.NewDiscord("cid", relaySecret, "http://localhost:8090/auth/callback")
		cl.TokenURL, cl.ProfileURL = d.URL+"/token", d.URL+"/me"
		c.Discord = cl
	})
	// 1. The attacker's browser: a normal sign-in start; keep its cookies and Discord link.
	login := e.get("/auth/login?next=/", anon)
	attackerCookies := setCookies(login)
	link, _ := url.Parse(login.Header.Get("Location"))
	attackerState := link.Query().Get("state")
	for _, forged := range []string{
		auth.SetupStatePrefix + strings.Repeat("A", 43), // the shape #128 first accepted
		auth.SetupStatePrefix + strings.Repeat("A", 54), // the signed length, unsigned
		mintState(t, e, "a-guess-at-the-secret"),        // signed with the wrong secret
	} {
		// 2. Swap only the state. 3. The victim (user-priya) approves; Discord sends their
		// browser to the callback.
		q := link.Query()
		q.Set("state", forged)
		link.RawQuery = q.Encode()
		victim := d.approve(t, link.String(), "100000000000000002")
		page := e.get(victim, anon)
		if showsRelay(page) || page.Header.Get("Location") != "/login?error=state" {
			t.Fatalf("lured callback → %d %q, relay %v", page.Status, page.Header.Get("Location"), showsRelay(page))
		}
		cb, _ := url.Parse(victim)
		if strings.Contains(string(page.Raw), cb.Query().Get("code")) {
			t.Fatal("the code is on the page")
		}
	}
	// 4. Without the code, the attacker's pending sign-in has nothing to finish with; even a
	// guessed code fails.
	done := e.get("/auth/callback?"+url.Values{"code": {"guessed-code-0000"}, "state": {attackerState}}.Encode(), actor{cookies: attackerCookies})
	if cookieNamed(setCookies(done), e.srv.sessionCookieName()) != nil {
		t.Fatal("the attacker got a session")
	}
}

// As deployed, /auth/callback is behind Cloudflare Access: the relay is reachable only
// with a valid Access assertion, like the sign-in it sits in.
func TestSetupRelayIsBehindCloudflareAccess(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewAccessVerifier("test-team.cloudflareaccess.com", "expected-aud", "service-aud")
	if err != nil {
		t.Fatal(err)
	}
	verifier.client.Transport = accessTransport(func(r *http.Request) (*http.Response, error) {
		doc := `{"keys":[{"kty":"RSA","kid":"key","alg":"RS256","n":"` + base64.RawURLEncoding.EncodeToString(key.N.Bytes()) + `","e":"AQAB"}]}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(doc))}, nil
	})
	enc := func(v any) string { raw, _ := json.Marshal(v); return base64.RawURLEncoding.EncodeToString(raw) }
	msg := enc(map[string]any{"alg": "RS256", "kid": "key"}) + "." + enc(map[string]any{"iss": "https://test-team.cloudflareaccess.com", "aud": []string{"expected-aud"}, "exp": testNow.Unix() + 3600})
	digest := sha256.Sum256([]byte(msg))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	e, _ := relayEnv(t, func(c *Config) { c.Access = verifier })
	state := mintState(t, e, relaySecret)
	if r := e.get(relayURL(state, relayCode), anon); r.Status != 401 {
		t.Errorf("without Access → %d", r.Status)
	}
	signed := actor{headers: map[string]string{"Cf-Access-Jwt-Assertion": msg + "." + base64.RawURLEncoding.EncodeToString(sig)}}
	if r := e.get(relayURL(state, relayCode), signed); r.Status != 200 || !strings.Contains(string(r.Raw), relayCode) {
		t.Errorf("with Access → %d", r.Status)
	}
}

// Fix round 2: the page shows a signed state's code once. A second request with the same
// state, even with another code, takes the normal error path, with the normal session
// handling.
func TestSetupRelayShowsEachStateOnce(t *testing.T) {
	e, _ := relayEnv(t)
	state := mintState(t, e, relaySecret)
	if r := e.get(relayURL(state, relayCode), anon); !showsRelay(r) {
		t.Fatalf("first request → %d", r.Status)
	}
	normal := e.get("/auth/callback?code=x&state=tampered", anon)
	for _, code := range []string{relayCode, "Another0code0from0Discord0123"} {
		r := e.get(relayURL(state, code), anon)
		if showsRelay(r) || r.Header.Get("Location") != "/login?error=state" {
			t.Errorf("again with %s → %d %q", code[:7], r.Status, r.Header.Get("Location"))
		}
		if got, want := strings.Join(r.Header.Values("Set-Cookie"), "\n"), strings.Join(normal.Header.Values("Set-Cookie"), "\n"); got != want {
			t.Errorf("cookies differ from the normal path:\n%s\nwant\n%s", got, want)
		}
	}
	if r := e.get(relayURL(state, relayCode), actor{cookie: renewalDueSession(t, e)}); showsRelay(r) || !renewed(e, r) {
		t.Errorf("a shown state still skips the session handling (renewed %v)", renewed(e, r))
	}
	// Another state from the same terminal is shown.
	if r := e.get(relayURL(mintState(t, e, relaySecret), relayCode), anon); !showsRelay(r) {
		t.Error("a fresh state was refused")
	}
}

func TestRelaySeenIsBoundedExpiresAndClaimsOnce(t *testing.T) {
	var seen relaySeen
	now := testNow
	exp := now.Add(auth.SetupStateTTL)
	// Many requests at once for one state: exactly one shows it.
	var wg sync.WaitGroup
	var won atomic.Int64
	for range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if seen.claim("setup.one", exp, now) {
				won.Add(1)
			}
		}()
	}
	wg.Wait()
	if won.Load() != 1 || !seen.has("setup.one", now) {
		t.Errorf("%d claims won", won.Load())
	}
	// Full: further states are refused (fail closed) until entries expire.
	for i := 1; i < relaySeenMax; i++ {
		if !seen.claim(fmt.Sprint("setup.", i), exp, now) {
			t.Fatalf("claim %d refused before the cap", i)
		}
	}
	if seen.claim("setup.over", exp, now) {
		t.Error("claimed past the cap")
	}
	later := exp
	if !seen.claim("setup.over", later.Add(auth.SetupStateTTL), later) || seen.has("setup.one", later) {
		t.Error("expired entries were not dropped")
	}
	if n := seen.size(); n != 1 {
		t.Errorf("%d entries after expiry, want 1", n)
	}
}
