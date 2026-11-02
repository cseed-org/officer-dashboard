package api

import (
	"context"
	"errors"
	"net/http"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/store"
)

func (c *Ctx) health() error {
	db := c.S.db.Ping(c.Context()) == nil
	writeJSON(c.W, 200, map[string]any{"ok": true, "db": db, "version": c.S.cfg.Version})
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
	for _, n := range []string{oauthStateName, oauthVerifier, nextCookieName, inviteCookieName} {
		c.S.clearCookie(c.W, c.S.hostPrefixed(n))
	}
	if code == "" || state == "" || storedState == "" || verifier == "" || state != storedState {
		return c.loginRedirect("state")
	}
	d := c.S.cfg.Discord
	if d == nil {
		return c.loginRedirect("discord")
	}
	ctx := c.Context()
	profile, err := d.Exchange(ctx, code, verifier)
	if err != nil {
		c.S.log.Error("Discord OAuth failed", "error", err)
		return c.loginRedirect("discord")
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
	if denied != "" {
		return c.loginRedirect(denied)
	}
	if err := c.S.createSession(ctx, c.W, userID, c.R.UserAgent()); err != nil {
		c.S.log.Error("create session failed", "error", err)
		return c.loginRedirect("discord")
	}
	// Invitees who haven't finished setup always resume onboarding.
	if inv, err := c.S.db.Read().InviteForUser(ctx, userID); err == nil && inv.OnboardedAt == nil {
		next = "/onboarding"
	}
	http.Redirect(c.W, c.R, next, http.StatusFound)
	return nil
}

// authLogout is POST-only (a form button), so a cross-site link can't sign people out.
func (c *Ctx) authLogout() error {
	if c.SessionID != "" {
		ctx := context.WithoutCancel(c.Context())
		_ = c.S.db.Write(ctx, func(w *store.Queries) error { return w.DeleteSession(ctx, c.SessionID) })
	}
	c.S.clearCookie(c.W, c.S.sessionCookieName())
	http.Redirect(c.W, c.R, "/login", http.StatusSeeOther)
	return nil
}
