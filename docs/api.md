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
| officer | Any signed-in, active user (officers and directors). Enforced by `src/middleware.ts`. |
| director | Directors only; officers get 403. `/admin/*` and `/api/admin/*` are enforced by the middleware, the rest by `requireRole(locals, 'director')` in the handler. |
| token:&lt;scope&gt; | `Authorization: Bearer <service token>` with that scope (issued on `/admin/tokens`). No session. |

## Endpoints

| Method | Path | Auth | Query | Body | Response |
| --- | --- | --- | --- | --- | --- |
| GET | `/api/health` | public | — | — | `HealthResponse` |
| GET | `/api/me` | officer | — | — | `MeResponse` |
| GET | `/api/lookups` | officer | — | — | `LookupsResponse` |
| GET | `/api/overview` | officer | — | — | `OverviewResponse` |
| GET | `/api/events` | officer | `EventsListQuery` | — | `EventsListResponse` |
| POST | `/api/events` | officer | — | `EventUpsertRequest` | `EventUpsertResponse` |
| GET | `/api/events/:id` | officer | — | — | `EventDetailResponse` |
| PATCH | `/api/events/:id` | officer | — | `EventPatchRequest` | `EventUpsertResponse` |
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
| `/events`, `/events/:id` | officer | Event ratios + chart; CSV import on `/events/import` |
| `/requests`, `/requests/new`, `/requests/:id` | officer | Approve / deny buttons for directors |
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
