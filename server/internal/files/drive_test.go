package files

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func newKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	acct, _ := json.Marshal(map[string]string{"client_email": "test@example.iam.gserviceaccount.com", "private_key": pemKey})
	return key, string(acct)
}

func TestSignsAVerifiableRS256JWT(t *testing.T) {
	key, acctJSON := newKey(t)
	var acct ServiceAccount
	_ = json.Unmarshal([]byte(acctJSON), &acct)
	jwt, err := SignJWT(acct, 1000)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt %q", jwt)
	}
	payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims map[string]any
	_ = json.Unmarshal(payload, &claims)
	if claims["iss"] != acct.ClientEmail || claims["iat"] != float64(1000) || claims["exp"] != float64(4600) || claims["aud"] != "https://oauth2.googleapis.com/token" {
		t.Errorf("claims %v", claims)
	}
	header, _ := base64.RawURLEncoding.DecodeString(parts[0])
	if string(header) != `{"alg":"RS256","typ":"JWT"}` {
		t.Errorf("header %s", header)
	}
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, sum[:], sig); err != nil {
		t.Errorf("signature does not verify: %v", err)
	}
}

func TestValidateUpload(t *testing.T) {
	if err := ValidateUpload("text/html", 10); err == nil || !strings.Contains(err.Error(), "MIME") || !errors.Is(err, ErrInvalidUpload) {
		t.Errorf("html: %v", err)
	}
	if err := ValidateUpload("image/png", 10*1024*1024+1); err == nil || !strings.Contains(err.Error(), "10 MB") {
		t.Errorf("big: %v", err)
	}
	if err := ValidateUpload("application/pdf", 0); err == nil {
		t.Error("empty file")
	}
	if err := ValidateUpload("application/pdf", 3); err != nil {
		t.Error(err)
	}
}

// fakeDrive routes the Drive REST API: one shared drive, and folders another uploader can create first.
type fakeDrive struct {
	mu       sync.Mutex
	driveID  string
	raceWith string
	folders  []fakeFolder
	clock    int
	next     int
	calls    []fakeCall
	// custom overrides the folder listing when set.
	list func() []map[string]string
}
type fakeFolder struct {
	id, name, parent string
	created          int
}
type fakeCall struct {
	method string
	url    *url.URL
	body   []byte
}

func (f *fakeDrive) Do(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
	}
	f.calls = append(f.calls, fakeCall{req.Method, req.URL, body})
	js := func(v any) (*http.Response, error) {
		raw, _ := json.Marshal(v)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(raw)), Header: http.Header{}}, nil
	}
	u := req.URL
	switch {
	case u.Host == "oauth2.googleapis.com":
		return js(map[string]any{"access_token": "access", "expires_in": 3600})
	case req.Method == "GET" && u.Path == "/drive/v3/files/root":
		if f.driveID != "" {
			return js(map[string]string{"id": "root", "driveId": f.driveID})
		}
		return js(map[string]string{"id": "root"})
	case req.Method == "GET" && u.Path == "/drive/v3/files":
		if f.list != nil {
			return js(map[string]any{"files": f.list()})
		}
		m := regexp.MustCompile(`^'(.+?)' in parents and name = '(.+?)'`).FindStringSubmatch(u.Query().Get("q"))
		var match []fakeFolder
		for _, fo := range f.folders {
			if fo.parent == m[1] && fo.name == m[2] {
				match = append(match, fo)
			}
		}
		sort.Slice(match, func(i, j int) bool { return match[i].created < match[j].created })
		files := []map[string]string{}
		for _, fo := range match {
			files = append(files, map[string]string{"id": fo.id, "createdTime": time.Date(2026, 10, 31, 12, 0, fo.created, 0, time.UTC).Format(time.RFC3339)})
		}
		return js(map[string]any{"files": files})
	case req.Method == "POST" && u.Path == "/drive/v3/files":
		var b struct {
			Name    string   `json:"name"`
			Parents []string `json:"parents"`
		}
		_ = json.Unmarshal(body, &b)
		exists := false
		for _, fo := range f.folders {
			if fo.name == b.Name {
				exists = true
			}
		}
		if f.raceWith != "" && b.Name == f.raceWith && !exists {
			f.folders = append(f.folders, fakeFolder{"theirs-" + b.Name, b.Name, b.Parents[0], f.clock})
			f.clock++
		}
		id := fmt.Sprintf("mine-%d", f.next)
		f.next++
		f.folders = append(f.folders, fakeFolder{id, b.Name, b.Parents[0], f.clock})
		f.clock++
		return js(map[string]string{"id": id})
	case req.Method == "PATCH":
		parts := strings.Split(u.Path, "/")
		return js(map[string]string{"id": parts[len(parts)-1]})
	case strings.HasPrefix(u.Path, "/upload/"):
		return js(map[string]string{"id": "file", "webViewLink": "https://drive.google.com/file/d/file/view"})
	}
	return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("unexpected")), Header: http.Header{}}, nil
}

func (f *fakeDrive) lists() []fakeCall {
	var out []fakeCall
	for _, c := range f.calls {
		if c.method == "GET" && c.url.Path == "/drive/v3/files" {
			out = append(out, c)
		}
	}
	return out
}

func upload(t *testing.T, d *Drive, req string) Stored {
	t.Helper()
	s, err := d.Put(context.Background(), PutInput{Folder: []string{"FY2027", req}, Filename: "r.pdf", MimeType: "application/pdf", Body: []byte("PDF")})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func driveWith(t *testing.T, f *fakeDrive) *Drive {
	_, acct := newKey(t)
	d, err := NewDrive(acct, "root", f)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestUploadsThroughSharedDriveFolders(t *testing.T) {
	f := &fakeDrive{driveID: "shared-1"}
	s := upload(t, driveWith(t, f), "req")
	if s.Key != "file" || s.Storage != "drive" || s.SizeBytes != 3 || s.WebViewLink != "https://drive.google.com/file/d/file/view" {
		t.Errorf("stored %+v", s)
	}
	for _, c := range f.calls[1:] {
		if c.url.Query().Get("supportsAllDrives") != "true" {
			t.Errorf("%s %s lacks supportsAllDrives=true", c.method, c.url)
		}
	}
	last := f.calls[len(f.calls)-1]
	if !strings.Contains(string(last.body), "receipt") && !strings.Contains(string(last.body), `"name":"r.pdf"`) {
		t.Errorf("upload body %s", last.body)
	}
	if !strings.HasPrefix(last.url.Path, "/upload/") {
		t.Errorf("last call %s", last.url)
	}
}

func TestListsInTheSharedDriveCorpusAndReusesFolders(t *testing.T) {
	f := &fakeDrive{driveID: "shared-1"}
	d := driveWith(t, f)
	upload(t, d, "req-a")
	upload(t, d, "req-b")
	lists := f.lists()
	if len(lists) == 0 {
		t.Fatal("no list calls")
	}
	for _, l := range lists {
		q := l.url.Query()
		if q.Get("corpora") != "drive" || q.Get("driveId") != "shared-1" || q.Get("includeItemsFromAllDrives") != "true" {
			t.Errorf("list %s", l.url)
		}
	}
	fy := 0
	for _, fo := range f.folders {
		if fo.name == "FY2027" {
			fy++
		}
	}
	if fy != 1 {
		t.Errorf("FY folder created %d times", fy)
	}
	roots := 0
	for _, c := range f.calls {
		if c.url.Path == "/drive/v3/files/root" {
			roots++
		}
	}
	if roots != 1 {
		t.Errorf("root looked up %d times", roots)
	}
}

func TestFallsBackToAllDrivesCorpus(t *testing.T) {
	f := &fakeDrive{}
	upload(t, driveWith(t, f), "req")
	for _, l := range f.lists() {
		q := l.url.Query()
		if q.Get("corpora") != "allDrives" || q.Has("driveId") {
			t.Errorf("list %s", l.url)
		}
	}
}

func TestConvergesOnTheOldestFolderAfterARace(t *testing.T) {
	f := &fakeDrive{driveID: "shared-1", raceWith: "FY2027"}
	upload(t, driveWith(t, f), "req")
	var req fakeFolder
	for _, fo := range f.folders {
		if fo.name == "req" {
			req = fo
		}
	}
	if req.parent != "theirs-FY2027" {
		t.Errorf("request folder under %q", req.parent)
	}
	var patches []fakeCall
	for _, c := range f.calls {
		if c.method == "PATCH" {
			patches = append(patches, c)
		}
	}
	if len(patches) != 1 || !strings.HasSuffix(patches[0].url.Path, "mine-0") || strings.TrimSpace(string(patches[0].body)) != `{"trashed":true}` {
		t.Errorf("trash calls %+v", patches)
	}
	if got := f.lists()[1].url.Query().Get("orderBy"); got != "createdTime" {
		t.Errorf("relist orderBy = %q", got)
	}
}

func TestBreaksCreatedTimeTiesByID(t *testing.T) {
	f := &fakeDrive{driveID: "d", list: func() []map[string]string {
		return []map[string]string{{"id": "zzz", "createdTime": "2026-10-31T12:00:00.000Z"}, {"id": "aaa", "createdTime": "2026-10-31T12:00:00.000Z"}}
	}}
	upload(t, driveWith(t, f), "req")
	last := f.calls[len(f.calls)-1]
	if !strings.Contains(string(last.body), `"parents":["aaa"]`) {
		t.Errorf("upload body %s", last.body)
	}
}

func TestLocalStoreRoundTripAndKeyHygiene(t *testing.T) {
	l := Local{Root: t.TempDir() + "/receipts"}
	s, err := l.Put(context.Background(), PutInput{MimeType: "application/pdf", Filename: "a.pdf", Body: []byte("%PDF-1.4")})
	if err != nil || s.Storage != "local" || s.WebViewLink != "" || s.SizeBytes != 8 {
		t.Fatalf("stored %+v %v", s, err)
	}
	got, err := l.Get(context.Background(), s.Key)
	if err != nil || got == nil || string(got.Body) != "%PDF-1.4" || got.MimeType != "application/pdf" {
		t.Errorf("got %+v %v", got, err)
	}
	for _, key := range []string{"../etc/passwd", "nope", strings.Repeat("a", 36)} {
		if f, err := l.Get(context.Background(), key); f != nil || err != nil {
			t.Errorf("Get(%q) = %v, %v", key, f, err)
		}
	}
	if _, err := l.Put(context.Background(), PutInput{MimeType: "text/html", Body: []byte("x")}); err == nil {
		t.Error("local store validates too")
	}
	if _, err := os.Stat(l.Root); err != nil {
		t.Error(err)
	}
}
