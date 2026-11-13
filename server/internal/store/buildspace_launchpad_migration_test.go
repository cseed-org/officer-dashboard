package store

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

func TestBuildspaceLaunchpadMigrationPreservesData(t *testing.T) {
	path := t.TempDir() + "/upgrade.db"
	db, err := sql.Open(driverName, "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	must := func(s string, args ...any) {
		t.Helper()
		if _, err := db.Exec(s, args...); err != nil {
			t.Fatal(err)
		}
	}
	must(`CREATE TABLE schema_migrations(name TEXT PRIMARY KEY,applied_at INTEGER NOT NULL)`)
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() >= "0023" {
			continue
		}
		raw, err := migrationFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, stmt := range strings.Split(string(raw), "--> statement-breakpoint") {
			if strings.TrimSpace(stmt) != "" {
				must(stmt)
			}
		}
		must(`INSERT INTO schema_migrations VALUES(?,0)`, entry.Name())
	}

	must(`INSERT INTO buildspace_projects VALUES('p','Existing project','keep','Software','Building','[]',123)`)
	must(`INSERT INTO buildspace_people VALUES('b','Builder',NULL,'discord','p','2026-11-01')`)
	must(`INSERT INTO buildspace_activity VALUES('a','p','b',NULL,'weekly','2026-11-02','Progress','on-track',NULL,'manual','external',123)`)
	if err := migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	var name, body string
	if err := db.QueryRow(`SELECT p.name,a.body FROM buildspace_launchpad_projects p JOIN buildspace_launchpad_people b ON b.project_id=p.id JOIN buildspace_launchpad_activity a ON a.person_id=b.id`).Scan(&name, &body); err != nil {
		t.Fatal(err)
	}
	if name != "Existing project" || body != "Progress" {
		t.Fatalf("data changed: %q %q", name, body)
	}
	if err := migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("foreign key violation")
	}
}
