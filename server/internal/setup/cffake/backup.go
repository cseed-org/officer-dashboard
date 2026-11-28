package cffake

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"officerdashboard/internal/setup/sinkbundle"
)

// The backup Worker's endpoints: D1 create and query, the Worker upload, its workers.dev
// switches and its custom domain. Shapes follow the API reference pages the engine cites;
// where a reference leaves something open the choice is noted beside it, and the fake
// takes the stricter or the more dangerous reading.

// d1FreeLimit and scriptFreeLimit are the Free-plan limits: "Free: 10" databases
// (https://developers.cloudflare.com/d1/platform/limits/) and "Workers per account | 100"
// (https://developers.cloudflare.com/workers/platform/limits/).
const (
	d1FreeLimit     = 10
	scriptFreeLimit = 100
)

// createD1: POST /accounts/{a}/d1/database {"name"}. The result is one database object
// whose ID is "uuid" (create reference page). A second database with the same name is
// refused, as the dashboard does.
func (st *accountState) createD1(s *Server, body map[string]any) (any, *errReply) {
	name := orStr(body["name"])
	if name == "" {
		return nil, &errReply{http.StatusBadRequest, 7400, "name is required"}
	}
	list := s.list(acct("/d1/database"))
	for _, d := range list {
		if m, ok := d.(map[string]any); ok && m["name"] == name {
			return nil, &errReply{http.StatusBadRequest, 7502, "A database with that name already exists"}
		}
	}
	if len(list) >= d1FreeLimit {
		return nil, &errReply{http.StatusBadRequest, 7406, "database limit reached"}
	}
	db := map[string]any{"uuid": st.uuid(), "name": name, "created_at": "2026-11-28T00:00:00Z", "version": "production",
		"jurisdiction": nil, "num_tables": 0, "file_size": 12288}
	s.appendTo(acct("/d1/database"), db)
	s.routes[D1TablesKey(db["uuid"].(string))] = Response{Result: []any{"_cf_KV"}}
	return db, nil
}

func (s *Server) hasD1(uuid string) bool {
	for _, d := range s.list(acct("/d1/database")) {
		if m, ok := d.(map[string]any); ok && m["uuid"] == uuid {
			return true
		}
	}
	return false
}

var (
	createTable   = regexp.MustCompile(`(?i)CREATE TABLE (IF NOT EXISTS )?"?(\w+)"?`)
	createTrigger = regexp.MustCompile(`(?i)CREATE TRIGGER (IF NOT EXISTS )?"?(\w+)"?`)
	insertApplied = regexp.MustCompile(`(?i)INSERT INTO "?d1_migrations"? \(name\)\s*values \('([^']*)'\)`)
	destructive   = regexp.MustCompile(`(?i)\b(DROP|ALTER)\s|\bDELETE\s+FROM\b`)
)

// queryD1: POST /accounts/{a}/d1/database/{id}/query {"sql", "params"}. The result is an
// array of QueryResult {results, success, meta} (query reference page). The fake models
// only what setup sends: the schema listing and the d1_migrations listing (exactly
// sinkbundle's SchemaSQL and AppliedSQL), and a migration (CREATE TABLE and CREATE
// TRIGGER statements and Wrangler's d1_migrations record). It applies a migration all or
// nothing; the reference says only that statements "will be executed as a batch".
// Anything destructive is refused, so a test sees it.
func (st *accountState) queryD1(s *Server, uuid string, body map[string]any) (any, *errReply) {
	if !s.hasD1(uuid) {
		return nil, &errReply{http.StatusNotFound, 7404, "database not found"}
	}
	sql := orStr(body["sql"])
	tables, _ := s.routes[D1TablesKey(uuid)].Result.([]any)
	triggers, _ := s.routes[D1TriggersKey(uuid)].Result.([]any)
	applied, _ := s.routes[D1MigrationsKey(uuid)].Result.([]any)
	defs, _ := s.routes[D1SQLKey(uuid)].Result.(map[string]any)
	answer := func(rows []any) any {
		return []any{map[string]any{"results": rows, "success": true, "meta": map[string]any{"changes": 0, "rows_read": len(rows), "rows_written": 0, "changed_db": false}}}
	}
	sorted := func(l []any) []any {
		out := append([]any(nil), l...)
		slices.SortFunc(out, func(a, b any) int { return strings.Compare(fmt.Sprint(a), fmt.Sprint(b)) })
		return out
	}
	switch {
	case sql == "":
		return nil, &errReply{http.StatusBadRequest, 7400, "sql is required"}
	case destructive.MatchString(sql):
		return nil, &errReply{http.StatusBadRequest, 7500, "the fake refuses destructive SQL"}
	case sql == sinkbundle.SchemaSQL:
		rows := []any{}
		for _, n := range sorted(tables) {
			rows = append(rows, map[string]any{"type": "table", "name": n, "sql": defs[fmt.Sprint(n)]})
		}
		for _, n := range sorted(triggers) {
			rows = append(rows, map[string]any{"type": "trigger", "name": n, "sql": defs[fmt.Sprint(n)]})
		}
		return answer(rows), nil
	case sql == sinkbundle.AppliedSQL:
		if !slices.Contains(tables, any("d1_migrations")) {
			return nil, &errReply{http.StatusBadRequest, 7500, "no such table: d1_migrations: SQLITE_ERROR"}
		}
		rows := []any{}
		for _, n := range applied {
			rows = append(rows, map[string]any{"name": n})
		}
		return answer(rows), nil
	}
	created := createTable.FindAllStringSubmatch(sql, -1)
	if len(created) == 0 {
		return nil, &errReply{http.StatusBadRequest, 7500, "the fake does not model this SQL"}
	}
	next := append([]any(nil), tables...)
	for _, m := range created {
		exists := slices.Contains(next, any(m[2]))
		switch {
		case exists && m[1] == "":
			return nil, &errReply{http.StatusBadRequest, 7500, "table " + m[2] + " already exists: SQLITE_ERROR"}
		case !exists:
			next = append(next, m[2])
		}
	}
	nextTriggers := append([]any(nil), triggers...)
	for _, m := range createTrigger.FindAllStringSubmatch(sql, -1) {
		if slices.Contains(nextTriggers, any(m[2])) {
			return nil, &errReply{http.StatusBadRequest, 7500, "trigger " + m[2] + " already exists: SQLITE_ERROR"}
		}
		nextTriggers = append(nextTriggers, m[2])
	}
	if strings.Contains(sql, "AUTOINCREMENT") && !slices.Contains(next, any("sqlite_sequence")) {
		next = append(next, "sqlite_sequence")
	}
	for _, m := range insertApplied.FindAllStringSubmatch(sql, -1) {
		if slices.Contains(applied, any(m[1])) {
			return nil, &errReply{http.StatusBadRequest, 7500, "UNIQUE constraint failed: d1_migrations.name: SQLITE_CONSTRAINT"}
		}
		applied = append(applied, m[1])
	}
	nextDefs := map[string]any{}
	for k, v := range defs {
		nextDefs[k] = v
	}
	tb, tr := sinkbundle.Definitions(sql) // sqlite_master keeps each statement's text
	for _, m := range []map[string]string{tb, tr} {
		for k, v := range m {
			nextDefs[k] = v
		}
	}
	s.routes[D1TablesKey(uuid)] = Response{Result: next}
	s.routes[D1TriggersKey(uuid)] = Response{Result: nextTriggers}
	s.routes[D1SQLKey(uuid)] = Response{Result: nextDefs}
	s.routes[D1MigrationsKey(uuid)] = Response{Result: applied}
	return []any{map[string]any{"results": []any{}, "success": true, "meta": map[string]any{"changes": len(created), "changed_db": true}}}, nil
}

// Uploaded is one Worker upload the fake accepted: the metadata as sent and the module.
type Uploaded struct {
	Script   string
	Metadata map[string]any
	Module   string
}

// Uploads returns every Worker upload the fake accepted.
func (a *Account) Uploads() []Uploaded {
	a.acct.mu.Lock()
	defer a.acct.mu.Unlock()
	return append([]Uploaded(nil), a.acct.uploads...)
}

// upload: PUT /accounts/{a}/workers/scripts/{name}, multipart/form-data with a JSON
// "metadata" part whose main_module names the entry part, of type
// application/javascript+module (upload reference page). Bindings: d1 with database_id
// (the deprecated "id" is refused, so a test catches it), secret_text and plain_text with
// name and text. An existing script is replaced whole, which is why setup never sends
// this for one. The stored settings list secret_text bindings without their text, and a
// new script's workers.dev URL and preview URLs start on: the reference states no
// default, so the fake takes the exposed one.
func (st *accountState) upload(s *Server, req Request) (any, *errReply) {
	p := req.Path
	if req.Method != http.MethodPut || !strings.HasPrefix(p, acct("/workers/scripts/")) || strings.Contains(strings.TrimPrefix(p, acct("/workers/scripts/")), "/") {
		return nil, &errReply{http.StatusMethodNotAllowed, 10000, "the fake does not model multipart " + req.Method + " " + p}
	}
	name := strings.TrimPrefix(p, acct("/workers/scripts/"))
	bad := func(msg string) (any, *errReply) { return nil, &errReply{http.StatusBadRequest, 10021, msg} }
	_, params, err := mime.ParseMediaType(req.ContentType)
	if err != nil || params["boundary"] == "" {
		return bad("not multipart/form-data")
	}
	mr := multipart.NewReader(strings.NewReader(req.Body), params["boundary"])
	parts := map[string]string{}
	types := map[string]string{}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return bad("malformed multipart body")
		}
		raw, _ := io.ReadAll(part)
		parts[part.FormName()] = string(raw)
		types[part.FormName()] = part.Header.Get("Content-Type")
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(parts["metadata"]), &meta); err != nil {
		return bad("the metadata part is missing or not JSON")
	}
	main := orStr(meta["main_module"])
	if main == "" || parts[main] == "" {
		return bad("main_module must name a module part")
	}
	if types[main] != "application/javascript+module" {
		return bad("an ES module part must be application/javascript+module")
	}
	if orStr(meta["compatibility_date"]) == "" {
		return bad("compatibility_date is required by the fake")
	}
	var stored, secrets []any
	bindings, _ := meta["bindings"].([]any)
	for _, b := range bindings {
		m, _ := b.(map[string]any)
		switch m["type"] {
		case "d1":
			if _, ok := m["id"]; ok {
				return bad("binding " + orStr(m["name"]) + ": id has been renamed to database_id")
			}
			if !s.hasD1(orStr(m["database_id"])) {
				return bad("binding " + orStr(m["name"]) + ": D1 database not found")
			}
			stored = append(stored, map[string]any{"type": "d1", "name": m["name"], "database_id": m["database_id"]})
		case "secret_text":
			if orStr(m["name"]) == "" || orStr(m["text"]) == "" {
				return bad("a secret_text binding needs name and text")
			}
			stored = append(stored, map[string]any{"type": "secret_text", "name": m["name"]})
			secrets = append(secrets, map[string]any{"name": m["name"], "type": "secret_text"})
		case "plain_text":
			stored = append(stored, map[string]any{"type": "plain_text", "name": m["name"], "text": m["text"]})
		default:
			return bad(fmt.Sprintf("the fake does not model binding type %v", m["type"]))
		}
	}
	scripts := s.list(acct("/workers/scripts"))
	exists := false
	for _, sc := range scripts {
		if m, ok := sc.(map[string]any); ok && m["id"] == name {
			exists = true
		}
	}
	if !exists {
		if len(scripts) >= scriptFreeLimit {
			return nil, &errReply{http.StatusBadRequest, 10037, "script limit reached"}
		}
		s.appendTo(acct("/workers/scripts"), map[string]any{"id": name, "has_modules": true, "usage_model": "standard", "tags": []any{},
			"compatibility_date": meta["compatibility_date"], "compatibility_flags": orEmpty(meta["compatibility_flags"])})
	}
	script := acct("/workers/scripts/" + name)
	s.routes[script+"/settings"] = Response{Result: map[string]any{"bindings": stored, "compatibility_date": meta["compatibility_date"],
		"compatibility_flags": orEmpty(meta["compatibility_flags"]), "usage_model": "standard"}}
	s.routes[script+"/secrets"] = Response{Result: orEmpty(embeddedOrNil(secrets))}
	if !exists {
		s.routes[script+"/subdomain"] = Response{Result: map[string]any{"enabled": true, "previews_enabled": true}}
	}
	st.mu.Lock()
	st.uploads = append(st.uploads, Uploaded{Script: name, Metadata: meta, Module: parts[main]})
	st.mu.Unlock()
	return map[string]any{"id": name, "etag": "etag-" + name, "has_modules": true, "compatibility_date": meta["compatibility_date"],
		"created_on": "2026-11-28T00:00:00Z", "modified_on": "2026-11-28T00:00:00Z", "usage_model": "standard"}, nil
}

// subdomain: POST /accounts/{a}/workers/scripts/{name}/subdomain {"enabled",
// "previews_enabled"}; enabled is required, previews_enabled optional (reference page).
func (st *accountState) subdomain(s *Server, name string, body map[string]any) (any, *errReply) {
	path := acct("/workers/scripts/" + name + "/subdomain")
	cur, ok := s.routes[path].Result.(map[string]any)
	if !ok {
		return nil, &errReply{http.StatusNotFound, 10007, "This Worker does not exist on your account."}
	}
	en, ok := body["enabled"].(bool)
	if !ok {
		return nil, &errReply{http.StatusBadRequest, 10021, "enabled is required"}
	}
	next := map[string]any{"enabled": en, "previews_enabled": cur["previews_enabled"]}
	if pv, ok := body["previews_enabled"].(bool); ok {
		next["previews_enabled"] = pv
	}
	s.routes[path] = Response{Result: next}
	return next, nil
}

// attachDomain: PUT /accounts/{a}/workers/domains {"hostname", "service", "zone_id"};
// hostname and service are required (reference page). "You cannot create a Custom Domain
// on a hostname with an existing CNAME DNS record"
// (https://developers.cloudflare.com/workers/configuration/routing/custom-domains/).
func (st *accountState) attachDomain(s *Server, body map[string]any) (any, *errReply) {
	host, service := strings.ToLower(orStr(body["hostname"])), orStr(body["service"])
	if host == "" || service == "" {
		return nil, &errReply{http.StatusBadRequest, 100100, "hostname and service are required"}
	}
	found := false
	for _, sc := range s.list(acct("/workers/scripts")) {
		if m, ok := sc.(map[string]any); ok && m["id"] == service {
			found = true
		}
	}
	if !found {
		return nil, &errReply{http.StatusNotFound, 10007, "This Worker does not exist on your account."}
	}
	for _, r := range s.list(zone("/dns_records")) {
		if m, ok := r.(map[string]any); ok && strings.EqualFold(orStr(m["name"]), host) && m["type"] == "CNAME" {
			return nil, &errReply{http.StatusConflict, 100117, "Hostname already has externally managed DNS records"}
		}
	}
	d := map[string]any{"id": st.id("wd"), "zone_id": orDefault(orStr(body["zone_id"]), ZoneID), "zone_name": "example.org", "hostname": host,
		"service": service, "environment": "production", "cert_id": "cert-" + host, "enabled": true, "previews_enabled": false}
	s.appendTo(acct("/workers/domains"), d)
	return d, nil
}
