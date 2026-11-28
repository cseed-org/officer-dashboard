package setup

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"officerdashboard/internal/setup/cfapi"
	"officerdashboard/internal/setup/cffake"
	"officerdashboard/internal/setup/cfprovision"
	"officerdashboard/internal/setup/hostfiles"
	"officerdashboard/internal/setup/plan"
)

// applyToken is the setup token the tests hand the command. It is built at run time so
// the source holds nothing that looks like a credential.
var applyToken = "cf-apply-" + strings.Repeat("Ab3", 8)

// applyRig is one fake Cloudflare account and one deploy directory on which the host
// phase (`setup host --yes`) already ran, as the documented order requires.
type applyRig struct {
	t         *testing.T
	acct      *cffake.Account
	dir       string
	owner     hostfiles.Owner
	tokenFile string
	tty       bool
	stdin     io.Reader
	ctx       context.Context
	engines   int
}

type applyResult struct {
	code           int
	stdout, stderr string
}

func newApplyRig(t *testing.T, routes map[string]cffake.Response) *applyRig {
	t.Helper()
	return newApplyRigOwned(t, routes, &memOwner{})
}

func newApplyRigOwned(t *testing.T, routes map[string]cffake.Response, owner hostfiles.Owner) *applyRig {
	t.Helper()
	acct := cffake.NewAccount(routes)
	t.Cleanup(acct.Close)
	r := &applyRig{t: t, acct: acct, dir: deployDir(t), owner: owner, ctx: context.Background()}
	var hout, herr bytes.Buffer
	if code := Run(context.Background(), []string{"host", "--yes", "--deploy-dir", r.dir, "--app-url", "https://" + cffake.Host}, Deps{
		Getenv: func(string) string { return "" }, Stdout: &hout, Stderr: &herr, ReadFile: os.ReadFile,
		IsTerminal: func(int) bool { return false }, Stdin: strings.NewReader(""), Owner: owner,
	}); code != 0 {
		t.Fatalf("host phase: exit %d\n%s\n%s", code, hout.String(), herr.String())
	}
	r.tokenFile = filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(r.tokenFile, []byte(applyToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, w := range acct.Writes() {
			if w.Method != http.MethodPost && w.Method != http.MethodPut {
				t.Errorf("sent %s %s", w.Method, w.Path)
			}
		}
	})
	return r
}

// groupArgs are the members the two Access groups are created with.
var groupArgs = []string{"--officers-email", "president@example.org", "--launchpad-email", "mentor@example.org"}

func (r *applyRig) run(args ...string) applyResult {
	r.t.Helper()
	env := map[string]string{"APP_URL": "https://" + cffake.Host, TokenFileEnv: r.tokenFile}
	var out, errb bytes.Buffer
	stdin := r.stdin
	if stdin == nil {
		stdin = strings.NewReader("")
	}
	code := Run(r.ctx, append([]string{"--deploy-dir", r.dir}, args...), Deps{
		Getenv:       func(k string) string { return env[k] },
		Stdout:       &out,
		Stderr:       &errb,
		ReadFile:     os.ReadFile,
		IsTerminal:   func(int) bool { return r.tty },
		ReadPassword: func(int) ([]byte, error) { return nil, errors.New("no terminal") },
		NewReader: func(tok string) cfapi.Reader {
			return cfapi.New(tok, cfapi.WithBaseURL(r.acct.URL), cfapi.WithRetry())
		},
		NewEngine: func(tok string, d plan.Desired, sink cfprovision.SecretSink, w io.Writer, opt cfprovision.Options) *cfprovision.Engine {
			r.engines++
			return cfprovision.NewEngine(tok, d, sink, w, opt, cfprovision.WithAPIBase(r.acct.URL), cfprovision.WithReadRetry())
		},
		Stdin: stdin,
		Owner: r.owner,
	})
	res := applyResult{code, out.String(), errb.String()}
	all := res.stdout + res.stderr
	if strings.Contains(all, applyToken) {
		r.t.Fatal("the setup token was printed")
	}
	for _, s := range r.acct.Secrets() {
		if strings.Contains(all, s) {
			r.t.Fatalf("a secret Cloudflare handed out was printed:\n%s", all)
		}
	}
	return res
}

func (r *applyRig) writes() int { return len(r.acct.Writes()) }

// creates counts the POSTs to a path ending in suffix, across every run so far.
func (r *applyRig) creates(suffix string) int {
	n := 0
	for _, w := range r.acct.Writes() {
		if w.Method == http.MethodPost && strings.HasSuffix(w.Path, suffix) {
			n++
		}
	}
	return n
}

func (r *applyRig) file(rel string) string {
	r.t.Helper()
	raw, err := os.ReadFile(filepath.Join(r.dir, rel))
	if err != nil {
		r.t.Fatal(err)
	}
	return string(raw)
}

func (r *applyRig) isSecretFromCloudflare(value string) bool {
	for _, s := range r.acct.Secrets() {
		if strings.Contains(value, s) {
			return true
		}
	}
	return false
}

// requireSecretFile checks the file holds a value Cloudflare handed out, with the
// container's owner and mode 0400.
func (r *applyRig) requireSecretFile(rel string, ids hostfiles.IDs) {
	r.t.Helper()
	path := filepath.Join(r.dir, rel)
	info, err := os.Lstat(path)
	if err != nil {
		r.t.Fatalf("%s: %v", rel, err)
	}
	if info.Mode().Perm() != 0o400 && filepath.Separator == '/' {
		r.t.Errorf("%s has mode %04o, want 0400", rel, info.Mode().Perm())
	}
	got, err := r.owner.Owner(path)
	if err != nil || got != ids {
		r.t.Errorf("%s is owned by %v (%v), want %v", rel, got, err, ids)
	}
}

// inScopeCreates are the dry run's create lines this version still leaves: the backup
// Worker, D1 and the backup Access pieces come in a later release.
var outOfScopeCreate = regexp.MustCompile(`^  create +(D1 database|D1 migrations|Worker|Worker secret|Worker custom domain|Access application +Dashboard backups|Service token +dashboard-backups-host)\b`)

func TestApplyFreshAccountWithYesThenDryRunHasNothingToCreate(t *testing.T) {
	r := newApplyRig(t, cffake.Fresh())
	res := r.run(append([]string{"--yes"}, groupArgs...)...)
	if res.code != 0 {
		t.Fatalf("exit %d\n%s\n%s", res.code, res.stdout, res.stderr)
	}
	for _, want := range []string{
		"Created in Cloudflare:",
		"create tunnel officer-dashboard and keep its token",
		"Secrets kept on the host (paths only):",
		"secrets/tunnel-token",
		"secrets/admin/services-access-client-id",
		"secrets/admin/services-access-client-secret",
		"Left for you:",
		"Global session duration",
		"Agents and services: bypass cache",
		"Bot Fight Mode",
	} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, res.stdout)
		}
	}
	if strings.Contains(res.stdout, "it has no host file") {
		t.Error("the report says the services secret has no host file right after storing it")
	}
	r.requireSecretFile("secrets/tunnel-token", hostfiles.TunnelIDs)
	r.requireSecretFile("secrets/admin/services-access-client-id", hostfiles.Root)
	r.requireSecretFile("secrets/admin/services-access-client-secret", hostfiles.Root)
	if !strings.HasPrefix(r.file("secrets/tunnel-token"), "eyJ") || !r.isSecretFromCloudflare(r.file("secrets/tunnel-token")) {
		t.Error("secrets/tunnel-token does not hold the tunnel token Cloudflare returned")
	}
	if !r.isSecretFromCloudflare(r.file("secrets/admin/services-access-client-secret")) {
		t.Error("the services client secret file does not hold the secret Cloudflare returned")
	}
	if !strings.HasSuffix(r.file("secrets/admin/services-access-client-id"), ".access") {
		t.Errorf("client ID file holds %q", r.file("secrets/admin/services-access-client-id"))
	}

	writes := r.writes()
	dry := r.run("--dry-run")
	if dry.code != 0 || r.writes() != writes {
		t.Fatalf("dry run: exit %d, %d new writes\n%s", dry.code, r.writes()-writes, dry.stderr)
	}
	for _, line := range strings.Split(dry.stdout, "\n") {
		if strings.HasPrefix(line, "  create ") && !outOfScopeCreate.MatchString(line) {
			t.Errorf("the dry run after apply still wants: %s", line)
		}
		if strings.HasPrefix(line, "  drift ") {
			t.Errorf("drift after apply: %s", line)
		}
	}
	// A second apply finds nothing to do and needs no confirmation.
	again := r.run()
	if again.code != 0 || r.writes() != writes || !strings.Contains(again.stdout, "Nothing to create") {
		t.Fatalf("second apply: exit %d, %d writes\n%s\n%s", again.code, r.writes()-writes, again.stdout, again.stderr)
	}
}

func TestDryRunNeverBuildsTheWriteEngine(t *testing.T) {
	r := newApplyRig(t, cffake.Fresh())
	if res := r.run("--dry-run"); res.code != 0 {
		t.Fatalf("exit %d: %s", res.code, res.stderr)
	}
	if r.engines != 0 {
		t.Fatal("the dry run constructed the apply engine (and its write client)")
	}
}

func TestApplyWithoutConfirmationAndNoTerminalWritesNothing(t *testing.T) {
	r := newApplyRig(t, cffake.Fresh())
	res := r.run(groupArgs...)
	if res.code == 0 || r.writes() != 0 {
		t.Fatalf("exit %d, %d writes", res.code, r.writes())
	}
	if !strings.Contains(res.stderr, "--yes") || !strings.Contains(res.stderr, "nothing was changed") {
		t.Fatalf("stderr: %s", res.stderr)
	}
	// The plan was shown, so the admin can review it before passing --yes.
	if !strings.Contains(res.stdout, "Cloudflare apply plan for "+cffake.Host) {
		t.Fatalf("no plan printed:\n%s", res.stdout)
	}
}

func TestApplyAtATerminalNeedsTheHostnameTyped(t *testing.T) {
	r := newApplyRig(t, cffake.Fresh())
	r.tty = true
	for _, typed := range []string{"", "yes\n", "y\n", "officers.example.org.evil\n"} {
		r.stdin = strings.NewReader(typed)
		res := r.run(groupArgs...)
		if res.code == 0 || r.writes() != 0 {
			t.Fatalf("typed %q: exit %d, %d writes", typed, res.code, r.writes())
		}
		if !strings.Contains(res.stderr, "Type the dashboard hostname") || !strings.Contains(res.stderr, "nothing was changed") {
			t.Fatalf("typed %q: %s", typed, res.stderr)
		}
	}
	r.stdin = strings.NewReader(cffake.Host + "\n")
	if res := r.run(groupArgs...); res.code != 0 || r.writes() == 0 {
		t.Fatalf("typed the hostname: exit %d, %d writes\n%s", res.code, r.writes(), res.stderr)
	}
}

func TestApplyNeedsTheHostPhaseFirst(t *testing.T) {
	r := newApplyRig(t, cffake.Fresh())
	r.dir = deployDir(t) // templates only: `setup host --yes` never ran here
	res := r.run(append([]string{"--yes"}, groupArgs...)...)
	if res.code == 0 || len(r.acct.Requests()) != 0 {
		t.Fatalf("exit %d, %d requests", res.code, len(r.acct.Requests()))
	}
	if !strings.Contains(res.stderr, "scripts/setup host --yes") {
		t.Fatalf("stderr: %s", res.stderr)
	}
}

// The person confirmed one plan; if Cloudflare changes while they read it, the run
// stops before the first write.
func TestApplyStopsWhenTheAccountChangedAfterThePlanWasShown(t *testing.T) {
	r := newApplyRig(t, cffake.Fresh())
	r.tty = true
	r.stdin = &changeThenType{r: r, typed: cffake.Host + "\n"}
	res := r.run(groupArgs...)
	if res.code == 0 || !strings.Contains(res.stderr, "changed since the preview") {
		t.Fatalf("exit %d\n%s", res.code, res.stderr)
	}
	if r.writes() != 1 { // the test's own group create
		t.Fatalf("%d writes, want only the test's own", r.writes())
	}
}

// changeThenType creates the Officers group behind setup's back, then types.
type changeThenType struct {
	r     *applyRig
	typed string
	done  bool
}

func (c *changeThenType) Read(p []byte) (int, error) {
	if !c.done {
		c.done = true
		body := `{"name":"Officers","include":[{"email":{"email":"someone@example.org"}}]}`
		resp, err := http.Post(c.r.acct.URL+"/accounts/"+cffake.AccountID+"/access/groups", "application/json", strings.NewReader(body))
		if err != nil {
			c.r.t.Fatal(err)
		}
		resp.Body.Close()
	}
	if c.typed == "" {
		return 0, io.EOF
	}
	n := copy(p, c.typed)
	c.typed = c.typed[n:]
	return n, nil
}

func TestMissingPermissionReachesTheAdminIntact(t *testing.T) {
	r := newApplyRig(t, cffake.Fresh())
	r.acct.Inject(cffake.Injection{Method: http.MethodPost, Path: "/accounts/" + cffake.AccountID + "/access/groups", Status: http.StatusUnauthorized, Code: 1001, Message: "Not authorized"})
	res := r.run(append([]string{"--yes"}, groupArgs...)...)
	if res.code == 0 {
		t.Fatal("exit 0")
	}
	for _, want := range []string{
		`the setup token lacks the "Access: Groups Edit" permission: Cloudflare refused POST /accounts/acct-1/access/groups`,
		"Add it to the token and run setup again",
		"An apply token needs",
	} {
		if !strings.Contains(res.stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, res.stderr)
		}
	}
}

// failingOwner refuses to give a file to one owner, as a full disk or a broken mount
// refuses a write: the sink must then report the secret as not stored.
type failingOwner struct {
	*memOwner
	refuse hostfiles.IDs
}

func (o failingOwner) Chown(path string, ids hostfiles.IDs) error {
	if ids == o.refuse {
		return &fs.PathError{Op: "lchown", Path: path, Err: errors.New("operation not permitted")}
	}
	return o.memOwner.Chown(path, ids)
}

func TestSinkFailureStopsTheRunAndARerunRecoversWithoutADuplicate(t *testing.T) {
	r := newApplyRig(t, cffake.Fresh())
	mem := r.owner.(*memOwner)
	r.owner = failingOwner{mem, hostfiles.TunnelIDs}
	res := r.run(append([]string{"--yes"}, groupArgs...)...)
	if res.code == 0 || !strings.Contains(res.stderr, "could not keep the tunnel-token") {
		t.Fatalf("exit %d\n%s", res.code, res.stderr)
	}
	if r.creates("/cfd_tunnel") != 1 || r.creates("/dns_records") != 0 {
		t.Fatalf("tunnel creates %d, DNS creates %d", r.creates("/cfd_tunnel"), r.creates("/dns_records"))
	}
	if _, err := os.Lstat(filepath.Join(r.dir, "secrets/tunnel-token")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("secrets/tunnel-token after a failed store: %v", err)
	}

	r.owner = mem
	again := r.run(append([]string{"--yes"}, groupArgs...)...)
	if again.code != 0 {
		t.Fatalf("re-run: exit %d\n%s\n%s", again.code, again.stdout, again.stderr)
	}
	if !strings.Contains(again.stdout, "fetch the token of tunnel") {
		t.Errorf("the re-run did not fetch the token Cloudflare still has:\n%s", again.stdout)
	}
	if r.creates("/cfd_tunnel") != 1 || r.creates("/access/service_tokens") != 1 || r.creates("/dns_records") != 1 {
		t.Fatalf("duplicates: tunnel %d, service token %d, DNS %d", r.creates("/cfd_tunnel"), r.creates("/access/service_tokens"), r.creates("/dns_records"))
	}
	r.requireSecretFile("secrets/tunnel-token", hostfiles.TunnelIDs)
}

// An existing secret file is never replaced. The new value is kept aside, the run stops
// before anything else, later runs refuse to start until the admin decides, and then the
// run completes without a second service token.
func TestExistingSecretFileIsKeptAndTheRunStops(t *testing.T) {
	r := newApplyRig(t, cffake.Fresh())
	old := "old-services-secret-" + strings.Repeat("x", 12)
	main := filepath.Join(r.dir, "secrets/admin/services-access-client-secret")
	if err := os.MkdirAll(filepath.Dir(main), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(main, []byte(old), 0o400); err != nil {
		t.Fatal(err)
	}
	res := r.run(append([]string{"--yes"}, groupArgs...)...)
	if res.code == 0 {
		t.Fatalf("exit 0\n%s", res.stdout)
	}
	aside := regexp.MustCompile(`secrets/admin/services-access-client-secret\.\d{8}T\d{6}\.\d{9}Z`).FindString(res.stderr)
	if aside == "" || !strings.Contains(res.stderr, "not where the install reads it") || strings.Contains(res.stderr, "now lost") {
		t.Fatalf("stderr: %s", res.stderr)
	}
	if got := r.file("secrets/admin/services-access-client-secret"); got != old {
		t.Fatal("the existing secret file was replaced")
	}
	if !r.isSecretFromCloudflare(r.file(aside)) {
		t.Fatal("the set-aside file does not hold the new secret")
	}
	if r.creates("/access/apps") != 0 || r.creates("/cfd_tunnel") != 0 {
		t.Fatal("the run went on after the secret was kept aside")
	}

	writes := r.writes()
	blocked := r.run(append([]string{"--yes"}, groupArgs...)...)
	if blocked.code == 0 || r.writes() != writes || !strings.Contains(blocked.stderr, aside) {
		t.Fatalf("re-run with the aside file: exit %d, %d writes\n%s", blocked.code, r.writes()-writes, blocked.stderr)
	}

	// The admin keeps the new value.
	if err := os.Rename(filepath.Join(r.dir, aside), main); err != nil {
		t.Fatal(err)
	}
	done := r.run(append([]string{"--yes"}, groupArgs...)...)
	if done.code != 0 || r.creates("/access/service_tokens") != 1 {
		t.Fatalf("exit %d, %d service tokens\n%s", done.code, r.creates("/access/service_tokens"), done.stderr)
	}
}

// A run interrupted right after a create (the response never arrives) is re-run through
// the command: it matches what the first run made and creates no second copy.
func TestInterruptedAfterACreateThenRerunMakesNoDuplicate(t *testing.T) {
	for _, suffix := range []string{"/access/service_tokens", "/cfd_tunnel"} {
		t.Run(strings.TrimPrefix(suffix, "/"), func(t *testing.T) {
			r := newApplyRig(t, cffake.Fresh())
			ctx, cancel := context.WithCancel(context.Background())
			r.ctx = ctx
			r.acct.OnWrite(func(req cffake.Request) {
				if req.Method == http.MethodPost && strings.HasSuffix(req.Path, suffix) {
					cancel()
				}
			})
			if res := r.run(append([]string{"--yes"}, groupArgs...)...); res.code == 0 {
				t.Fatal("the interrupted run exited 0")
			}
			r.acct.OnWrite(nil)
			r.ctx = context.Background()
			res := r.run(append([]string{"--yes"}, groupArgs...)...)
			if res.code != 0 {
				t.Fatalf("re-run: exit %d\n%s\n%s", res.code, res.stdout, res.stderr)
			}
			if n := r.creates(suffix); n != 1 {
				t.Fatalf("%d creates of %s", n, suffix)
			}
			if suffix == "/access/service_tokens" && !strings.Contains(res.stdout, "Rotate") {
				t.Errorf("the lost client secret is not reported as a manual rotation:\n%s", res.stdout)
			}
			if suffix == "/cfd_tunnel" {
				r.requireSecretFile("secrets/tunnel-token", hostfiles.TunnelIDs)
			}
		})
	}
}

func TestApplyFlagsAreUsageErrorsWithDryRun(t *testing.T) {
	r := newApplyRig(t, cffake.Fresh())
	for _, args := range [][]string{{"--dry-run", "--yes"}} {
		if res := r.run(args...); res.code != 2 {
			t.Errorf("%v: exit %d, want 2", args, res.code)
		}
	}
}

// Every permission the engine names when Cloudflare refuses a request is in the list
// the command prints for an apply token.
func TestApplyTokenPermissionsCoverWhatTheEngineCalls(t *testing.T) {
	quoted := regexp.MustCompile(`"((?:Access: )?[A-Z][A-Za-z ]+ (?:Edit|Read))"`)
	found := map[string]bool{}
	for _, name := range []string{"apply.go", "discover.go"} {
		raw, err := os.ReadFile(filepath.Join("cfprovision", name))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range quoted.FindAllStringSubmatch(string(raw), -1) {
			found[m[1]] = true
		}
	}
	if len(found) < 10 {
		t.Fatalf("found only %v; the scan is broken", found)
	}
	for perm := range found {
		if !strings.Contains(ApplyPermissions, perm) && !strings.Contains(ApplyPermissions, strings.TrimSuffix(perm, " Read")+" Edit") {
			t.Errorf("the engine names %q, which the apply permission list lacks", perm)
		}
	}
}
