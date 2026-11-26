package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"officerdashboard/internal/calendar"
	"officerdashboard/internal/files"
	"officerdashboard/internal/files/drivefake"
	"officerdashboard/internal/notify"
	"officerdashboard/internal/store"
)

// doerTransport sends an http.Client's requests to the Drive fake (token, revoke, API).
type doerTransport struct{ d files.Doer }

func (t doerTransport) RoundTrip(r *http.Request) (*http.Response, error) { return t.d.Do(r) }

type alertLog struct {
	mu   sync.Mutex
	msgs []string
}

func (a *alertLog) all() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.msgs...)
}

const driveRootID = "cseedDriveRoot01"

// driveEnv is a server with the OAuth client configured, no fallback store, and Google
// (token endpoint and Drive API) answered by an in-memory shared drive.
func driveEnv(t *testing.T) (*testEnv, *drivefake.Fake, *alertLog) {
	t.Helper()
	fake := drivefake.New()
	fake.RootID, fake.DriveID = driveRootID, "shared-1"
	alerts := &alertLog{}
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &m)
		alerts.mu.Lock()
		alerts.msgs = append(alerts.msgs, m["content"].(string))
		alerts.mu.Unlock()
		w.WriteHeader(204)
	}))
	t.Cleanup(hook.Close)
	g := calendar.NewGoogle("synthetic-client", "synthetic-client-secret", "http://localhost:8090/auth/google/callback")
	g.HTTP = &http.Client{Transport: doerTransport{fake}}
	e := newEnv(t, func(c *Config) {
		c.Google, c.DriveHTTP, c.Files = g, fake, files.Unavailable{}
		c.Alerts = notify.NewAlerter(hook.URL, nil)
	})
	return e, fake, alerts
}

// connectDrive runs the owner's connect flow and chooses the root folder.
func connectDrive(t *testing.T, e *testEnv) actor {
	t.Helper()
	owner := e.as("user-leadership")
	r := e.get("/api/settings/drive/connect", owner)
	if r.Status != 302 {
		t.Fatalf("connect: %d %s", r.Status, r.Raw)
	}
	u, _ := url.Parse(r.Header.Get("Location"))
	owner.cookies = (&http.Response{Header: r.Header}).Cookies()
	r = e.get("/auth/drive-google/callback?state="+url.QueryEscape(u.Query().Get("state"))+"&code=synthetic-code", owner)
	if r.Status != 302 || !strings.HasSuffix(r.Header.Get("Location"), "drive=connected") {
		t.Fatalf("callback: %d %v", r.Status, r.Header)
	}
	owner.cookies = nil
	e.mustOK(e.do("PUT", "/api/settings/drive/root", map[string]any{"folderId": "https://drive.google.com/drive/folders/" + driveRootID + "?usp=sharing"}, owner))
	return owner
}

func TestDriveConnectIsOwnerOnlyAndChecksStatePKCEAndScope(t *testing.T) {
	e, fake, _ := driveEnv(t)
	for _, who := range []string{"user-priya", "user-director"} { // an officer, and a director who is not an owner
		a := e.as(who)
		expectStatus(t, who+" connect", e.get("/api/settings/drive/connect", a), 403)
		expectStatus(t, who+" status", e.get("/api/settings/drive/status", a), 403)
		expectStatus(t, who+" root", e.do("PUT", "/api/settings/drive/root", map[string]any{"folderId": driveRootID}, a), 403)
		expectStatus(t, who+" disconnect", e.do("DELETE", "/api/settings/drive", nil, a), 403)
	}
	owner := e.as("user-leadership")
	r := e.get("/api/settings/drive/connect", owner)
	if r.Status != 302 {
		t.Fatalf("owner connect: %d %s", r.Status, r.Raw)
	}
	u, _ := url.Parse(r.Header.Get("Location"))
	q := u.Query()
	if q.Get("scope") != "openid email https://www.googleapis.com/auth/drive" || q.Get("redirect_uri") != "http://localhost:8090/auth/drive-google/callback" ||
		q.Get("code_challenge_method") != "S256" || q.Has("include_granted_scopes") || q.Get("access_type") != "offline" {
		t.Fatalf("consent URL %s", u)
	}
	cookies := (&http.Response{Header: r.Header}).Cookies()

	// A forged state is refused and nothing is stored.
	owner.cookies = cookies
	r = e.get("/auth/drive-google/callback?state=forged&code=synthetic-code", owner)
	if !strings.HasSuffix(r.Header.Get("Location"), "drive=failed") || e.scalar(`SELECT count(*) FROM drive_google_account WHERE sealed_refresh<>''`) != int64(0) {
		t.Fatalf("forged state: %v", r.Header)
	}
	// Another owner's browser cannot finish this owner's flow.
	other := e.as("user-director")
	if err := e.db.Write(context.Background(), func(q *store.Queries) error {
		return q.Exec(context.Background(), `UPDATE users SET role='leadership', is_owner=1 WHERE id='user-director'`)
	}); err != nil {
		t.Fatal(err)
	}
	other.cookies = cookies
	r = e.get("/auth/drive-google/callback?state="+url.QueryEscape(q.Get("state"))+"&code=synthetic-code", other)
	if !strings.HasSuffix(r.Header.Get("Location"), "drive=failed") {
		t.Fatalf("other owner finished the flow: %v", r.Header)
	}

	// The real flow: the code exchange carries the PKCE verifier matching the challenge.
	r = e.get("/api/settings/drive/connect", owner)
	u, _ = url.Parse(r.Header.Get("Location"))
	owner.cookies = (&http.Response{Header: r.Header}).Cookies()
	r = e.get("/auth/drive-google/callback?state="+url.QueryEscape(u.Query().Get("state"))+"&code=synthetic-code", owner)
	if !strings.HasSuffix(r.Header.Get("Location"), "drive=connected") {
		t.Fatalf("callback: %v", r.Header)
	}
	var verifier string
	for _, c := range fake.Calls() {
		form, _ := url.ParseQuery(string(c.Body))
		if c.URL.Host == "oauth2.googleapis.com" && form.Get("grant_type") == "authorization_code" {
			verifier = form.Get("code_verifier")
		}
	}
	sum := sha256.Sum256([]byte(verifier))
	if verifier == "" || base64.RawURLEncoding.EncodeToString(sum[:]) != u.Query().Get("code_challenge") {
		t.Errorf("PKCE verifier %q does not match the challenge", verifier)
	}
	if e.scalar(`SELECT email FROM drive_google_account`) != "drive-member@example.org" {
		t.Error("account not stored")
	}
}

func TestDriveConnectWithoutTheDriveScopeIsRejected(t *testing.T) {
	e, fake, _ := driveEnv(t)
	fake.GrantScope = "openid https://www.googleapis.com/auth/userinfo.email"
	owner := e.as("user-leadership")
	r := e.get("/api/settings/drive/connect", owner)
	u, _ := url.Parse(r.Header.Get("Location"))
	owner.cookies = (&http.Response{Header: r.Header}).Cookies()
	r = e.get("/auth/drive-google/callback?state="+url.QueryEscape(u.Query().Get("state"))+"&code=synthetic-code", owner)
	if !strings.HasSuffix(r.Header.Get("Location"), "drive=scope_required") {
		t.Fatalf("missing scope: %v", r.Header)
	}
	if e.scalar(`SELECT count(*) FROM drive_google_account WHERE sealed_refresh<>''`) != int64(0) {
		t.Error("a grant without Drive access was stored")
	}
	if len(fake.Revoked) != 1 {
		t.Errorf("the unusable grant was not revoked: %v", fake.Revoked)
	}
}

func TestDriveStatusRootAndDisconnectNeverExposeTokens(t *testing.T) {
	e, fake, _ := driveEnv(t)
	owner := e.as("user-leadership")
	st := e.mustOK(e.get("/api/settings/drive/status", owner))
	if st.at("connected") != false || st.at("configured") != true || st.str("storage") != "" {
		t.Fatalf("before connect %s", st.Raw)
	}
	expectStatus(t, "root before connect", e.do("PUT", "/api/settings/drive/root", map[string]any{"folderId": driveRootID}, owner), 409)
	owner = connectDrive(t, e)
	fake.Add(drivefake.File{Name: "existing.pdf", Body: make([]byte, 1000)})
	st = e.mustOK(e.get("/api/settings/drive/status", owner))
	if st.at("connected") != true || st.str("email") != "drive-member@example.org" || st.str("rootFolderId") != driveRootID || st.at("rootReachable") != true ||
		st.at("sharedDrive") != true || st.at("canWrite") != true || st.str("storage") != "drive" || st.num("usage", "usedBytes") != 1000 || st.num("usage", "quotaBytes") != 100e9 {
		t.Fatalf("status %s", st.Raw)
	}
	expectStatus(t, "bad folder", e.do("PUT", "/api/settings/drive/root", map[string]any{"folderId": "../etc"}, owner), 400)
	expectStatus(t, "unknown folder", e.do("PUT", "/api/settings/drive/root", map[string]any{"folderId": "unknownFolder123"}, owner), 400)
	fake.ReadOnly = true
	expectStatus(t, "viewer-only folder", e.do("PUT", "/api/settings/drive/root", map[string]any{"folderId": driveRootID}, owner), 400)
	fake.ReadOnly = false

	sealedRefresh := e.scalar(`SELECT sealed_refresh FROM drive_google_account`).(string)
	sealedAccess := e.scalar(`SELECT sealed_access FROM drive_google_account`).(string)
	gone := e.mustOK(e.do("DELETE", "/api/settings/drive", nil, owner))
	if gone.at("connected") != false || gone.str("rootFolderId") != driveRootID {
		t.Fatalf("after disconnect %s", gone.Raw)
	}
	if len(fake.Revoked) != 1 || fake.Revoked[0] != "fake-refresh-token" {
		t.Errorf("refresh token not revoked at Google: %v", fake.Revoked)
	}
	audits := e.scalar(`SELECT group_concat(action||' '||coalesce(before_json,'')||coalesce(after_json,''),' | ') FROM audit_log WHERE action LIKE 'drive.%'`).(string)
	for _, want := range []string{"drive.google_connected", "drive.root_set", "drive.google_disconnected"} {
		if !strings.Contains(audits, want) {
			t.Errorf("audit lacks %s: %s", want, audits)
		}
	}
	for _, secret := range []string{"fake-refresh-token", "fake-access-token", sealedRefresh, sealedAccess} {
		if strings.Contains(string(st.Raw)+string(gone.Raw)+audits, secret) {
			t.Errorf("a token appears in a response or audit row")
		}
	}
}

// putReceipt makes a submitted request as Priya and attaches a PDF receipt.
func putReceipt(t *testing.T, e *testEnv) (requestID string, att reply) {
	t.Helper()
	requestID = e.mustOK(e.post("/api/requests", with(purchase, "submit", true), e.officer())).str("request", "id")
	att = e.do("POST", "/api/requests/"+requestID+"/attachments", formBody{
		fields: map[string]string{"kind": "receipt"}, files: []formFile{{"file", "r.pdf", "application/pdf", "%PDF-1.4\nsynthetic receipt"}},
	}, e.officer())
	return requestID, att
}

func TestDriveReceiptsAreStreamedOnlyAfterTheReceiptCheck(t *testing.T) {
	e, fake, _ := driveEnv(t)
	connectDrive(t, e)
	id, att := putReceipt(t, e)
	e.mustOK(att)
	link := att.str("attachment", "url")
	if !strings.HasPrefix(link, "/api/attachments/") {
		t.Fatalf("attachment url %q", link)
	}
	fin := fake.Path("Finance")
	if fin == nil {
		t.Fatal("no Finance folder")
	}
	years := fake.Children(fin.ID)
	if len(years) != 1 || years[0].Name != "FY2027" || fake.Path("Finance", "FY2027", id, "r.pdf") == nil {
		t.Fatalf("receipt not at Finance/FY2027/%s: %+v", id, years)
	}
	if n := e.scalar(`SELECT count(*) FROM attachments WHERE web_view_link IS NOT NULL`); n != int64(0) {
		t.Errorf("%v new rows carry a Drive link", n)
	}
	downloads := func() int {
		return fake.Count("GET", "/drive/v3/files/") - fake.Count("GET", "/drive/v3/files/"+driveRootID)
	}
	before := downloads()
	expectStatus(t, "anonymous", e.get(link, anon), 401)
	expectStatus(t, "another officer", e.get(link, e.as("user-marcus")), 403)
	if downloads() != before {
		t.Error("Drive was called for a refused download")
	}
	for _, who := range []string{"user-priya", "user-director"} {
		r := e.get(link, e.as(who))
		if r.Status != 200 || string(r.Raw) != "%PDF-1.4\nsynthetic receipt" || r.Header.Get("Cache-Control") != "private, no-store" ||
			r.Header.Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(r.Header.Get("Content-Disposition"), "attachment;") || r.Header.Get("Location") != "" {
			t.Errorf("%s download: %d %v %q", who, r.Status, r.Header, r.Raw)
		}
	}
	for _, who := range []string{"user-priya", "user-director", "user-marcus"} {
		if d := e.mustOK(e.get("/api/requests/"+id, e.as(who))); strings.Contains(string(d.Raw), "drive.google.com") {
			t.Errorf("%s: request JSON carries a Drive link: %s", who, d.Raw)
		}
	}
}

func TestEventDesignUploadsGoUnderTheEventFolder(t *testing.T) {
	e, fake, _ := driveEnv(t)
	connectDrive(t, e)
	priya := e.officer()
	png := formFile{"file", "ref.png", "image/png", "\x89PNG\r\n\x1a\nsynthetic"}
	linked := e.mustOK(e.post("/api/design-requests", designBody(), priya)).str("request", "id")
	up := e.mustOK(e.post("/api/design-requests/"+linked+"/files", formBody{fields: map[string]string{"kind": "reference"}, files: []formFile{png}}, priya))
	ev, _ := e.db.Read().EventByID(context.Background(), "event-hack-night")
	want := files.EventFolderName(eventStartMs(ev), ev.Name, ev.ID)
	if fake.Path("Events", want, "Design", linked, "ref.png") == nil {
		t.Errorf("event design file not under Events/%s/Design/%s", want, linked)
	}
	plain := e.mustOK(e.post("/api/design-requests", with(designBody(), "eventId", nil), priya)).str("request", "id")
	e.mustOK(e.post("/api/design-requests/"+plain+"/files", formBody{fields: map[string]string{"kind": "reference"}, files: []formFile{png}}, priya))
	if fake.Path("Design requests", plain, "ref.png") == nil {
		t.Error("an unlinked design file is not under Design requests/")
	}
	url := up.str("request", "files", 0, "url")
	r := e.get(url, priya)
	if !strings.HasPrefix(url, "/api/design-files/") || r.Status != 200 || string(r.Raw) != png.content {
		t.Errorf("design download %q: %d", url, r.Status)
	}
	d := e.mustOK(e.get("/api/design-requests/"+linked, priya))
	if strings.Contains(string(d.Raw), "drive.google.com") {
		t.Errorf("design JSON carries a Drive link: %s", d.Raw)
	}
}

func TestAFullDriveRefusesTheUploadWith507AndAlertsOnce(t *testing.T) {
	e, fake, alerts := driveEnv(t)
	connectDrive(t, e)
	fake.Fail = drivefake.Times(99, func(r *http.Request) bool { return strings.HasPrefix(r.URL.Path, "/upload/") },
		func() *http.Response { return drivefake.Error(403, "storageQuotaExceeded") })
	for i := 0; i < 2; i++ {
		_, att := putReceipt(t, e)
		if att.Status != 507 || att.str("error", "code") != "storage_quota" {
			t.Fatalf("full drive: %d %s", att.Status, att.Raw)
		}
	}
	if n := e.scalar(`SELECT count(*) FROM attachments`); n != int64(0) {
		t.Errorf("%v attachment rows after refused uploads", n)
	}
	if got := alerts.all(); len(got) != 1 || !strings.Contains(got[0], "out of storage") {
		t.Errorf("alerts %q; want one", got)
	}
}

func TestARevokedGrantDisconnectsAlertsAndAnswers503(t *testing.T) {
	e, fake, alerts := driveEnv(t)
	owner := connectDrive(t, e)
	// The access token has expired and Google refuses the refresh token.
	e.db.Write(context.Background(), func(q *store.Queries) error {
		return q.Exec(context.Background(), `UPDATE drive_google_account SET expires_at=0`)
	})
	fake.RefreshError = "invalid_grant"
	for i := 0; i < 2; i++ {
		_, att := putReceipt(t, e)
		if att.Status != 503 || att.str("error", "code") != "not_configured" || !strings.Contains(att.str("error", "message"), "reconnect") {
			t.Fatalf("revoked: %d %s", att.Status, att.Raw)
		}
	}
	st := e.mustOK(e.get("/api/settings/drive/status", owner))
	if st.at("connected") != false || st.at("needsReconnect") != true || st.str("email") != "drive-member@example.org" {
		t.Errorf("status after revocation %s", st.Raw)
	}
	if got := alerts.all(); len(got) != 1 || !strings.Contains(got[0], "revoked") {
		t.Errorf("alerts %q; want one", got)
	}
	// Reconnecting restores uploads.
	fake.RefreshError = ""
	connectDrive(t, e)
	_, att := putReceipt(t, e)
	e.mustOK(att)
}

func TestWithoutAConnectionUploadsAnswer503AndDownloadsOfAnotherBackendAreRefused(t *testing.T) {
	e, _, _ := driveEnv(t)
	_, att := putReceipt(t, e)
	if att.Status != 503 || att.str("error", "code") != "not_configured" {
		t.Fatalf("not connected: %d %s", att.Status, att.Raw)
	}
	// A receipt recorded in local storage is not handed to Drive's API.
	connectDrive(t, e)
	id, ok := putReceipt(t, e)
	e.mustOK(ok)
	e.db.Write(context.Background(), func(q *store.Queries) error {
		return q.Exec(context.Background(), `UPDATE attachments SET storage='local' WHERE request_id=?`, id)
	})
	expectStatus(t, "other backend", e.get(ok.str("attachment", "url"), e.officer()), 503)
}

func TestTheDailyQuotaCheckAlertsAtSeventyPercent(t *testing.T) {
	e, fake, alerts := driveEnv(t)
	e.srv.cfg.DriveQuotaBytes = 10_000
	connectDrive(t, e)
	fake.Add(drivefake.File{Name: "a", Body: make([]byte, 6_900)})
	if pct := e.srv.checkDriveQuota(context.Background()); pct != 69 || len(alerts.all()) != 0 {
		t.Fatalf("69%%: pct %d alerts %q", pct, alerts.all())
	}
	fake.Add(drivefake.File{Name: "b", Body: make([]byte, 200)})
	if pct := e.srv.checkDriveQuota(context.Background()); pct != 71 || len(alerts.all()) != 1 || !strings.Contains(alerts.all()[0], "71%") {
		t.Fatalf("71%%: pct %d alerts %q", pct, alerts.all())
	}
}
