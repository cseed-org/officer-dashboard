package plan

import (
	"strings"
	"testing"
)

func desired(t *testing.T) Desired {
	t.Helper()
	d, err := NewDesired(Inputs{AppURL: "https://officers.example.org", LaunchpadGroup: "Launchpad"})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func yes() *bool { b := true; return &b }
func no() *bool  { b := false; return &b }

// fresh is an account where only the manual steps are done: the zone is active and
// Zero Trust is open.
func fresh() Observed {
	return Observed{
		Zone:  Zone{Seen: Seen{Found: true, ID: "zone1"}, Name: "example.org", Status: "active", AccountID: "acct1"},
		Token: Token{Seen: Seen{Found: true}, Status: "active", ExpiresOn: "2026-12-01T00:00:00Z"},
		// A live organization without a global session duration omits the key.
		Org: Org{Seen: Seen{Found: true}, AuthDomain: "club.cloudflareaccess.com"},
		// A fresh Free zone reports Bot Fight Mode off.
		BotFightMode: BotFightMode{Seen: Seen{Found: true}, On: no()},
	}
}

// complete is an account set up exactly as the docs describe.
func complete(d Desired) Observed {
	o := fresh()
	o.Org.SessionDuration = "730h"
	o.Tunnel = Tunnel{Seen: Seen{Found: true, ID: "tun1"}, Name: d.Tunnel.Name, ConfigSrc: "cloudflare", MatchedBy: "name",
		Ingress: []Ingress{{Hostname: d.Hostname, Service: d.Tunnel.Service}, {Service: "http_status:404"}}}
	o.DNS = DNS{Records: []DNSRecord{{ID: "dns1", Type: "CNAME", Content: "tun1.cfargotunnel.com", Proxied: yes()}}}
	o.IdP = Seen{Found: true, ID: "otp1"}
	o.Groups = map[string]Seen{GroupOfficers: {Found: true, ID: "g-off"}, GroupLaunchpad: {Found: true, ID: "g-lp"}}
	o.ServiceTokens = map[string]ServiceToken{
		TokenServices: {Seen: Seen{Found: true, ID: "st-svc"}, ExpiresAt: "2027-11-01T00:00:00Z"},
		TokenBackups:  {Seen: Seen{Found: true, ID: "st-bak"}},
	}
	o.Apps = map[string]App{
		AppSite: {Seen: Seen{Found: true, ID: "app-site"}, Name: "Officer Dashboard", MatchedBy: "destinations", Destinations: []string{d.Hostname},
			SessionDuration: "730h", AllowedIdPs: []string{"otp1"}, AutoRedirect: yes(),
			Policies: []Policy{{ID: "p1", Name: "Officers and Launchpad", Decision: "allow", Include: []Include{{"group", "g-off"}, {"group", "g-lp"}}}}},
		AppAgents: {Seen: Seen{Found: true, ID: "app-agents"}, Name: "Officer Dashboard: agents (bypass)", MatchedBy: "destinations",
			Policies: []Policy{{ID: "p2", Name: "Bypass", Decision: "bypass", Include: []Include{{Type: "everyone"}}}}},
		AppServices: {Seen: Seen{Found: true, ID: "app-svc"}, Name: "Officer Dashboard: services (service token)", MatchedBy: "destinations", ServiceAuth401: yes(),
			Policies: []Policy{{ID: "p3", Name: "Service token", Decision: "non_identity", Include: []Include{{"service_token", "st-svc"}}}}},
		AppBackups: {Seen: Seen{Found: true, ID: "app-bak"}, Name: "backups", MatchedBy: "destinations",
			Policies: []Policy{{ID: "p4", Name: "host", Decision: "non_identity", Include: []Include{{"service_token", "st-bak"}}}}},
	}
	o.Rules = map[string]Rule{}
	for _, r := range d.Rules {
		idx, count := 0, 1
		if r.Position == "last" {
			idx, count = 2, 3
		}
		o.Rules[r.Key] = Rule{Seen: Seen{Found: true, ID: "r-" + r.Key}, RulesetID: "rs", MatchedBy: "description", Index: idx, Count: count,
			Description: r.Description, Expression: r.Expression, Action: r.Action, Enabled: yes(), Params: copyParams(r.Params)}
	}
	o.Rules[RuleSkip].Params["products"] = []any{"securityLevel", "rateLimit", "bic"}
	o.BotFightMode = BotFightMode{Seen: Seen{Found: true}, On: no()}
	o.D1 = Seen{Found: true, ID: "d1-uuid"}
	// An apply lists the tables (a D1 query); Wrangler's migrations add d1_migrations.
	o.D1Tables = Tables{Read: true, Names: []string{"_cf_KV", "budgets", "chunks", "d1_migrations", "snapshots", "sqlite_sequence"}}
	o.Worker = Worker{Seen: Seen{Found: true, ID: d.Worker.Script}, D1BindingID: "d1-uuid", Secrets: []string{"BACKUP_BEARER_SECRET"}, WorkersDev: no(), PreviewURLs: no(),
		Revision: "2",
		Domain:   WorkerDomain{Seen: Seen{Found: true, ID: "wd1"}, Service: d.Worker.Script}}
	return o
}

func copyParams(p map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range p {
		out[k] = v
	}
	return out
}

func byID(actions []Action) map[string]Action {
	m := map[string]Action{}
	for _, a := range actions {
		m[a.ID] = a
	}
	return m
}

func TestDiffFreshAccountCreatesEverything(t *testing.T) {
	got := Diff(desired(t), fresh())
	s := Summary(got)
	// The unset global session is the one manual line: setup does not write the
	// organization unless the apply engine is explicitly allowed to.
	if s[Drift] != 0 || s[Manual] != 1 || s[Unknowable] != 0 {
		t.Fatalf("summary %v", s)
	}
	for _, a := range got {
		want := Create
		switch a.ID {
		case "zone", "token", "zero_trust", "zone.bot_fight_mode":
			want = OK
		case "zero_trust.session":
			want = Manual
		}
		if a.Kind != want {
			t.Errorf("%s: %s, want %s (%v)", a.ID, a.Kind, want, a.Detail)
		}
	}
}

func TestDiffCompleteAccountChangesNothing(t *testing.T) {
	d := desired(t)
	got := Diff(d, complete(d))
	for _, a := range got {
		switch {
		case strings.HasSuffix(a.ID, ".secret"):
			if a.Kind != Unknowable {
				t.Errorf("%s: %s, want unknowable", a.ID, a.Kind)
			}
		case a.Kind != OK:
			t.Errorf("%s: %s %v", a.ID, a.Kind, a.Detail)
		}
	}
	if s := Summary(got); s[Create] != 0 || s[Drift] != 0 || s[Unknowable] != 3 {
		t.Fatalf("summary %v", s)
	}
}

func TestDiffStopsAtZone(t *testing.T) {
	for name, z := range map[string]Zone{
		"missing": {},
		"pending": {Seen: Seen{Found: true, ID: "z"}, Name: "example.org", Status: "pending"},
	} {
		t.Run(name, func(t *testing.T) {
			got := Diff(desired(t), Observed{Zone: z})
			if len(got) != 1 || got[0].Kind != Manual || !strings.Contains(got[0].Detail[0], "nameservers") {
				t.Fatalf("got %+v", got)
			}
		})
	}
	got := Diff(desired(t), Observed{Zone: Zone{Seen: Seen{Unknown: "token cannot read zones"}}})
	if len(got) != 1 || got[0].Kind != Unknowable {
		t.Fatalf("got %+v", got)
	}
}

func TestDiffTable(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Observed)
		id     string
		kind   Kind
		detail string // substring of some detail line
	}{
		{"no zero trust", func(o *Observed) { o.Org = Org{} }, "zero_trust", Manual, "team name"},
		{"global session set to 24h", func(o *Observed) { o.Org.SessionDuration = "24h" }, "zero_trust.session", Drift, "never changes a global value"},
		{"global session unset", func(o *Observed) { o.Org.SessionDuration = "" }, "zero_trust.session", Manual, "not set"},
		{"global session 730h written as minutes", func(o *Observed) { o.Org.SessionDuration = "43800m" }, "zero_trust.session", OK, ""},
		{"token not verified", func(o *Observed) { o.Token = Token{} }, "token", Unknowable, "verify"},
		{"token disabled", func(o *Observed) { o.Token.Status = "disabled" }, "token", Drift, "disabled"},
		{"tunnel inactive", func(o *Observed) { o.Tunnel.Status = "inactive" }, "tunnel", OK, "no cloudflared connector"},
		{"routed hostname without site app", func(o *Observed) { delete(o.Apps, AppSite) }, "access.app.site", Create, "WARNING: the tunnel already routes this hostname"},
		{"empty rule phase", func(o *Observed) { o.Rules[RuleConfig] = Rule{PhaseEmpty: true} }, "rule.config.consent", Create, "no http_config_settings ruleset yet"},
		{"tunnel route to wrong service", func(o *Observed) { o.Tunnel.Ingress[0].Service = "http://localhost:8090" }, "tunnel.route", Drift, "routes officers.example.org to http://localhost:8090"},
		{"tunnel route missing", func(o *Observed) { o.Tunnel.Ingress = o.Tunnel.Ingress[1:] }, "tunnel.route", Create, ""},
		{"locally managed tunnel", func(o *Observed) { o.Tunnel.ConfigSrc = "local" }, "tunnel.route", Unknowable, "local cloudflared file"},
		{"tunnel renamed, matched by id", func(o *Observed) { o.Tunnel.Name = "old"; o.Tunnel.MatchedBy = "id" }, "tunnel", OK, `named "old"`},
		{"A record where the tunnel CNAME goes", func(o *Observed) {
			o.DNS.Records = []DNSRecord{{ID: "a", Type: "A", Content: "192.0.2.1", Proxied: yes()}}
		}, "dns", Drift, "a A record to 192.0.2.1 already exists"},
		{"CNAME to another tunnel", func(o *Observed) { o.DNS.Records[0].Content = "other.cfargotunnel.com" }, "dns", Drift, "already exists"},
		{"CNAME not proxied", func(o *Observed) { o.DNS.Records[0].Proxied = no() }, "dns", Drift, "not proxied"},
		{"DNS proxied missing means false", func(o *Observed) { o.DNS.Records[0].Proxied = nil }, "dns", Drift, "not proxied"},
		{"no one-time PIN", func(o *Observed) { o.IdP = Seen{} }, "access.idp", Create, ""},
		{"site app allows another IdP", func(o *Observed) {
			a := o.Apps[AppSite]
			a.AllowedIdPs = []string{"otp1", "google"}
			o.Apps[AppSite] = a
		}, "access.app.site", Drift, "only One-time PIN"},
		{"site app session 24h", func(o *Observed) { a := o.Apps[AppSite]; a.SessionDuration = "24h"; o.Apps[AppSite] = a }, "access.app.site", Drift, "session duration is 24h"},
		{"site app instant auth off", func(o *Observed) { a := o.Apps[AppSite]; a.AutoRedirect = no(); o.Apps[AppSite] = a }, "access.app.site", Drift, "instant authentication"},
		{"site policy lacks Launchpad", func(o *Observed) {
			a := o.Apps[AppSite]
			a.Policies = []Policy{{Name: "Officers", Decision: "allow", Include: []Include{{"group", "g-off"}}}}
			o.Apps[AppSite] = a
		}, "access.app.site", Drift, "includes group:g-off"},
		{"site policy when groups are new", func(o *Observed) { delete(o.Groups, GroupLaunchpad) }, "access.app.site", Drift, "docs ask for group officers, group launchpad"},
		{"site app matched by name covers another host", func(o *Observed) {
			a := o.Apps[AppSite]
			a.MatchedBy, a.Destinations = "name", []string{"old.example.org"}
			o.Apps[AppSite] = a
		}, "access.app.site", Drift, "covers old.example.org"},
		{"site app renamed", func(o *Observed) { a := o.Apps[AppSite]; a.Name = "Dash"; o.Apps[AppSite] = a }, "access.app.site", OK, `named "Dash"`},
		{"services app without 401", func(o *Observed) { a := o.Apps[AppServices]; a.ServiceAuth401 = nil; o.Apps[AppServices] = a }, "access.app.services", Drift, "401 Response"},
		{"services policy with any token", func(o *Observed) {
			a := o.Apps[AppServices]
			a.Policies[0].Include = []Include{{Type: "other"}}
			o.Apps[AppServices] = a
		}, "access.app.services", Drift, "includes other"},
		{"backups app has a public allow", func(o *Observed) {
			a := o.Apps[AppBackups]
			a.Policies = append(a.Policies, Policy{Name: "Officers", Decision: "allow", Include: []Include{{Type: "everyone"}}})
			o.Apps[AppBackups] = a
		}, "access.app.backups", Drift, "docs forbid a public allow policy"},
		{"agents app without policy", func(o *Observed) { a := o.Apps[AppAgents]; a.Policies = nil; o.Apps[AppAgents] = a }, "access.app.agents", Drift, "no bypass policy"},
		{"app policies unreadable", func(o *Observed) { a := o.Apps[AppAgents]; a.PoliciesUnknown = "403"; o.Apps[AppAgents] = a }, "access.app.agents", Unknowable, "403"},
		{"group unreadable", func(o *Observed) {
			o.Groups[GroupOfficers] = Seen{Unknown: "token lacks Access: Organizations, Identity Providers, and Groups Read"}
		}, "access.group.officers", Unknowable, "lacks"},
		{"service token missing", func(o *Observed) { delete(o.ServiceTokens, TokenBackups) }, "access.service_token.backups", Create, ""},
		{"skip rule disabled", func(o *Observed) { r := o.Rules[RuleSkip]; r.Enabled = no(); o.Rules[RuleSkip] = r }, "rule.firewall.skip", Drift, "disabled"},
		{"skip rule not first", func(o *Observed) { r := o.Rules[RuleSkip]; r.Index, r.Count = 1, 2; o.Rules[RuleSkip] = r }, "rule.firewall.skip", Drift, "number 2 of 2"},
		{"skip rule misses securityLevel", func(o *Observed) { r := o.Rules[RuleSkip]; r.Params["products"] = []any{"bic"}; o.Rules[RuleSkip] = r }, "rule.firewall.skip", Drift, "action_parameters.products"},
		{"skip rule matched by expression", func(o *Observed) {
			r := o.Rules[RuleSkip]
			r.MatchedBy, r.Description = "expression", "mine"
			o.Rules[RuleSkip] = r
		}, "rule.firewall.skip", OK, `described as "mine"`},
		{"cache rule not last", func(o *Observed) { r := o.Rules[RuleCache]; r.Index = 0; o.Rules[RuleCache] = r }, "rule.cache.bypass", Drift, "docs ask for it last"},
		{"cache param absent", func(o *Observed) { r := o.Rules[RuleCache]; delete(r.Params, "cache"); o.Rules[RuleCache] = r }, "rule.cache.bypass", Unknowable, "action_parameters.cache not returned"},
		{"config rule edited expression", func(o *Observed) { r := o.Rules[RuleConfig]; r.Expression += " or true"; o.Rules[RuleConfig] = r }, "rule.config.consent", Drift, "expression differs"},
		{"config rule missing", func(o *Observed) { o.Rules[RuleConfig] = Rule{} }, "rule.config.consent", Create, ""},
		{"bot fight mode on", func(o *Observed) { o.BotFightMode.On = yes() }, "zone.bot_fight_mode", Drift, "whole zone"},
		{"super bot fight mode shape", func(o *Observed) { o.BotFightMode.On = nil }, "zone.bot_fight_mode", Unknowable, "fight_mode"},
		{"worker bound to other D1", func(o *Observed) { o.Worker.D1BindingID = "other" }, "worker", Drift, "bound to D1 database other"},
		{"worker without binding", func(o *Observed) { o.Worker.D1BindingID = "" }, "worker", Drift, "no D1 binding named BACKUPS"},
		{"workers.dev on, Worker not uploaded by setup", func(o *Observed) { o.Worker.WorkersDev = yes(); o.Worker.Revision = "" }, "worker.subdomain", Drift, "workers.dev URL is on"},
		{"preview URLs on, Worker not uploaded by setup", func(o *Observed) { o.Worker.PreviewURLs = yes(); o.Worker.Revision = "" }, "worker.subdomain", Drift, "preview URLs are on"},
		{"workers.dev on, Worker setup uploaded", func(o *Observed) { o.Worker.WorkersDev = yes() }, "worker.subdomain", Create, "setup uploaded this Worker"},
		{"preview urls missing means off", func(o *Observed) { o.Worker.PreviewURLs = nil }, "worker.subdomain", OK, ""},
		{"workers.dev switches unreadable", func(o *Observed) { o.Worker.SubdomainUnknown = "no permission" }, "worker.subdomain", Unknowable, "no permission"},
		{"no worker means subdomain create", func(o *Observed) { o.Worker = Worker{} }, "worker.subdomain", Create, ""},
		{"no worker means version create", func(o *Observed) { o.Worker = Worker{} }, "worker.version", Create, ""},
		{"worker deployed without setup's label", func(o *Observed) { o.Worker.Revision = "" }, "worker.version", Unknowable, "alerts when it is older"},
		{"worker older than this image", func(o *Observed) { o.Worker.Revision = "1" }, "worker.version", Drift, "never uploads over an existing Worker"},
		{"worker newer than this image", func(o *Observed) { o.Worker.Revision = "3" }, "worker.version", Drift, "newer than"},
		{"worker label not a number", func(o *Observed) { o.Worker.Revision = "x" }, "worker.version", Drift, "not a revision"},
		{"no D1 means migrations create", func(o *Observed) { o.D1 = Seen{} }, "d1.migrations", Create, ""},
		{"dry run cannot list D1 tables", func(o *Observed) { o.D1Tables = Tables{} }, "d1.migrations", Unknowable, "a dry run does not send"},
		{"D1 tables unreadable", func(o *Observed) { o.D1Tables = Tables{Unknown: "the token cannot query D1"} }, "d1.migrations", Unknowable, "cannot query D1"},
		{"empty D1 gets the migrations", func(o *Observed) {
			o.D1Tables = Tables{Read: true, Names: []string{"_cf_KV", "sqlite_sequence"}}
		}, "d1.migrations", Create, "has no tables"},
		{"D1 with other tables is never altered", func(o *Observed) { o.D1Tables = Tables{Read: true, Names: []string{"members"}} }, "d1.migrations", Drift, "never alters a database that already has tables"},
		{"D1 partly migrated is never altered", func(o *Observed) {
			o.D1Tables = Tables{Read: true, Names: []string{"d1_migrations", "snapshots"}}
		}, "d1.migrations", Drift, "budgets, chunks"},
		{"D1 migrated without Wrangler's record", func(o *Observed) {
			o.D1Tables = Tables{Read: true, Names: []string{"budgets", "chunks", "snapshots"}}
		}, "d1.migrations", OK, "no d1_migrations table"},
		{"backup hostname has a CNAME", func(o *Observed) {
			o.Worker.Domain = WorkerDomain{}
			o.BackupDNS = DNS{Records: []DNSRecord{{ID: "dns9", Type: "CNAME", Content: "old.example.net"}}}
		}, "worker.domain", Manual, "never removes a record"},
		{"backup hostname DNS unreadable", func(o *Observed) { o.Worker.Domain = WorkerDomain{}; o.BackupDNS = DNS{Unknown: "no DNS Read"} }, "worker.domain", Unknowable, "no DNS Read"},
		{"backup hostname outside the zone", func(o *Observed) { o.Worker.Domain = WorkerDomain{}; o.Zone.Name = "other.org" }, "worker.domain", Manual, "not in the zone other.org"},
		{"no custom domain means create", func(o *Observed) { o.Worker.Domain = WorkerDomain{} }, "worker.domain", Create, ""},
		{"worker secret missing", func(o *Observed) { o.Worker.Secrets = nil }, "worker.secret", Create, ""},
		{"no worker means secret create", func(o *Observed) { o.Worker = Worker{} }, "worker.secret", Create, ""},
		{"custom domain serves another Worker", func(o *Observed) { o.Worker.Domain.Service = "other" }, "worker.domain", Drift, `serves Worker "other"`},
	}
	d := desired(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := complete(d)
			tc.mutate(&o)
			a, ok := byID(Diff(d, o))[tc.id]
			if !ok {
				t.Fatalf("no action %s", tc.id)
			}
			if a.Kind != tc.kind {
				t.Fatalf("%s: kind %s, want %s (%v)", tc.id, a.Kind, tc.kind, a.Detail)
			}
			if tc.detail != "" && !strings.Contains(strings.Join(a.Detail, "\n"), tc.detail) {
				t.Fatalf("%s: detail %q lacks %q", tc.id, a.Detail, tc.detail)
			}
		})
	}
}

func TestNewDesired(t *testing.T) {
	for _, bad := range []string{"", "http://officers.example.org", "https://officers.example.org/path", "https://officers.example.org:8443", "https://localhost", "officers.example.org"} {
		if _, err := NewDesired(Inputs{AppURL: bad}); err == nil {
			t.Errorf("APP_URL %q accepted", bad)
		}
	}
	if _, err := NewDesired(Inputs{AppURL: "https://example.org"}); err == nil {
		t.Error("derived a backup hostname from a bare domain")
	}
	if _, err := NewDesired(Inputs{AppURL: "https://officers.example.org", BackupHostname: "officers.example.org"}); err == nil {
		t.Error("accepted the dashboard hostname as backup hostname")
	}

	d, err := NewDesired(Inputs{AppURL: "https://Officers.Example.org/", NamePrefix: "stg-"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Hostname != "officers.example.org" || d.BackupHostname != "backups.example.org" {
		t.Fatalf("hosts %q %q", d.Hostname, d.BackupHostname)
	}
	if d.Tunnel.Name != "stg-officer-dashboard" || d.D1Database != "stg-dashboard-backups" || d.Worker.Script != "stg-dashboard-backup-sink" ||
		d.Apps[0].Name != "stg-Officer Dashboard" || d.Groups[0].Name != "stg-Officers" || d.Rules[0].Description != "stg-Agents and services: skip bot checks" ||
		d.ServiceTokens[0].Name != "stg-officer-dashboard services" {
		t.Fatalf("prefix not applied everywhere: %+v", d)
	}
	if len(d.Groups) != 1 || len(d.Apps[0].Policy.Groups) != 1 || d.Apps[0].Policy.Name != "stg-Officers" {
		t.Fatalf("Launchpad should be omitted without --launchpad-group: %+v %+v", d.Groups, d.Apps[0].Policy)
	}
	// The doc expression, filled in (cloudflare.md step 8).
	if want := `(http.host eq "officers.example.org" and http.request.uri.path eq "/oauth/authorize")`; d.Rules[2].Expression != want {
		t.Fatalf("config expression %s", d.Rules[2].Expression)
	}
	if got := d.Apps[1].Destinations; len(got) != 6 || got[0] != "officers.example.org/mcp" || got[5] != "officers.example.org/.well-known/oauth-authorization-server" {
		t.Fatalf("agent destinations %v", got)
	}
}

func TestNormalizeDestination(t *testing.T) {
	for in, want := range map[string]string{
		"Officers.Example.org":              "officers.example.org",
		"officers.example.org/":             "officers.example.org",
		"https://officers.example.org/mcp/": "officers.example.org/mcp",
		"officers.example.org//api/agent":   "officers.example.org/api/agent",
	} {
		if got := NormalizeDestination(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}
