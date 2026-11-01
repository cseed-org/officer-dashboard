package store

import (
	"context"
	"encoding/json"
	"strings"
)

// ServiceToken is a service_tokens row. Only the SHA-256 of the secret is stored.
type ServiceToken struct {
	ID         string
	Name       string
	TokenHash  string
	Prefix     string
	Scopes     []string
	CreatedBy  string
	LastUsedAt *int64
	ExpiresAt  *int64
	RevokedAt  *int64
	CreatedAt  Ms
}

const tokenCols = `id, name, token_hash, prefix, scopes, created_by, last_used_at, expires_at, revoked_at, created_at`

func scanToken(s interface{ Scan(...any) error }) (*ServiceToken, error) {
	var t ServiceToken
	var scopes string
	var used, exp, rev *Ms
	if err := s.Scan(&t.ID, &t.Name, &t.TokenHash, &t.Prefix, &scopes, &t.CreatedBy, &used, &exp, &rev, &t.CreatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(scopes), &t.Scopes); err != nil {
		return nil, err
	}
	conv := func(m *Ms) *int64 {
		if m == nil {
			return nil
		}
		v := int64(*m)
		return &v
	}
	t.LastUsedAt, t.ExpiresAt, t.RevokedAt = conv(used), conv(exp), conv(rev)
	return &t, nil
}

// InsertToken stores a hashed token.
func (q *Queries) InsertToken(ctx context.Context, t ServiceToken) error {
	scopes, _ := json.Marshal(t.Scopes)
	_, err := q.q.ExecContext(ctx, `INSERT INTO service_tokens (id, name, token_hash, prefix, scopes, created_by, expires_at, created_at) VALUES (?,?,?,?,?,?,?,?)`,
		t.ID, t.Name, t.TokenHash, t.Prefix, string(scopes), t.CreatedBy, t.ExpiresAt, int64(t.CreatedAt))
	return err
}

// TokenByHash returns ErrNotFound for an unknown token.
func (q *Queries) TokenByHash(ctx context.Context, hash string) (*ServiceToken, error) {
	t, err := scanToken(q.q.QueryRowContext(ctx, `SELECT `+tokenCols+` FROM service_tokens WHERE token_hash = ?`, hash))
	return t, notFound(err)
}

// TokenByID returns ErrNotFound for an unknown id.
func (q *Queries) TokenByID(ctx context.Context, id string) (*ServiceToken, error) {
	t, err := scanToken(q.q.QueryRowContext(ctx, `SELECT `+tokenCols+` FROM service_tokens WHERE id = ?`, id))
	return t, notFound(err)
}

// Tokens lists tokens in creation order.
func (q *Queries) Tokens(ctx context.Context) ([]ServiceToken, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT `+tokenCols+` FROM service_tokens ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ServiceToken
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// RevokeToken marks a token revoked (the row stays for the audit trail).
func (q *Queries) RevokeToken(ctx context.Context, id string, now int64) error {
	_, err := q.q.ExecContext(ctx, `UPDATE service_tokens SET revoked_at = ? WHERE id = ?`, now, id)
	return err
}

// TouchToken records that a token was used.
func (q *Queries) TouchToken(ctx context.Context, id string, now int64) error {
	_, err := q.q.ExecContext(ctx, `UPDATE service_tokens SET last_used_at = ? WHERE id = ?`, now, id)
	return err
}

// ─── audit log ───

// AuditEntry is an audit_log row.
type AuditEntry struct {
	ID          string
	ActorUserID *string
	ActorToken  *string
	Action      string
	EntityType  string
	EntityID    *string
	BeforeJSON  *string
	AfterJSON   *string
	CreatedAt   Ms
}

// InsertAudit appends to the audit log.
func (q *Queries) InsertAudit(ctx context.Context, a AuditEntry) error {
	_, err := q.q.ExecContext(ctx, `INSERT INTO audit_log (id, actor_user_id, actor_token_id, action, entity_type, entity_id, before_json, after_json, created_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		a.ID, a.ActorUserID, a.ActorToken, a.Action, a.EntityType, a.EntityID, a.BeforeJSON, a.AfterJSON, int64(a.CreatedAt))
	return err
}

// AuditFilter selects audit rows, newest first.
type AuditFilter struct {
	EntityType string
	EntityID   string
	Limit      int
	// Continue after (created_at, id).
	HasCursor       bool
	CursorCreatedAt int64
	CursorID        string
}

// Audit lists audit rows newest first.
func (q *Queries) Audit(ctx context.Context, f AuditFilter) ([]AuditEntry, error) {
	var conds []string
	var args []any
	if f.EntityType != "" {
		conds = append(conds, "entity_type = ?")
		args = append(args, f.EntityType)
	}
	if f.EntityID != "" {
		conds = append(conds, "entity_id = ?")
		args = append(args, f.EntityID)
	}
	if f.HasCursor {
		conds = append(conds, "(created_at < ? OR (created_at = ? AND id < ?))")
		args = append(args, f.CursorCreatedAt, f.CursorCreatedAt, f.CursorID)
	}
	query := `SELECT id, actor_user_id, actor_token_id, action, entity_type, entity_id, before_json, after_json, created_at FROM audit_log`
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += " ORDER BY created_at DESC, id DESC"
	if f.Limit > 0 {
		query += " LIMIT " + itoa(f.Limit)
	}
	rows, err := q.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var a AuditEntry
		if err := rows.Scan(&a.ID, &a.ActorUserID, &a.ActorToken, &a.Action, &a.EntityType, &a.EntityID, &a.BeforeJSON, &a.AfterJSON, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AuditAfter lists audit rows created after a cutoff (ms), newest first. Reports use it to
// reconstruct request statuses as of a past date from the immutable before-images.
func (q *Queries) AuditAfter(ctx context.Context, cutoff int64) ([]AuditEntry, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT id, actor_user_id, actor_token_id, action, entity_type, entity_id, before_json, after_json, created_at FROM audit_log WHERE created_at > ? ORDER BY created_at DESC, rowid DESC`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var a AuditEntry
		if err := rows.Scan(&a.ID, &a.ActorUserID, &a.ActorToken, &a.Action, &a.EntityType, &a.EntityID, &a.BeforeJSON, &a.AfterJSON, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
