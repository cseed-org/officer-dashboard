package store

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"testing"
)

// Database-level integrity rules (migrations 0000–0003): posted books are immutable,
// REPLACE/UPSERT cannot rewrite them, the audit log is append-only, money is integer cents.

type tdb struct {
	t  *testing.T
	db *sql.DB
	n  int
}

func newTDB(t *testing.T) *tdb {
	t.Helper()
	d, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	x := &tdb{t: t, db: d.W}
	for _, s := range []string{
		`INSERT INTO accounts (id, code, name, type, normal_balance) VALUES ('cash', '1000', 'Checking', 'asset', 'debit')`,
		`INSERT INTO accounts (id, code, name, type, normal_balance) VALUES ('rev', '4000', 'Contributions', 'revenue', 'credit')`,
		`INSERT INTO funds (id, code, name, kind, restriction) VALUES ('gen', 'GEN', 'General', 'general', 'without_donor')`,
		`INSERT INTO users (id, discord_id, name, role) VALUES ('u1', '100000000000000001', 'Officer', 'officer')`,
		`INSERT INTO donors (id, name, type) VALUES ('d1', 'Donor', 'individual')`,
	} {
		x.must(s)
	}
	return x
}

func (x *tdb) key() string { x.n++; return fmt.Sprintf("t%d", x.n) }
func (x *tdb) run(q string, args ...any) error {
	_, err := x.db.ExecContext(context.Background(), q, args...)
	return err
}
func (x *tdb) must(q string, args ...any) {
	x.t.Helper()
	if err := x.run(q, args...); err != nil {
		x.t.Fatalf("%s: %v", q, err)
	}
}
func (x *tdb) rejects(pattern string, q string, args ...any) {
	x.t.Helper()
	err := x.run(q, args...)
	if err == nil {
		x.t.Fatalf("expected %q to fail with /%s/", q, pattern)
	}
	if !regexp.MustCompile(pattern).MatchString(err.Error()) {
		x.t.Fatalf("error %q does not match /%s/ (%s)", err, pattern, q)
	}
}
func (x *tdb) draft(id, date string) {
	x.t.Helper()
	x.must(`INSERT INTO journal_entries (id, entry_date, memo) VALUES (?, ?, 'test')`, id, date)
}
func (x *tdb) line(entry string, no int, account string, debit, credit any, id ...string) error {
	lid := fmt.Sprintf("%s-%d", entry, no)
	if len(id) > 0 {
		lid = id[0]
	}
	return x.run(`INSERT INTO journal_lines (id, entry_id, line_no, account_id, fund_id, debit_cents, credit_cents) VALUES (?, ?, ?, ?, 'gen', ?, ?)`,
		lid, entry, no, account, debit, credit)
}
func (x *tdb) mustLine(entry string, no int, account string, debit, credit any) {
	x.t.Helper()
	if err := x.line(entry, no, account, debit, credit); err != nil {
		x.t.Fatal(err)
	}
}
func (x *tdb) post(id string) error {
	return x.run(`UPDATE journal_entries SET status = 'posted', posted_at = 1 WHERE id = ?`, id)
}
func (x *tdb) posted(id string, cents int64) {
	x.t.Helper()
	x.draft(id, "2026-09-15")
	x.mustLine(id, 1, "cash", cents, 0)
	x.mustLine(id, 2, "rev", 0, cents)
	if err := x.post(id); err != nil {
		x.t.Fatal(err)
	}
}
func (x *tdb) scalar(q string, args ...any) any {
	var v any
	err := x.db.QueryRow(q, args...).Scan(&v)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		x.t.Fatal(err)
	}
	return v
}
func (x *tdb) status(id string) (status, memo string) {
	x.t.Helper()
	if err := x.db.QueryRow(`SELECT status, memo FROM journal_entries WHERE id = ?`, id).Scan(&status, &memo); err != nil {
		x.t.Fatal(err)
	}
	return
}

func TestPostingAJournalEntry(t *testing.T) {
	x := newTDB(t)
	k := x.key()
	x.posted(k, 5000)
	if s, _ := x.status(k); s != "posted" {
		t.Errorf("status %s", s)
	}
	k = x.key()
	x.draft(k, "2026-09-15")
	x.mustLine(k, 1, "cash", 5000, 0)
	x.mustLine(k, 2, "rev", 0, 4999)
	if err := x.post(k); err == nil || !regexp.MustCompile("unbalanced").MatchString(err.Error()) {
		t.Errorf("unbalanced: %v", err)
	}
	k = x.key()
	x.draft(k, "2026-09-15")
	if err := x.post(k); err == nil || !regexp.MustCompile("at least two lines").MatchString(err.Error()) {
		t.Errorf("no lines: %v", err)
	}
	x.rejects("must be created as draft", `INSERT INTO journal_entries (id, entry_date, memo, status, posted_at) VALUES (?, '2026-09-01', 'm', 'posted', 1)`, x.key())

	k = x.key()
	x.must(`INSERT INTO fiscal_periods (id, fiscal_year, name, start_date, end_date, status) VALUES (?, 2031, 'Sep', '2030-09-01', '2030-09-30', 'closed')`, k)
	x.draft(k, "2030-09-15")
	x.mustLine(k, 1, "cash", 100, 0)
	x.mustLine(k, 2, "rev", 0, 100)
	if err := x.post(k); err == nil || !regexp.MustCompile("period is closed").MatchString(err.Error()) {
		t.Errorf("closed period: %v", err)
	}
}

func TestDraftsAreEditable(t *testing.T) {
	x := newTDB(t)
	k := x.key()
	x.draft(k, "2026-09-15")
	x.mustLine(k, 1, "cash", 100, 0)
	x.must(`UPDATE journal_lines SET debit_cents = 200 WHERE entry_id = ?`, k)
	x.must(`DELETE FROM journal_lines WHERE entry_id = ?`, k)
	x.must(`DELETE FROM journal_entries WHERE id = ?`, k)
	if x.scalar(`SELECT status FROM journal_entries WHERE id = ?`, k) != nil {
		t.Error("draft should be deleted")
	}
}

func TestPostedEntriesAreImmutable(t *testing.T) {
	x := newTDB(t)
	k := x.key()
	x.posted(k, 5000)
	x.rejects("immutable", `UPDATE journal_entries SET memo = 'edited' WHERE id = ?`, k)
	x.rejects("immutable", `UPDATE journal_entries SET status = 'draft', posted_at = NULL WHERE id = ?`, k)
	x.rejects("immutable", `DELETE FROM journal_entries WHERE id = ?`, k)
	x.draft(k+"-other", "2026-09-15")
	x.rejects("immutable", `UPDATE journal_lines SET debit_cents = 1 WHERE id = ?`, k+"-1")
	x.rejects("immutable", `UPDATE journal_lines SET entry_id = ? WHERE id = ?`, k+"-other", k+"-1")
	x.rejects("immutable", `DELETE FROM journal_lines WHERE id = ?`, k+"-1")
	if err := x.line(k, 3, "cash", 1, 0); err == nil || !regexp.MustCompile("immutable").MatchString(err.Error()) {
		t.Errorf("adding a line to a posted entry: %v", err)
	}
}

func TestReversalReferencesPostedEntry(t *testing.T) {
	x := newTDB(t)
	k := x.key()
	x.posted(k, 5000)
	r := k + "-rev"
	x.must(`INSERT INTO journal_entries (id, entry_date, memo, source, reverses_entry_id) VALUES (?, '2026-09-16', 'reverse', 'reversal', ?)`, r, k)
	x.mustLine(r, 1, "rev", 5000, 0)
	x.mustLine(r, 2, "cash", 0, 5000)
	if err := x.post(r); err != nil {
		t.Fatal(err)
	}
	// The partial unique index lets an entry be reversed once.
	r2 := k + "-rev2"
	x.rejects("UNIQUE", `INSERT INTO journal_entries (id, entry_date, memo, source, reverses_entry_id) VALUES (?, '2026-09-17', 'again', 'reversal', ?)`, r2, k)
}

// REPLACE deletes the conflicting row without firing DELETE triggers when recursive_triggers
// is off, so the INSERT guards must look at existing rows.
func TestReplaceAndUpsertCannotRewritePostedBooksOrAuditLog(t *testing.T) {
	x := newTDB(t)
	k := x.key()
	x.posted(k, 100)
	x.rejects("immutable", `INSERT OR REPLACE INTO journal_entries (id, entry_date, memo) VALUES (?, '2026-10-15', 'Overwritten')`, k)
	if s, m := x.status(k); s != "posted" || m != "test" {
		t.Errorf("entry %s %s", s, m)
	}
	x.rejects("immutable", `INSERT INTO journal_entries (id, entry_date, memo) VALUES (?, '2026-10-15', 'x') ON CONFLICT (id) DO UPDATE SET memo = excluded.memo`, k)

	x.draft(k+"-draft", "2026-09-15")
	x.rejects("immutable", `INSERT OR REPLACE INTO journal_lines (id, entry_id, line_no, account_id, fund_id, debit_cents, credit_cents) VALUES (?, ?, 1, 'cash', 'gen', 100, 0)`, k+"-1", k+"-draft")
	x.rejects("immutable", `INSERT OR REPLACE INTO journal_lines (id, entry_id, line_no, account_id, fund_id, debit_cents, credit_cents) VALUES (?, ?, 1, 'cash', 'gen', 999, 0)`, k+"-new", k)
	if n := x.scalar(`SELECT count(*) FROM journal_lines WHERE entry_id = ?`, k); n != int64(2) {
		t.Errorf("posted lines = %v", n)
	}

	x.draft(k+"-d", "2026-09-15")
	x.mustLine(k+"-d", 1, "cash", 7, 0)
	x.rejects("immutable", `UPDATE OR REPLACE journal_entries SET id = ? WHERE id = ?`, k, k+"-d")
	x.rejects("immutable", `UPDATE OR REPLACE journal_lines SET id = ? WHERE id = ?`, k+"-1", k+"-d-1")

	a := x.key()
	x.must(`INSERT INTO audit_log (id, action, entity_type) VALUES (?, 'original', 'test')`, a)
	x.rejects("append-only", `INSERT OR REPLACE INTO audit_log (id, action, entity_type) VALUES (?, 'rewritten', 'test')`, a)
	if got := x.scalar(`SELECT action FROM audit_log WHERE id = ?`, a); got != "original" {
		t.Errorf("audit action %v", got)
	}

	p := x.key()
	x.must(`INSERT INTO fiscal_periods (id, fiscal_year, name, start_date, end_date, status) VALUES (?, 2041, 'Jan', '2041-01-01', '2041-01-31', 'closed')`, p)
	x.rejects("closed fiscal period", `INSERT OR REPLACE INTO fiscal_periods (id, fiscal_year, name, start_date, end_date, status) VALUES (?, 2041, 'Jan', '2041-01-01', '2041-01-31', 'open')`, p+"-new")
	x.must(`INSERT INTO fiscal_periods (id, fiscal_year, name, start_date, end_date) VALUES (?, 2041, 'Feb', '2041-02-01', '2041-02-28')`, p+"-feb")
	x.rejects("closed fiscal period", `UPDATE OR REPLACE fiscal_periods SET id = ? WHERE id = ?`, p, p+"-feb")
	x.rejects("cannot reopen", `UPDATE fiscal_periods SET status = 'open' WHERE id = ?`, p)
	x.rejects("cannot be deleted", `DELETE FROM fiscal_periods WHERE id = ?`, p)
}

func TestAuditLogIsAppendOnly(t *testing.T) {
	x := newTDB(t)
	k := x.key()
	x.must(`INSERT INTO audit_log (id, action, entity_type, entity_id) VALUES (?, 'request.approve', 'purchase_request', 'r1')`, k)
	x.rejects("append-only", `UPDATE audit_log SET action = 'x' WHERE id = ?`, k)
	x.rejects("append-only", `DELETE FROM audit_log WHERE id = ?`, k)
}

func TestJournalLineAmounts(t *testing.T) {
	x := newTDB(t)
	k := x.key()
	x.draft(k, "2026-09-15")
	for i, c := range [][2]any{{-5, 0}, {5, 5}, {0, 0}, {0.5, 0}, {0, 0.5}} {
		err := x.line(k, i+1, "cash", c[0], c[1])
		if err == nil || !regexp.MustCompile("CHECK").MatchString(err.Error()) {
			t.Errorf("line %v: %v", c, err)
		}
	}
	x.mustLine(k, 10, "cash", 50, 0)
	if got := x.scalar(`SELECT typeof(debit_cents) FROM journal_lines WHERE id = ?`, k+"-10"); got != "integer" {
		t.Errorf("typeof %v", got)
	}
}

func TestMoneyColumnsHoldIntegerCents(t *testing.T) {
	x := newTDB(t)
	insert := func(unit, tax, total any) error {
		return x.run(`INSERT INTO purchase_requests (id, number, requester_id, type, title, vendor, quantity, unit_cost_cents, tax_ship_cents, total_cents, expense_account_id, fund_id, justification)
           VALUES (?, ?, 'u1', 'club_purchase', 't', 'v', 1, ?, ?, ?, 'cash', 'gen', 'j')`, x.key(), x.key(), unit, tax, total)
	}
	for _, c := range [][3]any{{10.5, 0, 10.5}, {10, 0.25, 10.25}} {
		if err := insert(c[0], c[1], c[2]); err == nil {
			t.Errorf("fractional request %v accepted", c)
		}
	}
	if err := insert(10, 5, 15); err != nil {
		t.Error(err)
	}
	x.rejects("CHECK", `INSERT INTO budgets (id, fiscal_year, account_id, amount_cents) VALUES (?, 2050, 'cash', 99.9)`, x.key())
	x.rejects("CHECK", `INSERT INTO contributions (id, donor_id, fund_id, received_date, kind, amount_cents) VALUES (?, 'd1', 'gen', '2026-09-01', 'cash', 100.5)`, x.key())
	x.rejects("CHECK", `INSERT INTO funds (id, code, name, kind, restriction, amount_awarded_cents) VALUES (?, ?, 'g', 'grant', 'with_donor', 1.5)`, x.key(), x.key())
}

func TestBudgetsAreUniquePerAccountFundYear(t *testing.T) {
	x := newTDB(t)
	ins := func(id string) error {
		return x.run(`INSERT INTO budgets (id, fiscal_year, account_id, fund_id, amount_cents) VALUES (?, 2060, 'cash', NULL, 100)`, id)
	}
	if err := ins(x.key()); err != nil {
		t.Fatal(err)
	}
	if err := ins(x.key()); err == nil || !regexp.MustCompile("UNIQUE").MatchString(err.Error()) {
		t.Errorf("second all-funds budget: %v", err)
	}
}

func TestBackendGuards(t *testing.T) {
	x := newTDB(t)
	// The last active director cannot be demoted or deactivated.
	x.must(`INSERT INTO users (id, discord_id, name, role) VALUES ('dir1', '100000000000000010', 'Dir', 'director')`)
	x.rejects("last active director", `UPDATE users SET active = 0 WHERE id = 'dir1'`)
	x.rejects("last active director", `UPDATE users SET role = 'officer' WHERE id = 'dir1'`)
	x.must(`INSERT INTO users (id, discord_id, name, role) VALUES ('dir2', '100000000000000011', 'Dir2', 'director')`)
	x.must(`UPDATE users SET active = 0 WHERE id = 'dir1'`)
	x.rejects("last active director", `UPDATE users SET active = 0 WHERE id = 'dir2'`)

	// A request can be approved once, and a settlement posted once.
	x.must(`INSERT INTO purchase_requests (id, number, requester_id, type, title, vendor, quantity, unit_cost_cents, tax_ship_cents, total_cents, expense_account_id, fund_id, justification)
           VALUES ('rq', '2027-0001', 'u1', 'club_purchase', 't', 'v', 1, 100, 0, 100, 'cash', 'gen', 'j')`)
	x.must(`INSERT INTO approvals (id, request_id, director_id, decision) VALUES ('a1', 'rq', 'dir2', 'approved')`)
	x.rejects("UNIQUE", `INSERT INTO approvals (id, request_id, director_id, decision) VALUES ('a2', 'rq', 'dir2', 'approved')`)
	x.must(`INSERT INTO approvals (id, request_id, director_id, decision) VALUES ('a3', 'rq', 'dir2', 'denied')`)
	x.must(`INSERT INTO journal_entries (id, entry_date, memo, settlement_key) VALUES ('s1', '2026-09-15', 'm', 'rq')`)
	x.rejects("UNIQUE", `INSERT INTO journal_entries (id, entry_date, memo, settlement_key) VALUES ('s2', '2026-09-15', 'm', 'rq')`)

	// `held` is a valid job status after migration 0003.
	x.must(`INSERT INTO fulfillment_jobs (id, request_id, status) VALUES ('j1', 'rq', 'held')`)
	x.rejects("CHECK", `INSERT INTO fulfillment_jobs (id, request_id, status) VALUES ('j2', 'rq', 'bogus')`)
}

func TestForeignKeysAreOn(t *testing.T) {
	x := newTDB(t)
	x.rejects("FOREIGN KEY", `INSERT INTO approvals (id, request_id, director_id, decision) VALUES ('a', 'nope', 'u1', 'approved')`)
}

func TestMigrationsAreIdempotentAndReaderSeesWriterCommits(t *testing.T) {
	path := t.TempDir() + "/m.db"
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.W.Exec(`INSERT INTO users (id, discord_id, name) VALUES ('u', '1', 'n')`); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := d.R.QueryRow(`SELECT count(*) FROM users`).Scan(&n); err != nil || n != 1 {
		t.Errorf("reader sees %d users (%v)", n, err)
	}
	if _, err := d.R.Exec(`INSERT INTO users (id, discord_id, name) VALUES ('x', '2', 'n')`); err == nil {
		t.Error("the reader pool must be read-only")
	}
	d.Close()
	d, err = Open(path)
	if err != nil {
		t.Fatalf("reopening re-runs migrations: %v", err)
	}
	defer d.Close()
	var mode string
	_ = d.W.QueryRow(`PRAGMA journal_mode`).Scan(&mode)
	if mode != "wal" {
		t.Errorf("journal_mode = %s", mode)
	}
	var applied int
	_ = d.W.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&applied)
	if applied != 6 {
		t.Errorf("applied migrations = %d (want 6)", applied)
	}
}
