import { DueDate } from '../../components/DueDate';
import { isDueSoon } from '../../../lib/deadline';
import type { PurchaseRequestListItem } from '../../../lib/contracts';
import type { PurchaseStatus } from '../../../lib/domain';
import { useApi } from '../../../lib/api/useApi';
import { dollars, fmtShort, formatCents, relative } from '../../../lib/format';
import { href } from '../../../lib/routes';
import { categoryHue } from '../../../lib/colors';
import { StatusBadge } from '../../components/kit';
import WidgetCard, { WidgetEmpty, WidgetState } from '../shell/WidgetCard';
import { FitList, Meta, Row, ROW_2 } from '../shell/rows';
import type { WidgetProps } from '../registry';

const OPEN: PurchaseStatus[] = ['draft', 'submitted', 'approved', 'fulfilling', 'fulfilled', 'reimbursed'];

/** A request as a compact row. `rich` (4×2) adds the number, category and requester. */
export function RequestRow({ r, showRequester, rich }: { r: PurchaseRequestListItem; showRequester?: boolean; rich?: boolean }) {
  const due = isDueSoon(r) ? <DueDate date={r.neededBy} status={r.status} hintOnly /> : r.neededBy ? `needed ${fmtShort(r.neededBy)}` : null;
  const when = relative(r.submittedAt ?? r.updatedAt);
  const meta = rich
    ? <Meta parts={[<span class="mono">#{r.number}</span>, showRequester && r.requester.name, <span class="st-dot" data-hue={categoryHue(r.expenseAccount.code)}>{r.expenseAccount.name}</span>, due, when]} />
    : <Meta parts={[showRequester ? r.requester.name : <span class="mono">#{r.number}</span>, when, due]} />;
  return <Row href={href.request(r.id)} attrs={{ 'data-request-link': r.number }} title={r.title} meta={meta}
    side={<><span class="row-amt">{formatCents(r.totalCents)}</span><StatusBadge status={r.status} /></>} />;
}

const sum = (items: PurchaseRequestListItem[]) => items.reduce((a, r) => a + r.totalCents, 0);

export default function MyActionWidget({ me, size, overview }: WidgetProps) {
  const director = me.role !== 'officer';
  const state = useApi('requestsList', { query: director ? { scope: 'all', status: 'pending', limit: 30 } : { scope: 'mine', limit: 50 } });
  const items = state.status === 'ready' ? (director ? state.data.items : state.data.items.filter((r) => OPEN.includes(r.status))) : [];
  // Directors: the overview's pending count covers requests beyond the fetched page.
  const total = director && overview.status === 'ready' ? Math.max(overview.data.kpis.pendingRequests, items.length) : items.length;
  const wide = size === '4x2';
  const aside = state.status === 'ready' && total ? `${total} waiting${wide ? ` · ${dollars(director && overview.status === 'ready' ? overview.data.kpis.pendingRequestsCents : sum(items))}` : ''}` : null;
  return (
    <WidgetCard id="my-action" me={me} title={director ? 'Awaiting your approval' : 'My open requests'} aside={director ? aside : aside?.replace('waiting', 'open')}>
      {state.status !== 'ready' ? <WidgetState state={state} label="requests" />
        : !items.length ? <WidgetEmpty icon="flag" size={size}>{director ? 'Nothing to approve' : 'No open requests'}</WidgetEmpty>
          : <FitList items={items} row={ROW_2} columns={wide ? 2 : 1} fallback={size === '2x1' ? 2 : wide ? 10 : 5} hiddenBeyond={total - items.length}
            render={(r) => <RequestRow key={r.id} r={r} showRequester={director} rich={wide} />}
            more={(hidden, count) => `+${count} more${hidden.length === count ? ` · ${dollars(sum(hidden))}` : ''}`} />}
    </WidgetCard>
  );
}
