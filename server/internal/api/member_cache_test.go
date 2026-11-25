package api

import (
	"context"
	"officerdashboard/internal/cloudflare"
	"officerdashboard/internal/store"
	"testing"
)

func TestMemberEmailCacheTracksCorrectionsReplacementsAndRollback(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c := &Ctx{S: e.srv}
	email := "PRiYA@uw.example"
	p := cloudflare.Person{ID: "cache-test", FirstName: "Priya", UWEmail: &email, JoinDate: "2026-01-01"}
	save := func() {
		t.Helper()
		if err := e.db.Write(ctx, func(q *store.Queries) error { return q.SaveMember(ctx, p) }); err != nil {
			t.Fatal(err)
		}
	}
	save()
	first, err := c.cachedMemberEmails(ctx, e.db.Read())
	if err != nil || first["priya@uw.example"] != p.ID {
		t.Fatal(first, err)
	}
	counted := &countQueries{Q: e.db.R}
	if _, err := c.cachedMemberEmails(ctx, store.On(counted)); err != nil || counted.calls != 1 {
		t.Fatalf("cache miss: calls=%d %v", counted.calls, err)
	}
	email = "new@uw.example"
	save()
	second, err := c.cachedMemberEmails(ctx, e.db.Read())
	if err != nil || second[email] != p.ID || second["priya@uw.example"] != "" {
		t.Fatal(second, err)
	}
	if _, err := e.db.W.Exec(`DELETE FROM members_mirror`); err != nil {
		t.Fatal(err)
	}
	third, err := c.cachedMemberEmails(ctx, e.db.Read())
	if err != nil || len(third) != 0 {
		t.Fatal(third, err)
	}
	// A rolled-back import must not invalidate a committed snapshot permanently.
	tx, err := e.db.W.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.On(tx).SaveMember(ctx, p); err != nil {
		t.Fatal(err)
	}
	inside, err := c.cachedMemberEmails(ctx, store.On(tx))
	if err != nil || inside[email] != p.ID {
		t.Fatal(inside, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	outside, err := c.cachedMemberEmails(ctx, e.db.Read())
	if err != nil || len(outside) != 0 {
		t.Fatal(outside, err)
	}
}
