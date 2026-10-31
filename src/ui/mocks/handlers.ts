/**
 * Mock implementations of the API endpoints the pages call, typed by the contract. Only
 * loaded when UI_MOCKS is on (./switch.ts). Rules mirror docs/api.md ("Rules the backend
 * enforces") closely enough to drive the UI, but this is not the backend.
 *
 * Add `?mock=empty`, `?mock=error` or `?mock=loading` to a page URL to see its empty,
 * error and loading states.
 */
import { flattenError, type z } from 'zod';
import type {
  AdminUser,
  EndpointName,
  EventListItem,
  FinanceReportResponse,
  MeResponse,
  PurchaseRequestDetail,
  PurchaseRequestListItem,
} from '../../core/contracts';
import { ENDPOINTS } from '../../core/contracts';
import type { FinanceReport, PurchaseStatus } from '../../core/domain';
import { OFFICER_FINANCE_REPORTS } from '../../core/domain';
import { averageRate, eventMetrics } from '../../core/metrics';
import { nextStatuses } from '../../core/purchase-lifecycle';
import { fiscalYearFor, fiscalYearRange } from '../../core/fiscal';
import { ApiCallError, type CallOptions } from '../api/client';
import { counts, db, nextId, save, summary, type MockDb, type MockEvent, type MockRequest } from './data';

type Me = MeResponse['user'];
type Opts = CallOptions<EndpointName> & { params?: Record<string, string>; query?: Record<string, unknown>; body?: unknown };
type Handler = (o: Opts, me: Me) => unknown | Promise<unknown>;

let mePromise: Promise<Me> | null = null;
function currentUser(): Promise<Me> {
  return (mePromise ??= fetch('/api/me', { credentials: 'same-origin', headers: { accept: 'application/json' } }).then(async (r) => {
    if (!r.ok) {
      mePromise = null;
      throw new ApiCallError(r.status, r.status === 401 ? 'unauthenticated' : 'forbidden', 'Sign in first');
    }
    return ((await r.json()) as MeResponse).user;
  }));
}

const scenario = (): string | null => {
  try {
    return new URLSearchParams(location.search).get('mock');
  } catch {
    return null;
  }
};
const isEmpty = () => scenario() === 'empty';
const now = () => new Date();
const nowIso = () => {
  const d = now();
  const off = -d.getTimezoneOffset();
  const sign = off >= 0 ? '+' : '-';
  const pad = (n: number) => String(Math.floor(Math.abs(n))).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}${sign}${pad(off / 60)}:${pad(off % 60)}`;
};
const today = () => nowIso().slice(0, 10);

const fail = (status: number, code: ConstructorParameters<typeof ApiCallError>[1], message: string, details?: unknown): never => {
  throw new ApiCallError(status, code, message, details);
};
const parseBody = <S extends z.ZodType>(schema: S, body: unknown): z.output<S> => {
  const r = schema.safeParse(body);
  if (!r.success) return fail(400, 'bad_request', 'Some fields need attention.', flattenError(r.error));
  return r.data;
};
const requireDirector = (me: Me) => {
  if (me.role !== 'director') fail(403, 'forbidden', 'Requires the director role');
};

// ───────────── events ─────────────

function eventItem(e: MockEvent): EventListItem {
  return {
    id: e.id,
    lumaId: e.lumaId,
    name: e.name,
    startsAt: e.startsAt,
    location: e.location,
    host: e.hostId ? summary(e.hostId) : null,
    metrics: counts(e),
  };
}
const byStartDesc = (a: MockEvent, b: MockEvent) => b.startsAt.localeCompare(a.startsAt);
const isUpcoming = (e: MockEvent) => new Date(e.startsAt).getTime() > now().getTime();

function findEvent(id: string): MockEvent {
  return db().events.find((e) => e.id === id) ?? fail(404, 'not_found', 'No event with that id.');
}

// ───────────── requests ─────────────

const transitionsFor = (r: MockRequest, me: Me): PurchaseStatus[] => {
  const next = nextStatuses(r.type, r.status);
  const mine = r.requesterId === me.id;
  return next.filter((to) => {
    if (to === 'submitted') return mine;
    if (to === 'cancelled') return r.status === 'draft' || r.status === 'submitted' ? mine || me.role === 'director' : me.role === 'director';
    if (to === 'approved' || to === 'denied') return me.role === 'director' && !mine && r.status === 'submitted';
    return me.role === 'director';
  });
};

function detail(r: MockRequest, me: Me): PurchaseRequestDetail {
  const { requesterId, ...rest } = r;
  return { ...rest, requester: summary(requesterId), allowedTransitions: transitionsFor(r, me), canEdit: r.status === 'draft' && requesterId === me.id };
}
function listItem(r: MockRequest): PurchaseRequestListItem {
  return {
    id: r.id,
    number: r.number,
    title: r.title,
    vendor: r.vendor,
    type: r.type,
    status: r.status,
    totalCents: r.totalCents,
    requester: summary(r.requesterId),
    expenseAccount: r.expenseAccount,
    fund: r.fund,
    eventId: r.eventId,
    submittedAt: r.submittedAt,
    updatedAt: r.updatedAt,
  };
}
function findRequest(id: string): MockRequest {
  return db().requests.find((r) => r.id === id) ?? fail(404, 'not_found', 'No request with that id.');
}
const touch = (r: MockRequest) => {
  r.updatedAt = nowIso();
  save();
};

// ───────────── ledger ─────────────

const COMMITTED: PurchaseStatus[] = ['approved', 'fulfilling'];
const PENDING: PurchaseStatus[] = ['submitted'];

function ledger(d: MockDb, fy: number, fundId?: string) {
  const { start, end } = fiscalYearRange(fy);
  const lines = d.lines.filter((l) => l.date <= end && (!fundId || l.fundId === fundId));
  const acct = (id: string) => d.accounts.find((a) => a.id === id)!;
  const fund = (id: string) => d.funds.find((f) => f.id === id)!;
  const ref = (id: string) => {
    const a = acct(id);
    return { id: a.id, code: a.code, name: a.name, type: a.type, form990Line: a.form990Line };
  };
  /** Natural balance (debit-normal for assets/expenses). */
  const natural = (accountId: string, ls = lines) => {
    const a = acct(accountId);
    const dr = ls.filter((l) => l.accountId === accountId).reduce((s, l) => s + l.debitCents - l.creditCents, 0);
    return a.type === 'asset' || a.type === 'expense' ? dr : -dr;
  };
  const inYear = lines.filter((l) => l.date >= start && !l.opening);
  const before = lines.filter((l) => l.date < start || l.opening);
  const used = [...new Set(lines.map((l) => l.accountId))].map(acct).sort((a, b) => a.code.localeCompare(b.code));
  const restricted = (l: { fundId: string }) => fund(l.fundId).restriction === 'with_donor';
  const expenseOf = (pred: (l: (typeof lines)[number]) => boolean) =>
    inYear.filter((l) => acct(l.accountId).type === 'expense' && pred(l)).reduce((s, l) => s + l.debitCents - l.creditCents, 0);
  const releases = expenseOf(restricted);
  return { start, end, lines, inYear, before, used, acct, fund, ref, natural, restricted, expenseOf, releases };
}

function budgetRows(d: MockDb, fy: number, fundId?: string) {
  const L = ledger(d, fy, fundId);
  return d.budgets
    .filter((b) => !fundId || b.fundId === fundId)
    .map((b) => {
      const a = L.acct(b.accountId);
      const actual = L.inYear
        .filter((l) => l.accountId === b.accountId && (b.fundId === null || l.fundId === b.fundId))
        .reduce((s, l) => s + l.debitCents - l.creditCents, 0);
      const committed = d.requests
        .filter((r) => COMMITTED.includes(r.status) && r.expenseAccount.id === b.accountId && (b.fundId === null || r.fund.id === b.fundId))
        .reduce((s, r) => s + r.totalCents, 0);
      return { b, a, actual, committed };
    });
}

function report(name: FinanceReport, fy: number, fundId: string | undefined): FinanceReportResponse {
  const d = db();
  const L = ledger(d, fy, fundId);
  const meta = { fiscalYear: fy, from: L.start, asOf: today() < L.end ? today() : L.end, generatedAt: nowIso(), fundId: fundId ?? null };
  const empty = isEmpty();
  switch (name) {
    case 'trial-balance': {
      const rows = empty
        ? []
        : L.used.map((a) => {
            const n = L.lines.filter((l) => l.accountId === a.id).reduce((s, l) => s + l.debitCents - l.creditCents, 0);
            return { account: L.ref(a.id), debitCents: Math.max(n, 0), creditCents: Math.max(-n, 0) };
          });
      return {
        ...meta,
        report: name,
        rows,
        totals: { debitCents: rows.reduce((s, r) => s + r.debitCents, 0), creditCents: rows.reduce((s, r) => s + r.creditCents, 0) },
      };
    }
    case 'statement-of-activities': {
      const row = (type: 'revenue' | 'expense') =>
        empty
          ? []
          : L.used
              .filter((a) => a.type === type)
              .map((a) => {
                const sign = type === 'revenue' ? -1 : 1;
                const amt = (p: (l: (typeof L.inYear)[number]) => boolean) =>
                  sign * L.inYear.filter((l) => l.accountId === a.id && p(l)).reduce((s, l) => s + l.debitCents - l.creditCents, 0);
                // Expenses are reported in "without donor restrictions"; restricted spending appears as a release.
                const withDonor = type === 'revenue' ? amt(L.restricted) : 0;
                const total = amt(() => true);
                return { account: L.ref(a.id), withoutDonorCents: total - withDonor, withDonorCents: withDonor, totalCents: total };
              });
      const revenue = row('revenue');
      const expenses = row('expense');
      const sum = (rs: typeof revenue, k: 'withoutDonorCents' | 'withDonorCents' | 'totalCents') => rs.reduce((s, r) => s + r[k], 0);
      const releases = empty ? 0 : L.releases;
      const changeWithout = sum(revenue, 'withoutDonorCents') + releases - sum(expenses, 'totalCents');
      const changeWith = sum(revenue, 'withDonorCents') - releases;
      const beginWithout = empty ? 0 : -L.before.filter((l) => !L.restricted(l) && L.acct(l.accountId).type === 'net_asset').reduce((s, l) => s + l.debitCents - l.creditCents, 0);
      const beginWith = empty ? 0 : -L.before.filter((l) => L.restricted(l) && L.acct(l.accountId).type === 'net_asset').reduce((s, l) => s + l.debitCents - l.creditCents, 0);
      const t = (w: number, r: number) => ({ withoutDonorCents: w, withDonorCents: r, totalCents: w + r });
      return {
        ...meta,
        report: name,
        revenue,
        releasesCents: releases,
        expenses,
        changeInNetAssets: t(changeWithout, changeWith),
        netAssetsBeginning: t(beginWithout, beginWith),
        netAssetsEnding: t(beginWithout + changeWithout, beginWith + changeWith),
      };
    }
    case 'statement-of-financial-position': {
      const bal = (type: 'asset' | 'liability') =>
        empty ? [] : L.used.filter((a) => a.type === type).map((a) => ({ account: L.ref(a.id), balanceCents: L.natural(a.id) }));
      const assets = bal('asset');
      const liabilities = bal('liability');
      const netOf = (restrictedSide: boolean) =>
        empty
          ? 0
          : -L.lines
              .filter((l) => L.restricted(l) === restrictedSide && ['net_asset', 'revenue', 'expense'].includes(L.acct(l.accountId).type))
              .reduce((s, l) => s + l.debitCents - l.creditCents, 0);
      // Move restricted spending out of "with" and into "without" (the release).
      const releases = empty ? 0 : L.releases;
      const netAssets = { withoutDonorCents: netOf(false) + releases, withDonorCents: netOf(true) - releases };
      const assetsCents = assets.reduce((s, r) => s + r.balanceCents, 0);
      const liabilitiesCents = liabilities.reduce((s, r) => s + r.balanceCents, 0);
      const netAssetsCents = netAssets.withoutDonorCents + netAssets.withDonorCents;
      return {
        ...meta,
        report: name,
        assets,
        liabilities,
        netAssets,
        totals: { assetsCents, liabilitiesCents, netAssetsCents, balanced: assetsCents === liabilitiesCents + netAssetsCents },
      };
    }
    case 'statement-of-functional-expenses': {
      const rows = empty
        ? []
        : L.used
            .filter((a) => a.type === 'expense')
            .map((a) => {
              const by = (fc: string) =>
                L.inYear.filter((l) => l.accountId === a.id && (l.functionalClass ?? 'program') === fc).reduce((s, l) => s + l.debitCents - l.creditCents, 0);
              const programCents = by('program');
              const mgmtGeneralCents = by('mgmt_general');
              const fundraisingCents = by('fundraising');
              return { account: L.ref(a.id), programCents, mgmtGeneralCents, fundraisingCents, totalCents: programCents + mgmtGeneralCents + fundraisingCents };
            });
      const tot = (k: 'programCents' | 'mgmtGeneralCents' | 'fundraisingCents' | 'totalCents') => rows.reduce((s, r) => s + r[k], 0);
      return {
        ...meta,
        report: name,
        rows,
        totals: { programCents: tot('programCents'), mgmtGeneralCents: tot('mgmtGeneralCents'), fundraisingCents: tot('fundraisingCents'), totalCents: tot('totalCents') },
      };
    }
    case 'budget-vs-actual': {
      const rows = empty
        ? []
        : budgetRows(d, fy, fundId).map(({ b, a, actual, committed }) => {
            const f = b.fundId ? d.funds.find((x) => x.id === b.fundId)! : null;
            return {
              account: L.ref(a.id),
              fund: f ? { id: f.id, code: f.code, name: f.name } : null,
              budgetCents: b.amountCents,
              actualCents: actual,
              committedCents: committed,
              varianceCents: b.amountCents - actual - committed,
              usedRate: b.amountCents > 0 ? actual / b.amountCents : null,
            };
          });
      const tot = (k: 'budgetCents' | 'actualCents' | 'committedCents' | 'varianceCents') => rows.reduce((s, r) => s + r[k], 0);
      return {
        ...meta,
        report: name,
        rows,
        totals: { budgetCents: tot('budgetCents'), actualCents: tot('actualCents'), committedCents: tot('committedCents'), varianceCents: tot('varianceCents') },
      };
    }
    case 'fund-balances': {
      const rows = empty
        ? []
        : d.funds
            .filter((f) => !fundId || f.id === fundId)
            .map((f) => {
              const of = (ls: typeof L.lines) => ls.filter((l) => l.fundId === f.id);
              const cash = (ls: typeof L.lines) => -ls.filter((l) => ['net_asset', 'revenue', 'expense'].includes(L.acct(l.accountId).type)).reduce((s, l) => s + l.debitCents - l.creditCents, 0);
              const beginningCents = cash(of(L.before));
              const additionsCents = of(L.inYear).filter((l) => L.acct(l.accountId).type === 'revenue').reduce((s, l) => s + l.creditCents - l.debitCents, 0);
              const expensesCents = of(L.inYear).filter((l) => L.acct(l.accountId).type === 'expense').reduce((s, l) => s + l.debitCents - l.creditCents, 0);
              const releasesCents = f.restriction === 'with_donor' ? expensesCents : 0;
              return {
                fund: { id: f.id, code: f.code, name: f.name, kind: f.kind, restriction: f.restriction },
                beginningCents,
                additionsCents,
                releasesCents,
                expensesCents,
                endingCents: beginningCents + additionsCents - expensesCents,
              };
            });
      return { ...meta, report: name, rows };
    }
    default: {
      // 'form-990' (backend contract update, not in this branch's domain.ts yet): ledger by 990 line.
      const groups = new Map<string, { part: 'VIII' | 'IX'; line: string; totalCents: number; programCents: number; mgmtGeneralCents: number; fundraisingCents: number }>();
      for (const l of empty ? [] : L.inYear) {
        const a = L.acct(l.accountId);
        if (a.type !== 'revenue' && a.type !== 'expense') continue;
        const [part, line] = (a.form990Line ?? ' ').split(' ') as ['VIII' | 'IX', string];
        const g = groups.get(a.form990Line ?? '') ?? { part, line, totalCents: 0, programCents: 0, mgmtGeneralCents: 0, fundraisingCents: 0 };
        const amt = a.type === 'revenue' ? l.creditCents - l.debitCents : l.debitCents - l.creditCents;
        g.totalCents += amt;
        if (a.type === 'expense') {
          const fc = l.functionalClass ?? 'program';
          if (fc === 'program') g.programCents += amt;
          else if (fc === 'mgmt_general') g.mgmtGeneralCents += amt;
          else g.fundraisingCents += amt;
        }
        groups.set(a.form990Line ?? '', g);
      }
      return { ...meta, report: name, rows: [...groups.values()] } as unknown as FinanceReportResponse;
    }
  }
}

// ───────────── handlers ─────────────

const handlers: Partial<Record<EndpointName, Handler>> = {
  lookups: () => {
    const d = db();
    return {
      expenseAccounts: d.accounts.filter((a) => a.type === 'expense').map(({ id, code, name, type, form990Line }) => ({ id, code, name, type, form990Line })),
      funds: d.funds.map(({ id, code, name, kind, restriction }) => ({ id, code, name, kind, restriction })),
      events: [...d.events].sort(byStartDesc).map((e) => ({ id: e.id, name: e.name, startsAt: e.startsAt })),
      currentFiscalYear: fiscalYearFor(today()),
    };
  },

  overview: () => {
    const d = db();
    const fy = fiscalYearFor(today());
    const { start, end } = fiscalYearRange(fy);
    const events = isEmpty() ? [] : [...d.events].sort(byStartDesc);
    const past = events.filter((e) => !isUpcoming(e) && e.startsAt.slice(0, 10) >= start && e.startsAt.slice(0, 10) <= end);
    const rows = isEmpty() ? [] : budgetRows(d, fy);
    const budget = rows.reduce((s, r) => s + r.b.amountCents, 0);
    const spent = rows.reduce((s, r) => s + r.actual, 0);
    const committed = rows.reduce((s, r) => s + r.committed, 0);
    const pending = isEmpty() ? [] : d.requests.filter((r) => PENDING.includes(r.status));
    return {
      fiscalYear: fy,
      kpis: {
        upcomingEvents: events.filter(isUpcoming).length,
        avgCheckInRate: averageRate(past.map((e) => counts(e).checkInRate)),
        avgReflectionRate: averageRate(past.map((e) => counts(e).reflectionRate)),
        budgetCents: budget,
        spentCents: spent,
        committedCents: committed,
        remainingCents: budget - spent - committed,
        pendingRequests: pending.length,
        pendingRequestsCents: pending.reduce((s, r) => s + r.totalCents, 0),
      },
      recentEvents: events.slice(0, 6).map(eventItem),
    };
  },

  eventsList: (o) => {
    const q = ENDPOINTS.eventsList.query.parse(o.query ?? {});
    let items = isEmpty() ? [] : [...db().events].sort(byStartDesc);
    if (q.when === 'upcoming') items = items.filter(isUpcoming).reverse();
    if (q.when === 'past') items = items.filter((e) => !isUpcoming(e));
    if (q.q) {
      const needle = q.q.toLowerCase();
      items = items.filter((e) => `${e.name} ${e.location ?? ''}`.toLowerCase().includes(needle));
    }
    return { items: items.map(eventItem), nextCursor: null };
  },

  eventDetail: (o) => {
    const e = findEvent(o.params!.id!);
    const d = db();
    const reqs = d.requests.filter((r) => r.eventId === e.id && r.status !== 'draft');
    const spent = reqs.filter((r) => r.actualTotalCents !== null).reduce((s, r) => s + (r.actualTotalCents ?? 0), 0);
    return { event: { ...eventItem(e), endsAt: e.endsAt, url: e.url, updatedAt: e.updatedAt }, requests: reqs.map(listItem), spentCents: spent };
  },

  eventMetrics: (o) => {
    const e = findEvent(o.params!.id!);
    const c = counts(e);
    const start = new Date(e.startsAt);
    const days = 21;
    const registrationsByDay = Array.from({ length: days + 1 }, (_, i) => {
      const date = new Date(start.getTime() - (days - i) * 86_400_000).toISOString().slice(0, 10);
      // S-curve: slow start, a push in the last week.
      const t = i / days;
      return { date, cumulative: Math.round(c.registered * (t * t * (3 - 2 * t))) };
    });
    const past = !isUpcoming(e);
    const weights = [0.45, 0.25, 0.12, 0.08, 0.05, 0.03, 0.02];
    let left = past ? c.reflections : 0;
    const reflectionsByDay = past
      ? weights.map((w, i) => {
          const n = i === weights.length - 1 ? left : Math.min(left, Math.round(c.reflections * w));
          left -= n;
          return { date: new Date(start.getTime() + i * 86_400_000).toISOString().slice(0, 10), count: n };
        })
      : [];
    return { eventId: e.id, totals: c, registrationsByDay, reflectionsByDay };
  },

  eventsImport: async (o) => {
    const form = o.body as unknown as FormData;
    const fields = form.get('kind') === 'reflections' ? 'reflections' : 'luma_guests';
    const file = form.get('file');
    if (!(file instanceof File) || file.size === 0) return fail(400, 'bad_request', 'Choose a CSV file to import.', { fieldErrors: { file: ['Choose a CSV file.'] } });
    const text = await file.text();
    const [header, ...rows] = text.split(/\r?\n/).filter((l) => l.trim());
    const cols = (header ?? '').toLowerCase().split(',').map((c) => c.trim().replace(/^"|"$/g, ''));
    const emailCol = cols.findIndex((c) => c.includes('email'));
    if (emailCol < 0) return fail(422, 'import_failed', 'No email column found. Is this a Luma guest export or the reflection survey export?');
    const checkCol = cols.findIndex((c) => c.includes('checked_in') || c.includes('check-in') || c.includes('checked in'));
    const errors: { row: number; message: string }[] = [];
    const emails: string[] = [];
    let checked = 0;
    rows.forEach((line, i) => {
      const cells = line.split(',').map((c) => c.trim().replace(/^"|"$/g, ''));
      const email = cells[emailCol] ?? '';
      if (!/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(email)) errors.push({ row: i + 2, message: `"${email || '(blank)'}" is not an email address` });
      else {
        emails.push(email.toLowerCase());
        if (checkCol >= 0 && (cells[checkCol] ?? '').trim()) checked++;
      }
    });
    const dryRun = form.get('dryRun') === 'true';
    const d = db();
    let ev: MockEvent | undefined;
    const eventId = form.get('eventId');
    if (typeof eventId === 'string' && eventId) ev = findEvent(eventId);
    else {
      const name = String(form.get('eventName') ?? '').trim();
      const lumaId = String(form.get('lumaEventId') ?? '').trim();
      if (!name && !lumaId) return fail(400, 'bad_request', 'Pick an event, or give the new event a name.', { fieldErrors: { eventId: ['Pick an event or name a new one.'] } });
      ev = d.events.find((e) => lumaId && e.lumaId === lumaId);
      if (!ev) {
        ev = {
          id: nextId('event'),
          lumaId: lumaId || null,
          name: name || lumaId,
          startsAt: String(form.get('eventStartsAt') || nowIso()),
          endsAt: null,
          location: null,
          url: null,
          hostId: null,
          updatedAt: nowIso(),
          counts: { registered: 0, checkedIn: 0, reflections: 0, matchedReflections: 0 },
        };
        if (!dryRun) d.events.push(ev);
      }
    }
    const next = { ...ev.counts };
    if (fields === 'reflections') {
      next.reflections = emails.length;
      next.matchedReflections = Math.min(emails.length, next.checkedIn);
    } else {
      next.registered = emails.length;
      next.checkedIn = checked;
      next.matchedReflections = Math.min(next.matchedReflections, checked);
    }
    if (!dryRun) {
      ev.counts = next;
      ev.updatedAt = nowIso();
      save();
    }
    return {
      eventId: ev.id,
      kind: fields,
      dryRun,
      rows: rows.length,
      created: emails.length,
      updated: 0,
      skipped: errors.length,
      errors,
      metrics: eventMetrics(next),
    };
  },

  requestsList: (o, me) => {
    const q = ENDPOINTS.requestsList.query.parse(o.query ?? {});
    const scoped = isEmpty() ? [] : db().requests.filter((r) => (q.scope === 'mine' ? r.requesterId === me.id : r.status !== 'draft' || r.requesterId === me.id));
    const countsBy: Partial<Record<PurchaseStatus, number>> = {};
    for (const r of scoped) countsBy[r.status] = (countsBy[r.status] ?? 0) + 1;
    let items = scoped;
    if (q.status) items = items.filter((r) => (q.status === 'pending' ? r.status === 'submitted' : r.status === q.status));
    if (q.eventId) items = items.filter((r) => r.eventId === q.eventId);
    if (q.q) {
      const needle = q.q.toLowerCase();
      items = items.filter((r) => `${r.title} ${r.vendor} ${r.number}`.toLowerCase().includes(needle));
    }
    items = [...items].sort((a, b) => b.updatedAt.localeCompare(a.updatedAt));
    return { items: items.map(listItem), nextCursor: null, counts: countsBy };
  },

  requestCreate: (o, me) => {
    const body = parseBody(ENDPOINTS.requestCreate.body, o.body);
    const d = db();
    const a = d.accounts.find((x) => x.id === body.expenseAccountId) ?? fail(400, 'bad_request', 'Unknown budget category.', { fieldErrors: { expenseAccountId: ['Unknown category'] } });
    const f = d.funds.find((x) => x.id === body.fundId) ?? fail(400, 'bad_request', 'Unknown fund.', { fieldErrors: { fundId: ['Unknown fund'] } });
    const ev = body.eventId ? findEvent(body.eventId) : null;
    const n = d.requests.length + 1;
    const ts = nowIso();
    const r: MockRequest = {
      id: nextId('req'),
      number: `${fiscalYearFor(today())}-${String(n).padStart(4, '0')}`,
      title: body.title,
      vendor: body.vendor,
      type: body.type,
      status: body.submit ? 'submitted' : 'draft',
      totalCents: body.quantity * body.unitCostCents + body.taxShipCents,
      requesterId: me.id,
      expenseAccount: { id: a.id, code: a.code, name: a.name },
      fund: { id: f.id, code: f.code, name: f.name },
      eventId: ev?.id ?? null,
      submittedAt: body.submit ? ts : null,
      updatedAt: ts,
      itemUrl: body.itemUrl ?? null,
      quantity: body.quantity,
      unitCostCents: body.unitCostCents,
      taxShipCents: body.taxShipCents,
      actualTotalCents: null,
      functionalClass: body.functionalClass,
      justification: body.justification,
      neededBy: body.neededBy ?? null,
      event: ev ? { id: ev.id, name: ev.name } : null,
      decidedAt: null,
      createdAt: ts,
      approvals: [],
      attachments: [],
      journalEntryIds: [],
    };
    d.requests.push(r);
    save();
    return { request: detail(r, me) };
  },

  requestDetail: (o, me) => ({ request: detail(findRequest(o.params!.id!), me) }),

  requestSubmit: (o, me) => {
    const r = findRequest(o.params!.id!);
    if (r.requesterId !== me.id) fail(403, 'forbidden', 'Only the requester can submit this request.');
    if (r.status !== 'draft') fail(409, 'invalid_transition', `A ${r.status} request can't be submitted.`);
    r.status = 'submitted';
    r.submittedAt = nowIso();
    touch(r);
    return { request: detail(r, me) };
  },

  requestDelete: (o, me) => {
    const d = db();
    const r = findRequest(o.params!.id!);
    if (r.status !== 'draft' || r.requesterId !== me.id) fail(409, 'conflict', 'Only your own drafts can be deleted.');
    d.requests = d.requests.filter((x) => x.id !== r.id);
    save();
    return { ok: true };
  },

  requestApprove: (o, me) => decide(o, me, 'approved'),
  requestDeny: (o, me) => decide(o, me, 'denied'),

  requestTransition: (o, me) => {
    requireDirector(me);
    const r = findRequest(o.params!.id!);
    const body = parseBody(ENDPOINTS.requestTransition.body, o.body);
    if (!nextStatuses(r.type, r.status).includes(body.to)) fail(409, 'invalid_transition', `Can't move a ${r.status} request to ${body.to}.`);
    r.status = body.to;
    if (body.to === 'fulfilled' || body.to === 'reimbursed') {
      r.actualTotalCents = body.actualTotalCents;
      const entry = nextId('je');
      r.journalEntryIds.push(entry);
      const date = today();
      db().lines.push(
        { entryId: entry, date, accountId: r.expenseAccount.id, fundId: r.fund.id, functionalClass: r.functionalClass, debitCents: body.actualTotalCents, creditCents: 0, opening: false },
        { entryId: entry, date, accountId: body.to === 'reimbursed' ? 'acct-2100' : 'acct-1000', fundId: r.fund.id, functionalClass: null, debitCents: 0, creditCents: body.actualTotalCents, opening: false },
      );
    }
    touch(r);
    return { request: detail(r, me) };
  },

  requestAttach: (o, me) => {
    const r = findRequest(o.params!.id!);
    const form = o.body as unknown as FormData;
    const file = form.get('file');
    if (!(file instanceof File)) return fail(400, 'bad_request', 'No file attached.');
    const kind = (form.get('kind') as 'receipt' | 'invoice' | 'quote' | 'other' | null) ?? 'receipt';
    const att = {
      id: nextId('att'),
      kind,
      filename: file.name,
      mimeType: file.type || 'application/octet-stream',
      sizeBytes: file.size,
      storage: 'local' as const,
      url: null,
      uploadedBy: summary(me.id),
      createdAt: nowIso(),
    };
    r.attachments.push(att);
    touch(r);
    return { attachment: att };
  },

  financeSummary: (o) => {
    const q = ENDPOINTS.financeSummary.query.parse(o.query ?? {});
    const d = db();
    const fy = q.fiscalYear ?? fiscalYearFor(today());
    const rows = isEmpty() ? [] : budgetRows(d, fy, q.fundId);
    const byAccount = new Map<string, { account: { id: string; code: string; name: string }; budgetCents: number; spentCents: number; committedCents: number }>();
    for (const { b, a, actual, committed } of rows) {
      const cur = byAccount.get(a.id) ?? { account: { id: a.id, code: a.code, name: a.name }, budgetCents: 0, spentCents: 0, committedCents: 0 };
      cur.budgetCents += b.amountCents;
      cur.spentCents += actual;
      cur.committedCents += committed;
      byAccount.set(a.id, cur);
    }
    const byCategory = [...byAccount.values()]
      .sort((x, y) => x.account.code.localeCompare(y.account.code))
      .map((c) => ({ ...c, remainingCents: c.budgetCents - c.spentCents - c.committedCents }));
    const tot = (k: 'budgetCents' | 'spentCents' | 'committedCents') => byCategory.reduce((s, c) => s + c[k], 0);
    const fb = report('fund-balances', fy, q.fundId);
    const funds =
      fb.report === 'fund-balances'
        ? fb.rows.map((r) => {
            const f = d.funds.find((x) => x.id === r.fund.id)!;
            return { ...r.fund, balanceCents: r.endingCents, awardedCents: f.awardedCents, endDate: f.endDate };
          })
        : [];
    return {
      fiscalYear: fy,
      asOf: today(),
      totals: { budgetCents: tot('budgetCents'), spentCents: tot('spentCents'), committedCents: tot('committedCents'), remainingCents: tot('budgetCents') - tot('spentCents') - tot('committedCents') },
      byCategory,
      funds,
    };
  },

  financeReport: (o, me) => {
    const name = o.params!.report as FinanceReport;
    if (me.role !== 'director' && !(OFFICER_FINANCE_REPORTS as readonly string[]).includes(name)) fail(403, 'forbidden', 'Directors only.');
    const q = ENDPOINTS.financeReport.query.parse(o.query ?? {});
    return report(name, q.fiscalYear ?? fiscalYearFor(today()), q.fundId);
  },

  adminUsers: (_o, me) => {
    requireDirector(me);
    const users = isEmpty() ? db().users.filter((u) => u.id === me.id) : db().users;
    return { users };
  },
  adminUserCreate: (o, me) => {
    requireDirector(me);
    const body = parseBody(ENDPOINTS.adminUserCreate.body, o.body);
    const d = db();
    if (d.users.some((u) => u.discordId === body.discordId)) fail(409, 'conflict', 'That Discord account is already on the list.');
    const u: AdminUser = {
      id: nextId('user'),
      discordId: body.discordId,
      name: body.name,
      role: body.role,
      title: body.title ?? null,
      email: body.email ?? null,
      avatarUrl: null,
      active: true,
      lastLoginAt: null,
      createdAt: nowIso(),
    };
    d.users.push(u);
    save();
    return { user: u };
  },
  adminUserUpdate: (o, me) => {
    requireDirector(me);
    const body = parseBody(ENDPOINTS.adminUserUpdate.body, o.body);
    const d = db();
    const u = d.users.find((x) => x.id === o.params!.id) ?? fail(404, 'not_found', 'No such user.');
    const next = { ...u, ...Object.fromEntries(Object.entries(body).filter(([, v]) => v !== undefined)) } as AdminUser;
    const directorsLeft = d.users.filter((x) => (x.id === u.id ? next : x)).filter((x) => x.role === 'director' && x.active).length;
    if (directorsLeft === 0) fail(409, 'conflict', 'This is the last active director. Make someone else a director first.');
    Object.assign(u, next);
    save();
    return { user: u };
  },

  adminTokens: (_o, me) => {
    requireDirector(me);
    return { tokens: isEmpty() ? [] : db().tokens };
  },
  adminTokenCreate: (o, me) => {
    requireDirector(me);
    const body = parseBody(ENDPOINTS.adminTokenCreate.body, o.body);
    const rand = Array.from(crypto.getRandomValues(new Uint8Array(24)), (b) => b.toString(16).padStart(2, '0')).join('');
    const plaintext = `cst_${rand}`;
    const token = {
      id: nextId('tok'),
      name: body.name,
      prefix: plaintext.slice(0, 8),
      scopes: body.scopes,
      createdBy: summary(me.id),
      createdAt: nowIso(),
      lastUsedAt: null,
      expiresAt: body.expiresInDays ? new Date(Date.now() + body.expiresInDays * 86_400_000).toISOString() : null,
      revokedAt: null,
    };
    db().tokens.unshift(token);
    save();
    return { token, plaintext };
  },
  adminTokenRevoke: (o, me) => {
    requireDirector(me);
    const t = db().tokens.find((x) => x.id === o.params!.id) ?? fail(404, 'not_found', 'No such token.');
    t.revokedAt = nowIso();
    save();
    return { ok: true };
  },
};

function decide(o: Opts, me: Me, decision: 'approved' | 'denied') {
  requireDirector(me);
  const r = findRequest(o.params!.id!);
  const body = parseBody(decision === 'denied' ? ENDPOINTS.requestDeny.body : ENDPOINTS.requestApprove.body, o.body ?? {});
  if (r.requesterId === me.id) fail(403, 'forbidden', "Directors can't approve their own requests.");
  if (r.status !== 'submitted') fail(409, 'invalid_transition', `This request is ${r.status}, not awaiting a decision.`);
  r.status = decision;
  r.decidedAt = nowIso();
  r.approvals.push({ id: nextId('appr'), director: summary(me.id), decision, comment: body.comment || null, createdAt: r.decidedAt });
  touch(r);
  return { request: detail(r, me) };
}

export async function mockCall(name: EndpointName, opts: CallOptions<EndpointName>): Promise<{ handled: true; data: unknown } | { handled: false }> {
  const handler = handlers[name];
  if (!handler) return { handled: false };
  const s = scenario();
  const isRead = ENDPOINTS[name].method === 'GET';
  if (isRead && s === 'loading') await new Promise(() => {});
  if (isRead && s === 'error') fail(500, 'internal', 'The server had a problem loading this. (mock error)');
  await new Promise((r) => setTimeout(r, isRead ? 120 : 250));
  const me = await currentUser();
  return { handled: true, data: await handler(opts as Opts, me) };
}
