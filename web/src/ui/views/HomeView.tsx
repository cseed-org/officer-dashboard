import type { ComponentChildren } from 'preact';
import type { AuditLogEntry, CurrentUser, PurchaseRequestListItem } from '../../lib/contracts';
import type { PurchaseStatus } from '../../lib/domain';
import { useApi } from '../../lib/api/useApi';
import { dollars, formatCents, pct, relative } from '../../lib/format';
import { href } from '../../lib/routes';
import { DataTable, Kpi, StatusBadge } from '../components/kit';
import { Async, Empty, Loading } from '../components/states';
import { eventColumns } from './columns';

const OPEN: PurchaseStatus[] = ['draft', 'submitted', 'approved', 'fulfilling', 'fulfilled', 'reimbursed'];

/** A titled card in the home grid. */
function Block({ id, title, action, span, children }: { id: string; title: string; action?: ComponentChildren; span?: 'wide' | 'full'; children: ComponentChildren }) {
  return (
    <section class={`block${span ? ` block--${span}` : ''}`} aria-labelledby={`${id}-h`} data-block={id}>
      <header class="block-head">
        <h2 id={`${id}-h`}>{title}</h2>
        {action && <div class="block-action">{action}</div>}
      </header>
      <div class="block-body">{children}</div>
    </section>
  );
}

export default function HomeView({ me }: { me: CurrentUser }) {
  const director = me.role === 'director';
  const overview = useApi('overview');
  const fy = overview.status === 'ready' ? overview.data.fiscalYear : undefined;

  return (
    <div class="home-grid">
      <section class="block block--full block--bare" aria-label="Key figures" data-block="kpis">
        <Async state={overview} loading={<Loading tiles={5} rows={0} label="Loading key figures" />}>
          {(d) => {
            const k = d.kpis;
            return (
              <div class="kpis">
                <Kpi label="upcoming events" value={k.upcomingEvents} href={href.events({ when: 'upcoming' })} sub="on the calendar" />
                <Kpi label="avg check-in rate" value={pct(k.avgCheckInRate)} sub={`past events, FY${d.fiscalYear}`} />
                <Kpi label="reflection rate" value={pct(k.avgReflectionRate)} sub="of checked-in guests" />
                <Kpi
                  label="budget remaining"
                  value={dollars(k.remainingCents)}
                  tone={k.remainingCents < 0 ? 'negative' : undefined}
                  href={href.finance('budgets')}
                  sub={`${dollars(k.spentCents)} spent · ${dollars(k.committedCents)} committed of ${dollars(k.budgetCents)}`}
                />
                <Kpi
                  label="pending requests"
                  value={k.pendingRequests}
                  href={href.requests({ scope: 'all', status: 'pending' })}
                  sub={k.pendingRequests ? `${dollars(k.pendingRequestsCents)} awaiting a director` : 'nothing waiting'}
                />
              </div>
            );
          }}
        </Async>
      </section>

      <Block id="my-action" title={director ? 'awaiting your approval' : 'my open requests'} action={<a href={href.newRequest()}>new request →</a>}>
        <MyAction me={me} />
      </Block>

      <Block id="recent-events" title="recent events" action={<a href={href.events()}>all events →</a>}>
        <Async state={overview} loading={<Loading rows={4} label="Loading events" />}>
          {(d) =>
            d.recentEvents.length === 0 ? (
              <Empty title="No events yet" action={<a href={href.eventImport()}>import a Luma guest export →</a>}>
                Events appear here once a Luma guest list is imported.
              </Empty>
            ) : (
              <DataTable rows={d.recentEvents.slice(0, 5)} columns={eventColumns(Date.now(), { compact: true })} rowKey={(e) => e.id} caption="Recent events" dense />
            )
          }
        </Async>
      </Block>

      <Block id="budget" title="budget vs actual" action={<a href={href.finance('budgets')}>budgets →</a>}>
        {fy === undefined ? <Loading rows={4} label="Loading the budget" /> : <BudgetMini fiscalYear={fy} />}
      </Block>

      <Block id="activity" title={director ? 'recent activity' : 'latest requests'} action={director ? <a href={href.admin('audit')}>audit log →</a> : <a href={href.requests({ scope: 'all' })}>all requests →</a>}>
        {director ? <AuditFeed /> : <RequestFeed />}
      </Block>
    </div>
  );
}

function RequestList({ items, showRequester }: { items: PurchaseRequestListItem[]; showRequester?: boolean }) {
  return (
    <ul class="list">
      {items.map((r) => (
        <li class="list-row" key={r.id}>
          <div class="list-main">
            <a class="cell-title" href={href.request(r.id)} data-request-link={r.number}>
              {r.title}
            </a>
            <span class="cell-sub">
              <span class="mono">#{r.number}</span>
              {showRequester ? ` · ${r.requester.name}` : ''} · {relative(r.submittedAt ?? r.updatedAt)}
            </span>
          </div>
          <div class="list-side">
            <span class="num">{formatCents(r.totalCents)}</span>
            <StatusBadge status={r.status} />
          </div>
        </li>
      ))}
    </ul>
  );
}

function MyAction({ me }: { me: CurrentUser }) {
  const director = me.role === 'director';
  const state = useApi('requestsList', { query: director ? { scope: 'all', status: 'pending', limit: 6 } : { scope: 'mine', limit: 50 } });
  return (
    <Async state={state} loading={<Loading rows={3} label="Loading requests" />}>
      {(d) => {
        const items = director ? d.items : d.items.filter((r) => OPEN.includes(r.status)).slice(0, 6);
        if (!items.length)
          return director ? (
            <Empty title="Nothing to approve">New requests from officers land here.</Empty>
          ) : (
            <Empty title="No open requests" action={<a class="btn btn--small" href={href.newRequest()}>new purchase request</a>}>
              Need supplies, food or a reimbursement? Ask here and a director will review it.
            </Empty>
          );
        return (
          <>
            <RequestList items={items} showRequester={director} />
            {director && d.items.length >= 6 && (
              <p class="small">
                <a href={href.requests({ scope: 'all', status: 'pending' })}>see every pending request →</a>
              </p>
            )}
          </>
        );
      }}
    </Async>
  );
}

function RequestFeed() {
  const state = useApi('requestsList', { query: { scope: 'all', limit: 6 } });
  return (
    <Async state={state} loading={<Loading rows={4} label="Loading requests" />}>
      {(d) => (d.items.length ? <RequestList items={d.items} showRequester /> : <Empty title="No requests yet">Purchase requests from every officer show here.</Empty>)}
    </Async>
  );
}

function BudgetMini({ fiscalYear }: { fiscalYear: number }) {
  const state = useApi('financeSummary', { query: { fiscalYear } });
  return (
    <Async state={state} loading={<Loading rows={4} label="Loading the budget" />}>
      {(d) => {
        if (!d.byCategory.length) return <Empty title="No budget set">A director sets the year’s budget by category.</Empty>;
        const rows = [...d.byCategory].sort((a, b) => b.spentCents + b.committedCents - (a.spentCents + a.committedCents)).slice(0, 6);
        return (
          <DataTable
            dense
            caption={`Budget vs actual, FY${d.fiscalYear}`}
            rows={rows}
            rowKey={(c) => c.account.id}
            columns={[
              { key: 'a', label: 'category', primary: true, render: (c) => c.account.name },
              { key: 'b', label: 'budget', num: true, render: (c) => dollars(c.budgetCents) },
              { key: 's', label: 'actual', num: true, render: (c) => dollars(c.spentCents + c.committedCents) },
              {
                key: 'r',
                label: 'left',
                num: true,
                render: (c) => <span class={c.remainingCents < 0 ? 'check-bad' : undefined}>{dollars(c.remainingCents)}</span>,
              },
            ]}
            foot={
              <tr>
                <td class="dt-primary">all categories</td>
                <td class="num" data-label="budget">{dollars(d.totals.budgetCents)}</td>
                <td class="num" data-label="actual">{dollars(d.totals.spentCents + d.totals.committedCents)}</td>
                <td class="num" data-label="left">{dollars(d.totals.remainingCents)}</td>
              </tr>
            }
          />
        );
      }}
    </Async>
  );
}

/** "request.approve" → "approve"; "purchase_request" → "purchase request". */
const words = (s: string) => s.replace(/[._]/g, ' ').trim();
export function auditHref(e: Pick<AuditLogEntry, 'entityType' | 'entityId'>): string | null {
  if (!e.entityId) return null;
  if (/request/.test(e.entityType)) return href.request(e.entityId);
  if (/event/.test(e.entityType)) return href.event(e.entityId);
  if (/user/.test(e.entityType)) return href.admin('users');
  if (/token/.test(e.entityType)) return href.admin('tokens');
  return null;
}
export function auditSummary(e: AuditLogEntry) {
  const who = e.actor?.name ?? (e.actorTokenName ? `token “${e.actorTokenName}”` : 'system');
  const verb = words(e.action.split('.').pop() ?? e.action);
  return { who, what: `${verb} ${words(e.entityType)}` };
}

function AuditFeed() {
  const state = useApi('adminAudit', { query: { limit: 8 } });
  return (
    <Async state={state} loading={<Loading rows={4} label="Loading activity" />}>
      {(d) =>
        d.items.length === 0 ? (
          <Empty title="No activity yet">Approvals, imports and admin changes show up here.</Empty>
        ) : (
          <ol class="feed">
            {d.items.map((e) => {
              const s = auditSummary(e);
              const link = auditHref(e);
              return (
                <li key={e.id} class="feed-item">
                  <span class="feed-text">
                    <strong>{s.who}</strong> {s.what}
                    {link && (
                      <>
                        {' · '}
                        <a href={link}>open</a>
                      </>
                    )}
                  </span>
                  <time class="feed-time" dateTime={e.createdAt}>
                    {relative(e.createdAt)}
                  </time>
                </li>
              );
            })}
          </ol>
        )
      }
    </Async>
  );
}
