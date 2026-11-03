import { useEffect, useRef, useState } from 'preact/hooks';
import { api, endpointUrl } from '../../lib/api/client';
import type { CalendarFeedItem, CalendarFeedSource, CalendarWidgetOptions } from '../../lib/contracts';
import { addDays, addMonths, dayNumber, monthGrid, monthLabel, monthOf, mondayOf, pacificParts, pacificToday, shortDay, timeRange, weekdayIndex, weekdayShort, weekDays, weekLabel } from '../../lib/calendar';
import { effectiveTimezone } from '../../lib/timezone';
import { href } from '../../lib/routes';
import type { BlockSize } from './registry';
import { Icon } from '../components/Icon';
import { Async, Loading } from '../components/states';
import { useApi } from '../../lib/api/useApi';
import { createPortal } from 'preact/compat';
import { useId } from 'preact/hooks';
import '../../styles/calendar-widget.css';

type Feed = Awaited<ReturnType<typeof loadFeed>>;
const cache = new Map<string, { at: number; data: { items: CalendarFeedItem[]; timezone: string; sources: CalendarFeedSource[] } }>();
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
// Mirrors store.CalendarSources so the legend paints at its final size before the feed's
// registry metadata arrives (which then replaces it). Unknown future sources still appear
// once the feed loads.
const fallbackSources: CalendarFeedSource[] = [
  { id: 'events', label: 'Club events', icon: 'calendar', hue: 'blue' },
  { id: 'design', label: 'Design deadlines', icon: 'pen', hue: 'amber' },
  { id: 'purchase-deadlines', label: 'Purchase deadlines', icon: 'wallet', hue: 'green' },
  { id: 'buildspace-absences', label: 'Buildspace absences', icon: 'user', hue: 'coral' },
  { id: 'buildspace-weekly', label: 'Buildspace weekly check-ins', icon: 'check', hue: 'teal' },
  { id: 'buildspace-mentor', label: 'Buildspace mentor check-ins', icon: 'user', hue: 'violet' },
];
const safeHue = (hue?: string) => ['blue', 'amber', 'teal', 'coral', 'violet', 'green'].includes(hue ?? '') ? hue! : 'teal';
const SOURCE_ICONS = ['pen', 'user', 'check', 'wallet'] as const;
const sourceIcon = (source?: CalendarFeedSource) => SOURCE_ICONS.find(icon => icon === source?.icon) ?? 'calendar';
function itemHue(item: CalendarFeedItem, by: CalendarWidgetOptions['colourBy'], source?: CalendarFeedSource) {
  if (by === 'source') return safeHue(source?.hue);
  if (item.source === 'events') return item.type === 'cancelled' ? 'coral' : item.type === 'draft' ? 'amber' : 'teal';
  return ({ poster: 'violet', social: 'blue', slides: 'teal', merch: 'amber', video: 'coral' } as Record<string, string>)[item.type] ?? safeHue(source?.hue);
}
const shortTime = (iso: string) => {
  const minutes = pacificParts(iso).minutes;
  const hour = Math.floor(minutes / 60);
  return `${hour % 12 || 12}${minutes % 60 ? ':' + String(minutes % 60).padStart(2, '0') : ''}${hour < 12 ? 'a' : 'p'}`;
};

function CalendarChip({ item, source, colourBy }: { item: CalendarFeedItem; source?: CalendarFeedSource; colourBy: CalendarWidgetOptions['colourBy'] }) {
  const [position, setPosition] = useState<{ top: number; left: number } | null>(null);
  const wrapper = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLAnchorElement>(null);
  const card = useRef<HTMLDivElement>(null);
  const id = useId();
  const closeTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const cancelClose = () => { if (closeTimer.current) clearTimeout(closeTimer.current); };
  useEffect(() => () => cancelClose(), []);
  const contains = (node: Node | null) => !!node && (!!wrapper.current?.contains(node) || !!card.current?.contains(node));
  const leave = () => { cancelClose(); closeTimer.current = setTimeout(() => { if (!contains(document.activeElement)) setPosition(null); }, 180); };
  const blur = (event: FocusEvent) => { if (!contains(event.relatedTarget as Node | null)) setPosition(null); };
  const escape = (event: KeyboardEvent) => { if (event.key === 'Escape') { event.preventDefault(); cancelClose(); trigger.current?.focus(); setPosition(null); } };
  const open = () => {
    cancelClose();
    const rect = trigger.current?.getBoundingClientRect();
    if (!rect) return;
    const width = Math.min(280, window.innerWidth - 24);
    const top = rect.bottom + 200 > window.innerHeight ? Math.max(12, rect.top - 190) : rect.bottom + 5;
    setPosition({ top, left: Math.max(12, Math.min(rect.left, window.innerWidth - width - 12)) });
  };
  useEffect(() => {
    if (!position) return;
    const close = () => setPosition(null);
    window.addEventListener('scroll', close, true);
    window.addEventListener('resize', close);
    return () => { window.removeEventListener('scroll', close, true); window.removeEventListener('resize', close); };
  }, [!!position]);
  return <div ref={wrapper} class="cw-item cw-item--mini" style={{ '--cw-colour': `var(--c-${itemHue(item, colourBy, source)})` }}
    onMouseEnter={open} onMouseLeave={leave}
    onFocusIn={open} onFocusOut={blur} onKeyDown={e => {
      if (e.key === 'Tab' && !e.shiftKey && position && e.target === trigger.current) { e.preventDefault(); card.current?.querySelector<HTMLAnchorElement>('a')?.focus(); }
      else escape(e);
    }}>
    <a ref={trigger} href={item.href} class="cw-item-title" aria-describedby={position ? id : undefined}>
      <Icon name={sourceIcon(source)} size={11} />
      {item.startsAt && <time dateTime={item.startsAt} class="cw-chip-time">{shortTime(item.startsAt)}</time>}
      <span class="cw-chip-name">{item.title}</span>
    </a>
    {position && createPortal(<div ref={card} id={id} class="cw-item-popover" onMouseEnter={cancelClose} onMouseLeave={leave} onFocusIn={cancelClose} onFocusOut={blur} onKeyDown={e => {
        const links = card.current?.querySelectorAll('a');
        if (e.key === 'Tab' && e.shiftKey && e.target === links?.[0]) { e.preventDefault(); trigger.current?.focus(); }
        else if (e.key === 'Tab' && !e.shiftKey && e.target === links?.[links.length - 1]) {
          const stops = [...document.querySelectorAll<HTMLElement>('a[href],button:not(:disabled),summary,select,input,[tabindex="0"]')].filter(el => el.getClientRects().length && !card.current?.contains(el));
          const next = stops[stops.indexOf(trigger.current!) + 1];
          if (next) { e.preventDefault(); next.focus(); setPosition(null); }
        } else escape(e);
      }}
      style={{ top: `${position.top}px`, left: `${position.left}px`, '--cw-colour': `var(--c-${itemHue(item, colourBy, source)})` }} role="group" aria-label={`${item.title} details`}>
      <strong>{item.title}</strong>
      <span>{item.startsAt ? timeRange(item.startsAt, item.endsAt) : 'All day'} · {shortDay(item.date)}</span>
      <span>{item.location || (item.startsAt ? 'Location not set' : 'Deadline')}</span>
      {item.detail && <span class="cw-popover-detail">{item.detail}</span>}
      <span class="cw-popover-source"><Icon name={sourceIcon(source)} size={13} />{source?.label ?? item.source}</span>
      <a href={item.href}>Open details →</a>
      {item.lumaUrl && <a href={item.lumaUrl} target="_blank" rel="noopener noreferrer">Open in Luma ↗</a>}
    </div>, document.body)}
  </div>;
}

function SubscribeLinks() {
  const state = useApi('calendarGet');
  if (state.status === 'loading') return <p class="small" role="status">Loading delivery options…</p>;
  if (state.status === 'error') return <p class="small">Delivery options couldn’t load. <a href="/settings#calendar">Open calendar settings →</a></p>;
  const settings = state.data;
  return <div class="cw-subscribe">
    <p>Club events can arrive as ICS email invitations or sync to Google Calendar.</p>
    <a href="/settings#calendar">Manage ICS invitations &amp; calendar delivery →</a>
    {settings.google.configured && <a href={settings.google.connected ? '/settings#calendar' : endpointUrl('googleConnect') + '?next=%2F'}>
      {settings.google.connected ? 'Manage Google Calendar sync →' : 'Connect Google Calendar →'}
    </a>}
    {!settings.ics.configured && !settings.google.configured && <span class="muted">Calendar delivery isn’t configured yet.</span>}
  </div>;
}

export default function CalendarWidget({ size }: { size: BlockSize }) {
  const [options, setOptions] = useState<CalendarWidgetOptions>(defaults);
  const [draft, setDraft] = useState(defaults);
  const [settingsReady, setSettingsReady] = useState(false);
  const [settingsError, setSettingsError] = useState('');
  const [subscribe, setSubscribe] = useState(false);
  const [saving, setSaving] = useState(false);
  const [anchor, setAnchor] = useState(pacificToday);
  const [zone, setZone] = useState(effectiveTimezone);
  const [feed, setFeed] = useState<Feed | null>(null);
  const [sourceDefs, setSourceDefs] = useState(fallbackSources);
  const [error, setError] = useState('');
  const [tick, setTick] = useState(0);
  const [selected, setSelected] = useState<string | null>(null);
  const popover = useRef<HTMLDetailsElement>(null);
  const sourcesMenu = useRef<HTMLDetailsElement>(null);
  useEffect(() => {
    const outside = (e: PointerEvent) => { const menu = sourcesMenu.current; if (menu?.open && !menu.contains(e.target as Node)) menu.open = false; };
    document.addEventListener('pointerdown', outside);
    return () => document.removeEventListener('pointerdown', outside);
  }, []);
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
    loadFeed(start, end, sources, zone, tick > 0).then(d => { if (alive) { setFeed(d); setSourceDefs(d.sources); } }).catch(() => { if (alive) setError('Couldn’t load the calendar.'); });
    return () => { alive = false; };
  }, [start, end, sources, zone, tick]);

  async function save(next = draft, close = true) {
    setSaving(true); setSettingsError('');
    try {
      const p = await api('calendarWidgetPut', { body: next });
      setOptions(p); setDraft(p); if (close) popover.current?.removeAttribute('open');
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
  const renderItem = (item: CalendarFeedItem, mini = false) => {
    const source = sourceDefs.find(s => s.id === item.source);
    if (mini) return <CalendarChip key={item.source + item.id} item={item} source={source} colourBy={options.colourBy} />;
    return <div class="cw-item" style={{ '--cw-colour': `var(--c-${itemHue(item, options.colourBy, source)})` }} key={item.source + item.id}>
      <a href={item.href} class="cw-item-title"><Icon name={sourceIcon(source)} size={13} /> {item.title}</a>
      <span class="cw-item-meta">{item.startsAt ? timeRange(item.startsAt, item.endsAt) : 'All day'} · {source?.label ?? item.source}{options.colourBy === 'type' ? ` · ${item.type}` : ''}{item.type === 'draft' ? ' · Draft' : ''}</span>
      {item.location && <span class="cw-item-meta">{item.location}</span>}
      {item.detail && <span class="cw-item-meta">{item.detail}</span>}
      {item.lumaUrl && <a class="cw-luma" href={item.lumaUrl} target="_blank" rel="noopener noreferrer">Open in Luma ↗</a>}
    </div>;
  };
  const shownAgenda = selected ? [selected] : agendaDates;
  // Six source chips need a full-width (4×2) tile; smaller tiles use a compact menu whose
  // panel floats over the calendar, so opening it never moves the layout.
  const menuLegend = size !== '4x2';
  const shownCount = sourceDefs.filter(source => options.sources.includes(source.id)).length;
  const toggles = <div class="cw-sources" role="group" aria-label="Calendar sources">
    {sourceDefs.map(source => <button type="button" key={source.id} class="cw-source-toggle" style={{ '--cw-colour': `var(--c-${safeHue(source.hue)})` }}
      // aria-disabled while saving keeps keyboard focus on the toggle (disabling would drop it).
      aria-pressed={options.sources.includes(source.id)} disabled={!settingsReady} aria-disabled={saving || undefined}
      onClick={() => !saving && save({ ...options, sources: options.sources.includes(source.id) ? options.sources.filter(id => id !== source.id) : [...options.sources, source.id] }, false)}>
      <Icon name={sourceIcon(source)} size={13} />{source.label}
    </button>)}
  </div>;
  return <section class={`tile cw cw--${view}${compact ? ' cw--compact' : ''}`} aria-labelledby="calendar-widget-title" data-block="calendar">
    <header class="tile-head">
      <h2 class="tile-title" id="calendar-widget-title">calendar</h2>
      <a class="cw-add" href={href.eventNew()} aria-label="Add event">+ event</a>
      <details class="cw-settings" ref={popover} onKeyDown={e => { if (e.key === 'Escape') { popover.current?.removeAttribute('open'); popover.current?.querySelector('summary')?.focus(); } }}>
        <summary aria-label="Calendar options" onClick={() => setDraft(options)}>Options</summary>
        <div class="cw-popover">
          <details class="cw-subscribe-menu" onToggle={e => setSubscribe(e.currentTarget.open)}>
            <summary>Subscribe</summary>{subscribe && <SubscribeLinks />}
          </details>
          <label>View<select value={draft.view} disabled={saving} onChange={e => setDraft({ ...draft, view: e.currentTarget.value as CalendarWidgetOptions['view'] })}>
            <option value="auto">Fit widget size</option><option value="agenda">Agenda</option><option value="week">Week</option><option value="month">Month</option>
          </select></label>
          {compact && <p class="small muted">Small widgets show a one-day agenda.</p>}
          <fieldset disabled={saving}><legend>Sources</legend>{sourceDefs.map(source => <label key={source.id} class="cw-check"><input type="checkbox" checked={draft.sources.includes(source.id)} onChange={e => setDraft({ ...draft, sources: e.currentTarget.checked ? [...draft.sources, source.id] : draft.sources.filter(s => s !== source.id) })} />{source.label}</label>)}</fieldset>
          <label>Colour by<select value={draft.colourBy} disabled={saving} onChange={e => setDraft({ ...draft, colourBy: e.currentTarget.value as CalendarWidgetOptions['colourBy'] })}><option value="source">Source</option><option value="type">Type / event status</option></select></label>
          <label class="cw-check"><input type="checkbox" disabled={saving} checked={draft.showWeekends} onChange={e => setDraft({ ...draft, showWeekends: e.currentTarget.checked })} />Show weekends</label>
          <p class="small muted">Time zone: {zone}. Change it in <a href={href.settings}>Settings</a>.</p>
          <button class="btn btn--small btn--primary" disabled={saving || !settingsReady} onClick={() => save()}>{saving ? 'Saving…' : 'Save options'}</button>
        </div>
      </details>
    </header>
    {settingsError && <p class="small" role="alert">{settingsError}</p>}
    <div class="cw-toolbar">
      <strong aria-live="polite">{label}</strong>
      <div class="cw-nav" role="group" aria-label="Calendar dates"><button class="btn btn--small" aria-label="Previous calendar period" onClick={() => navigate(-1)}>‹</button><button class="btn btn--small" onClick={() => { setAnchor(pacificToday()); setSelected(null); }}>Today</button><button class="btn btn--small" aria-label="Next calendar period" onClick={() => navigate(1)}>›</button></div>
    </div>
    <div class={`cw-legend${menuLegend ? ' cw-legend--menu' : ''}`}>
      {menuLegend ? <details class="cw-sources-menu" ref={sourcesMenu}
        onKeyDown={e => { if (e.key === 'Escape' && sourcesMenu.current?.open) { e.preventDefault(); sourcesMenu.current.open = false; sourcesMenu.current.querySelector('summary')?.focus(); } }}
        onFocusOut={e => { const next = e.relatedTarget as Node | null; if (next && !sourcesMenu.current?.contains(next)) sourcesMenu.current?.removeAttribute('open'); }}>
        <summary aria-label={`Calendar sources, ${shownCount} of ${sourceDefs.length} shown`}>
          <span class="cw-sources-dots" aria-hidden="true">{sourceDefs.filter(source => options.sources.includes(source.id)).slice(0, 4).map(source => <span key={source.id} style={{ '--cw-colour': `var(--c-${safeHue(source.hue)})` }} />)}</span>
          Sources ({shownCount}/{sourceDefs.length})
        </summary>
        <div class="cw-sources-panel">{toggles}</div>
      </details> : toggles}
      {!compact && <span class="cw-zone">{zone}</span>}
    </div>
    {error ? <p role="alert">{error} <button class="btn btn--small" onClick={() => setTick(t => t + 1)}>Retry</button></p> : <Async state={feed ? { status: 'ready', data: feed } : { status: 'loading' }} loading={<Loading rows={compact ? 1 : 3} label="Loading calendar" />}>{() => <>
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
        {shownAgenda.filter(date => onDate(date).length).map(date => <div key={date} class="cw-agenda-day motion-list">
          {!compact && <h3>{date === now ? 'Today' : weekdayShort(date)} <span>{shortDay(date)}</span></h3>}
          {onDate(date).map(item => renderItem(item))}
        </div>)}
        {shownAgenda.every(date => !onDate(date).length) && <div class="cw-empty"><strong>{sources ? 'A little breathing room' : 'No sources selected'}</strong><span>{sources ? 'Nothing scheduled for these dates.' : 'Choose sources in Options.'}</span></div>}
      </div>}
      {view === 'month' && !selected && <p class="cw-footnote">Hover or focus an item for details. Select a date for its agenda.</p>}
    </>}</Async>}
  </section>;
}
