package hostfiles

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// WriteFileAtomic replaces path with data so that a reader sees the old file or the new
// one, never part of either: it writes a temporary file in the same directory, flushes
// it, sets its mode (and owner, when ids is not nil), renames it over path and flushes
// the directory. On any error the temporary file is removed and path is unchanged.
func WriteFileAtomic(path string, data []byte, mode os.FileMode, ids *IDs, o Owner) error {
	return writeAtomic(path, data, mode, ids, o, true)
}

// createAtomic is WriteFileAtomic that never replaces: the finished temporary file is
// hard-linked to path, which fails if path appeared meanwhile, so a secret another
// process wrote first is kept.
func createAtomic(path string, data []byte, mode os.FileMode, ids *IDs, o Owner) error {
	return writeAtomic(path, data, mode, ids, o, false)
}

func writeAtomic(path string, data []byte, mode os.FileMode, ids *IDs, o Owner, replace bool) (err error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("%s: cannot create a temporary file: %w", path, pathless(err))
	}
	name := tmp.Name()
	done := false
	defer func() {
		if !done {
			tmp.Close()
			os.Remove(name)
		}
	}()
	// Set the final mode before writing, so the content is never readable more widely
	// than the finished file.
	if err := tmp.Chmod(mode); err != nil {
		return fmt.Errorf("%s: cannot set mode %04o: %w", path, mode, pathless(err))
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("%s: write failed: %w", path, pathless(err))
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("%s: flush failed: %w", path, pathless(err))
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("%s: close failed: %w", path, pathless(err))
	}
	if ids != nil {
		if err := o.Chown(name, *ids); err != nil {
			return fmt.Errorf("%s: cannot set owner %d:%d: %w", path, ids.UID, ids.GID, pathless(err))
		}
	}
	if replace {
		if err := os.Rename(name, path); err != nil {
			return fmt.Errorf("%s: rename failed: %w", path, pathless(err))
		}
	} else {
		if err := os.Link(name, path); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return fmt.Errorf("%s: %w", path, fs.ErrExist)
			}
			return fmt.Errorf("%s: cannot create: %w", path, pathless(err))
		}
		os.Remove(name)
	}
	done = true
	if err := syncDir(dir); err != nil {
		return fmt.Errorf("%s: written, but flushing its directory failed: %w", path, pathless(err))
	}
	return nil
}

// pathless drops the path from an *fs.PathError or *os.LinkError, whose paths callers
// already name (and which would otherwise show temporary file names).
func pathless(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return le.Err
	}
	return err
}
