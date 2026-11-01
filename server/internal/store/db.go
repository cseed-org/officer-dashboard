// Package store owns every SQL statement. The rest of the program talks to it through
// typed methods so another SQL engine could replace SQLite later; nothing here is built
// for that today.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"runtime"
	"sort"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3" // SQLite via cgo: the faster option for a single box
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// DB is one SQLite file opened twice: a single writer connection (so writes are serialised
// by the connection itself, not by lock errors) and a pool of read-only connections.
type DB struct {
	W    *sql.DB
	R    *sql.DB
	Path string
}

func dsn(path string, params url.Values) string {
	return "file:" + path + "?" + params.Encode()
}

// Open opens (creating if needed) the database in WAL mode, applies pending migrations and
// returns the writer and reader handles.
func Open(path string) (*DB, error) {
	common := func(p url.Values) url.Values {
		p.Set("_busy_timeout", "5000")
		p.Set("_foreign_keys", "on")
		return p
	}
	wp := common(url.Values{})
	wp.Set("_journal_mode", "WAL")
	wp.Set("_synchronous", "NORMAL")
	wp.Set("_txlock", "immediate")
	w, err := sql.Open("sqlite3", dsn(path, wp))
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	w.SetMaxIdleConns(1)
	if err := w.Ping(); err != nil {
		w.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if err := migrate(context.Background(), w); err != nil {
		w.Close()
		return nil, err
	}
	rp := common(url.Values{})
	rp.Set("mode", "ro")
	r, err := sql.Open("sqlite3", dsn(path, rp))
	if err != nil {
		w.Close()
		return nil, err
	}
	n := runtime.NumCPU() * 2
	if n < 4 {
		n = 4
	}
	r.SetMaxOpenConns(n)
	r.SetMaxIdleConns(n)
	return &DB{W: w, R: r, Path: path}, nil
}

// Close checkpoints the WAL and closes both pools.
func (d *DB) Close() {
	_, _ = d.W.Exec(`PRAGMA optimize`)
	_ = d.R.Close()
	_ = d.W.Close()
}

// Ping checks that the database answers.
func (d *DB) Ping(ctx context.Context) error {
	var one int
	return d.R.QueryRowContext(ctx, `SELECT 1`).Scan(&one)
}

func migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		return err
	}
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		var done int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE name = ?`, name).Scan(&done); err != nil {
			return err
		}
		if done > 0 {
			continue
		}
		raw, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		for i, stmt := range strings.Split(string(raw), "--> statement-breakpoint") {
			if strings.TrimSpace(stmt) == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("migration %s statement %d: %w", name, i+1, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (name, applied_at) VALUES (?, ?)`, name, time.Now().UnixMilli()); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	// 0003 rebuilds a table with foreign_keys=OFF, which SQLite ignores inside a transaction;
	// verify nothing was left dangling.
	rows, err := db.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return errors.New("foreign_key_check failed after migrations")
	}
	return rows.Err()
}

// Q is what both *sql.DB and *sql.Tx offer; store methods run on either.
type Q interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	PrepareContext(ctx context.Context, query string) (*sql.Stmt, error)
}

// Queries is the typed method set, bound to the reader pool or to one write transaction.
type Queries struct{ q Q }

// Read returns queries bound to the reader pool.
func (d *DB) Read() *Queries { return &Queries{q: d.R} }

// Write runs fn in one IMMEDIATE transaction on the single writer connection and returns
// fn's error after rolling back; a nil error commits.
func (d *DB) Write(ctx context.Context, fn func(w *Queries) error) error {
	tx, err := d.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(&Queries{q: tx}); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// ErrNotFound is returned for a missing row.
var ErrNotFound = errors.New("not found")

func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// Exec runs a raw statement. It exists for the seed command, which loads fixtures;
// application code uses the typed methods.
func (q *Queries) Exec(ctx context.Context, query string, args ...any) error {
	_, err := q.q.ExecContext(ctx, query, args...)
	return err
}
