# cseed officer dashboard

Internal dashboard for CSEED officers: event health (Luma registrations → check-ins →
reflection surveys), budget status, a purchase-request flow (officer submits → director
approves → fulfilment), and a nonprofit-grade double-entry ledger that can later produce
Form 990 / Washington filings and financial statements.

Sign-in is Discord OAuth, limited to an allowlist of officers. The design follows
[cseed.co](https://cseed.co): paper and ink, Playfair Display, Poppins, Anonymous Pro.

## Layout

| Path | What it is |
| --- | --- |
| `server/` | The Go backend: JSON API, SQLite (embedded migrations), Discord sign-in, receipts, notifications; serves `web/dist`. Runs on a Raspberry Pi or in a Cloudflare Container. See [server/README.md](server/README.md). |
| `web/` | The static Astro + Preact front end (five pages, sidebar, ⌘K search) and its Playwright suite. See [web/README.md](web/README.md). |
| `docs/api.md` | The HTTP API reference. |
| `src/core/contracts.ts` | The zod schemas for every request and response (`domain.ts` holds the enums it uses). The single source of truth for the API shape. |
| `scripts/contract-check.mjs` | CI job logic: starts the Go server on a seeded temp database and validates every response against `contracts.ts`. |

The old v1 TypeScript / Cloudflare Workers / D1 app has been removed; it lives in git history
before the v1-cleanup PR.

## Quick start

Needs Go 1.24+ with a C compiler (cgo, for SQLite) and Node ≥ 22.23.

```sh
(cd web && npm ci && npm run build)   # static front end -> web/dist
cd server && make dev                 # seeds ./data/dev.db, serves with the dev login
```

See [server/README.md](server/README.md) for configuration, secrets and deployment, and
[web/README.md](web/README.md) for the front-end dev server.

## Checks

| Check | Command |
| --- | --- |
| Go vet + tests | `cd server && go vet ./... && go test ./...` |
| API contract | `npm ci && npm run contract-check` (builds and runs the Go server itself) |
| Front end build + types | `cd web && npm ci && npm run build && npx astro check` |
| Front end e2e (local only) | `cd web && npm run test:e2e` (Playwright; not run in CI) |

CI (`.github/workflows/ci.yml`) runs the `go`, `contract` and `web` jobs on every push to
`main` and every pull request.

## Accounting model and operating rules

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
currently run July–June (FY2027 ends June 2027). Period creation/close and manual ledger/
contribution entry are engine/adapter operations, not dashboard API workflows;
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

## CSV import and service integrations

CSV imports accept UTF-8 (optional BOM), quoted commas/newlines, and CRLF or LF. Header
matching ignores case, spaces, underscores and punctuation. Guest files need `email`;
optional headers are `name` / `full name`, `guest id` / `api id`, `approval status` /
`registration status`, `registered at` / `registration time`, and `checked in at` /
`check-in time`. Reflection files need `email` / `email address` and `timestamp` /
`submitted at`. Timestamps with an offset are preferred. Offset-less ISO dates/times or
`MM/DD/YYYY HH:mm:ss [AM|PM]` are interpreted in America/Los_Angeles; nonexistent spring
DST times are rejected and ambiguous autumn times choose the earlier instant.

Emails are trimmed/lowercased; duplicate event/email rows upsert. Blank/missing CSV fields and omitted JSON fields preserve existing values, including
check-ins; explicit JSON null clears a nullable guest field.
Pending approval, invited, waitlisted, declined and cancelled guests do not count as
registrations unless they checked in. Reflection rates count distinct respondents
matched to checked-in guests. Bad rows are reported by physical source row and skipped;
`dryRun=true` previews the merged metrics without writes, including for a new event.
Files are limited to 5 MB; JSON
ingest is limited to 5,000 guests by contract.

Directors issue service tokens once, store only SHA-256 hashes, and may revoke them.
`ingest:luma` permits `/api/ingest/luma`; `agent:jobs` permits jobs list/claim/complete.
A failed job returns its request to approved, clears its token claim and re-queues
automatically while fewer than three total claims have been attempted. The default
queued-job poll discovers retries. After the third failed attempt the job stays failed
for a director to resolve manually. Only the current claiming token may complete it. Revoking a token
prevents future use; deactivating a user ends sessions but does not revoke independently
issued service tokens (revoke those separately).

**Agent settlement tolerance.** The purchasing agent's `POST /api/agent/jobs/:id/complete`
may post an `actualTotalCents` of up to the larger of the estimate + 10% and the
estimate + $25 (set by `AGENT_SETTLEMENT_TOLERANCE_PERCENT` / `_CENTS`). Above that it
gets a 409 and the job becomes `held`: its claim is cleared, so later complete, failed or
claim calls from the agent return 409 and can never re-queue it (no second purchase).
Directors get a `request.held` notification with the attempted vs approved amount, and
the request page shows a "held for director review" banner (`settlementHold`). A
director settles it with `POST /api/requests/:id/transition` (`to: 'fulfilled'`), which
clears the hold; sending it back to `approved` cancels the held job instead. The
director's own settlement has no limit.

Receipts allow PDF, PNG, JPEG, HEIC and WebP, up to 10 MB. Configure
`GOOGLE_SERVICE_ACCOUNT_JSON` and `DRIVE_ROOT_FOLDER_ID` (the older
`GOOGLE_DRIVE_FOLDER_ID` is accepted as an alias). Share the Drive folder with the
service-account identity. The adapter uses WebCrypto RS256 JWT exchange, Drive v3
multipart uploads and shared-drive flags, following Google's
[service-account protocol](https://developers.google.com/identity/protocols/oauth2/service-account)
and [upload API](https://developers.google.com/workspace/drive/api/guides/manage-uploads).
The token requests the full `https://www.googleapis.com/auth/drive` scope to access the pre-existing shared folder. Files are
stored under `FY<year>/<request-id>/`, and officers need Drive access to open its links.
Folder lookups search the shared drive that holds the root folder (`corpora=drive` +
`driveId`, or `corpora=allDrives` if the root isn't in a shared drive). When two first
uploads race to create the same folder, both re-list after creating, keep the oldest,
and the loser's empty duplicate is trashed. Trashing needs the service account to be a
**Content manager** of the shared drive; as a Contributor the empty duplicate just stays.

**Lock down the service account.** Because the scope is the full `drive` scope, the key
reaches every file shared with that account. Make the service account a member of the
receipts shared drive **only** (don't share other folders or drives with it), and keep
its key **only** in the server's environment / secret store (`GOOGLE_SERVICE_ACCOUNT_JSON`):
never in the repo, CI variables or chat. Rotate the key if it leaks.

Production receipt operations return HTTP 503 if Drive credentials or the root folder
are missing. Development selects the local-disk file store.
Uploads precede the attachment metadata transaction, so a failed metadata commit may
leave an unreferenced file for later cleanup. MIME validation checks the declared type;
there is no virus scan or content classification.

`DISCORD_WEBHOOK_URL` enables bounded, best-effort embeds; unset webhooks log events.
Delivery failure does not undo the committed purchase. Live Google/Discord credentials
are not needed for the local test suite; production delivery must be checked with the
organization's actual credentials.
