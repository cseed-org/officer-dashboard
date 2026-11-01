import { useMemo, useState } from 'preact/hooks';
import type { AuditLogEntry, CurrentUser } from '../../lib/contracts';
import { api } from '../../lib/api/client';
import { useApi } from '../../lib/api/useApi';
import { fmtDateTime, pacificDate, relative } from '../../lib/format';
import { setParams, useSearch } from '../../lib/nav';
import type { AdminTab } from '../../lib/routes';
import { Icon } from '../components/Icon';
import { Tabs } from '../components/kit';
import { Avatar } from '../components/people';
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
  // ?new=1 opens the tab's create drawer (add officer / create token), as on Finance.
  const creating = search.get('new') === '1';
  const closeCreate = () => creating && setParams({ new: null });

  if (me.role === 'officer')
    return (
      <div class="state state--error" role="alert" data-state="forbidden">
        <p class="state-title">Directors only</p>
        <p class="state-body">Officers, service tokens and the audit log are managed by directors. Ask one if something needs changing.</p>
        <div class="state-action">
          <a href="/">back home →</a>
        </div>
      </div>
    );

  const action = tab === 'users' ? 'add officer' : tab === 'tokens' ? 'create token' : null;

  return (
    <div class="stack">
      <div class="toolbar">
        <Tabs label="Admin" idPrefix="adm" active={tab} onChange={(k) => setParams({ tab: k === 'users' ? null : k, new: null }, { replace: true })} tabs={TABS} />
        {action && (
          <div class="toolbar-actions">
            <button type="button" class="btn btn--primary" data-admin-create onClick={() => setParams({ new: 1 })}>
              <Icon name="plus" size={16} /> {action}
            </button>
          </div>
        )}
      </div>
      <div key={tab} id="adm-panel" role="tabpanel" aria-labelledby={`adm-tab-${tab}`} class="tab-panel">
        {tab === 'users' ? (
          <AdminUsersView canChangeRoles={me.role === 'leadership'} meId={me.id} adding={creating} onCloseAdd={closeCreate} />
        ) : tab === 'tokens' ? (
          <AdminTokensView creating={creating} onCloseCreate={closeCreate} />
        ) : (
          <AuditLog />
        )}
      </div>
    </div>
  );
}

const ENTITY_TYPES: [string, string][] = [
  ['', 'everything'],
  ['purchase_request', 'requests'],
  ['event', 'events'],
  ['user', 'officers'],
  ['service_token', 'tokens'],
  ['budget', 'budgets'],
  ['fiscal_period', 'fiscal periods'],
];

/**
 * Audit log: dense rows (actor avatar, what happened, the entity link, relative time with
 * the exact time on hover). "What" filters on the server; actor, action and date filter
 * the loaded entries, and "load older" pages further back.
 */
function AuditLog() {
  const [entityType, setEntityType] = useState('');
  const [actor, setActor] = useState('');
  const [action, setAction] = useState('');
  const [since, setSince] = useState('');
  const first = useApi('adminAudit', { query: { limit: 50, entityType: entityType || undefined } });
  const [more, setMore] = useState<{ items: AuditLogEntry[]; next: string | null; key: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const extra = more?.key === entityType ? more : null;
  const loaded = useMemo(() => (first.status === 'ready' ? [...first.data.items, ...(extra?.items ?? [])] : []), [first, extra]);
  const actorOf = (e: AuditLogEntry) => auditSummary(e).who;
  const actors = useMemo(() => [...new Set(loaded.map(actorOf))].sort(), [loaded]);
  const actions = useMemo(() => [...new Set(loaded.map((e) => e.action))].sort(), [loaded]);
  const filtered = loaded.filter((e) => (!actor || actorOf(e) === actor) && (!action || e.action === action) && (!since || pacificDate(e.createdAt) >= since));
  const filtering = !!(actor || action || since);

  return (
    <div class="stack stack--tight">
      <div class="filters audit-filters">
        <div class="field field--inline">
          <label class="field-label" for="audit-type">
            what
          </label>
          <select id="audit-type" class="select select--quiet" value={entityType} onChange={(e) => setEntityType(e.currentTarget.value)}>
            {ENTITY_TYPES.map(([v, l]) => (
              <option value={v} key={v}>
                {l}
              </option>
            ))}
          </select>
        </div>
        <div class="field field--inline">
          <label class="field-label" for="audit-actor">
            who
          </label>
          <select id="audit-actor" class="select select--quiet" value={actor} onChange={(e) => setActor(e.currentTarget.value)}>
            <option value="">anyone</option>
            {actors.map((a) => (
              <option value={a} key={a}>
                {a}
              </option>
            ))}
          </select>
        </div>
        <div class="field field--inline">
          <label class="field-label" for="audit-action">
            action
          </label>
          <select id="audit-action" class="select select--quiet" value={action} onChange={(e) => setAction(e.currentTarget.value)}>
            <option value="">any</option>
            {actions.map((a) => (
              <option value={a} key={a}>
                {a}
              </option>
            ))}
          </select>
        </div>
        <div class="field field--inline">
          <label class="field-label" for="audit-since">
            since
          </label>
          <input id="audit-since" class="input input--date select--quiet" type="date" value={since} onInput={(e) => setSince(e.currentTarget.value)} />
        </div>
        {filtering && (
          <button
            type="button"
            class="btn btn--text btn--small"
            onClick={() => {
              setActor('');
              setAction('');
              setSince('');
            }}
          >
            clear
          </button>
        )}
      </div>

      <Async state={first} loading={<Loading rows={8} label="Loading the audit log" />}>
        {(d) => {
          const next = extra ? extra.next : d.nextCursor;
          if (!loaded.length) return <Empty title="Nothing logged yet">Every change made through the dashboard or a service token is recorded here.</Empty>;
          return (
            <>
              <p class="small muted flush" data-audit-count>
                {filtering ? `${filtered.length} of ${loaded.length} loaded entries` : `${loaded.length} most recent entries`}
                {next ? ' · older entries not loaded yet' : ''}
              </p>
              {filtered.length === 0 ? (
                <Empty title="No entries match">Clear a filter, or load older entries.</Empty>
              ) : (
                <ol class="audit" aria-label="Audit log">
                  {filtered.map((e) => {
                    const s = auditSummary(e);
                    const link = auditHref(e);
                    return (
                      <li key={e.id} class="audit-row">
                        <Avatar name={e.actor?.name ?? e.actorTokenName ?? 'system'} id={e.actor?.id ?? e.actorTokenName} size="sm" />
                        <span class="audit-text">
                          <strong>{s.who}</strong> {s.what}
                          {e.entityId &&
                            (link ? (
                              <a href={link} class="audit-entity">
                                open
                              </a>
                            ) : (
                              <span class="audit-entity mono">{e.entityId}</span>
                            ))}
                          <span class="audit-action mono">{e.action}</span>
                        </span>
                        <time class="audit-time" dateTime={e.createdAt} title={fmtDateTime(e.createdAt)}>
                          {relative(e.createdAt)}
                        </time>
                      </li>
                    );
                  })}
                </ol>
              )}
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
                      setMore({ key: entityType, items: [...(extra?.items ?? []), ...page.items], next: page.nextCursor });
                    } catch (err) {
                      setError(errorMessage(err));
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
