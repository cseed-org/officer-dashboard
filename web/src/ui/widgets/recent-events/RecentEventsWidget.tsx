import type { EventListItem } from '../../../lib/contracts';
import { pct } from '../../../lib/format';
import { href } from '../../../lib/routes';
import { pacificParts, shortDay } from '../../../lib/calendar';
import { Progress } from '../../components/kit';
import WidgetCard, { WidgetEmpty, WidgetState } from '../shell/WidgetCard';
import { FitList, Meta, Row, ROW_2 } from '../shell/rows';
import type { WidgetProps } from '../registry';

function EventRow({ e, rich }: { e: EventListItem; rich: boolean }) {
  const m = e.metrics;
  const date = shortDay(pacificParts(e.startsAt).date);
  const upcoming = new Date(e.startsAt).getTime() > Date.now();
  const checked = upcoming ? 'upcoming' : m.checkInRate === null ? 'no check-ins' : `${pct(m.checkInRate)} checked in`;
  const meta = rich
    ? <Meta parts={[date, e.location, `${m.registered} guests`, !upcoming && m.reflectionRate !== null && `${pct(m.reflectionRate)} reflected`]} />
    : <Meta parts={[date, checked]} />;
  const side = rich
    ? <>{upcoming || m.checkInRate === null ? <span /> : <Progress value={m.checkInRate} hue="teal" label={`${e.name}: ${pct(m.checkInRate)} checked in`} />}<span class="row-amt">{upcoming ? '—' : pct(m.checkInRate)}</span></>
    : <><span class="row-amt">{m.registered}</span><span class="row-meta">guests</span></>;
  return <Row href={href.event(e.id)} title={e.name} meta={meta} side={side} sideClass={rich ? 'row-side--bar' : undefined} />;
}

export default function RecentEventsWidget({ me, size, overview }: WidgetProps) {
  const rich = size === '4x2';
  return (
    <WidgetCard id="recent-events" me={me} aside={rich ? 'check-in rate' : null}>
      {overview.status !== 'ready' ? <WidgetState state={overview} label="events" />
        : !overview.data.recentEvents.length ? <WidgetEmpty icon="ticket" size={size}>No events yet · <a href={href.eventImport()}>import a Luma guest list</a></WidgetEmpty>
          : <FitList items={overview.data.recentEvents} row={ROW_2} fallback={rich ? 6 : 5} render={(e) => <EventRow key={e.id} e={e} rich={rich} />} />}
    </WidgetCard>
  );
}
