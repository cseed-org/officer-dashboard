// Package setup is the `dashboard setup` command.
//
//   - `dashboard setup --dry-run` is the read-only Cloudflare preview: it reads a
//     Cloudflare account, compares it with what docs/deploy/cloudflare.md and
//     docs/deploy/fresh-install.md describe, and prints the difference. It writes
//     nothing, locally or remotely.
//   - `dashboard setup` without --dry-run applies: it prints the same reading as a plan
//     of steps, and creates the missing pieces through the cfprovision engine only once
//     the admin confirms (--yes, or the hostname typed at a terminal). The secrets
//     Cloudflare hands out go to the deploy directory's host files (apply_sink.go).
//   - `dashboard setup host` plans the deployment's host files (env files, secret
//     directories and files) and writes them only with --yes. It has no network code.
//
// scripts/setup runs them from the pinned image on the host, host first.
package setup

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"golang.org/x/term"

	"officerdashboard/internal/setup/cfapi"
	"officerdashboard/internal/setup/cfprovision"
	"officerdashboard/internal/setup/discord"
	"officerdashboard/internal/setup/hostfiles"
	"officerdashboard/internal/setup/plan"
)

// TokenFileEnv names the file holding the Cloudflare API token used by setup.
const TokenFileEnv = "CLOUDFLARE_SETUP_TOKEN_FILE"

const usage = `usage: dashboard setup --dry-run [flags]
       dashboard setup [--yes] --deploy-dir DIR [flags]
       dashboard setup host [--dry-run | --yes] [flags]   (see dashboard setup host -h)
       dashboard setup discord [connect] [flags]          (see dashboard setup discord -h)

With --dry-run, reads your Cloudflare account and prints what the dashboard still
needs. Nothing is changed. Run it first.

Without --dry-run, it applies: it reads the account again, prints the plan, and creates
the missing tunnel, DNS record, Access pieces and rules only once you confirm, by typing
the dashboard hostname at a terminal, or with --yes. Without a terminal and without
--yes it changes nothing and exits 1. It writes the secrets Cloudflare hands out into
--deploy-dir, so run ` + "`dashboard setup host --yes`" + ` there first. It never deletes, never
changes what exists, and never starts a service.

The Cloudflare API token is read from the file named by CLOUDFLARE_SETUP_TOKEN_FILE,
or asked for without echo when that is unset. It is never accepted as a flag and never
printed. A dry run needs only read permissions; an apply needs:
` + ApplyPermissions + `

Flags:
`

// Deps are the command's connections to the outside, replaceable in tests.
type Deps struct {
	Getenv       func(string) string
	Stdout       io.Writer
	Stderr       io.Writer
	ReadFile     func(string) ([]byte, error)
	StdinFD      int
	IsTerminal   func(fd int) bool
	ReadPassword func(fd int) ([]byte, error)
	NewReader    func(token string) cfapi.Reader
	// NewEngine builds the apply engine (nil: the real one). Only an apply calls it, so
	// a dry run never constructs the write client.
	NewEngine NewEngineFunc
	// Used by `setup host` and by an apply (the confirmation and the secret files).
	Stdin  io.Reader
	Owner  hostfiles.Owner // nil: the real chown and stat
	Random io.Reader       // nil: crypto/rand
	// Used by `setup discord` (nil: the real Discord API).
	Discord *discord.Client
	Now     func() time.Time // the clock for setup link expiry (nil: time.Now)
}

// Main runs the command with the real process environment and terminal.
func Main(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	return Run(context.Background(), args, Deps{
		Getenv:       getenv,
		Stdout:       stdout,
		Stderr:       stderr,
		ReadFile:     os.ReadFile,
		StdinFD:      int(os.Stdin.Fd()),
		IsTerminal:   term.IsTerminal,
		ReadPassword: term.ReadPassword,
		NewReader:    func(token string) cfapi.Reader { return cfapi.New(token) },
		NewEngine:    defaultNewEngine,
		Stdin:        os.Stdin,
	})
}

// Run parses args and runs the preview. Exit codes: 0 done, 1 failed, 2 usage.
func Run(ctx context.Context, args []string, deps Deps) int {
	if len(args) > 0 && args[0] == "host" {
		return runHost(ctx, args[1:], deps)
	}
	if len(args) > 0 && args[0] == "discord" {
		return runDiscord(ctx, args[1:], deps)
	}
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(deps.Stderr)
	fs.Usage = func() {
		fmt.Fprint(deps.Stderr, usage)
		fs.PrintDefaults()
	}
	var in plan.Inputs
	var dryRun, asJSON bool
	var tunnelTokenFile string
	fs.BoolVar(&dryRun, "dry-run", false, "only read and report; changes nothing. Without it, setup applies after you confirm")
	fs.BoolVar(&asJSON, "json", false, "print the plan as JSON")
	fs.StringVar(&in.AppURL, "app-url", deps.Getenv("APP_URL"), "dashboard origin, https://<hostname> (APP_URL)")
	fs.StringVar(&in.BackupHostname, "backup-hostname", backupHostDefault(deps.Getenv("BACKUP_SINK_URL")), "backup Worker hostname (default: host of BACKUP_SINK_URL, else backups.<parent of the APP_URL host>)")
	fs.StringVar(&in.OfficersGroup, "officers-group", "Officers", "Access group of officers")
	fs.StringVar(&in.LaunchpadGroup, "launchpad-group", "Launchpad", "Access group of Launchpad builders and mentors; empty for none")
	fs.StringVar(&in.NamePrefix, "name-prefix", "", "prefix for every resource name, for a staging install beside the real one")
	fs.StringVar(&in.TunnelID, "tunnel-id", "", "match the tunnel by this ID before its name")
	fs.StringVar(&tunnelTokenFile, "tunnel-token-file", deps.Getenv("TUNNEL_TOKEN_FILE"), "read the tunnel ID from this tunnel token file (TUNNEL_TOKEN_FILE); only the ID is used. --dry-run only")
	var ap applyArgs
	fs.BoolVar(&ap.yes, "yes", false, "apply without asking (the plan is still printed first)")
	fs.StringVar(&ap.dir, "deploy-dir", ".", "apply: the deployment directory (server/deploy) where `setup host --yes` ran; secrets and restore records go here")
	fs.Func("officers-email", "apply: a member of the Officers group, when setup creates it (repeatable; include your own)", func(s string) error {
		ap.officers = append(ap.officers, s)
		return nil
	})
	fs.Func("launchpad-email", "apply: a member of the Launchpad group, when setup creates it (repeatable)", func(s string) error {
		ap.launchpad = append(ap.launchpad, s)
		return nil
	})
	fs.BoolVar(&ap.allowBotFightModeOff, "allow-bot-fight-mode-off", false, "apply: turn Bot Fight Mode off for the whole zone (default: print the manual step)")
	fs.BoolVar(&ap.allowOrgSessionWrite, "allow-org-session-write", false, "apply: set the Zero Trust organization's global session duration when unset (default: print the manual step)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(deps.Stderr, "setup: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if dryRun && ap.yes {
		fmt.Fprintln(deps.Stderr, "setup: --dry-run and --yes contradict each other")
		return 2
	}
	if !dryRun && asJSON {
		fmt.Fprintln(deps.Stderr, "setup: --json is for --dry-run; an apply prints its plan and report as text")
		return 2
	}
	if !dryRun {
		fs.Visit(func(f *flag.Flag) { ap.tunnelTokenFileSet = ap.tunnelTokenFileSet || f.Name == "tunnel-token-file" })
		return runApply(ctx, in, ap, deps)
	}
	if in.TunnelID == "" && tunnelTokenFile != "" {
		if id, err := tunnelIDFromFile(deps.ReadFile, tunnelTokenFile); err != nil {
			fmt.Fprintf(deps.Stderr, "setup: %v; matching the tunnel by name\n", err)
		} else {
			in.TunnelID = id
		}
	}
	desired, err := plan.NewDesired(in)
	if err != nil {
		fmt.Fprintln(deps.Stderr, "setup:", err)
		return 2
	}
	if strings.TrimSpace(in.BackupHostname) == "" {
		fmt.Fprintf(deps.Stderr, "setup: no backup hostname given (--backup-hostname, or BACKUP_SINK_URL from backup.env); assuming %s\n", desired.BackupHostname)
	}
	token, err := readToken(deps)
	if err != nil {
		fmt.Fprintln(deps.Stderr, "setup:", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	observed, err := cfprovision.Discover(ctx, deps.NewReader(token), desired)
	if cfapi.IsInvalidToken(err) {
		fmt.Fprintln(deps.Stderr, "setup: Cloudflare does not accept this API token (code 1000): check it was copied whole and is still active")
		return 1
	}
	if err != nil {
		fmt.Fprintln(deps.Stderr, "setup: reading Cloudflare failed:", scrub(err.Error(), token))
		return 1
	}
	report := plan.Report{Hostname: desired.Hostname, DryRun: true, Actions: plan.Diff(desired, observed)}
	if asJSON {
		err = report.WriteJSON(deps.Stdout)
	} else {
		err = report.WriteText(deps.Stdout)
	}
	if err != nil {
		fmt.Fprintln(deps.Stderr, "setup:", err)
		return 1
	}
	if plan.PrefixConflict(desired, report.Actions) {
		fmt.Fprintf(deps.Stderr, "setup: WARNING: --name-prefix %q found resources without that prefix on %s (the drift lines say which).\n"+
			"setup: This hostname belongs to another install. A staging install needs its own APP_URL as well as a prefix.\n", desired.NamePrefix, desired.Hostname)
	}
	return 0
}

func readToken(deps Deps) (string, error) {
	if path := strings.TrimSpace(deps.Getenv(TokenFileEnv)); path != "" {
		raw, err := deps.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("cannot read %s: %w", TokenFileEnv, unwrapPath(err))
		}
		return checkToken(string(raw))
	}
	if !deps.IsTerminal(deps.StdinFD) {
		return "", fmt.Errorf("no Cloudflare API token: set %s to a file holding it, or run in a terminal to type it", TokenFileEnv)
	}
	fmt.Fprint(deps.Stderr, "Cloudflare API token (input hidden): ")
	raw, err := deps.ReadPassword(deps.StdinFD)
	fmt.Fprintln(deps.Stderr)
	if err != nil {
		return "", errors.New("reading the token failed")
	}
	return checkToken(string(raw))
}

var tokenShape = regexp.MustCompile(`^[A-Za-z0-9_.\-]{20,200}$`)

func checkToken(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errors.New("the Cloudflare API token is empty")
	}
	if !tokenShape.MatchString(s) {
		return "", errors.New("that does not look like a Cloudflare API token (expected one line of letters, digits, '-', '_' or '.')")
	}
	return s, nil
}

// unwrapPath drops the path from an *os.PathError, keeping only the reason.
func unwrapPath(err error) error {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}

func scrub(s, token string) string {
	if token != "" {
		s = strings.ReplaceAll(s, token, "[redacted]")
	}
	return s
}

func backupHostDefault(sinkURL string) string {
	s := strings.TrimSpace(sinkURL)
	if s == "" {
		return ""
	}
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	host, _, _ := strings.Cut(s, "/")
	host, _, _ = strings.Cut(host, ":")
	return host
}

var uuidShape = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// tunnelIDFromFile reads a tunnel token file and returns only the tunnel ID in it.
// The token is base64 JSON {"a": account tag, "t": tunnel ID, "s": secret}; this shape is
// what cloudflared accepts, not a documented API, so any surprise falls back to name
// matching. The secret is never kept or printed.
func tunnelIDFromFile(readFile func(string) ([]byte, error), path string) (string, error) {
	raw, err := readFile(path)
	if err != nil {
		return "", fmt.Errorf("cannot read the tunnel token file (%v)", unwrapPath(err))
	}
	tok := strings.TrimSpace(string(raw))
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		b, err := enc.DecodeString(tok)
		if err != nil {
			continue
		}
		var v struct {
			T string `json:"t"`
		}
		if json.Unmarshal(b, &v) == nil && uuidShape.MatchString(v.T) {
			return strings.ToLower(v.T), nil
		}
	}
	return "", errors.New("the tunnel token file does not hold a tunnel token")
}
