import { useEffect, useRef, useState } from 'preact/hooks';
import { api } from '../../lib/api/client';
import type { CalendarFeedItem, CalendarWidgetOptions } from '../../lib/contracts';
import { addDays, addMonths, dayNumber, monthGrid, monthLabel, monthOf, mondayOf, pacificParts, pacificToday, shortDay, timeRange, weekdayIndex, weekdayShort, weekDays, weekLabel } from '../../lib/calendar';
import { effectiveTimezone } from '../../lib/timezone';
import { href } from '../../lib/routes';
import type { BlockSize } from './registry';
import '../../styles/calendar-widget.css';

type Feed = Awaited<ReturnType<typeof loadFeed>>;
const cache = new Map<string, { at: number; data: { items: CalendarFeedItem[]; timezone: string } }>();
async function loadFeed(start: string, end: string, sources: string, zone: string, fresh = false) {
  const key = JSON.stringify([start, end, sources, zone]);
  const hit = cache.get(key);
  if (!fresh && hit && Date.now() - hit.at < 60_000) return hit.data;
  const data = await api('calendarFeed', { query: { start, end, sources: sources || 'none' } });
  if (cache.size > 24) cache.clear();
  cache.set(key, { at: Date.now(), data });
  return data;
}
const defaults: CalendarWidgetOptions = { view: 'auto', sources: ['events', 'design'], colourBy: 'source', showWeekends: true };
const itemHref = (item: CalendarFeedItem) => item.source === 'events' ? href.event(item.id) : href.designRequest(item.id);
const sourceLabel = (item: CalendarFeedItem) => item.source === 'events' ? 'Club event' : 'Design deadline';
function itemHue(item: CalendarFeedItem, by: CalendarWidgetOptions['colourBy']) {
  if (by === 'source') return item.source === 'events' ? 'blue' : 'violet';
  if (item.source === 'events') return item.type === 'cancelled' ? 'coral' : item.type === 'draft' ? 'amber' : 'teal';
  return ({ poster: 'violet', social: 'blue', slides: 'teal', merch: 'amber', video: 'coral' } as Record<string, string>)[item.type] ?? 'green';
}

export default function CalendarWidget({ size }: { size: BlockSize }) {
  const [options, setOptions] = useState<CalendarWidgetOptions>(defaults);
  const [draft, setDraft] = useState(defaults);
  const [settingsReady, setSettingsReady] = useState(false);
  const [settingsError, setSettingsError] = useState('');
  const [saving, setSaving] = useState(false);
  const [anchor, setAnchor] = useState(pacificToday);
  const [zone, setZone] = useState(effectiveTimezone);
  const [feed, setFeed] = useState<Feed | null>(null);
  const [error, setError] = useState('');
  const [tick, setTick] = useState(0);
  const [selected, setSelected] = useState<string | null>(null);
  const popover = useRef<HTMLDetailsElement>(null);
  const [now, setNow] = useState(pacificToday);
  useEffect(() => {
    let alive = true;
    api('calendarWidgetGet').then(p => { if (alive) { setOptions(p); setDraft(p); setSettingsReady(true); } }).catch(() => { if (alive) setSettingsError('Couldn’t load calendar options. Reload to try again.'); });
    const update = () => { setZone(effectiveTimezone()); setNow(pacificToday()); };
    window.addEventListener('cseed-timezone', update);
    window.addEventListener('focus', update);
    const timer = setInterval(update, 60_000);
    return () => { alive = false; clearInterval(timer); window.removeEventListener('cseed-timezone', update); window.removeEventListener('focus', update); };
  }, []);
  const compact = size === '2x1';
  // Small tiles always show next-up; larger tiles honor an explicit view.
  const view = compact ? 'agenda' : options.view === 'auto' ? size === '4x2' ? 'month' : 'week' : options.view;
  const month = monthOf(anchor);
  const grid = monthGrid(month);
  const start = view === 'month' ? grid[0]!.date : view === 'week' ? mondayOf(anchor) : anchor;
  const end = view === 'month' ? addDays(grid[grid.length - 1]!.date, 1) : addDays(start, view === 'week' ? 7 : compact ? 1 : 7);
  const sources = options.sources.join(',');
  useEffect(() => {
    let alive = true;
    setFeed(null); setError('');
    loadFeed(start, end, sources, zone, tick > 0).then(d => { if (alive) setFeed(d); }).catch(() => { if (alive) setError('Couldn’t load the calendar.'); });
    return () => { alive = false; };
  }, [start, end, sources, zone, tick]);

  async function save() {
    setSaving(true); setSettingsError('');
    try {
      const p = await api('calendarWidgetPut', { body: { view: draft.view, colourBy: draft.colourBy, showWeekends: draft.showWeekends, events: draft.sources.includes('events'), design: draft.sources.includes('design') } });
      setOptions(p); popover.current?.removeAttribute('open');
    } catch { setSettingsError('Couldn’t save options. Please try again.'); }
    finally { setSaving(false); }
  }
  const visibleDay = (date: string) => options.showWeekends || weekdayIndex(date) < 5;
  const items = (feed?.items ?? []).filter(item => item.type !== 'cancelled');
  const onDate = (date: string) => items.filter(item => {
    if (!item.startsAt) return item.date === date;
    // Show an event on every local date it overlaps; exclusive midnight ends stay on the previous day.
    const last = item.endsAt ? pacificParts(new Date(new Date(item.endsAt).getTime() - 1)).date : item.date;
    return item.date <= date && last >= date;
  });
  const agendaDates: string[] = [];
  for (let d = start; d < end; d = addDays(d, 1)) if (visibleDay(d)) agendaDates.push(d);
  const dates = view === 'month' ? grid.map(d => d.date).filter(visibleDay) : weekDays(start).filter(visibleDay);
  const label = view === 'month' ? monthLabel(month) : compact ? shortDay(start) : weekLabel(start);
  const navigate = (n: number) => {
    setSelected(null);
    setAnchor(view === 'month' ? addMonths(month, n) + '-01' : addDays(start, n * (compact ? 1 : 7)));
  };
  const renderItem = (item: CalendarFeedItem, mini = false) => <div class={`cw-item${mini ? ' cw-item--mini' : ''}`} style={{ '--cw-colour': `var(--c-${itemHue(item, options.colourBy)})` }} key={item.source + item.id}>
    <a href={itemHref(item)} class="cw-item-title">{item.title}</a>
    {!mini && <span class="cw-item-meta">{item.startsAt ? timeRange(item.startsAt, item.endsAt) : 'All day'} · {sourceLabel(item)}{options.colourBy === 'type' ? ` · ${item.type}` : ''}{item.type === 'draft' ? ' · Draft' : ''}</span>}
    {!mini && item.lumaUrl && <a class="cw-luma" href={item.lumaUrl} target="_blank" rel="noopener noreferrer">Open in Luma ↗</a>}
  </div>;
  const shownAgenda = selected ? [selected] : agendaDates;
  return <section class={`tile cw cw--${view}${compact ? ' cw--compact' : ''}`} aria-labelledby="calendar-widget-title" data-block="calendar">
    <header class="tile-head">
      <h2 class="tile-title" id="calendar-widget-title">calendar</h2>
      <a class="cw-add" href={href.eventNew()} aria-label="Add event">+ event</a>
      <details class="cw-settings" ref={popover} onKeyDown={e => { if (e.key === 'Escape') { popover.current?.removeAttribute('open'); popover.current?.querySelector('summary')?.focus(); } }}>
        <summary aria-label="Calendar options" onClick={() => setDraft(options)}>Options</summary>
        <div class="cw-popover">
          <label>View<select value={draft.view} disabled={saving} onChange={e => setDraft({ ...draft, view: e.currentTarget.value as CalendarWidgetOptions['view'] })}>
            <option value="auto">Fit widget size</option><option value="agenda">Agenda</option><option value="week">Week</option><option value="month">Month</option>
          </select></label>
          {compact && <p class="small muted">Small widgets show a one-day agenda.</p>}
          <fieldset disabled={saving}><legend>Sources</legend>{(['events', 'design'] as const).map(source => <label key={source} class="cw-check"><input type="checkbox" checked={draft.sources.includes(source)} onChange={e => setDraft({ ...draft, sources: e.currentTarget.checked ? [...draft.sources, source] : draft.sources.filter(s => s !== source) })} />{source === 'events' ? 'Club events · includes Luma' : 'Design deadlines'}</label>)}</fieldset>
          <label>Colour by<select value={draft.colourBy} disabled={saving} onChange={e => setDraft({ ...draft, colourBy: e.currentTarget.value as CalendarWidgetOptions['colourBy'] })}><option value="source">Source</option><option value="type">Type / event status</option></select></label>
          <label class="cw-check"><input type="checkbox" disabled={saving} checked={draft.showWeekends} onChange={e => setDraft({ ...draft, showWeekends: e.currentTarget.checked })} />Show weekends</label>
          <p class="small muted">Time zone: {zone}. Change it in <a href={href.settings}>Settings</a>.</p>
          <button class="btn btn--small btn--primary" disabled={saving || !settingsReady} onClick={save}>{saving ? 'Saving…' : 'Save options'}</button>
        </div>
      </details>
    </header>
    {settingsError && <p class="small" role="alert">{settingsError}</p>}
    <div class="cw-toolbar">
      <strong aria-live="polite">{label}</strong>
      <div class="cw-nav" role="group" aria-label="Calendar dates"><button class="btn btn--small" aria-label="Previous calendar period" onClick={() => navigate(-1)}>‹</button><button class="btn btn--small" onClick={() => { setAnchor(pacificToday()); setSelected(null); }}>Today</button><button class="btn btn--small" aria-label="Next calendar period" onClick={() => navigate(1)}>›</button></div>
    </div>
    {!compact && <p class="cw-zone">{zone} · <span class="cw-key cw-key--events">Club events</span> · <span class="cw-key cw-key--design">Design deadlines</span></p>}
    {error ? <p role="alert">{error} <button class="btn btn--small" onClick={() => setTick(t => t + 1)}>Retry</button></p> : !feed ? <p class="small muted" role="status">Loading calendar…</p> : <>
      {view !== 'agenda' && <div class={`cw-grid${options.showWeekends ? '' : ' cw-grid--weekdays'}`} role="group" aria-label={label}>
        {(options.showWeekends ? ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'] : ['Mon', 'Tue', 'Wed', 'Thu', 'Fri']).map(day => <span class="cw-dow" key={day}>{day}</span>)}
        {dates.map(date => {
          const list = onDate(date);
          const outside = view === 'month' && monthOf(date) !== month;
          return <div key={date} class={`cw-day${date === now ? ' is-today' : ''}${outside ? ' is-outside' : ''}${selected === date ? ' is-selected' : ''}`}>
            <button class="cw-date" aria-label={`${weekdayShort(date)}, ${shortDay(date)}, ${list.length} items`} aria-pressed={selected === date} onClick={() => setSelected(selected === date ? null : date)}>{dayNumber(date)}</button>
            {list.slice(0, 2).map(item => renderItem(item, true))}
            {list.length > 2 && <button class="cw-more" onClick={() => setSelected(date)}>+{list.length - 2} more</button>}
          </div>;
        })}
      </div>}
      {(view !== 'month' || selected) && <div class="cw-agenda" aria-label="Calendar agenda">
        {shownAgenda.filter(date => onDate(date).length).map(date => <div key={date} class="cw-agenda-day">
          {!compact && <h3>{date === now ? 'Today' : weekdayShort(date)} <span>{shortDay(date)}</span></h3>}
          {onDate(date).map(item => renderItem(item))}
        </div>)}
        {shownAgenda.every(date => !onDate(date).length) && <div class="cw-empty"><strong>{sources ? 'A little breathing room' : 'No sources selected'}</strong><span>{sources ? 'Nothing scheduled for these dates.' : 'Choose sources in Options.'}</span></div>}
      </div>}
      {view === 'month' && !selected && <p class="cw-footnote">Select a date for times and Luma links.</p>}
    </>}
  </section>;
}
