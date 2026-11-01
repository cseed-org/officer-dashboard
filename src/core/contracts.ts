/**
 * API contract between the UI and the backend. Every request body, query string and
 * response the UI calls is a zod schema here, with its TS type inferred next to it.
 * Endpoints, methods and roles are listed in docs/api.md and in ENDPOINTS below.
 *
 * Conventions
 * - JSON over HTTPS; multipart/form-data only for file uploads (CSV import, attachments).
 * - Money is integer cents (`*Cents`). Rates are fractions 0..1 or null when undefined.
 * - Dates are `YYYY-MM-DD`; datetimes are ISO 8601 with offset; timestamps from the DB are
 *   serialised as ISO datetimes.
 * - Errors always use ApiError with an HTTP status: 400 validation, 401 not signed in,
 *   403 wrong role, 404 not found, 409 invalid state transition / conflict, 422 import errors.
 * - Lists are cursor-paginated: pass `cursor` from the previous `nextCursor`.
 */
import { z } from 'zod';
import {
  ACCOUNT_TYPES,
  APPROVAL_DECISIONS,
  ATTACHMENT_KINDS,
  CSV_IMPORT_KINDS,
  FINANCE_REPORTS,
  FISCAL_PERIOD_STATUSES,
  FORM990_PARTS,
  FULFILLMENT_JOB_STATUSES,
  FUNCTIONAL_CLASSES,
  FUND_KINDS,
  PURCHASE_STATUSES,
  PURCHASE_TYPES,
  RESTRICTIONS,
  ROLES,
  SERVICE_TOKEN_SCOPES,
  STORAGE_BACKENDS,
} from './domain';

// ───────────────────────────── Primitives ─────────────────────────────

export const Id = z.string().min(1).max(64);
export const IsoDate = z.iso.date();
export const IsoDateTime = z.iso.datetime({ offset: true });
export const Cents = z.number().int();
export const NonNegCents = Cents.nonnegative();
export const Rate = z.number().min(0).max(1).nullable();
export const DiscordId = z.string().regex(/^\d{17,20}$/, 'Discord IDs are 17–20 digits');
export const FiscalYear = z.number().int().min(2000).max(2100);

export const Role = z.enum(ROLES);
export const PurchaseStatus = z.enum(PURCHASE_STATUSES);
export const PurchaseType = z.enum(PURCHASE_TYPES);
export const FunctionalClass = z.enum(FUNCTIONAL_CLASSES);
export const AccountType = z.enum(ACCOUNT_TYPES);
export const Restriction = z.enum(RESTRICTIONS);
export const FundKind = z.enum(FUND_KINDS);
export const AttachmentKind = z.enum(ATTACHMENT_KINDS);
export const ServiceTokenScope = z.enum(SERVICE_TOKEN_SCOPES);
export const FinanceReportName = z.enum(FINANCE_REPORTS);

/** Query-string integers arrive as strings. */
const QueryInt = z.coerce.number().int();

export const PageQuery = z.object({
  cursor: z.string().optional(),
  limit: QueryInt.min(1).max(200).default(50),
});

const page = <T extends z.ZodType>(item: T) =>
  z.object({ items: z.array(item), nextCursor: z.string().nullable() });

export const ApiError = z.object({
  error: z.object({
    code: z.enum([
      'bad_request',
      'unauthenticated',
      'forbidden',
      'not_found',
      'conflict',
      'invalid_transition',
      'import_failed',
      'internal',
    ]),
    message: z.string(),
    /** Field errors (from zod `flatten()`), or per-row import errors. */
    details: z.unknown().optional(),
  }),
});
export type ApiError = z.infer<typeof ApiError>;

export const OkResponse = z.object({ ok: z.literal(true) });
export type OkResponse = z.infer<typeof OkResponse>;

// ───────────────────────────── Shared shapes ─────────────────────────────

export const UserSummary = z.object({
  id: Id,
  name: z.string(),
  avatarUrl: z.string().nullable(),
});
export type UserSummary = z.infer<typeof UserSummary>;

export const CurrentUser = UserSummary.extend({
  discordId: z.string(),
  role: Role,
  title: z.string().nullable(),
});
export type CurrentUser = z.infer<typeof CurrentUser>;

export const AccountOption = z.object({
  id: Id,
  code: z.string(),
  name: z.string(),
  type: AccountType,
  form990Line: z.string().nullable(),
});
export type AccountOption = z.infer<typeof AccountOption>;

export const FundOption = z.object({
  id: Id,
  code: z.string(),
  name: z.string(),
  kind: FundKind,
  restriction: Restriction,
});
export type FundOption = z.infer<typeof FundOption>;

/**
 * Event health counts (see src/core/metrics.ts). reflectionRate counts only reflections
 * matched to a checked-in guest by normalized email, so it stays within 0..1; the rest
 * are reported as unmatchedReflections so the UI can flag the data problem.
 */
export const EventCounts = z.object({
  registered: z.number().int().nonnegative(),
  checkedIn: z.number().int().nonnegative(),
  /** All reflections received (distinct respondents). */
  reflections: z.number().int().nonnegative(),
  /** Reflections from guests who checked in (matched by normalized email). */
  matchedReflections: z.number().int().nonnegative(),
  /** Reflections with no matching check-in (reflections − matchedReflections). */
  unmatchedReflections: z.number().int().nonnegative(),
  /** checkedIn / registered */
  checkInRate: Rate,
  /** matchedReflections / checkedIn */
  reflectionRate: Rate,
});
export type EventCounts = z.infer<typeof EventCounts>;

// ───────────────────────────── Session / me ─────────────────────────────

/** GET /api/me */
export const MeResponse = z.object({ user: CurrentUser });
export type MeResponse = z.infer<typeof MeResponse>;

/** GET /api/lookups — dropdown options for forms. */
export const LookupsResponse = z.object({
  expenseAccounts: z.array(AccountOption),
  funds: z.array(FundOption),
  events: z.array(z.object({ id: Id, name: z.string(), startsAt: IsoDateTime })),
  currentFiscalYear: FiscalYear,
});
export type LookupsResponse = z.infer<typeof LookupsResponse>;

// ───────────────────────────── Search ─────────────────────────────

export const SearchKind = z.enum(['event', 'request', 'user', 'account']);

/**
 * GET /api/search?q=<text>&limit=<1..50, default 20> — the sidebar search. Matches
 * case-insensitively on event names and locations, request numbers, titles and vendors, user
 * names and emails, and account names and codes.
 */
export const SearchQuery = z.object({
  q: z.string().trim().min(1).max(100),
  limit: QueryInt.min(1).max(50).default(20),
});
export type SearchQuery = z.infer<typeof SearchQuery>;

/** `href` is an app path: /events/:id, /requests/:id, /admin/users or /finance?tab=accounts. */
export const SearchResult = z.object({
  kind: SearchKind,
  id: Id,
  title: z.string(),
  subtitle: z.string(),
  href: z.string(),
});
export type SearchResult = z.infer<typeof SearchResult>;

/** Users and accounts appear only for directors. Best matches first, at most `limit` results. */
export const SearchResponse = z.object({ results: z.array(SearchResult) });
export type SearchResponse = z.infer<typeof SearchResponse>;

// ───────────────────────────── Overview ─────────────────────────────

export const EventListItem = z.object({
  id: Id,
  lumaId: z.string().nullable(),
  name: z.string(),
  startsAt: IsoDateTime,
  location: z.string().nullable(),
  host: UserSummary.nullable(),
  metrics: EventCounts,
});
export type EventListItem = z.infer<typeof EventListItem>;

/** GET /api/overview */
export const OverviewResponse = z.object({
  fiscalYear: FiscalYear,
  kpis: z.object({
    upcomingEvents: z.number().int().nonnegative(),
    /** Mean over past events in the fiscal year (one vote per event). */
    avgCheckInRate: Rate,
    avgReflectionRate: Rate,
    budgetCents: NonNegCents,
    spentCents: Cents,
    /** Approved but not yet posted (encumbered). */
    committedCents: NonNegCents,
    /** budget − spent − committed; may be negative when over budget. */
    remainingCents: Cents,
    pendingRequests: z.number().int().nonnegative(),
    pendingRequestsCents: NonNegCents,
  }),
  recentEvents: z.array(EventListItem).max(10),
});
export type OverviewResponse = z.infer<typeof OverviewResponse>;

// ───────────────────────────── Events ─────────────────────────────

/** GET /api/events */
export const EventsListQuery = PageQuery.extend({
  when: z.enum(['upcoming', 'past', 'all']).default('all'),
  q: z.string().max(100).optional(),
  from: IsoDate.optional(),
  to: IsoDate.optional(),
});
export type EventsListQuery = z.infer<typeof EventsListQuery>;

export const EventsListResponse = page(EventListItem);
export type EventsListResponse = z.infer<typeof EventsListResponse>;

/** GET /api/events/:id */
export const EventDetailResponse = z.object({
  event: EventListItem.extend({
    endsAt: IsoDateTime.nullable(),
    url: z.string().nullable(),
    updatedAt: IsoDateTime,
  }),
  /** Purchase requests linked to this event. */
  requests: z.array(z.lazy(() => PurchaseRequestListItem)),
  spentCents: Cents,
});
export type EventDetailResponse = z.infer<typeof EventDetailResponse>;

/** GET /api/events/:id/metrics — chart data. */
export const EventMetricsResponse = z.object({
  eventId: Id,
  totals: EventCounts,
  /** Cumulative registrations by day up to the event date. */
  registrationsByDay: z.array(z.object({ date: IsoDate, cumulative: z.number().int().nonnegative() })),
  /** Reflections received by day after the event. */
  reflectionsByDay: z.array(z.object({ date: IsoDate, count: z.number().int().nonnegative() })),
});
export type EventMetricsResponse = z.infer<typeof EventMetricsResponse>;

/** POST /api/events (create manually) and PATCH /api/events/:id (all fields optional). */
export const EventUpsertRequest = z.object({
  name: z.string().trim().min(1).max(200),
  startsAt: IsoDateTime,
  endsAt: IsoDateTime.nullable().optional(),
  location: z.string().max(200).nullable().optional(),
  url: z.url().nullable().optional(),
  lumaId: z.string().max(100).nullable().optional(),
  hostUserId: Id.nullable().optional(),
});
export type EventUpsertRequest = z.infer<typeof EventUpsertRequest>;
export const EventPatchRequest = EventUpsertRequest.partial();
export type EventPatchRequest = z.infer<typeof EventPatchRequest>;

export const EventUpsertResponse = z.object({ event: EventListItem });
export type EventUpsertResponse = z.infer<typeof EventUpsertResponse>;

// ───────────────────────────── CSV import ─────────────────────────────

/**
 * POST /api/events/import — multipart/form-data with these text fields plus `file` (CSV,
 * ≤ 5 MB). `dryRun=true` validates and returns the same report without writing.
 * Luma guest export: one CSV per event (eventId, or lumaEventId/eventName to create one).
 */
export const CsvImportFields = z.object({
  kind: z.enum(CSV_IMPORT_KINDS),
  eventId: Id.optional(),
  /** Create or match an event by Luma id when importing a fresh guest export. */
  lumaEventId: z.string().optional(),
  eventName: z.string().optional(),
  eventStartsAt: IsoDateTime.optional(),
  dryRun: z.enum(['true', 'false']).default('false').transform((v) => v === 'true'),
});
export type CsvImportFields = z.infer<typeof CsvImportFields>;

export const CsvImportResponse = z.object({
  eventId: Id,
  kind: z.enum(CSV_IMPORT_KINDS),
  dryRun: z.boolean(),
  rows: z.number().int().nonnegative(),
  created: z.number().int().nonnegative(),
  updated: z.number().int().nonnegative(),
  skipped: z.number().int().nonnegative(),
  errors: z.array(z.object({ row: z.number().int().positive(), message: z.string() })),
  metrics: EventCounts,
});
export type CsvImportResponse = z.infer<typeof CsvImportResponse>;

/** POST /api/ingest/luma — service token with scope `ingest:luma` (future scraper agent). */
export const LumaIngestRequest = z.object({
  event: z.object({
    lumaId: z.string().min(1),
    name: z.string().min(1),
    startsAt: IsoDateTime,
    endsAt: IsoDateTime.nullable().optional(),
    location: z.string().nullable().optional(),
    url: z.url().nullable().optional(),
  }),
  guests: z
    .array(
      z.object({
        email: z.email().transform((e) => e.toLowerCase()),
        name: z.string().nullable().optional(),
        lumaGuestId: z.string().nullable().optional(),
        approvalStatus: z.string().nullable().optional(),
        registeredAt: IsoDateTime.nullable().optional(),
        checkedInAt: IsoDateTime.nullable().optional(),
      }),
    )
    .max(5000),
});
export type LumaIngestRequest = z.infer<typeof LumaIngestRequest>;
export const LumaIngestResponse = CsvImportResponse.omit({ kind: true, dryRun: true });
export type LumaIngestResponse = z.infer<typeof LumaIngestResponse>;

// ───────────────────────────── Purchase requests ─────────────────────────────

export const Attachment = z.object({
  id: Id,
  kind: AttachmentKind,
  filename: z.string(),
  mimeType: z.string(),
  sizeBytes: z.number().int().nonnegative(),
  storage: z.enum(STORAGE_BACKENDS),
  /** Drive webViewLink, or a /api/attachments/:id URL for the local stub. */
  url: z.string().nullable(),
  uploadedBy: UserSummary,
  createdAt: IsoDateTime,
});
export type Attachment = z.infer<typeof Attachment>;

export const Approval = z.object({
  id: Id,
  director: UserSummary,
  decision: z.enum(APPROVAL_DECISIONS),
  comment: z.string().nullable(),
  createdAt: IsoDateTime,
});
export type Approval = z.infer<typeof Approval>;

export const PurchaseRequestListItem = z.object({
  id: Id,
  number: z.string(),
  title: z.string(),
  vendor: z.string(),
  type: PurchaseType,
  status: PurchaseStatus,
  totalCents: NonNegCents,
  requester: UserSummary,
  expenseAccount: AccountOption.pick({ id: true, code: true, name: true }),
  fund: FundOption.pick({ id: true, code: true, name: true }),
  eventId: Id.nullable(),
  submittedAt: IsoDateTime.nullable(),
  updatedAt: IsoDateTime,
});
export type PurchaseRequestListItem = z.infer<typeof PurchaseRequestListItem>;

/** GET /api/requests — every officer can read every request (scope=all); `mine` is the default tab. */
export const PurchaseRequestsListQuery = PageQuery.extend({
  scope: z.enum(['mine', 'all']).default('mine'),
  status: z
    .union([PurchaseStatus, z.literal('pending')])
    .optional()
    .describe('`pending` = submitted (awaiting a director)'),
  eventId: Id.optional(),
  q: z.string().max(100).optional(),
});
export type PurchaseRequestsListQuery = z.infer<typeof PurchaseRequestsListQuery>;

export const PurchaseRequestsListResponse = page(PurchaseRequestListItem).extend({
  /** Counts per status for tab badges, over the same scope. */
  counts: z.partialRecord(PurchaseStatus, z.number().int().nonnegative()),
});
export type PurchaseRequestsListResponse = z.infer<typeof PurchaseRequestsListResponse>;

const PurchaseRequestFields = z.object({
  type: PurchaseType,
  title: z.string().trim().min(1).max(200),
  vendor: z.string().trim().min(1).max(200),
  itemUrl: z.url().nullable().optional(),
  quantity: z.number().int().min(1).max(10_000),
  unitCostCents: NonNegCents,
  taxShipCents: NonNegCents.default(0),
  expenseAccountId: Id,
  fundId: Id,
  eventId: Id.nullable().optional(),
  functionalClass: FunctionalClass.default('program'),
  justification: z.string().trim().min(1).max(4000),
  neededBy: IsoDate.nullable().optional(),
});

/**
 * POST /api/requests. The server computes totalCents (quantity × unitCost + taxShip).
 * `submit: true` creates and submits in one step; otherwise it is saved as a draft.
 */
export const PurchaseRequestCreateRequest = PurchaseRequestFields.extend({
  submit: z.boolean().default(false),
});
export type PurchaseRequestCreateRequest = z.infer<typeof PurchaseRequestCreateRequest>;

/** PATCH /api/requests/:id — drafts only (409 otherwise). */
export const PurchaseRequestUpdateRequest = PurchaseRequestFields.partial().extend({
  // Zod 4 applies defaults inside optional fields; PATCH must preserve omitted values.
  taxShipCents: NonNegCents.optional(),
  functionalClass: FunctionalClass.optional(),
});
export type PurchaseRequestUpdateRequest = z.infer<typeof PurchaseRequestUpdateRequest>;

export const PurchaseRequestDetail = PurchaseRequestListItem.extend({
  itemUrl: z.string().nullable(),
  quantity: z.number().int().positive(),
  unitCostCents: NonNegCents,
  taxShipCents: NonNegCents,
  /** Set at settlement (fulfilled / reimbursed); the amount actually posted. totalCents stays the approved estimate. */
  actualTotalCents: NonNegCents.nullable(),
  functionalClass: FunctionalClass,
  justification: z.string(),
  neededBy: IsoDate.nullable(),
  event: z.object({ id: Id, name: z.string() }).nullable(),
  decidedAt: IsoDateTime.nullable(),
  createdAt: IsoDateTime,
  approvals: z.array(Approval),
  attachments: z.array(Attachment),
  /** Journal entries posted for this request (fulfilment / reimbursement). */
  journalEntryIds: z.array(Id),
  /** Status moves the current user may make right now (drives the action buttons). */
  allowedTransitions: z.array(PurchaseStatus),
  /** Whether the current user may edit (draft + requester). */
  canEdit: z.boolean(),
  /**
   * Set while the purchasing agent's settlement is held for director review (it
   * reported more than the approved estimate plus tolerance). A director clears it
   * by settling the request with POST /transition.
   */
  settlementHold: z
    .object({ attemptedCents: NonNegCents, approvedCents: NonNegCents, reason: z.string(), heldAt: IsoDateTime })
    .nullable(),
});
export type PurchaseRequestDetail = z.infer<typeof PurchaseRequestDetail>;

/** Response of GET/POST/PATCH /api/requests/:id and every action endpoint. */
export const PurchaseRequestResponse = z.object({ request: PurchaseRequestDetail });
export type PurchaseRequestResponse = z.infer<typeof PurchaseRequestResponse>;

/** POST /api/requests/:id/approve | /deny (director). A comment is required to deny. */
export const PurchaseDecisionRequest = z.object({
  comment: z.string().trim().max(2000).optional(),
});
export type PurchaseDecisionRequest = z.infer<typeof PurchaseDecisionRequest>;
export const PurchaseDenyRequest = z.object({ comment: z.string().trim().min(1).max(2000) });
export type PurchaseDenyRequest = z.infer<typeof PurchaseDenyRequest>;

const TransitionComment = z.string().trim().max(2000).optional();

/**
 * Settlement: what was actually paid. Moving to `fulfilled` (club purchase) or
 * `reimbursed` (reimbursement) posts the journal entry for **actualTotalCents**, not the
 * approved estimate (`totalCents`); the encumbrance for the estimate is released at the
 * same time. The actual amount may be lower or higher than the estimate (discounts,
 * shipping); both are kept on the request so the difference stays visible.
 */
export const PurchaseSettlement = z.object({
  actualTotalCents: NonNegCents,
  /** Receipts already uploaded via POST /api/requests/:id/attachments. */
  attachmentIds: z.array(Id).max(20).default([]),
  /** e.g. order number from the vendor. */
  orderReference: z.string().trim().max(200).optional(),
  comment: TransitionComment,
});
export type PurchaseSettlement = z.infer<typeof PurchaseSettlement>;

/**
 * POST /api/requests/:id/transition — later lifecycle moves (director). The body is
 * discriminated on `to`: the two posting moves (`fulfilled`, `reimbursed`) require a
 * PurchaseSettlement; the others take an optional comment.
 */
export const PurchaseTransitionRequest = z.discriminatedUnion('to', [
  z.object({ to: z.literal('fulfilling'), comment: TransitionComment }),
  PurchaseSettlement.extend({ to: z.literal('fulfilled') }),
  PurchaseSettlement.extend({ to: z.literal('reimbursed') }),
  z.object({ to: z.literal('closed'), comment: TransitionComment }),
  z.object({ to: z.literal('cancelled'), comment: TransitionComment }),
]);
export type PurchaseTransitionRequest = z.infer<typeof PurchaseTransitionRequest>;

/** POST /api/requests/:id/attachments — multipart: `file` (≤ 10 MB; pdf/png/jpeg/heic/webp) + these fields. */
export const AttachmentUploadFields = z.object({ kind: AttachmentKind.default('receipt') });
export type AttachmentUploadFields = z.infer<typeof AttachmentUploadFields>;
export const AttachmentUploadResponse = z.object({ attachment: Attachment });
export type AttachmentUploadResponse = z.infer<typeof AttachmentUploadResponse>;

export const ATTACHMENT_MAX_BYTES = 10 * 1024 * 1024;
export const ATTACHMENT_MIME_TYPES = [
  'application/pdf',
  'image/png',
  'image/jpeg',
  'image/heic',
  'image/webp',
] as const;
export const CSV_MAX_BYTES = 5 * 1024 * 1024;

// ───────────────────────────── Finance ─────────────────────────────

export const FinanceQuery = z.object({
  fiscalYear: QueryInt.pipe(FiscalYear).optional(),
  /** Report "as of" / period end; defaults to today. */
  asOf: IsoDate.optional(),
  fundId: Id.optional(),
});
export type FinanceQuery = z.infer<typeof FinanceQuery>;

/** GET /api/finance/summary — what officers see on /finance. */
export const FinanceSummaryResponse = z.object({
  fiscalYear: FiscalYear,
  asOf: IsoDate,
  totals: z.object({
    budgetCents: NonNegCents,
    spentCents: Cents,
    committedCents: NonNegCents,
    remainingCents: Cents,
  }),
  byCategory: z.array(
    z.object({
      account: AccountOption.pick({ id: true, code: true, name: true }),
      budgetCents: NonNegCents,
      spentCents: Cents,
      committedCents: NonNegCents,
      remainingCents: Cents,
    }),
  ),
  funds: z.array(
    FundOption.extend({ balanceCents: Cents, awardedCents: NonNegCents.nullable(), endDate: IsoDate.nullable() }),
  ),
});
export type FinanceSummaryResponse = z.infer<typeof FinanceSummaryResponse>;

const ReportMeta = z.object({
  fiscalYear: FiscalYear,
  from: IsoDate,
  asOf: IsoDate,
  generatedAt: IsoDateTime,
  fundId: Id.nullable(),
});

const AccountRef = AccountOption.pick({ id: true, code: true, name: true, type: true, form990Line: true });

export const TrialBalanceReport = ReportMeta.extend({
  report: z.literal('trial-balance'),
  rows: z.array(z.object({ account: AccountRef, debitCents: NonNegCents, creditCents: NonNegCents })),
  totals: z.object({ debitCents: NonNegCents, creditCents: NonNegCents }),
});

const ActivityRow = z.object({
  account: AccountRef,
  withoutDonorCents: Cents,
  withDonorCents: Cents,
  totalCents: Cents,
});
export const StatementOfActivitiesReport = ReportMeta.extend({
  report: z.literal('statement-of-activities'),
  revenue: z.array(ActivityRow),
  /** Net assets released from restriction (reclassification). */
  releasesCents: Cents,
  expenses: z.array(ActivityRow),
  changeInNetAssets: z.object({ withoutDonorCents: Cents, withDonorCents: Cents, totalCents: Cents }),
  netAssetsBeginning: z.object({ withoutDonorCents: Cents, withDonorCents: Cents, totalCents: Cents }),
  netAssetsEnding: z.object({ withoutDonorCents: Cents, withDonorCents: Cents, totalCents: Cents }),
});

const BalanceRow = z.object({ account: AccountRef, balanceCents: Cents });
export const StatementOfFinancialPositionReport = ReportMeta.extend({
  report: z.literal('statement-of-financial-position'),
  assets: z.array(BalanceRow),
  liabilities: z.array(BalanceRow),
  netAssets: z.object({ withoutDonorCents: Cents, withDonorCents: Cents }),
  totals: z.object({
    assetsCents: Cents,
    liabilitiesCents: Cents,
    netAssetsCents: Cents,
    /** Must be true: assets = liabilities + net assets. */
    balanced: z.boolean(),
  }),
});

export const StatementOfFunctionalExpensesReport = ReportMeta.extend({
  report: z.literal('statement-of-functional-expenses'),
  rows: z.array(
    z.object({
      account: AccountRef,
      programCents: Cents,
      mgmtGeneralCents: Cents,
      fundraisingCents: Cents,
      totalCents: Cents,
    }),
  ),
  totals: z.object({
    programCents: Cents,
    mgmtGeneralCents: Cents,
    fundraisingCents: Cents,
    totalCents: Cents,
  }),
});

export const BudgetVsActualReport = ReportMeta.extend({
  report: z.literal('budget-vs-actual'),
  rows: z.array(
    z.object({
      account: AccountRef,
      fund: FundOption.pick({ id: true, code: true, name: true }).nullable(),
      budgetCents: NonNegCents,
      actualCents: Cents,
      committedCents: NonNegCents,
      varianceCents: Cents,
      /** actual / budget, null when budget is 0. Not bounded to 1 (over budget > 1). */
      usedRate: z.number().nonnegative().nullable(),
    }),
  ),
  totals: z.object({ budgetCents: NonNegCents, actualCents: Cents, committedCents: NonNegCents, varianceCents: Cents }),
});

export const FundBalancesReport = ReportMeta.extend({
  report: z.literal('fund-balances'),
  rows: z.array(
    z.object({
      fund: FundOption,
      beginningCents: Cents,
      additionsCents: Cents,
      releasesCents: Cents,
      expensesCents: Cents,
      endingCents: Cents,
      restrictedEndingCents: Cents,
      unrestrictedEndingCents: Cents,
    }),
  ),
});

/**
 * GET /api/finance/reports/:report — JSON by default; `?format=csv` returns text/csv
 * with a Content-Disposition filename. Officers may read OFFICER_FINANCE_REPORTS only.
 */
export const FinanceReportQuery = FinanceQuery.extend({ format: z.enum(['json', 'csv']).default('json') });
export type FinanceReportQuery = z.infer<typeof FinanceReportQuery>;

export const Form990Report = ReportMeta.extend({
  report: z.literal('form-990'),
  rows: z.array(z.object({ part: z.enum(['VIII', 'IX']), line: z.string(), totalCents: Cents, programCents: Cents, mgmtGeneralCents: Cents, fundraisingCents: Cents })),
  /** Totals over mapped lines: Part VIII line 12 (total revenue) and Part IX line 25 (total functional expenses). */
  totals: z.object({
    partVIIILine12Cents: Cents,
    partIXLine25: z.object({ totalCents: Cents, programCents: Cents, mgmtGeneralCents: Cents, fundraisingCents: Cents }),
  }),
  /** Line 1g memo: noncash (in-kind, stock) contributions already included in lines 1a–1f. */
  line1gNoncashCents: Cents,
  /** Revenue/expense accounts with no 990 mapping; their amounts are NOT in the totals above. */
  unmappedAccounts: z.array(z.object({ account: AccountRef, totalCents: Cents })),
});
export type Form990Report = z.infer<typeof Form990Report>;

export const FinanceReportResponse = z.discriminatedUnion('report', [
  TrialBalanceReport,
  StatementOfActivitiesReport,
  StatementOfFinancialPositionReport,
  StatementOfFunctionalExpensesReport,
  BudgetVsActualReport,
  FundBalancesReport,
  Form990Report,
]);
export type FinanceReportResponse = z.infer<typeof FinanceReportResponse>;

/** GET /api/finance/budgets?fiscalYear= */
export const BudgetLine = z.object({
  id: Id,
  fiscalYear: FiscalYear,
  account: AccountOption.pick({ id: true, code: true, name: true }),
  fund: FundOption.pick({ id: true, code: true, name: true }).nullable(),
  amountCents: NonNegCents,
  notes: z.string().nullable(),
});
export type BudgetLine = z.infer<typeof BudgetLine>;
export const BudgetsResponse = z.object({ fiscalYear: FiscalYear, lines: z.array(BudgetLine) });
export type BudgetsResponse = z.infer<typeof BudgetsResponse>;

/** PUT /api/finance/budgets (director) — replaces the whole fiscal year's budget. */
export const BudgetsUpsertRequest = z.object({
  fiscalYear: FiscalYear,
  lines: z
    .array(
      z.object({
        accountId: Id,
        fundId: Id.nullable(),
        amountCents: NonNegCents,
        notes: z.string().max(500).nullable().optional(),
      }),
    )
    .max(500),
});
export type BudgetsUpsertRequest = z.infer<typeof BudgetsUpsertRequest>;

/** GET /api/finance/periods (director) */
export const FiscalPeriodsResponse = z.object({
  periods: z.array(
    z.object({
      id: Id,
      fiscalYear: FiscalYear,
      name: z.string(),
      startDate: IsoDate,
      endDate: IsoDate,
      status: z.enum(FISCAL_PERIOD_STATUSES),
    }),
  ),
});
export type FiscalPeriodsResponse = z.infer<typeof FiscalPeriodsResponse>;
export type FinancePeriod = FiscalPeriodsResponse['periods'][number];

/** POST /api/finance/periods (director) — creates the 12 monthly periods of a fiscal year that has none. */
export const FiscalYearPeriodsCreateRequest = z.object({ fiscalYear: FiscalYear });
export type FiscalYearPeriodsCreateRequest = z.infer<typeof FiscalYearPeriodsCreateRequest>;

/**
 * POST /api/finance/periods/:id/close (director) — open → closed or locked, closed → locked.
 * Closed and locked periods reject new entries; neither can reopen.
 */
export const FiscalPeriodCloseRequest = z.object({ to: z.enum(['closed', 'locked']) });
export type FiscalPeriodCloseRequest = z.infer<typeof FiscalPeriodCloseRequest>;

/** GET /api/finance/accounts (director) — chart of accounts with 990 mapping. */
export const ChartOfAccountsResponse = z.object({
  accounts: z.array(
    AccountOption.extend({
      form990Part: z.enum(FORM990_PARTS).nullable(),
      form990Label: z.string().nullable(),
      restriction: Restriction.nullable(),
      budgetable: z.boolean(),
      active: z.boolean(),
    }),
  ),
});
export type ChartOfAccountsResponse = z.infer<typeof ChartOfAccountsResponse>;

// ───────────────────────────── Admin: users ─────────────────────────────

export const AdminUser = CurrentUser.extend({
  email: z.string().nullable(),
  active: z.boolean(),
  lastLoginAt: IsoDateTime.nullable(),
  createdAt: IsoDateTime,
});
export type AdminUser = z.infer<typeof AdminUser>;

/** GET /api/admin/users */
export const AdminUsersResponse = z.object({ users: z.array(AdminUser) });
export type AdminUsersResponse = z.infer<typeof AdminUsersResponse>;

/** POST /api/admin/users — allowlist a Discord account. 409 if the Discord ID exists. */
export const AdminUserCreateRequest = z.object({
  discordId: DiscordId,
  name: z.string().trim().min(1).max(100),
  role: Role.default('officer'),
  title: z.string().trim().max(100).nullable().optional(),
  email: z.email().nullable().optional(),
});
export type AdminUserCreateRequest = z.infer<typeof AdminUserCreateRequest>;

/**
 * PATCH /api/admin/users/:id — change role / deactivate. Deactivating ends the user's
 * sessions. 409 when it would leave no active director.
 */
export const AdminUserUpdateRequest = z.object({
  name: z.string().trim().min(1).max(100).optional(),
  role: Role.optional(),
  active: z.boolean().optional(),
  title: z.string().trim().max(100).nullable().optional(),
  email: z.email().nullable().optional(),
});
export type AdminUserUpdateRequest = z.infer<typeof AdminUserUpdateRequest>;

export const AdminUserResponse = z.object({ user: AdminUser });
export type AdminUserResponse = z.infer<typeof AdminUserResponse>;

// ───────────────────────────── Admin: service tokens ─────────────────────────────

export const ServiceToken = z.object({
  id: Id,
  name: z.string(),
  prefix: z.string(),
  scopes: z.array(ServiceTokenScope),
  createdBy: UserSummary,
  createdAt: IsoDateTime,
  lastUsedAt: IsoDateTime.nullable(),
  expiresAt: IsoDateTime.nullable(),
  revokedAt: IsoDateTime.nullable(),
});
export type ServiceToken = z.infer<typeof ServiceToken>;

/** GET /api/admin/tokens */
export const ServiceTokensResponse = z.object({ tokens: z.array(ServiceToken) });
export type ServiceTokensResponse = z.infer<typeof ServiceTokensResponse>;

/** POST /api/admin/tokens */
export const ServiceTokenCreateRequest = z.object({
  name: z.string().trim().min(1).max(100),
  scopes: z.array(ServiceTokenScope).min(1),
  /** Days until expiry; null = never. */
  expiresInDays: z.number().int().min(1).max(365).nullable().default(90),
});
export type ServiceTokenCreateRequest = z.infer<typeof ServiceTokenCreateRequest>;

/** The plaintext token appears only in this response. */
export const ServiceTokenCreateResponse = z.object({ token: ServiceToken, plaintext: z.string() });
export type ServiceTokenCreateResponse = z.infer<typeof ServiceTokenCreateResponse>;

// DELETE /api/admin/tokens/:id → OkResponse (revokes; the row is kept for audit).

// ───────────────────────────── Admin: audit log ─────────────────────────────

/** GET /api/admin/audit */
export const AuditLogQuery = PageQuery.extend({ entityType: z.string().optional(), entityId: Id.optional() });
export type AuditLogQuery = z.infer<typeof AuditLogQuery>;
export const AuditLogEntry = z.object({
  id: Id,
  actor: UserSummary.nullable(),
  actorTokenName: z.string().nullable(),
  action: z.string(),
  entityType: z.string(),
  entityId: z.string().nullable(),
  before: z.unknown().nullable(),
  after: z.unknown().nullable(),
  createdAt: IsoDateTime,
});
export type AuditLogEntry = z.infer<typeof AuditLogEntry>;
export const AuditLogResponse = page(AuditLogEntry);
export type AuditLogResponse = z.infer<typeof AuditLogResponse>;

// ───────────────────────────── Agent (service token `agent:jobs`) ─────────────────────────────

export const FulfillmentJob = z.object({
  id: Id,
  status: z.enum(FULFILLMENT_JOB_STATUSES),
  attempts: z.number().int().nonnegative(),
  request: PurchaseRequestDetail.omit({ allowedTransitions: true, canEdit: true }),
  createdAt: IsoDateTime,
  claimedAt: IsoDateTime.nullable(),
});
export type FulfillmentJob = z.infer<typeof FulfillmentJob>;

/** GET /api/agent/jobs?status=queued */
export const AgentJobsQuery = z.object({ status: z.enum(FULFILLMENT_JOB_STATUSES).default('queued') });
export type AgentJobsQuery = z.infer<typeof AgentJobsQuery>;
export const AgentJobsResponse = z.object({ jobs: z.array(FulfillmentJob) });
export type AgentJobsResponse = z.infer<typeof AgentJobsResponse>;

// POST /api/agent/jobs/:id/claim → { job: FulfillmentJob } (409 if already claimed)
export const AgentJobResponse = z.object({ job: FulfillmentJob });
export type AgentJobResponse = z.infer<typeof AgentJobResponse>;

/** POST /api/agent/jobs/:id/complete */
export const AgentJobCompleteRequest = z.discriminatedUnion('outcome', [
  z.object({
    outcome: z.literal('done'),
    /** What was actually paid; posted instead of the estimate, as in PurchaseSettlement. */
    actualTotalCents: NonNegCents,
    attachmentIds: z.array(Id).max(20).default([]),
    orderReference: z.string().max(200).optional(),
    notes: z.string().max(2000).optional(),
  }),
  z.object({ outcome: z.literal('failed'), error: z.string().min(1).max(2000) }),
]);
export type AgentJobCompleteRequest = z.infer<typeof AgentJobCompleteRequest>;

// ───────────────────────────── Health ─────────────────────────────

/** GET /api/health (public) */
export const HealthResponse = z.object({ ok: z.literal(true), db: z.boolean(), version: z.string() });
export type HealthResponse = z.infer<typeof HealthResponse>;

// ───────────────────────────── Endpoint index ─────────────────────────────

type Auth = 'public' | 'officer' | 'director' | `token:${(typeof SERVICE_TOKEN_SCOPES)[number]}`;
interface EndpointSpec {
  method: 'GET' | 'POST' | 'PATCH' | 'PUT' | 'DELETE';
  path: string;
  auth: Auth;
  query?: z.ZodType;
  body?: z.ZodType | 'multipart';
  response: z.ZodType | 'csv' | 'redirect' | 'binary';
}

/**
 * Machine-readable index of every endpoint; docs/api.md is the human version and a test
 * keeps the two in sync.
 */
export const ENDPOINTS = {
  health: { method: 'GET', path: '/api/health', auth: 'public', response: HealthResponse },
  me: { method: 'GET', path: '/api/me', auth: 'officer', response: MeResponse },
  lookups: { method: 'GET', path: '/api/lookups', auth: 'officer', response: LookupsResponse },
  overview: { method: 'GET', path: '/api/overview', auth: 'officer', response: OverviewResponse },
  search: { method: 'GET', path: '/api/search', auth: 'officer', query: SearchQuery, response: SearchResponse },

  eventsList: { method: 'GET', path: '/api/events', auth: 'officer', query: EventsListQuery, response: EventsListResponse },
  eventCreate: { method: 'POST', path: '/api/events', auth: 'officer', body: EventUpsertRequest, response: EventUpsertResponse },
  eventDetail: { method: 'GET', path: '/api/events/:id', auth: 'officer', response: EventDetailResponse },
  eventUpdate: { method: 'PATCH', path: '/api/events/:id', auth: 'officer', body: EventPatchRequest, response: EventUpsertResponse },
  eventMetrics: { method: 'GET', path: '/api/events/:id/metrics', auth: 'officer', response: EventMetricsResponse },
  eventsImport: { method: 'POST', path: '/api/events/import', auth: 'officer', body: 'multipart', response: CsvImportResponse },
  ingestLuma: { method: 'POST', path: '/api/ingest/luma', auth: 'token:ingest:luma', body: LumaIngestRequest, response: LumaIngestResponse },

  requestsList: { method: 'GET', path: '/api/requests', auth: 'officer', query: PurchaseRequestsListQuery, response: PurchaseRequestsListResponse },
  requestCreate: { method: 'POST', path: '/api/requests', auth: 'officer', body: PurchaseRequestCreateRequest, response: PurchaseRequestResponse },
  requestDetail: { method: 'GET', path: '/api/requests/:id', auth: 'officer', response: PurchaseRequestResponse },
  requestUpdate: { method: 'PATCH', path: '/api/requests/:id', auth: 'officer', body: PurchaseRequestUpdateRequest, response: PurchaseRequestResponse },
  requestDelete: { method: 'DELETE', path: '/api/requests/:id', auth: 'officer', response: OkResponse },
  requestSubmit: { method: 'POST', path: '/api/requests/:id/submit', auth: 'officer', response: PurchaseRequestResponse },
  requestApprove: { method: 'POST', path: '/api/requests/:id/approve', auth: 'director', body: PurchaseDecisionRequest, response: PurchaseRequestResponse },
  requestDeny: { method: 'POST', path: '/api/requests/:id/deny', auth: 'director', body: PurchaseDenyRequest, response: PurchaseRequestResponse },
  requestTransition: { method: 'POST', path: '/api/requests/:id/transition', auth: 'director', body: PurchaseTransitionRequest, response: PurchaseRequestResponse },
  requestAttach: { method: 'POST', path: '/api/requests/:id/attachments', auth: 'officer', body: 'multipart', response: AttachmentUploadResponse },

  attachmentDownload: { method: 'GET', path: '/api/attachments/:id', auth: 'officer', response: 'binary' },

  financeSummary: { method: 'GET', path: '/api/finance/summary', auth: 'officer', query: FinanceQuery, response: FinanceSummaryResponse },
  financeReport: { method: 'GET', path: '/api/finance/reports/:report', auth: 'officer', query: FinanceReportQuery, response: FinanceReportResponse },
  budgetsGet: { method: 'GET', path: '/api/finance/budgets', auth: 'officer', query: FinanceQuery, response: BudgetsResponse },
  budgetsPut: { method: 'PUT', path: '/api/finance/budgets', auth: 'director', body: BudgetsUpsertRequest, response: BudgetsResponse },
  periods: { method: 'GET', path: '/api/finance/periods', auth: 'director', response: FiscalPeriodsResponse },
  periodsCreate: { method: 'POST', path: '/api/finance/periods', auth: 'director', body: FiscalYearPeriodsCreateRequest, response: FiscalPeriodsResponse },
  periodClose: { method: 'POST', path: '/api/finance/periods/:id/close', auth: 'director', body: FiscalPeriodCloseRequest, response: FiscalPeriodsResponse },
  accounts: { method: 'GET', path: '/api/finance/accounts', auth: 'director', response: ChartOfAccountsResponse },

  adminUsers: { method: 'GET', path: '/api/admin/users', auth: 'director', response: AdminUsersResponse },
  adminUserCreate: { method: 'POST', path: '/api/admin/users', auth: 'director', body: AdminUserCreateRequest, response: AdminUserResponse },
  adminUserUpdate: { method: 'PATCH', path: '/api/admin/users/:id', auth: 'director', body: AdminUserUpdateRequest, response: AdminUserResponse },
  adminTokens: { method: 'GET', path: '/api/admin/tokens', auth: 'director', response: ServiceTokensResponse },
  adminTokenCreate: { method: 'POST', path: '/api/admin/tokens', auth: 'director', body: ServiceTokenCreateRequest, response: ServiceTokenCreateResponse },
  adminTokenRevoke: { method: 'DELETE', path: '/api/admin/tokens/:id', auth: 'director', response: OkResponse },
  adminAudit: { method: 'GET', path: '/api/admin/audit', auth: 'director', query: AuditLogQuery, response: AuditLogResponse },

  agentJobs: { method: 'GET', path: '/api/agent/jobs', auth: 'token:agent:jobs', query: AgentJobsQuery, response: AgentJobsResponse },
  agentJobClaim: { method: 'POST', path: '/api/agent/jobs/:id/claim', auth: 'token:agent:jobs', response: AgentJobResponse },
  agentJobComplete: { method: 'POST', path: '/api/agent/jobs/:id/complete', auth: 'token:agent:jobs', body: AgentJobCompleteRequest, response: AgentJobResponse },

  authLogin: { method: 'GET', path: '/auth/login', auth: 'public', response: 'redirect' },
  authCallback: { method: 'GET', path: '/auth/callback', auth: 'public', response: 'redirect' },
  authLogout: { method: 'POST', path: '/auth/logout', auth: 'officer', response: 'redirect' },
  authDev: { method: 'GET', path: '/auth/dev', auth: 'public', response: 'redirect' },
} as const satisfies Record<string, EndpointSpec>;
export type EndpointName = keyof typeof ENDPOINTS;
