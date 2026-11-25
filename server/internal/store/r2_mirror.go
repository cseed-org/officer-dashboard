package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// The R2 ledger lives in dashboard.db, and update rollback, interrupted-update
// recovery and off-box restore all replace that file wholesale, sometimes with
// a helper from an image that predates R2 and cannot merge the ledger. Cloudflare
// still counts what was spent, so billable operation counts must never go
// backwards. This mirror sits beside the database, is never part of a checkpoint
// or backup, and is rewritten inside the same SQLite write transaction as every
// billable reservation, before the request is sent. Both sides only ever take
// the maximum of the other, so the mirror can run ahead of the database (a
// failed commit) but never behind it.
const R2MirrorName = "r2-usage-ledger.json"

// Mirror days long enough to cover the 32-day rolling window plus clock slack.
const r2MirrorDays = 40

var errR2Mirror = fmt.Errorf("R2 usage mirror %s is unreadable; preserve it and repair it before R2 use resumes: %w", R2MirrorName, ErrR2Budget)

type r2Mirror struct {
	Version   int                 `json:"version"`
	Days      map[string][2]int64 `json:"days"`
	Months    map[string][2]int64 `json:"months"`
	LockUntil string              `json:"lock_until,omitempty"`
}

func (d *DB) r2MirrorPath() string { return filepath.Join(filepath.Dir(d.Path), R2MirrorName) }

func readR2Mirror(path string) (r2Mirror, error) {
	m := r2Mirror{Version: 1, Days: map[string][2]int64{}, Months: map[string][2]int64{}}
	body, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return m, errR2Mirror
	}
	if err := json.Unmarshal(body, &m); err != nil || m.Version != 1 {
		return m, errR2Mirror
	}
	if m.Days == nil {
		m.Days = map[string][2]int64{}
	}
	if m.Months == nil {
		m.Months = map[string][2]int64{}
	}
	return m, nil
}

func writeR2Mirror(path string, m r2Mirror) error {
	body, err := json.Marshal(m)
	if err != nil {
		return err
	}
	next := path + ".next"
	f, err := os.OpenFile(next, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(body); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(next, path); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		return nil // directories cannot be fsynced there; production is Linux
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func maxPair(a, b [2]int64) [2]int64 { return [2]int64{max(a[0], b[0]), max(a[1], b[1])} }

// syncR2Mirror max-merges the mirror and the ledger in both directions inside
// the caller's write transaction, then durably rewrites the mirror. It reports
// whether the ledger gained spending it did not know about (a rollback or
// restore happened while the app was stopped).
func syncR2Mirror(ctx context.Context, q *Queries, path string, now time.Time) (bool, error) {
	m, err := readR2Mirror(path)
	if err != nil {
		return false, err
	}
	cutoff := now.UTC().AddDate(0, 0, -r2MirrorDays).Format("2006-01-02")
	firstMonth := now.UTC().AddDate(0, -1, 0).Format("2006-01")
	for day := range m.Days {
		if day < cutoff {
			delete(m.Days, day)
		}
	}
	for month := range m.Months {
		if month < firstMonth {
			delete(m.Months, month)
		}
	}
	gained := false
	for day, v := range m.Days {
		r, err := q.q.ExecContext(ctx, `INSERT INTO r2_day(day,class_a,class_b) VALUES(?,?,?) ON CONFLICT(day) DO UPDATE SET class_a=max(class_a,excluded.class_a),class_b=max(class_b,excluded.class_b) WHERE class_a<excluded.class_a OR class_b<excluded.class_b`, day, v[0], v[1])
		if err != nil {
			return false, err
		}
		if n, _ := r.RowsAffected(); n > 0 && (v[0] > 0 || v[1] > 0) {
			gained = true
		}
	}
	for month, v := range m.Months {
		r, err := q.q.ExecContext(ctx, `INSERT INTO r2_month(month,class_a,class_b) VALUES(?,?,?) ON CONFLICT(month) DO UPDATE SET class_a=max(class_a,excluded.class_a),class_b=max(class_b,excluded.class_b) WHERE class_a<excluded.class_a OR class_b<excluded.class_b`, month, v[0], v[1])
		if err != nil {
			return false, err
		}
		if n, _ := r.RowsAffected(); n > 0 && (v[0] > 0 || v[1] > 0) {
			gained = true
		}
	}
	if m.LockUntil != "" {
		if _, err := q.q.ExecContext(ctx, `INSERT INTO r2_restore_lock VALUES(1,?) ON CONFLICT(id) DO UPDATE SET until_utc=max(until_utc,excluded.until_utc)`, m.LockUntil); err != nil {
			return false, err
		}
	}
	rows, err := q.q.QueryContext(ctx, `SELECT day,class_a,class_b FROM r2_day WHERE day>=?`, cutoff)
	if err != nil {
		return false, err
	}
	for rows.Next() {
		var day string
		var v [2]int64
		if err := rows.Scan(&day, &v[0], &v[1]); err != nil {
			rows.Close()
			return false, err
		}
		m.Days[day] = maxPair(m.Days[day], v)
	}
	if err := rows.Close(); err != nil {
		return false, err
	}
	rows, err = q.q.QueryContext(ctx, `SELECT month,class_a,class_b FROM r2_month WHERE month>=?`, firstMonth)
	if err != nil {
		return false, err
	}
	for rows.Next() {
		var month string
		var v [2]int64
		if err := rows.Scan(&month, &v[0], &v[1]); err != nil {
			rows.Close()
			return false, err
		}
		m.Months[month] = maxPair(m.Months[month], v)
	}
	if err := rows.Close(); err != nil {
		return false, err
	}
	if err := q.q.QueryRowContext(ctx, `SELECT coalesce((SELECT until_utc FROM r2_restore_lock WHERE id=1),'')`).Scan(&m.LockUntil); err != nil {
		return false, err
	}
	if err := writeR2Mirror(path, m); err != nil {
		return gained, fmt.Errorf("%w: %v", errR2Mirror, err)
	}
	return gained, nil
}
