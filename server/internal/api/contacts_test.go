package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestContactLifecycleAndOutreach(t *testing.T) {
	e := newEnv(t)
	expectStatus(t, "anonymous", e.get("/api/contacts", actor{}), 401)
	created := e.mustOK(e.post("/api/contacts", map[string]any{"name": "Ada Example", "org": "Climate Lab", "type": "both", "topics": []string{"Climate tech", " climate TECH ", "R&D"}, "notes": "General context"}, e.officer()))
	var body struct {
		Contact struct {
			ID string `json:"id"`
		}
	}
	if err := json.Unmarshal(created.Raw, &body); err != nil {
		t.Fatal(err)
	}
	id := body.Contact.ID
	path := "/api/contacts/" + id
	if id == "" {
		t.Fatal("no contact ID")
	}
	e.mustOK(e.post(path+"/outreach", map[string]any{"date": "2026-10-30T10:00:00-07:00", "channel": "email", "outcome": "interested", "note": "Latest"}, e.officer()))
	e.mustOK(e.post(path+"/outreach", map[string]any{"date": "2026-10-30T16:00:00Z", "channel": "linkedin", "outcome": "awaiting_reply", "note": "Older"}, e.director()))
	updated := e.mustOK(e.do("PATCH", path, map[string]any{"org": "New lab"}, e.director()))
	var v struct {
		Contact struct {
			Notes  string   `json:"notes"`
			Topics []string `json:"topics"`
			Last   struct {
				OfficerID string `json:"officerId"`
				Note      string `json:"note"`
			} `json:"lastOutreach"`
		}
	}
	if err := json.Unmarshal(updated.Raw, &v); err != nil {
		t.Fatal(err)
	}
	if v.Contact.Last.OfficerID != "user-priya" || v.Contact.Last.Note != "Latest" || v.Contact.Notes != "General context" || len(v.Contact.Topics) != 2 {
		t.Fatalf("incorrect contact: %s", updated.Raw)
	}
	for _, url := range []string{"/api/contacts?type=mentor&topic=climate%20TECH", "/api/contacts?q=New%20lab", "/api/search?q=Ada%20Example", "/api/search?q=R%26D"} {
		r := e.mustOK(e.get(url, e.officer()))
		if !strings.Contains(string(r.Raw), id) {
			t.Fatalf("missing result %s", r.Raw)
		}
	}
	var eventID string
	if err := e.db.R.QueryRow(`SELECT id FROM events LIMIT 1`).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	linked := e.mustOK(e.do("PATCH", path, map[string]any{"eventIds": []string{eventID}, "email": " ada@example.org ", "linkedin": " https://linkedin.com/in/ada "}, e.officer()))
	if !strings.Contains(string(linked.Raw), eventID) || !strings.Contains(string(linked.Raw), `"email":"ada@example.org"`) {
		t.Fatalf("link or normalization: %s", linked.Raw)
	}
	expectStatus(t, "duplicate event", e.do("PATCH", path, map[string]any{"eventIds": []string{eventID, eventID}}, e.officer()), 400)
	expectStatus(t, "bad event", e.do("PATCH", path, map[string]any{"name": "Should rollback", "eventIds": []string{"missing"}}, e.officer()), 400)
	if r := e.mustOK(e.get(path, e.officer())); strings.Contains(string(r.Raw), "Should rollback") {
		t.Fatal("partial update escaped rollback")
	}
	for _, b := range []map[string]any{{"name": " "}, {"type": "other"}, {"email": "bad"}, {"topics": []string{" "}}, {"topics": nil}, {"linkedin": "javascript:alert(1)"}} {

		expectStatus(t, "invalid patch", e.do("PATCH", path, b, e.officer()), 400)
	}
	expectStatus(t, "missing", e.get("/api/contacts/missing", e.officer()), 404)
	e.mustOK(e.do("DELETE", path, nil, e.officer()))
	expectStatus(t, "deleted", e.get(path, e.officer()), 404)
	var n int
	if err := e.db.R.QueryRow(`SELECT count(*) FROM contact_outreach WHERE contact_id=?`, id).Scan(&n); err != nil || n != 0 {
		t.Fatalf("orphan outreach %d %v", n, err)
	}
	if err := e.db.R.QueryRow(`SELECT count(*) FROM audit_log WHERE entity_id=?`, id).Scan(&n); err != nil || n != 6 {
		t.Fatalf("audit mutations %d %v", n, err)
	}
}
