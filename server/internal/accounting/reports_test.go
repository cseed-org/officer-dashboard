package accounting

import (
	"strings"
	"testing"
)

func acct(id, code, name, typ string, part, line, restriction *string) Account {
	return Account{ID: id, Code: code, Name: name, Type: typ, Form990Part: part, Form990Line: line, Restriction: restriction, Active: true}
}

func baseData() *ReportData {
	d := &ReportData{
		Accounts: []Account{
			acct("cash", "1000", "Cash", "asset", ptr("X"), ptr("1"), nil),
			acct("net", "3000", "Net assets", "net_asset", ptr("X"), ptr("27"), ptr("without_donor")),
			acct("rev", "4000", "Gifts", "revenue", ptr("VIII"), ptr("1f"), nil),
			acct("exp", "5100", "Supplies", "expense", ptr("IX"), ptr("24a"), nil),
			acct("netr", "3100", "Restricted net assets", "net_asset", ptr("X"), ptr("28"), ptr("with_donor")),
		},
		Funds: []Fund{
			{ID: "g", Code: "GEN", Name: "General", Kind: "general", Restriction: "without_donor", Active: true},
			{ID: "r", Code: "R", Name: "Grant", Kind: "grant", Restriction: "with_donor", Active: true},
		},
		Budgets: []Budget{{AccountID: "exp", FiscalYear: 2027, AmountCents: 500}},
	}
	return d
}

func (d *ReportData) pair(date, source, a, af, b, bf string, n int64) {
	fc := func(id string) string {
		if id == "exp" {
			return "program"
		}
		return ""
	}
	d.Lines = append(d.Lines,
		ReportLine{EntryDate: date, Source: source, AccountID: a, FundID: af, DebitCents: n, FunctionalClass: fc(a)},
		ReportLine{EntryDate: date, Source: source, AccountID: b, FundID: bf, CreditCents: n, FunctionalClass: fc(b)})
}

var q = Query{FiscalYear: 2027, AsOf: "2026-10-31"}

func mustReport[T any](t *testing.T, name string, d *ReportData, qq Query) T {
	t.Helper()
	r, err := Make(name, d, qq)
	if err != nil {
		t.Fatal(err)
	}
	v, ok := r.(T)
	if !ok {
		t.Fatalf("report %s has type %T", name, r)
	}
	return v
}

func TestStatementsTieAcrossOpeningReleasesAndReversals(t *testing.T) {
	d := baseData()
	d.pair("2026-06-01", "opening_balance", "cash", "g", "net", "g", 100)
	d.pair("2026-08-01", "contribution", "cash", "r", "rev", "r", 1000)
	d.pair("2026-08-02", "release", "netr", "r", "net", "r", 200)
	d.pair("2026-08-03", "purchase_request", "exp", "r", "cash", "r", 120)
	d.pair("2026-08-04", "reversal", "cash", "r", "exp", "r", 20)

	pos := mustReport[*FinancialPosition](t, "statement-of-financial-position", d, q)
	if pos.Totals.AssetsCents != 1000 || pos.Totals.LiabilitiesCents != 0 || pos.Totals.NetAssetsCents != 1000 || !pos.Totals.Balanced {
		t.Errorf("position %+v", pos.Totals)
	}
	act := mustReport[*Activities](t, "statement-of-activities", d, q)
	if act.ChangeInNetAssets != (Split{WithoutDonorCents: 100, WithDonorCents: 800, TotalCents: 900}) {
		t.Errorf("change %+v", act.ChangeInNetAssets)
	}
	if act.NetAssetsEnding.TotalCents-act.NetAssetsBeginning.TotalCents != act.ChangeInNetAssets.TotalCents {
		t.Error("ending − beginning ≠ change")
	}
	if act.ReleasesCents != 200 {
		t.Errorf("releases %d", act.ReleasesCents)
	}
	tb := mustReport[*TrialBalance](t, "trial-balance", d, q)
	if tb.Totals.DebitCents != tb.Totals.CreditCents {
		t.Errorf("trial balance %+v", tb.Totals)
	}
	for _, name := range []string{"fund-balances", "budget-vs-actual", "statement-of-functional-expenses", "form-990"} {
		if _, err := Make(name, d, q); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	bal := mustReport[*FundBalances](t, "fund-balances", d, q)
	for _, row := range bal.Rows {
		if row.BeginningCents+row.AdditionsCents-row.ExpensesCents != row.EndingCents {
			t.Errorf("fund %s does not roll forward: %+v", row.Fund.ID, row)
		}
	}
	rollup := Rollup990(d, q)
	found := false
	for _, r := range rollup.Rows {
		if r.Part == "IX" {
			found = true
			if r.Line != "24a" || r.TotalCents != 100 || r.ProgramCents != 100 {
				t.Errorf("IX row %+v", r)
			}
		}
	}
	if !found {
		t.Error("no part IX row")
	}
}

func TestUnbudgetedFundSpendingAndPerFundBalance(t *testing.T) {
	d := baseData()
	d.pair("2026-06-01", "opening_balance", "cash", "g", "net", "g", 100)
	d.pair("2026-08-01", "contribution", "cash", "r", "rev", "r", 1000)
	d.pair("2026-08-02", "release", "netr", "r", "net", "r", 200)
	d.pair("2026-08-03", "purchase_request", "exp", "r", "cash", "r", 120)
	d.pair("2026-08-04", "reversal", "cash", "r", "exp", "r", 20)
	d.Budgets = []Budget{{AccountID: "exp", FundID: ptr("g"), FiscalYear: 2027, AmountCents: 500}}
	bva := mustReport[*BudgetVsActual](t, "budget-vs-actual", d, q)
	if bva.Totals.ActualCents != 100 {
		t.Errorf("actual %d", bva.Totals.ActualCents)
	}
	for _, fund := range []string{"g", "r"} {
		qq := q
		qq.FundID = fund
		if p := mustReport[*FinancialPosition](t, "statement-of-financial-position", d, qq); !p.Totals.Balanced {
			t.Errorf("fund %s unbalanced: %+v", fund, p.Totals)
		}
	}
}

func TestActivitiesFootPerClass(t *testing.T) {
	d := baseData()
	d.pair("2026-07-01", "opening_balance", "cash", "g", "net", "g", 3200)
	d.pair("2026-08-01", "contribution", "cash", "g", "rev", "g", 500)
	d.pair("2026-08-01", "contribution", "cash", "r", "rev", "r", 1000)
	d.pair("2026-08-03", "purchase_request", "exp", "r", "cash", "r", 300)
	d.pair("2026-08-03", "release", "netr", "r", "net", "r", 300)
	d.pair("2026-08-05", "purchase_request", "exp", "g", "cash", "g", 120)
	a := mustReport[*Activities](t, "statement-of-activities", d, q)
	sum := func(rows []ActivityRow) (without, with int64) {
		for _, r := range rows {
			without += r.WithoutDonorCents
			with += r.WithDonorCents
		}
		return
	}
	rw, rd := sum(a.Revenue)
	ew, ed := sum(a.Expenses)
	// Known-good: unrestricted 500 − 420 + 300 = 380; restricted 1000 − 300 = 700.
	if a.ChangeInNetAssets != (Split{380, 700, 1080}) {
		t.Errorf("change %+v", a.ChangeInNetAssets)
	}
	if a.NetAssetsBeginning != (Split{3200, 0, 3200}) {
		t.Errorf("beginning %+v", a.NetAssetsBeginning)
	}
	if rw-ew+a.ReleasesCents != a.ChangeInNetAssets.WithoutDonorCents || rd-ed-a.ReleasesCents != a.ChangeInNetAssets.WithDonorCents {
		t.Error("activities do not foot per class")
	}
	bal := mustReport[*FundBalances](t, "fund-balances", d, q)
	for _, r := range bal.Rows {
		if r.Fund.ID == "g" && (r.BeginningCents != 3200 || r.AdditionsCents != 500 || r.ExpensesCents != 120 || r.EndingCents != 3580) {
			t.Errorf("general fund %+v", r)
		}
	}
}

func TestForm990RollupTotalsMemosAndUnmapped(t *testing.T) {
	d := baseData()
	d.Accounts = append(d.Accounts,
		acct("inkind", "4400", "In-kind", "revenue", ptr("VIII"), ptr("1f"), nil),
		acct("misc", "5990", "Unmapped misc", "expense", nil, nil, nil),
		acct("other", "4900", "Unmapped revenue", "revenue", nil, nil, nil))
	add := func(account string, dr, cr int64, mut func(*ReportLine)) {
		l := ReportLine{EntryDate: "2026-08-01", Source: "contribution", AccountID: account, FundID: "g", DebitCents: dr, CreditCents: cr}
		if mut != nil {
			mut(&l)
		}
		d.Lines = append(d.Lines, l)
	}
	opening := func(l *ReportLine) { l.Source, l.EntryDate = "opening_balance", "2026-07-01" }
	add("cash", 3200, 0, opening)
	add("net", 0, 3200, opening)
	add("cash", 1000, 0, nil)
	add("rev", 0, 1000, nil)
	add("exp", 300, 0, func(l *ReportLine) { l.FunctionalClass, l.Noncash = "program", true })
	add("inkind", 0, 300, func(l *ReportLine) { l.Noncash = true })
	add("misc", 50, 0, func(l *ReportLine) { l.Source, l.FunctionalClass = "manual", "mgmt_general" })
	add("cash", 0, 50, func(l *ReportLine) { l.Source = "manual" })
	r := mustReport[*Form990](t, "form-990", d, q)
	want := map[string]Form990Row{
		"VIII:1f": {Part: "VIII", Line: "1f", TotalCents: 1300},
		"IX:24a":  {Part: "IX", Line: "24a", TotalCents: 300, ProgramCents: 300},
	}
	for _, row := range r.Rows {
		if w, ok := want[row.Part+":"+row.Line]; ok {
			if row != w {
				t.Errorf("row %+v want %+v", row, w)
			}
			delete(want, row.Part+":"+row.Line)
		}
	}
	if len(want) != 0 {
		t.Errorf("missing rows %v", want)
	}
	if r.Totals.PartVIIILine12Cents != 1300 || r.Totals.PartIXLine25.TotalCents != 300 || r.Totals.PartIXLine25.ProgramCents != 300 {
		t.Errorf("totals %+v", r.Totals)
	}
	if r.Line1gNoncashCents != 300 {
		t.Errorf("1g %d", r.Line1gNoncashCents)
	}
	if len(r.UnmappedAccounts) != 2 || r.UnmappedAccounts[0].Account.Code != "4900" || r.UnmappedAccounts[0].TotalCents != 0 ||
		r.UnmappedAccounts[1].Account.Code != "5990" || r.UnmappedAccounts[1].TotalCents != 50 {
		t.Errorf("unmapped %+v", r.UnmappedAccounts)
	}
}

func TestReversalOfOpeningBalanceIsBeginningNetAssets(t *testing.T) {
	d := baseData()
	add := func(account string, dr, cr int64, source, reverses string) {
		d.Lines = append(d.Lines, ReportLine{EntryDate: "2026-07-02", Source: source, ReversesSource: reverses, AccountID: account, FundID: "g", DebitCents: dr, CreditCents: cr})
	}
	add("cash", 3200, 0, "opening_balance", "")
	add("net", 0, 3200, "opening_balance", "")
	add("cash", 0, 200, "reversal", "opening_balance")
	add("net", 200, 0, "reversal", "opening_balance")
	a := mustReport[*Activities](t, "statement-of-activities", d, q)
	if a.NetAssetsBeginning.TotalCents != 3000 || a.ChangeInNetAssets.TotalCents != 0 {
		t.Errorf("beginning %+v change %+v", a.NetAssetsBeginning, a.ChangeInNetAssets)
	}
}

func TestReportCSVNeutralisesFormulasAndKeepsOrder(t *testing.T) {
	csv := CSV(map[string]any{"vendor": "=1+1", "memo": `A,"B"`})
	if !strings.Contains(csv, "'=1+1") || !strings.Contains(csv, `"A,""B"""`) {
		t.Errorf("csv %q", csv)
	}
	d := baseData()
	d.pair("2026-08-01", "contribution", "cash", "g", "rev", "g", 1000)
	r := mustReport[*TrialBalance](t, "trial-balance", d, q)
	out := CSV(r)
	lines := strings.Split(out, "\r\n")
	header := lines[0]
	if !strings.HasPrefix(header, `"fiscalYear","from","asOf","generatedAt","fundId","report","section","account.id"`) {
		t.Errorf("header %s", header)
	}
	if !strings.HasSuffix(out, "\r\n") || len(lines) < 5 {
		t.Errorf("rows %d", len(lines))
	}
	if !strings.Contains(out, `"trial-balance","rows","cash","1000","Cash","asset","1","1000","0"`) {
		t.Errorf("expected the cash row, got %s", out)
	}
}
