// Package accounting is the double-entry core: journal validation, the posting engine and
// the financial reports. It is pure (no database); the store package persists its results.
package accounting

const MaxSafe = 1<<53 - 1

// Account is a chart-of-accounts row.
type Account struct {
	ID          string
	Code        string
	Name        string
	Type        string // asset | liability | net_asset | revenue | expense
	Form990Part *string
	Form990Line *string
	Form990Lbl  *string
	Restriction *string // without_donor | with_donor (net assets)
	Budgetable  bool
	Active      bool
}

// Fund is a fund row.
type Fund struct {
	ID                 string
	Code               string
	Name               string
	Kind               string // general | grant | sponsor | other
	Restriction        string // without_donor | with_donor
	Active             bool
	StartDate          *string
	EndDate            *string
	AmountAwardedCents *int64
}

// Period is a fiscal period.
type Period struct {
	ID        string
	StartDate string
	EndDate   string
	Status    string // open | closed | locked
}

// Context is what the posting engine validates entries against.
type Context struct {
	Accounts        []Account
	Funds           []Fund
	Periods         []Period
	ReversedEntries []string
}

// Line is a journal line.
type Line struct {
	AccountID       string
	FundID          string
	DebitCents      int64
	CreditCents     int64
	FunctionalClass string // "" = none
	Memo            string
}

// Entry is a journal entry to post.
type Entry struct {
	ID              string
	EntryDate       string
	Memo            string
	Source          string
	Lines           []Line
	ReversesEntryID string
}
