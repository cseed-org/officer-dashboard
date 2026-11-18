package store

import (
	"context"
	"testing"
)

// M-098: the hourly purge also removes portal sessions past their expiry.
func TestPurgeExpiredSessionsIncludesPortalSessions(t *testing.T) {
	db, err := Open(t.TempDir() + "/purge.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	const now = int64(1_800_000_000_000)
	err = db.Write(ctx, func(w *Queries) error {
		if err := w.Exec(ctx, `INSERT INTO contacts(id,name,type,created_at,updated_at) VALUES('m','Mentor','mentor',1,1)`); err != nil {
			return err
		}
		contact := "m"
		if err := w.SaveLaunchpadAccount(ctx, LaunchpadAccount{ID: "acct", Kind: "mentor", Email: "m@example.org", Status: "disabled", ContactID: &contact, CreatedAt: 1, UpdatedAt: 1}); err != nil {
			return err
		}
		if err := w.InsertLaunchpadSession(ctx, "expired", "acct", now-1, "", 1); err != nil {
			return err
		}
		if err := w.InsertLaunchpadSession(ctx, "edge", "acct", now, "", 1); err != nil {
			return err
		}
		return w.InsertLaunchpadSession(ctx, "live", "acct", now+1, "", 1)
	})
	if err != nil {
		t.Fatal(err)
	}
	var purged int64
	if err := db.Write(ctx, func(w *Queries) error {
		var err error
		purged, err = w.PurgeExpiredSessions(ctx, now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if purged != 2 {
		t.Fatalf("purged %d rows, want 2", purged)
	}
	rows, err := db.R.QueryContext(ctx, `SELECT id FROM buildspace_launchpad_sessions`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var left []string
	for rows.Next() {
		var id string
		_ = rows.Scan(&id)
		left = append(left, id)
	}
	if len(left) != 1 || left[0] != "live" {
		t.Fatalf("sessions left = %v, want [live]", left)
	}
}
