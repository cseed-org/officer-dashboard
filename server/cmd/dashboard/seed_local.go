//go:build !release

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"

	"officerdashboard/internal/api"
	"officerdashboard/internal/seed"
)

// Sample records are available to local builds, never linked into the release image.
func seedCmd(args []string, getenv func(string) string, stdout io.Writer, log *slog.Logger) error {
	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	reset := fs.Bool("reset", false, "delete the database first")
	force := fs.Bool("force", false, "seed a database that holds real data, or -reset one outside a dev build")

	s, err := loadSettings(fs, args, getenv)
	if err != nil {
		return err
	}
	if *reset {
		if _, err := os.Stat(s.DBPath); err == nil && !api.DevBuild && !*force {
			return fmt.Errorf("seed -reset would delete %s, and this is a release build; pass -force if that is really what you want", s.DBPath)
		}
		for _, suffix := range []string{"", "-wal", "-shm"} {
			if err := os.Remove(s.DBPath + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	db, err := openDB(s.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()
	if !*force {
		real, err := seed.HasRealData(context.Background(), db)
		if err != nil {
			return err
		}
		if real {
			return fmt.Errorf("%s already holds users or journal entries the seed did not create; seeding it would post immutable sample entries and close July-September for good. Pass -force to seed it anyway", s.DBPath)
		}
	}
	if err := seed.Apply(context.Background(), db); err != nil {
		return err
	}
	var users, events, requests, lines int
	for q, dst := range map[string]*int{
		`SELECT count(*) FROM users`: &users, `SELECT count(*) FROM events`: &events,
		`SELECT count(*) FROM purchase_requests`: &requests, `SELECT count(*) FROM journal_lines`: &lines,
	} {
		if err := db.R.QueryRow(q).Scan(dst); err != nil {
			return err
		}
	}
	fmt.Fprintf(stdout, "seeded %s: %d users, %d events, %d requests, %d journal lines (FY%d)\n", s.DBPath, users, events, requests, lines, seed.FiscalYear)
	return nil
}
