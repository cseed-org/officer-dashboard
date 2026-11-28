package api

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// raised returns the real driver error a trigger's RAISE(ABORT, msg) produces.
func raised(t *testing.T, msg string) error {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE t (v INTEGER); CREATE TRIGGER g BEFORE INSERT ON t BEGIN SELECT RAISE(ABORT, '` + msg + `'); END;`); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO t VALUES (1)`)
	if err == nil {
		t.Fatal("trigger did not fire")
	}
	return fmt.Errorf("wrapped: %w", err)
}

// constraintErr returns the real driver error the statement raises after setup.
func constraintErr(t *testing.T, setup, stmt string) error {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(setup); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(stmt); err == nil {
		t.Fatal("statement did not fail")
	}
	return fmt.Errorf("wrapped: %w", err)
}

// M-109: only SQLite constraint failures (and the engine's own open-period refusal) are 409s;
// an error that merely mentions "closed" or "constraint" is a server fault.
func TestToAPIErrorMatchesConstraintCodeNotText(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cases := []struct {
		name   string
		err    error
		status int
		msg    string
	}{
		{"closed network connection", errors.New("write tcp 10.0.0.1:443: use of closed network connection"), 500, "Operation failed"},
		{"server closed idle", errors.New("http: server closed idle connection"), 500, "Operation failed"},
		{"database closed", errors.New("sql: database is closed"), 500, "Operation failed"},
		{"rows closed", errors.New("sql: Rows are closed"), 500, "Operation failed"},
		{"constraint text without sqlite error", errors.New("NOT NULL constraint failed: users.name"), 500, "Operation failed"},
		{"closed period trigger", raised(t, "closed period cannot reopen or change dates"), 409, "Conflicting state, closed period, or duplicate record"},
		{"last director", raised(t, "cannot remove last active director"), 409, "Cannot remove the last active director"},
		{"last owner", raised(t, "cannot remove last owner"), 409, "Cannot remove the last owner"},
		{"access email trigger", raised(t, "access email belongs to a pending invite"), 409, "That access email is already in use by another person or pending invite."},
		{"access email unique index", constraintErr(t, `CREATE TABLE users (access_email TEXT); CREATE UNIQUE INDEX u ON users(access_email); INSERT INTO users VALUES ('a@b.org')`, `INSERT INTO users VALUES ('a@b.org')`), 409, "That access email is already in use by another person or pending invite."},
		{"access email check", constraintErr(t, `CREATE TABLE users (access_email TEXT CHECK(access_email = lower(access_email)))`, `INSERT INTO users VALUES ('A@b.org')`), 400, "That access email isn't valid: use a lower-case address with an @."},
		{"engine open period", errors.New("entry requires an open period"), 409, "Conflicting state, closed period, or duplicate record"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := toAPIError(c.err, log)
			if got.Status != c.status || got.Message != c.msg {
				t.Fatalf("toAPIError(%q) = %d %q, want %d %q", c.err, got.Status, got.Message, c.status, c.msg)
			}
		})
	}
}

// M-109 (review R-should-fix): real driver errors. NOT NULL and CHECK failures are handler
// bugs (input is validated before writing), so they answer 500; uniqueness and foreign-key
// failures stay 409 conflicts.
func TestReviewRealSQLiteProgrammingConstraintsAreServerErrors(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:?_foreign_keys=1")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE parent(id TEXT PRIMARY KEY);
		CREATE TABLE probe(v TEXT NOT NULL CHECK(length(v)>1) UNIQUE, p TEXT REFERENCES parent(id));
		INSERT INTO probe(v) VALUES('taken')`); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, c := range []struct {
		query  string
		status int
	}{
		{`INSERT INTO probe(v) VALUES(NULL)`, 500},
		{`INSERT INTO probe(v) VALUES('x')`, 500},
		{`INSERT INTO probe(v) VALUES('taken')`, 409},
		{`INSERT INTO probe(v,p) VALUES('orphan','missing')`, 409},
	} {
		_, err := db.Exec(c.query)
		if err == nil {
			t.Fatalf("%s: expected a constraint error", c.query)
		}
		if got := toAPIError(fmt.Errorf("wrapped: %w", err), log); got.Status != c.status {
			t.Errorf("%s: real error %v => HTTP %d, want %d", c.query, err, got.Status, c.status)
		}
	}
}
