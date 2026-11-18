package store

import (
	"database/sql"
	"path/filepath"
	"regexp"
	"testing"
)

func TestMigratedSchemaHasNoAppFunctions(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "schema.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.R.Query(`SELECT type, name, sql FROM sqlite_schema WHERE sql IS NOT NULL`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	appFunction := regexp.MustCompile(`(?i)\b(fold|instant_[a-z0-9_]+)\s*\(`)
	for rows.Next() {
		var kind, name, definition string
		if err := rows.Scan(&kind, &name, &definition); err != nil {
			t.Fatal(err)
		}
		if appFunction.MatchString(definition) {
			t.Errorf("%s %s depends on an app function: %s", kind, name, definition)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestMigratedDatabaseWorksWithoutAppFunctions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backup.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// Populate events before closing the application's connection.
	_, err = db.W.Exec(`INSERT INTO events(id,name,starts_at) VALUES('existing','Existing event','2026-11-18T12:00:00Z')`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	// The backup script and manual SQLite clients have none of our ConnectHook functions.
	plain, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	checkIntegrity := func(t *testing.T) {
		t.Helper()
		var result string
		if err := plain.QueryRow(`PRAGMA integrity_check`).Scan(&result); err != nil {
			t.Fatal(err)
		}
		if result != "ok" {
			t.Fatalf("integrity_check: %s", result)
		}
	}
	t.Run("backup integrity check", checkIntegrity)
	t.Run("manual event insert", func(t *testing.T) {
		if _, err := plain.Exec(`INSERT INTO events(id,name,starts_at) VALUES('manual','Manual event','2026-11-19T12:00:00Z')`); err != nil {
			t.Fatal(err)
		}
		checkIntegrity(t)
	})
}

func TestAuditCursorIndexReplacesCreatedIndex(t *testing.T) {
	x := newTDB(t)
	var redundant int
	if err := x.db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type='index' AND name='audit_created_idx'`).Scan(&redundant); err != nil {
		t.Fatal(err)
	}
	if redundant != 0 {
		t.Fatal("audit inserts still maintain the redundant created_at index")
	}
	if plan := explain(t, x.db, `SELECT id FROM audit_log ORDER BY created_at DESC,id DESC LIMIT 51`); !regexp.MustCompile(`\baudit_created_id_idx\b`).MatchString(plan) {
		t.Fatal(plan)
	}
}
