package setup

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"officerdashboard/internal/setup/cfprovision"
	"officerdashboard/internal/setup/hostfiles"
)

// hostSecretFile is one host file an engine secret is kept in.
type hostSecretFile struct {
	name  string // the hostfiles name, which decides the file (hostfiles.SecretPath)
	label string // what the report calls it
	value func(cfprovision.Secret) string
}

// engineSecretFiles maps each secret the apply engine hands out to its host files, in the
// order they are written. The tunnel token goes to secrets/tunnel-token, owned by UID
// 65532, which cloudflared reads (fresh-install.md step 5). The services token is for the
// bots and agents, not for a container, so its client ID and secret go to secrets/admin/
// (root only) for the admin to hand on. The client secret is written first: Cloudflare
// shows it only once, while the client ID stays on the token's dashboard page.
var engineSecretFiles = map[cfprovision.SecretName][]hostSecretFile{
	cfprovision.SecretTunnelToken: {
		{"tunnel-token", "tunnel token", func(s cfprovision.Secret) string { return s.Value }},
	},
	cfprovision.SecretServicesToken: {
		{"services-access-client-secret", "services token client secret", func(s cfprovision.Secret) string { return s.Value }},
		{"services-access-client-id", "services token client ID", func(s cfprovision.Secret) string { return s.ClientID }},
	},
}

// hostSecretSink is the engine's SecretSink (Store, Has) on top of hostfiles.HostSink
// (StoreSecret), so the engine and the host files each keep their own interface:
//
//   - Store writes each file through HostSink.StoreSecret, which returns only after
//     reading the file back, never replaces an existing file (Rotate is never set here),
//     and names no value in its errors.
//   - A *hostfiles.StoredAsideError (an older file was there, so the new value went to
//     secrets/admin/<name>.<time>) becomes a *cfprovision.NotInPlaceError: the value is
//     safe, but the install still reads the old file, so the engine stops the run.
//   - Has reports whether the secret's main file exists, so a re-run fetches a tunnel
//     token Cloudflare still has. It refuses while a set-aside copy of any of the
//     secret's files is there, so no run goes on as if the new value were in use until
//     the admin has chosen which one to keep.
type hostSecretSink struct {
	host *hostfiles.HostSink
	// kept are the report lines: where each secret is, paths only.
	kept []string
	// holds is set for a secret whose main file exists (found by Has, or stored in place).
	holds map[cfprovision.SecretName]bool
}

func newHostSecretSink(root string, owner hostfiles.Owner) *hostSecretSink {
	return &hostSecretSink{host: &hostfiles.HostSink{Root: root, Owner: owner}}
}

func (s *hostSecretSink) Store(_ context.Context, sec cfprovision.Secret) error {
	files, ok := engineSecretFiles[sec.Name]
	if !ok {
		return fmt.Errorf("setup has no host file for the %s", sec.Name)
	}
	var aside, where []string
	for i, f := range files {
		rel, owner := hostfiles.SecretPath(f.name)
		err := s.host.StoreSecret(f.name, f.value(sec))
		var sa *hostfiles.StoredAsideError
		switch {
		case errors.As(err, &sa):
			got := s.rel(sa.Path)
			aside = append(aside, fmt.Sprintf("the new %s is in %s, because %s already existed and is kept as it was", f.label, got, rel))
			where = append(where, "the new "+f.label+" is in "+got)
			s.note(fmt.Sprintf("%s: %s (set aside: %s is older and still the one in use)", f.label, got, rel))
		case err != nil && i == 0:
			return err // nothing kept: the engine reports the secret as lost
		case err != nil:
			// The client secret is kept; only the client ID, which the dashboard still
			// shows, is missing. The secret is not lost, so this is not a sink failure.
			return &cfprovision.NotInPlaceError{Detail: fmt.Sprintf("%s; the %s could not be written (%v): copy it from Zero Trust → Access controls → Service credentials → Service Tokens into %s (mode 0400, owner root)",
				strings.Join(where, "; "), f.label, err, rel)}
		default:
			where = append(where, "the "+f.label+" is in "+rel)
			s.note(fmt.Sprintf("%s: %s (owner %d:%d, mode 0400)", f.label, rel, owner.UID, owner.GID))
		}
	}
	if len(aside) > 0 {
		return &cfprovision.NotInPlaceError{Detail: strings.Join(aside, "; ")}
	}
	s.hold(sec.Name)
	return nil
}

func (s *hostSecretSink) hold(name cfprovision.SecretName) {
	if s.holds == nil {
		s.holds = map[cfprovision.SecretName]bool{}
	}
	s.holds[name] = true
}

func (s *hostSecretSink) note(line string) {
	for _, l := range s.kept {
		if l == line {
			return
		}
	}
	s.kept = append(s.kept, line)
}

func (s *hostSecretSink) Has(_ context.Context, name cfprovision.SecretName) (bool, error) {
	files, ok := engineSecretFiles[name]
	if !ok {
		return false, fmt.Errorf("setup has no host file for the %s", name)
	}
	for _, f := range files {
		found, err := hostfiles.AsideFiles(s.host.Root, f.name)
		if err != nil {
			return false, err
		}
		if len(found) > 0 {
			rel, _ := hostfiles.SecretPath(f.name)
			return false, fmt.Errorf("%s holds a newer %s an earlier run set aside, because %s already existed. Decide which value is current: move it over %s (mode 0400, same owner) or delete it, then run setup again",
				strings.Join(found, " and "), f.label, rel, rel)
		}
	}
	rel, _ := hostfiles.SecretPath(files[0].name)
	info, err := os.Lstat(filepath.Join(s.host.Root, rel))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("%s: %w", rel, unwrapPath(err))
	case !info.Mode().IsRegular():
		return false, fmt.Errorf("%s exists and is not a regular file; setup leaves it alone, and will not continue until it is a file or gone", rel)
	}
	s.note(fmt.Sprintf("%s: %s (already on the host, left as it was)", files[0].label, rel))
	s.hold(name)
	return true, nil
}

func (s *hostSecretSink) rel(path string) string {
	if r, err := filepath.Rel(s.host.Root, path); err == nil {
		return filepath.ToSlash(r)
	}
	return path
}
