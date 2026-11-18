package api

import (
	"context"
	"fmt"
	"testing"
	"time"

	"officerdashboard/internal/store"
)

// M-019: each kind ranks in SQL before its LIMIT, so an older (or later-named) exact or
// prefix match survives more weak secondary-field matches than the limit.
func TestSearchKeepsBestMatchPastWeakMatches(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	const weak = 25 // more than the default limit of 20
	err := e.db.Write(ctx, func(w *store.Queries) error {
		run := func(q string, args ...any) error { return w.Exec(ctx, q, args...) }
		if err := run(`INSERT INTO events (id, name, starts_at, location) VALUES ('ev-best', 'S3needle summit', '2020-01-01T10:00:00Z', 'Hall')`); err != nil {
			return err
		}
		if err := run(`INSERT INTO contacts (id, name, type, created_at, updated_at) VALUES ('ct-best', 'C3needle Zed', 'speaker', 1, 1)`); err != nil {
			return err
		}
		if err := run(`INSERT INTO users (id, discord_id, name, role) VALUES ('u-best', '900000000000000999', 'U3needle Zed', 'officer')`); err != nil {
			return err
		}
		if err := run(`INSERT INTO accounts (id, code, name, type, normal_balance) VALUES ('ac-best', '9999', 'A3needle fees', 'expense', 'debit')`); err != nil {
			return err
		}
		for i := 0; i < weak; i++ {
			if err := run(`INSERT INTO events (id, name, starts_at, location) VALUES (?, ?, ?, 'The s3needle hall')`, fmt.Sprintf("ev-weak-%02d", i), fmt.Sprintf("Meetup %02d", i), fmt.Sprintf("2027-01-%02dT10:00:00Z", i+1)); err != nil {
				return err
			}
			if err := run(`INSERT INTO contacts (id, name, org, type, created_at, updated_at) VALUES (?, ?, 'c3needle corp', 'speaker', 1, 1)`, fmt.Sprintf("ct-weak-%02d", i), fmt.Sprintf("Aa %02d", i)); err != nil {
				return err
			}
			if err := run(`INSERT INTO users (id, discord_id, name, title, role) VALUES (?, ?, ?, 'u3needle lead', 'officer')`, fmt.Sprintf("u-weak-%02d", i), fmt.Sprintf("9000000000000001%02d", i), fmt.Sprintf("Aa %02d", i)); err != nil {
				return err
			}
			if err := run(`INSERT INTO accounts (id, code, name, type, normal_balance) VALUES (?, ?, ?, 'expense', 'debit')`, fmt.Sprintf("ac-weak-%02d", i), fmt.Sprintf("01%02d", i), fmt.Sprintf("Misc a3needle %02d", i)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	e.clock = testNow.Add(-48 * time.Hour)
	best := e.mustOK(e.post("/api/requests", with(purchase, "title", "R3needle kits", "vendor", "Shop"), e.officer())).str("request", "id")
	for i := 0; i < weak; i++ {
		e.clock = e.clock.Add(time.Minute)
		e.mustOK(e.post("/api/requests", with(purchase, "title", fmt.Sprintf("Order %02d", i), "vendor", "r3needle supply"), e.officer()))
	}
	e.clock = testNow
	for _, c := range []struct{ q, kind, id string }{
		{"s3needle", "event", "ev-best"},
		{"c3needle", "contact", "ct-best"},
		{"u3needle", "user", "u-best"},
		{"a3needle", "account", "ac-best"},
		{"r3needle", "request", best},
	} {
		first := e.mustOK(e.get("/api/search?q="+c.q+"&limit=1", e.director()))
		if first.str("results", 0, "kind") != c.kind || first.str("results", 0, "id") != c.id {
			t.Errorf("%s limit=1: got %s %s, want %s %s", c.q, first.str("results", 0, "kind"), first.str("results", 0, "id"), c.kind, c.id)
		}
		all := e.mustOK(e.get("/api/search?q="+c.q, e.director()))
		found := false
		for _, it := range all.list("results") {
			found = found || it.(map[string]any)["id"] == c.id
		}
		if !found || all.str("results", 0, "id") != c.id {
			t.Errorf("%s default limit: best match missing or not first in %s", c.q, all.Raw)
		}
	}
}
