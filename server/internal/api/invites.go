package api

import (
	"officerdashboard/internal/auth"
	"officerdashboard/internal/store"
	"officerdashboard/internal/validate"
)

// inviteCreate provides the invite record and copy-link token. Discord delivery
// and acceptance/onboarding are implemented by the officers feature Pane.
func (c *Ctx) inviteCreate() error {
	u, err := c.director()
	if err != nil {
		return err
	}
	i := store.Invite{ID: auth.UUID(), InvitedBy: u.ID}
	if err := c.jsonBody(func(r *validate.Reader) {
		i.Name = r.Str("name", validate.S{Req: true, Trim: true, Min: 1, Max: 100}).V
		i.DiscordID = r.Str("discordId", validate.S{Req: true, Format: "discord"}).V
		i.SubteamID = r.Str("subteamId", validate.S{Req: true, Min: 1, Max: 64}).V
		i.Method = r.Str("method", validate.S{Enum: []string{"link"}, HasDef: true, Default: "link"}).V
	}); err != nil {
		return err
	}
	token := auth.RandomToken(32)
	i.TokenHash = auth.SHA256Hex(token)
	ctx := c.Context()
	err = c.S.db.Write(ctx, func(q *store.Queries) error {
		exists, err := q.SubteamExists(ctx, i.SubteamID)
		if err != nil {
			return err
		}
		if !exists {
			issues := &validate.Issues{}
			issues.Add("subteamId", "Subteam not found")
			return validationError(issues)
		}
		if err := q.InsertInvite(ctx, i, c.S.nowMs()); err != nil {
			return err
		}
		return c.audit(ctx, q, "invite.created", "invite", i.ID, nil, i)
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"invite": i, "token": token})
	return nil
}
