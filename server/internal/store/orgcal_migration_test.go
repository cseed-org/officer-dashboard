package store

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

func TestOrgCalendarUpgradePreservesEventsAndReferences(t *testing.T) {
	path := t.TempDir() + "/upgrade.db"
	db, err := sql.Open(driverName, "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	must := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	must(`CREATE TABLE schema_migrations(name TEXT PRIMARY KEY,applied_at INTEGER NOT NULL)`)
	entries, _ := migrationFS.ReadDir("migrations")
	for _, e := range entries {
		if e.Name() >= "0031" {
			continue
		}
		raw, _ := migrationFS.ReadFile("migrations/" + e.Name())
		for _, s := range strings.Split(string(raw), "--> statement-breakpoint") {
			if strings.TrimSpace(s) != "" {
				must(s)
			}
		}
		must(`INSERT INTO schema_migrations VALUES(?,0)`, e.Name())
	}
	must(`INSERT INTO events(id,name,starts_at) VALUES('e','Preserved','2026-11-21T18:00:00Z')`)
	must(`INSERT INTO event_covers VALUES('e','image/png',x'0001','cover',1)`)
	if err := migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	var name string
	if err := db.QueryRow(`SELECT name FROM events JOIN event_covers ON event_covers.event_id=events.id`).Scan(&name); err != nil || name != "Preserved" {
		t.Fatalf("lost event/cover: %s %v", name, err)
	}
	must(`INSERT INTO events(id,name,starts_at,tentative_from,tentative_to) VALUES('tentative','Tentative',NULL,'2026-11-21','2026-11-28')`)
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil || n != MigrationCount() {
		t.Fatalf("migration count %d: %v", n, err)
	}
	if err := migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
}
