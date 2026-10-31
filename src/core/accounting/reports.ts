import type {
  AccountType,
  Restriction,
  FunctionalClass,
  FinanceReport,
} from '../domain';
import type { FinanceReportResponse } from '../contracts';
import { fiscalYearRange } from '../fiscal';
export interface ReportAccount {
  id: string;
  code: string;
  name: string;
  type: AccountType;
  form990Line: string | null;
  form990Part: string | null;
  restriction?: Restriction | null;
}
export interface ReportFund {
  id: string;
  code: string;
  name: string;
  kind: 'general' | 'grant' | 'sponsor' | 'other';
  restriction: Restriction;
}
export interface ReportLine {
  accountId: string;
  fundId: string;
  entryDate: string;
  source: string;
  reversesSource?: string | null;
  debitCents: number;
  creditCents: number;
  functionalClass: FunctionalClass | null;
}
export interface ReportData {
  accounts: ReportAccount[];
  funds: ReportFund[];
  lines: ReportLine[];
  budgets: {
    accountId: string;
    fundId: string | null;
    amountCents: number;
    fiscalYear: number;
  }[];
  commitments: {
    expenseAccountId: string;
    fundId: string;
    totalCents: number;
    submittedDate: string;
  }[];
}
export interface ReportQuery {
  fiscalYear: number;
  asOf: string;
  fundId?: string;
}
const sum = (values: number[]) => values.reduce((a, b) => a + b, 0);
const amount = (l: ReportLine) => l.debitCents - l.creditCents;
const split = (withoutDonorCents = 0, withDonorCents = 0) => ({
  withoutDonorCents,
  withDonorCents,
  totalCents: withoutDonorCents + withDonorCents,
});
export function makeReport(
  report: FinanceReport,
  data: ReportData,
  query: ReportQuery,
): FinanceReportResponse {
  const from = fiscalYearRange(query.fiscalYear).start;
  const meta = {
    fiscalYear: query.fiscalYear,
    from,
    asOf: query.asOf,
    generatedAt: new Date().toISOString(),
    fundId: query.fundId ?? null,
  };
  const all = data.lines.filter(
    (l) =>
      l.entryDate <= query.asOf && (!query.fundId || l.fundId === query.fundId),
  );
  const current = all.filter((l) => l.entryDate >= from);
  const account = (id: string) => data.accounts.find((a) => a.id === id)!;
  const donor = (id: string) =>
    data.funds.find((f) => f.id === id)!.restriction === 'with_donor';
  const withDonor = (l: ReportLine) => {
    const a = account(l.accountId);
    if (a.type === 'expense') return false;
    if (
      a.type === 'revenue' &&
      a.form990Part === 'VIII' &&
      a.form990Line?.startsWith('2')
    )
      return false;
    if (a.type === 'net_asset' && a.restriction)
      return a.restriction === 'with_donor';
    return donor(l.fundId);
  };
  const balance = (lines: ReportLine[], id: string) =>
    sum(lines.filter((l) => l.accountId === id).map(amount));
  const netAssets = (lines: ReportLine[]) => {
    const result = split();
    for (const l of lines)
      if (
        ['net_asset', 'revenue', 'expense'].includes(account(l.accountId).type)
      ) {
        const key = withDonor(l) ? 'withDonorCents' : 'withoutDonorCents';
        result[key] -= amount(l);
      }
    result.totalCents = result.withDonorCents + result.withoutDonorCents;
    return result;
  };
  if (report === 'form-990')
    return { ...meta, report, rows: form990Rollup(data, query) };
  if (report === 'trial-balance') {
    const rows = data.accounts.map((account) => {
      const n = balance(all, account.id);
      return {
        account,
        debitCents: Math.max(n, 0),
        creditCents: Math.max(-n, 0),
      };
    });
    return {
      ...meta,
      report,
      rows,
      totals: {
        debitCents: sum(rows.map((r) => r.debitCents)),
        creditCents: sum(rows.map((r) => r.creditCents)),
      },
    };
  }
  if (report === 'statement-of-financial-position') {
    const assets = data.accounts
      .filter((a) => a.type === 'asset')
      .map((account) => ({ account, balanceCents: balance(all, account.id) }));
    const liabilities = data.accounts
      .filter((a) => a.type === 'liability')
      .map((account) => ({ account, balanceCents: -balance(all, account.id) }));
    const net = netAssets(all);
    const assetsCents = sum(assets.map((a) => a.balanceCents));
    const liabilitiesCents = sum(liabilities.map((a) => a.balanceCents));
    return {
      ...meta,
      report,
      assets,
      liabilities,
      netAssets: net,
      totals: {
        assetsCents,
        liabilitiesCents,
        netAssetsCents: net.totalCents,
        balanced: assetsCents === liabilitiesCents + net.totalCents,
      },
    };
  }
  if (report === 'statement-of-activities') {
    const activity = (type: 'revenue' | 'expense') =>
      data.accounts
        .filter((a) => a.type === type)
        .map((account) => {
          const lines = current.filter((l) => l.accountId === account.id);
          const sign = type === 'expense' ? 1 : -1;
          return {
            account,
            ...split(
              sum(
                lines.filter((l) => !withDonor(l)).map((l) => sign * amount(l)),
              ),
              sum(
                lines.filter((l) => withDonor(l)).map((l) => sign * amount(l)),
              ),
            ),
          };
        });
    const beginning = netAssets(all.filter((l) => l.entryDate < from));
    const ending = netAssets(all);
    return {
      ...meta,
      report,
      revenue: activity('revenue'),
      expenses: activity('expense'),
      releasesCents: sum(
        current
          .filter(
            (l) =>
              (l.source === 'release' ||
                (l.source === 'reversal' && l.reversesSource === 'release')) &&
              !withDonor(l) &&
              account(l.accountId).type === 'net_asset',
          )
          .map((l) => -amount(l)),
      ),
      netAssetsBeginning: beginning,
      netAssetsEnding: ending,
      changeInNetAssets: split(
        ending.withoutDonorCents - beginning.withoutDonorCents,
        ending.withDonorCents - beginning.withDonorCents,
      ),
    };
  }
  if (report === 'statement-of-functional-expenses') {
    const rows = data.accounts
      .filter((a) => a.type === 'expense')
      .map((account) => {
        const lines = current.filter((l) => l.accountId === account.id);
        const n = (f: FunctionalClass) =>
          sum(lines.filter((l) => l.functionalClass === f).map(amount));
        return {
          account,
          programCents: n('program'),
          mgmtGeneralCents: n('mgmt_general'),
          fundraisingCents: n('fundraising'),
          totalCents: sum(lines.map(amount)),
        };
      });
    return {
      ...meta,
      report,
      rows,
      totals: {
        programCents: sum(rows.map((r) => r.programCents)),
        mgmtGeneralCents: sum(rows.map((r) => r.mgmtGeneralCents)),
        fundraisingCents: sum(rows.map((r) => r.fundraisingCents)),
        totalCents: sum(rows.map((r) => r.totalCents)),
      },
    };
  }
  if (report === 'fund-balances') {
    return {
      ...meta,
      report,
      rows: data.funds
        .filter((f) => !query.fundId || f.id === query.fundId)
        .map((fund) => {
          const lines = all.filter((l) => l.fundId === fund.id);
          const period = lines.filter((l) => l.entryDate >= from);
          return {
            fund,
            beginningCents: netAssets(lines.filter((l) => l.entryDate < from))
              .totalCents,
            additionsCents: sum(
              period
                .filter(
                  (l) =>
                    ['revenue', 'net_asset'].includes(
                      account(l.accountId).type,
                    ) &&
                    l.source !== 'release' &&
                    !(
                      l.source === 'reversal' && l.reversesSource === 'release'
                    ),
                )
                .map((l) => -amount(l)),
            ),
            releasesCents: sum(
              period
                .filter(
                  (l) =>
                    (l.source === 'release' ||
                      (l.source === 'reversal' &&
                        l.reversesSource === 'release')) &&
                    account(l.accountId).type === 'net_asset' &&
                    withDonor(l),
                )
                .map(amount),
            ),
            expensesCents: sum(
              period
                .filter((l) => account(l.accountId).type === 'expense')
                .map(amount),
            ),
            endingCents: netAssets(lines).totalCents,
            restrictedEndingCents: netAssets(lines).withDonorCents,
            unrestrictedEndingCents: netAssets(lines).withoutDonorCents,
          };
        }),
    };
  }
  // Fund-specific and all-fund budgets may coexist for different accounts. Reject overlaps on write.
  const budgets = data.budgets.filter(
    (b) =>
      b.fiscalYear === query.fiscalYear &&
      (!query.fundId || b.fundId === query.fundId || b.fundId === null),
  );
  const rows = budgets.map((b) => {
    const lines = current.filter(
      (l) =>
        l.accountId === b.accountId && (!b.fundId || l.fundId === b.fundId),
    );
    const actualCents = sum(lines.map(amount));
    const committedCents = sum(
      data.commitments
        .filter(
          (c) =>
            c.expenseAccountId === b.accountId &&
            (!b.fundId || c.fundId === b.fundId) &&
            (!query.fundId || c.fundId === query.fundId) &&
            c.submittedDate >= from &&
            c.submittedDate <= query.asOf,
        )
        .map((c) => c.totalCents),
    );
    return {
      account: account(b.accountId),
      fund: b.fundId ? data.funds.find((f) => f.id === b.fundId)! : null,
      budgetCents: b.amountCents,
      actualCents,
      committedCents,
      varianceCents: b.amountCents - actualCents - committedCents,
      usedRate: b.amountCents ? Math.max(0, actualCents / b.amountCents) : null,
    };
  });
  // Include each unbudgeted account/fund combination, even when another fund has a budget.
  for (const a of data.accounts.filter((a) => a.type === 'expense')) {
    const funds = data.funds.filter(
      (f) =>
        (!query.fundId || f.id === query.fundId) &&
        !budgets.some(
          (b) => b.accountId === a.id && (!b.fundId || b.fundId === f.id),
        ),
    );
    for (const fund of funds) {
      const actualCents = balance(
        current.filter((l) => l.fundId === fund.id),
        a.id,
      );
      const committedCents = sum(
        data.commitments
          .filter(
            (c) =>
              c.expenseAccountId === a.id &&
              c.fundId === fund.id &&
              c.submittedDate >= from &&
              c.submittedDate <= query.asOf,
          )
          .map((c) => c.totalCents),
      );
      if (actualCents || committedCents)
        rows.push({
          account: a,
          fund,
          budgetCents: 0,
          actualCents,
          committedCents,
          varianceCents: -actualCents - committedCents,
          usedRate: null,
        });
    }
  }
  return {
    ...meta,
    report: 'budget-vs-actual',
    rows,
    totals: {
      budgetCents: sum(rows.map((r) => r.budgetCents)),
      actualCents: sum(rows.map((r) => r.actualCents)),
      committedCents: sum(rows.map((r) => r.committedCents)),
      varianceCents: sum(rows.map((r) => r.varianceCents)),
    },
  };
}
/** 990 rollup groups mapped revenue/expense accounts; functional columns mirror Part IX. */
export function form990Rollup(data: ReportData, query: ReportQuery) {
  const from = fiscalYearRange(query.fiscalYear).start;
  const groups = new Map<
    string,
    {
      part: 'VIII' | 'IX';
      line: string;
      totalCents: number;
      programCents: number;
      mgmtGeneralCents: number;
      fundraisingCents: number;
    }
  >();
  for (const l of data.lines.filter(
    (l) =>
      l.entryDate >= from &&
      l.entryDate <= query.asOf &&
      (!query.fundId || l.fundId === query.fundId),
  )) {
    const a = data.accounts.find((a) => a.id === l.accountId)!;
    if (!['VIII', 'IX'].includes(a.form990Part ?? '') || !a.form990Line)
      continue;
    const key = `${a.form990Part}:${a.form990Line}`;
    const row = groups.get(key) ?? {
      part: a.form990Part as 'VIII' | 'IX',
      line: a.form990Line,
      totalCents: 0,
      programCents: 0,
      mgmtGeneralCents: 0,
      fundraisingCents: 0,
    };
    const n = amount(l) * (a.type === 'revenue' ? -1 : 1);
    row.totalCents += n;
    if (a.type === 'expense' && l.functionalClass)
      row[
        l.functionalClass === 'program'
          ? 'programCents'
          : l.functionalClass === 'mgmt_general'
            ? 'mgmtGeneralCents'
            : 'fundraisingCents'
      ] += n;
    groups.set(key, row);
  }
  return [...groups.values()];
}
/** Tabular sections share metadata; negative numeric amounts remain numeric in spreadsheets. */
export function reportCsv(value: unknown): string {
  type Cell = string | number | boolean | null;
  const flatten = (
    v: unknown,
    prefix = '',
    out: Record<string, Cell> = {},
  ): Record<string, Cell> => {
    if (v && typeof v === 'object')
      for (const [k, n] of Object.entries(v))
        flatten(n, prefix ? `${prefix}.${k}` : k, out);
    else
      out[prefix] =
        v == null
          ? null
          : typeof v === 'number' || typeof v === 'boolean'
            ? v
            : String(v);
    return out;
  };
  const records: Record<string, Cell>[] = [];
  if (
    value &&
    typeof value === 'object' &&
    !Array.isArray(value) &&
    'report' in value
  ) {
    const meta: Record<string, Cell> = {};
    for (const [k, v] of Object.entries(value))
      if (v === null || typeof v !== 'object') meta[k] = v as Cell;
    for (const [k, v] of Object.entries(value)) {
      if (Array.isArray(v))
        for (const row of v)
          records.push({ ...meta, section: k, ...flatten(row) });
      else if (v && typeof v === 'object')
        records.push({ ...meta, section: k, ...flatten(v) });
    }
    if (!records.length) records.push(meta);
  } else if (Array.isArray(value))
    records.push(...value.map((v) => flatten(v)));
  else records.push(flatten(value));
  const columns = [...new Set(records.flatMap((r) => Object.keys(r)))];
  const cell = (v: Cell | undefined) => {
    let str = String(v ?? '');
    if (typeof v === 'string' && /^[=+@\t\r-]/.test(str)) str = "'" + str;
    return `"${str.replaceAll('"', '""')}"`;
  };
  return (
    [
      columns.map(cell).join(','),
      ...records.map((r) => columns.map((k) => cell(r[k])).join(',')),
    ].join('\r\n') + '\r\n'
  );
}
