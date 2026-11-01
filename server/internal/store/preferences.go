package store

import "context"

// WidgetPlacement is ordered in the layout array; size is S, M, or L.
type WidgetPlacement struct {
	ID   string `json:"id"`
	Size string `json:"size"`
}

func (q *Queries) WidgetLayout(ctx context.Context, userID string) (string, error) {
	var layout string
	err := q.q.QueryRowContext(ctx, `SELECT widget_layout FROM user_prefs WHERE user_id=?`, userID).Scan(&layout)
	return layout, notFound(err)
}
func (q *Queries) SaveWidgetLayout(ctx context.Context, userID, layout string, now int64) error {
	return q.Exec(ctx, `INSERT INTO user_prefs(user_id,widget_layout,updated_at) VALUES(?,?,?) ON CONFLICT(user_id) DO UPDATE SET widget_layout=excluded.widget_layout,updated_at=excluded.updated_at`, userID, layout, now)
}

type CalendarEmail struct {
	ID     string `json:"id"`
	Email  string `json:"email"`
	Active bool   `json:"active"`
}

func (q *Queries) CalendarEmails(ctx context.Context, userID string) ([]CalendarEmail, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT id,email,active FROM calendar_emails WHERE user_id=? ORDER BY rowid`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CalendarEmail{}
	for rows.Next() {
		var e CalendarEmail
		if err := rows.Scan(&e.ID, &e.Email, &e.Active); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SaveCalendarEmail switches the active address in the same transaction as the write.
func (q *Queries) SaveCalendarEmail(ctx context.Context, userID string, e CalendarEmail, create bool) error {
	if e.Active {
		if err := q.Exec(ctx, `UPDATE calendar_emails SET active=0 WHERE user_id=?`, userID); err != nil {
			return err
		}
	}
	if create {
		return q.Exec(ctx, `INSERT INTO calendar_emails(id,user_id,email,active) VALUES(?,?,?,?)`, e.ID, userID, e.Email, e.Active)
	}
	return q.Exec(ctx, `UPDATE calendar_emails SET email=?,active=? WHERE id=? AND user_id=?`, e.Email, e.Active, e.ID, userID)
}
func (q *Queries) DeleteCalendarEmail(ctx context.Context, userID, id string) error {
	return q.Exec(ctx, `DELETE FROM calendar_emails WHERE user_id=? AND id=?`, userID, id)
}
