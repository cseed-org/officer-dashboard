package driveexport_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"officerdashboard/internal/driveexport"
	"officerdashboard/internal/files"
	"officerdashboard/internal/files/drivefake"
	"officerdashboard/internal/notify"
	"officerdashboard/internal/store"
)

func fixedDrive(d *files.Drive) func(context.Context) (*files.Drive, error) {
	return func(context.Context) (*files.Drive, error) { return d, nil }
}

type tokens struct{}

func (tokens) Token(context.Context) (string, time.Time, error) {
	return "synthetic-token", time.Now().Add(time.Hour), nil
}

func fixture(t *testing.T) (*store.DB, *files.Drive, *drivefake.Fake) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "export.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	err = db.Write(context.Background(), func(q *store.Queries) error {
		return q.InsertEvent(context.Background(), store.Event{ID: "ev1", Name: "Winter Mixer", StartsAt: "2026-12-01T20:00:00Z", Status: "published"})
	})
	if err != nil {
		t.Fatal(err)
	}
	fake := drivefake.New()
	d := files.NewDriveWithTokens(tokens{}, fake.RootID, fake).WithClock(nil, func(context.Context, time.Duration) error { return nil })
	return db, d, fake
}

func TestExportWritesReadableEvent(t *testing.T) {
	db, d, fake := fixture(t)
	s := &driveexport.Service{DB: db, Drive: fixedDrive(d)}
	if err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	folder := "2026-12-01 Winter Mixer [ev1]"
	for _, name := range []string{"event.json", "summary.md"} {
		f := fake.Path("Events", folder, name)
		if f == nil || !strings.Contains(string(f.Body), "Winter Mixer") {
			t.Fatalf("missing readable %s: %v", name, f)
		}
	}
}

func TestUnchangedExportSurvivesServiceRestartWithoutUploads(t *testing.T) {
	db, d, fake := fixture(t)
	if err := (&driveexport.Service{DB: db, Drive: fixedDrive(d)}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	uploads := fake.Count("POST", "/upload/") + fake.Count("PATCH", "/upload/")
	time.Sleep(2 * time.Millisecond)
	if err := (&driveexport.Service{DB: db, Drive: fixedDrive(d)}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := fake.Count("POST", "/upload/") + fake.Count("PATCH", "/upload/"); got != uploads {
		t.Fatalf("unchanged pass uploaded %d files", got-uploads)
	}
}

func TestExportIncludesOnlyAllowedEventData(t *testing.T) {
	db, d, fake := fixture(t)
	ctx := context.Background()
	email, avatar, name, team := "synthetic@example.invalid", "https://private.invalid/avatar", "Host Name", "team1"
	err := db.Write(ctx, func(q *store.Queries) error {
		if err := q.InsertUser(ctx, store.User{ID: "host", DiscordID: "synthetic-discord-id", Name: name, Email: &email, AvatarURL: &avatar, Role: "officer", Active: true}); err != nil {
			return err
		}
		if err := q.Exec(ctx, `INSERT INTO subteams(id,name) VALUES('team1','Events Team')`); err != nil {
			return err
		}
		if err := q.Exec(ctx, `UPDATE events SET host_user_id='host',host_subteam_id=?,description='Meet our builders',capacity=40,location='Campus' WHERE id='ev1'`, team); err != nil {
			return err
		}
		phone := "+1-206-555-0189"
		if err := q.InsertDesignRequest(ctx, store.DesignRequest{ID: "design1", Title: "Mixer poster", Type: "poster", Urgency: "normal", Stage: "new", RequesterID: "host", EventID: ptr("ev1"), Formats: "[]", Brief: phone, Refs: "[]"}); err != nil {
			return err
		}
		if err := q.Exec(ctx, `INSERT INTO service_tokens(id,name,token_hash,prefix,scopes,created_by,created_at) VALUES('tok','test','synthetic-secret-hash','test','[]','host',0)`); err != nil {
			return err
		}
		for _, sql := range []string{
			`INSERT INTO accounts(id,code,name,type,normal_balance) VALUES('expense','5000','Event costs','expense','debit')`,
			`INSERT INTO funds(id,code,name,kind,restriction) VALUES('fund','gen','General','general','without_donor')`,
			`INSERT INTO purchase_requests(id,number,requester_id,type,title,vendor,unit_cost_cents,total_cents,expense_account_id,fund_id,event_id,justification,status) VALUES('r1','R1','host','club_purchase','Private expense','synthetic-vendor-secret',1200,1200,'expense','fund','ev1','synthetic-justification-secret','submitted')`,
			`INSERT INTO purchase_requests(id,number,requester_id,type,title,vendor,unit_cost_cents,total_cents,expense_account_id,fund_id,event_id,justification,status) VALUES('r2','R2','host','club_purchase','Private expense','synthetic-vendor-secret',300,300,'expense','fund','ev1','synthetic-justification-secret','submitted')`,
		} {
			if err := q.Exec(ctx, sql); err != nil {
				return err
			}
		}
		if err := q.Exec(ctx, `UPDATE purchase_requests SET actual_total_cents=350 WHERE id='r2'`); err != nil {
			return err
		}
		checked := int64(1)
		if err := q.UpsertGuests(ctx, "ev1", []store.GuestIn{{ID: "guest", Email: email, Name: ptr("Private Guest"), CheckedInAt: &checked}}, 0); err != nil {
			return err
		}
		return q.UpsertReflections(ctx, "ev1", []store.ReflectionIn{{ID: "reflection", Email: email, SubmittedAt: 1}}, 0)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := (&driveexport.Service{DB: db, Drive: fixedDrive(d)}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"event.json", "summary.md"} {
		f := fake.Path("Events", "2026-12-01 Winter Mixer [ev1]", n)
		if f == nil {
			t.Fatal(n)
		}
		for _, want := range []string{"Host Name", "Events Team", "Meet our builders", "Mixer poster", "Design/design1", "Campus"} {
			if !strings.Contains(string(f.Body), want) {
				t.Errorf("%s lacks %s", n, want)
			}
		}
	}
	raw := fake.Path("Events", "2026-12-01 Winter Mixer [ev1]", "event.json").Body
	// Decode JSON field names independently of the export types.
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	var counts map[string]int
	if err := json.Unmarshal(doc["guests"], &counts); err != nil {
		t.Fatal(err)
	}
	if counts["registered"] != 1 || counts["checked_in"] != 1 || counts["reflections"] != 1 {
		t.Fatalf("wrong guest counts: %s", doc["guests"])
	}
	var expenses map[string]struct {
		Count       int    `json:"count"`
		Estimate    int64  `json:"estimated_total_cents"`
		Actual      *int64 `json:"actual_total_cents"`
		ActualCount int    `json:"actual_count"`
	}
	if err := json.Unmarshal(doc["expenses"], &expenses); err != nil {
		t.Fatal(err)
	}
	if got := expenses["submitted"]; got.Count != 2 || got.Estimate != 1500 || got.Actual == nil || *got.Actual != 350 || got.ActualCount != 1 {
		t.Fatalf("wrong expense totals: %s", doc["expenses"])
	}
	for _, c := range fake.Calls() {
		if !strings.HasPrefix(c.URL.Path, "/upload/") {
			continue
		}
		for _, secret := range []string{"@", "+1-206-555-0189", "synthetic-secret-hash", "synthetic-discord-id", "synthetic-vendor-secret", "synthetic-justification-secret", avatar, "Private Guest"} {
			if strings.Contains(string(c.Body), secret) {
				t.Errorf("export contains private value %q", secret)
			}
		}
	}
}

func TestUnknownEventDateReadsToBeDecided(t *testing.T) {
	db, d, fake := fixture(t)
	ctx := context.Background()
	// A legacy empty start is permitted by the table's NOT NULL branch.
	if err := db.Write(ctx, func(q *store.Queries) error { return q.Exec(ctx, `UPDATE events SET starts_at='' WHERE id='ev1'`) }); err != nil {
		t.Fatal(err)
	}
	if err := (&driveexport.Service{DB: db, Drive: fixedDrive(d)}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	f := fake.Path("Events", "undated Winter Mixer [ev1]", "summary.md")
	if f == nil || !strings.Contains(string(f.Body), "Date: to be decided") || strings.Contains(string(f.Body), "Tentative window:  to ") {
		t.Fatal("missing plain-language undecided date")
	}
}

func ptr(s string) *string { return &s }

func TestCoverAndProgramExportIncludesExceptions(t *testing.T) {
	db, d, fake := fixture(t)
	ctx := context.Background()
	err := db.Write(ctx, func(q *store.Queries) error {
		if err := q.PutEventCover(ctx, store.Cover{EventID: "ev1", MimeType: "image/png", Bytes: []byte("synthetic-cover"), Filename: "private-original.png"}); err != nil {
			return err
		}
		if err := q.SaveProgram(ctx, store.Program{ID: "program1", TrackID: "buildspace", Name: "Weekly workshop", Weekday: 2, StartTime: "18:00", DurationMin: 60, StartsOn: "2026-12-01", EndsOn: "2026-12-31"}); err != nil {
			return err
		}
		return q.SaveProgramException(ctx, store.ProgramException{ProgramID: "program1", OnDate: "2026-12-08", Kind: "skip"})
	})
	if err != nil {
		t.Fatal(err)
	}
	s := &driveexport.Service{DB: db, Drive: fixedDrive(d)}
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	f := fake.Path("Events", "2026-12-01 Winter Mixer [ev1]", "cover.png")
	if f == nil || string(f.Body) != "synthetic-cover" {
		t.Fatalf("missing cover: %v", f)
	}
	p := fake.Path("Programs", "buildspace", "program.json")
	if p == nil || !strings.Contains(string(p.Body), "Weekly workshop") || !strings.Contains(string(p.Body), "2026-12-08") {
		t.Fatalf("missing program exception: %v", p)
	}
	before := fake.Count("POST", "/upload/") + fake.Count("PATCH", "/upload/")
	if err := db.Write(ctx, func(q *store.Queries) error {
		return q.SaveProgramException(ctx, store.ProgramException{ProgramID: "program1", OnDate: "2026-12-15", Kind: "skip"})
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if got := fake.Count("POST", "/upload/") + fake.Count("PATCH", "/upload/") - before; got != 1 {
		t.Fatalf("exception edit uploaded %d files", got)
	}
}

func TestEventEditAndRenamePreservesFolderAndCover(t *testing.T) {
	db, d, fake := fixture(t)
	ctx := context.Background()
	if err := db.Write(ctx, func(q *store.Queries) error {
		if err := q.InsertEvent(ctx, store.Event{ID: "ev2", Name: "Other event", StartsAt: "2026-12-02T20:00:00Z"}); err != nil {
			return err
		}
		return q.PutEventCover(ctx, store.Cover{EventID: "ev1", MimeType: "image/png", Bytes: []byte("cover")})
	}); err != nil {
		t.Fatal(err)
	}
	s := &driveexport.Service{DB: db, Drive: fixedDrive(d)}
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	old := fake.Path("Events", "2026-12-01 Winter Mixer [ev1]")
	cover := fake.Path("Events", old.Name, "cover.png")
	calls := len(fake.Calls())
	if err := db.Write(ctx, func(q *store.Queries) error {
		return q.Exec(ctx, `UPDATE events SET name='Renamed Mixer' WHERE id='ev1'`)
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	renamed := fake.Path("Events", "2026-12-01 Renamed Mixer [ev1]")
	if renamed == nil || renamed.ID != old.ID {
		t.Fatal("rename did not preserve folder id")
	}
	changed := 0
	for _, c := range fake.Calls()[calls:] {
		if strings.HasPrefix(c.URL.Path, "/upload/") {
			changed++
			if c.Method != "PATCH" || strings.HasSuffix(c.URL.Path, cover.ID) || strings.Contains(string(c.Body), "Other event") {
				t.Fatal("rewrote an unchanged export")
			}
		}
	}
	if changed != 2 {
		t.Fatalf("rename uploaded %d files; want 2", changed)
	}
	// Guest imports leave the event timestamp alone, but must change both records.
	before := fake.Count("PATCH", "/upload/")
	if err := db.Write(ctx, func(q *store.Queries) error {
		return q.UpsertGuests(ctx, "ev1", []store.GuestIn{{ID: "g", Email: "synthetic@example.invalid"}}, 0)
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if got := fake.Count("PATCH", "/upload/") - before; got != 2 {
		t.Fatalf("guest import uploaded %d files", got)
	}
}

func TestQuotaAndRateLimitStopPassAndResume(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		status       int
		want         error
	}{{"quota", "storageQuotaExceeded", 403, files.ErrQuota}, {"rate", "rateLimitExceeded", 429, files.ErrRateLimited}} {
		t.Run(tc.name, func(t *testing.T) {
			db, d, fake := fixture(t)
			ctx := context.Background()
			uploads := 0
			fake.Fail = func(r *http.Request) *http.Response {
				if strings.HasPrefix(r.URL.Path, "/upload/") {
					uploads++
					return drivefake.Error(tc.status, tc.reason)
				}
				return nil
			}
			s := &driveexport.Service{DB: db, Drive: fixedDrive(d)}
			if err := s.Run(ctx); !errors.Is(err, tc.want) {
				t.Fatalf("got %v; want %v", err, tc.want)
			}
			want := 1
			if tc.status == 429 {
				want = 5
			}
			if uploads != want {
				t.Fatalf("made %d upload attempts; want %d", uploads, want)
			}
			fake.Fail = nil
			if err := s.Run(ctx); err != nil {
				t.Fatal(err)
			}
			if fake.Path("Events", "2026-12-01 Winter Mixer [ev1]", "event.json") == nil {
				t.Fatal("failed upload was marked unchanged")
			}
		})
	}
}

func TestHourlyWaitsAtStartupAndStopsOnCancellation(t *testing.T) {
	db, d, fake := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); (&driveexport.Service{DB: db, Drive: fixedDrive(d)}).Hourly(ctx) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("loop did not stop")
	}
	if len(fake.Calls()) != 0 {
		t.Fatal("loop exported immediately instead of waiting five minutes")
	}
}

func TestConcurrentPassIsSkippedAndSnapshotDoesNotHoldWriter(t *testing.T) {
	db, d, fake := fixture(t)
	ctx := context.Background()
	entered, release := make(chan struct{}), make(chan struct{})
	first := true
	fake.Fail = func(*http.Request) *http.Response {
		if first {
			first = false
			close(entered)
			<-release
		}
		return nil
	}
	s := &driveexport.Service{DB: db, Drive: fixedDrive(d)}
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("export did not start")
	}
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	if err := s.Run(ctx); !errors.Is(err, driveexport.ErrBusy) {
		t.Fatalf("concurrent pass: %v", err)
	}
	writeCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := db.Write(writeCtx, func(q *store.Queries) error {
		return q.Exec(writeCtx, `UPDATE events SET description='Added during export' WHERE id='ev1'`)
	}); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	f := fake.Path("Events", "2026-12-01 Winter Mixer [ev1]", "event.json")
	if strings.Contains(string(f.Body), "Added during export") {
		t.Fatal("export mixed data from different snapshots")
	}
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(fake.Get(f.ID).Body), "Added during export") {
		t.Fatal("next pass missed committed change")
	}
}

func TestCurrentDriveIsResolvedForEveryPass(t *testing.T) {
	db, d, first := fixture(t)
	var current *files.Drive
	var resolveErr error = files.ErrNotConnected
	s := &driveexport.Service{DB: db, Drive: func(context.Context) (*files.Drive, error) { return current, resolveErr }}
	if err := s.Run(context.Background()); !errors.Is(err, files.ErrNotConnected) {
		t.Fatalf("disconnected: %v", err)
	}
	if len(first.Calls()) != 0 {
		t.Fatal("disconnected export contacted Drive")
	}
	current, resolveErr = d, nil
	if err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	second := drivefake.New()
	second.RootID = "another-root"
	current = files.NewDriveWithTokens(tokens{}, second.RootID, second)
	if err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if second.Path("Events", "2026-12-01 Winter Mixer [ev1]", "event.json") == nil {
		t.Fatal("new root was skipped using the old root's hashes")
	}
	resolveErr = files.ErrReconnect
	if err := s.Run(context.Background()); !errors.Is(err, files.ErrReconnect) {
		t.Fatalf("revoked: %v", err)
	}
}

type alertTransport func(*http.Request) (*http.Response, error)

func (f alertTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestExportAlertsUseSharedHourlyQuotaKey(t *testing.T) {
	db, d, fake := fixture(t)
	fake.Fail = func(*http.Request) *http.Response { return drivefake.Error(403, "storageQuotaExceeded") }
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	var messages []string
	a := notify.NewAlerter("https://synthetic.invalid/alert", slog.New(slog.NewTextHandler(io.Discard, nil)))
	a.Now = func() time.Time { return now }
	a.HTTP = &http.Client{Transport: alertTransport(func(r *http.Request) (*http.Response, error) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		messages = append(messages, string(b))
		return &http.Response{StatusCode: 204, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	s := &driveexport.Service{DB: db, Drive: fixedDrive(d), Alerts: a}
	for i := 0; i < 2; i++ {
		if err := s.Run(context.Background()); !errors.Is(err, files.ErrQuota) {
			t.Fatal(err)
		}
	}
	if len(messages) != 1 {
		t.Fatalf("sent %d alerts within an hour", len(messages))
	}
	// Request uploads and exports share this operational key.
	if a.Alert(context.Background(), "drive-quota-full", "request upload also failed") {
		t.Fatal("export did not use the shared quota key")
	}
	now = now.Add(time.Hour)
	if err := s.Run(context.Background()); !errors.Is(err, files.ErrQuota) {
		t.Fatal(err)
	}
	if len(messages) != 2 {
		t.Fatalf("sent %d alerts after an hour", len(messages))
	}
	for _, m := range messages {
		if strings.Contains(m, "Winter Mixer") || strings.Contains(m, "synthetic-token") {
			t.Fatal("alert contains source data")
		}
	}
}

func TestExportSharesEventFolderWithDesignUploads(t *testing.T) {
	db, d, fake := fixture(t)
	ctx := context.Background()
	at := time.Date(2026, 12, 1, 20, 0, 0, 0, time.UTC).UnixMilli()
	_, err := d.Put(ctx, files.PutInput{Folder: []string{"Design", "design1"}, Event: &files.EventRef{StartsAtMs: &at, Name: "Winter Mixer", ID: "ev1"}, Filename: "poster.png", MimeType: "image/png", Body: []byte("\x89PNG\r\n\x1a\nsynthetic")})
	if err != nil {
		t.Fatal(err)
	}
	folder := fake.Path("Events", "2026-12-01 Winter Mixer [ev1]")
	if err := (&driveexport.Service{DB: db, Drive: fixedDrive(d)}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if fake.Get(folder.ID) == nil || fake.Path("Events", folder.Name, "Design", "design1") == nil || fake.Path("Events", folder.Name, "event.json") == nil {
		t.Fatal("design and readable export did not share the event folder")
	}
}

func TestReconnectSkipsTwoPassesWithoutAlerts(t *testing.T) {
	db, _, _ := fixture(t)
	sent := 0
	a := notify.NewAlerter("https://synthetic.invalid/alert", slog.New(slog.NewTextHandler(io.Discard, nil)))
	a.HTTP = &http.Client{Transport: alertTransport(func(*http.Request) (*http.Response, error) {
		sent++
		return &http.Response{StatusCode: 204, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	s := &driveexport.Service{DB: db, Drive: func(context.Context) (*files.Drive, error) { return nil, files.ErrReconnect }, Alerts: a}
	for range 2 {
		if err := s.Run(context.Background()); !errors.Is(err, files.ErrReconnect) {
			t.Fatal(err)
		}
	}
	if sent != 0 {
		t.Fatalf("sent %d alerts while disconnected", sent)
	}
}

func TestUnchangedEventsDoNotRepeatFolderDiscovery(t *testing.T) {
	db, d, fake := fixture(t)
	ctx := context.Background()
	if err := db.Write(ctx, func(q *store.Queries) error {
		for _, id := range []string{"ev2", "ev3", "ev4"} {
			if err := q.InsertEvent(ctx, store.Event{ID: id, Name: id, StartsAt: "2026-12-02T20:00:00Z"}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s := &driveexport.Service{DB: db, Drive: fixedDrive(d)}
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	before := len(fake.Calls())
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	lookups := 0
	for _, c := range fake.Calls()[before:] {
		if c.Method != "GET" {
			t.Fatalf("unchanged pass mutated Drive: %s", c.Method)
		}
		if strings.Contains(c.URL.Query().Get("q"), "name = 'Events'") {
			lookups++
		}
	}
	if lookups > 1 {
		t.Fatalf("unchanged pass repeated Events lookup %d times", lookups)
	}
}

func TestCoverSourceVersionControlsByteLoading(t *testing.T) {
	db, d, fake := fixture(t)
	ctx := context.Background()
	put := func(at int64, body string) {
		t.Helper()
		if err := db.Write(ctx, func(q *store.Queries) error {
			return q.PutEventCover(ctx, store.Cover{EventID: "ev1", MimeType: "image/png", Bytes: []byte(body), UpdatedAt: at})
		}); err != nil {
			t.Fatal(err)
		}
	}
	put(1, "first cover")
	s := &driveexport.Service{DB: db, Drive: fixedDrive(d)}
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	cover := fake.Path("Events", "2026-12-01 Winter Mixer [ev1]", "cover.png")
	put(1, "second cover") // No new source version: the worker must not reload bytes.
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if string(fake.Get(cover.ID).Body) != "first cover" {
		t.Fatal("unchanged source version reloaded the cover")
	}
	put(2, "second cover")
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if string(fake.Get(cover.ID).Body) != "second cover" {
		t.Fatal("changed source version did not reload the cover")
	}
	before := fake.Count("PATCH", "/upload/")
	put(3, "second cover")
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if fake.Count("PATCH", "/upload/") != before {
		t.Fatal("identical cover bytes were re-uploaded after a metadata update")
	}
}

func TestBadCoverDoesNotBlockOtherExports(t *testing.T) {
	db, d, fake := fixture(t)
	ctx := context.Background()
	if err := db.Write(ctx, func(q *store.Queries) error {
		// Model a legacy row written before the current MIME constraint.
		if err := q.Exec(ctx, `PRAGMA ignore_check_constraints=ON`); err != nil {
			return err
		}
		if err := q.PutEventCover(ctx, store.Cover{EventID: "ev1", MimeType: "application/legacy", Bytes: []byte("unsupported"), UpdatedAt: 1}); err != nil {
			return err
		}
		if err := q.Exec(ctx, `PRAGMA ignore_check_constraints=OFF`); err != nil {
			return err
		}
		return q.InsertEvent(ctx, store.Event{ID: "ev2", Name: "Other event", StartsAt: "2026-12-02T20:00:00Z"})
	}); err != nil {
		t.Fatal(err)
	}
	err := (&driveexport.Service{DB: db, Drive: fixedDrive(d)}).Run(ctx)
	if err == nil {
		t.Fatal("bad cover did not report its error")
	}
	if fake.Path("Events", "2026-12-02 Other event [ev2]", "event.json") == nil || fake.Path("Programs", "buildspace", "program.json") == nil {
		t.Fatal("one bad event blocked later exports")
	}
}

func TestPassTimeoutAlertsButShutdownCancellationDoesNot(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(fmt.Sprint(shutdown), func(t *testing.T) {
			sent := 0
			a := notify.NewAlerter("https://synthetic.invalid/alert", slog.New(slog.NewTextHandler(io.Discard, nil)))
			a.HTTP = &http.Client{Transport: alertTransport(func(*http.Request) (*http.Response, error) {
				sent++
				return &http.Response{StatusCode: 204, Body: io.NopCloser(strings.NewReader(""))}, nil
			})}
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			if shutdown {
				cancel()
			}
			s := &driveexport.Service{Timeout: 5 * time.Millisecond, Alerts: a, Drive: func(ctx context.Context) (*files.Drive, error) { <-ctx.Done(); return nil, ctx.Err() }}
			err := s.Run(parent)
			if shutdown {
				if !errors.Is(err, context.Canceled) || sent != 0 {
					t.Fatalf("shutdown: %v, alerts %d", err, sent)
				}
			} else if !errors.Is(err, context.DeadlineExceeded) || sent != 1 {
				t.Fatalf("timeout: %v, alerts %d", err, sent)
			}
		})
	}
}

func TestTrackFoldersUseUniqueKeys(t *testing.T) {
	db, d, fake := fixture(t)
	ctx := context.Background()
	if err := db.Write(ctx, func(q *store.Queries) error {
		for _, track := range []store.ProgramTrack{{ID: "track-a", Key: "alpha", Name: "Same name", Hue: "violet"}, {ID: "track-b", Key: "beta", Name: "Same name", Hue: "coral"}} {
			if err := q.SaveProgramTrack(ctx, track); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s := &driveexport.Service{DB: db, Drive: fixedDrive(d)}
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	a, b := fake.Path("Programs", "alpha", "program.json"), fake.Path("Programs", "beta", "program.json")
	if a == nil || b == nil || a.ID == b.ID {
		t.Fatal("tracks with equal names collided")
	}
	if err := db.Write(ctx, func(q *store.Queries) error {
		return q.Exec(ctx, `UPDATE program_tracks SET name='Renamed' WHERE id='track-a'`)
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if got := fake.Path("Programs", "alpha", "program.json"); got.ID != a.ID || !strings.Contains(string(got.Body), "Renamed") {
		t.Fatal("track rename changed its destination")
	}
}

func TestDeletedEventsAndStaleCoversAreTrashed(t *testing.T) {
	db, d, fake := fixture(t)
	ctx := context.Background()
	s := &driveexport.Service{DB: db, Drive: fixedDrive(d)}
	cover := func(mime string, at int64) {
		t.Helper()
		if err := db.Write(ctx, func(q *store.Queries) error {
			return q.PutEventCover(ctx, store.Cover{EventID: "ev1", MimeType: mime, Bytes: []byte("cover bytes"), UpdatedAt: at})
		}); err != nil {
			t.Fatal(err)
		}
	}
	run := func() {
		t.Helper()
		if err := s.Run(ctx); err != nil {
			t.Fatal(err)
		}
	}
	cover("image/png", 1)
	run()
	events := fake.Path("Events")
	folder := fake.Path("Events", "2026-12-01 Winter Mixer [ev1]")
	oldCover := fake.Path("Events", folder.Name, "cover.png")
	orphan := fake.Add(drivefake.File{Name: "undated Removed [deleted]", Parent: events.ID, MimeType: drivefake.FolderMIME})
	planning := fake.Add(drivefake.File{Name: "Planning", Parent: events.ID, MimeType: drivefake.FolderMIME})
	stale := fake.Add(drivefake.File{Name: "cover.jpeg", Parent: folder.ID, Body: []byte("stale private cover")})
	poster := fake.Add(drivefake.File{Name: "poster.png", Parent: folder.ID, Body: []byte("design file")})
	cover("image/webp", 2)
	run()
	current := fake.Path("Events", folder.Name, "cover.webp")
	if current == nil || !fake.Get(oldCover.ID).Trashed || !fake.Get(stale).Trashed || !fake.Get(orphan).Trashed {
		t.Fatal("deleted event or obsolete covers remained live")
	}
	if fake.Get(planning).Trashed || fake.Get(poster).Trashed {
		t.Fatal("cleanup trashed unrelated material")
	}
	if err := db.Write(ctx, func(q *store.Queries) error { _, err := q.DeleteEventCover(ctx, "ev1"); return err }); err != nil {
		t.Fatal(err)
	}
	run()
	if !fake.Get(current.ID).Trashed {
		t.Fatal("removed source cover remained live")
	}
	cover("image/webp", 3)
	run()
	if got := fake.Path("Events", folder.Name, "cover.webp"); got == nil || got.ID == current.ID {
		t.Fatal("re-added identical cover was skipped using trashed-file state")
	}
	if err := db.Write(ctx, func(q *store.Queries) error { return q.DeleteEvent(ctx, "ev1") }); err != nil {
		t.Fatal(err)
	}
	run()
	if !fake.Get(folder.ID).Trashed {
		t.Fatal("deleted event folder remained live")
	}
	if err := db.Write(ctx, func(q *store.Queries) error {
		return q.InsertEvent(ctx, store.Event{ID: "ev1", Name: "Winter Mixer", StartsAt: "2026-12-01T20:00:00Z", Status: "published"})
	}); err != nil {
		t.Fatal(err)
	}
	run()
	if fake.Path("Events", folder.Name, "event.json") == nil {
		t.Fatal("re-created event was skipped using deleted-folder state")
	}
}

func TestCleanupPreservesEventsCreatedAfterSnapshot(t *testing.T) {
	db, d, fake := fixture(t)
	ctx := context.Background()
	s := &driveexport.Service{DB: db, Drive: fixedDrive(d)}
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	root := fake.Path("Events")
	added := false
	newFolder := ""
	fake.Fail = func(*http.Request) *http.Response {
		if !added {
			added = true
			if err := db.Write(ctx, func(q *store.Queries) error {
				return q.InsertEvent(ctx, store.Event{ID: "new", Name: "New event", StartsAt: "2026-12-04T20:00:00Z"})
			}); err != nil {
				t.Error(err)
			}
			newFolder = fake.Add(drivefake.File{Name: "2026-12-04 New event [new]", Parent: root.ID, MimeType: drivefake.FolderMIME})
		}
		return nil
	}
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if newFolder == "" || fake.Get(newFolder).Trashed {
		t.Fatal("cleanup trashed an event created after the snapshot")
	}
	fake.Fail = nil
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if fake.Path("Events", "2026-12-04 New event [new]", "event.json") == nil {
		t.Fatal("next pass missed the new event")
	}
}

func TestEventUploadFailureDoesNotBlockLaterEvents(t *testing.T) {
	db, d, fake := fixture(t)
	ctx := context.Background()
	if err := db.Write(ctx, func(q *store.Queries) error {
		return q.InsertEvent(ctx, store.Event{ID: "ev2", Name: "Other event", StartsAt: "2026-12-02T20:00:00Z"})
	}); err != nil {
		t.Fatal(err)
	}
	fake.Fail = func(r *http.Request) *http.Response {
		if strings.HasPrefix(r.URL.Path, "/upload/") {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			r.Body = io.NopCloser(strings.NewReader(string(body)))
			if strings.Contains(string(body), "Winter Mixer") {
				return drivefake.Error(400, "invalid")
			}
		}
		return nil
	}
	s := &driveexport.Service{DB: db, Drive: fixedDrive(d)}
	if err := s.Run(ctx); err == nil {
		t.Fatal("failed event was not reported")
	}
	if fake.Path("Events", "2026-12-02 Other event [ev2]", "event.json") == nil {
		t.Fatal("failed upload blocked a later event")
	}
	fake.Fail = nil
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if fake.Path("Events", "2026-12-01 Winter Mixer [ev1]", "event.json") == nil {
		t.Fatal("failed upload was not retried")
	}
}

func TestFailedStaleCoverTrashRetriesWithoutUploadingAgain(t *testing.T) {
	db, d, fake := fixture(t)
	ctx := context.Background()
	s := &driveexport.Service{DB: db, Drive: fixedDrive(d)}
	put := func(mime string, at int64) {
		t.Helper()
		if err := db.Write(ctx, func(q *store.Queries) error {
			return q.PutEventCover(ctx, store.Cover{EventID: "ev1", MimeType: mime, Bytes: []byte("cover"), UpdatedAt: at})
		}); err != nil {
			t.Fatal(err)
		}
	}
	put("image/png", 1)
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	old := fake.Path("Events", "2026-12-01 Winter Mixer [ev1]", "cover.png")
	put("image/webp", 2)
	fake.Fail = func(r *http.Request) *http.Response {
		if r.Method == "PATCH" && r.URL.Path == "/drive/v3/files/"+old.ID {
			return drivefake.Error(400, "invalid")
		}
		return nil
	}
	if err := s.Run(ctx); err == nil {
		t.Fatal("failed cleanup was not reported")
	}
	if fake.Get(old.ID).Trashed {
		t.Fatal("failed cleanup was marked complete")
	}
	before := fake.Count("POST", "/upload/") + fake.Count("PATCH", "/upload/")
	fake.Fail = nil
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if !fake.Get(old.ID).Trashed {
		t.Fatal("stale cover trash was not retried")
	}
	if fake.Count("POST", "/upload/")+fake.Count("PATCH", "/upload/") != before {
		t.Fatal("cleanup retry uploaded unchanged records")
	}
}
