package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Any wholesale replacement of dashboard.db (rollback by a pre-R2 helper,
// manual recovery, restore) must not re-grant spent operations.
func TestR2MirrorRestoresErasedSpending(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "dashboard.db")
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	d.BindR2(ctx, "account/bucket")
	d.ReconcileR2(ctx, now, 0, nil)
	for i := 0; i < 4; i++ {
		if _, err := d.ReserveR2(ctx, now, "list", "", "", 0); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.ReserveR2(ctx, now, "get", "", "", 0); err != nil {
		t.Fatal(err)
	}
	d.Close()
	// Replace the database wholesale, as a pre-R2 rollback does.
	for _, suffix := range []string{"", "-wal", "-shm"} {
		os.Remove(path + suffix)
	}
	d, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.BindR2(ctx, "account/bucket"); err != nil {
		t.Fatal(err)
	}
	u, err := d.R2Usage(ctx, now)
	if err != nil || u.ClassA != 4 || u.ClassB != 1 {
		t.Fatal("erased ledger re-granted spent operations", u, err)
	}
	var reconciled int64
	d.R.QueryRow(`SELECT reconciled_at FROM r2_budget`).Scan(&reconciled)
	if reconciled != 0 {
		t.Fatal("recovered spending must force a fresh inventory before uploads")
	}
}

func TestR2MirrorFailsClosedWhenUnreadable(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "dashboard.db")
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	now := time.Now().UTC()
	d.BindR2(ctx, "account/bucket")
	os.WriteFile(filepath.Join(dir, R2MirrorName), []byte("{truncated"), 0o600)
	if err := d.BindR2(ctx, "account/bucket"); err != nil {
		t.Fatal("unreadable mirror must not stop startup", err)
	}
	if _, err := d.ReserveR2(ctx, now, "get", "", "", 0); !errors.Is(err, ErrR2Budget) {
		t.Fatal("unreadable mirror must refuse billable requests", err)
	}
	if _, err := d.ReserveR2(ctx, now, "delete", "", "", 0); err != nil {
		t.Fatal("free deletes need no mirror", err)
	}
}
