package store

import (
	"context"
	"database/sql"
)

type Invite struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	DiscordID   string  `json:"discordId"`
	SubteamID   string  `json:"subteamId"`
	InvitedBy   string  `json:"invitedBy"`
	Method      string  `json:"method"`
	TokenHash   string  `json:"-"`
	DMSentAt    *int64  `json:"dmSentAt,omitempty"`
	DMError     *string `json:"dmError,omitempty"`
	ExpiresAt   *int64  `json:"expiresAt,omitempty"`
	AcceptedAt  *int64  `json:"acceptedAt,omitempty"`
	OnboardedAt *int64  `json:"onboardedAt,omitempty"`
	UserID      *string `json:"userId,omitempty"`
	// AccessEmail moves to the account when the invite is accepted.
	AccessEmail *string `json:"accessEmail"`
	Step        int     `json:"step"`
	CreatedAt   int64   `json:"createdAt"`
}

const inviteCols = `id,name,discord_id,subteam_id,invited_by,method,token_hash,dm_sent_at,dm_error,expires_at,accepted_at,onboarded_at,user_id,onboarding_step,access_email,created_at`

func scanInvite(s interface{ Scan(...any) error }) (*Invite, error) {
	var i Invite
	var dm, exp, acc, onb *Ms
	var created Ms
	if err := s.Scan(&i.ID, &i.Name, &i.DiscordID, &i.SubteamID, &i.InvitedBy, &i.Method, &i.TokenHash, &dm, &i.DMError, &exp, &acc, &onb, &i.UserID, &i.Step, &i.AccessEmail, &created); err != nil {
		return nil, err
	}
	i.DMSentAt, i.ExpiresAt, i.AcceptedAt, i.OnboardedAt = msPtr(dm), msPtr(exp), msPtr(acc), msPtr(onb)
	i.CreatedAt = int64(created)
	return &i, nil
}

func msPtr(m *Ms) *int64 {
	if m == nil {
		return nil
	}
	v := int64(*m)
	return &v
}

func (q *Queries) InsertInvite(ctx context.Context, i Invite, now int64) error {
	return q.Exec(ctx, `INSERT INTO invites(id,name,discord_id,subteam_id,invited_by,method,token_hash,expires_at,access_email,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, i.ID, i.Name, i.DiscordID, i.SubteamID, i.InvitedBy, i.Method, i.TokenHash, i.ExpiresAt, i.AccessEmail, now)
}

// ReissueInvite replaces a pending invite's details and token (a re-invite). The old
// link stops working and any earlier DM status is cleared.
func (q *Queries) ReissueInvite(ctx context.Context, i Invite, now int64) error {
	return q.Exec(ctx, `UPDATE invites SET name=?,subteam_id=?,invited_by=?,method=?,token_hash=?,expires_at=?,access_email=?,dm_sent_at=NULL,dm_error=NULL,created_at=? WHERE id=? AND accepted_at IS NULL`,
		i.Name, i.SubteamID, i.InvitedBy, i.Method, i.TokenHash, i.ExpiresAt, i.AccessEmail, now, i.ID)
}

// PendingInviteByDiscordID returns the not-yet-accepted invite for a Discord account.
func (q *Queries) PendingInviteByDiscordID(ctx context.Context, discordID string) (*Invite, error) {
	i, err := scanInvite(q.q.QueryRowContext(ctx, `SELECT `+inviteCols+` FROM invites WHERE discord_id=? AND accepted_at IS NULL`, discordID))
	return i, notFound(err)
}

func (q *Queries) InviteByID(ctx context.Context, id string) (*Invite, error) {
	i, err := scanInvite(q.q.QueryRowContext(ctx, `SELECT `+inviteCols+` FROM invites WHERE id=?`, id))
	return i, notFound(err)
}

// InviteForUser is the most recent accepted invite that created this account.
func (q *Queries) InviteForUser(ctx context.Context, userID string) (*Invite, error) {
	i, err := scanInvite(q.q.QueryRowContext(ctx, `SELECT `+inviteCols+` FROM invites WHERE user_id=? ORDER BY accepted_at DESC LIMIT 1`, userID))
	return i, notFound(err)
}

// Invites lists the newest invites first.
func (q *Queries) Invites(ctx context.Context, limit int) ([]Invite, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT `+inviteCols+` FROM invites ORDER BY created_at DESC, id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invite{}
	for rows.Next() {
		i, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *i)
	}
	return out, rows.Err()
}

func (q *Queries) SetInviteDM(ctx context.Context, id string, sentAt *int64, dmErr *string) error {
	return q.Exec(ctx, `UPDATE invites SET dm_sent_at=?,dm_error=? WHERE id=?`, sentAt, dmErr, id)
}

func (q *Queries) AcceptInvite(ctx context.Context, id, userID string, now int64) error {
	res, err := q.q.ExecContext(ctx, `UPDATE invites SET accepted_at=?,user_id=? WHERE id=? AND accepted_at IS NULL`, now, userID, id)
	return oneRow(res, err)
}

func (q *Queries) MarkOnboarded(ctx context.Context, id string, now int64) error {
	res, err := q.q.ExecContext(ctx, `UPDATE invites SET onboarded_at=? WHERE id=? AND accepted_at IS NOT NULL AND onboarded_at IS NULL`, now, id)
	return oneRow(res, err)
}

func (q *Queries) DeletePendingInvite(ctx context.Context, id string) error {
	res, err := q.q.ExecContext(ctx, `DELETE FROM invites WHERE id=? AND accepted_at IS NULL`, id)
	return oneRow(res, err)
}

func oneRow(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (q *Queries) SubteamExists(ctx context.Context, id string) (bool, error) {
	var n int
	err := q.q.QueryRowContext(ctx, `SELECT count(*) FROM subteams WHERE id=?`, id).Scan(&n)
	return n > 0, err
}

// UpdateInviteDetails changes the name and subteam of an invite that hasn't finished
// onboarding (pending or accepted). The token and expiry stay as they are.
func (q *Queries) UpdateInviteDetails(ctx context.Context, id, name, subteamID string) error {
	res, err := q.q.ExecContext(ctx, `UPDATE invites SET name=?,subteam_id=? WHERE id=? AND onboarded_at IS NULL`, name, subteamID, id)
	return oneRow(res, err)
}

// SetInviteAccessEmail changes the access email of an invite nobody has accepted yet.
func (q *Queries) SetInviteAccessEmail(ctx context.Context, id string, email *string) error {
	res, err := q.q.ExecContext(ctx, `UPDATE invites SET access_email=? WHERE id=? AND accepted_at IS NULL`, email, id)
	return oneRow(res, err)
}

// AccessEmailHolder names who already uses an access email, other than the Discord account
// discordID: an account (kind "user") or a pending invite (kind "invite"). ErrNotFound when
// nobody does.
func (q *Queries) AccessEmailHolder(ctx context.Context, email, discordID string) (kind, name string, err error) {
	err = q.q.QueryRowContext(ctx, `SELECT 'user', name FROM users WHERE access_email=? AND discord_id<>?
 UNION ALL SELECT 'invite', name FROM invites WHERE accepted_at IS NULL AND access_email=? AND discord_id<>? LIMIT 1`, email, discordID, email, discordID).Scan(&kind, &name)
	return kind, name, notFound(err)
}
