package hostfiles

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Secret is one secret file to plan.
type Secret struct {
	Path     string // relative to the plan's root
	Value    []byte // nil when no value was given
	Owner    IDs
	Rotate   bool   // replace an existing file with Value
	Hint     string // shown when the file is missing and no value was given
	Optional bool   // missing with no value is fine: nothing is listed
}

// WriteSecret writes value to path as a secret file: atomically, mode 0400, owned by
// ids, then checked with stat. The value is written exactly as given; no newline is
// added (the dashboard trims surrounding blanks when it reads *_FILE secrets,
// server/cmd/dashboard/secret_files.go).
//
// An existing file is never replaced unless rotate is true; WriteSecret then returns
// Exists and changes nothing. An empty or blank value is refused, since the dashboard
// refuses an empty secret file at startup. Errors name the path, never the value.
func WriteSecret(path string, value []byte, ids IDs, o Owner, rotate bool) (Kind, error) {
	if err := checkSecretValue(path, value); err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	switch {
	case err == nil && !info.Mode().IsRegular():
		return "", fmt.Errorf("%s exists and is not a regular file; left alone", path)
	case err == nil && !rotate:
		return Exists, nil
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return "", fmt.Errorf("%s: %w", path, pathless(err))
	}
	p := &Plan{owner: o}
	s := step{kind: stepSecret, path: path, data: value, create: err != nil, mode: SecretMode, ids: ids}
	if err := p.apply(s); err != nil {
		return "", err
	}
	if s.create {
		return Create, nil
	}
	return Update, nil
}

func checkSecretValue(path string, value []byte) error {
	if len(bytes.TrimSpace(value)) == 0 {
		return fmt.Errorf("%s: the secret value is empty; an empty secret file is never written", path)
	}
	return nil
}

// Secret plans the secret file s.Path: create it when it is missing and a value is
// given, replace it only when s.Rotate is set, and otherwise leave an existing file
// alone (reporting a conflict when its mode or owner differ from what the containers
// need). A missing file with no value is reported as missing, unless optional.
func (pl *Planner) Secret(s Secret) {
	if pl.err != nil {
		return
	}
	path := filepath.Join(pl.root, s.Path)
	want := fmt.Sprintf("mode %04o, owner %d:%d", SecretMode, s.Owner.UID, s.Owner.GID)
	if s.Value != nil {
		if err := checkSecretValue(s.Path, s.Value); err != nil {
			pl.fail(err)
			return
		}
	}
	info, err := os.Lstat(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		pl.fail(fmt.Errorf("%s: %w", s.Path, pathless(err)))
		return
	}
	if err != nil {
		switch {
		case s.Value != nil:
			pl.add(Create, s.Path, "", want)
			pl.plan.steps = append(pl.plan.steps, step{kind: stepSecret, path: path, data: s.Value, create: true, mode: SecretMode, ids: s.Owner})
		case !s.Optional:
			pl.add(Missing, s.Path, "", "no value given; "+s.Hint)
		}
		return
	}
	if !info.Mode().IsRegular() {
		pl.add(Conflict, s.Path, "", "exists and is not a regular file; left alone")
		return
	}
	if s.Rotate && s.Value != nil {
		pl.add(Update, s.Path, "", "rotate: replaced, "+want)
		pl.plan.steps = append(pl.plan.steps, step{kind: stepSecret, path: path, data: s.Value, mode: SecretMode, ids: s.Owner})
		return
	}
	pl.existing(s.Path, path, info, SecretMode, s.Owner, "secret kept")
}

// Dir plans the directory rel with mode and owner. An existing directory is left alone;
// a different mode or owner is reported as a conflict.
func (pl *Planner) Dir(rel string, mode os.FileMode, ids IDs) {
	if pl.err != nil {
		return
	}
	path := filepath.Join(pl.root, rel)
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		pl.add(Create, rel, "", fmt.Sprintf("directory, mode %04o, owner %d:%d", mode, ids.UID, ids.GID))
		pl.plan.steps = append(pl.plan.steps, step{kind: stepDir, path: path, mode: mode, ids: ids})
		return
	}
	if err != nil {
		pl.fail(fmt.Errorf("%s: %w", rel, pathless(err)))
		return
	}
	if !info.IsDir() {
		pl.fail(fmt.Errorf("%s exists and is not a directory", rel))
		return
	}
	pl.existing(rel, path, info, mode, ids, "directory")
}

// existing reports a file or directory that is already there: exists when its mode and
// owner are as wanted, else a conflict naming both. Either way nothing is changed.
func (pl *Planner) existing(rel, path string, info os.FileInfo, mode os.FileMode, ids IDs, what string) {
	got, err := pl.plan.owner.Owner(path)
	if err != nil {
		pl.fail(fmt.Errorf("%s: cannot read its owner: %w", rel, pathless(err)))
		return
	}
	if info.Mode().Perm() == mode && got == ids {
		pl.add(Exists, rel, "", what)
		return
	}
	pl.add(Conflict, rel, "", fmt.Sprintf("%s is mode %04o, owner %d:%d; the containers need %04o, %d:%d; left alone",
		what, info.Mode().Perm(), got.UID, got.GID, mode, ids.UID, ids.GID))
}
