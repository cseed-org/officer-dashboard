package api

import (
	"context"
	"errors"
	"strings"
	"time"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/store"
	"officerdashboard/internal/validate"
)

var serviceScopes = []string{"ingest:luma", "agent:jobs"}

func (c *Ctx) adminUsers() error {
	if _, err := c.director(); err != nil {
		return err
	}
	users, err := c.S.db.Read().Users(c.Context())
	if err != nil {
		return err
	}
	out := make([]AdminUser, len(users))
	for i := range users {
		out[i] = adminUser(&users[i])
	}
	writeJSON(c.W, 200, map[string]any{"users": out})
	return nil
}

func (c *Ctx) adminUserCreate() error {
	if _, err := c.director(); err != nil {
		return err
	}
	var discordID, name, role, subteamID string
	var title, email validate.Field[string]
	if err := c.jsonBody(func(r *validate.Reader) {
		discordID = r.Str("discordId", validate.S{Req: true, Format: "discord"}).V
		name = r.Str("name", validate.S{Req: true, Trim: true, Min: 1, Max: 100}).V
		subteamID = r.Str("subteamId", validate.S{Req: true, Min: 1, Max: 64}).V
		role = r.Str("role", validate.S{Enum: []string{"leadership", "director", "officer"}, HasDef: true, Default: "officer"}).V
		title = r.Str("title", validate.S{Null: true, Trim: true, Max: 100})
		email = r.Str("email", validate.S{Null: true, Format: "email"})
	}); err != nil {
		return err
	}
	if role != auth.RoleOfficer {
		if _, err := c.requireLeadership(); err != nil {
			return err
		}
	}
	ctx := c.Context()
	id := auth.UUID()
	var out AdminUser
	err := c.S.db.Write(ctx, func(w *store.Queries) error {
		if role != auth.RoleOfficer {
			if _, err := c.requireLeadershipIn(w); err != nil {
				return err
			}
		} else if me, err := w.UserByID(ctx, c.User.ID); err != nil || !me.Active || !auth.HasRole(me.Role, auth.RoleDirector) {
			return forbidden("Requires director role")
		}
		if ok, err := w.SubteamExists(ctx, subteamID); err != nil {
			return err
		} else if !ok {
			issues := &validate.Issues{}
			issues.Add("subteamId", "Subteam not found")
			return validationError(issues)
		}
		if _, err := w.PendingInviteByDiscordID(ctx, discordID); err == nil {
			return conflict("That Discord account has a pending invite. Revoke it on the Officers page first, or let them accept it.")
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if _, err := w.UserByDiscordID(ctx, discordID); err == nil {
			return conflict("That Discord account already has a dashboard account.")
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		now := store.Ms(c.S.nowMs())
		// An accepted invite row puts admin-created accounts through the same setup as
		// invitees (profile, calendar, widgets) before the rest of the API opens up.
		inv := store.Invite{ID: auth.UUID(), Name: name, DiscordID: discordID, SubteamID: subteamID, InvitedBy: c.User.ID, Method: "link", TokenHash: auth.SHA256Hex(auth.RandomToken(32))}
		if err := w.InsertInvite(ctx, inv, int64(now)); err != nil {
			return err
		}
		u := store.User{ID: id, DiscordID: discordID, Name: name, Role: role, Active: true, Title: ptrIf(title), Email: ptrIf(email), CreatedAt: now, UpdatedAt: now}
		if err := w.InsertUser(ctx, u); err != nil {
			return err
		}
		if err := w.AcceptInvite(ctx, inv.ID, id, int64(now)); err != nil {
			return err
		}
		if err := c.audit(ctx, w, "user.created", "user", id, nil, map[string]any{"discordId": discordID, "name": name, "role": role, "title": u.Title, "email": u.Email, "subteamId": subteamID, "inviteId": inv.ID}); err != nil {
			return err
		}
		got, err := w.UserByID(ctx, id)
		if err != nil {
			return err
		}
		out = adminUser(got)
		return nil
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"user": out})
	return nil
}

func (c *Ctx) adminUserUpdate() error {
	if _, err := c.director(); err != nil {
		return err
	}
	var name, role validate.Field[string]
	var active validate.Field[bool]
	var title, email validate.Field[string]
	if err := c.jsonBody(func(r *validate.Reader) {
		name = r.Str("name", validate.S{Trim: true, Min: 1, Max: 100})
		role = r.Str("role", validate.S{Enum: []string{"leadership", "director", "officer"}})
		active = r.BoolField("active")
		title = r.Str("title", validate.S{Null: true, Trim: true, Max: 100})
		email = r.Str("email", validate.S{Null: true, Format: "email"})
	}); err != nil {
		return err
	}
	ctx := c.Context()
	var out AdminUser
	err := c.S.db.Write(ctx, func(w *store.Queries) error {
		before, err := w.UserByID(ctx, c.R.PathValue("id"))
		if err != nil {
			return notFoundErr("Record not found")
		}
		if before.IsOwner && ((role.Set && role.V != before.Role) || (active.Set && !active.V)) {
			if _, err := c.ownerIn(w); err != nil {
				return err
			}
			// Owners may remove another owner's access, but the last-owner
			// trigger refuses the whole transaction if this strands the dashboard.
			if err := w.SetOwner(ctx, before.ID, false, c.S.nowMs()); err != nil {
				return err
			}
		}
		// Directors may deactivate or reactivate officers; only leadership changes the
		// active status of a director or leadership account (or edits leadership at all).
		activeChange := active.Set && active.V != before.Active
		if before.Role == auth.RoleLeadership || (role.Set && role.V != before.Role) || (activeChange && before.Role == auth.RoleDirector) {
			if _, err := c.requireLeadershipIn(w); err != nil {
				return err
			}
		}
		patch := store.UserPatch{Name: optOf(name), Role: optOf(role), Active: optOf(active), Title: optOf(title), Email: optOf(email)}
		// The users_last_director trigger refuses to demote or deactivate the last director.
		if err := w.UpdateUser(ctx, before.ID, patch, c.S.nowMs()); err != nil {
			return err
		}
		if (active.Set && !active.V) || (role.Has() && role.V != before.Role) {
			if err := w.DeleteUserSessions(ctx, before.ID); err != nil {
				return err
			}
		}
		// Service tokens carry their creator's director authority; losing it revokes them.
		if (active.Set && !active.V) || (role.Has() && !auth.HasRole(role.V, auth.RoleDirector)) {
			n, err := w.RevokeUserTokens(ctx, before.ID, c.S.nowMs())
			if err != nil {
				return err
			}
			if n > 0 {
				if err := c.audit(ctx, w, "token.revoked_with_user", "user", before.ID, nil, map[string]any{"count": n}); err != nil {
					return err
				}
			}
		}
		got, err := w.UserByID(ctx, before.ID)
		if err != nil {
			return err
		}
		if err := c.audit(ctx, w, "user.updated", "user", before.ID, userSnapshot(before), userSnapshot(got)); err != nil {
			return err
		}
		out = adminUser(got)
		return nil
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"user": out})
	return nil
}

func tokenView(cat *catalog, t *store.ServiceToken) (ServiceTokenDTO, error) {
	by, err := cat.user(t.CreatedBy)
	if err != nil {
		return ServiceTokenDTO{}, err
	}
	scopes := t.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	return ServiceTokenDTO{ID: t.ID, Name: t.Name, Prefix: t.Prefix, Scopes: scopes, CreatedBy: userSummary(by), CreatedAt: isoMs(int64(t.CreatedAt)),
		LastUsedAt: isoPtr(t.LastUsedAt), ExpiresAt: isoPtr(t.ExpiresAt), RevokedAt: isoPtr(t.RevokedAt)}, nil
}

func (c *Ctx) adminTokens() error {
	if _, err := c.director(); err != nil {
		return err
	}
	ctx := c.Context()
	q := c.S.db.Read()
	cat, err := loadCatalog(ctx, q, false)
	if err != nil {
		return err
	}
	toks, err := q.Tokens(ctx)
	if err != nil {
		return err
	}
	out := make([]ServiceTokenDTO, len(toks))
	for i := range toks {
		if out[i], err = tokenView(cat, &toks[i]); err != nil {
			return err
		}
	}
	writeJSON(c.W, 200, map[string]any{"tokens": out})
	return nil
}

func (c *Ctx) adminTokenCreate() error {
	u, err := c.director()
	if err != nil {
		return err
	}
	var name string
	var scopes []string
	var days validate.Field[int64]
	if err := c.jsonBody(func(r *validate.Reader) {
		name = r.Str("name", validate.S{Req: true, Trim: true, Min: 1, Max: 100}).V
		scopes = r.Strings("scopes", true, 1, 0, serviceScopes, 0)
		days = r.Int("expiresInDays", validate.I{Null: true, Min: validate.Lo(1), Max: validate.Lo(365), HasDef: true, Def: 90})
	}); err != nil {
		return err
	}
	seen := map[string]bool{}
	var uniq []string
	for _, s := range scopes {
		if !seen[s] {
			seen[s] = true
			uniq = append(uniq, s)
		}
	}
	plaintext := "cseed_" + auth.RandomToken(32)
	id := auth.UUID()
	ctx := c.Context()
	var out ServiceTokenDTO
	err = c.S.db.Write(ctx, func(w *store.Queries) error {
		t := store.ServiceToken{ID: id, Name: name, TokenHash: auth.SHA256Hex(plaintext), Prefix: plaintext[:12], Scopes: uniq, CreatedBy: u.ID, CreatedAt: store.Ms(c.S.nowMs())}
		if !days.Null {
			exp := time.UnixMilli(c.S.nowMs()).Add(time.Duration(days.V) * 24 * time.Hour).UnixMilli()
			t.ExpiresAt = &exp
		}
		if err := w.InsertToken(ctx, t); err != nil {
			return err
		}
		if err := c.audit(ctx, w, "token.created", "service_token", id, nil, map[string]any{"name": name, "scopes": uniq, "prefix": t.Prefix}); err != nil {
			return err
		}
		cat, err := loadCatalog(ctx, w, false)
		if err != nil {
			return err
		}
		got, err := w.TokenByID(ctx, id)
		if err != nil {
			return err
		}
		out, err = tokenView(cat, got)
		return err
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"token": out, "plaintext": plaintext})
	return nil
}

func (c *Ctx) adminTokenRevoke() error {
	if _, err := c.director(); err != nil {
		return err
	}
	ctx := c.Context()
	err := c.S.db.Write(ctx, func(w *store.Queries) error {
		t, err := w.TokenByID(ctx, c.R.PathValue("id"))
		if err != nil {
			return notFoundErr("Record not found")
		}
		if t.RevokedAt != nil {
			return conflict("Token already revoked")
		}
		if err := w.RevokeToken(ctx, t.ID, c.S.nowMs()); err != nil {
			return err
		}
		return c.audit(ctx, w, "token.revoked", "service_token", t.ID, map[string]any{"name": t.Name}, map[string]any{"revoked": true})
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"ok": true})
	return nil
}

func (c *Ctx) adminAudit() error {
	if _, err := c.director(); err != nil {
		return err
	}
	var cursor string
	var limit int
	var entityType, entityID validate.Field[string]
	if err := c.query(func(r *validate.Reader) {
		cursor, limit = pageQuery(r)
		entityType = r.Str("entityType", validate.S{})
		entityID = r.Str("entityId", validate.S{Min: 1, Max: 64})
	}); err != nil {
		return err
	}
	ctx := c.Context()
	q := c.S.db.Read()
	f := store.AuditFilter{EntityType: entityType.V, EntityID: entityID.V, Limit: limit + 1}
	if cursor != "" {
		ms, id, err := parseCursor(cursor)
		if err != nil {
			return err
		}
		f.HasCursor, f.CursorCreatedAt, f.CursorID = true, ms, id
	}
	rows, err := q.Audit(ctx, f)
	if err != nil {
		return err
	}
	cat, err := loadCatalog(ctx, q, false)
	if err != nil {
		return err
	}
	toks, err := q.Tokens(ctx)
	if err != nil {
		return err
	}
	tokenNames := map[string]string{}
	for _, t := range toks {
		tokenNames[t.ID] = t.Name
	}
	type entry struct {
		ID             string       `json:"id"`
		Actor          *UserSummary `json:"actor"`
		ActorTokenName *string      `json:"actorTokenName"`
		Action         string       `json:"action"`
		EntityType     string       `json:"entityType"`
		EntityID       *string      `json:"entityId"`
		Before         any          `json:"before"`
		After          any          `json:"after"`
		CreatedAt      string       `json:"createdAt"`
	}
	out := page[entry]{Items: []entry{}}
	for i, a := range rows {
		if i == limit {
			next := cursorKey(int64(rows[limit-1].CreatedAt), rows[limit-1].ID)
			out.NextCursor = &next
			break
		}
		e := entry{ID: a.ID, Action: a.Action, EntityType: a.EntityType, EntityID: a.EntityID, CreatedAt: isoMs(int64(a.CreatedAt)), Before: rawJSON(a.BeforeJSON), After: rawJSON(a.AfterJSON)}
		if a.ActorUserID != nil {
			if u := cat.users[*a.ActorUserID]; u != nil {
				s := userSummary(u)
				e.Actor = &s
			}
		}
		if a.ActorToken != nil {
			if n, ok := tokenNames[*a.ActorToken]; ok {
				e.ActorTokenName = &n
			}
		}
		out.Items = append(out.Items, e)
	}
	writeJSON(c.W, 200, out)
	return nil
}

// rawJSON embeds a stored JSON text as a value (null when absent).
func rawJSON(s *string) any {
	if s == nil {
		return nil
	}
	return jsonRaw(*s)
}

type jsonRaw string

func (j jsonRaw) MarshalJSON() ([]byte, error) {
	if strings.TrimSpace(string(j)) == "" {
		return []byte("null"), nil
	}
	return []byte(j), nil
}

var _ = context.Background
var _ = errors.New
