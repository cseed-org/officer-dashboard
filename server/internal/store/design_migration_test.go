package store

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

// Upgrading a populated board must preserve both kinds of dependent records.
func TestDesignStaffMoveMigration(t *testing.T) {
	db, err := sql.Open(driverName, "file:"+t.TempDir()+"/upgrade.db?_foreign_keys=on")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	must := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	must(`CREATE TABLE schema_migrations(name TEXT PRIMARY KEY,applied_at INTEGER NOT NULL)`)
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() >= "0014" {
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
	must(`INSERT INTO users(id,discord_id,name,role) VALUES('u','123','Requester','officer')`)
	must(`INSERT INTO design_requests(id,title,type,brief,requester_id,created_at,updated_at) VALUES('r','Poster','poster','Brief','u',0,0)`)
	must(`INSERT INTO design_request_comments(id,request_id,author_id,body,created_at) VALUES('c','r','u','Keep me',0)`)
	must(`INSERT INTO design_request_files(id,request_id,kind,filename,mime_type,size_bytes,storage,storage_key,uploaded_by,created_at) VALUES('f','r','reference','logo.png','image/png',8,'local','logo','u',0)`)
	if err := migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"design_request_comments", "design_request_files"} {
		var n int
		if err := db.QueryRow("SELECT count(*) FROM " + table + " WHERE request_id='r'").Scan(&n); err != nil || n != 1 {
			t.Fatalf("lost %s: %d %v", table, n, err)
		}
	}
	must(`UPDATE design_requests SET stage='in-progress' WHERE id='r'`)
	must(`UPDATE design_requests SET stage='delivered' WHERE id='r'`)
	must(`DELETE FROM design_requests WHERE id='r'`)
	for _, table := range []string{"design_request_comments", "design_request_files"} {
		var n int
		if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("cascade broken for %s: %d %v", table, n, err)
		}
	}
}
