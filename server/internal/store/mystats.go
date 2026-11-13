package store

import "context"

// HostedEvent is the newest event a user hosted.
type HostedEvent struct {
	ID       string
	Name     string
	StartsAt string
}

// HostingStats are one user's own hosting numbers.
type HostingStats struct {
	EventsHosted int
	Attendees    int
	LastEvent    *HostedEvent
}

// hostedWhere defines "hosted": the user is the event's host_user_id, the event is published
// or completed (not a draft or cancelled) and it has already started.
const hostedWhere = `e.host_user_id = ? AND e.status IN ('published','completed') AND julianday(e.starts_at) <= julianday(?, 'unixepoch')`

// MyHostingStats returns events hosted, checked-in attendees across them and the most recent
// one. nowMs is the current time in Unix milliseconds.
func (q *Queries) MyHostingStats(ctx context.Context, userID string, nowMs int64) (HostingStats, error) {
	var s HostingStats
	nowSec := float64(nowMs) / 1000
	err := q.q.QueryRowContext(ctx, `SELECT count(*),
		coalesce(sum((SELECT count(*) FROM event_guests g WHERE g.event_id = e.id AND g.checked_in_at IS NOT NULL)), 0)
		FROM events e WHERE `+hostedWhere, userID, nowSec).Scan(&s.EventsHosted, &s.Attendees)
	if err != nil {
		return s, err
	}
	if s.EventsHosted == 0 {
		return s, nil
	}
	var h HostedEvent
	err = q.q.QueryRowContext(ctx, `SELECT e.id, e.name, e.starts_at FROM events e WHERE `+hostedWhere+` ORDER BY julianday(e.starts_at) DESC, e.id LIMIT 1`, userID, nowSec).Scan(&h.ID, &h.Name, &h.StartsAt)
	if err != nil {
		return s, err
	}
	s.LastEvent = &h
	return s, nil
}
