package seed

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"officerdashboard/internal/fiscal"
	"officerdashboard/internal/store"
)

func seeded(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(t.TempDir() + "/seed.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db
}

func one(t *testing.T, db *store.DB, q string) int64 {
	t.Helper()
	var n sql.NullInt64
	if err := db.R.QueryRow(q).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n.Int64
}

func TestAppliesCleanlyOnTopOfTheMigrations(t *testing.T) {
	db := seeded(t)
	for q, want := range map[string]int64{
		`SELECT count(*) FROM users WHERE role = 'leadership'`:                                       1,
		`SELECT count(*) FROM subteams`:                                                              4,
		`SELECT count(*) FROM officer_profiles`:                                                      6,
		`SELECT count(*) FROM users WHERE role = 'director'`:                                         1,
		`SELECT count(*) FROM users WHERE role = 'officer'`:                                          4,
		`SELECT count(*) FROM design_requests`:                                                       3,
		`SELECT count(*) FROM events`:                                                                6,
		`SELECT count(*) FROM funds WHERE restriction = 'with_donor'`:                                2,
		`SELECT count(*) FROM journal_entries WHERE status <> 'posted'`:                              0,
		`SELECT count(*) FROM accounts WHERE type IN ('revenue','expense') AND form990_line IS NULL`: 0,
	} {
		if got := one(t, db, q); got != want {
			t.Errorf("%s = %d want %d", q, got, want)
		}
	}
	if got := one(t, db, `SELECT count(DISTINCT status) FROM purchase_requests`); got < 5 {
		t.Errorf("only %d distinct statuses", got)
	}
}

func TestEveryEntryIsBalancedAndTheBooksBalance(t *testing.T) {
	db := seeded(t)
	if d, c := one(t, db, `SELECT sum(debit_cents) FROM journal_lines`), one(t, db, `SELECT sum(credit_cents) FROM journal_lines`); d != c {
		t.Errorf("debits %d credits %d", d, c)
	}
	bal := func(typ string) int64 {
		return one(t, db, fmt.Sprintf(`SELECT coalesce(sum(l.debit_cents - l.credit_cents), 0) FROM journal_lines l JOIN accounts a ON a.id = l.account_id WHERE a.type = '%s'`, typ))
	}
	assets, liabilities, netAssets := bal("asset"), -bal("liability"), -bal("net_asset")
	surplus := -bal("revenue") - bal("expense")
	if assets != liabilities+netAssets+surplus {
		t.Errorf("assets %d ≠ liabilities %d + net assets %d + surplus %d", assets, liabilities, netAssets, surplus)
	}
	// Worked example: cash 3,200 + 5,000 + 1,500 + 300 − 405 − 186.40 = 9,408.60,
	// plus the 2,500 grant receivable = 11,908.60.
	if assets != 1_190_860 {
		t.Errorf("assets = %d", assets)
	}
}

func TestRequestNumbersMatchTheAPIScheme(t *testing.T) {
	db := seeded(t)
	rows, err := db.R.Query(`SELECT number, submitted_at, created_at FROM purchase_requests ORDER BY number`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	i := 0
	for rows.Next() {
		i++
		var number string
		var submitted sql.NullInt64
		var created int64
		_ = rows.Scan(&number, &submitted, &created)
		at := created
		if submitted.Valid {
			at = submitted.Int64
		}
		fy, _ := fiscal.YearFor(fiscal.PacificDate(timeMs(at)), fiscal.DefaultStartMonth)
		if number != fmt.Sprintf("%d-%04d", fy, i) {
			t.Errorf("request %d numbered %s (fiscal year %d)", i, number, fy)
		}
	}
	if i == 0 {
		t.Error("no requests")
	}
}

func TestIsIdempotent(t *testing.T) {
	db := seeded(t)
	if err := Apply(context.Background(), db); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if got := one(t, db, `SELECT count(*) FROM journal_lines`); got != 18 {
		t.Errorf("journal lines %d", got)
	}
	if got := one(t, db, `SELECT count(*) FROM users`); got != 6 {
		t.Errorf("users %d", got)
	}
}

func TestGuestListsAreTheTypeScriptSeedByteForByte(t *testing.T) {
	db := seeded(t)
	// Values computed from the v1 TypeScript seed (mulberry32 + the same arithmetic).
	var email string
	var registered, checkedIn sql.NullInt64
	if err := db.R.QueryRow(`SELECT email, registered_at, checked_in_at FROM event_guests WHERE id = 'event-kickoff-g0'`).Scan(&email, &registered, &checkedIn); err != nil {
		t.Fatal(err)
	}
	if email != "alex.kim0@example.edu" || !checkedIn.Valid {
		t.Errorf("first guest %s %v", email, checkedIn)
	}
	if got := one(t, db, `SELECT count(*) FROM event_guests WHERE event_id = 'event-kickoff' AND checked_in_at IS NOT NULL`); got != 50 {
		t.Errorf("kickoff check-ins %d", got)
	}
	if got := one(t, db, `SELECT count(*) FROM reflections WHERE event_id = 'event-kickoff'`); got != 21 {
		t.Errorf("kickoff reflections %d", got)
	}
}

func timeMs(ms int64) time.Time { return time.UnixMilli(ms) }
