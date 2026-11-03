//go:build !dev

package api

import (
	"net/http"
	"testing"
)

// The default (release) build has no dev login at all: DEV_AUTH=1 cannot bring it back.
func TestDefaultBuildHasNoDevLogin(t *testing.T) {
	if DevBuild {
		t.Fatal("DevBuild must be false without -tags dev")
	}
	// Even with DEV_AUTH_KEY, the right key, and a dev_key cookie, nothing answers.
	const key = "release-key-0123456789"
	cookie := actor{cookies: []*http.Cookie{{Name: "dev_key", Value: "anything"}}}
	for _, devAuth := range []bool{false, true} {
		e := newEnv(t, func(c *Config) { c.DevAuth, c.DevAuthKey = devAuth, key })
		for _, p := range []string{"/auth/dev?as=director", "/auth/dev?as=officer", "/auth/dev", "/auth/dev/",
			"/auth/dev?as=leadership&key=" + key, "/auth/dev/info", "/auth/dev/info?key=" + key} {
			r := e.get(p, cookie)
			if r.Status != 404 {
				t.Errorf("DevAuth=%v GET %s → %d, want 404", devAuth, p, r.Status)
			}
			if len(setCookies(r)) != 0 {
				t.Errorf("GET %s set cookies: %v", p, setCookies(r))
			}
		}
	}
	// The route is not registered, so even the static fallback answers it.
	e := newEnv(t, func(c *Config) { c.DevAuth = true })
	if r := e.get("/auth/dev?as=director", anon); r.Status != 404 || e.scalar(`SELECT count(*) FROM sessions`) != int64(0) {
		t.Errorf("a session was created: %d", r.Status)
	}
}
