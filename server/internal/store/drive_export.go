package store

import "context"

// DriveExportState remembers successful uploads. Path includes the Drive root and stable entity ids rather
// than display names so renaming an event does not re-upload its unchanged cover.
type DriveExportState struct {
	Path, DriveFileID, SHA256 string
	ExportedAt                int64
}

func (q *Queries) DriveExportStates(ctx context.Context) (map[string]DriveExportState, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT path,coalesce(drive_file_id,''),sha256,exported_at FROM drive_export_state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]DriveExportState{}
	for rows.Next() {
		var v DriveExportState
		if err := rows.Scan(&v.Path, &v.DriveFileID, &v.SHA256, &v.ExportedAt); err != nil {
			return nil, err
		}
		out[v.Path] = v
	}
	return out, rows.Err()
}

func (q *Queries) SaveDriveExportState(ctx context.Context, v DriveExportState) error {
	return q.Exec(ctx, `INSERT INTO drive_export_state(path,drive_file_id,sha256,exported_at) VALUES(?,?,?,?)
 ON CONFLICT(path) DO UPDATE SET drive_file_id=excluded.drive_file_id,sha256=excluded.sha256,exported_at=excluded.exported_at`, v.Path, v.DriveFileID, v.SHA256, v.ExportedAt)
}

// DeleteDriveExportPrefix forgets only the generated records under this logical
// path. The prefix is compared literally, even if a Drive id contains SQL wildcards.
func (q *Queries) DeleteDriveExportPrefix(ctx context.Context, prefix string) error {
	return q.Exec(ctx, `DELETE FROM drive_export_state WHERE substr(path,1,length(?))=?`, prefix, prefix)
}

func (q *Queries) DeleteDriveExportState(ctx context.Context, path string) error {
	return q.Exec(ctx, `DELETE FROM drive_export_state WHERE path=?`, path)
}
