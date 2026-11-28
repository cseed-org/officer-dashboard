// Package cffake is a small fake of the Cloudflare v4 API for setup tests. It serves
// fixed results by path, pages list results by page/per_page, answers unknown paths
// with a Cloudflare-shaped 404, and records every request so a test can assert that
// only GET was ever sent.
//
// Start gives a read-only fake that refuses every write. NewAccount (account.go) gives
// a stateful one that accepts the writes setup's apply engine sends, stores them and
// serves them back to discovery, with failure injection.
//
// The fixtures' names, paths and expressions are written out literally from
// docs/deploy/cloudflare.md and docs/deploy/fresh-install.md, not built from
// plan.Desired, so tests compare the code against an independent copy of the docs.
// Their response shapes follow a live read-only run (spike stage 2, 2026-11-27).
package cffake

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"sync"
)

// Response is what one path returns. Result is encoded as the envelope's "result";
// a slice is paged. A non-zero Status with Code makes the request fail.
type Response struct {
	Status  int
	Code    int
	Message string
	Result  any
}

// Request is one request the fake received. Body is the request body as sent;
// ContentType is its Content-Type header (multipart for a Worker upload), and APIDate its
// Cloudflare-Workers-Script-Api-Date header.
type Request struct{ Method, Path, Query, Body, ContentType, APIDate string }

type Server struct {
	*httptest.Server
	mu       sync.Mutex
	routes   map[string]Response
	requests []Request
	acct     *accountState // nil for the read-only fake
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
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	req := Request{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Body: string(raw), ContentType: r.Header.Get("Content-Type"),
		APIDate: r.Header.Get("Cloudflare-Workers-Script-Api-Date")}
	if s.acct != nil && s.acct.refuses(req) {
		hangUp(w) // the process died before this request reached Cloudflare
		return
	}
	s.mu.Lock()
	s.requests = append(s.requests, req)
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	fail := func(status, code int, msg string) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "errors": []any{map[string]any{"code": code, "message": msg}}, "messages": []any{}, "result": nil})
	}
	if s.acct != nil && s.acct.intercept(s, req, w, fail) {
		return
	}
	s.mu.Lock()
	route, ok := s.routes[r.URL.Path]
	s.mu.Unlock()
	switch {
	case r.Method != http.MethodGet:
		fail(http.StatusMethodNotAllowed, 10000, "the fake Cloudflare API is read-only")
		return
	case !ok && workerSubresource.MatchString(r.URL.Path):
		// A script's settings, secrets or subdomain when the script does not exist: the
		// "Worker not found" error Wrangler checks (WORKER_NOT_FOUND_ERR_CODE = 10007).
		fail(http.StatusNotFound, 10007, "This Worker does not exist on your account.")
		return
	case !ok:
		fail(http.StatusNotFound, 7003, "Could not route to "+r.URL.Path)
		return
	case route.Status != 0:
		msg := route.Message
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d from the fake", route.Status)
		}
		fail(route.Status, route.Code, msg)
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

var workerSubresource = regexp.MustCompile(`^/accounts/[^/]+/workers/scripts/[^/]+/(settings|secrets|subdomain)$`)

// Fixture IDs.
const (
	Host      = "officers.example.org"
	Backup    = "backups.example.org"
	AccountID = "acct-1"
	ZoneID    = "zone-1"
	TunnelID  = "6ff42ae2-765d-4adf-8112-31c55c1551ef"
)

func acct(p string) string { return "/accounts/" + AccountID + p }

// D1TablesKey is the fake-only route that holds a D1 database's table names (a list of
// strings), which the stateful fake's D1 query endpoint serves and its writes change. It
// is no URL path, so no request can reach it.
func D1TablesKey(uuid string) string { return "fake:d1/" + uuid + "/tables" }

// D1TriggersKey and D1MigrationsKey hold a database's trigger names and the names in its
// d1_migrations table, the same way.
func D1TriggersKey(uuid string) string   { return "fake:d1/" + uuid + "/triggers" }
func D1MigrationsKey(uuid string) string { return "fake:d1/" + uuid + "/migrations" }

// D1SQLKey holds a database's CREATE statements by table or trigger name (a
// map[string]any), as sqlite_master's sql column shows them.
func D1SQLKey(uuid string) string { return "fake:d1/" + uuid + "/sql" }

// MigratedBackupSQL is what sqlite_master shows for the backup tables and trigger after
// `wrangler d1 migrations apply`: the statements of server/deploy/backup-sink/migrations/0001.sql,
// written out here, not read from the file.
func MigratedBackupSQL() map[string]any {
	return map[string]any{
		"snapshots": `CREATE TABLE snapshots (
 id TEXT PRIMARY KEY, kind TEXT NOT NULL CHECK(kind IN ('hourly','daily','d1-weekly')),
 created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
 sha256 TEXT NOT NULL, size INTEGER NOT NULL CHECK(size BETWEEN 1 AND 24700000),
 chunks INTEGER NOT NULL CHECK(chunks BETWEEN 1 AND 13), committed INTEGER NOT NULL DEFAULT 0,
 allocation INTEGER NOT NULL, write_cost INTEGER NOT NULL
)`,
		"chunks": `CREATE TABLE chunks (
 snapshot_id TEXT NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
 n INTEGER NOT NULL, blob BLOB NOT NULL CHECK(length(blob) BETWEEN 1 AND 1900000),
 PRIMARY KEY(snapshot_id,n)
)`,
		"budgets": `CREATE TABLE budgets (day TEXT PRIMARY KEY, writes INTEGER NOT NULL DEFAULT 0, uploads INTEGER NOT NULL DEFAULT 0)`,
		"reserve": `CREATE TRIGGER reserve BEFORE INSERT ON snapshots BEGIN
 SELECT CASE WHEN (SELECT count(*) FROM snapshots) >= 100
  OR COALESCE((SELECT sum(allocation) FROM snapshots),0) + NEW.allocation > 450000000
  OR COALESCE((SELECT writes FROM budgets WHERE day=date('now')),0) + NEW.write_cost > 40000
  OR COALESCE((SELECT uploads FROM budgets WHERE day=date('now')),0) >= 200
  THEN RAISE(ABORT,'backup budget exhausted') END;
 INSERT INTO budgets(day,writes,uploads) VALUES(date('now'),NEW.write_cost,1)
 ON CONFLICT(day) DO UPDATE SET writes=writes+NEW.write_cost,uploads=uploads+1;
END`,
		"d1_migrations":   `CREATE TABLE d1_migrations( id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT UNIQUE, applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP NOT NULL )`,
		"sqlite_sequence": `CREATE TABLE sqlite_sequence(name,seq)`,
		"_cf_KV":          `CREATE TABLE _cf_KV ( key TEXT PRIMARY KEY, value BLOB ) WITHOUT ROWID`,
	}
}
func zone(p string) string { return "/zones/" + ZoneID + p }

// Response shapes follow what a live, read-only run returned on 2026-11-27 (spike stage
// 2): keys a live response carried are included, so decoding is tested against them.

func noEntrypoint(phase string) Response {
	return Response{Status: 404, Code: 10003, Message: "could not find entrypoint ruleset in the " + phase + " phase", Result: map[string]any{}}
}

func pub(uris ...string) []any {
	out := make([]any, len(uris))
	for i, u := range uris {
		out[i] = map[string]any{"type": "public", "uri": u}
	}
	return out
}

// app is a self-hosted Access application as the live list returned it: destinations
// plus the deprecated self_hosted_domains, and its policies embedded.
func app(id, name string, extra map[string]any, policies []any, uris ...string) map[string]any {
	a := map[string]any{"id": id, "uid": id, "type": "self_hosted", "name": name, "aud": "aud-" + id,
		"domain": uris[0], "self_hosted_domains": append([]any(nil), toAny(uris)...), "destinations": pub(uris...),
		"app_launcher_visible": true, "allowed_idps": []any{}, "tags": []any{}, "auto_redirect_to_identity": false,
		"policies": policies, "session_duration": "24h", "enable_binding_cookie": false, "http_only_cookie_attribute": false,
		"options_preflight_bypass": false, "eager_redirect_cookie_setting": true}
	for k, v := range extra {
		a[k] = v
	}
	return a
}

func toAny(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

func policy(id, name, decision string, include ...any) map[string]any {
	return map[string]any{"id": id, "uid": id, "name": name, "decision": decision, "include": include, "exclude": []any{}, "require": []any{},
		"connection_rules": map[string]any{"rdp": map[string]any{}}, "reusable": true, "precedence": 1}
}

func group(id string) any { return map[string]any{"group": map[string]any{"id": id}} }

// withCount is a reusable policy as the policies list returns it, with app_count.
func withCount(p map[string]any, n int) map[string]any {
	p["app_count"] = n
	delete(p, "precedence")
	return p
}
func token(id string) any { return map[string]any{"service_token": map[string]any{"token_id": id}} }

func serviceToken(id, name, expires string) map[string]any {
	return map[string]any{"id": id, "name": name, "client_id": id + ".access", "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
		"expires_at": expires, "duration": "8760h", "client_secret_version": 1, "enabled": true, "tags": map[string]any{}}
}

func tunnel(id, name, status string) map[string]any {
	return map[string]any{"id": id, "account_tag": AccountID, "name": name, "deleted_at": nil, "connections": []any{}, "tun_type": "cfd_tunnel",
		"metadata": map[string]any{}, "status": status, "remote_config": true, "config_src": "cloudflare"}
}

// Fresh is an account where only the manual steps are done: example.org is an active
// zone and Zero Trust is open without a global session duration. Nothing else exists;
// a few unrelated resources are present to make sure they are not matched.
func Fresh() map[string]Response {
	return map[string]Response{
		"/zones": {Result: []any{
			map[string]any{"id": "zone-other", "name": "other.org", "status": "active", "account": map[string]any{"id": AccountID}},
			map[string]any{"id": ZoneID, "name": "example.org", "status": "active", "paused": false, "type": "full",
				"account": map[string]any{"id": AccountID, "name": "Club"}, "permissions": []any{"#zone:read"}, "plan": map[string]any{"legacy_id": "free"}},
		}},
		acct("/tokens/verify"):        {Result: map[string]any{"id": "tok-1", "status": "active", "expires_on": "2026-12-04T00:00:00Z"}},
		"/user/tokens/verify":         {Status: 401, Code: 1000, Message: "Invalid API Token"},
		acct("/access/organizations"): {Result: map[string]any{"auth_domain": "club.cloudflareaccess.com", "name": "club", "is_ui_read_only": false, "strict_service_token_auth": true}},
		acct("/cfd_tunnel"):           {Result: []any{tunnel("tun-other", "homelab", "healthy")}},
		zone("/dns_records"):          {Result: []any{}},
		acct("/access/identity_providers"): {Result: []any{map[string]any{"id": "idp-cf", "type": "cloudflare", "name": "Cloudflare",
			"config": map[string]any{"restrict_to_account_members": true}}}},
		acct("/access/groups"):         {Result: []any{map[string]any{"id": "g-x", "name": "Officers (old club)"}}},
		acct("/access/service_tokens"): {Result: []any{}},
		acct("/access/apps"):           {Result: []any{app("app-x", "Wiki", nil, []any{}, "wiki.example.org")}},
		acct("/access/policies"):       {Result: []any{}},
		zone("/bot_management"):        {Result: map[string]any{"enable_js": false, "fight_mode": false, "ai_bots_protection": "disabled", "using_latest_model": true}},
		acct("/d1/database"):           {Result: []any{map[string]any{"uuid": "d1-members", "name": "membership", "version": "production", "num_tables": 3}}},
		acct("/workers/scripts"):       {Result: []any{map[string]any{"id": "something-else", "has_modules": true}}},
		acct("/workers/domains"):       {Result: []any{}},
		zone("/rulesets/phases/http_request_firewall_custom/entrypoint"): noEntrypoint("http_request_firewall_custom"),
		zone("/rulesets/phases/http_request_cache_settings/entrypoint"): {Result: map[string]any{"id": "rs-cache", "phase": "http_request_cache_settings", "rules": []any{
			map[string]any{"id": "r-img", "description": "Cache images", "expression": `(http.request.uri.path.extension eq "png")`, "action": "set_cache_settings", "action_parameters": map[string]any{"cache": true}, "enabled": true},
		}}},
		zone("/rulesets/phases/http_config_settings/entrypoint"): noEntrypoint("http_config_settings"),
		zone("/rulesets/phases/http_ratelimit/entrypoint"):       noEntrypoint("http_ratelimit"),
	}
}

// agentExpr is the expression in docs/deploy/cloudflare.md step 6.2, for Host.
const agentExpr = `(http.host eq "officers.example.org" and (http.request.uri.path eq "/mcp" or starts_with(http.request.uri.path, "/mcp/") or http.request.uri.path in {"/oauth/register" "/oauth/token" "/.well-known/oauth-authorization-server"} or starts_with(http.request.uri.path, "/.well-known/oauth-protected-resource") or starts_with(http.request.uri.path, "/api/ingest/") or starts_with(http.request.uri.path, "/api/agent/")))`

// tunnelAndBackups adds what both Live and Complete have: the tunnel, its route and
// CNAME, the backup D1 database and Worker, and the backup service token.
func tunnelAndBackups(r map[string]Response) {
	r[acct("/cfd_tunnel")] = Response{Result: []any{tunnel("tun-other", "homelab", "healthy"), tunnel(TunnelID, "officer-dashboard", "healthy")}}
	r[acct("/cfd_tunnel/"+TunnelID+"/configurations")] = Response{Result: map[string]any{"tunnel_id": TunnelID, "source": "cloudflare", "version": 1, "config": map[string]any{
		"ingress":      []any{map[string]any{"service": "http://dashboard:8090", "hostname": Host}, map[string]any{"service": "http_status:404"}},
		"warp-routing": map[string]any{"enabled": false},
	}}}
	r[zone("/dns_records")] = Response{Result: []any{map[string]any{"id": "dns-1", "name": Host, "type": "CNAME", "content": TunnelID + ".cfargotunnel.com",
		"proxiable": true, "proxied": true, "ttl": 1, "settings": map[string]any{"flatten_cname": false}, "meta": map[string]any{}, "comment": nil, "tags": []any{}}}}
	r[acct("/d1/database")] = Response{Result: []any{
		map[string]any{"uuid": "d1-members", "name": "membership", "version": "production", "num_tables": 3},
		map[string]any{"uuid": "d1-backups", "name": "dashboard-backups", "version": "production", "jurisdiction": nil, "num_tables": 2, "file_size": 49152},
	}}
	// What `wrangler d1 migrations apply` leaves (fresh-install.md §4.5): the three backup
	// tables, Wrangler's d1_migrations with its AUTOINCREMENT sequence, and D1's own table.
	r[D1TablesKey("d1-backups")] = Response{Result: []any{"_cf_KV", "budgets", "chunks", "d1_migrations", "snapshots", "sqlite_sequence"}}
	r[D1TriggersKey("d1-backups")] = Response{Result: []any{"reserve"}}
	r[D1MigrationsKey("d1-backups")] = Response{Result: []any{"0001.sql"}}
	r[D1SQLKey("d1-backups")] = Response{Result: MigratedBackupSQL()}
	r[D1TablesKey("d1-members")] = Response{Result: []any{"_cf_KV", "members", "payments", "terms"}}
	r[acct("/workers/scripts")] = Response{Result: []any{map[string]any{"id": "something-else"}, map[string]any{"id": "dashboard-backup-sink", "has_modules": true, "usage_model": "standard",
		"compatibility_date": "2026-07-30", "compatibility_flags": []any{"nodejs_compat"}}}}
	r[acct("/workers/scripts/dashboard-backup-sink/settings")] = Response{Result: map[string]any{"bindings": []any{
		map[string]any{"type": "d1", "name": "BACKUPS", "database_id": "d1-backups"},
		map[string]any{"type": "secret_text", "name": "BACKUP_BEARER_SECRET"},
	}}}
	r[acct("/workers/scripts/dashboard-backup-sink/secrets")] = Response{Result: []any{map[string]any{"name": "BACKUP_BEARER_SECRET", "type": "secret_text"}}}
	r[acct("/workers/scripts/dashboard-backup-sink/subdomain")] = Response{Result: map[string]any{"enabled": false, "previews_enabled": false}}
	r[acct("/workers/domains")] = Response{Result: []any{map[string]any{"id": "wd-1", "zone_id": ZoneID, "zone_name": "example.org", "hostname": Backup,
		"service": "dashboard-backup-sink", "environment": "production", "cert_id": "c-1", "previews_enabled": false, "enabled": true}}}
}

// Mismatched is Live with backup pieces of another format: the budgets table has another
// shape, and the Worker binds BACKUPS to another database on an older compatibility date.
func Mismatched() map[string]Response {
	r := Live()
	defs := MigratedBackupSQL()
	defs["budgets"] = "CREATE TABLE budgets (day TEXT PRIMARY KEY, writes INTEGER)"
	r[D1SQLKey("d1-backups")] = Response{Result: defs}
	r[acct("/workers/scripts")] = Response{Result: []any{map[string]any{"id": "dashboard-backup-sink", "has_modules": true, "usage_model": "standard",
		"compatibility_date": "2025-01-01", "compatibility_flags": []any{"nodejs_compat"}}}}
	r[acct("/workers/scripts/dashboard-backup-sink/settings")] = Response{Result: map[string]any{"bindings": []any{
		map[string]any{"type": "d1", "name": "BACKUPS", "database_id": "d1-old"},
		map[string]any{"type": "secret_text", "name": "BACKUP_BEARER_SECRET"},
	}}}
	return r
}

// Live is the partial setup a live account had during spike stage 2: the tunnel (no
// connector running), its route and CNAME, and everything for backups exist; the
// one-time PIN provider, the groups, the three dashboard Access applications, the
// services token, the global session duration and the three rules do not, and Bot Fight
// Mode is on. Its backup Access app and policy carry names the docs do not give.
func Live() map[string]Response {
	r := Fresh()
	tunnelAndBackups(r)
	r[acct("/cfd_tunnel")] = Response{Result: []any{tunnel(TunnelID, "officer-dashboard", "inactive")}}
	r[acct("/access/service_tokens")] = Response{Result: []any{serviceToken("st-backups", "dashboard-backups-host", "2126-11-20T00:00:00Z")}}
	r[acct("/access/apps")] = Response{Result: []any{
		app("app-backups", "Backup sink", nil, []any{policy("pol-backups", "backup host only", "non_identity", token("st-backups"))}, Backup),
	}}
	// The live account had two reusable Service Auth policies: one used by the backup
	// app, one used by no application.
	r[acct("/access/policies")] = Response{Result: []any{
		withCount(policy("pol-backups", "backup host only", "non_identity", token("st-backups")), 1),
		withCount(policy("pol-spare", "<name>", "non_identity", token("st-old")), 0),
	}}
	r[zone("/bot_management")] = Response{Result: map[string]any{"enable_js": true, "fight_mode": true, "ai_bots_protection": "disabled",
		"ai_agents": map[string]any{"anthropic_agent": "disabled", "openai_agent": "disabled"}, "using_latest_model": true}}
	r[zone("/rulesets/phases/http_request_cache_settings/entrypoint")] = noEntrypoint("http_request_cache_settings")
	// The live backup database listed num_tables 0, a field the reference calls deprecated
	// and inaccurate. Its file_size, 49152 bytes (12 pages of 4 KiB), is that of the
	// migrated schema with no data, so here it has the migrated tables (review F-4 of #129).
	r[acct("/d1/database")] = Response{Result: []any{
		map[string]any{"uuid": "d1-members", "name": "membership", "version": "production", "num_tables": 3},
		map[string]any{"uuid": "d1-backups", "name": "dashboard-backups", "version": "production", "jurisdiction": nil, "num_tables": 0, "file_size": 49152},
	}}
	// The live zone had its own zone:http_ratelimit ruleset.
	r[zone("/rulesets/phases/http_ratelimit/entrypoint")] = Response{Result: map[string]any{"id": "rs-rl", "phase": "http_ratelimit", "kind": "zone",
		"rules": []any{map[string]any{"id": "r-rl", "description": "<name>", "expression": "true", "action": "block", "enabled": true}}}}
	return r
}

// LiveEmptyD1 is Live with a backup database that really has no tables, as if the
// migration never ran (a file of about 3 pages).
func LiveEmptyD1() map[string]Response {
	r := Live()
	r[acct("/d1/database")] = Response{Result: []any{
		map[string]any{"uuid": "d1-members", "name": "membership", "version": "production", "num_tables": 3},
		map[string]any{"uuid": "d1-backups", "name": "dashboard-backups", "version": "production", "jurisdiction": nil, "num_tables": 0, "file_size": 12288},
	}}
	r[D1TablesKey("d1-backups")] = Response{Result: []any{"_cf_KV"}}
	r[D1TriggersKey("d1-backups")] = Response{Result: []any{}}
	r[D1MigrationsKey("d1-backups")] = Response{Result: []any{}}
	r[D1SQLKey("d1-backups")] = Response{Result: map[string]any{"_cf_KV": MigratedBackupSQL()["_cf_KV"]}}
	return r
}

// Complete is an account set up exactly as the docs describe, by hand in the dashboard.
func Complete() map[string]Response {
	r := Fresh()
	tunnelAndBackups(r)
	r[acct("/access/organizations")] = Response{Result: map[string]any{"auth_domain": "club.cloudflareaccess.com", "name": "club", "session_duration": "730h"}}
	r[acct("/access/identity_providers")] = Response{Result: []any{
		map[string]any{"id": "idp-cf", "type": "cloudflare", "name": "Cloudflare", "config": map[string]any{}},
		map[string]any{"id": "idp-otp", "name": "One-time PIN login", "type": "onetimepin", "config": map[string]any{}},
	}}
	r[acct("/access/groups")] = Response{Result: []any{
		map[string]any{"id": "g-x", "name": "Officers (old club)"},
		map[string]any{"id": "g-officers", "name": "Officers", "include": []any{map[string]any{"email": map[string]any{"email": "a@example.org"}}}},
		map[string]any{"id": "g-launchpad", "name": "Launchpad"},
	}}
	r[acct("/access/service_tokens")] = Response{Result: []any{
		serviceToken("st-services", "officer-dashboard services", "2027-11-20T00:00:00Z"),
		serviceToken("st-backups", "dashboard-backups-host", "2126-11-20T00:00:00Z"),
	}}
	r[acct("/access/apps")] = Response{Result: []any{
		app("app-x", "Wiki", nil, []any{}, "wiki.example.org"),
		app("app-site", "Officer Dashboard", map[string]any{"session_duration": "730h", "allowed_idps": []any{"idp-otp"}, "auto_redirect_to_identity": true},
			[]any{policy("pol-site", "Officers and Launchpad", "allow", group("g-officers"), group("g-launchpad"))}, Host),
		app("app-agents", "Officer Dashboard: agents (bypass)", nil,
			[]any{policy("pol-agents", "Bypass", "bypass", map[string]any{"everyone": map[string]any{}})},
			Host+"/mcp", Host+"/oauth/register", Host+"/oauth/token", Host+"/.well-known/oauth-protected-resource",
			Host+"/.well-known/oauth-protected-resource/mcp", Host+"/.well-known/oauth-authorization-server"),
		app("app-services", "Officer Dashboard: services (service token)", map[string]any{"service_auth_401_redirect": true},
			[]any{policy("pol-services", "Service token", "non_identity", token("st-services"))}, Host+"/api/ingest", Host+"/api/agent"),
		app("app-backups", "Backup sink", nil, []any{policy("pol-backups", "backup host only", "non_identity", token("st-backups"))}, Backup),
	}}
	r[acct("/access/policies")] = Response{Result: []any{
		withCount(policy("pol-site", "Officers and Launchpad", "allow", group("g-officers"), group("g-launchpad")), 1),
		withCount(policy("pol-agents", "Bypass", "bypass", map[string]any{"everyone": map[string]any{}}), 1),
		withCount(policy("pol-services", "Service token", "non_identity", token("st-services")), 1),
		withCount(policy("pol-backups", "backup host only", "non_identity", token("st-backups")), 1),
	}}
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
	return r
}
