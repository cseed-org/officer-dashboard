package cfprovision

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"officerdashboard/internal/setup/cfapi"
	"officerdashboard/internal/setup/cffake"
	"officerdashboard/internal/setup/plan"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/")

// rig is one fake account, the host's secret sink, and the runs made against them.
type rig struct {
	t    *testing.T
	acct *cffake.Account
	sink *MemorySink
	d    plan.Desired
	out  bytes.Buffer
	opt  Options
}

func newRig(t *testing.T, routes map[string]cffake.Response) *rig {
	t.Helper()
	r := &rig{t: t, acct: cffake.NewAccount(routes), sink: &MemorySink{}, d: desired(t, ""), opt: Options{
		GroupEmails: map[string][]string{plan.GroupOfficers: {"president@example.org"}, plan.GroupLaunchpad: {"mentor@example.org"}},
		StateDir:    t.TempDir(),
		Bundle:      testBundle(t),
	}}
	// `setup host` generated the backup bearer before any apply.
	if err := r.sink.Store(context.Background(), Secret{Name: SecretBackupBearer, Value: testBearer}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		r.acct.Close()
		// Setup never deletes and never patches: no request may use another method.
		for _, req := range r.acct.Writes() {
			if req.Method != http.MethodPost && req.Method != http.MethodPut {
				t.Errorf("sent %s %s", req.Method, req.Path)
			}
		}
	})
	return r
}

// preview is what `dashboard setup --dry-run` reports for the account now.
func (r *rig) preview() []plan.Action {
	r.t.Helper()
	before := len(r.acct.Writes())
	o, err := Discover(context.Background(), cfapi.New("test-token", cfapi.WithBaseURL(r.acct.URL), cfapi.WithRetry()), r.d)
	if err != nil {
		r.t.Fatalf("preview: %v", err)
	}
	if len(r.acct.Writes()) != before {
		r.t.Fatal("the preview wrote")
	}
	return plan.Diff(r.d, o)
}

func (r *rig) apply(ctx context.Context) (Outputs, error) {
	r.t.Helper()
	e := NewEngine("test-token", r.d, r.sink, &r.out, r.opt, WithAPIBase(r.acct.URL), WithReadRetry())
	// The plan the person reviews is the engine's own preview, as the command shows it:
	// only the engine lists the backup database's tables (a read-only D1 query), so a dry
	// run's d1.migrations line differs from it by design.
	rev, err := e.Preview(context.Background())
	if err != nil {
		return Outputs{}, err
	}
	return e.Apply(ctx, rev.Actions)
}

func byID(actions []plan.Action) map[string]plan.Action {
	m := map[string]plan.Action{}
	for _, a := range actions {
		m[a.ID] = a
	}
	return m
}

// inScope are the action IDs this version creates.
// The global session duration is not among them: by default setup does not write the
// organization (TestOrgSessionIsManualByDefault).
var inScope = []string{"access.idp", "access.group.officers", "access.group.launchpad",
	"access.service_token.services", "access.app.site", "access.app.agents", "access.app.services",
	"rule.firewall.skip", "rule.cache.bypass", "rule.config.consent", "tunnel", "tunnel.route", "dns",
	"access.service_token.backups", "access.app.backups", "d1", "worker", "worker.version", "worker.subdomain", "worker.domain"}

func requireAllOK(t *testing.T, actions []plan.Action, except ...string) {
	t.Helper()
	m := byID(actions)
	for _, id := range inScope {
		if slices.Contains(except, id) {
			continue
		}
		if a := m[id]; a.Kind != plan.OK {
			t.Errorf("%s: %s %v, want ok", id, a.Kind, a.Detail)
		}
	}
}

func TestApplyFreshAccountEndsAllOK(t *testing.T) {
	r := newRig(t, cffake.Fresh())
	out, err := r.apply(context.Background())
	if err != nil {
		t.Fatalf("%v\n%s", err, r.out.String())
	}
	after := r.preview()
	requireAllOK(t, after)
	m := byID(after)
	for _, id := range []string{"zone", "token", "zero_trust", "zone.bot_fight_mode"} {
		if m[id].Kind != plan.OK {
			t.Errorf("%s: %s", id, m[id].Kind)
		}
	}
	// What only the engine can see: the tables (a D1 query) read back ok. The Worker's
	// secret stays unknowable, as Cloudflare never returns it.
	fin := byID(out.Final)
	if a := fin["d1.migrations"]; a.Kind != plan.OK {
		t.Errorf("d1.migrations after the run: %s %v", a.Kind, a.Detail)
	}
	if a := m["worker.secret"]; a.Kind != plan.Unknowable {
		t.Errorf("worker.secret: %s %v", a.Kind, a.Detail)
	}
	if st, ok := r.sink.Get(SecretBackupsToken); !ok || st.ClientID == "" || st.Value == "" || st.ResourceID != m["access.service_token.backups"].ExistingID {
		t.Errorf("backup token not kept: %v", st)
	}
	if out.BackupSinkURL != "https://"+cffake.Backup || out.D1ID == "" || out.WorkerRevision != "2" {
		t.Errorf("backup outputs %+v", out)
	}
	if tok, ok := r.sink.Get(SecretTunnelToken); !ok || tok.ResourceID != out.TunnelID || !strings.HasPrefix(tok.Value, "eyJ") {
		t.Errorf("tunnel token not kept: %v", tok)
	}
	if st, ok := r.sink.Get(SecretServicesToken); !ok || st.ClientID == "" || st.Value == "" || st.ResourceID != m["access.service_token.services"].ExistingID {
		t.Errorf("services token not kept: %v", st)
	}
	if out.TeamDomain != "club.cloudflareaccess.com" || out.SiteAUD == "" || out.ServicesAUD == "" || out.TunnelID == "" {
		t.Errorf("outputs %+v", out)
	}
	if len(out.Writes) == 0 || len(out.Writes) != len(r.acct.Writes()) {
		t.Errorf("%d writes recorded, fake saw %d", len(out.Writes), len(r.acct.Writes()))
	}
	// Running apply again on the finished account writes nothing.
	n := len(r.acct.Writes())
	if _, err := r.apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.acct.Writes()) != n {
		t.Errorf("a second run wrote %d more requests", len(r.acct.Writes())-n)
	}
}

// writeIndex returns the position of the first write matching method and path suffix.
func writeIndex(ws []cffake.Request, method, suffix string) int {
	for i, w := range ws {
		if w.Method == method && strings.HasSuffix(w.Path, suffix) {
			return i
		}
	}
	return -1
}

func TestApplyNeverPublishesBeforeAccess(t *testing.T) {
	r := newRig(t, cffake.Fresh())
	if _, err := r.apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	ws := r.acct.Writes()
	route := writeIndex(ws, http.MethodPut, "/configurations")
	dns := writeIndex(ws, http.MethodPost, "/dns_records")
	if route < 0 || dns < 0 || dns < route {
		t.Fatalf("route at %d, DNS at %d", route, dns)
	}
	apps := 0
	for i, w := range ws {
		if w.Method == http.MethodPost && strings.HasSuffix(w.Path, "/access/apps") {
			apps++
			if i > route {
				t.Errorf("Access application created at write %d, after the route at %d", i, route)
			}
		}
	}
	if apps != 4 {
		t.Errorf("%d applications created, want 4 (three for the dashboard, one for backups)", apps)
	}
}

func TestApplyLiveAccountCreatesAccessFirstAndSaysSo(t *testing.T) {
	// The live account: the route and the CNAME exist, no Access application protects
	// the hostname, the tunnel is down, and the host has no tunnel token yet.
	r := newRig(t, cffake.Live())
	_, err := r.apply(context.Background())
	if err != nil {
		t.Fatalf("%v\n%s", err, r.out.String())
	}
	text := r.out.String()
	if !strings.Contains(text, "WARNING: officers.example.org is already published") || !strings.Contains(text, "Setup changes nothing that publishes the hostname, and creates its Access application at step") {
		t.Errorf("no loud warning:\n%s", text)
	}
	ws := r.acct.Writes()
	if len(ws) == 0 || !strings.Contains(ws[0].Path, "/access/") {
		t.Fatalf("first write %+v, want an Access write", ws)
	}
	for _, w := range ws {
		if strings.HasSuffix(w.Path, "/configurations") || strings.HasSuffix(w.Path, "/dns_records") || strings.HasSuffix(w.Path, "/cfd_tunnel") {
			t.Errorf("touched the existing publication: %s %s", w.Method, w.Path)
		}
	}
	after := byID(r.preview())
	// The live Worker was deployed with Wrangler, so it carries no revision label.
	requireAllOK(t, r.preview(), "worker.version")
	// Bot Fight Mode is zone-wide: left on, with the manual step printed.
	if after["zone.bot_fight_mode"].Kind != plan.Drift || !strings.Contains(text, "manual: Bot Fight Mode is on") || !strings.Contains(text, "Bot Fight Mode → Off") {
		t.Errorf("bot fight mode %s; output:\n%s", after["zone.bot_fight_mode"].Kind, text)
	}
	// The existing tunnel's token was fetched for the host, which had none.
	if tok, ok := r.sink.Get(SecretTunnelToken); !ok || tok.ResourceID != cffake.TunnelID {
		t.Errorf("tunnel token %v", tok)
	}
	if writeIndex(ws, http.MethodPut, "/bot_management") >= 0 {
		t.Error("Bot Fight Mode was changed without the option")
	}
}

// count returns how many stored items at path have field == value.
func count(a *cffake.Account, path, field, value string) int {
	route, _ := a.Route(path)
	list, _ := route.Result.([]any)
	n := 0
	for _, it := range list {
		if m, ok := it.(map[string]any); ok && fmt.Sprint(m[field]) == value {
			n++
		}
	}
	return n
}

func ruleCount(a *cffake.Account, phase, description string) int {
	route, _ := a.Route("/zones/" + cffake.ZoneID + "/rulesets/phases/" + phase + "/entrypoint")
	rs, _ := route.Result.(map[string]any)
	rules, _ := rs["rules"].([]any)
	n := 0
	for _, it := range rules {
		if m, ok := it.(map[string]any); ok && m["description"] == description {
			n++
		}
	}
	return n
}

// requireNoDuplicates checks each resource setup creates exists exactly once.
func requireNoDuplicates(t *testing.T, a *cffake.Account, d plan.Desired) {
	t.Helper()
	acct := "/accounts/" + cffake.AccountID
	checks := []struct {
		path, field, value string
	}{
		{acct + "/cfd_tunnel", "name", d.Tunnel.Name},
		{"/zones/" + cffake.ZoneID + "/dns_records", "name", d.Hostname},
		{acct + "/access/identity_providers", "type", "onetimepin"},
		{acct + "/access/service_tokens", "name", "officer-dashboard services"},
		{acct + "/access/service_tokens", "name", "dashboard-backups-host"},
		{acct + "/d1/database", "name", d.D1Database},
		{acct + "/workers/scripts", "id", d.Worker.Script},
		{acct + "/workers/domains", "hostname", d.Worker.Hostname},
	}
	for _, g := range d.Groups {
		checks = append(checks, struct{ path, field, value string }{acct + "/access/groups", "name", g.Name})
	}
	for _, spec := range d.Apps {
		checks = append(checks, struct{ path, field, value string }{acct + "/access/apps", "name", spec.Name},
			struct{ path, field, value string }{acct + "/access/policies", "name", spec.Policy.Name})
	}
	for _, c := range checks {
		if n := count(a, c.path, c.field, c.value); n != 1 {
			t.Errorf("%s %s=%q: %d, want exactly 1", c.path, c.field, c.value, n)
		}
	}
	for _, r := range d.Rules {
		if n := ruleCount(a, r.Phase, r.Description); n != 1 {
			t.Errorf("rule %q: %d, want 1", r.Description, n)
		}
	}
}

func TestApplyInterruptedAfterAnyWriteIsSafeToRerun(t *testing.T) {
	full := newRig(t, cffake.Fresh())
	if _, err := full.apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	total := len(full.acct.Writes())
	if total < 15 {
		t.Fatalf("only %d writes in a full run", total)
	}
	for k := 1; k <= total; k++ {
		t.Run(fmt.Sprintf("crash after write %d", k), func(t *testing.T) {
			r := newRig(t, cffake.Fresh())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			n := 0
			var crashed cffake.Request
			r.acct.OnWrite(func(req cffake.Request) {
				if n++; n == k {
					crashed = req
					cancel() // the process dies with this write stored by Cloudflare
				}
			})
			if _, err := r.apply(ctx); err == nil {
				t.Fatalf("run 1 survived the crash after %s %s", crashed.Method, crashed.Path)
			}
			r.acct.OnWrite(nil)
			// The cancel races with the fake's answer: sometimes the crashed run still got a
			// token's client secret, and then it must have kept it.
			keptBefore := map[SecretName]bool{}
			for _, sec := range []SecretName{SecretServicesToken, SecretBackupsToken} {
				_, keptBefore[sec] = r.sink.Get(sec)
			}
			if _, err := r.apply(context.Background()); err != nil {
				t.Fatalf("re-run after a crash at %s %s: %v\n%s", crashed.Method, crashed.Path, err, r.out.String())
			}
			requireAllOK(t, r.preview())
			requireNoDuplicates(t, r.acct, r.d)
			if _, ok := r.sink.Get(SecretTunnelToken); !ok {
				t.Error("the re-run did not recover the tunnel token")
			}
			// The backup Worker is uploaded once, whatever the crash; the host's bearer is
			// never rewritten.
			if n := len(r.acct.Uploads()); n != 1 {
				t.Errorf("%d Worker uploads", n)
			}
			if b, _ := r.sink.Load(context.Background(), SecretBackupBearer); b != testBearer {
				t.Error("the host's bearer changed")
			}
			for _, tok := range []struct {
				name   string
				secret SecretName
			}{{"officer-dashboard services", SecretServicesToken}, {"dashboard-backups-host", SecretBackupsToken}} {
				_, kept := r.sink.Get(tok.secret)
				if strings.HasSuffix(crashed.Path, "/access/service_tokens") && strings.Contains(crashed.Body, tok.name) && !keptBefore[tok.secret] {
					// The client secret existed only in the crashed process: say to rotate it.
					if kept || !strings.Contains(r.out.String(), "rotate it") || !strings.Contains(r.out.String(), tok.name) {
						t.Errorf("lost client secret of %s not reported:\n%s", tok.name, r.out.String())
					}
				} else if !kept {
					t.Errorf("the client secret of %s was lost", tok.name)
				}
			}
		})
	}
}

func TestDriftOrUnknownDependencyBlocksBeforeAnyWrite(t *testing.T) {
	acct := "/accounts/" + cffake.AccountID
	cases := map[string]func(map[string]cffake.Response){
		// The route sends the hostname elsewhere, so the DNS record (create) depends on drift.
		"route drift": func(r map[string]cffake.Response) {
			r[acct+"/cfd_tunnel"] = cffake.Response{Result: []any{map[string]any{"id": cffake.TunnelID, "name": "officer-dashboard", "config_src": "cloudflare", "status": "inactive"}}}
			r[acct+"/cfd_tunnel/"+cffake.TunnelID+"/configurations"] = cffake.Response{Result: map[string]any{"config": map[string]any{
				"ingress": []any{map[string]any{"hostname": cffake.Host, "service": "http://elsewhere:80"}, map[string]any{"service": "http_status:404"}}}}}
		},
		// The identity providers cannot be read, so the site application (create) has an
		// unknowable dependency.
		"unreadable dependency": func(r map[string]cffake.Response) {
			r[acct+"/access/identity_providers"] = cffake.Response{Status: 401, Code: 1001, Message: "Not authorized"}
		},
		// No Zero Trust organization: a manual step everything depends on.
		"manual prerequisite": func(r map[string]cffake.Response) {
			r[acct+"/access/organizations"] = cffake.Response{Status: 404, Code: 7003, Message: "no organization"}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			routes := cffake.Fresh()
			mutate(routes)
			r := newRig(t, routes)
			_, err := r.apply(context.Background())
			var blocked *BlockedError
			if !errors.As(err, &blocked) {
				t.Fatalf("err = %v, want BlockedError\n%s", err, r.out.String())
			}
			if len(r.acct.Writes()) != 0 {
				t.Fatalf("wrote %+v before stopping", r.acct.Writes())
			}
			if !strings.Contains(r.out.String(), "Setup will not start, and has changed nothing") {
				t.Errorf("output:\n%s", r.out.String())
			}
		})
	}
}

func TestReviewedPlanMustStillHold(t *testing.T) {
	r := newRig(t, cffake.Fresh())
	reviewed := r.preview()
	// Someone adds the Officers group by hand between the preview and the apply.
	r.acct.Set("/accounts/"+cffake.AccountID+"/access/groups", cffake.Response{Result: []any{map[string]any{"id": "g-hand", "name": "Officers"}}})
	e := NewEngine("test-token", r.d, r.sink, &r.out, r.opt, WithAPIBase(r.acct.URL), WithReadRetry())
	_, err := e.Apply(context.Background(), reviewed)
	var changed *PlanChangedError
	if !errors.As(err, &changed) || !strings.Contains(err.Error(), "access.group.officers") || len(r.acct.Writes()) != 0 {
		t.Fatalf("err = %v, writes %d", err, len(r.acct.Writes()))
	}
}

func TestMissingGroupEmailsBlock(t *testing.T) {
	r := newRig(t, cffake.Fresh())
	r.opt.GroupEmails = map[string][]string{plan.GroupOfficers: {"Not An Email"}}
	_, err := r.apply(context.Background())
	if err == nil || !strings.Contains(err.Error(), `"Launchpad" must be created`) || !strings.Contains(err.Error(), "not a plain email") || len(r.acct.Writes()) != 0 {
		t.Fatalf("err = %v", err)
	}
}

func TestFreeRuleSlotsAreCounted(t *testing.T) {
	routes := cffake.Fresh()
	var rules []any
	for i := range 5 {
		rules = append(rules, map[string]any{"id": fmt.Sprint("r", i), "description": fmt.Sprint("club rule ", i), "expression": "true", "action": "block", "enabled": true})
	}
	routes["/zones/"+cffake.ZoneID+"/rulesets/phases/http_request_firewall_custom/entrypoint"] = cffake.Response{Result: map[string]any{"id": "rs-fw", "rules": rules}}
	r := newRig(t, routes)
	_, err := r.apply(context.Background())
	if err == nil || !strings.Contains(err.Error(), "already has 5 of the 5 rules the free plan allows") || len(r.acct.Writes()) != 0 {
		t.Fatalf("err = %v", err)
	}
}

func TestRulesAppendToExistingPhasesAndKeepTheirRules(t *testing.T) {
	routes := cffake.Fresh() // the cache phase already has "Cache images"
	routes["/zones/"+cffake.ZoneID+"/rulesets/phases/http_request_firewall_custom/entrypoint"] = cffake.Response{Result: map[string]any{
		"id": "rs-fw", "phase": "http_request_firewall_custom", "rules": []any{
			map[string]any{"id": "r-block", "description": "Block bad countries", "expression": `(ip.src.country eq "XX")`, "action": "block", "enabled": true},
		}}}
	r := newRig(t, routes)
	if _, err := r.apply(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, r.out.String())
	}
	descs := func(phase string) string {
		route, _ := r.acct.Route("/zones/" + cffake.ZoneID + "/rulesets/phases/" + phase + "/entrypoint")
		var out []string
		for _, it := range route.Result.(map[string]any)["rules"].([]any) {
			out = append(out, it.(map[string]any)["description"].(string))
		}
		return strings.Join(out, " | ")
	}
	if got := descs("http_request_firewall_custom"); got != "Agents and services: skip bot checks | Block bad countries" {
		t.Errorf("firewall rules: %s (the skip rule goes first)", got)
	}
	if got := descs("http_request_cache_settings"); got != "Cache images | Agents and services: bypass cache" {
		t.Errorf("cache rules: %s (bypass goes last)", got)
	}
	if got := descs("http_config_settings"); got != "Consent page: no rewriting" {
		t.Errorf("config rules: %s", got)
	}
	ws := r.acct.Writes()
	if writeIndex(ws, http.MethodPost, "/rulesets/rs-fw/rules") < 0 || writeIndex(ws, http.MethodPost, "/rulesets") < 0 {
		t.Errorf("want one append to rs-fw and one ruleset create: %+v", ws)
	}
	for _, w := range ws {
		if w.Method == http.MethodPut && strings.Contains(w.Path, "/rulesets") {
			t.Errorf("replaced a ruleset: %s", w.Path)
		}
	}
}

func TestRouteKeepsEveryOtherRuleAndTheCatchAll(t *testing.T) {
	routes := cffake.Fresh()
	acct := "/accounts/" + cffake.AccountID
	routes[acct+"/cfd_tunnel"] = cffake.Response{Result: []any{map[string]any{"id": cffake.TunnelID, "name": "officer-dashboard", "config_src": "cloudflare",
		"status": "healthy", "conns_active_at": "2026-11-01T00:00:00Z"}}}
	wiki := map[string]any{"hostname": "wiki.example.org", "service": "http://wiki:80", "originRequest": map[string]any{"noTLSVerify": true}}
	routes[acct+"/cfd_tunnel/"+cffake.TunnelID+"/configurations"] = cffake.Response{Result: map[string]any{"tunnel_id": cffake.TunnelID, "version": 3,
		"config": map[string]any{"ingress": []any{wiki, map[string]any{"service": "http_status:404"}}, "warp-routing": map[string]any{"enabled": true},
			"originRequest": map[string]any{"connectTimeout": 10}}}}
	r := newRig(t, routes)
	if _, err := r.apply(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, r.out.String())
	}
	route, _ := r.acct.Route(acct + "/cfd_tunnel/" + cffake.TunnelID + "/configurations")
	got, _ := json.Marshal(route.Result.(map[string]any)["config"])
	want := `{"ingress":[{"hostname":"wiki.example.org","originRequest":{"noTLSVerify":true},"service":"http://wiki:80"},{"hostname":"officers.example.org","service":"http://dashboard:8090"},{"service":"http_status:404"}],"originRequest":{"connectTimeout":10},"warp-routing":{"enabled":true}}`
	if !jsonEqual(json.RawMessage(got), json.RawMessage(want)) {
		t.Errorf("config\n got %s\nwant %s", got, want)
	}
	if count(r.acct, acct+"/cfd_tunnel", "name", "officer-dashboard") != 1 {
		t.Error("a second tunnel was created")
	}
}

func TestUnreadableConfigOfAUsedTunnelBlocks(t *testing.T) {
	routes := cffake.Fresh()
	acct := "/accounts/" + cffake.AccountID
	// A tunnel that has run before, whose configuration answers 404: a PUT would replace
	// whatever Cloudflare could not show.
	routes[acct+"/cfd_tunnel"] = cffake.Response{Result: []any{map[string]any{"id": cffake.TunnelID, "name": "officer-dashboard", "config_src": "cloudflare",
		"status": "down", "conns_active_at": "2026-11-01T00:00:00Z"}}}
	r := newRig(t, routes)
	_, err := r.apply(context.Background())
	var blocked *BlockedError
	if !errors.As(err, &blocked) || !strings.Contains(err.Error(), "has been used before") || len(r.acct.Writes()) != 0 {
		t.Fatalf("err = %v, writes %d", err, len(r.acct.Writes()))
	}
}

func TestBotFightModeOnlyWithTheOption(t *testing.T) {
	routes := cffake.Live()
	routes["/zones/"+cffake.ZoneID+"/dns_records"] = cffake.Response{Result: []any{
		map[string]any{"id": "dns-1", "name": cffake.Host, "type": "CNAME", "content": cffake.TunnelID + ".cfargotunnel.com", "proxied": true},
		map[string]any{"id": "dns-2", "name": "www.example.org", "type": "A", "content": "192.0.2.1", "proxied": true},
		map[string]any{"id": "dns-3", "name": "example.org", "type": "A", "content": "192.0.2.1", "proxied": true},
	}}
	r := newRig(t, routes)
	r.opt.AllowBotFightModeOff = true
	r.opt.StateDir = t.TempDir()
	if _, err := r.apply(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, r.out.String())
	}
	if !strings.Contains(r.out.String(), "zone's other hostnames: example.org, www.example.org") {
		t.Errorf("other hostnames not listed:\n%s", r.out.String())
	}
	if k := byID(r.preview())["zone.bot_fight_mode"].Kind; k != plan.OK {
		t.Errorf("bot fight mode %s", k)
	}
	route, _ := r.acct.Route("/zones/" + cffake.ZoneID + "/bot_management")
	if m := route.Result.(map[string]any); m["enable_js"] != true || m["ai_bots_protection"] != "disabled" {
		t.Errorf("other bot settings changed: %v", m)
	}
}

func TestWildcardAppIsDriftAndBlocksTheSiteCreate(t *testing.T) {
	// F-17: a wildcard application may already decide who reaches the hostname. The
	// app-paths page says what a wildcard covers, not which application wins.
	routes := cffake.Live()
	acct := "/accounts/" + cffake.AccountID
	apps := routes[acct+"/access/apps"].Result.([]any)
	routes[acct+"/access/apps"] = cffake.Response{Result: append(apps, map[string]any{"id": "app-wild", "name": "All club sites", "type": "self_hosted",
		"domain": "*.example.org", "destinations": []any{map[string]any{"type": "public", "uri": "*.example.org"}},
		"policies": []any{map[string]any{"id": "p-wild", "name": "Members", "decision": "allow", "include": []any{map[string]any{"everyone": map[string]any{}}}}}})}
	r := newRig(t, routes)
	site := byID(r.preview())["access.app.site"]
	if site.Kind != plan.Drift || !strings.Contains(strings.Join(site.Detail, "\n"), "wildcard destination *.example.org") {
		t.Fatalf("site %s %v", site.Kind, site.Detail)
	}
	for _, id := range []string{"access.app.agents", "access.app.services"} {
		if a := byID(r.preview())[id]; a.Kind != plan.Drift {
			t.Errorf("%s: %s, want drift (the wildcard covers its paths too)", id, a.Kind)
		}
	}
	if _, err := r.apply(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, r.out.String())
	}
	if n := count(r.acct, acct+"/access/apps", "name", "Officer Dashboard"); n != 0 {
		t.Errorf("created the site app beside a wildcard app")
	}
}

func TestPartialAgentsAppIsDriftNotCreate(t *testing.T) {
	// F-21: an application holding only some of the six agent paths.
	routes := cffake.Live()
	acct := "/accounts/" + cffake.AccountID
	apps := routes[acct+"/access/apps"].Result.([]any)
	routes[acct+"/access/apps"] = cffake.Response{Result: append(apps, map[string]any{"id": "app-mcp", "name": "MCP only", "type": "self_hosted",
		"domain": cffake.Host + "/mcp", "destinations": []any{map[string]any{"type": "public", "uri": cffake.Host + "/mcp"}},
		"policies": []any{map[string]any{"id": "p-mcp", "name": "Open", "decision": "bypass", "include": []any{map[string]any{"everyone": map[string]any{}}}}}})}
	r := newRig(t, routes)
	m := byID(r.preview())
	agents := m["access.app.agents"]
	if agents.Kind != plan.Drift || !strings.Contains(strings.Join(agents.Detail, "\n"), `"MCP only" (app-mcp) also covers officers.example.org/mcp`) {
		t.Fatalf("agents %s %v", agents.Kind, agents.Detail)
	}
	if m["access.app.services"].Kind != plan.Create {
		t.Errorf("services %s: an app on /mcp does not overlap /api/ingest", m["access.app.services"].Kind)
	}
	// The site line is drift too (another app on its host), and the services app waits
	// for the site app (F-6), so the run refuses before any write.
	_, err := r.apply(context.Background())
	var blocked *BlockedError
	if !errors.As(err, &blocked) || len(r.acct.Writes()) != 0 {
		t.Fatalf("err = %v, writes %d", err, len(r.acct.Writes()))
	}
	if n := count(r.acct, acct+"/access/apps", "name", "Officer Dashboard: agents (bypass)"); n != 0 {
		t.Error("created a second application on /mcp")
	}
}
