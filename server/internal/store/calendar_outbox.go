package store

import (
	"context"
	"database/sql"
	"encoding/json"
)

// CalendarSnapshot is what a cancellation needs of a deleted event to rebuild its invite
// (UID, summary, times, place, links); the event row itself is gone by then.
type CalendarSnapshot struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	StartsAt string  `json:"startsAt"`
	EndsAt   *string `json:"endsAt,omitempty"`
	Location *string `json:"location,omitempty"`
	URL      *string `json:"url,omitempty"`
}

// SnapshotOf copies the calendar fields of an event.
func SnapshotOf(e *Event) CalendarSnapshot {
	return CalendarSnapshot{ID: e.ID, Name: e.Name, StartsAt: e.StartsAt, EndsAt: e.EndsAt, Location: e.Location, URL: e.URL}
}

// Event rebuilds the event fields a calendar invite uses.
func (s CalendarSnapshot) Event() *Event {
	return &Event{ID: s.ID, Name: s.Name, StartsAt: s.StartsAt, EndsAt: s.EndsAt, Location: s.Location, URL: s.URL, Status: "cancelled"}
}

// CalendarJob is one row of the calendar outbox.
//
//	sync:     deliver event EventID (re-read when it runs) to every recipient
//	backfill: deliver every upcoming event to UserID
//	cancel:   withdraw the deleted event Snapshot from the destinations in Priors
//
// A job stays until it has fully succeeded; nothing is dropped after some number of
// failures. Attempts counts failed runs, and the worker leaves a failed job alone until
// NextAttemptAt (backoff) unless it is told to retry everything (startup, hourly).
type CalendarJob struct {
	ID            int64
	Kind          string
	EventID       string
	UserID        string
	Snapshot      *CalendarSnapshot
	Priors        []Delivery
	Attempts      int
	LastError     *string
	NextAttemptAt int64
}

// EnqueueCalendar adds a job; call it in the transaction that makes the change it delivers.
func (q *Queries) EnqueueCalendar(ctx context.Context, j CalendarJob, now int64) error {
	var snap, priors any
	if j.Snapshot != nil {
		b, err := json.Marshal(j.Snapshot)
		if err != nil {
			return err
		}
		snap = string(b)
	}
	if j.Priors != nil {
		b, err := json.Marshal(j.Priors)
		if err != nil {
			return err
		}
		priors = string(b)
	}
	return q.Exec(ctx, `INSERT INTO calendar_outbox(kind,event_id,user_id,snapshot,priors,created_at) VALUES(?,?,?,?,?,?)`,
		j.Kind, nz(j.EventID), nz(j.UserID), snap, priors, now)
}

// NextCalendarJob returns the oldest job after afterID that is due at now (any job when
// all is true), or ErrNotFound.
func (q *Queries) NextCalendarJob(ctx context.Context, afterID, now int64, all bool) (*CalendarJob, error) {
	var j CalendarJob
	var eventID, userID, snap, priors sql.NullString
	err := q.q.QueryRowContext(ctx, `SELECT id,kind,event_id,user_id,snapshot,priors,attempts,last_error,next_attempt_at FROM calendar_outbox
 WHERE id>? AND (? OR next_attempt_at<=?) ORDER BY id LIMIT 1`, afterID, all, now).
		Scan(&j.ID, &j.Kind, &eventID, &userID, &snap, &priors, &j.Attempts, &j.LastError, &j.NextAttemptAt)
	if err != nil {
		return nil, notFound(err)
	}
	j.EventID, j.UserID = eventID.String, userID.String
	if snap.Valid {
		j.Snapshot = &CalendarSnapshot{}
		if err := json.Unmarshal([]byte(snap.String), j.Snapshot); err != nil {
			return nil, err
		}
	}
	if priors.Valid {
		if err := json.Unmarshal([]byte(priors.String), &j.Priors); err != nil {
			return nil, err
		}
	}
	return &j, nil
}

// UpdateCalendarJob records a partial or failed run: what is still to do, why, and when
// to try again.
func (q *Queries) UpdateCalendarJob(ctx context.Context, id int64, priors []Delivery, attempts int, lastError *string, nextAttemptAt int64) error {
	var p any
	if priors != nil {
		b, err := json.Marshal(priors)
		if err != nil {
			return err
		}
		p = string(b)
	}
	return q.Exec(ctx, `UPDATE calendar_outbox SET priors=?, attempts=?, last_error=?, next_attempt_at=? WHERE id=?`, p, attempts, lastError, nextAttemptAt, id)
}

// DeleteCalendarJob removes a finished job.
func (q *Queries) DeleteCalendarJob(ctx context.Context, id int64) error {
	return q.Exec(ctx, `DELETE FROM calendar_outbox WHERE id=?`, id)
}

// PendingCalendarJobs counts the outbox (tests, diagnostics).
func (q *Queries) PendingCalendarJobs(ctx context.Context) (int, error) {
	var n int
	err := q.q.QueryRowContext(ctx, `SELECT count(*) FROM calendar_outbox`).Scan(&n)
	return n, err
}
