package setup

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"officerdashboard/internal/setup/cfapi"
	"officerdashboard/internal/setup/cffake"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/")

const token = "cf-setup-TOKEN-0123456789abcdef"

type result struct {
	code           int
	stdout, stderr string
	srv            *cffake.Server
	tokenSeen      string
}

func runSetup(t *testing.T, routes map[string]cffake.Response, env map[string]string, args ...string) result {
	t.Helper()
	srv := cffake.Start(routes)
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "token")
	if err := os.WriteFile(tokenFile, []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	base := map[string]string{"APP_URL": "https://" + cffake.Host, TokenFileEnv: tokenFile}
	for k, v := range env {
		base[k] = v
	}
	var out, errb bytes.Buffer
	var seen string
	code := Run(context.Background(), args, Deps{
		Getenv:       func(k string) string { return base[k] },
		Stdout:       &out,
		Stderr:       &errb,
		ReadFile:     os.ReadFile,
		IsTerminal:   func(int) bool { return false },
		ReadPassword: func(int) ([]byte, error) { return nil, errors.New("no terminal") },
		NewReader: func(tok string) cfapi.Reader {
			seen = tok
			return cfapi.New(tok, cfapi.WithBaseURL(srv.URL), cfapi.WithRetry())
		},
	})
	if bad := srv.NonGET(); len(bad) > 0 {
		t.Fatalf("setup sent non-GET requests: %+v", bad)
	}
	if strings.Contains(out.String()+errb.String(), token) {
		t.Fatal("the token was printed")
	}
	return result{code, out.String(), errb.String(), srv, seen}
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test -update to create it)", err)
	}
	if norm := strings.ReplaceAll(string(want), "\r\n", "\n"); got != norm {
		t.Errorf("%s differs (go test -update rewrites it):\n--- got\n%s\n--- want\n%s", name, got, norm)
	}
}

func TestDryRunFreshAccount(t *testing.T) {
	r := runSetup(t, cffake.Fresh(), nil, "--dry-run")
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	if r.tokenSeen != token {
		t.Fatalf("token %q, want it trimmed from the file", r.tokenSeen)
	}
	golden(t, "dry-run-fresh.txt", r.stdout)
}

func TestDryRunCompleteAccount(t *testing.T) {
	r := runSetup(t, cffake.Complete(), nil, "--dry-run")
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	golden(t, "dry-run-complete.txt", r.stdout)
	if strings.Contains(r.stdout, " create ") || strings.Contains(r.stdout, " drift ") || !strings.Contains(r.stdout, "0 create, 0 drift") {
		t.Fatalf("a complete account must need nothing:\n%s", r.stdout)
	}

	j := runSetup(t, cffake.Complete(), nil, "--dry-run", "--json")
	if j.code != 0 {
		t.Fatalf("exit %d: %s", j.code, j.stderr)
	}
	golden(t, "dry-run-complete.json", j.stdout)
}

// TestDryRunLiveShapedAccount uses the partial setup a live account had during the
// read-only spike: it must report creates, one drift and the unprotected-route warning.
func TestDryRunLiveShapedAccount(t *testing.T) {
	r := runSetup(t, cffake.Live(), nil, "--dry-run")
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	golden(t, "dry-run-live.txt", r.stdout)
	for _, want := range []string{"drift       Bot Fight Mode", "WARNING: the tunnel already routes this hostname", "no cloudflared connector is running"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("output lacks %q", want)
		}
	}
}

func TestPrefixOnRealHostnameWarns(t *testing.T) {
	r := runSetup(t, cffake.Complete(), map[string]string{"BACKUP_SINK_URL": "https://" + cffake.Backup}, "--dry-run", "--name-prefix", "stg-")
	if r.code != 0 || !strings.Contains(r.stderr, `WARNING: --name-prefix "stg-" found resources without that prefix`) {
		t.Fatalf("exit %d\n%s", r.code, r.stderr)
	}
	r = runSetup(t, cffake.Complete(), map[string]string{"BACKUP_SINK_URL": "https://" + cffake.Backup}, "--dry-run")
	if strings.Contains(r.stderr, "WARNING") || strings.Contains(r.stderr, "assuming") {
		t.Fatalf("unexpected warning without a prefix: %s", r.stderr)
	}
}

func TestDefaultBackupHostnameIsAnnounced(t *testing.T) {
	r := runSetup(t, cffake.Complete(), nil, "--dry-run")
	if r.code != 0 || !strings.Contains(r.stderr, "assuming backups.example.org") {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
}

func TestInvalidTokenMessage(t *testing.T) {
	routes := cffake.Complete()
	routes["/zones"] = cffake.Response{Status: 401, Code: 1000, Message: "Invalid API Token"}
	r := runSetup(t, routes, nil, "--dry-run")
	if r.code != 1 || !strings.Contains(r.stderr, "does not accept this API token") || r.stdout != "" {
		t.Fatalf("exit %d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
}

func TestDryRunUsesTunnelIDFromTokenFile(t *testing.T) {
	routes := cffake.Complete()
	routes["/accounts/"+cffake.AccountID+"/cfd_tunnel"] = cffake.Response{Result: []any{
		map[string]any{"id": cffake.TunnelID, "name": "renamed-by-hand", "config_src": "cloudflare"},
	}}
	tok := base64.StdEncoding.EncodeToString([]byte(`{"a":"acct-tag","t":"` + strings.ToUpper(cffake.TunnelID) + `","s":"c2VjcmV0LXNlY3JldC1zZWNyZXQtc2VjcmV0LXNlY3JldA=="}`))
	path := filepath.Join(t.TempDir(), "tunnel-token")
	if err := os.WriteFile(path, []byte(tok), 0o400); err != nil {
		t.Fatal(err)
	}
	r := runSetup(t, routes, map[string]string{"TUNNEL_TOKEN_FILE": path}, "--dry-run")
	if r.code != 0 || !strings.Contains(r.stdout, `named "renamed-by-hand" (matched by the host's tunnel token)`) {
		t.Fatalf("exit %d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
	if strings.Contains(r.stdout+r.stderr, "c2VjcmV0") || strings.Contains(r.stdout+r.stderr, tok) {
		t.Fatal("tunnel token secret printed")
	}

	// An unreadable or malformed file falls back to name matching with a note.
	r = runSetup(t, cffake.Complete(), map[string]string{"TUNNEL_TOKEN_FILE": filepath.Join(t.TempDir(), "missing")}, "--dry-run")
	if r.code != 0 || !strings.Contains(r.stderr, "matching the tunnel by name") {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
}

func TestDryRunWritesNoFiles(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	before := listTree(t, dir)
	r := runSetup(t, cffake.Fresh(), nil, "--dry-run")
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	if after := listTree(t, dir); after != before {
		t.Fatalf("files changed in the working directory:\nbefore %s\nafter %s", before, after)
	}
}

func listTree(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, _ := d.Info()
		b.WriteString(p + " " + info.ModTime().String() + "\n")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// Without --dry-run the command applies, which needs the host phase in --deploy-dir
// first; here (no .env) it stops before reading the token or calling Cloudflare.
func TestApplyWithoutTheHostPhaseCallsNothing(t *testing.T) {
	r := runSetup(t, cffake.Complete(), nil, "--deploy-dir", t.TempDir())
	if r.code != 1 || !strings.Contains(r.stderr, "scripts/setup host --yes") || len(r.srv.Requests()) != 0 {
		t.Fatalf("exit %d, %d requests: %s", r.code, len(r.srv.Requests()), r.stderr)
	}
}

func TestTokenIsNeverAFlag(t *testing.T) {
	for _, f := range []string{"--token", "--api-token", "--cloudflare-token"} {
		r := runSetup(t, cffake.Complete(), nil, "--dry-run", f, token)
		if r.code != 2 || len(r.srv.Requests()) != 0 {
			t.Fatalf("%s accepted: exit %d", f, r.code)
		}
	}
}

func TestTokenSources(t *testing.T) {
	// No file and no terminal: refuse with a pointer to the variable.
	r := runSetup(t, cffake.Complete(), map[string]string{TokenFileEnv: ""}, "--dry-run")
	if r.code != 1 || !strings.Contains(r.stderr, TokenFileEnv) || len(r.srv.Requests()) != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	// An empty or multi-line token file is refused before any request.
	for _, content := range []string{"\n", "line one\nline two that is long enough\n"} {
		path := filepath.Join(t.TempDir(), "token")
		_ = os.WriteFile(path, []byte(content), 0o600)
		r = runSetup(t, cffake.Complete(), map[string]string{TokenFileEnv: path}, "--dry-run")
		if r.code != 1 || len(r.srv.Requests()) != 0 {
			t.Fatalf("token file %q: exit %d: %s", content, r.code, r.stderr)
		}
	}
	// A terminal prompt reads without echo through ReadPassword.
	srv := cffake.Start(cffake.Complete())
	defer srv.Close()
	var out, errb bytes.Buffer
	var seen string
	code := Run(context.Background(), []string{"--dry-run"}, Deps{
		Getenv:       func(k string) string { return map[string]string{"APP_URL": "https://" + cffake.Host}[k] },
		Stdout:       &out,
		Stderr:       &errb,
		ReadFile:     os.ReadFile,
		IsTerminal:   func(int) bool { return true },
		ReadPassword: func(int) ([]byte, error) { return []byte(" " + token + " "), nil },
		NewReader: func(tok string) cfapi.Reader {
			seen = tok
			return cfapi.New(tok, cfapi.WithBaseURL(srv.URL), cfapi.WithRetry())
		},
	})
	if code != 0 || seen != token || !strings.Contains(errb.String(), "input hidden") || strings.Contains(out.String()+errb.String(), token) {
		t.Fatalf("exit %d, token %q, stderr %q", code, seen, errb.String())
	}
}

func TestBadInputsAreUsageErrors(t *testing.T) {
	for _, env := range []map[string]string{{"APP_URL": ""}, {"APP_URL": "http://officers.example.org"}} {
		r := runSetup(t, cffake.Complete(), env, "--dry-run")
		if r.code != 2 || !strings.Contains(r.stderr, "APP_URL") || len(r.srv.Requests()) != 0 {
			t.Fatalf("%v: exit %d: %s", env, r.code, r.stderr)
		}
	}
}

func TestAmbiguityIsAnError(t *testing.T) {
	routes := cffake.Complete()
	routes["/accounts/"+cffake.AccountID+"/d1/database"] = cffake.Response{Result: []any{
		map[string]any{"uuid": "d-1", "name": "dashboard-backups"}, map[string]any{"uuid": "d-2", "name": "dashboard-backups"},
	}}
	r := runSetup(t, routes, nil, "--dry-run")
	if r.code != 1 || !strings.Contains(r.stderr, "d-1, d-2") || r.stdout != "" {
		t.Fatalf("exit %d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
}
