import { DueDate } from '../../components/DueDate';
import { isDueSoon } from '../../../lib/deadline';
import type { CurrentUser, PurchaseRequestListItem } from '../../../lib/contracts';
import type { PurchaseStatus } from '../../../lib/domain';
import { useApi } from '../../../lib/api/useApi';
import { formatCents, relative } from '../../../lib/format';
import { href } from '../../../lib/routes';
import { StatusBadge } from '../../components/kit';
import { Async, Empty, Loading } from '../../components/states';

import Block from '../shell/WidgetCard';
import type { WidgetProps } from '../registry';
const OPEN: PurchaseStatus[] = ['draft', 'submitted', 'approved', 'fulfilling', 'fulfilled', 'reimbursed'];
export default function MyActionWidget({ me, size }: WidgetProps) {
 const director = me.role !== 'officer';
  return (
              <Block id="my-action" title={director ? 'awaiting your approval' : 'my open requests'} action={<a href={href.newRequest()}>new request →</a>}>
                <MyAction me={me} limit={size === '2x1' ? 2 : 6} />
              </Block>
            );
}
export function RequestList({ items, showRequester }: { items: PurchaseRequestListItem[]; showRequester?: boolean }) {
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
              {isDueSoon(r) && (
                <>
                  {' · '}
                  <DueDate date={r.neededBy} status={r.status} hintOnly />
                </>
              )}
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

