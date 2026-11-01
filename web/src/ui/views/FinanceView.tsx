import { useEffect, useState } from 'preact/hooks';
import type { BudgetLine, BudgetsResponse, CurrentUser, LookupsResponse, PurchaseRequestsListQuery } from '../../lib/contracts';
import { FINANCE_REPORTS, OFFICER_FINANCE_REPORTS, type FinanceReport, type PurchaseStatus } from '../../lib/domain';
import { api } from '../../lib/api/client';
import { useApi } from '../../lib/api/useApi';
import { dollarsToCents, formatCents, REPORT_LABEL, STATUS_LABEL } from '../../lib/format';
import { setParams, useSearch } from '../../lib/nav';
import type { FinanceTab } from '../../lib/routes';
import { Drawer } from '../components/Drawer';
import { Icon } from '../components/Icon';
import { DataTable, Section, Tabs } from '../components/kit';
import { Async, Empty, errorMessage, Loading, Notice } from '../components/states';
import { requestColumns } from './columns';
import { BudgetSummary, ORDER, Periods, Report, type Q } from './finance-parts';
import RequestDetail, { type Flash } from './RequestDetail';
import RequestFormView from './RequestForm';

const TAB_LABEL: Record<FinanceTab, string> = { requests: 'requests', budgets: 'budgets', reports: 'reports', periods: 'fiscal periods' };

export default function FinanceView({ me }: { me: CurrentUser }) {
  const director = me.role === 'director';
  const search = useSearch();
  const tabs: FinanceTab[] = ['requests', 'budgets', 'reports', ...(director ? (['periods'] as const) : [])];
  const t = search.get('tab') as FinanceTab | null;
  const tab: FinanceTab = t && tabs.includes(t) ? t : 'requests';

  const lookups = useApi('lookups');
  const currentFy = lookups.status === 'ready' ? lookups.data.currentFiscalYear : null;
  const fyParam = Number(search.get('fy'));
  const fy = Number.isInteger(fyParam) && fyParam > 2000 ? fyParam : currentFy;
  const fundId = search.get('fund') ?? '';
  const query: Q | null = fy ? { fiscalYear: fy, ...(fundId ? { fundId } : {}) } : null;

  // Slide-overs: ?id= request detail, ?new=1 new request (works from any tab).
  const openId = search.get('id');
  const creating = search.get('new') === '1';
  const [flash, setFlash] = useState<Flash>({});
  const [title, setTitle] = useState('request');
  const [listTick, setListTick] = useState(0);
  useEffect(() => setTitle('request'), [openId]);

  return (
    <div class="stack">
      <div class="toolbar">
        <Tabs label="Finance" idPrefix="fin" active={tab} onChange={(k) => setParams({ tab: k === 'requests' ? null : k, report: null }, { replace: true })} tabs={tabs.map((k) => ({ key: k, label: TAB_LABEL[k] }))} />
        <div class="toolbar-actions">
          <a class="btn btn--primary" href={`/finance?${new URLSearchParams({ ...Object.fromEntries(search), new: '1' })}`} data-new-request>
            <Icon name="plus" size={16} /> new request
          </a>
        </div>
      </div>

      <div id="fin-panel" role="tabpanel" aria-labelledby={`fin-tab-${tab}`} class="tab-panel">
        {tab === 'requests' && <RequestsPanel director={director} tick={listTick} />}
        {tab !== 'requests' && (
          <>
            {tab !== 'periods' && <YearFundFilters lookups={lookups.status === 'ready' ? lookups.data : null} fy={fy} fundId={fundId} />}
            {!query ? (
              <Loading rows={6} label="Loading" />
            ) : tab === 'budgets' ? (
              <BudgetsPanel query={query} director={director} lookups={lookups.status === 'ready' ? lookups.data : null} />
            ) : tab === 'reports' ? (
              <ReportsPanel query={query} director={director} active={search.get('report')} />
            ) : (
              <Periods fiscalYear={query.fiscalYear} />
            )}
          </>
        )}
      </div>

      <Drawer
        id="request-panel"
        open={!!openId}
        onClose={() => {
          setFlash({});
          if (openId) setParams({ id: null });
        }}
        title={title}
        eyebrow="purchase request"
        size="lg"
      >
        {openId && (
          <RequestDetail
            key={openId}
            id={openId}
            meId={me.id}
            flash={flash}
            onTitle={setTitle}
            onChanged={() => setListTick((n) => n + 1)}
            onDeleted={() => {
              setListTick((n) => n + 1);
              setParams({ id: null });
            }}
          />
        )}
      </Drawer>

      <Drawer id="new-request" open={creating} onClose={() => creating && setParams({ new: null, eventId: null })} title="new purchase request" size="lg">
        <RequestFormView
          initialEventId={search.get('eventId') ?? undefined}
          onCancel={() => setParams({ new: null, eventId: null })}
          onCreated={(id, f) => {
            setFlash(f);
            setListTick((n) => n + 1);
            // Close the form and open the new request in one history step.
            setParams({ new: null, eventId: null, tab: null, id }, { replace: true });
          }}
        />
      </Drawer>
    </div>
  );
}

function YearFundFilters({ lookups, fy, fundId }: { lookups: LookupsResponse | null; fy: number | null; fundId: string }) {
  const current = lookups?.currentFiscalYear;
  return (
    <div class="filters">
      <div class="field field--inline">
        <label class="field-label" for="fin-fy">
          fiscal year
        </label>
        <select id="fin-fy" class="select" value={fy ?? ''} onChange={(e) => setParams({ fy: Number(e.currentTarget.value) === current ? null : e.currentTarget.value }, { replace: true })}>
          {current &&
            [current, current - 1].map((y) => (
              <option value={y} key={y}>
                FY{y} (Jul {y - 1} – Jun {y})
              </option>
            ))}
        </select>
      </div>
      <div class="field field--inline">
        <label class="field-label" for="fin-fund">
          fund
        </label>
        <select id="fin-fund" class="select" value={fundId} onChange={(e) => setParams({ fund: e.currentTarget.value || null }, { replace: true })}>
          <option value="">all funds</option>
          {lookups?.funds.map((f) => (
            <option value={f.id} key={f.id}>
              {f.name}
            </option>
          ))}
        </select>
      </div>
    </div>
  );
}

// ───────────────────────────── Requests ─────────────────────────────

type Scope = 'mine' | 'all';
type StatusFilter = NonNullable<PurchaseRequestsListQuery['status']> | 'any';

/** Filter chips, in lifecycle order. `pending` = submitted, awaiting a director. */
const FILTERS: { key: StatusFilter; label: string; statuses: PurchaseStatus[] }[] = [
  { key: 'any', label: 'any status', statuses: [] },
  { key: 'pending', label: 'pending', statuses: ['submitted'] },
  { key: 'draft', label: 'drafts', statuses: ['draft'] },
  { key: 'approved', label: 'approved', statuses: ['approved'] },
  { key: 'fulfilling', label: STATUS_LABEL.fulfilling, statuses: ['fulfilling'] },
  { key: 'fulfilled', label: STATUS_LABEL.fulfilled, statuses: ['fulfilled'] },
  { key: 'reimbursed', label: 'awaiting payment', statuses: ['reimbursed'] },
  { key: 'denied', label: 'denied', statuses: ['denied'] },
  { key: 'closed', label: 'closed', statuses: ['closed'] },
  { key: 'cancelled', label: 'cancelled', statuses: ['cancelled'] },
];

function RequestsPanel({ director, tick }: { director: boolean; tick: number }) {
  const search = useSearch();
  // Directors land on everyone's requests (they approve them); officers on their own.
  const scope: Scope = (search.get('scope') as Scope | null) ?? (director ? 'all' : 'mine');
  const s = search.get('status') as StatusFilter | null;
  const status: StatusFilter = s && FILTERS.some((f) => f.key === s) ? s : 'any';
  const [q, setQ] = useState('');
  const [debounced, setDebounced] = useState('');
  useEffect(() => {
    const t = setTimeout(() => setDebounced(q.trim()), 250);
    return () => clearTimeout(t);
  }, [q]);

  const state = useApi('requestsList', { query: { scope, status: status === 'any' ? undefined : status, q: debounced || undefined, limit: 100 } }, { scope, status, debounced, tick });
  const counts = state.status === 'ready' ? state.data.counts : {};
  const countFor = (f: (typeof FILTERS)[number]) =>
    f.key === 'any' ? Object.values(counts).reduce((a, b) => a + (b ?? 0), 0) : f.statuses.reduce((a, st) => a + (counts[st] ?? 0), 0);
  const visibleFilters = FILTERS.filter((f) => f.key === 'any' || f.key === status || countFor(f) > 0);
  const setStatus = (k: StatusFilter) => setParams({ status: k === 'any' ? null : k }, { replace: true });

  return (
    <div class="stack">
      <div class="toolbar toolbar--wrap">
        <div class="segmented" role="group" aria-label="Whose requests">
          {(['mine', 'all'] as const).map((k) => (
            <button type="button" class="segment" key={k} aria-pressed={scope === k} onClick={() => setParams({ scope: k }, { replace: true })}>
              {k === 'mine' ? 'mine' : director ? 'all officers' : 'everyone'}
            </button>
          ))}
        </div>
        <div class="toolbar-search">
          <label class="visually-hidden" for="req-search">
            Search requests
          </label>
          <input id="req-search" class="input" type="search" placeholder="search title, vendor or #" value={q} onInput={(e) => setQ(e.currentTarget.value)} />
        </div>
      </div>
      <div class="chips" role="group" aria-label="Filter by status">
        {visibleFilters.map((f) => (
          <button type="button" class="chip" aria-pressed={status === f.key} onClick={() => setStatus(f.key)} key={f.key}>
            {f.label}
            {state.status === 'ready' && <span class="chip-count">{countFor(f)}</span>}
          </button>
        ))}
      </div>

      <Async state={state} loading={<Loading rows={6} label="Loading requests" />}>
        {(d) =>
          d.items.length === 0 ? (
            status !== 'any' || debounced ? (
              <Empty
                title="Nothing matches"
                action={
                  <button
                    type="button"
                    class="linkish"
                    onClick={() => {
                      setStatus('any');
                      setQ('');
                    }}
                  >
                    clear filters
                  </button>
                }
              >
                No {status === 'any' ? '' : FILTERS.find((f) => f.key === status)?.label} requests{debounced ? ` match “${debounced}”` : ''}.
              </Empty>
            ) : scope === 'mine' ? (
              <Empty title="You haven't asked for anything yet" action={<a class="btn btn--primary" href="/finance?new=1">new purchase request</a>}>
                Need supplies, food or a reimbursement? Submit a request and a director will review it.
              </Empty>
            ) : (
              <Empty title="No requests yet">Purchase requests from every officer show here.</Empty>
            )
          ) : (
            <>
              <DataTable rows={d.items} columns={requestColumns({ showRequester: scope === 'all' })} rowKey={(r) => r.id} caption="Purchase requests" />
              <p class="muted small">
                {d.items.length} request{d.items.length === 1 ? '' : 's'} · {formatCents(d.items.reduce((sum, r) => sum + r.totalCents, 0))} total
              </p>
            </>
          )
        }
      </Async>
    </div>
  );
}

// ───────────────────────────── Budgets ─────────────────────────────

function BudgetsPanel({ query, director, lookups }: { query: Q; director: boolean; lookups: LookupsResponse | null }) {
  const lines = useApi('budgetsGet', { query: { fiscalYear: query.fiscalYear } });
  return (
    <div class="stack">
      <BudgetSummary query={query} onOpen={(report) => setParams({ tab: 'reports', report }, {})} />
      <Section title={`budget lines · FY${query.fiscalYear}`} id="budget-lines">
        <Async state={lines} loading={<Loading rows={5} label="Loading budget lines" />}>
          {(d) =>
            director && lookups ? (
              <BudgetEditor data={d} lookups={lookups} onSaved={(next) => lines.set(next)} key={`${d.fiscalYear}-${d.lines.length}`} />
            ) : d.lines.length === 0 ? (
              <Empty title="No budget set">A director sets the year’s budget by category.</Empty>
            ) : (
              <DataTable
                caption="Budget lines"
                rows={d.lines}
                rowKey={(l) => l.id}
                columns={[
                  { key: 'a', label: 'category', primary: true, render: (l) => <><span class="mono muted">{l.account.code}</span> {l.account.name}</> },
                  { key: 'f', label: 'fund', render: (l) => l.fund?.name ?? 'all funds' },
                  { key: 'n', label: 'notes', hideOnPhone: true, render: (l) => <span class="small">{l.notes ?? ''}</span> },
                  { key: 'm', label: 'amount', num: true, render: (l) => formatCents(l.amountCents) },
                ]}
              />
            )
          }
        </Async>
      </Section>
    </div>
  );
}

type Draft = { key: string; accountId: string; fundId: string; amount: string; notes: string };
const toDraft = (l: BudgetLine, i: number): Draft => ({ key: `${l.id}-${i}`, accountId: l.account.id, fundId: l.fund?.id ?? '', amount: (l.amountCents / 100).toFixed(2), notes: l.notes ?? '' });

/** Director: edit the whole fiscal year's budget (PUT replaces every line). */
function BudgetEditor({ data, lookups, onSaved }: { data: BudgetsResponse; lookups: LookupsResponse; onSaved: (d: BudgetsResponse) => void }) {
  const [rows, setRows] = useState<Draft[]>(() => data.lines.map(toDraft));
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<{ tone: 'positive' | 'negative'; text: string } | null>(null);
  const expense = lookups.expenseAccounts.filter((a) => a.type === 'expense');
  const set = (key: string, patch: Partial<Draft>) => setRows((rs) => rs.map((r) => (r.key === key ? { ...r, ...patch } : r)));
  const total = rows.reduce((s, r) => s + (dollarsToCents(r.amount) || 0), 0);
  const dirty = JSON.stringify(rows.map(({ key: _k, ...r }) => r)) !== JSON.stringify(data.lines.map(toDraft).map(({ key: _k, ...r }) => r));

  const save = async () => {
    const e: Record<string, string> = {};
    const seen = new Set<string>();
    for (const r of rows) {
      const c = dollarsToCents(r.amount);
      if (!r.accountId) e[r.key] = 'Pick a category.';
      else if (c === null || Number.isNaN(c)) e[r.key] = 'Enter an amount, like 500.00.';
      else if (seen.has(`${r.accountId}|${r.fundId}`)) e[r.key] = 'This category and fund already has a line.';
      seen.add(`${r.accountId}|${r.fundId}`);
    }
    setErrors(e);
    if (Object.keys(e).length) return;
    if (!confirm(`Save the FY${data.fiscalYear} budget (${formatCents(total)} across ${rows.length} line${rows.length === 1 ? '' : 's'})? This replaces the year’s budget.`)) return;
    setBusy(true);
    setNotice(null);
    try {
      const next = await api('budgetsPut', {
        body: {
          fiscalYear: data.fiscalYear,
          lines: rows.map((r) => ({ accountId: r.accountId, fundId: r.fundId || null, amountCents: dollarsToCents(r.amount)!, notes: r.notes.trim() || null })),
        },
      });
      onSaved(next);
      setNotice({ tone: 'positive', text: `FY${data.fiscalYear} budget saved.` });
    } catch (err) {
      setNotice({ tone: 'negative', text: errorMessage(err) });
    } finally {
      setBusy(false);
    }
  };

  return (
    <div class="stack" data-budget-editor>
      {notice && <Notice tone={notice.tone}>{notice.text}</Notice>}
      {rows.length === 0 && <Empty title="No budget set">Add a line per category to set the year’s budget.</Empty>}
      {rows.length > 0 && (
        <div class="table-wrap">
          <table class="dt dt--edit">
            <caption class="visually-hidden">Budget lines (editable)</caption>
            <thead>
              <tr>
                <th scope="col">category</th>
                <th scope="col">fund</th>
                <th scope="col" class="num">
                  amount
                </th>
                <th scope="col">notes</th>
                <th scope="col">
                  <span class="visually-hidden">remove</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r, i) => (
                <tr key={r.key} class={errors[r.key] ? 'has-error' : undefined}>
                  <td class="dt-primary" data-label="category">
                    <label class="visually-hidden" for={`bl-a-${i}`}>
                      Category, line {i + 1}
                    </label>
                    <select id={`bl-a-${i}`} class="select" value={r.accountId} onChange={(e) => set(r.key, { accountId: e.currentTarget.value })} aria-invalid={errors[r.key] ? true : undefined} aria-describedby={errors[r.key] ? `bl-err-${i}` : undefined}>
                      <option value="">choose…</option>
                      {expense.map((a) => (
                        <option value={a.id} key={a.id}>
                          {a.code} · {a.name}
                        </option>
                      ))}
                    </select>
                    {errors[r.key] && (
                      <p class="field-error" id={`bl-err-${i}`}>
                        {errors[r.key]}
                      </p>
                    )}
                  </td>
                  <td data-label="fund">
                    <label class="visually-hidden" for={`bl-f-${i}`}>
                      Fund, line {i + 1}
                    </label>
                    <select id={`bl-f-${i}`} class="select" value={r.fundId} onChange={(e) => set(r.key, { fundId: e.currentTarget.value })}>
                      <option value="">all funds</option>
                      {lookups.funds.map((f) => (
                        <option value={f.id} key={f.id}>
                          {f.name}
                        </option>
                      ))}
                    </select>
                  </td>
                  <td class="num" data-label="amount">
                    <label class="visually-hidden" for={`bl-m-${i}`}>
                      Amount, line {i + 1}
                    </label>
                    <span class="money">
                      <input id={`bl-m-${i}`} class="input input--money" inputMode="decimal" value={r.amount} onInput={(e) => set(r.key, { amount: e.currentTarget.value })} />
                    </span>
                  </td>
                  <td data-label="notes">
                    <label class="visually-hidden" for={`bl-n-${i}`}>
                      Notes, line {i + 1}
                    </label>
                    <input id={`bl-n-${i}`} class="input" value={r.notes} maxLength={500} onInput={(e) => set(r.key, { notes: e.currentTarget.value })} />
                  </td>
                  <td data-label="">
                    <button type="button" class="linkish linkish--danger" onClick={() => setRows(rows.filter((x) => x.key !== r.key))}>
                      remove
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
            <tfoot>
              <tr>
                <td class="dt-primary">total</td>
                <td />
                <td class="num" data-label="total">
                  {formatCents(total)}
                </td>
                <td colSpan={2} />
              </tr>
            </tfoot>
          </table>
        </div>
      )}
      <div class="form-actions">
        <button type="button" class="btn" onClick={() => setRows([...rows, { key: `new-${Date.now()}`, accountId: '', fundId: '', amount: '0.00', notes: '' }])}>
          <Icon name="plus" size={16} /> add line
        </button>
        <button type="button" class="btn btn--primary" disabled={busy || !dirty} onClick={() => void save()}>
          {busy ? 'saving…' : 'save budget'}
        </button>
        {dirty && !busy && (
          <button type="button" class="linkish" onClick={() => setRows(data.lines.map(toDraft))}>
            discard changes
          </button>
        )}
      </div>
    </div>
  );
}

// ───────────────────────────── Reports ─────────────────────────────

function ReportsPanel({ query, director, active }: { query: Q; director: boolean; active: string | null }) {
  const allowed: FinanceReport[] = director ? [...FINANCE_REPORTS].sort((a, b) => ORDER.indexOf(a) - ORDER.indexOf(b)) : [...OFFICER_FINANCE_REPORTS];
  const report: FinanceReport = active && (allowed as string[]).includes(active) ? (active as FinanceReport) : allowed[0]!;
  return (
    <div class="stack">
      <div class="chips" role="group" aria-label="Report">
        {allowed.map((r) => (
          <button type="button" class="chip" key={r} aria-pressed={report === r} onClick={() => setParams({ report: r }, { replace: true })}>
            {REPORT_LABEL[r]}
          </button>
        ))}
      </div>
      <Report name={report} query={query} key={report} />
      {!director && <p class="muted small">The trial balance and financial statements are for directors. Ask one if you need them.</p>}
    </div>
  );
}
