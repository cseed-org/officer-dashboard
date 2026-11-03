//go:build dev

package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestDevLoginNeedsTheDevBuildAndDEV_AUTH(t *testing.T) {
	if !DevBuild {
		t.Fatal("DevBuild must be true with -tags dev")
	}
	off := newEnv(t, func(c *Config) { c.DevAuth = false })
	expectStatus(t, "DEV_AUTH unset", off.get("/auth/dev?as=director", anon), 404)

	e := newEnv(t, func(c *Config) { c.DevAuth = true })
	expectStatus(t, "bad role", e.get("/auth/dev?as=root", anon), 400)
	r := e.get("/auth/dev?as=director&next=/finance", anon)
	if r.Status != 302 || r.Header.Get("Location") != "/finance" {
		t.Fatalf("dev login → %d %s", r.Status, r.Header.Get("Location"))
	}
	session := cookieNamed(setCookies(r), e.srv.sessionCookieName())
	if session == nil {
		t.Fatal("no session cookie")
	}
	me := e.get("/api/me", actor{cookie: session})
	if me.str("user", "role") != "director" || me.str("user", "id") != "user-director" {
		t.Errorf("me %s", me.Raw)
	}
	r = e.get("/auth/dev?as=officer&next=//evil.example", anon)
	if r.Header.Get("Location") != "/" {
		t.Errorf("open redirect: %s", r.Header.Get("Location"))
	}
	r = e.get("/auth/dev?as=officer&user=user-riley", anon)
	if me := e.get("/api/me", actor{cookie: cookieNamed(setCookies(r), e.srv.sessionCookieName())}); me.str("user", "id") != "user-riley" {
		t.Errorf("dev login as a chosen user: %s", me.Raw)
	}
	expectStatus(t, "chosen user with another role", e.get("/auth/dev?as=director&user=user-riley", anon), 400)
}

const testDevKey = "test-dev-key-0123456789abcdef"

func keyed(c *Config) { c.DevAuth, c.DevAuthKey = true, testDevKey }

// With DEV_AUTH_KEY set, /auth/dev needs the key: a missing or wrong key looks exactly like
// a release build (404, no cookies, no session, no hint).
func TestDevLoginKeyRequired(t *testing.T) {
	e := newEnv(t, keyed)
	for _, p := range []string{
		"/auth/dev?as=director",
		"/auth/dev?as=director&key=",
		"/auth/dev?as=director&key=wrong",
		"/auth/dev?as=director&key=" + testDevKey + "x",
		"/auth/dev?discord=000000000000000001",
	} {
		r := e.get(p, anon)
		if r.Status != 404 || len(setCookies(r)) != 0 {
			t.Errorf("GET %s → %d cookies %v, want a bare 404", p, r.Status, setCookies(r))
		}
		if strings.Contains(string(r.Raw), "DEV_AUTH") || strings.Contains(strings.ToLower(string(r.Raw)), "key") {
			t.Errorf("GET %s leaks a hint: %s", p, r.Raw)
		}
	}
	// A wrong cookie is refused too.
	if r := e.get("/auth/dev?as=director", actor{cookies: []*http.Cookie{{Name: devKeyCookie, Value: devKeyDigest("nope")}}}); r.Status != 404 {
		t.Errorf("wrong dev_key cookie → %d", r.Status)
	}
	if n := e.scalar(`SELECT count(*) FROM sessions`); n != int64(0) {
		t.Fatalf("%v sessions created without the key", n)
	}

	// The right key signs in, keeps next (SafeNext still applies) and remembers the key.
	r := e.get("/auth/dev?as=officer&key="+testDevKey+"&next=/events", anon)
	if r.Status != 302 || r.Header.Get("Location") != "/events" {
		t.Fatalf("keyed dev login → %d %s", r.Status, r.Header.Get("Location"))
	}
	if cookieNamed(setCookies(r), e.srv.sessionCookieName()) == nil {
		t.Fatal("no session cookie")
	}
	dk := cookieNamed(setCookies(r), devKeyCookie)
	if dk == nil || !dk.HttpOnly || dk.SameSite != http.SameSiteLaxMode || dk.Path != "/" || dk.MaxAge != 30*24*3600 {
		t.Fatalf("dev_key cookie %+v", dk)
	}
	if dk.Value == testDevKey || strings.Contains(dk.Value, testDevKey) {
		t.Error("the dev_key cookie must not carry the key itself")
	}
	if r := e.get("/auth/dev?as=officer&key="+testDevKey+"&next=//evil.example", anon); r.Header.Get("Location") != "/" {
		t.Errorf("open redirect: %s", r.Header.Get("Location"))
	}

	// Later, the cookie alone is enough to switch roles.
	r = e.get("/auth/dev?as=director&next=/finance", actor{cookies: []*http.Cookie{dk}})
	if r.Status != 302 || r.Header.Get("Location") != "/finance" {
		t.Fatalf("cookie dev login → %d %s", r.Status, r.Header.Get("Location"))
	}
	me := e.get("/api/me", actor{cookie: cookieNamed(setCookies(r), e.srv.sessionCookieName())})
	if me.str("user", "role") != "director" {
		t.Errorf("me %s", me.Raw)
	}

	// A different key invalidates old cookies.
	e2 := newEnv(t, func(c *Config) { c.DevAuth, c.DevAuthKey = true, "another-key-0123456789" })
	if r := e2.get("/auth/dev?as=director", actor{cookies: []*http.Cookie{dk}}); r.Status != 404 {
		t.Errorf("cookie for an old key → %d", r.Status)
	}
}

func TestDevKeyCookieSecureOverHTTPS(t *testing.T) {
	e := newEnv(t, keyed, func(c *Config) { c.AppURL = "https://dash.example.org" })
	r := e.get("/auth/dev?as=officer&key="+testDevKey, anon)
	dk := cookieNamed(setCookies(r), "__Host-"+devKeyCookie)
	if r.Status != 302 || dk == nil || !dk.Secure {
		t.Fatalf("https dev login → %d cookie %+v", r.Status, dk)
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
	if read(r) != (info{true, true, true}) {
		t.Errorf("keyed, right key: %s", r.Raw)
	}
	dk := cookieNamed(setCookies(r), devKeyCookie)
	if dk == nil {
		t.Fatal("info with the key should remember it")
	}
	if r := e.get("/auth/dev/info", actor{cookies: []*http.Cookie{dk}}); read(r) != (info{true, true, true}) {
		t.Errorf("keyed, cookie: %s", r.Raw)
	}
	if strings.Contains(string(r.Raw), testDevKey) {
		t.Error("info echoes the key")
	}
}
