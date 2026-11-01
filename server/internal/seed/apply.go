package seed

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"officerdashboard/internal/accounting"
	"officerdashboard/internal/fiscal"
	"officerdashboard/internal/luma"
	"officerdashboard/internal/store"
)

// now is the fixed clock that keeps the seed deterministic: 2026-10-30T12:00:00Z.
var now = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC).UnixMilli()

// SeedClock is the instant the seed treats as "now" (rows are stamped with it).
func SeedClock() time.Time { return time.UnixMilli(now).UTC() }

func ms(iso string) int64 {
	if len(iso) == 10 {
		iso += "T12:00:00-07:00"
	}
	t, err := luma.ParseInstant(iso)
	if err != nil {
		panic(fmt.Sprintf("seed: bad date %q", iso))
	}
	return t.UnixMilli()
}

// mulberry32 is the small deterministic PRNG the TypeScript seed used for guest lists.
func mulberry32(seed uint32) func() float64 {
	return func() float64 {
		seed += 0x6d2b79f5
		t := (seed ^ (seed >> 15)) * (1 | seed)
		t = (t + (t^(t>>7))*(61|t)) ^ t
		return float64(t^(t>>14)) / 4294967296
	}
}

var (
	firstNames = []string{"alex", "bailey", "casey", "devon", "emery", "finley", "gray", "harper", "indy", "jules", "kai", "logan", "morgan", "noor", "oakley", "parker", "quinn", "riley", "sage", "taylor"}
	lastNames  = []string{"kim", "nguyen", "patel", "garcia", "smith", "okafor", "muller", "tanaka", "silva", "cohen", "haddad", "ivanov"}
)

func title(s string) string { return strings.ToUpper(s[:1]) + s[1:] }

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}

// insert adds a row unless one with this id exists. Not INSERT OR IGNORE: the integrity
// triggers (rightly) refuse any insert that collides with a posted entry, closed period or
// audit row, and a plain guarded insert never reaches them.
func insert(ctx context.Context, w *store.Queries, table, cols string, vals ...any) error {
	n := strings.Count(cols, ",") + 1
	marks := strings.TrimSuffix(strings.Repeat("?,", n), ",")
	args := append(append([]any{}, vals...), vals[0])
	return w.Exec(ctx, fmt.Sprintf("INSERT INTO %s (%s) SELECT %s WHERE NOT EXISTS (SELECT 1 FROM %s WHERE id = ?)", table, cols, marks, table), args...)
}

// Apply loads the sample data. It is idempotent: running it twice changes nothing.
func Apply(ctx context.Context, db *store.DB) error {
	return db.Write(ctx, func(w *store.Queries) error { return apply(ctx, w) })
}

func apply(ctx context.Context, w *store.Queries) error {
	for _, u := range Users {
		if err := insert(ctx, w, "users", "id, discord_id, name, role, title, email, active, created_at, updated_at",
			u.ID, u.DiscordID, u.Name, u.Role, u.Title, u.Email, 1, now, now); err != nil {
			return err
		}
	}
	for _, a := range Accounts {
		if err := insert(ctx, w, "accounts", "id, code, name, type, normal_balance, restriction, form990_part, form990_line, form990_label, budgetable, active, description, created_at",
			AccountID(a.Code), a.Code, a.Name, a.Type, a.Normal, nullable(a.Restriction), nullable(a.Part), nullable(a.Line), nullable(a.Label), b2i(a.Budgetable), 1, nullable(a.Description), now); err != nil {
			return err
		}
	}
	for _, d := range Donors {
		if err := insert(ctx, w, "donors", "id, name, type, email, created_at", d.ID, d.Name, d.Type, nullable(d.Email), now); err != nil {
			return err
		}
	}
	for _, f := range Funds {
		if err := insert(ctx, w, "funds", "id, code, name, kind, restriction, purpose, donor_id, start_date, end_date, amount_awarded_cents, active, created_at",
			f.ID, f.Code, f.Name, f.Kind, f.Restriction, nullable(f.Purpose), nullable(f.DonorID), nullable(f.Start), nullable(f.End), nullInt(f.AwardedCents), 1, now); err != nil {
			return err
		}
	}
	for ei, e := range Events {
		if err := insert(ctx, w, "events", "id, luma_id, name, starts_at, ends_at, location, url, host_user_id, created_at, updated_at",
			e.ID, e.LumaID, e.Name, e.StartsAt, e.EndsAt, e.Location, "https://lu.ma/"+e.LumaID, e.Host, now, now); err != nil {
			return err
		}
		rand := mulberry32(uint32(ei + 1))
		start := ms(e.StartsAt)
		checkedIn := int(math.Round(float64(e.Registered) * e.CheckInPct))
		reflected := int(math.Round(float64(checkedIn) * e.ReflectionPct))
		for g := 0; g < e.Registered; g++ {
			first := firstNames[g%len(firstNames)]
			last := lastNames[(g/len(firstNames)+ei)%len(lastNames)]
			email := fmt.Sprintf("%s.%s%d@example.edu", first, last, g)
			registeredAt := start - int64(math.Floor(rand()*14*86_400_000)) - 3_600_000
			isIn := g < checkedIn
			var checkedAt any
			if isIn {
				checkedAt = start + int64(math.Floor(rand()*45*60_000))
			}
			if err := insert(ctx, w, "event_guests", "id, event_id, email, name, luma_guest_id, approval_status, registered_at, checked_in_at, created_at",
				fmt.Sprintf("%s-g%d", e.ID, g), e.ID, email, title(first)+" "+title(last), fmt.Sprintf("gst-%s-%d", e.LumaID, g), "approved", registeredAt, checkedAt, now); err != nil {
				return err
			}
			if isIn && g < reflected {
				submitted := start + 86_400_000 + int64(math.Floor(rand()*3*86_400_000))
				rating := 3 + int64(math.Floor(rand()*3))
				if err := insert(ctx, w, "reflections", "id, event_id, email, submitted_at, rating, created_at",
					fmt.Sprintf("%s-r%d", e.ID, g), e.ID, email, submitted, rating, now); err != nil {
					return err
				}
			}
		}
	}
	for _, r := range Requests {
		total := r.Quantity*r.UnitCostCents + r.TaxShipCents
		var submitted, decided any
		created := now
		if r.SubmittedAt != "" {
			submitted = ms(r.SubmittedAt)
			created = ms(r.SubmittedAt) - 3_600_000
		}
		if r.DecidedAt != "" {
			decided = ms(r.DecidedAt)
		}
		if err := insert(ctx, w, "purchase_requests", "id, number, requester_id, type, title, vendor, item_url, quantity, unit_cost_cents, tax_ship_cents, total_cents, expense_account_id, fund_id, event_id, functional_class, justification, status, submitted_at, decided_at, created_at, updated_at",
			r.ID, r.Number, r.Requester, r.Type, r.Title, r.Vendor, nullable(r.ItemURL), r.Quantity, r.UnitCostCents, r.TaxShipCents, total, AccountID(r.Account), r.Fund, nullable(r.Event), r.FunctionalClass, r.Justification, r.Status, submitted, decided, created, now); err != nil {
			return err
		}
		if r.Decision != nil && r.DecidedAt != "" {
			if err := insert(ctx, w, "approvals", "id, request_id, director_id, decision, comment, created_at",
				r.ID+"-decision", r.ID, "user-director", r.Decision.Decision, r.Decision.Comment, ms(r.DecidedAt)); err != nil {
				return err
			}
		}
		if r.Type == "club_purchase" && (r.Status == "approved" || r.Status == "fulfilling" || r.Status == "fulfilled") {
			status, attempts := "queued", 0
			if r.Status == "fulfilled" {
				status, attempts = "done", 1
			}
			if err := insert(ctx, w, "fulfillment_jobs", "id, request_id, status, attempts, created_at, updated_at",
				r.ID+"-job", r.ID, status, attempts, now, now); err != nil {
				return err
			}
		}
	}
	for _, b := range Budgets {
		fund := "all"
		if b.Fund != "" {
			fund = b.Fund
		}
		if err := insert(ctx, w, "budgets", "id, fiscal_year, account_id, fund_id, amount_cents, notes, created_at, updated_at",
			fmt.Sprintf("budget-%d-%s-%s", FiscalYear, b.Account, fund), FiscalYear, AccountID(b.Account), nullable(b.Fund), b.AmountCents, b.Notes, now, now); err != nil {
			return err
		}
	}
	// Journal: draft → lines (only while draft) → post (only while draft).
	for _, e := range Journal {
		if err := insert(ctx, w, "journal_entries", "id, entry_date, memo, status, source, purchase_request_id, created_by, created_at",
			e.ID, e.Date, e.Memo, "draft", e.Source, nullable(e.Request), "user-director", now); err != nil {
			return err
		}
		for i, l := range e.Lines {
			id := fmt.Sprintf("%s-%d", e.ID, i+1)
			if err := w.Exec(ctx, `INSERT INTO journal_lines (id, entry_id, line_no, account_id, fund_id, functional_class, debit_cents, credit_cents, memo)
				SELECT ?, ?, ?, ?, ?, ?, ?, ?, NULL
				WHERE (SELECT status FROM journal_entries WHERE id = ?) = 'draft' AND NOT EXISTS (SELECT 1 FROM journal_lines WHERE id = ?)`,
				id, e.ID, i+1, AccountID(l.Account), l.Fund, nullable(l.FC), l.Debit, l.Credit, e.ID, id); err != nil {
				return err
			}
		}
		if err := w.Exec(ctx, `UPDATE journal_entries SET status = 'posted', posted_at = ?, posted_by = 'user-director' WHERE id = ? AND status = 'draft'`, now, e.ID); err != nil {
			return err
		}
	}
	for _, c := range Contributions {
		ob := accounting.ContributionObligations(c.AmountCents, c.QuidProQuo)
		if err := insert(ctx, w, "contributions", "id, donor_id, fund_id, received_date, kind, amount_cents, quid_pro_quo_cents, acknowledgment_required, journal_entry_id, created_at",
			c.ID, c.Donor, c.Fund, c.Date, c.Kind, c.AmountCents, c.QuidProQuo, b2i(ob.AcknowledgmentRequired), c.Entry, now); err != nil {
			return err
		}
	}
	// Monthly fiscal periods; July–September closed. Inserted after the journal so the
	// sample July–September entries could still post.
	for i, p := range fiscal.MonthlyPeriods(FiscalYear, fiscal.DefaultStartMonth) {
		status := "open"
		var closedAt, closedBy any
		if i < 3 {
			status, closedAt, closedBy = "closed", now, "user-director"
		}
		if err := insert(ctx, w, "fiscal_periods", "id, fiscal_year, name, start_date, end_date, status, closed_at, closed_by, created_at",
			"period-"+p.StartDate[:7], FiscalYear, p.Name, p.StartDate, p.EndDate, status, closedAt, closedBy, now); err != nil {
			return err
		}
	}
	return nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
