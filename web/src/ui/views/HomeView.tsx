import CalendarWidget from '../widgets/CalendarWidget';
import LinksWidget from '../widgets/LinksWidget';
import CloudflareWidget, { cloudflareIds } from '../widgets/CloudflareWidget';
import WidgetGrid from '../widgets/WidgetGrid';
import { kpiIds, widgetDef, type BlockSize } from '../widgets/registry';
import type { ComponentChildren } from 'preact';
import { useRef } from 'preact/hooks';
import type { AuditLogEntry, CurrentUser, PurchaseRequestListItem } from '../../lib/contracts';
import type { PurchaseStatus } from '../../lib/domain';
import { useApi } from '../../lib/api/useApi';
import { dollars, formatCents, pct, relative } from '../../lib/format';
import { href } from '../../lib/routes';
import { usageHue } from '../../lib/colors';
import { CategoryDot, DataTable, Kpi, Progress, Sparkline, StatusBadge } from '../components/kit';
import { Async, Empty, Loading } from '../components/states';
import { eventColumns } from './columns';

const OPEN: PurchaseStatus[] = ['draft', 'submitted', 'approved', 'fulfilling', 'fulfilled', 'reimbursed'];

/** A titled widget tile in the Home block grid. */
function Block({ id, title, action, children }: { id: string; title: string; action?: ComponentChildren; children: ComponentChildren }) {
  return (
    <section class="tile" aria-labelledby={`${id}-h`} data-block={id}>
      <header class="tile-head">
        <h2 class="tile-title" id={`${id}-h`}>{title}</h2>
        {action && <div class="tile-link">{action}</div>}
      </header>
      <div class="tile-body">{children}</div>
    </section>
  );
}

/** A loading or failed stat tile keeps its block so the grid does not jump. */
function StatState({ label, state }: { label: string; state: { status: string; reload?: () => void } }) {
  return (
    <section class="tile tile--stat" aria-label={label} aria-busy={state.status === 'loading' ? 'true' : undefined}>
      <span class="kpi-label">{label}</span>
      {state.status === 'loading' ? <><span class="visually-hidden">Loading {label}…</span><span class="skel skel--kpi" /></> : <span class="kpi-sub">Couldn’t load. <button class="btn btn--small" onClick={state.reload}>Retry</button></span>}
    </section>
  );
}

const wide = (size: BlockSize) => size.startsWith('4');

export default function HomeView({ me }: { me: CurrentUser }) {
  const director = me.role !== 'officer';
  const overview = useApi('overview');
  const cloudflare = useApi('cloudflareWidgets');
  const fy = overview.status === 'ready' ? overview.data.fiscalYear : undefined;
  // Commit the primary grid in one layout: loading stat cards and a short table skeleton
  // otherwise grow different shared grid tracks when overview arrives.
  const committed = useRef(false);
  if (overview.status !== 'loading') committed.current = true;
  if (!committed.current) return <Loading rows={6} label="Loading home" />;

  /** One stat tile per key figure (formerly one "Key figures" row). */
  function stat(id: string) {
    const label = widgetDef(id)?.title ?? id;
    if (overview.status !== 'ready') return <StatState label={label} state={overview} />;
    const d = overview.data;
    const k = d.kpis;
    const now = Date.now();
    // Past events oldest → newest, for the rate sparklines.
    const past = d.recentEvents.filter((e) => new Date(e.startsAt).getTime() <= now).reverse();
    const used = k.budgetCents ? (k.spentCents + k.committedCents) / k.budgetCents : null;
    switch (id) {
      case 'kpi-upcoming':
        return <Kpi tile label="upcoming events" icon="calendar" hue="blue" value={k.upcomingEvents} href={href.events({ when: 'upcoming' })} sub="on the calendar" />;
      case 'kpi-checkin':
        return <Kpi tile label="avg check-in rate" icon="check" hue="teal" value={pct(k.avgCheckInRate)} trend={<Sparkline values={past.map((e) => e.metrics.checkInRate)} hue="teal" />} sub={`past events, FY${d.fiscalYear}`} />;
      case 'kpi-reflection':
        return <Kpi tile label="reflection rate" icon="message" hue="violet" value={pct(k.avgReflectionRate)} trend={<Sparkline values={past.map((e) => e.metrics.reflectionRate)} hue="violet" />} sub="of checked-in guests" />;
      case 'kpi-budget':
        return (
          <Kpi tile label="budget remaining" icon="wallet" hue="green" value={dollars(k.remainingCents)} tone={k.remainingCents < 0 ? 'negative' : undefined} href={href.finance('budgets')}
            trend={<Progress value={used} hue={usageHue(used)} label={`${pct(used)} of the budget used or committed`} />}
            sub={`${dollars(k.spentCents)} spent · ${dollars(k.committedCents)} committed of ${dollars(k.budgetCents)}`} />
        );
      default:
        return <Kpi tile label="pending requests" icon="inbox" hue="amber" value={k.pendingRequests} href={href.requests({ scope: 'all', status: 'pending' })} sub={k.pendingRequests ? `${dollars(k.pendingRequestsCents)} awaiting a director` : 'nothing waiting'} />;
    }
  }

  return (
    <WidgetGrid>
      {(id, size) => {
        if (cloudflareIds.includes(id)) return <CloudflareWidget id={id} size={size} state={cloudflare} />;
        if (kpiIds.includes(id)) return stat(id);
        if (id === 'calendar') return <CalendarWidget size={size} />;
        switch (id) {
          case 'links': return <LinksWidget me={me} />;
          case 'my-action':
            return (
              <Block id="my-action" title={director ? 'awaiting your approval' : 'my open requests'} action={<a href={href.newRequest()}>new request →</a>}>
                <MyAction me={me} limit={size === '2x1' ? 2 : 6} />
              </Block>
            );
          case 'recent-events':
            return (
              <Block id="recent-events" title="recent events" action={<a href={href.events()}>all events →</a>}>
                <Async state={overview} loading={<Loading rows={4} label="Loading events" />}>
                  {(d) =>
                    d.recentEvents.length === 0 ? (
                      <Empty title="No events yet" action={<a href={href.eventImport()}>import a Luma guest export →</a>}>
                        Events appear here once a Luma guest list is imported.
                      </Empty>
                    ) : (
                      <DataTable rows={d.recentEvents.slice(0, wide(size) ? 5 : 3)} columns={eventColumns(Date.now(), { compact: !wide(size) })} rowKey={(e) => e.id} caption="Recent events" dense />
                    )
                  }
                </Async>
              </Block>
            );
          case 'budget':
            return (
              <Block id="budget" title="budget vs actual" action={<a href={href.finance('budgets')}>budgets →</a>}>
                {fy === undefined ? <Loading rows={4} label="Loading the budget" /> : <BudgetMini fiscalYear={fy} rows={wide(size) ? 8 : 5} />}
              </Block>
            );
          case 'activity':
            return (
              <Block id="activity" title={director ? 'recent activity' : 'latest requests'} action={director ? <a href={href.admin('audit')}>audit log →</a> : <a href={href.requests({ scope: 'all' })}>all requests →</a>}>
                {director ? <AuditFeed limit={size === '2x1' ? 2 : size === '1x2' ? 5 : 8} /> : <RequestFeed limit={size === '2x1' ? 2 : size === '1x2' ? 4 : 6} />}
              </Block>
            );
        }
        return null;
      }}
    </WidgetGrid>
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

function MyAction({ me, limit }: { me: CurrentUser; limit: number }) {
  const director = me.role !== 'officer';
  const state = useApi('requestsList', { query: director ? { scope: 'all', status: 'pending', limit } : { scope: 'mine', limit: 50 } });
  return (
    <Async state={state} loading={<Loading rows={3} label="Loading requests" />}>
      {(d) => {
        const items = (director ? d.items : d.items.filter((r) => OPEN.includes(r.status))).slice(0, limit);
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
            {director && d.items.length >= limit && (
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

function RequestFeed({ limit }: { limit: number }) {
  const state = useApi('requestsList', { query: { scope: 'all', limit } });
  return (
    <Async state={state} loading={<Loading rows={4} label="Loading requests" />}>
      {(d) => (d.items.length ? <RequestList items={d.items} showRequester /> : <Empty title="No requests yet">Purchase requests from every officer show here.</Empty>)}
    </Async>
  );
}

function BudgetMini({ fiscalYear, rows: limit = 6 }: { fiscalYear: number; rows?: number }) {
  const state = useApi('financeSummary', { query: { fiscalYear } });
  return (
    <Async state={state} loading={<Loading rows={4} label="Loading the budget" />}>
      {(d) => {
        if (!d.byCategory.length) return <Empty title="No budget set">A director sets the year’s budget by category.</Empty>;
        const rows = [...d.byCategory].sort((a, b) => b.spentCents + b.committedCents - (a.spentCents + a.committedCents)).slice(0, limit);
        return (
          <DataTable
            dense
            caption={`Budget vs actual, FY${d.fiscalYear}`}
            rows={rows}
            rowKey={(c) => c.account.id}
            columns={[
              {
                key: 'a',
                label: 'category',
                primary: true,
                render: (c) => (
                  <span class="cat-name">
                    <CategoryDot code={c.account.code} />
                    {c.account.name}
                  </span>
                ),
              },
              {
                key: 'u',
                label: 'used',
                hideOnPhone: true,
                render: (c) => {
                  const rate = c.budgetCents ? (c.spentCents + c.committedCents) / c.budgetCents : null;
                  return <Progress value={rate} hue={usageHue(rate)} label={`${c.account.name}: ${pct(rate)} used`} />;
                },
              },
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
                <td class="dt-hide-phone" data-label="used" />
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

function AuditFeed({ limit }: { limit: number }) {
  const state = useApi('adminAudit', { query: { limit } });
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
