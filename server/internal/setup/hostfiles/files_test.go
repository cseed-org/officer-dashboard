package hostfiles

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeOwner stands in for chown and ownership stat, so tests run without root. It
// records every chown and remembers the IDs per file (by identity, so they follow a
// rename or a hard link, as real ownership does).
type fakeOwner struct {
	owned  []ownedFile
	chowns []string
	fail   error
}

type ownedFile struct {
	info os.FileInfo
	ids  IDs
}

func newFakeOwner() *fakeOwner { return &fakeOwner{} }

func (o *fakeOwner) Chown(path string, ids IDs) error {
	if o.fail != nil {
		return o.fail
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	o.chowns = append(o.chowns, filepath.Base(path))
	o.owned = append(o.owned, ownedFile{info, ids})
	return nil
}

func (o *fakeOwner) Owner(path string) (IDs, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return IDs{}, err
	}
	for i := len(o.owned) - 1; i >= 0; i-- {
		if os.SameFile(o.owned[i].info, info) {
			return o.owned[i].ids, nil
		}
	}
	return IDs{}, nil // files the test made itself belong to root, as on a real host
}

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dashboard.env")
	o := newFakeOwner()
	if err := WriteFileAtomic(path, []byte("A=1\n"), 0o600, nil, o); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(path, []byte("A=2\n"), 0o600, nil, o); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	info, _ := os.Stat(path)
	if string(got) != "A=2\n" || info.Mode().Perm() != 0o600 {
		t.Fatalf("content %q mode %v", got, info.Mode().Perm())
	}
	if names := listDir(t, dir); len(names) != 1 {
		t.Fatalf("temp file left behind: %v", names)
	}
}

func TestWriteFileAtomicFailureLeavesTheOldFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	o := newFakeOwner()
	o.fail = errors.New("chown refused")
	err := WriteFileAtomic(path, []byte("new"), 0o400, &IDs{UID: 10001, GID: 10001}, o)
	if err == nil {
		t.Fatal("expected the chown failure")
	}
	if got, _ := os.ReadFile(path); string(got) != "old" {
		t.Fatalf("a failed write changed the file: %q", got)
	}
	if names := listDir(t, dir); len(names) != 1 {
		t.Fatalf("temp file left behind: %v", names)
	}
}

func TestRenderFromTemplate(t *testing.T) {
	f, err := RenderFromTemplate(templatePath(".env.example"), []KV{
		{"APP_URL", "https://dash.club.example"},
		{"TUNNEL_NET", "10.250.90"},
		{"NEW_KEY", "x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(templatePath(".env.example"))
	out := string(f.Bytes())
	if v, _ := f.Get("APP_URL"); v != "https://dash.club.example" {
		t.Fatalf("APP_URL %q", v)
	}
	if !strings.Contains(out, "TUNNEL_NET=10.250.90") || strings.Contains(out, "#TUNNEL_NET=") {
		t.Fatal("TUNNEL_NET was not uncommented in place")
	}
	if !strings.HasSuffix(strings.TrimRight(out, "\r\n"), "NEW_KEY=x") {
		t.Fatal("unknown key not appended")
	}
	// Every comment line of the template survives.
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "# ") && !strings.Contains(out, line) {
			t.Fatalf("lost comment %q", line)
		}
	}
	if _, err := RenderFromTemplate(filepath.Join(t.TempDir(), "missing.example"), nil); err == nil {
		t.Fatal("missing template accepted")
	}
}

func TestEnvPlanAdoptRule(t *testing.T) {
	dir := t.TempDir()
	tmpl := "APP_URL=https://officers.example.org\nTUNNEL_TOKEN_FILE=./secrets/tunnel-token\n#TUNNEL_NET=10.250.89\nEMPTY=\n"
	write(t, filepath.Join(dir, ".env.example"), tmpl)
	existing := "APP_URL=https://officers.example.org\nTUNNEL_TOKEN_FILE=/elsewhere/token\nEMPTY=\nEXTRA=kept # admin's own\n"
	write(t, filepath.Join(dir, ".env"), existing)
	pl := NewPlanner(dir, newFakeOwner())
	pl.Env(".env", ".env.example", []KV{
		{"APP_URL", "https://dash.club.example"},        // template example: filled
		{"TUNNEL_TOKEN_FILE", "./secrets/tunnel-token"}, // admin's own value: conflict, kept
		{"TUNNEL_NET", "10.250.90"},                     // missing: set
		{"EMPTY", "v"},                                  // empty: filled
		{"EXTRA", "kept"},                               // same value: nothing to do
	})
	p, err := pl.Plan()
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]Kind{}
	for _, a := range p.Actions {
		kinds[a.Key] = a.Kind
	}
	want := map[string]Kind{"APP_URL": Update, "TUNNEL_TOKEN_FILE": Conflict, "TUNNEL_NET": Update, "EMPTY": Update, "EXTRA": Exists}
	for k, v := range want {
		if kinds[k] != v {
			t.Errorf("%s: %s, want %s", k, kinds[k], v)
		}
	}
	if err := p.Apply(); err != nil {
		t.Fatal(err)
	}
	got := ParseEnv(read(t, filepath.Join(dir, ".env")))
	for k, v := range map[string]string{"APP_URL": "https://dash.club.example", "TUNNEL_TOKEN_FILE": "/elsewhere/token", "TUNNEL_NET": "10.250.90", "EMPTY": "v", "EXTRA": "kept"} {
		if g, _ := got.Get(k); g != v {
			t.Errorf("after apply %s=%q, want %q", k, g, v)
		}
	}
	if !bytes.Contains(read(t, filepath.Join(dir, ".env")), []byte("EXTRA=kept # admin's own")) {
		t.Error("an untouched line changed")
	}
}

func TestEnvPlanCreatesFromTemplate(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "backup.env.example"), "# keep me\nBACKUP_SINK_URL=https://backups.example.org\n")
	pl := NewPlanner(dir, newFakeOwner())
	pl.Env("backup.env", "backup.env.example", []KV{{"BACKUP_SINK_URL", "https://backups.club.example"}})
	p, err := pl.Plan()
	if err != nil {
		t.Fatal(err)
	}
	if p.Actions[0].Kind != Create || p.Actions[0].Path != "backup.env" {
		t.Fatalf("first action %+v", p.Actions[0])
	}
	if _, err := os.Stat(filepath.Join(dir, "backup.env")); !os.IsNotExist(err) {
		t.Fatal("planning wrote the file")
	}
	if err := p.Apply(); err != nil {
		t.Fatal(err)
	}
	if got := string(read(t, filepath.Join(dir, "backup.env"))); got != "# keep me\nBACKUP_SINK_URL=https://backups.club.example\n" {
		t.Fatalf("got %q", got)
	}
	if info, _ := os.Stat(filepath.Join(dir, "backup.env")); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode().Perm())
	}
}

func TestApplyRefusesAFileChangedSincePlanning(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, ".env"), "A=1\n")
	pl := NewPlanner(dir, newFakeOwner())
	pl.Env(".env", "", []KV{{"B", "2"}})
	p, err := pl.Plan()
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, ".env"), "A=1\nC=admin edit\n")
	if err := p.Apply(); err == nil {
		t.Fatal("apply overwrote an edit made after planning")
	}
	if got := string(read(t, filepath.Join(dir, ".env"))); got != "A=1\nC=admin edit\n" {
		t.Fatalf("got %q", got)
	}
}

func write(t *testing.T, path, s string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
