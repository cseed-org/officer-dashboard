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
	return checkWith(t, Options{URL: url, Register: register})
}

func checkWith(t *testing.T, o Options) ([]Result, string, bool) {
	t.Helper()
	url := o.URL
	res, err := Run(context.Background(), o)
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

const (
	testClientID     = "6f1c2a0b9e.access"
	testClientSecret = "c0ffee5ecret0123456789abcdef0123456789abcdef0123456789abcdef0123"
)

// serviceAccess imitates the setup in docs/deployment.md: the agent paths bypass Access,
// api/ingest and api/agent have a Service Auth policy for one service token, and everything
// else meets the sign-in. strict401 is the "401 Response for Service Auth policies" setting;
// echo puts the presented secret in Access's page, to prove the report never repeats it.
func serviceAccess(strict401, echo bool) func(http.Handler) http.Handler {
	signIn := access(agentPath)
	return func(next http.Handler) http.Handler {
		redirect := signIn(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, "/api/ingest/") && !strings.HasPrefix(r.URL.Path, "/api/agent/") {
				redirect.ServeHTTP(w, r)
				return
			}
			w.Header().Set("Cf-Ray", "8f00000000000009-SEA")
			if r.Header.Get("CF-Access-Client-Id") == testClientID && r.Header.Get("CF-Access-Client-Secret") == testClientSecret {
				next.ServeHTTP(w, r)
				return
			}
			if !strict401 {
				redirect.ServeHTTP(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusUnauthorized)
			body := "<title>Unauthorized</title>"
			if echo {
				body += " presented " + r.Header.Get("CF-Access-Client-Secret")
			}
			_, _ = io.WriteString(w, body)
		})
	}
}

func TestServiceTokenPassesAccess(t *testing.T) {
	for _, strict := range []bool{false, true} {
		ts := dashboard(t, "", serviceAccess(strict, false))
		res, out, ok := checkWith(t, Options{URL: ts.URL, AccessClientID: testClientID, AccessClientSecret: testClientSecret})
		if r := find(res, "POST /api/ingest/luma"); !ok || r.Status != Pass || !strings.Contains(r.Detail, "Access let the service token through") {
			t.Fatalf("strict=%v: the service token should pass:\n%s", strict, out)
		}
		if strings.Contains(out, testClientSecret) {
			t.Fatalf("the report prints the secret:\n%s", out)
		}
	}
}

func TestWrongServiceTokenFails(t *testing.T) {
	// Access refuses the token, as a 401 that echoes it (strict) or as its sign-in redirect.
	for _, strict := range []bool{false, true} {
		ts := dashboard(t, "", serviceAccess(strict, true))
		res, out, ok := checkWith(t, Options{URL: ts.URL, AccessClientID: testClientID, AccessClientSecret: "wrong-" + testClientSecret})
		r := find(res, "POST /api/ingest/luma")
		if ok || r.Status != Fail || !strings.Contains(r.Detail, "Access refused the service token") || !strings.Contains(r.Hint, "step 5") {
			t.Fatalf("strict=%v: a refused service token must fail:\n%s", strict, out)
		}
		if strings.Contains(out, testClientSecret) {
			t.Fatalf("the report prints the secret:\n%s", out)
		}
	}
}

func TestServicePathBehindAccessWithoutTokenIsInfo(t *testing.T) {
	for _, strict := range []bool{false, true} {
		ts := dashboard(t, "", serviceAccess(strict, false))
		res, out, ok := check(t, ts.URL, false)
		r := find(res, "POST /api/ingest/luma")
		if !ok || r.Status != Info || !strings.Contains(r.Detail, "Cloudflare Access") || !strings.Contains(r.Hint, "CF_ACCESS_CLIENT_ID") {
			t.Fatalf("strict=%v: Access in front of the service path without a token is expected:\n%s", strict, out)
		}
	}
}

func TestServiceTokenWithoutAccessReachesTheDashboard(t *testing.T) {
	// Straight to the dashboard (no Cloudflare): the token headers do no harm.
	ts := dashboard(t, "", nil)
	res, out, ok := checkWith(t, Options{URL: ts.URL, AccessClientID: testClientID, AccessClientSecret: testClientSecret})
	if r := find(res, "POST /api/ingest/luma"); !ok || r.Status != Pass {
		t.Fatalf("direct, with a token:\n%s", out)
	}
	res, out, _ = check(t, ts.URL, false)
	if r := find(res, "POST /api/ingest/luma"); r.Status != Info || !strings.Contains(r.Detail, "no Cloudflare in front") {
		t.Fatalf("direct, without a token:\n%s", out)
	}
}

func TestServiceTokenNeedsBothHalves(t *testing.T) {
	for _, o := range []Options{{URL: "https://officers.example", AccessClientID: testClientID}, {URL: "https://officers.example", AccessClientSecret: testClientSecret}} {
		_, err := Run(context.Background(), o)
		if err == nil || !strings.Contains(err.Error(), "set both") || strings.Contains(err.Error(), testClientSecret) {
			t.Errorf("%+v → %v", o, err)
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
		"https://officers.example":         "https://officers.example",
		"https://officers.example/":        "https://officers.example",
		"https://officers.example/mcp":     "https://officers.example",
		"https://officers.example/mcp/":    "https://officers.example",
		" http://localhost:8090/mcp ":      "http://localhost:8090",
		"https://Officers.Example:443/mcp": "https://officers.example",
		"http://[::1]:80/":                 "http://[::1]",
		"https://user:pw@officers.example": "https://officers.example",
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

func TestCloudflareWithoutAccessWarns(t *testing.T) {
	// Through Cloudflare (a quick tunnel, or a forgotten step 2) the consent page reaches the
	// dashboard with no Access in front: not a failure, but never a silent pass.
	ts := dashboard(t, "", func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cf-Ray", "8f00000000000006-SEA")
			w.Header().Set("Cf-Cache-Status", "DYNAMIC")
			next.ServeHTTP(w, r)
		})
	})
	res, out, ok := check(t, ts.URL, false)
	if !ok {
		t.Fatalf("no Access is a warning, not a failure:\n%s", out)
	}
	if r := find(res, "GET /oauth/authorize"); r.Status != Warn || !strings.Contains(r.Hint, "step 3") {
		t.Errorf("authorize through Cloudflare without Access: %+v", r)
	}
	if r := find(res, "POST /api/ingest/luma"); r.Status != Warn || !strings.Contains(r.Hint, "step 5") {
		t.Errorf("service path through Cloudflare without Access: %+v", r)
	}
}

func TestEdgeCacheableFails(t *testing.T) {
	// A MISS means a cache rule made the answer cacheable: the next agent gets this copy.
	// BYPASS and DYNAMIC (what Cloudflare sends for no-store and for JSON) are fine.
	for status, wantFail := range map[string]bool{"MISS": true, "EXPIRED": true, "STALE": true, "BYPASS": false, "DYNAMIC": false} {
		ts := dashboard(t, "", func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/.well-known/") {
					w.Header().Set("Cf-Cache-Status", status)
				}
				next.ServeHTTP(w, r)
			})
		})
		res, out, _ := check(t, ts.URL, false)
		r := find(res, "GET /.well-known/oauth-authorization-server")
		if failed := r.Status == Fail && strings.Contains(r.Detail, "cf-cache-status: "+status) && strings.Contains(r.Hint, "cache rule"); failed != wantFail {
			t.Errorf("cf-cache-status %s: fail=%v, want %v\n%s", status, failed, wantFail, out)
		}
	}
}

func TestCloudflareClassification(t *testing.T) {
	resp := func(status int, header http.Header, body string) *response {
		if header == nil {
			header = http.Header{}
		}
		header.Set("Cf-Ray", "8f00000000000007-SEA")
		return &response{Response: &http.Response{StatusCode: status, Header: header}, body: []byte(body)}
	}
	// Cloudflare's JavaScript Detections script on an ordinary page is not a challenge.
	jsd := `<html><body>not registered<script src="/cdn-cgi/challenge-platform/scripts/jsd/main.js"></script></body></html>`
	if i := cloudflare(resp(400, nil, jsd)); i != nil {
		t.Errorf("JS detections on a 400 page: %+v", i)
	}
	if i := cloudflare(resp(403, nil, `<script src="/cdn-cgi/challenge-platform/h/b/orchestrate/chl_page/v1"></script>`)); i == nil || i.kind != kindChallenge {
		t.Errorf("challenge page without a title: %+v", i)
	}
	// The tunnel-down page in HTML (what a browser-like Accept gets), and a bare 530.
	html := `<title>Cloudflare Tunnel error | officers.example | Cloudflare</title><span class="inline-block">Error</span> <span class="cf-error-code">1033</span>`
	if i := cloudflare(resp(530, http.Header{"Content-Type": {"text/html"}}, html)); i == nil || i.kind != kindTunnel || !strings.Contains(i.what, "1033") {
		t.Errorf("HTML 1033: %+v", i)
	}
	if i := cloudflare(resp(530, nil, "")); i == nil || i.kind != kindTunnel {
		t.Errorf("bare 530: %+v", i)
	}
}

func TestAuthorizeCheckDoesNotDependOnClubBranding(t *testing.T) {
	for _, register := range []bool{false, true} {
		ts := dashboard(t, "", func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/oauth/authorize" {
					next.ServeHTTP(w, r)
					return
				}
				if register {
					io.WriteString(w, `<html><title>Other Club</title><form method="post" action="/oauth/authorize"></form></html>`)
				} else {
					w.WriteHeader(http.StatusBadRequest)
					io.WriteString(w, `<html><title>Other Club</title><h1>Cannot connect this app</h1><p>This app is not registered with the dashboard.</p></html>`)
				}
			})
		})
		res, out, _ := check(t, ts.URL, register)
		if find(res, "GET /oauth/authorize").Status != Pass {
			t.Fatal(out)
		}
	}
}
