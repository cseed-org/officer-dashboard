package store

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// M-071: DB_PATH is a literal filename. Characters SQLite's URI parser reads specially must
// not change which file opens or drop the reader pool's mode=ro.
func TestOpenUsesLiteralPath(t *testing.T) {
	for _, name := range []string{"go#literal.db", "go%20literal.db", "go%literal.db", "go?x=1.db"} {
		t.Run(name, func(t *testing.T) {
			if name == "go?x=1.db" && runtime.GOOS == "windows" {
				t.Skip("'?' is not a valid Windows filename character")
			}
			dir := t.TempDir()
			path := filepath.Join(dir, name)
			db, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if _, err := db.W.ExecContext(ctx, `CREATE TABLE probe (v TEXT)`); err != nil {
				t.Fatal(err)
			}
			if _, err := db.W.ExecContext(ctx, `INSERT INTO probe VALUES ('kept')`); err != nil {
				t.Fatal(err)
			}
			var file string
			if err := db.R.QueryRowContext(ctx, `SELECT file FROM pragma_database_list WHERE name='main'`).Scan(&file); err != nil {
				t.Fatal(err)
			}
			want, _ := filepath.Abs(path)
			if filepath.Clean(file) != filepath.Clean(want) {
				t.Fatalf("database_list file = %q, want %q", file, want)
			}
			if _, err := db.R.ExecContext(ctx, `INSERT INTO probe VALUES ('reader')`); err == nil {
				t.Fatal("reader pool accepted a write; mode=ro was lost")
			}
			db.Close()
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("literal file missing: %v", err)
			}
			again, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer again.Close()
			var v string
			if err := again.R.QueryRowContext(ctx, `SELECT v FROM probe`).Scan(&v); err != nil || v != "kept" {
				t.Fatalf("reopen: v=%q err=%v", v, err)
			}
		})
	}
}

// M-071: relative paths keep working.
func TestOpenRelativePath(t *testing.T) {
	dir := t.TempDir()
	wd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	db, err := Open("rel#1.db")
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := os.Stat(filepath.Join(dir, "rel#1.db")); err != nil {
		t.Fatal(err)
	}
}
