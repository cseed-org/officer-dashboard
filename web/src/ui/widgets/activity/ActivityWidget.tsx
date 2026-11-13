import type { AuditLogEntry } from '../../../lib/contracts';
import { useApi } from '../../../lib/api/useApi';
import { relative } from '../../../lib/format';
import { href } from '../../../lib/routes';
import { personHue } from '../../../lib/colors';
import { pacificParts, pacificToday, addDays, weekdayShort, shortDay } from '../../../lib/calendar';
import WidgetCard, { WidgetEmpty, WidgetState } from '../shell/WidgetCard';
import { FitList, Row, ROW_1, ROW_2 } from '../shell/rows';
import { useBox } from '../shell/fit';
import { RequestRow } from '../my-action/MyActionWidget';
import type { BlockSize, WidgetProps } from '../registry';

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

/** "2h", "3d", "now". */
function ago(iso: string, now = Date.now()) {
  const m = Math.max(0, Math.round((now - new Date(iso).getTime()) / 60_000));
  if (m < 1) return 'now';
  if (m < 60) return `${m}m`;
  if (m < 24 * 60) return `${Math.round(m / 60)}h`;
  return `${Math.round(m / 1440)}d`;
}
const initials = (name: string) => name.replace(/[“”]/g, '').replace(/^token /, '').split(/\s+/).map((s) => s[0] ?? '').join('').slice(0, 2).toUpperCase();
function dayName(iso: string) {
  const date = pacificParts(iso).date;
  const today = pacificToday();
  if (date === today) return 'Today';
  if (date === addDays(today, -1)) return 'Yesterday';
  return date > addDays(today, -7) ? weekdayShort(date) : shortDay(date);
}

function AuditRow({ e, compact }: { e: AuditLogEntry; compact: boolean }) {
  const s = auditSummary(e);
  return <Row href={auditHref(e) ?? href.admin('audit')}
    lead={<span class="avatar" data-hue={personHue(e.actor?.id ?? s.who)} aria-hidden="true">{initials(s.who)}</span>}
    title={<><b>{s.who}</b> <span class="row-what">{s.what}</span></>}
    meta={compact ? undefined : <time dateTime={e.createdAt}>{relative(e.createdAt)}</time>}
    side={compact ? <time class="row-meta" dateTime={e.createdAt}>{ago(e.createdAt)}</time> : undefined} />;
}

/** 4×2: two columns of one-line rows grouped by day, filled column by column to the height it has. */
function AuditColumns({ items }: { items: AuditLogEntry[] }) {
  const [ref, box] = useBox();
  const LABEL = 18;
  const columns: { label?: string; e?: AuditLogEntry }[][] = [[], []];
  let col = 0, used = 0, lastDay = '';
  const limit = box.height || 6 * (ROW_1 + 2);
  for (const e of items) {
    const day = dayName(e.createdAt);
    const needs = (day !== lastDay || used === 0 ? LABEL : 0) + ROW_1 + 2;
    if (used + needs > limit) { if (++col > 1) break; used = 0; }
    if (day !== lastDay || used === 0) { columns[col]!.push({ label: day }); used += LABEL; lastDay = day; }
    columns[col]!.push({ e }); used += ROW_1 + 2;
  }
  return <div class="wc-fit wc-cols" ref={ref}>{columns.map((c, i) => <div key={i} class="wc-col">{c.map((x) => x.label
    ? <p class="day-label" key={x.label}>{x.label}</p>
    : <ul class="rows" key={x.e!.id}><AuditRow e={x.e!} compact /></ul>)}</div>)}</div>;
}

function AuditFeed({ size }: { size: BlockSize }) {
  const state = useApi('adminAudit', { query: { limit: 30 } });
  if (state.status !== 'ready') return <WidgetState state={state} label="activity" />;
  if (!state.data.items.length) return <WidgetEmpty icon="activity" size={size}>No activity yet</WidgetEmpty>;
  if (size === '4x2') return <AuditColumns items={state.data.items} />;
  const compact = size === '2x1';
  return <FitList items={state.data.items} row={compact ? ROW_1 : ROW_2} fallback={compact ? 2 : 6} render={(e) => <AuditRow key={e.id} e={e} compact={compact} />} />;
}

function RequestFeed({ size }: { size: BlockSize }) {
  const state = useApi('requestsList', { query: { scope: 'all', limit: 30 } });
  if (state.status !== 'ready') return <WidgetState state={state} label="requests" />;
  if (!state.data.items.length) return <WidgetEmpty icon="activity" size={size}>No requests yet</WidgetEmpty>;
  return <FitList items={state.data.items} row={ROW_2} columns={size === '4x2' ? 2 : 1} fallback={size === '2x1' ? 2 : 6}
    render={(r) => <RequestRow key={r.id} r={r} showRequester rich={size === '4x2'} />} />;
}

export default function ActivityWidget({ me, size }: WidgetProps) {
  const director = me.role !== 'officer';
  return (
    <WidgetCard id="activity" me={me} title={director ? 'Recent activity' : 'Latest requests'}>
      {director ? <AuditFeed size={size} /> : <RequestFeed size={size} />}
    </WidgetCard>
  );
}
