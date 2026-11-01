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
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"officerdashboard/internal/auth"
)

// ServiceAccount is the part of a Google service-account key file the JWT flow needs.
type ServiceAccount struct {
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri,omitempty"`
}

var b64 = base64.RawURLEncoding

// SignJWT builds the RS256 assertion exchanged for a Drive access token.
func SignJWT(acct ServiceAccount, now int64) (string, error) {
	block, _ := pem.Decode([]byte(acct.PrivateKey))
	if block == nil {
		return "", errors.New("service account private_key is not PEM")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("service account key: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return "", errors.New("service account key is not RSA")
	}
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	claims, _ := json.Marshal(map[string]any{
		"iss": acct.ClientEmail, "scope": "https://www.googleapis.com/auth/drive",
		"aud": "https://oauth2.googleapis.com/token", "iat": now, "exp": now + 3600,
	})
	content := b64.EncodeToString(header) + "." + b64.EncodeToString(claims)
	sum := sha256.Sum256([]byte(content))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return content + "." + b64.EncodeToString(sig), nil
}

// Doer is the slice of http.Client the Drive store needs (swapped in tests).
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Drive stores receipts in a Google shared drive using a service account.
type Drive struct {
	account ServiceAccount
	rootID  string
	http    Doer
	base    string
	tokenAt string

	mu     sync.Mutex
	token  string
	expiry time.Time
	corpus url.Values
}

// NewDrive parses the service-account JSON. `client` may be nil for a default http client.
func NewDrive(serviceAccountJSON, rootFolderID string, client Doer) (*Drive, error) {
	var acct ServiceAccount
	if err := json.Unmarshal([]byte(serviceAccountJSON), &acct); err != nil {
		return nil, fmt.Errorf("GOOGLE_SERVICE_ACCOUNT_JSON: %w", err)
	}
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	return &Drive{account: acct, rootID: rootFolderID, http: client, base: "https://www.googleapis.com", tokenAt: "https://oauth2.googleapis.com/token"}, nil
}

func (d *Drive) accessToken(ctx context.Context) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.token != "" && time.Until(d.expiry) > time.Minute {
		return d.token, nil
	}
	jwt, err := SignJWT(d.account, time.Now().Unix())
	if err != nil {
		return "", err
	}
	form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"}, "assertion": {jwt}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, d.tokenAt, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := d.http.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return "", fmt.Errorf("Drive token failed (%d)", res.StatusCode)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return "", err
	}
	d.token, d.expiry = out.AccessToken, time.Now().Add(time.Duration(out.ExpiresIn)*time.Second)
	return d.token, nil
}

func (d *Drive) call(ctx context.Context, method, path string, header http.Header, body io.Reader) (*http.Response, error) {
	tok, err := d.accessToken(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, d.base+path, body)
	if err != nil {
		return nil, err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	res, err := d.http.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode/100 != 2 {
		res.Body.Close()
		return nil, fmt.Errorf("Drive operation failed (%d)", res.StatusCode)
	}
	return res, nil
}

func (d *Drive) callJSON(ctx context.Context, method, path string, payload any, out any) error {
	var body io.Reader
	h := http.Header{}
	if payload != nil {
		raw, _ := json.Marshal(payload)
		body = bytes.NewReader(raw)
		h.Set("Content-Type", "application/json")
	}
	res, err := d.call(ctx, method, path, h, body)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if out == nil {
		_, _ = io.Copy(io.Discard, res.Body)
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

// listCorpus makes list queries search the shared drive that holds the root folder: the
// default `user` corpus can miss shared-drive children. Looked up once per store.
func (d *Drive) listCorpus(ctx context.Context) (url.Values, error) {
	d.mu.Lock()
	if d.corpus != nil {
		c := d.corpus
		d.mu.Unlock()
		return c, nil
	}
	d.mu.Unlock()
	var f struct {
		DriveID string `json:"driveId"`
	}
	if err := d.callJSON(ctx, http.MethodGet, "/drive/v3/files/"+url.PathEscape(d.rootID)+"?fields=id,driveId&supportsAllDrives=true", nil, &f); err != nil {
		return nil, err
	}
	c := url.Values{"corpora": {"allDrives"}}
	if f.DriveID != "" {
		c = url.Values{"corpora": {"drive"}, "driveId": {f.DriveID}}
	}
	d.mu.Lock()
	d.corpus = c
	d.mu.Unlock()
	return c, nil
}

func escapeQ(s string) string {
	return strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s)
}

func (d *Drive) findFolders(ctx context.Context, name, parent string) ([]string, error) {
	corpus, err := d.listCorpus(ctx)
	if err != nil {
		return nil, err
	}
	q := url.Values{
		"q":                         {fmt.Sprintf("'%s' in parents and name = '%s' and mimeType = 'application/vnd.google-apps.folder' and trashed = false", escapeQ(parent), escapeQ(name))},
		"fields":                    {"files(id,createdTime)"},
		"orderBy":                   {"createdTime"},
		"supportsAllDrives":         {"true"},
		"includeItemsFromAllDrives": {"true"},
	}
	for k, v := range corpus {
		q[k] = v
	}
	var out struct {
		Files []struct {
			ID          string `json:"id"`
			CreatedTime string `json:"createdTime"`
		} `json:"files"`
	}
	if err := d.callJSON(ctx, http.MethodGet, "/drive/v3/files?"+q.Encode(), nil, &out); err != nil {
		return nil, err
	}
	// Tie-break equal createdTime by id so concurrent uploaders agree on the oldest.
	sort.SliceStable(out.Files, func(i, j int) bool {
		a, b := out.Files[i], out.Files[j]
		if a.CreatedTime != b.CreatedTime {
			return a.CreatedTime < b.CreatedTime
		}
		return a.ID < b.ID
	})
	ids := make([]string, len(out.Files))
	for i, f := range out.Files {
		ids[i] = f.ID
	}
	return ids, nil
}

// folder finds or creates a folder. Concurrent first uploads can both create it, so after
// creating we re-list and every uploader keeps the oldest; a losing duplicate (ours, still
// empty) is trashed.
func (d *Drive) folder(ctx context.Context, name, parent string) (string, error) {
	existing, err := d.findFolders(ctx, name, parent)
	if err != nil {
		return "", err
	}
	if len(existing) > 0 {
		return existing[0], nil
	}
	var created struct {
		ID string `json:"id"`
	}
	err = d.callJSON(ctx, http.MethodPost, "/drive/v3/files?supportsAllDrives=true&fields=id", map[string]any{
		"name": name, "mimeType": "application/vnd.google-apps.folder", "parents": []string{parent},
	}, &created)
	if err != nil {
		return "", err
	}
	again, err := d.findFolders(ctx, name, parent)
	if err != nil {
		return "", err
	}
	oldest := created.ID
	if len(again) > 0 {
		oldest = again[0]
	}
	if oldest != created.ID {
		_ = d.callJSON(ctx, http.MethodPatch, "/drive/v3/files/"+url.PathEscape(created.ID)+"?supportsAllDrives=true", map[string]any{"trashed": true}, nil)
	}
	return oldest, nil
}

// Put uploads a file under the folder path below the root folder.
func (d *Drive) Put(ctx context.Context, in PutInput) (Stored, error) {
	if err := ValidateUpload(in.MimeType, int64(len(in.Body))); err != nil {
		return Stored{}, err
	}
	parent := d.rootID
	for _, part := range in.Folder {
		id, err := d.folder(ctx, part, parent)
		if err != nil {
			return Stored{}, err
		}
		parent = id
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.SetBoundary("cseed_" + auth.UUID())
	metaHdr := textproto.MIMEHeader{"Content-Type": {"application/json; charset=UTF-8"}}
	mp, _ := mw.CreatePart(metaHdr)
	meta, _ := json.Marshal(struct {
		Name    string   `json:"name"`
		Parents []string `json:"parents"`
	}{in.Filename, []string{parent}})
	_, _ = mp.Write(meta)
	fp, _ := mw.CreatePart(textproto.MIMEHeader{"Content-Type": {in.MimeType}})
	_, _ = fp.Write(in.Body)
	_ = mw.Close()
	h := http.Header{"Content-Type": {"multipart/related; boundary=" + mw.Boundary()}}
	res, err := d.call(ctx, http.MethodPost, "/upload/drive/v3/files?uploadType=multipart&supportsAllDrives=true&fields=id,webViewLink,size", h, &buf)
	if err != nil {
		return Stored{}, err
	}
	defer res.Body.Close()
	var f struct {
		ID          string `json:"id"`
		WebViewLink string `json:"webViewLink"`
	}
	if err := json.NewDecoder(res.Body).Decode(&f); err != nil {
		return Stored{}, err
	}
	link := f.WebViewLink
	if link == "" {
		link = "https://drive.google.com/file/d/" + f.ID + "/view"
	}
	return Stored{Storage: "drive", Key: f.ID, WebViewLink: link, SizeBytes: int64(len(in.Body))}, nil
}

// Get downloads a file by id.
func (d *Drive) Get(ctx context.Context, key string) (*File, error) {
	res, err := d.call(ctx, http.MethodGet, "/drive/v3/files/"+url.PathEscape(key)+"?alt=media&supportsAllDrives=true", nil, nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, MaxBytes+1))
	if err != nil {
		return nil, err
	}
	mt := res.Header.Get("Content-Type")
	if mt == "" {
		mt = "application/octet-stream"
	}
	return &File{Body: body, MimeType: mt}, nil
}
