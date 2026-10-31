import type { ComponentChildren } from 'preact';
import { useEffect, useState } from 'preact/hooks';
import type { FinanceReportResponse, FinanceSummaryResponse } from '../../core/contracts';
import { FINANCE_REPORTS, OFFICER_FINANCE_REPORTS, type FinanceReport } from '../../core/domain';
import { endpointUrl } from '../api/client';
import { useApi } from '../api/useApi';
import { BudgetBars } from '../components/charts';
import { DataTable, Kpi, Section, Tabs } from '../components/kit';
import { Async, Empty, Loading } from '../components/states';
import { UI_MOCKS } from '../mocks/switch';
import { dollars, fmtDate, fmtDateTime, formatCents, FUND_KIND_LABEL, pct, REPORT_LABEL, RESTRICTION_LABEL } from '../format';
import { reportToCsv } from './finance-csv';

type TabKey = 'summary' | FinanceReport;

export default function FinanceView({ isDirector, fiscalYear, initialTab }: { isDirector: boolean; fiscalYear: number; initialTab?: string | null }) {
  const allowed: FinanceReport[] = isDirector ? [...FINANCE_REPORTS].sort((a, b) => order(a) - order(b)) : [...OFFICER_FINANCE_REPORTS];
  const tabs: TabKey[] = ['summary', ...allowed];
  const [tab, setTab] = useState<TabKey>(() => (initialTab && (tabs as string[]).includes(initialTab) ? (initialTab as TabKey) : 'summary'));
  const [fy, setFy] = useState(fiscalYear);
  const [fundId, setFundId] = useState('');
  useEffect(() => {
    const url = new URL(location.href);
    tab === 'summary' ? url.searchParams.delete('tab') : url.searchParams.set('tab', tab);
    history.replaceState(null, '', url);
  }, [tab]);
  const lookups = useApi('lookups');
  const query = { fiscalYear: fy, fundId: fundId || undefined };

  return (
    <div>
      <div class="page-tools">
        <div class="page-tools-main">
          <div class="field">
            <label class="field-label" for="fin-fy">
              fiscal year
            </label>
            <select id="fin-fy" class="select" value={fy} onChange={(e) => setFy(Number(e.currentTarget.value))}>
              {[fiscalYear, fiscalYear - 1].map((y) => (
                <option value={y} key={y}>
                  FY{y} (Jul {y - 1} – Jun {y})
                </option>
              ))}
            </select>
          </div>
          <div class="field">
            <label class="field-label" for="fin-fund">
              fund
            </label>
            <select id="fin-fund" class="select" value={fundId} onChange={(e) => setFundId(e.currentTarget.value)}>
              <option value="">all funds</option>
              {lookups.status === 'ready' &&
                lookups.data.funds.map((f) => (
                  <option value={f.id} key={f.id}>
                    {f.name}
                  </option>
                ))}
            </select>
          </div>
        </div>
      </div>

      <Tabs label="Finance views" idPrefix="fin" active={tab} onChange={setTab} tabs={tabs.map((k) => ({ key: k, label: k === 'summary' ? 'summary' : (REPORT_LABEL[k] ?? k) }))} />
      <div id="fin-panel" role="tabpanel" aria-labelledby={`fin-tab-${tab}`} style={{ paddingTop: 'var(--space-6)' }}>
        {tab === 'summary' ? <Summary query={query} isDirector={isDirector} onOpen={setTab} /> : <Report name={tab} query={query} key={tab} />}
      </div>
      {!isDirector && (
        <p class="muted small" style={{ marginTop: 'var(--space-6)' }}>
          The trial balance and financial statements are for directors. Ask one if you need them.
        </p>
      )}
    </div>
  );
}

const ORDER = ['budget-vs-actual', 'fund-balances', 'statement-of-activities', 'statement-of-financial-position', 'statement-of-functional-expenses', 'form-990', 'trial-balance'];
/** Unknown (newer) reports sort to the end. */
const order = (r: string) => (ORDER.indexOf(r) + ORDER.length + 1) % (ORDER.length + 1);

type Q = { fiscalYear: number; fundId?: string };

function Summary({ query, onOpen }: { query: Q; isDirector: boolean; onOpen: (t: TabKey) => void }) {
  const state = useApi('financeSummary', { query });
  return (
    <Async state={state} loading={<Loading tiles={4} rows={6} label="Loading the budget" />}>
      {(d: FinanceSummaryResponse) => {
        const t = d.totals;
        const used = t.budgetCents ? (t.spentCents + t.committedCents) / t.budgetCents : null;
        return (
          <div class="stack">
            <div class="kpis" style={{ '--kpi-cols': 4 }}>
              <Kpi label="budget" value={dollars(t.budgetCents)} sub={`FY${d.fiscalYear} · as of ${fmtDate(d.asOf)}`} />
              <Kpi label="spent" value={dollars(t.spentCents)} sub="posted to the ledger" />
              <Kpi label="committed" value={dollars(t.committedCents)} sub="approved, not yet paid" />
              <Kpi label="remaining" value={dollars(t.remainingCents)} tone={t.remainingCents < 0 ? 'negative' : undefined} sub={`${pct(used)} used or committed`} />
            </div>
            <div class="cols cols--2">
              <Section title="by category" id="by-category" actions={<button type="button" class="linkish" onClick={() => onOpen('budget-vs-actual')}>full report →</button>}>
                {d.byCategory.length === 0 ? (
                  <Empty title="No budget set">A director sets the year’s budget by category; spending is compared against it here.</Empty>
                ) : (
                  <>
                    <BudgetBars
                      label="Spending against budget by category"
                      rows={d.byCategory.map((c) => ({ key: c.account.id, label: c.account.name, budgetCents: c.budgetCents, spentCents: c.spentCents, committedCents: c.committedCents }))}
                    />
                    <DataTable
                      dense
                      caption="Budget by category"
                      rows={d.byCategory}
                      rowKey={(c) => c.account.id}
                      columns={[
                        { key: 'a', label: 'category', primary: true, render: (c) => <><span class="mono muted">{c.account.code}</span> {c.account.name}</> },
                        { key: 'b', label: 'budget', num: true, render: (c) => formatCents(c.budgetCents) },
                        { key: 's', label: 'spent', num: true, render: (c) => <Signed cents={c.spentCents} /> },
                        { key: 'c', label: 'committed', num: true, render: (c) => formatCents(c.committedCents) },
                        { key: 'r', label: 'remaining', num: true, render: (c) => <Signed cents={c.remainingCents} /> },
                      ]}
                    />
                  </>
                )}
              </Section>
              <Section title="funds" id="funds" actions={<button type="button" class="linkish" onClick={() => onOpen('fund-balances')}>fund balances →</button>}>
                {d.funds.length === 0 ? (
                  <Empty title="No funds">Funds appear once a director sets them up.</Empty>
                ) : (
                  <DataTable
                    caption="Fund balances"
                    rows={d.funds}
                    rowKey={(f) => f.id}
                    columns={[
                      {
                        key: 'n',
                        label: 'fund',
                        primary: true,
                        render: (f) => (
                          <>
                            <span class="cell-title">{f.name}</span>
                            <span class="cell-sub">
                              {FUND_KIND_LABEL[f.kind]} · {RESTRICTION_LABEL[f.restriction]}
                              {f.endDate ? ` · ends ${fmtDate(f.endDate)}` : ''}
                            </span>
                          </>
                        ),
                      },
                      { key: 'aw', label: 'awarded', num: true, render: (f) => (f.awardedCents === null ? '—' : formatCents(f.awardedCents)) },
                      { key: 'b', label: 'balance', num: true, render: (f) => <Signed cents={f.balanceCents} /> },
                    ]}
                  />
                )}
              </Section>
            </div>
          </div>
        );
      }}
    </Async>
  );
}

function Signed({ cents: raw }: { cents: number }) {
  const cents = raw === 0 ? 0 : raw; // no "-$0.00"
  return <span class={cents < 0 ? 'check-bad' : undefined}>{cents < 0 ? `(${formatCents(-cents)})` : formatCents(cents)}</span>;
}

function Report({ name, query }: { name: FinanceReport; query: Q }) {
  const state = useApi('financeReport', { params: { report: name }, query });
  const csvHref = endpointUrl('financeReport', { params: { report: name }, query: { ...query, format: 'csv' } });
  const filename = `cseed-${name}-FY${query.fiscalYear}.csv`;
  return (
    <Async state={state} loading={<Loading rows={8} label={`Loading ${REPORT_LABEL[name] ?? name}`} />}>
      {(r: FinanceReportResponse) => (
        <section class="section" aria-labelledby="report-h">
          <div class="section-head">
            <h2 id="report-h">{REPORT_TITLE[name] ?? REPORT_LABEL[name] ?? name}</h2>
            <div class="section-actions">
              {UI_MOCKS ? (
                <button type="button" class="btn btn--small" onClick={() => download(filename, reportToCsv(r))}>
                  export CSV
                </button>
              ) : (
                <a class="btn btn--small" href={csvHref} download={filename}>
                  export CSV
                </a>
              )}
            </div>
          </div>
          <p class="report-meta">
            <span>
              {fmtDate(r.from)} – {fmtDate(r.asOf)}
            </span>
            <span>generated {fmtDateTime(r.generatedAt)}</span>
            {r.fundId && <span>one fund only</span>}
          </p>
          <ReportBody r={r} />
        </section>
      )}
    </Async>
  );
}

function download(filename: string, text: string) {
  const url = URL.createObjectURL(new Blob([text], { type: 'text/csv' }));
  const a = Object.assign(document.createElement('a'), { href: url, download: filename });
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

const REPORT_TITLE: Record<string, string | undefined> = {
  'form-990': 'form 990 lines (draft)',
  'budget-vs-actual': 'budget vs actual',
  'fund-balances': 'fund balances',
  'trial-balance': 'trial balance',
  'statement-of-activities': 'statement of activities',
  'statement-of-financial-position': 'statement of financial position',
  'statement-of-functional-expenses': 'statement of functional expenses',
};

const acctCell = (a: { code: string; name: string; form990Line: string | null }) => (
  <>
    <span class="mono muted">{a.code}</span> {a.name}
    {a.form990Line && <span class="cell-sub">990 {a.form990Line}</span>}
  </>
);
const money = (c: number) => <Signed cents={c} />;

function Rows({ children }: { children: ComponentChildren }) {
  return <>{children}</>;
}

/**
 * Form990Report from the backend contract update (rows by Part VIII / IX line). Typed here
 * structurally so this page renders it as soon as the backend's contracts.ts lands, without
 * copying that file early.
 */
export interface Form990Row {
  part: 'VIII' | 'IX';
  line: string;
  totalCents: number;
  programCents: number;
  mgmtGeneralCents: number;
  fundraisingCents: number;
}
export const isForm990 = (r: { report: string }): r is { report: 'form-990'; rows: Form990Row[] } => r.report === 'form-990';

function Form990Body({ rows }: { rows: Form990Row[] }) {
  if (!rows.length) return <Empty title="Nothing to map yet">No revenue or expenses posted for this period.</Empty>;
  const byLine = (a: Form990Row, b: Form990Row) => a.line.localeCompare(b.line, 'en', { numeric: true });
  const viii = rows.filter((x) => x.part === 'VIII').sort(byLine);
  const ix = rows.filter((x) => x.part === 'IX').sort(byLine);
  const sum = (rs: Form990Row[], k: keyof Omit<Form990Row, 'part' | 'line'>) => rs.reduce((s, x) => s + x[k], 0);
  return (
    <div class="stack" style={{ gap: 'var(--space-6)' }}>
      <p class="small muted" style={{ margin: 0 }}>
        Ledger totals grouped by Form 990 line, to hand to the CPA. Not a filing and not tax advice.
      </p>
      <DataTable
        showCaption caption="Part VIII revenue"
        rows={viii}
        rowKey={(x) => `VIII-${x.line}`}
        columns={[
          { key: 'l', label: 'Part VIII line', primary: true, render: (x) => <span class="mono">VIII {x.line}</span> },
          { key: 't', label: 'total revenue', num: true, render: (x) => money(x.totalCents) },
        ]}
        foot={<tr><td class="dt-primary">total revenue</td><td class="num" data-label="total revenue">{money(sum(viii, 'totalCents'))}</td></tr>}
      />
      <DataTable
        showCaption caption="Part IX functional expenses"
        rows={ix}
        rowKey={(x) => `IX-${x.line}`}
        columns={[
          { key: 'l', label: 'Part IX line', primary: true, render: (x) => <span class="mono">IX {x.line}</span> },
          { key: 't', label: '(A) total', num: true, render: (x) => money(x.totalCents) },
          { key: 'p', label: '(B) program', num: true, render: (x) => money(x.programCents) },
          { key: 'm', label: '(C) management & general', cardLabel: '(C) mgmt & general', num: true, render: (x) => money(x.mgmtGeneralCents) },
          { key: 'f', label: '(D) fundraising', num: true, render: (x) => money(x.fundraisingCents) },
        ]}
        foot={
          <tr>
            <td class="dt-primary">total functional expenses</td>
            <td class="num" data-label="(A) total">{money(sum(ix, 'totalCents'))}</td>
            <td class="num" data-label="(B) program">{money(sum(ix, 'programCents'))}</td>
            <td class="num" data-label="(C) mgmt & general">{money(sum(ix, 'mgmtGeneralCents'))}</td>
            <td class="num" data-label="(D) fundraising">{money(sum(ix, 'fundraisingCents'))}</td>
          </tr>
        }
      />
    </div>
  );
}

function ReportBody({ r }: { r: FinanceReportResponse }) {
  const loose = r as unknown as { report: string; rows?: unknown };
  if (isForm990(loose)) return <Form990Body rows={loose.rows} />;
  switch (r.report) {
    case 'budget-vs-actual':
      if (!r.rows.length) return <Empty title="No budget lines">There is no budget for this year or fund yet.</Empty>;
      return (
        <DataTable
          caption="Budget vs actual"
          rows={r.rows}
          rowKey={(x) => `${x.account.id}-${x.fund?.id ?? 'all'}`}
          columns={[
            { key: 'a', label: 'category', primary: true, render: (x) => <>{acctCell(x.account)}<span class="cell-sub">{x.fund?.name ?? 'all funds'}</span></> },
            { key: 'b', label: 'budget', num: true, render: (x) => formatCents(x.budgetCents) },
            { key: 'ac', label: 'actual', num: true, render: (x) => money(x.actualCents) },
            { key: 'c', label: 'committed', num: true, render: (x) => formatCents(x.committedCents) },
            { key: 'v', label: 'variance', num: true, render: (x) => money(x.varianceCents) },
            { key: 'u', label: 'used', num: true, render: (x) => <span class={x.usedRate !== null && x.usedRate > 1 ? 'check-bad' : undefined}>{pct(x.usedRate)}</span> },
          ]}
          foot={
            <tr>
              <td class="dt-primary">total</td>
              <td class="num" data-label="budget">{formatCents(r.totals.budgetCents)}</td>
              <td class="num" data-label="actual">{money(r.totals.actualCents)}</td>
              <td class="num" data-label="committed">{formatCents(r.totals.committedCents)}</td>
              <td class="num" data-label="variance">{money(r.totals.varianceCents)}</td>
              <td class="num" data-label="used">{pct(r.totals.budgetCents ? r.totals.actualCents / r.totals.budgetCents : null)}</td>
            </tr>
          }
        />
      );
    case 'fund-balances': {
      if (!r.rows.length) return <Empty title="No funds">Nothing to show for this selection.</Empty>;
      // Backend contract update adds restrictedEndingCents / unrestrictedEndingCents; show them once present.
      type Split = { restrictedEndingCents?: number; unrestrictedEndingCents?: number };
      const split = r.rows.some((x) => typeof (x as Split).restrictedEndingCents === 'number');
      return (
        <>
        <DataTable
          caption="Fund balances"
          rows={r.rows}
          rowKey={(x) => x.fund.id}
          columns={[
            { key: 'f', label: 'fund', primary: true, render: (x) => <><span class="cell-title">{x.fund.name}</span><span class="cell-sub">{RESTRICTION_LABEL[x.fund.restriction]}</span></> },
            { key: 'b', label: 'beginning', num: true, render: (x) => money(x.beginningCents) },
            { key: 'a', label: 'additions', num: true, render: (x) => money(x.additionsCents) },
            { key: 'r', label: 'released (reclass)', cardLabel: 'released', num: true, render: (x) => money(x.releasesCents) },
            { key: 'e', label: 'expenses', num: true, render: (x) => money(x.expensesCents) },
            { key: 'n', label: 'ending', num: true, render: (x) => money(x.endingCents) },
            ...(split
              ? [
                  { key: 'ur', label: 'ending, unrestricted', cardLabel: 'unrestricted', num: true, render: (x: (typeof r.rows)[number]) => money((x as Split).unrestrictedEndingCents ?? 0) },
                  { key: 'rr', label: 'ending, restricted', cardLabel: 'restricted', num: true, render: (x: (typeof r.rows)[number]) => money((x as Split).restrictedEndingCents ?? 0) },
                ]
              : []),
          ]}
        />
        <p class="muted small">
          Released = restricted money reclassified to unrestricted as it was spent on its purpose. It moves between columns and is not
          subtracted again: ending = beginning + additions − expenses.
        </p>
        </>
      );
    }
    case 'trial-balance': {
      if (!r.rows.length) return <Empty title="No postings">No journal entries are posted for this period.</Empty>;
      const ok = r.totals.debitCents === r.totals.creditCents;
      return (
        <>
          <p class={ok ? 'check-ok small' : 'check-bad small'}>{ok ? '✓ debits equal credits' : '✗ debits and credits differ — tell the treasurer'}</p>
          <DataTable
            dense
            caption="Trial balance"
            rows={r.rows}
            rowKey={(x) => x.account.id}
            columns={[
              { key: 'a', label: 'account', primary: true, render: (x) => acctCell(x.account) },
              { key: 'd', label: 'debit', num: true, render: (x) => (x.debitCents ? formatCents(x.debitCents) : '') },
              { key: 'c', label: 'credit', num: true, render: (x) => (x.creditCents ? formatCents(x.creditCents) : '') },
            ]}
            foot={
              <tr>
                <td class="dt-primary">total</td>
                <td class="num" data-label="debit">{formatCents(r.totals.debitCents)}</td>
                <td class="num" data-label="credit">{formatCents(r.totals.creditCents)}</td>
              </tr>
            }
          />
        </>
      );
    }
    case 'statement-of-activities': {
      type Row = (typeof r.revenue)[number];
      const cols = (label: string) => [
        { key: 'a', label, primary: true, render: (x: Row) => acctCell(x.account) },
        { key: 'w', label: 'without donor restrictions', cardLabel: 'unrestricted', num: true, render: (x: Row) => money(x.withoutDonorCents) },
        { key: 'r', label: 'with donor restrictions', cardLabel: 'restricted', num: true, render: (x: Row) => money(x.withDonorCents) },
        { key: 't', label: 'total', num: true, render: (x: Row) => money(x.totalCents) },
      ];
      const line = (label: string, v: { withoutDonorCents: number; withDonorCents: number; totalCents: number }) => (
        <tr class="is-subtotal">
          <td class="dt-primary">{label}</td>
          <td class="num" data-label="unrestricted">{money(v.withoutDonorCents)}</td>
          <td class="num" data-label="restricted">{money(v.withDonorCents)}</td>
          <td class="num" data-label="total">{money(v.totalCents)}</td>
        </tr>
      );
      if (!r.revenue.length && !r.expenses.length) return <Empty title="No activity">No revenue or expenses posted for this period.</Empty>;
      return (
        <div class="stack" style={{ gap: 'var(--space-6)' }}>
          <DataTable showCaption caption="Revenue and support" rows={r.revenue} rowKey={(x) => x.account.id} columns={cols('revenue & support')} />
          <DataTable
            showCaption caption="Expenses"
            rows={r.expenses}
            rowKey={(x) => x.account.id}
            columns={cols('expenses')}
            foot={
              <Rows>
                <tr>
                  <td class="dt-primary">net assets released from restriction</td>
                  <td class="num" data-label="unrestricted">{money(r.releasesCents)}</td>
                  <td class="num" data-label="restricted">{money(-r.releasesCents)}</td>
                  <td class="num" data-label="total">{money(0)}</td>
                </tr>
                {line('change in net assets', r.changeInNetAssets)}
                {line('net assets, beginning', r.netAssetsBeginning)}
                {line('net assets, ending', r.netAssetsEnding)}
              </Rows>
            }
          />
        </div>
      );
    }
    case 'statement-of-financial-position': {
      const t = r.totals;
      if (!r.assets.length && !r.liabilities.length) return <Empty title="Nothing posted">No balances for this period.</Empty>;
      return (
        <div class="stack" style={{ gap: 'var(--space-6)' }}>
          <p class={t.balanced ? 'check-ok small' : 'check-bad small'}>
            {t.balanced ? '✓ assets = liabilities + net assets' : '✗ out of balance — tell the treasurer'}
          </p>
          <DataTable
            showCaption caption="Assets"
            rows={r.assets}
            rowKey={(x) => x.account.id}
            columns={[
              { key: 'a', label: 'assets', primary: true, render: (x) => acctCell(x.account) },
              { key: 'b', label: 'balance', num: true, render: (x) => money(x.balanceCents) },
            ]}
            foot={<tr><td class="dt-primary">total assets</td><td class="num" data-label="balance">{money(t.assetsCents)}</td></tr>}
          />
          <DataTable
            showCaption caption="Liabilities and net assets"
            rows={r.liabilities}
            rowKey={(x) => x.account.id}
            columns={[
              { key: 'a', label: 'liabilities', primary: true, render: (x) => acctCell(x.account) },
              { key: 'b', label: 'balance', num: true, render: (x) => money(x.balanceCents) },
            ]}
            foot={
              <Rows>
                <tr><td class="dt-primary">total liabilities</td><td class="num" data-label="balance">{money(t.liabilitiesCents)}</td></tr>
                <tr><td class="dt-primary">net assets without donor restrictions</td><td class="num" data-label="balance">{money(r.netAssets.withoutDonorCents)}</td></tr>
                <tr><td class="dt-primary">net assets with donor restrictions</td><td class="num" data-label="balance">{money(r.netAssets.withDonorCents)}</td></tr>
                <tr class="is-subtotal"><td class="dt-primary">total liabilities & net assets</td><td class="num" data-label="balance">{money(t.liabilitiesCents + t.netAssetsCents)}</td></tr>
              </Rows>
            }
          />
        </div>
      );
    }
    case 'statement-of-functional-expenses':
      if (!r.rows.length) return <Empty title="No expenses">No expenses posted for this period.</Empty>;
      return (
        <DataTable
          caption="Functional expenses"
          rows={r.rows}
          rowKey={(x) => x.account.id}
          columns={[
            { key: 'a', label: 'expense', primary: true, render: (x) => acctCell(x.account) },
            { key: 'p', label: 'program', num: true, render: (x) => money(x.programCents) },
            { key: 'm', label: 'management & general', cardLabel: 'mgmt & general', num: true, render: (x) => money(x.mgmtGeneralCents) },
            { key: 'f', label: 'fundraising', num: true, render: (x) => money(x.fundraisingCents) },
            { key: 't', label: 'total', num: true, render: (x) => money(x.totalCents) },
          ]}
          foot={
            <tr>
              <td class="dt-primary">total</td>
              <td class="num" data-label="program">{money(r.totals.programCents)}</td>
              <td class="num" data-label="mgmt & general">{formatCents(r.totals.mgmtGeneralCents)}</td>
              <td class="num" data-label="fundraising">{formatCents(r.totals.fundraisingCents)}</td>
              <td class="num" data-label="total">{formatCents(r.totals.totalCents)}</td>
            </tr>
          }
        />
      );
    default:
      return <Empty title="This report has no view yet">Use “export CSV” to get the numbers.</Empty>;
  }
}
