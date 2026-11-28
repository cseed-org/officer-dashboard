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
//   - A *hostfiles.StoredAsideError (a file was there, so the new value went to
//     secrets/admin/<name>.<time>) becomes a *cfprovision.NotInPlaceError: the value is
//     safe, but the install still reads the old file, so the engine stops the run. The
//     new value is the one Cloudflare now holds for the resource this run created or
//     fetched, so the existing file is the older one; a note beside the set-aside file
//     records that resource for later runs.
//   - A client ID that cannot be written while its client secret was kept becomes a
//     *cfprovision.IncompleteSecretError: the secret is not lost, and the ID is still in
//     the dashboard.
//   - Has reports whether the secret's main file exists, so a re-run fetches a tunnel
//     token Cloudflare still has. It refuses while a set-aside copy of any of the
//     secret's files is there, and records any other file of the secret that is missing
//     (the client ID): the command reports it and exits 1 until it is there.
type hostSecretSink struct {
	host *hostfiles.HostSink
	// servicesName is the services token's name in Cloudflare, for the copy steps.
	servicesName string
	// kept are the report lines: where each secret is, paths only.
	kept []string
	// missing are files a kept secret still lacks, each with the step that adds it.
	missing []string
	// holds is set for a secret whose main file exists (found by Has, or stored in place).
	holds map[cfprovision.SecretName]bool
}

func newHostSecretSink(root string, owner hostfiles.Owner, servicesName string) *hostSecretSink {
	return &hostSecretSink{host: &hostfiles.HostSink{Root: root, Owner: owner}, servicesName: servicesName}
}

// resource names what a secret belongs to, as the notes and messages say it.
func (s *hostSecretSink) resource(sec cfprovision.Secret) string {
	if sec.Name == cfprovision.SecretTunnelToken {
		return "tunnel " + sec.ResourceID
	}
	return fmt.Sprintf("service token %s (%s)", sec.ResourceID, s.servicesName)
}

// putInPlace is the command, run in the deploy directory, that moves a set-aside file
// over the main one with the main file's owner, and removes its note.
func putInPlace(aside, main string) string {
	_, owner := hostSecretOwner(main)
	cmd := "mv " + aside + " " + main
	if owner != hostfiles.Root {
		cmd += fmt.Sprintf(" && chown %d:%d %s", owner.UID, owner.GID, main)
	}
	return cmd + " && rm -f " + aside + ".about"
}

func hostSecretOwner(main string) (string, hostfiles.IDs) {
	for _, files := range engineSecretFiles {
		for _, f := range files {
			if rel, owner := hostfiles.SecretPath(f.name); rel == main {
				return f.name, owner
			}
		}
	}
	return "", hostfiles.Root
}

// asideGuidance explains a set-aside file: whose value it holds, that the existing file
// is older, and how to put the new one in place.
func asideGuidance(label, resource, aside, main string) string {
	if resource == "" {
		resource = "the resource setup created or fetched when it saved it (not recorded)"
	}
	return fmt.Sprintf("%s holds the %s Cloudflare now holds for %s; setup saved it there because %s already existed, so %s is older. "+
		"Put the new one in place, from the deploy directory: %s", aside, label, resource, main, main, putInPlace(aside, main))
}

func (s *hostSecretSink) copyIDStep(rel string) string {
	return fmt.Sprintf("copy the Client ID from Zero Trust → Access controls → Service credentials → Service Tokens → %s into %s in the deploy directory, "+
		"mode 0400, owner root: (umask 377; cat > %s), paste it, then press Ctrl-D", s.servicesName, rel, rel)
}

func (s *hostSecretSink) Store(_ context.Context, sec cfprovision.Secret) error {
	files, ok := engineSecretFiles[sec.Name]
	if !ok {
		return fmt.Errorf("setup has no host file for the %s", sec.Name)
	}
	var aside []string
	for i, f := range files {
		rel, owner := hostfiles.SecretPath(f.name)
		err := s.host.StoreSecret(f.name, f.value(sec))
		var sa *hostfiles.StoredAsideError
		switch {
		case errors.As(err, &sa):
			got := s.rel(sa.Path)
			_ = s.host.NoteAside(sa.Path, s.resource(sec)) // informational: the value itself is kept
			aside = append(aside, asideGuidance(f.label, s.resource(sec), got, rel))
			s.note(fmt.Sprintf("%s: %s (set aside: %s is older and still the one in use)", f.label, got, rel))
		case err != nil && i == 0:
			return err // nothing kept: the engine reports the secret as lost
		case err != nil:
			// The client secret is kept; only the client ID, which the dashboard still
			// shows, is missing. The secret is not lost, so this is not a sink failure.
			s.miss(rel)
			// HostSink's wrapper says "keep the value and retry", which the admin cannot do
			// here; its cause (path and reason, never the value) is what helps.
			cause := err
			if inner := errors.Unwrap(err); inner != nil {
				cause = inner
			}
			detail := fmt.Sprintf("the %s could not be written (%v). The %s is in %s; %s",
				f.label, cause, files[0].label, mustRel(files[0].name), s.copyIDStep(rel))
			if len(aside) > 0 {
				return &cfprovision.NotInPlaceError{Detail: strings.Join(append(aside, detail), ". ")}
			}
			return &cfprovision.IncompleteSecretError{Detail: detail}
		default:
			s.note(fmt.Sprintf("%s: %s (owner %d:%d, mode 0400)", f.label, rel, owner.UID, owner.GID))
		}
	}
	if len(aside) > 0 {
		return &cfprovision.NotInPlaceError{Detail: strings.Join(aside, ". ")}
	}
	s.hold(sec.Name)
	return nil
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
			var why []string
			for _, a := range found {
				why = append(why, asideGuidance(f.label, hostfiles.AsideNote(s.host.Root, a), a, rel))
			}
			return false, errors.New(strings.Join(why, ". "))
		}
	}
	main := mustRel(files[0].name)
	switch found, err := s.regular(main); {
	case err != nil:
		return false, err
	case !found:
		return false, nil
	}
	for _, f := range files[1:] {
		rel := mustRel(f.name)
		found, err := s.regular(rel)
		if err != nil {
			return false, err
		}
		if !found {
			s.miss(rel)
		}
	}
	s.note(fmt.Sprintf("%s: %s (already on the host, left as it was)", files[0].label, main))
	s.hold(name)
	return true, nil
}

// regular reports whether rel exists as a regular file; anything else in its place is
// an error, since setup neither replaces it nor reads through it.
func (s *hostSecretSink) regular(rel string) (bool, error) {
	info, err := os.Lstat(filepath.Join(s.host.Root, rel))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("%s: %w", rel, unwrapPath(err))
	case !info.Mode().IsRegular():
		return false, fmt.Errorf("%s exists and is not a regular file; setup leaves it alone, and will not continue until it is a file or gone", rel)
	}
	return true, nil
}

// miss records a file a kept secret lacks, with the step that adds it. Only the client
// ID can be missing this way.
func (s *hostSecretSink) miss(rel string) {
	line := fmt.Sprintf("%s. The services token's client secret is kept, but its client ID is not: %s. Setup exits 1 until the file is there", rel, s.copyIDStep(rel))
	for _, l := range s.missing {
		if l == line {
			return
		}
	}
	s.missing = append(s.missing, line)
}

func (s *hostSecretSink) note(line string) {
	for _, l := range s.kept {
		if l == line {
			return
		}
	}
	s.kept = append(s.kept, line)
}

func (s *hostSecretSink) hold(name cfprovision.SecretName) {
	if s.holds == nil {
		s.holds = map[cfprovision.SecretName]bool{}
	}
	s.holds[name] = true
}

func (s *hostSecretSink) rel(path string) string {
	if r, err := filepath.Rel(s.host.Root, path); err == nil {
		return filepath.ToSlash(r)
	}
	return path
}

func mustRel(name string) string {
	rel, _ := hostfiles.SecretPath(name)
	return rel
}
