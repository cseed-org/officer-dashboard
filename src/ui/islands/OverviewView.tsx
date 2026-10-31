import type { EventListItem } from '../../core/contracts';
import { useApi } from '../api/useApi';
import { Async, Empty, Loading } from '../components/states';
import { DataTable, Kpi, Meter, Section, type Column } from '../components/kit';
import { dollars, fmtDate, fmtWeekday, pct } from '../format';

const upcoming = (e: EventListItem, now: number) => new Date(e.startsAt).getTime() > now;

export function eventColumns(now = Date.now()): Column<EventListItem>[] {
  return [
    {
      key: 'name',
      label: 'event',
      primary: true,
      render: (e) => (
        <>
          <a class="cell-title" href={`/events/${encodeURIComponent(e.id)}`}>
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
    { key: 'host', label: 'host', hideOnPhone: true, render: (e) => e.host?.name ?? <span class="muted">—</span> },
    { key: 'reg', label: 'registered', num: true, render: (e) => e.metrics.registered },
    {
      key: 'checkin',
      label: 'checked in',
      render: (e) =>
        upcoming(e, now) ? <span class="muted small">not yet</span> : <RateCell rate={e.metrics.checkInRate} label={`${e.name} check-in rate`} />,
    },
    {
      key: 'refl',
      label: 'reflected',
      render: (e) =>
        upcoming(e, now) ? (
          <span class="muted small">not yet</span>
        ) : (
        <span>
          <RateCell rate={e.metrics.reflectionRate} label={`${e.name} reflection rate`} />
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

export function RateCell({ rate, label }: { rate: number | null; label: string }) {
  return (
    <span class="rate-cell">
      <span class="num">{pct(rate)}</span>
      <Meter value={rate} label={label} />
    </span>
  );
}

export default function OverviewView() {
  const state = useApi('overview');
  return (
    <Async state={state} loading={<Loading tiles={5} rows={5} label="Loading the overview" />}>
      {(d) => {
        const k = d.kpis;
        const over = k.remainingCents < 0;
        return (
          <div class="stack">
            <section aria-label="Key figures">
              <div class="kpis">
                <Kpi label="upcoming events" value={k.upcomingEvents} href="/events?when=upcoming" sub="on the calendar" />
                <Kpi label="avg check-in rate" value={pct(k.avgCheckInRate)} sub={`past events, FY${d.fiscalYear}`} />
                <Kpi label="avg reflection rate" value={pct(k.avgReflectionRate)} sub="of checked-in guests" />
                <Kpi
                  label="budget remaining"
                  value={dollars(k.remainingCents)}
                  tone={over ? 'negative' : undefined}
                  href="/finance"
                  sub={`${dollars(k.spentCents)} spent · ${dollars(k.committedCents)} committed of ${dollars(k.budgetCents)}`}
                />
                <Kpi
                  label="pending requests"
                  value={k.pendingRequests}
                  href="/requests?scope=all&status=pending"
                  sub={k.pendingRequests ? `${dollars(k.pendingRequestsCents)} awaiting a director` : 'nothing waiting'}
                />
              </div>
            </section>

            <Section title="recent events" id="recent-events" actions={<a href="/events">all events →</a>}>
              {d.recentEvents.length === 0 ? (
                <Empty title="No events yet" action={<a href="/events/import">import a Luma guest export →</a>}>
                  Events appear here once a Luma guest list is imported.
                </Empty>
              ) : (
                <DataTable rows={d.recentEvents} columns={eventColumns()} rowKey={(e) => e.id} caption="Recent events" />
              )}
            </Section>
          </div>
        );
      }}
    </Async>
  );
}
