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
	"officerdashboard/internal/luma"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-sqlite3" // SQLite via cgo: the faster option for a single box
)

// Fold lower-cases text for case-insensitive matching. SQLite's own lower() folds only
// ASCII, so searches use this Unicode-aware function (registered as fold()) and fold the
// needle with the same code.
func Fold(s string) string { return strings.ToLower(s) }

const driverName = "sqlite3_fold"

func init() {
	sql.Register(driverName, &sqlite3.SQLiteDriver{ConnectHook: func(c *sqlite3.SQLiteConn) error {
		if err := c.RegisterFunc("instant_ms", func(s string) any {
			t, err := luma.ParseInstant(s)
			if err != nil {
				return nil
			}
			return t.UnixMilli()
		}, true); err != nil {
			return err
		}
		for name, field := range map[string]func(time.Time) int64{
			"instant_seconds":  func(t time.Time) int64 { return t.Unix() },
			"instant_fraction": func(t time.Time) int64 { return int64(t.Nanosecond()) },
		} {
			if err := c.RegisterFunc(name, func(s string) any {
				t, err := luma.ParseInstant(s)
				if err != nil {
					return nil
				}
				return field(t)
			}, true); err != nil {
				return err
			}
		}

		return c.RegisterFunc("fold", func(s string) string { return Fold(s) }, true)
	}})
}

//go:embed migrations/*.sql
var migrationFS embed.FS

// DB is one SQLite file opened twice: a single writer connection (so writes are serialised
// by the connection itself, not by lock errors) and a pool of read-only connections.
type DB struct {
	W     *sql.DB
	R     *sql.DB
	Path  string
	cache *queryCache
}

// queryCache stores only statements explicitly selected by read models. Their
// query strings are fixed code, never request input; arbitrary filters stay uncached.
type queryCache struct {
	mu    sync.Mutex
	stmts map[string]*sql.Stmt
}

// uriPath escapes the characters SQLite's URI parser treats specially ('%' starts an escape,
// '?' the query, '#' a fragment), so a literal filesystem path such as "go#1.db" opens that
// file and keeps the query parameters (mode=ro on the reader pool). Every other character,
// including Windows drive letters and separators, passes through unchanged.
var uriPath = strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23")

func dsn(path string, params url.Values) string {
	return "file:" + uriPath.Replace(path) + "?" + params.Encode()
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
	w, err := sql.Open(driverName, dsn(path, wp))
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
	r, err := sql.Open(driverName, dsn(path, rp))
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
	return &DB{W: w, R: r, Path: path, cache: &queryCache{stmts: map[string]*sql.Stmt{}}}, nil
}

// Close checkpoints the WAL and closes both pools.
func (d *DB) Close() {
	_, _ = d.W.Exec(`PRAGMA optimize`)
	if d.cache != nil {
		d.cache.mu.Lock()
		for _, stmt := range d.cache.stmts {
			_ = stmt.Close()
		}
		// Keep the map usable: a reader finishing a concurrent prepare may still
		// insert its statement before the reader pool closes.
		d.cache.mu.Unlock()
	}
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
	// Restore enforcement even if a migration fails. Open uses one writer connection.
	defer db.ExecContext(context.Background(), `PRAGMA foreign_keys=ON`)
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
		// Table rebuilds must disable FK enforcement outside the transaction. The
		// single writer connection prevents another operation from borrowing it.
		if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
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
		check, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		broken := check.Next()
		checkErr := check.Err()
		check.Close()
		if broken || checkErr != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %s: foreign_key_check failed: %v", name, checkErr)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (name, applied_at) VALUES (?, ?)`, name, time.Now().UnixMilli()); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys=ON`); err != nil {
		return err
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
type Queries struct {
	q     Q
	cache *queryCache
}

// query reuses selected read-only statements. Transaction-bound Queries have no
// cache, keeping every statement on that transaction's snapshot/connection.
func (q *Queries) query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if q.cache == nil {
		return q.q.QueryContext(ctx, query, args...)
	}
	q.cache.mu.Lock()
	stmt := q.cache.stmts[query]
	var err error
	if stmt == nil {
		stmt, err = q.q.PrepareContext(ctx, query)
		if err == nil {
			q.cache.stmts[query] = stmt
		}
	}
	q.cache.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return stmt.QueryContext(ctx, args...)
}

func (q *Queries) queryRow(ctx context.Context, query string, args ...any) *sql.Row {
	if q.cache == nil {
		return q.q.QueryRowContext(ctx, query, args...)
	}
	q.cache.mu.Lock()
	stmt := q.cache.stmts[query]
	var err error
	if stmt == nil {
		stmt, err = q.q.PrepareContext(ctx, query)
		if err == nil {
			q.cache.stmts[query] = stmt
		}
	}
	q.cache.mu.Unlock()
	if err != nil {
		return q.q.QueryRowContext(ctx, query, args...)
	}
	return stmt.QueryRowContext(ctx, args...)
}

// Read returns queries bound to the reader pool.
func (d *DB) Read() *Queries { return &Queries{q: d.R, cache: d.cache} }

// ReadTx runs fn in one read transaction on the reader pool: in WAL mode every query fn
// makes sees the same snapshot, without taking the single writer connection.
func (d *DB) ReadTx(ctx context.Context, fn func(r *Queries) error) error {
	tx, err := d.R.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return fn(&Queries{q: tx})
}

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

// IsConflict reports whether err is SQLite refusing a write because of the data already
// there: a UNIQUE/PRIMARY KEY/FOREIGN KEY constraint or a trigger's RAISE(ABORT), which
// SQLite reports as SQLITE_CONSTRAINT. NOT NULL and CHECK failures are left out: handlers
// validate their input before writing, so one of those is a server bug, not a conflict the
// caller can resolve. It matches the driver's (extended) error code, not its text, so a
// network or "database is closed" error that happens to contain such words never matches.
func IsConflict(err error) bool {
	var se sqlite3.Error
	if !errors.As(err, &se) || se.Code != sqlite3.ErrConstraint {
		return false
	}
	switch se.ExtendedCode {
	case sqlite3.ErrConstraintNotNull, sqlite3.ErrConstraintCheck:
		return false
	}
	return true
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

// On binds typed queries to an explicit read transaction.
func On(q Q) *Queries { return &Queries{q: q} }
