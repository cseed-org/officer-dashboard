package setup

import (
	"bufio"
	"bytes"
	"context"
	"crypto/subtle"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/setup/discord"
	"officerdashboard/internal/setup/hostfiles"
	"officerdashboard/internal/setup/plan"
)

const discordUsage = `usage: dashboard setup discord [--dry-run | --yes] [flags]
       dashboard setup discord connect [--dry-run] [flags]

The Discord part of setup. Two steps stay manual, because Discord has no API for them:
create the application in the Discord Developer Portal, and register its redirect URI
(printed below). Then:

  dashboard setup discord          reads the client ID, the client secret and, optionally,
                                   the bot token (hidden at a terminal, or from files),
                                   checks each with Discord, and stores them with --yes.
  dashboard setup discord connect  once the dashboard is running: prints one Discord link.
                                   You approve it in any browser, and the dashboard shows a
                                   setup code to paste back here. Setup then writes your
                                   Discord user ID as the first owner and a notification
                                   webhook. It writes without --yes, since a code works only
                                   once, and it restarts nothing.

Values are never taken from flags, and never printed. Anything already in the host files
is kept and reported; a different value is a conflict, left alone.

Flags:
`

var (
	clientSecretShape = regexp.MustCompile(`^[!-~]{8,256}$`)            // printable, no blanks; Discord checks the rest
	botTokenShape     = regexp.MustCompile(`^[A-Za-z0-9_.\-]{30,200}$`) // three base64url parts joined by '.'
)

func discordClient(deps Deps) *discord.Client {
	if deps.Discord != nil {
		return deps.Discord
	}
	return discord.New()
}

// discordSecret is where one Discord secret stands in the deploy directory.
type discordSecret struct {
	spec      hostfiles.SecretSpec
	value     string // the stored value, when here
	here      bool   // the file setup manages exists
	elsewhere string // the env key points at another file, which the admin manages
}

func storedSecret(dir, name string) (discordSecret, error) {
	spec, _ := hostfiles.LookupSecret(name)
	s := discordSecret{spec: spec}
	if set := realValue(dir, spec.EnvFile, spec.EnvKey); set != "" && set != spec.EnvValue {
		s.elsewhere = set
		return s, nil
	}
	raw, err := os.ReadFile(filepath.Join(dir, spec.Path))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return s, nil
	case err != nil:
		return s, fmt.Errorf("cannot read %s: %v", spec.Path, unwrapPath(err))
	}
	s.here, s.value = true, strings.TrimSpace(string(raw))
	return s, nil
}

// discordAppURL is APP_URL from --app-url, dashboard.env or .env, as an https origin.
func discordAppURL(dir, flagValue string) (string, error) {
	v := flagValue
	if v == "" {
		v = realValue(dir, "dashboard.env", "APP_URL")
	}
	if v == "" {
		v = realValue(dir, ".env", "APP_URL")
	}
	if v == "" {
		return "", errors.New("no APP_URL: run `scripts/setup host --yes --app-url https://<dashboard hostname>` first, or pass --app-url")
	}
	d, err := plan.NewDesired(plan.Inputs{AppURL: v})
	if err != nil {
		return "", fmt.Errorf("APP_URL: %v", err)
	}
	return "https://" + d.Hostname, nil
}

// readValue reads one value from a file, or hidden at a terminal. "" means not given.
func readValue(deps Deps, path, prompt string, ask bool) (string, error) {
	if path != "" {
		raw, err := deps.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("cannot read %s: %v", path, unwrapPath(err))
		}
		return strings.TrimSpace(string(raw)), nil
	}
	if !ask || !deps.IsTerminal(deps.StdinFD) {
		return "", nil
	}
	fmt.Fprint(deps.Stderr, prompt)
	raw, err := deps.ReadPassword(deps.StdinFD)
	fmt.Fprintln(deps.Stderr)
	if err != nil {
		return "", errors.New("reading from the terminal failed")
	}
	return strings.TrimSpace(string(raw)), nil
}

// runDiscord is `dashboard setup discord`. Exit codes: 0 done (missing and conflict lines
// may remain; they are printed), 1 failed or refused by Discord, 2 usage.
func runDiscord(ctx context.Context, args []string, deps Deps) int {
	if len(args) > 0 && args[0] == "connect" {
		return runDiscordConnect(ctx, args[1:], deps)
	}
	fs := flag.NewFlagSet("setup discord", flag.ContinueOnError)
	fs.SetOutput(deps.Stderr)
	fs.Usage = func() {
		fmt.Fprint(deps.Stderr, discordUsage)
		fs.PrintDefaults()
	}
	var dir, appURL, idFile, secretFile, botFile string
	var dryRun, yes, noBot bool
	fs.StringVar(&dir, "deploy-dir", ".", "the deployment directory (server/deploy)")
	fs.BoolVar(&dryRun, "dry-run", false, "check with Discord and print the plan; write nothing")
	fs.BoolVar(&yes, "yes", false, "write the planned files")
	fs.StringVar(&appURL, "app-url", "", "dashboard origin (default: APP_URL from dashboard.env or .env)")
	fs.StringVar(&idFile, "client-id-file", "", "read the client ID from this file (default: dashboard.env, else asked at a terminal)")
	fs.StringVar(&secretFile, "client-secret-file", "", "read the client secret from this file (default: the stored one, else asked, hidden)")
	fs.StringVar(&botFile, "bot-token-file", "", "read the bot token from this file (optional; default: the stored one, else asked, hidden)")
	fs.BoolVar(&noBot, "no-bot-token", false, "do not ask for a bot token")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	fail := func(format string, a ...any) int {
		fmt.Fprintf(deps.Stderr, "setup discord: "+format+"\n", a...)
		return 1
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(deps.Stderr, "setup discord: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if dryRun && yes {
		fmt.Fprintln(deps.Stderr, "setup discord: --dry-run and --yes contradict each other")
		return 2
	}
	app, err := discordAppURL(dir, appURL)
	if err != nil {
		return fail("%v", err)
	}
	redirect := app + "/auth/callback"

	storedID := realValue(dir, "dashboard.env", "DISCORD_CLIENT_ID")
	secret, err := storedSecret(dir, "discord-client-secret")
	if err != nil {
		return fail("%v", err)
	}
	bot, err := storedSecret(dir, "discord-bot-token")
	if err != nil {
		return fail("%v", err)
	}
	givenID, err := readValue(deps, idFile, "Discord client ID (Developer Portal → your app → OAuth2; input hidden): ", storedID == "")
	if err != nil {
		return fail("--client-id-file: %v", err)
	}
	givenSecret, err := readValue(deps, secretFile, "Discord client secret (input hidden): ", !secret.here && secret.elsewhere == "")
	if err != nil {
		return fail("--client-secret-file: %v", err)
	}
	givenBot, err := readValue(deps, botFile, "Discord bot token (optional, for invitation DMs; Enter to skip; input hidden): ", !noBot && !bot.here && bot.elsewhere == "")
	if err != nil {
		return fail("--bot-token-file: %v", err)
	}
	switch {
	case givenID != "" && !snowflake.MatchString(givenID):
		return fail("the client ID given is not a Discord client ID (17–20 digits; Developer Portal → your app → OAuth2)")
	case givenSecret != "" && !clientSecretShape.MatchString(givenSecret):
		return fail("the client secret given does not look like one (one line, no spaces)")
	case givenBot != "" && !botTokenShape.MatchString(givenBot):
		return fail("the bot token given does not look like one (Developer Portal → your app → Bot → Reset Token)")
	}

	// What will be in place: a stored value wins over a given one (the adopt rule).
	effID, effSecret, effBot := storedID, secret.value, bot.value
	if effID == "" {
		effID = givenID
	}
	if !secret.here {
		effSecret = givenSecret
	}
	if !bot.here {
		effBot = givenBot
	}
	dc := discordClient(deps)
	switch {
	case secret.elsewhere != "":
		fmt.Fprintf(deps.Stdout, "The client secret is kept at %s (DISCORD_CLIENT_SECRET_FILE), outside setup's files; it was not checked.\n", secret.elsewhere)
	case effID != "" && effSecret != "":
		err := dc.CheckClient(ctx, effID, discord.Secret(effSecret))
		if discord.IsRejected(err) {
			hint := "check that both were copied from the same application (Developer Portal → your app → OAuth2)"
			if secret.here {
				hint = "the stored secret (secrets/app/discord-client-secret) is out of date; after Reset Secret in the Developer Portal, replace it with `scripts/setup host --yes --rotate discord-client-secret --secret discord-client-secret=FILE`"
			}
			return fail("Discord refused the client ID and secret: %s. Nothing was written.", hint)
		}
		if err != nil {
			return fail("%v. Nothing was written.", err)
		}
		fmt.Fprintln(deps.Stdout, "Discord accepted the client ID and secret.")
	}
	redirectState := "Setup could not check it: give a bot token to have it read the app's redirects, or check it by hand."
	if bot.elsewhere != "" {
		fmt.Fprintf(deps.Stdout, "The bot token is kept at %s (DISCORD_BOT_TOKEN_FILE), outside setup's files; it was not checked.\n", bot.elsewhere)
	} else if effBot != "" {
		a, err := dc.Application(ctx, discord.Secret(effBot))
		if discord.IsRejected(err) {
			return fail("Discord refused the bot token: copy it again (Developer Portal → your app → Bot → Reset Token). Nothing was written.")
		}
		if err != nil {
			return fail("%v. Nothing was written.", err)
		}
		if effID != "" && a.ID != effID {
			return fail("the bot token belongs to application %s, not %s (the client ID): use the bot of the same application. Nothing was written.", a.ID, effID)
		}
		fmt.Fprintf(deps.Stdout, "Discord accepted the bot token (application %s).\n", a.ID)
		switch {
		case !a.RedirectURIsKnown:
			redirectState = "Setup could not check it: Discord did not return the app's redirects. Check it by hand."
		case contains(a.RedirectURIs, redirect):
			redirectState = "It is registered (read from Discord with the bot token)."
		default:
			redirectState = "It is NOT registered yet: add it under Redirects, then Save Changes. Sign-in and `setup discord connect` fail until it is."
		}
	}

	owner := deps.Owner
	if owner == nil {
		owner = hostfiles.SystemOwner()
	}
	pl := hostfiles.NewPlanner(dir, owner)
	for _, d := range hostfiles.Dirs[:2] { // secrets, secrets/app
		pl.Dir(d.Path, d.Mode, d.Owner)
	}
	pl.Hint("DISCORD_CLIENT_ID", "give it with --client-id-file FILE, or type it at a terminal")
	envKeys := []hostfiles.KV{{Key: "DISCORD_CLIENT_ID", Value: givenID}}
	for _, s := range []struct {
		d     discordSecret
		given string
	}{{secret, givenSecret}, {bot, givenBot}} {
		spec := s.d.spec
		if s.d.elsewhere != "" {
			envKeys = append(envKeys, hostfiles.KV{Key: spec.EnvKey, Value: spec.EnvValue})
			continue
		}
		if s.d.here && s.given != "" && subtle.ConstantTimeCompare([]byte(s.given), []byte(s.d.value)) != 1 {
			pl.Report(hostfiles.Conflict, spec.Path, "", fmt.Sprintf("holds another value than the one given; kept. To replace it: scripts/setup host --yes --rotate %s --secret %s=FILE", spec.Name, spec.Name))
		} else {
			var value []byte
			if !s.d.here && s.given != "" {
				value = []byte(s.given)
			}
			optional := spec.Optional && !s.d.here && value == nil
			pl.Secret(hostfiles.Secret{Path: spec.Path, Value: value, Owner: spec.Owner, Optional: optional,
				Hint: "give it with --client-secret-file FILE, or type it at a terminal"})
			if optional {
				continue
			}
		}
		envKeys = append(envKeys, hostfiles.KV{Key: spec.EnvKey, Value: spec.EnvValue})
	}
	pl.Env("dashboard.env", "dashboard.env.example", envKeys)
	p, err := pl.Plan()
	if err != nil {
		return fail("%v", err)
	}
	fmt.Fprintf(deps.Stdout, "Host files in %s:\n", dir)
	if err := p.WriteText(deps.Stdout); err != nil {
		return 1
	}
	switch {
	case p.Changes() == 0:
	case dryRun:
		fmt.Fprintln(deps.Stdout, "Dry run: nothing was written.")
	case !yes:
		fmt.Fprintln(deps.Stdout, "Nothing was written. Run again with --yes to carry out the create and update lines; conflict and missing lines are left for you.")
	default:
		if err := p.Apply(); err != nil {
			return fail("%v", err)
		}
		fmt.Fprintf(deps.Stdout, "Wrote %d changes.\n", p.Changes())
	}

	fmt.Fprintf(deps.Stdout, "\nIn the Discord Developer Portal (your app → OAuth2 → Redirects), the dashboard needs exactly this redirect:\n  %s\n%s\n", redirect, redirectState)
	if effBot != "" && effID != "" {
		fmt.Fprintf(deps.Stdout, "\nAdd the bot to your Discord server with this link. It asks for no permissions: the bot only sends\ninvitation DMs, which needs it to share a server with the people invited.\n  %s\n", discord.InviteURL(effID))
	}
	fmt.Fprintln(deps.Stdout, "\nNext: start the dashboard (docs/deploy/fresh-install.md step 6), then run `scripts/setup discord connect`\nto make your Discord account the first owner and add a notification webhook.")
	return 0
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

const pasteAttempts = 3

// runDiscordConnect is `dashboard setup discord connect`. Exit codes: 0 done, 1 failed, 2 usage.
func runDiscordConnect(ctx context.Context, args []string, deps Deps) int {
	fs := flag.NewFlagSet("setup discord connect", flag.ContinueOnError)
	fs.SetOutput(deps.Stderr)
	fs.Usage = func() {
		fmt.Fprint(deps.Stderr, discordUsage)
		fs.PrintDefaults()
	}
	var dir, appURL string
	var dryRun bool
	fs.StringVar(&dir, "deploy-dir", ".", "the deployment directory (server/deploy)")
	fs.StringVar(&appURL, "app-url", "", "dashboard origin (default: APP_URL from dashboard.env or .env)")
	fs.BoolVar(&dryRun, "dry-run", false, "say what connect would ask Discord for; no link, no request, no write")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	fail := func(format string, a ...any) int {
		fmt.Fprintf(deps.Stderr, "setup discord connect: "+format+"\n", a...)
		return 1
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(deps.Stderr, "setup discord connect: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	app, err := discordAppURL(dir, appURL)
	if err != nil {
		return fail("%v", err)
	}
	redirect := app + "/auth/callback"
	clientID := realValue(dir, "dashboard.env", "DISCORD_CLIENT_ID")
	secret, err := storedSecret(dir, "discord-client-secret")
	if err != nil {
		return fail("%v", err)
	}
	if clientID == "" || !secret.here {
		return fail("connect needs the client ID in dashboard.env and the client secret in secrets/app/discord-client-secret: run `scripts/setup discord --yes` first")
	}
	ownerID := realValue(dir, "dashboard.env", "BOOTSTRAP_OWNER_DISCORD_ID")
	if ownerID == "" {
		ownerID = realValue(dir, "dashboard.env", "BOOTSTRAP_DIRECTOR_DISCORD_ID")
	}
	hook, err := storedSecret(dir, "discord-webhook-url")
	if err != nil {
		return fail("%v", err)
	}
	backupHook, err := storedSecret(dir, "backup-discord-webhook-url")
	if err != nil {
		return fail("%v", err)
	}
	var scopes []string
	if ownerID == "" {
		scopes = append(scopes, "identify")
	}
	if !hook.here && hook.elsewhere == "" {
		scopes = append(scopes, "webhook.incoming")
	}
	// An install with the app's webhook but no backup copy gets the copy from the app's
	// file: no Discord call, nothing replaced (review of #128, F-8).
	copyBackup := hook.here && !backupHook.here && backupHook.elsewhere == ""
	if dryRun {
		if copyBackup {
			fmt.Fprintln(deps.Stdout, "connect would copy secrets/app/discord-webhook-url to secrets/backup/discord-webhook-url for the backup alerts.")
		}
		if len(scopes) > 0 {
			fmt.Fprintf(deps.Stdout, "connect would print one Discord link asking for: %s\n", strings.Join(scopes, " "))
		}
		fmt.Fprintln(deps.Stdout, "Dry run: no link, no request, nothing written.")
		return 0
	}
	owner := deps.Owner
	if owner == nil {
		owner = hostfiles.SystemOwner()
	}
	restart := map[string]bool{}
	// apply writes one plan and notes which services read what it changed.
	apply := func(pl *hostfiles.Planner) error {
		p, err := pl.Plan()
		if err != nil {
			return err
		}
		if err := p.WriteText(deps.Stdout); err != nil {
			return err
		}
		if err := p.Apply(); err != nil {
			return err
		}
		for _, a := range p.Actions {
			if a.Kind == hostfiles.Create || a.Kind == hostfiles.Update {
				if a.Path == "dashboard.env" || strings.HasPrefix(a.Path, "secrets/app/") {
					restart["dashboard"] = true
				}
				if a.Path == "backup.env" || strings.HasPrefix(a.Path, "secrets/backup/") {
					restart["backup"] = true
				}
			}
		}
		return nil
	}
	if copyBackup {
		fmt.Fprintln(deps.Stdout, "The backup service has no copy of the notification webhook: copying it from secrets/app/discord-webhook-url (no Discord call).")
		pl := hostfiles.NewPlanner(dir, owner)
		for _, d := range hostfiles.Dirs {
			pl.Dir(d.Path, d.Mode, d.Owner)
		}
		pl.Secret(hostfiles.Secret{Path: backupHook.spec.Path, Value: []byte(hook.value), Owner: backupHook.spec.Owner})
		pl.Env(backupHook.spec.EnvFile, backupHook.spec.EnvFile+".example", []hostfiles.KV{{Key: backupHook.spec.EnvKey, Value: backupHook.spec.EnvValue}})
		if err := apply(pl); err != nil {
			return fail("%v", err)
		}
	}
	if len(scopes) == 0 {
		if !copyBackup {
			fmt.Fprintf(deps.Stdout, "Discord: nothing to do. The first owner is set (BOOTSTRAP_OWNER_DISCORD_ID=%s) and the webhook file exists.\n", ownerID)
		}
		printRestart(deps, restart, false, app)
		return 0
	}
	if !deps.IsTerminal(deps.StdinFD) {
		return fail("connect needs a terminal: you paste the setup code the dashboard shows into it")
	}
	now := deps.Now
	if now == nil {
		now = time.Now
	}
	dc := discordClient(deps)
	clientSecret := discord.Secret(secret.value)
	used := map[string]bool{}
	ownerWritten, ownerFailed := false, false
	for round := 0; len(scopes) > 0; round++ {
		if round == 2 {
			return fail("Discord did not give %s after two links; nothing more was asked", strings.Join(scopes, " "))
		}
		newLink := func() (*discord.Consent, error) {
			cons, err := discord.NewConsent(deps.Random, now(), clientID, clientSecret, redirect, scopes)
			if err != nil {
				return nil, err
			}
			if contains(scopes, "webhook.incoming") {
				fmt.Fprintln(deps.Stdout, "Discord asks you to pick the channel for dashboard notifications.")
			}
			fmt.Fprintf(deps.Stdout, "\n  %s\n\nThe link works for %d minutes. The dashboard then shows a setup code, once (behind the usual Cloudflare sign-in).\n"+
				"Paste it here, and only here. If you lose the page, setup prints a new link.\n"+
				"If the dashboard shows a sign-in error instead of a setup code: restart it so it reads the current client secret\n"+
				"(docker compose … up -d --force-recreate dashboard), and check the host clock.\n", cons.URL, int(auth.SetupStateTTL/time.Minute))
			return cons, nil
		}
		if round == 0 {
			fmt.Fprintln(deps.Stdout, "Open this link in a browser where you are signed in to Discord as yourself (the club's admin), and approve it.")
		} else {
			fmt.Fprintf(deps.Stdout, "Discord did not include %s in that answer. One more link, for that alone:\n", strings.Join(scopes, " "))
		}
		cons, err := newLink()
		if err != nil {
			return fail("%v", err)
		}
		tok, ok := pasteAndExchange(ctx, deps, dc, cons, newLink, now, clientID, clientSecret, redirect, used)
		if !ok {
			return fail("no usable setup code; nothing more was written. Run `scripts/setup discord connect` again for a new link")
		}
		var got []string
		pl := hostfiles.NewPlanner(dir, owner)
		// Each env file is planned once, after the secret files its keys name, so a key never
		// points at a file that is not there yet.
		envKeys := map[string][]hostfiles.KV{}
		// The webhook first: Discord made it already, so it is written whatever happens to
		// the owner below.
		if contains(scopes, "webhook.incoming") && tok.Webhook != nil {
			for _, d := range hostfiles.Dirs {
				pl.Dir(d.Path, d.Mode, d.Owner)
			}
			value := []byte(tok.Webhook.URL.Reveal())
			for _, name := range []string{"discord-webhook-url", "backup-discord-webhook-url"} {
				s, err := storedSecret(dir, name)
				if err != nil {
					return fail("%v", err)
				}
				if s.elsewhere != "" {
					fmt.Fprintf(deps.Stdout, "%s keeps its own webhook file (%s=%s); left alone.\n", s.spec.EnvFile, s.spec.EnvKey, s.elsewhere)
					continue
				}
				pl.Secret(hostfiles.Secret{Path: s.spec.Path, Value: value, Owner: s.spec.Owner})
				envKeys[s.spec.EnvFile] = append(envKeys[s.spec.EnvFile], hostfiles.KV{Key: s.spec.EnvKey, Value: s.spec.EnvValue})
			}
			got = append(got, "webhook.incoming")
			where := "channel " + tok.Webhook.ChannelID
			if tok.Webhook.GuildID != "" {
				where += " of server " + tok.Webhook.GuildID
			}
			name := printable(tok.Webhook.Name)
			fmt.Fprintf(deps.Stdout, "Discord made a webhook named %q in %s.\n"+
				"If you did not mean to make it, delete it in Discord: that channel's settings (Edit Channel) → Integrations → Webhooks → %q → Delete Webhook.\n", name, where, name)
		}
		if contains(scopes, "identify") {
			// Asked once: a declined or failed owner is not asked for again in this run.
			got = append(got, "identify")
			u, err := dc.CurrentUser(ctx, tok.AccessToken)
			switch {
			case err != nil:
				fmt.Fprintf(deps.Stderr, "setup discord connect: %v; no owner was written\n", err)
				ownerFailed = true
			case confirmOwner(deps, u):
				kvs := []hostfiles.KV{{Key: "BOOTSTRAP_OWNER_DISCORD_ID", Value: u.ID}}
				if realValue(dir, "dashboard.env", "BOOTSTRAP_OWNER_NAME") == "" && printable(u.DisplayName()) != "" {
					kvs = append(kvs, hostfiles.KV{Key: "BOOTSTRAP_OWNER_NAME", Value: printable(u.DisplayName())})
				}
				envKeys["dashboard.env"] = append(envKeys["dashboard.env"], kvs...)
				ownerWritten = true
			default:
				fmt.Fprintln(deps.Stdout, "No owner was written. To choose another account, run `scripts/setup discord connect` again from a browser signed in to that Discord account,\n"+
					"or set it with `scripts/setup host --yes --owner-discord-id <ID>`.")
			}
		}
		for _, f := range []string{"dashboard.env", "backup.env"} {
			if len(envKeys[f]) > 0 {
				pl.Env(f, f+".example", envKeys[f])
			}
		}
		// Write what this consent gave before asking for more, so a webhook Discord made
		// is never lost to a later failure.
		if err := apply(pl); err != nil {
			return fail("%v", err)
		}
		var left []string
		for _, s := range scopes {
			if !contains(got, s) {
				left = append(left, s)
			}
		}
		scopes = left
	}
	printRestart(deps, restart, ownerWritten, app)
	if ownerFailed {
		return 1
	}
	return 0
}

func printRestart(deps Deps, restart map[string]bool, ownerWritten bool, app string) {
	var services []string
	for _, s := range []string{"dashboard", "backup"} {
		if restart[s] {
			services = append(services, s)
		}
	}
	if len(services) > 0 {
		fmt.Fprintf(deps.Stdout, "\nSetup restarts nothing. To load the new settings, run in the deploy directory (server/deploy):\n"+
			"  docker compose --env-file .env --env-file prod-image.env -f compose.cloudflare.yml up -d --force-recreate %s\n", strings.Join(services, " "))
	}
	if ownerWritten {
		fmt.Fprintf(deps.Stdout, "When it starts with no owner yet, the dashboard makes that Discord account the owner. Then sign in at %s.\n", app)
	}
}

// printable drops control characters (Cc: escape sequences, bells) and format characters
// (Cf: bidi overrides and isolates, zero-width spaces and joiners) from text Discord chose,
// such as names, before it is shown or written: either kind could reorder, hide or forge
// what the admin reads next to it (review of #128, S-2).
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
}

// confirmOwner shows the Discord account that approved the link and asks for a typed
// "yes" before it becomes the first owner (review of #128, F-2): the browser may have been
// signed in to another account, and ownership cannot be taken back by configuration once
// the dashboard has made it. No terminal, or anything but "yes", is a no.
//
// Input already waiting on the terminal is discarded first, so a second line pasted with
// the setup code (perhaps text someone else supplied) can never answer the question
// (review of #128, S-1). The answer is read from the same terminal (Stdin is StdinFD).
// The ID, which Discord validates, comes first on its own line; names follow, stripped.
func confirmOwner(deps Deps, u *discord.User) bool {
	fmt.Fprintf(deps.Stdout, "Discord says this link was approved by this account:\n  ID:       %s\n  Name:     %s\n  Username: @%s\n",
		u.ID, printable(u.DisplayName()), printable(u.Username))
	if !deps.IsTerminal(deps.StdinFD) || deps.Stdin == nil {
		fmt.Fprintln(deps.Stdout, "No terminal to confirm on, so no owner was written.")
		return false
	}
	flush := deps.FlushInput
	if flush == nil {
		flush = flushInput
	}
	if err := flush(deps.StdinFD); err != nil {
		fmt.Fprintf(deps.Stdout, "Could not clear what is waiting on the terminal (%v), so no owner was written.\n", err)
		return false
	}
	fmt.Fprint(deps.Stderr, "Make this account the dashboard's first owner? Type yes: ")
	line, _ := bufio.NewReader(deps.Stdin).ReadString('\n')
	return strings.TrimSpace(line) == "yes"
}

// pasteAndExchange asks for the setup code up to pasteAttempts times and trades the first
// good one. The dashboard shows each link's code once, so after a paste that fails it
// prints a new link (with newLink), and it still takes a code from any earlier link of this
// round that has not expired. A paste from no link of this round (another state), a code
// already tried, and a code from an expired link are refused without asking Discord.
func pasteAndExchange(ctx context.Context, deps Deps, dc *discord.Client, cons *discord.Consent, newLink func() (*discord.Consent, error),
	now func() time.Time, clientID string, secret discord.Secret, redirect string, used map[string]bool) (*discord.Token, bool) {
	links := []*discord.Consent{cons}
	another := func(why string) bool {
		fmt.Fprintln(deps.Stdout, why)
		next, err := newLink()
		if err != nil {
			fmt.Fprintln(deps.Stderr, "setup discord connect:", err)
			return false
		}
		links = append(links, next)
		return true
	}
	expiredMsg := fmt.Sprintf("That link has expired (links work for %d minutes). Here is a new one:", int(auth.SetupStateTTL/time.Minute))
	for attempt := 0; attempt < pasteAttempts; attempt++ {
		if attempt > 0 {
			fmt.Fprintln(deps.Stdout, "If you saw a sign-in error before you used the link yourself, someone else may have opened it. Tell the dashboard owner.")
			why := "Here is a new link (the dashboard shows each setup code once):"
			if !now().Before(links[len(links)-1].Expiry) {
				why = expiredMsg
			}
			if !another(why) {
				return nil, false
			}
		} else if !now().Before(cons.Expiry) && !another(expiredMsg) {
			return nil, false
		}
		fmt.Fprint(deps.Stderr, "Setup code from the dashboard page (input hidden): ")
		raw, err := deps.ReadPassword(deps.StdinFD)
		fmt.Fprintln(deps.Stderr)
		if err != nil {
			fmt.Fprintln(deps.Stderr, "setup discord connect: reading from the terminal failed")
			return nil, false
		}
		state, code, err := discord.ParsePaste(string(bytes.TrimSpace(raw)))
		if err != nil {
			fmt.Fprintln(deps.Stderr, "setup discord connect:", err)
			continue
		}
		var match *discord.Consent
		for _, l := range links {
			if subtle.ConstantTimeCompare([]byte(state), []byte(l.State)) == 1 {
				match = l
			}
		}
		if match == nil {
			fmt.Fprintln(deps.Stderr, "setup discord connect: that setup code is not from a link above (another state). Open the newest link above and paste what it shows.")
			continue
		}
		if !now().Before(match.Expiry) {
			fmt.Fprintln(deps.Stderr, "setup discord connect: that code came after its link expired.")
			continue
		}
		if used[code] {
			fmt.Fprintln(deps.Stderr, "setup discord connect: that code was already used.")
			continue
		}
		used[code] = true
		tok, err := dc.ExchangeCode(ctx, clientID, secret, code, match.Verifier(), redirect)
		if discord.IsInvalidGrant(err) {
			fmt.Fprintln(deps.Stderr, "setup discord connect: Discord refused that code: it was used already, has expired, or is from another link.")
			continue
		}
		if discord.IsRejected(err) {
			fmt.Fprintln(deps.Stderr, "setup discord connect: Discord refused the stored client ID and secret; run `scripts/setup discord` to check them")
			return nil, false
		}
		if err != nil {
			fmt.Fprintln(deps.Stderr, "setup discord connect:", err)
			return nil, false
		}
		return tok, true
	}
	return nil, false
}
