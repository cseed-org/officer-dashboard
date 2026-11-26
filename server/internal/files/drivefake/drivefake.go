// Package drivefake is an in-memory Google Drive v3 REST server for tests. It implements
// files.Doer, so a *files.Drive built with files.NewDrive(acct, fake.RootID, fake) talks to it.
// It understands the requests files.Drive sends: folder and file lists (with pagination),
// multipart create and update, resumable uploads, media download, metadata patches (trash,
// rename, appProperties) and about.storageQuota. Errors are injected with Fail.
package drivefake

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// FolderMIME is Drive's folder MIME type.
const FolderMIME = "application/vnd.google-apps.folder"

// File is one stored item (a folder has FolderMIME and no body).
type File struct {
	ID, Name, MimeType, Parent string
	Body                       []byte
	AppProperties              map[string]string
	Created, Modified          time.Time
	Trashed                    bool
}

// Call is one recorded request.
type Call struct {
	Method string
	URL    *url.URL
	Header http.Header
	Body   []byte
}

// Fake is the server. Create it with New; every field is safe to set before use.
type Fake struct {
	mu sync.Mutex

	// RootID is the folder the dashboard stores under (DRIVE_ROOT_FOLDER_ID).
	RootID string
	// DriveID is the shared drive holding the root; "" behaves like My Drive.
	DriveID string
	// PageSize caps every list page (default 100), so pagination can be exercised.
	PageSize int
	// QuotaLimit and QuotaUsage are returned by about.storageQuota.
	QuotaLimit, QuotaUsage int64
	// Grant is what the OAuth token endpoint answers to an authorization_code exchange:
	// the scope string and the account in the ID token. Defaults: the Drive scope and
	// a synthetic member account.
	GrantScope, GrantSubject, GrantEmail string
	// RefreshError, when set, is the OAuth error a refresh_token grant answers (e.g. "invalid_grant").
	RefreshError string
	// ReadOnly makes folders report canAddChildren=false (a Viewer, not a Content manager).
	ReadOnly bool
	// Fail, when set, runs before routing; a non-nil response is returned as is.
	Fail func(req *http.Request) *http.Response

	// Revoked lists the tokens sent to the OAuth revoke endpoint.
	Revoked []string

	files    map[string]*File
	sessions map[string]*session
	seq      int
	clock    time.Time
	calls    []Call
}

type session struct {
	meta     File
	size     int64
	received []byte
}

// New returns an empty fake whose root folder is "root".
func New() *Fake {
	f := &Fake{RootID: "root", files: map[string]*File{}, sessions: map[string]*session{}, clock: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)}
	return f
}

// Add stores a file or folder directly and returns its id. Empty Parent means the root.
func (f *Fake) Add(file File) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.add(file)
}

func (f *Fake) add(file File) string {
	if file.ID == "" {
		f.seq++
		file.ID = fmt.Sprintf("id-%d", f.seq)
	}
	if file.Parent == "" {
		file.Parent = f.RootID
	}
	if file.MimeType == "" {
		file.MimeType = "application/octet-stream"
	}
	f.clock = f.clock.Add(time.Second)
	if file.Created.IsZero() {
		file.Created = f.clock
	}
	if file.Modified.IsZero() {
		file.Modified = f.clock
	}
	if file.AppProperties == nil {
		file.AppProperties = map[string]string{}
	}
	cp := file
	f.files[file.ID] = &cp
	return file.ID
}

// Get returns a copy of a stored item, or nil.
func (f *Fake) Get(id string) *File {
	f.mu.Lock()
	defer f.mu.Unlock()
	if x := f.files[id]; x != nil {
		cp := *x
		return &cp
	}
	return nil
}

// Path returns the item at the slash-free name path below the root ("" parts are not allowed), or nil.
func (f *Fake) Path(names ...string) *File {
	f.mu.Lock()
	defer f.mu.Unlock()
	parent := f.RootID
	var cur *File
	for _, n := range names {
		cur = nil
		for _, x := range f.sorted() {
			if x.Parent == parent && x.Name == n && !x.Trashed {
				cur = x
				break
			}
		}
		if cur == nil {
			return nil
		}
		parent = cur.ID
	}
	if cur == nil {
		return nil
	}
	cp := *cur
	return &cp
}

// Children returns copies of the untrashed items directly under parent, oldest first.
func (f *Fake) Children(parent string) []File {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []File
	for _, x := range f.sorted() {
		if x.Parent == parent && !x.Trashed {
			out = append(out, *x)
		}
	}
	return out
}

// Calls returns the recorded requests.
func (f *Fake) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Call(nil), f.calls...)
}

// Count returns how many recorded requests match method and path prefix.
func (f *Fake) Count(method, pathPrefix string) int {
	n := 0
	for _, c := range f.Calls() {
		if c.Method == method && strings.HasPrefix(c.URL.Path, pathPrefix) {
			n++
		}
	}
	return n
}

// Error builds a Drive JSON error response, e.g. Error(403, "storageQuotaExceeded").
func Error(status int, reason string) *http.Response {
	raw, _ := json.Marshal(map[string]any{"error": map[string]any{
		"code": status, "message": "fake " + reason,
		"errors": []map[string]string{{"reason": reason, "message": "fake " + reason, "domain": "global"}},
	}})
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(raw))}
}

// Times returns a Fail hook that answers matching requests with resp() for the first n matches.
// A nil match matches every Drive request (token requests are never failed).
func Times(n int, match func(*http.Request) bool, resp func() *http.Response) func(*http.Request) *http.Response {
	var mu sync.Mutex
	return func(r *http.Request) *http.Response {
		if r.URL.Host == "oauth2.googleapis.com" || (match != nil && !match(r)) {
			return nil
		}
		mu.Lock()
		defer mu.Unlock()
		if n <= 0 {
			return nil
		}
		n--
		return resp()
	}
}

func (f *Fake) sorted() []*File {
	out := make([]*File, 0, len(f.files))
	for _, x := range f.files {
		out = append(out, x)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Created.Equal(out[j].Created) {
			return out[i].Created.Before(out[j].Created)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func js(status int, v any) *http.Response {
	raw, _ := json.Marshal(v)
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(raw))}
}

func (f *Fake) meta(x *File) map[string]any {
	m := map[string]any{"id": x.ID, "name": x.Name, "mimeType": x.MimeType, "parents": []string{x.Parent},
		"createdTime": x.Created.Format(time.RFC3339Nano), "modifiedTime": x.Modified.Format(time.RFC3339Nano),
		"trashed": x.Trashed, "appProperties": x.AppProperties}
	if x.MimeType != FolderMIME {
		m["size"] = strconv.Itoa(len(x.Body))
	} else {
		m["capabilities"] = map[string]bool{"canAddChildren": !f.ReadOnly}
	}
	if f.DriveID != "" {
		m["driveId"] = f.DriveID
	}
	return m
}

// Do implements files.Doer.
func (f *Fake) Do(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(body))
	}
	f.mu.Lock()
	f.calls = append(f.calls, Call{req.Method, req.URL, req.Header.Clone(), body})
	fail := f.Fail
	f.mu.Unlock()
	if fail != nil {
		if r := fail(req); r != nil {
			return r, nil
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.route(req, body), nil
}

var idPath = regexp.MustCompile(`^/(upload/)?drive/v3/files/([^/]+)$`)

func (f *Fake) route(req *http.Request, body []byte) *http.Response {
	u := req.URL
	q := u.Query()
	if u.Host == "oauth2.googleapis.com" {
		return f.oauth(u.Path, body)
	}
	switch {
	case req.Method == http.MethodGet && u.Path == "/drive/v3/about":
		return js(200, map[string]any{"storageQuota": map[string]string{"limit": strconv.FormatInt(f.QuotaLimit, 10), "usage": strconv.FormatInt(f.QuotaUsage, 10)}})
	case req.Method == http.MethodGet && u.Path == "/drive/v3/files":
		return f.list(q)
	case req.Method == http.MethodPost && u.Path == "/drive/v3/files":
		var m File
		if err := json.Unmarshal(body, &metaIn{&m}); err != nil {
			return Error(400, "badRequest")
		}
		id := f.add(m)
		return js(200, f.meta(f.files[id]))
	case req.Method == http.MethodPost && u.Path == "/upload/drive/v3/files" && q.Get("uploadType") == "multipart":
		m, media, ok := parseMultipart(req.Header.Get("Content-Type"), body)
		if !ok {
			return Error(400, "badMultipart")
		}
		m.Body = media
		id := f.add(m)
		return js(200, f.meta(f.files[id]))
	case req.Method == http.MethodPost && u.Path == "/upload/drive/v3/files" && q.Get("uploadType") == "resumable":
		var m File
		if err := json.Unmarshal(body, &metaIn{&m}); err != nil {
			return Error(400, "badRequest")
		}
		if m.MimeType == "" {
			m.MimeType = req.Header.Get("X-Upload-Content-Type")
		}
		size, _ := strconv.ParseInt(req.Header.Get("X-Upload-Content-Length"), 10, 64)
		f.seq++
		sid := fmt.Sprintf("session-%d", f.seq)
		f.sessions[sid] = &session{meta: m, size: size}
		r := js(200, map[string]any{})
		r.Header.Set("Location", "https://www.googleapis.com/upload/drive/v3/files?uploadType=resumable&upload_id="+sid)
		return r
	case req.Method == http.MethodPut && u.Path == "/upload/drive/v3/files" && q.Get("upload_id") != "":
		return f.resumable(q.Get("upload_id"), req.Header.Get("Content-Range"), body)
	}
	m := idPath.FindStringSubmatch(u.Path)
	if m == nil {
		return Error(400, "unexpectedRoute")
	}
	x := f.files[m[2]]
	if m[2] == f.RootID && x == nil {
		x = &File{ID: f.RootID, Name: "root", MimeType: FolderMIME}
	}
	if x == nil {
		return Error(404, "notFound")
	}
	switch {
	case req.Method == http.MethodGet && m[1] == "" && q.Get("alt") == "media":
		r := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {x.MimeType}}, Body: io.NopCloser(bytes.NewReader(x.Body))}
		r.ContentLength = int64(len(x.Body))
		return r
	case req.Method == http.MethodGet && m[1] == "":
		return js(200, f.meta(x))
	case req.Method == http.MethodPatch && m[1] == "":
		var patch struct {
			Name          *string            `json:"name"`
			Trashed       *bool              `json:"trashed"`
			AppProperties map[string]*string `json:"appProperties"`
		}
		if err := json.Unmarshal(body, &patch); err != nil {
			return Error(400, "badRequest")
		}
		if patch.Name != nil {
			x.Name = *patch.Name
		}
		if patch.Trashed != nil {
			x.Trashed = *patch.Trashed
		}
		mergeProps(x, patch.AppProperties)
		f.clock = f.clock.Add(time.Second)
		x.Modified = f.clock
		return js(200, f.meta(x))
	case req.Method == http.MethodPatch && m[1] == "upload/" && q.Get("uploadType") == "multipart":
		var upd File
		meta, media, ok := parseMultipartRaw(req.Header.Get("Content-Type"), body)
		if !ok {
			return Error(400, "badMultipart")
		}
		var props struct {
			AppProperties map[string]*string `json:"appProperties"`
			MimeType      string             `json:"mimeType"`
		}
		_ = json.Unmarshal(meta, &props)
		_ = json.Unmarshal(meta, &metaIn{&upd})
		if upd.Name != "" {
			x.Name = upd.Name
		}
		if props.MimeType != "" {
			x.MimeType = props.MimeType
		}
		mergeProps(x, props.AppProperties)
		x.Body = media
		f.clock = f.clock.Add(time.Second)
		x.Modified = f.clock
		return js(200, f.meta(x))
	case req.Method == http.MethodDelete:
		delete(f.files, x.ID)
		return &http.Response{StatusCode: 204, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}
	}
	return Error(400, "unexpectedRoute")
}

func (f *Fake) oauth(path string, body []byte) *http.Response {
	form, _ := url.ParseQuery(string(body))
	switch {
	case path == "/revoke":
		f.Revoked = append(f.Revoked, form.Get("token"))
		return js(200, map[string]any{})
	case form.Get("grant_type") == "authorization_code":
		scope, sub, email := f.GrantScope, f.GrantSubject, f.GrantEmail
		if scope == "" {
			scope = "openid https://www.googleapis.com/auth/userinfo.email https://www.googleapis.com/auth/drive"
		}
		if sub == "" {
			sub, email = "drive-member-sub", "drive-member@example.org"
		}
		claims, _ := json.Marshal(map[string]string{"sub": sub, "email": email})
		idToken := "e30." + base64.RawURLEncoding.EncodeToString(claims) + ".sig"
		return js(200, map[string]any{"access_token": "fake-access-token", "refresh_token": "fake-refresh-token", "expires_in": 3600, "id_token": idToken, "scope": scope})
	case form.Get("grant_type") == "refresh_token" && f.RefreshError != "":
		return js(400, map[string]any{"error": f.RefreshError})
	}
	return js(200, map[string]any{"access_token": "fake-access-token", "expires_in": 3600})
}

func mergeProps(x *File, in map[string]*string) {
	for k, v := range in {
		if v == nil {
			delete(x.AppProperties, k)
		} else {
			x.AppProperties[k] = *v
		}
	}
}

// metaIn decodes Drive file metadata JSON into a File.
type metaIn struct{ f *File }

func (m *metaIn) UnmarshalJSON(raw []byte) error {
	var v struct {
		Name          string            `json:"name"`
		MimeType      string            `json:"mimeType"`
		Parents       []string          `json:"parents"`
		AppProperties map[string]string `json:"appProperties"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	m.f.Name, m.f.MimeType, m.f.AppProperties = v.Name, v.MimeType, v.AppProperties
	if len(v.Parents) > 0 {
		m.f.Parent = v.Parents[0]
	}
	return nil
}

func parseMultipartRaw(ct string, body []byte) (meta, media []byte, ok bool) {
	meta, media, _, ok = parseParts(ct, body)
	return meta, media, ok
}

// parseParts splits a multipart/related upload into its JSON metadata and media parts and
// returns the media part's Content-Type, which Drive uses when the metadata names none.
func parseParts(ct string, body []byte) (meta, media []byte, mediaType string, ok bool) {
	mt, params, err := mime.ParseMediaType(ct)
	if err != nil || mt != "multipart/related" {
		return nil, nil, "", false
	}
	r := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	p1, err := r.NextPart()
	if err != nil {
		return nil, nil, "", false
	}
	meta, _ = io.ReadAll(p1)
	p2, err := r.NextPart()
	if err != nil {
		return nil, nil, "", false
	}
	media, _ = io.ReadAll(p2)
	return meta, media, p2.Header.Get("Content-Type"), true
}

func parseMultipart(ct string, body []byte) (File, []byte, bool) {
	meta, media, mediaType, ok := parseParts(ct, body)
	if !ok {
		return File{}, nil, false
	}
	var m File
	if err := json.Unmarshal(meta, &metaIn{&m}); err != nil {
		return File{}, nil, false
	}
	if m.MimeType == "" {
		m.MimeType = mediaType
	}
	return m, media, true
}

var rangeRE = regexp.MustCompile(`^bytes (\d+)-(\d+)/(\d+|\*)$|^bytes \*/(\d+)$`)

func (f *Fake) resumable(id, contentRange string, body []byte) *http.Response {
	s := f.sessions[id]
	if s == nil {
		return Error(404, "notFound")
	}
	m := rangeRE.FindStringSubmatch(contentRange)
	if m == nil {
		return Error(400, "badContentRange")
	}
	if m[4] == "" { // a chunk
		start, _ := strconv.ParseInt(m[1], 10, 64)
		end, _ := strconv.ParseInt(m[2], 10, 64)
		if start != int64(len(s.received)) || end-start+1 != int64(len(body)) {
			return Error(400, "badContentRange")
		}
		s.received = append(s.received, body...)
	}
	if int64(len(s.received)) < s.size {
		r := &http.Response{StatusCode: 308, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}
		if len(s.received) > 0 {
			r.Header.Set("Range", fmt.Sprintf("bytes=0-%d", len(s.received)-1))
		}
		return r
	}
	file := s.meta
	file.Body = s.received
	delete(f.sessions, id)
	nid := f.add(file)
	return js(200, f.meta(f.files[nid]))
}

var clauseRE = regexp.MustCompile(`^(?:'((?:[^'\\]|\\.)*)' in parents|(name|mimeType) (=|!=) '((?:[^'\\]|\\.)*)'|trashed = (true|false))`)

func unq(s string) string { return strings.NewReplacer(`\'`, `'`, `\\`, `\`).Replace(s) }

func (f *Fake) list(q url.Values) *http.Response {
	type clause struct{ field, op, value string }
	var clauses []clause
	rest := strings.TrimSpace(q.Get("q"))
	for rest != "" {
		m := clauseRE.FindStringSubmatch(rest)
		if m == nil {
			return Error(400, "invalidQuery")
		}
		switch {
		case strings.HasSuffix(m[0], "in parents"):
			clauses = append(clauses, clause{"parent", "=", unq(m[1])})
		case m[2] != "":
			clauses = append(clauses, clause{m[2], m[3], unq(m[4])})
		default:
			clauses = append(clauses, clause{"trashed", "=", m[5]})
		}
		rest = strings.TrimSpace(rest[len(m[0]):])
		rest = strings.TrimSpace(strings.TrimPrefix(rest, "and"))
	}
	var match []*File
	for _, x := range f.sorted() {
		ok := true
		for _, c := range clauses {
			var v string
			switch c.field {
			case "parent":
				v = x.Parent
			case "name":
				v = x.Name
			case "mimeType":
				v = x.MimeType
			case "trashed":
				v = strconv.FormatBool(x.Trashed)
			}
			if (c.op == "=") != (v == c.value) {
				ok = false
			}
		}
		if ok {
			match = append(match, x)
		}
	}
	size := f.PageSize
	if size <= 0 {
		size = 100
	}
	if n, _ := strconv.Atoi(q.Get("pageSize")); n > 0 && n < size {
		size = n
	}
	off, _ := strconv.Atoi(q.Get("pageToken"))
	if off > len(match) {
		off = len(match)
	}
	end := off + size
	if end > len(match) {
		end = len(match)
	}
	out := map[string]any{}
	files := []map[string]any{}
	for _, x := range match[off:end] {
		files = append(files, f.meta(x))
	}
	out["files"] = files
	if end < len(match) {
		out["nextPageToken"] = strconv.Itoa(end)
	}
	return js(200, out)
}
