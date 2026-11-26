//go:build linux

package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"filippo.io/age"
)

func TestDriveOutboxPublishManifestAndCap(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DB_PATH", filepath.Join(dir, "dashboard.db"))
	source := filepath.Join(dir, "snapshot.age")
	if err := os.WriteFile(source, []byte("ciphertext-bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	days := []string{"2026-11-20", "2026-11-21", "2026-11-22", "2026-11-23"}
	for _, day := range days {
		now, err := time.Parse("2006-01-02", day)
		if err != nil {
			t.Fatal(err)
		}
		if err := publishDriveOutbox(source, now); err != nil {
			t.Fatal(err)
		}
	}
	outbox := driveOutboxDir()
	matches, err := filepath.Glob(filepath.Join(outbox, "dashboard-*.db.gz.age"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 3 {
		t.Fatalf("outbox cap kept %d files, want 3", len(matches))
	}
	if filepath.Base(matches[0]) != "dashboard-2026-11-21.db.gz.age" {
		t.Fatalf("oldest file not evicted first: %s", filepath.Base(matches[0]))
	}
	if _, err := os.Stat(filepath.Join(outbox, "dashboard-2026-11-20.json")); !os.IsNotExist(err) {
		t.Fatal("evicted file's manifest was kept")
	}
	raw, err := os.ReadFile(filepath.Join(outbox, "dashboard-2026-11-23.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m outboxManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	sha, size, err := digest(source)
	if err != nil {
		t.Fatal(err)
	}
	if m.SHA256 != sha || m.Size != size || m.Kind != "daily" || m.Created == "" {
		t.Fatalf("manifest mismatch: %+v (want sha %s size %d)", m, sha, size)
	}
	info, err := os.Stat(filepath.Join(outbox, "dashboard-2026-11-23.db.gz.age"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0640 {
		t.Fatalf("published mode %v, want 0640", info.Mode().Perm())
	}
	body, err := os.ReadFile(filepath.Join(outbox, "dashboard-2026-11-23.db.gz.age"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ciphertext-bytes" {
		t.Fatal("published content mismatch")
	}
	leftovers, err := filepath.Glob(filepath.Join(outbox, ".publish-*"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("publish left temp files: %v", leftovers)
	}
}

func TestDriveOutboxPublishesOncePerUTCDay(t *testing.T) {
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "dashboard.db"))
	if driveOutboxPublisher() == nil {
		t.Fatal("first run of the day should publish")
	}
	day := time.Now().UTC().Format("2006-01-02")
	if err := markDriveOutboxDay(day); err != nil {
		t.Fatal(err)
	}
	if driveOutboxPublisher() != nil {
		t.Fatal("already-published day should not publish again")
	}
	if err := markDriveOutboxDay("2026-01-01"); err != nil {
		t.Fatal(err)
	}
	if driveOutboxPublisher() == nil {
		t.Fatal("a new UTC day should publish again")
	}
}

// fakeSink is the D1 backup Worker's API, in memory: reservations, chunks, commit,
// verification download, list and daily promotion.
type fakeSink struct {
	mu        sync.Mutex
	snapshots []snapshot
	chunks    map[string][][]byte
	kinds     map[string]string
	promoted  []string
}

func newFakeSink() *fakeSink {
	return &fakeSink{chunks: map[string][][]byte{}, kinds: map[string]string{}}
}

func (f *fakeSink) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/prune":
			fmt.Fprint(w, "{}")
		case r.Method == http.MethodGet && r.URL.Path == "/usage":
			fmt.Fprint(w, `{"bytes":0,"writes":0,"uploads":0,"byte_limit":350000000,"write_limit":40000}`)
		case r.Method == http.MethodGet && r.URL.Path == "/snapshots":
			out := make([]snapshot, len(f.snapshots))
			for i, m := range f.snapshots {
				m.Kind = f.kinds[m.ID]
				out[i] = m
			}
			json.NewEncoder(w).Encode(out)
		case r.Method == http.MethodPost && r.URL.Path == "/snapshots":
			var m snapshot
			json.NewDecoder(r.Body).Decode(&m)
			m.CreatedAt = time.Now().UTC().Format(time.RFC3339)
			f.snapshots = append(f.snapshots, m)
			f.kinds[m.ID] = m.Kind
			fmt.Fprint(w, "{}")
		case r.Method == http.MethodPut && len(parts) == 4 && parts[0] == "snapshots" && parts[2] == "chunks":
			body, _ := io.ReadAll(r.Body)
			f.chunks[parts[1]] = append(f.chunks[parts[1]], body)
			fmt.Fprint(w, "{}")
		case r.Method == http.MethodGet && len(parts) == 4 && parts[0] == "snapshots" && parts[2] == "chunks":
			var n int
			fmt.Sscan(parts[3], &n)
			w.Write(f.chunks[parts[1]][n])
		case r.Method == http.MethodPost && len(parts) == 3 && parts[0] == "snapshots" && parts[2] == "commit":
			var committed snapshot
			for _, m := range f.snapshots {
				if m.ID == parts[1] {
					committed = m
				}
			}
			json.NewEncoder(w).Encode(committed)
		case r.Method == http.MethodPost && len(parts) == 3 && parts[0] == "snapshots" && parts[2] == "promote":
			f.promoted = append(f.promoted, parts[1])
			f.kinds[parts[1]] = "daily"
			fmt.Fprint(w, "{}")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func (f *fakeSink) ciphertext(t *testing.T, id string) []byte {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	var all []byte
	for _, c := range f.chunks[id] {
		all = append(all, c...)
	}
	if len(all) == 0 {
		t.Fatalf("snapshot %s has no chunks", id)
	}
	return all
}

// TestScheduledPublishesDriveOutbox exercises the whole hourly pass: the D1 upload is
// unchanged, and that hour's ciphertext also lands in the outbox with a matching manifest.
func TestScheduledPublishesDriveOutbox(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DB_PATH", filepath.Join(dir, "dashboard.db"))
	db, err := sql.Open("sqlite3", dbPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE proof(value); INSERT INTO proof VALUES('present')"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("BACKUP_AGE_RECIPIENT", identity.Recipient().String())
	t.Setenv("BACKUP_BEARER_SECRET", "synthetic-test-bearer")
	sink := newFakeSink()
	server := httptest.NewServer(sink.handler())
	defer server.Close()
	t.Setenv("BACKUP_SINK_URL", server.URL)

	var alertsMu sync.Mutex
	var alerts []string
	discord := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Content string `json:"content"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		alertsMu.Lock()
		alerts = append(alerts, body.Content)
		alertsMu.Unlock()
	}))
	defer discord.Close()
	t.Setenv("DISCORD_WEBHOOK_URL", discord.URL)

	if err := scheduled(); err != nil {
		t.Fatal(err)
	}
	day := time.Now().UTC().Format("2006-01-02")
	ciphertext := sink.ciphertext(t, sink.snapshots[0].ID)
	sha, size, err := digest(filepath.Join(driveOutboxDir(), "dashboard-"+day+".db.gz.age"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(driveOutboxDir(), "dashboard-"+day+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var m outboxManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	remoteSHA, remoteSize, err := digestBytes(ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	if m.SHA256 != sha || m.SHA256 != remoteSHA || m.Size != size || size != remoteSize {
		t.Fatalf("outbox and D1 ciphertext differ: manifest %+v, D1 sha %s", m, remoteSHA)
	}
	if got := driveOutboxDay(); got != day {
		t.Fatalf("marker = %q, want %q", got, day)
	}
	if len(sink.promoted) != 1 {
		t.Fatalf("daily promotion calls: %v", sink.promoted)
	}

	// The next hourly run uploads to D1 again but does not republish to the outbox.
	if err := scheduled(); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(driveOutboxDir(), "dashboard-*.db.gz.age"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("same-day run republished: %v", matches)
	}
	if len(sink.snapshots) != 2 {
		t.Fatalf("hourly D1 snapshots: %d", len(sink.snapshots))
	}
}

func digestBytes(body []byte) (string, int64, error) {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), int64(len(body)), nil
}

// TestDriveOutboxFailureKeepsD1 proves a broken outbox only alerts: the D1 snapshot is
// still taken and uploaded.
func TestDriveOutboxFailureKeepsD1(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DB_PATH", filepath.Join(dir, "dashboard.db"))
	db, err := sql.Open("sqlite3", dbPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE proof(value)"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("BACKUP_AGE_RECIPIENT", identity.Recipient().String())
	t.Setenv("BACKUP_BEARER_SECRET", "synthetic-test-bearer")
	sink := newFakeSink()
	server := httptest.NewServer(sink.handler())
	defer server.Close()
	t.Setenv("BACKUP_SINK_URL", server.URL)

	var alertsMu sync.Mutex
	var alerts []string
	discord := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Content string `json:"content"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		alertsMu.Lock()
		alerts = append(alerts, body.Content)
		alertsMu.Unlock()
	}))
	defer discord.Close()
	t.Setenv("DISCORD_WEBHOOK_URL", discord.URL)

	// A regular file where the outbox directory belongs makes every publish fail.
	if err := os.WriteFile(driveOutboxDir(), []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := scheduled(); err != nil {
		t.Fatalf("outbox failure broke the D1 path: %v", err)
	}
	if len(sink.snapshots) != 1 {
		t.Fatalf("D1 upload did not happen: %d snapshots", len(sink.snapshots))
	}
	alertsMu.Lock()
	defer alertsMu.Unlock()
	found := false
	for _, a := range alerts {
		if strings.Contains(a, "outbox") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no outbox alert sent: %v", alerts)
	}
}
