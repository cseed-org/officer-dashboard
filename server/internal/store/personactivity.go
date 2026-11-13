package store

import (
	"context"
	"sort"
	"time"
)

// PersonStats are the numbers on a person drawer's hero, all computed from existing rows:
// events hosted (the same definition as MyHostingStats), design requests still open that the
// person requested or is designing, and purchase requests of theirs that are still in flight.
type PersonStats struct {
	EventsHosted  int
	OpenDesign    int
	OpenPurchases int
}

// ActivityItem is one line of a person's recent activity.
type ActivityItem struct {
	Kind   string // event | design | purchase
	ID     string
	Title  string
	Status string // event: hosted | hosting; design: stage; purchase: status
	Role   string // design: requested | designing; otherwise empty
	At     time.Time
}

// openPurchase matches purchase requests that are submitted and not yet finished (the same
// set the purchase calendar treats as in flight). Drafts are private to their requester.
const openPurchase = `status IN ('submitted','approved','fulfilling')`

// PersonActivity returns a user's hero stats and up to limit recent activity items, newest
// first. Drafts (events and purchases) and cancelled events are left out: other officers
// cannot see them elsewhere either.
func (q *Queries) PersonActivity(ctx context.Context, userID string, nowMs int64, limit int) (PersonStats, []ActivityItem, error) {
	var s PersonStats
	nowSec := float64(nowMs) / 1000
	if err := q.q.QueryRowContext(ctx, `SELECT count(*) FROM events e WHERE `+hostedWhere, userID, nowSec).Scan(&s.EventsHosted); err != nil {
		return s, nil, err
	}
	if err := q.q.QueryRowContext(ctx, `SELECT count(*) FROM design_requests WHERE stage <> 'delivered' AND (requester_id = ? OR assignee_id = ?)`, userID, userID).Scan(&s.OpenDesign); err != nil {
		return s, nil, err
	}
	if err := q.q.QueryRowContext(ctx, `SELECT count(*) FROM purchase_requests WHERE requester_id = ? AND `+openPurchase, userID).Scan(&s.OpenPurchases); err != nil {
		return s, nil, err
	}

	var items []ActivityItem
	rows, err := q.q.QueryContext(ctx, `SELECT id, name, starts_at, julianday(starts_at) <= julianday(?, 'unixepoch') FROM events
		WHERE host_user_id = ? AND status IN ('published','completed') ORDER BY julianday(starts_at) DESC, id LIMIT ?`, nowSec, userID, limit)
	if err != nil {
		return s, nil, err
	}
	for rows.Next() {
		var it ActivityItem
		var starts string
		var past bool
		if err := rows.Scan(&it.ID, &it.Title, &starts, &past); err != nil {
			rows.Close()
			return s, nil, err
		}
		at, err := time.Parse(time.RFC3339, starts)
		if err != nil {
			continue // a malformed start time is skipped rather than failing the drawer
		}
		it.Kind, it.At, it.Status = "event", at, "hosting"
		if past {
			it.Status = "hosted"
		}
		items = append(items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return s, nil, err
	}

	rows, err = q.q.QueryContext(ctx, `SELECT id, title, stage, requester_id = ?, updated_at FROM design_requests
		WHERE requester_id = ? OR assignee_id = ? ORDER BY updated_at DESC, id LIMIT ?`, userID, userID, userID, limit)
	if err != nil {
		return s, nil, err
	}
	for rows.Next() {
		var it ActivityItem
		var requested bool
		var ms int64
		if err := rows.Scan(&it.ID, &it.Title, &it.Status, &requested, &ms); err != nil {
			rows.Close()
			return s, nil, err
		}
		it.Kind, it.At, it.Role = "design", time.UnixMilli(ms).UTC(), "designing"
		if requested {
			it.Role = "requested"
		}
		items = append(items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return s, nil, err
	}

	rows, err = q.q.QueryContext(ctx, `SELECT id, title, status, coalesce(submitted_at, created_at) FROM purchase_requests
		WHERE requester_id = ? AND status <> 'draft' ORDER BY coalesce(submitted_at, created_at) DESC, id LIMIT ?`, userID, limit)
	if err != nil {
		return s, nil, err
	}
	for rows.Next() {
		var it ActivityItem
		var ms int64
		if err := rows.Scan(&it.ID, &it.Title, &it.Status, &ms); err != nil {
			rows.Close()
			return s, nil, err
		}
		it.Kind, it.At = "purchase", time.UnixMilli(ms).UTC()
		items = append(items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return s, nil, err
	}

	sort.SliceStable(items, func(i, j int) bool { return items[i].At.After(items[j].At) })
	if len(items) > limit {
		items = items[:limit]
	}
	return s, items, nil
}
