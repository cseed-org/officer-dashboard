import { useEffect, useMemo, useState } from 'preact/hooks';
import { api } from '../../../lib/api/client';
import { useApi } from '../../../lib/api/useApi';
import { addDays, monthLabel, monthOf, mondayOf, pacificToday, weekLabel } from '../../../lib/calendar';
import type { CalendarFeedItem, CalendarFeedSource, CurrentUser, OrgCalendar, OrgCalendarEvent, OrgCalendarKey } from '../../../lib/contracts';
import { setParams, useSearch } from '../../../lib/nav';
import {
  CAL_KEYS,
  FALLBACK_CALENDARS,
  OVERLAYS,
  OVERLAY_SOURCES,
  loadToggles,
  occurrenceKey,
  orgEntry,
  overlayEntry,
  overlayGroups,
  overlayTitle,
  saveToggles,
  stepView,
  viewRange,
  type OverlayKey,
  type Toggles,
} from '../../../lib/orgcal';
import { effectiveTimezone } from '../../../lib/timezone';
import type { CalEntry } from '../../components/calendar/layout';
import { MonthView } from '../../components/calendar/MonthView';
import { WeekView } from '../../components/calendar/WeekView';
import { Drawer } from '../../components/Drawer';
import { Icon } from '../../components/Icon';
import { Empty, ErrorState, errorMessage, Loading, Notice } from '../../components/states';
import { ItemCard } from './ItemCard';
import { MeetingForm } from './MeetingForm';
import { MiniMonth } from './MiniMonth';

type View = 'month' | 'week';
const isMonth = (s: string | null): s is string => !!s && /^\d{4}-\d{2}$/.test(s);
const isDate = (s: string | null): s is string => !!s && /^\d{4}-\d{2}-\d{2}$/.test(s);
/** Every panel parameter; opening one closes the others. */
const PANELS = { id: null, at: null, edit: null, new: null, delete: null } as const;

type Item = { kind: 'org'; event: OrgCalendarEvent; calendar: OrgCalendar } | { kind: 'feed'; items: CalendarFeedItem[]; source: CalendarFeedSource | undefined };

export default function CalendarView({ me }: { me: CurrentUser }) {
  const search = useSearch();
  const view: View = search.get('view') === 'week' ? 'week' : 'month';
  const today = pacificToday();
  const wParam = search.get('w');
  const mParam = search.get('m');
  // The week follows ?w; a month-only link picks its first week. The month follows ?m, else the week.
  const monday = isDate(wParam) ? mondayOf(wParam) : isMonth(mParam) && mParam !== monthOf(today) ? mondayOf(`${mParam}-01`) : mondayOf(today);
  const month = isMonth(mParam) ? mParam : isDate(wParam) ? monthOf(addDays(mondayOf(wParam), 3)) : monthOf(today);
  const openId = search.get('id');
  const openAt = search.get('at');
  const editId = search.get('edit');
  const newParam = search.get('new');
  const deleteId = search.get('delete');

  const [toggles, setToggles] = useState<Toggles>(() => loadToggles());
  const flip = (k: keyof Toggles) =>
    setToggles((t) => {
      const next = { ...t, [k]: !t[k] };
      saveToggles(next);
      return next;
    });

  // Phones start with the calendar list folded so the agenda comes first.
  const [railOpen] = useState(() => typeof matchMedia === 'undefined' || !matchMedia('(max-width: 767px)').matches);
  const range = viewRange(view, month, monday);
  const cals = useApi('orgCalendars');
  const events = useApi('orgEvents', { query: { from: range.from, to: range.to } }, range);
  const feed = useApi('calendarFeed', { query: { start: range.from, end: range.to, sources: OVERLAY_SOURCES.join(',') } }, range);
  const leadership = me.role === 'leadership';
  const google = useApi('orgGoogleStatus', {}, null, leadership);

  const calendars = cals.status === 'ready' ? cals.data.calendars : FALLBACK_CALENDARS;
  const calOf = (k: OrgCalendarKey) => calendars.find((c) => c.key === k) ?? FALLBACK_CALENDARS.find((c) => c.key === k)!;
  const notConnected = leadership ? google.status === 'ready' && !google.data.connected : cals.status === 'ready' && cals.data.calendars.every((c) => !c.googleCalendarId);

  const [flash, setFlash] = useState<string | null>(null);
  useEffect(() => {
    if (!flash) return;
    const t = setTimeout(() => setFlash(null), 6000);
    return () => clearTimeout(t);
  }, [flash]);

  // Entries for the visible calendars and overlays, with a lookup back to the source item.
  const { entries, items } = useMemo(() => {
    const items = new Map<string, Item>();
    const entries: CalEntry[] = [];
    if (events.status === 'ready') {
      for (const e of events.data.events) {
        if (!toggles[e.calendarKey]) continue;
        const c = calOf(e.calendarKey);
        const entry = orgEntry(e, c.hue, c.name);
        if (items.has(entry.id)) continue;
        items.set(entry.id, { kind: 'org', event: e, calendar: c });
        entries.push(entry);
      }
    }
    if (feed.status === 'ready') {
      const sources = new Map(feed.data.sources.map((s) => [s.id, s]));
      const shown = feed.data.items.filter((it) => OVERLAYS.some((o) => toggles[o.key] && (o.sources as readonly string[]).includes(it.source)));
      for (const group of overlayGroups(shown)) {
        const source = sources.get(group[0]!.source);
        const entry = overlayEntry(group, source);
        if (items.has(entry.id)) continue;
        items.set(entry.id, { kind: 'feed', items: group, source });
        entries.push(entry);
      }
    }
    return { entries, items };
  }, [events, feed, toggles, calendars]);

  const allEvents = events.status === 'ready' ? events.data.events : [];
  const byOccurrence = (id: string | null, at: string | null) => (id ? (allEvents.find((e) => e.id === id && (!at || occurrenceKey(e) === at)) ?? allEvents.find((e) => e.id === id) ?? null) : null);
  const opened: Item | null = openId?.startsWith('feed~')
    ? (items.get(openId) ?? null)
    : (() => {
        const e = byOccurrence(openId, openAt);
        return e ? { kind: 'org' as const, event: e, calendar: calOf(e.calendarKey) } : null;
      })();
  // Only manual meetings open in the editor; Weekly Programs and Events items are read-only.
  const manual = (e: OrgCalendarEvent | null) => (e && e.sourceKind === 'manual' && !calOf(e.calendarKey).locked ? e : null);
  const editTarget = byOccurrence(editId, openAt);
  const editing = manual(editTarget);
  const deleting = manual(byOccurrence(deleteId, null));

  const open = (en: CalEntry) => {
    const it = items.get(en.id);
    if (!it) return;
    if (it.kind === 'feed') setParams({ ...PANELS, id: en.id });
    else setParams({ ...PANELS, id: it.event.id, at: occurrenceKey(it.event) });
  };
  const go = (p: { m: string; w: string }) => setParams({ m: p.m === monthOf(today) ? null : p.m, w: p.w === mondayOf(today) ? null : p.w }, { replace: true });
  const reload = () => events.reload();

  const label = view === 'month' ? monthLabel(month) : weekLabel(monday);
  const inView = (d: string) => view === 'week' && d >= monday && d < addDays(monday, 7);
  const defaultKey = toggles.internal || !toggles.external ? 'internal' : 'external';

  return (
    <div class="orgcal" data-view={view}>
      <div class="orgcal-bar" role="toolbar" aria-label="Calendar">
        <button type="button" class="btn" onClick={() => go({ m: monthOf(today), w: mondayOf(today) })}>
          Today
        </button>
        <span class="orgcal-step">
          <button type="button" class="icon-btn" aria-label={`Previous ${view}`} onClick={() => go(stepView(view, month, monday, -1))}>
            ‹
          </button>
          <button type="button" class="icon-btn" aria-label={`Next ${view}`} onClick={() => go(stepView(view, month, monday, 1))}>
            ›
          </button>
        </span>
        <h2 class="orgcal-period" id="orgcal-period" tabIndex={-1} aria-live="polite">
          {label}
          <span class="orgcal-tz"> · {effectiveTimezone()}</span>
        </h2>
        <span class="orgcal-bar-end">
          <span class="segmented" role="group" aria-label="Calendar view">
            {(['month', 'week'] as View[]).map((v) => (
              <button type="button" class="segment" key={v} aria-pressed={view === v} onClick={() => setParams({ view: v === 'month' ? null : v }, { replace: true })}>
                {v === 'month' ? 'Month' : 'Week'}
              </button>
            ))}
          </span>
          <button type="button" class="btn btn--primary orgcal-create" onClick={() => setParams({ ...PANELS, new: 1 })}>
            <Icon name="plus" size={16} /> Create
          </button>
        </span>
      </div>

      <div class="orgcal-layout">
        <aside class="orgcal-rail" aria-label="Calendars">
          <MiniMonth
            month={month}
            inView={inView}
            onPick={(d) => go({ m: monthOf(d), w: mondayOf(d) })}
          />
          <details class="orgcal-filters" open={railOpen}>
            <summary class="orgcal-filters-summary">Calendars and overlays</summary>
            <fieldset class="orgcal-toggles">
              <legend class="orgcal-legend">Calendars</legend>
              {CAL_KEYS.map((k) => {
                const c = calOf(k);
                return (
                  <label class="orgcal-toggle" key={k} style={{ '--hue': `var(--c-${c.hue})` }} data-cal={k}>
                    <input type="checkbox" checked={toggles[k]} onChange={() => flip(k)} />
                    <span class="orgcal-swatch" aria-hidden="true" />
                    <span class="orgcal-toggle-name">{c.name}</span>
                    {c.locked && (
                      <span class="orgcal-lock" title="Changes come from the Events page and Buildspace programs">
                        locked
                      </span>
                    )}
                  </label>
                );
              })}
            </fieldset>
            <fieldset class="orgcal-toggles">
              <legend class="orgcal-legend">Dashboard overlays</legend>
              {OVERLAYS.map((o) => (
                <label class="orgcal-toggle" key={o.key} style={{ '--hue': `var(--c-${o.hue})` }} data-overlay={o.key}>
                  <input type="checkbox" checked={toggles[o.key as OverlayKey]} onChange={() => flip(o.key as OverlayKey)} />
                  <span class="orgcal-swatch orgcal-swatch--overlay" aria-hidden="true" />
                  <span class="orgcal-toggle-name">{o.label}</span>
                </label>
              ))}
              <p class="orgcal-rail-note">Overlays show on this page only; they are not added to Google Calendar.</p>
            </fieldset>
          </details>
        </aside>

        <div class="orgcal-main">
          {notConnected && (
            <div class="notice notice--attention orgcal-google" role="status" data-google="not-connected">
              Google Calendar isn't connected yet. Meetings are saved here and sync to the organization's Google calendars once leadership connects the org account.
              {leadership && (
                <>
                  {' '}
                  <a href="/api/settings/org-google/connect" data-reload>
                    Connect the org Google account →
                  </a>
                </>
              )}
            </div>
          )}
          {flash && <Notice tone="positive">{flash}</Notice>}
          {editId && events.status === 'ready' && !editing && (
            <Notice tone="attention">{editTarget ? 'Weekly Programs and Events items are read-only here. Change them from their source.' : `That meeting isn't in this ${view}. Open it from its date to edit it.`}</Notice>
          )}
          {feed.status === 'error' && <Notice tone="attention">Dashboard overlays couldn't load: {errorMessage(feed.error)}</Notice>}
          {events.status === 'error' ? (
            <ErrorState error={events.error} onRetry={reload} />
          ) : events.status === 'loading' ? (
            <Loading label="Loading the calendar" rows={6} />
          ) : (
            <>
              {view === 'month' ? (
                <MonthView month={month} entries={entries} onOpen={open} onWeek={(w) => setParams({ view: 'week', w, m: null }, { replace: true })} onDayClick={(d) => setParams({ ...PANELS, new: d })} />
              ) : (
                <WeekView monday={monday} entries={entries} onOpen={open} onSlotClick={(d, min) => setParams({ ...PANELS, new: `${d}T${String(Math.floor(min / 60)).padStart(2, '0')}:${String(min % 60).padStart(2, '0')}` })} nowLine />
              )}
              {entries.length === 0 && (
                <Empty title={`Nothing on the calendar for ${label}`} action={<button type="button" class="btn" onClick={() => setParams({ ...PANELS, new: 1 })}>Create a meeting</button>}>
                  {CAL_KEYS.some((k) => !toggles[k]) ? 'Some calendars are hidden. Turn them on in the list of calendars to see more.' : 'Create a meeting, or pick another period.'}
                </Empty>
              )}
            </>
          )}
        </div>
      </div>

      <Drawer
        id="orgcal-item"
        kind="dialog"
        open={!!openId && !!opened}
        onClose={() => openId && setParams({ id: null, at: null })}
        title={opened ? (opened.kind === 'org' ? opened.event.title : overlayTitle(opened.items)) : 'Calendar item'}
        eyebrow={opened ? (opened.kind === 'org' ? opened.calendar.name : (opened.source?.label ?? 'Dashboard')) : undefined}
      >
        {opened && (
          <ItemCard
            item={opened}
            onEdit={(e) => setParams({ ...PANELS, edit: e.id, at: occurrenceKey(e) })}
            onDelete={(e) => setParams({ ...PANELS, delete: e.id })}
          />
        )}
      </Drawer>

      <Drawer
        id="orgcal-editor"
        open={newParam !== null || !!editing}
        onClose={() => (newParam !== null || editId) && setParams({ new: null, edit: null, at: null })}
        title={editing ? 'Edit meeting' : 'New meeting'}
        eyebrow="Organization calendar"
        size="lg"
      >
        {(newParam !== null || editing) && (
          <MeetingForm
            event={editing}
            newParam={newParam ?? '1'}
            calendars={calendars}
            defaultKey={defaultKey}
            onCancel={() => setParams({ new: null, edit: null, at: null })}
            onDelete={(e) => setParams({ ...PANELS, delete: e.id })}
            onSaved={(e, created) => {
              setParams({ ...PANELS }, { replace: true });
              setFlash(created ? `“${e.title}” added to ${calOf(e.calendarKey).name}.` : `“${e.title}” saved.`);
              reload();
            }}
          />
        )}
      </Drawer>

      <Drawer id="orgcal-delete" kind="dialog" open={!!deleting} onClose={() => deleteId && setParams({ delete: null })} title="Delete meeting?">
        {deleting && (
          <ConfirmDelete
            event={deleting}
            onCancel={() => setParams({ delete: null })}
            onDone={() => {
              setParams({ ...PANELS }, { replace: true });
              setFlash(`“${deleting.title}” deleted.`);
              reload();
              requestAnimationFrame(() => document.getElementById('orgcal-period')?.focus());
            }}
          />
        )}
      </Drawer>
    </div>
  );
}

function ConfirmDelete({ event, onCancel, onDone }: { event: OrgCalendarEvent; onCancel: () => void; onDone: () => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  return (
    <div class="stack">
      <p>
        “{event.title}” will be removed from the dashboard and from Google Calendar
        {event.rrule ? ', every occurrence included' : ''}.{event.sendInvites ? ' Invitees are told it was cancelled.' : ''}
      </p>
      {error && <Notice tone="negative">{error}</Notice>}
      <div class="action-row">
        <button
          type="button"
          class="btn btn--danger"
          disabled={busy}
          onClick={async () => {
            setBusy(true);
            setError(null);
            try {
              await api('orgEventDelete', { params: { id: event.id } });
              onDone();
            } catch (err) {
              setError(errorMessage(err));
              setBusy(false);
            }
          }}
        >
          {busy ? 'Deleting…' : 'Delete meeting'}
        </button>
        <button type="button" class="btn" onClick={onCancel} disabled={busy}>
          Keep it
        </button>
      </div>
    </div>
  );
}
