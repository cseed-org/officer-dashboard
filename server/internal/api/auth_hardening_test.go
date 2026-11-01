package api

import (
	"net/http"
	"testing"
)

// Regression tests from the sign-in red-team (F1, F3).

// F1: over https the short-lived OAuth cookies are __Host- prefixed so a sibling subdomain
// or a plain-http attacker cannot plant them; on http://localhost they keep the plain names.
func TestRT_OAuthTempCookiesNotHostPrefixed(t *testing.T) {
	cases := []struct {
		name   string
		secure bool
		prefix string
	}{
		{"https", true, "__Host-"},
		{"http localhost", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := discordEnv(t, `{"id":"100000000000000002","username":"priya"}`, "", nil)
			e.srv.secure = tc.secure
			r := e.get("/auth/login?next=/finance", anon)
			cs := setCookies(r)
			for _, base := range []string{oauthStateName, oauthVerifier, nextCookieName} {
				c := cookieNamed(cs, tc.prefix+base)
				if c == nil {
					t.Fatalf("missing cookie %q in %v", tc.prefix+base, cs)
				}
				if c.Secure != tc.secure || c.Path != "/" || c.Domain != "" {
					t.Errorf("%s: secure=%v path=%q domain=%q", c.Name, c.Secure, c.Path, c.Domain)
				}
				if tc.secure && cookieNamed(cs, base) != nil {
					t.Errorf("unprefixed %q still set over https", base)
				}
			}
		})
	}
}

// F1: the callback reads and clears the prefixed names, and ignores a planted plain-named state.
func TestRT_CallbackUsesHostPrefixedTempCookies(t *testing.T) {
	e := discordEnv(t, `{"id":"100000000000000002","username":"priya"}`, "", nil)
	e.srv.secure = true
	cs, state := startLogin2(t, e)
	// Planted plain-named cookies alone must not satisfy the callback.
	planted := []*http.Cookie{{Name: oauthStateName, Value: "x"}, {Name: oauthVerifier, Value: "y"}}
	r := e.get("/auth/callback?code=good-code&state=x", actor{cookies: planted})
	if loc := r.Header.Get("Location"); loc != "/login?error=state" {
		t.Fatalf("planted plain cookies accepted: %d %s", r.Status, loc)
	}
	r = e.get("/auth/callback?code=good-code&state="+state, actor{cookies: cs})
	if loc := r.Header.Get("Location"); r.Status != 302 || loc == "/login?error=state" {
		t.Fatalf("legit callback → %d %s", r.Status, loc)
	}
	cleared := map[string]bool{}
	for _, c := range setCookies(r) {
		if c.MaxAge < 0 {
			cleared[c.Name] = true
		}
	}
	for _, base := range []string{oauthStateName, oauthVerifier, nextCookieName} {
		if !cleared["__Host-"+base] {
			t.Errorf("__Host-%s not cleared", base)
		}
	}
}

func startLogin2(t *testing.T, e *testEnv) ([]*http.Cookie, string) {
	t.Helper()
	r := e.get("/auth/login?next=/", anon)
	cs := setCookies(r)
	st := cookieNamed(cs, "__Host-"+oauthStateName)
	if st == nil {
		t.Fatalf("no state cookie: %v", cs)
	}
	return cs, st.Value
}

// F3: an officer never reaches admin data through non-canonical paths.
func TestRT_PathTricks(t *testing.T) {
	e := newEnv(t)
	officer := e.officer()
	paths := []string{
		"/api/admin/users",
		"/api//admin/users",
		"/API/admin/users",
		"/api/admin/../admin/users",
		"/api/admin/users/",
		"/api/admin/users/..",
		"/api/%61dmin/users",
		"/api/admin%2fusers",
		"/api/./admin/users",
		"/api/admin/./users",
		"/api/admin/users%2e",
		"/api/admin/users/.",
	}
	for _, p := range paths {
		if r := e.get(p, officer); r.Status == 200 && r.at("users") != nil {
			t.Fatalf("PATH-TRICK LEAK (officer): %q → 200 with users payload", p)
		}
		if r := e.get(p, anon); r.Status == 200 && r.at("users") != nil {
			t.Fatalf("PATH-TRICK LEAK (anon): %q → 200 with users payload", p)
		}
	}
}

// F3: the session gate itself classifies non-canonical paths as director-only.
func TestCleanPathCanonicalizes(t *testing.T) {
	for in, want := range map[string]string{
		"/api//admin/users":     "/api/admin/users",
		"/api/./admin/users":    "/api/admin/users",
		"/api/x/../admin/users": "/api/admin/users",
		"/api/admin/users/":     "/api/admin/users",
		"/api/health":           "/api/health",
	} {
		if got := cleanPath(in); got != want {
			t.Errorf("cleanPath(%q) = %q, want %q", in, got, want)
		}
	}
}
