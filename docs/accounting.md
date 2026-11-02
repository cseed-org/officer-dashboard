# Accounting model and operating rules

All ledger amounts are safe integer cents. Account codes map natural categories to
Form 990 Part VIII (revenue), IX (expense) and X (position); the `form-990` report groups
VIII/IX lines, and the functional expense report splits program, management & general,
and fundraising. This is a preparatory rollup, not a complete filing or WA return.
Funds retain grant/sponsor purpose tracking independently of net asset class. Fund balances
include restricted/unrestricted ending amounts; releases show the gross reclassification
as an informational column, not an additional deduction from the combined ending balance. Revenue
uses the donor restriction of its fund; expenses are always presented without donor
restrictions. Settling a restricted-fund purchase also posts Dr 3100 / Cr 3000 in that
same fund, releasing the actual cost from donor restriction. The transaction refuses
to release more than the remaining restricted balance. A director's approval is the
operational assertion that the purchase satisfies the grant purpose; time-restricted
or otherwise conditional gifts need CPA review before being spent.

Approval records an encumbrance memo in the audit log; approved/fulfilling requests
count as commitments. A $100 estimate settled for $95 posts $95 of expense and releases
all $100 of commitment. Club purchases credit checking (1000); reimbursements credit
reimbursements payable (2100). **Closing a reimbursement confirms it has been paid**
and posts Dr 2100 / Cr 1000. Closing a club purchase only archives it. Free settlements
(actual cost zero) retain the lifecycle/audit evidence and post no zero-value journal.
Receipts selected at settlement link to its expense entry. Draft deletion records its
attachment metadata in the immutable audit log; stored bytes are retained.

Posting requires a matching open fiscal period, at least two one-sided balanced lines,
active accounts/funds, and a functional class on expense lines. Posted entries and lines
are immutable in both the engine and SQL triggers; correct them with reversing entries.
A period can move open → closed → locked and cannot reopen. Period close is a posting
lock, not a year-end sweep of revenue/expense accounts: cumulative trial balances retain
prior-year nominal accounts until explicit closing entries are prepared. Fiscal years
currently run July–June (FY2027 ends June 2027). Manual ledger/contribution entry remains an engine/adapter operation. Fiscal period creation and close are available through director APIs;
provision future open periods before the current year ends.

Pure functions are in `server/internal/accounting/`; the API persists posting statements with
request state and audit rows in one SQLite transaction. Contributions validate their journal total
and revenue against the gift amount, track in-kind descriptions, and compute donor
acknowledgment and quid-pro-quo disclosure flags. The thresholds follow the
[IRS substantiation guidance](https://www.irs.gov/charities-non-profits/charitable-organizations/charitable-organizations-substantiation-and-disclosure-requirements).

**CPA review is required before keeping real books or preparing filings.** Confirm the
chart mappings, fiscal year, restricted gift terms, in-kind valuation, capitalization,
revenue recognition and allocation policy. Automatic release assumes a purpose
restriction satisfied by the approved purchase; it cannot interpret donor agreements.
