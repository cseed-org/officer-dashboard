package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

func TestV03CalendarDatabaseGuards(t *testing.T) {
	x := newTDB(t)
	for i := 0; i < 5; i++ {
		x.must(`INSERT INTO calendar_emails(id,user_id,email,active) VALUES(?,'u1',?,?)`, fmt.Sprint(i), fmt.Sprintf("email%d@example.org", i), i == 0)
	}
	x.rejects("at most 5", `INSERT INTO calendar_emails(id,user_id,email) VALUES('six','u1','six@example.org')`)
	x.rejects("UNIQUE", `UPDATE calendar_emails SET active=1 WHERE id='1'`)
	x.must(`INSERT INTO users(id,discord_id,name) VALUES('u2','100000000000000002','Other')`)
	x.must(`INSERT INTO calendar_emails(id,user_id,email) VALUES('other','u2','other@example.org')`)
	x.rejects("at most 5", `UPDATE calendar_emails SET user_id='u1' WHERE id='other'`)
}

func TestV03UpgradePreservesReferencesAndIntegrityTriggers(t *testing.T) {
	path := t.TempDir() + "/upgrade.db"
	db, err := sql.Open(driverName, "file:"+path+"?_foreign_keys=on")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	must := func(s string, args ...any) {
		t.Helper()
		if _, err := db.Exec(s, args...); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	must(`CREATE TABLE schema_migrations(name TEXT PRIMARY KEY,applied_at INTEGER NOT NULL)`)
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() >= "0005" {
			continue
		}
		raw, err := migrationFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range strings.Split(string(raw), "--> statement-breakpoint") {
			if strings.TrimSpace(s) != "" {
				must(s)
			}
		}
		must(`INSERT INTO schema_migrations VALUES(?,0)`, entry.Name())
	}
	must(`INSERT INTO users(id,discord_id,name,role) VALUES('boss','100000000000000001','Boss','director')`)
	must(`INSERT INTO sessions(id,user_id,expires_at) VALUES('session','boss',9999999999999)`)
	must(`INSERT INTO audit_log(id,actor_user_id,action,entity_type) VALUES('audit','boss','test','user')`)
	must(`INSERT INTO events(id,name,starts_at,host_user_id) VALUES('event','Test','2026-11-01T00:00:00Z','boss')`)
	db.Close()
	upgraded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	for _, table := range []string{"users", "sessions", "audit_log", "events"} {
		var n int
		if err := upgraded.R.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil || n != 1 {
			t.Fatalf("%s count %d: %v", table, n, err)
		}
	}
	if _, err := upgraded.W.Exec(`UPDATE users SET role='leadership' WHERE id='boss'`); err != nil {
		t.Fatal(err)
	}
	if _, err := upgraded.W.Exec(`UPDATE users SET active=0 WHERE id='boss'`); err == nil {
		t.Fatal("last admin guard lost")
	}
	if _, err := upgraded.W.Exec(`UPDATE audit_log SET action='changed'`); err == nil {
		t.Fatal("audit immutability lost")
	}
	var triggers int
	if err := upgraded.R.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='trigger' AND name LIKE 'journal_%'`).Scan(&triggers); err != nil || triggers < 10 {
		t.Fatalf("journal triggers %d: %v", triggers, err)
	}
	if _, err := upgraded.W.ExecContext(context.Background(), `INSERT INTO sessions(id,user_id,expires_at) VALUES('bad','missing',0)`); err == nil {
		t.Fatal("foreign key enforcement not restored")
	}
}
