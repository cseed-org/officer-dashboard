import { useState } from 'preact/hooks';
import type { AuditLogEntry, CurrentUser } from '../../lib/contracts';
import { api } from '../../lib/api/client';
import { useApi } from '../../lib/api/useApi';
import { fmtDateTime, relative } from '../../lib/format';
import { setParams, useSearch } from '../../lib/nav';
import type { AdminTab } from '../../lib/routes';
import { DataTable, Tabs } from '../components/kit';
import { Async, Empty, errorMessage, Loading, Notice } from '../components/states';
import AdminTokensView from './AdminTokensView';
import AdminUsersView from './AdminUsersView';
import { auditHref, auditSummary } from './HomeView';

const TABS: { key: AdminTab; label: string }[] = [
  { key: 'users', label: 'officers' },
  { key: 'tokens', label: 'service tokens' },
  { key: 'audit', label: 'audit log' },
];

export default function AdminView({ me }: { me: CurrentUser }) {
  const search = useSearch();
  const t = search.get('tab') as AdminTab | null;
  const tab: AdminTab = t && TABS.some((x) => x.key === t) ? t : 'users';

  if (me.role !== 'director')
    return (
      <div class="state state--error" role="alert" data-state="forbidden">
        <p class="state-title">Directors only</p>
        <p class="state-body">Officers, service tokens and the audit log are managed by directors. Ask one if something needs changing.</p>
        <div class="state-action">
          <a href="/">back home →</a>
        </div>
      </div>
    );

  return (
    <div class="stack">
      <Tabs label="Admin" idPrefix="adm" active={tab} onChange={(k) => setParams({ tab: k === 'users' ? null : k }, { replace: true })} tabs={TABS} />
      <div id="adm-panel" role="tabpanel" aria-labelledby={`adm-tab-${tab}`} class="tab-panel">
        {tab === 'users' ? <AdminUsersView meId={me.id} /> : tab === 'tokens' ? <AdminTokensView /> : <AuditLog />}
      </div>
    </div>
  );
}

function AuditLog() {
  const [entityType, setEntityType] = useState('');
  const first = useApi('adminAudit', { query: { limit: 50, entityType: entityType || undefined } });
  const [more, setMore] = useState<{ items: AuditLogEntry[]; next: string | null; key: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const key = entityType;
  const extra = more?.key === key ? more : null;

  return (
    <div class="stack">
      <div class="toolbar">
        <div class="field field--inline">
          <label class="field-label" for="audit-type">
            show
          </label>
          <select id="audit-type" class="select" value={entityType} onChange={(e) => setEntityType(e.currentTarget.value)}>
            <option value="">everything</option>
            <option value="purchase_request">requests</option>
            <option value="event">events</option>
            <option value="user">officers</option>
            <option value="service_token">tokens</option>
            <option value="budget">budgets</option>
            <option value="fiscal_period">fiscal periods</option>
          </select>
        </div>
      </div>
      <Async state={first} loading={<Loading rows={8} label="Loading the audit log" />}>
        {(d) => {
          const items = [...d.items, ...(extra?.items ?? [])];
          const next = extra ? extra.next : d.nextCursor;
          if (!items.length) return <Empty title="Nothing logged yet">Every change made through the dashboard or a service token is recorded here.</Empty>;
          return (
            <>
              <DataTable
                caption="Audit log"
                rows={items}
                rowKey={(e) => e.id}
                columns={[
                  {
                    key: 'w',
                    label: 'what',
                    primary: true,
                    render: (e) => {
                      const s = auditSummary(e);
                      const link = auditHref(e);
                      return (
                        <>
                          <span class="cell-title">
                            {s.who} · {s.what}
                          </span>
                          <span class="cell-sub mono">
                            {e.action}
                            {e.entityId && (
                              <>
                                {' · '}
                                {link ? <a href={link}>{e.entityId}</a> : e.entityId}
                              </>
                            )}
                          </span>
                        </>
                      );
                    },
                  },
                  { key: 't', label: 'when', render: (e) => <time dateTime={e.createdAt} title={fmtDateTime(e.createdAt)} class="small">{relative(e.createdAt)}</time> },
                ]}
              />
              {error && <Notice tone="negative">{error}</Notice>}
              {next && (
                <button
                  type="button"
                  class="btn"
                  disabled={busy}
                  onClick={async () => {
                    setBusy(true);
                    setError(null);
                    try {
                      const page = await api('adminAudit', { query: { limit: 50, cursor: next, entityType: entityType || undefined } });
                      setMore({ key, items: [...(extra?.items ?? []), ...page.items], next: page.nextCursor });
                    } catch (e) {
                      setError(errorMessage(e));
                    } finally {
                      setBusy(false);
                    }
                  }}
                >
                  {busy ? 'loading…' : 'load older entries'}
                </button>
              )}
            </>
          );
        }}
      </Async>
    </div>
  );
}
