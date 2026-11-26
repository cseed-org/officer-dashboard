package api

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/calendar"
	"officerdashboard/internal/files"
	"officerdashboard/internal/notify"
	"officerdashboard/internal/store"
	"officerdashboard/internal/validate"
)

// The dashboard stores receipts and design files in a Google Shared Drive, reached as a
// member Google account an owner connects in Settings (OAuth, the org-calendar client).
// Without a connection it falls back to the configured store: a service-account Drive
// (GOOGLE_SERVICE_ACCOUNT_JSON_FILE), local disk, or none (uploads answer 503).

const driveScope = "https://www.googleapis.com/auth/drive"

// driveConsentScope is what the connect flow asks for: the account email for display,
// and full Drive access (the drive.file scope cannot see a shared drive made by someone else).
const driveConsentScope = "openid email " + driveScope

// defaultDriveQuota is DRIVE_QUOTA_BYTES when unset: 100 GB.
const defaultDriveQuota = int64(100e9)

// driveQuotaAlertPercent is the daily check's warning threshold.
const driveQuotaAlertPercent = 70

func (s *Server) driveGoogle() *calendar.Google {
	if s.cfg.Google == nil {
		return nil
	}
	g := *s.cfg.Google
	g.RedirectURL = strings.TrimRight(s.cfg.AppURL, "/") + "/auth/drive-google/callback"
	return &g
}

func (s *Server) driveQuota() int64 {
	if s.cfg.DriveQuotaBytes > 0 {
		return s.cfg.DriveQuotaBytes
	}
	return defaultDriveQuota
}

// storeSwitch is cfg.Files when the OAuth client is configured: each call uses the connected
// account's Drive when there is one (and a root folder), else the fallback store.
type storeSwitch struct {
	s        *Server
	fallback files.Store

	mu    sync.Mutex
	key   string
	drive *files.Drive
}

// errDriveNoRoot: an account is connected but no root folder is chosen yet.
var errDriveNoRoot = fail(503, "not_configured", "Choose the Google Drive folder for dashboard files in Settings.")

func fallbackIsUnavailable(f files.Store) bool {
	_, ok := f.(files.Unavailable)
	return ok
}

// account reads the Drive account row; nil when there has never been one.
func (s *Server) driveAccount(ctx context.Context) (*store.DriveGoogleAccount, error) {
	a, err := s.db.Read().DriveGoogleAccount(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	return a, err
}

func (s *Server) driveRoot(a *store.DriveGoogleAccount) (root string, fromEnv bool) {
	if s.cfg.DriveRootEnv != "" {
		return s.cfg.DriveRootEnv, true
	}
	if a != nil {
		return a.RootFolderID, false
	}
	return "", false
}

// current picks the store for one call. Its error explains why nothing can store files.
func (w *storeSwitch) current(ctx context.Context) (files.Store, error) {
	a, err := w.s.driveAccount(ctx)
	if err != nil {
		return nil, err
	}
	root, _ := w.s.driveRoot(a)
	if a != nil && a.Connected() {
		if root == "" {
			if fallbackIsUnavailable(w.fallback) {
				return nil, errDriveNoRoot
			}
			return w.fallback, nil
		}
		return w.accountDrive(a, root), nil
	}
	if fallbackIsUnavailable(w.fallback) {
		if a != nil && a.RevokedAt != nil {
			return nil, files.ErrReconnect
		}
		return nil, files.ErrNotConnected
	}
	return w.fallback, nil
}

// accountDrive returns the cached Drive for this grant and root, building one when either changed.
func (w *storeSwitch) accountDrive(a *store.DriveGoogleAccount, root string) *files.Drive {
	key := a.Subject + "\x00" + strconv.FormatInt(a.ConnectedAt, 10) + "\x00" + root
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.drive == nil || w.key != key {
		w.drive, w.key = w.s.newAccountDrive(a, root), key
	}
	return w.drive
}

func (w *storeSwitch) forget() {
	w.mu.Lock()
	w.drive, w.key = nil, ""
	w.mu.Unlock()
}

func (w *storeSwitch) Put(ctx context.Context, in files.PutInput) (files.Stored, error) {
	st, err := w.current(ctx)
	if err != nil {
		return files.Stored{}, err
	}
	return st.Put(ctx, in)
}

func (w *storeSwitch) Get(ctx context.Context, key string) (*files.File, error) {
	st, err := w.current(ctx)
	if err != nil {
		return nil, err
	}
	return st.Get(ctx, key)
}

func (w *storeSwitch) Delete(ctx context.Context, key string) error {
	st, err := w.current(ctx)
	if err != nil {
		return err
	}
	return st.Delete(ctx, key)
}

func (s *Server) driveHTTP() files.Doer {
	if s.cfg.DriveHTTP != nil {
		return s.cfg.DriveHTTP
	}
	return nil
}

func (s *Server) newAccountDrive(a *store.DriveGoogleAccount, root string) *files.Drive {
	return files.NewDriveWithTokens(&driveTokens{s: s, subject: a.Subject, connectedAt: a.ConnectedAt}, root, s.driveHTTP())
}

// driveTokens is the connected account's TokenSource: the sealed access token while it is
// fresh, else a refresh-token grant whose result is sealed back into the row.
type driveTokens struct {
	s           *Server
	subject     string
	connectedAt int64
}

func (t *driveTokens) Token(ctx context.Context) (string, time.Time, error) {
	s := t.s
	a, err := s.driveAccount(ctx)
	if err != nil {
		return "", time.Time{}, err
	}
	if a == nil || a.Subject != t.subject || a.ConnectedAt != t.connectedAt {
		return "", time.Time{}, files.ErrNotConnected // disconnected or replaced since this Drive was built
	}
	if a.RevokedAt != nil {
		return "", time.Time{}, files.ErrReconnect
	}
	if !a.Connected() {
		return "", time.Time{}, files.ErrNotConnected
	}
	if a.ExpiresAt > s.nowMs()+60_000 {
		if access, err := s.sealer.Open(a.SealedAccess); err == nil && access != "" {
			return access, time.UnixMilli(a.ExpiresAt), nil
		}
	}
	g := s.driveGoogle()
	if g == nil {
		return "", time.Time{}, files.ErrNotConnected
	}
	src := &files.OAuthRefreshSource{
		ClientID: g.ClientID, ClientSecret: g.ClientSecret, URL: g.TokenURL, HTTP: g.HTTP, Now: s.now,
		RefreshToken: func(context.Context) (string, error) { return s.sealer.Open(a.SealedRefresh) },
		Saved: func(ctx context.Context, tok files.OAuthToken) error {
			refresh := ""
			if tok.Refresh != "" {
				refresh = s.sealer.Seal(tok.Refresh)
			}
			return s.db.Write(ctx, func(q *store.Queries) error {
				_, err := q.SaveDriveAccess(ctx, t.subject, t.connectedAt, s.sealer.Seal(tok.Access), tok.Expiry.UnixMilli(), refresh)
				return err
			})
		},
		Revoked: func(ctx context.Context) { s.driveRevoked(ctx, t.subject, t.connectedAt) },
	}
	if g.HTTP == nil {
		src.HTTP = http.DefaultClient
	}
	return src.Token(ctx)
}

// driveRevoked marks the grant Google refused as disconnected and alerts once.
func (s *Server) driveRevoked(ctx context.Context, subject string, connectedAt int64) {
	var changed bool
	err := s.db.Write(context.WithoutCancel(ctx), func(q *store.Queries) error {
		var err error
		changed, err = q.MarkDriveGoogleRevoked(ctx, subject, connectedAt, s.nowMs())
		return err
	})
	if err != nil {
		s.log.Error("could not record the Drive revocation", "error", err)
		return
	}
	if changed {
		s.log.Warn("Google refused the Drive refresh token; Drive is disconnected until an owner reconnects it")
		s.cfg.Alerts.Alert(ctx, "drive-revoked", "Google Drive access for the dashboard was revoked or expired. Uploads are paused until an owner reconnects Google Drive in Settings.")
	}
}

// Drive returns the Drive the dashboard currently stores files in (the connected account's,
// or the service-account fallback). It fails with files.ErrNotConnected, files.ErrReconnect
// or a 503 APIError when there is none. The event export and quota check use it.
func (s *Server) Drive(ctx context.Context) (*files.Drive, error) {
	st := s.cfg.Files
	if w, ok := st.(*storeSwitch); ok {
		cur, err := w.current(ctx)
		if err != nil {
			return nil, err
		}
		st = cur
	}
	if d, ok := st.(*files.Drive); ok {
		return d, nil
	}
	return nil, files.ErrNotConnected
}

// Alerts is the server's ops alerter (one per key per hour), for loops started beside it
// such as the Drive event export.
func (s *Server) Alerts() *notify.Alerter { return s.cfg.Alerts }

// storeFile uploads through cfg.Files and alerts when Drive is full or throttling us; the
// API then answers 507 storage_quota (toAPIError), so the upload is never lost silently.
func (s *Server) storeFile(ctx context.Context, in files.PutInput) (files.Stored, error) {
	stored, err := s.cfg.Files.Put(ctx, in)
	switch {
	case errors.Is(err, files.ErrQuota):
		s.cfg.Alerts.Alert(ctx, "drive-quota-full", "Google Drive refused a dashboard upload: the shared drive is out of storage. Free space or raise the drive's limit; uploads fail until then.")
	case errors.Is(err, files.ErrRateLimited):
		s.cfg.Alerts.Alert(ctx, "drive-rate-limited", "Google Drive is rate limiting the dashboard; an upload failed after retries.")
	}
	return stored, err
}

// storageKind names the backend files are stored in now, as the storage column records it,
// so a download or delete after a backend switch never hands one backend the other's keys.
func (s *Server) storageKind(ctx context.Context) string {
	st := s.cfg.Files
	if w, ok := st.(*storeSwitch); ok {
		cur, err := w.current(ctx)
		if err != nil {
			return ""
		}
		st = cur
	}
	switch st.(type) {
	case files.Local, *files.Local:
		return "local"
	case *files.Drive:
		return "drive"
	}
	return ""
}

// DriveQuotaDaily checks Drive usage against DRIVE_QUOTA_BYTES once a day until ctx ends,
// alerting at 70%.
func (s *Server) DriveQuotaDaily(ctx context.Context) {
	wait := 5 * time.Minute // let startup settle; then daily
	for {
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		s.bg.Add(1)
		s.checkDriveQuota(ctx)
		s.bg.Done()
		wait = 24 * time.Hour
	}
}

// checkDriveQuota measures usage once; it returns the percent used (-1 when not measured).
func (s *Server) checkDriveQuota(ctx context.Context) int64 {
	d, err := s.Drive(ctx)
	if err != nil {
		return -1
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	u, err := d.About(ctx)
	if err != nil {
		s.log.Warn("Drive quota check failed", "error", err)
		return -1
	}
	quota := s.driveQuota()
	pct := u.UsedBytes * 100 / quota
	if pct >= driveQuotaAlertPercent {
		s.cfg.Alerts.Alert(ctx, "drive-quota", fmt.Sprintf("Google Drive storage is at %d%% of the dashboard's %s limit (%s used). Free space or raise DRIVE_QUOTA_BYTES before uploads fail.", pct, gb(quota), gb(u.UsedBytes)))
	}
	return pct
}

func gb(n int64) string { return fmt.Sprintf("%.1f GB", float64(n)/1e9) }

// ─── owner endpoints: GET status, connect, callback, DELETE, PUT root ───

func (c *Ctx) driveOwner() error {
	_, err := c.ownerIn(c.S.db.Read())
	return err
}

func (c *Ctx) driveStatus() error {
	if err := c.driveOwner(); err != nil {
		return err
	}
	writeJSON(c.W, 200, c.S.driveStatusBody(c.Context()))
	return nil
}

// driveStatusBody is the Settings view of Drive. It never carries tokens.
func (s *Server) driveStatusBody(ctx context.Context) map[string]any {
	a, _ := s.driveAccount(ctx)
	root, fromEnv := s.driveRoot(a)
	out := map[string]any{
		"configured": s.cfg.Google != nil, "connected": a != nil && a.Connected(), "email": nil,
		"needsReconnect": a != nil && a.RevokedAt != nil, "rootFolderId": root, "rootFromEnv": fromEnv,
		"rootReachable": false, "rootName": nil, "sharedDrive": false, "canWrite": false,
		"storage": s.storageKind(ctx), "usage": nil, "error": nil,
	}
	if a != nil && a.Email != "" {
		out["email"] = a.Email
	}
	d, err := s.Drive(ctx)
	if err != nil {
		return out
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	info, err := d.CheckRoot(ctx)
	if err != nil {
		out["error"] = driveProblem(err)
		return out
	}
	out["rootReachable"], out["rootName"], out["sharedDrive"], out["canWrite"] = true, info.Name, info.DriveID != "", info.CanAddChildren
	if u, err := d.About(ctx); err == nil {
		out["usage"] = map[string]any{"usedBytes": u.UsedBytes, "quotaBytes": s.driveQuota(), "percent": u.UsedBytes * 100 / s.driveQuota()}
	}
	return out
}

// driveProblem is a short, token-free explanation of a Drive failure for Settings.
func driveProblem(err error) string {
	switch {
	case errors.Is(err, files.ErrReconnect):
		return files.ErrReconnect.Error()
	case errors.Is(err, files.ErrNotFound):
		return "The connected account cannot see that folder. Check the folder id and that the account is a member of the shared drive."
	case errors.Is(err, files.ErrQuota):
		return files.ErrQuota.Error()
	case errors.Is(err, files.ErrRateLimited):
		return files.ErrRateLimited.Error()
	}
	var ae *files.APIError
	if errors.As(err, &ae) {
		return ae.Error()
	}
	return "Google Drive could not be reached."
}

func (c *Ctx) driveConnect() error {
	if err := c.driveOwner(); err != nil {
		return err
	}
	g := c.S.driveGoogle()
	if g == nil {
		return fail(503, "not_configured", "Google OAuth is not configured (GOOGLE_OAUTH_CLIENT_ID / GOOGLE_OAUTH_CLIENT_SECRET)")
	}
	state, verifier := auth.RandomToken(32), auth.RandomToken(32)
	for name, v := range map[string]string{"drive_google_state": state, "drive_google_verifier": verifier, "drive_google_user": c.User.ID} {
		c.S.setCookie(c.W, c.S.hostPrefixed(name), v, c.S.now().Add(10*time.Minute), 600)
	}
	consent, err := url.Parse(g.ConsentURL(state, pkceChallenge(verifier)))
	if err != nil {
		return err
	}
	q := consent.Query()
	q.Set("scope", driveConsentScope)
	q.Del("include_granted_scopes") // this grant carries Drive only, never the calendar scopes
	consent.RawQuery = q.Encode()
	http.Redirect(c.W, c.R, consent.String(), http.StatusFound)
	return nil
}

func (c *Ctx) driveCallback() error {
	stored := c.cookie(c.S.hostPrefixed("drive_google_state"))
	verifier := c.cookie(c.S.hostPrefixed("drive_google_verifier"))
	user := c.cookie(c.S.hostPrefixed("drive_google_user"))
	for _, n := range []string{"drive_google_state", "drive_google_verifier", "drive_google_user"} {
		c.S.clearCookie(c.W, c.S.hostPrefixed(n))
	}
	back := func(reason string) error {
		http.Redirect(c.W, c.R, "/settings?drive="+reason, http.StatusFound)
		return nil
	}
	if err := c.driveOwner(); err != nil {
		return err
	}
	if c.User.ID != user {
		return back("failed")
	}
	v := c.R.URL.Query()
	state := v.Get("state")
	if state == "" || stored == "" || verifier == "" || subtle.ConstantTimeCompare([]byte(state), []byte(stored)) != 1 || v.Get("code") == "" {
		return back("failed")
	}
	g := c.S.driveGoogle()
	if g == nil {
		return back("not_configured")
	}
	tok, err := g.Exchange(c.Context(), v.Get("code"), verifier)
	if err != nil {
		return back("failed")
	}
	account := calendar.AccountFromIDToken(tok.IDToken)
	if tok.RefreshToken == "" || account.Sub == "" || account.Email == "" {
		return back("failed")
	}
	hasScope := false
	for _, scope := range strings.Fields(tok.Scope) {
		hasScope = hasScope || scope == driveScope
	}
	if !hasScope {
		g.Revoke(c.Context(), tok.RefreshToken)
		return back("scope_required")
	}
	a := store.DriveGoogleAccount{Subject: account.Sub, Email: account.Email, SealedRefresh: c.S.sealer.Seal(tok.RefreshToken),
		SealedAccess: c.S.sealer.Seal(tok.AccessToken), ExpiresAt: c.S.nowMs() + tok.ExpiresIn*1000, ConnectedBy: &c.User.ID, ConnectedAt: c.S.nowMs()}
	err = c.S.db.Write(c.Context(), func(q *store.Queries) error {
		if _, err := c.ownerIn(q); err != nil {
			return err
		}
		if err := q.ConnectDriveGoogle(c.Context(), a); err != nil {
			return err
		}
		return c.audit(c.Context(), q, "drive.google_connected", "drive_google_account", "1", nil, map[string]any{"email": a.Email})
	})
	if err != nil {
		return err
	}
	if w, ok := c.S.cfg.Files.(*storeSwitch); ok {
		w.forget()
	}
	return back("connected")
}

func (c *Ctx) driveDisconnect() error {
	if err := c.driveOwner(); err != nil {
		return err
	}
	a, err := c.S.driveAccount(c.Context())
	if err != nil {
		return err
	}
	err = c.S.db.Write(c.Context(), func(q *store.Queries) error {
		if _, err := c.ownerIn(q); err != nil {
			return err
		}
		if err := q.DisconnectDriveGoogle(c.Context()); err != nil {
			return err
		}
		return c.audit(c.Context(), q, "drive.google_disconnected", "drive_google_account", "1", nil, nil)
	})
	if err != nil {
		return err
	}
	if w, ok := c.S.cfg.Files.(*storeSwitch); ok {
		w.forget()
	}
	// Revoke after the row is gone, best effort: Google forgets the grant too.
	if g := c.S.driveGoogle(); g != nil && a != nil && a.SealedRefresh != "" {
		if refresh, err := c.S.sealer.Open(a.SealedRefresh); err == nil {
			g.Revoke(context.WithoutCancel(c.Context()), refresh)
		}
	}
	return c.driveStatus()
}

var driveFolderURL = regexp.MustCompile(`^https://drive\.google\.com/(?:drive/(?:u/\d+/)?folders/|open\?id=)([A-Za-z0-9_-]+)`)
var driveFolderID = regexp.MustCompile(`^[A-Za-z0-9_-]{10,200}$`)

// driveRootPut sets the root folder (an id or a drive.google.com folder link) after checking
// that the connected account can add files to it.
func (c *Ctx) driveRootPut() error {
	if err := c.driveOwner(); err != nil {
		return err
	}
	var folder validate.Field[string]
	if err := c.jsonBody(func(r *validate.Reader) {
		folder = r.Str("folderId", validate.S{Req: true, Trim: true, Min: 1, Max: 2048})
	}); err != nil {
		return err
	}
	if c.S.cfg.DriveRootEnv != "" {
		return conflict("DRIVE_ROOT_FOLDER_ID is set on the server, so the folder is chosen there.")
	}
	id := folder.V
	if m := driveFolderURL.FindStringSubmatch(id); m != nil {
		id = m[1]
	}
	if !driveFolderID.MatchString(id) {
		return badRequest("Paste the shared drive folder's link or id.")
	}
	a, err := c.S.driveAccount(c.Context())
	if err != nil {
		return err
	}
	if a == nil || !a.Connected() {
		return conflict("Connect a Google account first.")
	}
	ctx, cancel := context.WithTimeout(c.Context(), 30*time.Second)
	defer cancel()
	info, err := c.S.newAccountDrive(a, id).CheckRoot(ctx)
	if err != nil {
		return badRequest(driveProblem(err))
	}
	if !info.CanAddChildren {
		return badRequest("The connected account cannot add files to that folder. Give it Content manager access on the shared drive.")
	}
	err = c.S.db.Write(c.Context(), func(q *store.Queries) error {
		if _, err := c.ownerIn(q); err != nil {
			return err
		}
		if err := q.SetDriveRoot(c.Context(), id); err != nil {
			return err
		}
		return c.audit(c.Context(), q, "drive.root_set", "drive_google_account", "1", nil, map[string]any{"folderId": id})
	})
	if err != nil {
		return err
	}
	return c.driveStatus()
}
