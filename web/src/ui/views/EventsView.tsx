import { useEffect, useState } from 'preact/hooks';
import { useApi } from '../../lib/api/useApi';
import { dollars, fmtDate, fmtTime, fmtWeekday, lumaManageUrl, pct, plural, safeUrl } from '../../lib/format';
import { setParams, useSearch } from '../../lib/nav';
import { href } from '../../lib/routes';
import { ColumnChart, FunnelChart, LineChart } from '../components/charts';
import { CsvImportForm } from '../components/CsvImportForm';
import { Drawer } from '../components/Drawer';
import { Icon } from '../components/Icon';
import { DataTable, Kpi, Section } from '../components/kit';
import { Async, Empty, ErrorState, Loading, Notice } from '../components/states';
import { eventColumns, requestColumns } from './columns';

type When = 'all' | 'upcoming' | 'past';
const WHEN: { key: When; label: string }[] = [
  { key: 'all', label: 'all' },
  { key: 'upcoming', label: 'upcoming' },
  { key: 'past', label: 'past' },
];

export default function EventsView() {
  const search = useSearch();
  const w = search.get('when');
  const when: When = w === 'upcoming' || w === 'past' ? w : 'all';
  const urlQ = search.get('q') ?? '';
  const openId = search.get('id');
  const importing = search.get('import') === '1';

  const [q, setQ] = useState(urlQ);
  const [debounced, setDebounced] = useState(urlQ);
  useEffect(() => setQ(urlQ), [urlQ]);
  useEffect(() => {
    const t = setTimeout(() => setDebounced(q.trim()), 250);
    return () => clearTimeout(t);
  }, [q]);
  useEffect(() => {
    if (debounced !== urlQ) setParams({ q: debounced || null }, { replace: true });
  }, [debounced]);

  const [title, setTitle] = useState('event');
  useEffect(() => setTitle('event'), [openId]);
  const state = useApi('eventsList', { query: { when, q: debounced || undefined, limit: 100 } });

  return (
    <div class="stack">
      <div class="toolbar">
        <div class="toolbar-main">
          <div class="segmented" role="group" aria-label="Show events">
            {WHEN.map((x) => (
              <button type="button" class="segment" aria-pressed={when === x.key} onClick={() => setParams({ when: x.key === 'all' ? null : x.key }, { replace: true })} key={x.key}>
                {x.label}
              </button>
            ))}
          </div>
          <div class="toolbar-search">
            <label class="visually-hidden" for="event-search">
              Search events
            </label>
            <input id="event-search" class="input" type="search" placeholder="search by name or place" value={q} onInput={(e) => setQ(e.currentTarget.value)} />
          </div>
        </div>
        <div class="toolbar-actions">
          <a class="btn btn--primary" href={href.eventImport()} data-open-import>
            <Icon name="upload" size={16} /> import CSV
          </a>
        </div>
      </div>

      <Async state={state} loading={<Loading rows={6} label="Loading events" />}>
        {(d) =>
          d.items.length === 0 ? (
            debounced || when !== 'all' ? (
              <Empty
                title="No matching events"
                action={
                  <button
                    type="button"
                    class="linkish"
                    onClick={() => {
                      setQ('');
                      setParams({ q: null, when: null }, { replace: true });
                    }}
                  >
                    clear filters
                  </button>
                }
              >
                Nothing {when !== 'all' ? when : ''} matches{debounced ? ` “${debounced}”` : ''}.
              </Empty>
            ) : (
              <Empty title="No events yet" action={<a href={href.eventImport()}>import a Luma guest export →</a>}>
                Import the guest CSV from Luma to start tracking registrations, check-ins and reflections.
              </Empty>
            )
          ) : (
            <>
              <DataTable rows={d.items} columns={eventColumns()} rowKey={(e) => e.id} caption="Events" />
              {d.nextCursor && <p class="muted small">Showing the first {d.items.length} events. Narrow the search to find older ones.</p>}
            </>
          )
        }
      </Async>

      <Drawer id="event-panel" open={!!openId} onClose={() => openId && setParams({ id: null })} title={title} eyebrow="event" size="lg">
        {openId && <EventDetail key={openId} id={openId} onChanged={state.reload} onTitle={setTitle} />}
      </Drawer>

      <Drawer id="import-dialog" kind="dialog" open={importing} onClose={() => importing && setParams({ import: null })} title="import a CSV">
        <CsvImportForm onImported={() => state.reload()} />
      </Drawer>
    </div>
  );
}

export function EventDetail({ id, onChanged, onTitle }: { id: string; onChanged?: () => void; onTitle?: (name: string) => void }) {
  const detail = useApi('eventDetail', { params: { id } });
  const loadedName = detail.status === 'ready' ? detail.data.event.name : null;
  useEffect(() => {
    if (loadedName) onTitle?.(loadedName);
  }, [loadedName]);
  const metrics = useApi('eventMetrics', { params: { id } });

  if (detail.status === 'loading') return <Loading tiles={4} rows={6} label="Loading the event" />;
  if (detail.status === 'error') return <ErrorState error={detail.error} onRetry={detail.reload} />;

  const { event: e, requests, spentCents } = detail.data;
  const m = e.metrics;
  const upcoming = new Date(e.startsAt).getTime() > Date.now();
  const luma = safeUrl(e.url) ?? lumaManageUrl(e.lumaId);
  const sub = [e.location, e.host ? `hosted by ${e.host.name}` : null].filter(Boolean).join(' · ');

  return (
    <div class="stack" data-state="ready" data-event-detail={e.id}>
      <div class="detail-meta">
        <p class="page-sub">
          {upcoming ? 'upcoming' : 'past'} · {fmtWeekday(e.startsAt)} {fmtDate(e.startsAt)}, {fmtTime(e.startsAt)}
          {sub ? ` · ${sub}` : ''}
        </p>
        <div class="action-row">
          {luma && (
            <a class="btn btn--small" href={luma} target="_blank" rel="noopener noreferrer">
              Luma page <span aria-hidden="true">↗</span>
            </a>
          )}
          <a class="btn btn--small" href={`#import-${e.id}`}>
            import CSV
          </a>
          <a class="btn btn--small" href={href.newRequest(e.id)}>
            new request for this event
          </a>
        </div>
      </div>

      <section aria-label="Turnout figures">
        <div class="kpis kpis--4">
          <Kpi label="registered" value={m.registered} sub={e.lumaId ? <span class="mono">{e.lumaId}</span> : 'no Luma id'} />
          <Kpi label="checked in" value={upcoming && m.checkedIn === 0 ? '—' : m.checkedIn} sub={upcoming && m.checkedIn === 0 ? 'after the event' : `${pct(m.checkInRate)} of registered`} />
          <Kpi label="reflections" value={m.matchedReflections} sub={m.checkedIn ? `${pct(m.reflectionRate)} of checked in` : 'none yet'} />
          <Kpi label="spent" value={dollars(spentCents)} sub={plural(requests.length, 'request')} />
        </div>
      </section>

      {m.unmatchedReflections > 0 && (
        <Notice tone="attention">
          <strong>
            {m.unmatchedReflections} reflection{m.unmatchedReflections === 1 ? '' : 's'} didn't match a checked-in guest
          </strong>{' '}
          and {m.unmatchedReflections === 1 ? "isn't" : "aren't"} counted in the rate. Usually a missed door scan or a typo'd email — fix it in Luma and re-import.
        </Notice>
      )}

      <Section title="turnout" id={`turnout-${e.id}`}>
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

      <Section title="registrations" id={`registrations-${e.id}`}>
        <Async state={metrics} loading={<Loading rows={3} label="Loading the chart" />}>
          {(md) =>
            md.registrationsByDay.length === 0 ? (
              <Empty title="No registration history">Import the Luma guest list to see how sign-ups built up.</Empty>
            ) : (
              <>
                <LineChart label={`Cumulative registrations for ${e.name}`} valueLabel="registered" points={md.registrationsByDay.map((p) => ({ date: p.date, value: p.cumulative }))} />
                {md.reflectionsByDay.length > 0 && (
                  <>
                    <h3 class="subhead">reflections received per day after the event</h3>
                    {m.unmatchedReflections > 0 && (
                      <p class="small muted" data-unmatched-note>
                        All responses, including {plural(m.unmatchedReflections, 'response')} that didn’t match a check-in. The reflection KPI counts only the {m.matchedReflections} matched.
                      </p>
                    )}
                    <ColumnChart label="Reflections per day" valueLabel="reflections" points={md.reflectionsByDay.map((p) => ({ date: p.date, value: p.count }))} />
                  </>
                )}
              </>
            )
          }
        </Async>
      </Section>

      <Section title="guests" id={`guests-${e.id}`}>
        <dl class="facts">
          <div>
            <dt>registered</dt>
            <dd>{m.registered}</dd>
          </div>
          <div>
            <dt>checked in</dt>
            <dd>{m.checkedIn}</dd>
          </div>
          <div>
            <dt>reflections received</dt>
            <dd>{m.reflections}</dd>
          </div>
          <div>
            <dt>matched to a check-in</dt>
            <dd>{m.matchedReflections}</dd>
          </div>
        </dl>
        <p class="muted small">Guest lists come from Luma. Re-import the export to update registrations and check-ins; guests are matched by email.</p>
      </Section>

      <Section title="spending" id={`spending-${e.id}`} actions={<a href={href.newRequest(e.id)}>new request →</a>}>
        {requests.length === 0 ? (
          <Empty title="No purchase requests">Requests linked to this event show here.</Empty>
        ) : (
          <DataTable rows={requests} columns={requestColumns({ compact: true })} rowKey={(r) => r.id} caption={`Requests for ${e.name}`} />
        )}
      </Section>

      <section class="section" id={`import-${e.id}`} aria-labelledby={`import-${e.id}-h`}>
        <div class="section-head">
          <h2 id={`import-${e.id}-h`}>import CSV</h2>
        </div>
        <CsvImportForm
          eventId={e.id}
          compact
          onImported={() => {
            detail.reload();
            metrics.reload();
            onChanged?.();
          }}
        />
      </section>
    </div>
  );
}
