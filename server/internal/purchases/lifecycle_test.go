package purchases

import (
	"reflect"
	"testing"
)

func TestLifecycle(t *testing.T) {
	yes := [][3]string{
		{ClubPurchase, "draft", "submitted"}, {ClubPurchase, "submitted", "approved"}, {ClubPurchase, "submitted", "denied"},
		{ClubPurchase, "fulfilling", "fulfilled"}, {ClubPurchase, "fulfilling", "approved"},
		{ClubPurchase, "fulfilled", "closed"}, {Reimbursement, "reimbursed", "closed"}, {Reimbursement, "approved", "reimbursed"},
	}
	for _, c := range yes {
		if !CanTransition(c[0], c[1], c[2]) {
			t.Errorf("%v should be allowed", c)
		}
	}
	no := [][3]string{
		{ClubPurchase, "draft", "approved"}, {Reimbursement, "submitted", "reimbursed"}, {Reimbursement, "approved", "fulfilling"},
	}
	for _, c := range no {
		if CanTransition(c[0], c[1], c[2]) {
			t.Errorf("%v should be refused", c)
		}
	}
	if got := NextStatuses(ClubPurchase, "approved"); !reflect.DeepEqual(got, []string{"fulfilling", "cancelled"}) {
		t.Errorf("club approved → %v", got)
	}
	if got := NextStatuses(Reimbursement, "approved"); !reflect.DeepEqual(got, []string{"reimbursed", "cancelled"}) {
		t.Errorf("reimbursement approved → %v", got)
	}
	for _, s := range []string{"denied", "closed", "cancelled"} {
		if got := NextStatuses(ClubPurchase, s); len(got) != 0 {
			t.Errorf("%s is final, got %v", s, got)
		}
	}
	if !IsEditable("draft") || IsEditable("submitted") {
		t.Error("only drafts are editable")
	}
}

func TestMaxAgentSettlement(t *testing.T) {
	d := DefaultTolerance
	cases := map[int64]int64{10_000: 12_500, 25_000: 27_500, 100_000: 110_000, 0: 2_500}
	for est, want := range cases {
		if got := MaxAgentSettlementCents(est, d); got != want {
			t.Errorf("max(%d) = %d want %d", est, got, want)
		}
	}
	tol := ToleranceFromEnv("5", "1000")
	if got := MaxAgentSettlementCents(100_000, tol); got != 105_000 {
		t.Errorf("5%% of $1000 = %d", got)
	}
	if got := MaxAgentSettlementCents(10_000, tol); got != 11_000 {
		t.Errorf("$10 flat = %d", got)
	}
	if got := ToleranceFromEnv("lots", "-3"); got != d {
		t.Errorf("invalid values should fall back, got %+v", got)
	}
}
