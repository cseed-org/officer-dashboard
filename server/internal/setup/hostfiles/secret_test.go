package hostfiles

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sentinel = "SENTINEL-secret-value-do-not-print"

var appIDs = IDs{10001, 10001}

func TestWriteSecretSetsModeOwnerAndExactBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "discord-client-secret")
	o := newFakeOwner()
	kind, err := WriteSecret(path, []byte(sentinel), appIDs, o, false)
	if err != nil || kind != Create {
		t.Fatalf("kind %s err %v", kind, err)
	}
	if got := read(t, path); string(got) != sentinel {
		t.Fatalf("value written as %q (no newline may be added)", got)
	}
	info, _ := os.Lstat(path)
	if info.Mode().Perm() != SecretMode {
		t.Fatalf("mode %04o", info.Mode().Perm())
	}
	if ids, _ := o.Owner(path); ids != appIDs {
		t.Fatalf("owner %+v", ids)
	}
	if len(o.chowns) != 1 {
		t.Fatalf("chowns %v", o.chowns)
	}
	if names := listDir(t, dir); len(names) != 1 {
		t.Fatalf("temp file left behind: %v", names)
	}
}

func TestWriteSecretNeverOverwritesUnlessRotating(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bearer")
	write(t, path, "admin's value")
	o := newFakeOwner()
	kind, err := WriteSecret(path, []byte(sentinel), appIDs, o, false)
	if err != nil || kind != Exists {
		t.Fatalf("kind %s err %v", kind, err)
	}
	if got := read(t, path); string(got) != "admin's value" {
		t.Fatal("an existing secret was overwritten")
	}
	kind, err = WriteSecret(path, []byte("rotated"), appIDs, o, true)
	if err != nil || kind != Update {
		t.Fatalf("rotate: kind %s err %v", kind, err)
	}
	if got := read(t, path); string(got) != "rotated" {
		t.Fatalf("rotate wrote %q", got)
	}
}

func TestWriteSecretRefusesEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s")
	for _, v := range [][]byte{nil, {}, []byte(" \n")} {
		if _, err := WriteSecret(path, v, appIDs, newFakeOwner(), false); err == nil {
			t.Fatalf("empty secret %q accepted", v)
		}
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("an empty secret file was created")
	}
}

func TestSecretErrorsAndReportNeverShowTheValue(t *testing.T) {
	dir := t.TempDir()
	o := newFakeOwner()
	o.fail = os.ErrPermission
	_, err := WriteSecret(filepath.Join(dir, "s"), []byte(sentinel), appIDs, o, false)
	if err == nil || strings.Contains(err.Error(), sentinel) {
		t.Fatalf("err %v", err)
	}
	_, err = WriteSecret(filepath.Join(dir, "no-such-dir", "s"), []byte(sentinel), appIDs, newFakeOwner(), false)
	if err == nil || strings.Contains(err.Error(), sentinel) {
		t.Fatalf("err %v", err)
	}
	pl := NewPlanner(dir, newFakeOwner())
	pl.Secret(Secret{Path: "new", Value: []byte(sentinel), Owner: appIDs})
	write(t, filepath.Join(dir, "old"), "x")
	pl.Secret(Secret{Path: "old", Value: []byte(sentinel), Owner: appIDs})
	pl.Secret(Secret{Path: "rot", Value: []byte(sentinel), Owner: appIDs, Rotate: true})
	p, err := pl.Plan()
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := p.WriteText(&out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), sentinel) || strings.Contains(strings.ToLower(out.String()), "sentinel") {
		t.Fatalf("report shows the value:\n%s", out.String())
	}
}

func TestPlannedSecrets(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "kept"), "x")
	o := newFakeOwner()
	pl := NewPlanner(dir, o)
	pl.Secret(Secret{Path: "new", Value: []byte("v"), Owner: appIDs})
	pl.Secret(Secret{Path: "kept", Value: []byte("v"), Owner: Root})
	pl.Secret(Secret{Path: "wrong-owner", Value: nil, Owner: appIDs}) // set up below
	pl.Secret(Secret{Path: "absent", Owner: appIDs, Hint: "pass --secret absent=FILE"})
	pl.Secret(Secret{Path: "optional", Owner: appIDs, Optional: true})
	write(t, filepath.Join(dir, "wrong-owner"), "x")
	p, err := pl.Plan()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Action{}
	for _, a := range p.Actions {
		got[a.Path] = a
	}
	if got["new"].Kind != Create || got["kept"].Kind != Conflict || got["absent"].Kind != Missing {
		t.Fatalf("actions %+v", p.Actions)
	}
	if !strings.Contains(got["kept"].Detail, "0600") || !strings.Contains(got["absent"].Detail, "--secret absent=FILE") {
		t.Fatalf("details %+v", p.Actions)
	}
	if _, listed := got["optional"]; listed {
		t.Fatal("an optional secret with no value and no file was listed")
	}
	if _, err := os.Lstat(filepath.Join(dir, "new")); !os.IsNotExist(err) {
		t.Fatal("planning (a dry run) wrote a secret")
	}
	if err := p.Apply(); err != nil {
		t.Fatal(err)
	}
	if ids, _ := o.Owner(filepath.Join(dir, "new")); ids != appIDs {
		t.Fatalf("owner %+v", ids)
	}
	if got := read(t, filepath.Join(dir, "kept")); string(got) != "x" {
		t.Fatal("a conflicting secret was changed")
	}
}

func TestPlanRefusesEmptySecretValue(t *testing.T) {
	pl := NewPlanner(t.TempDir(), newFakeOwner())
	pl.Secret(Secret{Path: "s", Value: []byte{}, Owner: appIDs})
	if _, err := pl.Plan(); err == nil {
		t.Fatal("an empty secret value was planned")
	}
}

func TestPlannedDirectories(t *testing.T) {
	dir := t.TempDir()
	o := newFakeOwner()
	pl := NewPlanner(dir, o)
	pl.Dir("secrets", 0o711, Root)
	pl.Dir("secrets/app", 0o700, appIDs)
	pl.Secret(Secret{Path: "secrets/app/x", Value: []byte("v"), Owner: appIDs})
	p, err := pl.Plan()
	if err != nil {
		t.Fatal(err)
	}
	if p.Count(Create) != 3 {
		t.Fatalf("actions %+v", p.Actions)
	}
	if err := p.Apply(); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{"secrets": 0o711, "secrets/app": 0o700} {
		info, err := os.Lstat(filepath.Join(dir, path))
		if err != nil || !info.IsDir() || info.Mode().Perm() != want {
			t.Fatalf("%s: %v %v", path, info, err)
		}
	}
	if ids, _ := o.Owner(filepath.Join(dir, "secrets/app")); ids != appIDs {
		t.Fatalf("owner %+v", ids)
	}
	// An existing directory with another owner is reported and left alone.
	pl = NewPlanner(dir, newFakeOwner()) // the new fake sees everything as root
	pl.Dir("secrets/app", 0o700, appIDs)
	p, _ = pl.Plan()
	if p.Actions[0].Kind != Conflict || len(p.steps) != 0 {
		t.Fatalf("actions %+v", p.Actions)
	}
}

// snapshot records what the no-op test promises not to change.
type fileState struct {
	data  string
	mode  os.FileMode
	mtime time.Time
	ids   IDs
}

func snapshot(t *testing.T, root string, o Owner) map[string]fileState {
	t.Helper()
	out := map[string]fileState{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		s := fileState{mode: info.Mode(), mtime: info.ModTime()}
		s.ids, _ = o.Owner(path)
		if !info.IsDir() {
			b, _ := os.ReadFile(path)
			s.data = string(b)
		}
		out[path] = s
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestCompleteInstallIsANoOp is the adopt rule end to end: planning against files that
// are already complete and correct reports nothing to do, and applying that plan changes
// no file's bytes, mode, owner or mtime.
func TestCompleteInstallIsANoOp(t *testing.T) {
	dir := t.TempDir()
	o := newFakeOwner()
	build := func() *Plan {
		pl := NewPlanner(dir, o)
		pl.Dir("secrets", 0o711, Root)
		pl.Dir("secrets/app", 0o700, appIDs)
		pl.Env(".env", "", []KV{{"APP_URL", "https://dash.club.example"}, {"TUNNEL_NET", "10.250.90"}})
		pl.Secret(Secret{Path: "secrets/app/discord-client-secret", Value: []byte("v"), Owner: appIDs})
		pl.Secret(Secret{Path: "secrets/app/bot", Owner: appIDs, Optional: true})
		p, err := pl.Plan()
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	if err := build().Apply(); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, dir, o)
	chowns := len(o.chowns)
	time.Sleep(20 * time.Millisecond) // any rewrite would now show a new mtime
	p := build()
	var out bytes.Buffer
	p.WriteText(&out)
	if p.Changes() != 0 || len(p.steps) != 0 || p.Count(Conflict) != 0 || !strings.Contains(out.String(), "nothing to do") {
		t.Fatalf("second run would change things:\n%s", out.String())
	}
	if err := p.Apply(); err != nil {
		t.Fatal(err)
	}
	after := snapshot(t, dir, o)
	if len(o.chowns) != chowns {
		t.Fatalf("no-op apply called chown: %v", o.chowns[chowns:])
	}
	for path, b := range before {
		if a, ok := after[path]; !ok || a != b {
			t.Errorf("%s changed: %+v -> %+v", path, b, a)
		}
	}
	if len(after) != len(before) {
		t.Errorf("files appeared: %d -> %d", len(before), len(after))
	}
}
