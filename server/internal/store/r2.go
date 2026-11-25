package store

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Decimal GB and a persistent upper bound on live bytes keep even daily peaks
// below the free GB-month allowance. Failed/ambiguous requests are never refunded.
const R2StorageLimit int64 = 10_000_000_000
const R2ClassALimit int64 = 1_000_000
const R2ClassBLimit int64 = 10_000_000
const R2UploadsPerUserDay = 20

var ErrR2Budget = errors.New("R2 free-tier safety budget reached; try again later or contact a director")
var ErrR2DailyQuota = fmt.Errorf("Daily upload limit is 20 attempts per user; retry after midnight UTC: %w", ErrR2Budget)

type R2Usage struct{ Bytes, ClassA, ClassB int64 }

// Bind the durable ledger to one account/bucket; changing it requires a new ledger.
func (d *DB) BindR2(ctx context.Context, bucket string) error {
	if _, err := d.W.ExecContext(ctx, `PRAGMA synchronous=FULL`); err != nil {
		return err
	}
	return d.Write(ctx, func(q *Queries) error {
		if _, err := q.q.ExecContext(ctx, `INSERT OR IGNORE INTO r2_budget(id,bucket) VALUES(1,?)`, bucket); err != nil {
			return err
		}
		var existing string
		if err := q.q.QueryRowContext(ctx, `SELECT bucket FROM r2_budget WHERE id=1`).Scan(&existing); err != nil {
			return err
		}
		if existing != bucket {
			return errors.New("R2 bucket differs from budget ledger")
		}
		return nil
	})
}

// ReserveR2 commits BEFORE the network request. SQLite IMMEDIATE transactions
// serialize callers, including other processes using this same ledger.
func (d *DB) ReserveR2(ctx context.Context, now time.Time, class, key, user string, size int64) (bool, error) {
	alert := false
	err := d.Write(ctx, func(q *Queries) error {
		month := now.UTC().Format("2006-01")
		if class == "put" || class == "list" || class == "get" {
			var until string
			if err := q.q.QueryRowContext(ctx, `SELECT coalesce((SELECT until_utc FROM r2_restore_lock WHERE until_utc>?), '')`, now.UTC().Format("2006-01-02T15:04:05Z")).Scan(&until); err != nil {
				return err
			}
			if until != "" {
				return fmt.Errorf("R2 uploads and downloads locked until %s after database restore: %w", until, ErrR2Budget)
			}
		}
		if _, err := q.q.ExecContext(ctx, `INSERT OR IGNORE INTO r2_month(month) VALUES(?)`, month); err != nil {
			return err
		}
		var u R2Usage
		var reconciled int64
		var alerted int
		var attempt int64
		if err := q.q.QueryRowContext(ctx, `SELECT bytes,reconciled_at,class_a,class_b,alerted,alert_attempt_at FROM r2_budget,r2_month WHERE id=1 AND month=?`, month).Scan(&u.Bytes, &reconciled, &u.ClassA, &u.ClassB, &alerted, &attempt); err != nil {
			return err
		}
		if class == "put" && (size <= 0 || size > 10*1024*1024 || user == "" || reconciled < now.Add(-25*time.Hour).Unix() || reconciled > now.Unix()) {
			return ErrR2Budget
		}
		if class == "put" || class == "list" {
			if u.ClassA+1 > R2ClassALimit*9/10 {
				return ErrR2Budget
			}
			u.ClassA++
		}
		if class == "get" {
			if u.ClassB+1 > R2ClassBLimit*9/10 {
				return ErrR2Budget
			}
			u.ClassB++
		}
		// Current UTC day plus 31 preceding days covers any billing period up
		// to 31 days, with an extra day for requests crossing midnight in flight.
		var rollingA, rollingB int64
		if err := q.q.QueryRowContext(ctx, `SELECT coalesce(sum(class_a),0),coalesce(sum(class_b),0) FROM r2_day WHERE day>=?`, now.UTC().AddDate(0, 0, -31).Format("2006-01-02")).Scan(&rollingA, &rollingB); err != nil {
			return err
		}
		a, b := boolInt(class == "put" || class == "list"), boolInt(class == "get")
		if rollingA+int64(a) > R2ClassALimit*9/10 || rollingB+int64(b) > R2ClassBLimit*9/10 {
			return ErrR2Budget
		}
		if class == "put" {
			var existing int64
			if err := q.q.QueryRowContext(ctx, `SELECT coalesce((SELECT bytes FROM r2_objects WHERE key=?),0)`, key).Scan(&existing); err != nil {
				return err
			}
			if existing != 0 && existing != size {
				return errors.New("R2 retry content size differs")
			}
			if u.Bytes+size-existing > R2StorageLimit*9/10 {
				return ErrR2Budget
			}
			day := now.UTC().Format("2006-01-02")
			if _, err := q.q.ExecContext(ctx, `INSERT OR IGNORE INTO r2_user_day VALUES(?,?,0)`, user, day); err != nil {
				return err
			}
			r, err := q.q.ExecContext(ctx, `UPDATE r2_user_day SET uploads=uploads+1 WHERE user_id=? AND day=? AND uploads<?`, user, day, R2UploadsPerUserDay)
			if err != nil {
				return err
			}
			n, err := r.RowsAffected()
			if err != nil {
				return err
			}
			if n != 1 {
				return ErrR2DailyQuota
			}
			if _, err := q.q.ExecContext(ctx, `INSERT OR IGNORE INTO r2_objects VALUES(?,?)`, key, size); err != nil {
				return err
			}
			u.Bytes += size - existing
		}
		if _, err := q.q.ExecContext(ctx, `UPDATE r2_budget SET bytes=? WHERE id=1`, u.Bytes); err != nil {
			return err
		}
		if _, err := q.q.ExecContext(ctx, `INSERT INTO r2_day(day,class_a,class_b) VALUES(?,?,?) ON CONFLICT(day) DO UPDATE SET class_a=class_a+excluded.class_a,class_b=class_b+excluded.class_b`, now.UTC().Format("2006-01-02"), a, b); err != nil {
			return err
		}
		mask := 0
		if u.Bytes >= R2StorageLimit*7/10 {
			mask |= 1
		}
		if u.ClassA >= R2ClassALimit*7/10 || rollingA+int64(a) >= R2ClassALimit*7/10 {
			mask |= 2
		}
		if u.ClassB >= R2ClassBLimit*7/10 || rollingB+int64(b) >= R2ClassBLimit*7/10 {
			mask |= 4
		}
		if mask&^alerted != 0 && attempt <= now.Add(-15*time.Minute).Unix() {
			alert = true
			attempt = now.Unix()
			if _, err := q.q.ExecContext(ctx, `UPDATE r2_month SET alert_claim_mask=? WHERE month=?`, mask&^alerted, month); err != nil {
				return err
			}
		}
		_, err := q.q.ExecContext(ctx, `UPDATE r2_month SET class_a=?,class_b=?,deletes=deletes+?,alerted=?,alert_attempt_at=? WHERE month=?`, u.ClassA, u.ClassB, boolInt(class == "delete"), alerted, attempt, month)
		return err
	})
	return alert && err == nil, err
}

func (d *DB) AckR2Alert(ctx context.Context, now time.Time) error {
	_, err := d.W.ExecContext(ctx, `UPDATE r2_month SET alerted=alerted | alert_claim_mask WHERE month=?`, now.UTC().Format("2006-01"))
	return err
}

func (d *DB) R2LockedUntil(ctx context.Context) (time.Time, error) {
	var value string
	err := d.R.QueryRowContext(ctx, `SELECT coalesce((SELECT until_utc FROM r2_restore_lock WHERE id=1),'')`).Scan(&value)
	if err != nil || value == "" {
		return time.Time{}, err
	}
	return time.Parse("2006-01-02T15:04:05Z", value)
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (d *DB) R2Usage(ctx context.Context, now time.Time) (R2Usage, error) {
	var u R2Usage
	err := d.R.QueryRowContext(ctx, `SELECT bytes,coalesce(class_a,0),coalesce(class_b,0) FROM r2_budget LEFT JOIN r2_month ON month=? WHERE id=1`, now.UTC().Format("2006-01")).Scan(&u.Bytes, &u.ClassA, &u.ClassB)
	return u, err
}

// DeletedR2 frees only a known reservation, and only after confirmed deletion.
func (d *DB) DeletedR2(ctx context.Context, key string) error {
	return d.Write(ctx, func(q *Queries) error {
		if _, err := q.q.ExecContext(ctx, `UPDATE r2_migrations SET uploaded=0 WHERE key=?`, key); err != nil {
			return err
		}
		if _, err := q.q.ExecContext(ctx, `UPDATE r2_budget SET bytes=bytes-coalesce((SELECT bytes FROM r2_objects WHERE key=?),0) WHERE id=1`, key); err != nil {
			return err
		}
		_, err := q.q.ExecContext(ctx, `DELETE FROM r2_objects WHERE key=?`, key)
		return err
	})
}

// R2MigrationKey journals a server-generated destination before sending bytes.
func (d *DB) R2MigrationKey(ctx context.Context, source, proposed string) (string, bool, error) {
	var key string
	var uploaded bool
	err := d.Write(ctx, func(q *Queries) error {
		if _, err := q.q.ExecContext(ctx, `INSERT OR IGNORE INTO r2_migrations(source,key) VALUES(?,?)`, source, proposed); err != nil {
			return err
		}
		return q.q.QueryRowContext(ctx, `SELECT key,uploaded FROM r2_migrations WHERE source=?`, source).Scan(&key, &uploaded)
	})
	return key, uploaded, err
}

func (d *DB) R2MigrationUploaded(ctx context.Context, source string) error {
	_, err := d.W.ExecContext(ctx, `UPDATE r2_migrations SET uploaded=1 WHERE source=?`, source)
	return err
}

// Reconciliation never lowers the reservation total: uncertain writes, crashes
// and concurrent puts cannot create headroom. Untracked bytes are conservative.
func (d *DB) ReconcileR2(ctx context.Context, now time.Time, total int64) error {
	_, err := d.W.ExecContext(ctx, `UPDATE r2_budget SET bytes=max(bytes,?),reconciled_at=? WHERE id=1`, total, now.Unix())
	return err
}

// InvalidateR2 closes uploads before a scan so a failed scan cannot reuse freshness.
func (d *DB) InvalidateR2(ctx context.Context) error {
	_, err := d.W.ExecContext(ctx, `UPDATE r2_budget SET reconciled_at=0 WHERE id=1`)
	return err
}
