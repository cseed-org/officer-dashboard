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
  expect(form990Rollup(data, q).find((r) => r.part === 'IX')).toMatchObject({
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
