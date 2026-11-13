package api

import (
	"context"
	"testing"
	"time"

	"officerdashboard/internal/store"
)

func TestCurrentTerm(t *testing.T) {
	for in, want := range map[string]string{"2026-01-10": "Spring 2026", "2026-05-31": "Spring 2026", "2026-06-01": "Summer 2026", "2026-08-31": "Summer 2026", "2026-09-01": "Fall 2026", "2026-12-31": "Fall 2026"} {
		d, _ := time.Parse("2006-01-02", in)
		if got := currentTerm(d); got != want {
			t.Errorf("%s → %s, want %s", in, got, want)
		}
	}
}

// An account below the current onboarding version sees the flow once, with its profile
// pre-filled; the start term stays as it was and only an admin edits it.
func TestOnboardingVersionRollout(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if err := e.db.Write(ctx, func(q *store.Queries) error { return q.SetOnboardingVersion(ctx, "user-priya", 0) }); err != nil {
		t.Fatal(err)
	}
	officer := e.officer()
	me := e.mustOK(e.get("/api/me", officer))
	if me.at("onboarding") == nil || me.at("onboarding", "returning") != true || me.num("onboarding", "step") != 1 || me.at("onboarding", "startTerm") == nil {
		t.Fatalf("existing officer %s", me.Raw)
	}
	before := e.scalar(`SELECT coalesce(start_term,'') FROM officer_profiles WHERE user_id='user-priya'`)
	// Saving the profile again (even with a startTerm in the body) never changes the start term.
	p := e.mustOK(e.do("PUT", "/api/me/profile", map[string]any{"phone": "+1 206 555 0111", "birthday": "03-04", "team": "Marketing", "startTerm": "Spring 1999"}, officer))
	if p.str("profile", "team") != "Marketing" || e.scalar(`SELECT coalesce(start_term,'') FROM officer_profiles WHERE user_id='user-priya'`) != before {
		t.Fatalf("profile %s", p.Raw)
	}
	e.mustOK(e.do("PUT", "/api/me/onboarding", map[string]any{"step": 3}, officer))
	if me := e.mustOK(e.get("/api/me", officer)); me.num("onboarding", "step") != 3 {
		t.Fatalf("resume %s", me.Raw)
	}
	expectStatus(t, "step 6", e.do("PUT", "/api/me/onboarding", map[string]any{"step": 6}, officer), 400)
	e.mustOK(e.post("/api/me/onboarding/complete", nil, officer))
	if me := e.mustOK(e.get("/api/me", officer)); me.at("onboarding") != nil {
		t.Fatalf("still onboarding %s", me.Raw)
	}
	expectStatus(t, "save after done", e.do("PUT", "/api/me/onboarding", map[string]any{"step": 2}, officer), 409)

	// Start term: officers cannot edit it; directors can.
	expectStatus(t, "officer edits term", e.do("PATCH", "/api/officers/user-priya", map[string]any{"startTerm": "Fall 2024"}, officer), 403)
	r := e.mustOK(e.do("PATCH", "/api/officers/user-priya", map[string]any{"startTerm": "Fall 2024"}, e.director()))
	if r.str("officer", "startTerm") != "Fall 2024" {
		t.Fatalf("admin edit %s", r.Raw)
	}
}

func TestNewProfileGetsCurrentTerm(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if err := e.db.Write(ctx, func(q *store.Queries) error {
		return q.Exec(ctx, `DELETE FROM officer_profiles WHERE user_id='user-priya'`)
	}); err != nil {
		t.Skipf("cannot reset profile: %v", err)
	}
	p := e.mustOK(e.do("PUT", "/api/me/profile", map[string]any{"phone": "+1 206 555 0111", "birthday": "03-04", "subteamId": "team-events", "startTerm": "Spring 1999"}, e.officer()))
	want := currentTerm(e.srv.now())
	if p.str("profile", "startTerm") != want {
		t.Fatalf("startTerm = %s, want %s", p.str("profile", "startTerm"), want)
	}
}
