package api

import "testing"

func upcomingByID(r reply) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, v := range r.list("upcomingEvents") {
		m, _ := v.(map[string]any)
		id, _ := m["id"].(string)
		out[id] = m
	}
	return out
}

// The Design page's rail: events being planned in the next six weeks, each with a design
// indicator derived from design_need and the linked requests.
func TestDesignUpcomingEvents(t *testing.T) {
	e := newEnv(t)
	priya, ava, director := e.officer(), e.as("user-ava"), e.director()

	// A draft Priya does not host, inside the window; one past the six weeks; one cancelled.
	draft := e.mustOK(e.post("/api/events", map[string]any{"name": "Pitch practice", "startsAt": "2026-11-20T18:00:00-08:00", "hostUserId": "user-ava"}, priya))
	draftID := draft.str("event", "id")
	if draft.str("event", "designNeed") != "unknown" {
		t.Fatalf("new events start unknown: %s", draft.Raw)
	}
	e.mustOK(e.post("/api/events", map[string]any{"name": "Spring gala", "startsAt": "2027-02-20T18:00:00-08:00"}, priya))
	e.mustOK(e.post("/api/events", map[string]any{"name": "Called off", "startsAt": "2026-11-21T18:00:00-08:00", "status": "cancelled"}, priya))

	board := e.mustOK(e.get("/api/design-requests", priya))
	up := upcomingByID(board)
	if len(up) != 2 || up["event-hack-night"] == nil || up[draftID] == nil {
		t.Fatalf("window holds Hack Night and the draft only: %s", board.Raw)
	}
	if first, _ := board.list("upcomingEvents")[0].(map[string]any); first["id"] != "event-hack-night" {
		t.Fatalf("soonest first: %v", first)
	}
	// Hack Night has the seeded open poster request; the draft has nothing yet (unknown counts as needed).
	if up["event-hack-night"]["indicator"] != "requested" || len(up["event-hack-night"]["requests"].([]any)) != 1 {
		t.Fatalf("hack night %v", up["event-hack-night"])
	}
	if up[draftID]["indicator"] != "needs-design" || up[draftID]["canSetNotNeeded"] != false {
		t.Fatalf("draft for priya %v", up[draftID])
	}

	// Not needed is director+ or the host's call; anyone may say it is needed.
	expectStatus(t, "officer not host", e.do("PUT", "/api/events/"+draftID+"/design-need", map[string]any{"designNeed": "not_needed"}, priya), 403)
	expectStatus(t, "bad value", e.do("PUT", "/api/events/"+draftID+"/design-need", map[string]any{"designNeed": "maybe"}, priya), 400)
	host := e.mustOK(e.do("PUT", "/api/events/"+draftID+"/design-need", map[string]any{"designNeed": "not_needed"}, ava))
	if host.str("event", "indicator") != "not-needed" || host.str("event", "designNeed") != "not_needed" {
		t.Fatalf("host marks not needed: %s", host.Raw)
	}
	if e.auditCount("event.design_need", draftID) != 1 {
		t.Fatal("design need not audited")
	}
	// Repeating the same value writes nothing.
	e.mustOK(e.do("PUT", "/api/events/"+draftID+"/design-need", map[string]any{"designNeed": "not_needed"}, director))
	if e.auditCount("event.design_need", draftID) != 1 {
		t.Fatal("a no-op was audited")
	}
	e.mustOK(e.do("PUT", "/api/events/"+draftID+"/design-need", map[string]any{"designNeed": "needed"}, priya))
	e.mustOK(e.do("PUT", "/api/events/"+draftID+"/design-need", map[string]any{"designNeed": "not_needed"}, director))

	// A request on a not-needed event wins: requested, then done once delivered.
	req := e.mustOK(e.post("/api/events/"+draftID+"/design-requests", map[string]any{}, priya))
	rid := req.str("request", "id")
	if upcomingByID(e.mustOK(e.get("/api/design-requests", priya)))[draftID]["indicator"] != "requested" {
		t.Fatal("linked open request shows requested")
	}
	riley := e.as("user-riley")
	e.mustOK(e.post("/api/design-requests/"+rid+"/move", map[string]any{"stage": "in-review"}, riley))
	e.mustOK(e.post("/api/design-requests/"+rid+"/approve", nil, priya))
	if upcomingByID(e.mustOK(e.get("/api/design-requests", priya)))[draftID]["indicator"] != "done" {
		t.Fatal("all delivered shows done")
	}

	// The event editor carries the same rule.
	expectStatus(t, "editor not host", e.do("PATCH", "/api/events/event-hack-night", map[string]any{"designNeed": "not_needed"}, priya), 403)
	edited := e.mustOK(e.do("PATCH", "/api/events/event-hack-night", map[string]any{"designNeed": "not_needed"}, ava))
	if edited.str("event", "designNeed") != "not_needed" {
		t.Fatalf("host edits design need: %s", edited.Raw)
	}
	// Saving the editor again with the stored value is fine for anyone.
	e.mustOK(e.do("PATCH", "/api/events/event-hack-night", map[string]any{"designNeed": "not_needed", "name": "Hack Night"}, priya))
	expectStatus(t, "create not needed for another host", e.post("/api/events", map[string]any{"name": "X", "startsAt": "2026-11-22T18:00:00-08:00", "designNeed": "not_needed", "hostUserId": "user-ava"}, priya), 403)
	e.mustOK(e.post("/api/events", map[string]any{"name": "Y", "startsAt": "2026-11-22T18:00:00-08:00", "designNeed": "not_needed", "hostUserId": "user-priya"}, priya))
	expectStatus(t, "unknown event", e.do("PUT", "/api/events/nope/design-need", map[string]any{"designNeed": "needed"}, priya), 404)
}
