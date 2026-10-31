# cseed officer dashboard

Internal dashboard for CSEED officers: event health (Luma registrations → check-ins →
reflection surveys), budget status, a purchase-request flow (officer submits → director
approves → fulfilment), and a nonprofit-grade double-entry ledger that can later produce
Form 990 / Washington filings and financial statements.

One Astro app (SSR) on Cloudflare Workers with D1 (SQLite) through Drizzle ORM. Sign-in
is Discord OAuth, limited to an allowlist of officers. The design follows
[cseed.co](https://cseed.co): paper and ink, Playfair Display, Poppins, Anonymous Pro.

> **Status: Phase 1.** API handlers, accounting, CSV ingest, Drive storage, Discord
> notifications and every page (overview, events, requests, finance, admin) are implemented.

## Quick start

Needs Node ≥ 22.23 (tested on Node 26, Raspberry Pi 5 / linux arm64).

```sh
npm install
npm run db:migrate:local   # create the local D1 database (.wrangler/state)
npm run db:seed:local      # sample officers, events, requests, budget, journal
npm run dev                # http://localhost:4321
```

`npm run dev` creates `.dev.vars` from `.dev.vars.example` on first run, which turns on
the **dev login**: open <http://localhost:4321/login> and choose *director* or *officer*
(or go straight to `/auth/dev?as=director`). You don't need a Discord app for local work.

> When Astro detects a coding agent it starts the dev server in the background
> (`astro dev stop|status|logs`). Add `--ignore-lock` to keep it in the foreground.

Reset the local database: `npm run db:reset:local`. Do this after pulling a change
that rewrites existing migrations (as PR #1's review fixes did, before anything was deployed).

## Scripts

| Script | What it does |
| --- | --- |
| `dev` | Astro dev server, running the Worker in workerd with local D1 |
| `build` | Production build to `dist/`, then `scripts/check-build.mjs` fails if the dev login survived |
| `preview` | Build, then serve `dist/` with `wrangler dev` |
| `typecheck` | `wrangler types` + `astro check` + `tsc` for tests and scripts |
| `test` | Vitest: core logic, the contract, migrations/triggers, seed |
| `test:e2e` | Playwright smoke test on phone 390×844, tablet 820×1180, desktop 1440×900 |
| `db:generate` | Diff `src/server/db/schema.ts` into a new SQL migration in `migrations/` |
| `db:migrate:local` / `db:migrate:remote` | Apply `migrations/*.sql` to local / production D1 |
| `db:seed:local` | Generate `scripts/seed` SQL and run it on local D1 (safe to re-run) |
| `db:reset:local` | Delete local D1, migrate, seed |
| `deploy` | Build, migrate remote D1, `wrangler deploy` |

Playwright needs a browser once: `npx playwright install chromium`. The e2e run reuses a dev
server on port 4321 or starts one (migrating and seeding first; set `E2E_PORT` to change it)
and saves a screenshot of every page, light and dark, to `tmp/screenshots/<viewport>/<scheme>/`,
plus empty / error / loading states under `states_*`.

## Environment and secrets

Local values go in `.dev.vars` (git-ignored). In production, set secrets with
`wrangler secret put <NAME>`; non-secret vars live in `wrangler.jsonc`.

| Name | Kind | Needed for |
| --- | --- | --- |
| `DB` | D1 binding | Everything. `wrangler d1 create cseed-officer-dashboard`, then put the id in `wrangler.jsonc` |
| `APP_URL` | var | Public origin used for the OAuth redirect, e.g. `https://officers.cseed.co` |
| `DISCORD_CLIENT_ID` | secret | Discord sign-in |
| `DISCORD_CLIENT_SECRET` | secret | Discord sign-in |
| `BOOTSTRAP_DIRECTOR_DISCORD_ID` | secret/var | Your Discord user id; created as the first director on first sign-in |
| `GOOGLE_SERVICE_ACCOUNT_JSON` | secret | Receipt uploads to Google Drive (service-account key JSON) |
| `DRIVE_ROOT_FOLDER_ID` | secret/var | Shared-drive folder receipts are filed under |
| `DISCORD_WEBHOOK_URL` | secret | Request notifications to a Discord channel |
| `DEV_AUTH` | `.dev.vars` only | `1` enables `/auth/dev` in `astro dev`. Production builds compile it out |

### Discord app

1. <https://discord.com/developers/applications> → New Application → OAuth2.
2. Add redirects `http://localhost:4321/auth/callback` and `https://<your domain>/auth/callback`.
3. Copy the client id and secret into `.dev.vars` / `wrangler secret put`.
4. Only the `identify` scope is requested. Sign-in is allowed only for Discord ids in the
   `users` table (directors add officers on `/admin/users`), plus the bootstrap director.

## Deploy (Cloudflare)

```sh
npx wrangler login
npx wrangler d1 create cseed-officer-dashboard      # copy database_id into wrangler.jsonc
npx wrangler secret put DISCORD_CLIENT_ID           # repeat for each secret above
npm run deploy                                      # build → migrate remote D1 → deploy
```

Then add a custom domain to the Worker and set `APP_URL` to it.

## Architecture

```
src/
  core/            framework-free domain logic: no Astro, Cloudflare or Drizzle imports
    contracts.ts   zod schemas + types for every API request/response (the UI ↔ backend contract)
    domain.ts      enums (roles, statuses, account types, functional classes, …)
    auth.ts        role rules, path → required role, dev-login guard, login allowlist decision
    ports.ts       FileStore and Notifier interfaces
    money.ts, metrics.ts, journal.ts, purchase-lifecycle.ts, fiscal.ts, contributions.ts
  server/          adapters
    db/            Drizzle schema (all tables) and D1 client
    auth/          sessions (hashed tokens in D1), Discord OAuth (arctic), requireRole()
    files/         FileStore: in-memory stub now, Google Drive in Phase 1
    notify/        Notifier: console stub now, Discord webhook in Phase 1
    env.ts         the one place that reads Worker bindings
  middleware.ts    attaches locals.user, enforces roles per path (403 for officers on /admin/*)
  pages/api/       thin JSON handlers (parse with contracts → call core → respond)
  pages/           UI routes: server-rendered shell + h1, body is a Preact island
  ui/              the UI layer (Phase 1)
    api/client.ts  api('overview', …): typed by ENDPOINTS, responses validated by the contract schema
    islands/       one interactive island per page (OverviewView, RequestFormView, FinanceView, …)
    components/    states (loading/empty/error), tables that become cards on phones, SVG charts, forms
    mocks/         seed-shaped mock API behind ONE switch (mocks/switch.ts) while the backend lands
  styles/tokens/   CSEED colour (light + dark), type and layout tokens
migrations/        SQL migrations applied by wrangler (0001 = integrity triggers)
scripts/seed/      seed data and SQL generator
tests/core, tests/db   Vitest     tests/e2e   Playwright
docs/api.md        endpoint table: method, path, role, schema names
```

`src/core` stays portable: a later Raspberry Pi / Node deployment swaps the adapters
(`@astrojs/node`, better-sqlite3 with the same Drizzle schema, a filesystem FileStore)
without touching domain code.

### Data integrity

- Money is stored as integer cents everywhere. CHECK `typeof(col) = 'integer'` rejects
  fractional values, which SQLite's INTEGER affinity would otherwise store as REAL.
- Enum columns have CHECK constraints; journal lines must be non-negative with exactly one
  of debit or credit; purchase totals must equal quantity × unit cost + tax/shipping.
- Triggers (`migrations/0001_integrity_triggers.sql`): journal entries are created as
  drafts and can only be posted when balanced, with ≥ 2 lines, outside closed fiscal
  periods. **Posted entries and their lines can't be updated or deleted**; corrections
  are reversing entries. `audit_log` is append-only.
- D1 runs with `recursive_triggers` off, so `INSERT OR REPLACE` / `UPDATE OR REPLACE`
  delete conflicting rows without firing DELETE triggers. Extra guards therefore refuse any
  insert or key change that collides with a posted entry or line, an audit row, or a
  closed fiscal period. `tests/db/integrity-triggers.test.ts` runs every rule against both
  `node:sqlite` and real local D1 (workerd via `getPlatformProxy`).
- Sessions store only a SHA-256 of the cookie token. Service tokens likewise.

### Accounting note

The seeded chart of accounts, its Form 990 Part VIII/IX/X line mapping, the July–June
fiscal year, and the donor-acknowledgment rules in `src/core/contributions.ts` are a
reasonable starting point for a small 501(c)(3), **not tax or accounting advice. Have
CSEED's CPA review them** before real books are kept or filings are prepared.

## UI data flow and mocks

Pages render the app shell and heading on the server; each page body is a Preact island that
calls `/api/*` through `src/ui/api/client.ts` (`api(name, { params, query, body })`). Names,
paths, bodies and responses all come from `ENDPOINTS` in `src/core/contracts.ts`, and every
response is checked against its zod schema, so a contract drift shows as an error state.

While Phase 1 runs in parallel, endpoints without a backend yet are answered in the browser by
`src/ui/mocks/` (built from the same seed, so `event-kickoff`, `req-0001` … match). The single
switch is `src/ui/mocks/switch.ts`: set `PUBLIC_UI_MOCKS=off` to use only the real API, and
delete the folder once the backend is merged. With the mocks on, add `?mock=empty`,
`?mock=error` or `?mock=loading` to any page to see those states.

## Design

Colours, fonts and the nav come from the cseed.co landing site
(`cseed-org/cseed-landing-page`). Tokens live in `src/styles/tokens/`: the verbatim
palette plus semantic tokens (surfaces, text, rules, status, charts) with a dark theme
that follows the system preference or the nav's theme toggle. Fonts load from the same
Google Fonts URL as the landing site. Status colours: sage = approved, sun = pending (ink
text), sky = info, brick = denied / over budget.

## CI

`.github/workflows/ci.yml` runs typecheck and unit tests on pushes and pull requests.


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
contribution entry are engine/adapter operations, not dashboard API workflows in v1;
provision future open periods before the current year ends.

Pure functions are in `src/core/accounting/`; the API persists posting statements with
request state and audit rows in one D1 batch. Contributions validate their journal total
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
Imports use JSON bulk inserts to avoid D1 bind limits. Files are limited to 5 MB; JSON
ingest is limited to 5,000 guests by contract.

Directors issue service tokens once, store only SHA-256 hashes, and may revoke them.
`ingest:luma` permits `/api/ingest/luma`; `agent:jobs` permits jobs list/claim/complete.
A failed job returns its request to approved, clears its token claim and re-queues
automatically while fewer than three total claims have been attempted. The default
queued-job poll discovers retries. After the third failed attempt the job stays failed
for a director to resolve manually. Only the current claiming token may complete it. Revoking a token
prevents future use; deactivating a user ends sessions but does not revoke independently
issued service tokens (revoke those separately).

Receipts allow PDF, PNG, JPEG, HEIC and WebP, up to 10 MB. Configure
`GOOGLE_SERVICE_ACCOUNT_JSON` and `DRIVE_ROOT_FOLDER_ID` (the older
`GOOGLE_DRIVE_FOLDER_ID` is accepted as an alias). Share the Drive folder with the
service-account identity. The adapter uses WebCrypto RS256 JWT exchange, Drive v3
multipart uploads and shared-drive flags, following Google's
[service-account protocol](https://developers.google.com/identity/protocols/oauth2/service-account)
and [upload API](https://developers.google.com/workspace/drive/api/guides/manage-uploads).
The token requests the full `https://www.googleapis.com/auth/drive` scope to access the pre-existing shared folder. Files are
stored under `FY<year>/<request-id>/`, and officers need Drive access to open its links.
Production receipt operations return HTTP 503 if Drive credentials or the root folder
are missing. Development selects `LocalFileStore`: Node/Pi uses the filesystem directly;
Workerd uses its local D1 SQLite-file backing because its virtual filesystem is request-scoped.
Uploads precede the attachment metadata transaction, so a failed metadata commit may
leave an unreferenced file for later cleanup. MIME validation checks the declared type;
there is no virus scan or content classification.

`DISCORD_WEBHOOK_URL` enables bounded, best-effort embeds; unset webhooks log events.
Delivery failure does not undo the committed purchase. Live Google/Discord credentials
are not needed for the local test suite; production delivery must be checked with the
organization's actual credentials.
