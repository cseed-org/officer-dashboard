package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/store"
)

const testRedirect = "http://127.0.0.1:33418/callback"

// loopback keeps the OAuth calls clear of the per-IP sign-in rate limit.
func loopback(a actor) actor {
	a.remote = "127.0.0.1:5555"
	return a
}

func registerClient(t *testing.T, e *testEnv, name string, uris ...string) string {
	t.Helper()
	if len(uris) == 0 {
		uris = []string{testRedirect}
	}
	r := e.do("POST", "/oauth/register", map[string]any{"client_name": name, "redirect_uris": uris}, loopback(anon))
	if r.Status != 201 || r.str("client_id") == "" {
		t.Fatalf("register: %d %s", r.Status, r.Raw)
	}
	return r.str("client_id")
}

func pkcePair() (verifier, challenge string) {
	verifier = auth.RandomToken(32)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

func authorizeForm(clientID, challenge, decision string) url.Values {
	return url.Values{"client_id": {clientID}, "redirect_uri": {testRedirect}, "response_type": {"code"}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}, "state": {"st8"}, "decision": {decision}}
}

// connectAgent runs the whole OAuth flow as userID and returns the token response.
func connectAgent(t *testing.T, e *testEnv, userID, clientName string) (clientID string, tokens reply) {
	t.Helper()
	clientID = registerClient(t, e, clientName)
	verifier, challenge := pkcePair()
	who := loopback(e.as(userID))
	r := e.do("POST", "/oauth/authorize", authorizeForm(clientID, challenge, "allow"), who)
	if r.Status != 302 {
		t.Fatalf("authorize: %d %s", r.Status, r.Raw)
	}
	loc, _ := url.Parse(r.Header.Get("Location"))
	if loc.Query().Get("state") != "st8" || loc.Query().Get("code") == "" || !strings.HasPrefix(loc.String(), testRedirect) {
		t.Fatalf("authorize redirect: %s", loc)
	}
	tokens = e.do("POST", "/oauth/token", url.Values{"grant_type": {"authorization_code"}, "code": {loc.Query().Get("code")}, "code_verifier": {verifier}, "client_id": {clientID}, "redirect_uri": {testRedirect}}, loopback(anon))
	if tokens.Status != 200 || tokens.str("access_token") == "" || tokens.str("refresh_token") == "" {
		t.Fatalf("token: %d %s", tokens.Status, tokens.Raw)
	}
	return clientID, tokens
}

type rpcReply struct {
	Status int
	Result map[string]any
	Error  map[string]any
	Raw    []byte
}

func mcpRPC(e *testEnv, token, method string, params any) rpcReply {
	e.t.Helper()
	body := map[string]any{"jsonrpc": "2.0", "id": 7, "method": method}
	if params != nil {
		body["params"] = params
	}
	r := e.do("POST", "/mcp", body, bearer(token))
	out := rpcReply{Status: r.Status, Raw: r.Raw}
	out.Result, _ = r.Body["result"].(map[string]any)
	out.Error, _ = r.Body["error"].(map[string]any)
	return out
}

// toolCall returns the tool's text and isError flag.
func toolCall(e *testEnv, token, name string, args map[string]any) (string, bool) {
	e.t.Helper()
	r := mcpRPC(e, token, "tools/call", map[string]any{"name": name, "arguments": args})
	if r.Result == nil {
		e.t.Fatalf("tools/call %s: %d %s", name, r.Status, r.Raw)
	}
	content, _ := r.Result["content"].([]any)
	text := ""
	if len(content) > 0 {
		text, _ = content[0].(map[string]any)["text"].(string)
	}
	isErr, _ := r.Result["isError"].(bool)
	return text, isErr
}

func toolNames(r rpcReply) map[string]bool {
	out := map[string]bool{}
	tools, _ := r.Result["tools"].([]any)
	for _, t := range tools {
		out[t.(map[string]any)["name"].(string)] = true
	}
	return out
}

func TestMCPDiscoveryAndUnauthenticated(t *testing.T) {
	e := newEnv(t)
	pr := e.mustOK(e.get("/.well-known/oauth-protected-resource", anon))
	if pr.str("resource") != "http://localhost:8090/mcp" || pr.list("authorization_servers")[0] != "http://localhost:8090" {
		t.Fatalf("protected resource: %s", pr.Raw)
	}
	e.mustOK(e.get("/.well-known/oauth-protected-resource/mcp", anon))
	as := e.mustOK(e.get("/.well-known/oauth-authorization-server", anon))
	if as.str("token_endpoint") != "http://localhost:8090/oauth/token" || as.list("code_challenge_methods_supported")[0] != "S256" {
		t.Fatalf("server metadata: %s", as.Raw)
	}
	r := e.do("POST", "/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize"}, anon)
	if r.Status != 401 || !strings.Contains(r.Header.Get("WWW-Authenticate"), `resource_metadata="http://localhost:8090/.well-known/oauth-protected-resource"`) {
		t.Fatalf("no token: %d %q", r.Status, r.Header.Get("WWW-Authenticate"))
	}
	// A dashboard session cookie is not an MCP credential.
	expectStatus(t, "cookie only", e.do("POST", "/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"}, e.officer()), 401)
	r = e.do("POST", "/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"}, bearer("cseed_at_bogus"))
	if r.Status != 401 || !strings.Contains(r.Header.Get("WWW-Authenticate"), `error="invalid_token"`) {
		t.Fatalf("bad token: %d %q", r.Status, r.Header.Get("WWW-Authenticate"))
	}
	expectStatus(t, "GET /mcp has no stream", e.get("/mcp", anon), 405)
	pre := e.do("OPTIONS", "/mcp", nil, anon)
	if pre.Status != 204 || pre.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("preflight: %d %v", pre.Status, pre.Header)
	}
}

func TestOAuthRegisterValidatesRedirects(t *testing.T) {
	e := newEnv(t)
	for _, bad := range []string{"javascript:alert(1)", "http://evil.example/cb", "data:text/html,x", "https://ok.example/cb#frag", "not a url"} {
		r := e.do("POST", "/oauth/register", map[string]any{"client_name": "x", "redirect_uris": []string{bad}}, loopback(anon))
		if r.Status != 400 || r.str("error") != "invalid_redirect_uri" {
			t.Fatalf("%s accepted: %d %s", bad, r.Status, r.Raw)
		}
	}
	expectStatus(t, "no uris", e.do("POST", "/oauth/register", map[string]any{"client_name": "x"}, loopback(anon)), 400)
	expectStatus(t, "confidential client", e.do("POST", "/oauth/register", map[string]any{"redirect_uris": []string{testRedirect}, "token_endpoint_auth_method": "client_secret_basic"}, loopback(anon)), 400)
	for _, good := range []string{"https://claude.ai/api/mcp/auth_callback", "http://localhost:6274/oauth/callback", "cursor://anysphere.cursor-retrieval/oauth/callback"} {
		registerClient(t, e, "ok", good)
	}
}

func TestOAuthAuthorizeFlowAndConsent(t *testing.T) {
	e := newEnv(t)
	clientID := registerClient(t, e, `<b>Evil</b> Agent`)
	_, challenge := pkcePair()
	q := authorizeForm(clientID, challenge, "")
	q.Del("decision")
	path := "/oauth/authorize?" + q.Encode()

	// Signed out: off to sign-in, coming back to the same authorize URL.
	r := e.get(path, anon)
	if r.Status != 302 || r.Header.Get("Location") != "/login?next="+url.QueryEscape(path) {
		t.Fatalf("signed out: %d %q", r.Status, r.Header.Get("Location"))
	}
	if got := auth.SafeNext(path); got != path {
		t.Fatalf("post-login redirect drops the authorize URL: %q", got)
	}
	// Signed in: the consent page, escaped, framed nowhere, no script.
	r = e.get(path, e.officer())
	page := string(r.Raw)
	if r.Status != 200 || !strings.Contains(page, "&lt;b&gt;Evil&lt;/b&gt; Agent") || strings.Contains(page, "<b>Evil") || !strings.Contains(page, "Priya Shah") {
		t.Fatalf("consent page: %d %s", r.Status, page)
	}
	if csp := r.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "frame-ancestors 'none'") || strings.Contains(csp, "script-src") {
		t.Fatalf("consent CSP: %q", csp)
	}
	// Browsers send Origin: null from a no-referrer page, and the Allow post would be refused.
	if rp := r.Header.Get("Referrer-Policy"); rp != "same-origin" {
		t.Fatalf("consent Referrer-Policy: %q", rp)
	}
	// An unregistered redirect URI gets an error page, never a redirect.
	bad := authorizeForm(clientID, challenge, "")
	bad.Set("redirect_uri", "https://evil.example/cb")
	if r := e.get("/oauth/authorize?"+bad.Encode(), e.officer()); r.Status != 400 || r.Header.Get("Location") != "" {
		t.Fatalf("unregistered redirect: %d %q", r.Status, r.Header.Get("Location"))
	}
	if r := e.get("/oauth/authorize?client_id=nope&redirect_uri="+url.QueryEscape(testRedirect), e.officer()); r.Status != 400 || r.Header.Get("Location") != "" {
		t.Fatalf("unknown client: %d", r.Status)
	}
	// Loopback redirects may change port (RFC 8252).
	port := authorizeForm(clientID, challenge, "")
	port.Set("redirect_uri", "http://127.0.0.1:9999/callback")
	expectStatus(t, "loopback port", e.get("/oauth/authorize?"+port.Encode(), e.officer()), 200)
	// PKCE is required.
	noPKCE := authorizeForm(clientID, "", "")
	noPKCE.Del("code_challenge")
	r = e.get("/oauth/authorize?"+noPKCE.Encode(), e.officer())
	if r.Status != 400 || r.Header.Get("Location") != "" || !strings.Contains(string(r.Raw), "S256") {
		t.Fatalf("no PKCE: %d %q", r.Status, r.Header.Get("Location"))
	}
	// Cancel tells the client access_denied.
	r = e.do("POST", "/oauth/authorize", authorizeForm(clientID, challenge, "deny"), loopback(e.officer()))
	if loc, _ := url.Parse(r.Header.Get("Location")); r.Status != 302 || loc.Query().Get("error") != "access_denied" || loc.Query().Get("state") != "st8" {
		t.Fatalf("deny: %d %q", r.Status, r.Header.Get("Location"))
	}
	// A cross-site post cannot authorize.
	xs := loopback(e.officer())
	xs.headers = map[string]string{"Origin": "https://evil.example"}
	expectStatus(t, "cross-origin allow", e.do("POST", "/oauth/authorize", authorizeForm(clientID, challenge, "allow"), xs), 403)
	// Signed out post shows an error, issues nothing.
	if r := e.do("POST", "/oauth/authorize", authorizeForm(clientID, challenge, "allow"), loopback(anon)); r.Status != 401 || r.Header.Get("Location") != "" {
		t.Fatalf("signed-out allow: %d %q", r.Status, r.Header.Get("Location"))
	}
}

func TestOAuthTokenExchangeAndRefresh(t *testing.T) {
	e := newEnv(t)
	clientID := registerClient(t, e, "Claude Code")
	verifier, challenge := pkcePair()
	code := func() string {
		r := e.do("POST", "/oauth/authorize", authorizeForm(clientID, challenge, "allow"), loopback(e.officer()))
		loc, _ := url.Parse(r.Header.Get("Location"))
		return loc.Query().Get("code")
	}
	exchange := func(c, v, cid string) reply {
		return e.do("POST", "/oauth/token", url.Values{"grant_type": {"authorization_code"}, "code": {c}, "code_verifier": {v}, "client_id": {cid}}, loopback(anon))
	}
	other, _ := pkcePair()
	if r := exchange(code(), other, clientID); r.Status != 400 || r.str("error") != "invalid_grant" {
		t.Fatalf("wrong verifier: %d %s", r.Status, r.Raw)
	}
	c := code()
	if r := exchange(c, verifier, "mcp_other"); r.Status != 400 {
		t.Fatalf("wrong client: %d", r.Status)
	}
	// The failed attempt above consumed the code: codes are single use either way.
	expectStatus(t, "reused code", exchange(c, verifier, clientID), 400)
	c = code()
	e.clock = e.clock.Add(oauthCodeTTL + time.Second)
	expectStatus(t, "expired code", exchange(c, verifier, clientID), 400)
	e.clock = testNow

	tok := e.mustOK(exchange(code(), verifier, clientID))
	if tok.str("token_type") != "Bearer" || tok.num("expires_in") != 3600 || tok.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("token response: %s", tok.Raw)
	}
	access, refresh := tok.str("access_token"), tok.str("refresh_token")
	if r := mcpRPC(e, access, "ping", nil); r.Status != 200 {
		t.Fatalf("ping: %d %s", r.Status, r.Raw)
	}
	// Refresh rotates both tokens; the old ones stop working.
	ref := func(rt string) reply {
		return e.do("POST", "/oauth/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {rt}, "client_id": {clientID}}, loopback(anon))
	}
	next := e.mustOK(ref(refresh))
	// (Replaying the rotated refresh token revokes the grant: TestOAuthRefreshReuseRevokes.)
	expectStatus(t, "rotated access", e.do("POST", "/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"}, bearer(access)), 401)
	access = next.str("access_token")
	if r := mcpRPC(e, access, "ping", nil); r.Status != 200 {
		t.Fatalf("ping after refresh: %d", r.Status)
	}
	// Access tokens expire after an hour.
	e.clock = e.clock.Add(oauthAccessTTL + time.Second)
	expectStatus(t, "expired access", e.do("POST", "/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"}, bearer(access)), 401)
	e.mustOK(ref(next.str("refresh_token")))
	e.clock = testNow
	expectStatus(t, "unknown grant type", e.do("POST", "/oauth/token", url.Values{"grant_type": {"password"}}, loopback(anon)), 400)
}

func TestMCPToolsActAsTheOfficer(t *testing.T) {
	e := newEnv(t)
	_, officerTok := connectAgent(t, e, "user-priya", "Claude Code")
	_, directorTok := connectAgent(t, e, "user-director", "Cursor")
	ot, dt := officerTok.str("access_token"), directorTok.str("access_token")

	init := mcpRPC(e, ot, "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "1"}})
	if init.Result["protocolVersion"] != "2025-06-18" || init.Result["instructions"] == "" {
		t.Fatalf("initialize: %s", init.Raw)
	}
	if r := e.do("POST", "/mcp", map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}, bearer(ot)); r.Status != 202 {
		t.Fatalf("notification: %d %s", r.Status, r.Raw)
	}
	if r := mcpRPC(e, ot, "nope", nil); r.Error == nil || r.Error["code"].(float64) != -32601 {
		t.Fatalf("unknown method: %s", r.Raw)
	}

	// The tool list follows the role: officers are not offered director tools.
	officerTools, directorTools := toolNames(mcpRPC(e, ot, "tools/list", nil)), toolNames(mcpRPC(e, dt, "tools/list", nil))
	if !officerTools["create_event"] || officerTools["approve_purchase_request"] || officerTools["update_member"] {
		t.Fatalf("officer tools: %v", officerTools)
	}
	if !directorTools["approve_purchase_request"] || !directorTools["update_member"] || directorTools["set_buildspace_schedule"] {
		t.Fatalf("director tools: %v", directorTools)
	}
	// And calling one anyway is refused.
	if text, isErr := toolCall(e, ot, "update_member", map[string]any{"id": "x", "body": map[string]any{}}); !isErr || !strings.Contains(text, "director") {
		t.Fatalf("officer update_member: %v %s", isErr, text)
	}

	text, isErr := toolCall(e, ot, "whoami", nil)
	if isErr || !strings.Contains(text, `"id":"user-priya"`) {
		t.Fatalf("whoami: %s", text)
	}
	// A write lands as the officer, via the agent, in the audit log.
	text, isErr = toolCall(e, ot, "create_contact", map[string]any{"body": map[string]any{"name": "Grace Agent", "type": "speaker"}})
	if isErr {
		t.Fatalf("create_contact: %s", text)
	}
	var created struct{ Contact struct{ ID string } }
	_ = json.Unmarshal([]byte(text), &created)
	var actor, agentName string
	if err := e.db.R.QueryRow(`SELECT a.actor_user_id, c.name FROM audit_log a JOIN oauth_grants g ON g.id = a.actor_agent_id JOIN oauth_clients c ON c.id = g.client_id WHERE a.entity_id = ?`, created.Contact.ID).Scan(&actor, &agentName); err != nil {
		t.Fatal(err)
	}
	if actor != "user-priya" || agentName != "Claude Code" {
		t.Fatalf("audit: %s via %s", actor, agentName)
	}
	audit := e.mustOK(e.get("/api/admin/audit?entityType=contact&entityId="+created.Contact.ID, e.director()))
	if audit.str("items", 0, "actorAgentName") != "Claude Code" {
		t.Fatalf("audit API: %s", audit.Raw)
	}
	// Path parameters, queries and validation errors pass through.
	text, isErr = toolCall(e, ot, "get_contact", map[string]any{"id": created.Contact.ID})
	if isErr || !strings.Contains(text, "Grace Agent") {
		t.Fatalf("get_contact: %s", text)
	}
	text, isErr = toolCall(e, ot, "list_contacts", map[string]any{"q": "Grace"})
	if isErr || !strings.Contains(text, created.Contact.ID) {
		t.Fatalf("list_contacts: %s", text)
	}
	if text, isErr = toolCall(e, ot, "create_contact", map[string]any{"body": map[string]any{"type": "speaker"}}); !isErr || !strings.Contains(text, "HTTP 400") {
		t.Fatalf("validation error: %s", text)
	}
	if text, isErr = toolCall(e, ot, "get_contact", map[string]any{}); !isErr || !strings.Contains(text, "id") {
		t.Fatalf("missing path param: %s", text)
	}
	if text, isErr = toolCall(e, ot, "get_contact", map[string]any{"id": "../../admin/users"}); !isErr || !strings.Contains(text, "Invalid id") {
		t.Fatalf("path escape: %s", text)
	}
	if r := mcpRPC(e, ot, "tools/call", map[string]any{"name": "no_such_tool"}); r.Error == nil {
		t.Fatalf("unknown tool: %s", r.Raw)
	}
	// Money moves only through the same handler rules: a director's agent approves an
	// officer's submitted request; the requester's own agent cannot.
	if text, isErr := toolCall(e, ot, "approve_purchase_request", map[string]any{"id": "req-0005", "body": map[string]any{}}); !isErr {
		t.Fatalf("officer approved via agent: %s", text)
	}
	text, isErr = toolCall(e, dt, "approve_purchase_request", map[string]any{"id": "req-0005", "body": map[string]any{"comment": "ok"}})
	if isErr || !strings.Contains(text, `"status":"approved"`) {
		t.Fatalf("director approve via agent: %s", text)
	}
}

func TestMCPUploadTool(t *testing.T) {
	e := newEnv(t)
	_, tok := connectAgent(t, e, "user-priya", "Claude Code")
	text, isErr := toolCall(e, tok.str("access_token"), "create_purchase_request", nil)
	if !isErr {
		t.Fatalf("create without body should fail validation: %s", text)
	}
	var id string
	if err := e.db.R.QueryRow(`SELECT id FROM purchase_requests WHERE requester_id = 'user-priya' AND status NOT IN ('closed','cancelled','denied') LIMIT 1`).Scan(&id); err != nil {
		t.Skip("no open request for user-priya in seed data")
	}
	pdf := base64.StdEncoding.EncodeToString([]byte("%PDF-1.4\n1 0 obj<<>>endobj\ntrailer<<>>\n%%EOF\n"))
	text, isErr = toolCall(e, tok.str("access_token"), "attach_receipt", map[string]any{"id": id, "filename": "receipt.pdf", "mimeType": "application/pdf", "contentBase64": pdf})
	if isErr || !strings.Contains(text, "receipt.pdf") {
		t.Fatalf("attach_receipt: %s", text)
	}
	if text, isErr = toolCall(e, tok.str("access_token"), "attach_receipt", map[string]any{"id": id, "filename": "x.pdf", "mimeType": "application/pdf", "contentBase64": "!!!"}); !isErr || !strings.Contains(text, "base64") {
		t.Fatalf("bad base64: %s", text)
	}
}

func TestConnectedAgentsAndRevocation(t *testing.T) {
	e := newEnv(t)
	cid, tok := connectAgent(t, e, "user-priya", "Claude Code")
	access := tok.str("access_token")
	list := e.mustOK(e.get("/api/me/agents", e.officer()))
	if len(list.list("agents")) != 1 || list.str("agents", 0, "clientName") != "Claude Code" {
		t.Fatalf("agents: %s", list.Raw)
	}
	id := list.str("agents", 0, "id")
	if len(e.mustOK(e.get("/api/me/agents", e.director())).list("agents")) != 0 {
		t.Fatal("another officer's agents leaked")
	}
	expectStatus(t, "disconnect someone else's", e.do("DELETE", "/api/me/agents/"+id, nil, e.director()), 404)
	e.mustOK(e.do("DELETE", "/api/me/agents/"+id, nil, e.officer()))
	expectStatus(t, "disconnected", e.do("POST", "/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"}, bearer(access)), 401)
	expectStatus(t, "refresh after disconnect", e.do("POST", "/oauth/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok.str("refresh_token")}, "client_id": {cid}}, loopback(anon)), 400)
	expectStatus(t, "disconnect twice", e.do("DELETE", "/api/me/agents/"+id, nil, e.officer()), 404)

	// Deactivation cuts every agent off at once.
	_, tok = connectAgent(t, e, "user-priya", "Cursor")
	e.mustOK(e.do("PATCH", "/api/admin/users/user-priya", map[string]any{"active": false}, e.director()))
	expectStatus(t, "deactivated officer's agent", e.do("POST", "/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"}, bearer(tok.str("access_token"))), 401)
	var live int
	if err := e.db.R.QueryRow(`SELECT count(*) FROM oauth_grants WHERE user_id = 'user-priya' AND revoked_at IS NULL`).Scan(&live); err != nil || live != 0 {
		t.Fatalf("grants left live after deactivation: %d %v", live, err)
	}
	// Revoked on the server's clock, and audited with the count.
	if n := e.scalar(`SELECT count(*) FROM oauth_grants WHERE user_id = 'user-priya' AND revoked_at != ?`, e.clock.UnixMilli()).(int64); n != 0 {
		t.Fatalf("revoked_at not the server clock: %d", n)
	}
	if got := e.scalar(`SELECT after_json FROM audit_log WHERE action = 'agent.revoked_with_user' AND entity_id = 'user-priya'`); got == nil || !strings.Contains(got.(string), `"count":1`) {
		t.Fatalf("deactivation's agent revocation not audited: %v", got)
	}
	// An inactive officer cannot authorize a new agent either.
	clientID := registerClient(t, e, "Again")
	_, challenge := pkcePair()
	if r := e.do("POST", "/oauth/authorize", authorizeForm(clientID, challenge, "allow"), loopback(e.as("user-priya"))); r.Header.Get("Location") != "" {
		t.Fatalf("inactive officer authorized: %d %q", r.Status, r.Header.Get("Location"))
	}
}

// Every tool must name a real route, or the agent gets "Endpoint not found".
func TestMCPToolsMatchRoutes(t *testing.T) {
	e := newEnv(t)
	mux := http.NewServeMux()
	e.srv.routes(mux)
	if len(mcpTools) < 50 {
		t.Fatalf("only %d tools loaded", len(mcpTools))
	}
	for _, tool := range mcpTools {
		pattern := tool.Path
		for _, seg := range strings.Split(tool.Path, "/") {
			if strings.HasPrefix(seg, ":") {
				pattern = strings.Replace(pattern, seg, "{"+seg[1:]+"}", 1)
			}
		}
		req, _ := http.NewRequest(tool.Method, "http://localhost"+strings.ReplaceAll(tool.Path, ":", "x"), nil)
		_, got := mux.Handler(req)
		if got != tool.Method+" "+pattern {
			t.Errorf("%s: %s %s routes to %q", tool.Name, tool.Method, tool.Path, got)
		}
		if strings.HasPrefix(tool.Path, "/api/admin") || strings.HasPrefix(tool.Path, "/api/ingest") || strings.HasPrefix(tool.Path, "/api/agent") || strings.HasPrefix(tool.Path, "/auth") {
			t.Errorf("%s exposes %s", tool.Name, tool.Path)
		}
		var schema map[string]any
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil || schema["type"] != "object" {
			t.Errorf("%s: input schema must be an object: %v", tool.Name, err)
		}
	}
}

// Arguments outside the schema are refused, not dropped: an update with its fields next to
// `body` (instead of inside it) must not report success while changing nothing.
func TestMCPArgumentChecks(t *testing.T) {
	e := newEnv(t)
	_, tok := connectAgent(t, e, "user-priya", "Claude Code")
	at := tok.str("access_token")
	text, isErr := toolCall(e, at, "update_event", map[string]any{"id": "event-hack-night", "location": "Moved"})
	if !isErr || !strings.Contains(text, "Unknown: location") || !strings.Contains(text, "inside `body`") {
		t.Fatalf("top-level field: %v %s", isErr, text)
	}
	if text, isErr = toolCall(e, at, "update_event", map[string]any{"id": "event-hack-night"}); !isErr || !strings.Contains(text, "Missing: body") {
		t.Fatalf("missing body: %v %s", isErr, text)
	}
	if text, isErr = toolCall(e, at, "update_event", map[string]any{"id": "event-hack-night", "body": nil}); !isErr || !strings.Contains(text, "Missing: body") {
		t.Fatalf("null body: %v %s", isErr, text)
	}
	if text, isErr = toolCall(e, at, "calendar_feed", map[string]any{"start": "2026-11-01"}); !isErr || !strings.Contains(text, "Missing: end") {
		t.Fatalf("missing query arg: %v %s", isErr, text)
	}
	if text, isErr = toolCall(e, at, "update_event", map[string]any{"id": "event-hack-night", "body": map[string]any{"location": "Moved"}}); isErr || !strings.Contains(text, `"location":"Moved"`) {
		t.Fatalf("proper update: %s", text)
	}
	// Optional arguments may be left out.
	if _, isErr = toolCall(e, at, "list_events", map[string]any{}); isErr {
		t.Fatal("list_events without arguments")
	}
	// Files over the limit are refused with their size, before reaching the handler.
	big := base64.StdEncoding.EncodeToString(make([]byte, 10*1024*1024+1))
	if text, isErr = toolCall(e, at, "attach_receipt", map[string]any{"id": "req-0005", "filename": "x.pdf", "mimeType": "application/pdf", "contentBase64": big}); !isErr || !strings.Contains(text, "maximum is 10 MB") {
		t.Fatalf("big file: %s", text)
	}
	if text, isErr = toolCall(e, at, "attach_receipt", map[string]any{"id": "req-0005", "filename": "x.pdf", "mimeType": "application/pdf", "contentBase64": ""}); !isErr {
		t.Fatalf("empty file: %s", text)
	}
	if text, isErr = toolCall(e, at, "finance_report", map[string]any{"report": "trial-balance"}); !isErr || !strings.Contains(text, "director") {
		t.Fatalf("officer trial balance: %s", text)
	}
	if text, isErr = toolCall(e, at, "finance_report", map[string]any{"report": "fund-balances"}); isErr {
		t.Fatalf("officer fund balances: %s", text)
	}
}

func TestMCPTransportDetails(t *testing.T) {
	e := newEnv(t)
	_, tok := connectAgent(t, e, "user-priya", "Claude Code")
	at := tok.str("access_token")
	ping := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"}
	who := bearer(at)
	who.headers = map[string]string{"Mcp-Protocol-Version": "1999-01-01"}
	expectStatus(t, "unsupported protocol header", e.do("POST", "/mcp", ping, who), 400)
	who.headers = map[string]string{"Mcp-Protocol-Version": "2025-06-18"}
	expectStatus(t, "supported protocol header", e.do("POST", "/mcp", ping, who), 200)
	expectStatus(t, "trailing slash", e.do("POST", "/mcp/", ping, bearer(at)), 200)
	expectStatus(t, "no subpaths", e.do("POST", "/mcp/x", ping, bearer(at)), 404)
	if r := e.do("POST", "/mcp", "{nope", bearer(at)); r.Status != 400 || r.at("error", "code").(float64) != -32700 {
		t.Fatalf("parse error: %d %s", r.Status, r.Raw)
	}
	r := e.do("POST", "/mcp", []any{ping, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}, map[string]any{"jsonrpc": "2.0", "id": "s", "method": "ping"}}, bearer(at))
	var batch []map[string]any
	if err := json.Unmarshal(r.Raw, &batch); err != nil || len(batch) != 2 || batch[1]["id"] != "s" {
		t.Fatalf("batch: %d %s", r.Status, r.Raw)
	}
	expectStatus(t, "only notifications", e.do("POST", "/mcp", []any{map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}}, bearer(at)), 202)
	if r := mcpRPC(e, at, "initialize", map[string]any{"protocolVersion": "2030-01-01"}); r.Result["protocolVersion"] != mcpLatestProto {
		t.Fatalf("unknown version negotiation: %s", r.Raw)
	}
}

func TestOAuthHardening(t *testing.T) {
	e := newEnv(t)
	// Client names cannot smuggle invisible or reordering characters onto the consent page.
	if got := cleanClientName("Claude‮ edoC\u0000\n  Code​"); got != "Claude edoC Code" {
		t.Fatalf("cleanClientName: %q", got)
	}
	if got := cleanClientName(strings.Repeat("é", 200)); len([]rune(got)) != 80 {
		t.Fatalf("long name: %d", len([]rune(got)))
	}
	if got := cleanClientName(" ‮ "); got != "MCP client" {
		t.Fatalf("empty name: %q", got)
	}
	clientID := registerClient(t, e, "Claude Code")
	verifier, challenge := pkcePair()
	// Invalid requests get an error page, never a redirect to the (self-registered) client.
	errorPage := func(q url.Values) string {
		r := e.get("/oauth/authorize?"+q.Encode(), e.officer())
		if r.Status != 400 || r.Header.Get("Location") != "" {
			t.Fatalf("expected error page: %d %q", r.Status, r.Header.Get("Location"))
		}
		return string(r.Raw)
	}
	q := authorizeForm(clientID, challenge, "")
	q.Del("decision")
	q.Set("resource", "https://other.example/mcp")
	if got := errorPage(q); !strings.Contains(got, "resource parameter") {
		t.Fatalf("foreign resource: %s", got)
	}
	q.Set("resource", "http://localhost:8090/mcp")
	expectStatus(t, "own resource", e.get("/oauth/authorize?"+q.Encode(), e.officer()), 200)
	q.Del("resource")
	q.Set("state", strings.Repeat("s", 1025))
	errorPage(q)
	q.Set("state", "ok")
	q.Set("code_challenge_method", "plain")
	if got := errorPage(q); !strings.Contains(got, "S256") {
		t.Fatalf("plain PKCE: %s", got)
	}

	// Refresh: client_id may be omitted, must match when sent; a foreign resource is refused.
	allow := e.do("POST", "/oauth/authorize", authorizeForm(clientID, challenge, "allow"), loopback(e.officer()))
	loc, _ := url.Parse(allow.Header.Get("Location"))
	tok := e.mustOK(e.do("POST", "/oauth/token", url.Values{"grant_type": {"authorization_code"}, "code": {loc.Query().Get("code")}, "code_verifier": {verifier}, "client_id": {clientID}, "resource": {"http://localhost:8090/mcp"}}, loopback(anon)))
	expectStatus(t, "refresh foreign resource", e.do("POST", "/oauth/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok.str("refresh_token")}, "resource": {"https://other.example"}}, loopback(anon)), 400)
	expectStatus(t, "refresh other client", e.do("POST", "/oauth/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok.str("refresh_token")}, "client_id": {"mcp_other"}}, loopback(anon)), 400)
	e.mustOK(e.do("POST", "/oauth/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok.str("refresh_token")}}, loopback(anon)))
}

// Connecting the same client again replaces its grant; expired grants drop off the list.
func TestConnectedAgentsListStaysClean(t *testing.T) {
	e := newEnv(t)
	clientID := registerClient(t, e, "Claude Code")
	connect := func() reply {
		verifier, challenge := pkcePair()
		r := e.do("POST", "/oauth/authorize", authorizeForm(clientID, challenge, "allow"), loopback(e.officer()))
		loc, _ := url.Parse(r.Header.Get("Location"))
		return e.mustOK(e.do("POST", "/oauth/token", url.Values{"grant_type": {"authorization_code"}, "code": {loc.Query().Get("code")}, "code_verifier": {verifier}, "client_id": {clientID}}, loopback(anon)))
	}
	first, second := connect(), connect()
	if n := len(e.mustOK(e.get("/api/me/agents", e.officer())).list("agents")); n != 1 {
		t.Fatalf("reconnect left %d rows", n)
	}
	expectStatus(t, "replaced grant", e.do("POST", "/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"}, bearer(first.str("access_token"))), 401)
	if r := mcpRPC(e, second.str("access_token"), "ping", nil); r.Status != 200 {
		t.Fatalf("new grant: %d", r.Status)
	}
	e.clock = e.clock.Add(oauthRefreshTTL + time.Hour)
	if n := len(e.mustOK(e.get("/api/me/agents", e.as("user-priya"))).list("agents")); n != 0 {
		t.Fatalf("expired grant still listed: %d", n)
	}
}

func TestPurgeOAuth(t *testing.T) {
	e := newEnv(t)
	used := registerClient(t, e, "used")
	connectAgent(t, e, "user-priya", "kept")
	_ = used
	registerClient(t, e, "abandoned")
	var before int
	_ = e.db.R.QueryRow(`SELECT count(*) FROM oauth_clients`).Scan(&before)
	var n int64
	err := e.db.Write(context.Background(), func(w *store.Queries) error {
		var err error
		n, err = w.PurgeOAuth(context.Background(), e.clock.Add(8*24*time.Hour).UnixMilli())
		return err
	})
	var after int
	_ = e.db.R.QueryRow(`SELECT count(*) FROM oauth_clients`).Scan(&after)
	// "used" never got a grant either (only registered), so two of three go; "kept" has a grant.
	if err != nil || n != 2 || after != before-2 {
		t.Fatalf("purge: n=%d before=%d after=%d err=%v", n, before, after, err)
	}
}

// Findings of the security review, each pinned by a test.
func TestMCPReviewFixes(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.UploadRateLimit = 2 })
	// No open redirect: a signed-out visitor with a bad request to a client registered by
	// anyone gets a page from the dashboard, not a 302 to the client's URI.
	evil := registerClient(t, e, "x", "https://evil.example/landing")
	for _, q := range []string{"response_type=token", "response_type=code&code_challenge=short&code_challenge_method=S256"} {
		r := e.get("/oauth/authorize?client_id="+evil+"&"+q, anon)
		if r.Header.Get("Location") != "" || r.Status != 400 {
			t.Fatalf("open redirect for %s: %d %q", q, r.Status, r.Header.Get("Location"))
		}
	}

	_, tok := connectAgent(t, e, "user-director", "Claude Code")
	at := tok.str("access_token")
	// Batches are capped.
	var batch []any
	for i := 0; i < mcpMaxBatch+1; i++ {
		batch = append(batch, map[string]any{"jsonrpc": "2.0", "id": i, "method": "ping"})
	}
	expectStatus(t, "oversized batch", e.do("POST", "/mcp", batch, bearer(at)), 400)
	expectStatus(t, "max batch", e.do("POST", "/mcp", batch[:mcpMaxBatch], bearer(at)), 200)

	// Uploads through tools draw on the per-IP upload budget (bearer() comes from 192.0.2.1).
	csv := "name,email,approval_status,checked_in_at\nAda,ada@example.org,approved,\n"
	var limited bool
	for i := 0; i < 4; i++ {
		text, isErr := toolCall(e, at, "import_event_csv", map[string]any{"kind": "luma_guests", "eventId": "event-hack-night", "dryRun": "true", "content": csv})
		limited = limited || (isErr && strings.Contains(text, "Rate limited"))
	}
	if !limited {
		t.Fatal("tool uploads bypass the upload rate limit")
	}

	// Redirects are errors, and dot ids never reach the router.
	for _, id := range []string{".", "..", "../admin/users", `a\b`} {
		if text, isErr := toolCall(e, at, "get_event", map[string]any{"id": id}); !isErr || !strings.Contains(text, "Invalid id") {
			t.Fatalf("id %q: %v %s", id, isErr, text)
		}
	}

	// Officer invites hand out sign-up links, a credential: no officer-invite tools at all
	// (Launchpad invites go by email only through an agent: TestMCPLaunchpadTools).
	for _, tool := range mcpTools {
		if strings.HasPrefix(tool.Path, "/api/invites") {
			t.Fatalf("%s exposes officer invites", tool.Name)
		}
	}
}

// A rotated-away refresh token presented again means a leaked copy: the grant is revoked.
func TestOAuthRefreshReuseRevokes(t *testing.T) {
	e := newEnv(t)
	cid, tok := connectAgent(t, e, "user-priya", "Claude Code")
	refresh := func(rt string) reply {
		return e.do("POST", "/oauth/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {rt}, "client_id": {cid}}, loopback(anon))
	}
	next := e.mustOK(refresh(tok.str("refresh_token")))
	// The same token again at once is the client refreshing twice concurrently (two windows
	// sharing a sign-in): it loses the race, and the agent stays connected.
	expectStatus(t, "concurrent refresh", refresh(tok.str("refresh_token")), 400)
	e.mustOK(e.do("POST", "/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"}, bearer(next.str("access_token"))))
	// Past the race window, a replay means a leaked copy.
	e.clock = e.clock.Add(oauthRefreshRaceGrace)
	defer func() { e.clock = testNow }()
	expectStatus(t, "replayed old refresh", refresh(tok.str("refresh_token")), 400)
	expectStatus(t, "current refresh after replay", refresh(next.str("refresh_token")), 400)
	expectStatus(t, "access after replay", e.do("POST", "/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"}, bearer(next.str("access_token"))), 401)
	var action string
	if err := e.db.R.QueryRow(`SELECT action FROM audit_log WHERE action = 'agent.revoked_token_reuse'`).Scan(&action); err != nil {
		t.Fatalf("reuse not audited: %v", err)
	}
}

// Each agent connection (OAuth grant) gets 120 /mcp calls a minute; one agent hitting it
// leaves the officer's other agents and other officers' agents alone.
func TestMCPRateLimitPerAgent(t *testing.T) {
	e := newEnv(t)
	cid, a := connectAgent(t, e, "user-director", "Claude Code")
	_, b := connectAgent(t, e, "user-director", "Cursor") // same officer, second agent
	_, c := connectAgent(t, e, "user-priya", "Claude Code")
	at := a.str("access_token")
	burn := func(n int) {
		t.Helper()
		for i := 0; i < n; i++ {
			if r := mcpRPC(e, at, "ping", nil); r.Status != 200 {
				t.Fatalf("call %d: %d %s", i+1, r.Status, r.Raw)
			}
		}
	}
	limited := func(what, token string) {
		t.Helper()
		r := e.do("POST", "/mcp", map[string]any{"jsonrpc": "2.0", "id": 7, "method": "ping"}, bearer(token))
		if r.Status != 429 {
			t.Fatalf("%s: want 429, got %d %s", what, r.Status, r.Raw)
		}
		if secs, err := strconv.Atoi(r.Header.Get("Retry-After")); err != nil || secs < 1 || secs > 60 {
			t.Fatalf("%s: Retry-After %q", what, r.Header.Get("Retry-After"))
		}
		msg, _ := r.Body["error"].(map[string]any)["message"].(string)
		if r.Body["jsonrpc"] != "2.0" || r.Body["id"] != float64(7) || r.Body["error"].(map[string]any)["code"] != float64(-32000) || !strings.Contains(msg, "120 calls a minute") {
			t.Fatalf("%s: JSON-RPC error body: %s", what, r.Raw)
		}
	}

	burn(defaultMCPRateLimit)
	limited("121st call", at)
	// Other agents, of the same officer and of another, are unaffected.
	for name, tok := range map[string]string{"second agent": b.str("access_token"), "other officer": c.str("access_token")} {
		if r := mcpRPC(e, tok, "ping", nil); r.Status != 200 {
			t.Fatalf("%s: %d %s", name, r.Status, r.Raw)
		}
	}
	// Refreshing the token keeps the connection's budget: it is the same grant.
	next := e.mustOK(e.do("POST", "/oauth/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {a.str("refresh_token")}, "client_id": {cid}}, loopback(anon)))
	at = next.str("access_token")
	limited("after refresh", at)
	// Unauthenticated requests keep their 401 challenge.
	expectStatus(t, "no token", e.do("POST", "/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"}, anon), 401)

	// The budget refills: a minute later the agent has its full 120 calls again.
	e.clock = e.clock.Add(time.Minute)
	defer func() { e.clock = testNow }()
	burn(defaultMCPRateLimit)
	limited("121st call after reset", at)

	// Every message in a batch is a call: a batch bigger than what is left is refused whole,
	// with an error for each request in it.
	e.clock = e.clock.Add(5 * time.Second) // 10 calls back
	var batch []any
	for i := 0; i < 11; i++ {
		batch = append(batch, map[string]any{"jsonrpc": "2.0", "id": i, "method": "ping"})
	}
	r := e.do("POST", "/mcp", batch, bearer(at))
	if r.Status != 429 || r.Header.Get("Retry-After") == "" || strings.Count(string(r.Raw), `"code":-32000`) != 11 {
		t.Fatalf("over-budget batch: %d %s", r.Status, r.Raw)
	}
	expectStatus(t, "batch within budget", e.do("POST", "/mcp", batch[:10], bearer(at)), 200)
	limited("after batch", at)
}

// #83's officer Launchpad endpoints are tools; the builder/mentor portal is not, and the
// wall between portal accounts and officers' agents holds both ways.
func TestMCPLaunchpadTools(t *testing.T) {
	m := &fakeMailer{}
	e := newEnv(t, func(c *Config) { c.Mailer = m })
	_, ot := connectAgent(t, e, "user-priya", "Claude Code") // officer
	_, dt := connectAgent(t, e, "user-director", "Claude Code")
	officer, director := ot.str("access_token"), dt.str("access_token")

	// No tool reaches the portal (and none is outside /api).
	for _, tool := range mcpTools {
		if !strings.HasPrefix(tool.Path, "/api/") || strings.HasPrefix(tool.Path, "/api/portal") {
			t.Fatalf("%s maps to %s", tool.Name, tool.Path)
		}
	}
	// Director-only Launchpad tools are hidden from an officer's agent and refused on call.
	listed := toolNames(mcpRPC(e, officer, "tools/list", nil))
	for _, name := range []string{"buildspace_overview", "invite_launchpad_builders", "archive_launchpad_project", "preview_launchpad_mentor_assignment", "set_launchpad_mentor_settings"} {
		if !listed[name] {
			t.Fatalf("officer's agent is missing %s", name)
		}
	}
	for _, name := range []string{"create_launchpad_cohort", "delete_launchpad_cohort", "delete_launchpad_project", "invite_launchpad_mentor", "disable_launchpad_account"} {
		if listed[name] {
			t.Fatalf("officer's agent is offered director-only %s", name)
		}
	}
	if !toolNames(mcpRPC(e, director, "tools/list", nil))["delete_launchpad_cohort"] {
		t.Fatal("director's agent is missing delete_launchpad_cohort")
	}
	if text, isErr := toolCall(e, officer, "delete_launchpad_cohort", map[string]any{"id": "cohort-spring-2026"}); !isErr || !strings.Contains(text, "Not allowed") {
		t.Fatalf("officer deleted a cohort: %v %s", isErr, text)
	}
	if e.scalar(`SELECT count(*) FROM buildspace_launchpad_cohorts WHERE id='cohort-spring-2026'`).(int64) != 1 {
		t.Fatal("cohort is gone")
	}

	// An officer Launchpad tool works through /mcp, attributed to the agent.
	if text, isErr := toolCall(e, officer, "buildspace_overview", map[string]any{"cohort": "cohort-spring-2026"}); isErr || !strings.Contains(text, "bs-lumen") {
		t.Fatalf("overview: %v %s", isErr, text)
	}
	if text, isErr := toolCall(e, officer, "archive_launchpad_project", map[string]any{"id": "bs-moss"}); isErr {
		t.Fatalf("archive: %s", text)
	}
	if e.scalar(`SELECT count(*) FROM audit_log WHERE entity_id='bs-moss' AND actor_user_id='user-priya' AND actor_agent_id IS NOT NULL`).(int64) == 0 {
		t.Fatal("archive not attributed to the agent")
	}
	if text, isErr := toolCall(e, officer, "restore_launchpad_project", map[string]any{"id": "bs-moss"}); isErr {
		t.Fatalf("restore: %s", text)
	}
	if text, isErr := toolCall(e, director, "create_launchpad_cohort", map[string]any{"body": map[string]any{"name": "Winter 2027", "startsOn": "2027-01-05"}}); isErr || !strings.Contains(text, "Winter 2027") {
		t.Fatalf("director's agent creates a cohort: %v %s", isErr, text)
	}

	// Invites through an agent go by email only; a sign-up link never reaches the agent.
	inv := func(method string) (string, bool) {
		return toolCall(e, officer, "invite_launchpad_builders", map[string]any{"id": "cohort-fall-2026", "body": map[string]any{"invites": []any{map[string]any{"email": "agent.invite@uw.edu"}}, "method": method}})
	}
	for _, method := range []string{"link", "both"} {
		if text, isErr := inv(method); !isErr || !strings.Contains(text, "email only") {
			t.Fatalf("method %s: %v %s", method, isErr, text)
		}
	}
	text, isErr := inv("email")
	if isErr || strings.Contains(text, "/launchpad/join") || !strings.Contains(text, `"links":[]`) {
		t.Fatalf("email invite: %v %s", isErr, text)
	}
	e.srv.WaitBackground()
	if len(m.all()) != 1 {
		t.Fatalf("invite mails: %d", len(m.all()))
	}
	var accountID string
	if err := e.db.R.QueryRow(`SELECT id FROM buildspace_launchpad_accounts WHERE email='agent.invite@uw.edu'`).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	if text, isErr := toolCall(e, officer, "resend_launchpad_invite", map[string]any{"id": accountID}); isErr || strings.Contains(text, "/launchpad/join") || !strings.Contains(text, `"link":null`) {
		t.Fatalf("resend: %v %s", isErr, text)
	}
	// The same calls from the dashboard still hand back the link.
	if r := e.mustOK(e.post("/api/buildspace-launchpad/accounts/"+accountID+"/resend", nil, e.officer())); !strings.Contains(string(r.Raw), "/launchpad/join") {
		t.Fatalf("dashboard resend lost its link: %s", r.Raw)
	}

	// The wall: a portal session never reaches /mcp or the consent page, and a bearer token
	// never authenticates a portal route.
	portal := e.portalLogin("lp-account-alex")
	expectStatus(t, "portal cookie on /mcp", e.do("POST", "/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"}, portal), 401)
	expectStatus(t, "bearer on the portal", e.get("/api/portal/me", bearer(officer)), 401)
	expectStatus(t, "bearer on the builder portal", e.get("/api/portal/builder", bearer(officer)), 401)
	cid := registerClient(t, e, "Portal probe")
	_, challenge := pkcePair()
	q := authorizeForm(cid, challenge, "allow")
	q.Del("decision")
	if r := e.get("/oauth/authorize?"+q.Encode(), portal); r.Status != 302 || !strings.HasPrefix(r.Header.Get("Location"), "/login?") {
		t.Fatalf("portal session at consent: %d %s", r.Status, r.Header.Get("Location"))
	}
	if r := e.do("POST", "/oauth/authorize", authorizeForm(cid, challenge, "allow"), loopback(portal)); r.Status != 401 || r.Header.Get("Location") != "" {
		t.Fatalf("portal session allowed an agent: %d %s", r.Status, r.Header.Get("Location"))
	}
}

// Without invite email configured, an agent cannot invite at all (it would need the link).
func TestMCPLaunchpadInviteNeedsMailer(t *testing.T) {
	e := newEnv(t)
	_, tok := connectAgent(t, e, "user-priya", "Claude Code")
	text, isErr := toolCall(e, tok.str("access_token"), "invite_launchpad_builders", map[string]any{"id": "cohort-fall-2026", "body": map[string]any{"invites": []any{map[string]any{"email": "x.y@uw.edu"}}}})
	if !isErr || !strings.Contains(text, "not configured") || strings.Contains(text, "/launchpad/join") {
		t.Fatalf("%v %s", isErr, text)
	}
	if e.scalar(`SELECT count(*) FROM buildspace_launchpad_accounts WHERE email='x.y@uw.edu'`).(int64) != 0 {
		t.Fatal("an account was created anyway")
	}
}

// A redeemed code presented again revokes the tokens it bought (RFC 6749 §4.1.2); a code
// that never produced tokens (its one try failed) is simply refused.
func TestOAuthCodeReplayRevokes(t *testing.T) {
	e := newEnv(t)
	clientID := registerClient(t, e, "Claude Code")
	verifier, challenge := pkcePair()
	code := func() string {
		r := e.do("POST", "/oauth/authorize", authorizeForm(clientID, challenge, "allow"), loopback(e.officer()))
		loc, _ := url.Parse(r.Header.Get("Location"))
		return loc.Query().Get("code")
	}
	exchange := func(c string) reply {
		return e.do("POST", "/oauth/token", url.Values{"grant_type": {"authorization_code"}, "code": {c}, "code_verifier": {verifier}, "client_id": {clientID}}, loopback(anon))
	}
	c := code()
	tok := e.mustOK(exchange(c))
	ping := func() int {
		return e.do("POST", "/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"}, bearer(tok.str("access_token"))).Status
	}
	if ping() != 200 {
		t.Fatal("fresh token refused")
	}
	if r := exchange(c); r.Status != 400 || r.str("error") != "invalid_grant" {
		t.Fatalf("replayed code: %d %s", r.Status, r.Raw)
	}
	if ping() != 401 {
		t.Fatal("tokens from a replayed code still work")
	}
	if e.scalar(`SELECT count(*) FROM audit_log WHERE action = 'agent.revoked_code_reuse'`).(int64) != 1 {
		t.Fatal("code replay not audited")
	}
	expectStatus(t, "third try", exchange(c), 400)
	if e.scalar(`SELECT count(*) FROM audit_log WHERE action = 'agent.revoked_code_reuse'`).(int64) != 1 {
		t.Fatal("a revoked grant was revoked (and audited) again")
	}
	// A late replay (past expiry, inside the hour codes are kept) is still recognised.
	c = code()
	tok = e.mustOK(exchange(c))
	e.clock = e.clock.Add(30 * time.Minute)
	defer func() { e.clock = testNow }()
	expectStatus(t, "late replay", exchange(c), 400)
	if e.scalar(`SELECT count(*) FROM oauth_grants WHERE revoked_at IS NULL`).(int64) != 0 {
		t.Fatal("late replay left the grant live")
	}
}

// A signed-out officer goes to sign-in with the authorize URL in a cookie; one too long for
// a cookie gets a page instead of a silent drop to the home page.
func TestOAuthAuthorizeLongRequestWhileSignedOut(t *testing.T) {
	e := newEnv(t)
	clientID := registerClient(t, e, "Claude Code")
	_, challenge := pkcePair()
	q := authorizeForm(clientID, challenge, "")
	q.Del("decision")
	if r := e.get("/oauth/authorize?"+q.Encode(), anon); r.Status != 302 || !strings.HasPrefix(r.Header.Get("Location"), "/login?next=") {
		t.Fatalf("short: %d %s", r.Status, r.Header.Get("Location"))
	}
	q.Set("state", strings.Repeat("s", 1000))
	q.Set("extra", strings.Repeat("%", 700)) // escapes to 2100 bytes
	r := e.get("/oauth/authorize?"+q.Encode(), anon)
	if r.Status != 400 || r.Header.Get("Location") != "" || !strings.Contains(string(r.Raw), "Sign in to the dashboard in this browser first") {
		t.Fatalf("long: %d %s", r.Status, r.Header.Get("Location"))
	}
	// Signed in, the same request shows consent: the cookie is not needed.
	if r := e.get("/oauth/authorize?"+q.Encode(), e.officer()); r.Status != 200 {
		t.Fatalf("long, signed in: %d", r.Status)
	}
}
