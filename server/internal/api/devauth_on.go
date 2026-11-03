//go:build dev

package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/store"
)

// DevBuild is true in binaries built with `-tags dev`.
const DevBuild = true

// Refusals (dev login off, missing or wrong key) are answered by the front-end handler, so
// they look exactly like the release build, where these routes do not exist.

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

// devKeyOK reports whether key unlocks dev login: always when DEV_AUTH_KEY is unset,
// otherwise only the exact key (compared in constant time). Nothing is remembered between
// requests: every sign-in carries the key.
func (s *Server) devKeyOK(key string) bool {
	want := s.cfg.DevAuthKey
	if want == "" {
		return true
	}
	return key != "" && subtle.ConstantTimeCompare([]byte(devKeyDigest(key)), []byte(devKeyDigest(want))) == 1
}

// devSameOrigin refuses a dev-login POST that another site submitted (login CSRF): a
// cross-site Sec-Fetch-Site, or an Origin naming another host. Unlike originOK it applies
// to anonymous requests, since signing in is the point.
func (c *Ctx) devSameOrigin() bool {
	if c.R.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	origin := c.R.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, c.R.Host) || (c.S.host != "" && strings.EqualFold(u.Host, c.S.host))
}

var devConfirmPage = template.Must(template.New("dev").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="robots" content="noindex"><title>Developer sign-in</title>
<style>body{font:16px/1.5 system-ui,sans-serif;max-width:28rem;margin:4rem auto;padding:0 1rem}button{font:inherit;padding:.5rem 1rem}</style>
</head><body><main>
<h1>Developer sign-in</h1>
<p>Test build: continue as <strong>{{.Who}}</strong>{{if .Next}}, then open <code>{{.Next}}</code>{{end}}.</p>
<form method="post" action="/auth/dev">
{{range .Fields}}<input type="hidden" name="{{.Name}}" value="{{.Value}}">
{{end}}<button type="submit">Continue</button>
</form>
</main></body></html>
`))

type devField struct{ Name, Value string }

// registerDev adds the developer sign-in of test builds. It needs DEV_AUTH=1 as well, and,
// with DEV_AUTH_KEY set, the key on every request.
//
//   - POST /auth/dev (form: as=leadership|director|officer [user=<id>] | discord=<id>,
//     next, key) signs in. as= picks the oldest active user with that role (creating a
//     placeholder on an empty database) or the given user; discord= signs in as that
//     Discord account the way /auth/callback would, never as leadership or an owner.
//   - GET /auth/dev?… only shows a confirmation page whose button posts that form, so a
//     shared link still works but a link or image elsewhere cannot sign anyone in.
//   - GET /auth/dev/info tells the static login page whether to show the developer panel.
//
// Without the key every variant answers exactly like the release build (no route).
func (s *Server) registerDev(_ *http.ServeMux, h func(string, handlerFunc)) {
	h("GET /auth/dev/info", func(c *Ctx) error {
		if !c.S.cfg.DevAuth {
			c.S.staticHandler().ServeHTTP(c.W, c.R)
			return nil
		}
		c.W.Header().Set("Cache-Control", "no-store")
		writeJSON(c.W, http.StatusOK, devInfo{Enabled: true, KeyRequired: c.S.cfg.DevAuthKey != "", Authorized: c.S.devKeyOK(c.R.URL.Query().Get("key"))})
		return nil
	})
	h("GET /auth/dev", func(c *Ctx) error {
		q := c.R.URL.Query()
		if !c.S.cfg.DevAuth || !c.S.devKeyOK(q.Get("key")) {
			c.S.staticHandler().ServeHTTP(c.W, c.R)
			return nil
		}
		var fields []devField
		for _, k := range []string{"as", "user", "discord", "next", "key"} {
			if v := q.Get(k); v != "" {
				fields = append(fields, devField{k, v})
			}
		}
		who := q.Get("as")
		if d := q.Get("discord"); d != "" {
			who = "Discord account " + d
		}
		if who == "" {
			who = "?"
		}
		next := ""
		if n := auth.SafeNext(q.Get("next")); n != "/" {
			next = n
		}
		hdr := c.W.Header()
		hdr.Set("Content-Type", "text/html; charset=utf-8")
		hdr.Set("Cache-Control", "no-store")
		hdr.Set("Referrer-Policy", "no-referrer")
		return devConfirmPage.Execute(c.W, map[string]any{"Who": who, "Next": next, "Fields": fields})
	})
	h("POST /auth/dev", func(c *Ctx) error {
		c.R.Body = http.MaxBytesReader(c.W, c.R.Body, 8<<10)
		if err := c.R.ParseForm(); err != nil {
			return badRequest("Invalid form")
		}
		f := c.R.PostForm
		if !c.S.cfg.DevAuth || !c.S.devKeyOK(f.Get("key")) {
			c.S.staticHandler().ServeHTTP(c.W, c.R)
			return nil
		}
		if !c.devSameOrigin() {
			return forbidden("Cross-origin request blocked")
		}
		next := auth.SafeNext(f.Get("next"))
		ctx := c.Context()
		if id := f.Get("discord"); id != "" {
			var userID, reason string
			err := c.S.db.Write(ctx, func(w *store.Queries) error {
				if u, err := w.UserByDiscordID(ctx, id); err == nil {
					// Leadership and owners keep needing real Discord sign-in.
					if u.IsOwner || auth.HasRole(u.Role, auth.RoleLeadership) {
						return forbidden("Developer sign-in by Discord ID cannot target leadership")
					}
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
			if err := c.newSession(ctx, userID); err != nil {
				return err
			}
			if inv, err := c.S.db.Read().InviteForUser(ctx, userID); err == nil && inv.OnboardedAt == nil {
				next = "/onboarding"
			}
			http.Redirect(c.W, c.R, next, http.StatusSeeOther)
			return nil
		}
		as := f.Get("as")
		if as != auth.RoleDirector && as != auth.RoleOfficer && as != auth.RoleLeadership {
			http.Error(c.W, "as must be leadership, director or officer", http.StatusBadRequest)
			return nil
		}
		var userID string
		err := c.S.db.Write(ctx, func(w *store.Queries) error {
			if id := f.Get("user"); id != "" {
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
		if err := c.newSession(ctx, userID); err != nil {
			return err
		}
		http.Redirect(c.W, c.R, next, http.StatusSeeOther)
		return nil
	})
}
