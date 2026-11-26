package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"officerdashboard/internal/auth"
	"officerdashboard/internal/files"
	"officerdashboard/internal/notify"
	"officerdashboard/internal/store"
	"strings"
	"time"
)

// filesMigrateCmd is operator-invoked only. Source files are retained for rollback.
func filesMigrateCmd(args []string, getenv func(string) string, out io.Writer) error {
	fs := flag.NewFlagSet("files-migrate-r2", flag.ContinueOnError)
	source := fs.String("source", "", "local or drive")
	apply := fs.Bool("apply", false, "copy files and update references (stop the app first); default is read-only dry-run")
	s, err := loadSettings(fs, args, getenv)
	if err != nil {
		return err
	}
	if s.FileStorage != "r2" {
		return errors.New("migration requires FILE_STORAGE=r2")
	}
	ctx := context.Background()
	plan, err := store.PlanFileMigration(ctx, s.DBPath, *source)
	if err != nil {
		return err
	}
	var total int64
	for _, f := range plan {
		total += f.SizeBytes
	}
	fmt.Fprintf(out, "%d files, %d recorded bytes from %s; at least %d Class A writes plus inventory; apply=%t (source files retained)\n", len(plan), total, *source, len(plan), *apply)
	if !*apply || len(plan) == 0 {
		return nil
	}
	if s.WebhookURL == "" {
		return errors.New("R2 migration requires Discord budget alerts")
	}
	var origin files.Store
	switch *source {
	case "local":
		if s.ReceiptsDir == "" {
			return errors.New("local migration requires RECEIPTS_DIR")
		}
		origin = files.Local{Root: s.ReceiptsDir}
	case "drive":
		origin, err = files.NewDrive(s.ServiceAccountJSON, s.DriveRoot, nil)
		if err != nil {
			return err
		}
	}
	db, err := openDB(s.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()
	target, err := files.NewR2(db, s.R2Account, s.R2Bucket, s.R2Access, s.R2Secret)
	if err != nil {
		return err
	}
	notifier := notify.NewDiscord(s.WebhookURL)
	target.Alert = func(ctx context.Context, reason string) bool {
		return notifier.Deliver(ctx, notify.Event{Number: "R2", Type: "storage.budget", Title: "R2 budget alert during file migration", Reason: reason}, s.AppURL)
	}
	if s.R2AnalyticsToken != "" {
		target.Analytics = files.NewR2Analytics(s.R2Account, s.R2Bucket, s.R2AnalyticsToken)
	}
	// A restored ledger is re-seeded before the migration spends anything;
	// while R2 is paused (D-21) the migration does not start.
	if paused, err := target.Reseed(ctx); paused {
		if err != nil {
			return fmt.Errorf("%w (%v)", store.ErrR2Reseeding, err)
		}
		return store.ErrR2Reseeding
	} else if err != nil {
		fmt.Fprintf(out, "R2 usage re-seed: %v\n", err)
	}
	scan, cancel := context.WithTimeout(ctx, 2*time.Minute)
	err = target.Reconcile(scan)
	cancel()
	if err != nil {
		return err
	}
	for _, f := range plan {
		file, err := origin.Get(ctx, f.Key)
		if err != nil {
			return fmt.Errorf("migration %s/%s: %w", f.Table, f.ID, err)
		}
		if file == nil {
			return fmt.Errorf("migration %s/%s: source file missing", f.Table, f.ID)
		}
		identity := auth.SHA256Hex(strings.Join([]string{f.Table, f.ID, f.Storage, f.Key, auth.SHA256Hex(string(file.Body))}, "/"))
		saved, err := target.Put(ctx, files.PutInput{UserID: "operator-migration", MigrationID: identity, MimeType: f.MIME, Body: file.Body})
		if err != nil {
			return fmt.Errorf("migration %s/%s: %w", f.Table, f.ID, err)
		}
		if err := db.FinishFileMigration(ctx, f, saved.Key, saved.SizeBytes); err != nil {
			return err
		}
	}
	fmt.Fprintln(out, "Migration complete; rerunning skips R2 references. Source files retained.")
	return nil
}
