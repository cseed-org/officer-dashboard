package store

import (
	"context"
	"database/sql"
	"errors"
)

// CalendarItem retains date-only deadlines separately from event instants.
type CalendarItem struct {
	ID       string  `json:"id"`
	Source   string  `json:"source"`
	Type     string  `json:"type"`
	Title    string  `json:"title"`
	Date     string  `json:"date"`
	StartsAt *string `json:"startsAt"`
	EndsAt   *string `json:"endsAt"`
	LumaURL  *string `json:"lumaUrl"`
}

func (q *Queries) CalendarItems(ctx context.Context, start, end, firstDate, endDate string, events, design bool) ([]CalendarItem, error) {
	out := []CalendarItem{}
	if events {
		rows, err := q.q.QueryContext(ctx, `SELECT id,name,starts_at,ends_at,luma_url,status FROM events
   WHERE julianday(starts_at)<julianday(?) AND
   (julianday(starts_at)>=julianday(?) OR julianday(ends_at)>julianday(?))
   ORDER BY julianday(starts_at),id`, end, start, start)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			v := CalendarItem{Source: "events"}
			if err := rows.Scan(&v.ID, &v.Title, &v.StartsAt, &v.EndsAt, &v.LumaURL, &v.Type); err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		rows.Close()
	}
	if design {
		rows, err := q.q.QueryContext(ctx, `SELECT id,title,due_date,type FROM design_requests WHERE due_date>=? AND due_date<? AND stage<>'delivered' ORDER BY due_date,id`, firstDate, endDate)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			v := CalendarItem{Source: "design"}
			if err := rows.Scan(&v.ID, &v.Title, &v.Date, &v.Type); err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (q *Queries) CalendarWidgetSettings(ctx context.Context, userID string) (string, error) {
	var raw string
	err := q.q.QueryRowContext(ctx, `SELECT settings FROM calendar_widget_settings WHERE user_id=?`, userID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return raw, err
}
func (q *Queries) SaveCalendarWidgetSettings(ctx context.Context, userID, raw string, now int64) error {
	return q.Exec(ctx, `INSERT INTO calendar_widget_settings(user_id,settings,updated_at) VALUES(?,?,?) ON CONFLICT(user_id) DO UPDATE SET settings=excluded.settings,updated_at=excluded.updated_at`, userID, raw, now)
}
