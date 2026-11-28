package hostfiles

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
)

// Kind says what applying the plan does to one file, directory or env key.
type Kind string

const (
	Create   Kind = "create"   // the file or directory does not exist; apply creates it
	Update   Kind = "update"   // the key is missing, commented out, empty or the template's example; apply sets it
	Exists   Kind = "exists"   // already as wanted, or an existing secret: nothing to do
	Conflict Kind = "conflict" // present but different from what setup wants: reported, left alone
	Missing  Kind = "missing"  // needed, but no value was given: a person must supply it
)

// Action is one line of the plan. It never holds a secret value.
type Action struct {
	Kind   Kind
	Path   string // relative to the plan's root
	Key    string // env key, for env file actions
	Detail string
}

// KV is one env key and its wanted value.
type KV struct{ Key, Value string }

// Modes and owners of what setup writes.
const (
	// docs/deploy/fresh-install.md, "5. Prepare the deployment files":
	// "chmod 0600 .env dashboard.env backup.env"
	EnvMode os.FileMode = 0o600
	// "App and backup files need owner UID 10001 and mode 0400; the tunnel token file
	// needs UID 65532 and mode 0400."
	SecretMode os.FileMode = 0o400
)

// Plan is the list of actions and the writes that carry them out. Build it with a
// Planner, show it with WriteText, then Apply it; a dry run stops after WriteText.
type Plan struct {
	Root    string
	Actions []Action
	steps   []step
	owner   Owner
}

type stepKind int

const (
	stepDir stepKind = iota
	stepEnv
	stepSecret
)

type step struct {
	kind   stepKind
	path   string
	data   []byte // env content or secret value; never printed
	orig   []byte // env content read when planning; nil when creating
	create bool
	mode   os.FileMode
	ids    IDs
}

// Changes counts the actions that apply would carry out.
func (p *Plan) Changes() int {
	n := 0
	for _, a := range p.Actions {
		if a.Kind == Create || a.Kind == Update {
			n++
		}
	}
	return n
}

// Count counts actions of one kind.
func (p *Plan) Count(k Kind) int {
	n := 0
	for _, a := range p.Actions {
		if a.Kind == k {
			n++
		}
	}
	return n
}

// WriteText prints the plan, one action per line, then a summary.
func (p *Plan) WriteText(w io.Writer) error {
	var table bytes.Buffer
	tw := tabwriter.NewWriter(&table, 0, 0, 2, ' ', 0)
	for _, a := range p.Actions {
		target := a.Path
		if a.Key != "" {
			target += " " + a.Key
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", a.Kind, target, a.Detail)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	// tabwriter pads a row's last cell when other rows go on; drop that padding.
	for _, line := range strings.SplitAfter(table.String(), "\n") {
		if line == "" {
			continue
		}
		if _, err := io.WriteString(w, strings.TrimRight(line, " \n")+"\n"); err != nil {
			return err
		}
	}
	var parts []string
	for _, k := range []Kind{Create, Update, Exists, Conflict, Missing} {
		if n := p.Count(k); n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, k))
		}
	}
	if p.Changes() == 0 {
		parts = append([]string{"nothing to do"}, parts...)
	}
	_, err := fmt.Fprintf(w, "Host files: %s.\n", strings.Join(parts, ", "))
	return err
}

// Apply carries out the plan's writes in order. Each write is atomic; on an error it
// stops, and running setup again plans only what is still missing. An env file that
// changed since it was planned, or a secret that appeared, is left alone and reported as
// an error.
func (p *Plan) Apply() error {
	for _, s := range p.steps {
		if err := p.apply(s); err != nil {
			return err
		}
	}
	return nil
}

func (p *Plan) apply(s step) error {
	switch s.kind {
	case stepDir:
		// Build the directory under a temporary name and rename it into place, so a failure
		// part way never leaves a directory with the wrong mode or owner behind.
		tmp, err := os.MkdirTemp(filepath.Dir(s.path), "."+filepath.Base(s.path)+".tmp-*")
		if err != nil {
			return fmt.Errorf("%s: cannot create directory: %w", s.path, pathless(err))
		}
		done := false
		defer func() {
			if !done {
				os.Remove(tmp)
			}
		}()
		if err := os.Chmod(tmp, s.mode); err != nil {
			return fmt.Errorf("%s: cannot set mode %04o: %w", s.path, s.mode, pathless(err))
		}
		if err := p.owner.Chown(tmp, s.ids); err != nil {
			return fmt.Errorf("%s: cannot set owner %d:%d: %w", s.path, s.ids.UID, s.ids.GID, pathless(err))
		}
		if _, err := os.Lstat(s.path); !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%s appeared after setup checked it; kept it, run setup again", s.path)
		}
		if err := os.Rename(tmp, s.path); err != nil {
			return fmt.Errorf("%s: cannot create directory: %w", s.path, pathless(err))
		}
		done = true
		if err := syncDir(filepath.Dir(s.path)); err != nil {
			return fmt.Errorf("%s: created, but flushing its parent failed: %w", s.path, pathless(err))
		}
		return p.verify(s.path, s.mode, s.ids)
	case stepEnv:
		if s.create {
			return createAtomic(s.path, s.data, s.mode, nil, p.owner)
		}
		if info, err := os.Lstat(s.path); err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("%s changed after setup read it; run setup again", s.path)
		}
		now, err := os.ReadFile(s.path)
		if err != nil {
			return fmt.Errorf("%s: %w", s.path, pathless(err))
		}
		if !bytes.Equal(now, s.orig) {
			return fmt.Errorf("%s changed after setup read it; run setup again", s.path)
		}
		ids, err := p.owner.Owner(s.path)
		if err != nil {
			return fmt.Errorf("%s: cannot read its owner: %w", s.path, pathless(err))
		}
		// Keep the file's owner and mode (s.mode is the mode it had when planned): rewriting
		// it must not hand it to whoever runs setup or change who can read it.
		var keep *IDs
		if ids != processIDs() {
			keep = &ids
		}
		return WriteFileAtomic(s.path, s.data, s.mode, keep, p.owner)
	case stepSecret:
		var err error
		if s.create {
			err = createAtomic(s.path, s.data, s.mode, &s.ids, p.owner)
		} else {
			err = WriteFileAtomic(s.path, s.data, s.mode, &s.ids, p.owner)
		}
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%s appeared after setup checked it; kept it, run setup again: %w", s.path, fs.ErrExist)
		}
		if err != nil {
			return err
		}
		return p.verify(s.path, s.mode, s.ids)
	}
	return nil
}

// verify checks with stat that path has the mode and owner just set.
func (p *Plan) verify(path string, mode os.FileMode, ids IDs) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("%s: cannot check it: %w", path, pathless(err))
	}
	got, err := p.owner.Owner(path)
	if err != nil {
		return fmt.Errorf("%s: cannot check its owner: %w", path, pathless(err))
	}
	if info.Mode().Perm() != mode || got != ids {
		return fmt.Errorf("%s: is mode %04o owner %d:%d after writing, want %04o %d:%d", path, info.Mode().Perm(), got.UID, got.GID, mode, ids.UID, ids.GID)
	}
	return nil
}

func processIDs() IDs { return IDs{os.Geteuid(), os.Getegid()} }

// Planner builds a Plan by reading the files under Root. Planning writes nothing. The
// first error stops planning and is returned by Plan.
type Planner struct {
	root  string
	plan  Plan
	err   error
	hints map[string]string
}

// NewPlanner plans files under root, reading and changing ownership through o.
func NewPlanner(root string, o Owner) *Planner {
	return &Planner{root: root, plan: Plan{Root: root, owner: o}}
}

// Plan returns the finished plan, or the first planning error.
func (pl *Planner) Plan() (*Plan, error) {
	if pl.err != nil {
		return nil, pl.err
	}
	p := pl.plan
	return &p, nil
}

// Hint is shown beside env key when it is missing: how to supply it.
func (pl *Planner) Hint(key, hint string) {
	if pl.hints == nil {
		pl.hints = map[string]string{}
	}
	pl.hints[key] = hint
}

// Report adds a line to the plan that writes nothing: an Exists, Conflict or Missing
// finding the caller worked out itself.
func (pl *Planner) Report(kind Kind, rel, key, detail string) {
	if kind == Create || kind == Update {
		pl.fail(fmt.Errorf("Report cannot add a %s action", kind))
		return
	}
	pl.add(kind, rel, key, detail)
}

func (pl *Planner) add(kind Kind, rel, key, detail string) {
	pl.plan.Actions = append(pl.plan.Actions, Action{Kind: kind, Path: filepath.ToSlash(rel), Key: key, Detail: detail})
}

func (pl *Planner) fail(err error) {
	if pl.err == nil {
		pl.err = err
	}
}

// Env plans the env file rel. If it does not exist it is created from template (both
// relative to the root; an empty template starts an empty file). Then each value is
// applied under the adopt rule: a key that already has the wanted value is left alone;
// a key that is missing, commented out, empty, or still the template's example value is
// set; a key with any other value is reported as a conflict and left alone.
//
// An existing file keeps its mode and owner. A symlink (or anything but a regular file)
// is reported as a conflict and left alone: replacing it would leave the admin's real
// file unchanged.
//
// Env files hold settings, not secrets (secrets are always *_FILE paths), so the report
// shows values.
func (pl *Planner) Env(rel, template string, values []KV) {
	if pl.err != nil {
		return
	}
	if err := pl.noSymlinkAbove(rel); err != nil {
		pl.fail(err)
		return
	}
	path := filepath.Join(pl.root, rel)
	mode := EnvMode
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			pl.add(Conflict, rel, "", "is a symlink or not a regular file; setup does not write through it, left alone")
			return
		}
		mode = info.Mode().Perm()
	} else if !errors.Is(err, fs.ErrNotExist) {
		pl.fail(fmt.Errorf("%s: %w", rel, pathless(err)))
		return
	}
	var examples *EnvFile
	if template != "" {
		if raw, err := os.ReadFile(filepath.Join(pl.root, template)); err == nil {
			examples = ParseEnv(raw)
		} else if !errors.Is(err, fs.ErrNotExist) {
			pl.fail(fmt.Errorf("%s: %w", template, pathless(err)))
			return
		}
	}
	orig, err := os.ReadFile(path)
	create := errors.Is(err, fs.ErrNotExist)
	if err != nil && !create {
		pl.fail(fmt.Errorf("%s: %w", rel, pathless(err)))
		return
	}
	var f *EnvFile
	if create {
		if template != "" && examples == nil {
			pl.fail(fmt.Errorf("%s does not exist and neither does its template %s", rel, template))
			return
		}
		f = &EnvFile{}
		if examples != nil {
			f = ParseEnv(examples.Bytes())
		}
		detail := fmt.Sprintf("mode %04o", EnvMode)
		if template != "" {
			detail = "from " + template + ", " + detail
		}
		pl.add(Create, rel, "", detail)
	} else {
		f = ParseEnv(orig)
	}
	changed := create
	for _, kv := range values {
		cur, ok := f.Get(kv.Key)
		kept := ok && cur != "" && !isExample(examples, kv.Key, cur)
		switch {
		case kv.Value == "" && kept:
			pl.add(Exists, rel, kv.Key, "")
			continue
		case kv.Value == "":
			pl.add(Missing, rel, kv.Key, strings.TrimSuffix("no value given; "+pl.hints[kv.Key], "; "))
			continue
		case ok && cur == kv.Value:
			pl.add(Exists, rel, kv.Key, "")
			continue
		case kept:
			pl.add(Conflict, rel, kv.Key, fmt.Sprintf("is %q, setup would write %q; kept", cur, kv.Value))
			continue
		}
		if err := f.Set(kv.Key, kv.Value); err != nil {
			pl.fail(fmt.Errorf("%s: %w", rel, err))
			return
		}
		changed = true
		pl.add(Update, rel, kv.Key, "= "+kv.Value)
	}
	if changed {
		pl.plan.steps = append(pl.plan.steps, step{kind: stepEnv, path: path, data: f.Bytes(), orig: orig, create: create, mode: mode})
	}
}

// noSymlinkAbove refuses rel when a directory between the root and rel is a symlink (or
// not a directory): writing below it would land outside the deploy directory's layout.
func (pl *Planner) noSymlinkAbove(rel string) error {
	return noSymlinkAbove(pl.root, rel)
}

func noSymlinkAbove(root, rel string) error {
	dir := filepath.Dir(filepath.Clean(rel))
	if dir == "." {
		return nil
	}
	walked := root
	for _, part := range strings.Split(filepath.ToSlash(dir), "/") {
		walked = filepath.Join(walked, part)
		info, err := os.Lstat(walked)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%s: %w", walked, pathless(err))
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return symlinkError(walked)
		}
		if !info.IsDir() {
			return fmt.Errorf("%s exists and is not a directory", walked)
		}
	}
	return nil
}

func symlinkError(path string) error {
	return fmt.Errorf("%s is a symlink; setup does not write through symlinks. Replace it with a real directory, or set these files up by hand", path)
}

func isExample(examples *EnvFile, key, value string) bool {
	if examples == nil {
		return false
	}
	v, ok := examples.Get(key)
	return ok && v == value
}

// RenderFromTemplate reads an *.example file and sets values in it, keeping every
// comment and example line it does not change.
func RenderFromTemplate(templatePath string, values []KV) (*EnvFile, error) {
	raw, err := os.ReadFile(templatePath)
	if err != nil {
		return nil, fmt.Errorf("template %s: %w", templatePath, pathless(err))
	}
	f := ParseEnv(raw)
	for _, kv := range values {
		if err := f.Set(kv.Key, kv.Value); err != nil {
			return nil, err
		}
	}
	return f, nil
}
