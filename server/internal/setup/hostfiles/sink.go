package hostfiles

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// SecretSink stores one named secret on the host the moment it is known. The Cloudflare
// apply engine calls it right after the API returns a secret it will never show again
// (a tunnel token, a service token's client secret), before its next API call, so a
// crash cannot lose the value (plan risk 5).
//
// The interface is deliberately one method; the apply engine declares the same shape
// on its side.
type SecretSink interface {
	StoreSecret(name, value string) error
}

// HostSink is the SecretSink backed by the deploy directory at Root.
//
//   - A name in Secrets goes to its documented file (for example "tunnel-token" to
//     secrets/tunnel-token, owned by UID 65532), creating the secrets directories when
//     needed.
//   - Any other name, such as "services-access-client-id" and
//     "services-access-client-secret" for the token bots and agents use, goes to
//     secrets/admin/<name>, owned by root, mode 0400, for the admin to hand on.
//   - An existing file is never replaced unless Rotate is set. The new value is then
//     written to secrets/admin/<name>.<UTC time> instead, and StoreSecret returns a
//     *StoredAsideError naming that file, so the value is neither lost nor silently
//     swapped in.
//
// Names are lower-case letters, digits and dashes. Values must not be empty. Neither
// the value nor any part of it appears in an error.
type HostSink struct {
	Root   string
	Owner  Owner
	Rotate bool
	Now    func() time.Time // for tests; nil means time.Now
}

// StoredAsideError reports a secret written beside an existing one instead of over it.
type StoredAsideError struct {
	Name, Existing, Path string
}

func (e *StoredAsideError) Error() string {
	return fmt.Sprintf("%s already exists, so the new %s was saved to %s instead; replace the old file with it if the new one is the one in use", e.Existing, e.Name, e.Path)
}

var secretName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

func (s *HostSink) StoreSecret(name, value string) error {
	if !secretName.MatchString(name) {
		return fmt.Errorf("invalid secret name %q", name)
	}
	spec, known := LookupSecret(name)
	dirs := append([]DirSpec(nil), Dirs...)
	if !known {
		spec = SecretSpec{Name: name, Path: filepath.ToSlash(filepath.Join(AdminDir.Path, name)), Owner: Root}
		dirs = append(dirs, AdminDir)
	}
	if err := checkSecretValue(spec.Path, []byte(value)); err != nil {
		return err
	}
	if err := s.ensureDirs(dirs); err != nil {
		return err
	}
	path := filepath.Join(s.Root, spec.Path)
	kind, err := WriteSecret(path, []byte(value), spec.Owner, s.Owner, s.Rotate)
	if err != nil || kind != Exists {
		return err
	}
	if err := s.ensureDirs([]DirSpec{Dirs[0], AdminDir}); err != nil {
		return err
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	aside := filepath.Join(s.Root, AdminDir.Path, name+"."+now().UTC().Format("20060102T150405Z"))
	if _, err := WriteSecret(aside, []byte(value), Root, s.Owner, false); err != nil {
		return fmt.Errorf("%s exists and keeping the new value aside failed: %w", spec.Path, err)
	}
	return &StoredAsideError{Name: name, Existing: path, Path: aside}
}

// ensureDirs creates the missing directories with their documented modes and owners.
// Existing directories are left as they are.
func (s *HostSink) ensureDirs(dirs []DirSpec) error {
	p := &Plan{owner: s.Owner}
	for _, d := range dirs {
		path := filepath.Join(s.Root, d.Path)
		if _, err := os.Lstat(path); err == nil {
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%s: %w", d.Path, pathless(err))
		}
		if err := p.apply(step{kind: stepDir, path: path, mode: d.Mode, ids: d.Owner}); err != nil {
			return err
		}
	}
	return nil
}
