package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// ErrCodeReused is TakeOAuthCode's answer for a code that was already redeemed; the code it
// returns carries the grant that redemption produced (if any), to be revoked.
var ErrCodeReused = errors.New("authorization code already used")

// usedCodeKeepMs is how long past its expiry a code row is kept, so a late replay of a
// redeemed code is still recognised.
const usedCodeKeepMs = 60 * 60 * 1000

// OAuthClient is an oauth_clients row: an MCP client (Claude Code, Cursor, …) that
// registered itself. Clients are public (no secret); PKCE protects the code exchange.
type OAuthClient struct {
	ID           string
	Name         string
	RedirectURIs []string
	CreatedAt    Ms
}

// InsertOAuthClient stores a dynamically registered client.
func (q *Queries) InsertOAuthClient(ctx context.Context, c OAuthClient) error {
	uris, err := json.Marshal(c.RedirectURIs)
	if err != nil {
		return err
	}
	_, err = q.q.ExecContext(ctx, `INSERT INTO oauth_clients (id, name, redirect_uris, created_at) VALUES (?,?,?,?)`, c.ID, c.Name, string(uris), int64(c.CreatedAt))
	return err
}

// OAuthClientByID returns ErrNotFound for an unknown client.
func (q *Queries) OAuthClientByID(ctx context.Context, id string) (*OAuthClient, error) {
	var c OAuthClient
	var uris string
	err := q.q.QueryRowContext(ctx, `SELECT id, name, redirect_uris, created_at FROM oauth_clients WHERE id = ?`, id).Scan(&c.ID, &c.Name, &uris, &c.CreatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	if err := json.Unmarshal([]byte(uris), &c.RedirectURIs); err != nil {
		return nil, err
	}
	return &c, nil
}

// OAuthCode is a pending authorization code (single use, short-lived).
type OAuthCode struct {
	CodeHash      string
	ClientID      string
	UserID        string
	RedirectURI   string
	CodeChallenge string
	ExpiresAt     int64
	GrantID       *string // the grant its redemption produced
}

// InsertOAuthCode stores a new authorization code.
func (q *Queries) InsertOAuthCode(ctx context.Context, c OAuthCode) error {
	_, err := q.q.ExecContext(ctx, `INSERT INTO oauth_codes (code_hash, client_id, user_id, redirect_uri, code_challenge, expires_at) VALUES (?,?,?,?,?,?)`,
		c.CodeHash, c.ClientID, c.UserID, c.RedirectURI, c.CodeChallenge, c.ExpiresAt)
	return err
}

// TakeOAuthCode marks a code redeemed and returns it, so it can be redeemed only once, and
// drops long-expired codes on the way. ErrNotFound when it is unknown; ErrCodeReused (with
// the code) when it was already redeemed.
func (q *Queries) TakeOAuthCode(ctx context.Context, codeHash string, now int64) (*OAuthCode, error) {
	if _, err := q.q.ExecContext(ctx, `DELETE FROM oauth_codes WHERE expires_at <= ?`, now-usedCodeKeepMs); err != nil {
		return nil, err
	}
	var c OAuthCode
	var grant sql.NullString
	err := q.q.QueryRowContext(ctx, `UPDATE oauth_codes SET used_at = ? WHERE code_hash = ? AND used_at IS NULL RETURNING code_hash, client_id, user_id, redirect_uri, code_challenge, expires_at`, now, codeHash).
		Scan(&c.CodeHash, &c.ClientID, &c.UserID, &c.RedirectURI, &c.CodeChallenge, &c.ExpiresAt)
	if err == nil {
		return &c, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	err = q.q.QueryRowContext(ctx, `SELECT code_hash, client_id, user_id, grant_id FROM oauth_codes WHERE code_hash = ?`, codeHash).Scan(&c.CodeHash, &c.ClientID, &c.UserID, &grant)
	if err != nil {
		return nil, notFound(err)
	}
	if grant.Valid {
		c.GrantID = &grant.String
	}
	return &c, ErrCodeReused
}

// SetCodeGrant records the grant a redeemed code produced.
func (q *Queries) SetCodeGrant(ctx context.Context, codeHash, grantID string) error {
	_, err := q.q.ExecContext(ctx, `UPDATE oauth_codes SET grant_id = ? WHERE code_hash = ?`, grantID, codeHash)
	return err
}

// OAuthGrant is one connected agent: an officer's consent to one client, with the current
// access and refresh token hashes (both rotate on refresh).
type OAuthGrant struct {
	ID               string
	ClientID         string
	ClientName       string
	UserID           string
	AccessHash       string
	AccessExpiresAt  int64
	RefreshHash      string
	RefreshExpiresAt int64
	CreatedAt        int64
	LastUsedAt       *int64
	RevokedAt        *int64
	RotatedAt        *int64 // when the refresh token last rotated
}

const grantCols = `g.id, g.client_id, c.name, g.user_id, g.access_hash, g.access_expires_at, g.refresh_hash, g.refresh_expires_at, g.created_at, g.last_used_at, g.revoked_at, g.rotated_at`
const grantFrom = ` FROM oauth_grants g JOIN oauth_clients c ON c.id = g.client_id`

func scanGrant(s interface{ Scan(...any) error }) (*OAuthGrant, error) {
	var g OAuthGrant
	var used, rev, rot sql.NullInt64
	if err := s.Scan(&g.ID, &g.ClientID, &g.ClientName, &g.UserID, &g.AccessHash, &g.AccessExpiresAt, &g.RefreshHash, &g.RefreshExpiresAt, &g.CreatedAt, &used, &rev, &rot); err != nil {
		return nil, err
	}
	if used.Valid {
		g.LastUsedAt = &used.Int64
	}
	if rev.Valid {
		g.RevokedAt = &rev.Int64
	}
	if rot.Valid {
		g.RotatedAt = &rot.Int64
	}
	return &g, nil
}

// InsertOAuthGrant stores a new grant.
func (q *Queries) InsertOAuthGrant(ctx context.Context, g OAuthGrant) error {
	_, err := q.q.ExecContext(ctx, `INSERT INTO oauth_grants (id, client_id, user_id, access_hash, access_expires_at, refresh_hash, refresh_expires_at, created_at) VALUES (?,?,?,?,?,?,?,?)`,
		g.ID, g.ClientID, g.UserID, g.AccessHash, g.AccessExpiresAt, g.RefreshHash, g.RefreshExpiresAt, g.CreatedAt)
	return err
}

// GrantByAccessHash returns ErrNotFound for an unknown access token.
func (q *Queries) GrantByAccessHash(ctx context.Context, hash string) (*OAuthGrant, error) {
	g, err := scanGrant(q.q.QueryRowContext(ctx, `SELECT `+grantCols+grantFrom+` WHERE g.access_hash = ?`, hash))
	return g, notFound(err)
}

// GrantByRefreshHash returns ErrNotFound for an unknown (or already rotated) refresh token.
func (q *Queries) GrantByRefreshHash(ctx context.Context, hash string) (*OAuthGrant, error) {
	g, err := scanGrant(q.q.QueryRowContext(ctx, `SELECT `+grantCols+grantFrom+` WHERE g.refresh_hash = ?`, hash))
	return g, notFound(err)
}

// GrantByPrevRefreshHash finds the grant whose previous (already rotated) refresh token
// this is. ErrNotFound when none.
func (q *Queries) GrantByPrevRefreshHash(ctx context.Context, hash string) (*OAuthGrant, error) {
	g, err := scanGrant(q.q.QueryRowContext(ctx, `SELECT `+grantCols+grantFrom+` WHERE g.prev_refresh_hash = ?`, hash))
	return g, notFound(err)
}

// GrantByID returns ErrNotFound for an unknown grant.
func (q *Queries) GrantByID(ctx context.Context, id string) (*OAuthGrant, error) {
	g, err := scanGrant(q.q.QueryRowContext(ctx, `SELECT `+grantCols+grantFrom+` WHERE g.id = ?`, id))
	return g, notFound(err)
}

// UserGrants lists a user's live grants (not revoked, refresh token not expired), newest first.
func (q *Queries) UserGrants(ctx context.Context, userID string, now int64) ([]OAuthGrant, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT `+grantCols+grantFrom+` WHERE g.user_id = ? AND g.revoked_at IS NULL AND g.refresh_expires_at > ? ORDER BY g.created_at DESC, g.rowid DESC`, userID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OAuthGrant
	for rows.Next() {
		g, err := scanGrant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *g)
	}
	return out, rows.Err()
}

// RotateGrant replaces both tokens of a grant (refresh-token rotation).
func (q *Queries) RotateGrant(ctx context.Context, id, accessHash string, accessExp int64, refreshHash string, refreshExp, now int64) error {
	_, err := q.q.ExecContext(ctx, `UPDATE oauth_grants SET prev_refresh_hash = refresh_hash, access_hash = ?, access_expires_at = ?, refresh_hash = ?, refresh_expires_at = ?, rotated_at = ? WHERE id = ?`,
		accessHash, accessExp, refreshHash, refreshExp, now, id)
	return err
}

// TouchGrant records that a grant's access token was used.
func (q *Queries) TouchGrant(ctx context.Context, id string, now int64) error {
	_, err := q.q.ExecContext(ctx, `UPDATE oauth_grants SET last_used_at = ? WHERE id = ?`, now, id)
	return err
}

// RevokeGrant disconnects one agent (the row stays for the audit trail).
func (q *Queries) RevokeGrant(ctx context.Context, id string, now int64) error {
	_, err := q.q.ExecContext(ctx, `UPDATE oauth_grants SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`, now, id)
	return err
}

// RevokeUserGrants disconnects every agent of a user and reports how many were live.
func (q *Queries) RevokeUserGrants(ctx context.Context, userID string, now int64) (int64, error) {
	res, err := q.q.ExecContext(ctx, `UPDATE oauth_grants SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`, now, userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// RevokeClientGrants revokes a user's live grants for one client (it is connecting again).
func (q *Queries) RevokeClientGrants(ctx context.Context, userID, clientID string, now int64) error {
	_, err := q.q.ExecContext(ctx, `UPDATE oauth_grants SET revoked_at = ? WHERE user_id = ? AND client_id = ? AND revoked_at IS NULL`, now, userID, clientID)
	return err
}

// PurgeOAuth drops expired authorization codes and registered clients that never got a
// grant within a week (abandoned or spam registrations). Grants stay for the audit trail.
func (q *Queries) PurgeOAuth(ctx context.Context, now int64) (int64, error) {
	if _, err := q.q.ExecContext(ctx, `DELETE FROM oauth_codes WHERE expires_at <= ?`, now-usedCodeKeepMs); err != nil {
		return 0, err
	}
	res, err := q.q.ExecContext(ctx, `DELETE FROM oauth_clients WHERE created_at < ? AND NOT EXISTS (SELECT 1 FROM oauth_grants g WHERE g.client_id = oauth_clients.id) AND NOT EXISTS (SELECT 1 FROM oauth_codes c WHERE c.client_id = oauth_clients.id)`, now-7*24*60*60*1000)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// AgentNames maps the given grant ids to their client names, for the audit log.
func (q *Queries) AgentNames(ctx context.Context, ids []string) (map[string]string, error) {
	out := map[string]string{}
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := q.q.QueryContext(ctx, `SELECT g.id, c.name`+grantFrom+` WHERE g.id IN (?`+strings.Repeat(",?", len(ids)-1)+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}
