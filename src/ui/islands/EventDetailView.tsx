import type { ComponentChildren } from 'preact';
import type { PurchaseRequestListItem } from '../../core/contracts';
import { useApi } from '../api/useApi';
import { ColumnChart, FunnelChart, LineChart } from '../components/charts';
import { CsvImportForm } from '../components/CsvImportForm';
import { DataTable, Kpi, Section, StatusBadge, type Column } from '../components/kit';
import { Async, Empty, ErrorState, Loading, Notice } from '../components/states';
import { dollars, lumaManageUrl, plural, safeUrl, fmtDate, fmtTime, fmtWeekday, formatCents, pct, TYPE_LABEL } from '../format';

export const requestColumns = (opts: { showRequester?: boolean } = {}): Column<PurchaseRequestListItem>[] => [
  {
    key: 'title',
    label: 'request',
    primary: true,
    render: (r) => (
      <>
        <a class="cell-title" href={`/requests/${encodeURIComponent(r.id)}`}>
          {r.title}
        </a>
        <span class="cell-sub">
          <span class="mono">#{r.number}</span> · {r.vendor} · {TYPE_LABEL[r.type]}
        </span>
      </>
    ),
  },
  ...(opts.showRequester === false ? [] : [{ key: 'who', label: 'requester', render: (r: PurchaseRequestListItem) => r.requester.name }]),
  { key: 'cat', label: 'category · fund', hideOnPhone: true, render: (r) => <span class="small">{r.expenseAccount.name} · {r.fund.code}</span> },
  { key: 'status', label: 'status', render: (r) => <StatusBadge status={r.status} /> },
  { key: 'total', label: 'total', num: true, render: (r) => formatCents(r.totalCents) },
];

function Title({ name, eyebrow, sub, children }: { name: string; eyebrow?: string; sub?: string; children?: ComponentChildren }) {
  return (
    <header class="page-title">
      <div>
        {eyebrow && <span class="eyebrow">{eyebrow}</span>}
        <h1>{name}</h1>
        {sub && <p class="page-sub">{sub}</p>}
      </div>
      {children && <div class="page-title-actions">{children}</div>}
    </header>
  );
}

export default function EventDetailView({ id }: { id: string }) {
  const detail = useApi('eventDetail', { params: { id } });
  const metrics = useApi('eventMetrics', { params: { id } });

  if (detail.status === 'loading')
    return (
      <>
        <Title name="event" eyebrow="events" />
        <Loading tiles={4} rows={6} label="Loading the event" />
      </>
    );
  if (detail.status === 'error')
    return (
      <>
        <Title name="event" eyebrow="events" />
        <ErrorState error={detail.error} onRetry={detail.reload} />
      </>
    );

  const { event: e, requests, spentCents } = detail.data;
  const m = e.metrics;
  const upcoming = new Date(e.startsAt).getTime() > Date.now();

  return (
    <div data-state="ready">
      <Title
        name={e.name}
        eyebrow={`${upcoming ? 'upcoming' : 'past'} event · ${fmtWeekday(e.startsAt)} ${fmtDate(e.startsAt)}, ${fmtTime(e.startsAt)}`}
        sub={[e.location, e.host ? `hosted by ${e.host.name}` : null].filter(Boolean).join(' · ') || undefined}
      >
        {(safeUrl(e.url) ?? lumaManageUrl(e.lumaId)) && (
          <a class="btn" href={(safeUrl(e.url) ?? lumaManageUrl(e.lumaId))!} target="_blank" rel="noopener noreferrer">
            Luma page <span aria-hidden="true">↗</span>
          </a>
        )}
        <a class="btn" href="#import">
          import CSV
        </a>
      </Title>

      <div class="stack">
        <section aria-label="Turnout figures">
          <div class="kpis" style={{ '--kpi-cols': 4 }}>
            <Kpi label="registered" value={m.registered} sub={e.lumaId ? <span class="mono">{e.lumaId}</span> : 'no Luma id'} />
            <Kpi label="checked in" value={upcoming && m.checkedIn === 0 ? '—' : m.checkedIn} sub={upcoming && m.checkedIn === 0 ? 'after the event' : `${pct(m.checkInRate)} of registered`} />
            <Kpi label="reflections" value={m.matchedReflections} sub={m.checkedIn ? `${pct(m.reflectionRate)} of checked in` : 'none yet'} />
            <Kpi label="spent on this event" value={dollars(spentCents)} sub={`${requests.length} request${requests.length === 1 ? '' : 's'}`} />
          </div>
        </section>

        {m.unmatchedReflections > 0 && (
          <Notice tone="attention">
            <strong>{m.unmatchedReflections} reflection{m.unmatchedReflections === 1 ? '' : 's'} didn't match a checked-in guest</strong> and{' '}
            {m.unmatchedReflections === 1 ? "isn't" : "aren't"} counted in the rate. Usually a missed door scan or a typo'd email — fix it in
            Luma and re-import.
          </Notice>
        )}

        <div class="cols cols--even">
          <Section title="turnout" id="turnout">
            <FunnelChart
              label={`Turnout for ${e.name}`}
              steps={[
                { label: 'registered', value: m.registered },
                { label: 'checked in', value: m.checkedIn, rate: m.checkInRate },
                { label: 'reflected', value: m.matchedReflections, rate: m.reflectionRate },
              ]}
            />
            <p class="muted small">Rates: checked in ÷ registered, then reflections from checked-in guests ÷ checked in.</p>
          </Section>

          <Section title="registrations" id="registrations">
            <Async state={metrics} loading={<Loading rows={3} label="Loading the chart" />}>
              {(md) =>
                md.registrationsByDay.length === 0 ? (
                  <Empty title="No registration history">Import the Luma guest list to see how sign-ups built up.</Empty>
                ) : (
                  <>
                    <LineChart
                      label={`Cumulative registrations for ${e.name}`}
                      valueLabel="registered"
                      points={md.registrationsByDay.map((p) => ({ date: p.date, value: p.cumulative }))}
                    />
                    {md.reflectionsByDay.length > 0 && (
                      <>
                        <h3 class="small muted" style={{ fontFamily: 'var(--font-body)', letterSpacing: 0 }}>
                          reflections received per day after the event
                        </h3>
                        {m.unmatchedReflections > 0 && (
                          <p class="small muted" style={{ margin: 0 }} data-unmatched-note>
                            All responses, including {plural(m.unmatchedReflections, 'response')} that didn’t match a check-in. The
                            reflection KPI counts only the {m.matchedReflections} matched.
                          </p>
                        )}
                        <ColumnChart
                          label="Reflections per day"
                          valueLabel="reflections"
                          points={md.reflectionsByDay.map((p) => ({ date: p.date, value: p.count }))}
                        />
                      </>
                    )}
                  </>
                )
              }
            </Async>
          </Section>
        </div>

        <Section title="spending" id="spending" actions={<a href={`/requests/new?eventId=${encodeURIComponent(e.id)}`}>new request for this event →</a>}>
          {requests.length === 0 ? (
            <Empty title="No purchase requests">Requests linked to this event will show here.</Empty>
          ) : (
            <DataTable rows={requests} columns={requestColumns()} rowKey={(r) => r.id} caption={`Requests for ${e.name}`} />
          )}
        </Section>

        <section class="section" id="import" aria-labelledby="import-h">
          <div class="section-head">
            <h2 id="import-h">import CSV</h2>
          </div>
          <CsvImportForm
            eventId={e.id}
            compact
            onImported={() => {
              detail.reload();
              metrics.reload();
            }}
          />
        </section>
      </div>
    </div>
  );
}
