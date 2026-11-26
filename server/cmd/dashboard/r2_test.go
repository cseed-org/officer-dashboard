package main

import (
	"bytes"
	"context"
	"flag"
	"io"
	"log/slog"
	"officerdashboard/internal/seed"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStorageSelectionFailsClosed(t *testing.T) {
	for _, values := range []map[string]string{
		{"FILE_STORAGE": "typo"},
		{"FILE_STORAGE": "drive"},
		{"FILE_STORAGE": "local"},
		{"R2_ACCOUNT_ID": "orphaned"},
		{"R2_ANALYTICS_TOKEN": "orphaned"},
	} {
		f := flag.NewFlagSet("test", flag.ContinueOnError)
		f.SetOutput(io.Discard)
		if _, err := loadSettings(f, nil, env(values)); err == nil {
			t.Fatalf("accepted invalid storage settings: %v", values)
		}
	}
	if _, err := fileSettings(env(map[string]string{"R2_SECRET_ACCESS_KEY": "do-not-use-direct-env"})); err == nil {
		t.Fatal("direct R2 secret accepted")
	}
	if _, err := fileSettings(env(map[string]string{"R2_ANALYTICS_TOKEN": "do-not-use-direct-env"})); err == nil {
		t.Fatal("direct R2 analytics token accepted")
	}
}

// Review #1: no R2 state (exhausted budget, restore pause, unreachable or
// rejecting R2: startup makes no R2 request) may stop the dashboard.
func TestExhaustedR2DoesNotStopDashboard(t *testing.T) {
	for name, state := range map[string]string{
		"exhausted":      `INSERT INTO r2_day VALUES(strftime('%Y-%m-%d','now'),900000,9000000)`,
		"restore-paused": `INSERT INTO r2_reseed(id,requested_at) VALUES(1,CAST(strftime('%s','now') AS INTEGER))`,
	} {
		t.Run(name, func(t *testing.T) {
			db, err := openDB(filepath.Join(t.TempDir(), "db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			account := strings.Repeat("a", 32)
			if err := db.BindR2(context.Background(), account+"/receipts"); err != nil {
				t.Fatal(err)
			}
			if _, err := db.W.Exec(state); err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			_, err = buildServer(settings{AppURL: "https://example.test", FileStorage: "r2", R2Account: account, R2Bucket: "receipts", R2Access: "test", R2Secret: "test", WebhookURL: "https://example.test/webhook"}, db, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err != nil {
				t.Fatal("R2 state stopped unrelated app routes", err)
			}
			if time.Since(started) > 10*time.Second {
				t.Fatal("startup waited on R2")
			}
		})
	}
}

func TestFileMigrationDryRunIsReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	db, err := openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	db.Close()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := filesMigrateCmd([]string{"-source", "local"}, env(map[string]string{"DB_PATH": path, "FILE_STORAGE": "r2"}), &out); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("dry-run changed database", err)
	}
}
