//go:build dev

package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"time"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/store"
)

// DevBuild is true in binaries built with `-tags dev`.
const DevBuild = true

// Refusals (dev login off, missing or wrong key) are answered by the front-end handler, so
// they look exactly like the release build, where these routes do not exist.

// devKeyCookie remembers a valid DEV_AUTH_KEY in the browser (as sha256(key), never the key
// itself) so a person who arrived by a keyed link can switch roles from /login later.
const devKeyCookie = "dev_key"

const devKeyCookieAge = 30 * 24 * time.Hour

// devInfo is the DevInfoResponse of src/core/contracts.ts.
type devInfo struct {
	Enabled     bool `json:"enabled"`
	KeyRequired bool `json:"keyRequired"`
	Authorized  bool `json:"authorized"`
}

func devKeyDigest(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// devKeyStatus reports whether this request may use dev login: always when DEV_AUTH_KEY is
// unset, otherwise only with ?key=<DEV_AUTH_KEY> or the dev_key cookie a keyed visit set.
// fromQuery is true when the query carried the valid key (so the cookie should be (re)set).
func (c *Ctx) devKeyStatus() (authorized, fromQuery bool) {
	want := c.S.cfg.DevAuthKey
	if want == "" {
		return true, false
	}
	wantDigest := devKeyDigest(want)
	if k := c.R.URL.Query().Get("key"); k != "" && subtle.ConstantTimeCompare([]byte(devKeyDigest(k)), []byte(wantDigest)) == 1 {
		return true, true
	}
	if ck := c.cookie(c.S.hostPrefixed(devKeyCookie)); ck != "" && subtle.ConstantTimeCompare([]byte(ck), []byte(wantDigest)) == 1 {
		return true, false
	}
	return false, false
}

func (c *Ctx) rememberDevKey() {
	c.S.setCookie(c.W, c.S.hostPrefixed(devKeyCookie), devKeyDigest(c.S.cfg.DevAuthKey), time.Now().Add(devKeyCookieAge), int(devKeyCookieAge/time.Second))
}

// registerDev adds /auth/dev?as=leadership|director|officer, which signs in as the oldest active user
// with that role (creating a placeholder on an empty database). It needs DEV_AUTH=1 as well.
// &user=<id> picks a specific active user with that role instead (e.g. the seeded designer).
// With DEV_AUTH_KEY set it also needs &key=<DEV_AUTH_KEY> (or the dev_key cookie); without
// it the route answers 404 exactly as if dev login were off.
//
// GET /auth/dev/info tells the static login page whether to show the developer-mode panel.
func (s *Server) registerDev(_ *http.ServeMux, h func(string, handlerFunc)) {
	h("GET /auth/dev/info", func(c *Ctx) error {
		if !c.S.cfg.DevAuth {
			c.S.staticHandler().ServeHTTP(c.W, c.R)
			return nil
		}
		authorized, fromQuery := c.devKeyStatus()
		if fromQuery {
			c.rememberDevKey()
		}
		c.W.Header().Set("Cache-Control", "no-store")
		writeJSON(c.W, http.StatusOK, devInfo{Enabled: true, KeyRequired: c.S.cfg.DevAuthKey != "", Authorized: authorized})
		return nil
	})
	h("GET /auth/dev", func(c *Ctx) error {
		if !c.S.cfg.DevAuth {
			c.S.staticHandler().ServeHTTP(c.W, c.R)
			return nil
		}
		authorized, fromQuery := c.devKeyStatus()
		if !authorized {
			c.S.staticHandler().ServeHTTP(c.W, c.R)
			return nil
		}
		if fromQuery {
			c.rememberDevKey()
		}
		// ?discord=<id> signs in as that Discord account the way /auth/callback would:
		// an existing user, or a pending invite accepted into a new officer (e2e onboarding).
		if id := c.R.URL.Query().Get("discord"); id != "" {
			ctx := c.Context()
			var userID, reason string
			err := c.S.db.Write(ctx, func(w *store.Queries) error {
				if u, err := w.UserByDiscordID(ctx, id); err == nil {
					if !u.Active {
						reason = "inactive"
					}
					userID = u.ID
					return nil
				}
				var err error
				userID, reason, err = c.S.acceptInvite(ctx, w, id, nil, "")
				if err == nil && userID == "" && reason == "" {
					reason = "not_allowlisted"
				}
				return err
			})
			if err != nil {
				return err
			}
			if reason != "" {
				return c.loginRedirect(reason)
			}
			if err := c.S.createSession(ctx, c.W, userID, c.R.UserAgent()); err != nil {
				return err
			}
			next := auth.SafeNext(c.R.URL.Query().Get("next"))
			if inv, err := c.S.db.Read().InviteForUser(ctx, userID); err == nil && inv.OnboardedAt == nil {
				next = "/onboarding"
			}
			http.Redirect(c.W, c.R, next, http.StatusFound)
			return nil
		}
		as := c.R.URL.Query().Get("as")
		if as != auth.RoleDirector && as != auth.RoleOfficer && as != auth.RoleLeadership {
			http.Error(c.W, "as must be leadership, director or officer", http.StatusBadRequest)
			return nil
		}
		ctx := c.Context()
		var userID string
		err := c.S.db.Write(ctx, func(w *store.Queries) error {
			if id := c.R.URL.Query().Get("user"); id != "" {
				u, err := w.UserByID(ctx, id)
				if err != nil || !u.Active || u.Role != as {
					return badRequest("user must be an active " + as)
				}
				userID = u.ID
				return nil
			}
			u, err := w.FirstActiveUserWithRole(ctx, as)
			if err == nil {
				userID = u.ID
				return nil
			}
			userID = "dev-" + as
			discord := "000000000000000002"
			if as == auth.RoleDirector {
				discord = "000000000000000001"
			}
			if as == auth.RoleLeadership {
				discord = "000000000000000003"
			}
			now := store.Ms(c.S.nowMs())
			return w.InsertUser(ctx, store.User{ID: userID, DiscordID: discord, Name: "Dev " + as, Role: as, Active: true, CreatedAt: now, UpdatedAt: now})
		})
		if err != nil {
			return err
		}
		c.S.log.Warn("[dev-auth] signing in", "as", as, "user", userID)
		if err := c.S.createSession(ctx, c.W, userID, c.R.UserAgent()); err != nil {
			return err
		}
		http.Redirect(c.W, c.R, auth.SafeNext(c.R.URL.Query().Get("next")), http.StatusFound)
		return nil
	})
}
