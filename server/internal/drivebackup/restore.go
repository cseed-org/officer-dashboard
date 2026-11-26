package drivebackup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"officerdashboard/internal/files"
)

// Backup is one restorable copy held in Drive.
type Backup struct {
	Kind  string // "daily" or "monthly"
	Entry files.Entry
}

var monthNameRE = regexp.MustCompile(`^dashboard-\d{4}-\d{2}\.db\.gz\.age$`)

func kindFolder(kind string) ([]string, func(string) bool, error) {
	switch kind {
	case "daily":
		return []string{"Backups", "daily"}, func(name string) bool { return dayNameRE.MatchString(name) }, nil
	case "monthly":
		return []string{"Backups", "monthly"}, monthNameRE.MatchString, nil
	}
	return nil, nil, fmt.Errorf("unknown backup kind %q (daily or monthly)", kind)
}

// List returns the backup copies of one kind ("daily" or "monthly"), oldest first.
func List(ctx context.Context, store Store, kind string) ([]Backup, error) {
	folder, match, err := kindFolder(kind)
	if err != nil {
		return nil, err
	}
	id, err := store.EnsureFolder(ctx, folder)
	if err != nil {
		return nil, err
	}
	entries, err := store.List(ctx, id)
	if err != nil {
		return nil, err
	}
	var out []Backup
	for _, e := range entries {
		if e.IsFolder() || !match(e.Name) {
			continue
		}
		out = append(out, Backup{Kind: kind, Entry: e})
	}
	return out, nil
}

// RestoreOptions selects one Drive copy and where to write it.
type RestoreOptions struct {
	// Name is an exact Drive file name (e.g. dashboard-2026-11-20.db.gz.age). Latest picks
	// the newest copy when Name is empty.
	Name   string
	Latest bool
	// Kind is "daily" (default) or "monthly".
	Kind string
	// Output is the new file to write. It must not exist; the verified download is
	// published atomically, never replacing anything.
	Output string
}

// Restore downloads one copy, verifies it against Drive's stored size and sha256
// (failing closed), and publishes it at opts.Output. The bytes stay ciphertext
// (age over gzip); decryption happens offline with the age identity.
func Restore(ctx context.Context, store Store, opts RestoreOptions) (Backup, error) {
	kind := opts.Kind
	if kind == "" {
		kind = "daily"
	}
	if opts.Output == "" {
		return Backup{}, errors.New("restore needs an output path")
	}
	if opts.Name != "" && opts.Latest {
		return Backup{}, errors.New("pass either a name or --latest, not both")
	}
	if _, err := os.Stat(opts.Output); err == nil {
		return Backup{}, fmt.Errorf("%s already exists; restore never overwrites", opts.Output)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Backup{}, err
	}
	copies, err := List(ctx, store, kind)
	if err != nil {
		return Backup{}, err
	}
	if len(copies) == 0 {
		return Backup{}, fmt.Errorf("no %s backups in Drive", kind)
	}
	pick := -1
	if opts.Name != "" {
		for i := range copies {
			if copies[i].Entry.Name == opts.Name {
				pick = i
			}
		}
		if pick < 0 {
			return Backup{}, fmt.Errorf("no %s backup named %s in Drive", kind, opts.Name)
		}
	} else {
		pick = len(copies) - 1
	}
	b := copies[pick]
	sha := b.Entry.AppProperties["sha256"]
	if sha == "" {
		return Backup{}, fmt.Errorf("Drive backup %s has no sha256 metadata; refusing to trust it", b.Entry.Name)
	}
	rc, err := store.Download(ctx, b.Entry.ID)
	if err != nil {
		return Backup{}, err
	}
	defer rc.Close()
	temp, err := os.CreateTemp(filepath.Dir(opts.Output), ".dashboard-restore-*")
	if err != nil {
		return Backup{}, err
	}
	temporary := temp.Name()
	defer os.Remove(temporary)
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(temp, hash), rc)
	if err == nil {
		err = temp.Sync()
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return Backup{}, err
	}
	if size != b.Entry.Size || hex.EncodeToString(hash.Sum(nil)) != sha {
		return Backup{}, fmt.Errorf("Drive backup %s failed verification (size or sha256 mismatch)", b.Entry.Name)
	}
	// A hard link publishes the verified inode atomically without replacing a destination.
	if err := os.Link(temporary, opts.Output); err != nil {
		return Backup{}, err
	}
	return b, nil
}
