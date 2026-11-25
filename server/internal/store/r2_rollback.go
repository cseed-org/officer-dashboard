package store

import (
	"context"
	"database/sql"
	"net/url"
	"strings"
)

// MergeR2BeforeRollback preserves usage spent by a candidate image before its
// failed health check. Both app processes must be stopped. No app records move.
func MergeR2BeforeRollback(ctx context.Context, live, checkpoint string) error {
	dst, err := sql.Open(driverName, dsn(checkpoint, url.Values{"mode": {"rw"}, "_synchronous": {"FULL"}}))
	if err != nil {
		return err
	}
	defer dst.Close()
	dst.SetMaxOpenConns(1)
	if _, err := dst.ExecContext(ctx, `ATTACH DATABASE ? AS r2_live`, dsn(live, url.Values{"mode": {"ro"}})); err != nil {
		return err
	}
	defer dst.ExecContext(context.Background(), `DETACH DATABASE r2_live`)
	var exists int
	if err := dst.QueryRowContext(ctx, `SELECT count(*) FROM r2_live.sqlite_master WHERE type='table' AND name='r2_day'`).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return nil
	}
	tx, err := dst.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	tables := []string{"r2_budget", "r2_day", "r2_month", "r2_user_day", "r2_objects", "r2_migrations", "r2_restore_lock"}
	for _, table := range tables {
		var schema string
		if err := tx.QueryRowContext(ctx, `SELECT sql FROM r2_live.sqlite_master WHERE type='table' AND name=?`, table).Scan(&schema); err != nil {
			return err
		}
		if !strings.HasPrefix(schema, "CREATE TABLE IF NOT EXISTS ") {
			schema = strings.Replace(schema, "CREATE TABLE ", "CREATE TABLE IF NOT EXISTS ", 1)
		}
		if _, err := tx.ExecContext(ctx, schema); err != nil {
			return err
		}
	}
	for _, query := range []string{
		`INSERT INTO r2_budget SELECT * FROM r2_live.r2_budget WHERE true ON CONFLICT(id) DO UPDATE SET bytes=max(bytes,excluded.bytes),reconciled_at=0`,
		`INSERT INTO r2_day SELECT * FROM r2_live.r2_day WHERE true ON CONFLICT(day) DO UPDATE SET class_a=max(class_a,excluded.class_a),class_b=max(class_b,excluded.class_b)`,
		`INSERT INTO r2_month SELECT * FROM r2_live.r2_month WHERE true ON CONFLICT(month) DO UPDATE SET class_a=max(class_a,excluded.class_a),class_b=max(class_b,excluded.class_b),deletes=max(deletes,excluded.deletes),alerted=0,alert_attempt_at=0,alert_claim_mask=0`,
		`INSERT INTO r2_user_day SELECT * FROM r2_live.r2_user_day WHERE true ON CONFLICT(user_id,day) DO UPDATE SET uploads=max(uploads,excluded.uploads)`,
		`INSERT OR IGNORE INTO r2_objects SELECT * FROM r2_live.r2_objects`,
		`INSERT OR IGNORE INTO r2_migrations SELECT * FROM r2_live.r2_migrations`,
		`INSERT INTO r2_restore_lock SELECT * FROM r2_live.r2_restore_lock WHERE true ON CONFLICT(id) DO UPDATE SET until_utc=max(until_utc,excluded.until_utc)`,
		`UPDATE r2_budget SET bytes=max(bytes,coalesce((SELECT sum(bytes) FROM r2_objects),0)),reconciled_at=0`,
	} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	return tx.Commit()
}
