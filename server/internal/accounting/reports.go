package accounting

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"officerdashboard/internal/fiscal"
)

// ReportLine is one posted journal line with the entry facts reports need.
type ReportLine struct {
	AccountID       string
	FundID          string
	EntryDate       string
	Source          string
	ReversesSource  string // source of the entry this one reverses, if any
	DebitCents      int64
	CreditCents     int64
	FunctionalClass string
	Noncash         bool // part of a noncash (in-kind or stock) contribution; feeds 990 line 1g
}

// Budget is one budget line.
type Budget struct {
	AccountID   string
	FundID      *string
	FiscalYear  int
	AmountCents int64
}

// Commitment is an approved-but-unposted purchase (encumbrance).
type Commitment struct {
	ExpenseAccountID string
	FundID           string
	TotalCents       int64
	SubmittedDate    string
}

// ReportData is everything reports are computed from.
type ReportData struct {
	Accounts    []Account
	Funds       []Fund
	Lines       []ReportLine
	Budgets     []Budget
	Commitments []Commitment
}

// Query selects a fiscal year, an as-of date and optionally one fund.
type Query struct {
	FiscalYear  int
	AsOf        string
	FundID      string
	GeneratedAt string // ISO; defaults to now
}

// ─── JSON shapes (field order = the contract's key order, which drives CSV columns) ───

type AccountRef struct {
	ID          string  `json:"id"`
	Code        string  `json:"code"`
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	Form990Line *string `json:"form990Line"`
}

type AccountBrief struct {
	ID   string `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

type FundBrief struct {
	ID   string `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

type FundOption struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Restriction string `json:"restriction"`
}

type ReportMeta struct {
	FiscalYear  int     `json:"fiscalYear"`
	From        string  `json:"from"`
	AsOf        string  `json:"asOf"`
	GeneratedAt string  `json:"generatedAt"`
	FundID      *string `json:"fundId"`
}

type Split struct {
	WithoutDonorCents int64 `json:"withoutDonorCents"`
	WithDonorCents    int64 `json:"withDonorCents"`
	TotalCents        int64 `json:"totalCents"`
}

type TBRow struct {
	Account     AccountRef `json:"account"`
	DebitCents  int64      `json:"debitCents"`
	CreditCents int64      `json:"creditCents"`
}
type TrialBalance struct {
	ReportMeta
	Report string  `json:"report"`
	Rows   []TBRow `json:"rows"`
	Totals struct {
		DebitCents  int64 `json:"debitCents"`
		CreditCents int64 `json:"creditCents"`
	} `json:"totals"`
}

type ActivityRow struct {
	Account           AccountRef `json:"account"`
	WithoutDonorCents int64      `json:"withoutDonorCents"`
	WithDonorCents    int64      `json:"withDonorCents"`
	TotalCents        int64      `json:"totalCents"`
}
type Activities struct {
	ReportMeta
	Report             string        `json:"report"`
	Revenue            []ActivityRow `json:"revenue"`
	ReleasesCents      int64         `json:"releasesCents"`
	Expenses           []ActivityRow `json:"expenses"`
	ChangeInNetAssets  Split         `json:"changeInNetAssets"`
	NetAssetsBeginning Split         `json:"netAssetsBeginning"`
	NetAssetsEnding    Split         `json:"netAssetsEnding"`
}

type BalanceRow struct {
	Account      AccountRef `json:"account"`
	BalanceCents int64      `json:"balanceCents"`
}
type FinancialPosition struct {
	ReportMeta
	Report      string       `json:"report"`
	Assets      []BalanceRow `json:"assets"`
	Liabilities []BalanceRow `json:"liabilities"`
	NetAssets   struct {
		WithoutDonorCents int64 `json:"withoutDonorCents"`
		WithDonorCents    int64 `json:"withDonorCents"`
	} `json:"netAssets"`
	Totals struct {
		AssetsCents      int64 `json:"assetsCents"`
		LiabilitiesCents int64 `json:"liabilitiesCents"`
		NetAssetsCents   int64 `json:"netAssetsCents"`
		Balanced         bool  `json:"balanced"`
	} `json:"totals"`
}

type FunctionalRow struct {
	Account          AccountRef `json:"account"`
	ProgramCents     int64      `json:"programCents"`
	MgmtGeneralCents int64      `json:"mgmtGeneralCents"`
	FundraisingCents int64      `json:"fundraisingCents"`
	TotalCents       int64      `json:"totalCents"`
}
type FunctionalExpenses struct {
	ReportMeta
	Report string          `json:"report"`
	Rows   []FunctionalRow `json:"rows"`
	Totals struct {
		ProgramCents     int64 `json:"programCents"`
		MgmtGeneralCents int64 `json:"mgmtGeneralCents"`
		FundraisingCents int64 `json:"fundraisingCents"`
		TotalCents       int64 `json:"totalCents"`
	} `json:"totals"`
}

type BudgetRow struct {
	Account        AccountRef `json:"account"`
	Fund           *FundBrief `json:"fund"`
	BudgetCents    int64      `json:"budgetCents"`
	ActualCents    int64      `json:"actualCents"`
	CommittedCents int64      `json:"committedCents"`
	VarianceCents  int64      `json:"varianceCents"`
	UsedRate       *float64   `json:"usedRate"`
}
type BudgetVsActual struct {
	ReportMeta
	Report string      `json:"report"`
	Rows   []BudgetRow `json:"rows"`
	Totals struct {
		BudgetCents    int64 `json:"budgetCents"`
		ActualCents    int64 `json:"actualCents"`
		CommittedCents int64 `json:"committedCents"`
		VarianceCents  int64 `json:"varianceCents"`
	} `json:"totals"`
}

type FundBalanceRow struct {
	Fund                    FundOption `json:"fund"`
	BeginningCents          int64      `json:"beginningCents"`
	AdditionsCents          int64      `json:"additionsCents"`
	ReleasesCents           int64      `json:"releasesCents"`
	ExpensesCents           int64      `json:"expensesCents"`
	EndingCents             int64      `json:"endingCents"`
	RestrictedEndingCents   int64      `json:"restrictedEndingCents"`
	UnrestrictedEndingCents int64      `json:"unrestrictedEndingCents"`
}
type FundBalances struct {
	ReportMeta
	Report string           `json:"report"`
	Rows   []FundBalanceRow `json:"rows"`
}

type Form990Row struct {
	Part             string `json:"part"`
	Line             string `json:"line"`
	TotalCents       int64  `json:"totalCents"`
	ProgramCents     int64  `json:"programCents"`
	MgmtGeneralCents int64  `json:"mgmtGeneralCents"`
	FundraisingCents int64  `json:"fundraisingCents"`
}
type Form990Unmapped struct {
	Account    AccountRef `json:"account"`
	TotalCents int64      `json:"totalCents"`
}
type Form990Totals struct {
	PartVIIILine12Cents int64 `json:"partVIIILine12Cents"`
	PartIXLine25        struct {
		TotalCents       int64 `json:"totalCents"`
		ProgramCents     int64 `json:"programCents"`
		MgmtGeneralCents int64 `json:"mgmtGeneralCents"`
		FundraisingCents int64 `json:"fundraisingCents"`
	} `json:"partIXLine25"`
}
type Form990Rollup struct {
	Rows               []Form990Row      `json:"rows"`
	Totals             Form990Totals     `json:"totals"`
	Line1gNoncashCents int64             `json:"line1gNoncashCents"`
	UnmappedAccounts   []Form990Unmapped `json:"unmappedAccounts"`
}
type Form990 struct {
	ReportMeta
	Report string `json:"report"`
	Form990Rollup
}

// Ref converts an account to its report reference.
func (a Account) Ref() AccountRef {
	return AccountRef{ID: a.ID, Code: a.Code, Name: a.Name, Type: a.Type, Form990Line: a.Form990Line}
}

// Brief converts an account to the id/code/name triple.
func (a Account) Brief() AccountBrief { return AccountBrief{ID: a.ID, Code: a.Code, Name: a.Name} }

// Option converts a fund to its dropdown shape.
func (f Fund) Option() FundOption {
	return FundOption{ID: f.ID, Code: f.Code, Name: f.Name, Kind: f.Kind, Restriction: f.Restriction}
}

// Brief converts a fund to the id/code/name triple.
func (f Fund) Brief() FundBrief { return FundBrief{ID: f.ID, Code: f.Code, Name: f.Name} }

type calc struct {
	d       *ReportData
	q       Query
	from    string
	meta    ReportMeta
	acct    map[string]*Account
	fund    map[string]*Fund
	all     []ReportLine
	current []ReportLine
}

func (c *calc) isOpening(l ReportLine) bool {
	return l.EntryDate < c.from || l.Source == "opening_balance" || l.ReversesSource == "opening_balance"
}

func amount(l ReportLine) int64 { return l.DebitCents - l.CreditCents }

func sumLines(lines []ReportLine, keep func(ReportLine) bool, f func(ReportLine) int64) int64 {
	var n int64
	for _, l := range lines {
		if keep == nil || keep(l) {
			n += f(l)
		}
	}
	return n
}

func (c *calc) withDonor(l ReportLine) bool {
	a := c.acct[l.AccountID]
	if a == nil {
		return false
	}
	if a.Type == "expense" {
		return false
	}
	if a.Type == "revenue" && a.Form990Part != nil && *a.Form990Part == "VIII" && a.Form990Line != nil && strings.HasPrefix(*a.Form990Line, "2") {
		return false
	}
	if a.Type == "net_asset" && a.Restriction != nil && *a.Restriction != "" {
		return *a.Restriction == "with_donor"
	}
	f := c.fund[l.FundID]
	return f != nil && f.Restriction == "with_donor"
}

func (c *calc) balance(lines []ReportLine, accountID string) int64 {
	return sumLines(lines, func(l ReportLine) bool { return l.AccountID == accountID }, amount)
}

func (c *calc) netAssets(lines []ReportLine) Split {
	var r Split
	for _, l := range lines {
		a := c.acct[l.AccountID]
		if a == nil || (a.Type != "net_asset" && a.Type != "revenue" && a.Type != "expense") {
			continue
		}
		if c.withDonor(l) {
			r.WithDonorCents -= amount(l)
		} else {
			r.WithoutDonorCents -= amount(l)
		}
	}
	r.TotalCents = r.WithDonorCents + r.WithoutDonorCents
	return r
}

func isReleaseish(l ReportLine) bool {
	return l.Source == "release" || (l.Source == "reversal" && l.ReversesSource == "release")
}

func newCalc(d *ReportData, q Query) *calc {
	from, _ := fiscal.YearRange(q.FiscalYear, fiscal.DefaultStartMonth)
	gen := q.GeneratedAt
	if gen == "" {
		gen = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	}
	c := &calc{d: d, q: q, from: from, acct: map[string]*Account{}, fund: map[string]*Fund{}}
	for i := range d.Accounts {
		c.acct[d.Accounts[i].ID] = &d.Accounts[i]
	}
	for i := range d.Funds {
		c.fund[d.Funds[i].ID] = &d.Funds[i]
	}
	var fundID *string
	if q.FundID != "" {
		v := q.FundID
		fundID = &v
	}
	c.meta = ReportMeta{FiscalYear: q.FiscalYear, From: from, AsOf: q.AsOf, GeneratedAt: gen, FundID: fundID}
	for _, l := range d.Lines {
		if l.EntryDate <= q.AsOf && (q.FundID == "" || l.FundID == q.FundID) {
			c.all = append(c.all, l)
			if !c.isOpening(l) {
				c.current = append(c.current, l)
			}
		}
	}
	return c
}

// Make builds the named report. The result is a pointer to one of the report structs.
func Make(report string, d *ReportData, q Query) (any, error) {
	c := newCalc(d, q)
	switch report {
	case "form-990":
		return &Form990{ReportMeta: c.meta, Report: report, Form990Rollup: Rollup990(d, q)}, nil
	case "trial-balance":
		return c.trialBalance(), nil
	case "statement-of-financial-position":
		return c.position(), nil
	case "statement-of-activities":
		return c.activities(), nil
	case "statement-of-functional-expenses":
		return c.functional(), nil
	case "fund-balances":
		return c.fundBalances(), nil
	case "budget-vs-actual":
		return c.budgetVsActual(), nil
	}
	return nil, fmt.Errorf("unknown report %q", report)
}

func (c *calc) trialBalance() *TrialBalance {
	r := &TrialBalance{ReportMeta: c.meta, Report: "trial-balance", Rows: []TBRow{}}
	for _, a := range c.d.Accounts {
		n := c.balance(c.all, a.ID)
		row := TBRow{Account: a.Ref()}
		if n > 0 {
			row.DebitCents = n
		} else {
			row.CreditCents = -n
		}
		r.Rows = append(r.Rows, row)
		r.Totals.DebitCents += row.DebitCents
		r.Totals.CreditCents += row.CreditCents
	}
	return r
}

func (c *calc) position() *FinancialPosition {
	r := &FinancialPosition{ReportMeta: c.meta, Report: "statement-of-financial-position", Assets: []BalanceRow{}, Liabilities: []BalanceRow{}}
	var assets, liabilities int64
	for _, a := range c.d.Accounts {
		switch a.Type {
		case "asset":
			b := c.balance(c.all, a.ID)
			r.Assets = append(r.Assets, BalanceRow{Account: a.Ref(), BalanceCents: b})
			assets += b
		case "liability":
			b := -c.balance(c.all, a.ID)
			r.Liabilities = append(r.Liabilities, BalanceRow{Account: a.Ref(), BalanceCents: b})
			liabilities += b
		}
	}
	net := c.netAssets(c.all)
	r.NetAssets.WithoutDonorCents, r.NetAssets.WithDonorCents = net.WithoutDonorCents, net.WithDonorCents
	r.Totals.AssetsCents, r.Totals.LiabilitiesCents, r.Totals.NetAssetsCents = assets, liabilities, net.TotalCents
	r.Totals.Balanced = assets == liabilities+net.TotalCents
	return r
}

func (c *calc) activities() *Activities {
	r := &Activities{ReportMeta: c.meta, Report: "statement-of-activities", Revenue: []ActivityRow{}, Expenses: []ActivityRow{}}
	activity := func(typ string, sign int64) []ActivityRow {
		rows := []ActivityRow{}
		for _, a := range c.d.Accounts {
			if a.Type != typ {
				continue
			}
			row := ActivityRow{Account: a.Ref()}
			for _, l := range c.current {
				if l.AccountID != a.ID {
					continue
				}
				if c.withDonor(l) {
					row.WithDonorCents += sign * amount(l)
				} else {
					row.WithoutDonorCents += sign * amount(l)
				}
			}
			row.TotalCents = row.WithoutDonorCents + row.WithDonorCents
			rows = append(rows, row)
		}
		return rows
	}
	r.Revenue, r.Expenses = activity("revenue", -1), activity("expense", 1)
	var opening []ReportLine
	for _, l := range c.all {
		if c.isOpening(l) {
			opening = append(opening, l)
		}
	}
	r.NetAssetsBeginning, r.NetAssetsEnding = c.netAssets(opening), c.netAssets(c.all)
	r.ReleasesCents = sumLines(c.current, func(l ReportLine) bool {
		a := c.acct[l.AccountID]
		return isReleaseish(l) && !c.withDonor(l) && a != nil && a.Type == "net_asset"
	}, func(l ReportLine) int64 { return -amount(l) })
	r.ChangeInNetAssets = Split{
		WithoutDonorCents: r.NetAssetsEnding.WithoutDonorCents - r.NetAssetsBeginning.WithoutDonorCents,
		WithDonorCents:    r.NetAssetsEnding.WithDonorCents - r.NetAssetsBeginning.WithDonorCents,
	}
	r.ChangeInNetAssets.TotalCents = r.ChangeInNetAssets.WithoutDonorCents + r.ChangeInNetAssets.WithDonorCents
	return r
}

func (c *calc) functional() *FunctionalExpenses {
	r := &FunctionalExpenses{ReportMeta: c.meta, Report: "statement-of-functional-expenses", Rows: []FunctionalRow{}}
	for _, a := range c.d.Accounts {
		if a.Type != "expense" {
			continue
		}
		var lines []ReportLine
		for _, l := range c.current {
			if l.AccountID == a.ID {
				lines = append(lines, l)
			}
		}
		n := func(class string) int64 {
			return sumLines(lines, func(l ReportLine) bool { return l.FunctionalClass == class }, amount)
		}
		row := FunctionalRow{Account: a.Ref(), ProgramCents: n("program"), MgmtGeneralCents: n("mgmt_general"), FundraisingCents: n("fundraising"), TotalCents: sumLines(lines, nil, amount)}
		r.Rows = append(r.Rows, row)
		r.Totals.ProgramCents += row.ProgramCents
		r.Totals.MgmtGeneralCents += row.MgmtGeneralCents
		r.Totals.FundraisingCents += row.FundraisingCents
		r.Totals.TotalCents += row.TotalCents
	}
	return r
}

func (c *calc) fundBalances() *FundBalances {
	r := &FundBalances{ReportMeta: c.meta, Report: "fund-balances", Rows: []FundBalanceRow{}}
	for _, f := range c.d.Funds {
		if c.q.FundID != "" && f.ID != c.q.FundID {
			continue
		}
		var lines, opening, period []ReportLine
		for _, l := range c.all {
			if l.FundID != f.ID {
				continue
			}
			lines = append(lines, l)
			if c.isOpening(l) {
				opening = append(opening, l)
			} else {
				period = append(period, l)
			}
		}
		typeOf := func(l ReportLine) string {
			if a := c.acct[l.AccountID]; a != nil {
				return a.Type
			}
			return ""
		}
		end := c.netAssets(lines)
		r.Rows = append(r.Rows, FundBalanceRow{
			Fund:           f.Option(),
			BeginningCents: c.netAssets(opening).TotalCents,
			AdditionsCents: sumLines(period, func(l ReportLine) bool {
				t := typeOf(l)
				return (t == "revenue" || t == "net_asset") && !isReleaseish(l)
			}, func(l ReportLine) int64 { return -amount(l) }),
			ReleasesCents: sumLines(period, func(l ReportLine) bool {
				return isReleaseish(l) && typeOf(l) == "net_asset" && c.withDonor(l)
			}, amount),
			ExpensesCents:           sumLines(period, func(l ReportLine) bool { return typeOf(l) == "expense" }, amount),
			EndingCents:             end.TotalCents,
			RestrictedEndingCents:   end.WithDonorCents,
			UnrestrictedEndingCents: end.WithoutDonorCents,
		})
	}
	return r
}

func (c *calc) committed(accountID string, fundID string, queryFund string) int64 {
	var n int64
	for _, cm := range c.d.Commitments {
		if cm.ExpenseAccountID == accountID && (fundID == "" || cm.FundID == fundID) &&
			(queryFund == "" || cm.FundID == queryFund) && cm.SubmittedDate >= c.from && cm.SubmittedDate <= c.q.AsOf {
			n += cm.TotalCents
		}
	}
	return n
}

func (c *calc) budgetVsActual() *BudgetVsActual {
	r := &BudgetVsActual{ReportMeta: c.meta, Report: "budget-vs-actual", Rows: []BudgetRow{}}
	// Fund-specific and all-fund budgets may coexist for different accounts; overlaps are rejected on write.
	var budgets []Budget
	for _, b := range c.d.Budgets {
		if b.FiscalYear == c.q.FiscalYear && (c.q.FundID == "" || b.FundID == nil || *b.FundID == c.q.FundID) {
			budgets = append(budgets, b)
		}
	}
	for _, b := range budgets {
		bf := ""
		if b.FundID != nil {
			bf = *b.FundID
		}
		actual := sumLines(c.current, func(l ReportLine) bool {
			return l.AccountID == b.AccountID && (bf == "" || l.FundID == bf)
		}, amount)
		committed := c.committed(b.AccountID, bf, c.q.FundID)
		row := BudgetRow{BudgetCents: b.AmountCents, ActualCents: actual, CommittedCents: committed, VarianceCents: b.AmountCents - actual - committed}
		if a := c.acct[b.AccountID]; a != nil {
			row.Account = a.Ref()
		}
		if bf != "" {
			if f := c.fund[bf]; f != nil {
				brief := f.Brief()
				row.Fund = &brief
			}
		}
		if b.AmountCents != 0 {
			rate := float64(actual) / float64(b.AmountCents)
			if rate < 0 {
				rate = 0
			}
			row.UsedRate = &rate
		}
		r.Rows = append(r.Rows, row)
	}
	// Include each unbudgeted account/fund combination, even when another fund has a budget.
	for _, a := range c.d.Accounts {
		if a.Type != "expense" {
			continue
		}
		for _, f := range c.d.Funds {
			if c.q.FundID != "" && f.ID != c.q.FundID {
				continue
			}
			budgeted := false
			for _, b := range budgets {
				if b.AccountID == a.ID && (b.FundID == nil || *b.FundID == f.ID) {
					budgeted = true
				}
			}
			if budgeted {
				continue
			}
			var fl []ReportLine
			for _, l := range c.current {
				if l.FundID == f.ID {
					fl = append(fl, l)
				}
			}
			actual := c.balance(fl, a.ID)
			committed := c.committed(a.ID, f.ID, "")
			if actual != 0 || committed != 0 {
				brief := f.Brief()
				r.Rows = append(r.Rows, BudgetRow{Account: a.Ref(), Fund: &brief, VarianceCents: -actual - committed, ActualCents: actual, CommittedCents: committed})
			}
		}
	}
	for _, row := range r.Rows {
		r.Totals.BudgetCents += row.BudgetCents
		r.Totals.ActualCents += row.ActualCents
		r.Totals.CommittedCents += row.CommittedCents
		r.Totals.VarianceCents += row.VarianceCents
	}
	return r
}

// naturalLess compares account codes like "5990" < "10000" (numeric runs numerically).
func naturalLess(a, b string) bool {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if isDigit(a[i]) && isDigit(b[j]) {
			si := i
			for i < len(a) && isDigit(a[i]) {
				i++
			}
			sj := j
			for j < len(b) && isDigit(b[j]) {
				j++
			}
			na, nb := strings.TrimLeft(a[si:i], "0"), strings.TrimLeft(b[sj:j], "0")
			if len(na) != len(nb) {
				return len(na) < len(nb)
			}
			if na != nb {
				return na < nb
			}
			continue
		}
		if a[i] != b[j] {
			return a[i] < b[j]
		}
		i++
		j++
	}
	return len(a)-i < len(b)-j
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func isNoncashLine(line string) bool {
	return len(line) == 2 && line[0] == '1' && line[1] >= 'a' && line[1] <= 'f'
}

// Rollup990 groups mapped revenue/expense accounts; functional columns mirror Part IX.
// Totals cover mapped lines only; unmapped accounts are listed so the gap stays visible
// instead of silently understating line 12 or line 25.
func Rollup990(d *ReportData, q Query) Form990Rollup {
	from, _ := fiscal.YearRange(q.FiscalYear, fiscal.DefaultStartMonth)
	acct := map[string]*Account{}
	for i := range d.Accounts {
		acct[d.Accounts[i].ID] = &d.Accounts[i]
	}
	mapped := func(a *Account) bool {
		return (a.Type == "revenue" && a.Form990Part != nil && *a.Form990Part == "VIII" && a.Form990Line != nil && *a.Form990Line != "") ||
			(a.Type == "expense" && a.Form990Part != nil && *a.Form990Part == "IX" && a.Form990Line != nil && *a.Form990Line != "")
	}
	groups := map[string]*Form990Row{}
	var order []string
	unmapped := map[string]int64{}
	var line1g int64
	for i := range d.Accounts {
		a := &d.Accounts[i]
		if (a.Type == "revenue" || a.Type == "expense") && !mapped(a) {
			unmapped[a.ID] = 0
		}
	}
	for _, l := range d.Lines {
		if l.EntryDate < from || l.EntryDate > q.AsOf || l.Source == "opening_balance" || (q.FundID != "" && l.FundID != q.FundID) {
			continue
		}
		a := acct[l.AccountID]
		if a == nil || (a.Type != "revenue" && a.Type != "expense") {
			continue
		}
		n := amount(l)
		if a.Type == "revenue" {
			n = -n
		}
		if !mapped(a) {
			unmapped[a.ID] += n
			continue
		}
		if a.Type == "revenue" && l.Noncash && isNoncashLine(*a.Form990Line) {
			line1g += n
		}
		key := *a.Form990Part + ":" + *a.Form990Line
		row := groups[key]
		if row == nil {
			row = &Form990Row{Part: *a.Form990Part, Line: *a.Form990Line}
			groups[key] = row
			order = append(order, key)
		}
		row.TotalCents += n
		if a.Type == "expense" {
			switch l.FunctionalClass {
			case "program":
				row.ProgramCents += n
			case "mgmt_general":
				row.MgmtGeneralCents += n
			case "fundraising":
				row.FundraisingCents += n
			}
		}
	}
	out := Form990Rollup{Rows: []Form990Row{}, Line1gNoncashCents: line1g, UnmappedAccounts: []Form990Unmapped{}}
	for _, k := range order {
		out.Rows = append(out.Rows, *groups[k])
	}
	sort.SliceStable(out.Rows, func(i, j int) bool {
		a, b := out.Rows[i], out.Rows[j]
		if a.Part != b.Part {
			return a.Part == "VIII"
		}
		return naturalLess(a.Line, b.Line)
	})
	for _, r := range out.Rows {
		if r.Part == "VIII" {
			out.Totals.PartVIIILine12Cents += r.TotalCents
		} else {
			t := &out.Totals.PartIXLine25
			t.TotalCents += r.TotalCents
			t.ProgramCents += r.ProgramCents
			t.MgmtGeneralCents += r.MgmtGeneralCents
			t.FundraisingCents += r.FundraisingCents
		}
	}
	var ids []*Account
	for i := range d.Accounts {
		if _, ok := unmapped[d.Accounts[i].ID]; ok {
			ids = append(ids, &d.Accounts[i])
		}
	}
	sort.SliceStable(ids, func(i, j int) bool { return naturalLess(ids[i].Code, ids[j].Code) })
	for _, a := range ids {
		out.UnmappedAccounts = append(out.UnmappedAccounts, Form990Unmapped{Account: a.Ref(), TotalCents: unmapped[a.ID]})
	}
	return out
}

// ─── CSV ───

type kv struct {
	k string
	v any
}

// ordered decodes JSON preserving object key order: objects → []kv, arrays → []any.
func decodeOrdered(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		if t == '{' {
			var out []kv
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				v, err := decodeOrdered(dec)
				if err != nil {
					return nil, err
				}
				out = append(out, kv{kt.(string), v})
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			if out == nil {
				out = []kv{}
			}
			return out, nil
		}
		out := []any{}
		for dec.More() {
			v, err := decodeOrdered(dec)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		return out, nil
	default:
		return tok, nil
	}
}

type cell struct {
	null bool
	v    any
}

type record struct {
	keys []string
	vals map[string]cell
}

func (r *record) set(k string, c cell) {
	if _, ok := r.vals[k]; !ok {
		r.keys = append(r.keys, k)
	}
	r.vals[k] = c
}

func newRecord() *record { return &record{vals: map[string]cell{}} }

func (r *record) clone() *record {
	n := newRecord()
	for _, k := range r.keys {
		n.set(k, r.vals[k])
	}
	return n
}

func flatten(v any, prefix string, out *record) {
	switch t := v.(type) {
	case []kv:
		for _, e := range t {
			p := e.k
			if prefix != "" {
				p = prefix + "." + e.k
			}
			flatten(e.v, p, out)
		}
	case []any:
		for i, e := range t {
			p := fmt.Sprint(i)
			if prefix != "" {
				p = prefix + "." + p
			}
			flatten(e, p, out)
		}
	case nil:
		out.set(prefix, cell{null: true})
	default:
		out.set(prefix, cell{v: t})
	}
}

func isPrimitive(v any) bool {
	switch v.(type) {
	case []kv, []any:
		return false
	}
	return true
}

func cellString(c cell) string {
	if c.null {
		return ""
	}
	switch t := c.v.(type) {
	case json.Number:
		return t.String()
	case bool:
		return fmt.Sprint(t)
	case string:
		s := t
		if s != "" && strings.ContainsRune("=+@\t\r-", rune(s[0])) {
			s = "'" + s
		}
		return s
	}
	return fmt.Sprint(c.v)
}

// CSV renders a report (or any JSON-able value) as RFC 4180 text. Report metadata repeats
// on every row, each section becomes rows, and formula-looking strings are neutralised.
func CSV(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	parsed, err := decodeOrdered(dec)
	if err != nil && err != io.EOF {
		return ""
	}
	var records []*record
	if obj, ok := parsed.([]kv); ok && hasKey(obj, "report") {
		meta := newRecord()
		for _, e := range obj {
			if isPrimitive(e.v) {
				flatten(e.v, e.k, meta)
			}
		}
		for _, e := range obj {
			switch t := e.v.(type) {
			case []any:
				for _, row := range t {
					rec := meta.clone()
					rec.set("section", cell{v: e.k})
					flatten(row, "", rec)
					records = append(records, rec)
				}
			case []kv:
				rec := meta.clone()
				rec.set("section", cell{v: e.k})
				flatten(t, "", rec)
				records = append(records, rec)
			}
		}
		if len(records) == 0 {
			records = append(records, meta)
		}
	} else if arr, ok := parsed.([]any); ok {
		for _, v := range arr {
			rec := newRecord()
			flatten(v, "", rec)
			records = append(records, rec)
		}
	} else {
		rec := newRecord()
		flatten(parsed, "", rec)
		records = append(records, rec)
	}
	var columns []string
	seen := map[string]bool{}
	for _, r := range records {
		for _, k := range r.keys {
			if !seen[k] {
				seen[k] = true
				columns = append(columns, k)
			}
		}
	}
	quote := func(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
	var b strings.Builder
	head := make([]string, len(columns))
	for i, c := range columns {
		head[i] = quote(cellString(cell{v: c}))
	}
	b.WriteString(strings.Join(head, ",") + "\r\n")
	for _, r := range records {
		row := make([]string, len(columns))
		for i, c := range columns {
			row[i] = quote(cellString(r.vals[c]))
		}
		b.WriteString(strings.Join(row, ",") + "\r\n")
	}
	return b.String()
}

func hasKey(obj []kv, k string) bool {
	for _, e := range obj {
		if e.k == k {
			return true
		}
	}
	return false
}
