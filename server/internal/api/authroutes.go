package api

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/store"
)

func (c *Ctx) health() error {
	body := map[string]any{"ok": true, "db": c.S.db.Ping(c.Context()) == nil}
	// The build version helps attackers match known bugs; only signed-in users see it.
	if c.User != nil {
		body["version"] = c.S.cfg.Version
	}
	writeJSON(c.W, 200, body)
	return nil
}

func (c *Ctx) me() error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	st, err := c.S.onboardingState(c.Context(), c.S.db.Read(), u.ID)
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"user": currentUser(u), "onboarding": st})
	return nil
}

// authLogin starts Discord OAuth2 with PKCE: state, verifier and the post-login target
// travel in short-lived httpOnly cookies.
func (c *Ctx) authLogin() error {
	d := c.S.cfg.Discord
	if d == nil {
		http.Error(c.W, "Discord sign-in is not configured (DISCORD_CLIENT_ID / DISCORD_CLIENT_SECRET)", http.StatusServiceUnavailable)
		return nil
	}
	state, verifier := auth.RandomToken(32), auth.RandomToken(32)
	for name, value := range map[string]string{oauthStateName: state, oauthVerifier: verifier, nextCookieName: auth.SafeNext(c.R.URL.Query().Get("next"))} {
		c.S.setCookie(c.W, c.S.hostPrefixed(name), value, c.S.now().Add(10*60e9), 600)
	}
	// An invite link marks this sign-in as an invite acceptance, so a wrong Discord
	// account gets the "invite unavailable" screen instead of "not allowlisted".
	if t := c.R.URL.Query().Get("invite"); t != "" && len(t) <= 128 {
		c.S.setCookie(c.W, c.S.hostPrefixed(inviteCookieName), auth.SHA256Hex(t), c.S.now().Add(10*60e9), 600)
	} else {
		c.S.clearCookie(c.W, c.S.hostPrefixed(inviteCookieName))
	}
	// A Launchpad invite (builder/mentor portal) binds to whichever Discord account signs in;
	// portal=1 asks for a portal session rather than the officer dashboard.
	if t := c.R.URL.Query().Get("launchpad"); t != "" && len(t) <= 128 {
		c.S.setCookie(c.W, c.S.hostPrefixed(lpInviteCookieName), auth.SHA256Hex(t), c.S.now().Add(10*60e9), 600)
	} else {
		c.S.clearCookie(c.W, c.S.hostPrefixed(lpInviteCookieName))
	}
	if c.R.URL.Query().Get("portal") == "1" {
		c.S.setCookie(c.W, c.S.hostPrefixed(lpLoginCookieName), "1", c.S.now().Add(10*60e9), 600)
	} else {
		c.S.clearCookie(c.W, c.S.hostPrefixed(lpLoginCookieName))
	}
	http.Redirect(c.W, c.R, d.AuthURL(state, verifier), http.StatusFound)
	return nil
}

func (c *Ctx) cookie(name string) string {
	if ck, err := c.R.Cookie(name); err == nil {
		return ck.Value
	}
	return ""
}

func (c *Ctx) loginRedirect(reason string) error {
	http.Redirect(c.W, c.R, "/login?error="+reason, http.StatusFound)
	return nil
}

// authCallback completes the sign-in: only allowlisted users (or the bootstrap director)
// get a session.
func (c *Ctx) authCallback() error {
	q := c.R.URL.Query()
	code, state := q.Get("code"), q.Get("state")
	storedState, verifier := c.cookie(c.S.hostPrefixed(oauthStateName)), c.cookie(c.S.hostPrefixed(oauthVerifier))
	next := auth.SafeNext(c.cookie(c.S.hostPrefixed(nextCookieName)))
	inviteHash := c.cookie(c.S.hostPrefixed(inviteCookieName))
	fromInvite := inviteHash != ""
	lpInvite, lpLogin := c.cookie(c.S.hostPrefixed(lpInviteCookieName)), c.cookie(c.S.hostPrefixed(lpLoginCookieName)) == "1"
	for _, n := range []string{oauthStateName, oauthVerifier, nextCookieName, inviteCookieName, lpInviteCookieName, lpLoginCookieName} {
		c.S.clearCookie(c.W, c.S.hostPrefixed(n))
	}
	// A Launchpad sign-in that fails before Discord answers goes back to the portal page it
	// started from, not the officer login.
	oauthFailed := func(reason string) error {
		switch {
		case lpInvite != "":
			return c.portalRedirect("/launchpad/join?error=" + reason)
		case lpLogin:
			return c.portalRedirect("/launchpad/login?error=" + reason)
		}
		return c.loginRedirect(reason)
	}
	if code == "" || state == "" || storedState == "" || verifier == "" || subtle.ConstantTimeCompare([]byte(state), []byte(storedState)) != 1 {
		return oauthFailed("state")
	}
	d := c.S.cfg.Discord
	if d == nil {
		return oauthFailed("discord")
	}
	ctx := c.Context()
	profile, err := d.Exchange(ctx, code, verifier)
	if err != nil {
		c.S.log.Error("Discord OAuth failed", "error", err)
		return oauthFailed("discord")
	}
	// Launchpad portal sign-ins: an invite binds first, then an explicit portal sign-in.
	if lpInvite != "" {
		return c.portalAccept(profile.ID, lpInvite)
	}
	if lpLogin {
		_, err := c.portalSignIn(profile.ID)
		return err
	}
	var userID string
	var denied string
	var avatar *string
	if a := profile.AvatarURL(); a != "" {
		avatar = &a
	}
	err = c.S.db.Write(ctx, func(w *store.Queries) error {
		existing, err := w.UserByDiscordID(ctx, profile.ID)
		var known *auth.KnownUser
		switch {
		case err == nil:
			known = &auth.KnownUser{ID: existing.ID, Role: existing.Role, Active: existing.Active}
		case !errors.Is(err, store.ErrNotFound):
			return err
		default:
			// No account yet: a pending invite for this Discord ID creates one.
			id, reason, err := c.S.acceptInvite(ctx, w, profile.ID, avatar, inviteHash)
			if err != nil {
				return err
			}
			if reason != "" {
				denied = reason
				return nil
			}
			if id != "" {
				known = &auth.KnownUser{ID: id, Role: auth.RoleOfficer, Active: true}
			} else if fromInvite {
				denied = "invite_unavailable"
				return nil
			}
		}
		decision := auth.DecideLogin(known)
		switch decision.Kind {
		case auth.Deny:
			denied = decision.Reason
			return nil
		default:
			userID = decision.UserID
			return w.SetAvatar(ctx, userID, avatar)
		}
	})
	if err != nil {
		c.S.log.Error("sign-in failed", "error", err)
		return c.loginRedirect("discord")
	}
	if denied == "invite_unavailable" {
		http.Redirect(c.W, c.R, "/onboarding?unavailable=1", http.StatusFound)
		return nil
	}
	if denied == "not_allowlisted" && !fromInvite {
		// No officer account: a builder or mentor who used the officer button still gets in.
		if id, reason, err := c.S.portalAccountFor(ctx, profile.ID); err == nil && reason == "" {
			if err := c.S.createPortalSession(ctx, c.W, c.R, id); err == nil {
				return c.portalRedirect("/launchpad")
			}
		}
	}
	if denied != "" {
		return c.loginRedirect(denied)
	}
	if err := c.newSession(ctx, userID); err != nil {
		c.S.log.Error("create session failed", "error", err)
		return c.loginRedirect("discord")
	}
	// Anyone who hasn't finished the current onboarding flow resumes it.
	if st, err := c.S.onboardingState(ctx, c.S.db.Read(), userID); err == nil && st != nil {
		next = "/onboarding"
	}
	http.Redirect(c.W, c.R, next, http.StatusFound)
	return nil
}

// authLogout is POST-only (a form button), so a cross-site link can't sign people out.
// A form field next=/login?invite=<token> returns there afterwards (an invite link opened in a browser
// where someone else was signed in); anything else lands on /login.
func (c *Ctx) authLogout() error {
	if !c.S.originOK(c.R) {
		return forbidden("Cross-origin request blocked")
	}
	c.R.Body = http.MaxBytesReader(c.W, c.R.Body, 8<<10)
	var formErr error
	if strings.HasPrefix(c.R.Header.Get("Content-Type"), "multipart/form-data") {
		formErr = c.R.ParseMultipartForm(8 << 10)
		if c.R.MultipartForm != nil {
			defer c.R.MultipartForm.RemoveAll()
		}
	} else {
		formErr = c.R.ParseForm()
	}
	if formErr != nil {
		return badRequest("Invalid logout form")
	}
	if c.SessionID != "" {
		ctx := context.WithoutCancel(c.Context())
		_ = c.S.db.Write(ctx, func(w *store.Queries) error { return w.DeleteSession(ctx, c.SessionID) })
	}
	c.S.clearCookie(c.W, c.S.sessionCookieName())
	target := "/login"
	if next := c.R.PostForm.Get("next"); inviteLoginRE.MatchString(next) {
		target = next
	}
	http.Redirect(c.W, c.R, target, http.StatusSeeOther)
	return nil
}

// inviteLoginRE is the one place logout may return to besides /login: an invite link.
var inviteLoginRE = regexp.MustCompile(`^/login\?invite=[A-Za-z0-9_-]{1,128}$`)
