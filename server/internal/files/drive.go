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
	"math/big"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"regexp"
	"sort"
	"strconv"
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

// Typed Drive failures. Every non-2xx answer is an *APIError; errors.Is matches these.
var (
	// ErrQuota: the shared drive is out of storage or item slots (403 storageQuotaExceeded,
	// teamDriveFileLimitExceeded, ...). Retrying does not help; the API answers 507.
	ErrQuota = errors.New("Google Drive storage is full")
	// ErrRateLimited: Drive throttled the dashboard (429, 403 rateLimitExceeded or
	// userRateLimitExceeded) and the bounded retries ran out.
	ErrRateLimited = errors.New("Google Drive is rate limiting the dashboard")
	// ErrNotFound: Drive answered 404.
	ErrNotFound = errors.New("Drive file not found")
)

var quotaReasons = map[string]bool{
	"storageQuotaExceeded": true, "teamDriveFileLimitExceeded": true,
	"numChildrenInNonRootLimitExceeded": true, "activeItemCreationLimitExceeded": true,
	"quotaExceeded": true,
}

// rateReasons are throttling answers; the first three clear with backoff, dailyLimitExceeded does not.
var rateReasons = map[string]bool{
	"rateLimitExceeded": true, "userRateLimitExceeded": true, "sharingRateLimitExceeded": true,
	"dailyLimitExceeded": true,
}

// APIError is a non-2xx Drive answer: its status and the first `reason` from the JSON error body.
type APIError struct {
	Status     int
	Reason     string
	retryAfter string
}

func (e *APIError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("Drive operation failed (%d %s)", e.Status, e.Reason)
	}
	return fmt.Sprintf("Drive operation failed (%d)", e.Status)
}

func (e *APIError) Is(target error) bool {
	switch target {
	case ErrQuota:
		return quotaReasons[e.Reason]
	case ErrRateLimited:
		return e.Status == http.StatusTooManyRequests || rateReasons[e.Reason]
	case ErrNotFound:
		return e.Status == http.StatusNotFound
	}
	return false
}

func (e *APIError) retryable() bool {
	switch e.Status {
	case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return e.Reason != "dailyLimitExceeded" && rateReasons[e.Reason]
}

// errorBodyLimit caps how much of a Drive error body is read.
const errorBodyLimit = 16 << 10

func apiError(res *http.Response) *APIError {
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, errorBodyLimit))
	var body struct {
		Error struct {
			Errors []struct {
				Reason string `json:"reason"`
			} `json:"errors"`
			Status string `json:"status"`
		} `json:"error"`
	}
	e := &APIError{Status: res.StatusCode, retryAfter: res.Header.Get("Retry-After")}
	if json.Unmarshal(raw, &body) == nil && len(body.Error.Errors) > 0 {
		e.Reason = body.Error.Errors[0].Reason
	}
	return e
}

// netError is a transport failure (connection reset, timeout): retried like a 5xx.
type netError struct{ err error }

func (e netError) Error() string { return "Drive request failed: " + e.err.Error() }
func (e netError) Unwrap() error { return e.err }

// Drive stores receipts in a Google shared drive using a service account.
type Drive struct {
	tokens TokenSource
	rootID string
	http   Doer
	base   string

	// Retry policy: tries attempts in all, waiting backoff·2ⁿ (capped at maxWait) with jitter,
	// or the server's Retry-After when it is longer. now and sleep are swapped in tests.
	tries   int
	backoff time.Duration
	maxWait time.Duration
	now     func() time.Time
	sleep   func(context.Context, time.Duration) error
	jitter  func(time.Duration) time.Duration
	chunk   int

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
	return NewDriveWithTokens(&ServiceAccountSource{Account: acct, HTTP: client}, rootFolderID, client), nil
}

// NewDriveWithTokens builds a Drive store that authenticates with any TokenSource.
// `client` may be nil for a default http client.
func NewDriveWithTokens(tokens TokenSource, rootFolderID string, client Doer) *Drive {
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	return &Drive{
		tokens: tokens, rootID: rootFolderID, http: client, base: "https://www.googleapis.com",
		tries: 5, backoff: time.Second, maxWait: 32 * time.Second,
		now: time.Now, sleep: sleepCtx, jitter: randDuration, chunk: 8 << 20,
	}
}

// WithClock replaces the clock and the sleeper used between retries (tests pass a fake
// sleeper so backoff costs nothing). Either may be nil to keep the default. It returns d.
func (d *Drive) WithClock(now func() time.Time, sleep func(context.Context, time.Duration) error) *Drive {
	if now != nil {
		d.now = now
	}
	if sleep != nil {
		d.sleep = sleep
	}
	return d
}

// RootID is the configured root folder (DRIVE_ROOT_FOLDER_ID).
func (d *Drive) RootID() string { return d.rootID }

func sleepCtx(ctx context.Context, wait time.Duration) error {
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func randDuration(n time.Duration) time.Duration {
	if n <= 0 {
		return 0
	}
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0
	}
	return time.Duration(v.Int64())
}

// TokenSource mints Drive access tokens. The service-account JWT flow is one source; an
// OAuth refresh-token source (a signed-in member account) can be another. Drive caches the
// token until a minute before expiry and asks again after a 401.
type TokenSource interface {
	Token(ctx context.Context) (token string, expiry time.Time, err error)
}

// ServiceAccountSource exchanges a signed JWT for an access token (RFC 7523).
type ServiceAccountSource struct {
	Account ServiceAccount
	HTTP    Doer
	URL     string           // token endpoint; "" means Google's
	Now     func() time.Time // nil means time.Now
}

func (s *ServiceAccountSource) Token(ctx context.Context) (string, time.Time, error) {
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	endpoint := s.URL
	if endpoint == "" {
		endpoint = "https://oauth2.googleapis.com/token"
	}
	jwt, err := SignJWT(s.Account, now().Unix())
	if err != nil {
		return "", time.Time{}, err
	}
	form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"}, "assertion": {jwt}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := s.HTTP.Do(req)
	if err != nil {
		return "", time.Time{}, netError{err}
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return "", time.Time{}, fmt.Errorf("Drive token failed (%d)", res.StatusCode)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&out); err != nil {
		return "", time.Time{}, err
	}
	if out.AccessToken == "" {
		return "", time.Time{}, errors.New("Drive token response had no access_token")
	}
	return out.AccessToken, now().Add(time.Duration(out.ExpiresIn) * time.Second), nil
}

// ErrNotConnected: no Google account is connected for Drive (the API answers 503).
var ErrNotConnected = errors.New("Google Drive is not connected. An owner must connect it in Settings.")

// ErrReconnect: Google refused the stored refresh token (invalid_grant: revoked, expired or
// the password changed). The account is treated as disconnected until an owner reconnects.
var ErrReconnect = errors.New("Google Drive access was revoked. An owner must reconnect it in Settings.")

// OAuthToken is one refresh-token grant result.
type OAuthToken struct {
	Access  string
	Expiry  time.Time
	Refresh string // set only when Google rotated the refresh token
}

// OAuthRefreshSource mints access tokens for a member Google account from its refresh token
// (grant_type=refresh_token). Drive caches the access token; this source is asked only when
// the cached one is missing, within a minute of expiry, or refused with a 401.
type OAuthRefreshSource struct {
	ClientID, ClientSecret string
	// RefreshToken returns the account's refresh token, or "" when none is connected. It is
	// read on every refresh so a reconnect or disconnect takes effect at once.
	RefreshToken func(ctx context.Context) (string, error)
	// Saved, when set, persists a minted token (sealed) so other processes can reuse it.
	Saved func(ctx context.Context, t OAuthToken) error
	// Revoked, when set, runs once per refusal when Google answers invalid_grant; it marks the
	// account disconnected and alerts.
	Revoked func(ctx context.Context)
	HTTP    Doer
	URL     string           // token endpoint; "" means Google's
	Now     func() time.Time // nil means time.Now
}

func (s *OAuthRefreshSource) Token(ctx context.Context) (string, time.Time, error) {
	refresh, err := s.RefreshToken(ctx)
	if err != nil {
		return "", time.Time{}, err
	}
	if refresh == "" {
		return "", time.Time{}, ErrNotConnected
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	endpoint := s.URL
	if endpoint == "" {
		endpoint = "https://oauth2.googleapis.com/token"
	}
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {s.ClientID}, "client_secret": {s.ClientSecret}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := s.HTTP.Do(req)
	if err != nil {
		return "", time.Time{}, netError{err}
	}
	defer res.Body.Close()
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		Error        string `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&out)
	if out.Error == "invalid_grant" {
		if s.Revoked != nil {
			s.Revoked(ctx)
		}
		return "", time.Time{}, ErrReconnect
	}
	if res.StatusCode/100 != 2 || out.AccessToken == "" {
		return "", time.Time{}, fmt.Errorf("Drive token refresh failed (%d %s)", res.StatusCode, out.Error)
	}
	t := OAuthToken{Access: out.AccessToken, Expiry: now().Add(time.Duration(out.ExpiresIn) * time.Second), Refresh: out.RefreshToken}
	if s.Saved != nil {
		if err := s.Saved(ctx, t); err != nil {
			return "", time.Time{}, err
		}
	}
	return t.Access, t.Expiry, nil
}

func (d *Drive) accessToken(ctx context.Context) (string, error) {
	d.mu.Lock()
	if d.token != "" && d.expiry.Sub(d.now()) > time.Minute {
		t := d.token
		d.mu.Unlock()
		return t, nil
	}
	d.mu.Unlock()
	// The lock is not held across the token call; concurrent callers may both refresh, which is harmless.
	tok, expiry, err := d.tokens.Token(ctx)
	if err != nil {
		return "", err
	}
	d.mu.Lock()
	d.token, d.expiry = tok, expiry
	d.mu.Unlock()
	return tok, nil
}

func (d *Drive) dropToken() {
	d.mu.Lock()
	d.token = ""
	d.mu.Unlock()
}

// once sends one request to an absolute googleapis URL and returns the response whatever its
// status; the error is a token or transport failure.
func (d *Drive) once(ctx context.Context, method, rawURL string, header http.Header, body []byte) (*http.Response, error) {
	tok, err := d.accessToken(ctx)
	if err != nil {
		return nil, err
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, rd)
	if err != nil {
		return nil, err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	res, err := d.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, netError{err}
	}
	return res, nil
}

// wait says how long to pause before retry number `attempt` (0 = the first retry), and
// whether to retry at all.
func (d *Drive) wait(err error, attempt int) (time.Duration, bool) {
	var ne netError
	var ae *APIError
	switch {
	case errors.As(err, &ae):
		if !ae.retryable() {
			return 0, false
		}
	case errors.As(err, &ne):
	default:
		return 0, false
	}
	backoff := d.backoff << attempt
	if backoff > d.maxWait || backoff <= 0 {
		backoff = d.maxWait
	}
	wait := backoff/2 + d.jitter(backoff/2)
	if ae != nil && ae.retryAfter != "" {
		ra, ok := d.parseRetryAfter(ae.retryAfter)
		if ok && ra > d.maxWait {
			return 0, false // the server asked for longer than we are willing to hold a request
		}
		if ok && ra > wait {
			wait = ra
		}
	}
	return wait, true
}

func (d *Drive) parseRetryAfter(v string) (time.Duration, bool) {
	if s, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && s >= 0 {
		return time.Duration(s) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		w := t.Sub(d.now())
		if w < 0 {
			w = 0
		}
		return w, true
	}
	return 0, false
}

// send makes a request with bounded retries and returns a 2xx response, or the last error.
func (d *Drive) send(ctx context.Context, method, rawURL string, header http.Header, body []byte) (*http.Response, error) {
	refreshed := false
	for attempt := 0; ; attempt++ {
		res, err := d.once(ctx, method, rawURL, header, body)
		if err == nil {
			if res.StatusCode/100 == 2 {
				return res, nil
			}
			ae := apiError(res)
			if ae.Status == http.StatusUnauthorized && !refreshed {
				refreshed = true // an expired or revoked token: fetch a new one once
				d.dropToken()
				attempt--
				continue
			}
			err = ae
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		wait, ok := d.wait(err, attempt)
		if !ok || attempt+1 >= d.tries {
			return nil, err
		}
		if err := d.sleep(ctx, wait); err != nil {
			return nil, err
		}
	}
}

func (d *Drive) call(ctx context.Context, method, path string, header http.Header, body []byte) (*http.Response, error) {
	return d.send(ctx, method, d.base+path, header, body)
}

func (d *Drive) callJSON(ctx context.Context, method, path string, payload any, out any) error {
	var body []byte
	h := http.Header{}
	if payload != nil {
		body, _ = json.Marshal(payload)
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

const folderMIME = "application/vnd.google-apps.folder"

// entryFields is the partial-response field list every Entry is decoded from.
const entryFields = "id,name,mimeType,size,createdTime,modifiedTime,appProperties"

// Entry is one Drive item as List, FindChild, Upsert and UploadStream return it.
type Entry struct {
	ID            string
	Name          string
	MimeType      string
	Size          int64 // 0 for folders
	Created       time.Time
	Modified      time.Time
	AppProperties map[string]string
}

// IsFolder reports whether the entry is a Drive folder.
func (e Entry) IsFolder() bool { return e.MimeType == folderMIME }

type wireEntry struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	MimeType      string            `json:"mimeType"`
	Size          string            `json:"size"`
	CreatedTime   string            `json:"createdTime"`
	ModifiedTime  string            `json:"modifiedTime"`
	AppProperties map[string]string `json:"appProperties"`
}

func (w wireEntry) entry() Entry {
	e := Entry{ID: w.ID, Name: w.Name, MimeType: w.MimeType, AppProperties: w.AppProperties}
	e.Size, _ = strconv.ParseInt(w.Size, 10, 64)
	e.Created, _ = time.Parse(time.RFC3339Nano, w.CreatedTime)
	e.Modified, _ = time.Parse(time.RFC3339Nano, w.ModifiedTime)
	if e.AppProperties == nil {
		e.AppProperties = map[string]string{}
	}
	return e
}

// query lists every item matching the Drive search q in the root's corpus, following pages,
// oldest first (ties broken by id, so concurrent writers agree on "the oldest").
func (d *Drive) query(ctx context.Context, q string, fields string) ([]Entry, error) {
	corpus, err := d.listCorpus(ctx)
	if err != nil {
		return nil, err
	}
	var out []Entry
	token := ""
	for page := 0; ; page++ {
		if page > 10000 {
			return nil, errors.New("Drive list did not finish")
		}
		v := url.Values{
			"q":                         {q},
			"fields":                    {"nextPageToken,files(" + fields + ")"},
			"orderBy":                   {"createdTime"},
			"pageSize":                  {"1000"},
			"supportsAllDrives":         {"true"},
			"includeItemsFromAllDrives": {"true"},
		}
		for k, vv := range corpus {
			v[k] = vv
		}
		if token != "" {
			v.Set("pageToken", token)
		}
		var res struct {
			NextPageToken string      `json:"nextPageToken"`
			Files         []wireEntry `json:"files"`
		}
		if err := d.callJSON(ctx, http.MethodGet, "/drive/v3/files?"+v.Encode(), nil, &res); err != nil {
			return nil, err
		}
		for _, w := range res.Files {
			out = append(out, w.entry())
		}
		if res.NextPageToken == "" {
			break
		}
		token = res.NextPageToken
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if !a.Created.Equal(b.Created) {
			return a.Created.Before(b.Created)
		}
		return a.ID < b.ID
	})
	return out, nil
}

func (d *Drive) findFolders(ctx context.Context, name, parent string) ([]string, error) {
	q := fmt.Sprintf("'%s' in parents and name = '%s' and mimeType = '%s' and trashed = false", escapeQ(parent), escapeQ(name), folderMIME)
	found, err := d.query(ctx, q, "id,createdTime")
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(found))
	for i, f := range found {
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
		"name": name, "mimeType": folderMIME, "parents": []string{parent},
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

// EnsureFolder finds or creates each folder of path below the root folder and returns the
// last one's id (the root's id for an empty path).
func (d *Drive) EnsureFolder(ctx context.Context, path []string) (string, error) {
	parent := d.rootID
	for _, part := range path {
		if strings.TrimSpace(part) == "" {
			return "", errors.New("Drive folder names must not be empty")
		}
		id, err := d.folder(ctx, part, parent)
		if err != nil {
			return "", err
		}
		parent = id
	}
	return parent, nil
}

// FindChild returns the oldest untrashed item (file or folder) called name directly under
// parentID, or nil when there is none.
func (d *Drive) FindChild(ctx context.Context, parentID, name string) (*Entry, error) {
	found, err := d.query(ctx, fmt.Sprintf("'%s' in parents and name = '%s' and trashed = false", escapeQ(parentID), escapeQ(name)), entryFields)
	if err != nil || len(found) == 0 {
		return nil, err
	}
	return &found[0], nil
}

// List returns every untrashed item directly under folderID (all pages), sorted by name.
func (d *Drive) List(ctx context.Context, folderID string) ([]Entry, error) {
	found, err := d.query(ctx, fmt.Sprintf("'%s' in parents and trashed = false", escapeQ(folderID)), entryFields)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].Name < found[j].Name })
	return found, nil
}

func multipartBody(meta any, mimeType string, media []byte) ([]byte, string) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.SetBoundary("cseed_" + auth.UUID())
	mp, _ := mw.CreatePart(textproto.MIMEHeader{"Content-Type": {"application/json; charset=UTF-8"}})
	raw, _ := json.Marshal(meta)
	_, _ = mp.Write(raw)
	fp, _ := mw.CreatePart(textproto.MIMEHeader{"Content-Type": {mimeType}})
	_, _ = fp.Write(media)
	_ = mw.Close()
	return buf.Bytes(), "multipart/related; boundary=" + mw.Boundary()
}

func (d *Drive) uploadJSON(ctx context.Context, method, path string, meta any, mimeType string, media []byte, out any) error {
	body, ct := multipartBody(meta, mimeType, media)
	res, err := d.call(ctx, method, path, http.Header{"Content-Type": {ct}}, body)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(out)
}

// Put uploads a receipt or design file under the folder path below the root folder (or
// below the event's folder when in.Event is set).
func (d *Drive) Put(ctx context.Context, in PutInput) (Stored, error) {
	if err := ValidateUpload(in.MimeType, int64(len(in.Body))); err != nil {
		return Stored{}, err
	}
	var parent string
	var err error
	if in.Event != nil {
		parent, err = EventFolder(ctx, d, in.Event.StartsAtMs, in.Event.Name, in.Event.ID)
		for _, part := range in.Folder {
			if err != nil {
				break
			}
			parent, err = d.folder(ctx, part, parent)
		}
	} else {
		parent, err = d.EnsureFolder(ctx, in.Folder)
	}
	if err != nil {
		return Stored{}, err
	}
	meta := struct {
		Name    string   `json:"name"`
		Parents []string `json:"parents"`
	}{in.Filename, []string{parent}}
	var f struct {
		ID string `json:"id"`
	}
	if err := d.uploadJSON(ctx, http.MethodPost, "/upload/drive/v3/files?uploadType=multipart&supportsAllDrives=true&fields=id", meta, in.MimeType, in.Body, &f); err != nil {
		return Stored{}, err
	}
	if f.ID == "" {
		return Stored{}, errors.New("Drive upload returned no file id")
	}
	return Stored{Storage: "drive", Key: f.ID, SizeBytes: int64(len(in.Body))}, nil
}

// RootInfo describes the configured root folder.
type RootInfo struct {
	ID, Name string
	DriveID  string // the shared drive holding it; "" when it is in someone's My Drive
	// CanAddChildren: the connected account may create folders and files in it.
	CanAddChildren bool
}

// CheckRoot reads the root folder and fails unless it is an untrashed folder.
func (d *Drive) CheckRoot(ctx context.Context) (RootInfo, error) {
	if !driveIDRE.MatchString(d.rootID) {
		return RootInfo{}, errors.New("the Drive root folder id is not valid")
	}
	var f struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		MimeType     string `json:"mimeType"`
		DriveID      string `json:"driveId"`
		Trashed      bool   `json:"trashed"`
		Capabilities struct {
			CanAddChildren bool `json:"canAddChildren"`
		} `json:"capabilities"`
	}
	if err := d.callJSON(ctx, http.MethodGet, "/drive/v3/files/"+url.PathEscape(d.rootID)+"?fields=id,name,mimeType,driveId,trashed,capabilities(canAddChildren)&supportsAllDrives=true", nil, &f); err != nil {
		return RootInfo{}, err
	}
	if f.MimeType != folderMIME || f.Trashed {
		return RootInfo{}, errors.New("the Drive root is not a folder, or it is in the trash")
	}
	return RootInfo{ID: f.ID, Name: f.Name, DriveID: f.DriveID, CanAddChildren: f.Capabilities.CanAddChildren}, nil
}

// UpsertInput is one generated file (an export, a manifest) to write under Folder.
type UpsertInput struct {
	Folder        []string // below the root folder; created when missing
	Name          string
	MimeType      string
	Body          []byte
	AppProperties map[string]string
	// MaxBytes is required: the largest Body this caller accepts.
	MaxBytes int64
	// AllowedMIME, when set, lists the MIME types this caller may write.
	AllowedMIME []string
}

// ErrTooLarge is returned by Upsert and UploadStream when the body is over the caller's limit.
var ErrTooLarge = errors.New("file is larger than allowed")

// Upsert writes Name under Folder: it replaces the content (and merges AppProperties) of an
// existing untrashed file of that name, or creates one. It does not apply the receipt rules
// of ValidateUpload; the caller's MaxBytes and AllowedMIME are its limits.
func (d *Drive) Upsert(ctx context.Context, in UpsertInput) (Entry, error) {
	if strings.TrimSpace(in.Name) == "" || in.MimeType == "" {
		return Entry{}, errors.New("Upsert needs a name and a MIME type")
	}
	if in.MaxBytes <= 0 {
		return Entry{}, errors.New("Upsert needs MaxBytes")
	}
	if int64(len(in.Body)) > in.MaxBytes {
		return Entry{}, fmt.Errorf("%w: %s is %d bytes, the limit is %d", ErrTooLarge, in.Name, len(in.Body), in.MaxBytes)
	}
	if len(in.AllowedMIME) > 0 {
		ok := false
		for _, m := range in.AllowedMIME {
			ok = ok || m == in.MimeType
		}
		if !ok {
			return Entry{}, fmt.Errorf("Upsert: MIME type %q is not allowed", in.MimeType)
		}
	}
	parent, err := d.EnsureFolder(ctx, in.Folder)
	if err != nil {
		return Entry{}, err
	}
	q := fmt.Sprintf("'%s' in parents and name = '%s' and mimeType != '%s' and trashed = false", escapeQ(parent), escapeQ(in.Name), folderMIME)
	found, err := d.query(ctx, q, "id,createdTime")
	if err != nil {
		return Entry{}, err
	}
	var w wireEntry
	if len(found) > 0 {
		meta := map[string]any{"mimeType": in.MimeType}
		if len(in.AppProperties) > 0 {
			meta["appProperties"] = in.AppProperties
		}
		err = d.uploadJSON(ctx, http.MethodPatch, "/upload/drive/v3/files/"+url.PathEscape(found[0].ID)+"?uploadType=multipart&supportsAllDrives=true&fields="+entryFields, meta, in.MimeType, in.Body, &w)
	} else {
		meta := map[string]any{"name": in.Name, "parents": []string{parent}, "mimeType": in.MimeType}
		if len(in.AppProperties) > 0 {
			meta["appProperties"] = in.AppProperties
		}
		err = d.uploadJSON(ctx, http.MethodPost, "/upload/drive/v3/files?uploadType=multipart&supportsAllDrives=true&fields="+entryFields, meta, in.MimeType, in.Body, &w)
	}
	if err != nil {
		return Entry{}, err
	}
	return w.entry(), nil
}

var committedRE = regexp.MustCompile(`^bytes=0-(\d+)$`)

// UploadStream creates a new file Name under folder from r, which must yield exactly size
// bytes, with a resumable upload sent in chunks; a failed chunk is retried from the offset
// Drive confirms. It is for large files (backups); it never replaces an existing file.
func (d *Drive) UploadStream(ctx context.Context, folder []string, name, mimeType string, r io.Reader, size int64, appProps map[string]string) (Entry, error) {
	if strings.TrimSpace(name) == "" || mimeType == "" || size <= 0 {
		return Entry{}, errors.New("UploadStream needs a name, a MIME type and a size")
	}
	parent, err := d.EnsureFolder(ctx, folder)
	if err != nil {
		return Entry{}, err
	}
	meta := map[string]any{"name": name, "parents": []string{parent}, "mimeType": mimeType}
	if len(appProps) > 0 {
		meta["appProperties"] = appProps
	}
	raw, _ := json.Marshal(meta)
	res, err := d.call(ctx, http.MethodPost, "/upload/drive/v3/files?uploadType=resumable&supportsAllDrives=true&fields="+entryFields, http.Header{
		"Content-Type":            {"application/json; charset=UTF-8"},
		"X-Upload-Content-Type":   {mimeType},
		"X-Upload-Content-Length": {strconv.FormatInt(size, 10)},
	}, raw)
	if err != nil {
		return Entry{}, err
	}
	session := res.Header.Get("Location")
	_, _ = io.Copy(io.Discard, res.Body)
	res.Body.Close()
	// The bearer token goes to the session URL, so it must be Drive's own upload endpoint.
	if !strings.HasPrefix(session, d.base+"/upload/") {
		return Entry{}, errors.New("Drive returned an unexpected upload session URL")
	}
	buf := make([]byte, d.chunk)
	total := strconv.FormatInt(size, 10)
	var offset int64 // bytes Drive has confirmed
	for offset < size {
		n := int64(len(buf))
		if size-offset < n {
			n = size - offset
		}
		chunk := buf[:n]
		if _, err := io.ReadFull(r, chunk); err != nil {
			return Entry{}, fmt.Errorf("upload source ended before %d bytes: %w", size, err)
		}
		start := offset
		failures := 0
		for offset < start+n {
			part := chunk[offset-start:]
			hdr := http.Header{"Content-Range": {fmt.Sprintf("bytes %d-%d/%s", offset, offset+int64(len(part))-1, total)}}
			res, err := d.once(ctx, http.MethodPut, session, hdr, part)
			if err == nil {
				switch {
				case res.StatusCode == http.StatusOK || res.StatusCode == http.StatusCreated:
					defer res.Body.Close()
					var w wireEntry
					if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&w); err != nil {
						return Entry{}, err
					}
					if offset+int64(len(part)) != size {
						return Entry{}, errors.New("Drive finished the upload early")
					}
					return w.entry(), nil
				case res.StatusCode == http.StatusPermanentRedirect:
					got, perr := committed(res)
					if perr != nil {
						return Entry{}, perr
					}
					if got < offset {
						return Entry{}, errors.New("Drive lost part of the upload")
					}
					offset = got
					continue
				}
				err = apiError(res)
			}
			if ctx.Err() != nil {
				return Entry{}, ctx.Err()
			}
			wait, ok := d.wait(err, failures)
			failures++
			if !ok || failures >= d.tries {
				return Entry{}, err
			}
			if err := d.sleep(ctx, wait); err != nil {
				return Entry{}, err
			}
			// Ask Drive how much of this chunk it kept before sending the rest.
			res, err = d.once(ctx, http.MethodPut, session, http.Header{"Content-Range": {"bytes */" + total}}, []byte{})
			if err == nil && res.StatusCode == http.StatusPermanentRedirect {
				if got, perr := committed(res); perr == nil && got >= start && got <= start+n {
					offset = got
				}
			} else if err == nil {
				_ = apiError(res)
			}
		}
	}
	return Entry{}, errors.New("Drive did not confirm the upload")
}

// committed reads the byte count Drive confirms in a 308 answer to a resumable upload.
func committed(res *http.Response) (int64, error) {
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, errorBodyLimit))
	r := res.Header.Get("Range")
	if r == "" {
		return 0, nil
	}
	m := committedRE.FindStringSubmatch(r)
	if m == nil {
		return 0, fmt.Errorf("Drive sent an unexpected Range %q", r)
	}
	last, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, err
	}
	return last + 1, nil
}

// driveIDRE matches Drive file ids; anything else is an unknown key.
var driveIDRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,200}$`)

// Download streams a file's content. A missing file is ErrNotFound.
func (d *Drive) Download(ctx context.Context, id string) (io.ReadCloser, error) {
	rc, _, err := d.download(ctx, id)
	return rc, err
}

func (d *Drive) download(ctx context.Context, id string) (io.ReadCloser, string, error) {
	if !driveIDRE.MatchString(id) {
		return nil, "", &APIError{Status: http.StatusNotFound, Reason: "notFound"}
	}
	res, err := d.call(ctx, http.MethodGet, "/drive/v3/files/"+url.PathEscape(id)+"?alt=media&supportsAllDrives=true", nil, nil)
	if err != nil {
		return nil, "", err
	}
	return res.Body, res.Header.Get("Content-Type"), nil
}

// Get downloads a receipt or design file by id; an unknown id returns nil, nil. The bytes are
// checked against the receipt rules again, so a file changed in Drive is never served as-is.
func (d *Drive) Get(ctx context.Context, key string) (*File, error) {
	rc, ct, err := d.download(ctx, key)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	body, err := io.ReadAll(io.LimitReader(rc, MaxBytes+1))
	if err != nil {
		return nil, err
	}
	mt, _, perr := mime.ParseMediaType(ct)
	if perr != nil {
		mt = "application/octet-stream"
	}
	if ValidateUpload(mt, int64(len(body))) != nil || CheckContent(mt, body) != nil {
		return nil, fmt.Errorf("Drive file %s is not an allowed %s upload", key, mt)
	}
	return &File{Body: body, MimeType: mt}, nil
}

// DeleteByID trashes an item (Drive keeps trashed items recoverable for 30 days). An
// unknown id is not an error.
func (d *Drive) DeleteByID(ctx context.Context, id string) error {
	if !driveIDRE.MatchString(id) {
		return nil
	}
	err := d.callJSON(ctx, http.MethodPatch, "/drive/v3/files/"+url.PathEscape(id)+"?supportsAllDrives=true&fields=id", map[string]any{"trashed": true}, nil)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

// Delete trashes a stored receipt (see DeleteByID).
func (d *Drive) Delete(ctx context.Context, key string) error { return d.DeleteByID(ctx, key) }

// RenameFolder renames an item in place.
func (d *Drive) RenameFolder(ctx context.Context, id, name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("Drive folder names must not be empty")
	}
	return d.callJSON(ctx, http.MethodPatch, "/drive/v3/files/"+url.PathEscape(id)+"?supportsAllDrives=true&fields=id", map[string]any{"name": name}, nil)
}

// Usage is how much storage the dashboard's drive holds.
type Usage struct {
	// UsedBytes: on a shared drive, the sum of every file's size in it, trashed files
	// included (they count until purged); otherwise the service account's own usage.
	UsedBytes int64
	// Files is the number of files summed (shared drive only).
	Files int
	// LimitBytes is Drive's own limit when it reports one (0 = unknown or unlimited).
	LimitBytes int64
	// SharedDrive says which way UsedBytes was measured.
	SharedDrive bool
}

// About measures storage for the daily quota check.
func (d *Drive) About(ctx context.Context) (Usage, error) {
	corpus, err := d.listCorpus(ctx)
	if err != nil {
		return Usage{}, err
	}
	if corpus.Get("corpora") == "drive" {
		found, err := d.query(ctx, fmt.Sprintf("mimeType != '%s'", folderMIME), "id,size,createdTime")
		if err != nil {
			return Usage{}, err
		}
		u := Usage{SharedDrive: true, Files: len(found)}
		for _, f := range found {
			u.UsedBytes += f.Size
		}
		return u, nil
	}
	var out struct {
		StorageQuota struct {
			Limit string `json:"limit"`
			Usage string `json:"usage"`
		} `json:"storageQuota"`
	}
	if err := d.callJSON(ctx, http.MethodGet, "/drive/v3/about?fields=storageQuota(limit,usage)", nil, &out); err != nil {
		return Usage{}, err
	}
	u := Usage{}
	u.UsedBytes, _ = strconv.ParseInt(out.StorageQuota.Usage, 10, 64)
	u.LimitBytes, _ = strconv.ParseInt(out.StorageQuota.Limit, 10, 64)
	return u, nil
}
