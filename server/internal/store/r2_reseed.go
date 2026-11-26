package store

import (
	"context"
	"fmt"
	"time"
)

// Restoring dashboard.db cannot bring back R2 operations spent after the
// snapshot. A restore therefore leaves an r2_reseed marker, and billable R2
// requests (uploads, listings and downloads) stay paused (D-21) until either:
//   - Cloudflare's analytics re-seeds the ledger (ReseedR2) under the strict
//     rule below. Downloads then resume; uploads also wait for a complete
//     inventory taken afterwards.
//   - R2ReseedPauseDays UTC days pass from the restore, so every operation the
//     ledger may have lost has left both the monthly and the rolling window.
// Nothing is ever assumed: an unknown ledger never spends.
//
// Cloudflare's adaptive analytics are sampled estimates that lag ingestion,
// so a reading is not an upper bound by itself. A reading only ends the pause
// when (1) it is taken at least R2ReseedLag after the restore marker (R2 has
// sent nothing since, and pre-restore operations have had time to arrive),
// (2) twice the reading is merged into the ledger, and (3) every resulting
// counter is still below the 90% refusal line. Otherwise the pause runs to
// the 32-day cap.

// R2ReseedPauseDays is the safety cap: the guard's rolling window is the
// current UTC day plus the 31 before it, so from the 32nd UTC day after the
// restore day no lost operation is inside it (or inside the calendar month).
const R2ReseedPauseDays = 32

// R2ReseedLag is how long after the restore marker an analytics reading may
// first end the pause.
const R2ReseedLag = 6 * time.Hour

var ErrR2Reseeding = fmt.Errorf("R2 storage is unavailable: paused after a database restore until Cloudflare analytics re-seeds the usage counters (at most %d days): %w", R2ReseedPauseDays, ErrR2Budget)

// R2Seed is usage measured outside the ledger: operation counts for the current
// UTC month and for the guard's rolling window (current UTC day plus the 31
// before it), and stored bytes.
type R2Seed struct{ MonthA, MonthB, RollingA, RollingB, Bytes int64 }

// R2ReseedState describes a restore pause. Pending false means none.
type R2ReseedState struct {
	Pending bool
	// RequestedAt is the restore marker; Until is when the pause ends on its
	// own; AlertedAt is the last delivered pause alert (zero if none);
	// OverGuard is when a doubled reading reached the 90% line (zero if not),
	// after which the pause can only end at Until.
	RequestedAt, Until, AlertedAt, OverGuard time.Time
}

// ReadyAt is the earliest time an analytics reading may end the pause.
func (s R2ReseedState) ReadyAt() time.Time { return s.RequestedAt.Add(R2ReseedLag) }

// R2ReseedResult is the outcome of an analytics re-seed attempt.
type R2ReseedResult int

const (
	// R2ReseedUnlocked: the pause ended (or there was none).
	R2ReseedUnlocked R2ReseedResult = iota
	// R2ReseedTooEarly: inside the lag window; nothing changed.
	R2ReseedTooEarly
	// R2ReseedOverGuard: the doubled reading was merged but reaches the 90%
	// line, so R2 stays paused until the 32-day cap.
	R2ReseedOverGuard
)

// r2ReseedUntil is the start of the 32nd UTC day after the restore day.
const r2ReseedUntil = `(requested_at/86400+32)*86400`

func unixOrZero(v int64) time.Time {
	if v <= 0 {
		return time.Time{}
	}
	return time.Unix(v, 0).UTC()
}

func (d *DB) R2Reseed(ctx context.Context) (R2ReseedState, error) {
	var s R2ReseedState
	var n, requested, until, alerted, over int64
	err := d.R.QueryRowContext(ctx, `SELECT count(*),coalesce(max(requested_at),0),coalesce(max(`+r2ReseedUntil+`),0),coalesce(max(alerted_at),0),coalesce(max(over_guard_at),0) FROM r2_reseed`).Scan(&n, &requested, &until, &alerted, &over)
	s.Pending, s.RequestedAt, s.Until = n > 0, time.Unix(requested, 0).UTC(), time.Unix(until, 0).UTC()
	s.AlertedAt, s.OverGuard = unixOrZero(alerted), unixOrZero(over)
	return s, err
}

// RequestR2Reseed marks the ledger as possibly stale and pauses R2.
func (d *DB) RequestR2Reseed(ctx context.Context, now time.Time) error {
	_, err := d.W.ExecContext(ctx, `INSERT INTO r2_reseed(id,requested_at) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET requested_at=excluded.requested_at,alerted_at=0,over_guard_at=0`, now.Unix())
	return err
}

// r2SeedCap keeps doubling far from overflow; any larger reading is far past
// every allowance anyway.
const r2SeedCap = 1 << 50

func doubled(v int64) int64 { return 2 * min(v, r2SeedCap) }

// ReseedR2 applies a Cloudflare measurement under the strict rule above. It
// merges max(ledger, 2 × measured) per counter and per window, never lowering
// anything. A rolling-window shortfall is added to today's row, which keeps it
// inside the window for the longest possible time.
func (d *DB) ReseedR2(ctx context.Context, now time.Time, measured R2Seed) (R2ReseedResult, error) {
	if measured.MonthA < 0 || measured.MonthB < 0 || measured.RollingA < 0 || measured.RollingB < 0 || measured.Bytes < 0 {
		return 0, fmt.Errorf("invalid R2 usage seed")
	}
	seed := R2Seed{doubled(measured.MonthA), doubled(measured.MonthB), doubled(measured.RollingA), doubled(measured.RollingB), doubled(measured.Bytes)}
	result := R2ReseedUnlocked
	err := d.Write(ctx, func(q *Queries) error {
		now := now.UTC()
		var pending int
		var requested, over int64
		if err := q.q.QueryRowContext(ctx, `SELECT count(*),coalesce(max(requested_at),0),coalesce(max(over_guard_at),0) FROM r2_reseed`).Scan(&pending, &requested, &over); err != nil {
			return err
		}
		if pending == 0 {
			return nil
		}
		if over > 0 {
			result = R2ReseedOverGuard
			return nil
		}
		if now.Before(time.Unix(requested, 0).Add(R2ReseedLag)) {
			result = R2ReseedTooEarly
			return nil
		}
		month, day := now.Format("2006-01"), now.Format("2006-01-02")
		if _, err := q.q.ExecContext(ctx, `INSERT OR IGNORE INTO r2_month(month) VALUES(?)`, month); err != nil {
			return err
		}
		if _, err := q.q.ExecContext(ctx, `UPDATE r2_month SET class_a=max(class_a,?),class_b=max(class_b,?) WHERE month=?`, seed.MonthA, seed.MonthB, month); err != nil {
			return err
		}
		since := now.AddDate(0, 0, -31).Format("2006-01-02")
		var rollingA, rollingB int64
		if err := q.q.QueryRowContext(ctx, `SELECT coalesce(sum(class_a),0),coalesce(sum(class_b),0) FROM r2_day WHERE day>=?`, since).Scan(&rollingA, &rollingB); err != nil {
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
		// Unlock only if every seeded counter can still take a request.
		var monthA, monthB, bytes int64
		if err := q.q.QueryRowContext(ctx, `SELECT class_a,class_b,(SELECT coalesce(max(bytes),0) FROM r2_budget) FROM r2_month WHERE month=?`, month).Scan(&monthA, &monthB, &bytes); err != nil {
			return err
		}
		if err := q.q.QueryRowContext(ctx, `SELECT coalesce(sum(class_a),0),coalesce(sum(class_b),0) FROM r2_day WHERE day>=?`, since).Scan(&rollingA, &rollingB); err != nil {
			return err
		}
		if max(monthA, rollingA) >= R2ClassALimit*9/10 || max(monthB, rollingB) >= R2ClassBLimit*9/10 || bytes >= R2StorageLimit*9/10 {
			result = R2ReseedOverGuard
			// A new alert says why; there is nothing further to retry.
			_, err := q.q.ExecContext(ctx, `UPDATE r2_reseed SET over_guard_at=?,alerted_at=0`, now.Unix())
			return err
		}
		return endR2Pause(ctx, q, `DELETE FROM r2_reseed`)
	})
	return result, err
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
