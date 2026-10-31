import { eq, sql, gt } from 'drizzle-orm';
import * as s from '../db/schema';
import * as C from '../../core/contracts';
import {
  makeReport,
  reportCsv,
  type ReportData,
} from '../../core/accounting/reports';
import { fiscalYearFor, fiscalYearRange, monthlyPeriods } from '../../core/fiscal';
import { closePeriod } from '../../core/accounting/engine';
import { OFFICER_FINANCE_REPORTS } from '../../core/domain';
import { averageRate } from '../../core/metrics';
import {
  type ApiContext,
  type BatchItem,
  director,
  fail,
  now,
  uuid,
  mutate,
} from './context';
import { catalog, eventItem } from './read-model';
export async function reportData(
  c: ApiContext,
  asOf = now().toISOString().slice(0, 10),
): Promise<ReportData> {
  const [accounts, funds, lines, budgets, requests] = await Promise.all([
    c.db.select().from(s.accounts),
    c.db.select().from(s.funds),
    c.db
      .select({
        accountId: s.journalLines.accountId,
        fundId: s.journalLines.fundId,
        entryDate: s.journalEntries.entryDate,
        source: s.journalEntries.source,
        purchaseRequestId: s.journalEntries.purchaseRequestId,
        reversesSource: sql<
          string | null
        >`(SELECT source FROM journal_entries reversed WHERE reversed.id = ${s.journalEntries.reversesEntryId})`,
        debitCents: s.journalLines.debitCents,
        creditCents: s.journalLines.creditCents,
        functionalClass: s.journalLines.functionalClass,
        // In-kind/stock gifts (and their reversals) feed Form 990 line 1g.
        noncash: sql<number>`EXISTS (SELECT 1 FROM contributions c WHERE c.kind IN ('in_kind', 'stock') AND c.journal_entry_id IN (${s.journalEntries.id}, ${s.journalEntries.reversesEntryId}))`,
      })
      .from(s.journalLines)
      .innerJoin(
        s.journalEntries,
        eq(s.journalEntries.id, s.journalLines.entryId),
      )
      .where(eq(s.journalEntries.status, 'posted')),
    c.db.select().from(s.budgets),
    c.db.select().from(s.purchaseRequests),
  ]);
  const cutoff = new Date(`${asOf}T23:59:59.999Z`);
  const history =
    asOf < now().toISOString().slice(0, 10)
      ? (
          await c.db
            .select()
            .from(s.auditLog)
            .where(gt(s.auditLog.createdAt, cutoff))
        ).sort((a, b) => b.createdAt.getTime() - a.createdAt.getTime())
      : [];
  const committedAt = (r: typeof s.purchaseRequests.$inferSelect) => {
    let status: string | null = r.status;
    // Undo later mutations using their immutable before-images.
    for (const a of history) {
      const before = a.beforeJson ? JSON.parse(a.beforeJson) : null;
      if (a.entityType === 'purchase_request' && a.entityId === r.id)
        status = before?.status ?? null;
      if (a.entityType === 'fulfillment_job' && before?.requestId === r.id)
        status = a.action === 'job.claimed' ? 'approved' : 'fulfilling';
    }
    if ((r.submittedAt ?? r.createdAt) > cutoff) return false;
    if (r.decidedAt && r.decidedAt > cutoff) return false;
    const settlementDate = lines
      .filter(
        (l) =>
          l.purchaseRequestId === r.id &&
          accounts.some((a) => a.id === l.accountId && a.type === 'expense'),
      )
      .map((l) => l.entryDate)
      .sort()[0];
    if (
      settlementDate &&
      settlementDate > asOf &&
      ['fulfilled', 'reimbursed', 'closed'].includes(status ?? '')
    )
      status = 'approved';
    return status === 'approved' || status === 'fulfilling';
  };
  return {
    accounts,
    funds,
    lines,
    budgets,
    commitments: requests.filter(committedAt).map((r) => ({
      expenseAccountId: r.expenseAccountId,
      fundId: r.fundId,
      totalCents: r.totalCents,
      submittedDate: (r.submittedAt ?? r.createdAt).toISOString().slice(0, 10),
    })),
  };
}
export function financeQuery(input: unknown) {
  const q = C.FinanceQuery.parse(input);
  const asOf = q.asOf ?? now().toISOString().slice(0, 10);
  const fiscalYear = q.fiscalYear ?? fiscalYearFor(asOf);
  const range = fiscalYearRange(fiscalYear);
  return {
    fiscalYear,
    asOf:
      q.asOf ??
      (asOf < range.start ? range.start : asOf > range.end ? range.end : asOf),
    fundId: q.fundId,
  };
}
export function summary(data: ReportData, q: ReturnType<typeof financeQuery>) {
  const report = makeReport('budget-vs-actual', data, q);
  if (report.report !== 'budget-vs-actual') throw new Error('report mismatch');
  const balances = makeReport('fund-balances', data, q);
  if (balances.report !== 'fund-balances') throw new Error('report mismatch');
  const groups = new Map<
    string,
    C.FinanceSummaryResponse['byCategory'][number]
  >();
  for (const r of report.rows) {
    const row = groups.get(r.account.id) ?? {
      account: r.account,
      budgetCents: 0,
      spentCents: 0,
      committedCents: 0,
      remainingCents: 0,
    };
    row.budgetCents += r.budgetCents;
    row.spentCents += r.actualCents;
    row.committedCents += r.committedCents;
    row.remainingCents += r.varianceCents;
    groups.set(r.account.id, row);
  }
  return {
    fiscalYear: q.fiscalYear,
    asOf: q.asOf,
    totals: {
      budgetCents: report.totals.budgetCents,
      spentCents: report.totals.actualCents,
      committedCents: report.totals.committedCents,
      remainingCents: report.totals.varianceCents,
    },
    byCategory: [...groups.values()],
    funds: balances.rows.map((r) => ({
      ...r.fund,
      balanceCents: r.endingCents,
      awardedCents:
        (
          data.funds.find((f) => f.id === r.fund.id) as
            | typeof s.funds.$inferSelect
            | undefined
        )?.amountAwardedCents ?? null,
      endDate:
        (
          data.funds.find((f) => f.id === r.fund.id) as
            | typeof s.funds.$inferSelect
            | undefined
        )?.endDate ?? null,
    })),
  };
}
export async function finance(
  c: ApiContext,
  name: string,
  id: string | undefined,
  body: unknown,
  query: unknown,
): Promise<unknown> {
  if (name === 'periodsCreate') {
    director(c);
    const b = C.FiscalYearPeriodsCreateRequest.parse(body);
    const existing = await c.db
      .select()
      .from(s.fiscalPeriods)
      .where(eq(s.fiscalPeriods.fiscalYear, b.fiscalYear));
    if (existing.length)
      fail(409, 'conflict', `FY${b.fiscalYear} already has fiscal periods`);
    const periods = monthlyPeriods(b.fiscalYear).map((p) => ({
      id: `period-${p.startDate.slice(0, 7)}`,
      fiscalYear: b.fiscalYear,
      ...p,
    }));
    await mutate(
      c,
      sql`NOT EXISTS (SELECT 1 FROM fiscal_periods WHERE fiscal_year = ${b.fiscalYear})`,
      [c.db.insert(s.fiscalPeriods).values(periods)],
      'fiscal_year.periods_created',
      'fiscal_period',
      `FY${b.fiscalYear}`,
      null,
      periods,
    );
  }
  if (name === 'periodClose') {
    director(c);
    const b = C.FiscalPeriodCloseRequest.parse(body);
    const p = await c.db
      .select()
      .from(s.fiscalPeriods)
      .where(eq(s.fiscalPeriods.id, id!))
      .get();
    if (!p) fail(404, 'not_found', 'Fiscal period not found');
    let next;
    try {
      next = closePeriod(p, b.to);
    } catch {
      fail(409, 'conflict', `A ${p.status} period cannot become ${b.to}`);
    }
    if (next.status === p.status)
      fail(409, 'conflict', `Period is already ${p.status}`);
    const change = {
      status: next.status,
      closedAt: p.closedAt ?? now(),
      closedBy: p.closedBy ?? c.user!.id,
    };
    await mutate(
      c,
      sql`EXISTS (SELECT 1 FROM fiscal_periods WHERE id = ${p.id} AND status = ${p.status})`,
      [
        c.db
          .update(s.fiscalPeriods)
          .set(change)
          .where(eq(s.fiscalPeriods.id, p.id)),
      ],
      `fiscal_period.${next.status}`,
      'fiscal_period',
      p.id,
      p,
      change,
    );
  }
  if (name === 'periods' || name === 'periodsCreate' || name === 'periodClose') {
    director(c);
    return {
      periods: (await c.db.select().from(s.fiscalPeriods)).sort((a, b) =>
        a.startDate.localeCompare(b.startDate),
      ),
    };
  }
  if (name === 'accounts') {
    director(c);
    return { accounts: await c.db.select().from(s.accounts) };
  }
  if (name === 'lookups') {
    const cat = await catalog(c.db);
    return {
      expenseAccounts: cat.accounts.filter(
        (a) => a.active && a.budgetable && a.type === 'expense',
      ),
      funds: cat.funds.filter((f) => f.active),
      events: cat.events,
      currentFiscalYear: fiscalYearFor(now().toISOString().slice(0, 10)),
    };
  }
  let q = financeQuery(query);
  if (name === 'budgetsPut') {
    director(c);
    const b = C.BudgetsUpsertRequest.parse(body);
    q = financeQuery({ fiscalYear: b.fiscalYear });
    const keys = new Set<string>();
    for (const line of b.lines) {
      const key = `${line.accountId}:${line.fundId ?? ''}`;
      if (
        keys.has(key) ||
        b.lines.some(
          (l) =>
            l !== line &&
            l.accountId === line.accountId &&
            (l.fundId === null || line.fundId === null),
        )
      )
        fail(400, 'bad_request', 'Duplicate or overlapping budget lines');
      keys.add(key);
    }
    const cat = await catalog(c.db);
    for (const line of b.lines)
      if (
        !cat.accounts.some(
          (a) =>
            a.id === line.accountId &&
            a.active &&
            a.type === 'expense' &&
            a.budgetable,
        ) ||
        (line.fundId &&
          !cat.funds.some((f) => f.id === line.fundId && f.active))
      )
        fail(400, 'bad_request', 'Invalid budget account/fund');
    const before = await c.db
      .select()
      .from(s.budgets)
      .where(eq(s.budgets.fiscalYear, b.fiscalYear));
    const writes: BatchItem[] = [
      c.db.delete(s.budgets).where(eq(s.budgets.fiscalYear, b.fiscalYear)),
    ];
    for (const l of b.lines)
      writes.push(
        c.db
          .insert(s.budgets)
          .values({ id: uuid(), fiscalYear: b.fiscalYear, ...l }),
      );
    await mutate(
      c,
      sql`1`,
      writes,
      'budget.replaced',
      'budget',
      String(b.fiscalYear),
      before,
      b,
    );
  }
  if (name === 'budgetsGet' || name === 'budgetsPut') {
    const cat = await catalog(c.db),
      lines = await c.db
        .select()
        .from(s.budgets)
        .where(eq(s.budgets.fiscalYear, q.fiscalYear));
    return {
      fiscalYear: q.fiscalYear,
      lines: lines
        .filter((l) => !q.fundId || !l.fundId || l.fundId === q.fundId)
        .map((l) => ({
          ...l,
          account: cat.accounts.find((a) => a.id === l.accountId),
          fund: cat.funds.find((f) => f.id === l.fundId) ?? null,
        })),
    };
  }
  const data = await reportData(c, q.asOf);
  if (name === 'financeReport') {
    const report = C.FinanceReportName.parse(id);
    if (
      c.user?.role !== 'director' &&
      !(OFFICER_FINANCE_REPORTS as readonly string[]).includes(report)
    )
      fail(403, 'forbidden', 'This report requires director role');
    const result = C.FinanceReportResponse.parse(makeReport(report, data, q));
    if (C.FinanceReportQuery.parse(query).format === 'csv')
      return new Response(reportCsv(result), {
        headers: {
          'Content-Type': 'text/csv; charset=utf-8',
          'Content-Disposition': `attachment; filename="${report}-${q.fiscalYear}.csv"`,
        },
      });
    return result;
  }
  const result = summary(data, q);
  if (name === 'financeSummary') return result;
  const cat = await catalog(c.db),
    allEvents = await Promise.all(
      cat.events.map((e) => eventItem(c.db, e, cat)),
    );
  const today = now().toISOString();
  const from = fiscalYearRange(q.fiscalYear).start;
  const past = allEvents.filter(
    (e) =>
      Date.parse(e.startsAt) >= Date.parse(from) &&
      Date.parse(e.startsAt) <= Date.parse(today),
  );
  const pending = (await c.db.select().from(s.purchaseRequests)).filter(
    (r) => r.status === 'submitted',
  );
  return {
    fiscalYear: q.fiscalYear,
    kpis: {
      upcomingEvents: allEvents.filter(
        (e) => Date.parse(e.startsAt) >= Date.parse(today),
      ).length,
      avgCheckInRate: averageRate(past.map((e) => e.metrics.checkInRate)),
      avgReflectionRate: averageRate(past.map((e) => e.metrics.reflectionRate)),
      ...result.totals,
      pendingRequests: pending.length,
      pendingRequestsCents: pending.reduce((sum, r) => sum + r.totalCents, 0),
    },
    recentEvents: allEvents
      .sort((a, b) => Date.parse(b.startsAt) - Date.parse(a.startsAt))
      .slice(0, 10),
  };
}
