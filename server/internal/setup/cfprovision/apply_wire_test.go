package cfprovision

import (
	"context"
	"errors"
	"strings"
	"testing"

	"officerdashboard/internal/setup/cffake"
	"officerdashboard/internal/setup/plan"
)

// The command shows the person Preview's plan, asks, then runs ApplyReviewed with it.

func (r *rig) engine() *Engine {
	return NewEngine("test-token", r.d, r.sink, &r.out, r.opt, WithAPIBase(r.acct.URL), WithReadRetry())
}

func TestPreviewWritesNothingAndApplyReviewedRunsIt(t *testing.T) {
	r := newRig(t, cffake.Fresh())
	reviewed, err := r.engine().Preview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n := len(r.acct.Writes()); n != 0 {
		t.Fatalf("Preview sent %d writes", n)
	}
	if len(reviewed.Plan.Steps) == 0 || len(reviewed.Plan.Blockers) != 0 {
		t.Fatalf("steps %v, blockers %v", reviewed.Plan.Steps, reviewed.Plan.Blockers)
	}
	if _, err := r.engine().ApplyReviewed(context.Background(), reviewed); err != nil {
		t.Fatalf("%v\n%s", err, r.out.String())
	}
	requireAllOK(t, r.preview())
}

// The plan lines can stay the same while the steps change: here the host gains the
// tunnel token between the preview and the run, so the fetch step disappears. The run
// must not do something other than what the person confirmed.
func TestApplyReviewedStopsWhenTheStepsChanged(t *testing.T) {
	r := newRig(t, cffake.Complete())
	reviewed, err := r.engine().Preview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(reviewed.Plan.Steps) != 1 || reviewed.Plan.Steps[0].Kind != StepTunnelToken {
		t.Fatalf("want one fetch step, got %+v", reviewed.Plan.Steps)
	}
	_ = r.sink.Store(context.Background(), Secret{Name: SecretTunnelToken, ResourceID: cffake.TunnelID, Value: "kept-by-another-run-0123456789"})
	_, err = r.engine().ApplyReviewed(context.Background(), reviewed)
	var pc *PlanChangedError
	if !errors.As(err, &pc) || !strings.Contains(err.Error(), "fetch the token") {
		t.Fatalf("got %v, want a PlanChangedError naming the step", err)
	}
	if n := len(r.acct.Writes()); n != 0 {
		t.Fatalf("%d writes after a changed plan", n)
	}
}

// A sink that kept the secret, but beside an existing file the install still reads,
// stops the run with that explanation, never "lost", and before publication.
func TestSecretKeptAsideStopsTheRun(t *testing.T) {
	r := newRig(t, cffake.Fresh())
	r.sink.FailStore = map[SecretName]error{SecretTunnelToken: &NotInPlaceError{Detail: "it is in secrets/admin/tunnel-token.20261128T000000.000000000Z; secrets/tunnel-token is older"}}
	_, err := r.apply(context.Background())
	var nip *NotInPlaceError
	if !errors.As(err, &nip) {
		t.Fatalf("got %T %v, want *NotInPlaceError", err, err)
	}
	msg := err.Error()
	if nip.Secret != SecretTunnelToken || nip.ResourceID == "" || strings.Contains(msg, "lost") ||
		!strings.Contains(msg, "secrets/admin/tunnel-token.2026") || !strings.Contains(msg, "stopped") {
		t.Fatalf("message: %s", msg)
	}
	for _, w := range r.acct.Writes() {
		if strings.Contains(w.Path, "/configurations") || strings.Contains(w.Path, "/dns_records") {
			t.Fatalf("published after the secret was kept aside: %s %s", w.Method, w.Path)
		}
	}
	for _, s := range r.acct.Secrets() {
		if strings.Contains(msg+r.out.String(), s) {
			t.Fatal("a secret reached the output")
		}
	}
	m := byID(r.preview())
	if m["tunnel"].Kind != plan.OK || m["dns"].Kind != plan.Create {
		t.Fatalf("tunnel %s, dns %s", m["tunnel"].Kind, m["dns"].Kind)
	}
}
