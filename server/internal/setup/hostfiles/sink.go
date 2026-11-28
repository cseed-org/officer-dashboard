package hostfiles

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// SecretSink stores one named secret on the host the moment it is known. The Cloudflare
// apply engine calls it right after the API returns a secret it will never show again
// (a tunnel token, a service token's client secret), before its next API call, so a
// crash cannot lose the value (plan risk 5).
//
// The interface is deliberately one method. The Cloudflare apply engine declares its own
// SecretSink (cfprovision.SecretSink: Store and Has); the setup command adapts HostSink
// to it (server/internal/setup/apply_sink.go), so neither package imports the other.
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
//     written to a new file, secrets/admin/<name>.<UTC time to the nanosecond>[-n],
//     created exclusively, and StoreSecret returns a *StoredAsideError naming it, so
//     the value is neither lost nor silently swapped in.
//   - StoreSecret returns nil or a *StoredAsideError only after reading the file back
//     with exactly the value. Any other error means the value was NOT stored anywhere:
//     the caller must keep it in memory and retry (or show it to the admin).
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
	rel, owner := SecretPath(name)
	spec := SecretSpec{Name: name, Path: rel, Owner: owner}
	dirs := append([]DirSpec(nil), Dirs...)
	if _, known := LookupSecret(name); !known {
		dirs = append(dirs, AdminDir)
	}
	if err := checkSecretValue(spec.Path, []byte(value)); err != nil {
		return err
	}
	notStored := func(err error) error {
		return fmt.Errorf("the new %s was NOT stored (%w); keep the value and retry", name, err)
	}
	if err := s.ensureDirs(dirs); err != nil {
		return notStored(err)
	}
	path := filepath.Join(s.Root, spec.Path)
	kind, err := WriteSecret(path, []byte(value), spec.Owner, s.Owner, s.Rotate)
	if err != nil {
		return notStored(err)
	}
	if kind != Exists {
		if err := sameContent(path, value); err != nil {
			return notStored(err)
		}
		return nil
	}
	aside, err := s.storeAside(name, value)
	if err != nil {
		return fmt.Errorf("%s already exists, and %w", spec.Path, notStored(err))
	}
	return &StoredAsideError{Name: name, Existing: path, Path: aside}
}

// SecretPath is where StoreSecret keeps name, relative to the deploy directory, and who
// owns the file: the documented file for a name in Secrets, else secrets/admin/<name>.
func SecretPath(name string) (string, IDs) {
	if spec, ok := LookupSecret(name); ok {
		return spec.Path, spec.Owner
	}
	return filepath.ToSlash(filepath.Join(AdminDir.Path, name)), Root
}

// asideTime is the UTC time in a set-aside file's name.
const asideTime = "20060102T150405.000000000Z"

var asideSuffix = regexp.MustCompile(`^\.[0-9]{8}T[0-9]{6}\.[0-9]{9}Z(-[0-9]+)?$`)

// AsideFiles lists the files StoreSecret set aside for name under root (paths relative
// to root, oldest first): each holds a value that was not put in place because the
// secret's file already existed. A missing secrets/admin means none.
func AsideFiles(root, name string) ([]string, error) {
	dir := filepath.Join(root, AdminDir.Path)
	if info, err := os.Lstat(dir); err == nil && info.Mode()&fs.ModeSymlink != 0 {
		return nil, symlinkError(dir)
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", AdminDir.Path, pathless(err))
	}
	var out []string
	for _, e := range entries {
		if rest, ok := strings.CutPrefix(e.Name(), name); ok && asideSuffix.MatchString(rest) {
			out = append(out, filepath.ToSlash(filepath.Join(AdminDir.Path, e.Name())))
		}
	}
	return out, nil
}

// storeAside writes value to a new file secrets/admin/<name>.<UTC time>[-n], created
// exclusively (an existing file is never replaced), and returns its path only once the
// file is read back holding exactly value.
func (s *HostSink) storeAside(name, value string) (string, error) {
	if err := s.ensureDirs([]DirSpec{Dirs[0], AdminDir}); err != nil {
		return "", err
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	base := filepath.Join(s.Root, AdminDir.Path, name+"."+now().UTC().Format(asideTime))
	p := &Plan{owner: s.Owner}
	for n := 0; n < 1000; n++ {
		path := base
		if n > 0 {
			path = fmt.Sprintf("%s-%d", base, n)
		}
		err := p.apply(step{kind: stepSecret, path: path, data: []byte(value), create: true, mode: SecretMode, ids: Root})
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if err := sameContent(path, value); err != nil {
			return "", err
		}
		return path, nil
	}
	return "", errors.New("no free file name in secrets/admin")
}

// sameContent reads path back and checks it holds exactly value.
func sameContent(path, value string) error {
	got, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("%s: cannot read it back: %w", path, pathless(err))
	}
	if string(got) != value {
		return fmt.Errorf("%s does not hold the value just written", path)
	}
	return nil
}

// ensureDirs creates the missing directories with their documented modes and owners.
// Existing directories are left as they are; a symlink in their place is refused, so a
// secret is never written outside the deploy directory's layout.
func (s *HostSink) ensureDirs(dirs []DirSpec) error {
	p := &Plan{owner: s.Owner}
	for _, d := range dirs {
		path := filepath.Join(s.Root, d.Path)
		info, err := os.Lstat(path)
		switch {
		case err == nil && info.Mode()&fs.ModeSymlink != 0:
			return symlinkError(path)
		case err == nil && !info.IsDir():
			return fmt.Errorf("%s exists and is not a directory", path)
		case err == nil:
			continue
		case !errors.Is(err, fs.ErrNotExist):
			return fmt.Errorf("%s: %w", d.Path, pathless(err))
		}
		if err := p.apply(step{kind: stepDir, path: path, mode: d.Mode, ids: d.Owner}); err != nil {
			return err
		}
	}
	return nil
}
