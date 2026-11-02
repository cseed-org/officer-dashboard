package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"officerdashboard/internal/cfsync"
	"officerdashboard/internal/cloudflare"
	"officerdashboard/internal/store"
	"strings"
	"testing"
)

func TestMembersRolesValidationAndAudit(t *testing.T) {
	p := cloudflare.Person{ID: "p1", FirstName: "Ada", LastName: "Lovelace", JoinDate: "2026-01-01", HasBeenOfficer: 1}
	calls := 0
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var b struct {
			SQL    string `json:"sql"`
			Params []any  `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&b)
		if !strings.HasPrefix(b.SQL, "UPDATE People SET major=? WHERE person_id=? RETURNING *") && !strings.HasPrefix(b.SQL, "UPDATE People SET major=NULL WHERE person_id=? RETURNING *") {
			t.Errorf("unexpected SQL %s", b.SQL)
		}
		for _, param := range b.Params {
			if param == nil {
				t.Error("D1 params must not contain JSON null")
			}
		}
		if strings.Contains(b.SQL, "major=NULL") {
			p.Major = nil
		} else {
			major := b.Params[0].(string)
			p.Major = &major
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": []any{map[string]any{"success": true, "results": []any{p}}}})
	}))
	defer fake.Close()
	e := newEnv(t, func(cfg *Config) {
		client := cloudflare.New(cloudflare.Config{Token: "secret", AccountID: "a", DatabaseID: "d"})
		client.BaseURL = fake.URL
		cfg.Cloudflare = &cfsync.Service{Client: client, DB: cfg.DB}
	})
	ctx := context.Background()
	if err := e.db.Write(ctx, func(q *store.Queries) error { return q.SaveMember(ctx, p) }); err != nil {
		t.Fatal(err)
	}
	officer, director := e.officer(), e.director()
	expectStatus(t, "anonymous", e.do("GET", "/api/members", nil, anon), 401)
	expectStatus(t, "list", e.do("GET", "/api/members?q=ada&officer=yes", nil, officer), 200)
	for _, path := range []string{"/api/members?limit=201", "/api/members?offset=-1", "/api/members?officer=maybe"} {
		expectStatus(t, path, e.do("GET", path, nil, officer), 400)
	}
	expectStatus(t, "officer write", e.do("PATCH", "/api/members/p1", map[string]any{"major": "CS", "auditNote": "Correction"}, officer), 403)
	expectStatus(t, "officer sync", e.do("POST", "/api/cloudflare/sync", nil, officer), 403)
	expectStatus(t, "officer export", e.do("GET", "/api/members/export", nil, officer), 403)
	for _, b := range []map[string]any{{"major": "CS"}, {"auditNote": "why", "uw_email": "a@example.com"}, {"auditNote": "why", "cs_email": "a@uw.edu"}, {"auditNote": "why", "join_date": "2026-02-01"}, {"auditNote": "why", "has_been_cseed_officer": 0}} {
		expectStatus(t, "validation", e.do("PATCH", "/api/members/p1", b, director), 400)
	}
	if calls != 0 {
		t.Fatal("invalid/unauthorized writes reached D1")
	}
	expectStatus(t, "director write", e.do("PATCH", "/api/members/p1", map[string]any{"major": "CS", "auditNote": "Correct major"}, director), 200)
	after, err := e.db.Read().Member(ctx, "p1")
	if err != nil || after.Major == nil || *after.Major != "CS" || after.JoinDate != "2026-01-01" || after.HasBeenOfficer != 1 {
		t.Fatalf("mirror update: %+v %v", after, err)
	}
	var n int
	_ = e.db.R.QueryRow("SELECT count(*) FROM audit_log WHERE action IN ('member.update_requested','member.updated')").Scan(&n)
	if n != 2 {
		t.Fatalf("audit rows %d", n)
	}
	if err = e.db.Write(ctx, func(q *store.Queries) error {
		return q.Exec(ctx, "UPDATE users SET role='leadership' WHERE id='user-director'")
	}); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, "leadership write", e.do("PATCH", "/api/members/p1", map[string]any{"major": "Math", "auditNote": "Correction"}, director), 200)
	expectStatus(t, "export", e.do("GET", "/api/members/export?q=Ada", nil, director), 200)
	expectStatus(t, "clear nullable", e.do("PATCH", "/api/members/p1", map[string]any{"major": nil, "auditNote": "Clear incorrect major"}, director), 200)
	cleared, err := e.db.Read().Member(ctx, "p1")
	if err != nil || cleared.Major != nil {
		t.Fatal("nullable field not cleared")
	}

}
func TestMembersNotConnectedAndCSV(t *testing.T) {
	e := newEnv(t)
	o, d := e.officer(), e.director()
	r := e.do("GET", "/api/cloudflare/widgets", nil, o)
	expectStatus(t, "widgets", r, 200)
	if r.at("widgets", 0, "connected") != false || r.at("widgets", 0, "data") != nil {
		t.Fatal("disconnected widget fabricated data")
	}
	expectStatus(t, "skip sync", e.do("POST", "/api/cloudflare/sync", nil, d), 200)
	expectStatus(t, "write disconnected", e.do("PATCH", "/api/members/p1", map[string]any{"major": "CS", "auditNote": "Correction"}, d), 503)
	for _, s := range []string{"=1+1", " +1", "-1", "@x", "\tvalue", "\rvalue"} {
		if !strings.HasPrefix(csvCell(s), "'") {
			t.Errorf("unsafe CSV %q", s)
		}
	}
}
