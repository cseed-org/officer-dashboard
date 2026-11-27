package cfprovision

// Tests for the findings of the PR #125 review (.pane/prompts/review-pr125.md), each
// starting from the reviewer's repro.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"

	"officerdashboard/internal/setup/cffake"
	"officerdashboard/internal/setup/plan"
)

const (
	acctPath = "/accounts/" + cffake.AccountID
	zonePath = "/zones/" + cffake.ZoneID
)

func org(r *rig) map[string]any {
	route, _ := r.acct.Route(acctPath + "/access/organizations")
	return route.Result.(map[string]any)
}

// --- F-1: the organization is not written by default; opt-in is read-modify-write. ---

func TestOrgSessionIsManualByDefault(t *testing.T) {
	r := newRig(t, cffake.Fresh())
	out, err := r.apply(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if writeIndex(r.acct.Writes(), http.MethodPut, "/access/organizations") >= 0 {
		t.Fatal("wrote the Zero Trust organization without the option")
	}
	if !strings.Contains(r.out.String(), "manual: the Zero Trust organization has no global session duration") ||
		!strings.Contains(r.out.String(), "Set your global session duration → Edit → 1 month → Save") {
		t.Errorf("no manual step:\n%s", r.out.String())
	}
	if k := byID(r.preview())["zero_trust.session"].Kind; k != plan.Create {
		t.Errorf("session %s, want create (left for the person)", k)
	}
	// Q-b: the services token follows the docs ("the longest offered").
	if out.ServicesTokenExpiresAt != "2126-11-27T00:00:00Z" {
		t.Errorf("expires_at %q", out.ServicesTokenExpiresAt)
	}
	if w := r.acct.Writes()[writeIndex(r.acct.Writes(), http.MethodPost, "/access/service_tokens")]; !strings.Contains(w.Body, `"duration":"forever"`) {
		t.Errorf("service token body %s", w.Body)
	}
}

func TestOrgSessionWriteNeedsAStateDirectory(t *testing.T) {
	r := newRig(t, cffake.Fresh())
	r.opt.AllowOrgSessionWrite = true
	_, err := r.apply(context.Background())
	var blocked *BlockedError
	if !errors.As(err, &blocked) || !strings.Contains(err.Error(), "needs a state directory") || len(r.acct.Writes()) != 0 {
		t.Fatalf("err = %v", err)
	}
}

func TestOrgSessionReadModifyWriteSurvivesReplaceSemantics(t *testing.T) {
	// The reviewer's repro: a PUT that replaces, resetting every writable field the body
	// omits. With a read-modify-write body nothing else changes.
	r := newRig(t, cffake.Fresh())
	r.acct.PutReplaces(true)
	r.opt.AllowOrgSessionWrite = true
	r.opt.StateDir = t.TempDir()
	if _, err := r.apply(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, r.out.String())
	}
	o := org(r)
	if o["strict_service_token_auth"] != true || o["is_ui_read_only"] != false || o["name"] != "club" || o["session_duration"] != "730h" {
		t.Errorf("organization after = %v", o)
	}
	if byID(r.preview())["zero_trust.session"].Kind != plan.OK {
		t.Error("session not ok")
	}
}

func TestOrgDamageIsRecordedWithOldValuesAndBlocksReruns(t *testing.T) {
	r := newRig(t, cffake.Fresh())
	r.opt.AllowOrgSessionWrite = true
	r.opt.StateDir = t.TempDir()
	// Cloudflare drops a setting the body carried: the run cannot prevent it, only record it.
	r.acct.Inject(cffake.Injection{Method: "PUT", Path: acctPath + "/access/organizations", After: func() {
		o := org(r)
		delete(o, "strict_service_token_auth")
		o["is_ui_read_only"] = true
		r.acct.Set(acctPath+"/access/organizations", cffake.Response{Result: o})
	}})
	_, err := r.apply(context.Background())
	var ve *VerifyError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v", err)
	}
	for _, want := range []string{"strict_service_token_auth: was true, now (missing)", "is_ui_read_only: was false, now true", "old values saved to"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q:\n%v", want, err)
		}
	}
	files, _ := pendingRestores(r.opt.StateDir)
	if len(files) != 1 {
		t.Fatalf("restore records %v", files)
	}
	raw, _ := os.ReadFile(files[0])
	var rec struct {
		Changed map[string]struct{ Before, After json.RawMessage }
		Before  map[string]json.RawMessage
	}
	if err := json.Unmarshal(raw, &rec); err != nil || string(rec.Changed["strict_service_token_auth"].Before) != "true" || string(rec.Before["name"]) != `"club"` {
		t.Fatalf("record %s (%v)", raw, err)
	}
	if fi, _ := os.Stat(files[0]); fi.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Errorf("record mode %v", fi.Mode())
	}
	// The re-run does not finish clean: it refuses while the record is there.
	n := len(r.acct.Writes())
	_, err = r.apply(context.Background())
	var blocked *BlockedError
	if !errors.As(err, &blocked) || !strings.Contains(err.Error(), files[0]) || len(r.acct.Writes()) != n {
		t.Fatalf("re-run: err = %v, %d new writes", err, len(r.acct.Writes())-n)
	}
	// Once the person has restored and removed the record, setup carries on.
	if err := os.Remove(files[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := r.apply(context.Background()); err != nil {
		t.Fatalf("after the record was removed: %v", err)
	}
}

func TestOrgSessionSetSinceThePreviewIsNotChanged(t *testing.T) {
	// The recheck right before the PUT (reviewer's M06): someone set the session after
	// the engine's own discovery and preflight read the organization.
	r := newRig(t, cffake.Fresh())
	r.opt.AllowOrgSessionWrite = true
	r.opt.StateDir = t.TempDir()
	set := map[string]any{}
	for k, v := range org(r) {
		set[k] = v
	}
	set["session_duration"] = "24h"
	r.acct.Inject(cffake.Injection{Method: "GET", Path: acctPath + "/access/organizations", Skip: 3, Result: set}) // the preview, the engine's discovery, its preflight
	_, err := r.apply(context.Background())
	var rej *RejectedError
	if !errors.As(err, &rej) || !strings.Contains(err.Error(), "never changes one that is set") {
		t.Fatalf("err = %v", err)
	}
	if writeIndex(r.acct.Writes(), http.MethodPut, "/access/organizations") >= 0 {
		t.Error("wrote the organization")
	}
}

// --- F-2: Bot Fight Mode, the same read-modify-write. ---

func TestBotFightModeReadModifyWrite(t *testing.T) {
	r := newRig(t, cffake.Live())
	r.acct.PutReplaces(true)
	r.opt.AllowBotFightModeOff = true
	r.opt.StateDir = t.TempDir()
	if _, err := r.apply(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, r.out.String())
	}
	route, _ := r.acct.Route(zonePath + "/bot_management")
	m := route.Result.(map[string]any)
	if m["fight_mode"] != false || m["enable_js"] != true || m["ai_bots_protection"] != "disabled" || m["ai_agents"] == nil || m["using_latest_model"] != true {
		t.Errorf("bot management after = %v", m)
	}
	w := r.acct.Writes()[writeIndex(r.acct.Writes(), http.MethodPut, "/bot_management")]
	if strings.Contains(w.Body, "using_latest_model") || !strings.Contains(w.Body, `"enable_js":true`) {
		t.Errorf("body %s: want the writable fields only", w.Body)
	}
}

func TestBotFightModeDamageIsRecorded(t *testing.T) {
	r := newRig(t, cffake.Live())
	r.opt.AllowBotFightModeOff = true
	r.opt.StateDir = t.TempDir()
	r.acct.Inject(cffake.Injection{Method: "PUT", Path: zonePath + "/bot_management", After: func() {
		route, _ := r.acct.Route(zonePath + "/bot_management")
		m := route.Result.(map[string]any)
		m["enable_js"] = false
		r.acct.Set(zonePath+"/bot_management", cffake.Response{Result: m})
	}})
	_, err := r.apply(context.Background())
	if err == nil || !strings.Contains(err.Error(), "enable_js: was true, now false") {
		t.Fatalf("err = %v", err)
	}
	if files, _ := pendingRestores(r.opt.StateDir); len(files) != 1 {
		t.Fatalf("records %v", files)
	}
}

// --- F-3 and F-6: publication, and the bypass and services apps, wait for the site app. ---

func TestPublicationWaitsForTheSiteApp(t *testing.T) {
	siteApp := func(extra map[string]any) map[string]any {
		a := map[string]any{"id": "app-site", "name": "Officer Dashboard", "type": "self_hosted", "domain": cffake.Host,
			"destinations": []any{map[string]any{"type": "public", "uri": cffake.Host}}, "session_duration": "730h",
			"allowed_idps": []any{}, "auto_redirect_to_identity": false}
		for k, v := range extra {
			a[k] = v
		}
		return a
	}
	cases := map[string]func(map[string]cffake.Response){
		// The reviewer's P4: an app on /mcp only makes the site and agents lines drift.
		"partial app on the host": func(r map[string]cffake.Response) {
			r[acctPath+"/access/apps"] = cffake.Response{Result: []any{map[string]any{"id": "app-mcp", "name": "MCP only", "type": "self_hosted",
				"domain": cffake.Host + "/mcp", "destinations": []any{map[string]any{"type": "public", "uri": cffake.Host + "/mcp"}}, "policies": []any{}}}}
		},
		// A site app with the wrong session duration: drift.
		"site app drift": func(r map[string]cffake.Response) {
			r[acctPath+"/access/apps"] = cffake.Response{Result: []any{siteApp(map[string]any{"session_duration": "24h", "policies": []any{}})}}
		},
		// A site app whose policies cannot be read: unknowable.
		"site app unknowable": func(r map[string]cffake.Response) {
			r[acctPath+"/access/apps"] = cffake.Response{Result: []any{siteApp(nil)}}
			r[acctPath+"/access/apps/app-site/policies"] = cffake.Response{Status: 403, Code: 10000, Message: "Authentication error"}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			routes := cffake.Fresh()
			mutate(routes)
			r := newRig(t, routes)
			m := byID(r.preview())
			if m["tunnel.route"].Kind != plan.Create || m["dns"].Kind != plan.Create || m["access.app.site"].Kind == plan.OK || m["access.app.site"].Kind == plan.Create {
				t.Fatalf("setup: route %s, dns %s, site %s", m["tunnel.route"].Kind, m["dns"].Kind, m["access.app.site"].Kind)
			}
			_, err := r.apply(context.Background())
			var blocked *BlockedError
			if !errors.As(err, &blocked) || len(r.acct.Writes()) != 0 {
				t.Fatalf("err = %v, writes %+v", err, r.acct.Writes())
			}
			for _, want := range []string{"route " + cffake.Host + " to http://dashboard:8090", "create the proxied CNAME"} {
				found := false
				for _, reason := range blocked.Reasons {
					found = found || strings.HasPrefix(reason, want) && strings.Contains(reason, `needs Access application "Officer Dashboard",`)
				}
				if !found {
					t.Errorf("no blocker holding %q back on the site app:\n%s", want, strings.Join(blocked.Reasons, "\n"))
				}
			}
		})
	}
}

func TestBypassAndServicesAppsNeedTheSiteApp(t *testing.T) {
	routes := cffake.Live()
	// The live account plus a site app with the wrong session duration.
	apps := routes[acctPath+"/access/apps"].Result.([]any)
	routes[acctPath+"/access/apps"] = cffake.Response{Result: append(apps, map[string]any{"id": "app-site", "name": "Officer Dashboard", "type": "self_hosted",
		"domain": cffake.Host, "destinations": []any{map[string]any{"type": "public", "uri": cffake.Host}}, "session_duration": "24h", "policies": []any{}})}
	r := newRig(t, routes)
	_, err := r.apply(context.Background())
	var blocked *BlockedError
	if !errors.As(err, &blocked) || len(r.acct.Writes()) != 0 {
		t.Fatalf("err = %v", err)
	}
	text := strings.Join(blocked.Reasons, "\n")
	for _, app := range []string{"Officer Dashboard: agents (bypass)", "Officer Dashboard: services (service token)"} {
		if !strings.Contains(text, "create Access application "+app+" with policy") {
			t.Errorf("%s is not held back:\n%s", app, text)
		}
	}
}

func TestLiveWarningNamesTheSiteStep(t *testing.T) {
	r := newRig(t, cffake.Live())
	if _, err := r.apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.out.String(), "creates its Access application at step 5") {
		t.Errorf("warning:\n%s", r.out.String())
	}
}

// --- F-4: read-backs and refusals, each made to fail. ---

func TestAppReadBackCatchesAMissingPolicy(t *testing.T) {
	r := newRig(t, cffake.Fresh())
	r.acct.Inject(cffake.Injection{Method: "POST", Path: acctPath + "/access/apps", After: func() {
		route, _ := r.acct.Route(acctPath + "/access/apps")
		list := route.Result.([]any)
		list[len(list)-1].(map[string]any)["policies"] = []any{}
	}})
	_, err := r.apply(context.Background())
	var ve *VerifyError
	if !errors.As(err, &ve) || !strings.Contains(err.Error(), "no allow policy") {
		t.Fatalf("err = %v", err)
	}
	if writeIndex(r.acct.Writes(), http.MethodPut, "/configurations") >= 0 {
		t.Error("published after a failed read-back")
	}
}

func TestBypassAppReadBackTellsToDeleteAWiderApp(t *testing.T) {
	r := newRig(t, cffake.Fresh())
	// The second app create is the bypass app; Cloudflare stores it wider than asked.
	r.acct.Inject(cffake.Injection{Method: "POST", Path: acctPath + "/access/apps", Skip: 1, After: func() {
		route, _ := r.acct.Route(acctPath + "/access/apps")
		list := route.Result.([]any)
		a := list[len(list)-1].(map[string]any)
		a["destinations"] = append(a["destinations"].([]any), map[string]any{"type": "public", "uri": cffake.Host + "/oauth"})
	}})
	_, err := r.apply(context.Background())
	var ve *VerifyError
	if !errors.As(err, &ve) || !strings.Contains(err.Error(), "delete it at once") || !strings.Contains(err.Error(), "Setup cannot delete it") {
		t.Fatalf("err = %v", err)
	}
}

// usedTunnel is a fixture tunnel that has run before, with one other route.
func usedTunnel(routes map[string]cffake.Response, extra map[string]any) {
	routes[acctPath+"/cfd_tunnel"] = cffake.Response{Result: []any{map[string]any{"id": cffake.TunnelID, "name": "officer-dashboard", "config_src": "cloudflare",
		"status": "healthy", "conns_active_at": "2026-11-01T00:00:00Z"}}}
	cfg := map[string]any{"ingress": []any{map[string]any{"hostname": "wiki.example.org", "service": "http://wiki:80"}, map[string]any{"service": "http_status:404"}}}
	for k, v := range extra {
		cfg[k] = v
	}
	routes[acctPath+"/cfd_tunnel/"+cffake.TunnelID+"/configurations"] = cffake.Response{Result: map[string]any{"tunnel_id": cffake.TunnelID, "config": cfg}}
}

func setConfig(r *rig, mutate func(cfg map[string]any)) func() {
	return func() {
		path := acctPath + "/cfd_tunnel/" + cffake.TunnelID + "/configurations"
		route, _ := r.acct.Route(path)
		res := route.Result.(map[string]any)
		cfg := map[string]any{}
		for k, v := range res["config"].(map[string]any) {
			cfg[k] = v
		}
		mutate(cfg)
		r.acct.Set(path, cffake.Response{Result: map[string]any{"tunnel_id": cffake.TunnelID, "config": cfg}})
	}
}

func TestRouteReadBackFailures(t *testing.T) {
	cases := map[string]struct {
		mutate func(cfg map[string]any)
		want   string
	}{
		"catch-all lost": {func(cfg map[string]any) {
			in := cfg["ingress"].([]any)
			cfg["ingress"] = in[:len(in)-1]
		}, "the previous routes in order"},
		"rules reordered": {func(cfg map[string]any) {
			in := cfg["ingress"].([]any)
			cfg["ingress"] = []any{in[1], in[0], in[2]}
		}, "the previous routes in order"},
		"config key dropped": {func(cfg map[string]any) { delete(cfg, "warp-routing") }, `config.warp-routing: was {"enabled":true}, now (missing)`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			routes := cffake.Fresh()
			usedTunnel(routes, map[string]any{"warp-routing": map[string]any{"enabled": true}})
			r := newRig(t, routes)
			r.acct.Inject(cffake.Injection{Method: "PUT", Path: acctPath + "/cfd_tunnel/" + cffake.TunnelID + "/configurations", After: setConfig(r, c.mutate)})
			_, err := r.apply(context.Background())
			var ve *VerifyError
			if !errors.As(err, &ve) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v", err)
			}
			if writeIndex(r.acct.Writes(), http.MethodPost, "/dns_records") >= 0 {
				t.Error("created the DNS record after a failed route read-back")
			}
		})
	}
}

func TestRouteReadBackAcceptsAKeyCloudflareAdds(t *testing.T) {
	// F-5: a fresh tunnel's configuration comes back with a warp-routing default.
	r := newRig(t, cffake.Fresh())
	added := false
	// The tunnel ID is not known in advance, so add the key on whichever config is written.
	r.acct.OnWrite(func(req cffake.Request) {
		if req.Method != http.MethodPut || !strings.HasSuffix(req.Path, "/configurations") {
			return
		}
		route, _ := r.acct.Route(req.Path)
		res := route.Result.(map[string]any)
		cfg := res["config"].(map[string]any)
		cfg["warp-routing"] = map[string]any{"enabled": false}
		added = true
	})
	if _, err := r.apply(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, r.out.String())
	}
	if !added || !strings.Contains(r.out.String(), `note: Cloudflare now also returns config.warp-routing = {"enabled":false}`) {
		t.Errorf("output:\n%s", r.out.String())
	}
	requireAllOK(t, r.preview())
}

func TestRouteRefusesWhenTheTunnelAlreadyRoutesTheHost(t *testing.T) {
	routes := cffake.Fresh()
	usedTunnel(routes, nil)
	r := newRig(t, routes)
	// The host's route appears while setup is busy with earlier steps.
	r.acct.Inject(cffake.Injection{Method: "POST", Path: zonePath + "/rulesets", After: setConfig(r, func(cfg map[string]any) {
		in := cfg["ingress"].([]any)
		cfg["ingress"] = []any{in[0], map[string]any{"hostname": cffake.Host, "service": "http://elsewhere:80"}, in[1]}
	})})
	_, err := r.apply(context.Background())
	var rej *RejectedError
	if !errors.As(err, &rej) || !strings.Contains(err.Error(), "already routes "+cffake.Host) {
		t.Fatalf("err = %v", err)
	}
	if writeIndex(r.acct.Writes(), http.MethodPut, "/configurations") >= 0 {
		t.Error("replaced the configuration anyway")
	}
}

func TestLeftoverPolicyIsReusedOnlyWhenItMatchesExactly(t *testing.T) {
	bypass := func(extra map[string]any) map[string]any {
		p := map[string]any{"id": "pol-left", "name": "Everyone (bypass)", "decision": "bypass", "include": []any{map[string]any{"everyone": map[string]any{}}},
			"exclude": []any{}, "require": []any{}, "reusable": true, "app_count": 0}
		for k, v := range extra {
			p[k] = v
		}
		return p
	}
	t.Run("exact and unused: reused", func(t *testing.T) {
		routes := cffake.Fresh()
		routes[acctPath+"/access/policies"] = cffake.Response{Result: []any{bypass(nil)}}
		r := newRig(t, routes)
		if _, err := r.apply(context.Background()); err != nil {
			t.Fatal(err)
		}
		if n := count(r.acct, acctPath+"/access/policies", "name", "Everyone (bypass)"); n != 1 {
			t.Errorf("%d bypass policies", n)
		}
	})
	t.Run("with a require rule: refused", func(t *testing.T) {
		routes := cffake.Fresh()
		routes[acctPath+"/access/policies"] = cffake.Response{Result: []any{bypass(map[string]any{"require": []any{map[string]any{"email_domain": map[string]any{"domain": "example.org"}}}})}}
		r := newRig(t, routes)
		_, err := r.apply(context.Background())
		var blocked *BlockedError
		if !errors.As(err, &blocked) || !strings.Contains(err.Error(), "already exists with other rules") || len(r.acct.Writes()) != 0 {
			t.Fatalf("err = %v", err)
		}
	})
}

// --- Q-d: rate limiting rules stay a manual step. ---

func TestRateLimitRulesAreAManualStep(t *testing.T) {
	r := newRig(t, cffake.Live())
	if _, err := r.apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.out.String(), `manual: the zone has 1 rate limiting rules. docs/deploy/cloudflare.md step 6.2 says the skip rule should also tick "All rate limiting rules"`) {
		t.Errorf("output:\n%s", r.out.String())
	}
	fresh := newRig(t, cffake.Fresh())
	if _, err := fresh.apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fresh.out.String(), "rate limiting") {
		t.Errorf("fresh zone has no rate limiting rules:\n%s", fresh.out.String())
	}
}

// --- F-8: scrubbing keeps the error's type and prints only the clean text. ---

func TestScrubbedErrorKeepsItsType(t *testing.T) {
	const secret = "FAKE-CLIENT-SECRET-99-0123456789abcdef"
	r := &redactor{}
	r.add(secret)
	err := r.scrubErr(&RejectedError{Step: "s", Method: "POST", Path: "/p", Cause: "Cloudflare echoed " + secret})
	var rej *RejectedError
	if !errors.As(err, &rej) {
		t.Fatalf("type lost: %T", err)
	}
	if text := errText(err); strings.Contains(text, secret) || !strings.Contains(text, "[redacted]") {
		t.Fatalf("printed %q", text)
	}
}
