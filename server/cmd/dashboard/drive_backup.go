package main

import (
	"context"
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

// connectDrive builds the Drive the dashboard currently stores files in, the same choice
// api.storeSwitch makes: the member account an owner connected in Settings, else the
// service-account fallback, else ErrNotConnected (never connected) or ErrReconnect (Google
// refused the grant; an owner must reconnect). client is nil in production (default HTTP).
func connectDrive(ctx context.Context, s settings, db *store.DB, client files.Doer) (*files.Drive, error) {
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	a, err := db.Read().DriveGoogleAccount(ctx)
	if errors.Is(err, store.ErrNotFound) {
		a, err = nil, nil
	}
	if err != nil {
		return nil, err
	}
	fallback := func() (*files.Drive, error) {
		if s.ServiceAccountJSON == "" || s.DriveRoot == "" {
			return nil, nil
		}
		return files.NewDrive(s.ServiceAccountJSON, s.DriveRoot, client)
	}
	if a != nil && a.Connected() {
		root := s.DriveRoot // the environment wins when set, as in the app
		if root == "" {
			root = a.RootFolderID
		}
		if root == "" {
			if fb, ferr := fallback(); fb != nil || ferr != nil {
				return fb, ferr
			}
			return nil, errors.New("choose the Google Drive folder for dashboard files in Settings (or set DRIVE_ROOT_FOLDER_ID)")
		}
		src := &cliDriveTokens{db: db, client: client, id: s.GoogleClientID, secret: s.GoogleSecret, subject: a.Subject, connectedAt: a.ConnectedAt}
		return files.NewDriveWithTokens(src, root, client), nil
	}
	if fb, ferr := fallback(); fb != nil || ferr != nil {
		return fb, ferr
	}
	if a != nil && a.RevokedAt != nil {
		return nil, files.ErrReconnect
	}
	return nil, files.ErrNotConnected
}

// cliDriveTokens mirrors api's driveTokens for this offline command: the sealed access
// token while it is fresh, else a refresh-token grant whose result is sealed back into the
// account row, so later runs (and the dashboard) reuse it.
type cliDriveTokens struct {
	db          *store.DB
	client      files.Doer
	id, secret  string
	subject     string
	connectedAt int64
}

func (t *cliDriveTokens) Token(ctx context.Context) (string, time.Time, error) {
	sealer := calendar.NewSealer(t.secret)
	a, err := t.db.Read().DriveGoogleAccount(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return "", time.Time{}, files.ErrNotConnected
	}
	if err != nil {
		return "", time.Time{}, err
	}
	if a.Subject != t.subject || a.ConnectedAt != t.connectedAt {
		return "", time.Time{}, files.ErrNotConnected // disconnected or replaced since this Drive was built
	}
	if a.RevokedAt != nil {
		return "", time.Time{}, files.ErrReconnect
	}
	if !a.Connected() {
		return "", time.Time{}, files.ErrNotConnected
	}
	if a.ExpiresAt > time.Now().UnixMilli()+60_000 {
		if access, err := sealer.Open(a.SealedAccess); err == nil && access != "" {
			return access, time.UnixMilli(a.ExpiresAt), nil
		}
	}
	if t.id == "" || t.secret == "" {
		return "", time.Time{}, files.ErrNotConnected
	}
	src := &files.OAuthRefreshSource{
		ClientID: t.id, ClientSecret: t.secret,
		RefreshToken: func(context.Context) (string, error) { return sealer.Open(a.SealedRefresh) },
		Saved: func(ctx context.Context, tok files.OAuthToken) error {
			refresh := ""
			if tok.Refresh != "" {
				refresh = sealer.Seal(tok.Refresh)
			}
			return t.db.Write(ctx, func(q *store.Queries) error {
				_, err := q.SaveDriveAccess(ctx, t.subject, t.connectedAt, sealer.Seal(tok.Access), tok.Expiry.UnixMilli(), refresh)
				return err
			})
		},
		Revoked: func(ctx context.Context) {
			_ = t.db.Write(context.WithoutCancel(ctx), func(q *store.Queries) error {
				_, err := q.MarkDriveGoogleRevoked(ctx, t.subject, t.connectedAt, time.Now().UnixMilli())
				return err
			})
		},
		HTTP: t.client,
	}
	return src.Token(ctx)
}
