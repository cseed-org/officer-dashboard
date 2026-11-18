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
