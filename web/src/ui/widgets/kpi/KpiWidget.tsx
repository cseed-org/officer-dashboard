import type { ComponentChildren } from 'preact';
import type { EventListItem } from '../../../lib/contracts';
import { useApi } from '../../../lib/api/useApi';
import { dollars, pct } from '../../../lib/format';
import { href } from '../../../lib/routes';
import { usageHue, type Hue } from '../../../lib/colors';
import { pacificParts, shortDay, timeRange, weekdayShort, addDays, pacificToday } from '../../../lib/calendar';
import { CountUp, Progress, Sparkline } from '../../components/kit';
import WidgetCard, { WidgetEmpty, WidgetState } from '../shell/WidgetCard';
import { useBox } from '../shell/fit';
import { widgetDef, type WidgetProps } from '../registry';

/** The figure: the final text first (count-up animates it only during the Home entrance). */
const Value = ({ children, negative }: { children: string | number; negative?: boolean }) =>
  <span class={`kpi-value stat-value${negative ? ' stat-value--negative' : ''}`}><CountUp value={children} /></span>;

/** A sparkline that scales to the space its column has (measured by ResizeObserver). */
function FillSpark({ values, hue }: { values: (number | null)[]; hue: Hue }) {
  const [ref, box] = useBox();
  return <div class="stat-chart" ref={ref}>{box.width > 0 && <Sparkline values={values} hue={hue} width={box.width} height={Math.min(box.height, 72)} />}</div>;
}

/** "Thu" for this week, "Nov 19" beyond it. */
function dayLabel(iso: string) {
  const date = pacificParts(iso).date;
  return date < addDays(pacificToday(), 7) ? weekdayShort(date) : shortDay(date);
}

function Split({ main, side }: { main: ComponentChildren; side: ComponentChildren }) {
  return <div class="stat-split"><div class="stat-main">{main}</div><div>{side}</div></div>;
}

function PendingTop() {
  const state = useApi('requestsList', { query: { scope: 'all', status: 'pending', limit: 5 } });
  if (state.status !== 'ready') return null;
  return <div class="mini">{state.data.items.slice(0, 4).map((r) => <a key={r.id} href={href.request(r.id)}><i>{dollars(r.totalCents)}</i><b>{r.title}</b></a>)}</div>;
}

function UpcomingList({ events }: { events: EventListItem[] }) {
  return <div class="mini">{events.slice(0, 4).map((e) => <a key={e.id} href={href.event(e.id)}><i>{dayLabel(e.startsAt)}</i><b>{e.name}</b></a>)}</div>;
}

export default function KpiWidget({ id, size, me, overview }: WidgetProps) {
  const def = widgetDef(id);
  const wide = size === '2x1';
  const card = (body: ComponentChildren) => <WidgetCard id={id} me={me} class="wc--stat">{body}</WidgetCard>;
  if (overview.status !== 'ready') return card(<WidgetState state={overview} label={def?.title ?? id} />);
  const d = overview.data;
  const k = d.kpis;
  const now = Date.now();
  // recentEvents is newest first; past events oldest → newest for the sparklines.
  const past = d.recentEvents.filter((e) => new Date(e.startsAt).getTime() <= now).reverse();
  const upcoming = d.recentEvents.filter((e) => new Date(e.startsAt).getTime() > now).sort((a, b) => a.startsAt.localeCompare(b.startsAt));
  const empty = (line: string) => card(<WidgetEmpty icon={def?.icon ?? 'blocks'} size={size}>{line}</WidgetEmpty>);

  switch (id) {
    case 'kpi-upcoming': {
      if (!k.upcomingEvents) return empty('Nothing on the calendar');
      const next = upcoming[0];
      const sub = next ? `next: ${next.name} · ${dayLabel(next.startsAt)} ${timeRange(next.startsAt)}` : 'on the calendar';
      return card(wide && upcoming.length
        ? <Split main={<><Value>{k.upcomingEvents}</Value><span class="stat-sub">on the calendar</span></>} side={<UpcomingList events={upcoming} />} />
        : <div class="stat"><Value>{k.upcomingEvents}</Value><span class="stat-sub">{sub}</span></div>);
    }
    case 'kpi-checkin':
    case 'kpi-reflection': {
      const checkin = id === 'kpi-checkin';
      const rates = past.map((e) => (checkin ? e.metrics.checkInRate : e.metrics.reflectionRate));
      const value = checkin ? k.avgCheckInRate : k.avgReflectionRate;
      if (!past.length || value === null) return empty(checkin ? 'No past events yet' : 'No reflections yet');
      const hue: Hue = checkin ? 'teal' : 'violet';
      const sub = checkin ? `past events, FY${d.fiscalYear}` : 'of checked-in guests';
      if (!wide) return card(<div class="stat"><div class="stat-row"><Value>{pct(value)}</Value><Sparkline values={rates} hue={hue} width={72} height={28} /></div><span class="stat-sub">{sub}</span></div>);
      const rated = past.filter((e) => (checkin ? e.metrics.checkInRate : e.metrics.reflectionRate) !== null);
      const rate = (e: EventListItem) => (checkin ? e.metrics.checkInRate : e.metrics.reflectionRate) ?? 0;
      const best = rated.reduce<EventListItem | null>((b, e) => (!b || rate(e) > rate(b) ? e : b), null);
      const low = rated.reduce<EventListItem | null>((b, e) => (!b || rate(e) < rate(b) ? e : b), null);
      const last = rated[rated.length - 1];
      const note = checkin
        ? best && low ? `best ${pct(rate(best))} ${best.name} · low ${pct(rate(low))}` : ''
        : last ? `last event ${pct(rate(last))} · ${last.name}` : '';
      return card(<Split main={<><Value>{pct(value)}</Value><span class="stat-sub">{sub}</span></>} side={<><FillSpark values={rates} hue={hue} />{note && <span class="stat-sub">{note}</span>}</>} />);
    }
    case 'kpi-budget': {
      if (!k.budgetCents) return empty(`No budget set for FY${d.fiscalYear}`);
      const used = (k.spentCents + k.committedCents) / k.budgetCents;
      const bar = <Progress value={used} hue={usageHue(used)} label={`${pct(used)} of the budget used or committed`} />;
      const value = <Value negative={k.remainingCents < 0}>{dollars(k.remainingCents)}</Value>;
      if (!wide) return card(<div class="stat">{value}{bar}<span class="stat-sub">{dollars(k.spentCents)} spent · {dollars(k.committedCents)} committed</span></div>);
      return card(<Split
        main={<>{value}<span class="stat-sub">left of {dollars(k.budgetCents)} · FY{d.fiscalYear}</span></>}
        side={<>{bar}<span class="split-legend"><span><i class="dot" data-hue={usageHue(used)} />{dollars(k.spentCents)} spent</span><span><i class="dot" data-hue="amber" />{dollars(k.committedCents)} committed</span></span></>} />);
    }
    default: {
      const sub = k.pendingRequests ? `${dollars(k.pendingRequestsCents)} awaiting a director` : 'Nothing waiting';
      return card(wide && k.pendingRequests
        ? <Split main={<><Value>{k.pendingRequests}</Value><span class="stat-sub">{sub}</span></>} side={<PendingTop />} />
        : <div class="stat"><Value>{k.pendingRequests}</Value><span class="stat-sub">{sub}</span></div>);
    }
  }
}
