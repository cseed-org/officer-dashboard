import { expect, it } from 'vitest';
import {
  makeReport,
  form990Rollup,
  reportCsv,
  type ReportData,
} from '../../../src/core/accounting/reports';
import { FinanceReportResponse } from '../../../src/core/contracts';
const accounts: ReportData['accounts'] = [
  {
    id: 'cash',
    code: '1000',
    name: 'Cash',
    type: 'asset',
    form990Part: 'X',
    form990Line: '1',
  },
  {
    id: 'net',
    restriction: 'without_donor',
    code: '3000',
    name: 'Net assets',
    type: 'net_asset',
    form990Part: 'X',
    form990Line: '27',
  },
  {
    id: 'rev',
    code: '4000',
    name: 'Gifts',
    type: 'revenue',
    form990Part: 'VIII',
    form990Line: '1f',
  },
  {
    id: 'exp',
    code: '5100',
    name: 'Supplies',
    type: 'expense',
    form990Part: 'IX',
    form990Line: '24a',
  },
];
accounts.push({
  id: 'netr',
  code: '3100',
  name: 'Restricted net assets',
  type: 'net_asset',
  restriction: 'with_donor',
  form990Part: 'X',
  form990Line: '28',
});
const data: ReportData = {
  accounts,
  funds: [
    {
      id: 'g',
      code: 'GEN',
      name: 'General',
      kind: 'general',
      restriction: 'without_donor',
    },
    {
      id: 'r',
      code: 'R',
      name: 'Grant',
      kind: 'grant',
      restriction: 'with_donor',
    },
  ],
  budgets: [
    { accountId: 'exp', fundId: null, fiscalYear: 2027, amountCents: 500 },
  ],
  commitments: [],
  lines: [],
};
function pair(
  date: string,
  source: string,
  a: string,
  af: string,
  b: string,
  bf: string,
  n: number,
) {
  data.lines.push(
    {
      entryDate: date,
      source,
      accountId: a,
      fundId: af,
      debitCents: n,
      creditCents: 0,
      functionalClass: a === 'exp' ? 'program' : null,
    },
    {
      entryDate: date,
      source,
      accountId: b,
      fundId: bf,
      debitCents: 0,
      creditCents: n,
      functionalClass: b === 'exp' ? 'program' : null,
    },
  );
}
pair('2026-06-01', 'opening_balance', 'cash', 'g', 'net', 'g', 100);
pair('2026-08-01', 'contribution', 'cash', 'r', 'rev', 'r', 1000);
pair('2026-08-02', 'release', 'netr', 'r', 'net', 'r', 200);

pair('2026-08-03', 'purchase_request', 'exp', 'r', 'cash', 'r', 120);
pair('2026-08-04', 'reversal', 'cash', 'r', 'exp', 'r', 20);
const q = { fiscalYear: 2027, asOf: '2026-10-31' };
it('ties financial statements across opening balances, restriction releases and reversals', () => {
  const position = makeReport('statement-of-financial-position', data, q);
  const activity = makeReport('statement-of-activities', data, q);
  const tb = makeReport('trial-balance', data, q);
  if (
    position.report !== 'statement-of-financial-position' ||
    activity.report !== 'statement-of-activities' ||
    tb.report !== 'trial-balance'
  )
    throw new Error('wrong report');
  expect(position.totals).toEqual({
    assetsCents: 1000,
    liabilitiesCents: 0,
    netAssetsCents: 1000,
    balanced: true,
  });
  expect(activity.changeInNetAssets).toEqual({
    withoutDonorCents: 100,
    withDonorCents: 800,
    totalCents: 900,
  });
  expect(
    activity.netAssetsEnding.totalCents -
      activity.netAssetsBeginning.totalCents,
  ).toBe(activity.changeInNetAssets.totalCents);
  expect(activity.releasesCents).toBe(200);
  expect(tb.totals.debitCents).toBe(tb.totals.creditCents);
  for (const report of [
    'fund-balances',
    'budget-vs-actual',
    'statement-of-functional-expenses',
    'form-990',
  ] as const)
    expect(
      FinanceReportResponse.safeParse(makeReport(report, data, q)).success,
      report,
    ).toBe(true);
  const balances = makeReport('fund-balances', data, q);
  if (balances.report !== 'fund-balances') throw new Error();
  for (const row of balances.rows)
    expect(row.beginningCents + row.additionsCents - row.expensesCents).toBe(
      row.endingCents,
    );
  expect(form990Rollup(data, q).rows.find((r) => r.part === 'IX')).toMatchObject({
    line: '24a',
    totalCents: 100,
    programCents: 100,
  });
  expect(reportCsv({ vendor: '=1+1', memo: 'A,"B"' })).toContain("'=1+1");
});

it('includes unbudgeted fund spending and keeps individual funds balanced after release', () => {
  const budget = makeReport(
    'budget-vs-actual',
    {
      ...data,
      budgets: [
        { accountId: 'exp', fundId: 'g', fiscalYear: 2027, amountCents: 500 },
      ],
    },
    q,
  );
  if (budget.report !== 'budget-vs-actual') throw new Error();
  expect(budget.totals.actualCents).toBe(100);
  for (const fundId of ['g', 'r']) {
    const position = makeReport('statement-of-financial-position', data, {
      ...q,
      fundId,
    });
    if (position.report !== 'statement-of-financial-position')
      throw new Error();
    expect(position.totals.balanced).toBe(true);
  }
});

it('statement of activities foots: revenue − expenses ± releases = change in net assets, per class', () => {
  // Opening balance dated inside the fiscal year (as in the seed) must count as beginning net assets.
  const inYear: ReportData = { ...data, lines: [] };
  const push = (
    date: string,
    source: string,
    dr: string,
    drFund: string,
    cr: string,
    crFund: string,
    n: number,
  ) =>
    inYear.lines.push(
      { entryDate: date, source, accountId: dr, fundId: drFund, debitCents: n, creditCents: 0, functionalClass: dr === 'exp' ? 'program' : null },
      { entryDate: date, source, accountId: cr, fundId: crFund, debitCents: 0, creditCents: n, functionalClass: cr === 'exp' ? 'program' : null },
    );
  push('2026-07-01', 'opening_balance', 'cash', 'g', 'net', 'g', 3200);
  push('2026-08-01', 'contribution', 'cash', 'g', 'rev', 'g', 500);
  push('2026-08-01', 'contribution', 'cash', 'r', 'rev', 'r', 1000);
  push('2026-08-03', 'purchase_request', 'exp', 'r', 'cash', 'r', 300);
  push('2026-08-03', 'release', 'netr', 'r', 'net', 'r', 300);
  push('2026-08-05', 'purchase_request', 'exp', 'g', 'cash', 'g', 120);
  const activity = makeReport('statement-of-activities', inYear, q);
  if (activity.report !== 'statement-of-activities') throw new Error();
  const total = (rows: { withoutDonorCents: number; withDonorCents: number }[]) => ({
    without: rows.reduce((a, r) => a + r.withoutDonorCents, 0),
    with: rows.reduce((a, r) => a + r.withDonorCents, 0),
  });
  const rev = total(activity.revenue);
  const exp = total(activity.expenses);
  // Known-good: unrestricted 500 − 420 + 300 = 380; restricted 1000 − 300 = 700.
  expect(activity.changeInNetAssets).toEqual({ withoutDonorCents: 380, withDonorCents: 700, totalCents: 1080 });
  expect(activity.netAssetsBeginning).toEqual({ withoutDonorCents: 3200, withDonorCents: 0, totalCents: 3200 });
  expect(rev.without - exp.without + activity.releasesCents).toBe(activity.changeInNetAssets.withoutDonorCents);
  expect(rev.with - exp.with - activity.releasesCents).toBe(activity.changeInNetAssets.withDonorCents);
  expect(rev.without + rev.with - exp.without - exp.with).toBe(activity.changeInNetAssets.totalCents);
  const balances = makeReport('fund-balances', inYear, q);
  if (balances.report !== 'fund-balances') throw new Error();
  const general = balances.rows.find((r) => r.fund.id === 'g')!;
  expect(general).toMatchObject({ beginningCents: 3200, additionsCents: 500, expensesCents: 120, endingCents: 3580 });
});

it('Form 990 rollup totals Part VIII line 12 and Part IX line 25, memos noncash 1g and flags unmapped accounts', () => {
  const d: ReportData = {
    ...data,
    accounts: [
      ...accounts,
      { id: 'inkind', code: '4400', name: 'In-kind', type: 'revenue', form990Part: 'VIII', form990Line: '1f' },
      { id: 'misc', code: '5990', name: 'Unmapped misc', type: 'expense', form990Part: null, form990Line: null },
      { id: 'other', code: '4900', name: 'Unmapped revenue', type: 'revenue', form990Part: null, form990Line: null },
    ],
    lines: [],
  };
  const line = (accountId: string, dr: number, cr: number, extra: Partial<ReportData['lines'][number]> = {}) =>
    d.lines.push({ entryDate: '2026-08-01', source: 'contribution', accountId, fundId: 'g', debitCents: dr, creditCents: cr, functionalClass: null, ...extra });
  line('cash', 3200, 0, { source: 'opening_balance', entryDate: '2026-07-01' });
  line('net', 0, 3200, { source: 'opening_balance', entryDate: '2026-07-01' });
  line('cash', 1000, 0);
  line('rev', 0, 1000);
  // Donated supplies, valued at $3.00, used in programs.
  line('exp', 300, 0, { source: 'contribution', functionalClass: 'program', noncash: true });
  line('inkind', 0, 300, { noncash: true });
  line('misc', 50, 0, { source: 'manual', functionalClass: 'mgmt_general' });
  line('cash', 0, 50, { source: 'manual' });
  const r = makeReport('form-990', d, q);
  if (r.report !== 'form-990') throw new Error();
  expect(FinanceReportResponse.safeParse(r).success).toBe(true);
  expect(r.rows).toEqual(
    expect.arrayContaining([
      { part: 'VIII', line: '1f', totalCents: 1300, programCents: 0, mgmtGeneralCents: 0, fundraisingCents: 0 },
      { part: 'IX', line: '24a', totalCents: 300, programCents: 300, mgmtGeneralCents: 0, fundraisingCents: 0 },
    ]),
  );
  expect(r.totals).toEqual({
    partVIIILine12Cents: 1300,
    partIXLine25: { totalCents: 300, programCents: 300, mgmtGeneralCents: 0, fundraisingCents: 0 },
  });
  expect(r.line1gNoncashCents).toBe(300);
  expect(r.unmappedAccounts).toEqual([
    { account: expect.objectContaining({ code: '4900' }), totalCents: 0 },
    { account: expect.objectContaining({ code: '5990' }), totalCents: 50 },
  ]);
});

it('treats a reversal of an opening balance as beginning net assets too', () => {
  const d: ReportData = { ...data, lines: [] };
  const l = (accountId: string, dr: number, cr: number, source: string, reversesSource: string | null = null) =>
    d.lines.push({ entryDate: '2026-07-02', source, reversesSource, accountId, fundId: 'g', debitCents: dr, creditCents: cr, functionalClass: null });
  l('cash', 3200, 0, 'opening_balance');
  l('net', 0, 3200, 'opening_balance');
  l('cash', 0, 200, 'reversal', 'opening_balance');
  l('net', 200, 0, 'reversal', 'opening_balance');
  const a = makeReport('statement-of-activities', d, q);
  if (a.report !== 'statement-of-activities') throw new Error();
  expect(a.netAssetsBeginning.totalCents).toBe(3000);
  expect(a.changeInNetAssets.totalCents).toBe(0);
});
