package api

import (
	"slices"
	"strings"
	"testing"
)

func designBody() map[string]any {
	return map[string]any{"title": "Venture night poster", "type": "poster", "formats": "A3 PDF", "dueDate": "2026-11-06", "urgency": "high",
		"brief": "Lead with the date.", "references": "Fall template", "eventId": "event-hack-night"}
}

func stringsOf(l []any) []string {
	out := []string{}
	for _, v := range l {
		s, _ := v.(string)
		out = append(out, s)
	}
	return out
}

func (e *testEnv) auditCount(action, id string) int64 {
	n, _ := e.scalar(`SELECT count(*) FROM audit_log WHERE action=? AND entity_id=?`, action, id).(int64)
	return n
}

// The board flow: an officer requests, a designer self-assigns by moving, the requester
// approves in review, and only that approval delivers.
func TestDesignRequestLifecycle(t *testing.T) {
	e := newEnv(t)
	priya, riley, director, ava := e.officer(), e.as("user-riley"), e.director(), e.as("user-ava")

	created := e.mustOK(e.post("/api/design-requests", designBody(), priya))
	id := created.str("request", "id")
	if created.str("request", "stage") != "new" || created.str("request", "requester", "id") != "user-priya" || created.str("request", "event", "name") != "Hack Night" {
		t.Fatalf("created %s", created.Raw)
	}
	if e.auditCount("design_request.created", id) != 1 {
		t.Fatal("create not audited")
	}

	// Everyone sees the same board, with the design team listed.
	board := e.mustOK(e.get("/api/design-requests", ava))
	if len(board.list("items")) != 4 || board.at("viewer", "isDesigner") != false || len(board.list("designers")) != 1 {
		t.Fatalf("board %s", board.Raw)
	}
	if e.mustOK(e.get("/api/design-requests", riley)).at("viewer", "isDesigner") != true {
		t.Fatal("riley is on the Design subteam")
	}

	// Non-designers cannot move or assign; requester rank does not matter.
	expectStatus(t, "officer moves", e.post("/api/design-requests/"+id+"/move", map[string]any{"stage": "accepted"}, ava), 403)
	expectStatus(t, "officer assigns", e.post("/api/design-requests/"+id+"/assign", map[string]any{"assigneeId": "user-ava"}, ava), 403)
	expectStatus(t, "assignee off the team", e.post("/api/design-requests/"+id+"/assign", map[string]any{"assigneeId": "user-ava"}, director), 400)
	// A director who is not a designer cannot move an unassigned card past New.
	expectStatus(t, "director moves unassigned", e.post("/api/design-requests/"+id+"/move", map[string]any{"stage": "accepted"}, director), 409)

	// A designer moving an unassigned card takes it.
	moved := e.mustOK(e.post("/api/design-requests/"+id+"/move", map[string]any{"stage": "accepted"}, riley))
	if moved.str("request", "assignee", "id") != "user-riley" || moved.str("request", "stage") != "accepted" {
		t.Fatalf("moved %s", moved.Raw)
	}
	expectStatus(t, "unassign past new", e.post("/api/design-requests/"+id+"/assign", map[string]any{"assigneeId": nil}, riley), 409)
	expectStatus(t, "designer delivers", e.post("/api/design-requests/"+id+"/move", map[string]any{"stage": "delivered"}, riley), 409)
	e.mustOK(e.post("/api/design-requests/"+id+"/move", map[string]any{"stage": "in-review"}, riley))

	// Only the requester approves; rank grants no shortcut.
	expectStatus(t, "designer approves", e.post("/api/design-requests/"+id+"/approve", nil, riley), 403)
	expectStatus(t, "director approves", e.post("/api/design-requests/"+id+"/approve", nil, director), 403)
	expectStatus(t, "leadership approves", e.post("/api/design-requests/"+id+"/approve", nil, e.as("user-leadership")), 403)
	view := e.mustOK(e.get("/api/design-requests/"+id, priya))
	if view.at("request", "permissions", "canApprove") != true || !slices.Equal(stringsOf(view.list("request", "permissions", "moveTo")), []string{"in-progress"}) {
		t.Fatalf("requester permissions %s", view.Raw)
	}
	// Requesting changes sends it back to In progress.
	e.mustOK(e.post("/api/design-requests/"+id+"/move", map[string]any{"stage": "in-progress"}, priya))
	expectStatus(t, "approve outside review", e.post("/api/design-requests/"+id+"/approve", nil, priya), 409)
	e.mustOK(e.post("/api/design-requests/"+id+"/move", map[string]any{"stage": "in-review"}, riley))
	done := e.mustOK(e.post("/api/design-requests/"+id+"/approve", nil, priya))
	if done.str("request", "stage") != "delivered" || done.str("request", "approvedBy", "id") != "user-priya" || done.str("request", "approvedAt") == "" {
		t.Fatalf("approved %s", done.Raw)
	}
	expectStatus(t, "edit delivered", e.do("PATCH", "/api/design-requests/"+id, map[string]any{"title": "x"}, priya), 409)

	// Reopening clears the approval.
	reopened := e.mustOK(e.post("/api/design-requests/"+id+"/move", map[string]any{"stage": "in-progress"}, riley))
	if reopened.at("request", "approvedAt") != nil {
		t.Fatalf("reopen kept approval %s", reopened.Raw)
	}
	for _, a := range []string{"design_request.moved", "design_request.approved"} {
		if e.auditCount(a, id) == 0 {
			t.Errorf("%s not audited", a)
		}
	}
}

func TestDesignRequestEditCommentsFilesAndDelete(t *testing.T) {
	e := newEnv(t)
	priya, riley, ava := e.officer(), e.as("user-riley"), e.as("user-ava")
	id := e.mustOK(e.post("/api/design-requests", designBody(), priya)).str("request", "id")

	expectStatus(t, "missing brief", e.post("/api/design-requests", map[string]any{"title": "x", "type": "poster"}, priya), 400)
	expectStatus(t, "bad type", e.post("/api/design-requests", with(designBody(), "type", "banner"), priya), 400)
	expectStatus(t, "bad event", e.post("/api/design-requests", with(designBody(), "eventId", "nope"), priya), 400)
	expectStatus(t, "other officer edits", e.do("PATCH", "/api/design-requests/"+id, map[string]any{"title": "Mine now"}, ava), 403)
	edited := e.mustOK(e.do("PATCH", "/api/design-requests/"+id, map[string]any{"title": "Hack Night poster v2", "eventId": nil, "program": "Officer program", "dueDate": nil}, priya))
	if edited.str("request", "title") != "Hack Night poster v2" || edited.at("request", "event") != nil || edited.str("request", "program") != "Officer program" || edited.at("request", "dueDate") != nil {
		t.Fatalf("edited %s", edited.Raw)
	}

	// Anyone comments.
	commented := e.mustOK(e.post("/api/design-requests/"+id+"/comments", map[string]any{"body": "Could the date be larger?"}, ava))
	if len(commented.list("request", "comments")) != 1 || commented.str("request", "comments", 0, "author", "id") != "user-ava" {
		t.Fatalf("comment %s", commented.Raw)
	}
	expectStatus(t, "empty comment", e.post("/api/design-requests/"+id+"/comments", map[string]any{"body": "  "}, ava), 400)

	png := formFile{"file", "ref.png", "image/png", "\x89PNG\r\n\x1a\nrest"}
	up := func(kind string, who actor) reply {
		return e.post("/api/design-requests/"+id+"/files", formBody{fields: map[string]string{"kind": kind}, files: []formFile{png}}, who)
	}
	expectStatus(t, "bystander reference", up("reference", ava), 403)
	expectStatus(t, "requester review file", up("review", priya), 403)
	ref := e.mustOK(up("reference", priya))
	e.mustOK(up("review", riley))
	files := ref.list("request", "files")
	if len(files) != 1 {
		t.Fatalf("files %s", ref.Raw)
	}
	url := ref.str("request", "files", 0, "url")
	dl := e.get(url, ava)
	if dl.Status != 200 || !strings.HasPrefix(string(dl.Raw), "\x89PNG") {
		t.Fatalf("download %d", dl.Status)
	}
	expectStatus(t, "fake png", e.post("/api/design-requests/"+id+"/files", formBody{fields: map[string]string{"kind": "reference"}, files: []formFile{{"file", "x.png", "image/png", "<html>"}}}, priya), 400)

	// Withdraw: requester while New only; directors any time.
	e.mustOK(e.post("/api/design-requests/"+id+"/assign", map[string]any{"assigneeId": "user-riley"}, riley))
	e.mustOK(e.post("/api/design-requests/"+id+"/move", map[string]any{"stage": "accepted"}, riley))
	expectStatus(t, "requester deletes accepted", e.do("DELETE", "/api/design-requests/"+id, nil, priya), 403)
	e.mustOK(e.do("DELETE", "/api/design-requests/"+id, nil, e.director()))
	expectStatus(t, "gone", e.get("/api/design-requests/"+id, priya), 404)
	if n, _ := e.scalar(`SELECT count(*) FROM design_request_files WHERE request_id=?`, id).(int64); n != 0 {
		t.Fatal("files left behind")
	}
	expectStatus(t, "anonymous", e.get("/api/design-requests", anon), 401)
}

func TestDesignRequestFromEvent(t *testing.T) {
	e := newEnv(t)
	draft := e.mustOK(e.get("/api/events/event-hack-night/design-request-draft", e.officer()))
	// Hack Night starts Nov 13 (Pacific); the draft is due a week earlier.
	if draft.str("draft", "dueDate") != "2026-11-06" || draft.str("draft", "eventId") != "event-hack-night" || !strings.Contains(draft.str("draft", "brief"), "Engineering Atrium") {
		t.Fatalf("draft %s", draft.Raw)
	}
	past := e.mustOK(e.get("/api/events/event-kickoff/design-request-draft", e.officer()))
	if past.at("draft", "dueDate") != nil {
		t.Fatalf("past event draft %s", past.Raw)
	}
	expectStatus(t, "unknown event", e.get("/api/events/nope/design-request-draft", e.officer()), 404)

	plain := e.mustOK(e.post("/api/events/event-hack-night/design-requests", nil, e.officer()))
	if plain.str("request", "title") != "Hack Night promo" || plain.str("request", "event", "id") != "event-hack-night" || plain.str("request", "dueDate") != "2026-11-06" {
		t.Fatalf("plain %s", plain.Raw)
	}
	custom := e.mustOK(e.post("/api/events/event-hack-night/design-requests", map[string]any{"title": "Hack Night shirts", "type": "merch", "eventId": "event-kickoff"}, e.officer()))
	if custom.str("request", "type") != "merch" || custom.str("request", "event", "id") != "event-hack-night" {
		t.Fatalf("custom %s", custom.Raw)
	}
	expectStatus(t, "bad override", e.post("/api/events/event-hack-night/design-requests", map[string]any{"type": "banner"}, e.officer()), 400)
}
