// Package mcpcheck checks, from outside, that a deployed dashboard's MCP endpoint is
// reachable the way agents reach it (docs/deployment.md, "Cloudflare Tunnel + Access"):
// both OAuth discovery documents, the 401 challenge on /mcp, the CORS preflight, the token
// endpoint and the consent page. It names Cloudflare interference specifically: an Access
// sign-in redirect, a bot or WAF challenge, a WAF block, an edge-cached answer, a tunnel
// that is down. Nothing it does by default changes the dashboard; Register opts in to
// registering one OAuth client (unused clients are removed after a week).
package mcpcheck

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Status is the outcome of one check.
type Status string

const (
	Pass Status = "PASS"
	Warn Status = "WARN"
	Fail Status = "FAIL"
	Info Status = "INFO"
)

// Result is one line of the report.
type Result struct {
	Status Status
	Name   string
	Detail string
	Hint   string // what to change, for a failure or warning
}

// Options configures Run.
type Options struct {
	// URL is the dashboard's public origin, or its /mcp URL.
	URL string
	// Register also checks POST /oauth/register, which creates a client in the dashboard.
	Register bool
	// Client makes the requests; nil uses one with a 15 s timeout. Redirects are never followed.
	Client *http.Client
}

// Base normalises the URL an officer would paste (https://host, https://host/mcp) to the
// dashboard's origin.
func Base(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", fmt.Errorf("%q is not an http(s) URL: give the dashboard's public URL, e.g. https://officers.example.org", raw)
	}
	switch strings.TrimRight(u.Path, "/") {
	case "", "/mcp":
	default:
		return "", fmt.Errorf("%q: give the dashboard's origin or its /mcp URL, not %s", raw, u.Path)
	}
	// Hosts compare case-insensitively and the default port is implied: normalise both, so
	// the checks compare like with APP_URL's origin.
	host := strings.ToLower(u.Hostname())
	if p := u.Port(); p != "" && !(u.Scheme == "https" && p == "443") && !(u.Scheme == "http" && p == "80") {
		host = net.JoinHostPort(host, p)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return u.Scheme + "://" + host, nil
}

// Run performs every check against the dashboard at o.URL.
func Run(ctx context.Context, o Options) ([]Result, error) {
	base, err := Base(o.URL)
	if err != nil {
		return nil, err
	}
	hc := o.Client
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	c := &checker{ctx: ctx, base: base, hc: noRedirects(hc)}
	if strings.HasPrefix(base, "http://") {
		c.add(Warn, "URL scheme", base+" is plain http", "agents need the public https URL (APP_URL); http is only for a local server")
	}
	c.protectedResource("/.well-known/oauth-protected-resource/mcp")
	c.protectedResource("/.well-known/oauth-protected-resource")
	c.authorizationServer()
	c.mcpChallenge()
	c.preflight()
	c.token()
	clientID := ""
	if o.Register {
		clientID = c.register()
	}
	c.authorize(clientID)
	switch {
	case c.sawCloudflare:
		c.add(Info, "Cloudflare", "responses came through Cloudflare (cf-ray "+c.ray+")", "")
	default:
		c.add(Info, "Cloudflare", "no Cloudflare headers seen: this URL does not go through Cloudflare", "")
	}
	return c.results, nil
}

// Report writes the results as a pass/fail list and reports whether nothing failed.
func Report(w io.Writer, target string, results []Result) bool {
	fmt.Fprintf(w, "mcp-check %s\n\n", target)
	counts := map[Status]int{}
	for _, r := range results {
		counts[r.Status]++
		fmt.Fprintf(w, "  %s  %s: %s\n", r.Status, r.Name, r.Detail)
		if r.Hint != "" {
			fmt.Fprintf(w, "        → %s\n", r.Hint)
		}
	}
	fmt.Fprintf(w, "\n%d passed, %d warnings, %d failed\n", counts[Pass], counts[Warn], counts[Fail])
	return counts[Fail] == 0
}

func noRedirects(hc *http.Client) *http.Client {
	cp := *hc
	cp.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &cp
}

type checker struct {
	ctx           context.Context
	base          string
	hc            *http.Client
	results       []Result
	sawCloudflare bool
	ray           string
}

func (c *checker) add(s Status, name, detail, hint string) {
	c.results = append(c.results, Result{Status: s, Name: name, Detail: detail, Hint: hint})
}

type response struct {
	*http.Response
	body []byte
}

// json decodes a JSON body; nil unless the response is JSON.
func (r *response) json() map[string]any {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(r.body, &m) != nil {
		return nil
	}
	return m
}

func (c *checker) send(method, path string, body io.Reader, header map[string]string) (*response, error) {
	req, err := http.NewRequestWithContext(c.ctx, method, c.base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "dashboard-mcp-check/1")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	res, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 256<<10))
	if ray := res.Header.Get("Cf-Ray"); ray != "" {
		c.sawCloudflare, c.ray = true, ray
	}
	return &response{Response: res, body: raw}, nil
}

// Kinds of Cloudflare interference, each with the fix in docs/deployment.md.
type interference struct {
	kind, what string
}

const (
	kindAccess    = "access"
	kindChallenge = "challenge"
	kindBlock     = "block"
	kindCache     = "cache"
	kindTunnel    = "tunnel"
	kindEdge      = "edge"
)

var hints = map[string]string{
	kindAccess:    "add the agent paths to the Cloudflare Access Bypass application (docs/deployment.md, Cloudflare Tunnel + Access, step 3)",
	kindChallenge: "turn off Bot Fight Mode (Free plan: it cannot be skipped), or skip Super Bot Fight Mode and Browser Integrity Check for the agent paths (step 4)",
	kindBlock:     "a WAF rule blocks this request: find it under Security → Events and skip it for the agent paths (step 4)",
	kindCache:     "add the cache rule that bypasses the cache for the agent paths (step 5)",
	kindTunnel:    "the tunnel is not connected: check `docker compose logs cloudflared` and TUNNEL_TOKEN (step 1)",
	kindEdge:      "Cloudflare answered instead of the dashboard: check the tunnel's published route points at http://dashboard:8090 (step 1)",
}

var (
	// The plain-text error ("error code: 1033") or the HTML page's code span
	// (<span class="cf-error-code">1033</span>).
	cfErrorCode = regexp.MustCompile(`(?i)error(?: code:)?\s*(1\d{3}|5\d{2})\b|cf-error-code[^>]*>\s*(1\d{3}|5\d{2})\b`)
	accessHost  = regexp.MustCompile(`(?i)[a-z0-9-]+\.cloudflareaccess\.com`)
)

// cloudflare names Cloudflare interference in a response, or returns nil.
func cloudflare(r *response) *interference {
	h := r.Header
	if loc := h.Get("Location"); loc != "" {
		if u, err := url.Parse(loc); err == nil {
			host := strings.ToLower(u.Hostname())
			if strings.HasSuffix(host, ".cloudflareaccess.com") || strings.Contains(u.Path, "/cdn-cgi/access/") {
				return &interference{kindAccess, "Cloudflare Access redirected to its sign-in page (" + u.Host + ")"}
			}
		}
	}
	for _, ck := range r.Cookies() {
		if ck.Name == "CF_Authorization" || ck.Name == "CF_AppSession" {
			return &interference{kindAccess, "Cloudflare Access answered (it set the " + ck.Name + " cookie)"}
		}
	}
	if m := accessHost.Find(r.body); m != nil && h.Get("Cf-Ray") != "" {
		return &interference{kindAccess, "Cloudflare Access answered with its sign-in page (" + string(m) + ")"}
	}
	if strings.EqualFold(h.Get("Cf-Mitigated"), "challenge") {
		return &interference{kindChallenge, "Cloudflare challenged the request (cf-mitigated: challenge): Bot Fight Mode, Browser Integrity Check, Security Level or a WAF rule"}
	}
	// The challenge platform's path alone is not enough: Cloudflare injects its JavaScript
	// Detections script (/cdn-cgi/challenge-platform/scripts/jsd/…) into ordinary pages too.
	// A challenge page is a 403 or 503.
	challengeStatus := r.StatusCode == http.StatusForbidden || r.StatusCode == http.StatusServiceUnavailable
	if bytes.Contains(r.body, []byte("<title>Just a moment...</title>")) || (challengeStatus && bytes.Contains(r.body, []byte("/cdn-cgi/challenge-platform/h/"))) {
		return &interference{kindChallenge, `Cloudflare answered with a "Just a moment..." challenge page`}
	}
	fromCloudflare := strings.EqualFold(h.Get("Server"), "cloudflare") || h.Get("Cf-Ray") != ""
	if fromCloudflare && (bytes.Contains(r.body, []byte("Sorry, you have been blocked")) || bytes.Contains(r.body, []byte("Attention Required! | Cloudflare"))) {
		return &interference{kindBlock, "Cloudflare's WAF blocked the request"}
	}
	switch st := strings.ToUpper(h.Get("Cf-Cache-Status")); st {
	case "HIT", "STALE", "REVALIDATED", "UPDATING":
		return &interference{kindCache, "the answer came from Cloudflare's edge cache (cf-cache-status: " + st + "), not the dashboard"}
	case "MISS", "EXPIRED":
		// Cloudflare answers BYPASS for what it will not cache; MISS means a cache rule made
		// this answer cacheable despite no-store, and the next caller may get this copy.
		return &interference{kindCache, "Cloudflare stores this answer in its edge cache (cf-cache-status: " + st + "): the next caller may get this copy"}
	}
	if fromCloudflare && r.StatusCode >= 500 && !isDashboardJSON(r) {
		code := ""
		if m := cfErrorCode.FindSubmatch(r.body); m != nil {
			code = string(m[1]) + string(m[2])
		}
		if code == "1033" || code == "530" || (code == "" && r.StatusCode == 530) {
			return &interference{kindTunnel, fmt.Sprintf("Cloudflare error %s (HTTP %d): the tunnel has no connected cloudflared", or(code, "530"), r.StatusCode)}
		}
		if code != "" {
			return &interference{kindEdge, fmt.Sprintf("Cloudflare error %s (HTTP %d)", code, r.StatusCode)}
		}
		if r.StatusCode == http.StatusBadGateway {
			return &interference{kindEdge, "HTTP 502 from the tunnel: cloudflared is connected but cannot reach the dashboard"}
		}
	}
	return nil
}

// isDashboardJSON reports whether a body is one of the dashboard's own JSON answers.
func isDashboardJSON(r *response) bool { return r.json() != nil }

// fail records a failed check, naming Cloudflare interference when there is any.
func (c *checker) fail(name string, r *response, err error, expected string) {
	switch {
	case err != nil:
		hint := "check the URL and that the tunnel is running"
		var ue *url.Error
		if errors.As(err, &ue) && ue.Timeout() {
			hint = "the request timed out: is the tunnel connected and the dashboard healthy?"
		}
		c.add(Fail, name, err.Error(), hint)
	default:
		if i := cloudflare(r); i != nil {
			c.add(Fail, name, i.what, hints[i.kind])
			return
		}
		c.add(Fail, name, fmt.Sprintf("HTTP %d %s; expected %s", r.StatusCode, snippet(r), expected), "")
	}
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func snippet(r *response) string {
	s := strings.Join(strings.Fields(string(r.body)), " ")
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	if s == "" {
		return "(empty body)"
	}
	return "«" + s + "»"
}

// cacheNote is the warning for an agent-endpoint answer a shared cache could keep.
func (c *checker) cacheNote(name string, r *response) {
	if !strings.Contains(strings.ToLower(r.Header.Get("Cache-Control")), "no-store") {
		c.add(Warn, name, "the answer has no Cache-Control: no-store (a dashboard older than V0.14?)", "upgrade the dashboard, and keep the cache rule (step 5)")
	}
}

func (c *checker) protectedResource(path string) {
	name := "GET " + path
	r, err := c.send("GET", path, nil, map[string]string{"Accept": "application/json"})
	if err != nil || r.StatusCode != 200 || cloudflare(r) != nil {
		c.fail(name, r, err, "200 with the protected resource metadata")
		return
	}
	m := r.json()
	if m == nil {
		c.add(Fail, name, "HTTP 200 but not JSON "+snippet(r), hints[kindEdge])
		return
	}
	want := c.base + "/mcp"
	if res, _ := m["resource"].(string); res != want {
		c.add(Fail, name, fmt.Sprintf("resource is %q, expected %q", res, want), "set APP_URL to "+c.base+" (the URL agents use) and restart the dashboard")
		return
	}
	if !containsString(m["authorization_servers"], c.base) {
		c.add(Fail, name, fmt.Sprintf("authorization_servers %v does not name %s", m["authorization_servers"], c.base), "set APP_URL to "+c.base)
		return
	}
	c.add(Pass, name, "resource "+want, "")
	c.cacheNote(name, r)
}

func (c *checker) authorizationServer() {
	const path = "/.well-known/oauth-authorization-server"
	name := "GET " + path
	r, err := c.send("GET", path, nil, map[string]string{"Accept": "application/json"})
	if err != nil || r.StatusCode != 200 || cloudflare(r) != nil {
		c.fail(name, r, err, "200 with the authorization server metadata")
		return
	}
	m := r.json()
	if m == nil {
		c.add(Fail, name, "HTTP 200 but not JSON "+snippet(r), hints[kindEdge])
		return
	}
	for k, want := range map[string]string{"issuer": c.base, "authorization_endpoint": c.base + "/oauth/authorize", "token_endpoint": c.base + "/oauth/token", "registration_endpoint": c.base + "/oauth/register"} {
		if got, _ := m[k].(string); got != want {
			c.add(Fail, name, fmt.Sprintf("%s is %q, expected %q", k, got, want), "set APP_URL to "+c.base+" and restart the dashboard")
			return
		}
	}
	if !containsString(m["code_challenge_methods_supported"], "S256") {
		c.add(Fail, name, "code_challenge_methods_supported does not list S256", "")
		return
	}
	c.add(Pass, name, "issuer "+c.base, "")
	c.cacheNote(name, r)
}

func (c *checker) mcpChallenge() {
	name := "POST /mcp without a token"
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"mcp-check","version":"1"}}}`
	r, err := c.send("POST", "/mcp", strings.NewReader(body), map[string]string{"Content-Type": "application/json", "Accept": "application/json, text/event-stream"})
	if err != nil || r.StatusCode != 401 || cloudflare(r) != nil {
		c.fail(name, r, err, "the dashboard's 401 with WWW-Authenticate")
		return
	}
	wa := r.Header.Get("Www-Authenticate")
	want := `resource_metadata="` + c.base + `/.well-known/oauth-protected-resource"`
	m := r.json()
	e, _ := m["error"].(map[string]any)
	switch {
	case !strings.HasPrefix(wa, "Bearer "):
		c.add(Fail, name, fmt.Sprintf("401 without a Bearer challenge (WWW-Authenticate %q)", wa), hints[kindEdge])
	case !strings.Contains(wa, want):
		c.add(Fail, name, fmt.Sprintf("the challenge names another resource_metadata URL: %s", wa), "set APP_URL to "+c.base+" and restart the dashboard")
	case e == nil || e["code"] != "unauthenticated":
		c.add(Fail, name, "401 is not the dashboard's own "+snippet(r), hints[kindEdge])
	default:
		c.add(Pass, name, "401, WWW-Authenticate: Bearer "+want, "")
		c.cacheNote(name, r)
	}
}

func (c *checker) preflight() {
	name := "OPTIONS /mcp (CORS preflight)"
	r, err := c.send("OPTIONS", "/mcp", nil, map[string]string{
		"Origin": "https://mcp-check.invalid", "Access-Control-Request-Method": "POST",
		"Access-Control-Request-Headers": "authorization, content-type, mcp-protocol-version",
	})
	if err != nil || r.StatusCode/100 != 2 || cloudflare(r) != nil {
		c.fail(name, r, err, "204 with CORS headers")
		return
	}
	origin := r.Header.Get("Access-Control-Allow-Origin")
	allowH := strings.ToLower(r.Header.Get("Access-Control-Allow-Headers"))
	allowM := strings.ToUpper(r.Header.Get("Access-Control-Allow-Methods"))
	if (origin != "*" && origin != "https://mcp-check.invalid") || !strings.Contains(allowH, "authorization") || !strings.Contains(allowH, "mcp-protocol-version") || !strings.Contains(allowM, "POST") {
		c.add(Fail, name, fmt.Sprintf("HTTP %d but CORS headers are missing (Allow-Origin %q, Allow-Headers %q, Allow-Methods %q)", r.StatusCode, origin, allowH, allowM),
			"something between Cloudflare and the dashboard answers OPTIONS itself; turn off Access's own CORS settings for the Bypass application")
		return
	}
	c.add(Pass, name, fmt.Sprintf("%d, Access-Control-Allow-Origin %s", r.StatusCode, origin), "")
}

func (c *checker) token() {
	name := "POST /oauth/token (bad grant)"
	form := url.Values{"grant_type": {"authorization_code"}, "code": {"mcp-check-not-a-code"}, "client_id": {"mcp-check"}, "code_verifier": {strings.Repeat("v", 43)}}
	r, err := c.send("POST", "/oauth/token", strings.NewReader(form.Encode()), map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Accept": "application/json"})
	if err == nil && r.StatusCode == http.StatusTooManyRequests && cloudflare(r) == nil {
		c.add(Fail, name, "429: this IP used up its sign-in budget", "wait a minute and run mcp-check again")
		return
	}
	if err != nil || r.StatusCode != 400 || cloudflare(r) != nil {
		c.fail(name, r, err, "the dashboard's 400 invalid_grant JSON error")
		return
	}
	if m := r.json(); m == nil || m["error"] != "invalid_grant" {
		c.add(Fail, name, "400 is not the dashboard's OAuth error "+snippet(r), hints[kindEdge])
		return
	}
	c.add(Pass, name, `400 {"error":"invalid_grant"} from the dashboard`, "")
}

// register creates one OAuth client (opt-in) and returns its id, or "".
func (c *checker) register() string {
	name := "POST /oauth/register"
	body := `{"client_name":"mcp-check (unused; removed after a week)","redirect_uris":["http://127.0.0.1:9/mcp-check"],"token_endpoint_auth_method":"none"}`
	r, err := c.send("POST", "/oauth/register", strings.NewReader(body), map[string]string{"Content-Type": "application/json", "Accept": "application/json"})
	if err != nil || r.StatusCode != 201 || cloudflare(r) != nil {
		c.fail(name, r, err, "201 with a client_id")
		return ""
	}
	id, _ := r.json()["client_id"].(string)
	if id == "" {
		c.add(Fail, name, "201 without a client_id "+snippet(r), hints[kindEdge])
		return ""
	}
	c.add(Pass, name, "201, client "+id+" (never used; the dashboard drops it after a week)", "")
	return id
}

// authorize checks the consent page the officer's browser opens. Behind Access it is the
// Access sign-in; without Access it is the dashboard's own page. Either is fine.
func (c *checker) authorize(clientID string) {
	name := "GET /oauth/authorize"
	registered := clientID != ""
	if !registered {
		clientID = "mcp-check-unregistered"
	}
	q := url.Values{"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {"http://127.0.0.1:9/mcp-check"},
		"code_challenge": {challenge()}, "code_challenge_method": {"S256"}, "state": {"mcp-check"}}
	r, err := c.send("GET", "/oauth/authorize?"+q.Encode(), nil, map[string]string{"Accept": "text/html"})
	if err != nil {
		c.fail(name, r, err, "")
		return
	}
	if i := cloudflare(r); i != nil {
		switch i.kind {
		case kindAccess:
			c.add(Pass, name, "Cloudflare Access sign-in ("+accessTeam(r)+"): officers sign in to Access, then to the dashboard", "")
		case kindChallenge:
			c.add(Warn, name, i.what, "a browser can usually pass a challenge, but turning it off for /oauth/authorize avoids a step (step 4)")
		default:
			c.fail(name, r, nil, "")
		}
		return
	}
	loc := r.Header.Get("Location")
	html := string(r.body)
	var detail string
	switch {
	case r.StatusCode == http.StatusFound && strings.HasPrefix(loc, "/login?next="):
		detail = "the dashboard's sign-in (302 → /login), no Cloudflare Access in front"
	case r.StatusCode == http.StatusOK && strings.Contains(html, "CSEED Officer Dashboard"):
		detail = "the dashboard's consent page, no Cloudflare Access in front"
	case !registered && r.StatusCode == http.StatusBadRequest && strings.Contains(html, "CSEED Officer Dashboard") && strings.Contains(html, "not registered"):
		detail = "the dashboard's own page (it refuses the check's unregistered client), no Cloudflare Access in front"
	default:
		c.fail(name, r, nil, "the dashboard's sign-in or a Cloudflare Access sign-in")
		return
	}
	if r.Header.Get("Cf-Ray") != "" {
		// Through Cloudflare the human pages should meet Access (step 2). Only a quick-tunnel
		// rehearsal has none on purpose, so this is a warning rather than a failure.
		c.add(Warn, name, detail, "this page came through Cloudflare without Access: add the Access application for the whole host (step 2); expected only on a quick-tunnel rehearsal")
		return
	}
	c.add(Pass, name, detail, "")
}

// accessTeam is the Access team domain an Access answer points at.
func accessTeam(r *response) string {
	if m := accessHost.FindString(r.Header.Get("Location")); m != "" {
		return m
	}
	if m := accessHost.Find(r.body); m != nil {
		return string(m)
	}
	return "Cloudflare Access"
}

func challenge() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	sum := sha256.Sum256([]byte(base64.RawURLEncoding.EncodeToString(b)))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func containsString(v any, want string) bool {
	list, _ := v.([]any)
	for _, x := range list {
		if s, _ := x.(string); s == want {
			return true
		}
	}
	return false
}
