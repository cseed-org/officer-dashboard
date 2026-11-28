package cffake

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// Account is a stateful fake account. It starts from a fixture (Fresh, Live, Complete),
// accepts the writes the apply engine sends, stores them in its routes, and serves them
// back on the GET paths discovery reads. Response shapes follow the API reference and
// the live reads in the spike report; where the reference left a shape open the choice
// is noted beside the handler.
//
// It answers any write it does not model with 405, and records every request, so a test
// can assert that no DELETE (or any unexpected write) was ever sent.
type Account struct {
	*Server
}

// Injection makes matching requests behave differently, for failure tests. A request
// matches when Method and Path are equal (Path is the URL path, without query).
//
//   - Status != 0: answer with that error and do not apply the write (unless Apply).
//   - Result != nil: apply the write, then answer success with Result instead of the
//     normal response (a create that returns a different shape).
//   - After != nil: apply the write, then call After before answering; a test uses
//     it to change what is stored (Set), so a read-back sees something else.
//
// Skip lets that many matching requests through untouched first. Times is how many
// requests it then affects (0 means one).
type Injection struct {
	Method, Path string
	Status, Code int
	Message      string
	Apply        bool
	Result       any
	After        func()
	Skip         int
	Times        int
}

type accountState struct {
	mu         sync.Mutex
	next       int
	injections []*Injection
	onWrite    func(Request)
	// TunnelTokenInCreate makes a tunnel create answer include "token", as the
	// guide's example shows; the reference schema lists no token.
	tokenInCreate bool
	secrets       []string
	// putReplaces makes the organization and Bot Management PUTs replace every writable
	// field: a field the body omits is reset. The reference does not say which it is.
	putReplaces bool
}

// NewAccount starts a stateful fake from routes.
func NewAccount(routes map[string]Response) *Account {
	s := Start(routes)
	s.acct = &accountState{}
	// Every tunnel in the fixture can hand out its token (GET …/token).
	for _, t := range s.list(acct("/cfd_tunnel")) {
		if m, ok := t.(map[string]any); ok {
			id := fmt.Sprint(m["id"])
			s.routes[acct("/cfd_tunnel/"+id+"/token")] = Response{Result: s.acct.tunnelToken(id)}
		}
	}
	return &Account{Server: s}
}

// Inject adds a failure injection.
func (a *Account) Inject(in Injection) {
	a.acct.mu.Lock()
	defer a.acct.mu.Unlock()
	if in.Times == 0 {
		in.Times = 1
	}
	a.acct.injections = append(a.acct.injections, &in)
}

// OnWrite calls f after each write is stored, before its response is sent. A test uses
// it to simulate a crash between a create and the next step.
func (a *Account) OnWrite(f func(Request)) {
	a.acct.mu.Lock()
	defer a.acct.mu.Unlock()
	a.acct.onWrite = f
}

// TunnelTokenInCreate makes tunnel creates return the token in the create response.
func (a *Account) TunnelTokenInCreate(on bool) {
	a.acct.mu.Lock()
	defer a.acct.mu.Unlock()
	a.acct.tokenInCreate = on
}

// PutReplaces switches the organization and Bot Management PUTs from merging the body
// (the default) to replacing every writable field, so an omitted field is reset.
func (a *Account) PutReplaces(on bool) {
	a.acct.mu.Lock()
	defer a.acct.mu.Unlock()
	a.acct.putReplaces = on
}

// Secrets returns every secret value the fake has handed out (tunnel tokens, client
// secrets), so a test can check none of them reached output, errors or logs.
func (a *Account) Secrets() []string {
	a.acct.mu.Lock()
	defer a.acct.mu.Unlock()
	return append([]string(nil), a.acct.secrets...)
}

// Writes returns every request that was not a GET.
func (a *Account) Writes() []Request { return a.NonGET() }

// Route returns the current result stored for a path (for assertions).
func (a *Account) Route(path string) (Response, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	r, ok := a.routes[path]
	return r, ok
}

func (st *accountState) injection(req Request) *Injection {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, in := range st.injections {
		if in.Times > 0 && in.Method == req.Method && in.Path == req.Path {
			if in.Skip > 0 {
				in.Skip--
				continue
			}
			in.Times--
			return in
		}
	}
	return nil
}

func (st *accountState) id(prefix string) string {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.next++
	return fmt.Sprintf("%s-%d", prefix, st.next)
}

func (st *accountState) uuid() string {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.next++
	return fmt.Sprintf("%08x-0000-4000-8000-%012x", st.next, st.next)
}

func (st *accountState) secret(kind string) string {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.next++
	v := fmt.Sprintf("%s-%d-0123456789abcdef", kind, st.next)
	st.secrets = append(st.secrets, v)
	return v
}

// errReply is a write the fake refuses, Cloudflare-shaped.
type errReply struct {
	status, code int
	msg          string
}

// intercept handles injections and writes. It returns false to let the read-only
// handler serve the request (every GET without an injection).
func (st *accountState) intercept(s *Server, req Request, w http.ResponseWriter, fail func(int, int, string)) bool {
	in := st.injection(req)
	if in != nil && in.Status != 0 && !in.Apply {
		fail(in.Status, in.Code, orDefault(in.Message, fmt.Sprintf("HTTP %d from the fake", in.Status)))
		return true
	}
	if req.Method == http.MethodGet && in == nil {
		return false
	}
	var result any
	var bad *errReply
	if req.Method == http.MethodGet {
		s.mu.Lock()
		route := s.routes[req.Path]
		s.mu.Unlock()
		result = route.Result
	} else {
		var body map[string]any
		if err := json.Unmarshal([]byte(req.Body), &body); err != nil {
			fail(http.StatusBadRequest, 10026, "request body is not a JSON object")
			return true
		}
		s.mu.Lock()
		result, bad = st.write(s, req, body)
		s.mu.Unlock()
		if bad != nil {
			fail(bad.status, bad.code, bad.msg)
			return true
		}
		st.mu.Lock()
		hook := st.onWrite
		st.mu.Unlock()
		if hook != nil {
			hook(req)
		}
		if in != nil && in.After != nil {
			in.After()
		}
	}
	if in != nil {
		if in.Status != 0 {
			fail(in.Status, in.Code, orDefault(in.Message, fmt.Sprintf("HTTP %d from the fake", in.Status)))
			return true
		}
		if in.Result != nil {
			result = in.Result
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "errors": []any{}, "messages": []any{}, "result": result})
	return true
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// list returns the stored list at path, creating an empty one.
func (s *Server) list(path string) []any {
	l, _ := s.routes[path].Result.([]any)
	return l
}

func (s *Server) appendTo(path string, v any) {
	s.routes[path] = Response{Result: append(s.list(path), v)}
}

// write applies one write to the routes. The caller holds s.mu.
func (st *accountState) write(s *Server, req Request, body map[string]any) (any, *errReply) {
	p := req.Path
	notModelled := &errReply{http.StatusMethodNotAllowed, 10000, "the fake does not model " + req.Method + " " + p}
	switch req.Method {
	case http.MethodPost:
		switch {
		case p == acct("/cfd_tunnel"):
			return st.createTunnel(s, body)
		case p == zone("/dns_records"):
			// The reference: content "Must not match the record's name"; a CNAME cannot
			// share its name with any other record.
			if fmt.Sprint(body["content"]) == fmt.Sprint(body["name"]) {
				return nil, &errReply{http.StatusBadRequest, 9039, "content must not match the record's name"}
			}
			for _, r := range s.list(p) {
				if m, ok := r.(map[string]any); ok && strings.EqualFold(fmt.Sprint(m["name"]), fmt.Sprint(body["name"])) {
					return nil, &errReply{http.StatusBadRequest, 81053, "An A, AAAA, or CNAME record with that host already exists."}
				}
			}
			rec := map[string]any{"id": st.id("dns"), "name": body["name"], "type": body["type"], "content": body["content"],
				"proxiable": true, "proxied": body["proxied"], "ttl": body["ttl"], "settings": map[string]any{"flatten_cname": false},
				"meta": map[string]any{}, "comment": nil, "tags": []any{}, "created_on": "2026-11-27T00:00:00Z", "modified_on": "2026-11-27T00:00:00Z"}
			s.appendTo(p, rec)
			return rec, nil
		case p == acct("/access/identity_providers"):
			idp := map[string]any{"id": st.id("idp"), "name": body["name"], "type": body["type"], "config": body["config"]}
			s.appendTo(p, idp)
			return idp, nil
		case p == acct("/access/groups"):
			if inc, _ := body["include"].([]any); len(inc) == 0 {
				return nil, &errReply{http.StatusBadRequest, 12130, "include is required"}
			}
			g := map[string]any{"id": st.id("g"), "name": body["name"], "include": body["include"], "exclude": []any{}, "require": []any{},
				"created_at": "2026-11-27T00:00:00Z", "updated_at": "2026-11-27T00:00:00Z"}
			s.appendTo(p, g)
			return g, nil
		case p == acct("/access/service_tokens"):
			id := st.id("st")
			stored := serviceToken(id, fmt.Sprint(body["name"]), "2027-11-27T00:00:00Z")
			if d, ok := body["duration"].(string); ok {
				stored["duration"] = d
				if d == "forever" {
					// A live "forever" token listed an expiry 100 years out.
					stored["expires_at"] = "2126-11-27T00:00:00Z"
				}
			}
			s.appendTo(p, stored)
			// The create response carries client_secret; the list never does.
			resp := map[string]any{}
			for k, v := range stored {
				resp[k] = v
			}
			delete(resp, "expires_at") // absent from the create-response schema
			resp["client_secret"] = st.secret("FAKE-CLIENT-SECRET")
			return resp, nil
		case p == acct("/access/policies"):
			// decision is one of "allow", "deny", "non_identity", "bypass"; include is required.
			switch body["decision"] {
			case "allow", "deny", "non_identity", "bypass":
			default:
				return nil, &errReply{http.StatusBadRequest, 12130, "invalid decision"}
			}
			if inc, _ := body["include"].([]any); len(inc) == 0 {
				return nil, &errReply{http.StatusBadRequest, 12130, "include is required"}
			}
			pol := map[string]any{"id": st.id("pol"), "uid": "u", "name": body["name"], "decision": body["decision"], "include": body["include"],
				"exclude": orEmpty(body["exclude"]), "require": orEmpty(body["require"]), "connection_rules": map[string]any{"rdp": map[string]any{}},
				"reusable": true, "app_count": 0, "created_at": "2026-11-27T00:00:00Z", "updated_at": "2026-11-27T00:00:00Z"}
			s.appendTo(p, pol)
			return pol, nil
		case p == acct("/access/apps"):
			return st.createApp(s, body)
		case p == zone("/rulesets"):
			return st.createRuleset(s, body)
		case strings.HasPrefix(p, zone("/rulesets/")) && strings.HasSuffix(p, "/rules"):
			return st.addRule(s, strings.TrimSuffix(strings.TrimPrefix(p, zone("/rulesets/")), "/rules"), body)
		}
	case http.MethodPut:
		switch {
		case strings.HasPrefix(p, acct("/cfd_tunnel/")) && strings.HasSuffix(p, "/configurations"):
			id := strings.TrimSuffix(strings.TrimPrefix(p, acct("/cfd_tunnel/")), "/configurations")
			if !st.hasTunnel(s, id) {
				return nil, &errReply{http.StatusNotFound, 1003, "tunnel not found"}
			}
			version := 1
			if old, ok := s.routes[p].Result.(map[string]any); ok {
				if v, ok := old["version"].(int); ok {
					version = v + 1
				}
			}
			cfg := map[string]any{"tunnel_id": id, "version": version, "config": body["config"], "source": "cloudflare", "created_at": "2026-11-27T00:00:00Z"}
			s.routes[p] = Response{Result: cfg}
			return cfg, nil
		case p == acct("/access/organizations"), p == zone("/bot_management"):
			cur, _ := s.routes[p].Result.(map[string]any)
			next := map[string]any{}
			for k, v := range cur {
				if st.putReplaces && writableOn(p)[k] {
					continue // replace semantics: an omitted writable field is reset
				}
				next[k] = v
			}
			for k, v := range body {
				next[k] = v
			}
			s.routes[p] = Response{Result: next}
			return next, nil
		}
	}
	return nil, notModelled
}

func orEmpty(v any) any {
	if v == nil {
		return []any{}
	}
	return v
}

func (st *accountState) hasTunnel(s *Server, id string) bool {
	for _, t := range s.list(acct("/cfd_tunnel")) {
		if m, ok := t.(map[string]any); ok && m["id"] == id {
			return true
		}
	}
	return false
}

// createTunnel: the stored tunnel has the list shape the spike saw. The token is served
// by GET …/cfd_tunnel/{id}/token, whose result is a string (reference: "result: string").
func (st *accountState) createTunnel(s *Server, body map[string]any) (any, *errReply) {
	name, _ := body["name"].(string)
	if name == "" {
		return nil, &errReply{http.StatusBadRequest, 1001, "name is required"}
	}
	id := st.uuid()
	t := tunnel(id, name, "inactive")
	t["conns_active_at"], t["conns_inactive_at"], t["created_at"] = nil, nil, "2026-11-27T00:00:00Z"
	if src, ok := body["config_src"].(string); ok {
		t["config_src"] = src
		t["remote_config"] = src == "cloudflare"
	}
	s.appendTo(acct("/cfd_tunnel"), t)
	tok := st.tunnelToken(id)
	s.routes[acct("/cfd_tunnel/"+id+"/token")] = Response{Result: tok}
	resp := map[string]any{}
	for k, v := range t {
		resp[k] = v
	}
	if st.tokenInCreate {
		resp["token"] = tok
	}
	return resp, nil
}

// createApp stores a self-hosted app in the shape the live list returned, with its
// reusable policies embedded and their precedence, and bumps each policy's app_count.
func (st *accountState) createApp(s *Server, body map[string]any) (any, *errReply) {
	// SelfHostedApplication: domain is unmarked (required) and type is "self_hosted".
	if body["type"] != "self_hosted" || orStr(body["domain"]) == "" {
		return nil, &errReply{http.StatusBadRequest, 12130, "type self_hosted and domain are required"}
	}
	id := st.id("app")
	dests, _ := body["destinations"].([]any)
	var uris []any
	for _, d := range dests {
		if m, ok := d.(map[string]any); ok {
			uris = append(uris, m["uri"])
		}
	}
	if len(uris) == 0 {
		return nil, &errReply{http.StatusBadRequest, 12130, "destinations are required"}
	}
	var embedded []any
	links, _ := body["policies"].([]any)
	pols := s.list(acct("/access/policies"))
	for _, l := range links {
		link, _ := l.(map[string]any)
		if _, ok := link["precedence"].(float64); !ok {
			return nil, &errReply{http.StatusBadRequest, 12130, "each policy link needs a precedence"}
		}
		var found map[string]any
		for _, p := range pols {
			if m, ok := p.(map[string]any); ok && m["id"] == link["id"] {
				found = m
			}
		}
		if found == nil {
			return nil, &errReply{http.StatusBadRequest, 12130, fmt.Sprintf("policy %v not found", link["id"])}
		}
		found["app_count"] = toInt(found["app_count"]) + 1
		e := map[string]any{}
		for k, v := range found {
			if k != "app_count" {
				e[k] = v
			}
		}
		e["precedence"] = link["precedence"]
		embedded = append(embedded, e)
	}
	a := map[string]any{"id": id, "uid": id, "type": body["type"], "name": body["name"], "aud": "aud-" + id,
		"domain": body["domain"], "self_hosted_domains": uris, "destinations": dests,
		"app_launcher_visible": true, "allowed_idps": orEmpty(body["allowed_idps"]), "tags": []any{},
		"auto_redirect_to_identity": orFalse(body["auto_redirect_to_identity"]), "policies": orEmpty(embeddedOrNil(embedded)),
		"session_duration": orDefault(fmt.Sprint(orStr(body["session_duration"])), "24h"), "enable_binding_cookie": false,
		"http_only_cookie_attribute": false, "options_preflight_bypass": false, "eager_redirect_cookie_setting": true,
		"created_at": "2026-11-27T00:00:00Z", "updated_at": "2026-11-27T00:00:00Z"}
	// Like the live list, service_auth_401_redirect is left out when it is not set.
	if v, ok := body["service_auth_401_redirect"].(bool); ok && v {
		a["service_auth_401_redirect"] = true
	}
	s.appendTo(acct("/access/apps"), a)
	return a, nil
}

func embeddedOrNil(e []any) any {
	if e == nil {
		return nil
	}
	return e
}

func orFalse(v any) any {
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

func orStr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	}
	return 0
}

func entrypoint(phase string) string { return zone("/rulesets/phases/" + phase + "/entrypoint") }

// createRuleset creates a phase's entry point ruleset. A second entry point for a phase
// is refused: the guide says to create one only after a 404.
func (st *accountState) createRuleset(s *Server, body map[string]any) (any, *errReply) {
	phase, _ := body["phase"].(string)
	if body["kind"] != "zone" || phase == "" {
		return nil, &errReply{http.StatusBadRequest, 20021, "kind zone and a phase are required"}
	}
	if r, ok := s.routes[entrypoint(phase)]; ok && r.Status == 0 {
		return nil, &errReply{http.StatusBadRequest, 20217, "a zone entry point ruleset already exists for " + phase}
	}
	rules, _ := body["rules"].([]any)
	if lim, ok := freeLimit[phase]; ok && len(rules) > lim {
		return nil, &errReply{http.StatusBadRequest, 20200, "rule limit reached for " + phase}
	}
	rs := map[string]any{"id": st.id("rs"), "name": body["name"], "kind": "zone", "phase": phase, "version": "1",
		"last_updated": "2026-11-27T00:00:00Z", "description": orStr(body["description"]), "source": "firewall_custom"}
	var stored []any
	for _, r := range rules {
		stored = append(stored, st.storeRule(r))
	}
	rs["rules"] = stored
	s.routes[entrypoint(phase)] = Response{Result: rs}
	return rs, nil
}

func (st *accountState) storeRule(r any) map[string]any {
	in, _ := r.(map[string]any)
	out := map[string]any{"id": st.id("r"), "version": "1", "last_updated": "2026-11-27T00:00:00Z", "ref": ""}
	for k, v := range in {
		if k != "position" {
			out[k] = v
		}
	}
	out["ref"] = out["id"]
	return out
}

// addRule adds one rule to an existing ruleset, at the end by default, or where
// position says ({"index": n}, where 1 is the first; {"before": ""} is the top). The
// response is the whole ruleset.
func (st *accountState) addRule(s *Server, id string, body map[string]any) (any, *errReply) {
	for path, route := range s.routes {
		rs, ok := route.Result.(map[string]any)
		if !ok || rs["id"] != id || !strings.HasSuffix(path, "/entrypoint") {
			continue
		}
		rules, _ := rs["rules"].([]any)
		if lim, ok := freeLimit[fmt.Sprint(rs["phase"])]; ok && len(rules)+1 > lim {
			return nil, &errReply{http.StatusBadRequest, 20200, "rule limit reached"}
		}
		rule := st.storeRule(body)
		at := len(rules)
		if pos, ok := body["position"].(map[string]any); ok {
			if n, ok := pos["index"].(float64); ok && n >= 1 {
				at = min(int(n)-1, len(rules))
			}
			if b, ok := pos["before"].(string); ok && b == "" {
				at = 0
			}
		}
		next := append(append(append([]any{}, rules[:at]...), rule), rules[at:]...)
		cp := map[string]any{}
		for k, v := range rs {
			cp[k] = v
		}
		cp["rules"] = next
		s.routes[path] = Response{Result: cp}
		return cp, nil
	}
	return nil, &errReply{http.StatusNotFound, 10003, "ruleset not found"}
}

// freeLimit are the Free-plan rule slots per phase (every fixture zone is Free).
var freeLimit = map[string]int{"http_request_firewall_custom": 5, "http_request_cache_settings": 10, "http_config_settings": 10}

// writableOn are the documented writable fields of the organization and of Bot
// Management (BotFightModeConfiguration), from their update reference pages. Under
// PutReplaces these are reset when a PUT omits them; read-only fields are kept.
func writableOn(path string) map[string]bool {
	fields := []string{"allow_authenticate_via_warp", "auth_domain", "auto_redirect_to_identity", "custom_pages",
		"deny_unmatched_requests", "deny_unmatched_requests_exempted_zone_names", "disable_end_user_app_creation",
		"is_ui_read_only", "login_design", "mfa_config", "mfa_piv_key_requirements", "mfa_required_for_all_apps", "name",
		"service_token_inactivity", "session_duration", "strict_service_token_auth", "ui_read_only_toggle_reason",
		"user_seat_expiration_inactive_time", "warp_auth_non_browser_401", "warp_auth_session_duration"}
	if path == zone("/bot_management") {
		fields = []string{"ai_agents", "ai_bots_migration_opt_out", "ai_bots_protection", "ai_search", "ai_training",
			"ai_user", "bot_preference_sync_enabled", "cf_robots_variant", "content_bots_protection", "crawler_protection",
			"enable_js", "fight_mode", "is_robots_txt_managed", "jsd_api_results_enabled"}
	}
	m := map[string]bool{}
	for _, f := range fields {
		m[f] = true
	}
	return m
}

// tunnelToken is a tunnel token in the shape cloudflared takes and setup decodes for the
// tunnel ID: base64 of {"a": account tag, "t": tunnel ID, "s": secret}. It is built at
// run time, and both it and its secret part are recorded in Secrets.
func (st *accountState) tunnelToken(id string) string {
	s := st.secret("FAKE-TUNNEL-SECRET")
	raw, _ := json.Marshal(map[string]string{"a": AccountID, "t": id, "s": s})
	tok := base64.StdEncoding.EncodeToString(raw)
	st.mu.Lock()
	defer st.mu.Unlock()
	st.secrets = append(st.secrets, tok)
	return tok
}
