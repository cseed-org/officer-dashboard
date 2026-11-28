package cfprovision

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	"officerdashboard/internal/setup/cfapi"
	"officerdashboard/internal/setup/cfprovision/internal/cfwrite"
	"officerdashboard/internal/setup/plan"
)

// Engine fills the gaps a dry run reports: it runs only the plan's create actions, in an
// order that never publishes the dashboard before its Access applications exist, and it
// reads every write back through Discover before the next one.
//
// No create has been tried against real Cloudflare: the request bodies follow the API
// reference pages quoted beside each one, and the first real run is the proof. So the
// engine refuses to start on anything it does not understand, stops at the first
// surprise, and is safe to run again: every run discovers the account first and matches
// what an earlier, interrupted run created.
type Engine struct {
	d     plan.Desired
	read  cfapi.Reader
	write *cfwrite.Client
	sink  SecretSink
	out   *redactor
	opt   Options

	o        plan.Observed // the account as last read
	created  map[string]bool
	outputs  Outputs
	readOpts []cfapi.Option
}

// Outputs are what an apply run produced. They never hold a secret.
type Outputs struct {
	TunnelID    string `json:"tunnel_id,omitempty"`
	TeamDomain  string `json:"team_domain,omitempty"`
	SiteAUD     string `json:"site_aud,omitempty"`
	ServicesAUD string `json:"services_aud,omitempty"`
	// ServicesTokenExpiresAt is the services token's expires_at as Cloudflare lists it.
	ServicesTokenExpiresAt string `json:"services_token_expires_at,omitempty"`
	// Done lists each completed step and the ID it created or fetched for.
	Done []Done `json:"done"`
	// Writes lists every write sent, with the API reference page its body follows.
	Writes []Write `json:"writes"`
	// Final is the plan after the run (empty if the run stopped before it).
	Final []plan.Action `json:"final,omitempty"`
}

type Done struct {
	Step string `json:"step"`
	ID   string `json:"id,omitempty"`
}

type Write struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Ref    string `json:"ref"`
}

// Option configures an Engine.
type Option func(*config)

type config struct {
	baseURL string
	retry   []time.Duration
}

// WithAPIBase points both clients at another API root. For tests: the command never
// exposes it, so the token only ever goes to Cloudflare.
func WithAPIBase(u string) Option { return func(c *config) { c.baseURL = u } }

// WithReadRetry sets the read client's retry waits (tests use none).
func WithReadRetry(waits ...time.Duration) Option {
	return func(c *config) { c.retry = append([]time.Duration{}, waits...) }
}

// NewEngine returns an engine for one apply run. out receives progress lines; any
// secret the run obtains is removed from them.
func NewEngine(token string, d plan.Desired, sink SecretSink, out io.Writer, opt Options, opts ...Option) *Engine {
	var c config
	for _, o := range opts {
		o(&c)
	}
	var ro []cfapi.Option
	var wo []cfwrite.Option
	if c.baseURL != "" {
		ro = append(ro, cfapi.WithBaseURL(c.baseURL))
		wo = append(wo, cfwrite.WithBaseURL(c.baseURL))
	}
	if c.retry != nil {
		ro = append(ro, cfapi.WithRetry(c.retry...))
	}
	if opt.ServiceTokenDuration == "" {
		opt.ServiceTokenDuration = "forever"
	}
	return &Engine{d: d, read: cfapi.New(token, ro...), write: cfwrite.New(token, wo...), sink: sink,
		out: &redactor{w: out}, opt: opt, created: map[string]bool{}}
}

// Apply runs the create actions of actions, the plan the person reviewed. It discovers
// the account again first and stops if anything changed. It returns what it did even
// when it stops with an error; a run stopped by any error is safe to start again.
//
// The returned error, and every error it wraps, is scrubbed of the secrets the run saw.
// Print the returned error itself: it carries the whole explanation.
func (e *Engine) Apply(ctx context.Context, actions []plan.Action) (Outputs, error) {
	out, err := e.apply(ctx, actions, nil)
	return out, e.out.scrubErr(err)
}

// Reviewed is a plan as Preview computed it: the plan lines and the apply steps the
// person was shown before they confirmed.
type Reviewed struct {
	Actions []plan.Action
	Plan    ApplyPlan
}

// Preview reads the account and the host's secret files and returns what Apply would do
// now, blockers included. It writes nothing and prints nothing.
func (e *Engine) Preview(ctx context.Context) (Reviewed, error) {
	if err := e.discover(ctx); err != nil {
		return Reviewed{}, e.out.scrubErr(err)
	}
	fresh := plan.Diff(e.d, e.o)
	facts, err := e.facts(ctx, fresh)
	if err != nil {
		return Reviewed{Actions: fresh}, e.out.scrubErr(err)
	}
	ap := PlanApply(e.d, e.o, fresh, e.opt, facts)
	ap.Blockers = append(ap.Blockers, e.preflight(ctx, ap)...)
	return Reviewed{Actions: fresh, Plan: ap}, nil
}

// ApplyReviewed is Apply for a plan the person confirmed after Preview. Besides the plan
// lines, the steps and warnings computed on the fresh discovery must be the ones they
// were shown, or the run stops before the first write. It does not print the plan again.
func (e *Engine) ApplyReviewed(ctx context.Context, r Reviewed) (Outputs, error) {
	out, err := e.apply(ctx, r.Actions, &r.Plan)
	return out, e.out.scrubErr(err)
}

func (e *Engine) apply(ctx context.Context, actions []plan.Action, reviewed *ApplyPlan) (Outputs, error) {
	if err := e.discover(ctx); err != nil {
		return e.outputs, err
	}
	fresh := plan.Diff(e.d, e.o)
	if changes := planChanges(actions, fresh); len(changes) > 0 {
		return e.outputs, &PlanChangedError{Changes: changes}
	}
	facts, err := e.facts(ctx, fresh)
	if err != nil {
		return e.outputs, err
	}
	ap := PlanApply(e.d, e.o, fresh, e.opt, facts)
	ap.Blockers = append(ap.Blockers, e.preflight(ctx, ap)...)
	if reviewed != nil {
		if changes := stepChanges(*reviewed, ap); len(changes) > 0 {
			return e.outputs, &PlanChangedError{Changes: changes}
		}
		fmt.Fprintln(e.out, "Cloudflare still matches the plan above.")
	} else if err := ap.WriteText(e.out); err != nil {
		return e.outputs, err
	}
	if len(ap.Blockers) > 0 {
		return e.outputs, &BlockedError{Reasons: ap.Blockers}
	}
	for i, s := range ap.Steps {
		fmt.Fprintf(e.out, "\n[%d/%d] %s\n", i+1, len(ap.Steps), s.Title)
		id, err := e.run(ctx, s)
		if err != nil {
			return e.outputs, err
		}
		e.outputs.Done = append(e.outputs.Done, Done{Step: s.Title, ID: id})
	}
	e.outputs.Final = plan.Diff(e.d, e.o)
	e.collect()
	fmt.Fprintf(e.out, "\nDone: %d steps, %d writes.\n", len(ap.Steps), len(e.outputs.Writes))
	return e.outputs, nil
}

func (e *Engine) discover(ctx context.Context) error {
	o, err := Discover(ctx, e.read, e.d)
	if err != nil {
		if cfapi.IsInvalidToken(err) {
			return errors.New("Cloudflare does not accept this API token (code 1000): check it was copied whole and is still active")
		}
		return fmt.Errorf("reading Cloudflare failed: %w", err)
	}
	e.o = o
	return nil
}

func (e *Engine) collect() {
	e.outputs.TeamDomain = e.o.Org.AuthDomain
	if e.o.Tunnel.Found {
		e.outputs.TunnelID = e.o.Tunnel.ID
	}
	e.outputs.SiteAUD = e.o.Apps[plan.AppSite].AUD
	e.outputs.ServicesAUD = e.o.Apps[plan.AppServices].AUD
	e.outputs.ServicesTokenExpiresAt = e.o.ServiceTokens[plan.TokenServices].ExpiresAt
}

// planChanges compares the reviewed plan with a fresh one by ID, kind and existing ID.
func planChanges(reviewed, fresh []plan.Action) []string {
	type key struct {
		kind plan.Kind
		id   string
	}
	f := map[string]key{}
	for _, a := range fresh {
		f[a.ID] = key{a.Kind, a.ExistingID}
	}
	var out []string
	seen := map[string]bool{}
	for _, a := range reviewed {
		seen[a.ID] = true
		got, ok := f[a.ID]
		switch {
		case !ok:
			out = append(out, fmt.Sprintf("%s: no longer in the plan", a.ID))
		case got.kind != a.Kind || got.id != a.ExistingID:
			out = append(out, fmt.Sprintf("%s: was %s %s, now %s %s", a.ID, a.Kind, a.ExistingID, got.kind, got.id))
		}
	}
	for _, a := range fresh {
		if !seen[a.ID] {
			out = append(out, fmt.Sprintf("%s: new in the plan (%s)", a.ID, a.Kind))
		}
	}
	if len(reviewed) == 0 {
		out = append(out, "no reviewed plan was given")
	}
	return out
}

// stepChanges compares the steps and warnings the person was shown with fresh ones.
func stepChanges(reviewed, fresh ApplyPlan) []string {
	var out []string
	for i := 0; i < max(len(reviewed.Steps), len(fresh.Steps)); i++ {
		switch {
		case i >= len(fresh.Steps):
			out = append(out, fmt.Sprintf("step %d (%s) is no longer needed", i+1, reviewed.Steps[i].Title))
		case i >= len(reviewed.Steps):
			out = append(out, fmt.Sprintf("step %d (%s) is new", i+1, fresh.Steps[i].Title))
		case !reflect.DeepEqual(reviewed.Steps[i], fresh.Steps[i]):
			out = append(out, fmt.Sprintf("step %d was %q, now %q", i+1, reviewed.Steps[i].Title, fresh.Steps[i].Title))
		}
	}
	if !slices.Equal(reviewed.Warnings, fresh.Warnings) {
		out = append(out, "the warnings changed; now: "+strings.Join(fresh.Warnings, "; "))
	}
	return out
}

func (e *Engine) facts(ctx context.Context, actions []plan.Action) (Facts, error) {
	var f Facts
	var err error
	if f.TunnelTokenKept, err = e.sink.Has(ctx, SecretTunnelToken); err != nil {
		return f, fmt.Errorf("cannot check the host's secret files: %w", err)
	}
	if f.ServicesSecretKept, err = e.sink.Has(ctx, SecretServicesToken); err != nil {
		return f, fmt.Errorf("cannot check the host's secret files: %w", err)
	}
	if f.PendingRestores, err = pendingRestores(e.opt.StateDir); err != nil {
		return f, err
	}
	f.RateLimitRules, f.RateLimitUnknown = e.rateLimitRules(ctx)
	if e.opt.AllowBotFightModeOff && e.o.BotFightMode.On != nil && *e.o.BotFightMode.On {
		// GET /zones/{zone_id}/dns_records: every record name, to show what else the
		// zone-wide switch affects.
		// https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/list/
		type rec struct{ Name string }
		recs, err := cfapi.ListAll[rec](ctx, e.read, e.zonePath("/dns_records"), nil)
		if err != nil {
			return f, e.readErr("DNS Read", "GET", e.zonePath("/dns_records"), err)
		}
		seen := map[string]bool{}
		for _, r := range recs {
			n := strings.ToLower(strings.TrimSuffix(r.Name, "."))
			if n != e.d.Hostname && !seen[n] {
				seen[n] = true
				f.OtherHostnames = append(f.OtherHostnames, n)
			}
		}
		sort.Strings(f.OtherHostnames)
	}
	return f, nil
}

func (e *Engine) accountPath(p string) string {
	return "/accounts/" + url.PathEscape(e.o.Zone.AccountID) + p
}
func (e *Engine) zonePath(p string) string { return "/zones/" + url.PathEscape(e.o.Zone.ID) + p }

// preflight reads, before any write, everything the steps will read later, so a missing
// read permission or an unexpected state stops the run while nothing has changed.
func (e *Engine) preflight(ctx context.Context, ap ApplyPlan) []string {
	var out []string
	for _, s := range ap.Steps {
		switch s.Kind {
		case StepApp:
			spec := e.app(s.Key)
			if _, problem, err := e.reusablePolicy(ctx, spec); err != nil {
				out = append(out, err.Error())
			} else if problem != "" {
				out = append(out, problem)
			}
		case StepRoute:
			if !e.o.Tunnel.Found {
				continue // created by an earlier step: it has no routes yet
			}
			if _, err := e.tunnelConfig(ctx); err != nil {
				out = append(out, err.Error())
			}
		case StepSession:
			if _, err := e.rawObject(ctx, "Access: Organizations Read", e.accountPath("/access/organizations")); err != nil {
				out = append(out, err.Error())
			}
		case StepBotFightMode:
			if _, err := e.rawObject(ctx, "Bot Management Read", e.zonePath("/bot_management")); err != nil {
				out = append(out, err.Error())
			}
		}
	}
	return out
}

func (e *Engine) app(key string) plan.AppSpec {
	for _, a := range e.d.Apps {
		if a.Key == key {
			return a
		}
	}
	return plan.AppSpec{}
}

func (e *Engine) rule(key string) plan.RuleSpec {
	for _, r := range e.d.Rules {
		if r.Key == key {
			return r
		}
	}
	return plan.RuleSpec{}
}

func (e *Engine) group(key string) plan.GroupSpec {
	for _, g := range e.d.Groups {
		if g.Key == key {
			return g
		}
	}
	return plan.GroupSpec{}
}

// run performs one step and verifies it. It returns the ID it created.
func (e *Engine) run(ctx context.Context, s Step) (string, error) {
	switch s.Kind {
	case StepSession:
		return "", e.session(ctx, s)
	case StepIdP:
		return e.idp(ctx, s)
	case StepGroup:
		return e.createGroup(ctx, s)
	case StepServiceToken:
		return e.serviceToken(ctx, s)
	case StepApp:
		return e.createApp(ctx, s)
	case StepRule:
		return e.createRule(ctx, s)
	case StepTunnel:
		return e.createTunnel(ctx, s)
	case StepTunnelToken:
		return e.o.Tunnel.ID, e.fetchTunnelToken(ctx, s, e.o.Tunnel.ID)
	case StepRoute:
		return "", e.route(ctx, s)
	case StepDNS:
		return e.dns(ctx, s)
	case StepBotFightMode:
		return "", e.botFightMode(ctx, s)
	}
	return "", fmt.Errorf("unknown step %q", s.Kind)
}

// send performs one write and maps its failure to a typed error.
func (e *Engine) send(ctx context.Context, s Step, method, path, ref, perm string, body, out any) error {
	e.outputs.Writes = append(e.outputs.Writes, Write{Method: method, Path: path, Ref: ref})
	fmt.Fprintf(e.out, "  %s %s\n", method, path)
	var err error
	if method == "PUT" {
		err = e.write.Put(ctx, path, body, out)
	} else {
		err = e.write.Post(ctx, path, body, out)
	}
	if err == nil {
		return nil
	}
	var ce *cfapi.Error
	switch {
	case cfapi.IsInvalidToken(err):
		return errors.New("Cloudflare does not accept this API token (code 1000); nothing after the previous step was changed")
	case cfapi.IsForbidden(err):
		return &PermissionError{Permission: perm, Method: method, Path: path, Cause: err.Error()}
	case errors.As(err, &ce) && ce.Status >= 400 && ce.Status < 500 && ce.Status != 429:
		return &RejectedError{Step: s.Title, Method: method, Path: path, Cause: err.Error()}
	}
	// A 5xx, a 429, a transport failure, or success:false inside a 2xx: the write may
	// have taken effect.
	return &UncertainError{Step: s.Title, Method: method, Path: path, Cause: err.Error()}
}

func (e *Engine) readErr(perm, method, path string, err error) error {
	if cfapi.IsForbidden(err) {
		return &PermissionError{Permission: perm, Method: method, Path: path, Cause: err.Error()}
	}
	return fmt.Errorf("reading %s failed: %w", path, err)
}

// verify discovers the account again and requires the step's action to be ok, and to be
// the resource the step created (when the step knows its ID).
func (e *Engine) verify(ctx context.Context, s Step, createdID string) error {
	if err := e.discover(ctx); err != nil {
		return fmt.Errorf("%s: the write succeeded, but reading it back failed: %w", s.Title, err)
	}
	for _, a := range plan.Diff(e.d, e.o) {
		if a.ID != s.ActionID {
			continue
		}
		if a.Kind == plan.OK && (createdID == "" || a.ExistingID == createdID) {
			fmt.Fprintf(e.out, "  read back: ok %s\n", a.ExistingID)
			return nil
		}
		got := []string{fmt.Sprintf("%s %s %q [%s]", a.Kind, a.Resource, a.Name, a.ExistingID)}
		for _, d := range a.Detail {
			got = append(got, "- "+d)
		}
		want := "the " + a.Resource + " line to be ok"
		if createdID != "" {
			want += " for " + createdID
		}
		return &VerifyError{Step: s.Title, ResourceID: createdID, Expected: want, Got: got}
	}
	return &VerifyError{Step: s.Title, ResourceID: createdID, Expected: "a " + s.ActionID + " line in the plan", Got: []string{"no such line"}}
}

// keepSecret hands a secret to the sink before any other API call.
func (e *Engine) keepSecret(ctx context.Context, sec Secret, recovery string) error {
	e.out.add(sec.Value)
	if err := e.sink.Store(ctx, sec); err != nil {
		var nip *NotInPlaceError
		if errors.As(err, &nip) {
			return &NotInPlaceError{Secret: sec.Name, ResourceID: sec.ResourceID, Detail: e.out.scrub(nip.Detail)}
		}
		return &SinkError{Secret: sec.Name, ResourceID: sec.ResourceID, Cause: e.out.scrub(err.Error()), Recovery: recovery}
	}
	fmt.Fprintf(e.out, "  kept the %s on the host\n", sec.Name)
	return nil
}

// --- Steps. Each body is built from the API reference page quoted beside it. ---

// session: PUT /accounts/{account_id}/access/organizations, only with
// Options.AllowOrgSessionWrite.
// https://developers.cloudflare.com/api/resources/zero_trust/subresources/organizations/methods/update/
// ("Updates the configuration for your Zero Trust organization."; all 20 body fields
// are optional; session_duration "Must be in the format `300ms` or `2h45m`.")
// The reference does not say whether a PUT resets the fields it omits, so the body is a
// read-modify-write: every documented writable field the organization has now, with
// session_duration set. Every field is compared before and after; a difference is saved
// with its old value to a restore record, which stops later runs until it is dealt with.
func (e *Engine) session(ctx context.Context, s Step) error {
	path := e.accountPath("/access/organizations")
	before, err := e.rawObject(ctx, "Access: Organizations Read", path)
	if err != nil {
		return err
	}
	if v, ok := before["session_duration"]; ok && string(v) != `""` && string(v) != "null" {
		return &RejectedError{Step: s.Title, Method: "PUT", Path: path, Cause: "the organization now has a global session duration; setup never changes one that is set"}
	}
	body := writable(before, orgWritable)
	body["session_duration"] = jsonString(e.d.OrgSessionDuration)
	return e.readModifyWrite(ctx, s, path, "Access: Organizations Read", "Access: Organizations Edit",
		"https://developers.cloudflare.com/api/resources/zero_trust/subresources/organizations/methods/update/",
		"zero-trust-organization", before, body, "session_duration", "updated_at")
}

// orgWritable are the request body fields of the organization update, in the order of
// https://developers.cloudflare.com/api/resources/zero_trust/subresources/organizations/methods/update/
var orgWritable = []string{"allow_authenticate_via_warp", "auth_domain", "auto_redirect_to_identity", "custom_pages",
	"deny_unmatched_requests", "deny_unmatched_requests_exempted_zone_names", "disable_end_user_app_creation",
	"is_ui_read_only", "login_design", "mfa_config", "mfa_piv_key_requirements", "mfa_required_for_all_apps", "name",
	"service_token_inactivity", "session_duration", "strict_service_token_auth", "ui_read_only_toggle_reason",
	"user_seat_expiration_inactive_time", "warp_auth_non_browser_401", "warp_auth_session_duration"}

// botWritable are the BotFightModeConfiguration request body fields of
// https://developers.cloudflare.com/api/resources/bot_management/methods/update/
// without the two the page calls "A read-only field" (stale_zone_configuration,
// using_latest_model).
var botWritable = []string{"ai_agents", "ai_bots_migration_opt_out", "ai_bots_protection", "ai_search", "ai_training",
	"ai_user", "bot_preference_sync_enabled", "cf_robots_variant", "content_bots_protection", "crawler_protection",
	"enable_js", "fight_mode", "is_robots_txt_managed", "jsd_api_results_enabled"}

// writable returns the fields of obj named in fields.
func writable(obj map[string]json.RawMessage, fields []string) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for _, k := range fields {
		if v, ok := obj[k]; ok {
			out[k] = v
		}
	}
	return out
}

// readModifyWrite PUTs body to path, reads the object back and compares it with before,
// ignoring the keys this write is meant to change (skip; the first is the one it sets).
// Any other change is printed with its old value and saved to a restore record; every
// later run refuses to start while that record exists.
func (e *Engine) readModifyWrite(ctx context.Context, s Step, path, readPerm, writePerm, ref, record string,
	before, body map[string]json.RawMessage, skip ...string) error {
	if err := e.send(ctx, s, "PUT", path, ref, writePerm, body, nil); err != nil {
		return err
	}
	after, err := e.rawObject(ctx, readPerm, path)
	if err != nil {
		return fmt.Errorf("%s: the write succeeded, but reading it back failed: %w", s.Title, err)
	}
	changed, added := changedKeys(before, after, skip...)
	for _, k := range added {
		fmt.Fprintf(e.out, "  note: Cloudflare now also returns %s = %s\n", k, after[k])
	}
	if len(changed) > 0 {
		file, ferr := e.saveRestore(record, path, before, after, changed)
		got := []string{"these settings changed although setup sent their old values:"}
		for _, k := range changed {
			got = append(got, fmt.Sprintf("  %s: was %s, now %s", k, orMissing(before[k]), orMissing(after[k])))
		}
		if ferr != nil {
			got = append(got, "the restore record could not be saved ("+ferr.Error()+"); copy the old values above")
		} else {
			got = append(got, "old values saved to "+file+"; setup will not run again until you restore them and delete that file")
		}
		return &VerifyError{Step: s.Title, Expected: "only " + skip[0] + " to change", Got: got}
	}
	return e.verify(ctx, s, "")
}

func orMissing(v json.RawMessage) string {
	if v == nil {
		return "(missing)"
	}
	return string(v)
}

// restorePrefix names restore records in Options.StateDir.
const restorePrefix = "cloudflare-restore-"

// saveRestore writes the old values of the changed keys, and the whole object as it was,
// to a restore record in the state directory (mode 0600; settings only, no secret).
func (e *Engine) saveRestore(record, path string, before, after map[string]json.RawMessage, changed []string) (string, error) {
	if e.opt.StateDir == "" {
		return "", errors.New("no state directory")
	}
	type change struct {
		Before json.RawMessage `json:"before"`
		After  json.RawMessage `json:"after"`
	}
	now := time.Now().UTC()
	rec := struct {
		Resource string                     `json:"resource"`
		Path     string                     `json:"path"`
		SavedAt  string                     `json:"saved_at"`
		Changed  map[string]change          `json:"changed"`
		Before   map[string]json.RawMessage `json:"before"`
	}{Resource: record, Path: path, SavedAt: now.Format(time.RFC3339), Changed: map[string]change{}, Before: before}
	for _, k := range changed {
		rec.Changed[k] = change{Before: before[k], After: after[k]}
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return "", err
	}
	file := filepath.Join(e.opt.StateDir, restorePrefix+record+"-"+now.Format("20060102T150405.000000000Z")+".json")
	if err := os.WriteFile(file, append(data, '\n'), 0o600); err != nil {
		return "", err
	}
	return file, nil
}

// pendingRestores lists restore records left in dir.
func pendingRestores(dir string) ([]string, error) {
	if dir == "" {
		return nil, nil
	}
	files, err := filepath.Glob(filepath.Join(dir, restorePrefix+"*.json"))
	if err != nil {
		return nil, fmt.Errorf("cannot list the state directory: %w", err)
	}
	sort.Strings(files)
	return files, nil
}

// idp: POST /accounts/{account_id}/access/identity_providers
// https://developers.cloudflare.com/api/resources/zero_trust/subresources/identity_providers/methods/create/
// (fields name, type, config; "onetimepin" is a type value). The body is the guide's:
// {"name":"One-time PIN login","type":"onetimepin","config":{}}
// https://developers.cloudflare.com/cloudflare-one/integrations/identity-providers/one-time-pin/
func (e *Engine) idp(ctx context.Context, s Step) (string, error) {
	body := map[string]any{"name": "One-time PIN login", "type": e.d.IdentityProvider, "config": map[string]any{}}
	var r struct {
		ID string `json:"id"`
	}
	if err := e.send(ctx, s, "POST", e.accountPath("/access/identity_providers"),
		"https://developers.cloudflare.com/api/resources/zero_trust/subresources/identity_providers/methods/create/",
		"Access: Identity Providers Edit", body, &r); err != nil {
		return "", err
	}
	return r.ID, e.verify(ctx, s, r.ID)
}

// createGroup: POST /accounts/{account_id}/access/groups
// https://developers.cloudflare.com/api/resources/zero_trust/subresources/access/subresources/groups/methods/create/
// (name; "include: array of AccessRule"; "EmailRule object { email }" → {"email":{"email":…}})
func (e *Engine) createGroup(ctx context.Context, s Step) (string, error) {
	g := e.group(s.Key)
	var include []any
	for _, m := range e.opt.GroupEmails[s.Key] {
		include = append(include, map[string]any{"email": map[string]any{"email": m}})
	}
	body := map[string]any{"name": g.Name, "include": include}
	var r struct {
		ID string `json:"id"`
	}
	if err := e.send(ctx, s, "POST", e.accountPath("/access/groups"),
		"https://developers.cloudflare.com/api/resources/zero_trust/subresources/access/subresources/groups/methods/create/",
		"Access: Groups Edit", body, &r); err != nil {
		return "", err
	}
	return r.ID, e.verify(ctx, s, r.ID)
}

// serviceToken: POST /accounts/{account_id}/access/service_tokens {"name", "duration"}
// https://developers.cloudflare.com/api/resources/zero_trust/subresources/access/subresources/service_tokens/methods/create/
// (only name is required; duration "Must be in the format `300ms` or `2h45m`, or the
// special value `forever`"; the response has id, client_id, client_secret.)
// The client secret goes to the sink before any other API call.
func (e *Engine) serviceToken(ctx context.Context, s Step) (string, error) {
	name := nameOf(e.d, s.Key)
	body := map[string]any{"name": name, "duration": e.opt.ServiceTokenDuration}
	var r struct {
		ID           string `json:"id"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if err := e.send(ctx, s, "POST", e.accountPath("/access/service_tokens"),
		"https://developers.cloudflare.com/api/resources/zero_trust/subresources/access/subresources/service_tokens/methods/create/",
		"Access: Service Tokens Edit", body, &r); err != nil {
		return "", err
	}
	rotate := fmt.Sprintf("Rotate its client secret: Zero Trust → Access controls → Service credentials → Service Tokens → %s → Rotate "+
		"(API: POST %s), then give the new secret to the bots. This version of setup does not rotate it for you", name, e.accountPath("/access/service_tokens/"+orNone(r.ID)+"/rotate"))
	if r.ID == "" || r.ClientID == "" || r.ClientSecret == "" {
		e.out.add(r.ClientSecret)
		return r.ID, &ShapeError{Step: s.Title, ResourceID: r.ID, Missing: "id, client_id and client_secret", Recovery: rotate}
	}
	if err := e.keepSecret(ctx, Secret{Name: SecretServicesToken, ResourceID: r.ID, ClientID: r.ClientID, Value: r.ClientSecret}, rotate); err != nil {
		return r.ID, err
	}
	return r.ID, e.verify(ctx, s, r.ID)
}

// policyBody is a reusable Access policy:
// POST /accounts/{account_id}/access/policies {"name", "decision", "include"}
// https://developers.cloudflare.com/api/resources/zero_trust/subresources/access/subresources/policies/methods/create/
// (decision "allow" | "deny" | "non_identity" | "bypass"; include rules
// {"group":{"id"}}, {"everyone":{}}, {"service_token":{"token_id"}}).
func (e *Engine) policyBody(p plan.PolicySpec) (map[string]any, []string) {
	var include []any
	var keys []string
	if p.Everyone {
		include = append(include, map[string]any{"everyone": map[string]any{}})
		keys = append(keys, "everyone")
	}
	for _, g := range p.Groups {
		id := e.o.Groups[g].ID
		include = append(include, map[string]any{"group": map[string]any{"id": id}})
		keys = append(keys, "group:"+id)
	}
	if p.ServiceToken != "" {
		id := e.o.ServiceTokens[p.ServiceToken].ID
		include = append(include, map[string]any{"service_token": map[string]any{"token_id": id}})
		keys = append(keys, "service_token:"+id)
	}
	sort.Strings(keys)
	return map[string]any{"name": p.Name, "decision": p.Decision, "include": include}, keys
}

type listedPolicy struct {
	ID       string                       `json:"id"`
	Name     string                       `json:"name"`
	Decision string                       `json:"decision"`
	Include  []map[string]json.RawMessage `json:"include"`
	Require  []json.RawMessage            `json:"require"`
	Exclude  []json.RawMessage            `json:"exclude"`
	AppCount *int                         `json:"app_count"`
}

// reusablePolicy looks for a reusable policy with the spec's policy name, left by an
// earlier interrupted run: GET /accounts/{account_id}/access/policies (no name filter)
// https://developers.cloudflare.com/api/resources/zero_trust/subresources/access/subresources/policies/methods/list/
// It returns the ID to reuse when one matches exactly and no application uses it, or a
// problem when a same-named policy exists that setup must not take over.
func (e *Engine) reusablePolicy(ctx context.Context, spec plan.AppSpec) (string, string, error) {
	path := e.accountPath("/access/policies")
	all, err := cfapi.ListAll[listedPolicy](ctx, e.read, path, nil)
	if err != nil {
		return "", "", e.readErr("Access: Policies Read", "GET", path, err)
	}
	_, want := e.policyBody(spec.Policy)
	var hits []listedPolicy
	for _, p := range all {
		if p.Name == spec.Policy.Name {
			hits = append(hits, p)
		}
	}
	switch {
	case len(hits) == 0:
		return "", "", nil
	case len(hits) > 1:
		return "", fmt.Sprintf("%d reusable Access policies are named %q; setup will not pick one. Rename the extra ones", len(hits), spec.Policy.Name), nil
	}
	p := hits[0]
	var got []string
	for _, rule := range p.Include {
		in := parseInclude(rule)
		if in.ID == "" {
			got = append(got, in.Type)
		} else {
			got = append(got, in.Type+":"+in.ID)
		}
	}
	sort.Strings(got)
	switch {
	case p.AppCount == nil || *p.AppCount != 0:
		return "", fmt.Sprintf("a reusable Access policy named %q (%s) already exists and is used by applications; setup will not reuse or change it. Rename it, then run setup again", p.Name, p.ID), nil
	case p.Decision != spec.Policy.Decision || !reflect.DeepEqual(got, want) || len(p.Require) > 0 || len(p.Exclude) > 0:
		return "", fmt.Sprintf("a reusable Access policy named %q (%s) already exists with other rules (%s, %s); setup will not reuse or change it. Rename it, then run setup again", p.Name, p.ID, p.Decision, strings.Join(got, ", ")), nil
	}
	return p.ID, "", nil
}

// createApp creates (or reuses, after an interrupted run) the app's reusable policy,
// then the app with it.
//
// POST /accounts/{account_id}/access/apps
// https://developers.cloudflare.com/api/resources/zero_trust/subresources/access/subresources/applications/methods/create/
// Self-hosted body: type "self_hosted"; name ("The name of the application."); domain
// ("The primary hostname and path secured by Access", unmarked on the self-hosted
// variant, so it is sent); destinations [{"type":"public","uri":"<host>[/<path>]"}]
// ("The public hostname and optional path to secure."); session_duration;
// allowed_idps; auto_redirect_to_identity ("You must specify only one identity provider
// in allowed_idps."); service_auth_401_redirect; policies [{"id","precedence"}]
// (AccessAppPolicyLink). A live list returned destinations, session_duration,
// allowed_idps, auto_redirect_to_identity, aud and the embedded policies with precedence.
func (e *Engine) createApp(ctx context.Context, s Step) (string, error) {
	spec := e.app(s.Key)
	polID, problem, err := e.reusablePolicy(ctx, spec)
	switch {
	case err != nil:
		return "", err
	case problem != "":
		return "", &RejectedError{Step: s.Title, Method: "POST", Path: e.accountPath("/access/policies"), Cause: problem}
	}
	pbody, want := e.policyBody(spec.Policy)
	if polID != "" {
		fmt.Fprintf(e.out, "  reusing policy %s, left unused by an earlier run\n", polID)
	} else {
		var r struct {
			ID string `json:"id"`
		}
		if err := e.send(ctx, s, "POST", e.accountPath("/access/policies"),
			"https://developers.cloudflare.com/api/resources/zero_trust/subresources/access/subresources/policies/methods/create/",
			"Access: Policies Edit", pbody, &r); err != nil {
			return "", err
		}
		if r.ID == "" {
			return "", &ShapeError{Step: s.Title, Missing: "the policy id", Recovery: "Run setup again: it reuses an unused policy with this name"}
		}
		polID = r.ID
		// Read the policy back before an application depends on it.
		id, problem, err := e.reusablePolicy(ctx, spec)
		if err != nil {
			return "", err
		}
		if id != polID {
			return "", &VerifyError{Step: s.Title, ResourceID: polID, Expected: fmt.Sprintf("one unused reusable policy %q, %s, including %s", spec.Policy.Name, spec.Policy.Decision, strings.Join(want, ", ")),
				Got: []string{orNone(problem)}}
		}
	}
	var dests []any
	for _, d := range spec.Destinations {
		dests = append(dests, map[string]any{"type": "public", "uri": d})
	}
	body := map[string]any{
		"type":         "self_hosted",
		"name":         spec.Name,
		"domain":       spec.Destinations[0],
		"destinations": dests,
		"policies":     []any{map[string]any{"id": polID, "precedence": 1}},
	}
	if spec.SessionDuration != "" {
		body["session_duration"] = spec.SessionDuration
	}
	if spec.OnlyIdP {
		body["allowed_idps"] = []any{e.o.IdP.ID}
	}
	if spec.AutoRedirect {
		body["auto_redirect_to_identity"] = true
	}
	if spec.ServiceAuth401 {
		body["service_auth_401_redirect"] = true
	}
	var r struct {
		ID string `json:"id"`
	}
	if err := e.send(ctx, s, "POST", e.accountPath("/access/apps"),
		"https://developers.cloudflare.com/api/resources/zero_trust/subresources/access/subresources/applications/methods/create/",
		"Access: Apps Edit", body, &r); err != nil {
		return "", err
	}
	// From here the application exists. A bypass application that is not exactly what
	// setup asked for may open the dashboard, and setup cannot delete it, so any failure
	// now leads with what the person must check.
	afterPost := func(err error) error {
		if err == nil || spec.Policy.Decision != "bypass" {
			return err
		}
		return &bypassAppError{AppID: orNone(r.ID), Paths: spec.Destinations, Err: err}
	}
	if r.ID == "" {
		return "", afterPost(&ShapeError{Step: s.Title, Missing: "the application id", Recovery: "Run setup again: it finds the application by its hostname and paths"})
	}
	return r.ID, afterPost(e.verify(ctx, s, r.ID))
}

// bypassAppError is any failure after a bypass application was created: the person must
// check the new application at once, because it skips the sign-in.
type bypassAppError struct {
	AppID string
	Paths []string
	Err   error
}

func (e *bypassAppError) Error() string {
	cause := e.Err.Error()
	var amb *AmbiguousError
	if errors.As(e.Err, &amb) {
		// Not the ambiguity's own advice: renaming would leave the bypass in place.
		cause = fmt.Sprintf("Cloudflare now lists more than one %s for %q (%s)", amb.Resource, amb.Name, strings.Join(amb.IDs, ", "))
	}
	return fmt.Sprintf("Access application %s was just created and skips the sign-in, but setup could not confirm it is what it asked for. "+
		"Open it now in Zero Trust → Access controls → Applications and check its destinations: if they are wider than %s, delete it at once. "+
		"Setup cannot delete it.\nWhat went wrong: %s", e.AppID, strings.Join(e.Paths, ", "), cause)
}

func (e *bypassAppError) Unwrap() error { return e.Err }

// createRule adds one rule to its phase.
//
// Empty phase (404 with code 10003): POST /zones/{zone_id}/rulesets
// {"kind":"zone","name":"default","phase":…,"rules":[rule]}
// https://developers.cloudflare.com/api/resources/rulesets/methods/create/ ("kind":
// "managed" | "custom" | "root" | "zone"); the guide: "If the entry point ruleset does
// not exist … create it using the Create a zone ruleset operation."
// https://developers.cloudflare.com/waf/custom-rules/create-api/
//
// Non-empty phase: POST /zones/{zone_id}/rulesets/{ruleset_id}/rules
// https://developers.cloudflare.com/api/resources/rulesets/subresources/rules/methods/create/
// ("Adds a new rule to an account or zone ruleset. The rule will be added to the end of
// the existing list of rules in the ruleset by default."; position {"index": n}, "where
// index 1 is the first rule"). Existing rules are never sent, so they are kept.
//
// Rule fields: description, expression, action, action_parameters, enabled (rulesets
// create page); the skip rule's "logging": {"enabled": true} and products from
// https://developers.cloudflare.com/waf/custom-rules/skip/options/.
func (e *Engine) createRule(ctx context.Context, s Step) (string, error) {
	spec := e.rule(s.Key)
	r := e.o.Rules[spec.Key]
	rule := map[string]any{"description": spec.Description, "expression": spec.Expression, "action": spec.Action,
		"action_parameters": spec.Params, "enabled": true}
	if spec.Action == "skip" {
		rule["logging"] = map[string]any{"enabled": true} // cloudflare.md §6.2: "Keep Log matching requests on."
	}
	perm := map[string]string{"http_request_firewall_custom": "Zone WAF Edit", "http_request_cache_settings": "Cache Rules Edit",
		"http_config_settings": "Config Rules Edit"}[spec.Phase]
	var res struct {
		ID    string `json:"id"`
		Rules []struct {
			ID          string `json:"id"`
			Description string `json:"description"`
		} `json:"rules"`
	}
	switch {
	case r.PhaseEmpty:
		body := map[string]any{"kind": "zone", "name": "default", "phase": spec.Phase, "rules": []any{rule}}
		if err := e.send(ctx, s, "POST", e.zonePath("/rulesets"), "https://developers.cloudflare.com/api/resources/rulesets/methods/create/", perm, body, &res); err != nil {
			return "", err
		}
	case r.RulesetID != "":
		if spec.Position == "first" {
			rule["position"] = map[string]any{"index": 1}
		}
		if err := e.send(ctx, s, "POST", e.zonePath("/rulesets/"+url.PathEscape(r.RulesetID)+"/rules"),
			"https://developers.cloudflare.com/api/resources/rulesets/subresources/rules/methods/create/", perm, rule, &res); err != nil {
			return "", err
		}
	default:
		return "", &RejectedError{Step: s.Title, Method: "POST", Path: e.zonePath("/rulesets"), Cause: "the phase's ruleset state is not known"}
	}
	// The rule create response is not confirmed by the reference; if the rule is not in
	// it, the read-back still finds it by description.
	id := ""
	for _, x := range res.Rules {
		if x.Description == spec.Description {
			id = x.ID
		}
	}
	return id, e.verify(ctx, s, id)
}

// createTunnel: POST /accounts/{account_id}/cfd_tunnel {"name", "config_src":"cloudflare"}
// https://developers.cloudflare.com/api/resources/zero_trust/subresources/tunnels/subresources/cloudflared/methods/create/
// (config_src: "If `cloudflare`, manage the tunnel on the Zero Trust dashboard."; the
// reference: "After creation, retrieve its token". The response schema lists no token,
// but the API guide's example response has one; either way it goes to the sink first.)
func (e *Engine) createTunnel(ctx context.Context, s Step) (string, error) {
	body := map[string]any{"name": e.d.Tunnel.Name, "config_src": "cloudflare"}
	var r struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	if err := e.send(ctx, s, "POST", e.accountPath("/cfd_tunnel"),
		"https://developers.cloudflare.com/api/resources/zero_trust/subresources/tunnels/subresources/cloudflared/methods/create/",
		"Cloudflare Tunnel Edit", body, &r); err != nil {
		return "", err
	}
	if r.ID == "" {
		e.out.add(r.Token)
		return "", &ShapeError{Step: s.Title, Missing: "the tunnel id", Recovery: "Run setup again: it finds the tunnel by name"}
	}
	e.created[r.ID] = true
	if r.Token != "" {
		if err := e.keepSecret(ctx, Secret{Name: SecretTunnelToken, ResourceID: r.ID, Value: r.Token}, tunnelRecovery(e.d.Tunnel.Name)); err != nil {
			return r.ID, err
		}
	} else if err := e.fetchTunnelToken(ctx, s, r.ID); err != nil {
		return r.ID, err
	}
	return r.ID, e.verify(ctx, s, r.ID)
}

func tunnelRecovery(name string) string {
	return "The tunnel exists and Cloudflare can return its token again: fix the host's secret files and run setup again, " +
		"or open Zero Trust → Networks → Tunnels → " + name + " and copy the token"
}

// fetchTunnelToken: GET /accounts/{account_id}/cfd_tunnel/{tunnel_id}/token
// https://developers.cloudflare.com/api/resources/zero_trust/subresources/tunnels/subresources/cloudflared/subresources/token/methods/get/
// ("result: string"; it accepts only Write permissions, so it needs Cloudflare Tunnel Edit).
func (e *Engine) fetchTunnelToken(ctx context.Context, s Step, id string) error {
	path := e.accountPath("/cfd_tunnel/" + url.PathEscape(id) + "/token")
	var tok string
	if err := e.read.Get(ctx, path, nil, &tok); err != nil {
		if cfapi.IsForbidden(err) {
			return &PermissionError{Permission: "Cloudflare Tunnel Edit", Method: "GET", Path: path, Cause: err.Error()}
		}
		return fmt.Errorf("%s: fetching the tunnel token failed (%v). %s", s.Title, err, tunnelRecovery(e.d.Tunnel.Name))
	}
	if tok == "" {
		return &ShapeError{Step: s.Title, ResourceID: id, Missing: "the tunnel token", Recovery: tunnelRecovery(e.d.Tunnel.Name)}
	}
	return e.keepSecret(ctx, Secret{Name: SecretTunnelToken, ResourceID: id, Value: tok}, tunnelRecovery(e.d.Tunnel.Name))
}

// tunnelConfig reads the tunnel's whole remote configuration, keeping every field.
// GET /accounts/{account_id}/cfd_tunnel/{tunnel_id}/configurations
// https://developers.cloudflare.com/api/resources/zero_trust/subresources/tunnels/subresources/cloudflared/subresources/configurations/methods/get/
// A PUT replaces the configuration, so setup writes one only when it could read it. A
// 404 counts as "no configuration yet" only for a tunnel this run created, or one with
// the doc name that has never had a connection (one left by an interrupted run).
func (e *Engine) tunnelConfig(ctx context.Context) (map[string]json.RawMessage, error) {
	id := e.o.Tunnel.ID
	path := e.accountPath("/cfd_tunnel/" + url.PathEscape(id) + "/configurations")
	var res struct {
		Config map[string]json.RawMessage `json:"config"`
	}
	err := e.read.Get(ctx, path, nil, &res)
	switch {
	case err == nil:
	case cfapi.IsNotFound(err):
		fresh, ferr := e.neverConnected(ctx, id)
		if ferr != nil {
			return nil, ferr
		}
		if !e.created[id] && !fresh {
			return nil, fmt.Errorf("Cloudflare answered 404 for the configuration of tunnel %s, which has been used before; a route is added by replacing the whole configuration, so setup will not write one it could not read (%v)", id, err)
		}
	default:
		return nil, e.readErr("Cloudflare Tunnel Read", "GET", path, err)
	}
	if res.Config == nil {
		res.Config = map[string]json.RawMessage{}
	}
	return res.Config, nil
}

// neverConnected reports whether the tunnel has the doc name and no recorded connection
// (conns_active_at null), from GET /accounts/{account_id}/cfd_tunnel?uuid=…
// https://developers.cloudflare.com/api/resources/zero_trust/subresources/tunnels/subresources/cloudflared/methods/list/
func (e *Engine) neverConnected(ctx context.Context, id string) (bool, error) {
	type tun struct {
		ID            string  `json:"id"`
		Name          string  `json:"name"`
		ConnsActiveAt *string `json:"conns_active_at"`
	}
	path := e.accountPath("/cfd_tunnel")
	all, err := cfapi.ListAll[tun](ctx, e.read, path, url.Values{"uuid": {id}, "is_deleted": {"false"}})
	if err != nil {
		return false, e.readErr("Cloudflare Tunnel Read", "GET", path, err)
	}
	for _, t := range all {
		if strings.EqualFold(t.ID, id) {
			return t.Name == e.d.Tunnel.Name && t.ConnsActiveAt == nil, nil
		}
	}
	return false, nil
}

// route adds the dashboard's ingress rule, keeping every other rule, every other config
// field and the catch-all last.
// PUT /accounts/{account_id}/cfd_tunnel/{tunnel_id}/configurations {"config": {…}}
// https://developers.cloudflare.com/api/resources/zero_trust/subresources/tunnels/subresources/cloudflared/subresources/configurations/methods/update/
// ("Replaces the configuration for a remotely managed Cloudflare Tunnel, including its
// ingress rules and origin request settings."; ingress items {hostname, service, path,
// originRequest}). The catch-all {"service":"http_status:404"} is the API guide's
// https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/get-started/create-remote-tunnel-api/
func (e *Engine) route(ctx context.Context, s Step) error {
	path := e.accountPath("/cfd_tunnel/" + url.PathEscape(e.o.Tunnel.ID) + "/configurations")
	cfg, err := e.tunnelConfig(ctx)
	if err != nil {
		return err
	}
	var ingress []map[string]json.RawMessage
	if raw, ok := cfg["ingress"]; ok && string(raw) != "null" {
		if err := json.Unmarshal(raw, &ingress); err != nil {
			return &RejectedError{Step: s.Title, Method: "PUT", Path: path, Cause: "the tunnel's ingress rules have a shape setup does not know"}
		}
	}
	ours := map[string]json.RawMessage{"hostname": jsonString(e.d.Hostname), "service": jsonString(e.d.Tunnel.Service)}
	catchAll := map[string]json.RawMessage{"service": jsonString("http_status:404")}
	var next []map[string]json.RawMessage
	switch {
	case len(ingress) == 0:
		next = []map[string]json.RawMessage{ours, catchAll}
	case isCatchAll(ingress[len(ingress)-1]):
		next = append(append(append(next, ingress[:len(ingress)-1]...), ours), ingress[len(ingress)-1])
	default:
		return &RejectedError{Step: s.Title, Method: "PUT", Path: path, Cause: "the tunnel's last ingress rule is not a catch-all; setup will not edit routes it does not understand"}
	}
	for _, r := range ingress {
		if h := stringField(r, "hostname"); strings.EqualFold(h, e.d.Hostname) && strings.Trim(stringField(r, "path"), "/") == "" {
			return &RejectedError{Step: s.Title, Method: "PUT", Path: path, Cause: "the tunnel already routes " + e.d.Hostname}
		}
	}
	newCfg := map[string]any{}
	for k, v := range cfg {
		newCfg[k] = v
	}
	newCfg["ingress"] = next
	// The GET and this PUT are milliseconds apart and the PUT carries no version
	// precondition (none is documented), so an edit made in the dashboard in between
	// would be lost. That risk is accepted; the read-back below catches it for every key
	// the GET returned.
	if err := e.send(ctx, s, "PUT", path,
		"https://developers.cloudflare.com/api/resources/zero_trust/subresources/tunnels/subresources/cloudflared/subresources/configurations/methods/update/",
		"Cloudflare Tunnel Edit", map[string]any{"config": newCfg}, nil); err != nil {
		return err
	}
	after, err := e.tunnelConfig(ctx)
	if err != nil {
		return fmt.Errorf("%s: the write succeeded, but reading it back failed: %w", s.Title, err)
	}
	if !jsonEqual(after["ingress"], next) {
		return &VerifyError{Step: s.Title, ResourceID: e.o.Tunnel.ID, Expected: "the previous routes in order, with " + e.d.Hostname + " added before the catch-all",
			Got: []string{"ingress " + string(after["ingress"])}}
	}
	// A key Cloudflare adds on its own (a fresh tunnel's warp-routing default, say) is
	// reported, not failed; a key that was there before must be unchanged.
	changed, added := changedKeys(cfg, after, "ingress")
	for _, k := range added {
		fmt.Fprintf(e.out, "  note: Cloudflare now also returns config.%s = %s\n", k, after[k])
	}
	if len(changed) > 0 {
		got := []string{}
		for _, k := range changed {
			got = append(got, fmt.Sprintf("config.%s: was %s, now %s", k, orMissing(cfg[k]), orMissing(after[k])))
		}
		return &VerifyError{Step: s.Title, ResourceID: e.o.Tunnel.ID, Expected: "only the ingress rules to change", Got: got}
	}
	return e.verify(ctx, s, "")
}

func isCatchAll(r map[string]json.RawMessage) bool {
	return stringField(r, "hostname") == "" && stringField(r, "path") == "" && stringField(r, "service") != ""
}

// dns: POST /zones/{zone_id}/dns_records
// {"type":"CNAME","name":<host>,"content":"<tunnel-id>.cfargotunnel.com","proxied":true,"ttl":1}
// https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/create/
// (content "A valid hostname"; ttl "Setting to 1 means 'automatic'"). The API does not
// create this record with the route, unlike the dashboard's "Add route":
// https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/get-started/create-remote-tunnel-api/
func (e *Engine) dns(ctx context.Context, s Step) (string, error) {
	body := map[string]any{"type": "CNAME", "name": e.d.Hostname, "content": e.o.Tunnel.ID + ".cfargotunnel.com", "proxied": true, "ttl": 1}
	var r struct {
		ID string `json:"id"`
	}
	if err := e.send(ctx, s, "POST", e.zonePath("/dns_records"),
		"https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/create/", "DNS Edit", body, &r); err != nil {
		return "", err
	}
	return r.ID, e.verify(ctx, s, r.ID)
}

// botFightMode: PUT /zones/{zone_id}/bot_management, only with
// Options.AllowBotFightModeOff.
// https://developers.cloudflare.com/api/resources/bot_management/methods/update/
// ("fight_mode: optional boolean" "Whether to enable Bot Fight Mode."). The reference
// does not say whether omitted fields are kept, so the body is a read-modify-write of
// the BotFightModeConfiguration fields the zone has now, with fight_mode false.
func (e *Engine) botFightMode(ctx context.Context, s Step) error {
	path := e.zonePath("/bot_management")
	before, err := e.rawObject(ctx, "Bot Management Read", path)
	if err != nil {
		return err
	}
	body := writable(before, botWritable)
	body["fight_mode"] = json.RawMessage("false")
	return e.readModifyWrite(ctx, s, path, "Bot Management Read", "Bot Management Edit",
		"https://developers.cloudflare.com/api/resources/bot_management/methods/update/", "bot-management", before, body, "fight_mode")
}

// rateLimitRules counts the rules in the zone's http_ratelimit phase:
// GET /zones/{zone_id}/rulesets/phases/http_ratelimit/entrypoint
// https://developers.cloudflare.com/api/resources/rulesets/subresources/phases/methods/get/
// A 404 with code 10003 means the phase has no rules; the live zone had this ruleset.
func (e *Engine) rateLimitRules(ctx context.Context) (int, string) {
	var rs struct {
		Rules []json.RawMessage `json:"rules"`
	}
	err := e.read.Get(ctx, e.zonePath("/rulesets/phases/http_ratelimit/entrypoint"), nil, &rs)
	switch {
	case err == nil:
		return len(rs.Rules), ""
	case cfapi.IsNotFound(err) && cfapi.HasCode(err, codeNoEntrypoint):
		return 0, ""
	}
	return 0, err.Error()
}

func (e *Engine) rawObject(ctx context.Context, perm, path string) (map[string]json.RawMessage, error) {
	var m map[string]json.RawMessage
	if err := e.read.Get(ctx, path, nil, &m); err != nil {
		return nil, e.readErr(perm, "GET", path, err)
	}
	return m, nil
}

// changedKeys compares before and after, ignoring skip. changed lists the keys that were
// present before and now differ or are gone; added lists keys Cloudflare returns only
// after the write (a default it fills in, for example), which are reported, not failed.
func changedKeys(before, after map[string]json.RawMessage, skip ...string) (changed, added []string) {
	for k, v := range before {
		if contains(skip, k) {
			continue
		}
		if w, ok := after[k]; !ok || !jsonEqual(v, w) {
			changed = append(changed, k)
		}
	}
	for k := range after {
		if _, ok := before[k]; !ok && !contains(skip, k) {
			added = append(added, k)
		}
	}
	sort.Strings(changed)
	sort.Strings(added)
	return changed, added
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func jsonEqual(a, b any) bool {
	norm := func(v any) any {
		raw, ok := v.(json.RawMessage)
		if !ok {
			var err error
			if raw, err = json.Marshal(v); err != nil {
				return nil
			}
		}
		var out any
		if json.Unmarshal(raw, &out) != nil {
			return nil
		}
		return out
	}
	return reflect.DeepEqual(norm(a), norm(b))
}

func jsonString(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

func stringField(m map[string]json.RawMessage, k string) string {
	var s string
	if raw, ok := m[k]; ok {
		_ = json.Unmarshal(raw, &s)
	}
	return s
}

// redactor removes every secret the run has seen from progress output and errors.
type redactor struct {
	w       io.Writer
	secrets []string
}

func (r *redactor) add(s string) {
	if len(s) >= 8 {
		r.secrets = append(r.secrets, s)
	}
}

func (r *redactor) scrub(s string) string {
	for _, sec := range r.secrets {
		s = strings.ReplaceAll(s, sec, "[redacted]")
	}
	return s
}

func (r *redactor) Write(p []byte) (int, error) {
	if r.w == nil {
		return len(p), nil
	}
	if _, err := io.WriteString(r.w, r.scrub(string(p))); err != nil {
		return 0, err
	}
	return len(p), nil
}

// scrubErr returns err with every secret the run saw removed from it and from every
// error it wraps, so that neither printing it nor unwrapping it (errors.As, errors.Unwrap)
// can reach a secret. A setup error type is returned as a scrubbed copy of the same type,
// so errors.As still works; a wrapping error is rebuilt around its scrubbed cause; any
// other error whose text holds a secret becomes a plain error with the text scrubbed.
func (r *redactor) scrubErr(err error) error {
	if err == nil || len(r.secrets) == 0 {
		return err
	}
	if c, ok := r.scrubStruct(err); ok {
		return c
	}
	msg := err.Error()
	if u := errors.Unwrap(err); u != nil && strings.HasSuffix(msg, u.Error()) {
		return fmt.Errorf("%s%w", r.scrub(strings.TrimSuffix(msg, u.Error())), r.scrubErr(u))
	}
	if clean := r.scrub(msg); clean != msg {
		return errors.New(clean)
	}
	return err
}

// scrubStruct copies an error that is a pointer to a struct with only exported fields,
// scrubbing its strings, string slices and wrapped errors. It reports false for any
// other error, or when the copy still prints a secret.
func (r *redactor) scrubStruct(err error) (error, bool) {
	v := reflect.ValueOf(err)
	if v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Struct {
		return nil, false
	}
	cp := reflect.New(v.Elem().Type())
	cp.Elem().Set(v.Elem())
	errType := reflect.TypeOf((*error)(nil)).Elem()
	for i := 0; i < cp.Elem().NumField(); i++ {
		f := cp.Elem().Field(i)
		if !f.CanSet() {
			return nil, false
		}
		switch {
		case f.Kind() == reflect.String:
			f.SetString(r.scrub(f.String()))
		case f.Kind() == reflect.Slice && f.Type().Elem().Kind() == reflect.String:
			s := reflect.MakeSlice(f.Type(), f.Len(), f.Len())
			for j := 0; j < f.Len(); j++ {
				s.Index(j).SetString(r.scrub(f.Index(j).String()))
			}
			f.Set(s)
		case f.Type() == errType && !f.IsNil():
			f.Set(reflect.ValueOf(r.scrubErr(f.Interface().(error))))
		}
	}
	c, ok := cp.Interface().(error)
	if !ok || r.scrub(c.Error()) != c.Error() {
		return nil, false
	}
	return c, true
}
