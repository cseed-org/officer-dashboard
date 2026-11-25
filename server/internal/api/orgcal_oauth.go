package api

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"officerdashboard/internal/auth"
	"officerdashboard/internal/calendar"
	"officerdashboard/internal/orgcal"
	"officerdashboard/internal/store"
	"strings"
	"time"
)

func (s *Server) orgGoogle() *calendar.Google {
	if s.cfg.Google == nil {
		return nil
	}
	g := *s.cfg.Google
	g.RedirectURL = strings.TrimRight(s.cfg.AppURL, "/") + "/auth/org-google/callback"
	return &g
}

// orgGoogleState reports the org account (nil email when none is connected), whether
// Google revoked its access, and the last recorded pass error ("" when none).
func (c *Ctx) orgGoogleState() (email *string, reconnect bool, lastErr string, err error) {
	a, err := c.S.db.Read().OrgGoogleAccount(c.Context())
	if err == nil {
		email = &a.Email
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, false, "", err
	}
	if email == nil {
		return nil, false, "", nil
	}
	c.S.orgRunMu.Lock()
	defer c.S.orgRunMu.Unlock()
	return email, c.S.orgNeedsReauth, c.S.orgLastError, nil
}
func (c *Ctx) orgGoogleStatus() error {
	if _, err := c.requireLeadership(); err != nil {
		return err
	}
	email, reconnect, last, err := c.orgGoogleState()
	if err != nil {
		return err
	}
	var lastErr any
	if last != "" {
		lastErr = "Calendar sync failed. Retry or reconnect the calendar account."
	}
	writeJSON(c.W, 200, map[string]any{"configured": c.S.cfg.Google != nil, "connected": email != nil, "email": email, "needsReconnect": reconnect, "lastSyncError": lastErr})
	return nil
}
func (c *Ctx) orgGoogleConnect() error {
	if _, err := c.requireLeadership(); err != nil {
		return err
	}
	g := c.S.orgGoogle()
	if g == nil {
		return fail(503, "not_configured", "Google Calendar OAuth is not configured")
	}
	state, verifier := auth.RandomToken(32), auth.RandomToken(32)
	for name, v := range map[string]string{"org_google_state": state, "org_google_verifier": verifier, "org_google_user": c.User.ID} {
		c.S.setCookie(c.W, c.S.hostPrefixed(name), v, c.S.now().Add(10*time.Minute), 600)
	}
	http.Redirect(c.W, c.R, (orgcal.Client{OAuth: g}).ConsentURL(state, pkceChallenge(verifier)), http.StatusFound)
	return nil
}
func (c *Ctx) orgGoogleCallback() error {
	stored := c.cookie(c.S.hostPrefixed("org_google_state"))
	verifier := c.cookie(c.S.hostPrefixed("org_google_verifier"))
	user := c.cookie(c.S.hostPrefixed("org_google_user"))
	for _, n := range []string{"org_google_state", "org_google_verifier", "org_google_user"} {
		c.S.clearCookie(c.W, c.S.hostPrefixed(n))
	}
	back := func(reason string) error {
		http.Redirect(c.W, c.R, "/settings?orgGoogle="+reason, http.StatusFound)
		return nil
	}
	if _, err := c.requireLeadership(); err != nil {
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
	g := c.S.orgGoogle()
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
		hasScope = hasScope || scope == "https://www.googleapis.com/auth/calendar"
	}
	if !hasScope {
		return back("scope_required")
	}
	c.S.cancelOrgPass()
	c.S.orgMu.Lock()
	a := store.OrgGoogleAccount{Subject: account.Sub, Email: account.Email, SealedRefresh: c.S.sealer.Seal(tok.RefreshToken), SealedAccess: c.S.sealer.Seal(tok.AccessToken), ExpiresAt: c.S.nowMs() + tok.ExpiresIn*1000, ConnectedBy: &c.User.ID}
	err = c.S.db.Write(c.Context(), func(q *store.Queries) error {
		if _, err := c.requireLeadershipIn(q); err != nil {
			return err
		}
		old, err := q.OrgGoogleAccount(c.Context())
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if err != nil || old.Subject != a.Subject {
			if err = q.ResetOrgGoogle(c.Context()); err != nil {
				return err
			}
		}
		if err = q.SaveOrgGoogleAccount(c.Context(), a); err != nil {
			return err
		}
		return c.audit(c.Context(), q, "orgcal.google_connected", "org_google_account", "1", nil, map[string]any{"email": a.Email})
	})
	if err != nil {
		c.S.orgMu.Unlock()
		return err
	}
	c.S.orgNextACL = time.Time{}
	c.S.orgNextPull = time.Time{}
	c.S.orgRetryAt = time.Time{}
	c.S.orgMu.Unlock()
	c.S.recordOrgSync(nil)
	c.S.kickOrgCalendar()
	return back("connected")
}
func (c *Ctx) orgGoogleDisconnect() error {
	if _, err := c.requireLeadership(); err != nil {
		return err
	}
	c.S.cancelOrgPass()
	c.S.orgMu.Lock()
	err := c.S.db.Write(c.Context(), func(q *store.Queries) error {
		if _, err := c.requireLeadershipIn(q); err != nil {
			return err
		}
		if err := q.DisconnectOrgGoogle(c.Context()); err != nil {
			return err
		}
		return c.audit(c.Context(), q, "orgcal.google_disconnected", "org_google_account", "1", nil, nil)
	})
	c.S.orgMu.Unlock()
	if err != nil {
		return err
	}
	c.S.recordOrgSync(nil)
	return c.orgGoogleStatus()
}

// orgMu serializes token refresh with account replacement/disconnect.
func (s *Server) orgAccess(ctx context.Context) (string, error) {
	a, err := s.db.Read().OrgGoogleAccount(ctx)
	if err != nil {
		return "", err
	}
	if a.ExpiresAt > s.nowMs()+60000 {
		return s.sealer.Open(a.SealedAccess)
	}
	refresh, err := s.sealer.Open(a.SealedRefresh)
	if err != nil {
		return "", err
	}
	tok, err := s.orgGoogle().Refresh(ctx, refresh)
	if err != nil {
		return "", err
	}
	a.SealedAccess = s.sealer.Seal(tok.AccessToken)
	a.ExpiresAt = s.nowMs() + tok.ExpiresIn*1000
	if tok.RefreshToken != "" {
		a.SealedRefresh = s.sealer.Seal(tok.RefreshToken)
	}
	err = s.db.Write(ctx, func(q *store.Queries) error { return q.SaveOrgGoogleAccount(ctx, *a) })
	return tok.AccessToken, err
}
