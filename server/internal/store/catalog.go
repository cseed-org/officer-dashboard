package store

import (
	"context"

	"officerdashboard/internal/accounting"
)

// Accounts lists the chart of accounts in insertion order.
func (q *Queries) Accounts(ctx context.Context) ([]accounting.Account, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT id, code, name, type, form990_part, form990_line, form990_label, restriction, budgetable, active FROM accounts ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []accounting.Account
	for rows.Next() {
		var a accounting.Account
		if err := rows.Scan(&a.ID, &a.Code, &a.Name, &a.Type, &a.Form990Part, &a.Form990Line, &a.Form990Lbl, &a.Restriction, &a.Budgetable, &a.Active); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Funds lists funds in insertion order.
func (q *Queries) Funds(ctx context.Context) ([]accounting.Fund, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT id, code, name, kind, restriction, active, start_date, end_date, amount_awarded_cents FROM funds ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []accounting.Fund
	for rows.Next() {
		var f accounting.Fund
		if err := rows.Scan(&f.ID, &f.Code, &f.Name, &f.Kind, &f.Restriction, &f.Active, &f.StartDate, &f.EndDate, &f.AmountAwardedCents); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// FiscalPeriod is a fiscal_periods row.
type FiscalPeriod struct {
	ID         string
	FiscalYear int
	Name       string
	StartDate  string
	EndDate    string
	Status     string
	ClosedAt   *int64
	ClosedBy   *string
}

// FiscalPeriods lists periods by start date.
func (q *Queries) FiscalPeriods(ctx context.Context) ([]FiscalPeriod, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT id, fiscal_year, name, start_date, end_date, status, closed_at, closed_by FROM fiscal_periods ORDER BY start_date`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FiscalPeriod
	for rows.Next() {
		var p FiscalPeriod
		if err := rows.Scan(&p.ID, &p.FiscalYear, &p.Name, &p.StartDate, &p.EndDate, &p.Status, &p.ClosedAt, &p.ClosedBy); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// FiscalPeriodByID returns ErrNotFound for an unknown id.
func (q *Queries) FiscalPeriodByID(ctx context.Context, id string) (*FiscalPeriod, error) {
	var p FiscalPeriod
	err := q.q.QueryRowContext(ctx, `SELECT id, fiscal_year, name, start_date, end_date, status, closed_at, closed_by FROM fiscal_periods WHERE id = ?`, id).
		Scan(&p.ID, &p.FiscalYear, &p.Name, &p.StartDate, &p.EndDate, &p.Status, &p.ClosedAt, &p.ClosedBy)
	if err != nil {
		return nil, notFound(err)
	}
	return &p, nil
}

// CountPeriodsForYear says how many periods a fiscal year already has.
func (q *Queries) CountPeriodsForYear(ctx context.Context, fy int) (int, error) {
	var n int
	err := q.q.QueryRowContext(ctx, `SELECT count(*) FROM fiscal_periods WHERE fiscal_year = ?`, fy).Scan(&n)
	return n, err
}

// InsertPeriod adds one open period.
func (q *Queries) InsertPeriod(ctx context.Context, p FiscalPeriod, now int64) error {
	_, err := q.q.ExecContext(ctx, `INSERT INTO fiscal_periods (id, fiscal_year, name, start_date, end_date, status, created_at) VALUES (?,?,?,?,?, 'open', ?)`,
		p.ID, p.FiscalYear, p.Name, p.StartDate, p.EndDate, now)
	return err
}

// SetPeriodStatus closes or locks a period, keeping the first closer.
func (q *Queries) SetPeriodStatus(ctx context.Context, id, status string, closedAt int64, closedBy string) error {
	_, err := q.q.ExecContext(ctx, `UPDATE fiscal_periods SET status = ?, closed_at = coalesce(closed_at, ?), closed_by = coalesce(closed_by, ?) WHERE id = ?`, status, closedAt, closedBy, id)
	return err
}

// BudgetRow is a budgets row.
type BudgetRow struct {
	ID          string
	FiscalYear  int
	AccountID   string
	FundID      *string
	AmountCents int64
	Notes       *string
}

// Budgets lists the budget lines of a fiscal year (all years when fy is 0).
func (q *Queries) Budgets(ctx context.Context, fy int) ([]BudgetRow, error) {
	query := `SELECT id, fiscal_year, account_id, fund_id, amount_cents, notes FROM budgets`
	var args []any
	if fy != 0 {
		query += ` WHERE fiscal_year = ?`
		args = append(args, fy)
	}
	rows, err := q.q.QueryContext(ctx, query+` ORDER BY rowid`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BudgetRow
	for rows.Next() {
		var b BudgetRow
		if err := rows.Scan(&b.ID, &b.FiscalYear, &b.AccountID, &b.FundID, &b.AmountCents, &b.Notes); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ReplaceBudgets swaps the whole fiscal year's budget for the given lines.
func (q *Queries) ReplaceBudgets(ctx context.Context, fy int, lines []BudgetRow, now int64) error {
	if _, err := q.q.ExecContext(ctx, `DELETE FROM budgets WHERE fiscal_year = ?`, fy); err != nil {
		return err
	}
	for _, l := range lines {
		if _, err := q.q.ExecContext(ctx, `INSERT INTO budgets (id, fiscal_year, account_id, fund_id, amount_cents, notes, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?)`,
			l.ID, fy, l.AccountID, l.FundID, l.AmountCents, l.Notes, now, now); err != nil {
			return err
		}
	}
	return nil
}

// ReportLines loads every posted journal line with the entry facts reports need,
// in a stable order (entry date, entry id, line number).
func (q *Queries) ReportLines(ctx context.Context) ([]accounting.ReportLine, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT l.account_id, l.fund_id, e.entry_date, e.source, coalesce(r.source, ''), l.debit_cents, l.credit_cents, coalesce(l.functional_class, ''),
		EXISTS (SELECT 1 FROM contributions c WHERE c.kind IN ('in_kind', 'stock') AND c.journal_entry_id IN (e.id, e.reverses_entry_id))
		FROM journal_lines l
		JOIN journal_entries e ON e.id = l.entry_id
		LEFT JOIN journal_entries r ON r.id = e.reverses_entry_id
		WHERE e.status = 'posted'
		ORDER BY e.entry_date, e.id, l.line_no`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []accounting.ReportLine
	for rows.Next() {
		var l accounting.ReportLine
		if err := rows.Scan(&l.AccountID, &l.FundID, &l.EntryDate, &l.Source, &l.ReversesSource, &l.DebitCents, &l.CreditCents, &l.FunctionalClass, &l.Noncash); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// SettlementDates maps a purchase request to the date of its earliest posted expense line.
func (q *Queries) SettlementDates(ctx context.Context) (map[string]string, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT e.purchase_request_id, min(e.entry_date) FROM journal_entries e
		JOIN journal_lines l ON l.entry_id = e.id JOIN accounts a ON a.id = l.account_id AND a.type = 'expense'
		WHERE e.status = 'posted' AND e.purchase_request_id IS NOT NULL GROUP BY e.purchase_request_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, date string
		if err := rows.Scan(&id, &date); err != nil {
			return nil, err
		}
		out[id] = date
	}
	return out, rows.Err()
}

// AvailableRestricted is the donor-restricted money still available in a fund: revenue
// (except program-service revenue) plus restricted net assets, credit minus debit.
func (q *Queries) AvailableRestricted(ctx context.Context, fundID string) (int64, error) {
	var n int64
	err := q.q.QueryRowContext(ctx, `SELECT coalesce(sum(l.credit_cents - l.debit_cents), 0) FROM journal_lines l
		JOIN journal_entries e ON e.id = l.entry_id JOIN accounts a ON a.id = l.account_id
		WHERE e.status = 'posted' AND l.fund_id = ?
		AND ((a.type = 'revenue' AND NOT (coalesce(a.form990_part, '') = 'VIII' AND coalesce(a.form990_line, '') LIKE '2%'))
		  OR (a.type = 'net_asset' AND a.restriction = 'with_donor'))`, fundID).Scan(&n)
	return n, err
}
