package store

import (
	"context"
	"fmt"
	"time"
)

// Restoring dashboard.db cannot bring back R2 operations spent after the
// snapshot. A restore therefore leaves an r2_reseed marker, and billable R2
// requests (uploads, listings and downloads) stay paused (D-21) until either:
//   - Cloudflare's analytics re-seeds the ledger (ReseedR2). Downloads then
//     resume; uploads also wait for a complete inventory taken afterwards.
//   - R2ReseedPauseDays UTC days pass from the restore, so every operation the
//     ledger may have lost has left both the monthly and the rolling window.
// Nothing is ever assumed: an unknown ledger never spends.

// R2ReseedPauseDays is the safety cap: the guard's rolling window is the
// current UTC day plus the 31 before it, so from the 32nd UTC day after the
// restore day no lost operation is inside it (or inside the calendar month).
const R2ReseedPauseDays = 32

var ErrR2Reseeding = fmt.Errorf("R2 storage is unavailable: paused after a database restore until Cloudflare analytics re-seeds the usage counters (at most %d days): %w", R2ReseedPauseDays, ErrR2Budget)

// R2Seed is usage measured outside the ledger: operation counts for the current
// UTC month and for the guard's rolling window (current UTC day plus the 31
// before it), and stored bytes.
type R2Seed struct{ MonthA, MonthB, RollingA, RollingB, Bytes int64 }

// R2ReseedState describes a restore pause. Pending false means none.
type R2ReseedState struct {
	Pending bool
	// Until is when the pause ends on its own; AlertedAt is the last delivered
	// pause alert (zero if none).
	Until, AlertedAt time.Time
}

// r2ReseedUntil is the start of the 32nd UTC day after the restore day.
const r2ReseedUntil = `(requested_at/86400+32)*86400`

func (d *DB) R2Reseed(ctx context.Context) (R2ReseedState, error) {
	var s R2ReseedState
	var n, until, alerted int64
	err := d.R.QueryRowContext(ctx, `SELECT count(*),coalesce(max(`+r2ReseedUntil+`),0),coalesce(max(alerted_at),0) FROM r2_reseed`).Scan(&n, &until, &alerted)
	s.Pending, s.Until = n > 0, time.Unix(until, 0).UTC()
	if alerted > 0 {
		s.AlertedAt = time.Unix(alerted, 0).UTC()
	}
	return s, err
}

// RequestR2Reseed marks the ledger as possibly stale and pauses R2.
func (d *DB) RequestR2Reseed(ctx context.Context, now time.Time) error {
	_, err := d.W.ExecContext(ctx, `INSERT INTO r2_reseed(id,requested_at) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET requested_at=excluded.requested_at,alerted_at=0`, now.Unix())
	return err
}

// ReseedR2 max-merges a Cloudflare measurement into the ledger, per counter
// and per window, never lowering anything, and ends the pause. A rolling-window
// shortfall is added to today's row, which keeps it inside the window for the
// longest possible time.
func (d *DB) ReseedR2(ctx context.Context, now time.Time, seed R2Seed) error {
	if seed.MonthA < 0 || seed.MonthB < 0 || seed.RollingA < 0 || seed.RollingB < 0 || seed.Bytes < 0 {
		return fmt.Errorf("invalid R2 usage seed")
	}
	return d.Write(ctx, func(q *Queries) error {
		now := now.UTC()
		month, day := now.Format("2006-01"), now.Format("2006-01-02")
		if _, err := q.q.ExecContext(ctx, `INSERT OR IGNORE INTO r2_month(month) VALUES(?)`, month); err != nil {
			return err
		}
		if _, err := q.q.ExecContext(ctx, `UPDATE r2_month SET class_a=max(class_a,?),class_b=max(class_b,?) WHERE month=?`, seed.MonthA, seed.MonthB, month); err != nil {
			return err
		}
		var rollingA, rollingB int64
		if err := q.q.QueryRowContext(ctx, `SELECT coalesce(sum(class_a),0),coalesce(sum(class_b),0) FROM r2_day WHERE day>=?`, now.AddDate(0, 0, -31).Format("2006-01-02")).Scan(&rollingA, &rollingB); err != nil {
			return err
		}
		if a, b := max(seed.RollingA-rollingA, 0), max(seed.RollingB-rollingB, 0); a+b > 0 {
			if _, err := q.q.ExecContext(ctx, `INSERT INTO r2_day(day,class_a,class_b) VALUES(?,?,?) ON CONFLICT(day) DO UPDATE SET class_a=class_a+excluded.class_a,class_b=class_b+excluded.class_b`, day, a, b); err != nil {
				return err
			}
		}
		if _, err := q.q.ExecContext(ctx, `UPDATE r2_budget SET bytes=max(bytes,?) WHERE id=1`, seed.Bytes); err != nil {
			return err
		}
		if _, err := syncR2Mirror(ctx, q, d.r2MirrorPath(), now); err != nil {
			return err
		}
		return endR2Pause(ctx, q, `DELETE FROM r2_reseed`)
	})
}

// ExpireR2Reseed ends a pause whose safety cap has passed. It reports whether
// a pause is still in force.
func (d *DB) ExpireR2Reseed(ctx context.Context, now time.Time) (bool, error) {
	paused := false
	err := d.Write(ctx, func(q *Queries) error {
		if err := endR2Pause(ctx, q, `DELETE FROM r2_reseed WHERE `+r2ReseedUntil+`<=?`, now.Unix()); err != nil {
			return err
		}
		var n int
		err := q.q.QueryRowContext(ctx, `SELECT count(*) FROM r2_reseed`).Scan(&n)
		paused = n > 0
		return err
	})
	return paused, err
}

// endR2Pause runs the marker deletion and, in the same transaction, discards
// the inventory timestamp when it ended a pause: a restored inventory can look
// fresh but predates the restore, so uploads stay closed on the freshness
// gate until a complete inventory taken afterwards succeeds.
func endR2Pause(ctx context.Context, q *Queries, deletion string, args ...any) error {
	res, err := q.q.ExecContext(ctx, deletion, args...)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return err
	}
	_, err = q.q.ExecContext(ctx, `UPDATE r2_budget SET reconciled_at=0 WHERE id=1`)
	return err
}

// R2ReseedAlerted records the delivery time of a pause alert.
func (d *DB) R2ReseedAlerted(ctx context.Context, now time.Time) error {
	_, err := d.W.ExecContext(ctx, `UPDATE r2_reseed SET alerted_at=?`, now.Unix())
	return err
}
