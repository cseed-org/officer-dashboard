package store

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"
)

// Decimal GB and a persistent upper bound on live bytes keep even daily peaks
// below the free GB-month allowance. Ambiguous failures stay reserved until an
// inventory proves the object absent after R2PendingWindow.
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
		// A ledger with history but no mirror beside it came from another
		// volume (a manual restore or a host move): its counters may be stale.
		if _, err := os.Stat(d.r2MirrorPath()); errors.Is(err, fs.ErrNotExist) {
			if _, err := q.q.ExecContext(ctx, `INSERT OR IGNORE INTO r2_reseed(id,requested_at) SELECT 1,? WHERE EXISTS(SELECT 1 FROM r2_day WHERE class_a>0 OR class_b>0) OR EXISTS(SELECT 1 FROM r2_month WHERE class_a>0 OR class_b>0)`, time.Now().Unix()); err != nil {
				return err
			}
		}
		// Recover spending a rollback or restore erased from dashboard.db. If
		// any was missing, the inventory is stale too: close uploads until a scan.
		gained, err := syncR2Mirror(ctx, q, d.r2MirrorPath(), time.Now())
		if errors.Is(err, errR2Mirror) {
			return nil // startup continues; every billable reservation refuses
		}
		if err != nil {
			return err
		}
		if gained {
			_, err = q.q.ExecContext(ctx, `UPDATE r2_budget SET reconciled_at=0 WHERE id=1`)
		}
		return err
	})
}

// Alert claim bits: storage/Class A/Class B reached 70%, then the same metric
// refusing at 90%. Each bit is delivered once per UTC month.
const (
	R2AlertStorageWarn = 1 << iota
	R2AlertClassAWarn
	R2AlertClassBWarn
	R2AlertStorageRefused
	R2AlertClassARefused
	R2AlertClassBRefused
)

// ReserveR2 commits BEFORE the network request. SQLite IMMEDIATE transactions
// serialize callers, including other processes using this same ledger.
// A true result means this caller claimed an undelivered alert (at most one
// claim per 15 minutes). A 90% refusal still claims its alert while refusing.
func (d *DB) ReserveR2(ctx context.Context, now time.Time, class, key, user string, size int64) (bool, error) {
	alert := false
	var refusal error
	err := d.Write(ctx, func(q *Queries) error {
		month := now.UTC().Format("2006-01")
		if class == "put" || class == "list" || class == "get" {
			// D-21: a restored ledger never spends until analytics re-seeds
			// it or the safety cap passes; see r2_reseed.go.
			var pending int
			if err := q.q.QueryRowContext(ctx, `SELECT count(*) FROM r2_reseed WHERE `+r2ReseedUntil+`>?`, now.Unix()).Scan(&pending); err != nil {
				return err
			}
			if pending != 0 {
				return ErrR2Reseeding
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
		// Current UTC day plus 31 preceding days covers any billing period up
		// to 31 days, with an extra day for requests crossing midnight in flight.
		var rollingA, rollingB int64
		if err := q.q.QueryRowContext(ctx, `SELECT coalesce(sum(class_a),0),coalesce(sum(class_b),0) FROM r2_day WHERE day>=?`, now.UTC().AddDate(0, 0, -31).Format("2006-01-02")).Scan(&rollingA, &rollingB); err != nil {
			return err
		}
		a, b := int64(boolInt(class == "put" || class == "list")), int64(boolInt(class == "get"))
		var existing int64
		if class == "put" {
			if err := q.q.QueryRowContext(ctx, `SELECT coalesce((SELECT bytes FROM r2_objects WHERE key=?),0)`, key).Scan(&existing); err != nil {
				return err
			}
			if existing != 0 && existing != size {
				return errors.New("R2 retry content size differs")
			}
		}
		refused := 0
		if a == 1 && (u.ClassA+1 > R2ClassALimit*9/10 || rollingA+1 > R2ClassALimit*9/10) {
			refused |= R2AlertClassARefused | R2AlertClassAWarn
		}
		if b == 1 && (u.ClassB+1 > R2ClassBLimit*9/10 || rollingB+1 > R2ClassBLimit*9/10) {
			refused |= R2AlertClassBRefused | R2AlertClassBWarn
		}
		if class == "put" && u.Bytes+size-existing > R2StorageLimit*9/10 {
			refused |= R2AlertStorageRefused | R2AlertStorageWarn
		}
		if refused != 0 {
			// Commit only the alert claim: no counter moves and no request is sent.
			refusal = ErrR2Budget
			var err error
			alert, err = claimR2Alert(ctx, q, now, month, refused, alerted, attempt)
			return err
		}
		u.ClassA += a
		u.ClassB += b
		if class == "put" {
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
			// reserved_at lets inventory release a reservation whose PUT never
			// reached the bucket, once no in-flight write can still land.
			if _, err := q.q.ExecContext(ctx, `INSERT INTO r2_objects(key,bytes,reserved_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET reserved_at=excluded.reserved_at`, key, size, now.Unix()); err != nil {
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
		if _, err := q.q.ExecContext(ctx, `UPDATE r2_month SET class_a=?,class_b=?,deletes=deletes+? WHERE month=?`, u.ClassA, u.ClassB, boolInt(class == "delete"), month); err != nil {
			return err
		}
		if a+b > 0 {
			// Durable outside dashboard.db before the request can be sent.
			if _, err := syncR2Mirror(ctx, q, d.r2MirrorPath(), now); err != nil {
				return err
			}
		}
		mask := 0
		if u.Bytes >= R2StorageLimit*7/10 {
			mask |= R2AlertStorageWarn
		}
		if u.ClassA >= R2ClassALimit*7/10 || rollingA+a >= R2ClassALimit*7/10 {
			mask |= R2AlertClassAWarn
		}
		if u.ClassB >= R2ClassBLimit*7/10 || rollingB+b >= R2ClassBLimit*7/10 {
			mask |= R2AlertClassBWarn
		}
		var err error
		alert, err = claimR2Alert(ctx, q, now, month, mask, alerted, attempt)
		return err
	})
	if err != nil {
		return false, err
	}
	return alert, refusal
}

// claimR2Alert lets one caller per 15 minutes deliver the thresholds that
// were crossed and not yet acknowledged this month.
func claimR2Alert(ctx context.Context, q *Queries, now time.Time, month string, mask, alerted int, attempt int64) (bool, error) {
	pending := mask &^ alerted
	if pending == 0 || attempt > now.Add(-15*time.Minute).Unix() {
		return false, nil
	}
	_, err := q.q.ExecContext(ctx, `UPDATE r2_month SET alert_claim_mask=?,alert_attempt_at=? WHERE month=?`, pending, now.Unix(), month)
	return err == nil, err
}

// R2AlertClaim reports which thresholds the current claim covers.
func (d *DB) R2AlertClaim(ctx context.Context, now time.Time) (int, error) {
	var mask int
	err := d.R.QueryRowContext(ctx, `SELECT coalesce((SELECT alert_claim_mask FROM r2_month WHERE month=?),0)`, now.UTC().Format("2006-01")).Scan(&mask)
	return mask, err
}

func (d *DB) AckR2Alert(ctx context.Context, now time.Time) error {
	_, err := d.W.ExecContext(ctx, `UPDATE r2_month SET alerted=alerted | alert_claim_mask WHERE month=?`, now.UTC().Format("2006-01"))
	return err
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

// R2PendingWindow bounds how long an unlisted reservation may still be a PUT
// in flight. The client times out after 20 seconds and never retries.
const R2PendingWindow = time.Hour

// ReconcileR2 sets bytes from a complete listing taken while no in-process PUT
// or DELETE is in flight. Unlisted reservations younger than R2PendingWindow
// may be another process's PUT still landing, so they stay counted; older
// unlisted reservations never reached the bucket (or were removed) and are
// released. Listed objects without a reservation are counted via total.
func (d *DB) ReconcileR2(ctx context.Context, now time.Time, total int64, listed map[string]bool) error {
	return d.Write(ctx, func(q *Queries) error {
		rows, err := q.q.QueryContext(ctx, `SELECT key,bytes,reserved_at FROM r2_objects`)
		if err != nil {
			return err
		}
		var pending int64
		var stale []string
		for rows.Next() {
			var key string
			var bytes, reserved int64
			if err := rows.Scan(&key, &bytes, &reserved); err != nil {
				rows.Close()
				return err
			}
			switch {
			case listed[key]:
			case reserved > now.Add(-R2PendingWindow).Unix():
				pending += bytes
			default:
				stale = append(stale, key)
			}
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if err := rows.Err(); err != nil {
			return err
		}
		for _, key := range stale {
			if _, err := q.q.ExecContext(ctx, `DELETE FROM r2_objects WHERE key=?`, key); err != nil {
				return err
			}
			if _, err := q.q.ExecContext(ctx, `UPDATE r2_migrations SET uploaded=0 WHERE key=?`, key); err != nil {
				return err
			}
		}
		_, err = q.q.ExecContext(ctx, `UPDATE r2_budget SET bytes=?,reconciled_at=? WHERE id=1`, total+pending, now.Unix())
		return err
	})
}

// InvalidateR2 closes uploads before a scan so a failed scan cannot reuse freshness.
func (d *DB) InvalidateR2(ctx context.Context) error {
	_, err := d.W.ExecContext(ctx, `UPDATE r2_budget SET reconciled_at=0 WHERE id=1`)
	return err
}
