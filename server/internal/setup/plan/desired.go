package plan

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Inputs are the values an admin supplies. Everything else in Desired comes from
// docs/deploy/cloudflare.md and docs/deploy/fresh-install.md, quoted beside each value.
type Inputs struct {
	AppURL         string // APP_URL, for example https://officers.example.org
	BackupHostname string // empty: "backups." + the parent of the APP_URL host
	OfficersGroup  string // empty: "Officers"
	LaunchpadGroup string // empty: no Launchpad group (not every club runs Launchpad)
	NamePrefix     string // prefixed to every resource name, for a staging install
	TunnelID       string // optional: match the tunnel by ID before name
}

// Desired is what a complete Cloudflare setup for one dashboard looks like.
type Desired struct {
	Hostname       string
	BackupHostname string
	TunnelID       string

	Tunnel             TunnelSpec
	IdentityProvider   string // Access identity provider type
	Groups             []GroupSpec
	ServiceTokens      []ServiceTokenSpec
	Apps               []AppSpec
	Rules              []RuleSpec
	BotFightMode       bool
	OrgSessionDuration string
	D1Database         string
	Worker             WorkerSpec
}

type TunnelSpec struct{ Name, Service string }

// GroupSpec is an Access rule group. Setup only checks that it exists: membership is
// never compared or edited.
type GroupSpec struct{ Key, Name string }

type ServiceTokenSpec struct {
	Key, Name string
	// SecretFile is the host file that holds the client secret, when there is one.
	SecretFile string
}

type AppSpec struct {
	Key, Name string
	// Destinations are "host" or "host/path", normalized with NormalizeDestination.
	Destinations    []string
	SessionDuration string // empty: not checked
	OnlyIdP         bool   // allowed_idps must be exactly the one-time PIN provider
	AutoRedirect    bool   // auto_redirect_to_identity must be true
	ServiceAuth401  bool   // service_auth_401_redirect must be true
	Policy          PolicySpec
	// ForbiddenDecisions are decisions no policy on the app may have.
	ForbiddenDecisions []string
}

// PolicySpec is the one policy each app needs. It is matched by Decision within the
// app; Name is used only when setup creates it.
type PolicySpec struct {
	Name         string
	Decision     string   // allow, bypass, non_identity
	Groups       []string // GroupSpec keys
	Everyone     bool
	ServiceToken string // ServiceTokenSpec key
}

type RuleSpec struct {
	Key         string
	Phase       string
	Description string
	Expression  string
	Action      string
	// Params are checked as a subset of the rule's action_parameters: a list must
	// contain every listed value, anything else must be equal.
	Params   map[string]any
	Position string // "first", "last" or ""
}

type WorkerSpec struct {
	Script, Hostname, D1Binding, Secret string
}

// Stable keys used in action IDs.
const (
	GroupOfficers  = "officers"
	GroupLaunchpad = "launchpad"
	TokenServices  = "services"
	TokenBackups   = "backups"
	AppSite        = "site"
	AppAgents      = "agents"
	AppServices    = "services"
	AppBackups     = "backups"
	RuleSkip       = "firewall.skip"
	RuleCache      = "cache.bypass"
	RuleConfig     = "config.consent"
)

// NewDesired builds the desired state from the admin's inputs.
func NewDesired(in Inputs) (Desired, error) {
	host, err := hostFromAppURL(in.AppURL)
	if err != nil {
		return Desired{}, err
	}
	backup := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(in.BackupHostname), "."))
	if backup == "" {
		// fresh-install.md §2: "The backup service uses a separate hostname, such as
		// `backups.example.org`"; §4.4: "Create an Access application for `backups.<your-domain>`".
		i := strings.IndexByte(host, '.')
		if i < 0 || !strings.Contains(host[i+1:], ".") {
			return Desired{}, errors.New("cannot derive the backup hostname from APP_URL; pass --backup-hostname")
		}
		backup = "backups." + host[i+1:]
	}
	if backup == host {
		return Desired{}, errors.New("the backup hostname must differ from the dashboard hostname")
	}
	if strings.ContainsAny(backup, "/:@ ") {
		return Desired{}, fmt.Errorf("invalid backup hostname %q", backup)
	}
	officers := strings.TrimSpace(in.OfficersGroup)
	if officers == "" {
		officers = "Officers" // cloudflare.md §2: "Name `Officers`."
	}
	launchpad := strings.TrimSpace(in.LaunchpadGroup)
	p := in.NamePrefix
	n := func(s string) string { return p + s }

	d := Desired{
		Hostname:       host,
		BackupHostname: backup,
		TunnelID:       strings.ToLower(strings.TrimSpace(in.TunnelID)),
		Tunnel: TunnelSpec{
			// cloudflare.md §1.1: "Name it `officer-dashboard`"
			Name: n("officer-dashboard"),
			// cloudflare.md §1.3: "**Service URL:** `http://dashboard:8090`"
			Service: "http://dashboard:8090",
		},
		// cloudflare.md §2.1: "**One-time PIN**". API type string "onetimepin":
		// https://developers.cloudflare.com/api/resources/zero_trust/subresources/identity_providers/methods/list/
		IdentityProvider: "onetimepin",
		// cloudflare.md §6.1: "**Bot Fight Mode: off**"
		BotFightMode: false,
		// cloudflare.md §3: "**Set your global session duration** → **Edit** → **1 month**".
		// 1 month = 730h is an assumption: see the open questions in the PR.
		OrgSessionDuration: "730h",
		// fresh-install.md §4.2: "Create a D1 database named `dashboard-backups`."
		D1Database: n("dashboard-backups"),
		Worker: WorkerSpec{
			// server/deploy/backup-sink/wrangler.toml: name = "dashboard-backup-sink"
			Script: n("dashboard-backup-sink"),
			// fresh-install.md §4.6: routes = [{ pattern = "backups.example.org", custom_domain = true }]
			Hostname: backup,
			// wrangler.toml: binding = "BACKUPS"
			D1Binding: "BACKUPS",
			// fresh-install.md §4.5: "npx wrangler secret put BACKUP_BEARER_SECRET"
			Secret: "BACKUP_BEARER_SECRET",
		},
	}

	d.Groups = []GroupSpec{{Key: GroupOfficers, Name: n(officers)}}
	siteGroups := []string{GroupOfficers}
	if launchpad != "" {
		// cloudflare.md §2: "Name `Launchpad`."
		d.Groups = append(d.Groups, GroupSpec{Key: GroupLaunchpad, Name: n(launchpad)})
		siteGroups = append(siteGroups, GroupLaunchpad)
	}

	d.ServiceTokens = []ServiceTokenSpec{
		// cloudflare.md §5.1: "Name it `officer-dashboard services`." It has no host file.
		{Key: TokenServices, Name: n("officer-dashboard services")},
		// fresh-install.md §4.3: "Create a Cloudflare Access service token named `dashboard-backups-host`."
		{Key: TokenBackups, Name: n("dashboard-backups-host"), SecretFile: "secrets/backup/access-client-secret"},
	}

	agentPaths := []string{
		// cloudflare.md §4.2: "one of these **Paths** (no slash at the start)"
		"mcp",
		"oauth/register",
		"oauth/token",
		".well-known/oauth-protected-resource",
		".well-known/oauth-protected-resource/mcp",
		".well-known/oauth-authorization-server",
	}
	agentDest := make([]string, len(agentPaths))
	for i, path := range agentPaths {
		agentDest[i] = host + "/" + path
	}
	siteName := "Officer Dashboard"        // cloudflare.md §3.1: "**Application name:** `Officer Dashboard`."
	sitePolicy := "Officers and Launchpad" // cloudflare.md §3.4: "Name `Officers and Launchpad`, Action **Allow**."
	if launchpad == "" {
		sitePolicy = "Officers"
	}
	d.Apps = []AppSpec{
		{
			Key: AppSite, Name: n(siteName),
			// cloudflare.md §3.2: "Subdomain `<subdomain>`, Domain `<your-domain>`, **Path empty**."
			Destinations: []string{host},
			// cloudflare.md §3.3: "**Session Duration:** **1 month**" (730h assumed, see Org).
			SessionDuration: "730h",
			// cloudflare.md §3.5: "only **One-time PIN**. Untick any other. Turn on
			// **Apply instant authentication**"
			OnlyIdP: true, AutoRedirect: true,
			// cloudflare.md §3.4: "**Include** → selector **Rule group** → `Officers`. Add a
			// second include: **Rule group** → `Launchpad`."
			Policy: PolicySpec{Name: n(sitePolicy), Decision: "allow", Groups: siteGroups},
		},
		{
			// cloudflare.md §4.1: "**Application name:** `Officer Dashboard: agents (bypass)`."
			Key: AppAgents, Name: n("Officer Dashboard: agents (bypass)"),
			Destinations: agentDest,
			// cloudflare.md §4.3: "a new policy, Action **Bypass**, **Include** → **Everyone**."
			// The docs give the policy no name; this one is used only when setup creates it.
			Policy: PolicySpec{Name: n("Everyone (bypass)"), Decision: "bypass", Everyone: true},
		},
		{
			// cloudflare.md §5.2: "**Application name:** `Officer Dashboard: services (service token)`."
			Key: AppServices, Name: n("Officer Dashboard: services (service token)"),
			// cloudflare.md §5.2: "Paths `api/ingest` and `api/agent`."
			Destinations: []string{host + "/api/ingest", host + "/api/agent"},
			// cloudflare.md §5.2: "turn on **401 Response for Service Auth policies**"
			ServiceAuth401: true,
			// cloudflare.md §5.2: "a new policy named `Service token`, Action **Service Auth**
			// … **Include** → selector **Service Token** → `officer-dashboard services`."
			// Service Auth is the API decision "non_identity".
			Policy: PolicySpec{Name: n("Service token"), Decision: "non_identity", ServiceToken: TokenServices},
		},
		{
			// fresh-install.md §4.4: "Create an Access application for `backups.<your-domain>`
			// covering all paths." The docs give it no name; this one is used only on create.
			Key: AppBackups, Name: n("Dashboard backups (service token)"),
			Destinations: []string{backup},
			// fresh-install.md §4.4: "Add a **Service Auth** policy allowing only that service token."
			Policy: PolicySpec{Name: n("Backup host token"), Decision: "non_identity", ServiceToken: TokenBackups},
			// fresh-install.md §4.4: "Do not add a public Allow or Bypass policy."
			ForbiddenDecisions: []string{"allow", "bypass"},
		},
	}

	// cloudflare.md §6.2: the skip-rule expression, with <your-hostname> filled in.
	agentExpr := `(http.host eq "` + host + `" and (http.request.uri.path eq "/mcp" or starts_with(http.request.uri.path, "/mcp/") or http.request.uri.path in {"/oauth/register" "/oauth/token" "/.well-known/oauth-authorization-server"} or starts_with(http.request.uri.path, "/.well-known/oauth-protected-resource") or starts_with(http.request.uri.path, "/api/ingest/") or starts_with(http.request.uri.path, "/api/agent/")))`
	d.Rules = []RuleSpec{
		{
			Key: RuleSkip, Phase: "http_request_firewall_custom",
			// cloudflare.md §6.2: "**Rule name:** `Agents and services: skip bot checks`."
			Description: n("Agents and services: skip bot checks"),
			Expression:  agentExpr,
			// cloudflare.md §6.2: "**Action:** **Skip**. Tick **Browser Integrity Check** and
			// **Security Level**." API product names "bic" and "securityLevel":
			// https://developers.cloudflare.com/waf/custom-rules/skip/options/
			Action: "skip",
			Params: map[string]any{"products": []any{"bic", "securityLevel"}},
			// cloudflare.md §6.2: "move the rule to the **top** of the list."
			Position: "first",
		},
		{
			Key: RuleCache, Phase: "http_request_cache_settings",
			// cloudflare.md §7: "**Rule name:** `Agents and services: bypass cache`."
			Description: n("Agents and services: bypass cache"),
			// cloudflare.md §7: "the same expression as in step 6."
			Expression: agentExpr,
			// cloudflare.md §7: "**Cache eligibility:** **Bypass cache**." Action name from
			// https://developers.cloudflare.com/cache/how-to/cache-rules/create-api/
			// ("Set the rule action to `set_cache_settings`"); "cache": false is not shown
			// there (open question).
			Action: "set_cache_settings",
			Params: map[string]any{"cache": false},
			// cloudflare.md §7: "**Place at:** last."
			Position: "last",
		},
		{
			Key: RuleConfig, Phase: "http_config_settings",
			// cloudflare.md §8: "**Rule name:** `Consent page: no rewriting`."
			Description: n("Consent page: no rewriting"),
			// cloudflare.md §8: the expression, with <your-hostname> filled in.
			Expression: `(http.host eq "` + host + `" and http.request.uri.path eq "/oauth/authorize")`,
			// cloudflare.md §8: "**Rocket Loader**: off and **Email Obfuscation**: off".
			// Names from https://developers.cloudflare.com/rules/configuration-rules/settings/
			Action: "set_config",
			Params: map[string]any{"rocket_loader": false, "email_obfuscation": false},
		},
	}
	return d, nil
}

func hostFromAppURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("APP_URL is required (set it or pass --app-url)")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		return "", fmt.Errorf("APP_URL must be an https:// origin, got %q", raw)
	}
	if u.Port() != "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.User != nil {
		return "", fmt.Errorf("APP_URL must be a bare origin such as https://officers.example.org, got %q", raw)
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if !strings.Contains(host, ".") {
		return "", fmt.Errorf("APP_URL host %q is not a public hostname", host)
	}
	return host, nil
}

// NormalizeDestination turns an Access destination URI into "host" or "host/path":
// lower-case host, no scheme, no leading or trailing slash on the path.
func NormalizeDestination(uri string) string {
	s := strings.TrimSpace(uri)
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	host, path, _ := strings.Cut(s, "/")
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	path = strings.Trim(path, "/")
	if path == "" {
		return host
	}
	return host + "/" + path
}
