package store

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

func TestContactSpeakingHistoryMigration(t *testing.T) {
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
		if entry.Name() >= "0011" {
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
	must(`INSERT INTO users(id,discord_id,name,role) VALUES('host','123','Host','leadership')`)
	must(`INSERT INTO events(id,name,starts_at,host_user_id) VALUES('past','Past',strftime('%Y-%m-%dT%H:%M:%SZ','now','-1 day'),'host'),('future','Future',strftime('%Y-%m-%dT%H:%M:%SZ','now','+1 day'),'host')`)
	for _, id := range []string{"past", "future", "none", "both"} {
		must(`INSERT INTO contacts(id,name,type,created_at,updated_at) VALUES(?,?,'speaker',0,0)`, id, id)
	}
	must(`INSERT INTO contact_events VALUES('past','past'),('future','future'),('both','past'),('both','future')`)
	if err := migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"past", "future", "none", "both"} {
		var spoken bool
		if err := db.QueryRow(`SELECT has_spoken_for_us FROM contacts WHERE id=?`, id).Scan(&spoken); err != nil {
			t.Fatal(err)
		}
		if spoken != (id == "past" || id == "both") {
			t.Fatalf("%s spoken=%v", id, spoken)
		}
	}
	must(`UPDATE contacts SET has_spoken_for_us=0 WHERE id='past'`)
	if err := migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	var spoken bool
	if err := db.QueryRow(`SELECT has_spoken_for_us FROM contacts WHERE id='past'`).Scan(&spoken); err != nil || spoken {
		t.Fatalf("migration overwrote manual edit: %v %v", spoken, err)
	}
	if _, err := db.Exec(`UPDATE contacts SET has_spoken_for_us=2`); err == nil {
		t.Fatal("boolean constraint missing")
	}
}
