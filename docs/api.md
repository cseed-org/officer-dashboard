# API contract

The UI and the backend meet at `src/core/contracts.ts`: every request body, query string
and response is a zod schema there with its TypeScript type inferred under the same name.
`ENDPOINTS` in that file is the machine-readable version of the table below, and
`tests/core/contracts.test.ts` fails if the two drift apart.

## Conventions

- JSON in and out. File uploads (CSV import, receipts) are `multipart/form-data`.
- **Money is integer cents** (`*Cents`). Format it in the UI with `formatCents` from `src/core/money.ts`.
- **Rates** are fractions 0–1, or `null` when the denominator is zero ("no data", not 0%).
- **Event metrics** (`EventCounts`, computed by `src/core/metrics.ts`):
  - `checkInRate` = checkedIn ÷ registered.
  - `reflectionRate` = `matchedReflections` ÷ checkedIn, where a reflection is matched when
    its email, trimmed and lower-cased, belongs to a checked-in guest.
  - Reflections without a matching check-in are counted in `unmatchedReflections` and left
    out of the rate. The UI should flag a non-zero value as a data problem, such as a missed
    door scan or a typo'd email.
- Dates are `YYYY-MM-DD`; datetimes are ISO 8601 with an offset.
- Lists take `?cursor=&limit=` (`PageQuery`) and return `{ items, nextCursor }`.
- Every error is an `ApiError` body: `{ error: { code, message, details? } }`, with status
  400 `bad_request` (zod `flatten()` in `details`), 401 `unauthenticated`, 403 `forbidden`,
  404 `not_found`, 409 `conflict` / `invalid_transition`, 422 `import_failed`.
- Handlers parse input with the request schema and should `.parse()` their own output with
  the response schema in tests.

## Auth column

| Value | Meaning |
| --- | --- |
| public | No session needed. |
| owner | Active dashboard owner (`isOwner=true`, role leadership). Owner-only operations recheck authorization inside the transaction. |
| officer | Any signed-in, active officer, director, or leadership user. |
| director | Director or leadership; officers get 403. `/admin/*` and `/api/admin/*` are enforced by the middleware, the rest by `requireRole(locals, 'director')` in the handler. |
| token:&lt;scope&gt; | `Authorization: Bearer <service token>` with that scope (issued on `/admin/tokens`). No session. |

## Endpoints

| Method | Path | Auth | Query | Body | Response |
| --- | --- | --- | --- | --- | --- |
| GET | `/api/health` | public | — | — | `HealthResponse` |
| GET | `/api/me` | officer | — | — | `MeResponse` |
| GET | `/api/me/prefs` | officer | — | — | `WidgetPrefs` |
| PUT | `/api/me/prefs` | officer | — | `WidgetPrefs` | `WidgetPrefs` |
| GET | `/api/me/calendar-emails` | officer | — | — | `CalendarEmailsResponse` |
| POST | `/api/me/calendar-emails` | officer | — | `CalendarEmailCreateRequest` | `CalendarEmailResponse` |
| PATCH | `/api/me/calendar-emails/:id` | officer | — | `CalendarEmailUpdateRequest` | `CalendarEmailResponse` |
| DELETE | `/api/me/calendar-emails/:id` | officer | — | — | `OkResponse` |
| POST | `/api/invites` | director | — | `InviteCreateRequest` | `InviteResponse` |
| GET | `/api/lookups` | officer | — | — | `LookupsResponse` |
| GET | `/api/overview` | officer | — | — | `OverviewResponse` |
| GET | `/api/search` | officer | `SearchQuery` | — | `SearchResponse` |
| GET | `/api/events` | officer | `EventsListQuery` | — | `EventsListResponse` |
| POST | `/api/events` | officer | — | `EventUpsertRequest` | `EventUpsertResponse` |
| GET | `/api/events/:id` | officer | — | — | `EventDetailResponse` |
| PATCH | `/api/events/:id` | officer | — | `EventPatchRequest` | `EventUpsertResponse` |
| DELETE | `/api/events/:id` | officer | — | — | `OkResponse` |
| GET | `/api/events/:id/cover` | officer | — | — | cover image bytes |
| PUT | `/api/events/:id/cover` | officer | — | multipart: `file` (JPG, PNG or WebP, ≤ 5 MB) | `EventUpsertResponse` |
| DELETE | `/api/events/:id/cover` | officer | — | — | `EventUpsertResponse` |
| GET | `/api/events/:id/metrics` | officer | — | — | `EventMetricsResponse` |
| POST | `/api/events/import` | officer | — | multipart: `CsvImportFields` + `file` | `CsvImportResponse` |
| POST | `/api/ingest/luma` | token:ingest:luma | — | `LumaIngestRequest` | `LumaIngestResponse` |
| GET | `/api/requests` | officer | `PurchaseRequestsListQuery` | — | `PurchaseRequestsListResponse` |
| POST | `/api/requests` | officer | — | `PurchaseRequestCreateRequest` | `PurchaseRequestResponse` |
| GET | `/api/requests/:id` | officer | — | — | `PurchaseRequestResponse` |
| PATCH | `/api/requests/:id` | officer | — | `PurchaseRequestUpdateRequest` | `PurchaseRequestResponse` |
| DELETE | `/api/requests/:id` | officer | — | — | `OkResponse` |
| POST | `/api/requests/:id/submit` | officer | — | — | `PurchaseRequestResponse` |
| POST | `/api/requests/:id/approve` | director | — | `PurchaseDecisionRequest` | `PurchaseRequestResponse` |
| POST | `/api/requests/:id/deny` | director | — | `PurchaseDenyRequest` | `PurchaseRequestResponse` |
| POST | `/api/requests/:id/transition` | director | — | `PurchaseTransitionRequest` | `PurchaseRequestResponse` |
| POST | `/api/requests/:id/attachments` | officer | — | multipart: `AttachmentUploadFields` + `file` | `AttachmentUploadResponse` |
| GET | `/api/attachments/:id` | officer | — | — | receipt bytes or Drive redirect |
| GET | `/api/finance/summary` | officer | `FinanceQuery` | — | `FinanceSummaryResponse` |
| GET | `/api/finance/reports/:report` | officer | `FinanceReportQuery` | — | `FinanceReportResponse` or CSV ¹ |
| GET | `/api/finance/budgets` | officer | `FinanceQuery` | — | `BudgetsResponse` |
| PUT | `/api/finance/budgets` | director | — | `BudgetsUpsertRequest` | `BudgetsResponse` |
| GET | `/api/finance/periods` | director | — | — | `FiscalPeriodsResponse` |
| POST | `/api/finance/periods` | director | — | `FiscalYearPeriodsCreateRequest` | `FiscalPeriodsResponse` |
| POST | `/api/finance/periods/:id/close` | director | — | `FiscalPeriodCloseRequest` | `FiscalPeriodsResponse` |
| GET | `/api/finance/accounts` | director | — | — | `ChartOfAccountsResponse` |
| POST | `/api/admin/owners/:userId` | owner | — | — | `AdminUserResponse` |
| DELETE | `/api/admin/owners/:userId` | owner | — | — | `AdminUserResponse` |
| POST | `/api/admin/owners/transfer` | owner | — | `OwnerTransferRequest` | `AdminUsersResponse` |
| GET | `/api/admin/users` | director | — | — | `AdminUsersResponse` |
| POST | `/api/admin/users` | director | — | `AdminUserCreateRequest` | `AdminUserResponse` |
| PATCH | `/api/admin/users/:id` | director | — | `AdminUserUpdateRequest` | `AdminUserResponse` |
| GET | `/api/admin/tokens` | director | — | — | `ServiceTokensResponse` |
| POST | `/api/admin/tokens` | director | — | `ServiceTokenCreateRequest` | `ServiceTokenCreateResponse` |
| DELETE | `/api/admin/tokens/:id` | director | — | — | `OkResponse` |
| GET | `/api/admin/audit` | director | `AuditLogQuery` | — | `AuditLogResponse` |
| GET | `/api/agent/jobs` | token:agent:jobs | `AgentJobsQuery` | — | `AgentJobsResponse` |
| POST | `/api/agent/jobs/:id/claim` | token:agent:jobs | — | — | `AgentJobResponse` |
| POST | `/api/agent/jobs/:id/complete` | token:agent:jobs | — | `AgentJobCompleteRequest` | `AgentJobResponse` |
| GET | `/auth/login` | public | — | — | redirect |
| GET | `/auth/callback` | public | — | — | redirect |
| POST | `/auth/logout` | officer | — | — | redirect |
| GET | `/auth/dev` | public | — | — | redirect |

¹ `?format=csv` returns `text/csv`. `:report` is one of `FINANCE_REPORTS` (`src/core/domain.ts`).
Officers may read `budget-vs-actual` and `fund-balances` (`OFFICER_FINANCE_REPORTS`); the
other reports return 403 for officers.

## Search

`GET /api/search?q=<text>&limit=<1..50, default 20>` powers the sidebar search. `q` is trimmed,
1–100 characters, matched case-insensitively as a substring (LIKE wildcards in the text are
literal) against:

| Kind | Who sees it | Matched on | `title` / `subtitle` | `href` |
| --- | --- | --- | --- | --- |
| `event` | officers, directors | name, location, Luma id | name / `<Pacific date> · <location>` | `/events?id=<id>` |
| `request` | officers, directors | number, title, vendor | title / `<number> · <vendor> · <status>` | `/finance?id=<id>` |
| `user` | directors only | name, email, title | name / `<title> · <role> · <email>` | `/admin` |
| `account` | directors only | code, name | `<code> <name>` / account type | `/finance?tab=reports&report=trial-balance` |

Results are ordered best match first (the primary field starts with the query, then contains
it, then another field matches), ties broken by kind and recency, and cut to `limit`. No match
is `{ "results": [] }`. A missing or blank `q`, or a `limit` outside 1–50, is a 400.

## Rules the backend enforces

- **Requests.** Every officer can read every request. Only the requester edits or deletes a
  draft (`PATCH`/`DELETE` → 409 after submission) and submits it. Status moves must be allowed
  by `canTransition()` in `src/core/purchase-lifecycle.ts`, else 409 `invalid_transition`.
  A director cannot approve their own request (403). `totalCents` is always computed
  server-side. `PurchaseRequestDetail.allowedTransitions` and `canEdit` tell the UI which
  buttons to show for the current user.
- **Settlement: the actual amount replaces the estimate.** `totalCents` is the approved
  estimate (quantity × unit cost + tax/shipping). While a request is `approved` or
  `fulfilling`, that estimate counts as *committed* (encumbered) in budget figures. Moving
  to `fulfilled` or `reimbursed` (`POST /api/requests/:id/transition` with a
  `PurchaseSettlement` body, or the agent's `POST /api/agent/jobs/:id/complete`) requires
  `actualTotalCents`. The backend then:
  1. stores it on the request (`PurchaseRequestDetail.actualTotalCents`);
  2. posts the journal entry for **`actualTotalCents`**, not `totalCents`;
  3. releases the commitment for the estimate.

  Example: a $100.00 approved request bought for $95.00 shows $100.00 committed until
  settlement, then $95.00 spent and $0 committed. The actual amount may be higher or lower
  than the estimate; both stay on the request. `attachmentIds` links receipts already
  uploaded to the request.
- **Approve** enqueues a `fulfillment_jobs` row for club purchases and sends a Discord
  notification; submit / deny / fulfilled also notify.
- **Admin users.** Deactivating a user deletes their sessions. The last active director can't
  be demoted or deactivated (409).
- **Service tokens.** The plaintext is returned once by `POST /api/admin/tokens`; only a
  SHA-256 hash is stored. Revoking keeps the row for the audit log.
- **Audit.** Every state-changing call writes an `audit_log` row (append-only).

## Pages

| Path | Role | Notes |
| --- | --- | --- |
| `/login` | public | "Sign in with Discord" |
| `/` | officer | Overview KPIs + recent events (`OverviewResponse`) |
| `/events`, `/events?id=<id>` | officer | List, month and week views; editor, Luma handoff kit, request design; CSV import on `/events?import=1` |
| `/requests`, `/requests/new`, `/finance?id=<id>` | officer | Approve / deny buttons for directors |
| `/finance` | officer | Directors see every report; officers see the summary |
| `/admin/users`, `/admin/tokens` | director | 403 for officers |


## Backend implementation notes

- `FINANCE_REPORTS` additionally includes director-only `form-990` → `Form990Report`.
  It groups Part VIII/IX account mappings and functional expense columns; every report
  supports tabular `?format=csv` with metadata, section names and integer cents.
  `totals.partVIIILine12Cents` and `totals.partIXLine25` sum the mapped lines;
  `line1gNoncashCents` is the memo for in-kind/stock contributions (already inside
  lines 1a–1f); `unmappedAccounts` lists revenue/expense accounts with no 990 line,
  whose amounts are **not** in the totals.
- Opening-balance entries (`source = 'opening_balance'`) count toward net assets at the
  beginning of the year whatever their date, so the statement of activities foots:
  revenue − expenses + releases = change in unrestricted net assets, and
  revenue − releases = change in restricted net assets.
- Net expense/release fields are signed cents so correcting entries/refunds validate.
  Fund-balance rows additionally expose `restrictedEndingCents` and
  `unrestrictedEndingCents`; `releasesCents` is informational and is not subtracted a
  second time from the combined fund roll-forward.
- `GET /api/attachments/:id` is an officer-only download for local files or a redirect
  to a Drive link. Production uploads return 503 when Drive is unconfigured.
- A restricted-fund settlement posts its expense plus an atomic restriction release.
  Expenses appear without donor restriction; grant attribution stays on the original fund.
- Reimbursement `reimbursed→closed` records payment (debit payable, credit checking).
  The director must only close after confirming the officer was paid.
- Failed purchasing jobs automatically re-queue and clear ownership until the third
  total attempt. After that they remain failed for director intervention. Sparse guest
  imports preserve prior fields.
- `GET /api/admin/audit` and other lists use opaque date/id cursors in descending date
  order. List adapters currently load their filtered datasets in memory (v1 scale).
- Fiscal periods: `POST /api/finance/periods` creates the 12 monthly periods of a fiscal
  year that has none (409 otherwise). `POST /api/finance/periods/:id/close` moves
  open → closed/locked or closed → locked; nothing reopens (409). A period whose
  `endDate` is today or later (Pacific) can't be closed or locked (409), and the UI
  shows it as "in progress". The `/finance`
  "periods" tab (directors) drives both. Manual/contribution postings remain core-only.
- Agent settlements above max(estimate × 1.10, estimate + $25) return 409 and move the
  job to `held` (terminal for the agent: claim cleared, never re-queued). The request
  detail's `settlementHold` carries the attempted and approved amounts until a director
  settles it; a `request.held` notification fires (see README, "Agent settlement tolerance").
- Request numbers (`<FY>-<NNNN>`) come from a per-fiscal-year counter (migration 0004 in the Go
  server), so deleting a draft never frees its number. Journal entries are dated by the
  organisation's Pacific calendar day, not UTC.
- Receipt uploads must carry the magic bytes of their declared type (PDF, PNG, JPEG, WebP, HEIC).
- Known v1 parity: a director may move `approved → fulfilling` through `/transition` without the
  agent claiming a job; no purchasing job exists for that request, so a director settles it by hand.

## V0.3 foundation

Roles rank `leadership > director > officer`. Only leadership may change `role` in
PATCH `/api/admin/users/:id` or create a user with a role other than officer.
Directors may create officers and invites; modifying an existing leadership user also
requires leadership. Role changes invalidate the affected user's sessions. The last
active director-or-leadership account cannot be deactivated or demoted to officer.

`GET/PUT /api/me/prefs` returns/replaces `{widgetLayout: [{id, size}]}`. Array order is
widget order, IDs are unique (1–64 characters), sizes are `S`, `M`, `L`, maximum 50
widgets. An empty array is intentional. Missing prefs return the default Home cards.
Unknown IDs survive round trips for forward compatibility. Theme and density remain
in localStorage. All preference writes are audit logged.

Calendar email routes operate only on the signed-in user's collection. Up to five
addresses are allowed; the sixth returns 409 with “You can save at most 5 calendar
emails”. SQLite enforces the same limit on inserts and user reassignment. Addresses
are trimmed, lowercased and unique per user. The first is active automatically;
selecting another switches atomically, and deleting the active address selects the
oldest remaining one. A partial unique index prevents multiple active addresses.
Mutations audit the entire before/after collection. Calendar delivery mode is stored
on `officer_profiles` as `ics` (default) or `google`; delivery and OAuth belong to the
officers/onboarding feature.

POST `/api/invites` currently accepts copy-link invitations (`method: "link"`). It
returns a plaintext token once and stores only its SHA-256 hash. The token is omitted
from audit records. Schema supports `dm` and `both`, `dm_sent_at`, and `accepted_at`;
Discord delivery, accepting invitations, and onboarding are added by the officers
feature. This endpoint does not send a DM or allow the invitee to sign in yet.

Officer profiles require phone, birthday, and subteam; name and Discord ID remain on
`users`. Optional profile fields are preferred name, pronouns, personal/school email,
grad date, title, start term, and LinkedIn. No profile read endpoint is exposed here;
future profile endpoints must restrict phone visibility to director+.

Dashboard ownership: `CurrentUser` and `AdminUser` include `isOwner`. Owners are
always active leadership. Only owners may add/remove co-owners or transfer ownership.
Non-owner leadership receive 403 for these operations and for demoting or deactivating
an owner. Owners may demote/deactivate another owner; this also removes that person's
owner flag in the same transaction, subject to the last-owner guard. A name/title-only
edit does not change ownership. Direct user PATCH bodies cannot set `isOwner`.

POST `/api/admin/owners/:userId` adds an active target as co-owner, promoting their
role if needed. DELETE removes their owner flag, retaining leadership. Last-owner
removal returns 409. Repeating add/remove is safe. POST `/api/admin/owners/transfer`
accepts `{toUserId}`: the active target becomes an owner before the caller loses their
flag, atomically. Caller remains active leadership and other co-owners remain owners.
It returns `{users: [target, caller]}`. Self-transfer is 400; missing targets are 404;
inactive targets are 400. All changed users receive audit entries. Promotion invalidates
the target's sessions; transferring/removing ownership preserves sessions when the role
stays leadership, and subsequent requests read the current owner flag.

Startup provisioning uses `BOOTSTRAP_OWNER_DISCORD_ID` and `BOOTSTRAP_OWNER_NAME`.
The old `BOOTSTRAP_DIRECTOR_DISCORD_ID` is a deprecated ID alias. If no owner exists,
the configured user is upserted as active leadership/owner and audited. Name defaults to
the existing name, or “Dashboard owner” for a new account when unset. Once any owner
exists, the environment is ignored; a mismatch logs a warning. Ownership transfer is
permanent across restarts with the previous bootstrap configuration.

Migration preserves existing roles; deployment operators can configure the initial
owner before startup or explicitly recover an active account with `dashboard
provision-leadership --owner --db /path/dashboard.db --discord-id 123456789012345678`.
This is idempotent, ends changed accounts' sessions and audit-logs the change. Without
`--owner`, it grants leadership only. Seed data includes a fictional owner for local QA.

The bootstrap environment cannot reclaim ownership after transfer. Explicit operator
recovery with `provision-leadership --owner` remains available even when owners already
exist: shell/database administrators can intentionally add an owner, and this action
is audited. Discord login itself never creates users or grants leadership/ownership.

## Events (V0.3)

Every officer can create, edit and delete events, set the cover, and request design;
directors and leadership have the same event powers. Each change writes `event.created`,
`event.updated`, `event.deleted`, `event.cover_set` or `event.cover_removed` to the audit log.

- **Fields.** `status` is `draft | published | completed | cancelled`. New events start as
  `draft`; events created by a Luma import or ingest are `published` (so are the rows that
  existed before migration 0006). `description` (≤ 5000), `capacity` (1–100000),
  `hostSubteamId` (a `subteams` id; the name comes back as `hostSubteam`), `hostUserId`.
  `GET /api/lookups` lists `subteams` and active `hosts` for the editor.
- **Luma handoff.** There is no Luma API. The UI shows each field with a copy button,
  the cover to download, and a link to `https://lu.ma/create`. The officer pastes the
  published page back as `lumaUrl`. The server accepts only `https://` on `lu.ma`,
  `www.lu.ma`, `luma.com` or `www.luma.com` with the path `/<slug>` (letters, digits,
  hyphens) or `/event/evt-<id>`; Luma's own pages (`/create`, `/discover`, `/calendar`,
  `/home`, the bare home page and similar) are refused with 400. The stored value drops
  the query, fragment and trailing slash. A Luma page can be linked to one event only
  (409). Linking never imports attendees: those still arrive by CSV import or
  `POST /api/ingest/luma`.
- **Cover.** `PUT /api/events/:id/cover` takes one image (content is checked against its
  type). It is stored in the database; `coverUrl` is `/api/events/:id/cover?v=<ms>` and
  changes when the cover does. The GET answers with `nosniff` and a sandbox CSP.
- **Delete.** Guests, reflections and the cover go with the event. An event that
  purchase requests point at answers 409 until they are moved. The published Luma event
  must be removed in Luma separately.
- **Notion (stub).** On create the server asks `NotionSync` for a page; the stub stores a
  placeholder link and `notion.placeholder` is `true`. See `docs/notion-integration.md`.
- **Design request.** "Request design" opens the Design board's new-request form for the
  event (`/design?new=1&eventId=<id>`). The board prefills it from the event and keeps the
  link on its own side (`design_requests.event_id`), built in the design-requests Pane.
