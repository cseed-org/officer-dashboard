package drivebackup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"officerdashboard/internal/files"
	"officerdashboard/internal/files/drivefake"
)

type staticTokens struct{}

func (staticTokens) Token(context.Context) (string, time.Time, error) {
	return "synthetic-test-token", time.Now().Add(time.Hour), nil
}

func noSleep(context.Context, time.Duration) error { return nil }

func testDrive(fake *drivefake.Fake) *files.Drive {
	return files.NewDriveWithTokens(staticTokens{}, fake.RootID, fake).WithClock(nil, noSleep)
}

// staticDrive resolves the same store on every pass, as (*api.Server).Drive does while the
// connection is unchanged.
func staticDrive(store Store) func(context.Context) (Store, error) {
	return func(context.Context) (Store, error) { return store, nil }
}

// writeOutbox publishes a fake sidecar pair (ciphertext + manifest) into dir.
func writeOutbox(t *testing.T, dir, name string, body []byte, created time.Time) {
	t.Helper()
	if err := os.MkdirAll(dir, 0750); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	sha := hex.EncodeToString(sum[:])
	if err := os.WriteFile(filepath.Join(dir, name), body, 0640); err != nil {
		t.Fatal(err)
	}
	m := Manifest{SHA256: sha, Size: int64(len(body)), Created: created.UTC().Format(time.RFC3339), Kind: "daily"}
	raw, _ := json.Marshal(m)
	manifest := strings.TrimSuffix(name, ".db.gz.age") + ".json"
	if err := os.WriteFile(filepath.Join(dir, manifest), raw, 0640); err != nil {
		t.Fatal(err)
	}
}

func outboxFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestUploadPublishesDailyAndMonthly(t *testing.T) {
	dir := t.TempDir()
	outbox := filepath.Join(dir, "drive-outbox")
	now := time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC)
	writeOutbox(t, outbox, "dashboard-2026-11-20.db.gz.age", []byte("ciphertext day one"), now.Add(-24*time.Hour))
	fake := drivefake.New()
	svc := &Service{Drive: staticDrive(testDrive(fake)), Outbox: outbox, Now: func() time.Time { return now }}
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("ciphertext day one"))
	sha := hex.EncodeToString(sum[:])
	daily := fake.Path("Backups", "daily", "dashboard-2026-11-20.db.gz.age")
	if daily == nil {
		t.Fatal("daily copy missing in Drive")
	}
	if daily.AppProperties["sha256"] != sha || daily.AppProperties["kind"] != "daily" || daily.AppProperties["size"] == "" {
		t.Fatalf("daily appProperties: %v", daily.AppProperties)
	}
	if string(daily.Body) != "ciphertext day one" {
		t.Fatal("daily content mismatch")
	}
	monthly := fake.Path("Backups", "monthly", "dashboard-2026-11.db.gz.age")
	if monthly == nil {
		t.Fatal("monthly copy missing in Drive")
	}
	if monthly.AppProperties["kind"] != "monthly" || monthly.AppProperties["sha256"] != sha {
		t.Fatalf("monthly appProperties: %v", monthly.AppProperties)
	}
	if left := outboxFiles(t, outbox); len(left) != 0 {
		t.Fatalf("outbox not cleaned: %v", left)
	}
}

func TestIdenticalRemoteIsSkipped(t *testing.T) {
	dir := t.TempDir()
	outbox := filepath.Join(dir, "drive-outbox")
	now := time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC)
	body := []byte("ciphertext day one")
	writeOutbox(t, outbox, "dashboard-2026-11-20.db.gz.age", body, now.Add(-24*time.Hour))
	fake := drivefake.New()
	sum := sha256.Sum256(body)
	backups := fake.Add(drivefake.File{Name: "Backups", MimeType: drivefake.FolderMIME})
	dailyID := fake.Add(drivefake.File{Name: "daily", MimeType: drivefake.FolderMIME, Parent: backups})
	monthlyID := fake.Add(drivefake.File{Name: "monthly", MimeType: drivefake.FolderMIME, Parent: backups})
	fake.Add(drivefake.File{Name: "dashboard-2026-11-20.db.gz.age", Parent: dailyID, Body: body,
		AppProperties: map[string]string{"sha256": hex.EncodeToString(sum[:]), "kind": "daily"}})
	fake.Add(drivefake.File{Name: "dashboard-2026-11.db.gz.age", Parent: monthlyID, Body: body,
		AppProperties: map[string]string{"sha256": hex.EncodeToString(sum[:]), "kind": "monthly"}})
	svc := &Service{Drive: staticDrive(testDrive(fake)), Outbox: outbox, Now: func() time.Time { return now }}
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := fake.Count(http.MethodPost, "/upload/"); n != 0 {
		t.Fatalf("identical remote was re-uploaded (%d uploads)", n)
	}
	if left := outboxFiles(t, outbox); len(left) != 0 {
		t.Fatalf("outbox not cleaned after skip: %v", left)
	}
}

func TestMonthlyUploadedOncePerMonth(t *testing.T) {
	dir := t.TempDir()
	outbox := filepath.Join(dir, "drive-outbox")
	now := time.Date(2026, 11, 22, 8, 0, 0, 0, time.UTC)
	writeOutbox(t, outbox, "dashboard-2026-11-20.db.gz.age", []byte("ciphertext day one"), now.Add(-48*time.Hour))
	writeOutbox(t, outbox, "dashboard-2026-11-21.db.gz.age", []byte("ciphertext day two"), now.Add(-24*time.Hour))
	fake := drivefake.New()
	svc := &Service{Drive: staticDrive(testDrive(fake)), Outbox: outbox, Now: func() time.Time { return now }}
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if monthly := fake.Path("Backups", "monthly", "dashboard-2026-11.db.gz.age"); monthly == nil {
		t.Fatal("monthly copy missing")
	}
	monthlyID := fake.Path("Backups", "monthly").ID
	if n := len(fake.Children(monthlyID)); n != 1 {
		t.Fatalf("monthly copies after two same-month days: %d, want 1", n)
	}
	if left := outboxFiles(t, outbox); len(left) != 0 {
		t.Fatalf("outbox not cleaned: %v", left)
	}
}

func TestRetentionKeepsNewest(t *testing.T) {
	dir := t.TempDir()
	outbox := filepath.Join(dir, "drive-outbox")
	now := time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC)
	writeOutbox(t, outbox, "dashboard-2026-11-20.db.gz.age", []byte("today ciphertext"), now.Add(-24*time.Hour))
	fake := drivefake.New()
	backups := fake.Add(drivefake.File{Name: "Backups", MimeType: drivefake.FolderMIME})
	dailyID := fake.Add(drivefake.File{Name: "daily", MimeType: drivefake.FolderMIME, Parent: backups})
	monthlyID := fake.Add(drivefake.File{Name: "monthly", MimeType: drivefake.FolderMIME, Parent: backups})
	for day := 1; day <= 30; day++ {
		fake.Add(drivefake.File{Name: fmt.Sprintf("dashboard-2026-09-%02d.db.gz.age", day), Parent: dailyID, Body: []byte("old")})
	}
	months := []string{"2025-11", "2025-12", "2026-01", "2026-02", "2026-03", "2026-04", "2026-05", "2026-06", "2026-07", "2026-08", "2026-09", "2026-10"}
	for _, month := range months {
		fake.Add(drivefake.File{Name: "dashboard-" + month + ".db.gz.age", Parent: monthlyID, Body: []byte("old")})
	}
	svc := &Service{Drive: staticDrive(testDrive(fake)), Outbox: outbox, Now: func() time.Time { return now }}
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	daily := fake.Children(dailyID)
	if len(daily) != DailyKeep {
		t.Fatalf("daily copies after retention: %d, want %d", len(daily), DailyKeep)
	}
	if daily[0].Name != "dashboard-2026-09-02.db.gz.age" {
		t.Fatalf("oldest kept daily: %s", daily[0].Name)
	}
	monthly := fake.Children(monthlyID)
	if len(monthly) != MonthlyKeep {
		t.Fatalf("monthly copies after retention: %d, want %d", len(monthly), MonthlyKeep)
	}
	if monthly[0].Name != "dashboard-2025-12.db.gz.age" || monthly[len(monthly)-1].Name != "dashboard-2026-11.db.gz.age" {
		t.Fatalf("kept monthly range: %s .. %s", monthly[0].Name, monthly[len(monthly)-1].Name)
	}
}

func TestQuotaErrorKeepsOutboxAndAlerts(t *testing.T) {
	dir := t.TempDir()
	outbox := filepath.Join(dir, "drive-outbox")
	now := time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC)
	writeOutbox(t, outbox, "dashboard-2026-11-20.db.gz.age", []byte("ciphertext"), now.Add(-24*time.Hour))
	fake := drivefake.New()
	fake.Fail = func(req *http.Request) *http.Response {
		if req.Method == http.MethodPost && strings.HasPrefix(req.URL.Path, "/upload/") {
			return drivefake.Error(http.StatusForbidden, "storageQuotaExceeded")
		}
		return nil
	}
	var alerts []string
	svc := &Service{
		Drive: staticDrive(testDrive(fake)), Outbox: outbox, Now: func() time.Time { return now },
		Alert: func(_ context.Context, _, message string) { alerts = append(alerts, message) },
	}
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("a failing upload must not fail the pass: %v", err)
	}
	if left := outboxFiles(t, outbox); len(left) != 2 {
		t.Fatalf("outbox not kept for retry: %v", left)
	}
	if len(alerts) != 1 || !strings.Contains(alerts[0], "upload failed") {
		t.Fatalf("alerts: %v", alerts)
	}
	// The production alerter (notify.Alerter) rate-limits per key; the service itself alerts
	// on every failing pass.
	_ = svc.RunOnce(context.Background())
	if len(alerts) != 2 {
		t.Fatalf("second failing pass did not alert again: %v", alerts)
	}
}

// TestFailingFileDoesNotBlockNewer: a permanently failing file alerts and is set aside,
// while the newer snapshot behind it still uploads.
func TestFailingFileDoesNotBlockNewer(t *testing.T) {
	dir := t.TempDir()
	outbox := filepath.Join(dir, "drive-outbox")
	now := time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC)
	// The older pair's manifest lies about the sha: a permanent failure.
	writeOutbox(t, outbox, "dashboard-2026-11-19.db.gz.age", []byte("older ciphertext"), now.Add(-30*time.Hour))
	manifestPath := filepath.Join(outbox, "dashboard-2026-11-19.json")
	raw, _ := os.ReadFile(manifestPath)
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	m.SHA256 = strings.Repeat("0", 64)
	bad, _ := json.Marshal(m)
	if err := os.WriteFile(manifestPath, bad, 0640); err != nil {
		t.Fatal(err)
	}
	// The newer pair is fine.
	writeOutbox(t, outbox, "dashboard-2026-11-20.db.gz.age", []byte("newer ciphertext"), now.Add(-24*time.Hour))
	fake := drivefake.New()
	var alerts []string
	svc := &Service{
		Drive: staticDrive(testDrive(fake)), Outbox: outbox, Now: func() time.Time { return now },
		Alert: func(_ context.Context, _, message string) { alerts = append(alerts, message) },
	}
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("one bad file failed the pass: %v", err)
	}
	if daily := fake.Path("Backups", "daily", "dashboard-2026-11-20.db.gz.age"); daily == nil {
		t.Fatal("newer snapshot was blocked by the older failure")
	}
	if daily := fake.Path("Backups", "daily", "dashboard-2026-11-19.db.gz.age"); daily != nil {
		t.Fatal("corrupt file was uploaded")
	}
	if len(alerts) != 1 || !strings.Contains(alerts[0], "dashboard-2026-11-19") {
		t.Fatalf("alerts: %v", alerts)
	}
	// The rejected pair is set aside and no longer scanned.
	for _, name := range []string{"dashboard-2026-11-19.db.gz.age.rejected", "dashboard-2026-11-19.json.rejected"} {
		if _, err := os.Stat(filepath.Join(outbox, name)); err != nil {
			t.Fatalf("rejected pair not set aside: %v", err)
		}
	}
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 1 {
		t.Fatalf("rejected file alerted again: %v", alerts)
	}
}

func TestStuckOutboxAlerts(t *testing.T) {
	dir := t.TempDir()
	outbox := filepath.Join(dir, "drive-outbox")
	now := time.Date(2026, 11, 25, 8, 0, 0, 0, time.UTC)
	// 60h old: inside the sidecar's real cadence (cap 3, one publish per day), where the
	// oldest waiting file reaches about 72h before eviction.
	writeOutbox(t, outbox, "dashboard-2026-11-20.db.gz.age", []byte("ciphertext"), now.Add(-60*time.Hour))
	var alerts []string
	svc := &Service{
		Outbox: outbox, Now: func() time.Time { return now },
		Alert: func(_ context.Context, _, message string) { alerts = append(alerts, message) },
	}
	// Even with Drive disconnected, a snapshot waiting over 3 days alerts.
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 1 || !strings.Contains(alerts[0], "stuck") {
		t.Fatalf("alerts: %v", alerts)
	}
}

func TestFreshOutboxDoesNotAlert(t *testing.T) {
	dir := t.TempDir()
	outbox := filepath.Join(dir, "drive-outbox")
	now := time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC)
	writeOutbox(t, outbox, "dashboard-2026-11-20.db.gz.age", []byte("ciphertext"), now.Add(-24*time.Hour))
	var alerts []string
	svc := &Service{
		Outbox: outbox, Now: func() time.Time { return now },
		Alert: func(_ context.Context, _, message string) { alerts = append(alerts, message) },
	}
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 0 {
		t.Fatalf("fresh outbox alerted: %v", alerts)
	}
}

func TestMidPublishAndManifestMismatch(t *testing.T) {
	dir := t.TempDir()
	outbox := filepath.Join(dir, "drive-outbox")
	now := time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC)
	// A ciphertext without its manifest yet is mid-publish and must be left alone.
	writeOutbox(t, outbox, "dashboard-2026-11-19.db.gz.age", []byte("partial"), now.Add(-48*time.Hour))
	if err := os.Remove(filepath.Join(outbox, "dashboard-2026-11-19.json")); err != nil {
		t.Fatal(err)
	}
	// A manifest that does not match its file fails and keeps the pair for inspection.
	writeOutbox(t, outbox, "dashboard-2026-11-20.db.gz.age", []byte("real ciphertext"), now.Add(-24*time.Hour))
	manifestPath := filepath.Join(outbox, "dashboard-2026-11-20.json")
	raw, _ := os.ReadFile(manifestPath)
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	m.SHA256 = strings.Repeat("0", 64)
	bad, _ := json.Marshal(m)
	if err := os.WriteFile(manifestPath, bad, 0640); err != nil {
		t.Fatal(err)
	}
	var alerts []string
	fake := drivefake.New()
	svc := &Service{
		Drive: staticDrive(testDrive(fake)), Outbox: outbox, Now: func() time.Time { return now },
		Alert: func(_ context.Context, _, message string) { alerts = append(alerts, message) },
	}
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("manifest mismatch failed the pass: %v", err)
	}
	if n := fake.Count(http.MethodPost, "/upload/"); n != 0 {
		t.Fatalf("corrupt file uploaded (%d uploads)", n)
	}
	// The mid-publish file waits; the mismatched pair is set aside as .rejected.
	if _, err := os.Stat(filepath.Join(outbox, "dashboard-2026-11-19.db.gz.age")); err != nil {
		t.Fatal("mid-publish file was touched")
	}
	for _, name := range []string{"dashboard-2026-11-20.db.gz.age.rejected", "dashboard-2026-11-20.json.rejected"} {
		if _, err := os.Stat(filepath.Join(outbox, name)); err != nil {
			t.Fatalf("mismatched pair not set aside: %v", err)
		}
	}
	if len(alerts) == 0 {
		t.Fatal("mismatch did not alert")
	}
}

// stubStore implements Store in memory for retention unit tests.
type stubStore struct {
	listErr error
	entries []files.Entry
	deleted []string
}

func (s *stubStore) EnsureFolder(context.Context, []string) (string, error) { return "folder", nil }
func (s *stubStore) List(context.Context, string) ([]files.Entry, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.entries, nil
}
func (s *stubStore) UploadStream(context.Context, []string, string, string, io.Reader, int64, map[string]string) (files.Entry, error) {
	return files.Entry{}, errors.New("not implemented")
}
func (s *stubStore) Download(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}
func (s *stubStore) DeleteByID(_ context.Context, id string) error {
	s.deleted = append(s.deleted, id)
	return nil
}

func TestRetentionListError(t *testing.T) {
	st := &stubStore{listErr: errors.New("drive unreachable")}
	svc := &Service{}
	if err := svc.retention(context.Background(), st); err == nil {
		t.Fatal("List error not surfaced")
	}
	if len(st.deleted) != 0 {
		t.Fatalf("retention deleted on a List error: %v", st.deleted)
	}
}

func TestRetentionIgnoresForeignFiles(t *testing.T) {
	// dashboard-notes.txt is not a backup copy: it must not count toward the cap, and a
	// real copy must not be trashed early because of it.
	entries := []files.Entry{{ID: "notes", Name: "dashboard-notes.txt"}}
	for day := 1; day <= 31; day++ {
		entries = append(entries, files.Entry{ID: fmt.Sprintf("f%02d", day), Name: fmt.Sprintf("dashboard-2026-09-%02d.db.gz.age", day)})
	}
	st := &stubStore{entries: entries}
	svc := &Service{}
	if err := svc.retention(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	if len(st.deleted) != 1 || st.deleted[0] != "f01" {
		t.Fatalf("retention deleted %v, want just the oldest copy f01", st.deleted)
	}
}
