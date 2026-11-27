// Package cfprovision reads (and, in a later version, fills in) the Cloudflare side of
// a dashboard install. This version has only the discovery half: Discover reads an
// account through a cfapi.Reader, which cannot write, and reports what it found as a
// plan.Observed for plan.Diff.
//
// Endpoint shapes come from the Cloudflare API reference
// (https://developers.cloudflare.com/api/); each request below names its page.
package cfprovision

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"

	"officerdashboard/internal/setup/cfapi"
	"officerdashboard/internal/setup/plan"
)

// AmbiguousError means two or more resources match one desired resource. Setup never
// guesses which one is meant.
type AmbiguousError struct {
	Resource, Name string
	IDs            []string
}

func (e *AmbiguousError) Error() string {
	return fmt.Sprintf("%s %q matches more than one resource (%s); rename or delete the extra one, or use --name-prefix", e.Resource, e.Name, strings.Join(e.IDs, ", "))
}

// Discover reads everything Desired describes. A missing resource, or one the token
// cannot read, is recorded in Observed; an ambiguous match, a server error or a
// transport failure is returned as an error.
func Discover(ctx context.Context, api cfapi.Reader, d plan.Desired) (plan.Observed, error) {
	x := &discovery{api: api, d: d}
	o := plan.Observed{
		Groups:        map[string]plan.Seen{},
		ServiceTokens: map[string]plan.ServiceToken{},
		Apps:          map[string]plan.App{},
		Rules:         map[string]plan.Rule{},
	}
	var err error
	if o.Zone, err = x.zone(ctx); err != nil || !o.Zone.Found || o.Zone.Unknown != "" {
		return o, err
	}
	x.zoneID, x.acct = o.Zone.ID, o.Zone.AccountID
	if x.acct == "" {
		o.Zone.Unknown = "Cloudflare did not return the zone's account"
		return o, nil
	}
	steps := []func() error{
		func() (err error) { o.Org, err = x.org(ctx); return },
		func() (err error) { o.Tunnel, err = x.tunnel(ctx); return },
		func() (err error) { o.DNS, err = x.dns(ctx); return },
		func() (err error) { o.IdP, err = x.idp(ctx); return },
		func() error { return x.groups(ctx, o.Groups) },
		func() error { return x.serviceTokens(ctx, o.ServiceTokens) },
		func() error { return x.apps(ctx, o.Apps) },
		func() error { return x.rules(ctx, o.Rules) },
		func() (err error) { o.BotFightMode, err = x.botFightMode(ctx); return },
		func() (err error) { o.D1, err = x.d1(ctx); return },
		func() (err error) { o.Worker, err = x.worker(ctx); return },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return o, err
		}
	}
	return o, nil
}

type discovery struct {
	api          cfapi.Reader
	d            plan.Desired
	zoneID, acct string
}

func (x *discovery) accountPath(p string) string { return "/accounts/" + url.PathEscape(x.acct) + p }
func (x *discovery) zonePath(p string) string    { return "/zones/" + url.PathEscape(x.zoneID) + p }

// unreadable turns a permission error into an Unknown reason; any other error is fatal.
func unreadable(what string, err error) (string, error) {
	if cfapi.IsForbidden(err) {
		return fmt.Sprintf("the token cannot read %s (%v)", what, err), nil
	}
	return "", err
}

func ambiguous(resource, name string, ids []string) error {
	sort.Strings(ids)
	return &AmbiguousError{Resource: resource, Name: name, IDs: ids}
}

// zone: GET /zones?name=<name>
// https://developers.cloudflare.com/api/resources/zones/methods/list/
// Fields: id, name, status (initializing, pending, active, moved), account.id.
func (x *discovery) zone(ctx context.Context) (plan.Zone, error) {
	type zone struct {
		ID, Name, Status string
		Account          struct{ ID string } `json:"account"`
	}
	labels := strings.Split(x.d.Hostname, ".")
	// Longest suffix first, stopping before the bare TLD: officers.example.org, example.org.
	for i := 0; i < len(labels)-1; i++ {
		name := strings.Join(labels[i:], ".")
		zones, err := cfapi.ListAll[zone](ctx, x.api, "/zones", url.Values{"name": {name}})
		if err != nil {
			why, err := unreadable("zones", err)
			return plan.Zone{Seen: plan.Seen{Unknown: why}}, err
		}
		var hits []zone
		for _, z := range zones {
			if strings.EqualFold(z.Name, name) {
				hits = append(hits, z)
			}
		}
		switch len(hits) {
		case 0:
			continue
		case 1:
			z := hits[0]
			return plan.Zone{Seen: plan.Seen{Found: true, ID: z.ID}, Name: z.Name, Status: z.Status, AccountID: z.Account.ID}, nil
		default:
			ids := make([]string, len(hits))
			for i, z := range hits {
				ids[i] = z.ID
			}
			return plan.Zone{}, ambiguous("zone", name, ids)
		}
	}
	return plan.Zone{}, nil
}

// org: GET /accounts/{account_id}/access/organizations
// https://developers.cloudflare.com/api/resources/zero_trust/subresources/organizations/methods/list/
// Fields: auth_domain, name, session_duration. The reference does not say what an
// account without Zero Trust returns; a 404 or an empty auth_domain is taken as "not set up".
func (x *discovery) org(ctx context.Context) (plan.Org, error) {
	var r struct {
		AuthDomain      string `json:"auth_domain"`
		SessionDuration string `json:"session_duration"`
	}
	err := x.api.Get(ctx, x.accountPath("/access/organizations"), nil, &r)
	switch {
	case cfapi.IsNotFound(err):
		return plan.Org{}, nil
	case err != nil:
		why, err := unreadable("the Zero Trust organization", err)
		return plan.Org{Seen: plan.Seen{Unknown: why}}, err
	case r.AuthDomain == "":
		return plan.Org{}, nil
	}
	return plan.Org{Seen: plan.Seen{Found: true}, AuthDomain: r.AuthDomain, SessionDuration: r.SessionDuration}, nil
}

// tunnel: GET /accounts/{account_id}/cfd_tunnel?uuid=…|name=…&is_deleted=false
// https://developers.cloudflare.com/api/resources/zero_trust/subresources/tunnels/subresources/cloudflared/methods/list/
// Fields: id, name, deleted_at, config_src ("local" or "cloudflare").
// Ingress: GET /accounts/{account_id}/cfd_tunnel/{tunnel_id}/configurations
// https://developers.cloudflare.com/api/resources/zero_trust/subresources/tunnels/subresources/cloudflared/subresources/configurations/methods/get/
// Fields: config.ingress[].hostname, .service, .path.
func (x *discovery) tunnel(ctx context.Context) (plan.Tunnel, error) {
	type tunnel struct {
		ID        string  `json:"id"`
		Name      string  `json:"name"`
		DeletedAt *string `json:"deleted_at"`
		ConfigSrc string  `json:"config_src"`
	}
	find := func(q url.Values, keep func(tunnel) bool) ([]tunnel, error) {
		q.Set("is_deleted", "false")
		all, err := cfapi.ListAll[tunnel](ctx, x.api, x.accountPath("/cfd_tunnel"), q)
		var hits []tunnel
		for _, t := range all {
			if (t.DeletedAt == nil || *t.DeletedAt == "") && keep(t) {
				hits = append(hits, t)
			}
		}
		return hits, err
	}
	var hits []tunnel
	var err error
	matched := "id"
	if x.d.TunnelID != "" {
		hits, err = find(url.Values{"uuid": {x.d.TunnelID}}, func(t tunnel) bool { return strings.EqualFold(t.ID, x.d.TunnelID) })
	}
	if err == nil && len(hits) == 0 {
		matched = "name"
		hits, err = find(url.Values{"name": {x.d.Tunnel.Name}}, func(t tunnel) bool { return t.Name == x.d.Tunnel.Name })
	}
	if err != nil {
		why, err := unreadable("tunnels", err)
		return plan.Tunnel{Seen: plan.Seen{Unknown: why}}, err
	}
	switch len(hits) {
	case 0:
		return plan.Tunnel{}, nil
	case 1:
	default:
		ids := make([]string, len(hits))
		for i, t := range hits {
			ids[i] = t.ID
		}
		return plan.Tunnel{}, ambiguous("tunnel", x.d.Tunnel.Name, ids)
	}
	t := hits[0]
	out := plan.Tunnel{Seen: plan.Seen{Found: true, ID: t.ID}, Name: t.Name, ConfigSrc: t.ConfigSrc, MatchedBy: matched}
	if t.ConfigSrc == "local" {
		return out, nil
	}
	var cfg struct {
		Config struct {
			Ingress []struct{ Hostname, Service, Path string } `json:"ingress"`
		} `json:"config"`
	}
	if err := x.api.Get(ctx, x.accountPath("/cfd_tunnel/"+url.PathEscape(t.ID)+"/configurations"), nil, &cfg); err != nil {
		if cfapi.IsNotFound(err) {
			return out, nil // no remote configuration yet: no routes
		}
		why, err := unreadable("the tunnel's routes", err)
		out.IngressUnknown = why
		return out, err
	}
	for _, r := range cfg.Config.Ingress {
		out.Ingress = append(out.Ingress, plan.Ingress{Hostname: r.Hostname, Path: r.Path, Service: r.Service})
	}
	return out, nil
}

// dns: GET /zones/{zone_id}/dns_records?name.exact=<host>
// https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/list/
// Fields: id, name, type, content, proxied. (name and type are confirmed for CNAME in the
// reference; content and proxied are confirmed on other record types: see PR questions.)
func (x *discovery) dns(ctx context.Context) (plan.DNS, error) {
	type record struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Type    string `json:"type"`
		Content string `json:"content"`
		Proxied *bool  `json:"proxied"`
	}
	recs, err := cfapi.ListAll[record](ctx, x.api, x.zonePath("/dns_records"), url.Values{"name.exact": {x.d.Hostname}})
	if err != nil {
		why, err := unreadable("DNS records", err)
		return plan.DNS{Unknown: why}, err
	}
	var out plan.DNS
	for _, r := range recs {
		if strings.EqualFold(strings.TrimSuffix(r.Name, "."), x.d.Hostname) {
			out.Records = append(out.Records, plan.DNSRecord{ID: r.ID, Type: r.Type, Content: r.Content, Proxied: r.Proxied})
		}
	}
	return out, nil
}

// idp: GET /accounts/{account_id}/access/identity_providers
// https://developers.cloudflare.com/api/resources/zero_trust/subresources/identity_providers/methods/list/
// Fields: id, name, type ("onetimepin"). No name filter: every provider is listed.
func (x *discovery) idp(ctx context.Context) (plan.Seen, error) {
	type idp struct{ ID, Name, Type string }
	all, err := cfapi.ListAll[idp](ctx, x.api, x.accountPath("/access/identity_providers"), nil)
	if err != nil {
		why, err := unreadable("Access identity providers", err)
		return plan.Seen{Unknown: why}, err
	}
	var ids []string
	for _, p := range all {
		if p.Type == x.d.IdentityProvider {
			ids = append(ids, p.ID)
		}
	}
	switch len(ids) {
	case 0:
		return plan.Seen{}, nil
	case 1:
		return plan.Seen{Found: true, ID: ids[0]}, nil
	}
	return plan.Seen{}, ambiguous("identity provider", x.d.IdentityProvider, ids)
}

// byName lists path?name=<name> and keeps exact matches (the reference does not say
// whether the name filter is exact), returning one Seen or an AmbiguousError.
func byName(ctx context.Context, api cfapi.Reader, path, resource, name string, extra func(json.RawMessage)) (plan.Seen, error) {
	type named struct{ ID, Name string }
	var ids []string
	err := api.List(ctx, path, url.Values{"name": {name}}, func(raw json.RawMessage) error {
		var n named
		if err := json.Unmarshal(raw, &n); err != nil {
			return fmt.Errorf("cloudflare: GET %s: unexpected result shape: %w", path, err)
		}
		if n.Name == name {
			ids = append(ids, n.ID)
			if extra != nil {
				extra(raw)
			}
		}
		return nil
	})
	if err != nil {
		why, err := unreadable(resource+"s", err)
		return plan.Seen{Unknown: why}, err
	}
	switch len(ids) {
	case 0:
		return plan.Seen{}, nil
	case 1:
		return plan.Seen{Found: true, ID: ids[0]}, nil
	}
	return plan.Seen{}, ambiguous(resource, name, ids)
}

// groups: GET /accounts/{account_id}/access/groups?name=<name>
// https://developers.cloudflare.com/api/resources/zero_trust/subresources/access/subresources/groups/methods/list/
// Fields: id, name. Membership (include) is not read: setup never edits it.
func (x *discovery) groups(ctx context.Context, out map[string]plan.Seen) error {
	for _, g := range x.d.Groups {
		s, err := byName(ctx, x.api, x.accountPath("/access/groups"), "Access group", g.Name, nil)
		if err != nil {
			return err
		}
		out[g.Key] = s
	}
	return nil
}

// serviceTokens: GET /accounts/{account_id}/access/service_tokens?name=<name>
// https://developers.cloudflare.com/api/resources/zero_trust/subresources/access/subresources/service_tokens/methods/list/
// Fields: id, name, client_id, expires_at. The client secret is never returned.
func (x *discovery) serviceTokens(ctx context.Context, out map[string]plan.ServiceToken) error {
	for _, t := range x.d.ServiceTokens {
		var expires string
		s, err := byName(ctx, x.api, x.accountPath("/access/service_tokens"), "service token", t.Name, func(raw json.RawMessage) {
			var v struct {
				ExpiresAt string `json:"expires_at"`
			}
			_ = json.Unmarshal(raw, &v)
			expires = v.ExpiresAt
		})
		if err != nil {
			return err
		}
		out[t.Key] = plan.ServiceToken{Seen: s, ExpiresAt: expires}
	}
	return nil
}

// apps: GET /accounts/{account_id}/access/apps
// https://developers.cloudflare.com/api/resources/zero_trust/subresources/access/subresources/applications/methods/list/
// Self-hosted fields: id, name, domain, destinations[] {type "public", uri},
// self_hosted_domains (deprecated), session_duration, allowed_idps,
// auto_redirect_to_identity, service_auth_401_redirect.
// Policies: GET /accounts/{account_id}/access/apps/{app_id}/policies
// https://developers.cloudflare.com/api/resources/zero_trust/subresources/access/subresources/applications/subresources/policies/methods/list/
// Fields: id, name, decision, include[], precedence.
func (x *discovery) apps(ctx context.Context, out map[string]plan.App) error {
	type app struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		Domain       string `json:"domain"`
		Destinations []struct {
			Type string `json:"type"`
			URI  string `json:"uri"`
		} `json:"destinations"`
		SelfHostedDomains []string `json:"self_hosted_domains"`
		SessionDuration   string   `json:"session_duration"`
		AllowedIdPs       []string `json:"allowed_idps"`
		AutoRedirect      *bool    `json:"auto_redirect_to_identity"`
		ServiceAuth401    *bool    `json:"service_auth_401_redirect"`
	}
	all, err := cfapi.ListAll[app](ctx, x.api, x.accountPath("/access/apps"), nil)
	if err != nil {
		why, err := unreadable("Access applications", err)
		if err != nil {
			return err
		}
		for _, spec := range x.d.Apps {
			out[spec.Key] = plan.App{Seen: plan.Seen{Unknown: why}}
		}
		return nil
	}
	dests := make([][]string, len(all))
	for i, a := range all {
		var ds []string
		for _, d := range a.Destinations {
			if d.Type == "" || d.Type == "public" {
				ds = append(ds, plan.NormalizeDestination(d.URI))
			}
		}
		if len(ds) == 0 {
			for _, d := range a.SelfHostedDomains {
				ds = append(ds, plan.NormalizeDestination(d))
			}
		}
		if len(ds) == 0 && a.Domain != "" {
			ds = []string{plan.NormalizeDestination(a.Domain)}
		}
		sort.Strings(ds)
		dests[i] = ds
	}
	for _, spec := range x.d.Apps {
		want := append([]string(nil), spec.Destinations...)
		sort.Strings(want)
		var hits []int
		matched := "destinations"
		for i := range all {
			if slices.Equal(dests[i], want) {
				hits = append(hits, i)
			}
		}
		if len(hits) == 0 {
			matched = "name"
			for i, a := range all {
				if a.Name == spec.Name {
					hits = append(hits, i)
				}
			}
		}
		if len(hits) == 0 {
			out[spec.Key] = plan.App{}
			continue
		}
		if len(hits) > 1 {
			ids := make([]string, len(hits))
			for j, i := range hits {
				ids[j] = all[i].ID
			}
			return ambiguous("Access application", spec.Name, ids)
		}
		a := all[hits[0]]
		got := plan.App{Seen: plan.Seen{Found: true, ID: a.ID}, Name: a.Name, MatchedBy: matched, Destinations: dests[hits[0]],
			SessionDuration: a.SessionDuration, AllowedIdPs: a.AllowedIdPs, AutoRedirect: a.AutoRedirect, ServiceAuth401: a.ServiceAuth401}
		got.Policies, got.PoliciesUnknown, err = x.appPolicies(ctx, a.ID)
		if err != nil {
			return err
		}
		out[spec.Key] = got
	}
	return nil
}

func (x *discovery) appPolicies(ctx context.Context, appID string) ([]plan.Policy, string, error) {
	type policy struct {
		ID       string                       `json:"id"`
		Name     string                       `json:"name"`
		Decision string                       `json:"decision"`
		Include  []map[string]json.RawMessage `json:"include"`
	}
	all, err := cfapi.ListAll[policy](ctx, x.api, x.accountPath("/access/apps/"+url.PathEscape(appID)+"/policies"), nil)
	if err != nil {
		why, err := unreadable("the application's policies", err)
		return nil, why, err
	}
	var out []plan.Policy
	for _, p := range all {
		pol := plan.Policy{ID: p.ID, Name: p.Name, Decision: p.Decision}
		for _, rule := range p.Include {
			pol.Include = append(pol.Include, parseInclude(rule))
		}
		out = append(out, pol)
	}
	return out, "", nil
}

// parseInclude reads one Access rule: {"group":{"id":…}}, {"everyone":{}} or
// {"service_token":{"token_id":…}} (shapes from the groups and policies reference pages).
func parseInclude(rule map[string]json.RawMessage) plan.Include {
	if len(rule) != 1 {
		return plan.Include{Type: "other"}
	}
	for k, v := range rule {
		var body struct {
			ID      string `json:"id"`
			TokenID string `json:"token_id"`
		}
		_ = json.Unmarshal(v, &body)
		switch {
		case k == "everyone":
			return plan.Include{Type: "everyone"}
		case k == "group" && body.ID != "":
			return plan.Include{Type: "group", ID: body.ID}
		case k == "service_token" && body.TokenID != "":
			return plan.Include{Type: "service_token", ID: body.TokenID}
		}
	}
	return plan.Include{Type: "other"}
}

// rules: GET /zones/{zone_id}/rulesets/phases/{phase}/entrypoint
// https://developers.cloudflare.com/api/resources/rulesets/subresources/phases/methods/get/
// Fields: id, rules[] {id, description, expression, action, action_parameters, enabled}.
// A 404 means the zone has no entry point ruleset for the phase yet
// (https://developers.cloudflare.com/waf/custom-rules/create-api/).
func (x *discovery) rules(ctx context.Context, out map[string]plan.Rule) error {
	type rule struct {
		ID          string         `json:"id"`
		Description string         `json:"description"`
		Expression  string         `json:"expression"`
		Action      string         `json:"action"`
		Params      map[string]any `json:"action_parameters"`
		Enabled     *bool          `json:"enabled"`
	}
	type ruleset struct {
		ID    string `json:"id"`
		Rules []rule `json:"rules"`
	}
	phases := map[string]*ruleset{}
	unknown := map[string]string{}
	for _, spec := range x.d.Rules {
		rs, seen := phases[spec.Phase]
		if !seen {
			var r ruleset
			err := x.api.Get(ctx, x.zonePath("/rulesets/phases/"+url.PathEscape(spec.Phase)+"/entrypoint"), nil, &r)
			switch {
			case cfapi.IsNotFound(err):
			case err != nil:
				why, err := unreadable("the "+spec.Phase+" rules", err)
				if err != nil {
					return err
				}
				unknown[spec.Phase] = why
			default:
				rs = &r
			}
			phases[spec.Phase] = rs
		}
		if why := unknown[spec.Phase]; why != "" {
			out[spec.Key] = plan.Rule{Seen: plan.Seen{Unknown: why}}
			continue
		}
		if rs == nil {
			out[spec.Key] = plan.Rule{}
			continue
		}
		var hits []int
		matched := "description"
		for i, r := range rs.Rules {
			if r.Description == spec.Description {
				hits = append(hits, i)
			}
		}
		if len(hits) == 0 {
			matched = "expression"
			for i, r := range rs.Rules {
				if r.Expression == spec.Expression {
					hits = append(hits, i)
				}
			}
		}
		switch len(hits) {
		case 0:
			out[spec.Key] = plan.Rule{}
			continue
		case 1:
		default:
			ids := make([]string, len(hits))
			for j, i := range hits {
				ids[j] = rs.Rules[i].ID
			}
			return ambiguous("rule ("+spec.Phase+")", spec.Description, ids)
		}
		r := rs.Rules[hits[0]]
		out[spec.Key] = plan.Rule{Seen: plan.Seen{Found: true, ID: r.ID}, RulesetID: rs.ID, MatchedBy: matched,
			Index: hits[0], Count: len(rs.Rules), Description: r.Description, Expression: r.Expression,
			Action: r.Action, Enabled: r.Enabled, Params: r.Params}
	}
	return nil
}

// botFightMode: GET /zones/{zone_id}/bot_management
// https://developers.cloudflare.com/api/resources/bot_management/methods/get/
// Field: fight_mode (BotFightModeConfiguration). Other plans return other shapes,
// which leave On nil.
func (x *discovery) botFightMode(ctx context.Context) (plan.BotFightMode, error) {
	var r struct {
		FightMode *bool `json:"fight_mode"`
	}
	if err := x.api.Get(ctx, x.zonePath("/bot_management"), nil, &r); err != nil {
		why, err := unreadable("Bot Management settings", err)
		return plan.BotFightMode{Seen: plan.Seen{Unknown: why}}, err
	}
	return plan.BotFightMode{Seen: plan.Seen{Found: true}, On: r.FightMode}, nil
}

// d1: GET /accounts/{account_id}/d1/database?name=<name>
// https://developers.cloudflare.com/api/resources/d1/subresources/database/methods/list/
// Fields: uuid, name.
func (x *discovery) d1(ctx context.Context) (plan.Seen, error) {
	type db struct {
		UUID string `json:"uuid"`
		Name string `json:"name"`
	}
	all, err := cfapi.ListAll[db](ctx, x.api, x.accountPath("/d1/database"), url.Values{"name": {x.d.D1Database}})
	if err != nil {
		why, err := unreadable("D1 databases", err)
		return plan.Seen{Unknown: why}, err
	}
	var ids []string
	for _, d := range all {
		if d.Name == x.d.D1Database {
			ids = append(ids, d.UUID)
		}
	}
	switch len(ids) {
	case 0:
		return plan.Seen{}, nil
	case 1:
		return plan.Seen{Found: true, ID: ids[0]}, nil
	}
	return plan.Seen{}, ambiguous("D1 database", x.d.D1Database, ids)
}

// worker reads the script, its D1 binding, its secret names and its custom domain.
//
//   - GET /accounts/{account_id}/workers/scripts (no pagination; fields id)
//     https://developers.cloudflare.com/api/resources/workers/subresources/scripts/methods/list/
//   - GET /accounts/{account_id}/workers/scripts/{script_name}/settings
//     (bindings[] {type "d1", name, database_id; id is the deprecated name})
//     https://developers.cloudflare.com/api/resources/workers/subresources/scripts/subresources/script_and_version_settings/methods/get/
//   - GET /accounts/{account_id}/workers/scripts/{script_name}/secrets ("List the names of secrets")
//     https://developers.cloudflare.com/api/resources/workers/subresources/scripts/subresources/secrets/methods/list/
//   - GET /accounts/{account_id}/workers/scripts/{script_name}/subdomain (enabled, previews_enabled)
//     https://developers.cloudflare.com/api/resources/workers/subresources/scripts/subresources/subdomain/methods/get/
//   - GET /accounts/{account_id}/workers/domains?hostname=<host> (fields id, hostname, service)
//     https://developers.cloudflare.com/api/resources/workers/subresources/domains/methods/list/
func (x *discovery) worker(ctx context.Context) (plan.Worker, error) {
	var out plan.Worker
	var scripts []struct {
		ID string `json:"id"`
	}
	if err := x.api.Get(ctx, x.accountPath("/workers/scripts"), nil, &scripts); err != nil {
		why, err := unreadable("Workers scripts", err)
		out.Unknown = why
		if err != nil {
			return out, err
		}
	} else {
		for _, s := range scripts {
			if s.ID == x.d.Worker.Script {
				out.Found, out.ID = true, s.ID
			}
		}
	}
	if out.Found {
		script := x.accountPath("/workers/scripts/" + url.PathEscape(x.d.Worker.Script))
		var settings struct {
			Bindings []struct {
				Type       string `json:"type"`
				Name       string `json:"name"`
				DatabaseID string `json:"database_id"`
				ID         string `json:"id"`
			} `json:"bindings"`
		}
		if err := x.api.Get(ctx, script+"/settings", nil, &settings); err != nil {
			why, err := unreadable("the Worker's bindings", err)
			if err != nil {
				return out, err
			}
			out.BindingUnknown = why
		}
		for _, b := range settings.Bindings {
			if b.Type == "d1" && b.Name == x.d.Worker.D1Binding {
				out.D1BindingID = b.DatabaseID
				if out.D1BindingID == "" {
					out.D1BindingID = b.ID
				}
			}
		}
		var secrets []struct {
			Name string `json:"name"`
		}
		if err := x.api.Get(ctx, script+"/secrets", nil, &secrets); err != nil {
			why, err := unreadable("the Worker's secret names", err)
			if err != nil {
				return out, err
			}
			out.SecretsUnknown = why
		}
		for _, s := range secrets {
			out.Secrets = append(out.Secrets, s.Name)
		}
		var sub struct {
			Enabled         *bool `json:"enabled"`
			PreviewsEnabled *bool `json:"previews_enabled"`
		}
		if err := x.api.Get(ctx, script+"/subdomain", nil, &sub); err != nil {
			why, err := unreadable("the Worker's workers.dev settings", err)
			if err != nil {
				return out, err
			}
			out.SubdomainUnknown = why
		}
		out.WorkersDev, out.PreviewURLs = sub.Enabled, sub.PreviewsEnabled
	}
	var domains []struct {
		ID       string `json:"id"`
		Hostname string `json:"hostname"`
		Service  string `json:"service"`
	}
	err := x.api.Get(ctx, x.accountPath("/workers/domains"), url.Values{"hostname": {x.d.Worker.Hostname}}, &domains)
	if err != nil {
		why, err := unreadable("Workers custom domains", err)
		out.Domain.Unknown = why
		return out, err
	}
	var ids []string
	for _, d := range domains {
		if strings.EqualFold(d.Hostname, x.d.Worker.Hostname) {
			ids = append(ids, d.ID)
			out.Domain = plan.WorkerDomain{Seen: plan.Seen{Found: true, ID: d.ID}, Service: d.Service}
		}
	}
	if len(ids) > 1 {
		return out, ambiguous("Worker custom domain", x.d.Worker.Hostname, ids)
	}
	return out, nil
}
