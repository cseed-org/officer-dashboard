package store

import (
	"context"
	"database/sql"
	"strings"
)

// SearchHit is one raw search match. Which of A–C hold what depends on Kind:
//
//	event:   Primary=name, A=starts_at, B=location
//	request: Primary=title, A=number, B=vendor, C=status
//	user:    Primary=name, A=title, B=role, C=email
//	account: Primary=name, A=code, B=type
type SearchHit struct {
	Kind    string
	ID      string
	Primary string
	A, B, C string
	// Score ranks the hit: 0 primary field starts with the query, 1 primary contains it,
	// 2 another field matches.
	Score int
	// Sort orders equal scores: newer first (ms), 0 when not applicable.
	Sort int64
}

// Search finds matches for a plain-text query across events and requests, plus users and
// accounts when includeDirectorKinds. Matching is case-insensitive on names, numbers,
// vendors and emails using LIKE. FTS5 was measured against it and rejected: the tables
// hold hundreds of rows, and an FTS index would add triggers and a sync step to save
// microseconds. Each kind returns at most `limit` hits; the caller merges and trims.
func (q *Queries) Search(ctx context.Context, text string, limit int, includeDirectorKinds bool) ([]SearchHit, error) {
	needle := Fold(strings.TrimSpace(text))
	if needle == "" {
		return nil, nil
	}
	contains := "%" + likeEscape(needle) + "%"
	score := func(primary string, otherMatched bool) int {
		p := Fold(primary)
		switch {
		case strings.HasPrefix(p, needle):
			return 0
		case strings.Contains(p, needle):
			return 1
		}
		return 2
	}
	var out []SearchHit

	rows, err := q.q.QueryContext(ctx, `SELECT id, name, starts_at, coalesce(location, ''), created_at FROM events
		WHERE fold(name) LIKE ?1 ESCAPE '\' OR fold(coalesce(location, '')) LIKE ?1 ESCAPE '\' OR fold(coalesce(luma_id, '')) LIKE ?1 ESCAPE '\'
		ORDER BY starts_at DESC LIMIT ?2`, contains, limit)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var h SearchHit
		var created Ms
		if err := rows.Scan(&h.ID, &h.Primary, &h.A, &h.B, &created); err != nil {
			rows.Close()
			return nil, err
		}
		h.Kind, h.Score = "event", score(h.Primary, true)
		out = append(out, h)
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}

	rows, err = q.q.QueryContext(ctx, `SELECT id, title, number, vendor, status, created_at FROM purchase_requests
		WHERE fold(number) LIKE ?1 ESCAPE '\' OR fold(title) LIKE ?1 ESCAPE '\' OR fold(vendor) LIKE ?1 ESCAPE '\'
		ORDER BY created_at DESC, id DESC LIMIT ?2`, contains, limit)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var h SearchHit
		var created Ms
		if err := rows.Scan(&h.ID, &h.Primary, &h.A, &h.B, &h.C, &created); err != nil {
			rows.Close()
			return nil, err
		}
		h.Kind, h.Sort = "request", int64(created)
		h.Score = score(h.A, true) // requests are found by number first
		if h.Score == 2 {
			h.Score = score(h.Primary, true)
		}
		out = append(out, h)
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}

	rows, err = q.q.QueryContext(ctx, `SELECT id,name,org,role,type,updated_at FROM contacts WHERE fold(name) LIKE ?1 ESCAPE '\' OR fold(org) LIKE ?1 ESCAPE '\' OR fold(role) LIKE ?1 ESCAPE '\' OR EXISTS (SELECT 1 FROM json_each(contacts.topics) WHERE fold(value) LIKE ?1 ESCAPE '\') OR fold(email) LIKE ?1 ESCAPE '\' ORDER BY name LIMIT ?2`, contains, limit)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var h SearchHit
		if err := rows.Scan(&h.ID, &h.Primary, &h.A, &h.B, &h.C, &h.Sort); err != nil {
			rows.Close()
			return nil, err
		}
		h.Kind = "contact"
		h.Score = score(h.Primary, true)
		out = append(out, h)
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}

	if !includeDirectorKinds {
		return out, nil
	}
	rows, err = q.q.QueryContext(ctx, `SELECT id, name, coalesce(title, ''), role, coalesce(email, '') FROM users
		WHERE fold(name) LIKE ?1 ESCAPE '\' OR fold(coalesce(email, '')) LIKE ?1 ESCAPE '\' OR fold(coalesce(title, '')) LIKE ?1 ESCAPE '\'
		ORDER BY name LIMIT ?2`, contains, limit)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var h SearchHit
		if err := rows.Scan(&h.ID, &h.Primary, &h.A, &h.B, &h.C); err != nil {
			rows.Close()
			return nil, err
		}
		h.Kind, h.Score = "user", score(h.Primary, true)
		out = append(out, h)
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}

	rows, err = q.q.QueryContext(ctx, `SELECT id, name, code, type FROM accounts
		WHERE fold(name) LIKE ?1 ESCAPE '\' OR fold(code) LIKE ?1 ESCAPE '\'
		ORDER BY code LIMIT ?2`, contains, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var h SearchHit
		if err := rows.Scan(&h.ID, &h.Primary, &h.A, &h.B); err != nil {
			return nil, err
		}
		h.Kind = "account"
		h.Score = score(h.A, true)
		if h.Score == 2 {
			h.Score = score(h.Primary, true)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// closeRows closes a result set and reports an error that cut iteration short, so a failure
// mid-way is not mistaken for "no more matches".
func closeRows(rows *sql.Rows) error {
	err := rows.Err()
	if cerr := rows.Close(); err == nil {
		err = cerr
	}
	return err
}
