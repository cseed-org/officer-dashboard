package main

import (
	"bytes"
	"context"
	"flag"
	"log/slog"
	"path/filepath"
	"strings"
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

func TestStartupProvisionsOwnerBeforeLoginAndUsesLegacyAlias(t *testing.T) {
	path := filepath.Join(t.TempDir(), "startup.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	settings, err := loadSettings(flag.NewFlagSet("test", flag.ContinueOnError), nil, env(map[string]string{"DB_PATH": path, "BOOTSTRAP_OWNER_DISCORD_ID": "123456789012345678", "BOOTSTRAP_DIRECTOR_DISCORD_ID": "123456789012345679", "BOOTSTRAP_OWNER_NAME": "Configured Owner"}))
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	if _, err := buildServer(settings, db, log); err != nil {
		t.Fatal(err)
	}
	if _, err := buildServer(settings, db, log); err != nil {
		t.Fatal(err)
	}
	u, err := db.Read().UserByDiscordID(context.Background(), "123456789012345678")
	if err != nil || !u.IsOwner || u.Name != "Configured Owner" {
		t.Fatalf("startup owner %+v %v", u, err)
	}
	// Simulate a transfer, then start with the old environment still configured.
	if err := db.Write(context.Background(), func(q *store.Queries) error {
		if err := q.InsertUser(context.Background(), store.User{ID: "new-owner", DiscordID: "123456789012345680", Name: "Next", Active: true, Role: "leadership", IsOwner: true}); err != nil {
			return err
		}
		return q.SetOwner(context.Background(), u.ID, false, 1)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := buildServer(settings, db, log); err != nil {
		t.Fatal(err)
	}
	u, _ = db.Read().UserByID(context.Background(), u.ID)
	if u.IsOwner {
		t.Fatal("startup reclaimed ownership")
	}
	if !strings.Contains(logs.String(), "ownership already belongs to another account") {
		t.Fatal("missing mismatch warning")
	}
	alias, err := loadSettings(flag.NewFlagSet("test", flag.ContinueOnError), nil, env(map[string]string{"BOOTSTRAP_DIRECTOR_DISCORD_ID": "123456789012345679"}))
	if err != nil || alias.BootstrapDirector != "123456789012345679" {
		t.Fatalf("legacy alias %+v %v", alias, err)
	}
}

func TestLeadershipCommandOwnerFlag(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recover.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Write(context.Background(), func(q *store.Queries) error {
		return q.InsertUser(context.Background(), store.User{ID: "recover", DiscordID: "123456789012345678", Name: "Recover", Role: "leadership", Active: true})
	}); err != nil {
		t.Fatal(err)
	}
	db.Close()
	var out, errs bytes.Buffer
	for i := 0; i < 2; i++ {
		if run([]string{"provision-leadership", "--owner", "--discord-id", "123456789012345678"}, env(map[string]string{"DB_PATH": path}), &out, &errs) != 0 {
			t.Fatal(errs.String())
		}
	}
	db, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	u, err := db.Read().UserByID(context.Background(), "recover")
	if err != nil || !u.IsOwner {
		t.Fatalf("recover %+v %v", u, err)
	}
	var n int
	if err := db.R.QueryRow(`SELECT count(*) FROM audit_log WHERE action='user.provision_leadership'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("recovery audit %d %v", n, err)
	}
}
