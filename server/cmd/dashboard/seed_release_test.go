//go:build release

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

const testDBCommand = "migrate"

func TestReleaseCannotSeed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dashboard.db")
	var out bytes.Buffer
	if code := run([]string{"seed", "-force", "-reset"}, env(map[string]string{"DB_PATH": path}), &out, &out); code == 0 {
		t.Fatal("release binary seeded a database")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("release seed command touched the database")
	}
	// Even explicitly destructive flags cannot overwrite an existing volume.
	const content = "existing database sentinel"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"seed", "-force", "-reset"}, env(map[string]string{"DB_PATH": path}), &out, &out); code == 0 {
		t.Fatal("release binary accepted destructive seeding")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != content {
		t.Fatal("release seed command modified existing data")
	}
}
