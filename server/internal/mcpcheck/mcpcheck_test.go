package mcpcheck

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"officerdashboard/internal/api"
	"officerdashboard/internal/store"
)

// dashboard starts the real dashboard handler, with APP_URL set to its own URL (or appURL),
// behind wrap, which imitates whatever Cloudflare does in front of it.
func dashboard(t *testing.T, appURL string, wrap func(http.Handler) http.Handler) *httptest.Server {
	t.Helper()
	var h http.Handler
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.ServeHTTP(w, r) }))
	t.Cleanup(ts.Close)
	db, err := store.Open(t.TempDir() + "/d.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if appURL == "" {
		appURL = ts.URL
	}
	inner := api.New(api.Config{DB: db, AppURL: appURL, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}).Handler()
	h = inner
	if wrap != nil {
		h = wrap(inner)
	}
	return ts
}

func check(t *testing.T, url string, register bool) ([]Result, string, bool) {
	t.Helper()
	res, err := Run(context.Background(), Options{URL: url, Register: register})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	ok := Report(&out, url, res)
	return res, out.String(), ok
}

func find(res []Result, name string) Result {
	for _, r := range res {
		if strings.HasPrefix(r.Name, name) {
			return r
		}
	}
	return Result{}
}

// agentPath is what the Access Bypass application covers (docs/deployment.md).
func agentPath(p string) bool {
	return p == "/mcp" || strings.HasPrefix(p, "/mcp/") || p == "/oauth/register" || p == "/oauth/token" ||
		strings.HasPrefix(p, "/.well-known/oauth-protected-resource") || p == "/.well-known/oauth-authorization-server"
}

// access imitates Cloudflare Access: everything outside bypass redirects to the team's
// sign-in page and sets CF_AppSession, as Access does for a visitor without a session.
func access(bypass func(string) bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cf-Ray", "8f00000000000001-SEA")
			w.Header().Set("Server", "cloudflare")
			if bypass(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "CF_AppSession", Value: "n0", Path: "/", HttpOnly: true, Secure: true})
			w.Header().Set("Location", "https://cseed.cloudflareaccess.com/cdn-cgi/access/login/officers.example?kid=abc&redirect_url="+r.URL.Path)
			w.WriteHeader(http.StatusFound)
		})
	}
}

func TestPassesAgainstTheDashboard(t *testing.T) {
	ts := dashboard(t, "", nil)
	res, out, ok := check(t, ts.URL+"/mcp", false)
	if !ok {
		t.Fatalf("mcp-check failed against the dashboard:\n%s", out)
	}
	for _, name := range []string{"GET /.well-known/oauth-protected-resource/mcp", "GET /.well-known/oauth-protected-resource", "GET /.well-known/oauth-authorization-server", "POST /mcp without a token", "OPTIONS /mcp", "POST /oauth/token", "GET /oauth/authorize"} {
		if find(res, name).Status != Pass {
			t.Errorf("%s did not pass:\n%s", name, out)
		}
	}
	if find(res, "POST /oauth/register").Name != "" {
		t.Errorf("registration was checked without --register:\n%s", out)
	}
	if !strings.Contains(find(res, "GET /oauth/authorize").Detail, "no Cloudflare Access") {
		t.Errorf("authorize does not say it reached the dashboard directly:\n%s", out)
	}
	if !strings.Contains(out, "does not go through Cloudflare") || strings.Contains(out, "WARN  POST") {
		t.Errorf("report:\n%s", out)
	}
}

func TestRegisterOptIn(t *testing.T) {
	ts := dashboard(t, "", nil)
	res, out, ok := check(t, ts.URL, true)
	if !ok || find(res, "POST /oauth/register").Status != Pass {
		t.Fatalf("with --register:\n%s", out)
	}
	if d := find(res, "GET /oauth/authorize").Detail; !strings.Contains(d, "sign-in (302 → /login)") {
		t.Errorf("a registered client should reach the dashboard's sign-in: %s", d)
	}
}

func TestAccessWithTheBypassPasses(t *testing.T) {
	ts := dashboard(t, "", access(agentPath))
	res, out, ok := check(t, ts.URL, false)
	if !ok {
		t.Fatalf("Access with the bypass should pass:\n%s", out)
	}
	if a := find(res, "GET /oauth/authorize"); a.Status != Pass || !strings.Contains(a.Detail, "Cloudflare Access sign-in (cseed.cloudflareaccess.com)") {
		t.Errorf("authorize: %+v", a)
	}
	if !strings.Contains(out, "came through Cloudflare (cf-ray 8f00000000000001-SEA)") {
		t.Errorf("report:\n%s", out)
	}
}

func TestAccessWithoutTheBypassFails(t *testing.T) {
	// Only the discovery documents bypass Access: /mcp, the token endpoint and the
	// preflight hit the Access sign-in, which no agent can pass.
	ts := dashboard(t, "", access(func(p string) bool { return strings.HasPrefix(p, "/.well-known/") }))
	res, out, ok := check(t, ts.URL, false)
	if ok {
		t.Fatalf("passed with /mcp behind Access:\n%s", out)
	}
	for _, name := range []string{"POST /mcp without a token", "OPTIONS /mcp", "POST /oauth/token"} {
		r := find(res, name)
		if r.Status != Fail || !strings.Contains(r.Detail, "Cloudflare Access redirected") || !strings.Contains(r.Hint, "Bypass application") {
			t.Errorf("%s: %+v", name, r)
		}
	}
}

func TestBotChallengeFails(t *testing.T) {
	// Bot Fight Mode: every non-browser request gets the managed challenge.
	ts := dashboard(t, "", func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cf-Ray", "8f00000000000002-SEA")
			w.Header().Set("Cf-Mitigated", "challenge")
			w.Header().Set("Content-Type", "text/html; charset=UTF-8")
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `<!DOCTYPE html><html><head><title>Just a moment...</title></head><body><script src="/cdn-cgi/challenge-platform/h/b/orchestrate/chl_page/v1"></script></body></html>`)
		})
	})
	res, out, ok := check(t, ts.URL, false)
	if ok {
		t.Fatalf("passed behind a challenge:\n%s", out)
	}
	if r := find(res, "POST /mcp without a token"); r.Status != Fail || !strings.Contains(r.Detail, "cf-mitigated: challenge") || !strings.Contains(r.Hint, "Bot Fight Mode") {
		t.Errorf("mcp: %+v", r)
	}
	if r := find(res, "GET /oauth/authorize"); r.Status != Warn {
		t.Errorf("a challenge on the browser page is a warning, not a failure: %+v", r)
	}
	// The body alone names it too (some challenges come without the header).
	r := &response{Response: &http.Response{StatusCode: 403, Header: http.Header{"Server": {"cloudflare"}}}, body: []byte(`<title>Just a moment...</title>`)}
	if i := cloudflare(r); i == nil || i.kind != kindChallenge {
		t.Errorf("challenge page without header: %+v", i)
	}
}

func TestWAFBlockFails(t *testing.T) {
	ts := dashboard(t, "", func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cf-Ray", "8f00000000000003-SEA")
			if r.URL.Path == "/oauth/token" {
				w.Header().Set("Content-Type", "text/html")
				w.WriteHeader(http.StatusForbidden)
				_, _ = io.WriteString(w, `<title>Attention Required! | Cloudflare</title><h1>Sorry, you have been blocked</h1>`)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	res, out, ok := check(t, ts.URL, false)
	if r := find(res, "POST /oauth/token"); ok || r.Status != Fail || !strings.Contains(r.Detail, "WAF blocked") {
		t.Fatalf("WAF block on /oauth/token:\n%s", out)
	}
}

func TestEdgeCacheHitFails(t *testing.T) {
	ts := dashboard(t, "", func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cf-Ray", "8f00000000000004-SEA")
			if strings.HasPrefix(r.URL.Path, "/.well-known/") {
				w.Header().Set("Cf-Cache-Status", "HIT")
			}
			next.ServeHTTP(w, r)
		})
	})
	res, out, ok := check(t, ts.URL, false)
	if ok {
		t.Fatalf("passed with cached discovery:\n%s", out)
	}
	for _, name := range []string{"GET /.well-known/oauth-protected-resource/mcp", "GET /.well-known/oauth-authorization-server"} {
		if r := find(res, name); r.Status != Fail || !strings.Contains(r.Detail, "cf-cache-status: HIT") || !strings.Contains(r.Hint, "cache rule") {
			t.Errorf("%s: %+v", name, r)
		}
	}
	if r := find(res, "POST /mcp without a token"); r.Status != Pass {
		t.Errorf("mcp should still pass: %+v", r)
	}
}

func TestTunnelDownFails(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cf-Ray", "8f00000000000005-SEA")
		w.Header().Set("Server", "cloudflare")
		w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
		w.WriteHeader(530)
		_, _ = io.WriteString(w, "error code: 1033")
	}))
	defer ts.Close()
	res, out, ok := check(t, ts.URL, false)
	if r := find(res, "POST /mcp without a token"); ok || r.Status != Fail || !strings.Contains(r.Detail, "error 1033") || !strings.Contains(r.Hint, "cloudflared") {
		t.Fatalf("tunnel down:\n%s", out)
	}
}

func TestWrongAppURLFails(t *testing.T) {
	// The tunnel routes this hostname, but APP_URL names another one.
	ts := dashboard(t, "https://officers.example", nil)
	res, out, ok := check(t, ts.URL, false)
	if ok {
		t.Fatalf("passed with a mismatched APP_URL:\n%s", out)
	}
	if r := find(res, "GET /.well-known/oauth-protected-resource/mcp"); r.Status != Fail || !strings.Contains(r.Detail, `"https://officers.example/mcp"`) || !strings.Contains(r.Hint, "APP_URL") {
		t.Errorf("protected resource: %+v", r)
	}
	if r := find(res, "POST /mcp without a token"); r.Status != Fail || !strings.Contains(r.Detail, "another resource_metadata URL") {
		t.Errorf("challenge: %+v", r)
	}
}

func TestNotTheDashboardFails(t *testing.T) {
	// Some other site (a static host, a misrouted tunnel) answers every path with a page.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<h1>Welcome</h1>")
	}))
	defer ts.Close()
	res, out, ok := check(t, ts.URL, false)
	if ok {
		t.Fatalf("passed against another site:\n%s", out)
	}
	for _, r := range res {
		if r.Status == Pass {
			t.Errorf("%s passed against another site", r.Name)
		}
	}
}

func TestBase(t *testing.T) {
	for in, want := range map[string]string{
		"https://officers.example":      "https://officers.example",
		"https://officers.example/":     "https://officers.example",
		"https://officers.example/mcp":  "https://officers.example",
		"https://officers.example/mcp/": "https://officers.example",
		" http://localhost:8090/mcp ":   "http://localhost:8090",
	} {
		if got, err := Base(in); err != nil || got != want {
			t.Errorf("Base(%q) = %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"officers.example", "ftp://x", "https://officers.example/events", ""} {
		if _, err := Base(bad); err == nil {
			t.Errorf("Base(%q) accepted", bad)
		}
	}
}

func TestPlainHTTPIsAWarning(t *testing.T) {
	ts := dashboard(t, "", nil)
	res, _, ok := check(t, ts.URL, false)
	if !ok || find(res, "URL scheme").Status != Warn {
		t.Errorf("plain http: ok=%v %+v", ok, find(res, "URL scheme"))
	}
}
