import type { EventListItem, PurchaseRequestListItem } from '../../lib/contracts';
import { fmtDate, fmtWeekday, formatCents, pct, TYPE_LABEL } from '../../lib/format';
import { href } from '../../lib/routes';
import type { Hue } from '../../lib/colors';
import { CategoryDot, type Column, Meter, StatusBadge } from '../components/kit';

const upcoming = (e: EventListItem, now: number) => new Date(e.startsAt).getTime() > now;

export function RateCell({ rate, label, hue }: { rate: number | null; label: string; hue?: Hue }) {
  return (
    <span class="rate-cell">
      <span class="num">{pct(rate)}</span>
      <Meter value={rate} label={label} hue={hue} />
    </span>
  );
}

export function eventColumns(now = Date.now(), opts: { compact?: boolean } = {}): Column<EventListItem>[] {
  return [
    {
      key: 'name',
      label: 'event',
      primary: true,
      render: (e) => (
        <>
          <a class="cell-title" href={href.event(e.id)} data-event-link={e.id}>
            {e.name}
          </a>
          <span class="cell-sub">
            {fmtWeekday(e.startsAt)} {fmtDate(e.startsAt)}
            {e.location ? ` · ${e.location}` : ''}
            {upcoming(e, now) ? ' · upcoming' : ''}
          </span>
        </>
      ),
    },
    ...(opts.compact ? [] : [{ key: 'host', label: 'host', hideOnPhone: true, render: (e: EventListItem) => e.host?.name ?? <span class="muted">—</span> }]),
    { key: 'reg', label: 'registered', num: true, render: (e) => e.metrics.registered },
    {
      key: 'checkin',
      label: 'checked in',
      render: (e) => (upcoming(e, now) ? <span class="muted small">not yet</span> : <RateCell rate={e.metrics.checkInRate} label={`${e.name} check-in rate`} hue="teal" />),
    },
    {
      key: 'refl',
      label: 'reflected',
      render: (e) =>
        upcoming(e, now) ? (
          <span class="muted small">not yet</span>
        ) : (
          <span>
            <RateCell rate={e.metrics.reflectionRate} label={`${e.name} reflection rate`} hue="violet" />
            {e.metrics.unmatchedReflections > 0 && (
              <span class="flag" title="Reflections from emails that never checked in">
                ⚠ {e.metrics.unmatchedReflections} unmatched
              </span>
            )}
          </span>
        ),
    },
  ];
}

export const requestColumns = (opts: { showRequester?: boolean; compact?: boolean } = {}): Column<PurchaseRequestListItem>[] => [
  {
    key: 'title',
    label: 'request',
    primary: true,
    render: (r) => (
      <>
        <a class="cell-title" href={href.request(r.id)} data-request-link={r.number}>
          {r.title}
        </a>
        <span class="cell-sub">
          <span class="mono">#{r.number}</span> · {r.vendor} · {TYPE_LABEL[r.type]}
        </span>
      </>
    ),
  },
  ...(opts.showRequester === false ? [] : [{ key: 'who', label: 'requester', render: (r: PurchaseRequestListItem) => r.requester.name }]),
  ...(opts.compact
    ? []
    : [{ key: 'cat', label: 'category · fund', hideOnPhone: true, render: (r: PurchaseRequestListItem) => <span class="small cat-name">
            <CategoryDot code={r.expenseAccount.code} />
            {r.expenseAccount.name} · {r.fund.code}
          </span> }]),
  { key: 'status', label: 'status', render: (r) => <StatusBadge status={r.status} /> },
  { key: 'total', label: 'total', num: true, render: (r) => formatCents(r.totalCents) },
];
