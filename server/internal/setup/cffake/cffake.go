// Package cffake is a small fake of the Cloudflare v4 API for setup tests. It serves
// fixed results by path, pages list results by page/per_page, answers unknown paths
// with a Cloudflare-shaped 404, and records every request so a test can assert that
// only GET was ever sent. It is not stateful: a fuller fake is planned separately.
//
// The fixtures are written out literally from docs/deploy/cloudflare.md and
// docs/deploy/fresh-install.md, not built from plan.Desired, so tests compare the
// code against an independent copy of the docs.
package cffake

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
)

// Response is what one path returns. Result is encoded as the envelope's "result";
// a slice is paged. A non-zero Status with Code makes the request fail.
type Response struct {
	Status int
	Code   int
	Result any
}

// Request is one request the fake received.
type Request struct{ Method, Path, Query string }

type Server struct {
	*httptest.Server
	mu       sync.Mutex
	routes   map[string]Response
	requests []Request
}

// Start serves routes, keyed by URL path (no query), until Close.
func Start(routes map[string]Response) *Server {
	s := &Server{routes: maps.Clone(routes)}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

// Requests returns every request received so far.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.requests...)
}

// NonGET returns every request whose method was not GET.
func (s *Server) NonGET() []Request {
	var out []Request
	for _, r := range s.Requests() {
		if r.Method != http.MethodGet {
			out = append(out, r)
		}
	}
	return out
}

// Set replaces one route.
func (s *Server) Set(path string, r Response) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.routes[path] = r
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests = append(s.requests, Request{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery})
	route, ok := s.routes[r.URL.Path]
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	fail := func(status, code int, msg string) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "errors": []any{map[string]any{"code": code, "message": msg}}, "messages": []any{}, "result": nil})
	}
	switch {
	case r.Method != http.MethodGet:
		fail(http.StatusMethodNotAllowed, 10000, "the fake Cloudflare API is read-only")
		return
	case !ok:
		fail(http.StatusNotFound, 7003, "Could not route to "+r.URL.Path)
		return
	case route.Status != 0:
		fail(route.Status, route.Code, fmt.Sprintf("HTTP %d from the fake", route.Status))
		return
	}
	body := map[string]any{"success": true, "errors": []any{}, "messages": []any{}, "result": route.Result}
	if list, ok := route.Result.([]any); ok {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		per, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
		if page < 1 {
			page = 1
		}
		if per < 1 {
			per = 20
		}
		lo, hi := min((page-1)*per, len(list)), min(page*per, len(list))
		body["result"] = list[lo:hi]
		body["result_info"] = map[string]any{"page": page, "per_page": per, "count": hi - lo, "total_count": len(list), "total_pages": (len(list) + per - 1) / per}
	}
	_ = json.NewEncoder(w).Encode(body)
}

// Fixture IDs.
const (
	Host      = "officers.example.org"
	Backup    = "backups.example.org"
	AccountID = "acct-1"
	ZoneID    = "zone-1"
	TunnelID  = "6ff42ae2-765d-4adf-8112-31c55c1551ef"
)

func acct(p string) string { return "/accounts/" + AccountID + p }
func zone(p string) string { return "/zones/" + ZoneID + p }

// Fresh is an account where only the manual steps are done: example.org is an active
// zone and Zero Trust is open with its default 24h session. Nothing else exists, and a
// few unrelated resources are present to make sure they are not matched.
func Fresh() map[string]Response {
	return map[string]Response{
		"/zones": {Result: []any{
			map[string]any{"id": "zone-other", "name": "other.org", "status": "active", "account": map[string]any{"id": AccountID}},
			map[string]any{"id": ZoneID, "name": "example.org", "status": "active", "account": map[string]any{"id": AccountID, "name": "Club"}},
		}},
		acct("/access/organizations"):      {Result: map[string]any{"auth_domain": "club.cloudflareaccess.com", "name": "club", "session_duration": "24h"}},
		acct("/cfd_tunnel"):                {Result: []any{map[string]any{"id": "tun-other", "name": "homelab", "config_src": "cloudflare", "deleted_at": nil}}},
		zone("/dns_records"):               {Result: []any{}},
		acct("/access/identity_providers"): {Result: []any{}},
		acct("/access/groups"):             {Result: []any{map[string]any{"id": "g-x", "name": "Officers (old club)"}}},
		acct("/access/service_tokens"):     {Result: []any{}},
		acct("/access/apps"):               {Result: []any{map[string]any{"id": "app-x", "name": "Wiki", "type": "self_hosted", "domain": "wiki.example.org", "destinations": []any{map[string]any{"type": "public", "uri": "wiki.example.org"}}}}},
		zone("/bot_management"):            {Result: map[string]any{"fight_mode": false, "enable_js": false}},
		acct("/d1/database"):               {Result: []any{map[string]any{"uuid": "d1-members", "name": "membership"}}},
		acct("/workers/scripts"):           {Result: []any{map[string]any{"id": "something-else"}}},
		acct("/workers/domains"):           {Result: []any{}},
		zone("/rulesets/phases/http_request_cache_settings/entrypoint"): {Result: map[string]any{"id": "rs-cache", "phase": "http_request_cache_settings", "rules": []any{
			map[string]any{"id": "r-img", "description": "Cache images", "expression": `(http.request.uri.path.extension eq "png")`, "action": "set_cache_settings", "action_parameters": map[string]any{"cache": true}, "enabled": true},
		}}},
		// http_request_firewall_custom and http_config_settings have no entry point: 404.
	}
}

// agentExpr is the expression in docs/deploy/cloudflare.md step 6.2, for Host.
const agentExpr = `(http.host eq "officers.example.org" and (http.request.uri.path eq "/mcp" or starts_with(http.request.uri.path, "/mcp/") or http.request.uri.path in {"/oauth/register" "/oauth/token" "/.well-known/oauth-authorization-server"} or starts_with(http.request.uri.path, "/.well-known/oauth-protected-resource") or starts_with(http.request.uri.path, "/api/ingest/") or starts_with(http.request.uri.path, "/api/agent/")))`

// Complete is an account set up exactly as the docs describe, by hand in the dashboard.
func Complete() map[string]Response {
	r := Fresh()
	r[acct("/access/organizations")] = Response{Result: map[string]any{"auth_domain": "club.cloudflareaccess.com", "name": "club", "session_duration": "730h"}}
	r[acct("/cfd_tunnel")] = Response{Result: []any{
		map[string]any{"id": "tun-other", "name": "homelab", "config_src": "cloudflare", "deleted_at": nil},
		map[string]any{"id": TunnelID, "name": "officer-dashboard", "config_src": "cloudflare", "deleted_at": nil, "status": "healthy"},
	}}
	r[acct("/cfd_tunnel/"+TunnelID+"/configurations")] = Response{Result: map[string]any{"tunnel_id": TunnelID, "source": "cloudflare", "version": 3, "config": map[string]any{"ingress": []any{
		map[string]any{"hostname": Host, "service": "http://dashboard:8090"},
		map[string]any{"service": "http_status:404"},
	}}}}
	r[zone("/dns_records")] = Response{Result: []any{map[string]any{"id": "dns-1", "name": Host, "type": "CNAME", "content": TunnelID + ".cfargotunnel.com", "proxied": true}}}
	r[acct("/access/identity_providers")] = Response{Result: []any{map[string]any{"id": "idp-otp", "name": "One-time PIN", "type": "onetimepin", "config": map[string]any{}}}}
	r[acct("/access/groups")] = Response{Result: []any{
		map[string]any{"id": "g-x", "name": "Officers (old club)"},
		map[string]any{"id": "g-officers", "name": "Officers", "include": []any{map[string]any{"email": map[string]any{"email": "a@example.org"}}}},
		map[string]any{"id": "g-launchpad", "name": "Launchpad"},
	}}
	r[acct("/access/service_tokens")] = Response{Result: []any{
		map[string]any{"id": "st-services", "name": "officer-dashboard services", "client_id": "abc.access", "expires_at": "2027-11-20T00:00:00Z"},
		map[string]any{"id": "st-backups", "name": "dashboard-backups-host", "client_id": "def.access", "expires_at": "2027-11-21T00:00:00Z"},
	}}
	pub := func(uris ...string) []any {
		out := make([]any, len(uris))
		for i, u := range uris {
			out[i] = map[string]any{"type": "public", "uri": u}
		}
		return out
	}
	r[acct("/access/apps")] = Response{Result: []any{
		map[string]any{"id": "app-x", "name": "Wiki", "type": "self_hosted", "domain": "wiki.example.org", "destinations": pub("wiki.example.org")},
		map[string]any{"id": "app-site", "name": "Officer Dashboard", "type": "self_hosted", "domain": Host, "destinations": pub(Host),
			"session_duration": "730h", "allowed_idps": []any{"idp-otp"}, "auto_redirect_to_identity": true},
		map[string]any{"id": "app-agents", "name": "Officer Dashboard: agents (bypass)", "type": "self_hosted", "domain": Host + "/mcp",
			"destinations": pub(Host+"/mcp", Host+"/oauth/register", Host+"/oauth/token", Host+"/.well-known/oauth-protected-resource",
				Host+"/.well-known/oauth-protected-resource/mcp", Host+"/.well-known/oauth-authorization-server")},
		map[string]any{"id": "app-services", "name": "Officer Dashboard: services (service token)", "type": "self_hosted", "domain": Host + "/api/ingest",
			"destinations": pub(Host+"/api/ingest", Host+"/api/agent"), "service_auth_401_redirect": true},
		map[string]any{"id": "app-backups", "name": "Backups", "type": "self_hosted", "domain": Backup, "destinations": pub(Backup)},
	}}
	r[acct("/access/apps/app-site/policies")] = Response{Result: []any{map[string]any{"id": "pol-site", "name": "Officers and Launchpad", "decision": "allow", "precedence": 1,
		"include": []any{map[string]any{"group": map[string]any{"id": "g-officers"}}, map[string]any{"group": map[string]any{"id": "g-launchpad"}}}}}}
	r[acct("/access/apps/app-agents/policies")] = Response{Result: []any{map[string]any{"id": "pol-agents", "name": "Bypass", "decision": "bypass", "precedence": 1,
		"include": []any{map[string]any{"everyone": map[string]any{}}}}}}
	r[acct("/access/apps/app-services/policies")] = Response{Result: []any{map[string]any{"id": "pol-services", "name": "Service token", "decision": "non_identity", "precedence": 1,
		"include": []any{map[string]any{"service_token": map[string]any{"token_id": "st-services"}}}}}}
	r[acct("/access/apps/app-backups/policies")] = Response{Result: []any{map[string]any{"id": "pol-backups", "name": "Backup host", "decision": "non_identity", "precedence": 1,
		"include": []any{map[string]any{"service_token": map[string]any{"token_id": "st-backups"}}}}}}
	r[zone("/rulesets/phases/http_request_firewall_custom/entrypoint")] = Response{Result: map[string]any{"id": "rs-fw", "phase": "http_request_firewall_custom", "rules": []any{
		map[string]any{"id": "r-skip", "description": "Agents and services: skip bot checks", "expression": agentExpr, "action": "skip",
			"action_parameters": map[string]any{"products": []any{"bic", "securityLevel"}}, "enabled": true, "logging": map[string]any{"enabled": true}},
		map[string]any{"id": "r-block", "description": "Block bad countries", "expression": `(ip.src.country eq "XX")`, "action": "block", "enabled": true},
	}}}
	r[zone("/rulesets/phases/http_request_cache_settings/entrypoint")] = Response{Result: map[string]any{"id": "rs-cache", "phase": "http_request_cache_settings", "rules": []any{
		map[string]any{"id": "r-img", "description": "Cache images", "expression": `(http.request.uri.path.extension eq "png")`, "action": "set_cache_settings", "action_parameters": map[string]any{"cache": true}, "enabled": true},
		map[string]any{"id": "r-cache", "description": "Agents and services: bypass cache", "expression": agentExpr, "action": "set_cache_settings", "action_parameters": map[string]any{"cache": false}, "enabled": true},
	}}}
	r[zone("/rulesets/phases/http_config_settings/entrypoint")] = Response{Result: map[string]any{"id": "rs-config", "phase": "http_config_settings", "rules": []any{
		map[string]any{"id": "r-config", "description": "Consent page: no rewriting", "expression": `(http.host eq "officers.example.org" and http.request.uri.path eq "/oauth/authorize")`,
			"action": "set_config", "action_parameters": map[string]any{"rocket_loader": false, "email_obfuscation": false}, "enabled": true},
	}}}
	r[acct("/d1/database")] = Response{Result: []any{
		map[string]any{"uuid": "d1-members", "name": "membership"},
		map[string]any{"uuid": "d1-backups", "name": "dashboard-backups"},
	}}
	r[acct("/workers/scripts")] = Response{Result: []any{map[string]any{"id": "something-else"}, map[string]any{"id": "dashboard-backup-sink"}}}
	r[acct("/workers/scripts/dashboard-backup-sink/settings")] = Response{Result: map[string]any{"bindings": []any{
		map[string]any{"type": "d1", "name": "BACKUPS", "database_id": "d1-backups"},
		map[string]any{"type": "secret_text", "name": "BACKUP_BEARER_SECRET"},
	}}}
	r[acct("/workers/scripts/dashboard-backup-sink/secrets")] = Response{Result: []any{map[string]any{"name": "BACKUP_BEARER_SECRET", "type": "secret_text"}}}
	r[acct("/workers/scripts/dashboard-backup-sink/subdomain")] = Response{Result: map[string]any{"enabled": false, "previews_enabled": false}}
	r[acct("/workers/domains")] = Response{Result: []any{map[string]any{"id": "wd-1", "hostname": Backup, "service": "dashboard-backup-sink", "zone_id": ZoneID, "environment": "production"}}}
	return r
}
