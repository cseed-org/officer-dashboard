package setup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"officerdashboard/internal/setup/hostfiles"
	"officerdashboard/internal/setup/plan"
)

const hostUsage = `usage: dashboard setup host [--dry-run | --yes] [flags]

Writes the deployment's host files: .env, dashboard.env and backup.env (from their
*.example templates), the secrets/ directories and the secret files. It prints what it
would do; only --yes writes. Nothing is ever replaced: an env key with another value
and any existing secret file are reported and left alone (use --rotate to replace a
secret on purpose).

Secret values are read only from standard input with --secrets-stdin, as one JSON
object of name to value. Names: %s.
The backup bearer secret is generated when it does not exist. It never talks to
Cloudflare or Discord, and it does not create the backup age key.

Flags:
`

var (
	snowflake    = regexp.MustCompile(`^[0-9]{17,20}$`) // fresh-install.md §3: "This 17–20 digit number"
	tunnelNetRE  = regexp.MustCompile(`^(25[0-5]|2[0-4][0-9]|1?[0-9]?[0-9])\.(25[0-5]|2[0-4][0-9]|1?[0-9]?[0-9])\.(25[0-5]|2[0-4][0-9]|1?[0-9]?[0-9])$`)
	projectRE    = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`) // Compose project name rule
	ageRecipient = regexp.MustCompile(`^age1[qpzry9x8gf2tvdw0s3jn54khce6mua7l]{58}$`)
)

func secretNames() string {
	var names []string
	for _, s := range hostfiles.Secrets {
		names = append(names, s.Name)
	}
	return strings.Join(names, ", ")
}

// runHost is `dashboard setup host`. Exit codes: 0 done (missing and conflict lines may
// remain; they are printed), 1 failed, 2 usage.
func runHost(_ context.Context, args []string, deps Deps) int {
	fs := flag.NewFlagSet("setup host", flag.ContinueOnError)
	fs.SetOutput(deps.Stderr)
	fs.Usage = func() {
		fmt.Fprintf(deps.Stderr, hostUsage, secretNames())
		fs.PrintDefaults()
	}
	var dir, appURL, backupHost, clientID, ownerID, ownerName, age, project, tunnelNet string
	var dryRun, yes, fromStdin, installExists bool
	rotate := map[string]bool{}
	fs.StringVar(&dir, "deploy-dir", ".", "the deployment directory (server/deploy)")
	fs.BoolVar(&dryRun, "dry-run", false, "print the plan and write nothing")
	fs.BoolVar(&yes, "yes", false, "write the planned files")
	fs.BoolVar(&fromStdin, "secrets-stdin", false, "read secret values from standard input as a JSON object")
	fs.StringVar(&appURL, "app-url", "", "dashboard origin, https://<hostname> (default: APP_URL from .env)")
	fs.StringVar(&backupHost, "backup-hostname", "", "backup Worker hostname (default: from BACKUP_SINK_URL, else backups.<parent of the APP_URL host>)")
	fs.StringVar(&clientID, "discord-client-id", "", "Discord application client ID")
	fs.StringVar(&ownerID, "owner-discord-id", "", "first owner's Discord user ID (BOOTSTRAP_OWNER_DISCORD_ID)")
	fs.StringVar(&ownerName, "owner-name", "", "first owner's name (BOOTSTRAP_OWNER_NAME)")
	fs.StringVar(&age, "age-recipient", "", "public age recipient for backups, age1… (BACKUP_AGE_RECIPIENT)")
	fs.StringVar(&project, "compose-project-name", "", "Compose project name (COMPOSE_PROJECT_NAME), for a second install on this host; written only for a new install")
	fs.StringVar(&tunnelNet, "tunnel-net", "", "first three numbers of the tunnel network (TUNNEL_NET), for a second install on this host; written only for a new install")
	fs.BoolVar(&installExists, "install-exists", false, "Docker already has containers or volumes for this install (set by scripts/setup)")
	fs.Func("rotate", "replace this existing secret (repeatable); its new value must be on --secrets-stdin, except backup-bearer, which is generated", func(name string) error {
		if _, ok := hostfiles.LookupSecret(name); !ok {
			return fmt.Errorf("unknown secret %q (known: %s)", name, secretNames())
		}
		rotate[name] = true
		return nil
	})
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	usageErr := func(format string, a ...any) int {
		fmt.Fprintf(deps.Stderr, "setup host: "+format+"\n", a...)
		return 2
	}
	if fs.NArg() > 0 {
		return usageErr("unexpected argument %q", fs.Arg(0))
	}
	if dryRun && yes {
		return usageErr("--dry-run and --yes contradict each other")
	}
	for _, c := range []struct {
		value, flag string
		re          *regexp.Regexp
		want        string
	}{
		{clientID, "--discord-client-id", snowflake, "a 17–20 digit Discord ID"},
		{ownerID, "--owner-discord-id", snowflake, "a 17–20 digit Discord user ID"},
		{tunnelNet, "--tunnel-net", tunnelNetRE, "three numbers 0–255, such as 10.250.90"},
		{project, "--compose-project-name", projectRE, "lower-case letters, digits, '-' and '_'"},
		{age, "--age-recipient", ageRecipient, "an age public key, age1 followed by 58 characters"},
	} {
		if c.value != "" && !c.re.MatchString(c.value) {
			return usageErr("%s must be %s", c.flag, c.want)
		}
	}
	// Every install that has ever run Compose has an image pin (compose.cloudflare.yml
	// needs DASHBOARD_IMAGE, passed with --env-file prod-image.env), and a moved or
	// stopped install keeps it. A .env alone proves nothing: it may be copied.
	if installExists && !exists(filepath.Join(dir, "prod-image.env")) {
		fmt.Fprintln(deps.Stderr, "setup host: Docker already has containers or volumes for this Compose project, but this directory has no install that ever ran it"+
			" (no prod-image.env): the project belongs to another install. Choose a project name and subnet nothing else uses.")
		return 1
	}
	secrets := map[string][]byte{}
	if fromStdin {
		var err error
		if secrets, err = readSecrets(deps.Stdin); err != nil {
			return usageErr("%v", err)
		}
	}

	// Values already in the files count as given, so a second run needs no flags.
	current := func(file, key string) string { return realValue(dir, file, key) }
	appFromFlag := appURL != ""
	if !appFromFlag {
		appURL = current(".env", "APP_URL")
	}
	var sinkURL string
	if appURL != "" {
		host := backupHost
		if host == "" {
			host = backupHostDefault(current("backup.env", "BACKUP_SINK_URL"))
		}
		d, err := plan.NewDesired(plan.Inputs{AppURL: appURL, BackupHostname: host})
		if err != nil {
			if !appFromFlag {
				return usageErr("APP_URL in .env: %v", err)
			}
			return usageErr("%v", err)
		}
		if appFromFlag {
			appURL = "https://" + d.Hostname
		}
		// An existing sink URL is kept as written unless a hostname was asked for.
		if backupHost != "" || current("backup.env", "BACKUP_SINK_URL") == "" {
			sinkURL = "https://" + d.BackupHostname
		}
	}

	random := deps.Random
	if random == nil {
		random = rand.Reader
	}
	generatedBearer := false
	if spec, _ := hostfiles.LookupSecret("backup-bearer"); rotate[spec.Name] || !exists(filepath.Join(dir, spec.Path)) {
		if _, given := secrets[spec.Name]; !given {
			generatedBearer = true
			// backup.env.example: "Bearer secret must be at least 32 bytes; generate 32 random
			// bytes and encode them."
			b := make([]byte, 32)
			if _, err := io.ReadFull(random, b); err != nil {
				fmt.Fprintln(deps.Stderr, "setup host: generating the backup bearer failed:", err)
				return 1
			}
			secrets[spec.Name] = []byte(hex.EncodeToString(b))
		}
	}
	for name := range rotate {
		if secrets[name] == nil {
			return usageErr("--rotate %s needs its new value on --secrets-stdin", name)
		}
	}

	owner := deps.Owner
	if owner == nil {
		owner = hostfiles.SystemOwner()
	}
	pl := hostfiles.NewPlanner(dir, owner)
	for _, d := range hostfiles.Dirs {
		pl.Dir(d.Path, d.Mode, d.Owner)
	}
	pl.Hint("APP_URL", "pass --app-url https://<dashboard hostname>")
	pl.Hint("BACKUP_SINK_URL", "pass --app-url or --backup-hostname")
	pl.Hint("DISCORD_CLIENT_ID", "pass --discord-client-id (Discord Developer Portal → your app → OAuth2)")
	pl.Hint("BOOTSTRAP_OWNER_DISCORD_ID", "pass --owner-discord-id (fresh-install.md step 3)")
	pl.Hint("BACKUP_AGE_RECIPIENT", "pass --age-recipient age1… (age-keygen on the recovery workstation, fresh-install.md step 4)")

	files := map[string][]hostfiles.KV{
		".env":          {{Key: "APP_URL", Value: appURL}},
		"dashboard.env": {{Key: "APP_URL", Value: appURL}, {Key: "DISCORD_CLIENT_ID", Value: clientID}, {Key: "BOOTSTRAP_OWNER_DISCORD_ID", Value: ownerID}},
		"backup.env":    {{Key: "BACKUP_SINK_URL", Value: sinkURL}, {Key: "BACKUP_AGE_RECIPIENT", Value: age}},
	}
	if ownerName != "" {
		files["dashboard.env"] = append(files["dashboard.env"], hostfiles.KV{Key: "BOOTSTRAP_OWNER_NAME", Value: ownerName})
	}
	// The Compose project name selects the install's data volume, and TUNNEL_NET its
	// network. Setup writes them only into a .env it is creating for a new install, and
	// only when the admin passed them; it never picks them itself. For an existing
	// install (a .env, an image pin, or containers or volumes in Docker), an absent key
	// means the Compose default, and any other value is a conflict: changing it would
	// move the install to new, empty volumes.
	newInstall := !exists(filepath.Join(dir, ".env")) && !exists(filepath.Join(dir, "prod-image.env")) && !installExists
	var fixedReports []func()
	for _, k := range []struct{ key, value, def string }{
		{"COMPOSE_PROJECT_NAME", project, "officer-dashboard"},
		{"TUNNEL_NET", tunnelNet, "10.250.89"},
	} {
		switch {
		case k.value == "":
		case newInstall:
			files[".env"] = append(files[".env"], hostfiles.KV{Key: k.key, Value: k.value})
		default:
			cur, from := current(".env", k.key), "set in .env"
			if cur == "" {
				cur, from = k.def, "the Compose default"
			}
			k := k
			if k.value == cur {
				fixedReports = append(fixedReports, func() { pl.Report(hostfiles.Exists, ".env", k.key, "") })
			} else {
				detail := fmt.Sprintf("this install uses %q (%s); setup never renames an existing install, which would move it to new, empty volumes; kept", cur, from)
				fixedReports = append(fixedReports, func() { pl.Report(hostfiles.Conflict, ".env", k.key, detail) })
			}
		}
	}
	var planned []hostfiles.Secret
	for _, s := range hostfiles.Secrets {
		path := filepath.Join(dir, s.Path)
		set := current(s.EnvFile, s.EnvKey)
		if set != "" && set != s.EnvValue {
			// The admin keeps this secret elsewhere: report the key, leave the file to them.
			files[s.EnvFile] = append(files[s.EnvFile], hostfiles.KV{Key: s.EnvKey, Value: s.EnvValue})
			continue
		}
		value := secrets[s.Name]
		optional := s.Optional && set == "" && value == nil && !exists(path)
		if !optional {
			files[s.EnvFile] = append(files[s.EnvFile], hostfiles.KV{Key: s.EnvKey, Value: s.EnvValue})
		}
		planned = append(planned, hostfiles.Secret{
			Path: s.Path, Value: value, Owner: s.Owner, Rotate: rotate[s.Name], Optional: optional,
			Hint: fmt.Sprintf("give %s with `scripts/setup host --secret %s=FILE`", s.Name, s.Name),
		})
	}
	for _, name := range []string{".env", "dashboard.env", "backup.env"} {
		pl.Env(name, name+".example", files[name])
		if name == ".env" {
			for _, report := range fixedReports {
				report()
			}
		}
	}
	for _, s := range planned {
		pl.Secret(s)
	}
	p, err := pl.Plan()
	if err != nil {
		fmt.Fprintln(deps.Stderr, "setup host:", err)
		return 1
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
			fmt.Fprintln(deps.Stderr, "setup host:", err)
			return 1
		}
		fmt.Fprintf(deps.Stdout, "Wrote %d changes.\n", p.Changes())
		if generatedBearer && wrote(p, "secrets/backup/bearer") {
			fmt.Fprintln(deps.Stdout, "The backup bearer secret is new: give the backup Worker the same value from secrets/backup/bearer\n"+
				"(npx wrangler secret put BACKUP_BEARER_SECRET, fresh-install.md step 4.5), or it refuses the backups.")
		}
	}
	return 0
}

// wrote reports whether the applied plan created or replaced the file at rel.
func wrote(p *hostfiles.Plan, rel string) bool {
	for _, a := range p.Actions {
		if a.Path == rel && a.Key == "" && (a.Kind == hostfiles.Create || a.Kind == hostfiles.Update) {
			return true
		}
	}
	return false
}

// readSecrets reads {"name": "value", ...} from r. Only names setup knows are accepted,
// and no value may be empty. Errors never quote the input.
func readSecrets(r io.Reader) (map[string][]byte, error) {
	if r == nil {
		return nil, errors.New("--secrets-stdin: no standard input")
	}
	raw, err := io.ReadAll(io.LimitReader(r, 1<<20))
	if err != nil {
		return nil, errors.New("--secrets-stdin: reading standard input failed")
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, errors.New("--secrets-stdin: standard input is not a JSON object of secret names to string values")
	}
	out := map[string][]byte{}
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, ok := hostfiles.LookupSecret(name); !ok {
			return nil, fmt.Errorf("--secrets-stdin: unknown secret %q (known: %s)", name, secretNames())
		}
		if strings.TrimSpace(m[name]) == "" {
			return nil, fmt.Errorf("--secrets-stdin: %s is empty", name)
		}
		out[name] = []byte(m[name])
	}
	return out, nil
}

// realValue is key's value in the deploy directory's env file, or "" when the file or
// key is missing, empty or still the template's example.
func realValue(dir, file, key string) string {
	raw, err := os.ReadFile(filepath.Join(dir, file))
	if err != nil {
		return ""
	}
	v, _ := hostfiles.ParseEnv(raw).Get(key)
	if tmpl, err := os.ReadFile(filepath.Join(dir, file+".example")); err == nil {
		if ex, ok := hostfiles.ParseEnv(tmpl).Get(key); ok && ex == v {
			return ""
		}
	}
	return v
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return !errors.Is(err, fs.ErrNotExist)
}
