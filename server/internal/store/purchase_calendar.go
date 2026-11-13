package store

import (
	"context"
	"net/url"

	"officerdashboard/internal/money"
)

var purchaseStatusLabel = map[string]string{"draft": "draft", "submitted": "pending", "approved": "approved", "fulfilling": "buying"}

// loadPurchaseDeadlines returns open (submitted, approved, fulfilling) purchase requests whose date-only needed_by falls in
// the window. Every officer can read every request (the Finance list's scope=all), so the
// title, amount and status shown here stay inside that boundary; other officers' drafts
// are omitted as noise. Denied, cancelled, fulfilled, reimbursed and closed requests are
// hidden: the purchase is either done or not happening.
func loadPurchaseDeadlines(ctx context.Context, q *Queries, window CalendarRange) ([]CalendarItem, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT id,number,title,needed_by,status,total_cents FROM purchase_requests
 WHERE needed_by IS NOT NULL AND needed_by>=? AND needed_by<?
 AND (status IN ('submitted','approved','fulfilling') OR (status='draft' AND requester_id=?))
 ORDER BY needed_by,id`, window.FirstDate, window.EndDate, window.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CalendarItem{}
	for rows.Next() {
		var v CalendarItem
		var number string
		var cents int64
		if err := rows.Scan(&v.ID, &number, &v.Title, &v.Date, &v.Type, &cents); err != nil {
			return nil, err
		}
		detail := "#" + number + " · " + money.Format(cents) + " · " + purchaseStatusLabel[v.Type]
		v.Detail = &detail
		v.Href = "/finance?id=" + url.QueryEscape(v.ID)
		out = append(out, v)
	}
	return out, rows.Err()
}
