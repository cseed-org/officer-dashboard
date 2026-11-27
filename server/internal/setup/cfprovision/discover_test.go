package cfprovision

import (
	"context"
	"errors"
	"strings"
	"testing"

	"officerdashboard/internal/setup/cfapi"
	"officerdashboard/internal/setup/cffake"
	"officerdashboard/internal/setup/plan"
)

func desired(t *testing.T, prefix string) plan.Desired {
	t.Helper()
	d, err := plan.NewDesired(plan.Inputs{AppURL: "https://" + cffake.Host, LaunchpadGroup: "Launchpad", NamePrefix: prefix})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// discover runs Discover against the fake and fails the test if any request was not GET.
func discover(t *testing.T, routes map[string]cffake.Response, d plan.Desired) (plan.Observed, error) {
	t.Helper()
	srv := cffake.Start(routes)
	t.Cleanup(srv.Close)
	o, err := Discover(context.Background(), cfapi.New("test-token", cfapi.WithBaseURL(srv.URL)), d)
	if bad := srv.NonGET(); len(bad) != 0 {
		t.Fatalf("discovery sent non-GET requests: %+v", bad)
	}
	if len(srv.Requests()) == 0 {
		t.Fatal("discovery sent no requests")
	}
	return o, err
}

func kinds(actions []plan.Action) map[string]plan.Kind {
	m := map[string]plan.Kind{}
	for _, a := range actions {
		m[a.ID] = a.Kind
	}
	return m
}

func TestDiscoverFreshAccount(t *testing.T) {
	d := desired(t, "")
	o, err := discover(t, cffake.Fresh(), d)
	if err != nil {
		t.Fatal(err)
	}
	if !o.Zone.Found || o.Zone.ID != cffake.ZoneID || o.Zone.AccountID != cffake.AccountID {
		t.Fatalf("zone %+v", o.Zone)
	}
	if !o.Org.Found || o.Org.SessionDuration != "24h" {
		t.Fatalf("org %+v", o.Org)
	}
	if o.Tunnel.Found || o.IdP.Found || o.Groups[plan.GroupOfficers].Found || o.Apps[plan.AppSite].Found || o.D1.Found || o.Worker.Found {
		t.Fatalf("unrelated resources matched: %+v", o)
	}
	if r := o.Rules[plan.RuleCache]; r.Found || r.Unknown != "" {
		t.Fatalf("cache rule %+v", r)
	}
	if o.BotFightMode.On == nil || *o.BotFightMode.On {
		t.Fatalf("bot fight mode %+v", o.BotFightMode)
	}
	for id, k := range kinds(plan.Diff(d, o)) {
		if k != plan.Create && id != "zone" && id != "zero_trust" && id != "zone.bot_fight_mode" {
			t.Errorf("%s: %s, want create", id, k)
		}
	}
}

func TestDiscoverCompleteAccount(t *testing.T) {
	d := desired(t, "")
	o, err := discover(t, cffake.Complete(), d)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range plan.Diff(d, o) {
		if a.Kind == plan.Create || a.Kind == plan.Drift || a.Kind == plan.Manual {
			t.Errorf("%s: %s %v", a.ID, a.Kind, a.Detail)
		}
	}
	if o.Tunnel.ID != cffake.TunnelID || o.Apps[plan.AppSite].ID != "app-site" || o.Rules[plan.RuleCache].Index != 1 ||
		o.Worker.D1BindingID != "d1-backups" || o.ServiceTokens[plan.TokenServices].ExpiresAt != "2027-11-20T00:00:00Z" {
		t.Fatalf("observed %+v", o)
	}
}

func TestDiscoverWithPrefixIgnoresTheRealInstall(t *testing.T) {
	d := desired(t, "stg-")
	o, err := discover(t, cffake.Complete(), d)
	if err != nil {
		t.Fatal(err)
	}
	shared := map[string]bool{"zone": true, "zero_trust": true, "access.idp": true, "zone.bot_fight_mode": true,
		// Apps and rules match by hostname or expression first, so a staging install
		// needs its own hostname as well as a prefix; on the same hostname they match.
		"access.app.site": true, "access.app.agents": true, "access.app.services": true, "access.app.backups": true,
		"rule.firewall.skip": true, "rule.cache.bypass": true, "rule.config.consent": true,
		"tunnel.route": true, "dns": true, "worker.domain": true}
	for _, a := range plan.Diff(d, o) {
		if !shared[a.ID] && a.Kind != plan.Create {
			t.Errorf("%s: %s, want create (prefixed name must not match the real one)", a.ID, a.Kind)
		}
	}
}

func TestDiscoverMatchesTunnelByIDFirst(t *testing.T) {
	d := desired(t, "")
	d.TunnelID = cffake.TunnelID
	routes := cffake.Complete()
	routes["/accounts/"+cffake.AccountID+"/cfd_tunnel"] = cffake.Response{Result: []any{
		map[string]any{"id": cffake.TunnelID, "name": "renamed", "config_src": "cloudflare"},
	}}
	o, err := discover(t, routes, d)
	if err != nil {
		t.Fatal(err)
	}
	if !o.Tunnel.Found || o.Tunnel.MatchedBy != "id" || o.Tunnel.Name != "renamed" {
		t.Fatalf("tunnel %+v", o.Tunnel)
	}
}

func TestDiscoverTwoMatchesIsAnError(t *testing.T) {
	cases := map[string]struct {
		path   string
		result any
		ids    []string
	}{
		"tunnel": {"/accounts/" + cffake.AccountID + "/cfd_tunnel", []any{
			map[string]any{"id": "t-1", "name": "officer-dashboard"}, map[string]any{"id": "t-2", "name": "officer-dashboard"},
		}, []string{"t-1", "t-2"}},
		"zone": {"/zones", []any{
			map[string]any{"id": "z-1", "name": "example.org", "status": "active", "account": map[string]any{"id": "a"}},
			map[string]any{"id": "z-2", "name": "example.org", "status": "active", "account": map[string]any{"id": "b"}},
		}, []string{"z-1", "z-2"}},
		"group": {"/accounts/" + cffake.AccountID + "/access/groups", []any{
			map[string]any{"id": "g-1", "name": "Officers"}, map[string]any{"id": "g-2", "name": "Officers"},
		}, []string{"g-1", "g-2"}},
		"app by destinations": {"/accounts/" + cffake.AccountID + "/access/apps", []any{
			map[string]any{"id": "a-1", "name": "One", "destinations": []any{map[string]any{"type": "public", "uri": cffake.Host}}},
			map[string]any{"id": "a-2", "name": "Two", "self_hosted_domains": []any{cffake.Host + "/"}},
		}, []string{"a-1", "a-2"}},
		"rule by description": {"/zones/" + cffake.ZoneID + "/rulesets/phases/http_config_settings/entrypoint", map[string]any{"id": "rs", "rules": []any{
			map[string]any{"id": "r-1", "description": "Consent page: no rewriting", "expression": "true", "action": "set_config"},
			map[string]any{"id": "r-2", "description": "Consent page: no rewriting", "expression": "false", "action": "set_config"},
		}}, []string{"r-1", "r-2"}},
		"D1": {"/accounts/" + cffake.AccountID + "/d1/database", []any{
			map[string]any{"uuid": "d-1", "name": "dashboard-backups"}, map[string]any{"uuid": "d-2", "name": "dashboard-backups"},
		}, []string{"d-1", "d-2"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			routes := cffake.Complete()
			routes[tc.path] = cffake.Response{Result: tc.result}
			_, err := discover(t, routes, desired(t, ""))
			var amb *AmbiguousError
			if !errors.As(err, &amb) {
				t.Fatalf("err = %v, want AmbiguousError", err)
			}
			for _, id := range tc.ids {
				if !strings.Contains(err.Error(), id) {
					t.Fatalf("error %q does not name %s", err, id)
				}
			}
		})
	}
}

func TestDiscoverPermissionGapsAreUnknowable(t *testing.T) {
	routes := cffake.Complete()
	routes["/accounts/"+cffake.AccountID+"/access/groups"] = cffake.Response{Status: 403, Code: 10000}
	routes["/zones/"+cffake.ZoneID+"/bot_management"] = cffake.Response{Status: 403, Code: 10000}
	routes["/accounts/"+cffake.AccountID+"/workers/scripts/dashboard-backup-sink/secrets"] = cffake.Response{Status: 403, Code: 10000}
	d := desired(t, "")
	o, err := discover(t, routes, d)
	if err != nil {
		t.Fatal(err)
	}
	k := kinds(plan.Diff(d, o))
	for _, id := range []string{"access.group.officers", "access.group.launchpad", "zone.bot_fight_mode", "worker.secret"} {
		if k[id] != plan.Unknowable {
			t.Errorf("%s: %s, want unknowable", id, k[id])
		}
	}
	if !strings.Contains(o.Groups[plan.GroupOfficers].Unknown, "cannot read Access groups") {
		t.Fatalf("reason %q", o.Groups[plan.GroupOfficers].Unknown)
	}
}

func TestDiscoverServerErrorStops(t *testing.T) {
	routes := cffake.Complete()
	routes["/accounts/"+cffake.AccountID+"/d1/database"] = cffake.Response{Status: 500, Code: 1}
	_, err := discover(t, routes, desired(t, ""))
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("err = %v", err)
	}
}

func TestDiscoverZoneMissingStopsEarly(t *testing.T) {
	srv := cffake.Start(map[string]cffake.Response{"/zones": {Result: []any{}}})
	defer srv.Close()
	o, err := Discover(context.Background(), cfapi.New("t", cfapi.WithBaseURL(srv.URL)), desired(t, ""))
	if err != nil || o.Zone.Found {
		t.Fatalf("zone %+v, err %v", o.Zone, err)
	}
	// officers.example.org, then example.org; never the bare TLD.
	if n := len(srv.Requests()); n != 2 {
		t.Fatalf("requests %+v", srv.Requests())
	}
}
