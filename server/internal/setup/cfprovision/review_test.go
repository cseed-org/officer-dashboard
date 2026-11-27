package cfprovision

// Regression tests from the review of PR #124 (findings F-1 to F-10). Each started as the
// reviewer's repro, failing on 442676f, and now states the expected outcome.

import (
	"errors"
	"strings"
	"testing"

	"officerdashboard/internal/setup/cffake"
	"officerdashboard/internal/setup/plan"
)

const acctP = "/accounts/" + cffake.AccountID

func action(t *testing.T, d plan.Desired, o plan.Observed, id string) plan.Action {
	t.Helper()
	for _, a := range plan.Diff(d, o) {
		if a.ID == id {
			return a
		}
	}
	t.Fatalf("no action %s", id)
	return plan.Action{}
}

func expect(t *testing.T, a plan.Action, kind plan.Kind, detail string) {
	t.Helper()
	joined := strings.Join(a.Detail, "\n")
	if a.Kind != kind || (detail != "" && !strings.Contains(joined, detail)) {
		t.Errorf("%s: kind %s, detail %q; want %s with %q", a.ID, a.Kind, a.Detail, kind, detail)
	}
}

func pubDest(uris ...string) []any {
	out := []any{}
	for _, u := range uris {
		out = append(out, map[string]any{"type": "public", "uri": u})
	}
	return out
}

func siteApp(policies []any, uris ...string) map[string]any {
	return map[string]any{"id": "app-site", "name": "Officer Dashboard", "type": "self_hosted", "destinations": pubDest(uris...),
		"session_duration": "730h", "allowed_idps": []any{"idp-otp"}, "auto_redirect_to_identity": true, "policies": policies}
}

func pol(id, decision string, include ...any) map[string]any {
	return map[string]any{"id": id, "name": id, "decision": decision, "include": include, "exclude": []any{}, "require": []any{}}
}

var everyone = map[string]any{"everyone": map[string]any{}}

func grp(id string) any { return map[string]any{"group": map[string]any{"id": id}} }

func replaceSiteApp(routes map[string]cffake.Response, app map[string]any) {
	apps := append([]any(nil), routes[acctP+"/access/apps"].Result.([]any)...)
	for i, a := range apps {
		if a.(map[string]any)["id"] == "app-site" {
			apps[i] = app
		}
	}
	routes[acctP+"/access/apps"] = cffake.Response{Result: apps}
}

// F-1: an extra public policy on the whole-site app must not be ok.
func TestReviewExtraPublicPolicyIsDrift(t *testing.T) {
	for name, extra := range map[string]map[string]any{
		"bypass everyone": pol("p-open", "bypass", everyone),
		"allow everyone":  pol("p-open", "allow", everyone),
	} {
		t.Run(name, func(t *testing.T) {
			routes := cffake.Complete()
			replaceSiteApp(routes, siteApp([]any{pol("p-site", "allow", grp("g-officers"), grp("g-launchpad")), extra}, cffake.Host))
			d := desired(t, "")
			o, err := discover(t, routes, d)
			if err != nil {
				t.Fatal(err)
			}
			expect(t, action(t, d, o, "access.app.site"), plan.Drift, `extra policy "p-open"`)
			expect(t, action(t, d, o, "access.app.site"), plan.Drift, "Everyone")
		})
	}
}

// F-2: the host's tunnel ID is not in the account; another tunnel matched by name.
func TestReviewTunnelIDMissIsDrift(t *testing.T) {
	d := desired(t, "")
	d.TunnelID = "11111111-2222-3333-4444-555555555555"
	o, err := discover(t, cffake.Complete(), d)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, action(t, d, o, "tunnel"), plan.Drift, "11111111-2222-3333-4444-555555555555")
}

// F-3: a renamed tunnel is found through the hostname's CNAME target.
func TestReviewRenamedTunnelMatchedByCNAME(t *testing.T) {
	routes := cffake.Complete()
	routes[acctP+"/cfd_tunnel"] = cffake.Response{Result: []any{map[string]any{"id": cffake.TunnelID, "name": "club-tunnel", "status": "healthy", "config_src": "cloudflare", "deleted_at": nil}}}
	d := desired(t, "")
	o, err := discover(t, routes, d)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, action(t, d, o, "tunnel"), plan.OK, `named "club-tunnel" (matched by the hostname's CNAME record)`)
	expect(t, action(t, d, o, "tunnel.route"), plan.OK, "")
	expect(t, action(t, d, o, "dns"), plan.OK, "")
}

// F-3: the keys disagree: the CNAME points at one tunnel, another carries the doc name.
func TestReviewTunnelKeysDisagree(t *testing.T) {
	other := "22222222-3333-4444-5555-666666666666"
	routes := cffake.Complete()
	routes[acctP+"/cfd_tunnel"] = cffake.Response{Result: []any{
		map[string]any{"id": cffake.TunnelID, "name": "club-tunnel", "status": "healthy", "config_src": "cloudflare"},
		map[string]any{"id": other, "name": "officer-dashboard", "status": "inactive", "config_src": "cloudflare"},
	}}
	d := desired(t, "")
	o, err := discover(t, routes, d)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, action(t, d, o, "tunnel"), plan.Drift, "tunnel "+other+" is named \"officer-dashboard\"")
	if o.Tunnel.ID != cffake.TunnelID {
		t.Fatalf("matched %s, want the CNAME target %s", o.Tunnel.ID, cffake.TunnelID)
	}

	// The host's token names one tunnel, the CNAME another: the token wins, and the
	// CNAME disagreement is drift.
	d.TunnelID = other
	o, err = discover(t, routes, d)
	if err != nil {
		t.Fatal(err)
	}
	if o.Tunnel.ID != other {
		t.Fatalf("matched %s, want the token's %s", o.Tunnel.ID, other)
	}
	expect(t, action(t, d, o, "tunnel"), plan.Drift, "the hostname's CNAME points at tunnel "+cffake.TunnelID)
}

// F-5: only a 404 with code 10003 means an empty phase.
func TestReviewOther404IsUnknowable(t *testing.T) {
	routes := cffake.Complete()
	routes["/zones/"+cffake.ZoneID+"/rulesets/phases/http_config_settings/entrypoint"] = cffake.Response{Status: 404, Code: 7003, Message: "Could not route"}
	d := desired(t, "")
	o, err := discover(t, routes, d)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, action(t, d, o, "rule.config.consent"), plan.Unknowable, "7003")
}

// F-6: an app covering the hostname and more is that app, not a missing one.
func TestReviewAppCoveringHostPlusMoreIsDrift(t *testing.T) {
	routes := cffake.Complete()
	s := siteApp([]any{pol("p-site", "allow", grp("g-officers"), grp("g-launchpad"))}, cffake.Host, "status.example.org")
	s["name"] = "Club dashboards"
	replaceSiteApp(routes, s)
	d := desired(t, "")
	o, err := discover(t, routes, d)
	if err != nil {
		t.Fatal(err)
	}
	a := action(t, d, o, "access.app.site")
	expect(t, a, plan.Drift, `"Club dashboards" already covers officers.example.org along with status.example.org`)
	if strings.Contains(strings.Join(a.Detail, " "), "WARNING") {
		t.Errorf("false WARNING: %v", a.Detail)
	}
}

// F-6: the WARNING asks whether any app covers the host, whatever its name.
func TestReviewWarningOnlyWhenNoAppCoversHost(t *testing.T) {
	routes := cffake.Live()
	d := desired(t, "")
	o, err := discover(t, routes, d)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, action(t, d, o, "access.app.site"), plan.Create, "WARNING")

	// A differently shaped app on the bare host still protects it: no WARNING.
	apps := append([]any(nil), routes[acctP+"/access/apps"].Result.([]any)...)
	apps = append(apps, map[string]any{"id": "app-guard", "name": "Guard", "destinations": pubDest(cffake.Host, cffake.Host+"/extra"), "policies": []any{}})
	routes[acctP+"/access/apps"] = cffake.Response{Result: apps}
	o, err = discover(t, routes, d)
	if err != nil {
		t.Fatal(err)
	}
	if a := action(t, d, o, "access.app.site"); strings.Contains(strings.Join(a.Detail, " "), "WARNING") {
		t.Errorf("WARNING although app-guard covers the host: %v", a.Detail)
	}
}

// F-7: a permission gap on a dependency is unknowable, not drift.
func TestReviewDependencyGapIsUnknowable(t *testing.T) {
	routes := cffake.Complete()
	routes[acctP+"/cfd_tunnel"] = cffake.Response{Status: 403, Code: 10000}
	d := desired(t, "")
	o, err := discover(t, routes, d)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, action(t, d, o, "dns"), plan.Unknowable, "tunnel")

	routes = cffake.Complete()
	routes[acctP+"/d1/database"] = cffake.Response{Status: 403, Code: 10000}
	o, err = discover(t, routes, d)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, action(t, d, o, "worker"), plan.Unknowable, "D1")
}

// F-8: one endpoint failing with a 5xx marks that resource unknowable; the rest still report.
func TestReviewOne5xxIsUnknowableNotFatal(t *testing.T) {
	routes := cffake.Complete()
	routes[acctP+"/workers/scripts/dashboard-backup-sink/subdomain"] = cffake.Response{Status: 503, Code: 1}
	d := desired(t, "")
	o, err := discover(t, routes, d)
	if err != nil {
		t.Fatalf("err = %v, want a report", err)
	}
	expect(t, action(t, d, o, "worker"), plan.Unknowable, "HTTP 503")
	expect(t, action(t, d, o, "d1"), plan.OK, "")
}

// F-9: require or exclude rules on a policy are named in the detail.
func TestReviewPolicyConditionsAreNoted(t *testing.T) {
	routes := cffake.Complete()
	p := pol("p-site", "allow", grp("g-officers"), grp("g-launchpad"))
	p["require"] = []any{map[string]any{"email_domain": map[string]any{"domain": "example.org"}}}
	replaceSiteApp(routes, siteApp([]any{p}, cffake.Host))
	d := desired(t, "")
	o, err := discover(t, routes, d)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, action(t, d, o, "access.app.site"), plan.OK, "require")
}

// F-10: a prefixed run on the real install's hostname must say it matched the real install.
func TestReviewPrefixOnRealHostnameIsFlagged(t *testing.T) {
	d := desired(t, "stg-")
	o, err := discover(t, cffake.Complete(), d)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"access.app.site", "rule.firewall.skip"} {
		expect(t, action(t, d, o, id), plan.Drift, `lacks the name prefix "stg-"`)
	}
	if !plan.PrefixConflict(d, plan.Diff(d, o)) {
		t.Fatal("PrefixConflict = false")
	}
}

// Follow-up review at 7f28920 (findings F-12 to F-16).

const otherTunnel = "99999999-8888-7777-6666-555555555555"

func cname(target string) cffake.Response {
	return cffake.Response{Result: []any{map[string]any{"id": "dns-1", "name": cffake.Host, "type": "CNAME", "content": target, "proxied": true}}}
}

// F-12: the hostname's CNAME names a tunnel that is not in the account.
func TestFollowupCNAMEToMissingTunnelIsDrift(t *testing.T) {
	routes := cffake.Complete()
	routes["/zones/"+cffake.ZoneID+"/dns_records"] = cname(otherTunnel + ".cfargotunnel.com")
	d := desired(t, "")
	o, err := discover(t, routes, d)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, action(t, d, o, "tunnel"), plan.Drift, "the hostname's CNAME points at tunnel "+otherTunnel+", which is not in this account")
}

// F-13: a second app covering the dashboard host is named, and its public policy flagged.
func TestFollowupSecondAppCoveringHostIsDrift(t *testing.T) {
	routes := cffake.Complete()
	apps := append([]any(nil), routes[acctP+"/access/apps"].Result.([]any)...)
	open := siteApp([]any{pol("p-open", "bypass", everyone)}, cffake.Host, "status.example.org")
	open["id"], open["name"] = "app-open", "Status pages"
	routes[acctP+"/access/apps"] = cffake.Response{Result: append(apps, open)}
	d := desired(t, "")
	o, err := discover(t, routes, d)
	if err != nil {
		t.Fatal(err)
	}
	a := action(t, d, o, "access.app.site")
	expect(t, a, plan.Drift, `application "Status pages" (app-open) also covers officers.example.org`)
	expect(t, a, plan.Drift, `its policy "p-open" lets Everyone through`)
}

// F-14: duplicate doc names do not abort when the token ID (or CNAME) already decided.
func TestFollowupDuplicateNamesAfterIDMatchAreDrift(t *testing.T) {
	routes := cffake.Complete()
	routes[acctP+"/cfd_tunnel"] = cffake.Response{Result: []any{
		map[string]any{"id": cffake.TunnelID, "name": "officer-dashboard", "status": "healthy", "config_src": "cloudflare"},
		map[string]any{"id": otherTunnel, "name": "officer-dashboard", "status": "inactive", "config_src": "cloudflare"},
	}}
	d := desired(t, "")
	d.TunnelID = cffake.TunnelID
	o, err := discover(t, routes, d)
	if err != nil {
		t.Fatalf("err = %v, want a report", err)
	}
	if o.Tunnel.ID != cffake.TunnelID {
		t.Fatalf("matched %s", o.Tunnel.ID)
	}
	expect(t, action(t, d, o, "tunnel"), plan.Drift, "tunnel "+otherTunnel+` is named "officer-dashboard"`)

	// With no ID and no CNAME, two same-named tunnels are still ambiguous.
	routes["/zones/"+cffake.ZoneID+"/dns_records"] = cffake.Response{Result: []any{}}
	d.TunnelID = ""
	_, err = discover(t, routes, d)
	var amb *AmbiguousError
	if !errors.As(err, &amb) {
		t.Fatalf("err = %v, want AmbiguousError", err)
	}
}

// F-15: with DNS unreadable, a tunnel no key found is unknowable, not create.
func TestFollowupDNSFailureMakesTunnelUnknowable(t *testing.T) {
	routes := cffake.Complete()
	routes["/zones/"+cffake.ZoneID+"/dns_records"] = cffake.Response{Status: 503, Code: 1}
	routes[acctP+"/cfd_tunnel"] = cffake.Response{Result: []any{map[string]any{"id": cffake.TunnelID, "name": "club-tunnel", "status": "healthy", "config_src": "cloudflare"}}}
	d := desired(t, "")
	o, err := discover(t, routes, d)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, action(t, d, o, "tunnel"), plan.Unknowable, "DNS record could not be read")
}

// F-16: a prefix the real names already start with must not pass the real tunnel.
func TestFollowupPrefixThatPrefixesRealNames(t *testing.T) {
	d := desired(t, "officer-")
	o, err := discover(t, cffake.Complete(), d)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, action(t, d, o, "tunnel"), plan.Drift, `lacks the name prefix "officer-"`)
}
