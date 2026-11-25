package store

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
)

type FileMigration struct {
	Table, ID, Storage, Key, MIME string
	SizeBytes                     int64
}

// PlanFileMigration opens mode=ro: a dry run cannot migrate or change the DB.
func PlanFileMigration(ctx context.Context, path, source string) ([]FileMigration, error) {
	if source != "local" && source != "drive" {
		return nil, errors.New("migration source must be local or drive")
	}
	db, err := sql.Open(driverName, dsn(path, url.Values{"mode": {"ro"}}))
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT 'attachments',id,storage,storage_key,mime_type,size_bytes FROM attachments WHERE storage=? UNION ALL SELECT 'design_request_files',id,storage,storage_key,mime_type,size_bytes FROM design_request_files WHERE storage=?`, source, source)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FileMigration
	for rows.Next() {
		var f FileMigration
		if err := rows.Scan(&f.Table, &f.ID, &f.Storage, &f.Key, &f.MIME, &f.SizeBytes); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (d *DB) FinishFileMigration(ctx context.Context, f FileMigration, key string, size int64) error {
	if f.Table != "attachments" && f.Table != "design_request_files" {
		return errors.New("invalid migration table")
	}
	result, err := d.W.ExecContext(ctx, `UPDATE `+f.Table+` SET storage='r2',storage_key=?,web_view_link=NULL,size_bytes=? WHERE id=? AND storage=? AND storage_key=?`, key, size, f.ID, f.Storage, f.Key)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("file changed during migration; stop the app and retry")
	}
	return nil
}
