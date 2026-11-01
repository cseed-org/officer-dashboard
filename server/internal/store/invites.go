package store

import "context"

type Invite struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	DiscordID string `json:"discordId"`
	SubteamID string `json:"subteamId"`
	InvitedBy string `json:"invitedBy"`
	Method    string `json:"method"`
	TokenHash string `json:"-"`
}

func (q *Queries) InsertInvite(ctx context.Context, i Invite, now int64) error {
	return q.Exec(ctx, `INSERT INTO invites(id,name,discord_id,subteam_id,invited_by,method,token_hash,created_at) VALUES(?,?,?,?,?,?,?,?)`, i.ID, i.Name, i.DiscordID, i.SubteamID, i.InvitedBy, i.Method, i.TokenHash, now)
}
