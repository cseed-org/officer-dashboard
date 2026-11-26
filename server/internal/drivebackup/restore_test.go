package drivebackup

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"filippo.io/age"
	_ "github.com/mattn/go-sqlite3"

	"officerdashboard/internal/files/drivefake"
)

// encryptLikeSidecar gzip-compresses and age-encrypts exactly like the backup sidecar's
// encrypt(): a gzip stream inside the age stream.
func encryptLikeSidecar(t *testing.T, plaintext []byte, recipient age.Recipient) []byte {
	t.Helper()
	var out bytes.Buffer
	sealed, err := age.Encrypt(&out, recipient)
	if err != nil {
		t.Fatal(err)
	}
	zipped := gzip.NewWriter(sealed)
	if _, err := zipped.Write(plaintext); err != nil {
		t.Fatal(err)
	}
	if err := zipped.Close(); err != nil {
		t.Fatal(err)
	}
	if err := sealed.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestRestoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	// A real SQLite database, so the restored plaintext can be integrity-checked.
	source := filepath.Join(t.TempDir(), "source.db")
	db, err := sql.Open("sqlite3", source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE proof(value); INSERT INTO proof VALUES('present')"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	plaintext, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	ciphertext := encryptLikeSidecar(t, plaintext, identity.Recipient())
	sum := sha256.Sum256(ciphertext)
	sha := hex.EncodeToString(sum[:])

	fake := drivefake.New()
	drive := testDrive(fake)
	if _, err := drive.UploadStream(ctx, []string{"Backups", "daily"}, "dashboard-2026-11-20.db.gz.age", "application/octet-stream",
		bytes.NewReader(ciphertext), int64(len(ciphertext)),
		map[string]string{"sha256": sha, "size": strconv.Itoa(len(ciphertext)), "kind": "daily"}); err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(t.TempDir(), "restored.db.gz.age")
	b, err := Restore(ctx, drive, RestoreOptions{Latest: true, Kind: "daily", Output: output})
	if err != nil {
		t.Fatal(err)
	}
	if b.Entry.Name != "dashboard-2026-11-20.db.gz.age" {
		t.Fatalf("restored %s", b.Entry.Name)
	}
	// Decrypt offline with the identity, gunzip, and check SQLite integrity.
	f, err := os.Open(output)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	decrypted, err := age.Decrypt(f, identity)
	if err != nil {
		t.Fatal(err)
	}
	unzipped, err := gzip.NewReader(decrypted)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := io.ReadAll(unzipped)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restored, plaintext) {
		t.Fatal("round-trip content mismatch")
	}
	check := filepath.Join(t.TempDir(), "check.db")
	if err := os.WriteFile(check, restored, 0600); err != nil {
		t.Fatal(err)
	}
	cdb, err := sql.Open("sqlite3", check)
	if err != nil {
		t.Fatal(err)
	}
	defer cdb.Close()
	var result string
	if err := cdb.QueryRow("PRAGMA integrity_check").Scan(&result); err != nil {
		t.Fatal(err)
	}
	if result != "ok" {
		t.Fatalf("integrity check: %s", result)
	}
	var value string
	if err := cdb.QueryRow("SELECT value FROM proof").Scan(&value); err != nil || value != "present" {
		t.Fatalf("restored data: %q, %v", value, err)
	}
}

func TestRestoreRejectsShaMismatch(t *testing.T) {
	ctx := context.Background()
	ciphertext := []byte("ciphertext bytes")
	fake := drivefake.New()
	backups := fake.Add(drivefake.File{Name: "Backups", MimeType: drivefake.FolderMIME})
	dailyID := fake.Add(drivefake.File{Name: "daily", MimeType: drivefake.FolderMIME, Parent: backups})
	// The stored sha does not match the stored bytes: fail closed.
	fake.Add(drivefake.File{Name: "dashboard-2026-11-20.db.gz.age", Parent: dailyID, Body: ciphertext,
		AppProperties: map[string]string{"sha256": strings.Repeat("0", 64), "kind": "daily"}})
	output := filepath.Join(t.TempDir(), "out.age")
	if _, err := Restore(ctx, testDrive(fake), RestoreOptions{Latest: true, Kind: "daily", Output: output}); err == nil {
		t.Fatal("sha mismatch accepted")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("mismatched download published")
	}
	// A missing sha is refused the same way.
	fake.Add(drivefake.File{Name: "dashboard-2026-11-21.db.gz.age", Parent: dailyID, Body: ciphertext})
	if _, err := Restore(ctx, testDrive(fake), RestoreOptions{Latest: true, Kind: "daily", Output: output}); err == nil {
		t.Fatal("missing sha accepted")
	}
}

func TestRestoreSelectionAndNoOverwrite(t *testing.T) {
	ctx := context.Background()
	fake := drivefake.New()
	drive := testDrive(fake)
	put := func(folder []string, name string, body string) {
		t.Helper()
		sum := sha256.Sum256([]byte(body))
		if _, err := drive.UploadStream(ctx, folder, name, "application/octet-stream",
			strings.NewReader(body), int64(len(body)),
			map[string]string{"sha256": hex.EncodeToString(sum[:]), "kind": folder[1]}); err != nil {
			t.Fatal(err)
		}
	}
	put([]string{"Backups", "daily"}, "dashboard-2026-11-19.db.gz.age", "older daily")
	put([]string{"Backups", "daily"}, "dashboard-2026-11-20.db.gz.age", "newer daily")
	put([]string{"Backups", "monthly"}, "dashboard-2026-11.db.gz.age", "monthly copy")

	dir := t.TempDir()
	// --latest picks the newest daily by name.
	latest, err := Restore(ctx, drive, RestoreOptions{Latest: true, Kind: "daily", Output: filepath.Join(dir, "latest.age")})
	if err != nil || latest.Entry.Name != "dashboard-2026-11-20.db.gz.age" {
		t.Fatalf("latest: %+v, %v", latest.Entry.Name, err)
	}
	// --name picks an exact file.
	named, err := Restore(ctx, drive, RestoreOptions{Name: "dashboard-2026-11-19.db.gz.age", Kind: "daily", Output: filepath.Join(dir, "named.age")})
	if err != nil || named.Entry.Name != "dashboard-2026-11-19.db.gz.age" {
		t.Fatalf("named: %+v, %v", named.Entry.Name, err)
	}
	// Monthly restores come from the monthly folder.
	monthly, err := Restore(ctx, drive, RestoreOptions{Latest: true, Kind: "monthly", Output: filepath.Join(dir, "monthly.age")})
	if err != nil || monthly.Entry.Name != "dashboard-2026-11.db.gz.age" {
		t.Fatalf("monthly: %+v, %v", monthly.Entry.Name, err)
	}
	// List shows oldest first and only the matching kind.
	dailies, err := List(ctx, drive, "daily")
	if err != nil || len(dailies) != 2 || dailies[0].Entry.Name != "dashboard-2026-11-19.db.gz.age" {
		t.Fatalf("list: %+v, %v", dailies, err)
	}
	// An existing output is never replaced, and name+latest together is an error.
	if _, err := Restore(ctx, drive, RestoreOptions{Latest: true, Kind: "daily", Output: filepath.Join(dir, "latest.age")}); err == nil {
		t.Fatal("existing output overwritten")
	}
	if _, err := Restore(ctx, drive, RestoreOptions{Name: "x", Latest: true, Kind: "daily", Output: filepath.Join(dir, "other.age")}); err == nil {
		t.Fatal("name and latest accepted together")
	}
	if _, err := Restore(ctx, drive, RestoreOptions{Name: "dashboard-1999-01-01.db.gz.age", Kind: "daily", Output: filepath.Join(dir, "missing.age")}); err == nil {
		t.Fatal("unknown name accepted")
	}
	if _, err := Restore(ctx, drive, RestoreOptions{Latest: true, Kind: "weekly", Output: filepath.Join(dir, "kind.age")}); err == nil {
		t.Fatal("unknown kind accepted")
	}
	// Entries with a different sha under the same name restore their own bytes.
	body, err := os.ReadFile(filepath.Join(dir, "monthly.age"))
	if err != nil || string(body) != "monthly copy" {
		t.Fatalf("monthly content: %q, %v", body, err)
	}
}
