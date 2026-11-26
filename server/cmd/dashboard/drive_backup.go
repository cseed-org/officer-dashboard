package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"officerdashboard/internal/calendar"
	"officerdashboard/internal/drivebackup"
	"officerdashboard/internal/files"
	"officerdashboard/internal/store"
)

// driveBackupCmd restores or lists the encrypted database snapshots the uploader keeps in
// Google Drive, e.g.:
//
//	docker compose run --rm dashboard drive-backup list
//	docker compose run --rm dashboard drive-backup restore --latest --output /data/restore.db.gz.age
//
// It writes ciphertext only; decryption happens offline (backup.py restore --input).
func driveBackupCmd(args []string, getenv func(string) string, stdout io.Writer) error {
	cmd := "restore"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	if cmd != "list" && cmd != "restore" {
		return fmt.Errorf("unknown drive-backup command %q (list or restore)", cmd)
	}
	fs := flag.NewFlagSet("drive-backup", flag.ContinueOnError)
	kind := fs.String("kind", "daily", "which copies: daily or monthly")
	name := fs.String("name", "", "exact Drive file name to restore (default: the newest)")
	latest := fs.Bool("latest", false, "restore the newest copy")
	output := fs.String("output", "", "new file for the verified ciphertext; never overwritten")
	s, err := loadSettings(fs, args, getenv)
	if err != nil {
		return err
	}
	if cmd == "restore" {
		if *name == "" && !*latest {
			return errors.New("pass --latest or --name <file>; drive-backup list shows the names")
		}
		if *output == "" {
			return errors.New("restore needs --output")
		}
	}
	db, err := openDB(s.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	drive, err := connectDrive(ctx, s, db, nil)
	if err != nil {
		return err
	}
	switch cmd {
	case "list":
		kinds := []string{*kind}
		if *kind == "all" {
			kinds = []string{"daily", "monthly"}
		}
		for _, k := range kinds {
			copies, err := drivebackup.List(ctx, drive, k)
			if err != nil {
				return err
			}
			for _, c := range copies {
				fmt.Fprintf(stdout, "%s\t%s\t%d bytes\tsha256 %s\n", k, c.Entry.Name, c.Entry.Size, c.Entry.AppProperties["sha256"])
			}
		}
		return nil
	default:
		b, err := drivebackup.Restore(ctx, drive, drivebackup.RestoreOptions{
			Name: *name, Latest: *latest, Kind: *kind, Output: *output,
		})
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "restored %s (%d bytes, sha256 verified) to %s\n", b.Entry.Name, b.Entry.Size, *output)
		fmt.Fprintln(stdout, "still ciphertext: decrypt offline, e.g. backup.py restore --input <file> --identity <age identity> --output <new db>")
		return nil
	}
}

// driveAccount is the stored Drive sign-in (PR A's drive_google_account, written when an
// owner connects Google Drive in Settings). A row with an empty sealed refresh token is
// not connected.
type driveAccount struct {
	Subject, Email, SealedRefresh, SealedAccess string
	ExpiresAt                                   int64
	RootFolderID                                string
}

// readDriveAccount returns the stored account, or nil when the table or a connected row is
// absent (older databases, or Drive never connected).
func readDriveAccount(ctx context.Context, db *store.DB) (*driveAccount, error) {
	var n int
	if err := db.R.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='drive_google_account'`).Scan(&n); err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, nil
	}
	var a driveAccount
	err := db.R.QueryRowContext(ctx, `SELECT subject,email,sealed_refresh,sealed_access,expires_at,root_folder_id FROM drive_google_account WHERE id=1`).
		Scan(&a.Subject, &a.Email, &a.SealedRefresh, &a.SealedAccess, &a.ExpiresAt, &a.RootFolderID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if a.SealedRefresh == "" {
		return nil, nil
	}
	return &a, nil
}

// connectDrive builds the Drive client for drive-backup: the member Google account an
// owner connected in Settings when present, otherwise the service-account credentials,
// matching the app's storage selection. client is nil in production (default HTTP).
func connectDrive(ctx context.Context, s settings, db *store.DB, client files.Doer) (*files.Drive, error) {
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	account, err := readDriveAccount(ctx, db)
	if err != nil {
		return nil, err
	}
	if account != nil {
		if s.GoogleClientID == "" || s.GoogleSecret == "" {
			return nil, errors.New("a Google account is connected for Drive but GOOGLE_OAUTH_CLIENT_ID/GOOGLE_OAUTH_CLIENT_SECRET are not set")
		}
		root := s.DriveRoot // the environment wins when set, as in the app
		if root == "" {
			root = account.RootFolderID
		}
		if root == "" {
			return nil, errors.New("DRIVE_ROOT_FOLDER_ID is not set and no root folder was chosen in Settings")
		}
		sealer := calendar.NewSealer(s.GoogleSecret)
		src := &files.OAuthRefreshSource{
			ClientID: s.GoogleClientID, ClientSecret: s.GoogleSecret,
			RefreshToken: func(ctx context.Context) (string, error) {
				a, err := readDriveAccount(ctx, db)
				if err != nil || a == nil {
					return "", err
				}
				return sealer.Open(a.SealedRefresh)
			},
			Saved: func(ctx context.Context, t files.OAuthToken) error {
				return db.Write(ctx, func(w *store.Queries) error {
					return saveDriveToken(ctx, w, sealer, t)
				})
			},
			Revoked: func(ctx context.Context) {
				_ = db.Write(ctx, func(w *store.Queries) error {
					return markDriveDisconnected(ctx, w)
				})
			},
			HTTP: client,
		}
		return files.NewDriveWithTokens(src, root, client), nil
	}
	if s.ServiceAccountJSON != "" && s.DriveRoot != "" {
		return files.NewDrive(s.ServiceAccountJSON, s.DriveRoot, client)
	}
	return nil, files.ErrNotConnected
}

// saveDriveToken persists a minted access token (and a rotated refresh token), sealed like
// the org calendar's, so later runs reuse it.
func saveDriveToken(ctx context.Context, w *store.Queries, sealer *calendar.Sealer, t files.OAuthToken) error {
	if t.Refresh != "" {
		return w.Exec(ctx, `UPDATE drive_google_account SET sealed_access=?, expires_at=?, sealed_refresh=? WHERE id=1`,
			sealer.Seal(t.Access), t.Expiry.UnixMilli(), sealer.Seal(t.Refresh))
	}
	return w.Exec(ctx, `UPDATE drive_google_account SET sealed_access=?, expires_at=? WHERE id=1`,
		sealer.Seal(t.Access), t.Expiry.UnixMilli())
}

// markDriveDisconnected records that Google refused the refresh token; an owner must
// reconnect in Settings before Drive works again.
func markDriveDisconnected(ctx context.Context, w *store.Queries) error {
	return w.Exec(ctx, `UPDATE drive_google_account SET sealed_refresh='', sealed_access='', expires_at=0, revoked_at=? WHERE id=1`,
		time.Now().UnixMilli())
}
