package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// Exercise registrations through the real middleware stack. New private routes
// are included automatically, rather than silently missing a static deny-list.
func TestP2AnonymousRouteMatrix(t *testing.T) {
	src, err := parser.ParseFile(token.NewFileSet(), "routes.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	e := newEnv(t)
	actors := map[string]actor{
		"anonymous": anon,
		"forged edge identity": {headers: map[string]string{
			"Cf-Access-Authenticated-User-Email": "untrusted@example.invalid",
			"Cf-Access-Jwt-Assertion":            "untrusted",
			"X-Forwarded-User":                   "user-leadership",
		}},
		"invalid credentials": {
			token: "untrusted",
			cookies: []*http.Cookie{
				{Name: e.srv.sessionCookieName(), Value: "untrusted"},
				{Name: e.srv.portalCookieName(), Value: "untrusted"},
			},
		},
	}
	n := 0
	ast.Inspect(src, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) < 1 {
			return true
		}
		fn, ok := call.Fun.(*ast.Ident)
		if !ok || fn.Name != "h" {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true // public OPTIONS registrations are covered by MCP/CORS tests
		}
		pattern, _ := strconv.Unquote(lit.Value)
		method, path, ok := strings.Cut(pattern, " ")
		if !ok || (!strings.HasPrefix(path, "/api/") && path != "/mcp" && path != "/mcp/{$}") ||
			path == "/api/health" || path == "/api/portal/invite" {
			return true // health and invite preview intentionally accept anonymous requests
		}
		path = strings.NewReplacer("{$}", "", "{id}", "audit-target", "{userId}", "audit-target",
			"{contactId}", "audit-target", "{report}", "trial-balance").Replace(path)
		want := http.StatusUnauthorized
		if path == "/mcp" && method != "POST" {
			want = http.StatusMethodNotAllowed // no stream or session-delete surface
		}
		n++
		for name, who := range actors {
			t.Run(pattern+"/"+name, func(t *testing.T) {
				expectStatus(t, pattern, e.do(method, path, nil, who), want)
			})
		}
		return true
	})
	if n != 169 { // current inventory of private literal registrations, including aliases
		t.Fatalf("route enumeration unexpectedly incomplete: %d registrations", n)
	}
	t.Logf("checked %d private registrations with %d credential states", n, len(actors))
}

func TestP2CookieWritesRequireExactOrigin(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.AppURL = "https://officers.example.org" })
	officer := e.officer()
	for _, origin := range []string{"http://officers.example.org", "https://localhost:8090", "https://officers.example.org.evil.example", "null"} {
		t.Run(origin, func(t *testing.T) {
			who := officer
			who.headers = map[string]string{"Origin": origin}
			expectStatus(t, "officer write", e.do("PUT", "/api/me/prefs", map[string]any{"sidebarWidth": 100}, who), 403)
			who = e.portalLogin("lp-account-alex")
			who.headers = map[string]string{"Origin": origin}
			expectStatus(t, "portal write", e.post("/api/portal/logout", nil, who), 403)
			_, challenge := pkcePair()
			who = officer
			who.headers = map[string]string{"Origin": origin}
			expectStatus(t, "consent", e.post("/oauth/authorize", authorizeForm(registerClient(t, e, "Origin test"), challenge, "allow"), who), 403)
		})
	}
	officer.headers = map[string]string{"Origin": "https://officers.example.org"}
	expectStatus(t, "configured origin through HTTP tunnel", e.do("PUT", "/api/me/prefs", map[string]any{"sidebarWidth": 101}, officer), 200)
	officer.headers = map[string]string{"Origin": "https://officers.example.org:443"}
	expectStatus(t, "default HTTPS port", e.do("PUT", "/api/me/prefs", map[string]any{"sidebarWidth": 102}, officer), 200)
}

func TestP2OAuthRedirectDoesNotChangeMoreThanLoopbackPort(t *testing.T) {
	e := newEnv(t)
	client := registerClient(t, e, "Redirect test")
	_, challenge := pkcePair()
	for _, uri := range []string{
		"http://user@127.0.0.1:9999/callback",
		"http://127.0.0.1:9999/callback#fragment",
		"http://127.0.0.1:9999/%63allback",
	} {
		f := authorizeForm(client, challenge, "allow")
		f.Set("redirect_uri", uri)
		r := e.post("/oauth/authorize", f, e.officer())
		if r.Status != 400 || r.Header.Get("Location") != "" {
			t.Errorf("unregistered redirect variant accepted: status=%d", r.Status)
		}
	}
	// The existing RFC 8252 port exception is retained for native clients.
	f := authorizeForm(client, challenge, "allow")
	f.Set("redirect_uri", "http://127.0.0.1:9999/callback")
	expectStatus(t, "native callback", e.get("/oauth/authorize?"+url.Values(f).Encode(), e.officer()), 200)
}
