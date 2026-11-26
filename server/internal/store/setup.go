package store

import (
	"context"
	"database/sql"
)

// SetupHistory contains only successful operation timestamps from optional integrations.
type SetupHistory struct {
	ExportInstalled            bool
	LastExport, LastCloudflare int64
}

func (q *Queries) SetupHistory(ctx context.Context) (SetupHistory, error) {
	var h SetupHistory
	var count int
	if err := q.q.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='drive_export_state'`).Scan(&count); err != nil {
		return h, err
	}
	h.ExportInstalled = count != 0
	if h.ExportInstalled {
		var last sql.NullInt64
		if err := q.q.QueryRowContext(ctx, `SELECT max(exported_at) FROM drive_export_state`).Scan(&last); err != nil {
			return h, err
		}
		h.LastExport = last.Int64
	}
	var last sql.NullInt64
	err := q.q.QueryRowContext(ctx, `SELECT max(last_success) FROM cloudflare_sync_state`).Scan(&last)
	h.LastCloudflare = last.Int64
	return h, err
}
