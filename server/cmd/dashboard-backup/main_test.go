//go:build linux

package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"filippo.io/age"
	"officerdashboard/internal/store"
)

// This helper is compiled only into the isolated rehearsal image, never the release.
func TestRehearsalHelper(t *testing.T) {
	mode := os.Getenv("BACKUP_REHEARSAL_MODE")
	if mode == "" {
		t.Skip("isolated rehearsal helper")
	}
	switch mode {
	case "recipient":
		identity, err := age.GenerateX25519Identity()
		if err != nil {
			t.Fatal(err)
		}
		fmt.Println(identity.Recipient())
	case "prepare", "set", "probe":
		db, err := store.Open(dbPath())
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if mode == "probe" {
			var version string
			if err = db.R.QueryRow("SELECT value FROM rollback_probe").Scan(&version); err != nil {
				t.Fatal(err)
			}
			fmt.Println(version)
			return
		}
		if _, err = db.W.Exec("CREATE TABLE IF NOT EXISTS rollback_probe(value TEXT); DELETE FROM rollback_probe;"); err != nil {
			t.Fatal(err)
		}
		if _, err = db.W.Exec("INSERT INTO rollback_probe VALUES(?)", os.Getenv("BACKUP_REHEARSAL_VERSION")); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("unknown helper mode")
	}
}

func TestLocalSinkRoundTrip(t *testing.T) {
	if os.Getenv("BACKUP_REHEARSAL_MODE") != "roundtrip" {
		t.Skip("requires local Wrangler")
	}
	source := dbPath()
	db, err := sql.Open("sqlite3", source)
	if err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 2100000)
	if _, err = rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("CREATE TABLE records(id INTEGER PRIMARY KEY,value BLOB); INSERT INTO records(value) VALUES(?)", payload); err != nil {
		t.Fatal(err)
	}
	db.Close()
	reference := filepath.Join(filepath.Dir(source), "reference.db")
	if err = copyDatabase(source, reference); err != nil {
		t.Fatal(err)
	}
	if err = run([]string{"backup"}); err != nil {
		t.Fatal(err)
	}
	s, err := newSink()
	if err != nil {
		t.Fatal(err)
	}
	all, err := s.list()
	if err != nil || len(all) != 1 {
		t.Fatalf("expected one committed snapshot: %v", err)
	}
	if all[0].Chunks != 2 {
		t.Fatalf("expected two chunks, got %d", all[0].Chunks)
	}
	if err = s.download(all[0], filepath.Join(filepath.Dir(source), "downloaded.age")); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(all[0])
	if err = os.WriteFile(filepath.Join(filepath.Dir(source), "snapshot.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.request("/prune", "POST", map[string]int{"hourly": 24}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestStoppedCheckpointRestoresPreMigrationDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dashboard.db")
	t.Setenv("DB_PATH", path)
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("CREATE TABLE records(value TEXT); INSERT INTO records VALUES('before')"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err = run([]string{"checkpoint"}); err != nil {
		t.Fatal(err)
	}
	db, _ = sql.Open("sqlite3", path)
	if _, err = db.Exec("UPDATE records SET value='after'"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err = run([]string{"rollback"}); err != nil {
		t.Fatal(err)
	}
	db, _ = sql.Open("sqlite3", path)
	defer db.Close()
	var result string
	if err = db.QueryRow("SELECT value FROM records").Scan(&result); err != nil {
		t.Fatal(err)
	}
	if result != "before" {
		t.Fatalf("rollback retained migrated data: %q", result)
	}
	if err = integrity(path); err != nil {
		t.Fatal(err)
	}
}
