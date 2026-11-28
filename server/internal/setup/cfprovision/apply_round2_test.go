package cfprovision

// Tests for the follow-up review of PR #125 at e9154b9 (N-1 to N-6), each starting from
// the reviewer's probe in .pane/prompts/review-pr125-round2/.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"officerdashboard/internal/setup/cffake"
	"officerdashboard/internal/setup/plan"
)

// damageOrg makes the organization PUT change a setting the body carried.
func damageOrg(r *rig, key string, value any) {
	r.acct.Inject(cffake.Injection{Method: "PUT", Path: acctPath + "/access/organizations", After: func() {
		m := map[string]any{}
		for k, v := range org(r) {
			m[k] = v
		}
		m[key] = value
		r.acct.Set(acctPath+"/access/organizations", cffake.Response{Result: m})
	}})
}

// N-1(a), probe Q3: a later run without the state directory must not forget the damage.
func TestEveryApplyNeedsTheStateDirectory(t *testing.T) {
	r := newRig(t, cffake.Fresh())
	dir := r.opt.StateDir
	r.opt.AllowOrgSessionWrite = true
	damageOrg(r, "strict_service_token_auth", false)
	if _, err := r.apply(context.Background()); err == nil {
		t.Fatal("run 1 survived the damage")
	}
	// Run 3 of the probe: default options and no state directory.
	r.opt.AllowOrgSessionWrite, r.opt.StateDir = false, ""
	n := len(r.acct.Writes())
	_, err := r.apply(context.Background())
	var blocked *BlockedError
	if !errors.As(err, &blocked) || !strings.Contains(err.Error(), "Options.StateDir is required") || len(r.acct.Writes()) != n {
		t.Fatalf("run without a state directory: err = %v, %d new writes", err, len(r.acct.Writes())-n)
	}
	// With the directory, default options still report the record.
	r.opt.StateDir = dir
	_, err = r.apply(context.Background())
	if !errors.As(err, &blocked) || !strings.Contains(err.Error(), "cloudflare-restore-zero-trust-organization-") {
		t.Fatalf("run with default options: err = %v", err)
	}
}

// N-1(b), probe Q9: on the live account a misbehaving organization PUT must not stop
// the run before the Access applications exist.
func TestOptInWritesRunAfterTheAccessAppsAndRules(t *testing.T) {
	r := newRig(t, cffake.Live())
	r.opt.AllowOrgSessionWrite = true
	damageOrg(r, "name", "reset")
	_, err := r.apply(context.Background())
	var ve *VerifyError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v", err)
	}
	ws := r.acct.Writes()
	if last := ws[len(ws)-1]; last.Method != http.MethodPut || !strings.HasSuffix(last.Path, "/access/organizations") {
		t.Errorf("the organization write was not last: %s %s", last.Method, last.Path)
	}
	for _, id := range []string{"access.app.site", "access.app.agents", "access.app.services", "rule.firewall.skip", "rule.cache.bypass", "rule.config.consent"} {
		if k := byID(r.preview())[id].Kind; k != plan.OK {
			t.Errorf("%s: %s, want ok before the opt-in write", id, k)
		}
	}
	// Bot Fight Mode is an opt-in write too, after everything else in the plan.
	r2 := newRig(t, cffake.Live())
	r2.opt.AllowBotFightModeOff, r2.opt.AllowOrgSessionWrite = true, true
	if _, err := r2.apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	ws = r2.acct.Writes()
	if a, b := writeIndex(ws, http.MethodPut, "/access/organizations"), writeIndex(ws, http.MethodPut, "/bot_management"); a != len(ws)-2 || b != len(ws)-1 {
		t.Errorf("opt-in writes at %d and %d of %d", a, b, len(ws))
	}
}

// N-2, probe Q6: the bypass app stored on the whole host. Discovery then finds two
// apps for the site spec; the error must still say to check and delete the new app.
func TestBypassAppAnyFailureAfterThePostSaysDeleteIt(t *testing.T) {
	r := newRig(t, cffake.Fresh())
	apps := acctPath + "/access/apps"
	var createdID string
	r.acct.Inject(cffake.Injection{Method: "POST", Path: apps, Skip: 1, After: func() {
		route, _ := r.acct.Route(apps)
		list := route.Result.([]any)
		last := list[len(list)-1].(map[string]any)
		createdID = fmt.Sprint(last["id"])
		last["destinations"] = []any{map[string]any{"type": "public", "uri": cffake.Host}}
		r.acct.Set(apps, cffake.Response{Result: list})
	}})
	_, err := r.apply(context.Background())
	if err == nil {
		t.Fatal("no error")
	}
	text := err.Error()
	want := "Access application " + createdID + " was just created and skips the sign-in"
	if !strings.HasPrefix(text, want) || !strings.Contains(text, "delete it at once") || !strings.Contains(text, "Setup cannot delete it") {
		t.Fatalf("error does not start with the advice for %s:\n%s", createdID, text)
	}
	if strings.Contains(strings.ToLower(text), "rename") {
		t.Errorf("suggests renaming:\n%s", text)
	}
	var amb *AmbiguousError
	if !errors.As(err, &amb) {
		t.Errorf("the cause is lost: %T", err)
	}
	if last := r.acct.Writes()[len(r.acct.Writes())-1]; last.Path != apps {
		t.Errorf("wrote %s after the bypass app", last.Path)
	}
}

// N-3, probe Q7: errors.As must not hand back the unscrubbed inner error.
func TestErrorsAsCannotReachUnscrubbedText(t *testing.T) {
	// Built at run time, so the source holds nothing secret-shaped for scanners.
	secret := "planted-" + strings.Repeat("value-", 3) + fmt.Sprint(len("scrub-test"))
	r := &redactor{}
	r.add(secret)
	cases := []error{
		&RejectedError{Step: "s", Method: "POST", Path: "/p", Cause: "echo " + secret},
		&UncertainError{Step: "s", Method: "POST", Path: "/p", Cause: "echo " + secret},
		&PermissionError{Permission: "x", Method: "POST", Path: "/p", Cause: "echo " + secret},
		&VerifyError{Step: "s", Expected: "x", Got: []string{"echo " + secret}},
		&ShapeError{Step: "s", Missing: secret},
		&bypassAppError{AppID: "app-1", Err: &RejectedError{Cause: "echo " + secret}},
		fmt.Errorf("wrapped: %w", &RejectedError{Cause: "echo " + secret}),
	}
	for _, in := range cases {
		err := r.scrubErr(in)
		if text := errText(err); strings.Contains(text, secret) {
			t.Errorf("%T prints the secret: %s", in, text)
		}
		for e := err; e != nil; e = errors.Unwrap(e) {
			if text := errText(e); strings.Contains(text, secret) {
				t.Errorf("%T: an unwrapped %T still holds the secret", in, e)
			}
		}
		var rej *RejectedError
		if errors.As(err, &rej) && strings.Contains(rej.Cause, secret) {
			t.Errorf("%T: errors.As hands back the raw Cause", in)
		}
		// Scrubbing keeps the type, so the command can still tell errors apart.
		if target := reflect.New(reflect.TypeOf(in)); !errors.As(err, target.Interface()) {
			t.Errorf("%T became %T", in, err)
		}
	}
	if _, ok := r.scrubErr(cases[0]).(*RejectedError); !ok {
		t.Error("a typed error lost its type")
	}
}

// N-4: the Bot Fight Mode read-modify-write body is pinned, from the live shape.
func TestLiveRequestBodiesWithBothOptionsMatchGolden(t *testing.T) {
	r := newRig(t, cffake.Live())
	r.opt.AllowOrgSessionWrite, r.opt.AllowBotFightModeOff = true, true
	out, err := r.apply(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "apply-live-requests.txt", requestsText(t, r, out, "a live-shaped apply with both opt-in writes on"))
	if !strings.Contains(requestsText(t, r, out, ""), `"fight_mode": false`) {
		t.Error("no Bot Fight Mode body")
	}
}

// N-5: the rate-limit phase cannot be read: a manual line, not a blocker.
func TestUnreadableRateLimitRulesAreAManualLine(t *testing.T) {
	routes := cffake.Live()
	routes[zonePath+"/rulesets/phases/http_ratelimit/entrypoint"] = cffake.Response{Status: 403, Code: 10000, Message: "Authentication error"}
	r := newRig(t, routes)
	if _, err := r.apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.out.String(), "manual: setup could not read the zone's rate limiting rules") {
		t.Errorf("output:\n%s", r.out.String())
	}
}

// N-6: the dry run and apply agree: an unset global session is a manual line in both.
func TestDryRunAndApplyAgreeOnTheSession(t *testing.T) {
	r := newRig(t, cffake.Fresh())
	a := byID(r.preview())["zero_trust.session"]
	if a.Kind != plan.Manual || !strings.Contains(strings.Join(a.Detail, " "), "Set your global session duration") {
		t.Fatalf("dry run: %s %v", a.Kind, a.Detail)
	}
	if _, err := r.apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.out.String(), "manual: Global session duration") {
		t.Errorf("apply output:\n%s", r.out.String())
	}
	// With the option, apply writes it, and the dry run then says ok.
	r.opt.AllowOrgSessionWrite = true
	if _, err := r.apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if k := byID(r.preview())["zero_trust.session"].Kind; k != plan.OK {
		t.Errorf("after the opt-in write: %s", k)
	}
}
