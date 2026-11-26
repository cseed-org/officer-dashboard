-- The Google account the dashboard reaches the Shared Drive as (one row, id=1), and the
-- Drive root folder an owner chose in Settings. Tokens are sealed like org_google_account's.
-- A row with an empty sealed_refresh is not connected; revoked_at is set when Google
-- refused the refresh token (invalid_grant) and an owner has to reconnect.
CREATE TABLE drive_google_account (
 id INTEGER PRIMARY KEY CHECK(id=1),
 subject TEXT NOT NULL DEFAULT '', email TEXT NOT NULL DEFAULT '',
 sealed_refresh TEXT NOT NULL DEFAULT '', sealed_access TEXT NOT NULL DEFAULT '', expires_at INTEGER NOT NULL DEFAULT 0,
 connected_by TEXT REFERENCES users(id) ON DELETE SET NULL, connected_at INTEGER NOT NULL DEFAULT 0,
 revoked_at INTEGER,
 root_folder_id TEXT NOT NULL DEFAULT ''
);
