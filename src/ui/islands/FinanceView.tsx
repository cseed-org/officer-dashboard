import type { ComponentChildren } from 'preact';
import { useEffect, useState } from 'preact/hooks';
import type { FinancePeriod, FinanceReportResponse, FinanceSummaryResponse } from '../../core/contracts';
import { FINANCE_REPORTS, OFFICER_FINANCE_REPORTS, type FinanceReport } from '../../core/domain';
import { api, endpointUrl } from '../api/client';
import { pacificDate } from '../../core/luma/time';
import { useApi } from '../api/useApi';
import { BudgetBars } from '../components/charts';
import { Badge, DataTable, Kpi, Section, Tabs } from '../components/kit';
import { Async, Empty, errorMessage, Loading, Notice } from '../components/states';
import { dollars, fmtDate, fmtDateTime, formatCents, FUND_KIND_LABEL, pct, REPORT_LABEL, RESTRICTION_LABEL } from '../format';

type TabKey = 'summary' | FinanceReport | 'periods';

export default function FinanceView({ isDirector, fiscalYear, initialTab }: { isDirector: boolean; fiscalYear: number; initialTab?: string | null }) {
  const allowed: FinanceReport[] = isDirector ? [...FINANCE_REPORTS].sort((a, b) => order(a) - order(b)) : [...OFFICER_FINANCE_REPORTS];
  const tabs: TabKey[] = ['summary', ...allowed, ...(isDirector ? (['periods'] as const) : [])];
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

      <Tabs label="Finance views" idPrefix="fin" active={tab} onChange={setTab} tabs={tabs.map((k) => ({ key: k, label: k === 'summary' || k === 'periods' ? k : REPORT_LABEL[k] }))} />
      <div id="fin-panel" role="tabpanel" aria-labelledby={`fin-tab-${tab}`} style={{ paddingTop: 'var(--space-6)' }}>
        {tab === 'summary' ? <Summary query={query} isDirector={isDirector} onOpen={setTab} /> : tab === 'periods' ? <Periods fiscalYear={fy} /> : <Report name={tab} query={query} key={tab} />}
      </div>
      {!isDirector && (
        <p class="muted small" style={{ marginTop: 'var(--space-6)' }}>
          The trial balance and financial statements are for directors. Ask one if you need them.
        </p>
      )}
    </div>
  );
}

const ORDER: FinanceReport[] = ['budget-vs-actual', 'fund-balances', 'statement-of-activities', 'statement-of-financial-position', 'statement-of-functional-expenses', 'form-990', 'trial-balance'];
const order = (r: FinanceReport) => ORDER.indexOf(r);

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

const PERIOD_TONE = { open: 'positive', closed: 'neutral', locked: 'attention' } as const;

/** Director-only: create a fiscal year's monthly periods and close or lock them. */
function Periods({ fiscalYear }: { fiscalYear: number }) {
  const state = useApi('periods');
  const today = pacificDate(new Date());
  const [busy, setBusy] = useState<string | null>(null);
  const [notice, setNotice] = useState<{ tone: 'positive' | 'negative'; text: string } | null>(null);
  const run = async (key: string, fn: () => Promise<{ periods: FinancePeriod[] }>, done: string) => {
    setBusy(key);
    try {
      state.set(await fn());
      setNotice({ tone: 'positive', text: done });
    } catch (e) {
      setNotice({ tone: 'negative', text: errorMessage(e) });
    } finally {
      setBusy(null);
    }
  };
  return (
    <Async state={state} loading={<Loading rows={6} label="Loading fiscal periods" />}>
      {(d) => {
        const years = d.periods.map((p) => p.fiscalYear);
        const next = years.length ? Math.max(...years) + 1 : fiscalYear;
        const rowsFor = (fy: number) => d.periods.filter((p) => p.fiscalYear === fy);
        const fys = [...new Set([fiscalYear, ...years])].sort((a, b) => b - a);
        const create = (
          <button
            type="button"
            class="btn btn--small"
            disabled={busy !== null}
            data-create-periods={next}
            onClick={() => {
              if (!confirm(`Create the 12 monthly periods for FY${next} (Jul ${next - 1} – Jun ${next})?`)) return;
              void run('create', () => api('periodsCreate', { body: { fiscalYear: next } }), `FY${next} periods created.`);
            }}
          >
            {busy === 'create' ? 'creating…' : `create FY${next} periods`}
          </button>
        );
        return (
          <div class="stack">
            {notice && <Notice tone={notice.tone}>{notice.text}</Notice>}
            <Section title="fiscal periods" id="periods-h" actions={create}>
              <p class="small muted" style={{ margin: '0 0 var(--space-4)' }}>
                Entries post only into open periods. Closing a month stops new entries; locking makes it final. Neither can be undone.
              </p>
              {fys.map((fy) => (
                <div key={fy} style={{ marginBottom: 'var(--space-6)' }}>
                  {rowsFor(fy).length === 0 ? (
                    <Empty title={`No periods for FY${fy}`}>Nothing can post to this year until its periods exist.</Empty>
                  ) : (
                    <DataTable
                      showCaption caption={`FY${fy} (Jul ${fy - 1} – Jun ${fy})`}
                      rows={rowsFor(fy)}
                      rowKey={(p) => p.id}
                      columns={[
                        { key: 'n', label: 'period', primary: true, render: (p) => <><span class="cell-title">{p.name}</span><span class="cell-sub">{fmtDate(p.startDate)} – {fmtDate(p.endDate)}</span></> },
                        { key: 's', label: 'status', render: (p) => <Badge tone={PERIOD_TONE[p.status]}>{p.status}</Badge> },
                        {
                          key: 'a',
                          label: 'action',
                          render: (p) =>
                            p.status === 'locked' ? (
                              <span class="muted small">final</span>
                            ) : p.endDate >= today ? (
                              <span class="muted small">in progress</span>
                            ) : (
                              <button
                                type="button"
                                class="btn btn--small"
                                disabled={busy !== null}
                                data-period-action={p.status === 'open' ? 'close' : 'lock'}
                                onClick={() => {
                                  const to = p.status === 'open' ? 'closed' : 'locked';
                                  const warn = to === 'closed' ? `Close ${p.name}? No new entries can post to it, and it can’t reopen.` : `Lock ${p.name}? This is final.`;
                                  if (!confirm(warn)) return;
                                  void run(p.id, () => api('periodClose', { params: { id: p.id }, body: { to } }), `${p.name} is ${to}.`);
                                }}
                              >
                                {busy === p.id ? 'saving…' : p.status === 'open' ? 'close' : 'lock'}
                              </button>
                            ),
                        },
                      ]}
                    />
                  )}
                </div>
              ))}
            </Section>
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
    <Async state={state} loading={<Loading rows={8} label={`Loading ${REPORT_LABEL[name]}`} />}>
      {(r: FinanceReportResponse) => (
        <section class="section" aria-labelledby="report-h">
          <div class="section-head">
            <h2 id="report-h">{REPORT_TITLE[name]}</h2>
            <div class="section-actions">
              <a class="btn btn--small" href={csvHref} download={filename}>
                export CSV
              </a>
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

const REPORT_TITLE: Record<FinanceReport, string> = {
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

type Form990 = Extract<FinanceReportResponse, { report: 'form-990' }>;
type Form990Row = Form990['rows'][number];

function Form990Body({ r }: { r: Form990 }) {
  const { rows, totals, line1gNoncashCents, unmappedAccounts } = r;
  if (!rows.length && !unmappedAccounts.length)
    return <Empty title="Nothing to map yet">No revenue or expenses posted for this period.</Empty>;
  const byLine = (a: Form990Row, b: Form990Row) => a.line.localeCompare(b.line, 'en', { numeric: true });
  const viii = rows.filter((x) => x.part === 'VIII').sort(byLine);
  const ix = rows.filter((x) => x.part === 'IX').sort(byLine);
  const l25 = totals.partIXLine25;
  return (
    <div class="stack" style={{ gap: 'var(--space-6)' }}>
      <p class="small muted" style={{ margin: 0 }}>
        Ledger totals grouped by Form 990 line, to hand to the CPA. Not a filing and not tax advice.
      </p>
      {unmappedAccounts.length > 0 && (
        <div class="notice notice--attention" role="status" data-990-unmapped>
          <strong>
            {unmappedAccounts.length === 1 ? '1 account has' : `${unmappedAccounts.length} accounts have`} no Form 990 line
          </strong>{' '}
          and {unmappedAccounts.length === 1 ? 'is' : 'are'} left out of the totals below:{' '}
          {unmappedAccounts.map((u, i) => (
            <span key={u.account.id}>
              {i > 0 && ', '}
              <span class="mono">{u.account.code}</span> {u.account.name} ({formatCents(u.totalCents)})
            </span>
          ))}
          . Map them in the chart of accounts.
        </div>
      )}
      <DataTable
        showCaption caption="Part VIII revenue"
        rows={viii}
        rowKey={(x) => `VIII-${x.line}`}
        columns={[
          { key: 'l', label: 'Part VIII line', primary: true, render: (x) => <span class="mono">VIII {x.line}</span> },
          { key: 't', label: 'total revenue', num: true, render: (x) => money(x.totalCents) },
        ]}
        foot={
          <>
            <tr data-990-line="1g">
              <td class="dt-primary">
                <span class="mono">VIII 1g</span> <span class="muted">noncash contributions (memo, included in 1a–1f)</span>
              </td>
              <td class="num" data-label="total revenue">{money(line1gNoncashCents)}</td>
            </tr>
            <tr data-990-line="12">
              <td class="dt-primary"><span class="mono">VIII 12</span> total revenue</td>
              <td class="num" data-label="total revenue">{money(totals.partVIIILine12Cents)}</td>
            </tr>
          </>
        }
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
          <tr data-990-line="25">
            <td class="dt-primary"><span class="mono">IX 25</span> total functional expenses</td>
            <td class="num" data-label="(A) total">{money(l25.totalCents)}</td>
            <td class="num" data-label="(B) program">{money(l25.programCents)}</td>
            <td class="num" data-label="(C) mgmt & general">{money(l25.mgmtGeneralCents)}</td>
            <td class="num" data-label="(D) fundraising">{money(l25.fundraisingCents)}</td>
          </tr>
        }
      />
    </div>
  );
}

function ReportBody({ r }: { r: FinanceReportResponse }) {
  switch (r.report) {
    case 'form-990':
      return <Form990Body r={r} />;
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
      return (
        <>
        <DataTable
          wide
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
            { key: 'ur', label: 'ending, unrestricted', cardLabel: 'unrestricted', num: true, render: (x) => money(x.unrestrictedEndingCents) },
            { key: 'rr', label: 'ending, restricted', cardLabel: 'restricted', num: true, render: (x) => money(x.restrictedEndingCents) },
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
  }
}
