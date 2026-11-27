package cfprovision

import (
	"fmt"
	"io"
	"net/mail"
	"strings"

	"officerdashboard/internal/setup/plan"
)

// Options are the apply engine's choices. The command fills them; nothing here is read
// from the environment.
type Options struct {
	// GroupEmails are the members of each Access group setup creates, by plan group key
	// (plan.GroupOfficers, plan.GroupLaunchpad). Cloudflare needs at least one include
	// rule to create a group, and cloudflare.md §2 says "add every officer's email,
	// including your own". The members of an existing group are never read or changed.
	GroupEmails map[string][]string
	// ServiceTokenDuration is the "officer-dashboard services" token's lifetime, a Go
	// duration or "forever". Empty means "8760h", Cloudflare's default ("The default is
	// 1 year in hours (8760h).").
	ServiceTokenDuration string
	// AllowBotFightModeOff lets setup turn Bot Fight Mode off. It is zone-wide, so without
	// it setup prints the manual step; with it, the plan lists the zone's other hostnames.
	AllowBotFightModeOff bool
}

// StepKind is what one apply step does.
type StepKind string

const (
	StepSession      StepKind = "session"
	StepIdP          StepKind = "idp"
	StepGroup        StepKind = "group"
	StepServiceToken StepKind = "service_token"
	StepApp          StepKind = "app"
	StepRule         StepKind = "rule"
	StepTunnel       StepKind = "tunnel"
	StepTunnelToken  StepKind = "tunnel_token"
	StepRoute        StepKind = "route"
	StepDNS          StepKind = "dns"
	StepBotFightMode StepKind = "bot_fight_mode"
)

// Step is one write (or, for StepTunnelToken, one secret fetch) the engine performs,
// in order. ActionID is the plan action it fulfils; its read-back must make that action
// ok. Needs are the actions that must be ok, or be created by an earlier step, first.
type Step struct {
	Kind     StepKind `json:"kind"`
	ActionID string   `json:"action"`
	Key      string   `json:"key,omitempty"`
	Title    string   `json:"title"`
	Needs    []string `json:"needs,omitempty"`
}

// ApplyPlan is what an apply run will do, and why it may refuse to start.
type ApplyPlan struct {
	Hostname string
	Steps    []Step
	// Blockers stop the run before the first write.
	Blockers []string
	// Warnings are printed loudly before the steps.
	Warnings []string
	// Manual are steps a person must do; Skipped are creates this version does not make.
	Manual, Skipped []string
}

// Facts are what PlanApply needs beyond the plan: things the engine reads from the host
// or from Cloudflare outside discovery.
type Facts struct {
	// TunnelTokenKept is true when the host already holds the tunnel token.
	TunnelTokenKept bool
	// ServicesSecretKept is true when the host already holds the services client secret.
	ServicesSecretKept bool
	// OtherHostnames are the zone's DNS record names other than the dashboard's, shown
	// before Bot Fight Mode is turned off for the whole zone.
	OtherHostnames []string
}

// freeLimits are the rule slots per phase by zone plan, from the product pages quoted
// in the spike report: custom rules "5 | 20 | 100 | 1,000"
// (https://developers.cloudflare.com/waf/custom-rules/), cache rules "10 | 25 | 50 | 300"
// (https://developers.cloudflare.com/cache/how-to/cache-rules/), configuration rules
// "10 | 25 | 50 | 300" (https://developers.cloudflare.com/rules/configuration-rules/).
var ruleLimits = map[string]map[string]int{
	"http_request_firewall_custom": {"free": 5, "pro": 20, "business": 100, "enterprise": 1000},
	"http_request_cache_settings":  {"free": 10, "pro": 25, "business": 50, "enterprise": 300},
	"http_config_settings":         {"free": 10, "pro": 25, "business": 50, "enterprise": 300},
}

// outOfScope are creates the plan lists that this version does not make: the backup
// Worker, D1 and the backup Access pieces come in a later release.
var outOfScope = map[string]bool{
	"d1": true, "d1.migrations": true, "worker": true, "worker.secret": true, "worker.domain": true,
	"access.app." + plan.AppBackups: true, "access.service_token." + plan.TokenBackups: true,
}

// PlanApply decides the steps for the create actions, in an order that never publishes
// the hostname before its Access applications exist, and lists every reason to refuse.
// It is pure: the engine runs it on a fresh discovery.
func PlanApply(d plan.Desired, o plan.Observed, actions []plan.Action, opt Options, f Facts) ApplyPlan {
	p := ApplyPlan{Hostname: d.Hostname}
	by := map[string]plan.Action{}
	for _, a := range actions {
		by[a.ID] = a
	}
	kind := func(id string) plan.Kind { return by[id].Kind }

	// Everything lives in the zone and its account; the setup token must work.
	for _, id := range []string{"zone", "token", "zero_trust"} {
		a, ok := by[id]
		switch {
		case !ok:
			p.Blockers = append(p.Blockers, fmt.Sprintf("the plan has no %q line; run the preview again", id))
		case a.Kind != plan.OK:
			p.Blockers = append(p.Blockers, fmt.Sprintf("%s is %s%s; setup needs it ok before it changes anything", a.Resource, a.Kind, firstDetail(a)))
		}
	}

	add := func(s Step) { p.Steps = append(p.Steps, s) }
	creating := func(id string) bool { return kind(id) == plan.Create }

	// 1. Account-wide Access settings and identities. None of these publishes anything.
	if creating("zero_trust.session") {
		add(Step{Kind: StepSession, ActionID: "zero_trust.session", Title: "set the global session duration to " + d.OrgSessionDuration})
	}
	if creating("access.idp") {
		add(Step{Kind: StepIdP, ActionID: "access.idp", Title: "add the One-time PIN identity provider"})
	}
	for _, g := range d.Groups {
		id := "access.group." + g.Key
		if !creating(id) {
			continue
		}
		add(Step{Kind: StepGroup, ActionID: id, Key: g.Key, Title: "create Access group " + g.Name})
		emails := opt.GroupEmails[g.Key]
		if len(emails) == 0 {
			p.Blockers = append(p.Blockers, fmt.Sprintf("Access group %q must be created, and Cloudflare needs at least one member: give its emails (docs/deploy/cloudflare.md step 2 says to include your own)", g.Name))
		}
		for _, e := range emails {
			if a, err := mail.ParseAddress(e); err != nil || a.Address != e {
				p.Blockers = append(p.Blockers, fmt.Sprintf("%q is not a plain email address (group %s)", e, g.Name))
			}
		}
	}
	tokenID := "access.service_token." + plan.TokenServices
	if creating(tokenID) {
		add(Step{Kind: StepServiceToken, ActionID: tokenID, Key: plan.TokenServices, Title: "create service token " + nameOf(d, plan.TokenServices)})
	}

	// 2. The Access applications, each with its reusable policy.
	appNeeds := map[string][]string{plan.AppSite: {"access.idp"}, plan.AppServices: {tokenID}}
	for _, g := range d.Groups {
		appNeeds[plan.AppSite] = append(appNeeds[plan.AppSite], "access.group."+g.Key)
	}
	publishNeeds := []string{"tunnel"}
	for _, spec := range d.Apps {
		id := "access.app." + spec.Key
		if outOfScope[id] {
			continue
		}
		publishNeeds = append(publishNeeds, id)
		if creating(id) {
			add(Step{Kind: StepApp, ActionID: id, Key: spec.Key, Title: "create Access application " + spec.Name + " with policy " + spec.Policy.Name, Needs: appNeeds[spec.Key]})
		}
	}

	// 3. Zone rules: they change only requests to the dashboard hostname.
	for _, spec := range d.Rules {
		id := "rule." + spec.Key
		if !creating(id) {
			continue
		}
		r := o.Rules[spec.Key]
		how := "append it to the existing ruleset"
		if r.PhaseEmpty {
			how = "create the phase's ruleset with it"
		}
		add(Step{Kind: StepRule, ActionID: id, Key: spec.Key, Title: fmt.Sprintf("add rule %q to %s (%s)", spec.Description, spec.Phase, how)})
		limit, ok := ruleLimits[spec.Phase][o.Zone.Plan]
		switch {
		case !ok:
			p.Blockers = append(p.Blockers, fmt.Sprintf("cannot count the free %s rule slots: Cloudflare did not return a known zone plan (%q)", spec.Phase, o.Zone.Plan))
		case r.Count+1 > limit:
			p.Blockers = append(p.Blockers, fmt.Sprintf("the %s phase already has %d of the %d rules the %s plan allows; free a slot first", spec.Phase, r.Count, limit, o.Zone.Plan))
		}
	}

	// 4. Publication last: the tunnel, its route and the DNS record that put the
	// hostname on the internet, only once every Access application exists.
	if creating("tunnel") {
		add(Step{Kind: StepTunnel, ActionID: "tunnel", Title: "create tunnel " + d.Tunnel.Name + " and keep its token"})
		if d.TunnelID != "" {
			p.Blockers = append(p.Blockers, fmt.Sprintf("the host's tunnel token names tunnel %s, which is not in this account; a new tunnel would not match it. Move the old token file aside, then run setup again", d.TunnelID))
		}
		if o.Tunnel.CNAMEID != "" {
			p.Blockers = append(p.Blockers, fmt.Sprintf("the hostname's CNAME points at tunnel %s, which is not in this account; setup will not create another tunnel beside that record", o.Tunnel.CNAMEID))
		}
	} else if kind("tunnel") == plan.OK && !f.TunnelTokenKept {
		add(Step{Kind: StepTunnelToken, ActionID: "tunnel", Title: "fetch the token of tunnel " + by["tunnel"].ExistingID + " (the host has none)"})
	}
	if creating("tunnel.route") {
		add(Step{Kind: StepRoute, ActionID: "tunnel.route", Title: "route " + d.Hostname + " to " + d.Tunnel.Service + " in the tunnel, keeping every other route", Needs: publishNeeds})
	}
	if creating("dns") {
		add(Step{Kind: StepDNS, ActionID: "dns", Title: "create the proxied CNAME " + d.Hostname + " → <tunnel>.cfargotunnel.com", Needs: append([]string{"tunnel.route"}, publishNeeds...)})
	}

	// 5. Bot Fight Mode is zone-wide: only with the explicit option.
	if bfm := by["zone.bot_fight_mode"]; bfm.Kind == plan.Drift && o.BotFightMode.On != nil && *o.BotFightMode.On {
		if opt.AllowBotFightModeOff {
			add(Step{Kind: StepBotFightMode, ActionID: "zone.bot_fight_mode", Title: "turn Bot Fight Mode off for the whole zone"})
			others := "none"
			if len(f.OtherHostnames) > 0 {
				others = strings.Join(f.OtherHostnames, ", ")
			}
			p.Warnings = append(p.Warnings, "Bot Fight Mode is zone-wide: turning it off also affects the zone's other hostnames: "+others)
		} else {
			p.Manual = append(p.Manual, "Bot Fight Mode is on for the whole zone. Setup leaves it, because it also affects the zone's other hostnames. "+
				"In the Cloudflare dashboard, open the domain → Security → Settings → filter Bot traffic → Bot Fight Mode → Off (docs/deploy/cloudflare.md step 6.1)")
		}
	}

	// Every need must be ok already, or be created by an earlier step.
	done := map[string]bool{}
	for _, s := range p.Steps {
		for _, n := range s.Needs {
			a, ok := by[n]
			switch {
			case !ok:
				p.Blockers = append(p.Blockers, fmt.Sprintf("%s needs %q, which the plan does not list", s.Title, n))
			case a.Kind == plan.OK:
			case a.Kind == plan.Create && done[n]:
			default:
				p.Blockers = append(p.Blockers, fmt.Sprintf("%s needs %s %q, which is %s%s", s.Title, a.Resource, a.Name, a.Kind, firstDetail(a)))
			}
		}
		done[s.ActionID] = true
	}

	// The live account's state: published, but no Access application in front of it.
	if creating("access.app."+plan.AppSite) && (kind("tunnel.route") == plan.OK || kind("dns") == plan.OK) {
		p.Warnings = append(p.Warnings, fmt.Sprintf("%s is already published (tunnel route or DNS record exists) but no Access application protects it. "+
			"Setup creates the Access applications first, before anything else. Do not start cloudflared until this run reports done.", d.Hostname))
	}

	for _, a := range actions {
		switch {
		case a.Kind == plan.Manual:
			p.Manual = append(p.Manual, a.Resource+" "+a.Name+firstDetail(a))
		case a.Kind == plan.Create && outOfScope[a.ID]:
			p.Skipped = append(p.Skipped, fmt.Sprintf("%s %s: not created by this version (the backup Worker comes later)", a.Resource, a.Name))
		}
	}
	if kind(tokenID+".secret") == plan.Unknowable && kind(tokenID) == plan.OK && !f.ServicesSecretKept {
		p.Manual = append(p.Manual, "the host has no client secret for service token "+nameOf(d, plan.TokenServices)+
			"; if the bots lost it, rotate it: Zero Trust → Access controls → Service credentials → Service Tokens → "+nameOf(d, plan.TokenServices)+" → Rotate")
	}
	return p
}

func nameOf(d plan.Desired, tokenKey string) string {
	for _, t := range d.ServiceTokens {
		if t.Key == tokenKey {
			return t.Name
		}
	}
	return tokenKey
}

func firstDetail(a plan.Action) string {
	if len(a.Detail) == 0 {
		return ""
	}
	return " (" + a.Detail[0] + ")"
}

// WriteText prints the plan: warnings first, then the numbered steps, manual steps,
// skipped creates and any blocker.
func (p ApplyPlan) WriteText(w io.Writer) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Cloudflare apply plan for %s\n\n", p.Hostname)
	for _, s := range p.Warnings {
		fmt.Fprintf(&b, "  WARNING: %s\n", s)
	}
	if len(p.Warnings) > 0 {
		b.WriteByte('\n')
	}
	if len(p.Steps) == 0 {
		b.WriteString("  Nothing to create.\n")
	}
	for i, s := range p.Steps {
		fmt.Fprintf(&b, "  %2d. %s\n", i+1, s.Title)
	}
	for _, s := range p.Manual {
		fmt.Fprintf(&b, "  manual: %s\n", s)
	}
	for _, s := range p.Skipped {
		fmt.Fprintf(&b, "  skipped: %s\n", s)
	}
	if len(p.Blockers) > 0 {
		b.WriteString("\n  Setup will not start, and has changed nothing:\n")
		for _, s := range p.Blockers {
			fmt.Fprintf(&b, "    - %s\n", s)
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}
