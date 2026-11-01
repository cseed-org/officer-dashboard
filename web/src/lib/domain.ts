/**
 * Domain vocabulary shared by the database schema, the API contract and the UI.
 * Framework-free: no Astro, Cloudflare or Drizzle imports in src/core.
 */

export const ROLES = ['director', 'officer'] as const;
export type Role = (typeof ROLES)[number];

export const ACCOUNT_TYPES = ['asset', 'liability', 'net_asset', 'revenue', 'expense'] as const;
export type AccountType = (typeof ACCOUNT_TYPES)[number];

export const NORMAL_BALANCES = ['debit', 'credit'] as const;
export type NormalBalance = (typeof NORMAL_BALANCES)[number];

/** Form 990 parts an account can map to: VIII revenue, IX functional expenses, X balance sheet. */
export const FORM990_PARTS = ['VIII', 'IX', 'X'] as const;
export type Form990Part = (typeof FORM990_PARTS)[number];

export const FUND_KINDS = ['general', 'grant', 'sponsor', 'other'] as const;
export type FundKind = (typeof FUND_KINDS)[number];

/** ASC 958 net asset classes. */
export const RESTRICTIONS = ['without_donor', 'with_donor'] as const;
export type Restriction = (typeof RESTRICTIONS)[number];

/** Form 990 Part IX functional expense columns B/C/D. */
export const FUNCTIONAL_CLASSES = ['program', 'mgmt_general', 'fundraising'] as const;
export type FunctionalClass = (typeof FUNCTIONAL_CLASSES)[number];

export const PURCHASE_TYPES = ['reimbursement', 'club_purchase'] as const;
export type PurchaseType = (typeof PURCHASE_TYPES)[number];

export const PURCHASE_STATUSES = [
  'draft',
  'submitted',
  'approved',
  'denied',
  'fulfilling',
  'fulfilled',
  'reimbursed',
  'closed',
  'cancelled',
] as const;
export type PurchaseStatus = (typeof PURCHASE_STATUSES)[number];

export const APPROVAL_DECISIONS = ['approved', 'denied'] as const;
export type ApprovalDecision = (typeof APPROVAL_DECISIONS)[number];

export const ATTACHMENT_KINDS = ['receipt', 'invoice', 'quote', 'other'] as const;
export type AttachmentKind = (typeof ATTACHMENT_KINDS)[number];

export const STORAGE_BACKENDS = ['drive', 'local'] as const;
export type StorageBackend = (typeof STORAGE_BACKENDS)[number];

/** `held`: the agent reported an over-tolerance settlement; terminal for the agent, a director settles the request. */
export const FULFILLMENT_JOB_STATUSES = ['queued', 'claimed', 'held', 'done', 'failed', 'cancelled'] as const;
export type FulfillmentJobStatus = (typeof FULFILLMENT_JOB_STATUSES)[number];

export const JOURNAL_STATUSES = ['draft', 'posted'] as const;
export type JournalStatus = (typeof JOURNAL_STATUSES)[number];

export const JOURNAL_SOURCES = [
  'manual',
  'purchase_request',
  'contribution',
  'reversal',
  'release',
  'import',
  'opening_balance',
] as const;
export type JournalSource = (typeof JOURNAL_SOURCES)[number];

export const FISCAL_PERIOD_STATUSES = ['open', 'closed', 'locked'] as const;
export type FiscalPeriodStatus = (typeof FISCAL_PERIOD_STATUSES)[number];

export const DONOR_TYPES = ['individual', 'organization', 'foundation', 'government', 'corporate'] as const;
export type DonorType = (typeof DONOR_TYPES)[number];

export const CONTRIBUTION_KINDS = ['cash', 'check', 'online', 'in_kind', 'stock', 'other'] as const;
export type ContributionKind = (typeof CONTRIBUTION_KINDS)[number];

/** Scopes a director can grant to a service token (agents / scrapers). */
export const SERVICE_TOKEN_SCOPES = ['ingest:luma', 'agent:jobs'] as const;
export type ServiceTokenScope = (typeof SERVICE_TOKEN_SCOPES)[number];

export const CSV_IMPORT_KINDS = ['luma_guests', 'reflections'] as const;
export type CsvImportKind = (typeof CSV_IMPORT_KINDS)[number];

export const FINANCE_REPORTS = [
  'trial-balance',
  'statement-of-activities',
  'statement-of-financial-position',
  'statement-of-functional-expenses',
  'budget-vs-actual',
  'fund-balances',
  'form-990',
] as const;
export type FinanceReport = (typeof FINANCE_REPORTS)[number];

/** Reports an officer may read; directors may read all of FINANCE_REPORTS. */
export const OFFICER_FINANCE_REPORTS = ['budget-vs-actual', 'fund-balances'] as const satisfies readonly FinanceReport[];
