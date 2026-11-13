package store

import (
	"context"
	"testing"
)

func TestBootstrapOwnerIsIdempotentAndIgnoredAfterTransfer(t *testing.T) {
	db, err := Open(t.TempDir() + "/owner.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if mismatch, err := db.BootstrapOwner(ctx, "123456789012345678", "Original owner", 1); err != nil || mismatch {
		t.Fatalf("bootstrap %v %v", mismatch, err)
	}
	u, err := db.Read().UserByDiscordID(ctx, "123456789012345678")
	if err != nil || !u.IsOwner || !u.Active || u.Role != "leadership" || u.Name != "Original owner" {
		t.Fatalf("owner %+v %v", u, err)
	}
	if _, err := db.BootstrapOwner(ctx, u.DiscordID, "New env name", 2); err != nil {
		t.Fatal(err)
	}
	u, _ = db.Read().UserByID(ctx, u.ID)
	if u.Name != "Original owner" {
		t.Fatal("bootstrap overwrote established owner")
	}
	if err := db.Write(ctx, func(q *Queries) error {
		if err := q.InsertUser(ctx, User{ID: "next", DiscordID: "123456789012345679", Name: "Next", Role: "leadership", Active: true, IsOwner: true}); err != nil {
			return err
		}
		return q.SetOwner(ctx, u.ID, false, 3)
	}); err != nil {
		t.Fatal(err)
	}
	if mismatch, err := db.BootstrapOwner(ctx, u.DiscordID, "Old owner", 4); err != nil || !mismatch {
		t.Fatalf("expected ignore and warning %v %v", mismatch, err)
	}
	u, _ = db.Read().UserByID(ctx, u.ID)
	if u.IsOwner {
		t.Fatal("env reclaimed ownership")
	}
	if mismatch, err := db.BootstrapOwner(ctx, "123456789012345680", "Another", 5); err != nil || !mismatch {
		t.Fatalf("new env %v %v", mismatch, err)
	}
	if _, err := db.Read().UserByDiscordID(ctx, "123456789012345680"); err != ErrNotFound {
		t.Fatal("ignored env created user")
	}
	var n int
	if err := db.R.QueryRow(`SELECT count(*) FROM audit_log WHERE action='owner.bootstrap'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("audit %d %v", n, err)
	}
}
func TestBootstrapOwnerUpsertsInactiveExistingAccount(t *testing.T) {
	db, err := Open(t.TempDir() + "/owner.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.Write(ctx, func(q *Queries) error {
		return q.InsertUser(ctx, User{ID: "existing", DiscordID: "123456789012345678", Name: "Old name", Role: "officer", Active: false})
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.BootstrapOwner(ctx, "123456789012345678", "Configured name", 1); err != nil {
		t.Fatal(err)
	}
	u, err := db.Read().UserByID(ctx, "existing")
	if err != nil || !u.IsOwner || !u.Active || u.Role != "leadership" || u.Name != "Configured name" {
		t.Fatalf("upsert %+v %v", u, err)
	}
}
func TestLastOwnerDatabaseGuards(t *testing.T) {
	x := newTDB(t)
	x.must(`INSERT INTO users(id,discord_id,name,role,active,is_owner) VALUES('owner','123456789012345678','Owner','leadership',1,1)`)
	x.must(`INSERT INTO users(id,discord_id,name,role) VALUES('leader','123456789012345679','Leader','leadership')`)
	x.rejects("last owner", `UPDATE users SET is_owner=0 WHERE id='owner'`)
	x.rejects("last owner", `UPDATE users SET role='director' WHERE id='owner'`)
	x.rejects("last owner", `UPDATE users SET active=0 WHERE id='owner'`)
	x.rejects("last owner", `DELETE FROM users WHERE id='owner'`)
	x.rejects("replace an owner", `INSERT OR REPLACE INTO users(id,discord_id,name) VALUES('owner','123456789012345678','Replaced')`)
	x.rejects("CHECK", `UPDATE users SET is_owner=1 WHERE id='u1'`)
	x.rejects("CHECK", `UPDATE users SET is_owner=2 WHERE id='u1'`)
}
