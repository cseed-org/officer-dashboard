package setup

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"officerdashboard/internal/setup/hostfiles"
)

// memOwner records ownership by file identity, so tests run without root.
type memOwner struct {
	owned []struct {
		info os.FileInfo
		ids  hostfiles.IDs
	}
}

func (o *memOwner) Chown(path string, ids hostfiles.IDs) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	o.owned = append(o.owned, struct {
		info os.FileInfo
		ids  hostfiles.IDs
	}{info, ids})
	return nil
}

func (o *memOwner) Owner(path string) (hostfiles.IDs, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return hostfiles.IDs{}, err
	}
	for i := len(o.owned) - 1; i >= 0; i-- {
		if os.SameFile(o.owned[i].info, info) {
			return o.owned[i].ids, nil
		}
	}
	return hostfiles.Root, nil
}

const (
	clientSecret = "SENTINEL-discord-client-secret"
	tunnelToken  = "SENTINEL-tunnel-token"
	accessID     = "SENTINEL-access-id.access"
	accessSecret = "SENTINEL-access-secret"
)

// deployDir is a fresh deploy directory: the real templates and nothing else.
func deployDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "deploy dir with spaces")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".env.example", "dashboard.env.example", "backup.env.example"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "deploy", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

type hostResult struct {
	code           int
	stdout, stderr string
}

func hostCmd(t *testing.T, owner *memOwner, stdin string, args ...string) hostResult {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(context.Background(), append([]string{"host"}, args...), Deps{
		Getenv:       func(string) string { return "" },
		Stdout:       &out,
		Stderr:       &errb,
		ReadFile:     os.ReadFile,
		IsTerminal:   func(int) bool { return false },
		ReadPassword: func(int) ([]byte, error) { return nil, errors.New("no terminal") },
		Stdin:        strings.NewReader(stdin),
		Owner:        owner,
	})
	r := hostResult{code, out.String(), errb.String()}
	if strings.Contains(r.stdout+r.stderr, "SENTINEL") {
		t.Fatalf("a secret value was printed:\n%s\n%s", r.stdout, r.stderr)
	}
	return r
}

var freshArgs = []string{
	"--app-url", "https://dash.club.example",
	"--discord-client-id", "123456789012345678",
	"--owner-discord-id", "123456789012345679",
	"--owner-name", "Ada Lovelace",
}

const freshSecrets = `{"discord-client-secret":"` + clientSecret + `","tunnel-token":"` + tunnelToken +
	`","backup-access-client-id":"` + accessID + `","backup-access-client-secret":"` + accessSecret + `"}`

func env(t *testing.T, path string) *hostfiles.EnvFile {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return hostfiles.ParseEnv(raw)
}

func TestHostWritesAFreshInstall(t *testing.T) {
	dir := deployDir(t)
	o := &memOwner{}
	r := hostCmd(t, o, freshSecrets, append([]string{"--yes", "--deploy-dir", dir, "--secrets-stdin"}, freshArgs...)...)
	if r.code != 0 {
		t.Fatalf("exit %d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
	for file, want := range map[string]map[string]string{
		".env": {"APP_URL": "https://dash.club.example", "TUNNEL_TOKEN_FILE": "./secrets/tunnel-token"},
		"dashboard.env": {
			"APP_URL": "https://dash.club.example", "DISCORD_CLIENT_ID": "123456789012345678",
			"DISCORD_CLIENT_SECRET_FILE": "/run/secrets/app/discord-client-secret",
			"BOOTSTRAP_OWNER_DISCORD_ID": "123456789012345679", "BOOTSTRAP_OWNER_NAME": "Ada Lovelace",
		},
		"backup.env": {
			"BACKUP_SINK_URL":                  "https://backups.club.example",
			"BACKUP_BEARER_SECRET_FILE":        "/run/secrets/backup/bearer",
			"BACKUP_ACCESS_CLIENT_ID_FILE":     "/run/secrets/backup/access-client-id",
			"BACKUP_ACCESS_CLIENT_SECRET_FILE": "/run/secrets/backup/access-client-secret",
		},
	} {
		f := env(t, filepath.Join(dir, file))
		for k, v := range want {
			if got, _ := f.Get(k); got != v {
				t.Errorf("%s %s=%q, want %q", file, k, got, v)
			}
		}
		if info, _ := os.Stat(filepath.Join(dir, file)); info.Mode().Perm() != 0o600 {
			t.Errorf("%s mode %04o", file, info.Mode().Perm())
		}
	}
	// Optional integrations stay commented out when not given.
	if env(t, filepath.Join(dir, "dashboard.env")).Has("DISCORD_BOT_TOKEN_FILE") {
		t.Error("bot token file enabled without a bot token")
	}
	for path, want := range map[string]struct {
		value string
		ids   hostfiles.IDs
	}{
		"secrets/app/discord-client-secret":   {clientSecret, hostfiles.AppIDs},
		"secrets/tunnel-token":                {tunnelToken, hostfiles.TunnelIDs},
		"secrets/backup/access-client-id":     {accessID, hostfiles.AppIDs},
		"secrets/backup/access-client-secret": {accessSecret, hostfiles.AppIDs},
	} {
		got, err := os.ReadFile(filepath.Join(dir, path))
		if err != nil || string(got) != want.value {
			t.Errorf("%s: %q %v", path, got, err)
		}
		if ids, _ := o.Owner(filepath.Join(dir, path)); ids != want.ids {
			t.Errorf("%s owner %+v", path, ids)
		}
	}
	bearer, err := os.ReadFile(filepath.Join(dir, "secrets/backup/bearer"))
	if err != nil {
		t.Fatal(err)
	}
	if b, err := hex.DecodeString(string(bearer)); err != nil || len(b) != 32 {
		t.Fatalf("bearer is not 32 random bytes in hex: %d chars", len(bearer))
	}
	if !strings.Contains(r.stdout, "wrangler secret put BACKUP_BEARER_SECRET") {
		t.Error("a new bearer must come with the reminder to give the Worker the same value")
	}
	if strings.Contains(r.stdout, string(bearer)) {
		t.Fatal("the generated bearer was printed")
	}
	// The age key is a later step: setup says so rather than inventing one.
	if !regexp.MustCompile(`missing\s+backup.env BACKUP_AGE_RECIPIENT`).MatchString(r.stdout) {
		t.Errorf("age recipient not reported missing:\n%s", r.stdout)
	}
}

func TestHostDryRunWritesNothing(t *testing.T) {
	dir := deployDir(t)
	before, _ := os.ReadDir(dir)
	for _, mode := range [][]string{{"--dry-run"}, {}} {
		r := hostCmd(t, &memOwner{}, freshSecrets, append(append(mode, "--deploy-dir", dir, "--secrets-stdin"), freshArgs...)...)
		if r.code != 0 {
			t.Fatalf("%v: exit %d\n%s", mode, r.code, r.stderr)
		}
		if !regexp.MustCompile(`create\s+\.env\s`).MatchString(r.stdout) || !strings.Contains(r.stdout, "secrets/tunnel-token") {
			t.Fatalf("%v: plan not printed:\n%s", mode, r.stdout)
		}
		if len(mode) == 0 && !strings.Contains(r.stdout+r.stderr, "--yes") {
			t.Fatalf("without --yes setup must say how to apply:\n%s", r.stdout)
		}
		after, _ := os.ReadDir(dir)
		if len(after) != len(before) {
			t.Fatalf("%v wrote files: %v", mode, after)
		}
	}
}

type state struct {
	data  string
	mode  os.FileMode
	mtime time.Time
	ids   hostfiles.IDs
}

func tree(t *testing.T, root string, o *memOwner) map[string]state {
	t.Helper()
	out := map[string]state{}
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		s := state{mode: info.Mode(), mtime: info.ModTime()}
		s.ids, _ = o.Owner(path)
		if !info.IsDir() {
			b, _ := os.ReadFile(path)
			s.data = string(b)
		}
		out[path] = s
		return nil
	})
	return out
}

// TestHostCompleteInstallIsANoOp: run against a complete, correct install, setup reports
// nothing to do and changes no file's bytes, mode, owner or mtime.
func TestHostCompleteInstallIsANoOp(t *testing.T) {
	dir := deployDir(t)
	o := &memOwner{}
	if r := hostCmd(t, o, freshSecrets, append([]string{"--yes", "--deploy-dir", dir, "--secrets-stdin", "--age-recipient", "age1" + strings.Repeat("q", 58)}, freshArgs...)...); r.code != 0 {
		t.Fatalf("first run: %d %s", r.code, r.stderr)
	}
	before := tree(t, dir, o)
	time.Sleep(20 * time.Millisecond)
	for _, args := range [][]string{
		{"--yes", "--deploy-dir", dir}, // nothing given: all read back
		append([]string{"--yes", "--deploy-dir", dir, "--secrets-stdin"}, freshArgs...), // same values again
	} {
		stdin := ""
		if len(args) > 3 {
			stdin = freshSecrets
		}
		r := hostCmd(t, o, stdin, args...)
		if r.code != 0 || !strings.Contains(r.stdout, "nothing to do") || strings.Contains(r.stdout, "conflict") || strings.Contains(r.stdout, "missing") {
			t.Fatalf("%v: exit %d\n%s\n%s", args, r.code, r.stdout, r.stderr)
		}
	}
	after := tree(t, dir, o)
	for path, b := range before {
		if after[path] != b {
			t.Errorf("%s changed: %+v -> %+v", path, b, after[path])
		}
	}
	if len(after) != len(before) {
		t.Errorf("files appeared")
	}
}

func TestHostAdoptsAnExistingInstall(t *testing.T) {
	dir := deployDir(t)
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("APP_URL=https://other.club.example\nTUNNEL_TOKEN_FILE=./secrets/tunnel-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "secrets", "app"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secrets/app/discord-client-secret"), []byte("admin's"), 0o400); err != nil {
		t.Fatal(err)
	}
	r := hostCmd(t, &memOwner{}, freshSecrets, append([]string{"--yes", "--deploy-dir", dir, "--secrets-stdin"}, freshArgs...)...)
	if r.code != 0 {
		t.Fatalf("exit %d %s", r.code, r.stderr)
	}
	if !regexp.MustCompile(`conflict\s+\.env APP_URL`).MatchString(r.stdout) {
		t.Fatalf("APP_URL conflict not reported:\n%s", r.stdout)
	}
	if got, _ := env(t, filepath.Join(dir, ".env")).Get("APP_URL"); got != "https://other.club.example" {
		t.Fatalf("APP_URL changed to %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "secrets/app/discord-client-secret")); string(got) != "admin's" {
		t.Fatal("existing secret replaced")
	}
}

func TestHostOptionalSecretsEnableTheirKeys(t *testing.T) {
	dir := deployDir(t)
	stdin := `{"discord-bot-token":"SENTINEL-bot"}`
	r := hostCmd(t, &memOwner{}, stdin, append([]string{"--yes", "--deploy-dir", dir, "--secrets-stdin"}, freshArgs...)...)
	if r.code != 0 {
		t.Fatalf("exit %d %s", r.code, r.stderr)
	}
	if got, _ := env(t, filepath.Join(dir, "dashboard.env")).Get("DISCORD_BOT_TOKEN_FILE"); got != "/run/secrets/app/discord-bot-token" {
		t.Fatalf("DISCORD_BOT_TOKEN_FILE=%q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "secrets/app/discord-bot-token")); string(got) != "SENTINEL-bot" {
		t.Fatal("bot token not written")
	}
}

func TestHostProjectNameAndSubnet(t *testing.T) {
	dir := deployDir(t)
	r := hostCmd(t, &memOwner{}, "", append([]string{"--yes", "--deploy-dir", dir, "--compose-project-name", "officer-dashboard-2", "--tunnel-net", "10.250.90"}, freshArgs...)...)
	if r.code != 0 {
		t.Fatalf("exit %d %s", r.code, r.stderr)
	}
	f := env(t, filepath.Join(dir, ".env"))
	if got, _ := f.Get("COMPOSE_PROJECT_NAME"); got != "officer-dashboard-2" {
		t.Errorf("COMPOSE_PROJECT_NAME=%q", got)
	}
	if got, _ := f.Get("TUNNEL_NET"); got != "10.250.90" {
		t.Errorf("TUNNEL_NET=%q", got)
	}
}

func TestHostRejectsBadInput(t *testing.T) {
	for name, c := range map[string]struct {
		stdin string
		args  []string
	}{
		"discord id":      {"", []string{"--discord-client-id", "12ab"}},
		"owner id":        {"", []string{"--owner-discord-id", "1"}},
		"app url":         {"", []string{"--app-url", "http://dash.club.example/"}},
		"tunnel net":      {"", []string{"--tunnel-net", "10.250.300"}},
		"project name":    {"", []string{"--compose-project-name", "Bad Name"}},
		"age recipient":   {"", []string{"--age-recipient", "age1..."}},
		"unknown secret":  {`{"nope":"x"}`, []string{"--secrets-stdin"}},
		"empty secret":    {`{"tunnel-token":""}`, []string{"--secrets-stdin"}},
		"not json":        {`SENTINEL`, []string{"--secrets-stdin"}},
		"yes and dry run": {"", []string{"--yes", "--dry-run"}},
		"rotate unknown":  {"", []string{"--rotate", "nope"}},
		"extra argument":  {"", []string{"stray"}},
	} {
		t.Run(name, func(t *testing.T) {
			dir := deployDir(t)
			r := hostCmd(t, &memOwner{}, c.stdin, append([]string{"--deploy-dir", dir}, c.args...)...)
			if r.code != 2 {
				t.Fatalf("exit %d\n%s\n%s", r.code, r.stdout, r.stderr)
			}
		})
	}
}

// fullInstall makes a complete install, as TestHostCompleteInstallIsANoOp does.
func fullInstall(t *testing.T) (string, *memOwner) {
	t.Helper()
	dir := deployDir(t)
	o := &memOwner{}
	if r := hostCmd(t, o, freshSecrets, append([]string{"--yes", "--deploy-dir", dir, "--secrets-stdin", "--age-recipient", "age1" + strings.Repeat("q", 58)}, freshArgs...)...); r.code != 0 {
		t.Fatalf("first run: %d %s", r.code, r.stderr)
	}
	return dir, o
}

// Review F-1: an existing install's Compose project name and subnet are never written,
// even though the shipped .env has both keys commented out. A different value is a
// conflict; the Compose default counts as the install's current value.
func TestHostNeverRenamesAnExistingInstall(t *testing.T) {
	dir, o := fullInstall(t)
	before, _ := os.ReadFile(filepath.Join(dir, ".env"))
	r := hostCmd(t, o, "", "--yes", "--deploy-dir", dir, "--compose-project-name", "officer-dashboard-2", "--tunnel-net", "10.250.90")
	if r.code != 0 {
		t.Fatalf("exit %d %s", r.code, r.stderr)
	}
	after, _ := os.ReadFile(filepath.Join(dir, ".env"))
	if string(after) != string(before) {
		t.Fatalf(".env changed:\n%s", r.stdout)
	}
	for _, key := range []string{"COMPOSE_PROJECT_NAME", "TUNNEL_NET"} {
		if !regexp.MustCompile(`conflict\s+\.env ` + key + `\s.*never renames`).MatchString(r.stdout) {
			t.Errorf("%s conflict not reported:\n%s", key, r.stdout)
		}
	}
	// The same values as the defaults are simply this install's own: nothing to do.
	r = hostCmd(t, o, "", "--yes", "--deploy-dir", dir, "--compose-project-name", "officer-dashboard", "--tunnel-net", "10.250.89")
	if r.code != 0 || !strings.Contains(r.stdout, "nothing to do") || strings.Contains(r.stdout, "conflict") {
		t.Fatalf("exit %d\n%s", r.code, r.stdout)
	}
}

// Review F-1: an image pin, or containers and volumes Docker already has for the project
// (--install-exists, set by scripts/setup), also mark an existing install.
func TestHostProjectNameOnlyForANewInstall(t *testing.T) {
	for name, prep := range map[string]func(dir string) []string{
		"image pin": func(dir string) []string {
			os.WriteFile(filepath.Join(dir, "prod-image.env"), []byte("DASHBOARD_IMAGE=r.example/d@sha256:"+strings.Repeat("a", 64)+"\n"), 0o600)
			return nil
		},
		"containers or volumes": func(string) []string { return []string{"--install-exists"} },
		"an existing .env": func(dir string) []string {
			os.WriteFile(filepath.Join(dir, ".env"), []byte("#COMPOSE_PROJECT_NAME=officer-dashboard\n"), 0o600)
			return nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := deployDir(t)
			extra := prep(dir)
			r := hostCmd(t, &memOwner{}, "", append(append([]string{"--yes", "--deploy-dir", dir, "--compose-project-name", "club-two", "--tunnel-net", "10.250.90"}, extra...), freshArgs...)...)
			if r.code != 0 {
				t.Fatalf("exit %d %s", r.code, r.stderr)
			}
			f := env(t, filepath.Join(dir, ".env"))
			if f.Has("COMPOSE_PROJECT_NAME") || f.Has("TUNNEL_NET") {
				t.Fatalf("an existing install was given a new project or subnet:\n%s", r.stdout)
			}
		})
	}
}

// Review N-1: the bearer reminder appears only when the bearer file was really written.
func TestHostBearerReminderOnlyWhenWritten(t *testing.T) {
	dir := deployDir(t)
	if r := hostCmd(t, &memOwner{}, "", append([]string{"--dry-run", "--deploy-dir", dir}, freshArgs...)...); strings.Contains(r.stdout, "wrangler secret put") {
		t.Fatal("a dry run printed the reminder for a bearer it did not write")
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "backup.env.example"))
	os.WriteFile(filepath.Join(dir, "backup.env"), []byte(strings.Replace(string(raw), "/run/secrets/backup/bearer", "/run/secrets/backup/my-bearer", 1)), 0o600)
	r := hostCmd(t, &memOwner{}, "", append([]string{"--yes", "--deploy-dir", dir}, freshArgs...)...)
	if strings.Contains(r.stdout, "wrangler secret put") {
		t.Fatalf("reminder printed though the bearer lives elsewhere:\n%s", r.stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, "secrets/backup/bearer")); err == nil {
		t.Fatal("a bearer file was written although backup.env points elsewhere")
	}
}

// Review F-3 at the command level: updating a key keeps dashboard.env's 0640.
func TestHostKeepsAnEnvFilesMode(t *testing.T) {
	dir := deployDir(t)
	path := filepath.Join(dir, "dashboard.env")
	raw, _ := os.ReadFile(filepath.Join(dir, "dashboard.env.example"))
	os.WriteFile(path, raw, 0o640)
	os.Chmod(path, 0o640)
	if r := hostCmd(t, &memOwner{}, "", "--yes", "--deploy-dir", dir, "--discord-client-id", "123456789012345678", "--app-url", "https://dash.club.example"); r.code != 0 {
		t.Fatalf("exit %d %s", r.code, r.stderr)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o640 {
		t.Fatalf("mode %04o", info.Mode().Perm())
	}
}
