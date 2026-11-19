package store

import (
	"context"
	"encoding/json"
)

func (q *Queries) designRows(ctx context.Context, where string, args ...any) ([]DesignRequest, error) {
	rows, err := q.query(ctx, `SELECT `+designCols+` FROM design_requests `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DesignRequest{}
	for rows.Next() {
		d, err := scanDesign(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// DesignBoard retains every active card. Completed work has a separate cursor.
func (q *Queries) DesignBoard(ctx context.Context) ([]DesignRequest, error) {
	return q.designRows(ctx, `WHERE stage<>'delivered' ORDER BY updated_at DESC,id`)
}

func (q *Queries) DesignDeliveredTotal(ctx context.Context) (int, error) {
	rows, err := q.query(ctx, `SELECT count(*) FROM design_requests WHERE stage='delivered'`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	if !rows.Next() {
		return 0, rows.Err()
	}
	var total int
	if err := rows.Scan(&total); err != nil {
		return 0, err
	}
	return total, rows.Err()
}

func (q *Queries) DesignDeliveredPage(ctx context.Context, hasCursor bool, at int64, id string, limit int) ([]DesignRequest, int, error) {
	where := `WHERE stage='delivered'`
	var args []any
	if hasCursor {
		where += ` AND (updated_at < ? OR (updated_at=? AND id>?))`
		args = append(args, at, at, id)
	}
	args = append(args, limit)
	rows, err := q.query(ctx, `SELECT (SELECT count(*) FROM design_requests WHERE stage='delivered'),`+designCols+` FROM design_requests `+where+` ORDER BY updated_at DESC,id LIMIT ?`, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []DesignRequest{}
	total := 0
	for rows.Next() {
		var d DesignRequest
		if err := rows.Scan(&total, &d.ID, &d.Title, &d.Type, &d.Formats, &d.DueDate, &d.Urgency, &d.Brief, &d.Refs, &d.EventID, &d.Program, &d.Stage,
			&d.RequesterID, &d.AssigneeID, &d.ApprovedBy, &d.ApprovedAt, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if len(out) == 0 {
		total, err = q.DesignDeliveredTotal(ctx)
	}
	return out, total, err
}

func (q *Queries) DesignRequestsForEvents(ctx context.Context, ids []string) ([]DesignRequest, error) {
	if len(ids) == 0 {
		return []DesignRequest{}, nil
	}
	raw, _ := json.Marshal(ids)
	// The rail needs summaries, not briefs/references. Retain all active links and
	// six delivered links per event; the board's history cursor covers the rest.
	rows, err := q.query(ctx, `WITH ranked AS (
 SELECT id,title,event_id,stage,assignee_id,created_at,updated_at,
 row_number() OVER(PARTITION BY event_id,(stage='delivered') ORDER BY updated_at DESC,id) rn
 FROM design_requests WHERE event_id IN (SELECT value FROM json_each(?))
 ) SELECT id,title,event_id,stage,assignee_id,created_at FROM ranked WHERE stage<>'delivered' OR rn<=6 ORDER BY updated_at DESC,id`, string(raw))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DesignRequest{}
	for rows.Next() {
		var d DesignRequest
		if err := rows.Scan(&d.ID, &d.Title, &d.EventID, &d.Stage, &d.AssigneeID, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (q *Queries) EventsByIDs(ctx context.Context, ids []string) ([]Event, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	raw, _ := json.Marshal(ids)
	rows, err := q.query(ctx, eventSelect+` WHERE e.id IN (SELECT value FROM json_each(?))`, string(raw))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}
