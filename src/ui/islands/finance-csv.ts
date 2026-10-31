import type { FinanceReportResponse } from '../../core/contracts';

/**
 * Client-side CSV of a report, used for "export CSV" while the mocks are on. With the real
 * backend the button links to /api/finance/reports/:report?format=csv instead.
 */
export function reportToCsv(r: FinanceReportResponse): string {
  const d = (c: number) => (c / 100).toFixed(2);
  const acct = (a: { code: string; name: string; form990Line: string | null }) => [a.code, a.name, a.form990Line ?? ''];
  let rows: (string | number)[][] = [];
  const loose = r as unknown as { report: string; rows?: { part: string; line: string; totalCents: number; programCents: number; mgmtGeneralCents: number; fundraisingCents: number }[] };
  if (loose.report === 'form-990') {
    rows = [['part', 'line', 'total', 'program', 'mgmt_general', 'fundraising'], ...(loose.rows ?? []).map((x) => [x.part, x.line, d(x.totalCents), d(x.programCents), d(x.mgmtGeneralCents), d(x.fundraisingCents)])];
  } else switch (r.report) {
    case 'trial-balance':
      rows = [['code', 'account', 'form990', 'debit', 'credit'], ...r.rows.map((x) => [...acct(x.account), d(x.debitCents), d(x.creditCents)]), ['', 'total', '', d(r.totals.debitCents), d(r.totals.creditCents)]];
      break;
    case 'budget-vs-actual':
      rows = [
        ['code', 'account', 'form990', 'fund', 'budget', 'actual', 'committed', 'variance'],
        ...r.rows.map((x) => [...acct(x.account), x.fund?.code ?? '', d(x.budgetCents), d(x.actualCents), d(x.committedCents), d(x.varianceCents)]),
      ];
      break;
    case 'fund-balances':
      rows = [
        ['fund', 'restriction', 'beginning', 'additions', 'releases', 'expenses', 'ending'],
        ...r.rows.map((x) => [x.fund.code, x.fund.restriction, d(x.beginningCents), d(x.additionsCents), d(x.releasesCents), d(x.expensesCents), d(x.endingCents)]),
      ];
      break;
    case 'statement-of-activities':
      rows = [
        ['section', 'code', 'account', 'form990', 'without_donor', 'with_donor', 'total'],
        ...r.revenue.map((x) => ['revenue', ...acct(x.account), d(x.withoutDonorCents), d(x.withDonorCents), d(x.totalCents)]),
        ...r.expenses.map((x) => ['expense', ...acct(x.account), d(x.withoutDonorCents), d(x.withDonorCents), d(x.totalCents)]),
        ['releases', '', '', '', d(r.releasesCents), d(-r.releasesCents), '0.00'],
        ['change_in_net_assets', '', '', '', d(r.changeInNetAssets.withoutDonorCents), d(r.changeInNetAssets.withDonorCents), d(r.changeInNetAssets.totalCents)],
      ];
      break;
    case 'statement-of-financial-position':
      rows = [
        ['section', 'code', 'account', 'form990', 'balance'],
        ...r.assets.map((x) => ['asset', ...acct(x.account), d(x.balanceCents)]),
        ...r.liabilities.map((x) => ['liability', ...acct(x.account), d(x.balanceCents)]),
        ['net_assets_without_donor', '', '', '', d(r.netAssets.withoutDonorCents)],
        ['net_assets_with_donor', '', '', '', d(r.netAssets.withDonorCents)],
      ];
      break;
    case 'statement-of-functional-expenses':
      rows = [
        ['code', 'account', 'form990', 'program', 'mgmt_general', 'fundraising', 'total'],
        ...r.rows.map((x) => [...acct(x.account), d(x.programCents), d(x.mgmtGeneralCents), d(x.fundraisingCents), d(x.totalCents)]),
      ];
      break;
  }
  const esc = (v: string | number) => {
    const s = String(v);
    return /[",\n]/.test(s) ? `"${s.replaceAll('"', '""')}"` : s;
  };
  return rows.map((row) => row.map(esc).join(',')).join('\n') + '\n';
}
