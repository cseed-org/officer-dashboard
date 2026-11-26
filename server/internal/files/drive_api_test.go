package files

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"officerdashboard/internal/files/drivefake"
)

// onDrive returns a Drive talking to an in-memory shared drive, with retries that record
// their waits instead of sleeping.
func onDrive(t *testing.T) (*Drive, *drivefake.Fake, *[]time.Duration) {
	t.Helper()
	fake := drivefake.New()
	fake.DriveID = "shared-1"
	_, acct := newKey(t)
	d, err := NewDrive(acct, fake.RootID, fake)
	if err != nil {
		t.Fatal(err)
	}
	var waits []time.Duration
	d.WithClock(nil, func(_ context.Context, w time.Duration) error { waits = append(waits, w); return nil })
	return d, fake, &waits
}

var pdf = []byte("%PDF-1.4 synthetic receipt")

func isUpload(r *http.Request) bool { return strings.HasPrefix(r.URL.Path, "/upload/") }

func TestGetReturnsStoredBytesAndNilForUnknownIDs(t *testing.T) {
	d, _, _ := onDrive(t)
	ctx := context.Background()
	s, err := d.Put(ctx, PutInput{Folder: []string{"Finance", "FY2027", "req-1"}, Filename: "r.pdf", MimeType: "application/pdf", Body: pdf})
	if err != nil {
		t.Fatal(err)
	}
	got, err := d.Get(ctx, s.Key)
	if err != nil || got == nil || !bytes.Equal(got.Body, pdf) || got.MimeType != "application/pdf" {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	for _, key := range []string{"no-such-file", "../etc/passwd", ""} {
		if f, err := d.Get(ctx, key); f != nil || err != nil {
			t.Errorf("Get(%q) = %v, %v; want nil, nil", key, f, err)
		}
	}
}

func TestGetRefusesBytesThatNoLongerMatchTheirType(t *testing.T) {
	d, fake, _ := onDrive(t)
	id := fake.Add(drivefake.File{Name: "r.pdf", MimeType: "application/pdf", Body: []byte("<html><script>")})
	if f, err := d.Get(context.Background(), id); err == nil || f != nil {
		t.Errorf("Get of an HTML body labelled PDF = %v, %v; want an error", f, err)
	}
	big := fake.Add(drivefake.File{Name: "big.pdf", MimeType: "application/pdf", Body: append([]byte("%PDF-"), make([]byte, MaxBytes)...)})
	if f, err := d.Get(context.Background(), big); err == nil || f != nil {
		t.Errorf("Get of an oversized file = %v, %v; want an error", f, err)
	}
}

func TestDeleteTrashesAndUnknownIDsAreNotErrors(t *testing.T) {
	d, fake, _ := onDrive(t)
	id := fake.Add(drivefake.File{Name: "r.pdf", MimeType: "application/pdf", Body: pdf})
	if err := d.Delete(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if f := fake.Get(id); f == nil || !f.Trashed {
		t.Errorf("after Delete: %+v; want trashed", f)
	}
	if err := d.DeleteByID(context.Background(), "gone"); err != nil {
		t.Errorf("DeleteByID(unknown) = %v", err)
	}
}

func TestQuotaErrorsAreTypedAndNotRetried(t *testing.T) {
	for _, reason := range []string{"storageQuotaExceeded", "teamDriveFileLimitExceeded"} {
		d, fake, waits := onDrive(t)
		fake.Fail = drivefake.Times(99, isUpload, func() *http.Response { return drivefake.Error(403, reason) })
		_, err := d.Put(context.Background(), PutInput{Filename: "r.pdf", MimeType: "application/pdf", Body: pdf})
		if !errors.Is(err, ErrQuota) || errors.Is(err, ErrRateLimited) {
			t.Errorf("%s: err = %v; want ErrQuota", reason, err)
		}
		if n := fake.Count("POST", "/upload/"); n != 1 || len(*waits) != 0 {
			t.Errorf("%s: %d upload attempts, waits %v; want 1 and none", reason, n, *waits)
		}
	}
}

func TestRateLimitsBackOffHonouringRetryAfterThenSucceed(t *testing.T) {
	d, fake, waits := onDrive(t)
	fake.Fail = drivefake.Times(2, isUpload, func() *http.Response {
		r := drivefake.Error(429, "rateLimitExceeded")
		r.Header.Set("Retry-After", "7")
		return r
	})
	if _, err := d.Put(context.Background(), PutInput{Filename: "r.pdf", MimeType: "application/pdf", Body: pdf}); err != nil {
		t.Fatal(err)
	}
	if n := fake.Count("POST", "/upload/"); n != 3 {
		t.Errorf("%d upload attempts; want 3", n)
	}
	if len(*waits) != 2 || (*waits)[0] < 7*time.Second || (*waits)[1] < 7*time.Second {
		t.Errorf("waits %v; want two of at least 7s", *waits)
	}
}

func TestRateLimitRetriesAreBoundedAndGrow(t *testing.T) {
	d, fake, waits := onDrive(t)
	fake.Fail = drivefake.Times(99, isUpload, func() *http.Response { return drivefake.Error(403, "userRateLimitExceeded") })
	_, err := d.Put(context.Background(), PutInput{Filename: "r.pdf", MimeType: "application/pdf", Body: pdf})
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v; want ErrRateLimited", err)
	}
	if n := fake.Count("POST", "/upload/"); n != 5 {
		t.Errorf("%d attempts; want 5", n)
	}
	w := *waits
	if len(w) != 4 {
		t.Fatalf("waits %v; want 4", w)
	}
	// Jittered between half and all of 1s, 2s, 4s, 8s.
	for i, base := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second} {
		if w[i] < base/2 || w[i] > base {
			t.Errorf("wait %d = %v; want within [%v, %v]", i, w[i], base/2, base)
		}
	}
}

func TestServerErrorsAreRetriedButARetryAfterBeyondTheCapIsNot(t *testing.T) {
	d, fake, _ := onDrive(t)
	fake.Fail = drivefake.Times(1, isUpload, func() *http.Response { return drivefake.Error(503, "backendError") })
	if _, err := d.Put(context.Background(), PutInput{Filename: "r.pdf", MimeType: "application/pdf", Body: pdf}); err != nil {
		t.Errorf("one 503 then success: %v", err)
	}
	d, fake, waits := onDrive(t)
	fake.Fail = drivefake.Times(99, isUpload, func() *http.Response {
		r := drivefake.Error(429, "rateLimitExceeded")
		r.Header.Set("Retry-After", "3600")
		return r
	})
	_, err := d.Put(context.Background(), PutInput{Filename: "r.pdf", MimeType: "application/pdf", Body: pdf})
	if !errors.Is(err, ErrRateLimited) || len(*waits) != 0 || fake.Count("POST", "/upload/") != 1 {
		t.Errorf("Retry-After 1h: err %v, waits %v; want ErrRateLimited at once", err, *waits)
	}
}

func TestRetriesStopWhenTheContextEnds(t *testing.T) {
	d, fake, _ := onDrive(t)
	fake.Fail = drivefake.Times(99, isUpload, func() *http.Response { return drivefake.Error(503, "backendError") })
	ctx, cancel := context.WithCancel(context.Background())
	d.WithClock(nil, func(ctx context.Context, _ time.Duration) error { cancel(); return ctx.Err() })
	_, err := d.Put(ctx, PutInput{Filename: "r.pdf", MimeType: "application/pdf", Body: pdf})
	if !errors.Is(err, context.Canceled) || fake.Count("POST", "/upload/") != 1 {
		t.Errorf("err = %v after %d attempts; want context.Canceled after 1", err, fake.Count("POST", "/upload/"))
	}
}

func TestNotFoundIsTyped(t *testing.T) {
	d, _, _ := onDrive(t)
	if _, err := d.Download(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Download(missing) = %v; want ErrNotFound", err)
	}
	if err := d.RenameFolder(context.Background(), "missing", "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("RenameFolder(missing) = %v; want ErrNotFound", err)
	}
}

func TestAnExpiredTokenIsRefreshedOnce(t *testing.T) {
	d, fake, _ := onDrive(t)
	fake.Fail = drivefake.Times(1, isUpload, func() *http.Response { return drivefake.Error(401, "authError") })
	if _, err := d.Put(context.Background(), PutInput{Filename: "r.pdf", MimeType: "application/pdf", Body: pdf}); err != nil {
		t.Fatal(err)
	}
	if n := fake.Count("POST", "/token"); n != 2 {
		t.Errorf("%d token requests; want 2", n)
	}
}

type staticTokens struct{ n int }

func (s *staticTokens) Token(context.Context) (string, time.Time, error) {
	s.n++
	return "member-token", time.Now().Add(time.Hour), nil
}

func TestAnyTokenSourceCanAuthenticate(t *testing.T) {
	fake := drivefake.New()
	src := &staticTokens{}
	d := NewDriveWithTokens(src, fake.RootID, fake)
	if _, err := d.Upsert(context.Background(), UpsertInput{Name: "a.json", MimeType: "application/json", Body: []byte("{}"), MaxBytes: 10}); err != nil {
		t.Fatal(err)
	}
	for _, c := range fake.Calls() {
		if c.Header.Get("Authorization") != "Bearer member-token" {
			t.Errorf("%s %s sent %q", c.Method, c.URL.Path, c.Header.Get("Authorization"))
		}
	}
	if src.n != 1 {
		t.Errorf("token minted %d times; want 1 (cached)", src.n)
	}
}

func TestUpsertCreatesThenUpdatesInPlace(t *testing.T) {
	d, fake, _ := onDrive(t)
	ctx := context.Background()
	in := UpsertInput{Folder: []string{"Events", "2026-11-20 Demo [ev1]"}, Name: "event.json", MimeType: "application/json",
		Body: []byte(`{"v":1}`), AppProperties: map[string]string{"sha256": "aa"}, MaxBytes: 1 << 20}
	first, err := d.Upsert(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	in.Body, in.AppProperties = []byte(`{"v":2}`), map[string]string{"sha256": "bb"}
	second, err := d.Upsert(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || second.Name != "event.json" || second.Size != 7 || second.AppProperties["sha256"] != "bb" {
		t.Errorf("first %+v second %+v; want the same file updated", first, second)
	}
	f := fake.Path("Events", "2026-11-20 Demo [ev1]", "event.json")
	if f == nil || string(f.Body) != `{"v":2}` {
		t.Errorf("stored %+v", f)
	}
	if n := len(fake.Children(fake.Path("Events", "2026-11-20 Demo [ev1]").ID)); n != 1 {
		t.Errorf("%d files in the event folder; want 1", n)
	}
}

func TestUpsertEnforcesTheCallersLimitsNotTheReceiptRules(t *testing.T) {
	d, fake, _ := onDrive(t)
	ctx := context.Background()
	// Markdown is no receipt type, but an export may write it.
	if _, err := d.Upsert(ctx, UpsertInput{Name: "summary.md", MimeType: "text/markdown", Body: []byte("# Demo"), MaxBytes: 100, AllowedMIME: []string{"text/markdown"}}); err != nil {
		t.Errorf("markdown export: %v", err)
	}
	if _, err := d.Upsert(ctx, UpsertInput{Name: "x.md", MimeType: "text/markdown", Body: make([]byte, 101), MaxBytes: 100}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("over MaxBytes: %v; want ErrTooLarge", err)
	}
	if _, err := d.Upsert(ctx, UpsertInput{Name: "x.html", MimeType: "text/html", Body: []byte("x"), MaxBytes: 100, AllowedMIME: []string{"text/markdown"}}); err == nil {
		t.Error("a MIME type outside AllowedMIME was written")
	}
	if _, err := d.Upsert(ctx, UpsertInput{Name: "x.md", MimeType: "text/markdown", Body: []byte("x")}); err == nil {
		t.Error("Upsert without MaxBytes was accepted")
	}
	if n := fake.Count("POST", "/upload/"); n != 1 {
		t.Errorf("%d uploads; want only the allowed one", n)
	}
}

func TestListFollowsEveryPageAndSortsByName(t *testing.T) {
	d, fake, _ := onDrive(t)
	fake.PageSize = 2
	ctx := context.Background()
	folder, err := d.EnsureFolder(ctx, []string{"Backups", "daily"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"e", "c", "a", "d", "b"} {
		fake.Add(drivefake.File{Name: name, Parent: folder, Body: []byte(name), AppProperties: map[string]string{"kind": "daily"}})
	}
	fake.Add(drivefake.File{Name: "trashed", Parent: folder, Trashed: true})
	got, err := d.List(ctx, folder)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range got {
		names = append(names, e.Name)
		if e.Size != 1 || e.AppProperties["kind"] != "daily" || e.Modified.IsZero() {
			t.Errorf("entry %+v", e)
		}
	}
	if strings.Join(names, "") != "abcde" {
		t.Errorf("names %v; want a..e", names)
	}
	pages := 0
	for _, c := range fake.Calls() {
		if c.Method == "GET" && c.URL.Path == "/drive/v3/files" && strings.Contains(c.URL.Query().Get("q"), "'"+folder+"' in parents and trashed") {
			pages++
		}
	}
	if pages != 3 {
		t.Errorf("%d list pages; want 3", pages)
	}
}

func TestEnsureFolderFindChildAndRename(t *testing.T) {
	d, fake, _ := onDrive(t)
	ctx := context.Background()
	a, err := d.EnsureFolder(ctx, []string{"Finance", "FY2027"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := d.EnsureFolder(ctx, []string{"Finance", "FY2027"})
	if a != b || fake.Path("Finance", "FY2027").ID != a {
		t.Errorf("EnsureFolder ids %q, %q", a, b)
	}
	if root, _ := d.EnsureFolder(ctx, nil); root != fake.RootID {
		t.Errorf("EnsureFolder(nil) = %q", root)
	}
	if _, err := d.EnsureFolder(ctx, []string{"Finance", " "}); err == nil {
		t.Error("an empty folder name was accepted")
	}
	fin := fake.Path("Finance").ID
	if e, err := d.FindChild(ctx, fin, "FY2027"); err != nil || e == nil || e.ID != a || !e.IsFolder() {
		t.Errorf("FindChild = %+v, %v", e, err)
	}
	if e, err := d.FindChild(ctx, fin, "FY1999"); err != nil || e != nil {
		t.Errorf("FindChild(missing) = %+v, %v", e, err)
	}
	if err := d.RenameFolder(ctx, a, "FY2027 (closed)"); err != nil {
		t.Fatal(err)
	}
	if fake.Get(a).Name != "FY2027 (closed)" {
		t.Errorf("renamed to %q", fake.Get(a).Name)
	}
}

func TestUploadStreamSendsChunksAndResumesAfterAFailure(t *testing.T) {
	d, fake, waits := onDrive(t)
	d.chunk = 256 << 10
	body := bytes.Repeat([]byte("0123456789abcdef"), (600<<10)/16) // 600 KiB: three chunks
	puts := 0
	fake.Fail = func(r *http.Request) *http.Response {
		if r.Method == http.MethodPut && strings.HasPrefix(r.Header.Get("Content-Range"), "bytes 262144-") {
			puts++
			if puts == 1 {
				return drivefake.Error(503, "backendError")
			}
		}
		return nil
	}
	e, err := d.UploadStream(context.Background(), []string{"Backups", "daily"}, "dashboard-2026-11-25.db.gz.age", "application/octet-stream",
		bytes.NewReader(body), int64(len(body)), map[string]string{"sha256": "synthetic", "kind": "daily"})
	if err != nil {
		t.Fatal(err)
	}
	f := fake.Get(e.ID)
	if f == nil || !bytes.Equal(f.Body, body) || f.Name != "dashboard-2026-11-25.db.gz.age" || f.AppProperties["kind"] != "daily" || e.Size != int64(len(body)) {
		t.Fatalf("uploaded %+v / %+v", e, f)
	}
	if fake.Path("Backups", "daily", "dashboard-2026-11-25.db.gz.age") == nil {
		t.Error("not under Backups/daily")
	}
	if len(*waits) != 1 {
		t.Errorf("waits %v; want one retry", *waits)
	}
	// The token never leaves googleapis: the session URL is checked.
	for _, c := range fake.Calls() {
		if c.URL.Host != "www.googleapis.com" && c.URL.Host != "oauth2.googleapis.com" {
			t.Errorf("request to %s", c.URL)
		}
	}
	rc, err := d.Download(context.Background(), e.ID)
	if err != nil {
		t.Fatal(err)
	}
	back, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(back, body) {
		t.Error("Download did not return the uploaded bytes")
	}
}

func TestUploadStreamRefusesAShortSource(t *testing.T) {
	d, _, _ := onDrive(t)
	if _, err := d.UploadStream(context.Background(), nil, "x", "application/octet-stream", strings.NewReader("abc"), 10, nil); err == nil {
		t.Error("a source shorter than size was accepted")
	}
}

func TestAboutSumsTheSharedDriveOrReadsTheAccountQuota(t *testing.T) {
	d, fake, _ := onDrive(t)
	fake.PageSize = 1
	fake.Add(drivefake.File{Name: "a", Body: make([]byte, 100)})
	fake.Add(drivefake.File{Name: "b", Body: make([]byte, 50), Trashed: true})
	if _, err := d.EnsureFolder(context.Background(), []string{"Events"}); err != nil {
		t.Fatal(err)
	}
	u, err := d.About(context.Background())
	if err != nil || !u.SharedDrive || u.UsedBytes != 150 || u.Files != 2 {
		t.Errorf("shared drive About = %+v, %v; want 150 bytes in 2 files", u, err)
	}

	d, fake, _ = onDrive(t)
	fake.DriveID = ""
	fake.QuotaLimit, fake.QuotaUsage = 1000, 420
	u, err = d.About(context.Background())
	if err != nil || u.SharedDrive || u.UsedBytes != 420 || u.LimitBytes != 1000 {
		t.Errorf("My Drive About = %+v, %v", u, err)
	}
}

func TestOAuthRefreshSourceRefreshesCachesAndReportsRevocation(t *testing.T) {
	fake := drivefake.New()
	refresh := "synthetic-refresh-token"
	var saved []OAuthToken
	revoked := 0
	src := &OAuthRefreshSource{ClientID: "client", ClientSecret: "synthetic-secret", HTTP: fake,
		RefreshToken: func(context.Context) (string, error) { return refresh, nil },
		Saved:        func(_ context.Context, tok OAuthToken) error { saved = append(saved, tok); return nil },
		Revoked:      func(context.Context) { revoked++ }}
	d := NewDriveWithTokens(src, fake.RootID, fake)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, err := d.EnsureFolder(ctx, []string{"Finance"}); err != nil {
			t.Fatal(err)
		}
	}
	var grants []string
	for _, c := range fake.Calls() {
		if c.URL.Host == "oauth2.googleapis.com" {
			grants = append(grants, string(c.Body))
		}
	}
	if len(grants) != 1 || !strings.Contains(grants[0], "grant_type=refresh_token") || !strings.Contains(grants[0], "refresh_token=synthetic-refresh-token") {
		t.Errorf("token requests %q; want one refresh_token grant", grants)
	}
	if len(saved) != 1 || saved[0].Access != "fake-access-token" || saved[0].Expiry.IsZero() {
		t.Errorf("saved %+v", saved)
	}

	fake.Fail = func(r *http.Request) *http.Response {
		if r.URL.Host != "oauth2.googleapis.com" {
			return nil
		}
		return &http.Response{StatusCode: 400, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":"invalid_grant"}`))}
	}
	d = NewDriveWithTokens(src, fake.RootID, fake)
	if _, err := d.EnsureFolder(ctx, []string{"Finance"}); !errors.Is(err, ErrReconnect) || revoked != 1 {
		t.Errorf("invalid_grant: err %v, revoked %d; want ErrReconnect and one Revoked call", err, revoked)
	}
	refresh = ""
	if _, err := d.EnsureFolder(ctx, []string{"Finance"}); !errors.Is(err, ErrNotConnected) {
		t.Errorf("no account: %v; want ErrNotConnected", err)
	}
}

func TestPutForAnEventGoesUnderTheEventFolder(t *testing.T) {
	d, fake, _ := onDrive(t)
	ctx := context.Background()
	at := time.Date(2026, 11, 20, 19, 0, 0, 0, time.UTC).UnixMilli()
	ev := &EventRef{StartsAtMs: &at, Name: "Demo: Day", ID: "ev1"}
	s, err := d.Put(ctx, PutInput{Event: ev, Folder: []string{"Design", "dr1"}, Filename: "poster.png", MimeType: "image/png", Body: []byte("\x89PNG\r\n\x1a\nxx")})
	if err != nil {
		t.Fatal(err)
	}
	f := fake.Path("Events", "2026-11-20 Demo Day [ev1]", "Design", "dr1", "poster.png")
	if f == nil || f.ID != s.Key || s.WebViewLink != "" {
		t.Fatalf("stored %+v at %+v", s, f)
	}
	// Renamed event: the next upload reuses the renamed folder.
	ev.Name = "Demo Night"
	if _, err := d.Put(ctx, PutInput{Event: ev, Folder: []string{"Design", "dr1"}, Filename: "v2.png", MimeType: "image/png", Body: []byte("\x89PNG\r\n\x1a\nyy")}); err != nil {
		t.Fatal(err)
	}
	if fake.Path("Events", "2026-11-20 Demo Night [ev1]", "Design", "dr1", "poster.png") == nil || len(fake.Children(fake.Path("Events").ID)) != 1 {
		t.Error("the renamed event did not keep its folder")
	}
}

func TestCheckRootNeedsAWritableFolder(t *testing.T) {
	d, fake, _ := onDrive(t)
	ctx := context.Background()
	if r, err := d.CheckRoot(ctx); err != nil || !r.CanAddChildren || r.DriveID != "shared-1" {
		t.Errorf("CheckRoot = %+v, %v", r, err)
	}
	fake.ReadOnly = true
	if r, err := d.CheckRoot(ctx); err != nil || r.CanAddChildren {
		t.Errorf("read-only CheckRoot = %+v, %v", r, err)
	}
	file := fake.Add(drivefake.File{Name: "x.pdf", Body: pdf})
	_, acct := newKey(t)
	notFolder, _ := NewDrive(acct, file, fake)
	if _, err := notFolder.CheckRoot(ctx); err == nil {
		t.Error("a file was accepted as the root folder")
	}
	missing, _ := NewDrive(acct, "missing", fake)
	if _, err := missing.CheckRoot(ctx); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing root: %v", err)
	}
}
