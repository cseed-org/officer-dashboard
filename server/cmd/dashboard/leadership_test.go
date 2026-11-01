package main

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/store"
)

func TestOperatorLeadershipProvisioningIsExplicitAuditedAndIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	err = db.Write(ctx, func(q *store.Queries) error {
		if err := q.InsertUser(ctx, store.User{ID: "owner", DiscordID: "123456789012345678", Name: "Owner", Role: "director", Active: true}); err != nil {
			return err
		}
		return q.InsertSession(ctx, auth.SHA256Hex("session"), "owner", 9999999999999, "test", 0)
	})
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	cfg := env(map[string]string{"DB_PATH": path})
	if run([]string{"provision-leadership"}, cfg, &out, &errs) == 0 {
		t.Fatal("missing identity accepted")
	}
	for i := 0; i < 2; i++ {
		if code := run([]string{"provision-leadership", "--discord-id", "123456789012345678"}, cfg, &out, &errs); code != 0 {
			t.Fatalf("provision: %s", errs.String())
		}
	}
	db, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	user, err := db.Read().UserByID(ctx, "owner")
	if err != nil || user.Role != "leadership" {
		t.Fatalf("owner: %+v %v", user, err)
	}
	var n int
	if err := db.R.QueryRow(`SELECT count(*) FROM audit_log WHERE action='user.provision_leadership'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("audits %d %v", n, err)
	}
	if err := db.R.QueryRow(`SELECT count(*) FROM sessions`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("sessions %d %v", n, err)
	}
}
