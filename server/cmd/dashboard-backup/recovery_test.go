//go:build linux

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"filippo.io/age"
)

func TestRecoveryVerifiesExistingArtifactAndPublishesOnlyCompleteDownload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.db")
	t.Setenv("DB_PATH", path)
	t.Setenv("BACKUP_BEARER_SECRET", "synthetic-test-secret")
	data := []byte("complete ciphertext")
	hash := sha256.Sum256(data)
	m := snapshot{ID: "test", Kind: "hourly", Size: int64(len(data)), Chunks: 1, SHA256: hex.EncodeToString(hash[:])}
	var broken atomic.Bool
	broken.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/snapshots" {
			json.NewEncoder(w).Encode([]snapshot{m})
			return
		}
		if broken.Load() {
			fmt.Fprint(w, "truncated")
			return
		}
		w.Write(data)
	}))
	defer server.Close()
	t.Setenv("BACKUP_SINK_URL", server.URL)
	if err := loadSecretFiles(); err != nil {
		t.Fatal(err)
	}
	output := path + ".recovery.age"
	if err := locked(scheduled); err == nil {
		t.Fatal("truncated download accepted")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("incomplete file published")
	}
	// Simulate an artifact left by the old entry point and an abandoned temp download.
	os.WriteFile(output, nil, 0600)
	os.WriteFile(filepath.Join(filepath.Dir(path), ".dashboard-download-1234"), []byte("partial"), 0600)
	broken.Store(false)
	if err := locked(scheduled); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(output)
	if string(body) != string(data) {
		t.Fatal("old artifact not replaced")
	}
	// An existing matching artifact needs no chunks, but must be rechecked against D1.
	broken.Store(true)
	if err := locked(scheduled); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".dashboard-download-*"))
	if len(files) != 0 {
		t.Fatalf("download leftovers: %v", files)
	}
}

func TestDownloadDoesNotReplaceConcurrentDestination(t *testing.T) {
	target := filepath.Join(t.TempDir(), "output.age")
	hash := sha256.Sum256([]byte("abc"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		os.WriteFile(target, []byte("competing"), 0600)
		fmt.Fprint(w, "abc")
	}))
	defer server.Close()
	s := &sink{server.URL, server.Client()}
	if err := s.download(snapshot{ID: "test", Chunks: 1, Size: 3, SHA256: hex.EncodeToString(hash[:])}, target); err == nil {
		t.Fatal("competing output replaced")
	}
	body, _ := os.ReadFile(target)
	if string(body) != "competing" {
		t.Fatal("competing output modified")
	}
}

func TestRelativeDatabasePathsAndCheckpointPreservation(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("DB_PATH", "./relative%20?#.db")
	db, err := openDB(dbPath(), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("CREATE TABLE proof(value); INSERT INTO proof VALUES('before')"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err = run([]string{"checkpoint"}); err != nil {
		t.Fatal(err)
	}
	before, _, _ := digest(".update-before.db")
	if err = run([]string{"checkpoint"}); err == nil {
		t.Fatal("existing checkpoint overwritten")
	}
	if err = run([]string{"rollback"}); err != nil {
		t.Fatal(err)
	}
	after, _, err := digest(".update-before.db")
	if err != nil || before != after {
		t.Fatal("rollback consumed original checkpoint")
	}
	if err = integrity(dbPath()); err != nil {
		t.Fatal(err)
	}
}

func TestBackupSignalHelper(t *testing.T) {
	if os.Getenv("BACKUP_SIGNAL_HELPER") != "1" {
		return
	}
	os.Args = []string{"dashboard-backup", "backup"}
	if os.Getenv("BACKUP_SIGNAL_COMMAND") == "loop" {
		os.Args[1] = "loop"
	}
	main()
}

func TestKilledRecoveryDownloadRestartsWithoutAcceptingPartialFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.db")
	t.Setenv("DB_PATH", path)
	t.Setenv("BACKUP_BEARER_SECRET", "synthetic-test-secret")
	data := []byte("complete ciphertext")
	hash := sha256.Sum256(data)
	m := snapshot{ID: "test", Kind: "hourly", Size: int64(len(data)), Chunks: 1, SHA256: hex.EncodeToString(hash[:])}
	var block atomic.Bool
	block.Store(true)
	started := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/snapshots" {
			json.NewEncoder(w).Encode([]snapshot{m})
			return
		}
		if block.Load() {
			w.Write(data[:3])
			w.(http.Flusher).Flush()
			started <- struct{}{}
			<-r.Context().Done()
			return
		}
		w.Write(data)
	}))
	defer server.Close()
	t.Setenv("BACKUP_SINK_URL", server.URL)
	cmd := exec.Command(os.Args[0], "-test.run=^TestBackupSignalHelper$")
	cmd.Env = append(os.Environ(), "BACKUP_SIGNAL_HELPER=1", "BACKUP_SIGNAL_COMMAND=loop")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("download did not start")
	}
	if _, err := os.Stat(path + ".recovery.age"); !os.IsNotExist(err) {
		t.Fatal("partial final artifact visible")
	}
	cmd.Process.Kill()
	cmd.Wait()
	block.Store(false)
	if err := loadSecretFiles(); err != nil {
		t.Fatal(err)
	}
	if err := locked(scheduled); err != nil {
		t.Fatal(err)
	}
	result, _ := os.ReadFile(path + ".recovery.age")
	if string(result) != string(data) {
		t.Fatal("restart failed to stage verified ciphertext")
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, ".dashboard-download-*"))
	if len(leftovers) != 0 {
		t.Fatal("abandoned download not reclaimed")
	}
}

func TestSIGTERMCleansPlaintextAndRestartReclaimsAbandonedWork(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DB_PATH", filepath.Join(dir, "dashboard.db"))
	db, err := openDB(dbPath(), false)
	if err != nil {
		t.Fatal(err)
	}
	db.Exec("CREATE TABLE proof(value); INSERT INTO proof VALUES('private')")
	db.Close()
	identity, _ := age.GenerateX25519Identity()
	started := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		select {
		case started <- struct{}{}:
		default:
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestBackupSignalHelper$")
	cmd.Env = append(os.Environ(), "BACKUP_SIGNAL_HELPER=1", "BACKUP_BEARER_SECRET=test-synthetic-bearer", "BACKUP_SINK_URL="+server.URL, "BACKUP_AGE_RECIPIENT="+identity.Recipient().String())
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill() }()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("backup upload did not start")
	}
	dirs, _ := filepath.Glob(filepath.Join(dir, "backup-*"))
	if len(dirs) != 1 {
		t.Fatal("expected live plaintext working directory")
	}
	if err = cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("SIGTERM did not cancel upload")
	}
	dirs, _ = filepath.Glob(filepath.Join(dir, "backup-*"))
	if len(dirs) != 0 {
		t.Fatal("SIGTERM retained plaintext")
	}
	for _, name := range []string{"backup-1234", "membership-5678"} {
		os.Mkdir(filepath.Join(dir, name), 0700)
		os.WriteFile(filepath.Join(dir, name, "private"), []byte("private"), 0600)
	}
	os.Mkdir(filepath.Join(dir, "backup-unrelated"), 0700)
	if err = locked(func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if (strings.HasPrefix(entry.Name(), "backup-") && entry.Name() != "backup-unrelated") || strings.HasPrefix(entry.Name(), "membership-") {
			t.Fatalf("abandoned work retained: %s", entry.Name())
		}
	}
}
