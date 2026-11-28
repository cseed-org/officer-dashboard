package cfprovision

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"officerdashboard/internal/setup/cfapi"
	"officerdashboard/internal/setup/cffake"
)

// requireNoSecrets fails if any secret the fake handed out appears in text.
func requireNoSecrets(t *testing.T, a *cffake.Account, where string, texts ...string) {
	t.Helper()
	if len(a.Secrets()) == 0 {
		t.Fatal("the fake handed out no secrets; the check would prove nothing")
	}
	for _, s := range a.Secrets() {
		for _, text := range texts {
			if strings.Contains(text, s) {
				t.Errorf("a secret appears in %s", where)
			}
		}
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%v | %+v | %#v | %s", err, err, err, err.Error())
}

func TestSecretsNeverAppearInOutputErrorsOrOutputs(t *testing.T) {
	r := newRig(t, cffake.Fresh())
	r.acct.TunnelTokenInCreate(true) // the guide's example response, with the token in it
	out, err := r.apply(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	js, _ := json.Marshal(out)
	requireNoSecrets(t, r.acct, "the output", r.out.String())
	requireNoSecrets(t, r.acct, "the outputs", string(js), fmt.Sprintf("%v %+v %#v", out, out, out))
	tok, _ := r.sink.Get(SecretTunnelToken)
	sec, _ := r.sink.Get(SecretServicesToken)
	if s := fmt.Sprintf("%v %+v %#v %s", tok, sec, sec, sec); strings.Contains(s, tok.Value) || strings.Contains(s, sec.Value) {
		t.Error("formatting a Secret prints its value")
	}
	// With the token in the create response, it is not fetched again.
	for _, req := range r.acct.Requests() {
		if strings.HasSuffix(req.Path, "/token") {
			t.Errorf("fetched the token although the create response had it: %s", req.Path)
		}
	}
	// No request body carries a secret either.
	for _, w := range r.acct.Writes() {
		requireNoSecrets(t, r.acct, "a request body", w.Body)
	}
}

func TestSinkFailureStopsAtOnceAndSaysHowToRecover(t *testing.T) {
	cases := map[SecretName]struct{ lastWrite, recovery string }{
		SecretServicesToken: {"/access/service_tokens", "Rotate its client secret"},
		SecretTunnelToken:   {"/cfd_tunnel", "Cloudflare can return its token again"},
	}
	for name, want := range cases {
		t.Run(string(name), func(t *testing.T) {
			r := newRig(t, cffake.Fresh())
			r.sink.FailStore = map[SecretName]error{name: fmt.Errorf("%w (and a careless sink echoing its input: %s)", ErrSinkFull, "{{SECRET}}")}
			// The sink above cannot know the secret; make it echo the real one instead.
			r.sink.FailStore[name] = echoErr{}
			_, err := r.apply(context.Background())
			var sinkErr *SinkError
			if !errors.As(err, &sinkErr) && !strings.Contains(errText(err), "could not keep the") {
				t.Fatalf("err = %v", err)
			}
			if !strings.Contains(err.Error(), want.recovery) || !strings.Contains(err.Error(), "never printed") {
				t.Errorf("no recovery step: %v", err)
			}
			ws := r.acct.Writes()
			if last := ws[len(ws)-1]; !strings.HasSuffix(last.Path, want.lastWrite) {
				t.Errorf("wrote %s %s after the secret was lost", last.Method, last.Path)
			}
			requireNoSecrets(t, r.acct, "the error", errText(err))
			requireNoSecrets(t, r.acct, "the output", r.out.String())
			// Nothing was requested after the create that returned the secret, except
			// the token fetch that produced it.
			reqs := r.acct.Requests()
			lastWrite := 0
			for i, q := range reqs {
				if q.Method != http.MethodGet {
					lastWrite = i
				}
			}
			for _, q := range reqs[lastWrite+1:] {
				if !strings.HasSuffix(q.Path, "/token") {
					t.Errorf("API call after the secret was lost: %s %s", q.Method, q.Path)
				}
			}
		})
	}
}

// echoErr is a sink failure whose text would leak the secret if setup printed it raw;
// Store receives the secret, so it can put it in the error.
type echoErr struct{}

func (echoErr) Error() string { return "disk full" }

func TestSinkErrorTextIsScrubbed(t *testing.T) {
	r := newRig(t, cffake.Fresh())
	r.sink = nil
	leaky := &leakySink{}
	e := NewEngine("test-token", r.d, leaky, &r.out, r.opt, WithAPIBase(r.acct.URL), WithReadRetry())
	_, err := e.Apply(context.Background(), r.preview())
	var sinkErr *SinkError
	if !errors.As(err, &sinkErr) || !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("err = %v (want a scrubbed *SinkError)", err)
	}
	requireNoSecrets(t, r.acct, "the error", errText(err))
}

// leakySink fails with the secret in its error text.
type leakySink struct{}

func (leakySink) Store(_ context.Context, s Secret) error {
	return errors.New("cannot write " + s.Value)
}
func (leakySink) Has(context.Context, SecretName) (bool, error) {
	return false, nil
}

func (leakySink) Load(_ context.Context, name SecretName) (string, error) {
	if name == SecretBackupBearer {
		return testBearer, nil
	}
	return "", ErrNotKept
}

func TestWriteFailuresStopSafely(t *testing.T) {
	acct := "/accounts/" + cffake.AccountID
	cases := []struct {
		name string
		in   cffake.Injection
		want func(error) bool
		text string
	}{
		{"5xx before the write took effect", cffake.Injection{Method: "POST", Path: acct + "/access/apps", Status: 502},
			func(err error) bool { var e *UncertainError; return errors.As(err, &e) }, "may or may not have taken effect"},
		{"5xx after the write took effect", cffake.Injection{Method: "POST", Path: acct + "/access/apps", Status: 504, Apply: true},
			func(err error) bool { var e *UncertainError; return errors.As(err, &e) }, "will not create a second copy"},
		{"missing permission", cffake.Injection{Method: "POST", Path: acct + "/access/groups", Status: 401, Code: 1001, Message: "Not authorized"},
			func(err error) bool { var e *PermissionError; return errors.As(err, &e) }, `lacks the "Access: Groups Edit" permission`},
		{"missing permission on a rule phase", cffake.Injection{Method: "POST", Path: "/zones/" + cffake.ZoneID + "/rulesets", Status: 403, Code: 10000, Message: "Authentication error"},
			func(err error) bool { var e *PermissionError; return errors.As(err, &e) }, `lacks the "Zone WAF Edit" permission`},
		{"rejected body", cffake.Injection{Method: "POST", Path: acct + "/access/identity_providers", Status: 400, Code: 12130, Message: "config is invalid"},
			func(err error) bool { var e *RejectedError; return errors.As(err, &e) }, "config is invalid"},
		{"a create that returns another shape", cffake.Injection{Method: "POST", Path: acct + "/access/service_tokens", Apply: true,
			Result: map[string]any{"id": "st-x", "name": "officer-dashboard services"}},
			func(err error) bool { var e *ShapeError; return errors.As(err, &e) }, "Rotate its client secret"},
		{"a create whose read-back differs", cffake.Injection{Method: "POST", Path: acct + "/access/groups", Apply: true,
			Result: map[string]any{"id": "g-someone-else", "name": "Officers"}},
			func(err error) bool { var e *VerifyError; return errors.As(err, &e) }, "g-someone-else"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, cffake.Fresh())
			r.acct.Inject(c.in)
			_, err := r.apply(context.Background())
			if !c.want(err) || !strings.Contains(err.Error(), c.text) {
				t.Fatalf("err = %v\n%s", err, r.out.String())
			}
			// It stopped at the failing write.
			ws := r.acct.Writes()
			if last := ws[len(ws)-1]; last.Path != c.in.Path {
				t.Errorf("wrote %s after the failure", last.Path)
			}
			requireNoSecrets(t, r.acct, "the error", errText(err))
			// Every failure is safe to run again: the next run completes with no duplicate.
			if _, err := r.apply(context.Background()); err != nil {
				t.Fatalf("re-run: %v\n%s", err, r.out.String())
			}
			requireAllOK(t, r.preview())
			requireNoDuplicates(t, r.acct, r.d)
		})
	}
}

func TestOneFlakyReadDoesNotBreakVerification(t *testing.T) {
	// A live apps list answered 504 once; the read client retries reads (never writes).
	r := newRig(t, cffake.Fresh())
	reviewed := r.preview()
	// Two 504s in a row on the engine's own discovery; the third attempt succeeds.
	r.acct.Inject(cffake.Injection{Method: "GET", Path: "/accounts/" + cffake.AccountID + "/access/apps", Status: 504, Times: 2})
	e := NewEngine("test-token", r.d, r.sink, &r.out, r.opt, WithAPIBase(r.acct.URL), WithReadRetry(0, 0))
	if _, err := e.Apply(context.Background(), reviewed); err != nil {
		t.Fatal(err)
	}
}

// TestRequestBodiesMatchGolden pins every request body a fresh apply sends, with the API
// reference page it follows. go test -update rewrites the file; review the diff against
// the reference before committing it.
func TestRequestBodiesMatchGolden(t *testing.T) {
	r := newRig(t, cffake.Fresh())
	// The organization option is on, so its read-modify-write body is pinned too. Bot
	// Fight Mode is already off on this fixture; its body is pinned from the live shape
	// in apply-live-requests.txt (TestLiveRequestBodiesWithBothOptionsMatchGolden).
	r.opt.AllowOrgSessionWrite = true
	out, err := r.apply(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "apply-fresh-requests.txt", requestsText(t, r, out, "a fresh-account apply with the organization option on"))
}

// requestsText lists every write the run sent, in order, with the API reference page
// its body follows.
func requestsText(t *testing.T, r *rig, out Outputs, what string) string {
	t.Helper()
	ws := r.acct.Writes()
	if len(ws) != len(out.Writes) {
		t.Fatalf("%d writes, %d recorded", len(ws), len(out.Writes))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Every write %s sends, in order, with the API reference page its body follows.\n", what)
	b.WriteString("# Generated by go test -update. IDs are the fake's.\n")
	for i, w := range ws {
		if out.Writes[i].Method != w.Method || out.Writes[i].Path != w.Path {
			t.Fatalf("write %d: recorded %v, sent %s %s", i, out.Writes[i], w.Method, w.Path)
		}
		if !strings.HasPrefix(out.Writes[i].Ref, "https://developers.cloudflare.com/api/resources/") {
			t.Errorf("write %d has no API reference: %q", i, out.Writes[i].Ref)
		}
		var v any
		if strings.HasPrefix(w.ContentType, "multipart/form-data") {
			v = uploadSummary(t, r, w)
		} else if err := json.Unmarshal([]byte(w.Body), &v); err != nil {
			t.Fatalf("write %d body is not JSON: %v", i, err)
		}
		pretty, _ := json.MarshalIndent(v, "", "  ")
		header := ""
		if w.APIDate != "" {
			header = "# header: Cloudflare-Workers-Script-Api-Date: " + w.APIDate + "\n"
		}
		fmt.Fprintf(&b, "\n## %d. %s %s\n# ref: %s\n%s%s\n", i+1, w.Method, w.Path, out.Writes[i].Ref, header, pretty)
	}
	return b.String()
}

// uploadSummary is a Worker upload as the golden pins it: the metadata part as sent, with
// the bearer's text replaced by where it came from, and the module part's name, type and
// size (its content is the bundle, pinned by sinkbundle).
func uploadSummary(t *testing.T, r *rig, w cffake.Request) any {
	t.Helper()
	ups := r.acct.Uploads()
	if len(ups) != 1 {
		t.Fatalf("%d uploads", len(ups))
	}
	if !strings.Contains(w.Body, testBearer) {
		t.Fatal("the upload does not carry the host's bearer")
	}
	meta := ups[0].Metadata
	raw, _ := json.Marshal(meta)
	var cp map[string]any
	_ = json.Unmarshal(raw, &cp)
	for _, b := range cp["bindings"].([]any) {
		if m := b.(map[string]any); m["type"] == "secret_text" {
			if m["text"] != testBearer {
				t.Fatalf("secret_text %v is not the host's bearer", m["name"])
			}
			m["text"] = "<the value of secrets/backup/bearer>"
		}
	}
	return map[string]any{"multipart": map[string]any{"metadata": cp,
		"module": fmt.Sprintf("%s, application/javascript+module, %d bytes", meta["main_module"], len(ups[0].Module))}}
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test -update to create it)", err)
	}
	if norm := strings.ReplaceAll(string(want), "\r\n", "\n"); norm != got {
		t.Errorf("%s differs (go test -update rewrites it):\n--- got\n%s", name, got)
	}
}

func TestApplyPlanTextForTheLiveAccount(t *testing.T) {
	r := newRig(t, cffake.Live())
	actions := r.preview()
	o, err := Discover(context.Background(), newReader(r), r.d)
	if err != nil {
		t.Fatal(err)
	}
	ap := PlanApply(r.d, o, actions, r.opt, Facts{})
	var b strings.Builder
	if err := ap.WriteText(&b); err != nil {
		t.Fatal(err)
	}
	golden(t, "apply-plan-live.txt", b.String())
	if len(ap.Blockers) != 0 {
		t.Errorf("blockers %v", ap.Blockers)
	}
	for _, s := range ap.Steps {
		if s.Kind == StepRoute || s.Kind == StepDNS || s.Kind == StepTunnel {
			t.Errorf("live account: unexpected step %s", s.Title)
		}
	}
}

func newReader(r *rig) cfapi.Reader {
	return cfapi.New("test-token", cfapi.WithBaseURL(r.acct.URL), cfapi.WithRetry())
}
