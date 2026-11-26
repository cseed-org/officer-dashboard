package driveexport_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"officerdashboard/internal/driveexport"
	"officerdashboard/internal/files"
	"officerdashboard/internal/files/drivefake"
	"officerdashboard/internal/store"
)

// putDesign stores a design file under ev1's folder, as design uploads do, and
// records it in design_request_files linked to eventID.
func putDesign(t *testing.T, db *store.DB, d *files.Drive, eventID string) files.Stored {
	t.Helper()
	ctx := context.Background()
	at := time.Date(2026, 12, 1, 20, 0, 0, 0, time.UTC).UnixMilli()
	stored, err := d.Put(ctx, files.PutInput{Folder: []string{"Design", "design1"}, Event: &files.EventRef{StartsAtMs: &at, Name: "Winter Mixer", ID: "ev1"}, Filename: "poster.png", MimeType: "image/png", Body: []byte("\x89PNG\r\n\x1a\nsynthetic")})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Write(ctx, func(q *store.Queries) error {
		if err := q.InsertUser(ctx, store.User{ID: "u", DiscordID: "d", Name: "U", Role: "officer", Active: true}); err != nil {
			return err
		}
		if err := q.InsertDesignRequest(ctx, store.DesignRequest{ID: "design1", Title: "Poster", Type: "poster", Urgency: "normal", Stage: "delivered", RequesterID: "u", EventID: ptr(eventID), Formats: "[]", Brief: "b", Refs: "[]"}); err != nil {
			return err
		}
		return q.InsertDesignFile(ctx, store.DesignFile{ID: "f1", RequestID: "design1", Kind: "delivered", Filename: "poster.png", MimeType: "image/png", SizeBytes: 10, Storage: "drive", StorageKey: stored.Key, UploadedBy: "u", CreatedAt: 1})
	}); err != nil {
		t.Fatal(err)
	}
	return stored
}

// assertRetiredWithDesign checks that a deleted event's exported files are
// trashed while its folder, Design/ subtree and design file stay live and
// reachable under the renamed folder.
func assertRetiredWithDesign(t *testing.T, fake *drivefake.Fake, folder *drivefake.File, exported []*drivefake.File, designKey string) {
	t.Helper()
	for _, f := range exported {
		if f == nil || !fake.Get(f.ID).Trashed {
			t.Fatal("deleted event's exported files remained live")
		}
	}
	got := fake.Get(folder.ID)
	if got.Trashed || got.Name != folder.Name+" (deleted)" {
		t.Fatalf("event folder holding design files: trashed=%v name=%q", got.Trashed, got.Name)
	}
	if fake.Get(designKey).Trashed || fake.Path("Events", got.Name, "Design", "design1", "poster.png") == nil {
		t.Fatal("referenced design file was trashed or became unreachable")
	}
}

func TestDeletedEventKeepsReferencedDesignFiles(t *testing.T) {
	db, d, fake := fixture(t)
	ctx := context.Background()
	stored := putDesign(t, db, d, "ev1")
	s := &driveexport.Service{DB: db, Drive: fixedDrive(d)}
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	folder := fake.Path("Events", "2026-12-01 Winter Mixer [ev1]")
	exported := []*drivefake.File{fake.Path("Events", folder.Name, "event.json"), fake.Path("Events", folder.Name, "summary.md")}
	// ON DELETE SET NULL keeps the request and its design_request_files rows.
	if err := db.Write(ctx, func(q *store.Queries) error { return q.DeleteEvent(ctx, "ev1") }); err != nil {
		t.Fatal(err)
	}
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	assertRetiredWithDesign(t, fake, folder, exported, stored.Key)
	before := len(fake.Calls())
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	for _, c := range fake.Calls()[before:] {
		if c.Method != "GET" {
			t.Fatalf("a retired folder was touched again: %s %s", c.Method, c.URL.Path)
		}
	}
}

func TestRelinkedRequestKeepsFilesUnderDeletedEvent(t *testing.T) {
	db, d, fake := fixture(t)
	ctx := context.Background()
	stored := putDesign(t, db, d, "ev1")
	s := &driveexport.Service{DB: db, Drive: fixedDrive(d)}
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	folder := fake.Path("Events", "2026-12-01 Winter Mixer [ev1]")
	exported := []*drivefake.File{fake.Path("Events", folder.Name, "event.json"), fake.Path("Events", folder.Name, "summary.md")}
	// The request moves to a live event; its earlier files stay under ev1's folder.
	if err := db.Write(ctx, func(q *store.Queries) error {
		if err := q.InsertEvent(ctx, store.Event{ID: "ev2", Name: "Spring", StartsAt: "2027-03-01T20:00:00Z"}); err != nil {
			return err
		}
		if err := q.Exec(ctx, `UPDATE design_requests SET event_id='ev2' WHERE id='design1'`); err != nil {
			return err
		}
		return q.DeleteEvent(ctx, "ev1")
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	assertRetiredWithDesign(t, fake, folder, exported, stored.Key)
}

func TestForeignRootEventFoldersAreNotTouched(t *testing.T) {
	db, d, fake := fixture(t)
	ctx := context.Background()
	events := fake.Add(drivefake.File{Name: "Events", Parent: fake.RootID, MimeType: drivefake.FolderMIME})
	var ids []string
	for _, n := range []string{"2026-10-01 Prod Gala [5d1c9b0e-prod-1]", "2026-11-01 Prod Hack [5d1c9b0e-prod-2]"} {
		folder := fake.Add(drivefake.File{Name: n, Parent: events, MimeType: drivefake.FolderMIME})
		ids = append(ids, folder,
			fake.Add(drivefake.File{Name: "event.json", Parent: folder, Body: []byte("{}")}),
			fake.Add(drivefake.File{Name: "cover.png", Parent: folder, Body: []byte("synthetic cover")}))
	}
	// Another database's event folders (or a newer backup's): this root has no
	// export state for their ids, so nothing in them is ours to clean up.
	s := &driveexport.Service{DB: db, Drive: fixedDrive(d)}
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if f := fake.Get(id); f.Trashed || strings.HasSuffix(f.Name, "(deleted)") {
			t.Fatalf("foreign item %q was changed", f.Name)
		}
	}
}

func TestDeletedEventWithOnlyExportedFilesIsTrashed(t *testing.T) {
	db, d, fake := fixture(t)
	ctx := context.Background()
	s := &driveexport.Service{DB: db, Drive: fixedDrive(d)}
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	folder := fake.Path("Events", "2026-12-01 Winter Mixer [ev1]")
	json := fake.Path("Events", folder.Name, "event.json")
	if err := db.Write(ctx, func(q *store.Queries) error { return q.DeleteEvent(ctx, "ev1") }); err != nil {
		t.Fatal(err)
	}
	failed := false
	fake.Fail = func(r *http.Request) *http.Response {
		if !failed && r.Method == "PATCH" && r.URL.Path == "/drive/v3/files/"+json.ID {
			failed = true
			return drivefake.Error(400, "invalid")
		}
		return nil
	}
	if err := s.Run(ctx); err == nil {
		t.Fatal("failed file trash was not reported")
	}
	if fake.Get(folder.ID).Trashed || fake.Get(json.ID).Trashed {
		t.Fatal("folder retired although an exported file could not be trashed")
	}
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if !fake.Get(json.ID).Trashed || !fake.Get(folder.ID).Trashed {
		t.Fatal("emptied event folder was not trashed on retry")
	}
}

func TestUnchangedPassListsNoEventFolders(t *testing.T) {
	db, d, fake := fixture(t)
	ctx := context.Background()
	if err := db.Write(ctx, func(q *store.Queries) error {
		for _, id := range []string{"ev2", "ev3", "ev4"} {
			if err := q.InsertEvent(ctx, store.Event{ID: id, Name: id, StartsAt: "2026-12-02T20:00:00Z"}); err != nil {
				return err
			}
		}
		return q.PutEventCover(ctx, store.Cover{EventID: "ev1", MimeType: "image/png", Bytes: []byte("cover"), UpdatedAt: 1})
	}); err != nil {
		t.Fatal(err)
	}
	s := &driveexport.Service{DB: db, Drive: fixedDrive(d)}
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	root := fake.Path("Events")
	folders := map[string]bool{}
	for _, f := range fake.Children(root.ID) {
		folders[f.ID] = true
	}
	before := len(fake.Calls())
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	for _, c := range fake.Calls()[before:] {
		for id := range folders {
			if strings.Contains(c.URL.Query().Get("q"), "'"+id+"' in parents") {
				t.Fatalf("unchanged pass listed an event folder: %s", c.URL.Query().Get("q"))
			}
		}
	}
}
