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
  404 `not_found`, 409 `conflict` / `invalid_transition` / `over_tolerance`, 422 `import_failed`,
  429 `rate_limited` (with `Retry-After`).
- Handlers parse input with the request schema and should `.parse()` their own output with
  the response schema in tests.

## Auth column

| Value | Meaning |
| --- | --- |
| public | No session needed. |
| owner | Active dashboard owner (`isOwner=true`, role leadership). Owner-only operations recheck authorization inside the transaction. |
| officer | Any signed-in, active officer, director, or leadership user. |
| director | Director or leadership; officers get 403. `/api/admin/*` is enforced by the middleware, the rest by `requireRole(locals, 'director')` in the handler. |
| token:&lt;scope&gt; | `Authorization: Bearer <service token>` with that scope (issued on `/officers?tab=tokens`). No session. |

## Endpoints

| Method | Path | Auth | Query | Body | Response |
| --- | --- | --- | --- | --- | --- |
| GET | `/api/members` | officer | `MembersQuery` | — | `MembersResponse` |
| GET | `/api/members/export` | director | `MembersQuery` | — | CSV |
| GET | `/api/members/:id` | officer | — | — | `MemberResponse` |
| PATCH | `/api/members/:id` | director | — | `MemberUpdateRequest` | `MemberResponse` |
| POST | `/api/cloudflare/sync` | director | — | — | `CloudflareSyncResponse` |
| GET | `/api/cloudflare/widgets` | officer | — | — | `CloudflareWidgetsResponse` |
| GET | `/api/health` | public | — | — | `HealthResponse` |
| GET | `/api/me` | officer | — | — | `MeResponse` |
| GET | `/api/club/settings` | officer | — | — | `ClubSettings` |
| PUT | `/api/club/settings` | leadership | — | `ClubSettings` | `ClubSettings` |
| GET | `/api/me/prefs` | officer | — | — | `WidgetPrefs` |
| PUT | `/api/me/prefs` | officer | — | `PrefsUpdate` | `WidgetPrefs` |
| GET | `/api/me/calendar-emails` | officer | — | — | `CalendarEmailsResponse` |
| POST | `/api/me/calendar-emails` | officer | — | `CalendarEmailCreateRequest` | `CalendarEmailResponse` |
| PATCH | `/api/me/calendar-emails/:id` | officer | — | `CalendarEmailUpdateRequest` | `CalendarEmailResponse` |
| DELETE | `/api/me/calendar-emails/:id` | officer | — | — | `OkResponse` |
| GET | `/api/invites` | director | — | — | `InvitesResponse` |
| POST | `/api/invites` | director | — | `InviteCreateRequest` | `InviteResponse` |
| PATCH | `/api/invites/:id` | director | — | `InviteUpdateRequest` | `InviteUpdateResponse` |
| DELETE | `/api/invites/:id` | director | — | — | `OkResponse` |
| GET | `/api/officers` | officer | — | — | `OfficersResponse` |
| GET | `/api/officers/:id` | officer | — | — | `OfficerResponse` |
| PATCH | `/api/officers/:id` | director | — | `OfficerUpdateRequest` | `OfficerResponse` |
| GET | `/api/me/profile` | officer | — | — | `ProfileResponse` |
| PUT | `/api/me/profile` | officer | — | `ProfileUpsertRequest` | `ProfileResponse` |
| PUT | `/api/me/onboarding` | officer | — | `OnboardingSaveRequest` | `OnboardingResponse` |
| POST | `/api/me/onboarding/complete` | officer | — | — | `OnboardingCompleteResponse` |
| POST | `/api/me/onboarding/correction` | officer | — | `OnboardingCorrectionRequest` | `OnboardingCorrectionResponse` |
| GET | `/api/me/calendar` | officer | — | — | `CalendarSettings` |
| PUT | `/api/me/calendar` | officer | — | `CalendarModeRequest` | `CalendarSettings` |
| POST | `/api/me/calendar/test` | officer | — | — | `CalendarTestResponse` |
| GET | `/api/me/calendar/google/connect` | officer | `next` | — | redirect to Google |
| DELETE | `/api/me/calendar/google` | officer | — | — | `CalendarSettings` |
| GET | `/auth/google/callback` | public (session) | — | — | redirect |
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
| GET | `/api/design-requests` | officer | — | — | `DesignRequestsListResponse` |
| POST | `/api/design-requests` | officer | — | `DesignRequestCreateRequest` | `DesignRequestResponse` |
| GET | `/api/design-requests/:id` | officer | — | — | `DesignRequestResponse` |
| PATCH | `/api/design-requests/:id` | officer | — | `DesignRequestUpdateRequest` | `DesignRequestResponse` |
| DELETE | `/api/design-requests/:id` | officer | — | — | `OkResponse` |
| POST | `/api/design-requests/:id/assign` | officer | — | `DesignAssignRequest` | `DesignRequestResponse` |
| POST | `/api/design-requests/:id/move` | officer | — | `DesignMoveRequest` | `DesignRequestResponse` |
| POST | `/api/design-requests/:id/approve` | officer | — | — | `DesignRequestResponse` |
| POST | `/api/design-requests/:id/comments` | officer | — | `DesignCommentRequest` | `DesignRequestResponse` |
| POST | `/api/design-requests/:id/files` | officer | — | multipart: `DesignFileUploadFields` + `file` | `DesignRequestResponse` |
| GET | `/api/design-files/:id` | officer | — | — | file bytes or Drive redirect |
| GET | `/api/events/:id/design-request-draft` | officer | — | — | `DesignRequestDraftResponse` |
| POST | `/api/events/:id/design-requests` | officer | — | `DesignRequestFromEventRequest` (optional) | `DesignRequestResponse` |
| PUT | `/api/events/:id/design-need` | officer ² | — | `EventDesignNeedRequest` | `EventDesignNeedResponse` |
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
| GET | `/auth/dev` | public (dev builds) | `as`/`user`/`discord`, `next`, `key` | — | confirmation page (HTML form) |
| POST | `/auth/dev` | public (dev builds) | — | form: `as`/`user`/`discord`, `next`, `key` | redirect (303) |

¹ `?format=csv` returns `text/csv`. `:report` is one of `FINANCE_REPORTS` (`src/core/domain.ts`).
Officers may read `budget-vs-actual` and `fund-balances` (`OFFICER_FINANCE_REPORTS`); the
other reports return 403 for officers.

² Any officer may set `needed` or `unknown`; `not_needed` needs director+ or the event's host.

## Search

`GET /api/search?q=<text>&limit=<1..50, default 20>` powers the sidebar search. `q` is trimmed,
1–100 characters, matched case-insensitively as a substring (LIKE wildcards in the text are
literal) against:

| Kind | Who sees it | Matched on | `title` / `subtitle` | `href` |
| --- | --- | --- | --- | --- |
| `event` | officers, directors | name, location, Luma id | name / `<Pacific date> · <location>` | `/events?id=<id>` |
| `request` | officers, directors | number, title, vendor | title / `<number> · <vendor> · <status>` | `/finance?id=<id>` |
| `contact` | all officers | name, organization, role, email, topics | name / `<org> · <role> · <type>` | `/contacts?id=<id>` |
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
- **Purchase deadlines.** `neededBy` is an optional date-only `YYYY-MM-DD` (list items and
  detail both carry it). On create, and whenever an edit changes it, it must not be before
  today on the officer's effective time zone (400 with a `neededBy` field error); an
  unchanged deadline that has since passed may stay. Edits follow the draft rule above and
  are audited in `request.updated`. `GET /api/requests?sort=deadline` lists only requests
  with a deadline, soonest first (cursor `YYYY-MM-DD|id`); the default `sort=newest` is
  unchanged. See [Purchase deadlines](purchase-deadlines.md).
- **No self-payout.** The requester never settles (`fulfilled`, `reimbursed`) or closes their
  own request, whatever their role (403); another director does. `allowedTransitions` leaves
  those moves out for the requester. The same holds for the purchasing agent: a token never
  sees or claims (409) a job for a request its creator filed, and a completion that arrives
  anyway is held (`job.held_for_review`, 409) for another director to settle.
- **Settlement ceiling.** A director may record at most the approved estimate plus the agent
  tolerance (`AGENT_SETTLEMENT_TOLERANCE_*`, default max(estimate × 1.10, estimate + $25));
  more answers 409 `over_tolerance` and leadership settles it. `PurchaseRequestDetail.maxSettlementCents`
  carries the viewer's ceiling (null when they cannot settle, or for leadership).
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
- **Design requests.** Every officer sees the whole board (`GET /api/design-requests`) and
  can create a request, comment, and attach reference files. The design team is the active
  users whose officer profile is on the subteam named **Design** (migration 0008 creates it).
  - Designers assign any designer (themselves included) and move cards between New,
    Accepted, In progress and In review. Moving an unassigned card past New assigns the
    designer who moved it. Designers must move work back to New before unassigning (409).
    Directors and leadership can move unassigned work and assign or unassign at any stage,
    but can only assign designers (400).
  - **Requester approval uses `POST /approve`** while the card is In review; other users get
    403. Leadership alone may instead use `POST /move` with `stage: "delivered"` and
    `confirmDeliveryOverride: true`. This writes a `design_request.delivery_overridden` audit
    entry and leaves requester approval metadata empty. Directors cannot override delivery. The requester can instead send it back with `move` → `in-progress` ("Request
    changes"). Designers can reopen a delivered card to In progress, which clears the approval.
  - The requester (or a director) edits the fields until delivery (409 after). The requester
    can withdraw (`DELETE`) while the card is New; directors can delete any time.
  - Files: `reference` from the requester or the design team until delivery; `review` from
    the design team while the card is open; `delivered` from the design team at any stage
    (directors and leadership share the design team's file rights, as with assign/move). Same
    storage, size and type rules as receipts (PDF, PNG, JPEG, WebP, HEIC; up to 10 MB; content
    sniffed), rechecked inside the write. Downloads are officer-only. Deleting a request removes
    its local files and trashes Drive copies; the audit row lists the files and comments removed.
  - A call that changes nothing (moving to the current stage, an empty `PATCH`) answers with
    the current request and writes no audit row, but is still permission-checked.
  - `DesignRequestDetail.permissions` tells the UI which actions the viewer has.
  - **From an event.** `GET /api/events/:id/design-request-draft` proposes a request for
    the event: title "<event> promo", type social, a brief with the date, place and RSVP
    link, and a due date a week before the event (Vancouver time), never before today, or
    null for a past event. `POST /api/events/:id/design-requests` creates it directly; any
    supplied field overrides the draft, and the event link is fixed. The Design page also
    opens the prefilled form at `/design?new=1&eventId=<id>`.
  - **Upcoming events and design needs (V0.4).** `events.design_need` (migration 0013) is
    `unknown` (the default, treated as needed), `needed` or `not_needed`. Any officer may set
    `needed`; setting `not_needed` needs director+ or the event's host (403 otherwise), both
    through `PUT /api/events/:id/design-need` and the `designNeed` field of event create/PATCH
    (re-sending the stored value is always allowed). Each change is audited as
    `event.design_need` (or `event.updated` from the editor). `GET /api/design-requests` adds
    `upcomingEvents`: draft and published events starting within the next six weeks, soonest
    first, each with an `indicator` derived from `design_need` and its linked requests:
    `requested` while any linked request is open, `done` once all are delivered, otherwise
    `not-needed` or `needs-design`. A request on a not-needed event therefore still shows.
- **Admin users.** Directors may deactivate officers. Deactivating directors or leadership requires leadership; owner accounts additionally require an owner. Deactivating a user deletes their sessions. The last active director can't
  be demoted or deactivated (409).
- **Service tokens.** The plaintext is returned once by `POST /api/admin/tokens`; only a
  SHA-256 hash is stored. Revoking keeps the row for the audit log. A token acts with its
  creator's authority: it answers 401 once the creator is inactive or below director, and
  deactivating or demoting the creator below director revokes their tokens
  (`token.revoked_with_user` in the audit log).
- **Member paging.** Any officer may list members; reading past the first page
  (`offset > 0`) writes a `members.paged` audit row (offset, limit, rows returned, filters).
- **Health.** `GET /api/health` returns `version` only to signed-in callers.
- **Rate limits.** Per client IP, `/auth/*` and the upload/import routes
  (`POST /api/requests/:id/attachments`, `POST /api/design-requests/:id/files`,
  `PUT /api/events/:id/cover`, `POST /api/events/import`, `POST /api/ingest/luma`) allow 30
  requests a minute each, then answer 429. Behind a trusted proxy (`TRUSTED_PROXIES`, loopback by
  default) the client is the right-most `X-Forwarded-For` hop that is not itself a trusted proxy;
  IPv6 clients share a budget per /64; each limiter tracks at most 10,000 clients. Direct loopback
  requests without the header (local tools, tests) are not limited.
- **Audit.** Every state-changing call writes an `audit_log` row (append-only).

## Pages

| Path | Role | Notes |
| --- | --- | --- |
| `/login` | public | "Sign in with Discord" |
| `/` | officer | Overview KPIs + recent events (`OverviewResponse`) |
| `/events`, `/events?id=<id>` | officer | List, month and week views; editor, Luma handoff kit, request design; CSV import on `/events?import=1` |
| `/requests`, `/requests/new`, `/finance?id=<id>` | officer | Approve / deny buttons for directors |
| `/finance` | officer | Directors see every report; officers see the summary |
| `/admin`, `/admin/users`, `/admin/tokens`, `/admin/audit` | officer | Redirect to Officers; management tabs require director |
| `/design`, `/design?id=<id>`, `/design?new=1&eventId=<id>`, `?view=board\|list\|mine`, `?filter=mine\|unassigned\|due`, `?type=<type>` | officer | Design page: upcoming events with design needs, then the request board (list view on phones) or My work; opens a request or the prefilled form |


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
- `GET /api/attachments/:id` downloads a local receipt or redirects to its Drive link, for
  the uploader, the request's requester and directors only (403 for other officers). For other
  officers, `PurchaseRequestDetail.attachments[].url` is `null` (the metadata stays visible), so
  a Drive link is never handed out either. Production uploads return 503 when Drive is unconfigured.
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
Directors may create officers and invites, and deactivate or reactivate officers. Changing
the active status of a director or leadership account, or modifying an existing leadership
user at all, requires leadership. Role changes invalidate the affected user's sessions. The last
active director-or-leadership account cannot be deactivated or demoted to officer.

`GET/PUT /api/me/prefs` returns `{widgetLayout: [{id, size}], timezoneMode, timezone, effectiveTimezone}`. PUT accepts partial updates so widget and timezone preferences never overwrite each other. `timezoneMode` is `club` (the default), `auto`, or `manual`; `timezone` is the detected/manual IANA name, or an empty string for club fallback. Manual mode requires a valid zone. `effectiveTimezone` is read-only. Array order is
widget order, IDs are unique (1–64 characters), maximum 50 widgets. Sizes are Home grid
blocks, `W×H`: `1x1`, `2x1`, `1x2`, `2x2`, `4x1`, `4x2` (each widget supports a subset; see
`web/src/ui/widgets/registry.ts`). V0.3 sizes `S`, `M`, `L` and the old `kpis` widget are still
accepted and returned; the web client migrates them to blocks on load and automatically saves the converted
layout once on first load. An empty array is intentional. Missing prefs return the
default Home layout (five stat tiles, My actions, Recent events, Budget vs actual, Activity, Web Analytics).
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

See "V0.3 officers" below for invites, profiles, onboarding and calendar delivery.

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
is audited. Discord login never grants leadership/ownership; it creates an account only
for a pending, unexpired invite (below).

## V0.3 officers

**Invites.** POST `/api/invites` (director+) takes `{name, discordId, subteamId, method}`
with `method` `dm`, `link` (default) or `both`. It returns the invite, a one-time
`token`, the full `link` (`/login?invite=<token>`), `reinvited`, and `dm`:
`{status: sent|failed|not_configured|skipped, sentAt, message}`. The link is always
returned so a failed or unconfigured DM falls back to copy-link; `message` is plain
advice for the director (for example Discord error 50007, closed DMs). Only the token's
SHA-256 is stored. Invites expire after 7 days.

- A Discord ID that already belongs to an account is refused with 409 (active: "they
  can sign in now"; deactivated: "reactivate them on the Officers page"). A database
  trigger enforces the same rule.
- A Discord ID with a pending invite is **re-invited**: same invite id, new token, name,
  subteam, inviter and expiry; the old link and DM status are cleared.
- GET lists the newest 200 invites with `status` pending | expired | accepted |
  onboarded and `dmConfigured`. DELETE revokes a pending invite (409 once accepted).
- PATCH `/api/invites/:id {name?, subteamId?}` fixes the details of any invite that is
  not yet onboarded: pending, expired or accepted (409 once onboarded). The link, token
  and Discord ID stay the same. When the account it created is leadership or an owner,
  only leadership may edit it (403 for directors), as on PATCH `/api/officers/:id`. For an accepted invite it also renames the account
  and moves a saved profile to the new subteam, so the invitee's Confirm step shows the
  fix at once. Audited as `invite.updated`.
- POST `/api/admin/users` (Access tab, "add officer") now requires `subteamId` and
  records an accepted invite for the new account, so it goes through the same setup as
  an invitee. A Discord ID with a pending invite is refused (409): revoke the invite or
  let them accept it.
- DMs go through the Discord bot API (`DISCORD_BOT_TOKEN`, `notify.DiscordBot`). The bot
  must share a server with the invitee. Without the token, `dm.status` is
  `not_configured`.

**Accepting.** Signing in with Discord as an account that has no user row but a pending,
unexpired invite creates an active officer named as invited, marks the invite accepted,
audits `user.created` and `invite.accepted` as the new user, and redirects to
`/onboarding`. Without an invite the usual `not_allowlisted` applies; when the sign-in
started from an invite link (or the invite expired), it redirects to
`/onboarding?unavailable=1` without revealing any invite details. A sign-in started from
an invite link must carry the invite's current token: a re-invite makes older links show
"invitation unavailable", while signing in directly with the invited account still works. Until onboarding is
complete, every sign-in lands on `/onboarding`.

**Setup gate.** Until onboarding is complete, the server answers every API call outside
`/api/me` and `/api/me/*` (profile, prefs, calendar, calendar emails, onboarding),
`GET /api/club/settings` and `/api/health` with **403 `onboarding_required`**. The web
client sends the browser to `/onboarding` when it sees that code. Accounts with no invite
row (seeded, bootstrap, created before V0.4) are never gated.

**Signing out to an invite.** POST `/auth/logout` accepts a form field
`next=/login?invite=<token>` and returns there; any other value lands on `/login`. The
sign-in page uses it when an invite link opens in a browser where someone else is
signed in: it shows "Signed in as X" with a sign-out button instead of continuing as X.

**Onboarding** is for invitees and admin-created accounts. `/api/me` carries `onboarding`
(step to resume, the account's current name and subteam, inviter, `hasProfile`,
`correctionsByDm`, `correctionPending`) or `null`. `correctionPending` is true after a
correction until the inviter edits the invite. PUT
`/api/me/onboarding {step}` saves where to resume ("Finish later"). POST
`/api/me/onboarding/correction {message}` DMs the inviter that the pre-filled name or
subteam is wrong; without a bot it returns `not_configured` plus the inviter's Discord
ID so the client can open a DM. Corrections are limited to one a minute and five
delivered DMs per invite (409); the slot is reserved inside one write transaction before
the DM goes out, so parallel requests can't slip past the limit, and failed DMs don't
count toward five. A deactivated inviter gets no DM (`dm.status` `skipped`, with advice to
ask leadership). The relayed text has URL schemes broken and markdown and mentions
escaped, so the bot never posts a live link. POST `/api/me/onboarding/complete` requires a saved
profile (409 otherwise) and is idempotent; non-invitees get 409.

**Profiles.** PUT `/api/me/profile` replaces the caller's profile. Required: `phone`
(7–15 digits; `+ ( ) . -` and spaces allowed) and `birthday` as **MM-DD** (no year is
collected; Feb 29 allowed). Optional (null or omitted clears): preferredName, pronouns,
personalEmail, schoolEmail (lower-cased), gradDate (`YYYY-MM`), title (also the
user's display title), startTerm, linkedin (https on linkedin.com). The subteam comes
from the invite while onboarding (so an inviter's fix applies), then from the existing
profile; changing it is 403 ("ask a director"). Officers
with neither pick one with `subteamId` on first save. Phone is redacted from the audit log.

**Officers directory.** GET `/api/officers` lists active officers (director+ also see
deactivated accounts) with subteam, profile fields and onboarding state, plus
`subteams`, `canManage` and `canChangeRoles`. **Phone is only present for director+
viewers.** Birthday (month-day) is visible to every officer. PATCH
`/api/officers/:id` (director+) changes `subteamId` and `title`; editing a leadership
user requires leadership. Role and active status stay on PATCH `/api/admin/users/:id`. Making someone a co-owner
(POST `/api/admin/owners/:userId`) also promotes them to leadership.

**Calendar delivery.** Each officer's mode is `ics` or `google`.

- `ics`: dashboard events are emailed as RFC 5545 invitations (METHOD:REQUEST, a stable
  UID per event, SEQUENCE bumped on each update) to the officer's active calendar email.
  Needs `SMTP_HOST`, `SMTP_FROM` and usually `SMTP_USERNAME`/`SMTP_PASSWORD`; port 465
  is implicit TLS, others require STARTTLS (except localhost).
- `google`: GET `/api/me/calendar/google/connect?next=` sends the officer to Google
  consent (PKCE, offline access, scope `calendar.events`); `/auth/google/callback`
  stores the AES-GCM-sealed tokens (key derived from the client secret), switches the
  mode to google and returns to `next` with `?google=connected|cancelled|failed`.
  Needs `GOOGLE_OAUTH_CLIENT_ID`/`_SECRET`, redirect URI `APP_URL/auth/google/callback`.
  DELETE disconnects, revokes the grant and falls back to ics.
  The sealing key is derived from the client secret: rotating `GOOGLE_OAUTH_CLIENT_SECRET`
  makes stored tokens unreadable, so every officer has to reconnect Google.
- Without configuration, `ics.configured`/`google.configured` are false and the UI shows
  "not configured"; choosing google then is 409.
- Event create/update (including CSV import and Luma ingest) sends to every active officer
  with a profile, on one background worker in FIFO order; each job re-reads the event, so
  the newest content wins. Each recipient has its own 45s timeout. Upcoming events are
  backfilled when an officer saves their first profile, finishes onboarding, changes
  their active calendar email or delivery mode, or connects Google. `calendar_deliveries`
  records what each officer last received (a content fingerprint), so unchanged events
  are never resent and updates replace rather than duplicate (the Google event id is kept
  across mode switches). Shutdown waits up to 20s for queued deliveries. Event deletion
  sends cancellations: `eventDelete` snapshots `calendar_deliveries` (they cascade) and calls `Server.CancelEventOnCalendars`, which sends a CANCEL to the address the invite last went to, or cancels the Google event.
- POST `/api/me/calendar/test` sends a sample invite synchronously and reports the result.

## Events (V0.3)

Every officer can create, edit and delete events, set the cover, and request design;
directors and leadership have the same event powers. Each change writes `event.created`,
`event.updated`, `event.deleted`, `event.cover_set` or `event.cover_removed` to the audit log.

- **Fields.** `status` is `draft | published | completed | cancelled`. New events start as
  `draft`; events created by a Luma import or ingest are `published` (so are the rows that
  existed before migration 0009). `description` (≤ 5000), `capacity` (1–100000),
  `hostSubteamId` (a `subteams` id; the name comes back as `hostSubteam`), `hostUserId`.
  `GET /api/lookups` lists `subteams` and active `hosts` for the editor.
- **Luma handoff.** There is no Luma API. The UI shows each field with a copy button,
  the cover to download, and a link to `https://lu.ma/create`. The officer pastes the
  published page back as `lumaUrl`. The server accepts only `https://` on `lu.ma`,
  `www.lu.ma`, `luma.com` or `www.luma.com` with the path `/<slug>` (letters, digits,
  hyphens) or `/event/evt-<id>`; Luma's own pages (`/create`, `/discover`, `/calendar`,
  `/home`, the bare home page and similar) are refused with 400, and so are ports,
  userinfo and backslashes. The stored value is on the canonical host (`https://lu.ma/…`)
  without query, fragment or trailing slash. A Luma page can be linked to one event only
  (409), compared case-insensitively, so `luma.com/X` and `lu.ma/x` are the same page.
  Linking never imports attendees: those still arrive by CSV import or
  `POST /api/ingest/luma`.
- **Luma import publishes drafts.** A Luma guest list for a draft (CSV `kind=luma_guests`, not a
  dry run, or `POST /api/ingest/luma`) moves it to `published`, audited as `event.updated`
  with `reason: luma_import`. Reflection imports and other statuses are left alone.
- **Cover.** `PUT /api/events/:id/cover` takes one image (content is checked against its
  type). It is stored in the database; `coverUrl` is `/api/events/:id/cover?v=<ms>` and
  changes when the cover does. The GET answers with `nosniff` and a sandbox CSP.
- **Search.** `q` matches the event name or location.
- **Delete.** Guests, reflections and the cover go with the event. An event that
  purchase requests point at answers 409 until they are moved. The published Luma event
  must be removed in Luma separately.
- **Notion (stub).** On create the server asks `NotionSync` for a page; the stub stores a
  placeholder link and `notion.placeholder` is `true`. See `docs/notion-integration.md`.
- **Design request.** "Request design" opens the Design board's new-request form for the
  event (`/design?new=1&eventId=<id>`). The board prefills it from the event and keeps the
  link on its own side (`design_requests.event_id`), built in the design-requests Pane.

## Membership and Cloudflare

Members use SQL search, exact campus/graduation/officer filters and offset pagination
(default 50, maximum 200). `total` is the filtered count. CSV exports every matching
row, ignores offset/limit, neutralizes spreadsheet formulas, and records an audit event.
Member fields retain the source D1 column names and nulls. Detail is read from the local
mirror; submission payloads are stored locally but are not exposed by this API.

Directors and leadership may patch names, emails, major, graduation year, campus and
pronouns with a required `auditNote`. Join date, officer history, identity, demographics
and membership consent are read-only. UW and CS email domains are validated before D1's
own triggers run. Each external write records intent first, then success or an unconfirmed
outcome. A failed response can mean D1 committed: sync before retrying. Writes and sync
share one lock; overlapping operations return 409. Roles are rechecked before writes.

Cloudflare configuration is environment-only: `CLOUDFLARE_API_TOKEN`,
`CLOUDFLARE_ACCOUNT_ID`, `CLOUDFLARE_ZONE_ID`, `D1_DATABASE_ID` (the ID of
`cseed-membership`). No settings or tokens are sent to the browser. The token needs
Analytics Read and product dataset access; D1 reads need D1 Read, and member editing
also needs D1 Edit. A read-only token supports analytics and mirror pulls but cannot
perform edits. Unset configuration logs a skip for each job and shows Not connected.

The job runs on startup and every 24 hours, with a four-minute deadline (manual Sync now is bounded to 45 seconds, below the HTTP timeout). D1 pages are
250 rows, ordered by primary key. Only a complete successful pull replaces People and
Membership_submissions in a transaction. Analytics queries fail independently per widget;
last successful data remains available with an error and timestamp. Snapshots upsert by
widget and UTC date. Account analytics cover the account; traffic/security cover the
configured zone. Web Analytics reports seven days; other analytics report the prior 24 hours (D1 usage uses the previous UTC date). Demographics stay in the mirror and are never exposed in directory responses. Signup counts include today and 7/30-day windows. If a daily job collides with another operation, it retries in one minute.
Last sync timestamps are epoch milliseconds, nullable before the first success.

Cloudflare references: [D1 query API](https://developers.cloudflare.com/api/resources/d1/subresources/database/methods/query/),
[GraphQL datasets](https://developers.cloudflare.com/analytics/graphql-api/features/data-sets/),
[D1 analytics](https://developers.cloudflare.com/d1/observability/metrics-analytics/).

### External Contacts

All active officers (including director and leadership) may read and edit the shared directory.
Contact writes, event-link replacements and outreach appends are atomic with their audit records.
Service tokens cannot use these session-authenticated endpoints.

| Method | Route | Behavior |
| --- | --- | --- |
| GET | `/api/contacts?q=&type=&topic=&hasSpokenForUs=` | `{items: Contact[]}`, ordered by name. Search covers name, organization, role, email and topics. Type `speaker`/`mentor` includes `both`; `both` matches only `both`. Topic is a case-insensitive exact match. Optional `hasSpokenForUs=true|false` filters speaking history. |
| POST | `/api/contacts` | Create; requires `name`, `type`. Returns `{contact}`. |
| GET | `/api/contacts/:id` | `{contact}` or 404. |
| PATCH | `/api/contacts/:id` | Partial update; omitted fields are preserved. Returns `{contact}`. |
| DELETE | `/api/contacts/:id` | Delete contact, event links and outreach; returns `{ok:true}`. Audit retains the complete prior record. |
| POST | `/api/contacts/:id/outreach` | Append outreach; requires `date` (ISO datetime with offset), `channel`, `outcome`; optional `note`. Returns `{contact}`. Officer comes from the session. |

Contact fields: `id`, `name` (1–200), `org`, `role` (up to 200), `type`
(`speaker`, `mentor`, `both`), `topics` (up to 30 nonblank strings, 100 characters each),
`email` (email or empty), `linkedin` (http/https URL or empty), `notes` (up to 10,000),
`hasSpokenForUs` (boolean, editable; defaults to false for new contacts), `createdAt` and `updatedAt` (epoch milliseconds), `events`, `outreach`, `lastOutreach`.
Create/PATCH accepts `eventIds` (up to 100 distinct existing event IDs); it replaces the links.
Empty strings clear optional text fields; empty arrays clear topics/event links. Nulls are rejected.
Topics are trimmed and deduplicated case-insensitively. Linked events carry `id`, `name`, `startsAt`.
The UI labels only events that have started as past events.

Outreach entries contain `id`, `officerId`, `officerName`, `date`, `channel`, `outcome`, `note`,
`createdAt`. Channels: `email`, `linkedin`, `discord`, `phone`, `in_person`.
Outcomes: `awaiting_reply`, `interested`, `confirmed`, `declined`, `follow_up`.
Dates are normalized to UTC; the latest dated entry wins, with creation time then ID as deterministic
 tie breakers. Backdated entries remain in the history. `lastOutreach` is null before outreach is logged.
General contact edits never change the derived last outreach. Outreach is append-only.

`GET /api/search` also returns `kind: "contact"` for all officers, linking to `/contacts?id=…`.

External Contacts lives at `/contacts`; `/speakers` permanently redirects there, preserving query parameters. Existing `/api/contacts` endpoints remain unchanged. Migration 0011 initializes `has_spoken_for_us` from linked events whose start time is before migration time. Later event-link edits do not override this manually maintained field. Create/PATCH accepts optional `hasSpokenForUs`; omission on PATCH preserves the value, and changes appear in contact audit before/after records. Types remain `speaker`, `mentor`, and `both`; adding `other` is deferred because the existing database CHECK constraint requires a table rebuild.

### Club location and time zone

`GET /api/club/settings` returns `{location, clubTimezone}` for every officer. `PUT` requires active leadership (including owners), rechecks permission in the transaction, and records `club_settings.updated` with before/after values. Directors and officers receive 403. Both fields are required; location is 1–200 characters and clubTimezone is a valid IANA name. Defaults: University of Washington, Seattle / America/Los_Angeles.

Automatic mode detects the browser zone on load, focus, and visibility return, persisting changes through `/api/me/prefs` for calendar emails. Until detection is possible it uses the club zone. Date-only deadlines remain calendar dates; timestamps remain UTC instants. Invitations retain UTC DTSTART/DTEND for interoperability and include the recipient’s display zone as `X-CSEED-TIMEZONE` metadata. Email text uses that same zone. Calendar clients choose their own display zone.
