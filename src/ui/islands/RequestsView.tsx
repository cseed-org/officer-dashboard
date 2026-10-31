import { useEffect, useState } from 'preact/hooks';
import type { PurchaseRequestsListQuery } from '../../core/contracts';
import type { PurchaseStatus } from '../../core/domain';
import { useApi } from '../api/useApi';
import { DataTable, Tabs } from '../components/kit';
import { Async, Empty, Loading } from '../components/states';
import { formatCents, STATUS_LABEL } from '../format';
import { requestColumns } from './EventDetailView';

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

export default function RequestsView({ initialScope, initialStatus, isDirector }: { initialScope: Scope; initialStatus: StatusFilter; isDirector: boolean }) {
  const [scope, setScope] = useState<Scope>(initialScope);
  const [status, setStatus] = useState<StatusFilter>(initialStatus);
  const [q, setQ] = useState('');
  const [debounced, setDebounced] = useState('');
  useEffect(() => {
    const t = setTimeout(() => setDebounced(q.trim()), 250);
    return () => clearTimeout(t);
  }, [q]);
  useEffect(() => {
    const url = new URL(location.href);
    url.searchParams.set('scope', scope);
    status === 'any' ? url.searchParams.delete('status') : url.searchParams.set('status', status);
    history.replaceState(null, '', url);
  }, [scope, status]);

  const state = useApi('requestsList', { query: { scope, status: status === 'any' ? undefined : status, q: debounced || undefined, limit: 100 } });
  const counts = state.status === 'ready' ? state.data.counts : {};
  const countFor = (f: (typeof FILTERS)[number]) =>
    f.key === 'any' ? Object.values(counts).reduce((a, b) => a + (b ?? 0), 0) : f.statuses.reduce((a, s) => a + (counts[s] ?? 0), 0);
  const visibleFilters = FILTERS.filter((f) => f.key === 'any' || f.key === status || countFor(f) > 0);

  return (
    <div>
      <Tabs
        label="Whose requests"
        idPrefix="req"
        active={scope}
        onChange={(s) => setScope(s)}
        tabs={[
          { key: 'mine', label: 'mine' },
          { key: 'all', label: isDirector ? 'all officers' : 'everyone' },
        ]}
      />
      <div id="req-panel" role="tabpanel" aria-labelledby={`req-tab-${scope}`} style={{ paddingTop: 'var(--space-5)' }}>
        <div class="page-tools">
          <div class="page-tools-main">
            <div class="chips" role="group" aria-label="Filter by status">
              {visibleFilters.map((f) => (
                <button type="button" class="chip" aria-pressed={status === f.key} onClick={() => setStatus(f.key)} key={f.key}>
                  {f.label}
                  {state.status === 'ready' && <span class="tab-count">{countFor(f)}</span>}
                </button>
              ))}
            </div>
            <div class="field toolbar-search">
              <label class="visually-hidden" for="req-search">
                Search requests
              </label>
              <input id="req-search" class="input" type="search" placeholder="search title, vendor or #" value={q} onInput={(e) => setQ(e.currentTarget.value)} />
            </div>
          </div>
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
                <Empty title="You haven't asked for anything yet" action={<a class="btn btn--highlight" href="/requests/new">new purchase request</a>}>
                  Need supplies, food or a reimbursement? Submit a request and a director will review it.
                </Empty>
              ) : (
                <Empty title="No requests yet">Purchase requests from every officer will show here.</Empty>
              )
            ) : (
              <>
                <DataTable rows={d.items} columns={requestColumns({ showRequester: scope === 'all' })} rowKey={(r) => r.id} caption="Purchase requests" />
                <p class="muted small" style={{ marginTop: 'var(--space-3)' }}>
                  {d.items.length} request{d.items.length === 1 ? '' : 's'} · {formatCents(d.items.reduce((s, r) => s + r.totalCents, 0))} total
                </p>
              </>
            )
          }
        </Async>
      </div>
    </div>
  );
}
