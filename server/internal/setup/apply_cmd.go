package setup

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"officerdashboard/internal/setup/cfapi"
	"officerdashboard/internal/setup/cfprovision"
	"officerdashboard/internal/setup/hostfiles"
	"officerdashboard/internal/setup/plan"
)

// ApplyPermissions is what the API token of an apply run needs, in the names the
// Cloudflare token form shows (an Edit permission includes its Read). It lists every
// permission the engine names when Cloudflare refuses one of its requests
// (cfprovision/apply.go; TestApplyTokenPermissionsCoverWhatTheEngineCalls keeps them in
// step) and the reads discovery makes, as in the spike report's S4 table. Limit the token
// to your account and the dashboard's zone, give it a short expiry, and delete it after.
const ApplyPermissions = `Account (your account only):
  Cloudflare Tunnel Edit            create the tunnel, fetch its token, route the hostname
  Access: Apps Edit                 the three Access applications
  Access: Policies Edit             their reusable policies
  Access: Groups Edit               the Officers and Launchpad groups
  Access: Identity Providers Edit   the One-time PIN login
  Access: Service Tokens Edit       the "officer-dashboard services" token
  Access: Organizations Read        the team domain and global session
                                    (Access: Organizations Edit only with --allow-org-session-write)
  D1 Read, Workers Scripts Read     read only: this version does not create the backup Worker
Zone (the dashboard's zone only):
  Zone Read                         find the zone and its plan
  DNS Edit                          the hostname's CNAME to the tunnel
  Zone WAF Edit                     the skip rule for agents and services
  Cache Rules Edit                  the cache bypass rule
  Config Rules Edit                 the consent page rule
  Bot Management Read               Bot Fight Mode
                                    (Bot Management Edit only with --allow-bot-fight-mode-off)`

// applyArgs are the flags only an apply run reads.
type applyArgs struct {
	dir                  string
	yes                  bool
	officers, launchpad  []string
	allowBotFightModeOff bool
	allowOrgSessionWrite bool
	tunnelTokenFileSet   bool
}

// NewEngineFunc builds the apply engine; tests point it at the fake API.
type NewEngineFunc func(token string, d plan.Desired, sink cfprovision.SecretSink, out io.Writer, opt cfprovision.Options) *cfprovision.Engine

func defaultNewEngine(token string, d plan.Desired, sink cfprovision.SecretSink, out io.Writer, opt cfprovision.Options) *cfprovision.Engine {
	return cfprovision.NewEngine(token, d, sink, out, opt)
}

// runApply is `dashboard setup` without --dry-run: it checks the host phase ran, reads the
// account, prints the plan, asks, and applies exactly that plan. Exit codes: 0 when every
// step was created and read back ok (or there was nothing to create), 1 when it stopped
// or was not confirmed, 2 for usage.
func runApply(ctx context.Context, in plan.Inputs, a applyArgs, deps Deps) int {
	failf := func(format string, args ...any) int {
		fmt.Fprintf(deps.Stderr, "setup: "+format+"\n", args...)
		return 1
	}
	if a.tunnelTokenFileSet {
		fmt.Fprintln(deps.Stderr, "setup: --tunnel-token-file is for --dry-run; an apply reads the tunnel ID from secrets/tunnel-token in --deploy-dir, the file it writes")
		return 2
	}
	// 1. The host phase comes first: it creates the env files and the secret directories
	// with their owners, so the first secret Cloudflare hands out has a place to go.
	envFile, err := hostReady(a.dir)
	if err != nil {
		return failf("%v", err)
	}
	envApp, _ := envFile.Get("APP_URL")
	if strings.TrimSpace(in.AppURL) == "" {
		in.AppURL = envApp
	}
	desired, err := plan.NewDesired(in)
	if err != nil {
		fmt.Fprintln(deps.Stderr, "setup:", err)
		return 2
	}
	if mine, err := plan.NewDesired(plan.Inputs{AppURL: envApp}); err != nil || mine.Hostname != desired.Hostname {
		return failf("the APP_URL in %s is %q, not https://%s: setup writes this install's secrets for its own hostname only", filepath.Join(a.dir, ".env"), envApp, desired.Hostname)
	}
	tunnelFile := filepath.Join(a.dir, "secrets", "tunnel-token")
	if _, err := os.Lstat(tunnelFile); err == nil {
		if id, err := tunnelIDFromFile(deps.ReadFile, tunnelFile); err != nil {
			fmt.Fprintf(deps.Stderr, "setup: secrets/tunnel-token: %v; matching the tunnel by name\n", err)
		} else if in.TunnelID != "" && !strings.EqualFold(in.TunnelID, id) {
			return failf("--tunnel-id %s disagrees with secrets/tunnel-token, which names tunnel %s; setup will not pick one. "+
				"Leave out --tunnel-id to use the host's token, or move that file aside if it belongs to another tunnel", in.TunnelID, id)
		} else {
			in.TunnelID = id
			if desired, err = plan.NewDesired(in); err != nil {
				fmt.Fprintln(deps.Stderr, "setup:", err)
				return 2
			}
		}
	}

	token, err := readToken(deps)
	if err != nil {
		return failf("%v", err)
	}
	owner := deps.Owner
	if owner == nil {
		owner = hostfiles.SystemOwner()
	}
	sink := newHostSecretSink(a.dir, owner, servicesTokenName(desired))
	newEngine := deps.NewEngine
	if newEngine == nil {
		newEngine = defaultNewEngine
	}
	eng := newEngine(token, desired, sink, deps.Stdout, cfprovision.Options{
		GroupEmails:          map[string][]string{plan.GroupOfficers: a.officers, plan.GroupLaunchpad: a.launchpad},
		AllowBotFightModeOff: a.allowBotFightModeOff,
		AllowOrgSessionWrite: a.allowOrgSessionWrite,
		StateDir:             a.dir,
	})
	stopped := func(err error) int {
		msg := scrub(err.Error(), token)
		fmt.Fprintln(deps.Stderr, "setup:", msg)
		if needsPermission(err, msg) {
			fmt.Fprintf(deps.Stderr, "\nAn apply token needs these permissions:\n%s\n", ApplyPermissions)
		}
		return 1
	}

	// 2. The plan the person confirms.
	pctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	reviewed, err := eng.Preview(pctx)
	cancel()
	if err != nil {
		return stopped(err)
	}
	if err := (plan.Report{Hostname: desired.Hostname, Actions: reviewed.Actions}).WriteText(deps.Stdout); err != nil {
		return failf("%v", err)
	}
	fmt.Fprintln(deps.Stdout)
	if err := reviewed.Plan.WriteText(deps.Stdout); err != nil {
		return failf("%v", err)
	}
	if plan.PrefixConflict(desired, reviewed.Actions) {
		return failf("--name-prefix %q found resources without that prefix on %s (the drift lines say which): this hostname belongs to another install. "+
			"A staging install needs its own APP_URL as well as a prefix. Nothing was changed", desired.NamePrefix, desired.Hostname)
	}
	if len(reviewed.Plan.Blockers) > 0 {
		for _, b := range reviewed.Plan.Blockers {
			if strings.Contains(b, "the setup token lacks") {
				fmt.Fprintf(deps.Stderr, "An apply token needs these permissions:\n%s\n", ApplyPermissions)
				break
			}
		}
		return failf("nothing was changed")
	}
	if len(reviewed.Plan.Steps) == 0 {
		left := writeApplyReport(deps.Stdout, cfprovision.Outputs{Final: reviewed.Actions}, reviewed, sink)
		return finish(deps, desired.Hostname, left, sink)
	}

	// 3. Confirmation: --yes, or the hostname typed at a terminal.
	if !a.yes {
		if !deps.IsTerminal(deps.StdinFD) {
			return failf("no --yes and no terminal to confirm on, so nothing was changed. Review the plan above, then run again with --yes")
		}
		fmt.Fprintf(deps.Stderr, "\nSetup will make the %d changes above in Cloudflare; it never undoes them.\nType the dashboard hostname (%s) to go ahead, or anything else to stop: ",
			len(reviewed.Plan.Steps), desired.Hostname)
		var line string
		if deps.Stdin != nil {
			line, _ = bufio.NewReader(deps.Stdin).ReadString('\n')
		}
		if strings.TrimSpace(line) != desired.Hostname {
			return failf("not confirmed, so nothing was changed")
		}
	}

	// 4. Apply exactly the confirmed plan: the engine discovers again and stops before the
	// first write if the plan lines, steps or warnings are not the ones shown.
	actx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	out, err := eng.ApplyReviewed(actx, reviewed)
	left := writeApplyReport(deps.Stdout, out, reviewed, sink)
	if err != nil {
		code := stopped(err)
		fmt.Fprintln(deps.Stderr, "setup: Setup stopped. Do not start cloudflared yet: fix the cause above and run setup again; it reads the account first and continues where it stopped.")
		return code
	}
	return finish(deps, desired.Hostname, left, sink)
}

// finish prints the closing lines of a run whose steps all read back ok (or that had none)
// and returns its exit code: 1 while a kept secret lacks a file, else 0. It tells the
// admin to start the stack only when nothing left concerns Access or the hostname.
func finish(deps Deps, hostname string, left leftover, sink *hostSecretSink) int {
	if len(sink.missing) > 0 {
		fmt.Fprintf(deps.Stderr, "setup: a secret is only partly on the host (the \"missing\" line above); setup exits 1 until the file is there\n")
		return 1
	}
	except := ""
	if left.items > 0 {
		except = ", except the items left for you above"
	}
	fmt.Fprintf(deps.Stdout, "\nCloudflare is set up for %s: every step read back ok%s.\n", hostname, except)
	if len(left.blocking) > 0 {
		fmt.Fprintf(deps.Stdout, "Do not start the stack yet: these lines above concern Access or the hostname, so resolve them first (docs/deploy/cloudflare.md): %s.\n",
			strings.Join(left.blocking, "; "))
		return 0
	}
	fmt.Fprintln(deps.Stdout, "Next: check the two items above in the dashboard, run `scripts/setup host --dry-run`, then start the stack (docs/deploy/fresh-install.md).")
	return 0
}

// leftover is what the report listed for the admin: how many items, and the drift or
// unknowable lines about Access or the hostname that must be resolved before the stack
// starts.
type leftover struct {
	items    int
	blocking []string
}

// guardsTheHostname reports whether a plan line is part of what protects or publishes
// the dashboard hostname. Secrets, the backup pieces and zone settings are not.
func guardsTheHostname(id string) bool {
	if strings.HasSuffix(id, ".secret") {
		return false
	}
	for _, p := range []string{"zero_trust", "tunnel", "dns", "access.idp", "access.group.",
		"access.app." + plan.AppSite, "access.app." + plan.AppAgents, "access.app." + plan.AppServices, "access.service_token." + plan.TokenServices} {
		if id == p || strings.HasPrefix(id, p+".") || (strings.HasSuffix(p, ".") && strings.HasPrefix(id, p)) {
			return true
		}
	}
	return false
}

// hostReady checks that `setup host --yes` ran in dir and returns its .env: the apply
// writes the tunnel token where .env's TUNNEL_TOKEN_FILE points and into the secret
// directories that phase creates with their owners.
func hostReady(dir string) (*hostfiles.EnvFile, error) {
	first := "run `scripts/setup host --yes` first (docs/deploy/fresh-install.md): it writes the env files and the secret directories the Cloudflare apply writes into"
	raw, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		return nil, fmt.Errorf("%s has no .env (%v); %s", dir, unwrapPath(err), first)
	}
	env := hostfiles.ParseEnv(raw)
	tunnel, _ := hostfiles.LookupSecret("tunnel-token")
	if v, _ := env.Get(tunnel.EnvKey); strings.TrimSpace(v) != tunnel.EnvValue {
		return nil, fmt.Errorf("%s in .env is %q; setup keeps the tunnel token only in %s (fresh-install.md step 5). Set it to that, or leave the tunnel to the manual steps",
			tunnel.EnvKey, v, tunnel.EnvValue)
	}
	for _, d := range hostfiles.Dirs {
		info, err := os.Lstat(filepath.Join(dir, d.Path))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return nil, fmt.Errorf("%s is missing; %s", d.Path, first)
		case err != nil:
			return nil, fmt.Errorf("%s: %v", d.Path, unwrapPath(err))
		case !info.IsDir():
			return nil, fmt.Errorf("%s is not a directory (a symlink or a file); setup writes secrets only into real directories", d.Path)
		}
	}
	return env, nil
}

func needsPermission(err error, msg string) bool {
	var pe *cfprovision.PermissionError
	return errors.As(err, &pe) || strings.Contains(msg, "the setup token lacks") || cfapi.IsForbidden(err)
}

// writeApplyReport prints what the run created, where the secrets are, what is left for
// the admin, and the two settings setup cannot read back.
func writeApplyReport(w io.Writer, out cfprovision.Outputs, reviewed cfprovision.Reviewed, sink *hostSecretSink) leftover {
	var left leftover
	var b strings.Builder
	b.WriteString("\nCreated in Cloudflare:\n")
	if len(out.Done) == 0 {
		b.WriteString("  nothing\n")
	}
	for _, d := range out.Done {
		fmt.Fprintf(&b, "  - %s", d.Step)
		if d.ID != "" {
			fmt.Fprintf(&b, "  [%s]", d.ID)
		}
		b.WriteByte('\n')
	}
	b.WriteString("\nSecrets kept on the host (paths only):\n")
	if len(sink.kept) == 0 {
		b.WriteString("  none\n")
	}
	for _, k := range sink.kept {
		fmt.Fprintf(&b, "  - %s\n", k)
	}
	final := out.Final
	if len(final) == 0 {
		final = reviewed.Actions // the run stopped: what the preview showed is still the best list
	}
	b.WriteString("\nLeft for you:\n")
	n := 0
	for _, m := range sink.missing {
		fmt.Fprintf(&b, "  - missing: %s\n", m)
		n++
	}
	for _, m := range reviewed.Plan.Manual {
		fmt.Fprintf(&b, "  - manual: %s\n", m)
		n++
	}
	for _, a := range final {
		if a.Kind != plan.Drift && a.Kind != plan.Unknowable {
			continue
		}
		// The plan cannot see host files: its line says an apply reports the services
		// secret. When the sink holds it, the secrets list above already says where it is.
		if a.ID == "access.service_token."+plan.TokenServices+".secret" && sink.holds[cfprovision.SecretServicesToken] {
			continue
		}
		fmt.Fprintf(&b, "  - %s: %s %s", a.Kind, a.Resource, a.Name)
		if a.ExistingID != "" {
			fmt.Fprintf(&b, " [%s]", a.ExistingID)
		}
		b.WriteByte('\n')
		for _, d := range a.Detail {
			fmt.Fprintf(&b, "      %s\n", d)
		}
		n++
		if guardsTheHostname(a.ID) {
			left.blocking = append(left.blocking, fmt.Sprintf("%s %s %s", a.Kind, a.Resource, a.Name))
		}
	}
	for _, s := range reviewed.Plan.Skipped {
		fmt.Fprintf(&b, "  - skipped: %s\n", s)
		n++
	}
	if n == 0 {
		b.WriteString("  nothing\n")
	}
	b.WriteString("\nCheck these in the Cloudflare dashboard; setup cannot read them back:\n" +
		"  - the cache rule \"Agents and services: bypass cache\" (your domain → Caching → Cache Rules) must say Bypass cache.\n" +
		"    Setup sends cache: false, which no run against real Cloudflare has confirmed yet.\n" +
		"  - Bot Fight Mode (your domain → Security → Settings → Bot traffic) must be Off, or bots and agents are challenged\n" +
		"    (docs/deploy/cloudflare.md step 6.1). It is zone-wide, so setup leaves it unless --allow-bot-fight-mode-off is given.\n")
	_, _ = io.WriteString(w, b.String())
	left.items = n
	return left
}

func servicesTokenName(d plan.Desired) string {
	for _, t := range d.ServiceTokens {
		if t.Key == plan.TokenServices {
			return t.Name
		}
	}
	return plan.TokenServices
}
