package api

import (
	"encoding/json"
	"officerdashboard/internal/store"
	"testing"
)

func readBuildspace(t *testing.T, e *testEnv) store.Buildspace {
	t.Helper()
	r := e.mustOK(e.get("/api/buildspace-launchpad", e.officer()))
	var b store.Buildspace
	if err := json.Unmarshal(r.Raw, &b); err != nil {
		t.Fatal(err)
	}
	return b
}
func TestBuildspaceMembershipActivityAndValidation(t *testing.T) {
	e := newEnv(t)
	officer := e.officer()
	expectStatus(t, "anonymous", e.get("/api/buildspace-launchpad", anon), 401)
	p := map[string]any{"name": "Test project", "description": "Prototype", "track": "Software", "stage": "Building", "links": []string{"https://example.org"}, "mentorIds": []string{"bs-mentor-maya"}}
	e.mustOK(e.do("PUT", "/api/buildspace-launchpad/projects/test-project", p, officer))
	p["mentorIds"] = []string{"bs-mentor-maya", "bs-mentor-maya"}
	expectStatus(t, "duplicate mentors", e.do("PUT", "/api/buildspace-launchpad/projects/test-project", p, officer), 400)
	p["mentorIds"] = []string{"missing"}
	expectStatus(t, "missing mentor", e.do("PUT", "/api/buildspace-launchpad/projects/test-project", p, officer), 400)
	p["mentorIds"] = []string{}
	p["track"] = "Other"
	expectStatus(t, "track", e.do("PUT", "/api/buildspace-launchpad/projects/test-project", p, officer), 400)
	p["track"] = "Software"
	p["links"] = []string{"javascript:alert(1)"}
	expectStatus(t, "unsafe link", e.do("PUT", "/api/buildspace-launchpad/projects/test-project", p, officer), 400)
	person := map[string]any{"name": "Builder", "projectId": "test-project", "joinedOn": "2026-10-01", "memberId": nil, "discordId": "123456789012345678"}
	e.mustOK(e.do("PUT", "/api/buildspace-launchpad/people/test-person", person, officer))
	person["memberId"] = "missing"
	expectStatus(t, "invalid member", e.do("PUT", "/api/buildspace-launchpad/people/test-person", person, officer), 400)
	person["memberId"] = nil
	a := map[string]any{"projectId": "test-project", "personId": "test-person", "mentorId": nil, "purchaseId": nil, "kind": "weekly", "date": "2026-10-31", "body": "Prototype works", "status": "on-track"}
	e.mustOK(e.post("/api/buildspace-launchpad/activity", a, officer))
	a["personId"] = "bs-person-01"
	expectStatus(t, "cross-project builder", e.post("/api/buildspace-launchpad/activity", a, officer), 400)
	a["personId"] = nil
	expectStatus(t, "weekly requires person", e.post("/api/buildspace-launchpad/activity", a, officer), 400)
	a["kind"] = "mentor"
	a["mentorId"] = "bs-mentor-jordan"
	expectStatus(t, "unassigned mentor", e.post("/api/buildspace-launchpad/activity", a, officer), 400)
	a["mentorId"] = "bs-mentor-maya"
	e.mustOK(e.post("/api/buildspace-launchpad/activity", a, officer))
	a["kind"] = "purchase"
	a["mentorId"] = nil
	a["purchaseId"] = "req-0005"
	e.mustOK(e.post("/api/buildspace-launchpad/activity", a, officer))
	b := readBuildspace(t, e)
	if b.WeekStart != "2026-10-26" || b.Today != "2026-10-31" {
		t.Fatalf("incorrect week %s %s", b.WeekStart, b.Today)
	}
	var linked store.BuildspaceActivity
	for _, v := range b.Activity {
		if v.ProjectID == "test-project" && v.Kind == "purchase" {
			linked = v
		}
	}
	if linked.PurchaseStatus == nil || *linked.PurchaseStatus != "submitted" {
		t.Fatal("finance status not joined")
	}
	if linked.PurchaseNeededBy == nil || *linked.PurchaseNeededBy != "2026-11-12" {
		t.Fatal("finance deadline not joined")
	}
	expectStatus(t, "finance owns linked status", e.post("/api/buildspace-launchpad/activity/"+linked.ID+"/resolve", nil, officer), 400)
	a["kind"] = "request"
	a["purchaseId"] = nil
	a["status"] = "open"
	e.mustOK(e.post("/api/buildspace-launchpad/activity", a, officer))
	for _, v := range readBuildspace(t, e).Activity {
		if v.ProjectID == "test-project" && v.Kind == "request" {
			e.mustOK(e.post("/api/buildspace-launchpad/activity/"+v.ID+"/resolve", nil, officer))
		}
	}
	person["projectId"] = "bs-moss"
	expectStatus(t, "history protected", e.do("PUT", "/api/buildspace-launchpad/people/test-person", person, officer), 409)
}
func TestBuildspaceDiscordScopedReplayAndIdentity(t *testing.T) {
	e := newEnv(t)
	issued := e.mustOK(e.post("/api/admin/tokens", map[string]any{"name": "Buildspace + Launchpad bot", "scopes": []string{"ingest:buildspace"}, "expiresInDays": 30}, e.director()))
	token := issued.at("plaintext").(string)
	actor := bearer(token)
	payload := map[string]any{"discordId": "100000000000000001", "externalId": "message-1", "kind": "weekly", "date": "2026-10-31", "body": "Got feedback", "status": "needs-help"}
	expectStatus(t, "session not sufficient", e.post("/api/ingest/buildspace-launchpad", payload, e.officer()), 401)
	e.mustOK(e.post("/api/ingest/buildspace-launchpad", payload, actor))
	e.mustOK(e.post("/api/ingest/buildspace-launchpad", payload, actor))
	n := 0
	for _, a := range readBuildspace(t, e).Activity {
		if a.ExternalID != nil && *a.ExternalID == "message-1" {
			n++
			if a.PersonID == nil || *a.PersonID != "bs-person-01" {
				t.Fatal("wrong identity")
			}
		}
	}
	if n != 1 {
		t.Fatalf("retry duplicated update: %d", n)
	}
	payload["body"] = "Conflicting update"
	expectStatus(t, "replay conflict", e.post("/api/ingest/buildspace-launchpad", payload, actor), 409)
	payload["externalId"] = "message-2"
	payload["discordId"] = "999999999999999999"
	payload["personId"] = "bs-person-01"
	expectStatus(t, "cannot spoof builder", e.post("/api/ingest/buildspace-launchpad", payload, actor), 400)
	payload["discordId"] = "100000000000000001"
	payload["kind"] = "mentor"
	expectStatus(t, "bot weekly only", e.post("/api/ingest/buildspace-launchpad", payload, actor), 400)
	payload["kind"] = "weekly"
	wrong := e.mustOK(e.post("/api/admin/tokens", map[string]any{"name": "Luma token", "scopes": []string{"ingest:luma"}}, e.director()))
	expectStatus(t, "wrong scope", e.post("/api/ingest/buildspace-launchpad", payload, bearer(wrong.at("plaintext").(string))), 403)
	e.mustOK(e.do("DELETE", "/api/admin/tokens/"+issued.at("token", "id").(string), nil, e.director()))
	expectStatus(t, "revoked", e.post("/api/ingest/buildspace-launchpad", payload, actor), 401)
}

func TestBuildspaceLegacyAPIAlias(t *testing.T) {
	e := newEnv(t)
	testBuildspaceLegacyWrites(t, e)
	expectStatus(t, "legacy anonymous", e.get("/api/buildspace", anon), 401)
	legacy := e.mustOK(e.get("/api/buildspace", e.officer()))
	canonical := e.mustOK(e.get("/api/buildspace-launchpad", e.officer()))
	if string(legacy.Raw) != string(canonical.Raw) {
		t.Fatal("legacy and canonical responses differ")
	}
}

func testBuildspaceLegacyWrites(t *testing.T, e *testEnv) {
	issued := e.mustOK(e.post("/api/admin/tokens", map[string]any{"name": "Legacy bot", "scopes": []string{"ingest:buildspace"}, "expiresInDays": 30}, e.director()))
	payload := map[string]any{"discordId": "100000000000000001", "externalId": "legacy-message", "kind": "weekly", "date": "2026-10-31", "body": "Legacy progress", "status": "on-track"}
	expectStatus(t, "legacy ingest rejects session", e.post("/api/ingest/buildspace", payload, e.officer()), 401)
	actor := bearer(issued.at("plaintext").(string))
	e.mustOK(e.post("/api/ingest/buildspace", payload, actor))
	e.mustOK(e.post("/api/ingest/buildspace-launchpad", payload, actor))
	count := 0
	for _, a := range readBuildspace(t, e).Activity {
		if a.ExternalID != nil && *a.ExternalID == "legacy-message" {
			count++
			if a.Body != "Legacy progress" {
				t.Fatal("legacy ingest body changed")
			}
		}
	}
	if count != 1 {
		t.Fatalf("legacy replay created %d entries", count)
	}
	expectStatus(t, "legacy schedule enforces leadership", e.do("PUT", "/api/buildspace/schedule", map[string]any{"dueWeekday": 3, "dueTime": nil}, e.officer()), 403)
}
