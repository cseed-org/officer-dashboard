package hostfiles

// Safety properties found untested or broken in the PR #126 review (F-2 to F-6, N-2, N-5).

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// F-2: three values stored aside for one name within one second each get their own file,
// and each file holds the value StoreSecret said it saved there.
func TestSinkStoresAsideWithoutLosingAValue(t *testing.T) {
	dir := t.TempDir()
	now := func() time.Time { return time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC) }
	s := &HostSink{Root: dir, Owner: newFakeOwner(), Now: now}
	if err := s.StoreSecret("tunnel-token", "SENTINEL-original"); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, v := range []string{"SENTINEL-second", "SENTINEL-third", "SENTINEL-fourth"} {
		err := s.StoreSecret("tunnel-token", v)
		var aside *StoredAsideError
		if !errors.As(err, &aside) {
			t.Fatalf("%s: %v", v, err)
		}
		if seen[aside.Path] {
			t.Fatalf("%s reused %s", v, aside.Path)
		}
		seen[aside.Path] = true
		if got := read(t, aside.Path); string(got) != v {
			t.Fatalf("StoreSecret said %s holds the new value, but it holds %q", aside.Path, got)
		}
	}
	if got := read(t, filepath.Join(dir, "secrets/tunnel-token")); string(got) != "SENTINEL-original" {
		t.Fatal("the original was replaced")
	}
}

// F-2: when the value cannot be saved anywhere, StoreSecret fails and does not claim success.
func TestSinkFailsLoudlyWhenTheAsideWriteFails(t *testing.T) {
	dir := t.TempDir()
	o := newFakeOwner()
	s := &HostSink{Root: dir, Owner: o}
	if err := s.StoreSecret("tunnel-token", "first"); err != nil {
		t.Fatal(err)
	}
	o.fail = errors.New("chown refused")
	err := s.StoreSecret("tunnel-token", sentinel)
	var aside *StoredAsideError
	if err == nil || errors.As(err, &aside) || strings.Contains(err.Error(), sentinel) {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(err.Error(), "NOT") {
		t.Fatalf("the error must say the value was not stored: %v", err)
	}
}

// F-3 and mutation G3: updating a key keeps the file's mode and owner.
func TestEnvUpdateKeepsModeAndOwner(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dashboard.env")
	write(t, path, "DISCORD_CLIENT_ID=\n")
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	o := newFakeOwner()
	admin := IDs{1000, 1000}
	if err := o.Chown(path, admin); err != nil {
		t.Fatal(err)
	}
	pl := NewPlanner(dir, o)
	pl.Env("dashboard.env", "", []KV{{"DISCORD_CLIENT_ID", "123456789012345678"}})
	p, err := pl.Plan()
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Apply(); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Lstat(path)
	if info.Mode().Perm() != 0o640 {
		t.Errorf("mode %04o, want the file's own 0640", info.Mode().Perm())
	}
	if ids, _ := o.Owner(path); ids != admin {
		t.Errorf("owner %+v, want the file's own %+v", ids, admin)
	}
	if got, _ := ParseEnv(read(t, path)).Get("DISCORD_CLIENT_ID"); got != "123456789012345678" {
		t.Errorf("value %q", got)
	}
}

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
}

// F-4: a symlinked env file is reported and left alone, not replaced by a regular file.
func TestSymlinkedEnvFileIsLeftAlone(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "real.env"), "A=\n")
	symlinkOrSkip(t, "real.env", filepath.Join(dir, ".env"))
	symlinkOrSkip(t, "/nowhere/dangling.env", filepath.Join(dir, "dashboard.env"))
	pl := NewPlanner(dir, newFakeOwner())
	pl.Env(".env", "", []KV{{"A", "1"}})
	pl.Env("dashboard.env", "", []KV{{"A", "1"}})
	p, err := pl.Plan()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range p.Actions {
		if a.Kind != Conflict || !strings.Contains(a.Detail, "symlink") {
			t.Errorf("action %+v, want a symlink conflict", a)
		}
	}
	if err := p.Apply(); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Lstat(filepath.Join(dir, ".env")); info.Mode()&os.ModeSymlink == 0 {
		t.Fatal(".env was replaced by a regular file")
	}
	if got := string(read(t, filepath.Join(dir, "real.env"))); got != "A=\n" {
		t.Fatalf("target changed: %q", got)
	}
}

// F-4: setup never writes through a symlinked secrets directory, and neither does the sink (N-2).
func TestSymlinkedSecretDirectoriesAreRefused(t *testing.T) {
	for _, link := range []string{"secrets", "secrets/app"} {
		t.Run(link, func(t *testing.T) {
			dir := t.TempDir()
			outside := t.TempDir()
			if link == "secrets/app" {
				if err := os.Mkdir(filepath.Join(dir, "secrets"), 0o711); err != nil {
					t.Fatal(err)
				}
			}
			symlinkOrSkip(t, outside, filepath.Join(dir, link))
			pl := NewPlanner(dir, newFakeOwner())
			for _, d := range Dirs {
				pl.Dir(d.Path, d.Mode, d.Owner)
			}
			pl.Secret(Secret{Path: "secrets/app/discord-client-secret", Value: []byte(sentinel), Owner: AppIDs})
			if _, err := pl.Plan(); err == nil || !strings.Contains(err.Error(), "symlink") {
				t.Fatalf("plan: %v", err)
			}
			sink := &HostSink{Root: dir, Owner: newFakeOwner()}
			if err := sink.StoreSecret("discord-client-secret", sentinel); err == nil || !strings.Contains(err.Error(), "symlink") {
				t.Fatalf("sink: %v", err)
			}
			if entries, _ := os.ReadDir(outside); len(entries) != 0 {
				t.Fatalf("wrote outside the deploy directory: %v", entries)
			}
		})
	}
}

// Mutation G4: WriteSecret refuses a path that is a symlink, even when rotating.
func TestWriteSecretRefusesASymlink(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "target"), "kept")
	symlinkOrSkip(t, "target", filepath.Join(dir, "secret"))
	for _, rotate := range []bool{false, true} {
		if _, err := WriteSecret(filepath.Join(dir, "secret"), []byte("new"), appIDs, newFakeOwner(), rotate); err == nil {
			t.Fatalf("rotate=%v: wrote through a symlink", rotate)
		}
	}
	if got := string(read(t, filepath.Join(dir, "target"))); got != "kept" {
		t.Fatalf("target changed: %q", got)
	}
}

// Mutation G5: createAtomic never replaces a file that appeared meanwhile.
func TestCreateAtomicNeverReplaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	write(t, path, "first")
	err := createAtomic(path, []byte("second"), SecretMode, &appIDs, newFakeOwner())
	if !errors.Is(err, fs.ErrExist) {
		t.Fatalf("err %v, want ErrExist", err)
	}
	if got := string(read(t, path)); got != "first" {
		t.Fatalf("replaced: %q", got)
	}
	if names := listDir(t, dir); len(names) != 1 {
		t.Fatalf("temp file left behind: %v", names)
	}
}

// N-5: a directory whose chown fails is not left behind half-made.
func TestFailedDirectoryLeavesNothing(t *testing.T) {
	dir := t.TempDir()
	o := newFakeOwner()
	pl := NewPlanner(dir, o)
	pl.Dir("secrets", 0o711, Root)
	p, err := pl.Plan()
	if err != nil {
		t.Fatal(err)
	}
	o.fail = errors.New("chown refused")
	if err := p.Apply(); err == nil {
		t.Fatal("expected the chown failure")
	}
	if names := listDir(t, dir); len(names) != 0 {
		t.Fatalf("left behind: %v", names)
	}
}

// Mutation G4: a planned secret whose path is a symlink is a conflict, even with Rotate,
// and applying leaves the link and its target alone.
func TestPlannedSecretSymlinkIsAConflict(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "target"), "kept")
	symlinkOrSkip(t, "target", filepath.Join(dir, "secret"))
	pl := NewPlanner(dir, newFakeOwner())
	pl.Secret(Secret{Path: "secret", Value: []byte("new"), Owner: appIDs, Rotate: true})
	p, err := pl.Plan()
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Actions) != 1 || p.Actions[0].Kind != Conflict {
		t.Fatalf("actions %+v", p.Actions)
	}
	if err := p.Apply(); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Lstat(filepath.Join(dir, "secret")); info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink was replaced")
	}
	if got := string(read(t, filepath.Join(dir, "target"))); got != "kept" {
		t.Fatalf("target changed: %q", got)
	}
}

// Mutation R6: a secret below a symlinked directory is refused even when the plan does
// not list that directory itself.
func TestSecretBelowASymlinkIsRefused(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	symlinkOrSkip(t, outside, filepath.Join(dir, "secrets"))
	pl := NewPlanner(dir, newFakeOwner())
	pl.Secret(Secret{Path: "secrets/app/x", Value: []byte(sentinel), Owner: appIDs})
	if _, err := pl.Plan(); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("plan: %v", err)
	}
	pl = NewPlanner(dir, newFakeOwner())
	pl.Env("secrets/x.env", "", []KV{{"A", "1"}})
	if _, err := pl.Plan(); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("env plan: %v", err)
	}
}

// corruptingOwner damages the file it is asked to chown, standing in for a write that did
// not land intact.
type corruptingOwner struct {
	*fakeOwner
	after int // corrupt from this call on
	calls int
}

func (o *corruptingOwner) Chown(path string, ids IDs) error {
	o.calls++
	if o.calls > o.after {
		if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() {
			os.Chmod(path, 0o600)
			os.WriteFile(path, []byte("damaged"), 0o600)
			os.Chmod(path, info.Mode().Perm())
		}
	}
	return o.fakeOwner.Chown(path, ids)
}

// Mutation R3: StoreSecret reports success only after reading the value back, on the
// main path and the aside path alike.
func TestSinkReadsBackBeforeReportingSuccess(t *testing.T) {
	dir := t.TempDir()
	// Main path: the directories take 3 chowns; the 4th is the secret file.
	o := &corruptingOwner{fakeOwner: newFakeOwner(), after: 3}
	err := (&HostSink{Root: dir, Owner: o}).StoreSecret("tunnel-token", sentinel)
	if err == nil || !strings.Contains(err.Error(), "NOT") {
		t.Fatalf("main path: %v", err)
	}
	// Aside path: the original exists; the admin directory and the aside file follow.
	dir = t.TempDir()
	good := newFakeOwner()
	if err := (&HostSink{Root: dir, Owner: good}).StoreSecret("tunnel-token", "first"); err != nil {
		t.Fatal(err)
	}
	o = &corruptingOwner{fakeOwner: good, after: 1}
	err = (&HostSink{Root: dir, Owner: o}).StoreSecret("tunnel-token", sentinel)
	var aside *StoredAsideError
	if err == nil || errors.As(err, &aside) || !strings.Contains(err.Error(), "NOT") {
		t.Fatalf("aside path: %v", err)
	}
}
