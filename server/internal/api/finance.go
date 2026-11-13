package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"officerdashboard/internal/accounting"
	"officerdashboard/internal/auth"
	"officerdashboard/internal/fiscal"
	"officerdashboard/internal/luma"
	"officerdashboard/internal/store"
	"officerdashboard/internal/validate"
)

var financeReports = []string{"trial-balance", "statement-of-activities", "statement-of-financial-position", "statement-of-functional-expenses", "budget-vs-actual", "fund-balances", "form-990"}
var officerReports = map[string]bool{"budget-vs-actual": true, "fund-balances": true}

// loadReportData gathers books, budgets and commitments. A past asOf reconstructs which
// requests were committed then by undoing later changes with the audit log's before-images.
func (s *Server) loadReportData(ctx context.Context, q *store.Queries, asOf string) (*accounting.ReportData, error) {
	accounts, err := q.Accounts(ctx)
	if err != nil {
		return nil, err
	}
	funds, err := q.Funds(ctx)
	if err != nil {
		return nil, err
	}
	lines, err := q.ReportLines(ctx)
	if err != nil {
		return nil, err
	}
	budgetRows, err := q.Budgets(ctx, 0)
	if err != nil {
		return nil, err
	}
	requests, err := q.AllRequests(ctx)
	if err != nil {
		return nil, err
	}
	settled, err := q.SettlementDates(ctx)
	if err != nil {
		return nil, err
	}
	data := &accounting.ReportData{Accounts: accounts, Funds: funds, Lines: lines}
	for _, b := range budgetRows {
		data.Budgets = append(data.Budgets, accounting.Budget{AccountID: b.AccountID, FundID: b.FundID, FiscalYear: b.FiscalYear, AmountCents: b.AmountCents})
	}
	today := fiscal.PacificDate(s.now())
	// The end of the asOf day in the organisation's calendar, not in UTC.
	day, _ := time.ParseInLocation("2006-01-02", asOf, fiscal.Pacific)
	cutoff := day.AddDate(0, 0, 1).Add(-time.Millisecond)
	var history []store.AuditEntry
	if asOf < today {
		if history, err = q.AuditAfter(ctx, cutoff.UnixMilli()); err != nil {
			return nil, err
		}
	}
	for i := range requests {
		r := &requests[i]
		if !s.committedAt(r, history, settled[r.ID], asOf, cutoff) {
			continue
		}
		at := int64(r.CreatedAt)
		if r.SubmittedAt != nil {
			at = *r.SubmittedAt
		}
		data.Commitments = append(data.Commitments, accounting.Commitment{
			ExpenseAccountID: r.ExpenseAccountID, FundID: r.FundID, TotalCents: r.TotalCents, SubmittedDate: fiscal.PacificDate(time.UnixMilli(at)),
		})
	}
	return data, nil
}

func (s *Server) committedAt(r *store.Request, history []store.AuditEntry, settlementDate, asOf string, cutoff time.Time) bool {
	status := r.Status
	known := true
	// Undo later mutations using their immutable before-images (history is newest first).
	for _, a := range history {
		var before struct {
			Status    *string `json:"status"`
			RequestID string  `json:"requestId"`
		}
		if a.BeforeJSON != nil {
			_ = json.Unmarshal([]byte(*a.BeforeJSON), &before)
		}
		if a.EntityType == "purchase_request" && a.EntityID != nil && *a.EntityID == r.ID {
			if before.Status != nil {
				status, known = *before.Status, true
			} else {
				status, known = "", false
			}
		}
		if a.EntityType == "fulfillment_job" && before.RequestID == r.ID {
			if a.Action == "job.claimed" {
				status = "approved"
			} else {
				status = "fulfilling"
			}
			known = true
		}
	}
	at := int64(r.CreatedAt)
	if r.SubmittedAt != nil {
		at = *r.SubmittedAt
	}
	if at > cutoff.UnixMilli() {
		return false
	}
	if r.DecidedAt != nil && *r.DecidedAt > cutoff.UnixMilli() {
		return false
	}
	if settlementDate != "" && settlementDate > asOf && known && (status == "fulfilled" || status == "reimbursed" || status == "closed") {
		status = "approved"
	}
	return known && (status == "approved" || status == "fulfilling")
}

// financeParams is the resolved (fiscalYear, asOf, fundId) of a finance request.
type financeParams struct {
	FiscalYear int
	AsOf       string
	FundID     string
}

func (s *Server) resolveFinance(fiscalYear *int, asOf, fundID string) (financeParams, error) {
	today := fiscal.PacificDate(s.now())
	a := asOf
	if a == "" {
		a = today
	}
	fy := 0
	if fiscalYear != nil {
		fy = *fiscalYear
	} else {
		var err error
		if fy, err = fiscal.YearFor(a, fiscal.DefaultStartMonth); err != nil {
			return financeParams{}, badRequest(err.Error())
		}
	}
	start, end := fiscal.YearRange(fy, fiscal.DefaultStartMonth)
	out := asOf
	if out == "" {
		out = a
		if a < start {
			out = start
		} else if a > end {
			out = end
		}
	}
	return financeParams{FiscalYear: fy, AsOf: out, FundID: fundID}, nil
}

// readFinanceQuery parses FinanceQuery (fiscalYear, asOf, fundId).
func (c *Ctx) readFinanceQuery(extra func(r *validate.Reader)) (financeParams, *validate.Reader, error) {
	var fy validate.Field[int64]
	var asOf, fund validate.Field[string]
	rd := validate.FromValues(c.R.URL.Query())
	fy = rd.Int("fiscalYear", validate.I{Min: validate.Lo(2000), Max: validate.Lo(2100)})
	asOf = rd.Str("asOf", validate.S{Format: "date"})
	fund = rd.Str("fundId", validate.S{Min: 1, Max: 64})
	if extra != nil {
		extra(rd)
	}
	if rd.Issues.Any() {
		return financeParams{}, nil, validationError(rd.Issues)
	}
	var fyp *int
	if fy.Set {
		v := int(fy.V)
		fyp = &v
	}
	p, err := c.S.resolveFinance(fyp, asOf.V, fund.V)
	return p, rd, err
}

// ─── summary ───

type summaryTotals struct {
	BudgetCents    int64 `json:"budgetCents"`
	SpentCents     int64 `json:"spentCents"`
	CommittedCents int64 `json:"committedCents"`
	RemainingCents int64 `json:"remainingCents"`
}

type summaryCategory struct {
	Account        AccountBrief `json:"account"`
	BudgetCents    int64        `json:"budgetCents"`
	SpentCents     int64        `json:"spentCents"`
	CommittedCents int64        `json:"committedCents"`
	RemainingCents int64        `json:"remainingCents"`
}

type summaryFund struct {
	accounting.FundOption
	BalanceCents int64   `json:"balanceCents"`
	AwardedCents *int64  `json:"awardedCents"`
	EndDate      *string `json:"endDate"`
}

type financeSummary struct {
	FiscalYear int               `json:"fiscalYear"`
	AsOf       string            `json:"asOf"`
	Totals     summaryTotals     `json:"totals"`
	ByCategory []summaryCategory `json:"byCategory"`
	Funds      []summaryFund     `json:"funds"`
}

func makeSummary(data *accounting.ReportData, p financeParams) (*financeSummary, error) {
	query := accounting.Query{FiscalYear: p.FiscalYear, AsOf: p.AsOf, FundID: p.FundID}
	bva, err := accounting.Make("budget-vs-actual", data, query)
	if err != nil {
		return nil, err
	}
	fb, err := accounting.Make("fund-balances", data, query)
	if err != nil {
		return nil, err
	}
	report, balances := bva.(*accounting.BudgetVsActual), fb.(*accounting.FundBalances)
	sum := &financeSummary{FiscalYear: p.FiscalYear, AsOf: p.AsOf, ByCategory: []summaryCategory{}, Funds: []summaryFund{}}
	sum.Totals = summaryTotals{BudgetCents: report.Totals.BudgetCents, SpentCents: report.Totals.ActualCents, CommittedCents: report.Totals.CommittedCents, RemainingCents: report.Totals.VarianceCents}
	groups := map[string]*summaryCategory{}
	var order []string
	for _, r := range report.Rows {
		g := groups[r.Account.ID]
		if g == nil {
			g = &summaryCategory{Account: AccountBrief{ID: r.Account.ID, Code: r.Account.Code, Name: r.Account.Name}}
			groups[r.Account.ID] = g
			order = append(order, r.Account.ID)
		}
		g.BudgetCents += r.BudgetCents
		g.SpentCents += r.ActualCents
		g.CommittedCents += r.CommittedCents
		g.RemainingCents += r.VarianceCents
	}
	for _, id := range order {
		sum.ByCategory = append(sum.ByCategory, *groups[id])
	}
	byID := map[string]accounting.Fund{}
	for _, f := range data.Funds {
		byID[f.ID] = f
	}
	for _, r := range balances.Rows {
		f := byID[r.Fund.ID]
		sum.Funds = append(sum.Funds, summaryFund{FundOption: r.Fund, BalanceCents: r.EndingCents, AwardedCents: f.AmountAwardedCents, EndDate: f.EndDate})
	}
	return sum, nil
}

func (c *Ctx) financeSummary() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	p, _, err := c.readFinanceQuery(nil)
	if err != nil {
		return err
	}
	data, err := c.S.loadReportData(c.Context(), c.S.db.Read(), p.AsOf)
	if err != nil {
		return err
	}
	sum, err := makeSummary(data, p)
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, sum)
	return nil
}

func (c *Ctx) financeReport() error {
	user, err := c.officer()
	if err != nil {
		return err
	}
	var format validate.Field[string]
	p, _, err := c.readFinanceQuery(func(r *validate.Reader) {
		format = r.Str("format", validate.S{Enum: []string{"json", "csv"}, HasDef: true, Default: "json"})
	})
	if err != nil {
		return err
	}
	name := c.R.PathValue("report")
	known := false
	for _, n := range financeReports {
		known = known || n == name
	}
	if !known {
		iss := &validate.Issues{}
		iss.AddForm("Invalid option: expected one of " + strings.Join(financeReports, "|"))
		return validationError(iss)
	}
	if !auth.HasRole(user.Role, auth.RoleDirector) && !officerReports[name] {
		return forbidden("This report requires director role")
	}
	data, err := c.S.loadReportData(c.Context(), c.S.db.Read(), p.AsOf)
	if err != nil {
		return err
	}
	report, err := accounting.Make(name, data, accounting.Query{FiscalYear: p.FiscalYear, AsOf: p.AsOf, FundID: p.FundID})
	if err != nil {
		return err
	}
	if format.V == "csv" {
		c.W.Header().Set("Content-Type", "text/csv; charset=utf-8")
		c.W.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-%d.csv"`, name, p.FiscalYear))
		c.W.Header().Set("Cache-Control", "no-store")
		c.W.WriteHeader(http.StatusOK)
		_, _ = c.W.Write([]byte(accounting.CSV(report)))
		return nil
	}
	writeJSON(c.W, 200, report)
	return nil
}

// ─── budgets ───

type budgetLine struct {
	ID          string       `json:"id"`
	FiscalYear  int          `json:"fiscalYear"`
	Account     AccountBrief `json:"account"`
	Fund        *FundBrief   `json:"fund"`
	AmountCents int64        `json:"amountCents"`
	Notes       *string      `json:"notes"`
}

type budgetsResponse struct {
	FiscalYear int          `json:"fiscalYear"`
	Lines      []budgetLine `json:"lines"`
}

func (c *Ctx) budgetsView(ctx context.Context, q *store.Queries, p financeParams) (*budgetsResponse, error) {
	cat, err := loadCatalog(ctx, q, false)
	if err != nil {
		return nil, err
	}
	rows, err := q.Budgets(ctx, p.FiscalYear)
	if err != nil {
		return nil, err
	}
	out := &budgetsResponse{FiscalYear: p.FiscalYear, Lines: []budgetLine{}}
	for _, b := range rows {
		if p.FundID != "" && b.FundID != nil && *b.FundID != p.FundID {
			continue
		}
		line := budgetLine{ID: b.ID, FiscalYear: b.FiscalYear, AmountCents: b.AmountCents, Notes: b.Notes}
		if a := cat.acctByID[b.AccountID]; a != nil {
			line.Account = a.Brief()
		}
		if b.FundID != nil {
			if f := cat.fundByID[*b.FundID]; f != nil {
				br := f.Brief()
				line.Fund = &br
			}
		}
		out.Lines = append(out.Lines, line)
	}
	return out, nil
}

func (c *Ctx) budgetsGet() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	p, _, err := c.readFinanceQuery(nil)
	if err != nil {
		return err
	}
	v, err := c.budgetsView(c.Context(), c.S.db.Read(), p)
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, v)
	return nil
}

func (c *Ctx) budgetsPut() error {
	if _, err := c.director(); err != nil {
		return err
	}
	type line struct {
		AccountID string
		FundID    *string
		Amount    int64
		Notes     *string
	}
	var fy int
	var lines []line
	err := c.jsonBody(func(r *validate.Reader) {
		fy = int(r.Int("fiscalYear", validate.I{Req: true, Min: validate.Lo(2000), Max: validate.Lo(2100)}).V)
		r.Objects("lines", true, 500, func(_ int, sub *validate.Reader) {
			a := sub.Str("accountId", validate.S{Req: true, Min: 1, Max: 64})
			f := sub.Str("fundId", validate.S{Req: true, Null: true, Min: 1, Max: 64})
			amt := sub.Int("amountCents", validate.I{Req: true, Min: validate.Lo(0)})
			n := sub.Str("notes", validate.S{Null: true, Max: 500})
			l := line{AccountID: a.V, Amount: amt.V}
			if f.Has() {
				v := f.V
				l.FundID = &v
			}
			if n.Has() {
				v := n.V
				l.Notes = &v
			}
			lines = append(lines, l)
		})
	})
	if err != nil {
		return err
	}
	// Reject duplicate or overlapping lines: one row per account+fund, and an all-funds
	// line may not coexist with a fund-specific one for the same account.
	for i, l := range lines {
		for j, o := range lines {
			if i == j || l.AccountID != o.AccountID {
				continue
			}
			sameFund := (l.FundID == nil && o.FundID == nil) || (l.FundID != nil && o.FundID != nil && *l.FundID == *o.FundID)
			if sameFund || l.FundID == nil || o.FundID == nil {
				return badRequest("Duplicate or overlapping budget lines")
			}
		}
	}
	p, err := c.S.resolveFinance(&fy, "", "")
	if err != nil {
		return err
	}
	ctx := c.Context()
	err = c.S.db.Write(ctx, func(w *store.Queries) error {
		cat, err := loadCatalog(ctx, w, false)
		if err != nil {
			return err
		}
		for _, l := range lines {
			a := cat.acctByID[l.AccountID]
			if a == nil || !a.Active || a.Type != "expense" || !a.Budgetable {
				return badRequest("Invalid budget account/fund")
			}
			if l.FundID != nil {
				if f := cat.fundByID[*l.FundID]; f == nil || !f.Active {
					return badRequest("Invalid budget account/fund")
				}
			}
		}
		before, err := w.Budgets(ctx, fy)
		if err != nil {
			return err
		}
		rows := make([]store.BudgetRow, len(lines))
		for i, l := range lines {
			rows[i] = store.BudgetRow{ID: auth.UUID(), FiscalYear: fy, AccountID: l.AccountID, FundID: l.FundID, AmountCents: l.Amount, Notes: l.Notes}
		}
		if err := w.ReplaceBudgets(ctx, fy, rows, c.S.nowMs()); err != nil {
			return err
		}
		return c.audit(ctx, w, "budget.replaced", "budget", fmt.Sprint(fy), before, map[string]any{"fiscalYear": fy, "lines": lines2json(lines2rows(rows))})
	})
	if err != nil {
		return err
	}
	v, err := c.budgetsView(ctx, c.S.db.Read(), p)
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, v)
	return nil
}

func lines2rows(r []store.BudgetRow) []store.BudgetRow { return r }
func lines2json(r []store.BudgetRow) []map[string]any {
	out := make([]map[string]any, len(r))
	for i, b := range r {
		out[i] = map[string]any{"accountId": b.AccountID, "fundId": b.FundID, "amountCents": b.AmountCents, "notes": b.Notes}
	}
	return out
}

// ─── periods ───

type periodDTO struct {
	ID         string `json:"id"`
	FiscalYear int    `json:"fiscalYear"`
	Name       string `json:"name"`
	StartDate  string `json:"startDate"`
	EndDate    string `json:"endDate"`
	Status     string `json:"status"`
}

func (c *Ctx) periodsView(ctx context.Context, q *store.Queries) (map[string]any, error) {
	ps, err := q.FiscalPeriods(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]periodDTO, len(ps))
	for i, p := range ps {
		out[i] = periodDTO{ID: p.ID, FiscalYear: p.FiscalYear, Name: p.Name, StartDate: p.StartDate, EndDate: p.EndDate, Status: p.Status}
	}
	return map[string]any{"periods": out}, nil
}

func (c *Ctx) periods() error {
	if _, err := c.director(); err != nil {
		return err
	}
	v, err := c.periodsView(c.Context(), c.S.db.Read())
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, v)
	return nil
}

func (c *Ctx) periodsCreate() error {
	if _, err := c.director(); err != nil {
		return err
	}
	var fy int
	if err := c.jsonBody(func(r *validate.Reader) {
		fy = int(r.Int("fiscalYear", validate.I{Req: true, Min: validate.Lo(2000), Max: validate.Lo(2100)}).V)
	}); err != nil {
		return err
	}
	ctx := c.Context()
	err := c.S.db.Write(ctx, func(w *store.Queries) error {
		n, err := w.CountPeriodsForYear(ctx, fy)
		if err != nil {
			return err
		}
		if n > 0 {
			return conflict(fmt.Sprintf("FY%d already has fiscal periods", fy))
		}
		var created []periodDTO
		for _, p := range fiscal.MonthlyPeriods(fy, fiscal.DefaultStartMonth) {
			fp := store.FiscalPeriod{ID: "period-" + p.StartDate[:7], FiscalYear: fy, Name: p.Name, StartDate: p.StartDate, EndDate: p.EndDate}
			if err := w.InsertPeriod(ctx, fp, c.S.nowMs()); err != nil {
				return err
			}
			created = append(created, periodDTO{ID: fp.ID, FiscalYear: fy, Name: p.Name, StartDate: p.StartDate, EndDate: p.EndDate, Status: "open"})
		}
		return c.audit(ctx, w, "fiscal_year.periods_created", "fiscal_period", fmt.Sprintf("FY%d", fy), nil, created)
	})
	if err != nil {
		return err
	}
	v, err := c.periodsView(ctx, c.S.db.Read())
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, v)
	return nil
}

func (c *Ctx) periodClose() error {
	user, err := c.director()
	if err != nil {
		return err
	}
	var to string
	if err := c.jsonBody(func(r *validate.Reader) {
		to = r.Str("to", validate.S{Req: true, Enum: []string{"closed", "locked"}}).V
	}); err != nil {
		return err
	}
	ctx := c.Context()
	id := c.R.PathValue("id")
	err = c.S.db.Write(ctx, func(w *store.Queries) error {
		p, err := w.FiscalPeriodByID(ctx, id)
		if err != nil {
			return notFoundErr("Fiscal period not found")
		}
		// Closing never reopens, so a period still in progress (Pacific date) would block
		// every settlement and payout dated today until the month ends.
		if p.EndDate >= fiscal.PacificDate(c.S.now()) {
			return conflict(fmt.Sprintf("%s hasn't ended yet; close it after %s", p.Name, p.EndDate))
		}
		next, cerr := accounting.ClosePeriod(accounting.Period{ID: p.ID, StartDate: p.StartDate, EndDate: p.EndDate, Status: p.Status}, to)
		if cerr != nil {
			return conflict(fmt.Sprintf("A %s period cannot become %s", p.Status, to))
		}
		if next.Status == p.Status {
			return conflict("Period is already " + p.Status)
		}
		if err := w.SetPeriodStatus(ctx, p.ID, next.Status, c.S.nowMs(), user.ID); err != nil {
			return err
		}
		return c.audit(ctx, w, "fiscal_period."+next.Status, "fiscal_period", p.ID, p, map[string]any{"status": next.Status})
	})
	if err != nil {
		return err
	}
	v, err := c.periodsView(ctx, c.S.db.Read())
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, v)
	return nil
}

// ─── chart of accounts, lookups, overview ───

func (c *Ctx) accounts() error {
	if _, err := c.director(); err != nil {
		return err
	}
	list, err := c.S.db.Read().Accounts(c.Context())
	if err != nil {
		return err
	}
	type row struct {
		AccountOption
		Form990Part  *string `json:"form990Part"`
		Form990Label *string `json:"form990Label"`
		Restriction  *string `json:"restriction"`
		Budgetable   bool    `json:"budgetable"`
		Active       bool    `json:"active"`
	}
	out := make([]row, len(list))
	for i, a := range list {
		out[i] = row{AccountOption: accountOption(a), Form990Part: a.Form990Part, Form990Label: a.Form990Lbl, Restriction: a.Restriction, Budgetable: a.Budgetable, Active: a.Active}
	}
	writeJSON(c.W, 200, map[string]any{"accounts": out})
	return nil
}

func (c *Ctx) lookups() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	ctx := c.Context()
	cat, err := loadCatalog(ctx, c.S.db.Read(), true)
	if err != nil {
		return err
	}
	expense := []AccountOption{}
	for _, a := range cat.accounts {
		if a.Active && a.Budgetable && a.Type == "expense" {
			expense = append(expense, accountOption(a))
		}
	}
	funds := []accounting.FundOption{}
	for _, f := range cat.funds {
		if f.Active {
			funds = append(funds, f.Option())
		}
	}
	type ev struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		StartsAt string `json:"startsAt"`
	}
	events := make([]ev, len(cat.events))
	for i, e := range cat.events {
		events[i] = ev{e.ID, e.Name, e.StartsAt}
	}
	teams := make([]SubteamBrief, len(cat.subteams))
	for i, t := range cat.subteams {
		teams[i] = SubteamBrief{ID: t.ID, Name: t.Name}
	}
	hosts := []UserSummary{}
	for _, u := range cat.users {
		if u.Active {
			hosts = append(hosts, userSummary(u))
		}
	}
	sort.Slice(hosts, func(i, j int) bool { return hosts[i].Name < hosts[j].Name })
	fy, _ := fiscal.YearFor(fiscal.PacificDate(c.S.now()), fiscal.DefaultStartMonth)
	writeJSON(c.W, 200, map[string]any{"expenseAccounts": expense, "funds": funds, "events": events, "subteams": teams, "hosts": hosts, "currentFiscalYear": fy})
	return nil
}

func avg(rates []*float64) *float64 {
	var sum float64
	n := 0
	for _, r := range rates {
		if r != nil {
			sum += *r
			n++
		}
	}
	if n == 0 {
		return nil
	}
	v := sum / float64(n)
	return &v
}

func (c *Ctx) overview() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	ctx := c.Context()
	p, err := c.S.resolveFinance(nil, "", "")
	if err != nil {
		return err
	}
	q := c.S.db.Read()
	data, err := c.S.loadReportData(ctx, q, p.AsOf)
	if err != nil {
		return err
	}
	sum, err := makeSummary(data, p)
	if err != nil {
		return err
	}
	cat, err := loadCatalog(ctx, q, true)
	if err != nil {
		return err
	}
	items, err := c.S.eventItems(ctx, q, cat, cat.events, "")
	if err != nil {
		return err
	}
	now := c.S.now()
	fyStart, _ := fiscal.YearRange(p.FiscalYear, fiscal.DefaultStartMonth)
	from, _ := time.Parse("2006-01-02", fyStart)
	type timed struct {
		item EventListItem
		at   time.Time
	}
	var all []timed
	for _, it := range items {
		t, err := luma.ParseInstant(it.StartsAt)
		if err != nil {
			continue
		}
		all = append(all, timed{it, t})
	}
	upcoming := 0
	var checkIn, reflect []*float64
	for _, e := range all {
		if !e.at.Before(now) {
			upcoming++
		}
		if !e.at.Before(from) && !e.at.After(now) {
			checkIn = append(checkIn, e.item.Metrics.CheckInRate)
			reflect = append(reflect, e.item.Metrics.ReflectionRate)
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].at.After(all[j].at) })
	recent := []EventListItem{}
	for i := 0; i < len(all) && i < 10; i++ {
		recent = append(recent, all[i].item)
	}
	reqs, err := q.Requests(ctx, store.RequestFilter{Status: "submitted"})
	if err != nil {
		return err
	}
	var pendingCents int64
	for _, r := range reqs {
		pendingCents += r.TotalCents
	}
	writeJSON(c.W, 200, map[string]any{
		"fiscalYear": p.FiscalYear,
		"kpis": map[string]any{
			"upcomingEvents": upcoming, "avgCheckInRate": avg(checkIn), "avgReflectionRate": avg(reflect),
			"budgetCents": sum.Totals.BudgetCents, "spentCents": sum.Totals.SpentCents, "committedCents": sum.Totals.CommittedCents, "remainingCents": sum.Totals.RemainingCents,
			"pendingRequests": len(reqs), "pendingRequestsCents": pendingCents,
		},
		"recentEvents": recent,
	})
	return nil
}
