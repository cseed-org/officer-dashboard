package store

import (
	"context"
	"database/sql"
)

// SetupHistory contains only successful operation timestamps.
type SetupHistory struct {
	LastExport, LastCloudflare int64
}

func (q *Queries) SetupHistory(ctx context.Context) (SetupHistory, error) {
	var h SetupHistory
	var last sql.NullInt64
	if err := q.q.QueryRowContext(ctx, `SELECT max(exported_at) FROM drive_export_state`).Scan(&last); err != nil {
		return h, err
	}
	h.LastExport = last.Int64
	err := q.q.QueryRowContext(ctx, `SELECT max(last_success) FROM cloudflare_sync_state`).Scan(&last)
	h.LastCloudflare = last.Int64
	return h, err
}
