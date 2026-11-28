// Package plan compares what a dashboard needs from Cloudflare (Desired) with what an
// account has (Observed) and lists the difference as Actions. It is pure: no network,
// no files. cfprovision fills Observed; the setup command renders the actions.
package plan

import (
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"
)

// Kind says what setup would do about one resource.
type Kind string

const (
	OK         Kind = "ok"         // present and as the docs describe
	Create     Kind = "create"     // missing; setup would create it
	Drift      Kind = "drift"      // present but different; reported, never changed by default
	Unknowable Kind = "unknowable" // cannot be determined from the API (secret, permission, unverified shape)
	Manual     Kind = "manual"     // a person must do this step; setup cannot
)

// Kinds lists every Kind in display order.
var Kinds = []Kind{OK, Create, Drift, Unknowable, Manual}

// Action is one line of the plan.
type Action struct {
	ID         string   `json:"id"`
	Kind       Kind     `json:"kind"`
	Resource   string   `json:"resource"`
	Name       string   `json:"name"`
	ExistingID string   `json:"existing_id,omitempty"`
	Detail     []string `json:"detail,omitempty"`
}

// Seen is the common lookup outcome. When Unknown is set, Found and ID mean nothing.
type Seen struct {
	Found   bool
	ID      string
	Unknown string
}

type Observed struct {
	hostname      string // set by Diff for routed
	Zone          Zone
	Token         Token
	Org           Org
	Tunnel        Tunnel
	DNS           DNS
	IdP           Seen
	Groups        map[string]Seen // by GroupSpec key
	ServiceTokens map[string]ServiceToken
	Apps          map[string]App
	Rules         map[string]Rule
	BotFightMode  BotFightMode
	D1            Seen
	Worker        Worker
	// OtherApps lists, per hostname (the dashboard's and the backup Worker's), every
	// Access application with a destination on that hostname, bare or with a path, that
	// matched none of the documented applications. Each carries its policies.
	OtherApps map[string][]App
}

type Zone struct {
	Seen
	Name, Status, AccountID string
	// Plan is the zone's plan.legacy_id ("free", "pro", "business", "enterprise"), which a
	// live Free zone returned; empty when Cloudflare did not return it.
	Plan string
}

// Token is the setup token's own status, from Cloudflare's verify endpoint.
type Token struct {
	Seen
	Status, ExpiresOn string
}

type Org struct {
	Seen
	AuthDomain string
	// SessionDuration is the global session duration; empty when the organization does
	// not set one (a live account omitted the key).
	SessionDuration string
}

type Tunnel struct {
	Seen
	Name, ConfigSrc string
	Status          string // inactive, degraded, healthy, down
	MatchedBy       string // "id", "cname" or "name"
	Ingress         []Ingress
	IngressUnknown  string
	// The three matching keys, in order: the tunnel ID from the host's token, the
	// tunnel ID in the hostname's CNAME target, and the doc name. TokenID and CNAMEID
	// are the IDs looked for; ByToken and ByCNAME are the tunnels those keys found in
	// the account ("" for none), and ByName lists every tunnel with the doc name.
	TokenID, CNAMEID string
	ByToken, ByCNAME string
	ByName           []string
}

type Ingress struct{ Hostname, Path, Service string }

type DNS struct {
	Unknown string
	Records []DNSRecord
}

type DNSRecord struct {
	ID, Type, Content string
	Proxied           *bool
}

type ServiceToken struct {
	Seen
	ExpiresAt string
}

type App struct {
	Seen
	Name            string
	AUD             string // the application audience tag (aud), which a live list returned
	MatchedBy       string // "destinations" (exact set), "covers" (a superset) or "name"
	Destinations    []string
	SessionDuration string
	AllowedIdPs     []string
	AutoRedirect    *bool
	ServiceAuth401  *bool
	Policies        []Policy
	PoliciesUnknown string
}

type Policy struct {
	ID, Name, Decision string
	Include            []Include
	Reusable           bool
	// Conditions is true when the policy also has require or exclude rules, which
	// setup does not compare (they can only narrow who passes).
	Conditions bool
}

// Include is one include rule: Type "group" or "service_token" with an ID, or
// "everyone", or Type "other" for any selector setup does not use.
type Include struct{ Type, ID string }

type Rule struct {
	Seen
	RulesetID string
	MatchedBy string // "description" or "expression"
	Index     int
	// Count is the number of rules in the phase's entry point ruleset; it is set whether
	// or not the rule was found, so the free rule slots can be counted.
	Count       int
	Description string
	Expression  string
	Action      string
	Enabled     *bool
	Params      map[string]any
	// PhaseEmpty means the zone has no entry point ruleset for the phase yet (404, code
	// 10003 on a live account), so the first write creates the ruleset itself.
	PhaseEmpty bool
}

type BotFightMode struct {
	Seen
	On *bool
}

type Worker struct {
	Seen
	D1BindingID    string
	BindingUnknown string
	Secrets        []string
	SecretsUnknown string
	// WorkersDev and PreviewURLs are the script's workers.dev subdomain switches.
	WorkersDev, PreviewURLs *bool
	SubdomainUnknown        string
	Domain                  WorkerDomain
}

type WorkerDomain struct {
	Seen
	Service string
}

// check collects findings for one action; its Kind is the worst of them.
type check struct {
	a                      Action
	drift, unknown, notes  []string
	create, manual, absent bool
}

func (c *check) driftf(f string, a ...any)   { c.drift = append(c.drift, fmt.Sprintf(f, a...)) }
func (c *check) unknownf(f string, a ...any) { c.unknown = append(c.unknown, fmt.Sprintf(f, a...)) }
func (c *check) notef(f string, a ...any)    { c.notes = append(c.notes, fmt.Sprintf(f, a...)) }

func (c *check) done() Action {
	a := c.a
	switch {
	case c.manual:
		a.Kind = Manual
	case c.create:
		a.Kind = Create
	case len(c.drift) > 0:
		a.Kind = Drift
	case len(c.unknown) > 0:
		a.Kind = Unknowable
	default:
		a.Kind = OK
	}
	a.Detail = append(append(append(a.Detail, c.drift...), c.unknown...), c.notes...)
	return a
}

// seen applies a lookup outcome: unknown, absent (create) or found (note the ID).
// It reports whether the resource was found, so the caller goes on to compare fields.
func (c *check) seen(s Seen) bool {
	switch {
	case s.Unknown != "":
		c.unknownf("%s", s.Unknown)
		return false
	case !s.Found:
		c.create = true
		return false
	}
	c.a.ExistingID = s.ID
	return true
}

// Diff lists one action per desired resource, in a fixed order.
func Diff(d Desired, o Observed) []Action {
	o.hostname = d.Hostname
	var out []Action
	add := func(c *check) { out = append(out, c.done()) }

	zone := &check{a: Action{ID: "zone", Resource: "Zone", Name: d.Hostname}}
	switch {
	case o.Zone.Unknown != "":
		zone.unknownf("%s", o.Zone.Unknown)
	case !o.Zone.Found:
		zone.manual = true
		zone.notef("no zone in this account contains %s: add the domain and move its nameservers (docs/deploy/cloudflare.md step 0)", d.Hostname)
	default:
		zone.a.ExistingID = o.Zone.ID
		zone.a.Name = o.Zone.Name
		if o.Zone.Status != "active" {
			zone.manual = true
			zone.notef("zone is %q, not active: finish moving the nameservers (docs/deploy/cloudflare.md step 0.2)", o.Zone.Status)
		}
	}
	add(zone)
	if !o.Zone.Found || o.Zone.Unknown != "" || o.Zone.Status != "active" {
		// Everything else lives in the zone or its account; nothing more can be checked.
		return out
	}

	tok := &check{a: Action{ID: "token", Resource: "Setup token", Name: "Cloudflare API token"}}
	switch {
	case o.Token.Unknown != "":
		tok.unknownf("%s", o.Token.Unknown)
	case !o.Token.Found:
		tok.unknownf("Cloudflare's verify endpoints did not answer for this token")
	case o.Token.Status != "active":
		tok.driftf("token status is %q, not active", o.Token.Status)
	default:
		tok.a.Name = "active"
		if o.Token.ExpiresOn != "" {
			tok.notef("expires %s; revoke it when setup is done", o.Token.ExpiresOn)
		}
	}
	add(tok)

	org := &check{a: Action{ID: "zero_trust", Resource: "Zero Trust organization", Name: "team"}}
	switch {
	case o.Org.Unknown != "":
		org.unknownf("%s", o.Org.Unknown)
	case !o.Org.Found:
		org.manual = true
		org.notef("Zero Trust is not set up: open it and choose a team name (docs/deploy/cloudflare.md step 0.3)")
	default:
		org.a.Name = o.Org.AuthDomain
	}
	add(org)

	ses := &check{a: Action{ID: "zero_trust.session", Resource: "Global session duration", Name: d.OrgSessionDuration + " (1 month)"}}
	switch {
	case o.Org.Unknown != "" || !o.Org.Found:
		ses.unknownf("needs the Zero Trust organization")
	case o.Org.SessionDuration == "":
		// The organization holds account-wide Access settings, so setup leaves this to
		// the person unless the apply engine is explicitly allowed to write it.
		ses.manual = true
		ses.notef("not set (Cloudflare's default is 24h). Setup does not write the Zero Trust organization unless allowed. " +
			"In Zero Trust: Access controls → Access settings → Set your global session duration → Edit → 1 month → Save (docs/deploy/cloudflare.md step 3)")
	case !sameDuration(o.Org.SessionDuration, d.OrgSessionDuration):
		ses.driftf("set to %s; setup never changes a global value that is already set", o.Org.SessionDuration)
	}
	add(ses)

	// Tunnel and its route.
	out = append(out, diffTunnel(d, o.Tunnel))

	route := &check{a: Action{ID: "tunnel.route", Resource: "Tunnel route", Name: d.Hostname + " → " + d.Tunnel.Service}}
	switch {
	case o.Tunnel.Unknown != "":
		route.unknownf("tunnel could not be read")
	case !o.Tunnel.Found:
		route.create = true
	case o.Tunnel.ConfigSrc == "local":
		route.unknownf("tunnel is configured by a local cloudflared file; Cloudflare does not hold its routes")
	case o.Tunnel.IngressUnknown != "":
		route.unknownf("%s", o.Tunnel.IngressUnknown)
	default:
		found := false
		for _, r := range o.Tunnel.Ingress {
			if strings.EqualFold(r.Hostname, d.Hostname) && strings.Trim(r.Path, "/") == "" {
				found = true
				if r.Service != d.Tunnel.Service {
					route.driftf("routes %s to %s, docs ask for %s", d.Hostname, r.Service, d.Tunnel.Service)
				}
			}
		}
		if !found {
			route.create = true
		}
	}
	add(route)

	dns := &check{a: Action{ID: "dns", Resource: "DNS record", Name: d.Hostname}}
	switch {
	case o.DNS.Unknown != "":
		dns.unknownf("%s", o.DNS.Unknown)
	case len(o.DNS.Records) == 0:
		dns.create = true
	case o.Tunnel.Unknown != "":
		dns.unknownf("the tunnel could not be read, so the CNAME target cannot be checked")
	default:
		target := ""
		if o.Tunnel.Found && o.Tunnel.Unknown == "" {
			// A tunnel's public hostname is a proxied CNAME to <tunnel-id>.cfargotunnel.com
			// (https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/routing-to-tunnel/dns/).
			target = o.Tunnel.ID + ".cfargotunnel.com"
		}
		for _, r := range o.DNS.Records {
			dns.a.ExistingID = r.ID
			switch {
			case r.Type != "CNAME" || target == "" || !strings.EqualFold(strings.TrimSuffix(r.Content, "."), target):
				dns.driftf("a %s record to %s already exists; setup will not replace it", r.Type, orNone(r.Content))
			case r.Proxied == nil || !*r.Proxied: // a missing boolean is false
				dns.driftf("CNAME is not proxied")
			}
		}
	}
	add(dns)

	idp := &check{a: Action{ID: "access.idp", Resource: "Identity provider", Name: "One-time PIN"}}
	idp.seen(o.IdP)
	add(idp)

	for _, g := range d.Groups {
		c := &check{a: Action{ID: "access.group." + g.Key, Resource: "Access group", Name: g.Name}}
		if c.seen(o.Groups[g.Key]) {
			c.notef("membership is never compared or changed")
		}
		add(c)
	}

	for _, t := range d.ServiceTokens {
		ot := o.ServiceTokens[t.Key]
		c := &check{a: Action{ID: "access.service_token." + t.Key, Resource: "Service token", Name: t.Name}}
		found := c.seen(ot.Seen)
		if found && ot.ExpiresAt != "" {
			c.notef("expires %s", ot.ExpiresAt)
		}
		add(c)
		if found {
			s := &check{a: Action{ID: "access.service_token." + t.Key + ".secret", Resource: "Service token secret", Name: t.Name, ExistingID: ot.ID}}
			// A preview mounts no secret directory, so it cannot tell whether the file is
			// there; an apply, which has them, reports it.
			s.unknownf("Cloudflare never returns the client secret, and the preview cannot see the host's secret files: "+
				"an apply reports whether %s holds it. If it is lost, rotate it: Zero Trust → Access controls → Service credentials → Service Tokens → %s → Rotate", t.SecretFile, t.Name)
			add(s)
		}
	}

	for _, spec := range d.Apps {
		out = append(out, diffApp(d, spec, o))
	}

	for _, spec := range d.Rules {
		out = append(out, diffRule(d, spec, o.Rules[spec.Key]))
	}

	bfm := &check{a: Action{ID: "zone.bot_fight_mode", Resource: "Bot Fight Mode", Name: "off"}}
	switch {
	case o.BotFightMode.Unknown != "":
		bfm.unknownf("%s", o.BotFightMode.Unknown)
	case o.BotFightMode.On == nil:
		bfm.unknownf("Cloudflare did not return fight_mode (the zone may use Super Bot Fight Mode)")
	case *o.BotFightMode.On != d.BotFightMode:
		bfm.driftf("Bot Fight Mode is on for the whole zone; agents cannot pass its challenges (docs/deploy/cloudflare.md step 6.1)")
	}
	add(bfm)

	db := &check{a: Action{ID: "d1", Resource: "D1 database", Name: d.D1Database}}
	db.seen(o.D1)
	add(db)

	mig := &check{a: Action{ID: "d1.migrations", Resource: "D1 migrations", Name: d.D1Database}}
	switch {
	case o.D1.Unknown != "":
		mig.unknownf("D1 database could not be read")
	case !o.D1.Found:
		mig.create = true
	default:
		// Wrangler records applied migrations in the d1_migrations table; reading it
		// needs a D1 query, which is a POST, so a dry run cannot look.
		mig.unknownf("reading the d1_migrations table needs a D1 query (POST); a dry run sends only GET")
	}
	add(mig)

	w := &check{a: Action{ID: "worker", Resource: "Worker", Name: d.Worker.Script}}
	if w.seen(o.Worker.Seen) {
		switch {
		case o.Worker.BindingUnknown != "":
			w.unknownf("%s", o.Worker.BindingUnknown)
		case o.Worker.D1BindingID == "":
			w.driftf("no D1 binding named %s", d.Worker.D1Binding)
		case o.D1.Unknown != "":
			w.unknownf("the D1 database could not be read, so the %s binding cannot be checked", d.Worker.D1Binding)
		case !o.D1.Found:
			w.driftf("%s is bound to D1 database %s, which is not %s", d.Worker.D1Binding, o.Worker.D1BindingID, d.D1Database)
		case o.Worker.D1BindingID != o.D1.ID:
			w.driftf("%s is bound to D1 database %s, not %s (%s)", d.Worker.D1Binding, o.Worker.D1BindingID, d.D1Database, o.D1.ID)
		}
		// fresh-install.md §4.6: "Keep `workers_dev` and `preview_urls` disabled."
		switch {
		case o.Worker.SubdomainUnknown != "":
			w.unknownf("%s", o.Worker.SubdomainUnknown)
		default: // a missing boolean is false
			if o.Worker.WorkersDev != nil && *o.Worker.WorkersDev {
				w.driftf("the workers.dev URL is on; docs ask for workers_dev disabled")
			}
			if o.Worker.PreviewURLs != nil && *o.Worker.PreviewURLs {
				w.driftf("preview URLs are on; docs ask for preview_urls disabled")
			}
		}
	}
	add(w)

	sec := &check{a: Action{ID: "worker.secret", Resource: "Worker secret", Name: d.Worker.Secret}}
	switch {
	case o.Worker.Unknown != "":
		sec.unknownf("Worker could not be read")
	case !o.Worker.Found:
		sec.create = true
	case o.Worker.SecretsUnknown != "":
		sec.unknownf("%s", o.Worker.SecretsUnknown)
	case slices.Contains(o.Worker.Secrets, d.Worker.Secret):
		sec.unknownf("set, but Cloudflare never returns its value, and the preview cannot see the host's secret files: " +
			"if the backup service is refused (401), the Worker's value is not the one in secrets/backup/bearer (docs/deploy/fresh-install.md step 4.5)")
	default:
		sec.create = true
	}
	add(sec)

	dom := &check{a: Action{ID: "worker.domain", Resource: "Worker custom domain", Name: d.Worker.Hostname}}
	if dom.seen(o.Worker.Domain.Seen) && o.Worker.Domain.Service != d.Worker.Script {
		dom.driftf("serves Worker %q, not %q; setup will not move it", o.Worker.Domain.Service, d.Worker.Script)
	}
	add(dom)

	return out
}

// diffTunnel matches by the host's token ID, then the hostname's CNAME target, then the
// doc name, and reports any key that points at another tunnel, or at none, as drift.
func diffTunnel(d Desired, t Tunnel) Action {
	c := &check{a: Action{ID: "tunnel", Resource: "Tunnel", Name: d.Tunnel.Name}}
	if !c.seen(t.Seen) {
		if t.Unknown == "" && t.TokenID != "" {
			c.notef("the host's tunnel token names tunnel %s, which is not in this account", t.TokenID)
		}
		return c.done()
	}
	by := map[string]string{"id": "the host's tunnel token", "cname": "the hostname's CNAME record", "name": "name"}[t.MatchedBy]
	if t.TokenID != "" && t.ByToken == "" {
		c.driftf("the host's tunnel token names tunnel %s, which is not in this account; tunnel %s was matched by %s instead", t.TokenID, t.ID, by)
	}
	if t.CNAMEID != "" && t.ByCNAME == "" {
		c.driftf("the hostname's CNAME points at tunnel %s, which is not in this account; tunnel %s was matched by %s instead", t.CNAMEID, t.ID, by)
	}
	if t.ByCNAME != "" && t.ByCNAME != t.ID {
		c.driftf("the hostname's CNAME points at tunnel %s, not at %s", t.ByCNAME, t.ID)
	}
	for _, id := range t.ByName {
		if id != t.ID {
			c.driftf("tunnel %s is named %q but is not the tunnel matched by %s", id, d.Tunnel.Name, by)
		}
	}
	if t.Name != d.Tunnel.Name {
		c.notef("named %q (matched by %s)", t.Name, by)
		prefixCheck(c, d, t.Name, d.Tunnel.Name)
	}
	if t.Status == "inactive" || t.Status == "down" {
		c.notef("%s: no cloudflared connector is running", t.Status)
	}
	return c.done()
}

// prefixNote marks a resource matched by hostname or expression whose name lacks the
// run's --name-prefix: on the real install's hostname, a prefixed run finds the real install.
const prefixNote = "lacks the name prefix"

// prefixCheck flags name, found by hostname, CNAME or expression for a resource whose
// desired (prefixed) name is want. Besides a missing prefix it catches the real
// install's own name when the prefix happens to begin it: with prefix "officer-" the
// real tunnel "officer-dashboard" starts with the prefix but is the unprefixed name.
func prefixCheck(c *check, d Desired, name, want string) {
	p := d.NamePrefix
	if p != "" && (!strings.HasPrefix(name, p) || name == strings.TrimPrefix(want, p)) {
		c.driftf("matched %q, which %s %q: this hostname belongs to another install; a staging install needs its own hostname", name, prefixNote, d.NamePrefix)
	}
}

// PrefixConflict reports whether a --name-prefix run matched resources whose names lack
// the prefix, which means it is looking at another install on the same hostname.
func PrefixConflict(d Desired, actions []Action) bool {
	if d.NamePrefix == "" {
		return false
	}
	for _, a := range actions {
		for _, line := range a.Detail {
			if strings.Contains(line, prefixNote) {
				return true
			}
		}
	}
	return false
}

func diffApp(d Desired, spec AppSpec, o Observed) Action {
	app := o.Apps[spec.Key]
	c := &check{a: Action{ID: "access.app." + spec.Key, Resource: "Access application", Name: spec.Name}}
	others := overlapping(spec, o)
	if !c.seen(app.Seen) {
		if app.Unknown == "" && len(others) > 0 {
			// Another application already decides some of these requests (F-17, F-21):
			// adding a second one would leave two applications on the same paths.
			c.create = false
			c.driftf("setup will not create %q while another application covers the same hostname or paths", spec.Name)
			reportOthers(c, spec, others)
			return c.done()
		}
		if spec.Key == AppSite && app.Unknown == "" && routed(o) {
			c.notef("WARNING: the tunnel already routes this hostname and no Access application covers it; when cloudflared connects, the dashboard is public without the Access sign-in")
		}
		return c.done()
	}
	switch app.MatchedBy {
	case "destinations":
		if app.Name != spec.Name {
			c.notef("named %q (matched by hostname and paths)", app.Name)
		}
	case "covers":
		var extra []string
		for _, dest := range app.Destinations {
			if !slices.Contains(spec.Destinations, dest) {
				extra = append(extra, dest)
			}
		}
		c.driftf("%q already covers %s along with %s; setup will not add another application", app.Name, list(spec.Destinations), list(extra))
	case "name":
		c.driftf("covers %s, docs ask for %s", list(app.Destinations), list(spec.Destinations))
	}
	if app.MatchedBy != "name" {
		prefixCheck(c, d, app.Name, spec.Name)
	}
	// Any other application on these destinations decides who reaches them, and the docs
	// list none: they say every other path stays behind the PIN sign-in.
	reportOthers(c, spec, others)
	if spec.SessionDuration != "" && !sameDuration(app.SessionDuration, spec.SessionDuration) {
		c.driftf("session duration is %s, docs ask for %s", orNone(app.SessionDuration), spec.SessionDuration)
	}
	if spec.OnlyIdP {
		switch {
		case !o.IdP.Found:
			c.driftf("allowed identity providers cannot include One-time PIN until it exists")
		case len(app.AllowedIdPs) != 1 || app.AllowedIdPs[0] != o.IdP.ID:
			c.driftf("allowed identity providers are %s, docs ask for only One-time PIN (%s)", list(app.AllowedIdPs), o.IdP.ID)
		}
	}
	if spec.AutoRedirect && (app.AutoRedirect == nil || !*app.AutoRedirect) {
		c.driftf("instant authentication (auto_redirect_to_identity) is off")
	}
	if spec.ServiceAuth401 && (app.ServiceAuth401 == nil || !*app.ServiceAuth401) {
		c.driftf("401 Response for Service Auth policies (service_auth_401_redirect) is off")
	}
	if app.PoliciesUnknown != "" {
		c.unknownf("%s", app.PoliciesUnknown)
		return c.done()
	}
	// Every policy is checked: Access lets a request through if any one policy admits
	// it, so a single extra policy can open the application.
	want := wantIncludes(spec.Policy, o)
	matched := -1
	for i, p := range app.Policies { // the doc's policy, exactly
		if p.Decision == spec.Policy.Decision && reflect.DeepEqual(includeKeys(p.Include), want.keys) {
			matched = i
			break
		}
	}
	if matched < 0 {
		for i, p := range app.Policies { // else the first with the doc's decision
			if p.Decision == spec.Policy.Decision {
				matched = i
				c.driftf("policy %q includes %s, docs ask for %s", p.Name, list(includeKeys(p.Include)), list(want.labels))
				break
			}
		}
	}
	if matched < 0 {
		c.driftf("no %s policy; docs ask for %q including %s", spec.Policy.Decision, spec.Policy.Name, list(want.labels))
	}
	for i, p := range app.Policies {
		if p.Conditions {
			c.notef("policy %q also has require or exclude rules, which setup does not compare", p.Name)
		}
		if i == matched {
			continue
		}
		c.driftf("extra policy %q (%s) admits %s; docs ask for only %q", p.Name, p.Decision, list(includeLabels(p.Include)), spec.Policy.Name)
		if slices.Contains(spec.ForbiddenDecisions, p.Decision) {
			c.driftf("policy %q is %s; docs forbid a public %s policy here", p.Name, p.Decision, p.Decision)
		}
		if p.Decision != "deny" && slices.ContainsFunc(p.Include, func(in Include) bool { return in.Type == "everyone" }) {
			c.driftf("policy %q lets Everyone through: the application is public", p.Name)
		}
	}
	return c.done()
}

func includeLabels(inc []Include) []string {
	var out []string
	for _, in := range inc {
		switch in.Type {
		case "everyone":
			out = append(out, "Everyone")
		case "group":
			out = append(out, "group "+in.ID)
		case "service_token":
			out = append(out, "service token "+in.ID)
		default:
			out = append(out, "another selector")
		}
	}
	return out
}

type includes struct{ keys, labels []string }

// wantIncludes resolves a policy's includes to "type:id" keys for comparison and to
// readable labels. A referenced group or token that does not exist yet gets an empty ID,
// so an existing policy can never match it.
func wantIncludes(p PolicySpec, o Observed) includes {
	var in includes
	if p.Everyone {
		in.keys = append(in.keys, "everyone")
		in.labels = append(in.labels, "Everyone")
	}
	for _, g := range p.Groups {
		in.keys = append(in.keys, "group:"+o.Groups[g].ID)
		in.labels = append(in.labels, "group "+g)
	}
	if p.ServiceToken != "" {
		in.keys = append(in.keys, "service_token:"+o.ServiceTokens[p.ServiceToken].ID)
		in.labels = append(in.labels, "service token "+p.ServiceToken)
	}
	sort.Strings(in.keys)
	return in
}

func includeKeys(inc []Include) []string {
	keys := []string{}
	for _, i := range inc {
		if i.ID == "" {
			keys = append(keys, i.Type)
		} else {
			keys = append(keys, i.Type+":"+i.ID)
		}
	}
	sort.Strings(keys)
	return keys
}

func diffRule(d Desired, spec RuleSpec, r Rule) Action {
	c := &check{a: Action{ID: "rule." + spec.Key, Resource: "Rule (" + spec.Phase + ")", Name: spec.Description}}
	if !c.seen(r.Seen) {
		if r.PhaseEmpty {
			c.notef("the zone has no %s ruleset yet; the first write creates it", spec.Phase)
		}
		return c.done()
	}
	if r.MatchedBy == "expression" {
		c.notef("described as %q (matched by expression)", r.Description)
		prefixCheck(c, d, r.Description, spec.Description)
	}
	if r.Expression != spec.Expression {
		c.driftf("expression differs from docs/deploy/cloudflare.md")
	}
	if r.Action != spec.Action {
		c.driftf("action is %s, docs ask for %s", orNone(r.Action), spec.Action)
	}
	if r.Enabled != nil && !*r.Enabled {
		c.driftf("rule is disabled")
	}
	keys := make([]string, 0, len(spec.Params))
	for k := range spec.Params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		want, got := spec.Params[k], r.Params[k]
		if got == nil {
			c.unknownf("action_parameters.%s not returned", k)
			continue
		}
		if !paramMatches(want, got) {
			c.driftf("action_parameters.%s is %v, docs ask for %v", k, got, want)
		}
	}
	switch spec.Position {
	case "first":
		if r.Index != 0 {
			c.driftf("rule is number %d of %d; docs ask for it first", r.Index+1, r.Count)
		}
	case "last":
		if r.Index != r.Count-1 {
			c.driftf("rule is number %d of %d; docs ask for it last", r.Index+1, r.Count)
		}
	}
	return c.done()
}

func paramMatches(want, got any) bool {
	wl, ok := want.([]any)
	if !ok {
		return reflect.DeepEqual(want, got)
	}
	gl, ok := got.([]any)
	if !ok {
		return false
	}
	for _, w := range wl {
		if !slices.ContainsFunc(gl, func(g any) bool { return reflect.DeepEqual(w, g) }) {
			return false
		}
	}
	return true
}

// overlapping returns the applications that matched no documented application but have
// a destination overlapping one of spec's: on the same path, above or below it, or
// through a wildcard. For the whole-host specs (site, backups) that is every other
// application on the hostname.
func overlapping(spec AppSpec, o Observed) []App {
	if len(spec.Destinations) == 0 {
		return nil
	}
	host, _ := splitDest(spec.Destinations[0])
	var out []App
	for _, other := range o.OtherApps[host] {
		if len(overlaps(other.Destinations, spec.Destinations)) > 0 {
			out = append(out, other)
		}
	}
	return out
}

// overlaps returns the destinations in have that overlap any destination in want.
func overlaps(have, want []string) []string {
	var out []string
	for _, h := range have {
		if slices.ContainsFunc(want, func(w string) bool { return DestinationsOverlap(h, w) }) {
			out = append(out, h)
		}
	}
	return out
}

// reportOthers adds a drift line for each overlapping application, and for each of its
// policies that would let a request past the sign-in.
func reportOthers(c *check, spec AppSpec, others []App) {
	for _, other := range others {
		dests := overlaps(other.Destinations, spec.Destinations)
		if slices.ContainsFunc(dests, IsWildcard) {
			c.driftf("application %q (%s) has the wildcard destination %s, which may cover %s; Cloudflare's reference does not say which application decides when a wildcard and another application overlap", other.Name, other.ID, list(dests), list(spec.Destinations))
		} else {
			c.driftf("application %q (%s) also covers %s, which the docs do not list; its policies decide who reaches it", other.Name, other.ID, list(dests))
		}
		if other.PoliciesUnknown != "" {
			c.unknownf("%s", other.PoliciesUnknown)
		}
		for _, p := range other.Policies {
			if p.Decision == "bypass" {
				c.driftf("its policy %q is bypass: requests skip the Access sign-in", p.Name)
			}
			if p.Decision != "deny" && slices.ContainsFunc(p.Include, func(in Include) bool { return in.Type == "everyone" }) {
				c.driftf("its policy %q lets Everyone through: those paths may be public", p.Name)
			}
		}
	}
}

// routed reports whether the tunnel's routes send the dashboard hostname anywhere.
func routed(o Observed) bool {
	if !o.Tunnel.Found {
		return false
	}
	for _, in := range o.Tunnel.Ingress {
		if in.Hostname != "" && strings.Trim(in.Path, "/") == "" && strings.EqualFold(in.Hostname, o.hostname) {
			return true
		}
	}
	return false
}

func sameDuration(a, b string) bool {
	if a == b {
		return true
	}
	da, ea := time.ParseDuration(a)
	db, eb := time.ParseDuration(b)
	return ea == nil && eb == nil && da == db
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

func list(s []string) string {
	if len(s) == 0 {
		return "(none)"
	}
	return strings.Join(s, ", ")
}

// Summary counts actions by kind.
func Summary(actions []Action) map[Kind]int {
	m := map[Kind]int{}
	for _, k := range Kinds {
		m[k] = 0
	}
	for _, a := range actions {
		m[a.Kind]++
	}
	return m
}
