import { widgetDef } from '../registry';
import { dollars, pct } from '../../../lib/format';
import { href } from '../../../lib/routes';
import { usageHue } from '../../../lib/colors';
import { Kpi, Progress, Sparkline } from '../../components/kit';

import type { WidgetProps } from '../registry';
/** A loading or failed stat tile keeps its block so the grid does not jump. */
function StatState({ label, state }: { label: string; state: { status: string; reload?: () => void } }) {
  return (
    <section class="tile tile--stat" aria-label={label} aria-busy={state.status === 'loading' ? 'true' : undefined}>
      <span class="kpi-label">{label}</span>
      {state.status === 'loading' ? <><span class="visually-hidden">Loading {label}…</span><span class="skel skel--kpi" /></> : <span class="kpi-sub">Couldn’t load. <button class="btn btn--small" onClick={state.reload}>Retry</button></span>}
    </section>
  );
}


export default function KpiWidget({ id, overview }: WidgetProps) {
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
