package api

import (
	"strings"
	"testing"

	"officerdashboard/internal/notion"
)

var draftEvent = map[string]any{
	"name": "Founder stories: building from zero", "startsAt": "2026-11-12T18:00:00-08:00", "endsAt": "2026-11-12T20:00:00-08:00",
	"location": "The Nest · Room 250", "description": "An honest conversation with founders.", "capacity": 100,
	"hostUserId": "user-priya", "hostSubteamId": "team-events",
}

func TestEventDraftCRUDIsAuditedForOfficers(t *testing.T) {
	e := newEnv(t)
	created := e.mustOK(e.post("/api/events", draftEvent, e.officer()))
	id := created.str("event", "id")
	if got := created.str("event", "status"); got != "draft" {
		t.Fatalf("new events start as drafts, got %q", got)
	}
	if created.num("event", "capacity") != 100 || created.str("event", "hostSubteam", "name") != "Events" || created.str("event", "endsAt") == "" {
		t.Fatalf("created event: %s", created.Raw)
	}

	detail := e.mustOK(e.get("/api/events/"+id, e.officer()))
	if detail.str("event", "description") != "An honest conversation with founders." {
		t.Fatalf("description: %s", detail.Raw)
	}
	// TODO(notion): the stub stores a placeholder link and the API says it is one.
	if url := detail.str("event", "notion", "url"); url != notion.PlaceholderPrefix+id || detail.at("event", "notion", "placeholder") != true {
		t.Fatalf("notion: %s", detail.Raw)
	}

	patched := e.mustOK(e.do("PATCH", "/api/events/"+id, map[string]any{"capacity": nil, "description": nil, "status": "published"}, e.officer()))
	if patched.at("event", "capacity") != nil || patched.str("event", "status") != "published" {
		t.Fatalf("patched: %s", patched.Raw)
	}
	expectStatus(t, "bad status", e.do("PATCH", "/api/events/"+id, map[string]any{"status": "live"}, e.officer()), 400)
	expectStatus(t, "zero capacity", e.do("PATCH", "/api/events/"+id, map[string]any{"capacity": 0}, e.officer()), 400)
	expectStatus(t, "unknown subteam", e.do("PATCH", "/api/events/"+id, map[string]any{"hostSubteamId": "team-nope"}, e.officer()), 400)
	expectStatus(t, "description too long", e.do("PATCH", "/api/events/"+id, map[string]any{"description": strings.Repeat("x", 5001)}, e.officer()), 400)
	expectStatus(t, "anonymous delete", e.do("DELETE", "/api/events/"+id, nil, anon), 401)

	e.mustOK(e.do("DELETE", "/api/events/"+id, nil, e.officer()))
	expectStatus(t, "gone", e.get("/api/events/"+id, e.officer()), 404)
	expectStatus(t, "delete again", e.do("DELETE", "/api/events/"+id, nil, e.officer()), 404)
	for _, action := range []string{"event.created", "event.updated", "event.deleted"} {
		if n := e.scalar(`SELECT count(*) FROM audit_log WHERE entity_id = ? AND action = ? AND actor_user_id = 'user-priya'`, id, action); n != int64(1) {
			t.Errorf("%s audited %v times", action, n)
		}
	}
	if n := e.scalar(`SELECT count(*) FROM audit_log WHERE entity_id = ? AND action = 'event.deleted' AND before_json LIKE '%Founder stories%' AND after_json IS NULL`, id); n != int64(1) {
		t.Errorf("delete audit keeps the before snapshot: %v", n)
	}
}

func TestEventDeleteRefusesWhenRequestsPointAtIt(t *testing.T) {
	e := newEnv(t)
	e.mustOK(e.post("/api/requests", with(purchase, "eventId", "event-hack-night"), e.officer()))
	r := e.do("DELETE", "/api/events/event-hack-night", nil, e.officer())
	expectStatus(t, "delete with requests", r, 409)
	if !strings.Contains(r.str("error", "message"), "purchase request") {
		t.Errorf("message: %s", r.Raw)
	}
}

func TestEventDeleteCascadesGuests(t *testing.T) {
	e := newEnv(t)
	if n := e.scalar(`SELECT count(*) FROM purchase_requests WHERE event_id = 'event-resume'`); n != int64(0) {
		t.Skip("seed links requests to event-resume")
	}
	e.mustOK(e.do("DELETE", "/api/events/event-resume", nil, e.officer()))
	if n := e.scalar(`SELECT count(*) FROM event_guests WHERE event_id = 'event-resume'`); n != int64(0) {
		t.Errorf("guests left behind: %v", n)
	}
}

func TestEventLumaURLIsValidatedNormalizedAndUnique(t *testing.T) {
	e := newEnv(t)
	id := e.mustOK(e.post("/api/events", draftEvent, e.officer())).str("event", "id")
	for _, bad := range []string{"http://lu.ma/founder-stories", "https://lu.ma/create", "https://lu.ma/", "https://lu.ma/discover", "https://evil.example/founder",
		"https://lu.ma.evil.example/x1", "https://user@lu.ma/founder", "https://lu.ma/a/b", "javascript:alert(1)", "https://lu.ma:8443/founder"} {
		expectStatus(t, bad, e.do("PATCH", "/api/events/"+id, map[string]any{"lumaUrl": bad}, e.officer()), 400)
	}
	r := e.mustOK(e.do("PATCH", "/api/events/"+id, map[string]any{"lumaUrl": "https://LU.MA/founder-stories/?utm_source=x#top"}, e.officer()))
	if got := r.str("event", "lumaUrl"); got != "https://lu.ma/founder-stories" {
		t.Fatalf("normalized: %q", got)
	}
	e.mustOK(e.do("PATCH", "/api/events/"+id, map[string]any{"lumaUrl": "https://luma.com/event/evt-AbC123xyz"}, e.officer()))

	other := e.mustOK(e.post("/api/events", with(draftEvent, "name", "Second"), e.officer())).str("event", "id")
	expectStatus(t, "same Luma page twice", e.do("PATCH", "/api/events/"+other, map[string]any{"lumaUrl": "https://luma.com/event/evt-AbC123xyz"}, e.officer()), 409)
	e.mustOK(e.do("PATCH", "/api/events/"+id, map[string]any{"lumaUrl": nil}, e.officer()))
	e.mustOK(e.do("PATCH", "/api/events/"+other, map[string]any{"lumaUrl": "https://luma.com/event/evt-AbC123xyz"}, e.officer()))
}

func TestEventCoverUploadServeAndRemove(t *testing.T) {
	e := newEnv(t)
	id := e.mustOK(e.post("/api/events", draftEvent, e.officer())).str("event", "id")
	png := "\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 64)
	put := func(name, mime, content string) reply {
		return e.do("PUT", "/api/events/"+id+"/cover", formBody{files: []formFile{{"file", name, mime, content}}}, e.officer())
	}
	expectStatus(t, "svg refused", put("cover.svg", "image/svg+xml", "<svg/>"), 400)
	expectStatus(t, "lying png refused", put("cover.png", "image/png", "<html>"), 400)
	expectStatus(t, "too big", put("cover.png", "image/png", "\x89PNG\r\n\x1a\n"+strings.Repeat("\x00", coverMaxBytes)), 400)
	expectStatus(t, "no cover yet", e.get("/api/events/"+id+"/cover", e.officer()), 404)

	r := e.mustOK(put("founder.png", "image/png", png))
	cover := r.str("event", "coverUrl")
	if !strings.HasPrefix(cover, "/api/events/"+id+"/cover?v=") {
		t.Fatalf("coverUrl %q", cover)
	}
	got := e.get(cover, e.officer())
	expectStatus(t, "serve", got, 200)
	if got.Header.Get("Content-Type") != "image/png" || got.Header.Get("X-Content-Type-Options") != "nosniff" || string(got.Raw) != png {
		t.Fatalf("served %v %q", got.Header, got.Raw[:8])
	}
	expectStatus(t, "anonymous cover", e.get(cover, anon), 401)

	removed := e.mustOK(e.do("DELETE", "/api/events/"+id+"/cover", nil, e.officer()))
	if removed.at("event", "coverUrl") != nil {
		t.Fatalf("cover still listed: %s", removed.Raw)
	}
	if n := e.scalar(`SELECT count(*) FROM audit_log WHERE entity_id = ? AND action IN ('event.cover_set','event.cover_removed')`, id); n != int64(2) {
		t.Errorf("cover audits %v", n)
	}
}

func TestEventDesignRequestLinkAndLookups(t *testing.T) {
	e := newEnv(t)
	id := e.mustOK(e.post("/api/events", draftEvent, e.officer())).str("event", "id")
	e.mustOK(e.do("PATCH", "/api/events/"+id, map[string]any{"designRequestId": "dr-123"}, e.officer()))
	if got := e.mustOK(e.get("/api/events/"+id, e.officer())).str("event", "designRequestId"); got != "dr-123" {
		t.Fatalf("designRequestId %q", got)
	}
	look := e.mustOK(e.get("/api/lookups", e.officer()))
	if len(look.list("subteams")) != 3 || len(look.list("hosts")) == 0 {
		t.Fatalf("lookups: %s", look.Raw)
	}
}

func TestImportedEventsArePublished(t *testing.T) {
	e := newEnv(t)
	if got := e.mustOK(e.get("/api/events/event-kickoff", e.officer())).str("event", "status"); got != "published" {
		t.Fatalf("seeded event status %q", got)
	}
}
