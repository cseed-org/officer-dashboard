//go:build dev

package api

import "testing"

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
