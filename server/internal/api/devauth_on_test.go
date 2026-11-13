//go:build dev

package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// devPost signs in through the dev login form.
func (e *testEnv) devPost(form url.Values, who actor) reply {
	e.t.Helper()
	return e.do("POST", "/auth/dev", form, who)
}

func sessionFrom(e *testEnv, r reply) actor {
	return actor{cookie: cookieNamed(setCookies(r), e.srv.sessionCookieName())}
}

func TestDevLoginNeedsTheDevBuildAndDEV_AUTH(t *testing.T) {
	if !DevBuild {
		t.Fatal("DevBuild must be true with -tags dev")
	}
	off := newEnv(t, func(c *Config) { c.DevAuth = false })
	expectStatus(t, "DEV_AUTH unset", off.get("/auth/dev?as=director", anon), 404)
	expectStatus(t, "DEV_AUTH unset POST", off.devPost(url.Values{"as": {"director"}}, anon), 404)

	e := newEnv(t, func(c *Config) { c.DevAuth = true })
	expectStatus(t, "bad role", e.devPost(url.Values{"as": {"root"}}, anon), 400)
	r := e.devPost(url.Values{"as": {"director"}, "next": {"/finance"}}, anon)
	if r.Status != 303 || r.Header.Get("Location") != "/finance" {
		t.Fatalf("dev login → %d %s", r.Status, r.Header.Get("Location"))
	}
	me := e.get("/api/me", sessionFrom(e, r))
	if me.str("user", "role") != "director" || me.str("user", "id") != "user-director" {
		t.Errorf("me %s", me.Raw)
	}
	if r := e.devPost(url.Values{"as": {"officer"}, "next": {"//evil.example"}}, anon); r.Header.Get("Location") != "/" {
		t.Errorf("open redirect: %s", r.Header.Get("Location"))
	}
	r = e.devPost(url.Values{"as": {"officer"}, "user": {"user-riley"}}, anon)
	if me := e.get("/api/me", sessionFrom(e, r)); me.str("user", "id") != "user-riley" {
		t.Errorf("dev login as a chosen user: %s", me.Raw)
	}
	expectStatus(t, "chosen user with another role", e.devPost(url.Values{"as": {"director"}, "user": {"user-riley"}}, anon), 400)
}

// A GET never signs anyone in: a link (or an <img> on another site) only shows a
// confirmation page whose button posts the form.
func TestDevLoginGETOnlyConfirms(t *testing.T) {
	e := newEnv(t, keyed)
	r := e.get("/auth/dev?as=officer&key="+testDevKey+"&next=/events", anon)
	if r.Status != 200 || !strings.HasPrefix(r.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("confirm page → %d %s", r.Status, r.Header.Get("Content-Type"))
	}
	if len(setCookies(r)) != 0 || e.scalar(`SELECT count(*) FROM sessions`) != int64(0) {
		t.Fatal("a GET created a session or set cookies")
	}
	if csp := r.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "form-action 'self'") || strings.Contains(csp, "script-src") {
		t.Errorf("confirm page CSP %q", csp)
	}
	body := string(r.Raw)
	for _, want := range []string{`method="post"`, `action="/auth/dev"`, `name="as" value="officer"`, `name="next" value="/events"`, `name="key"`} {
		if !strings.Contains(body, want) {
			t.Errorf("confirm page lacks %s:\n%s", want, body)
		}
	}
	// Values are escaped, never reflected raw.
	r = e.get("/auth/dev?as=officer&key="+testDevKey+"&next=/%22%3E%3Cscript%3Ealert(1)%3C/script%3E", anon)
	if strings.Contains(string(r.Raw), "<script>alert(1)") {
		t.Errorf("reflected markup: %s", r.Raw)
	}
	expectStatus(t, "confirm without the key", e.get("/auth/dev?as=officer", anon), 404)
}

const testDevKey = "test-dev-key-0123456789abcdef"

func keyed(c *Config) { c.DevAuth, c.DevAuthKey = true, testDevKey }

// With DEV_AUTH_KEY set, every dev login must carry the key: a missing or wrong key looks
// exactly like a release build (no cookies, no session, no hint), and there is no
// remembered-key cookie any more.
func TestDevLoginKeyRequiredEveryTime(t *testing.T) {
	e := newEnv(t, keyed)
	for _, f := range []url.Values{
		{"as": {"director"}},
		{"as": {"director"}, "key": {""}},
		{"as": {"director"}, "key": {"wrong"}},
		{"as": {"director"}, "key": {testDevKey + "x"}},
		{"discord": {"000000000000000001"}},
	} {
		r := e.devPost(f, anon)
		if r.Status != 404 || len(setCookies(r)) != 0 {
			t.Errorf("POST %v → %d cookies %v, want the release build's 404", f, r.Status, setCookies(r))
		}
		if strings.Contains(string(r.Raw), "DEV_AUTH") || strings.Contains(strings.ToLower(string(r.Raw)), "key") {
			t.Errorf("POST %v leaks a hint: %s", f, r.Raw)
		}
	}
	// The old remembered-key cookie no longer authorizes anything.
	old := actor{cookies: []*http.Cookie{{Name: "dev_key", Value: devKeyDigest(testDevKey)}}}
	expectStatus(t, "dev_key cookie", e.devPost(url.Values{"as": {"director"}}, old), 404)
	if n := e.scalar(`SELECT count(*) FROM sessions`); n != int64(0) {
		t.Fatalf("%v sessions created without the key", n)
	}

	r := e.devPost(url.Values{"as": {"officer"}, "key": {testDevKey}, "next": {"/events"}}, anon)
	if r.Status != 303 || r.Header.Get("Location") != "/events" {
		t.Fatalf("keyed dev login → %d %s", r.Status, r.Header.Get("Location"))
	}
	for _, c := range setCookies(r) {
		if strings.Contains(c.Name, "dev_key") {
			t.Errorf("dev login set %s", c.Name)
		}
	}
	if me := e.get("/api/me", sessionFrom(e, r)); me.str("user", "role") != "officer" {
		t.Errorf("me %s", me.Raw)
	}
}

// Login CSRF: another site cannot post the form to sign a visitor in as someone else.
func TestDevLoginRefusesCrossSitePosts(t *testing.T) {
	e := newEnv(t, keyed)
	form := url.Values{"as": {"director"}, "key": {testDevKey}}
	for _, h := range []map[string]string{
		{"Origin": "https://evil.example"},
		{"Sec-Fetch-Site": "cross-site"},
	} {
		r := e.devPost(form, actor{headers: h})
		expectStatus(t, "cross-site "+strings.Join(mapKeys(h), ","), r, 403)
		if len(setCookies(r)) != 0 {
			t.Errorf("cross-site post set cookies")
		}
	}
	expectStatus(t, "same-origin", e.devPost(form, actor{headers: map[string]string{"Origin": "http://localhost:8090", "Sec-Fetch-Site": "same-origin"}}), 303)
}

func mapKeys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// ?discord= signs in as a Discord account the way /auth/callback would, but never as an
// owner or leadership account: those keep needing real Discord sign-in.
func TestDevLoginByDiscordCannotTargetLeadership(t *testing.T) {
	e := newEnv(t, keyed)
	for _, id := range []string{"100000000000000005"} { // user-leadership, the owner
		r := e.devPost(url.Values{"discord": {id}, "key": {testDevKey}}, anon)
		expectStatus(t, "discord= leadership", r, 403)
		if len(setCookies(r)) != 0 {
			t.Error("refused discord= login set cookies")
		}
	}
	r := e.devPost(url.Values{"discord": {"100000000000000002"}, "key": {testDevKey}}, anon)
	if r.Status != 303 {
		t.Fatalf("discord= officer → %d %s", r.Status, r.Raw)
	}
	if me := e.get("/api/me", sessionFrom(e, r)); me.str("user", "id") != "user-priya" {
		t.Errorf("me %s", me.Raw)
	}
}

func TestDevCookieSecureOverHTTPS(t *testing.T) {
	e := newEnv(t, keyed, func(c *Config) { c.AppURL = "https://dash.example.org" })
	r := e.do("POST", "/auth/dev", url.Values{"as": {"officer"}, "key": {testDevKey}}, actor{headers: map[string]string{"Origin": "https://dash.example.org"}})
	s := cookieNamed(setCookies(r), "__Host-cseed_session")
	if r.Status != 303 || s == nil || !s.Secure {
		t.Fatalf("https dev login → %d cookie %+v", r.Status, s)
	}
}

func TestDevInfo(t *testing.T) {
	type info struct{ enabled, keyRequired, authorized bool }
	read := func(r reply) info {
		b := func(k string) bool { v, _ := r.at(k).(bool); return v }
		return info{b("enabled"), b("keyRequired"), b("authorized")}
	}
	off := newEnv(t, func(c *Config) { c.DevAuth = false })
	if r := off.get("/auth/dev/info", anon); r.Status != 404 {
		t.Errorf("DEV_AUTH off: info → %d", r.Status)
	}

	open := newEnv(t, func(c *Config) { c.DevAuth = true })
	if r := open.get("/auth/dev/info", anon); r.Status != 200 || read(r) != (info{true, false, true}) {
		t.Errorf("no key: %d %s", r.Status, r.Raw)
	}

	e := newEnv(t, keyed)
	if r := e.get("/auth/dev/info", anon); r.Status != 200 || read(r) != (info{true, true, false}) || len(setCookies(r)) != 0 {
		t.Errorf("keyed, no key: %d %s", r.Status, r.Raw)
	}
	if r := e.get("/auth/dev/info?key=wrong", anon); read(r) != (info{true, true, false}) || len(setCookies(r)) != 0 {
		t.Errorf("keyed, wrong key: %s", r.Raw)
	}
	r := e.get("/auth/dev/info?key="+testDevKey, anon)
	if read(r) != (info{true, true, true}) || len(setCookies(r)) != 0 {
		t.Errorf("keyed, right key: %s cookies %v", r.Raw, setCookies(r))
	}
	if strings.Contains(string(r.Raw), testDevKey) {
		t.Error("info echoes the key")
	}
}

// Dev login never signs in as an owner: as=leadership picks (or creates) a leadership
// account without ownership, and user= cannot name an owner.
func TestDevLoginNeverSignsInAsAnOwner(t *testing.T) {
	e := newEnv(t, keyed)
	r := e.devPost(url.Values{"as": {"leadership"}, "key": {testDevKey}}, anon)
	if r.Status != 303 {
		t.Fatalf("as=leadership → %d %s", r.Status, r.Raw)
	}
	me := e.get("/api/me", sessionFrom(e, r))
	if me.str("user", "role") != "leadership" || me.at("user", "isOwner") == true || me.str("user", "id") == "user-leadership" {
		t.Errorf("as=leadership signed in as %s", me.Raw)
	}
	// Again: the same non-owner account, not a new one each time.
	again := e.get("/api/me", sessionFrom(e, e.devPost(url.Values{"as": {"leadership"}, "key": {testDevKey}}, anon)))
	if again.str("user", "id") != me.str("user", "id") {
		t.Errorf("second leadership login %s, first %s", again.str("user", "id"), me.str("user", "id"))
	}
	r = e.devPost(url.Values{"as": {"leadership"}, "user": {"user-leadership"}, "key": {testDevKey}}, anon)
	expectStatus(t, "user= an owner", r, 403)
	if len(setCookies(r)) != 0 {
		t.Error("refused owner login set cookies")
	}
}

// Owner controls still need e2e coverage, so a local dev server (no DEV_AUTH_KEY, which the
// startup check confines to loopback) may sign in with as=owner. A keyed, shared test
// deployment never can.
func TestDevLoginAsOwnerOnlyOnALocalUnkeyedServer(t *testing.T) {
	local := newEnv(t, func(c *Config) { c.DevAuth = true })
	r := local.devPost(url.Values{"as": {"owner"}}, anon)
	if r.Status != 303 {
		t.Fatalf("local as=owner → %d %s", r.Status, r.Raw)
	}
	if me := local.get("/api/me", sessionFrom(local, r)); me.at("user", "isOwner") != true {
		t.Errorf("as=owner signed in as %s", me.Raw)
	}
	shared := newEnv(t, keyed)
	r = shared.devPost(url.Values{"as": {"owner"}, "key": {testDevKey}}, anon)
	expectStatus(t, "keyed as=owner", r, 403)
	if len(setCookies(r)) != 0 {
		t.Error("refused owner login set cookies")
	}
}
