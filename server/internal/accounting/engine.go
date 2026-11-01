package accounting

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ValidateLines mirrors the database triggers so the API can answer with friendly errors
// before the database refuses. It returns the entry total, or the list of problems.
func ValidateLines(lines []Line) (int64, []string) {
	var errs []string
	if len(lines) < 2 {
		errs = append(errs, "an entry needs at least two lines")
	}
	var debits, credits int64
	for i, l := range lines {
		n := i + 1
		if l.DebitCents < 0 || l.CreditCents < 0 || l.DebitCents > MaxSafe || l.CreditCents > MaxSafe {
			errs = append(errs, fmt.Sprintf("line %d: amounts must be non-negative integer cents", n))
			continue
		}
		if (l.DebitCents == 0) == (l.CreditCents == 0) {
			errs = append(errs, fmt.Sprintf("line %d: exactly one of debit or credit must be non-zero", n))
		}
		debits += l.DebitCents
		credits += l.CreditCents
	}
	if debits > credits {
		errs = append(errs, fmt.Sprintf("debits exceed credits by %d cents", debits-credits))
	}
	if credits > debits {
		errs = append(errs, fmt.Sprintf("credits exceed debits by %d cents", credits-debits))
	}
	if len(errs) > 0 {
		return 0, errs
	}
	return debits, nil
}

func validDate(s string) bool {
	if len(s) != 10 {
		return false
	}
	t, err := time.Parse("2006-01-02", s)
	return err == nil && t.Format("2006-01-02") == s
}

// PostEntry validates an entry against the context and returns it ready to persist.
// Error messages are matched by the API layer to choose 409 vs 500.
func PostEntry(in Entry, ctx Context) (Entry, error) {
	total, errs := ValidateLines(in.Lines)
	if len(errs) > 0 {
		return Entry{}, errors.New(strings.Join(errs, "; "))
	}
	if total > MaxSafe {
		return Entry{}, errors.New("unsafe total cents")
	}
	if !validDate(in.EntryDate) {
		return Entry{}, errors.New("invalid entry date")
	}
	matched, allOpen := 0, true
	for _, p := range ctx.Periods {
		if p.StartDate <= in.EntryDate && p.EndDate >= in.EntryDate {
			matched++
			if p.Status != "open" {
				allOpen = false
			}
		}
	}
	if matched == 0 || !allOpen {
		return Entry{}, errors.New("entry requires an open period")
	}
	for _, l := range in.Lines {
		var acct *Account
		for i := range ctx.Accounts {
			if ctx.Accounts[i].ID == l.AccountID && ctx.Accounts[i].Active {
				acct = &ctx.Accounts[i]
				break
			}
		}
		fundOK := false
		for _, f := range ctx.Funds {
			if f.ID == l.FundID && f.Active {
				fundOK = true
				break
			}
		}
		if acct == nil || !fundOK {
			return Entry{}, errors.New("inactive or unknown account/fund")
		}
		if acct.Type == "expense" && l.FunctionalClass == "" {
			return Entry{}, errors.New("expense requires functional class")
		}
	}
	out := in
	out.Lines = append([]Line(nil), in.Lines...)
	return out, nil
}

// Reverse builds the mirror-image entry of a posted one.
func Reverse(entry Entry, id, entryDate string, ctx Context) (Entry, error) {
	for _, r := range ctx.ReversedEntries {
		if r == entry.ID {
			return Entry{}, errors.New("entry already reversed")
		}
	}
	lines := make([]Line, len(entry.Lines))
	for i, l := range entry.Lines {
		l.DebitCents, l.CreditCents = l.CreditCents, l.DebitCents
		lines[i] = l
	}
	return PostEntry(Entry{ID: id, EntryDate: entryDate, Memo: "Reversal: " + entry.Memo, Source: "reversal", ReversesEntryID: entry.ID, Lines: lines}, ctx)
}

// ClosePeriod moves open → closed|locked or closed → locked; nothing reopens.
func ClosePeriod(p Period, to string) (Period, error) {
	if p.Status == "locked" || (p.Status == "closed" && to != "locked") {
		return Period{}, errors.New("period cannot reopen")
	}
	p.Status = to
	return p, nil
}

// Release reclassifies net assets from with-donor to without-donor, keeping the fund
// for purpose tracking.
type Release struct {
	ID, EntryDate, Memo, FundID                  string
	AmountCents                                  int64
	RestrictedNetAssetID, UnrestrictedNetAssetID string
	AvailableRestrictedCents                     int64
}

// ReleaseRestriction posts the reclassification entry.
func ReleaseRestriction(in Release, ctx Context) (Entry, error) {
	find := func(id string) *Account {
		for i := range ctx.Accounts {
			if ctx.Accounts[i].ID == id {
				return &ctx.Accounts[i]
			}
		}
		return nil
	}
	restricted, unrestricted := find(in.RestrictedNetAssetID), find(in.UnrestrictedNetAssetID)
	fundOK := false
	for _, f := range ctx.Funds {
		if f.ID == in.FundID && f.Restriction == "with_donor" {
			fundOK = true
		}
	}
	isRestriction := func(a *Account, want string) bool {
		return a != nil && a.Type == "net_asset" && a.Restriction != nil && *a.Restriction == want
	}
	if !fundOK || !isRestriction(restricted, "with_donor") || !isRestriction(unrestricted, "without_donor") {
		return Entry{}, errors.New("release requires donor → without donor net asset accounts")
	}
	if in.AmountCents > in.AvailableRestrictedCents || in.AvailableRestrictedCents > MaxSafe {
		return Entry{}, errors.New("release exceeds available restricted net assets")
	}
	return PostEntry(Entry{
		ID: in.ID, EntryDate: in.EntryDate, Memo: in.Memo, Source: "release",
		Lines: []Line{
			{AccountID: restricted.ID, FundID: in.FundID, DebitCents: in.AmountCents},
			{AccountID: unrestricted.ID, FundID: in.FundID, CreditCents: in.AmountCents},
		},
	}, ctx)
}

// Obligations are the IRS donor-substantiation consequences of one contribution
// (IRC §170(f)(8) and §6115). Not tax advice; have the CPA confirm.
type Obligations struct {
	AcknowledgmentRequired       bool
	QuidProQuoDisclosureRequired bool
	DeductibleCents              int64
}

const (
	AcknowledgmentThresholdCents = 25_000 // $250
	QuidProQuoThresholdCents     = 7_500  // $75
)

// ContributionObligations computes the substantiation duties for a single gift.
func ContributionObligations(amountCents, quidProQuoCents int64) Obligations {
	ded := amountCents - quidProQuoCents
	if ded < 0 {
		ded = 0
	}
	return Obligations{
		AcknowledgmentRequired:       amountCents >= AcknowledgmentThresholdCents,
		QuidProQuoDisclosureRequired: amountCents > QuidProQuoThresholdCents && quidProQuoCents > 0,
		DeductibleCents:              ded,
	}
}

// Contribution is a gift plus the journal entry that records it.
type Contribution struct {
	Entry             Entry
	AmountCents       int64
	QuidProQuoCents   int64
	Kind              string
	InKindDescription string
}

// ContributionResult is the posted entry and the donor-substantiation flags.
type ContributionResult struct {
	Entry Entry
	Obligations
	InKind bool
}

// RecordContribution validates and posts a contribution.
func RecordContribution(in Contribution, ctx Context) (ContributionResult, error) {
	if in.AmountCents <= 0 || in.AmountCents > MaxSafe || in.QuidProQuoCents < 0 || in.QuidProQuoCents > in.AmountCents {
		return ContributionResult{}, errors.New("invalid contribution cents")
	}
	if in.Kind == "in_kind" && strings.TrimSpace(in.InKindDescription) == "" {
		return ContributionResult{}, errors.New("in-kind description required")
	}
	total, errs := ValidateLines(in.Entry.Lines)
	if len(errs) > 0 || total != in.AmountCents {
		return ContributionResult{}, errors.New("contribution entry must match contribution amount")
	}
	var revenue int64
	funds := map[string]bool{}
	for _, l := range in.Entry.Lines {
		funds[l.FundID] = true
		for _, a := range ctx.Accounts {
			if a.ID == l.AccountID && a.Type == "revenue" {
				revenue += l.CreditCents - l.DebitCents
			}
		}
	}
	if revenue != in.AmountCents {
		return ContributionResult{}, errors.New("contribution revenue must match amount")
	}
	if len(funds) != 1 {
		return ContributionResult{}, errors.New("contribution must use one fund")
	}
	e := in.Entry
	e.Source = "contribution"
	posted, err := PostEntry(e, ctx)
	if err != nil {
		return ContributionResult{}, err
	}
	return ContributionResult{Entry: posted, Obligations: ContributionObligations(in.AmountCents, in.QuidProQuoCents), InKind: in.Kind == "in_kind"}, nil
}
