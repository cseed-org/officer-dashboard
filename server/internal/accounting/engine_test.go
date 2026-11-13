package accounting

import (
	"strings"
	"testing"
)

func ptr(s string) *string { return &s }

func testContext() Context {
	return Context{
		Accounts: []Account{
			{ID: "cash", Type: "asset", Active: true},
			{ID: "rev", Type: "revenue", Active: true},
			{ID: "expense", Type: "expense", Active: true},
			{ID: "net", Type: "net_asset", Active: true, Restriction: ptr("without_donor")},
			{ID: "netr", Type: "net_asset", Active: true, Restriction: ptr("with_donor")},
		},
		Funds:   []Fund{{ID: "gen", Restriction: "without_donor", Active: true}, {ID: "grant", Restriction: "with_donor", Active: true}},
		Periods: []Period{{ID: "fy", StartDate: "2026-07-01", EndDate: "2027-06-30", Status: "open"}},
	}
}

func gift() Entry {
	return Entry{ID: "e", EntryDate: "2026-09-01", Memo: "Gift", Source: "contribution", Lines: []Line{
		{AccountID: "cash", FundID: "gen", DebitCents: 100},
		{AccountID: "rev", FundID: "gen", CreditCents: 100},
	}}
}

func TestPostAndReverseWithoutEditing(t *testing.T) {
	entry, err := PostEntry(gift(), testContext())
	if err != nil {
		t.Fatal(err)
	}
	rev, err := Reverse(entry, "r", "2026-09-02", testContext())
	if err != nil {
		t.Fatal(err)
	}
	if rev.ReversesEntryID != "e" || rev.Lines[0].CreditCents != 100 || entry.Lines[0].DebitCents != 100 {
		t.Errorf("reversal %+v original %+v", rev, entry)
	}
	ctx := testContext()
	ctx.ReversedEntries = []string{"e"}
	if _, err := Reverse(entry, "r2", "2026-09-02", ctx); err == nil {
		t.Error("an entry can be reversed once")
	}
}

func TestRejectsImbalanceUnsafeCentsAndPeriods(t *testing.T) {
	for _, amount := range []int64{99, 0, MaxSafe + 1, -5} {
		in := gift()
		in.Lines[1].CreditCents = amount
		if _, err := PostEntry(in, testContext()); err == nil {
			t.Errorf("credit %d should be rejected", amount)
		}
	}
	ctx := testContext()
	ctx.Periods = nil
	if _, err := PostEntry(gift(), ctx); err == nil || !strings.Contains(err.Error(), "open period") {
		t.Errorf("missing period: %v", err)
	}
	ctx = testContext()
	closed, _ := ClosePeriod(ctx.Periods[0], "closed")
	ctx.Periods = []Period{closed}
	if _, err := PostEntry(gift(), ctx); err == nil {
		t.Error("closed period must reject")
	}
	bad := gift()
	bad.EntryDate = "2026-02-30"
	if _, err := PostEntry(bad, testContext()); err == nil {
		t.Error("impossible date must be rejected")
	}
}

func TestExpenseNeedsFunctionalClass(t *testing.T) {
	in := gift()
	in.Lines[0].AccountID = "expense"
	if _, err := PostEntry(in, testContext()); err == nil || !strings.Contains(err.Error(), "functional") {
		t.Errorf("err %v", err)
	}
	in.Lines[0].FunctionalClass = "program"
	if _, err := PostEntry(in, testContext()); err != nil {
		t.Errorf("with class: %v", err)
	}
}

func TestPeriodsNeverReopen(t *testing.T) {
	open := Period{Status: "open"}
	closed, err := ClosePeriod(open, "closed")
	if err != nil || closed.Status != "closed" {
		t.Fatal(err)
	}
	if _, err := ClosePeriod(closed, "closed"); err == nil {
		t.Error("closed → closed is not a move")
	}
	locked, err := ClosePeriod(closed, "locked")
	if err != nil || locked.Status != "locked" {
		t.Fatal(err)
	}
	if _, err := ClosePeriod(locked, "locked"); err == nil {
		t.Error("locked never changes")
	}
}

func TestReleaseAndContribution(t *testing.T) {
	rel, err := ReleaseRestriction(Release{ID: "release", EntryDate: "2026-09-01", Memo: "Satisfied purpose", AmountCents: 50, FundID: "grant",
		RestrictedNetAssetID: "netr", UnrestrictedNetAssetID: "net", AvailableRestrictedCents: 100}, testContext())
	if err != nil {
		t.Fatal(err)
	}
	if rel.Lines[0].AccountID != "netr" || rel.Lines[1].AccountID != "net" || rel.Lines[0].FundID != "grant" || rel.Lines[1].FundID != "grant" {
		t.Errorf("release lines %+v", rel.Lines)
	}
	if _, err := ReleaseRestriction(Release{ID: "x", EntryDate: "2026-09-01", AmountCents: 101, FundID: "grant",
		RestrictedNetAssetID: "netr", UnrestrictedNetAssetID: "net", AvailableRestrictedCents: 100}, testContext()); err == nil || !strings.Contains(err.Error(), "release exceeds") {
		t.Errorf("over-release: %v", err)
	}
	if _, err := ReleaseRestriction(Release{ID: "x", EntryDate: "2026-09-01", AmountCents: 1, FundID: "gen",
		RestrictedNetAssetID: "netr", UnrestrictedNetAssetID: "net", AvailableRestrictedCents: 100}, testContext()); err == nil {
		t.Error("an unrestricted fund has nothing to release")
	}
	e := gift()
	e.Lines[0].DebitCents, e.Lines[1].CreditCents = 25000, 25000
	res, err := RecordContribution(Contribution{Entry: e, AmountCents: 25000, QuidProQuoCents: 8000, Kind: "in_kind", InKindDescription: "Equipment"}, testContext())
	if err != nil {
		t.Fatal(err)
	}
	if !res.AcknowledgmentRequired || !res.QuidProQuoDisclosureRequired || !res.InKind {
		t.Errorf("flags %+v", res)
	}
	if _, err := RecordContribution(Contribution{Entry: e, AmountCents: 25000, Kind: "in_kind"}, testContext()); err == nil {
		t.Error("in-kind needs a description")
	}
}

func TestValidateLines(t *testing.T) {
	L := func(d, c int64) Line { return Line{AccountID: "a", FundID: "f", DebitCents: d, CreditCents: c} }
	if total, errs := ValidateLines([]Line{L(500, 0), L(0, 500)}); errs != nil || total != 500 {
		t.Errorf("balanced: %d %v", total, errs)
	}
	if total, errs := ValidateLines([]Line{L(300, 0), L(200, 0), L(0, 500)}); errs != nil || total != 500 {
		t.Errorf("split: %d %v", total, errs)
	}
	_, errs := ValidateLines([]Line{L(500, 0), L(0, 499)})
	if !contains(errs, "debits exceed credits by 1 cents") {
		t.Errorf("imbalance: %v", errs)
	}
	_, errs = ValidateLines([]Line{L(500, 0)})
	if !contains(errs, "an entry needs at least two lines") {
		t.Errorf("one line: %v", errs)
	}
	_, errs = ValidateLines([]Line{L(5, 5), L(0, 0), L(-1, 0)})
	for _, want := range []string{
		"line 1: exactly one of debit or credit must be non-zero",
		"line 2: exactly one of debit or credit must be non-zero",
		"line 3: amounts must be non-negative integer cents",
	} {
		if !contains(errs, want) {
			t.Errorf("missing %q in %v", want, errs)
		}
	}
}

func contains(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

func TestContributionObligations(t *testing.T) {
	if !ContributionObligations(25000, 0).AcknowledgmentRequired || ContributionObligations(24999, 0).AcknowledgmentRequired {
		t.Error("$250 threshold")
	}
	if got := ContributionObligations(10000, 3000); got != (Obligations{false, true, 7000}) {
		t.Errorf("quid pro quo: %+v", got)
	}
	if ContributionObligations(7500, 2000).QuidProQuoDisclosureRequired || ContributionObligations(50000, 0).QuidProQuoDisclosureRequired {
		t.Error("no disclosure at exactly $75 or without goods")
	}
}
