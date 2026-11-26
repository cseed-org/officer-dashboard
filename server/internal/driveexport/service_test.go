package driveexport_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"officerdashboard/internal/driveexport"
	"officerdashboard/internal/files"
	"officerdashboard/internal/files/drivefake"
	"officerdashboard/internal/store"
)

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
	s := &driveexport.Service{DB: db, Drive: d}
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
	if err := (&driveexport.Service{DB: db, Drive: d}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	uploads := fake.Count("POST", "/upload/") + fake.Count("PATCH", "/upload/")
	time.Sleep(2 * time.Millisecond)
	if err := (&driveexport.Service{DB: db, Drive: d}).Run(context.Background()); err != nil {
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
		checked := int64(1)
		if err := q.UpsertGuests(ctx, "ev1", []store.GuestIn{{ID: "guest", Email: email, Name: ptr("Private Guest"), CheckedInAt: &checked}}, 0); err != nil {
			return err
		}
		return q.UpsertReflections(ctx, "ev1", []store.ReflectionIn{{ID: "reflection", Email: email, SubmittedAt: 1}}, 0)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := (&driveexport.Service{DB: db, Drive: d}).Run(ctx); err != nil {
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
		Count int   `json:"count"`
		Total int64 `json:"total_cents"`
	}
	if err := json.Unmarshal(doc["expenses"], &expenses); err != nil {
		t.Fatal(err)
	}
	if expenses["submitted"].Count != 2 || expenses["submitted"].Total != 1500 {
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
	s := &driveexport.Service{DB: db, Drive: d}
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
	s := &driveexport.Service{DB: db, Drive: d}
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
			s := &driveexport.Service{DB: db, Drive: d}
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
	go func() { defer close(done); (&driveexport.Service{DB: db, Drive: d}).Hourly(ctx) }()
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
	s := &driveexport.Service{DB: db, Drive: d}
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
