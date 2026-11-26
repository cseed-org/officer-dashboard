// Package drivebackup uploads the encrypted daily database snapshots the backup sidecar
// publishes in the data volume's drive-outbox to the shared Google Drive
// (Backups/daily/<date>.db.gz.age, plus a first-of-month copy in Backups/monthly), verifies
// each upload against Drive's metadata, and prunes old copies. The sidecar encrypts; the
// dashboard holds the Drive sign-in, so the uploader is the only writer with Drive access.
package drivebackup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"officerdashboard/internal/files"
)

// Retention: how many copies stay in Drive after a verified upload.
const (
	DailyKeep   = 30
	MonthlyKeep = 12
)

// stuckAfter is how long a snapshot may wait in the outbox before uploads are considered
// stuck and an alert goes out. The sidecar keeps three daily files (oldest about two days,
// evicted by the third publish after it), so the alert must fire well inside that window.
const stuckAfter = 36 * time.Hour

// Manifest is the sidecar's description of one published ciphertext (dashboard-<day>.json).
type Manifest struct {
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	Created string `json:"created"`
	Kind    string `json:"kind"`
}

// Store is the part of *files.Drive the uploader and restore need.
type Store interface {
	EnsureFolder(ctx context.Context, path []string) (string, error)
	List(ctx context.Context, folderID string) ([]files.Entry, error)
	UploadStream(ctx context.Context, folder []string, name, mimeType string, r io.Reader, size int64, appProps map[string]string) (files.Entry, error)
	Download(ctx context.Context, id string) (io.ReadCloser, error)
	DeleteByID(ctx context.Context, id string) error
}

// OutboxDir is the handoff directory below the database file, shared with the backup
// sidecar over the data volume.
func OutboxDir(dbPath string) string {
	return filepath.Join(filepath.Dir(dbPath), "drive-outbox")
}

// Service runs the hourly upload pass.
type Service struct {
	// Drive resolves the Drive store for one pass ((*api.Server).Drive in production); an
	// error (files.ErrNotConnected, files.ErrReconnect, or a 503) means Drive is not
	// connected and the pass only watches for stuck snapshots.
	Drive func(ctx context.Context) (Store, error)
	// Outbox is the directory the sidecar publishes to (OutboxDir).
	Outbox string
	// Alert delivers an operator alert ((*notify.Alerter).Alert in production, already
	// rate-limited per key); nil logs only.
	Alert func(ctx context.Context, key, message string)
	Log   *slog.Logger
	Now   func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// alert logs and forwards one alert; the production Alerter rate-limits per key.
func (s *Service) alert(ctx context.Context, key, message string) {
	s.log().Warn(message)
	if s.Alert != nil {
		s.Alert(ctx, key, message)
	}
}

var (
	dayNameRE  = regexp.MustCompile(`^dashboard-(\d{4}-\d{2}-\d{2})\.db\.gz\.age$`)
	manifestRE = regexp.MustCompile(`^dashboard-\d{4}-\d{2}-\d{2}\.json$`)
	backupMIME = "application/octet-stream"
)

// pending is one outbox ciphertext with its manifest, waiting for upload.
type pending struct {
	name     string
	path     string
	manifest Manifest
	created  time.Time
}

func (p pending) manifestPath() string {
	return strings.TrimSuffix(p.path, ".db.gz.age") + ".json"
}

// monthName is the Backups/monthly file name for this snapshot's month.
func (p pending) monthName() string {
	day := dayNameRE.FindStringSubmatch(p.name)[1]
	return "dashboard-" + day[:7] + ".db.gz.age"
}

// scan lists the upload-ready outbox files (oldest first) and drops manifests whose
// ciphertext is gone, which a finished upload can leave behind.
func (s *Service) scan() ([]pending, error) {
	entries, err := os.ReadDir(s.Outbox)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []pending
	for _, e := range entries {
		name := e.Name()
		if manifestRE.MatchString(name) {
			base := strings.TrimSuffix(name, ".json")
			if _, err := os.Stat(filepath.Join(s.Outbox, base+".db.gz.age")); errors.Is(err, os.ErrNotExist) {
				_ = os.Remove(filepath.Join(s.Outbox, name))
			}
			continue
		}
		if dayNameRE.FindStringSubmatch(name) == nil {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.Outbox, strings.TrimSuffix(name, ".db.gz.age")+".json"))
		if errors.Is(err, os.ErrNotExist) {
			continue // mid-publish: the sidecar writes the manifest last
		}
		if err != nil {
			return nil, err
		}
		var m Manifest
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("outbox manifest for %s: %w", name, err)
		}
		created, err := time.Parse(time.RFC3339, m.Created)
		if err != nil {
			created = time.Time{}
			if info, statErr := e.Info(); statErr == nil {
				created = info.ModTime()
			}
		}
		out = append(out, pending{name: name, path: filepath.Join(s.Outbox, name), manifest: m, created: created})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}

// RunOnce uploads every waiting snapshot, verifies it, then applies retention. A failure
// alerts (rate-limited) and keeps the outbox files for the next hourly pass.
func (s *Service) RunOnce(ctx context.Context) error {
	waiting, err := s.scan()
	if err != nil {
		s.alert(ctx, "drive-backup-scan", "Google Drive backups: cannot read the outbox: "+err.Error())
		return err
	}
	for _, p := range waiting {
		if !p.created.IsZero() && s.now().Sub(p.created) > stuckAfter {
			s.alert(ctx, "drive-backup-stuck", "Google Drive backups are stuck: "+p.name+" has waited over 36 hours for upload")
		}
	}
	var store Store
	if s.Drive != nil {
		st, err := s.Drive(ctx)
		if err != nil {
			if len(waiting) > 0 {
				s.log().Info("Google Drive backups waiting, but Drive is not connected", "count", len(waiting), "error", err)
			}
			return nil
		}
		store = st
	}
	if store == nil {
		if len(waiting) > 0 {
			s.log().Info("Google Drive backups waiting, but Drive is not connected", "count", len(waiting))
		}
		return nil
	}
	uploaded := false
	for _, p := range waiting {
		if err := s.upload(ctx, store, p); err != nil {
			if ctx.Err() != nil {
				return ctx.Err() // shutting down mid-upload is not a failure
			}
			// One bad file must not block the newer snapshots behind it.
			s.alert(ctx, "drive-backup-upload", "Google Drive backup upload failed for "+p.name+"; continuing with newer snapshots: "+err.Error())
			if errors.Is(err, errPermanent) {
				s.reject(p)
			}
			continue
		}
		uploaded = true
	}
	if uploaded {
		// Retention runs only after a verified upload.
		if err := s.retention(ctx, store); err != nil {
			s.alert(ctx, "drive-backup-retention", "Google Drive backup retention failed: "+err.Error())
			return err
		}
	}
	return nil
}

// errPermanent marks upload failures that can never succeed on retry (a corrupt outbox
// pair, or a different file already in Drive under the same name).
var errPermanent = errors.New("will not succeed on retry")

// reject sets a permanently failed pair aside (<name>.rejected) so later passes and the
// operator no longer trip over it. The bytes stay on disk for inspection.
func (s *Service) reject(p pending) {
	for _, path := range []string{p.path, p.manifestPath()} {
		if err := os.Rename(path, path+".rejected"); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.log().Warn("could not set aside the rejected outbox file", "path", path, "error", err)
		}
	}
}

// Hourly runs a pass at startup and then every hour until ctx ends, in the pattern of
// cfsync.Service.Daily.
func (s *Service) Hourly(ctx context.Context) {
	for {
		if err := s.RunOnce(ctx); err != nil && ctx.Err() == nil {
			s.log().Warn("Google Drive backup pass failed; retrying in an hour", "error", err)
		}
		timer := time.NewTimer(time.Hour)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func digest(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, f)
	return hex.EncodeToString(hash.Sum(nil)), size, err
}

func findByName(ctx context.Context, store Store, folderID, name string) (*files.Entry, error) {
	entries, err := store.List(ctx, folderID)
	if err != nil {
		return nil, err
	}
	for i := range entries {
		if entries[i].Name == name && !entries[i].IsFolder() {
			return &entries[i], nil
		}
	}
	return nil, nil
}

// upload moves one snapshot to Backups/daily and, if the month has no copy yet, to
// Backups/monthly, verifies the remote metadata, and removes the local pair. A snapshot
// already in Drive under the same name and sha is recognised and only deleted locally.
func (s *Service) upload(ctx context.Context, store Store, p pending) error {
	sha, size, err := digest(p.path)
	if err != nil {
		return err
	}
	if sha != p.manifest.SHA256 || size != p.manifest.Size {
		return fmt.Errorf("outbox file %s does not match its manifest: %w", p.name, errPermanent)
	}
	dailyID, err := store.EnsureFolder(ctx, []string{"Backups", "daily"})
	if err != nil {
		return err
	}
	existing, err := findByName(ctx, store, dailyID, p.name)
	if err != nil {
		return err
	}
	if existing != nil {
		if existing.AppProperties["sha256"] != sha {
			return fmt.Errorf("Drive already holds a different %s: %w", p.name, errPermanent)
		}
	} else {
		props := map[string]string{"sha256": sha, "size": strconv.FormatInt(size, 10), "kind": p.manifest.Kind}
		if err := s.uploadStream(ctx, store, []string{"Backups", "daily"}, p, size, props); err != nil {
			return err
		}
		if err := verify(ctx, store, dailyID, p.name, size, sha); err != nil {
			return err
		}
	}
	monthlyID, err := store.EnsureFolder(ctx, []string{"Backups", "monthly"})
	if err != nil {
		return err
	}
	month := p.monthName()
	monthly, err := findByName(ctx, store, monthlyID, month)
	if err != nil {
		return err
	}
	if monthly == nil {
		props := map[string]string{"sha256": sha, "size": strconv.FormatInt(size, 10), "kind": "monthly"}
		if err := s.uploadStream(ctx, store, []string{"Backups", "monthly"}, pending{name: month, path: p.path}, size, props); err != nil {
			return err
		}
		if err := verify(ctx, store, monthlyID, month, size, sha); err != nil {
			return err
		}
	}
	// The sidecar's outbox cap may have evicted the pair while it uploaded; that is fine.
	if err := os.Remove(p.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Remove(p.manifestPath()); err != nil {
		s.log().Warn("uploaded snapshot's manifest not removed", "path", p.manifestPath(), "error", err)
	}
	return nil
}

func (s *Service) uploadStream(ctx context.Context, store Store, folder []string, p pending, size int64, props map[string]string) error {
	f, err := os.Open(p.path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = store.UploadStream(ctx, folder, p.name, backupMIME, f, size, props)
	return err
}

// verify confirms Drive shows the uploaded name with the expected size and sha.
func verify(ctx context.Context, store Store, folderID, name string, size int64, sha string) error {
	e, err := findByName(ctx, store, folderID, name)
	if err != nil {
		return err
	}
	if e == nil {
		return fmt.Errorf("Drive upload of %s is not visible after upload", name)
	}
	if e.Size != size || e.AppProperties["sha256"] != sha {
		return fmt.Errorf("Drive upload of %s failed verification (size or sha mismatch)", name)
	}
	return nil
}

// retention keeps the newest DailyKeep daily and MonthlyKeep monthly copies by name and
// trashes the rest. Only dashboard-* files are touched.
func (s *Service) retention(ctx context.Context, store Store) error {
	for _, job := range []struct {
		folder []string
		keep   int
		match  func(string) bool
	}{
		{[]string{"Backups", "daily"}, DailyKeep, dayNameRE.MatchString},
		{[]string{"Backups", "monthly"}, MonthlyKeep, monthNameRE.MatchString},
	} {
		id, err := store.EnsureFolder(ctx, job.folder)
		if err != nil {
			return err
		}
		entries, err := store.List(ctx, id)
		if err != nil {
			return err
		}
		var copies []files.Entry
		for _, e := range entries {
			if !e.IsFolder() && job.match(e.Name) {
				copies = append(copies, e)
			}
		}
		sort.Slice(copies, func(i, j int) bool { return copies[i].Name < copies[j].Name })
		for len(copies) > job.keep {
			if err := store.DeleteByID(ctx, copies[0].ID); err != nil {
				return err
			}
			copies = copies[1:]
		}
	}
	return nil
}
