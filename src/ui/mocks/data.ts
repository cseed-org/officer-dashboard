/**
 * In-browser mock database for the UI while the backend endpoints are being built.
 * Built from the same seed as the real local D1 (scripts/seed/data.ts), so ids such as
 * `event-kickoff` and `req-0001` match. Kept in sessionStorage so a request created on
 * /requests/new is still there on /requests/:id. Remove with the rest of src/ui/mocks/.
 */
import type {
  AdminUser,
  Approval,
  Attachment,
  EventCounts,
  PurchaseRequestDetail,
  ServiceToken,
  UserSummary,
} from '../../core/contracts';
import type { AccountType, FunctionalClass, FundKind, Restriction } from '../../core/domain';
import { eventMetrics } from '../../core/metrics';
import * as seed from '../../../scripts/seed/data';

export interface MockAccount {
  id: string;
  code: string;
  name: string;
  type: AccountType;
  form990Line: string | null;
  budgetable: boolean;
}
export interface MockFund {
  id: string;
  code: string;
  name: string;
  kind: FundKind;
  restriction: Restriction;
  awardedCents: number | null;
  endDate: string | null;
}
export interface MockEvent {
  id: string;
  lumaId: string | null;
  name: string;
  startsAt: string;
  endsAt: string | null;
  location: string | null;
  url: string | null;
  hostId: string | null;
  updatedAt: string;
  counts: { registered: number; checkedIn: number; reflections: number; matchedReflections: number };
}
export type MockRequest = Omit<PurchaseRequestDetail, 'allowedTransitions' | 'canEdit' | 'requester'> & { requesterId: string };
export interface MockLine {
  entryId: string;
  date: string;
  accountId: string;
  fundId: string;
  functionalClass: FunctionalClass | null;
  debitCents: number;
  creditCents: number;
  /** Opening balances are beginning net assets, not activity for the year. */
  opening: boolean;
}
export interface MockDb {
  version: 1;
  seq: number;
  users: AdminUser[];
  accounts: MockAccount[];
  funds: MockFund[];
  events: MockEvent[];
  requests: MockRequest[];
  lines: MockLine[];
  budgets: { id: string; accountId: string; fundId: string | null; amountCents: number; notes: string | null }[];
  tokens: ServiceToken[];
}

const KEY = 'cseed-ui-mock-db-v1';
const iso = (d: string) => (d.length === 10 ? `${d}T12:00:00-07:00` : d);

export function counts(e: MockEvent): EventCounts {
  return eventMetrics(e.counts);
}

function seedDb(): MockDb {
  const created = '2026-07-01T09:00:00-07:00';
  const users: AdminUser[] = seed.users.map((u, i) => ({
    id: u.id,
    discordId: u.discordId,
    name: u.name,
    role: u.role,
    title: u.title,
    email: u.email,
    avatarUrl: null,
    active: true,
    lastLoginAt: i === 3 ? null : `2026-10-${30 - i}T1${i}:15:00-07:00`,
    createdAt: created,
  }));
  users.push({
    id: 'user-former',
    discordId: '100000000000000009',
    name: 'Riley Park',
    role: 'officer',
    title: 'Former Secretary',
    email: null,
    avatarUrl: null,
    active: false,
    lastLoginAt: '2026-06-02T10:00:00-07:00',
    createdAt: '2025-09-01T09:00:00-07:00',
  });

  const accounts: MockAccount[] = seed.accounts.map((a) => ({
    id: seed.acct(a.code),
    code: a.code,
    name: a.name,
    type: a.type,
    form990Line: a.part && a.line ? `${a.part} ${a.line}` : null,
    budgetable: a.budgetable ?? false,
  }));
  const funds: MockFund[] = seed.funds.map((f) => ({
    id: f.id,
    code: f.code,
    name: f.name,
    kind: f.kind,
    restriction: f.restriction,
    awardedCents: f.awardedCents,
    endDate: f.end,
  }));

  const events: MockEvent[] = seed.events.map((e, i) => {
    const checkedIn = Math.round(e.registered * e.checkInPct);
    const matched = Math.round(checkedIn * e.reflectionPct);
    // One event carries reflections from people who never checked in, so the UI's data-problem flag shows.
    const unmatched = e.id === 'event-buildspace-demo' ? 3 : 0;
    return {
      id: e.id,
      lumaId: e.lumaId,
      name: e.name,
      startsAt: e.startsAt,
      endsAt: e.endsAt,
      location: e.location,
      url: `https://lu.ma/${e.lumaId}`,
      hostId: e.host,
      updatedAt: `2026-10-${String(20 + (i % 9)).padStart(2, '0')}T09:00:00-07:00`,
      counts: { registered: e.registered, checkedIn, reflections: matched + unmatched, matchedReflections: matched },
    };
  });

  const userSummary = (id: string): UserSummary => {
    const u = users.find((x) => x.id === id)!;
    return { id: u.id, name: u.name, avatarUrl: null };
  };
  const director = userSummary('user-director');
  const acctRef = (code: string) => {
    const a = accounts.find((x) => x.code === code)!;
    return { id: a.id, code: a.code, name: a.name };
  };
  const fundRef = (id: string) => {
    const f = funds.find((x) => x.id === id)!;
    return { id: f.id, code: f.code, name: f.name };
  };

  const requests: MockRequest[] = seed.requests.map((r) => {
    const total = r.quantity * r.unitCostCents + r.taxShipCents;
    const approvals: Approval[] = r.decision
      ? [{ id: `appr-${r.id}`, director, decision: r.decision.decision, comment: r.decision.comment, createdAt: iso(r.decidedAt!) }]
      : [];
    const attachments: Attachment[] =
      r.status === 'reimbursed' || r.status === 'fulfilled' || r.status === 'submitted'
        ? [
            {
              id: `att-${r.id}`,
              kind: 'receipt',
              filename: `${r.vendor.toLowerCase().replace(/[^a-z0-9]+/g, '-')}-receipt.pdf`,
              mimeType: 'application/pdf',
              sizeBytes: 184_320,
              storage: 'local',
              url: null,
              uploadedBy: userSummary(r.requester),
              createdAt: iso(r.submittedAt!),
            },
          ]
        : [];
    const ev = r.event ? events.find((e) => e.id === r.event)! : null;
    const settled = r.status === 'reimbursed' || r.status === 'fulfilled';
    return {
      id: r.id,
      number: r.number,
      title: r.title,
      vendor: r.vendor,
      type: r.type,
      status: r.status,
      totalCents: total,
      requesterId: r.requester,
      expenseAccount: acctRef(r.account),
      fund: fundRef(r.fund),
      eventId: r.event,
      submittedAt: r.submittedAt ? iso(r.submittedAt) : null,
      updatedAt: iso(r.decidedAt ?? r.submittedAt ?? '2026-10-30'),
      itemUrl: r.itemUrl,
      quantity: r.quantity,
      unitCostCents: r.unitCostCents,
      taxShipCents: r.taxShipCents,
      actualTotalCents: settled ? (r.id === 'req-0001' ? 40_500 : 18_640) : null,
      functionalClass: r.functionalClass,
      justification: r.justification,
      neededBy: null,
      event: ev ? { id: ev.id, name: ev.name } : null,
      decidedAt: r.decidedAt ? iso(r.decidedAt) : null,
      createdAt: iso(r.submittedAt ?? '2026-10-30'),
      approvals,
      attachments,
      journalEntryIds: seed.journal.filter((j) => j.request === r.id).map((j) => j.id),
    };
  });

  const lines: MockLine[] = seed.journal.flatMap((j) =>
    j.lines.map((l) => ({
      entryId: j.id,
      date: j.date,
      accountId: seed.acct(l.account),
      fundId: l.fund,
      functionalClass: l.fc ?? null,
      debitCents: l.debit ?? 0,
      creditCents: l.credit ?? 0,
      opening: j.source === 'opening_balance',
    })),
  );

  const budgets = seed.budgets.map((b, i) => ({ id: `budget-${i + 1}`, accountId: seed.acct(b.account), fundId: b.fund, amountCents: b.amountCents, notes: b.notes }));

  const tokens: ServiceToken[] = [
    {
      id: 'tok-luma',
      name: 'Luma scraper',
      prefix: 'cst_lu4x',
      scopes: ['ingest:luma'],
      createdBy: director,
      createdAt: '2026-10-01T10:00:00-07:00',
      lastUsedAt: '2026-10-30T06:00:00-07:00',
      expiresAt: '2026-12-30T10:00:00-08:00',
      revokedAt: null,
    },
    {
      id: 'tok-old',
      name: 'Test agent (old)',
      prefix: 'cst_9fz2',
      scopes: ['agent:jobs'],
      createdBy: director,
      createdAt: '2026-08-10T10:00:00-07:00',
      lastUsedAt: null,
      expiresAt: null,
      revokedAt: '2026-09-01T10:00:00-07:00',
    },
  ];

  return { version: 1, seq: 100, users, accounts, funds, events, requests, lines, budgets, tokens };
}

let cache: MockDb | null = null;

export function db(): MockDb {
  if (cache) return cache;
  try {
    const raw = sessionStorage.getItem(KEY);
    if (raw) {
      const parsed = JSON.parse(raw) as MockDb;
      if (parsed.version === 1) return (cache = parsed);
    }
  } catch {
    /* storage blocked or corrupt: start fresh */
  }
  return (cache = seedDb());
}

export function save(): void {
  if (!cache) return;
  try {
    sessionStorage.setItem(KEY, JSON.stringify(cache));
  } catch {
    /* in-memory only */
  }
}

export function nextId(prefix: string): string {
  const d = db();
  d.seq += 1;
  return `${prefix}-${d.seq}`;
}

export function summary(id: string): UserSummary {
  const u = db().users.find((x) => x.id === id);
  return u ? { id: u.id, name: u.name, avatarUrl: u.avatarUrl } : { id, name: 'Unknown', avatarUrl: null };
}
