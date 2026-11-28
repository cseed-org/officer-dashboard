package api

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/store"
)

// The `setup.` branch of /auth/callback (setuprelay.go). Every test here is one of the
// requirements in the setup brief: it only shows the code, and nothing else happens.

var (
	relayState = "setup." + strings.Repeat("Ab3_-", 8) + "xyz" // 43 characters after the prefix
	relayCode  = "Zq9TqBy3mKcJ0dRw7LhVf2NsXe5Ua1"
)

func relayURL(state, code string) string {
	return "/auth/callback?" + url.Values{"code": {code}, "state": {state}}.Encode()
}

// relayEnv is a server with Discord configured against a fake that counts every request.
func relayEnv(t *testing.T, mutate ...func(*Config)) (*testEnv, *atomic.Int64) {
	var calls atomic.Int64
	d := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"access_token":"tok","token_type":"Bearer","id":"100000000000000002","username":"priya"}`))
	}))
	t.Cleanup(d.Close)
	e := newEnv(t, append([]func(*Config){func(c *Config) {
		cl := auth.NewDiscord("cid", "secret", "http://localhost:8090/auth/callback")
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

// signInCookies are the cookies an officer's browser may carry into the callback: a
// session about to be renewed and a sign-in in progress.
func signInCookies(t *testing.T, e *testEnv) []*http.Cookie {
	token := auth.RandomToken(32)
	if err := e.db.Write(context.Background(), func(w *store.Queries) error {
		return w.InsertSession(context.Background(), auth.SHA256Hex(token), "user-priya", e.clock.Add(time.Hour).UnixMilli(), "t", e.clock.UnixMilli())
	}); err != nil {
		t.Fatal(err)
	}
	return []*http.Cookie{
		{Name: e.srv.sessionCookieName(), Value: token},
		{Name: oauthStateName, Value: "pending-state"},
		{Name: oauthVerifier, Value: "pending-verifier"},
		{Name: nextCookieName, Value: "/events"},
	}
}

func TestSetupRelayShowsTheCodeAndDoesNothingElse(t *testing.T) {
	e, calls := relayEnv(t)
	cookies := signInCookies(t, e)
	before := dbFingerprint(t, e)
	r := e.get(relayURL(relayState, relayCode), actor{cookies: cookies})
	if r.Status != 200 {
		t.Fatalf("status %d %s", r.Status, r.Raw)
	}
	if got := r.Header.Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Content-Type %q", got)
	}
	if !strings.Contains(string(r.Raw), relayState+":"+relayCode) {
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

func TestSetupRelayHeaders(t *testing.T) {
	e, _ := relayEnv(t)
	r := e.get(relayURL(relayState, relayCode), anon)
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

// Anything that is not exactly a setup callback takes the normal sign-in path, which
// refuses it: the same redirect and cookie clearing as any bad state.
func TestSetupRelayRefusesAnythingElseThroughTheNormalErrorPath(t *testing.T) {
	e, calls := relayEnv(t)
	normal := e.get("/auth/callback?code=x&state=tampered", anon)
	if normal.Status != 302 || normal.Header.Get("Location") != "/login?error=state" {
		t.Fatalf("normal bad state → %d %s", normal.Status, normal.Header.Get("Location"))
	}
	long := strings.Repeat("a", 300)
	for name, query := range map[string]string{
		"markup in code":     url.Values{"code": {"abc<script>alert(1)</script>"}, "state": {relayState}}.Encode(),
		"quote in state":     url.Values{"code": {relayCode}, "state": {"setup." + strings.Repeat("A", 40) + `"x`}}.Encode(),
		"long code":          url.Values{"code": {long}, "state": {relayState}}.Encode(),
		"long state":         url.Values{"code": {relayCode}, "state": {"setup." + long}}.Encode(),
		"no code":            url.Values{"state": {relayState}}.Encode(),
		"consent cancelled":  url.Values{"error": {"access_denied"}, "state": {relayState}}.Encode(),
		"two states":         "code=" + relayCode + "&state=" + relayState + "&state=" + relayState,
		"two codes":          "code=" + relayCode + "&code=" + relayCode + "&state=" + relayState,
		"prefix in caps":     url.Values{"code": {relayCode}, "state": {"Setup." + relayState[6:]}}.Encode(),
		"prefix only":        url.Values{"code": {relayCode}, "state": {"setup."}}.Encode(),
		"encoded markup":     "code=abc%3Cb%3Edef123&state=" + relayState,
		"control characters": url.Values{"code": {relayCode + "\r\n"}, "state": {relayState}}.Encode(),
	} {
		t.Run(name, func(t *testing.T) {
			r := e.get("/auth/callback?"+query, anon)
			if r.Status != normal.Status || r.Header.Get("Location") != normal.Header.Get("Location") {
				t.Errorf("→ %d %q, want the normal error path", r.Status, r.Header.Get("Location"))
			}
			if got, want := strings.Join(r.Header.Values("Set-Cookie"), "\n"), strings.Join(normal.Header.Values("Set-Cookie"), "\n"); got != want {
				t.Errorf("cookies differ from the normal path:\n%s\nwant\n%s", got, want)
			}
			if strings.Contains(string(r.Raw), "setup code") {
				t.Error("showed the relay page")
			}
		})
	}
	if n := calls.Load(); n != 0 {
		t.Errorf("%d requests to Discord", n)
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
	// A setup-shaped state with sign-in cookies is still only the relay page: the cookies
	// are neither used nor cleared.
	cs, _ = startLogin(t, e, "/")
	r := e.get(relayURL(relayState, "good-code-1234"), actor{cookies: cs})
	if r.Status != 200 || len(r.Header.Values("Set-Cookie")) != 0 {
		t.Errorf("relay with sign-in cookies → %d, cookies %q", r.Status, r.Header.Values("Set-Cookie"))
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
	if r := e.get(relayURL(relayState, relayCode), anon); r.Status != 401 {
		t.Errorf("without Access → %d", r.Status)
	}
	signed := actor{headers: map[string]string{"Cf-Access-Jwt-Assertion": msg + "." + base64.RawURLEncoding.EncodeToString(sig)}}
	if r := e.get(relayURL(relayState, relayCode), signed); r.Status != 200 || !strings.Contains(string(r.Raw), relayCode) {
		t.Errorf("with Access → %d", r.Status)
	}
}

// The relay works whether or not Discord sign-in is configured: it never uses it.
func TestSetupRelayNeedsNoDiscordConfiguration(t *testing.T) {
	e := newEnv(t)
	if r := e.get(relayURL(relayState, relayCode), anon); r.Status != 200 {
		t.Errorf("→ %d", r.Status)
	}
}
