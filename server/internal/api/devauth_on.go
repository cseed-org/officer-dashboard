//go:build dev

package api

import (
	"net/http"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/store"
)

// DevBuild is true in binaries built with `-tags dev`.
const DevBuild = true

// registerDev adds /auth/dev?as=director|officer, which signs in as the oldest active user
// with that role (creating a placeholder on an empty database). It needs DEV_AUTH=1 as well.
func (s *Server) registerDev(_ *http.ServeMux, h func(string, handlerFunc)) {
	h("GET /auth/dev", func(c *Ctx) error {
		if !c.S.cfg.DevAuth {
			http.Error(c.W, "Dev login is off. Set DEV_AUTH=1.", http.StatusNotFound)
			return nil
		}
		as := c.R.URL.Query().Get("as")
		if as != auth.RoleDirector && as != auth.RoleOfficer {
			http.Error(c.W, "as must be director or officer", http.StatusBadRequest)
			return nil
		}
		ctx := c.Context()
		var userID string
		err := c.S.db.Write(ctx, func(w *store.Queries) error {
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
