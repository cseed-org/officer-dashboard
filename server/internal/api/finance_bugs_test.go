package api

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// M-113: an explicit asOf outside fiscalYear is clamped into that year, so FY2026 reports
// never include FY2027 activity.
func TestFinanceAsOfOutsideFiscalYearIsClamped(t *testing.T) {
	e := newEnv(t)
	d := e.director()
	for _, c := range []struct{ query, asOf string }{
		{"fiscalYear=2026&asOf=2026-10-31", "2026-06-30"},
		{"fiscalYear=2027&asOf=2026-01-15", "2026-07-01"},
		{"fiscalYear=2027&asOf=2027-12-01", "2027-06-30"},
		{"fiscalYear=2027&asOf=2026-10-31", "2026-10-31"},
	} {
		for _, report := range []string{"statement-of-functional-expenses", "form-990", "budget-vs-actual"} {
			r := e.mustOK(e.get("/api/finance/reports/"+report+"?"+c.query, d))
			if got := r.str("asOf"); got != c.asOf {
				t.Errorf("%s?%s asOf = %q, want %q", report, c.query, got, c.asOf)
			}
		}
		if got := e.mustOK(e.get("/api/finance/summary?"+c.query, d)).str("asOf"); got != c.asOf {
			t.Errorf("summary?%s asOf = %q, want %q", c.query, got, c.asOf)
		}
	}
	// The clamped FY2026 report equals the implicit FY2026 report: no FY2027 expenses leak in.
	explicit := e.mustOK(e.get("/api/finance/reports/statement-of-functional-expenses?fiscalYear=2026&asOf=2026-10-31", d))
	implicit := e.mustOK(e.get("/api/finance/reports/statement-of-functional-expenses?fiscalYear=2026", d))
	a, _ := json.Marshal(explicit.at("totals"))
	b, _ := json.Marshal(implicit.at("totals"))
	if string(a) != string(b) {
		t.Errorf("FY2026 totals with a FY2027 asOf %s, want %s", a, b)
	}
}

func (e *testEnv) committed(query string) int64 {
	e.t.Helper()
	return e.mustOK(e.get("/api/finance/summary?"+query, e.director())).num("totals", "committedCents")
}

// M-036: a request approved in June and still open in July counts as committed in the new
// fiscal year (its actual will post there), and moves to spent once settled.
func TestPriorYearOpenCommitmentCountsInNewFiscalYear(t *testing.T) {
	e := newEnv(t)
	baseCommitted := e.committed("fiscalYear=2027")
	baseSpent := e.mustOK(e.get("/api/finance/summary?fiscalYear=2027", e.director())).num("totals", "spentCents")
	e.clock = time.Date(2026, 6, 20, 18, 0, 0, 0, time.UTC)
	id := e.mustOK(e.post("/api/requests", with(purchase, "submit", true), e.officer())).str("request", "id")
	e.mustOK(e.post("/api/requests/"+id+"/approve", map[string]any{}, e.director()))
	e.clock = testNow
	if got := e.committed("fiscalYear=2027"); got != baseCommitted+10_000 {
		t.Fatalf("FY2027 committed = %d, want %d (June approval still open)", got, baseCommitted+10_000)
	}
	// FY2026's own report (clamped to Jun 30) also still shows it.
	if got := e.committed("fiscalYear=2026"); got < 10_000 {
		t.Errorf("FY2026 committed = %d, want it to include the open request", got)
	}
	e.mustOK(e.post("/api/requests/"+id+"/transition", map[string]any{"to": "fulfilling"}, e.director()))
	e.mustOK(e.post("/api/requests/"+id+"/transition", map[string]any{"to": "fulfilled", "actualTotalCents": 9_500}, e.director()))
	after := e.mustOK(e.get("/api/finance/summary?fiscalYear=2027", e.director()))
	if got := after.num("totals", "committedCents"); got != baseCommitted {
		t.Errorf("committed after settlement = %d, want %d", got, baseCommitted)
	}
	if got := after.num("totals", "spentCents"); got != baseSpent+9_500 {
		t.Errorf("spent after settlement = %d, want %d", got, baseSpent+9_500)
	}
}

// M-047: sending a request back from buying to approved keeps its decision date, and a
// report dated between approval and send-back still counts the commitment.
func TestSendBackKeepsDecisionDateAndHistoricalCommitment(t *testing.T) {
	e := newEnv(t)
	base := e.committed("fiscalYear=2027&asOf=2026-10-10")
	e.clock = time.Date(2026, 10, 5, 18, 0, 0, 0, time.UTC)
	approvedAt := e.clock
	id := e.mustOK(e.post("/api/requests", with(purchase, "submit", true), e.officer())).str("request", "id")
	e.mustOK(e.post("/api/requests/"+id+"/approve", map[string]any{}, e.director()))
	e.clock = time.Date(2026, 10, 6, 18, 0, 0, 0, time.UTC)
	e.mustOK(e.post("/api/requests/"+id+"/transition", map[string]any{"to": "fulfilling"}, e.director()))
	e.clock = time.Date(2026, 10, 20, 18, 0, 0, 0, time.UTC)
	e.mustOK(e.post("/api/requests/"+id+"/transition", map[string]any{"to": "approved"}, e.director()))
	e.clock = testNow
	if got := e.committed("fiscalYear=2027&asOf=2026-10-10"); got != base+10_000 {
		t.Errorf("committed at Oct 10 = %d, want %d", got, base+10_000)
	}
	got := e.mustOK(e.get("/api/requests/"+id, e.officer())).str("request", "decidedAt")
	if at, err := time.Parse(time.RFC3339, got); err != nil || !at.Equal(approvedAt) {
		t.Errorf("decidedAt = %q, want %s", got, approvedAt.Format(time.RFC3339))
	}
}

// M-042: a settlement committing while a report is between its ledger and request reads
// must leave the report showing either the whole commitment or the whole expense.
func TestReportReadsOneSnapshotAcrossConcurrentSettlement(t *testing.T) {
	e := newEnv(t)
	id := e.mustOK(e.post("/api/requests", with(purchase, "submit", true), e.officer())).str("request", "id")
	e.mustOK(e.post("/api/requests/"+id+"/approve", map[string]any{}, e.director()))
	before := e.mustOK(e.get("/api/finance/summary?fiscalYear=2027", e.director()))
	spent0, committed0 := before.num("totals", "spentCents"), before.num("totals", "committedCents")

	paused, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	afterReportLines = func() { once.Do(func() { close(paused); <-resume }) }
	defer func() { afterReportLines = nil }()
	director := e.director()
	done := make(chan reply)
	go func() { done <- e.get("/api/finance/summary?fiscalYear=2027", director) }()
	<-paused
	e.mustOK(e.post("/api/requests/"+id+"/transition", map[string]any{"to": "fulfilling"}, e.director()))
	e.mustOK(e.post("/api/requests/"+id+"/transition", map[string]any{"to": "fulfilled", "actualTotalCents": 9_500}, e.director()))
	close(resume)
	mid := <-done
	if mid.Status != 200 {
		t.Fatalf("summary %d %s", mid.Status, mid.Raw)
	}
	spent, committed := mid.num("totals", "spentCents"), mid.num("totals", "committedCents")
	oldState := spent == spent0 && committed == committed0
	newState := spent == spent0+9_500 && committed == committed0-10_000
	if !oldState && !newState {
		t.Fatalf("mixed snapshot: spent %d committed %d (before: %d/%d)", spent, committed, spent0, committed0)
	}
	next := e.mustOK(e.get("/api/finance/summary?fiscalYear=2027", e.director()))
	if next.num("totals", "spentCents") != spent0+9_500 || next.num("totals", "committedCents") != committed0-10_000 {
		t.Errorf("next report misses the settlement: %s", next.Raw)
	}
}
