package store

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
)

// Ms is a millisecond timestamp column. Columns defaulted by SQLite itself hold REAL
// (unixepoch('subsec') * 1000), so Scan accepts both.
type Ms int64

func (m *Ms) Scan(v any) error {
	switch t := v.(type) {
	case int64:
		*m = Ms(t)
	case float64:
		*m = Ms(math.Round(t))
	case nil:
		*m = 0
	default:
		return fmt.Errorf("cannot scan %T into Ms", v)
	}
	return nil
}

// Opt is a patch value: Set means the field was supplied, Null that it was explicitly null.
type Opt[T any] struct {
	V    T
	Set  bool
	Null bool
}

// val is the SQL value for a Set option.
func (o Opt[T]) val() any {
	if o.Null {
		return nil
	}
	return o.V
}

// User is a users row.
type User struct {
	ID          string
	DiscordID   string
	Name        string
	Email       *string
	AvatarURL   *string
	Role        string
	Active      bool
	Title       *string
	LastLoginAt *int64
	CreatedAt   Ms
	UpdatedAt   Ms
}

const userCols = `id, discord_id, name, email, avatar_url, role, active, title, last_login_at, created_at, updated_at`

func scanUser(s interface{ Scan(...any) error }) (*User, error) {
	var u User
	var last *Ms
	if err := s.Scan(&u.ID, &u.DiscordID, &u.Name, &u.Email, &u.AvatarURL, &u.Role, &u.Active, &u.Title, &last, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return nil, err
	}
	if last != nil {
		v := int64(*last)
		u.LastLoginAt = &v
	}
	return &u, nil
}

// UserByID returns ErrNotFound for an unknown id.
func (q *Queries) UserByID(ctx context.Context, id string) (*User, error) {
	u, err := scanUser(q.q.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id))
	return u, notFound(err)
}

// UserByDiscordID returns ErrNotFound for an unknown Discord account.
func (q *Queries) UserByDiscordID(ctx context.Context, discordID string) (*User, error) {
	u, err := scanUser(q.q.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE discord_id = ?`, discordID))
	return u, notFound(err)
}

// Users lists everyone in insertion order.
func (q *Queries) Users(ctx context.Context) ([]User, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT `+userCols+` FROM users ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

// FirstActiveUserWithRole is the dev-login pick: oldest active user with the role.
func (q *Queries) FirstActiveUserWithRole(ctx context.Context, role string) (*User, error) {
	u, err := scanUser(q.q.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE role = ? AND active = 1 ORDER BY created_at, id LIMIT 1`, role))
	return u, notFound(err)
}

// InsertUser adds a user.
func (q *Queries) InsertUser(ctx context.Context, u User) error {
	_, err := q.q.ExecContext(ctx, `INSERT INTO users (id, discord_id, name, email, avatar_url, role, active, title, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		u.ID, u.DiscordID, u.Name, u.Email, u.AvatarURL, u.Role, u.Active, u.Title, int64(u.CreatedAt), int64(u.UpdatedAt))
	return err
}

// UserPatch lists the user fields a director may change.
type UserPatch struct {
	Name   Opt[string]
	Role   Opt[string]
	Active Opt[bool]
	Title  Opt[string]
	Email  Opt[string]
}

// UpdateUser applies a patch and stamps updated_at.
func (q *Queries) UpdateUser(ctx context.Context, id string, p UserPatch, now int64) error {
	sets, args := []string{}, []any{}
	add := func(col string, set bool, v any) {
		if set {
			sets = append(sets, col+" = ?")
			args = append(args, v)
		}
	}
	add("name", p.Name.Set, p.Name.val())
	add("role", p.Role.Set, p.Role.val())
	add("active", p.Active.Set, p.Active.val())
	add("title", p.Title.Set, p.Title.val())
	add("email", p.Email.Set, p.Email.val())
	sets = append(sets, "updated_at = ?")
	args = append(args, now, id)
	_, err := q.q.ExecContext(ctx, `UPDATE users SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	return err
}

// SetAvatar refreshes the avatar from Discord on login.
func (q *Queries) SetAvatar(ctx context.Context, id string, avatar *string) error {
	_, err := q.q.ExecContext(ctx, `UPDATE users SET avatar_url = ? WHERE id = ?`, avatar, id)
	return err
}

// ─── sessions ───

// Session is a sessions row joined to its active user.
type Session struct {
	ID        string
	ExpiresAt int64
	User      User
}

// InsertSession stores a session under the sha256 of its cookie token and stamps last login.
func (q *Queries) InsertSession(ctx context.Context, idHash, userID string, expiresAt int64, userAgent string, now int64) error {
	var ua any
	if userAgent != "" {
		ua = userAgent
	}
	if _, err := q.q.ExecContext(ctx, `INSERT INTO sessions (id, user_id, expires_at, user_agent, created_at) VALUES (?,?,?,?,?)`, idHash, userID, expiresAt, ua, now); err != nil {
		return err
	}
	_, err := q.q.ExecContext(ctx, `UPDATE users SET last_login_at = ? WHERE id = ?`, now, userID)
	return err
}

// SessionByHash returns the live session and its active user, or ErrNotFound.
func (q *Queries) SessionByHash(ctx context.Context, idHash string, now int64) (*Session, error) {
	row := q.q.QueryRowContext(ctx, `SELECT s.id, s.expires_at, u.id, u.discord_id, u.name, u.email, u.avatar_url, u.role, u.active, u.title, u.last_login_at, u.created_at, u.updated_at
		FROM sessions s JOIN users u ON u.id = s.user_id WHERE s.id = ? AND s.expires_at > ? AND u.active = 1`, idHash, now)
	var s Session
	var last *Ms
	var exp Ms
	err := row.Scan(&s.ID, &exp, &s.User.ID, &s.User.DiscordID, &s.User.Name, &s.User.Email, &s.User.AvatarURL, &s.User.Role, &s.User.Active, &s.User.Title, &last, &s.User.CreatedAt, &s.User.UpdatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	s.ExpiresAt = int64(exp)
	return &s, nil
}

// ExtendSession slides the expiry.
func (q *Queries) ExtendSession(ctx context.Context, idHash string, expiresAt int64) error {
	_, err := q.q.ExecContext(ctx, `UPDATE sessions SET expires_at = ? WHERE id = ?`, expiresAt, idHash)
	return err
}

// DeleteSession signs one session out.
func (q *Queries) DeleteSession(ctx context.Context, idHash string) error {
	_, err := q.q.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, idHash)
	return err
}

// DeleteUserSessions ends every session of a user.
func (q *Queries) DeleteUserSessions(ctx context.Context, userID string) error {
	_, err := q.q.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}

// PurgeExpiredSessions removes dead sessions (housekeeping).
func (q *Queries) PurgeExpiredSessions(ctx context.Context, now int64) (int64, error) {
	res, err := q.q.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, now)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

var _ = sql.ErrNoRows
