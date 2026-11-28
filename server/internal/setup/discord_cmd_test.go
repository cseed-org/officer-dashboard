package setup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"officerdashboard/internal/setup/discord"
	"officerdashboard/internal/setup/discord/discordfake"
	"officerdashboard/internal/setup/hostfiles"
)

const appURL = "https://dash.club.example"

// discordHarness runs `dashboard setup discord …` against a fake Discord in a deploy
// directory. answers feed the hidden prompts in order; each gets everything printed so
// far, so it can play the admin approving the last link printed.
type discordHarness struct {
	t       *testing.T
	f       *discordfake.Server
	dir     string
	private string // the admin's private directory for value files
	owner   *memOwner
	tty     bool
	answers []func(printed string) string
	prompts []string
	waits   []time.Duration
	pasted  []string // every value typed or pasted, for the no-secret-in-output check
}

func newDiscordHarness(t *testing.T) *discordHarness {
	f := discordfake.New()
	t.Cleanup(f.Close)
	f.RedirectURIs = []string{appURL + "/auth/callback"}
	f.CombinedWebhook = true
	h := &discordHarness{t: t, f: f, dir: deployDir(t), private: t.TempDir(), owner: &memOwner{}}
	// What `setup host --yes --app-url` leaves: .env and dashboard.env with APP_URL.
	if r := hostCmd(t, h.owner, "", "--deploy-dir", h.dir, "--yes", "--app-url", appURL); r.code != 0 {
		t.Fatalf("host: %d %s", r.code, r.stderr)
	}
	return h
}

func (h *discordHarness) file(name, value string) string {
	p := filepath.Join(h.private, name)
	if err := os.WriteFile(p, []byte(value+"\n"), 0o600); err != nil {
		h.t.Fatal(err)
	}
	return p
}

func (h *discordHarness) type_(values ...string) {
	for _, v := range values {
		v := v
		h.answers = append(h.answers, func(string) string { return v })
	}
}

// approve answers a paste prompt the way an admin would: open the last link printed,
// approve it, and paste the line the relay page shows.
func (h *discordHarness) approve() {
	h.answers = append(h.answers, func(printed string) string {
		link := lastLink(h.t, printed)
		code, state, err := h.f.Approve(link)
		if err != nil {
			h.t.Fatalf("approving %s: %v", link, err)
		}
		return state + ":" + code
	})
}

var linkRE = regexp.MustCompile(`https://discord\.com/oauth2/authorize\?\S*response_type=code\S*`)

func lastLink(t *testing.T, printed string) string {
	links := linkRE.FindAllString(printed, -1)
	if len(links) == 0 {
		t.Fatalf("no consent link printed:\n%s", printed)
	}
	return links[len(links)-1]
}

func (h *discordHarness) run(args ...string) hostResult {
	h.t.Helper()
	var out, errb bytes.Buffer
	c := discord.New()
	c.BaseURL = h.f.URL + "/api"
	c.Sleep = func(_ context.Context, d time.Duration) error { h.waits = append(h.waits, d); return nil }
	code := Run(context.Background(), append([]string{"discord"}, args...), Deps{
		Getenv:     func(string) string { return "" },
		Stdout:     &out,
		Stderr:     &errb,
		ReadFile:   os.ReadFile,
		IsTerminal: func(int) bool { return h.tty },
		ReadPassword: func(int) ([]byte, error) {
			h.prompts = append(h.prompts, lastLine(errb.String()))
			if len(h.answers) == 0 {
				return nil, errors.New("no more answers")
			}
			a := h.answers[0]
			h.answers = h.answers[1:]
			v := a(out.String())
			h.pasted = append(h.pasted, v)
			return []byte(v), nil
		},
		Stdin:   strings.NewReader(""),
		Owner:   h.owner,
		Discord: c,
	})
	r := hostResult{code, out.String(), errb.String()}
	h.noSecretPrinted(r)
	return r
}

func lastLine(s string) string {
	s = strings.TrimRight(s, "\n")
	return s[strings.LastIndex(s, "\n")+1:]
}

// noSecretPrinted fails if any credential the fake knows, or anything pasted, was printed.
func (h *discordHarness) noSecretPrinted(r hostResult) {
	h.t.Helper()
	secrets := []string{h.f.ClientSecret, h.f.BotToken}
	for _, w := range h.f.Webhooks() {
		secrets = append(secrets, w.URL, w.Token)
	}
	secrets = append(secrets, h.f.AccessTokens()...)
	secrets = append(secrets, h.f.Codes()...)
	for _, p := range h.pasted {
		if strings.Contains(p, ":") { // a pasted state:code; the code is the secret part
			p = p[strings.LastIndex(p, ":")+1:]
		}
		if p != h.f.ClientID && p != "" {
			secrets = append(secrets, p)
		}
	}
	for _, s := range secrets {
		if s != "" && strings.Contains(r.stdout+r.stderr, s) {
			h.t.Fatalf("a secret was printed (%.6s…):\n%s\n%s", s, r.stdout, r.stderr)
		}
	}
}

func (h *discordHarness) read(rel string) string {
	raw, err := os.ReadFile(filepath.Join(h.dir, rel))
	if err != nil {
		h.t.Fatal(err)
	}
	return string(raw)
}

func (h *discordHarness) env(file, key string) string {
	v, _ := hostfiles.ParseEnv([]byte(h.read(file))).Get(key)
	return v
}

// snapshot is every file and directory in the deploy directory: content, mode, owner and
// modification time.
func (h *discordHarness) snapshot() string {
	h.t.Helper()
	var lines []string
	err := filepath.WalkDir(h.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		ids, _ := h.owner.Owner(p)
		line := fmt.Sprintf("%s %v %v %d", strings.TrimPrefix(p, h.dir), info.Mode(), ids, info.ModTime().UnixNano())
		if info.Mode().IsRegular() {
			raw, _ := os.ReadFile(p)
			line += fmt.Sprintf(" %x", sha256.Sum256(raw))
		}
		lines = append(lines, line)
		return nil
	})
	if err != nil {
		h.t.Fatal(err)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func (h *discordHarness) credentials() []string {
	return []string{
		"--client-id-file", h.file("client-id", h.f.ClientID),
		"--client-secret-file", h.file("client-secret", h.f.ClientSecret),
		"--bot-token-file", h.file("bot-token", h.f.BotToken),
	}
}

func checkSecretFile(t *testing.T, h *discordHarness, rel, want string) {
	t.Helper()
	info, err := os.Lstat(filepath.Join(h.dir, rel))
	if err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
	ids, _ := h.owner.Owner(filepath.Join(h.dir, rel))
	if info.Mode().Perm() != hostfiles.SecretMode || ids != hostfiles.AppIDs {
		t.Errorf("%s is %v %v, want 0400 10001:10001", rel, info.Mode().Perm(), ids)
	}
	if got := h.read(rel); got != want {
		t.Errorf("%s holds the wrong value", rel)
	}
}

func TestDiscordStoresValidatedCredentials(t *testing.T) {
	h := newDiscordHarness(t)
	r := h.run(append([]string{"--deploy-dir", h.dir, "--yes"}, h.credentials()...)...)
	if r.code != 0 {
		t.Fatalf("%d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
	checkSecretFile(t, h, "secrets/app/discord-client-secret", h.f.ClientSecret)
	checkSecretFile(t, h, "secrets/app/discord-bot-token", h.f.BotToken)
	for key, want := range map[string]string{
		"DISCORD_CLIENT_ID":          h.f.ClientID,
		"DISCORD_CLIENT_SECRET_FILE": "/run/secrets/app/discord-client-secret",
		"DISCORD_BOT_TOKEN_FILE":     "/run/secrets/app/discord-bot-token",
	} {
		if got := h.env("dashboard.env", key); got != want {
			t.Errorf("dashboard.env %s = %q, want %q", key, got, want)
		}
	}
	for _, want := range []string{
		"Discord accepted the client ID and secret",
		"Discord accepted the bot token (application " + h.f.ClientID + ")",
		"  " + appURL + "/auth/callback",
		"is registered",
		"https://discord.com/oauth2/authorize?client_id=" + h.f.ClientID + "&permissions=0&scope=bot",
		"scripts/setup discord connect",
	} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, r.stdout)
		}
	}
	// The client credentials check, then the bot token check; nothing else.
	if got := strings.Join(h.f.Requests(), ","); got != "POST /api/oauth2/token,GET /api/v10/applications/@me" {
		t.Errorf("requests %s", got)
	}
}

func TestDiscordReadsHiddenValuesAtATerminal(t *testing.T) {
	h := newDiscordHarness(t)
	h.tty = true
	h.type_(h.f.ClientID, h.f.ClientSecret, "") // no bot token
	r := h.run("--deploy-dir", h.dir, "--yes")
	if r.code != 0 {
		t.Fatalf("%d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
	if len(h.prompts) != 3 || !strings.Contains(h.prompts[1], "input hidden") || !strings.Contains(h.prompts[2], "optional") {
		t.Errorf("prompts %q", h.prompts)
	}
	checkSecretFile(t, h, "secrets/app/discord-client-secret", h.f.ClientSecret)
	if _, err := os.Lstat(filepath.Join(h.dir, "secrets/app/discord-bot-token")); !errors.Is(err, fs.ErrNotExist) {
		t.Error("a bot token file was written without a token")
	}
	if h.env("dashboard.env", "DISCORD_BOT_TOKEN_FILE") != "" {
		t.Error("DISCORD_BOT_TOKEN_FILE points at a file that does not exist")
	}
	if !strings.Contains(r.stdout, "could not check") {
		t.Errorf("without a bot token the redirect cannot be checked; output:\n%s", r.stdout)
	}
}

func TestDiscordRefusesBadValuesAndWritesNothing(t *testing.T) {
	for name, tc := range map[string]struct {
		mutate func(h *discordHarness) []string
		want   string
	}{
		"wrong secret": {func(h *discordHarness) []string {
			return []string{"--client-id-file", h.file("id", h.f.ClientID), "--client-secret-file", h.file("s", h.f.ClientSecret+"x")}
		}, "Discord refused the client ID and secret"},
		"bot of another application": {func(h *discordHarness) []string {
			h.f.BotAppID = "999999999999999999"
			return h.credentials()
		}, "the bot token belongs to application 999999999999999999, not " + "123456789012345678"},
		"rejected bot token": {func(h *discordHarness) []string {
			args := h.credentials()
			args[5] = h.file("bot2", h.f.BotToken+"x")
			return args
		}, "Discord refused the bot token"},
		"client ID not an ID": {func(h *discordHarness) []string {
			return []string{"--client-id-file", h.file("id", "my-app"), "--client-secret-file", h.file("s", h.f.ClientSecret)}
		}, "not a Discord client ID"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newDiscordHarness(t)
			before := h.snapshot()
			r := h.run(append([]string{"--deploy-dir", h.dir, "--yes"}, tc.mutate(h)...)...)
			if r.code != 1 || !strings.Contains(r.stderr, tc.want) {
				t.Errorf("exit %d, want 1 and %q:\n%s\n%s", r.code, tc.want, r.stdout, r.stderr)
			}
			if h.snapshot() != before {
				t.Error("files changed after a refusal")
			}
		})
	}
}

func TestDiscordWithoutYesWritesNothing(t *testing.T) {
	for _, mode := range [][]string{{}, {"--dry-run"}} {
		h := newDiscordHarness(t)
		before := h.snapshot()
		r := h.run(append(append([]string{"--deploy-dir", h.dir}, mode...), h.credentials()...)...)
		if r.code != 0 || !strings.Contains(r.stdout, "create  secrets/app/discord-client-secret") {
			t.Errorf("%v: %d\n%s\n%s", mode, r.code, r.stdout, r.stderr)
		}
		if h.snapshot() != before {
			t.Errorf("%v: files changed", mode)
		}
	}
}

func TestDiscordOnACompleteInstallChangesNothing(t *testing.T) {
	h := newDiscordHarness(t)
	if r := h.run(append([]string{"--deploy-dir", h.dir, "--yes"}, h.credentials()...)...); r.code != 0 {
		t.Fatal(r.stderr)
	}
	before := h.snapshot()
	time.Sleep(20 * time.Millisecond) // a rewrite would show in the modification times
	// No values given: what is on disk is checked again, and nothing is written.
	r := h.run("--deploy-dir", h.dir, "--yes")
	if r.code != 0 || !strings.Contains(r.stdout, "nothing to do") || !strings.Contains(r.stdout, "Discord accepted the client ID and secret") {
		t.Errorf("%d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
	// The same values given again: still nothing written.
	r = h.run(append([]string{"--deploy-dir", h.dir, "--yes"}, h.credentials()...)...)
	if r.code != 0 || !strings.Contains(r.stdout, "nothing to do") {
		t.Errorf("%d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
	if after := h.snapshot(); after != before {
		t.Errorf("files changed:\n%s\n---\n%s", before, after)
	}
}

func TestDiscordKeepsDifferentExistingValues(t *testing.T) {
	h := newDiscordHarness(t)
	if r := h.run(append([]string{"--deploy-dir", h.dir, "--yes"}, h.credentials()...)...); r.code != 0 {
		t.Fatal(r.stderr)
	}
	before := h.snapshot()
	args := []string{"--deploy-dir", h.dir, "--yes",
		"--client-id-file", h.file("id2", "223456789012345678"),
		"--client-secret-file", h.file("s2", h.f.ClientSecret+"-other"),
		"--bot-token-file", h.file("b2", h.f.BotToken+"-other")}
	r := h.run(args...)
	if r.code != 0 {
		t.Fatalf("%d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
	for _, want := range []string{
		`conflict  dashboard.env DISCORD_CLIENT_ID`,
		`conflict  secrets/app/discord-client-secret`,
		`conflict  secrets/app/discord-bot-token`,
		"--rotate discord-client-secret",
	} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, r.stdout)
		}
	}
	if h.snapshot() != before {
		t.Error("an existing value was replaced")
	}
}

func TestDiscordRedirectNotRegisteredIsSaid(t *testing.T) {
	h := newDiscordHarness(t)
	h.f.RedirectURIs = []string{"https://elsewhere.example/auth/callback"}
	r := h.run(append([]string{"--deploy-dir", h.dir, "--dry-run"}, h.credentials()...)...)
	if r.code != 0 || !strings.Contains(r.stdout, "is NOT registered") {
		t.Errorf("%d\n%s", r.code, r.stdout)
	}
	h.f.OmitRedirectURIs = true
	r = h.run(append([]string{"--deploy-dir", h.dir, "--dry-run"}, h.credentials()...)...)
	if r.code != 0 || !strings.Contains(r.stdout, "could not check") {
		t.Errorf("%d\n%s", r.code, r.stdout)
	}
}

func TestDiscordWaitsOutARateLimit(t *testing.T) {
	h := newDiscordHarness(t)
	h.f.RateLimit, h.f.RetryAfter = 1, 2.25
	if r := h.run(append([]string{"--deploy-dir", h.dir, "--dry-run"}, h.credentials()...)...); r.code != 0 {
		t.Fatalf("%d %s", r.code, r.stderr)
	}
	if len(h.waits) != 1 || h.waits[0] != 2250*time.Millisecond {
		t.Errorf("waits %v", h.waits)
	}
}

func TestDiscordValuesNeverComeFromFlags(t *testing.T) {
	h := newDiscordHarness(t)
	for _, flag := range []string{"--client-secret", "--bot-token", "--client-id"} {
		if r := h.run("--deploy-dir", h.dir, flag, "x"); r.code != 2 {
			t.Errorf("%s accepted: %d", flag, r.code)
		}
	}
}

// connected sets up a deploy directory with stored credentials, ready for connect.
func connected(t *testing.T) *discordHarness {
	h := newDiscordHarness(t)
	if r := h.run(append([]string{"--deploy-dir", h.dir, "--yes"}, h.credentials()...)...); r.code != 0 {
		t.Fatal(r.stderr)
	}
	h.tty = true
	return h
}

func linkScopes(t *testing.T, link string) string {
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get("scope")
}

func TestConnectOneConsentGivesOwnerAndWebhook(t *testing.T) {
	h := connected(t)
	h.approve()
	r := h.run("connect", "--deploy-dir", h.dir)
	if r.code != 0 {
		t.Fatalf("%d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
	links := linkRE.FindAllString(r.stdout, -1)
	if len(links) != 1 || linkScopes(t, links[0]) != "identify webhook.incoming" {
		t.Fatalf("links %q", links)
	}
	hooks := h.f.Webhooks()
	if len(hooks) != 1 {
		t.Fatalf("webhooks %d", len(hooks))
	}
	if h.env("dashboard.env", "BOOTSTRAP_OWNER_DISCORD_ID") != h.f.UserID || h.env("dashboard.env", "BOOTSTRAP_OWNER_NAME") != "Club Admin" {
		t.Errorf("owner not written:\n%s", h.read("dashboard.env"))
	}
	checkSecretFile(t, h, "secrets/app/discord-webhook-url", hooks[0].URL)
	checkSecretFile(t, h, "secrets/backup/discord-webhook-url", hooks[0].URL)
	if h.env("dashboard.env", "DISCORD_WEBHOOK_URL_FILE") != "/run/secrets/app/discord-webhook-url" ||
		h.env("backup.env", "DISCORD_WEBHOOK_URL_FILE") != "/run/secrets/backup/discord-webhook-url" {
		t.Error("webhook file keys not set")
	}
	for _, want := range []string{"up -d --force-recreate dashboard backup", "Club Admin (" + h.f.UserID + ")", "channel " + hooks[0].ChannelID} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, r.stdout)
		}
	}
	// connect restarts nothing and the paste was read hidden.
	if !strings.Contains(h.prompts[0], "input hidden") {
		t.Errorf("prompt %q", h.prompts[0])
	}
	// Run again: nothing left to ask, no link, no request, no write.
	before, requests := h.snapshot(), len(h.f.Requests())
	r = h.run("connect", "--deploy-dir", h.dir)
	if r.code != 0 || linkRE.MatchString(r.stdout) || !strings.Contains(r.stdout, "nothing to do") {
		t.Errorf("second connect: %d\n%s", r.code, r.stdout)
	}
	if h.snapshot() != before || len(h.f.Requests()) != requests {
		t.Error("the second connect changed something or called Discord")
	}
}

func TestConnectFallsBackToASecondLinkForTheWebhook(t *testing.T) {
	h := connected(t)
	h.f.CombinedWebhook = false
	h.approve()
	h.approve()
	r := h.run("connect", "--deploy-dir", h.dir)
	if r.code != 0 {
		t.Fatalf("%d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
	links := linkRE.FindAllString(r.stdout, -1)
	if len(links) != 2 || linkScopes(t, links[0]) != "identify webhook.incoming" || linkScopes(t, links[1]) != "webhook.incoming" {
		t.Fatalf("links %q", links)
	}
	if h.env("dashboard.env", "BOOTSTRAP_OWNER_DISCORD_ID") != h.f.UserID || len(h.f.Webhooks()) != 1 {
		t.Errorf("owner %q, webhooks %d", h.env("dashboard.env", "BOOTSTRAP_OWNER_DISCORD_ID"), len(h.f.Webhooks()))
	}
	checkSecretFile(t, h, "secrets/app/discord-webhook-url", h.f.Webhooks()[0].URL)
}

func TestConnectAsksOnlyForWhatIsMissing(t *testing.T) {
	h := connected(t)
	// The owner is already set (by `setup host --owner-discord-id`): only the webhook is asked for.
	if r := hostCmd(t, h.owner, "", "--deploy-dir", h.dir, "--yes", "--owner-discord-id", "123456789012345699"); r.code != 0 {
		t.Fatal(r.stderr)
	}
	h.approve()
	r := h.run("connect", "--deploy-dir", h.dir)
	if r.code != 0 {
		t.Fatalf("%d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
	if links := linkRE.FindAllString(r.stdout, -1); len(links) != 1 || linkScopes(t, links[0]) != "webhook.incoming" {
		t.Errorf("links %q", links)
	}
	if h.env("dashboard.env", "BOOTSTRAP_OWNER_DISCORD_ID") != "123456789012345699" {
		t.Error("the configured owner was replaced")
	}
	if !strings.Contains(r.stdout, "up -d --force-recreate dashboard backup") {
		t.Errorf("restart line:\n%s", r.stdout)
	}
}

func TestConnectRefusesAnotherStateAndAReusedCode(t *testing.T) {
	h := connected(t)
	h.f.CombinedWebhook = false
	var first string
	// Round one: a paste from another link (wrong state) is refused, then the right one.
	h.answers = append(h.answers, func(string) string { return "setup." + strings.Repeat("Z", 43) + ":abcdefghij0123456789" })
	h.answers = append(h.answers, func(printed string) string {
		code, state, err := h.f.Approve(lastLink(t, printed))
		if err != nil {
			t.Fatal(err)
		}
		first = code
		return state + ":" + code
	})
	// Round two: the first code again, under the new state, is refused without asking
	// Discord; then a code Discord already took (invalid_grant); then a fresh one.
	h.answers = append(h.answers, func(printed string) string {
		_, state, _ := h.f.Approve(lastLink(t, printed))
		return state + ":" + first
	})
	h.answers = append(h.answers, func(printed string) string {
		code, state, _ := h.f.Approve(lastLink(t, printed))
		h.f.Burn(code) // Discord already took it
		return state + ":" + code
	})
	h.approve()
	r := h.run("connect", "--deploy-dir", h.dir)
	if r.code != 0 {
		t.Fatalf("%d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
	for _, want := range []string{"not from the link above", "already used", "Discord refused that code"} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, r.stderr)
		}
	}
	tokens := 0
	for _, req := range h.f.Requests() {
		if req == "POST /api/oauth2/token" {
			tokens++
		}
	}
	// credentials check (1, from connected) + round one + the refused code + the fresh one;
	// the wrong state and the locally reused code never reached Discord.
	if tokens != 4 {
		t.Errorf("%d token requests, want 4: %q", tokens, h.f.Requests())
	}
}

func TestConnectGivesUpAfterThreeBadPastesAndWritesNothing(t *testing.T) {
	h := connected(t)
	before := h.snapshot()
	h.type_("garbage", "setup.short:abc", "setup."+strings.Repeat("Z", 43)+":abcdefghij0123456789")
	r := h.run("connect", "--deploy-dir", h.dir)
	if r.code != 1 {
		t.Errorf("exit %d\n%s", r.code, r.stderr)
	}
	if h.snapshot() != before || len(h.f.Webhooks()) != 0 {
		t.Error("something was written")
	}
}

func TestConnectNeedsATerminalAndStoredCredentials(t *testing.T) {
	h := connected(t)
	h.tty = false
	if r := h.run("connect", "--deploy-dir", h.dir); r.code != 1 || !strings.Contains(r.stderr, "terminal") {
		t.Errorf("no terminal: %d %s", r.code, r.stderr)
	}
	fresh := newDiscordHarness(t)
	fresh.tty = true
	if r := fresh.run("connect", "--deploy-dir", fresh.dir); r.code != 1 || !strings.Contains(r.stderr, "scripts/setup discord") {
		t.Errorf("no credentials: %d %s", r.code, r.stderr)
	}
	if n := len(fresh.f.Requests()); n != 0 {
		t.Errorf("%d requests", n)
	}
}

func TestConnectDryRunReadsOnly(t *testing.T) {
	h := connected(t)
	before, requests := h.snapshot(), len(h.f.Requests())
	r := h.run("connect", "--deploy-dir", h.dir, "--dry-run")
	if r.code != 0 || !strings.Contains(r.stdout, "identify webhook.incoming") || linkRE.MatchString(r.stdout) {
		t.Errorf("%d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
	if h.snapshot() != before || len(h.f.Requests()) != requests || len(h.prompts) != 0 {
		t.Error("the dry run wrote, called Discord or asked")
	}
}
