package store

import (
	"context"
	"fmt"
	"time"
)

// Restoring dashboard.db cannot bring back R2 operations spent after the
// snapshot. Instead of locking R2 (D-20), a restore leaves an r2_reseed marker
// and the app raises the ledger once, before it sends another billable request:
// from Cloudflare's analytics when that is configured and answers, otherwise to
// half of each free-tier allowance. The normal 70% warning and 90% refusal
// then apply as usual.

var ErrR2Reseeding = fmt.Errorf("R2 usage counters are being re-seeded after a database restore; retry in a minute: %w", ErrR2Budget)

// R2Seed is usage measured outside the ledger: operation counts for the current
// UTC month and for the guard's rolling window (current UTC day plus the 31
// before it), and stored bytes.
type R2Seed struct{ MonthA, MonthB, RollingA, RollingB, Bytes int64 }

// R2FallbackSeed assumes half of each free-tier allowance is already used.
func R2FallbackSeed() R2Seed {
	return R2Seed{MonthA: R2ClassALimit / 2, MonthB: R2ClassBLimit / 2, RollingA: R2ClassALimit / 2, RollingB: R2ClassBLimit / 2, Bytes: R2StorageLimit / 2}
}

// R2ReseedState describes a pending re-seed. Pending false means none is needed.
type R2ReseedState struct{ Pending, FallbackApplied, Alerted bool }

func (d *DB) R2Reseed(ctx context.Context) (R2ReseedState, error) {
	var s R2ReseedState
	var fallback, alerted int64
	err := d.R.QueryRowContext(ctx, `SELECT count(*),coalesce(max(fallback_at),0),coalesce(max(alerted),0) FROM r2_reseed`).Scan(&s.Pending, &fallback, &alerted)
	s.FallbackApplied, s.Alerted = fallback > 0, alerted != 0
	return s, err
}

// RequestR2Reseed marks the ledger as possibly stale.
func (d *DB) RequestR2Reseed(ctx context.Context, now time.Time) error {
	_, err := d.W.ExecContext(ctx, `INSERT INTO r2_reseed(id,requested_at) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET requested_at=excluded.requested_at,fallback_at=0,alerted=0`, now.Unix())
	return err
}

// ReseedR2 max-merges seed into the ledger, per counter and per window, and
// never lowers anything. A rolling-window shortfall is added to today's row,
// which keeps it inside the window for the longest possible time. measured
// (the seed came from Cloudflare) completes the re-seed; otherwise the marker
// records that the fallback was applied so billable requests resume and a later
// measurement can still raise the ledger further.
func (d *DB) ReseedR2(ctx context.Context, now time.Time, seed R2Seed, measured bool) error {
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
		// The next inventory replaces bytes with what is actually listed.
		if _, err := q.q.ExecContext(ctx, `UPDATE r2_budget SET bytes=max(bytes,?) WHERE id=1`, seed.Bytes); err != nil {
			return err
		}
		if _, err := syncR2Mirror(ctx, q, d.r2MirrorPath(), now); err != nil {
			return err
		}
		var err error
		if measured {
			_, err = q.q.ExecContext(ctx, `DELETE FROM r2_reseed`)
		} else {
			_, err = q.q.ExecContext(ctx, `UPDATE r2_reseed SET fallback_at=? WHERE fallback_at=0`, now.Unix())
		}
		return err
	})
}

// R2ReseedAlerted records the fallback warning as delivered; done also clears
// the marker (no analytics to retry).
func (d *DB) R2ReseedAlerted(ctx context.Context, done bool) error {
	query := `UPDATE r2_reseed SET alerted=1`
	if done {
		query = `DELETE FROM r2_reseed WHERE fallback_at>0`
	}
	_, err := d.W.ExecContext(ctx, query)
	return err
}
