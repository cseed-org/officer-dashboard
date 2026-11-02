package api

import (
	"context"
	"errors"
	"fmt"
	"time"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/notify"
	"officerdashboard/internal/store"
	"officerdashboard/internal/validate"
)

// inviteTTL is how long an invite link and its Discord ID match stay valid.
const inviteTTL = 7 * 24 * time.Hour

// InviteDTO is an invite as directors see it. The token is never part of it.
type InviteDTO struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	DiscordID   string       `json:"discordId"`
	Subteam     *SubteamDTO  `json:"subteam"`
	InvitedBy   *UserSummary `json:"invitedBy"`
	Method      string       `json:"method"`
	Status      string       `json:"status"` // pending | expired | accepted | onboarded
	DMSentAt    *string      `json:"dmSentAt"`
	DMError     *string      `json:"dmError"`
	CreatedAt   string       `json:"createdAt"`
	ExpiresAt   *string      `json:"expiresAt"`
	AcceptedAt  *string      `json:"acceptedAt"`
	OnboardedAt *string      `json:"onboardedAt"`
	UserID      *string      `json:"userId"`
}

type SubteamDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// DMResult says what happened to the Discord DM for this invite.
type DMResult struct {
	Status  string  `json:"status"` // sent | failed | not_configured | skipped
	SentAt  *string `json:"sentAt"`
	Message *string `json:"message"`
}

func inviteStatus(i *store.Invite, now int64) string {
	switch {
	case i.OnboardedAt != nil:
		return "onboarded"
	case i.AcceptedAt != nil:
		return "accepted"
	case i.ExpiresAt != nil && *i.ExpiresAt <= now:
		return "expired"
	}
	return "pending"
}

type peopleIndex struct {
	users    map[string]*store.User
	subteams map[string]store.Subteam
}

func loadPeople(ctx context.Context, q *store.Queries) (*peopleIndex, error) {
	users, err := q.Users(ctx)
	if err != nil {
		return nil, err
	}
	teams, err := q.Subteams(ctx)
	if err != nil {
		return nil, err
	}
	p := &peopleIndex{users: map[string]*store.User{}, subteams: map[string]store.Subteam{}}
	for i := range users {
		p.users[users[i].ID] = &users[i]
	}
	for _, t := range teams {
		p.subteams[t.ID] = t
	}
	return p, nil
}

func (p *peopleIndex) subteam(id string) *SubteamDTO {
	if t, ok := p.subteams[id]; ok {
		return &SubteamDTO{ID: t.ID, Name: t.Name}
	}
	return nil
}

func (p *peopleIndex) invite(i *store.Invite, now int64) InviteDTO {
	d := InviteDTO{ID: i.ID, Name: i.Name, DiscordID: i.DiscordID, Subteam: p.subteam(i.SubteamID), Method: i.Method,
		Status: inviteStatus(i, now), DMSentAt: isoPtr(i.DMSentAt), DMError: i.DMError, CreatedAt: isoMs(i.CreatedAt),
		ExpiresAt: isoPtr(i.ExpiresAt), AcceptedAt: isoPtr(i.AcceptedAt), OnboardedAt: isoPtr(i.OnboardedAt), UserID: i.UserID}
	if u, ok := p.users[i.InvitedBy]; ok {
		s := userSummary(u)
		d.InvitedBy = &s
	}
	return d
}

func (c *Ctx) invitesList() error {
	if _, err := c.director(); err != nil {
		return err
	}
	ctx := c.Context()
	q := c.S.db.Read()
	people, err := loadPeople(ctx, q)
	if err != nil {
		return err
	}
	rows, err := q.Invites(ctx, 200)
	if err != nil {
		return err
	}
	out := make([]InviteDTO, len(rows))
	for i := range rows {
		out[i] = people.invite(&rows[i], c.S.nowMs())
	}
	writeJSON(c.W, 200, map[string]any{"invites": out, "dmConfigured": c.S.cfg.DM != nil})
	return nil
}

// inviteCreate records an invite and delivers it by Discord DM, copy link, or both.
// A Discord ID that already belongs to an account is refused (reactivate them instead);
// a Discord ID with a pending invite is re-invited: new token, details and expiry.
func (c *Ctx) inviteCreate() error {
	u, err := c.director()
	if err != nil {
		return err
	}
	i := store.Invite{InvitedBy: u.ID}
	if err := c.jsonBody(func(r *validate.Reader) {
		i.Name = r.Str("name", validate.S{Req: true, Trim: true, Min: 1, Max: 100}).V
		i.DiscordID = r.Str("discordId", validate.S{Req: true, Trim: true, Format: "discord"}).V
		i.SubteamID = r.Str("subteamId", validate.S{Req: true, Min: 1, Max: 64}).V
		i.Method = r.Str("method", validate.S{Enum: []string{"dm", "link", "both"}, HasDef: true, Default: "link"}).V
	}); err != nil {
		return err
	}
	token := auth.RandomToken(32)
	i.TokenHash = auth.SHA256Hex(token)
	now := c.S.nowMs()
	exp := now + inviteTTL.Milliseconds()
	i.ExpiresAt = &exp
	ctx := c.Context()
	reinvited := false
	err = c.S.db.Write(ctx, func(q *store.Queries) error {
		// Re-read the caller: a director demoted mid-request must not invite.
		me, err := q.UserByID(ctx, u.ID)
		if err != nil || !me.Active || !auth.HasRole(me.Role, auth.RoleDirector) {
			return forbidden("Requires director role")
		}
		exists, err := q.SubteamExists(ctx, i.SubteamID)
		if err != nil {
			return err
		}
		if !exists {
			issues := &validate.Issues{}
			issues.Add("subteamId", "Subteam not found")
			return validationError(issues)
		}
		if owner, err := q.UserByDiscordID(ctx, i.DiscordID); err == nil {
			if owner.Active {
				return conflict(fmt.Sprintf("That Discord account already belongs to %s. They can sign in now.", owner.Name))
			}
			return conflict(fmt.Sprintf("That Discord account belongs to %s, who is deactivated. Reactivate them on the Officers page instead of inviting.", owner.Name))
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		prior, err := q.PendingInviteByDiscordID(ctx, i.DiscordID)
		switch {
		case err == nil:
			reinvited = true
			i.ID = prior.ID
			if err := q.ReissueInvite(ctx, i, now); err != nil {
				return err
			}
			return c.audit(ctx, q, "invite.reissued", "invite", i.ID, prior, i)
		case errors.Is(err, store.ErrNotFound):
			i.ID = auth.UUID()
			if err := q.InsertInvite(ctx, i, now); err != nil {
				return err
			}
			return c.audit(ctx, q, "invite.created", "invite", i.ID, nil, i)
		default:
			return err
		}
	})
	if err != nil {
		return err
	}
	link := c.S.link("/login?invite="+token, c.R)
	dm := DMResult{Status: "skipped"}
	if i.Method != "link" {
		dm = c.sendInviteDM(&i, u, link)
	}
	ctx = context.WithoutCancel(ctx)
	var out InviteDTO
	err = c.S.db.Write(ctx, func(q *store.Queries) error {
		if dm.Status == "sent" || dm.Status == "failed" || dm.Status == "not_configured" {
			var sentAt *int64
			if dm.Status == "sent" {
				t := c.S.nowMs()
				sentAt = &t
			}
			if err := q.SetInviteDM(ctx, i.ID, sentAt, dm.Message); err != nil {
				return err
			}
			if err := c.audit(ctx, q, "invite.dm", "invite", i.ID, nil, map[string]any{"status": dm.Status, "message": dm.Message}); err != nil {
				return err
			}
		}
		got, err := q.InviteByID(ctx, i.ID)
		if err != nil {
			return err
		}
		people, err := loadPeople(ctx, q)
		if err != nil {
			return err
		}
		out = people.invite(got, c.S.nowMs())
		return nil
	})
	if err != nil {
		return err
	}
	// The link is always returned so a failed or unconfigured DM can fall back to copy-link.
	writeJSON(c.W, 200, map[string]any{"invite": out, "token": token, "link": link, "reinvited": reinvited, "dm": dm})
	return nil
}

func (c *Ctx) sendInviteDM(i *store.Invite, inviter *store.User, link string) DMResult {
	if c.S.cfg.DM == nil {
		msg := "Discord DMs aren't set up (no DISCORD_BOT_TOKEN). Copy the link and send it yourself."
		return DMResult{Status: "not_configured", Message: &msg}
	}
	team := i.SubteamID
	if people, err := loadPeople(c.Context(), c.S.db.Read()); err == nil {
		if t := people.subteam(i.SubteamID); t != nil {
			team = t.Name
		}
	}
	content := fmt.Sprintf("hey %s! %s added you to the CSEED officer team (%s).\n\nSign in to the officer dashboard with this Discord account to set up your profile. It takes about three minutes:\n%s\n\nThis invite expires %s.",
		i.Name, inviter.Name, team, link, time.UnixMilli(*i.ExpiresAt).In(c.S.effectiveZone(c.Context(), "")).Format("Mon, Jan 2"))
	if err := c.S.cfg.DM.SendDM(c.Context(), i.DiscordID, content); err != nil {
		var de *notify.DMError
		msg := "The Discord DM failed. Copy the link instead."
		if errors.As(err, &de) {
			msg = de.Msg
		}
		c.S.log.Warn("invite DM failed", "invite", i.ID, "error", err)
		return DMResult{Status: "failed", Message: &msg}
	}
	at := isoMs(c.S.nowMs())
	return DMResult{Status: "sent", SentAt: &at}
}

func (c *Ctx) inviteRevoke() error {
	if _, err := c.director(); err != nil {
		return err
	}
	ctx := c.Context()
	err := c.S.db.Write(ctx, func(q *store.Queries) error {
		before, err := q.InviteByID(ctx, c.R.PathValue("id"))
		if err != nil {
			return notFoundErr("Invite not found")
		}
		if before.AcceptedAt != nil {
			return conflict("This invite was already accepted. Deactivate the officer instead.")
		}
		if err := q.DeletePendingInvite(ctx, before.ID); err != nil {
			return err
		}
		return c.audit(ctx, q, "invite.revoked", "invite", before.ID, before, nil)
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"ok": true})
	return nil
}

// acceptInvite runs inside the sign-in transaction for a Discord account with no user
// row. It creates the officer from their pending invite. reason is a /login error code.
// tokenHash is the sha256 of the link's token when the sign-in started from an invite
// link: an old link (replaced by a re-invite) no longer works, though signing in
// directly with the invited Discord account still does.
func (s *Server) acceptInvite(ctx context.Context, q *store.Queries, discordID string, avatar *string, tokenHash string) (userID, reason string, err error) {
	inv, err := q.PendingInviteByDiscordID(ctx, discordID)
	if errors.Is(err, store.ErrNotFound) {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	now := s.nowMs()
	if inv.ExpiresAt != nil && *inv.ExpiresAt <= now {
		return "", "invite_unavailable", nil
	}
	if tokenHash != "" && tokenHash != inv.TokenHash {
		return "", "invite_unavailable", nil
	}
	id := auth.UUID()
	u := store.User{ID: id, DiscordID: discordID, Name: inv.Name, AvatarURL: avatar, Role: auth.RoleOfficer, Active: true, CreatedAt: store.Ms(now), UpdatedAt: store.Ms(now)}
	if err := q.InsertUser(ctx, u); err != nil {
		return "", "", err
	}
	if err := q.AcceptInvite(ctx, inv.ID, id, now); err != nil {
		return "", "", err
	}
	entry := func(action, typ, eid string, before, after any) error {
		return q.InsertAudit(ctx, store.AuditEntry{ID: auth.UUID(), Action: action, EntityType: typ, EntityID: &eid, ActorUserID: &id,
			BeforeJSON: snapshot(before), AfterJSON: snapshot(after), CreatedAt: store.Ms(now)})
	}
	if err := entry("user.created", "user", id, nil, map[string]any{"discordId": discordID, "name": inv.Name, "role": auth.RoleOfficer, "via": "invite"}); err != nil {
		return "", "", err
	}
	return id, "", entry("invite.accepted", "invite", inv.ID, nil, map[string]any{"userId": id})
}
