import { useEffect, useState } from 'preact/hooks';
import { useApi } from '../../lib/api/useApi';
import { addDays, monthGrid, monthLabel, monthOf, mondayOf, pacificToday, weekLabel, whenShort } from '../../lib/calendar';
import type { EventListItem } from '../../lib/contracts';
import { dollars, fmtDate, fmtTime, fmtWeekday, lumaManageUrl, pct, plural, safeUrl } from '../../lib/format';
import { setParams, useSearch } from '../../lib/nav';
import { href } from '../../lib/routes';
import { ColumnChart, FunnelChart, LineChart } from '../components/charts';
import { CsvImportForm } from '../components/CsvImportForm';
import { Drawer } from '../components/Drawer';
import { Icon } from '../components/Icon';
import { type Column, DataTable, Kpi, Section } from '../components/kit';
import { Async, Empty, ErrorState, Loading, Notice } from '../components/states';
import { requestColumns } from './columns';
import { MonthView, WeekView } from './events/EventCalendar';
import { EventEditor } from './events/EventEditor';
import { HandoffKit } from './events/HandoffKit';
import { DeleteEvent } from './events/DeleteEvent';
import { designRequestHref, EventStatusBadge, hostLabel, listOrder, openPatch } from './events/shared';

type View = 'list' | 'month' | 'week';
const VIEWS: { key: View; label: string }[] = [
  { key: 'list', label: 'List' },
  { key: 'month', label: 'Month' },
  { key: 'week', label: 'Week' },
];
/** Every panel the page can show; opening one closes the others (except stacked editor → design/delete). */
const PANELS = { id: null, edit: null, new: null, kit: null, delete: null, import: null } as const;

const isMonth = (v: string | null) => !!v && /^\d{4}-\d{2}$/.test(v);
const isDate = (v: string | null) => !!v && /^\d{4}-\d{2}-\d{2}$/.test(v);

function openEvent(e: EventListItem) {
  setParams({ ...PANELS, ...openPatch(e) });
}

function listColumns(now: number): Column<EventListItem>[] {
  return [
    {
      key: 'name',
      label: 'Event · Vancouver time',
      cardLabel: 'Event',
      primary: true,
      render: (e) => (
        <>
          {e.status === 'draft' ? (
            <a class="cell-title" href={href.eventEdit(e.id)} data-event-edit={e.id}>
              {e.name}
            </a>
          ) : (
            <a class="cell-title" href={href.event(e.id)} data-event-link={e.id}>
              {e.name}
            </a>
          )}
          <span class="cell-sub">
            {whenShort(e.startsAt, e.endsAt)}
            {e.location && (
              <>
                <br />
                {e.location}
              </>
            )}
          </span>
        </>
      ),
    },
    { key: 'host', label: 'Host', render: (e) => hostLabel(e) ?? <span class="muted">—</span> },
    { key: 'status', label: 'Status', render: (e) => <EventStatusBadge event={e} now={now} /> },
    {
      key: 'reg',
      label: 'Registered / capacity',
      render: (e) =>
        e.metrics.registered === 0 && e.status === 'draft' ? (
          <span class="muted">{e.capacity ? `— / ${e.capacity}` : '—'}</span>
        ) : (
          `${e.metrics.registered}${e.capacity ? ` / ${e.capacity}` : ''}`
        ),
    },
    {
      key: 'handoff',
      label: 'Handoff',
      render: (e) => {
        const luma = safeUrl(e.lumaUrl);
        if (luma)
          return (
            <a href={href.eventKit(e.id)} data-handoff={e.id}>
              Luma linked
            </a>
          );
        if (e.metrics.registered > 0 || e.lumaId) return <span>CSV imported</span>;
        return (
          <a class="btn btn--small" href={href.eventKit(e.id)} data-handoff={e.id}>
            Open kit
          </a>
        );
      },
    },
  ];
}

/** The most recent past event with turnout figures. */
function healthEvent(items: EventListItem[], now: number) {
  return items
    .filter((e) => new Date(e.startsAt).getTime() < now && e.metrics.registered > 0)
    .sort((a, b) => b.startsAt.localeCompare(a.startsAt))[0];
}

function EventHealth({ e }: { e: EventListItem }) {
  const m = e.metrics;
  const noShow = Math.max(0, m.registered - m.checkedIn);
  const noReflection = Math.max(0, m.checkedIn - m.matchedReflections);
  return (
    <section class="block" aria-labelledby="health-h" id="health">
      <div class="block-head">
        <h2 id="health-h">Event health · {e.name}</h2>
      </div>
      <div class="kpis">
        <Kpi label="Registered" value={m.registered} sub={`${fmtDate(e.startsAt)} · from CSV`} />
        <Kpi label="Checked in" value={m.checkedIn} sub={`${pct(m.checkInRate)} of registrations`} />
        <Kpi label="Reflections" value={m.matchedReflections} sub={m.checkedIn ? `${pct(m.reflectionRate)} of attendees` : 'none yet'} />
      </div>
      {(noShow > 0 || noReflection > 0) && (
        <p class="notice notice--attention">
          {noShow > 0 && `${plural(noShow, 'registered guest')} did not check in. `}
          {noReflection > 0 && `${plural(noReflection, 'attendee')} ${noReflection === 1 ? 'has' : 'have'} no reflection yet.`}
        </p>
      )}
      <p class="small muted">Figures update after a CSV import; they are not live Luma totals.</p>
    </section>
  );
}

export default function EventsView() {
  const search = useSearch();
  const v = search.get('view');
  const view: View = v === 'month' || v === 'week' ? v : 'list';
  const today = pacificToday();
  const month = isMonth(search.get('m')) ? search.get('m')! : monthOf(today);
  const monday = isDate(search.get('w')) ? mondayOf(search.get('w')!) : mondayOf(today);
  const urlQ = search.get('q') ?? '';
  const openId = search.get('id');
  const editId = search.get('edit');
  const creating = search.get('new') === '1';
  const kitId = search.get('kit');
  const deleteId = search.get('delete');
  const importing = search.get('import') === '1';

  const [q, setQ] = useState(urlQ);
  const [debounced, setDebounced] = useState(urlQ);
  const [deleted, setDeleted] = useState<string | null>(null);
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

  // The list reads every event (tens per year); the calendars ask for their visible range.
  const grid = monthGrid(month);
  const range = view === 'month' ? { from: grid[0]!.date, to: grid[grid.length - 1]!.date } : view === 'week' ? { from: monday, to: addDays(monday, 6) } : {};
  const query = { when: 'all' as const, q: view === 'list' ? debounced || undefined : undefined, limit: 200, ...range };
  const state = useApi('eventsList', { query });
  const all = useApi('eventsList', { query: { when: 'all', limit: 200 } });
  const reload = () => {
    state.reload();
    all.reload();
  };

  const now = Date.now();
  const ready = state.status === 'ready' ? state.data : null;
  const total = all.status === 'ready' ? all.data.items.length : null;
  const upcoming = all.status === 'ready' ? all.data.items.filter((e) => new Date(e.startsAt).getTime() >= now).length : 0;
  const period =
    state.status === 'loading'
      ? 'Loading events…'
      : state.status === 'error'
        ? 'Events unavailable'
        : view === 'month'
          ? monthLabel(month)
          : view === 'week'
            ? weekLabel(monday)
            : total === 0
              ? 'No events'
              : debounced
                ? plural(ready!.items.length, 'matching event')
                : `${plural(ready!.items.length, 'event')} · ${upcoming} upcoming`;

  const health = all.status === 'ready' ? healthEvent(all.data.items, now) : undefined;
  const closeAll = () => setParams({ ...PANELS });
  /** After a delete: wait until every dialog has closed (each returns focus to its opener), then focus the heading. */
  const focusPeriod = () => {
    let tries = 0;
    const step = () => {
      if (document.querySelector('dialog[open]') && tries++ < 60) return void requestAnimationFrame(step);
      requestAnimationFrame(() => document.getElementById('period')?.focus());
    };
    requestAnimationFrame(step);
  };

  return (
    <div class="stack events-page">
      <div class="toolbar">
        <div class="toolbar-main">
          {view === 'list' && (
            <div class="toolbar-search">
              <label class="field-label" for="event-search">
                Search events
              </label>
              <input id="event-search" class="input" type="search" placeholder="Name or location" value={q} onInput={(e) => setQ(e.currentTarget.value)} />
            </div>
          )}
        </div>
        <div class="toolbar-actions action-row">
          <a class="btn" href={href.eventImport()} data-open-import>
            <Icon name="upload" size={16} /> Import CSV
          </a>
          <a class="btn btn--primary" href={href.eventNew()} data-new-event>
            <Icon name="plus" size={16} /> New event
          </a>
        </div>
      </div>

      <div class="cal-head">
        <h2 id="period" tabIndex={-1}>
          {period}
        </h2>
        {total !== 0 && (
          <div class="segmented" role="group" aria-label="Event view">
            {VIEWS.map((x) => (
              <button
                type="button"
                class="segment"
                aria-pressed={view === x.key}
                key={x.key}
                disabled={state.status === 'error'}
                onClick={() => setParams({ view: x.key === 'list' ? null : x.key }, { replace: true })}
              >
                {x.label}
              </button>
            ))}
          </div>
        )}
      </div>

      {deleted && (
        <p class="notice notice--positive" role="status" id="delete-note">
          {deleted} deleted. Remove any published Luma event separately.
        </p>
      )}

      {state.status === 'loading' ? (
        <Loading rows={6} label="Loading events" />
      ) : state.status === 'error' ? (
        <ErrorState error={state.error} onRetry={reload} title="Events couldn’t load" />
      ) : total === 0 ? (
        <div class="state" data-state="empty">
          <p class="state-title">No events yet</p>
          <p class="state-body">Plan your next CSEED gathering or import an existing Luma event.</p>
          <div class="state-action action-row">
            <a class="btn btn--primary" href={href.eventNew()}>
              Create first event
            </a>
            <a class="btn" href={href.eventImport()}>
              Import a Luma CSV
            </a>
          </div>
        </div>
      ) : view === 'month' ? (
        <div data-state="ready">
          <MonthView
            month={month}
            events={ready!.items}
            onOpen={openEvent}
            onMonth={(m) => setParams({ m: m === monthOf(today) ? null : m }, { replace: true })}
            onWeek={(w) => setParams({ view: 'week', w }, { replace: true })}
          />
        </div>
      ) : view === 'week' ? (
        <div data-state="ready">
          <WeekView monday={monday} events={ready!.items} onOpen={openEvent} onWeek={(w) => setParams({ w: w === mondayOf(today) ? null : w }, { replace: true })} />
        </div>
      ) : ready!.items.length === 0 ? (
        <Empty
          title="No matching events"
          action={
            <button
              type="button"
              class="linkish"
              onClick={() => {
                setQ('');
                setParams({ q: null }, { replace: true });
              }}
            >
              clear the search
            </button>
          }
        >
          Nothing matches “{debounced}”.
        </Empty>
      ) : (
        <section id="view-list" data-state="ready">
          <DataTable rows={listOrder(ready!.items, now)} columns={listColumns(now)} rowKey={(e) => e.id} caption="Events" />
          {ready!.nextCursor && <p class="muted small">Showing the first {ready!.items.length} events. Narrow the search to find older ones.</p>}
        </section>
      )}

      {health && state.status === 'ready' && <EventHealth e={health} />}

      <Drawer id="event-panel" open={!!openId} onClose={() => openId && setParams({ id: null })} title={title} eyebrow="event" size="lg">
        {openId && <EventDetail key={openId} id={openId} onChanged={reload} onTitle={setTitle} />}
      </Drawer>

      <Drawer id="editor" open={creating || !!editId} onClose={() => (creating || editId) && setParams({ new: null, edit: null })} title={editId ? 'Event editor' : 'New event'} size="lg">
        {(creating || editId) && (
          <EventEditor
            eventId={editId}
            onSaved={(id, created) => {
              reload();
              if (created) setParams({ new: null, edit: id }, { replace: true });
            }}
            onOpenKit={(id) => setParams({ ...PANELS, kit: id })}
            onRequestDesign={(id) => location.assign(designRequestHref(id))}
            onDelete={(id) => setParams({ delete: id })}
          />
        )}
      </Drawer>

      <Drawer id="handoff" open={!!kitId} onClose={() => kitId && setParams({ kit: null })} title="Luma handoff kit" size="lg">
        {kitId && <HandoffKit key={kitId} eventId={kitId} onChanged={reload} />}
      </Drawer>

      <Drawer id="delete" open={!!deleteId} onClose={() => deleteId && setParams({ delete: null })} title="Delete event?">
        {deleteId && (
          <DeleteEvent
            key={deleteId}
            eventId={deleteId}
            onCancel={() => setParams({ delete: null })}
            onDeleted={(name) => {
              setDeleted(name);
              closeAll();
              reload();
              focusPeriod();
            }}
          />
        )}
      </Drawer>

      <Drawer id="import-dialog" kind="dialog" open={importing} onClose={() => importing && setParams({ import: null })} title="Import event data">
        <CsvImportForm onImported={() => reload()} />
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
  const luma = safeUrl(e.lumaUrl) ?? safeUrl(e.url) ?? lumaManageUrl(e.lumaId);
  const sub = [e.location, e.host ? `hosted by ${e.host.name}` : null].filter(Boolean).join(' · ');

  return (
    <div class="stack" data-state="ready" data-event-detail={e.id}>
      <div class="detail-meta">
        <p class="page-sub">
          <EventStatusBadge event={e} /> · {fmtWeekday(e.startsAt)} {fmtDate(e.startsAt)}, {fmtTime(e.startsAt)}
          {sub ? ` · ${sub}` : ''}
        </p>
        <div class="action-row">
          <a class="btn btn--small" href={href.eventEdit(e.id)} data-edit-event>
            Edit
          </a>
          <a class="btn btn--small" href={href.eventKit(e.id)}>
            Luma handoff kit
          </a>
          <a class="btn btn--small" href={designRequestHref(e.id)} data-request-design>
            Request design
          </a>
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
          <a class="btn btn--small" href={href.eventDelete(e.id)}>
            Delete
          </a>
        </div>
      </div>

      <section aria-label="Turnout figures">
        <div class="kpis kpis--4">
          <Kpi label="registered" icon="user" hue="blue" value={m.registered} sub={e.lumaId ? <span class="mono">{e.lumaId}</span> : 'no Luma id'} />
          <Kpi label="checked in" icon="check" hue="teal" value={upcoming && m.checkedIn === 0 ? '—' : m.checkedIn} sub={upcoming && m.checkedIn === 0 ? 'after the event' : `${pct(m.checkInRate)} of registered`} />
          <Kpi label="reflections" icon="message" hue="violet" value={m.matchedReflections} sub={m.checkedIn ? `${pct(m.reflectionRate)} of checked in` : 'none yet'} />
          <Kpi label="spent" icon="wallet" hue="green" value={dollars(spentCents)} sub={plural(requests.length, 'request')} />
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
            { label: 'registered', value: m.registered, hue: 'blue' },
            { label: 'checked in', value: m.checkedIn, rate: m.checkInRate, hue: 'teal' },
            { label: 'reflected', value: m.matchedReflections, rate: m.reflectionRate, hue: 'violet' },
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
