package store

import "context"

// DriveGoogleAccount is the member Google account the dashboard stores files as, plus the
// root folder chosen in Settings. Connected reports whether a refresh token is held.
type DriveGoogleAccount struct {
	Subject, Email, SealedRefresh, SealedAccess string
	ExpiresAt                                   int64
	ConnectedBy                                 *string
	ConnectedAt                                 int64
	RevokedAt                                   *int64
	RootFolderID                                string
}

// Connected is true while a refresh token is held and Google has not refused it.
func (a *DriveGoogleAccount) Connected() bool { return a.SealedRefresh != "" && a.RevokedAt == nil }

const driveAccountCols = `subject,email,sealed_refresh,sealed_access,expires_at,connected_by,connected_at,revoked_at,root_folder_id`

// DriveGoogleAccount returns the row, or ErrNotFound when nothing was ever saved.
func (q *Queries) DriveGoogleAccount(ctx context.Context) (*DriveGoogleAccount, error) {
	var a DriveGoogleAccount
	err := q.q.QueryRowContext(ctx, `SELECT `+driveAccountCols+` FROM drive_google_account WHERE id=1`).
		Scan(&a.Subject, &a.Email, &a.SealedRefresh, &a.SealedAccess, &a.ExpiresAt, &a.ConnectedBy, &a.ConnectedAt, &a.RevokedAt, &a.RootFolderID)
	return &a, notFound(err)
}

// ConnectDriveGoogle stores a fresh grant and clears any revocation; the root folder is kept.
func (q *Queries) ConnectDriveGoogle(ctx context.Context, a DriveGoogleAccount) error {
	_, err := q.q.ExecContext(ctx, `INSERT INTO drive_google_account(id,subject,email,sealed_refresh,sealed_access,expires_at,connected_by,connected_at,revoked_at)
		VALUES(1,?,?,?,?,?,?,?,NULL) ON CONFLICT(id) DO UPDATE SET subject=excluded.subject,email=excluded.email,sealed_refresh=excluded.sealed_refresh,
		sealed_access=excluded.sealed_access,expires_at=excluded.expires_at,connected_by=excluded.connected_by,connected_at=excluded.connected_at,revoked_at=NULL`,
		a.Subject, a.Email, a.SealedRefresh, a.SealedAccess, a.ExpiresAt, a.ConnectedBy, a.ConnectedAt)
	return err
}

// SaveDriveAccess records a refreshed access token (and a rotated refresh token when Google
// sent one) only while the same grant is connected, so a refresh that finishes after a
// disconnect or reconnect cannot bring the old grant back. It reports whether it saved.
func (q *Queries) SaveDriveAccess(ctx context.Context, subject string, connectedAt int64, sealedAccess string, expiresAt int64, sealedRefresh string) (bool, error) {
	res, err := q.q.ExecContext(ctx, `UPDATE drive_google_account SET sealed_access=?,expires_at=?,sealed_refresh=CASE WHEN ?<>'' THEN ? ELSE sealed_refresh END
		WHERE id=1 AND subject=? AND connected_at=? AND sealed_refresh<>'' AND revoked_at IS NULL`, sealedAccess, expiresAt, sealedRefresh, sealedRefresh, subject, connectedAt)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// MarkDriveGoogleRevoked drops the tokens of the grant Google refused, keeping the email so
// Settings can say which account to reconnect. It reports whether that grant was still current.
func (q *Queries) MarkDriveGoogleRevoked(ctx context.Context, subject string, connectedAt, at int64) (bool, error) {
	res, err := q.q.ExecContext(ctx, `UPDATE drive_google_account SET sealed_refresh='',sealed_access='',expires_at=0,revoked_at=?
		WHERE id=1 AND subject=? AND connected_at=? AND revoked_at IS NULL`, at, subject, connectedAt)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// DisconnectDriveGoogle forgets the account and its tokens; the root folder is kept.
func (q *Queries) DisconnectDriveGoogle(ctx context.Context) error {
	_, err := q.q.ExecContext(ctx, `UPDATE drive_google_account SET subject='',email='',sealed_refresh='',sealed_access='',expires_at=0,connected_by=NULL,connected_at=0,revoked_at=NULL WHERE id=1`)
	return err
}

// SetDriveRoot stores the root folder chosen in Settings.
func (q *Queries) SetDriveRoot(ctx context.Context, folderID string) error {
	_, err := q.q.ExecContext(ctx, `INSERT INTO drive_google_account(id,root_folder_id) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET root_folder_id=excluded.root_folder_id`, folderID)
	return err
}
