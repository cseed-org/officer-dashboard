CREATE TABLE drive_export_state (
    path TEXT PRIMARY KEY,
    drive_file_id TEXT,
    sha256 TEXT NOT NULL,
    exported_at INTEGER NOT NULL
);
