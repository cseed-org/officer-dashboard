/**
 * Drizzle schema for D1 (SQLite). The same schema runs on better-sqlite3 for the
 * future Raspberry Pi / Node adapter.
 *
 * Conventions
 * - ids are text (crypto.randomUUID()), timestamps are integer unix milliseconds,
 *   calendar dates are ISO `YYYY-MM-DD` text.
 * - Money is always integer cents (`*_cents`).
 * - Enum columns carry a CHECK constraint generated from the lists in src/core/domain.ts.
 * - Triggers that Drizzle can't express (immutability, balanced posting, period locks)
 *   live in migrations/0001_integrity_triggers.sql.
 */
import { sql, type SQL } from 'drizzle-orm';
import {
  blob,
  check,
  index,
  integer,
  sqliteTable,
  text,
  uniqueIndex,
  type AnySQLiteColumn,
} from 'drizzle-orm/sqlite-core';
import {
  ACCOUNT_TYPES,
  APPROVAL_DECISIONS,
  ATTACHMENT_KINDS,
  CONTRIBUTION_KINDS,
  DONOR_TYPES,
  FISCAL_PERIOD_STATUSES,
  FORM990_PARTS,
  FULFILLMENT_JOB_STATUSES,
  FUNCTIONAL_CLASSES,
  FUND_KINDS,
  JOURNAL_SOURCES,
  JOURNAL_STATUSES,
  NORMAL_BALANCES,
  PURCHASE_STATUSES,
  PURCHASE_TYPES,
  RESTRICTIONS,
  ROLES,
  STORAGE_BACKENDS,
} from '../../core/domain';

/** `col IN ('a','b')` for an enum CHECK. Values are compile-time constants, never user input. */
const oneOf = (column: AnySQLiteColumn, values: readonly string[]): SQL =>
  sql`${column} IN (${sql.raw(values.map((v) => `'${v}'`).join(', '))})`;

/**
 * Whole cents only. INTEGER affinity alone stores 0.5 as REAL, so every money (and count)
 * column also checks the storage class. Nullable columns pass when NULL.
 */
const isInteger = (column: AnySQLiteColumn): SQL => sql`typeof(${column}) = 'integer'`;
const isIntegerOrNull = (column: AnySQLiteColumn): SQL => sql`${column} IS NULL OR typeof(${column}) = 'integer'`;

const id = () => text('id').primaryKey();
const createdAt = () =>
  integer('created_at', { mode: 'timestamp_ms' })
    .notNull()
    .default(sql`(unixepoch('subsec') * 1000)`);
const updatedAt = () =>
  integer('updated_at', { mode: 'timestamp_ms' })
    .notNull()
    .default(sql`(unixepoch('subsec') * 1000)`);
const bool = (name: string) => integer(name, { mode: 'boolean' });

// ───────────────────────────── People ─────────────────────────────

export const users = sqliteTable(
  'users',
  {
    id: id(),
    discordId: text('discord_id').notNull().unique(),
    name: text('name').notNull(),
    email: text('email'),
    avatarUrl: text('avatar_url'),
    role: text('role', { enum: ROLES }).notNull().default('officer'),
    active: bool('active').notNull().default(true),
    /** Free text such as "VP Events"; officer history may later sync from the membership API. */
    title: text('title'),
    lastLoginAt: integer('last_login_at', { mode: 'timestamp_ms' }),
    createdAt: createdAt(),
    updatedAt: updatedAt(),
  },
  (t) => [check('users_role_check', oneOf(t.role, ROLES))],
);

export const sessions = sqliteTable(
  'sessions',
  {
    /** sha-256 hex of the session token; the raw token only lives in the cookie. */
    id: id(),
    userId: text('user_id')
      .notNull()
      .references(() => users.id, { onDelete: 'cascade' }),
    expiresAt: integer('expires_at', { mode: 'timestamp_ms' }).notNull(),
    createdAt: createdAt(),
    userAgent: text('user_agent'),
  },
  (t) => [index('sessions_user_idx').on(t.userId)],
);

/** Append-only (UPDATE/DELETE blocked by trigger). */
export const auditLog = sqliteTable(
  'audit_log',
  {
    id: id(),
    actorUserId: text('actor_user_id').references(() => users.id),
    actorTokenId: text('actor_token_id').references(() => serviceTokens.id),
    action: text('action').notNull(),
    entityType: text('entity_type').notNull(),
    entityId: text('entity_id'),
    beforeJson: text('before_json'),
    afterJson: text('after_json'),
    createdAt: createdAt(),
  },
  (t) => [
    index('audit_entity_idx').on(t.entityType, t.entityId),
    index('audit_created_idx').on(t.createdAt),
  ],
);

export const serviceTokens = sqliteTable(
  'service_tokens',
  {
    id: id(),
    name: text('name').notNull(),
    /** sha-256 hex of the token; plaintext is shown once at creation. */
    tokenHash: text('token_hash').notNull().unique(),
    /** First characters of the plaintext token, for recognition in the UI. */
    prefix: text('prefix').notNull(),
    /** JSON array of ServiceTokenScope. */
    scopes: text('scopes', { mode: 'json' }).$type<string[]>().notNull(),
    createdBy: text('created_by')
      .notNull()
      .references(() => users.id),
    lastUsedAt: integer('last_used_at', { mode: 'timestamp_ms' }),
    expiresAt: integer('expires_at', { mode: 'timestamp_ms' }),
    revokedAt: integer('revoked_at', { mode: 'timestamp_ms' }),
    createdAt: createdAt(),
  },
  (t) => [check('service_tokens_scopes_json', sql`json_valid(${t.scopes})`)],
);

// ───────────────────────────── Accounting ─────────────────────────────

export const accounts = sqliteTable(
  'accounts',
  {
    id: id(),
    code: text('code').notNull().unique(),
    name: text('name').notNull(),
    type: text('type', { enum: ACCOUNT_TYPES }).notNull(),
    normalBalance: text('normal_balance', { enum: NORMAL_BALANCES }).notNull(),
    /** Net asset class for net_asset accounts. */
    restriction: text('restriction', { enum: RESTRICTIONS }),
    form990Part: text('form990_part', { enum: FORM990_PARTS }),
    /** e.g. "1f" (Part VIII line 1f) or "24a" (Part IX line 24a). */
    form990Line: text('form990_line'),
    form990Label: text('form990_label'),
    parentId: text('parent_id').references((): AnySQLiteColumn => accounts.id),
    /** Expense accounts officers may pick on a purchase request. */
    budgetable: bool('budgetable').notNull().default(false),
    active: bool('active').notNull().default(true),
    description: text('description'),
    createdAt: createdAt(),
  },
  (t) => [
    check('accounts_type_check', oneOf(t.type, ACCOUNT_TYPES)),
    check('accounts_normal_balance_check', oneOf(t.normalBalance, NORMAL_BALANCES)),
    check('accounts_restriction_check', sql`${t.restriction} IS NULL OR ${oneOf(t.restriction, RESTRICTIONS)}`),
    check('accounts_form990_part_check', sql`${t.form990Part} IS NULL OR ${oneOf(t.form990Part, FORM990_PARTS)}`),
    check(
      'accounts_form990_line_pair',
      sql`(${t.form990Part} IS NULL) = (${t.form990Line} IS NULL)`,
    ),
  ],
);

export const donors = sqliteTable(
  'donors',
  {
    id: id(),
    name: text('name').notNull(),
    type: text('type', { enum: DONOR_TYPES }).notNull(),
    email: text('email'),
    address: text('address'),
    notes: text('notes'),
    createdAt: createdAt(),
  },
  (t) => [check('donors_type_check', oneOf(t.type, DONOR_TYPES))],
);

export const funds = sqliteTable(
  'funds',
  {
    id: id(),
    code: text('code').notNull().unique(),
    name: text('name').notNull(),
    kind: text('kind', { enum: FUND_KINDS }).notNull(),
    restriction: text('restriction', { enum: RESTRICTIONS }).notNull(),
    purpose: text('purpose'),
    donorId: text('donor_id').references(() => donors.id),
    startDate: text('start_date'),
    endDate: text('end_date'),
    amountAwardedCents: integer('amount_awarded_cents'),
    /** Set when the restriction has been fully satisfied and released. */
    releasedAt: integer('released_at', { mode: 'timestamp_ms' }),
    active: bool('active').notNull().default(true),
    createdAt: createdAt(),
  },
  (t) => [
    check('funds_kind_check', oneOf(t.kind, FUND_KINDS)),
    check('funds_restriction_check', oneOf(t.restriction, RESTRICTIONS)),
    check('funds_amount_nonneg', sql`${t.amountAwardedCents} IS NULL OR ${t.amountAwardedCents} >= 0`),
    check('funds_amount_int', isIntegerOrNull(t.amountAwardedCents)),
    check('funds_dates', sql`${t.endDate} IS NULL OR ${t.startDate} IS NULL OR ${t.endDate} >= ${t.startDate}`),
  ],
);

export const fiscalPeriods = sqliteTable(
  'fiscal_periods',
  {
    id: id(),
    fiscalYear: integer('fiscal_year').notNull(),
    name: text('name').notNull(),
    startDate: text('start_date').notNull(),
    endDate: text('end_date').notNull(),
    status: text('status', { enum: FISCAL_PERIOD_STATUSES }).notNull().default('open'),
    closedAt: integer('closed_at', { mode: 'timestamp_ms' }),
    closedBy: text('closed_by').references(() => users.id),
    createdAt: createdAt(),
  },
  (t) => [
    check('fiscal_periods_status_check', oneOf(t.status, FISCAL_PERIOD_STATUSES)),
    check('fiscal_periods_dates', sql`${t.endDate} >= ${t.startDate}`),
    uniqueIndex('fiscal_periods_range_uq').on(t.startDate, t.endDate),
  ],
);

export const journalEntries = sqliteTable(
  'journal_entries',
  {
    id: id(),
    entryDate: text('entry_date').notNull(),
    memo: text('memo').notNull(),
    status: text('status', { enum: JOURNAL_STATUSES }).notNull().default('draft'),
    source: text('source', { enum: JOURNAL_SOURCES }).notNull().default('manual'),
    purchaseRequestId: text('purchase_request_id').references(() => purchaseRequests.id),
    contributionId: text('contribution_id'),
    settlementKey: text('settlement_key'),
    /** Set on a reversing entry; corrections never edit a posted entry. */
    reversesEntryId: text('reverses_entry_id').references((): AnySQLiteColumn => journalEntries.id),
    createdBy: text('created_by').references(() => users.id),
    postedBy: text('posted_by').references(() => users.id),
    postedAt: integer('posted_at', { mode: 'timestamp_ms' }),
    createdAt: createdAt(),
  },
  (t) => [
    check('journal_entries_status_check', oneOf(t.status, JOURNAL_STATUSES)),
    check('journal_entries_source_check', oneOf(t.source, JOURNAL_SOURCES)),
    check('journal_entries_posted_at', sql`(${t.status} = 'posted') = (${t.postedAt} IS NOT NULL)`),
    index('journal_entries_date_idx').on(t.entryDate),
    index('journal_entries_request_idx').on(t.purchaseRequestId),
  ],
);

export const journalLines = sqliteTable(
  'journal_lines',
  {
    id: id(),
    entryId: text('entry_id')
      .notNull()
      .references(() => journalEntries.id),
    lineNo: integer('line_no').notNull(),
    accountId: text('account_id')
      .notNull()
      .references(() => accounts.id),
    fundId: text('fund_id')
      .notNull()
      .references(() => funds.id),
    /** Required on expense lines by the posting engine (Statement of Functional Expenses). */
    functionalClass: text('functional_class', { enum: FUNCTIONAL_CLASSES }),
    debitCents: integer('debit_cents').notNull().default(0),
    creditCents: integer('credit_cents').notNull().default(0),
    memo: text('memo'),
  },
  (t) => [
    check('journal_lines_debit_nonneg', sql`${t.debitCents} >= 0`),
    check('journal_lines_debit_int', isInteger(t.debitCents)),
    check('journal_lines_credit_int', isInteger(t.creditCents)),
    check('journal_lines_credit_nonneg', sql`${t.creditCents} >= 0`),
    check('journal_lines_one_side', sql`(${t.debitCents} = 0) <> (${t.creditCents} = 0)`),
    check(
      'journal_lines_functional_class_check',
      sql`${t.functionalClass} IS NULL OR ${oneOf(t.functionalClass, FUNCTIONAL_CLASSES)}`,
    ),
    uniqueIndex('journal_lines_entry_line_uq').on(t.entryId, t.lineNo),
    index('journal_lines_account_idx').on(t.accountId),
    index('journal_lines_fund_idx').on(t.fundId),
  ],
);

export const contributions = sqliteTable(
  'contributions',
  {
    id: id(),
    donorId: text('donor_id')
      .notNull()
      .references(() => donors.id),
    fundId: text('fund_id')
      .notNull()
      .references(() => funds.id),
    receivedDate: text('received_date').notNull(),
    kind: text('kind', { enum: CONTRIBUTION_KINDS }).notNull(),
    /** Cash amount, or fair value for in-kind gifts. */
    amountCents: integer('amount_cents').notNull(),
    inKindDescription: text('in_kind_description'),
    /** Value of goods/services given back to the donor (quid pro quo). */
    quidProQuoCents: integer('quid_pro_quo_cents').notNull().default(0),
    goodsServicesDescription: text('goods_services_description'),
    /** Written acknowledgment required (single gift ≥ $250). Set via core/contributions. */
    acknowledgmentRequired: bool('acknowledgment_required').notNull().default(false),
    acknowledgedAt: integer('acknowledged_at', { mode: 'timestamp_ms' }),
    journalEntryId: text('journal_entry_id').references(() => journalEntries.id),
    createdAt: createdAt(),
  },
  (t) => [
    check('contributions_kind_check', oneOf(t.kind, CONTRIBUTION_KINDS)),
    check('contributions_amount_pos', sql`${t.amountCents} > 0`),
    check('contributions_amount_int', isInteger(t.amountCents)),
    check('contributions_qpq_int', isInteger(t.quidProQuoCents)),
    check(
      'contributions_qpq_range',
      sql`${t.quidProQuoCents} >= 0 AND ${t.quidProQuoCents} <= ${t.amountCents}`,
    ),
    check(
      'contributions_in_kind_desc',
      sql`${t.kind} <> 'in_kind' OR ${t.inKindDescription} IS NOT NULL`,
    ),
    index('contributions_donor_idx').on(t.donorId),
  ],
);

export const budgets = sqliteTable(
  'budgets',
  {
    id: id(),
    fiscalYear: integer('fiscal_year').notNull(),
    accountId: text('account_id')
      .notNull()
      .references(() => accounts.id),
    /** NULL = all funds. */
    fundId: text('fund_id').references(() => funds.id),
    amountCents: integer('amount_cents').notNull(),
    notes: text('notes'),
    createdAt: createdAt(),
    updatedAt: updatedAt(),
  },
  (t) => [
    check('budgets_amount_nonneg', sql`${t.amountCents} >= 0`),
    check('budgets_amount_int', isInteger(t.amountCents)),
    // Unique (fiscal_year, account_id, coalesce(fund_id, '')) is an expression index in
    // migrations/0001_integrity_triggers.sql (drizzle-kit can't emit it for SQLite).
    index('budgets_fy_idx').on(t.fiscalYear),
  ],
);

// ───────────────────────────── Events ─────────────────────────────

export const events = sqliteTable(
  'events',
  {
    id: id(),
    lumaId: text('luma_id').unique(),
    name: text('name').notNull(),
    /** ISO 8601 datetime with offset. */
    startsAt: text('starts_at').notNull(),
    endsAt: text('ends_at'),
    location: text('location'),
    url: text('url'),
    hostUserId: text('host_user_id').references(() => users.id),
    createdAt: createdAt(),
    updatedAt: updatedAt(),
  },
  (t) => [index('events_starts_idx').on(t.startsAt)],
);

export const eventGuests = sqliteTable(
  'event_guests',
  {
    id: id(),
    eventId: text('event_id')
      .notNull()
      .references(() => events.id, { onDelete: 'cascade' }),
    email: text('email').notNull(),
    name: text('name'),
    lumaGuestId: text('luma_guest_id'),
    /** Luma approval status, e.g. approved / waitlist / declined. */
    approvalStatus: text('approval_status'),
    registeredAt: integer('registered_at', { mode: 'timestamp_ms' }),
    checkedInAt: integer('checked_in_at', { mode: 'timestamp_ms' }),
    createdAt: createdAt(),
  },
  (t) => [
    uniqueIndex('event_guests_event_email_uq').on(t.eventId, t.email),
    check('event_guests_email_lower', sql`${t.email} = lower(${t.email})`),
  ],
);

export const reflections = sqliteTable(
  'reflections',
  {
    id: id(),
    eventId: text('event_id')
      .notNull()
      .references(() => events.id, { onDelete: 'cascade' }),
    email: text('email').notNull(),
    submittedAt: integer('submitted_at', { mode: 'timestamp_ms' }).notNull(),
    /** Optional 1–5 rating if the survey has one. */
    rating: integer('rating'),
    createdAt: createdAt(),
  },
  (t) => [
    uniqueIndex('reflections_event_email_uq').on(t.eventId, t.email),
    check('reflections_email_lower', sql`${t.email} = lower(${t.email})`),
    check('reflections_rating_range', sql`${t.rating} IS NULL OR ${t.rating} BETWEEN 1 AND 5`),
  ],
);

// ───────────────────────────── Purchases ─────────────────────────────

export const purchaseRequests = sqliteTable(
  'purchase_requests',
  {
    id: id(),
    /** Human-friendly number shown in the UI: fiscal year + 4-digit sequence, e.g. 2027-0007 (allocated in requestCreate). */
    number: text('number').notNull().unique(),
    requesterId: text('requester_id')
      .notNull()
      .references(() => users.id),
    type: text('type', { enum: PURCHASE_TYPES }).notNull(),
    title: text('title').notNull(),
    vendor: text('vendor').notNull(),
    itemUrl: text('item_url'),
    quantity: integer('quantity').notNull().default(1),
    unitCostCents: integer('unit_cost_cents').notNull(),
    taxShipCents: integer('tax_ship_cents').notNull().default(0),
    /** Approved estimate: quantity × unit cost + tax/shipping. Encumbered while approved. */
    totalCents: integer('total_cents').notNull(),
    /**
     * What was actually paid, recorded at settlement (fulfilled / reimbursed). The posting
     * engine books this amount, not totalCents, and releases the encumbrance.
     */
    actualTotalCents: integer('actual_total_cents'),
    /** Expense account (budget category) the purchase will be charged to. */
    expenseAccountId: text('expense_account_id')
      .notNull()
      .references(() => accounts.id),
    fundId: text('fund_id')
      .notNull()
      .references(() => funds.id),
    eventId: text('event_id').references(() => events.id),
    functionalClass: text('functional_class', { enum: FUNCTIONAL_CLASSES }).notNull().default('program'),
    justification: text('justification').notNull(),
    neededBy: text('needed_by'),
    status: text('status', { enum: PURCHASE_STATUSES }).notNull().default('draft'),
    submittedAt: integer('submitted_at', { mode: 'timestamp_ms' }),
    decidedAt: integer('decided_at', { mode: 'timestamp_ms' }),
    createdAt: createdAt(),
    updatedAt: updatedAt(),
  },
  (t) => [
    check('purchase_requests_type_check', oneOf(t.type, PURCHASE_TYPES)),
    check('purchase_requests_status_check', oneOf(t.status, PURCHASE_STATUSES)),
    check('purchase_requests_fc_check', oneOf(t.functionalClass, FUNCTIONAL_CLASSES)),
    check('purchase_requests_qty_pos', sql`${t.quantity} > 0`),
    check('purchase_requests_qty_int', isInteger(t.quantity)),
    check('purchase_requests_unit_int', isInteger(t.unitCostCents)),
    check('purchase_requests_taxship_int', isInteger(t.taxShipCents)),
    check('purchase_requests_total_int', isInteger(t.totalCents)),
    check('purchase_requests_actual_int', isIntegerOrNull(t.actualTotalCents)),
    check('purchase_requests_actual_nonneg', sql`${t.actualTotalCents} IS NULL OR ${t.actualTotalCents} >= 0`),
    check('purchase_requests_unit_nonneg', sql`${t.unitCostCents} >= 0`),
    check('purchase_requests_taxship_nonneg', sql`${t.taxShipCents} >= 0`),
    check(
      'purchase_requests_total',
      sql`${t.totalCents} = ${t.quantity} * ${t.unitCostCents} + ${t.taxShipCents}`,
    ),
    index('purchase_requests_requester_idx').on(t.requesterId),
    index('purchase_requests_status_idx').on(t.status),
  ],
);

export const approvals = sqliteTable(
  'approvals',
  {
    id: id(),
    requestId: text('request_id')
      .notNull()
      .references(() => purchaseRequests.id),
    directorId: text('director_id')
      .notNull()
      .references(() => users.id),
    decision: text('decision', { enum: APPROVAL_DECISIONS }).notNull(),
    comment: text('comment'),
    createdAt: createdAt(),
  },
  (t) => [
    check('approvals_decision_check', oneOf(t.decision, APPROVAL_DECISIONS)),
    index('approvals_request_idx').on(t.requestId),
  ],
);

export const attachments = sqliteTable(
  'attachments',
  {
    id: id(),
    requestId: text('request_id').references(() => purchaseRequests.id),
    journalEntryId: text('journal_entry_id').references(() => journalEntries.id),
    kind: text('kind', { enum: ATTACHMENT_KINDS }).notNull(),
    filename: text('filename').notNull(),
    mimeType: text('mime_type').notNull(),
    sizeBytes: integer('size_bytes').notNull(),
    storage: text('storage', { enum: STORAGE_BACKENDS }).notNull(),
    /** Drive fileId, or the local stub key. */
    storageKey: text('storage_key').notNull(),
    webViewLink: text('web_view_link'),
    uploadedBy: text('uploaded_by')
      .notNull()
      .references(() => users.id),
    createdAt: createdAt(),
  },
  (t) => [
    check('attachments_kind_check', oneOf(t.kind, ATTACHMENT_KINDS)),
    check('attachments_storage_check', oneOf(t.storage, STORAGE_BACKENDS)),
    check('attachments_size_nonneg', sql`${t.sizeBytes} >= 0`),
    check('attachments_size_int', isInteger(t.sizeBytes)),
    check('attachments_owner', sql`${t.requestId} IS NOT NULL OR ${t.journalEntryId} IS NOT NULL`),
    index('attachments_request_idx').on(t.requestId),
  ],
);

export const fulfillmentJobs = sqliteTable(
  'fulfillment_jobs',
  {
    id: id(),
    requestId: text('request_id')
      .notNull()
      .references(() => purchaseRequests.id),
    status: text('status', { enum: FULFILLMENT_JOB_STATUSES }).notNull().default('queued'),
    claimedByTokenId: text('claimed_by_token_id').references(() => serviceTokens.id),
    claimedAt: integer('claimed_at', { mode: 'timestamp_ms' }),
    attempts: integer('attempts').notNull().default(0),
    lastError: text('last_error'),
    resultJson: text('result_json'),
    payloadJson: text('payload_json'),
    createdAt: createdAt(),
    updatedAt: updatedAt(),
  },
  (t) => [
    check('fulfillment_jobs_status_check', oneOf(t.status, FULFILLMENT_JOB_STATUSES)),
    check('fulfillment_jobs_attempts_nonneg', sql`${t.attempts} >= 0`),
    index('fulfillment_jobs_status_idx').on(t.status),
  ],
);

export type UserRow = typeof users.$inferSelect;
export type SessionRow = typeof sessions.$inferSelect;

/** Internal assertion used only inside atomic D1 batches; never retained. */
export const mutationGuards = sqliteTable('mutation_guards', {
 id: text('id').primaryKey(), ok: integer('ok').notNull(),
}, t => [check('mutation_guard_ok', sql`${t.ok} = 1`)]);

/** Development receipt bytes, persisted inside local D1's filesystem-backed SQLite DB. */
export const localReceiptFiles = sqliteTable('local_receipt_files', {
 id: text('id').primaryKey(), mimeType: text('mime_type').notNull(), body: blob('body', { mode: 'buffer' }).notNull(),
});
