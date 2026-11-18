import { useCallback, useEffect, useMemo, useState } from 'preact/hooks';
import { api } from '../../../lib/api/client';
import type { CalendarFeedItem, CalendarFeedSource } from '../../../lib/contracts';
import { addDays, dayNumber, monthGrid, monthLabel, monthOf, mondayOf, pacificParts, pacificToday, shortDay, weekdayShort, weekLabel } from '../../../lib/calendar';
import { effectiveTimezone } from '../../../lib/timezone';
import WidgetCard from '../shell/WidgetCard';
import type { WidgetProps } from '../registry';
import { shortTime } from './CalendarChip';
import { MonthView } from './MonthView';
import { WeekView } from './WeekView';

type Feed = { items: CalendarFeedItem[]; timezone: string; sources: CalendarFeedSource[] };
const cache = new Map<string, { at: number; data: Feed }>();
/** One read per month range and time zone, shared by remounts for a minute. Sources: the server's role defaults. */
async function loadFeed(start: string, end: string, zone: string, role: string, fresh = false): Promise<Feed> {
  const key = JSON.stringify([start, end, zone, role]);
  const hit = cache.get(key);
  if (!fresh && hit && Date.now() - hit.at < 60_000) return hit.data;
  const data = await api('calendarFeed', { query: { start, end } });
  if (cache.size > 24) cache.clear();
  cache.set(key, { at: Date.now(), data });
  return data;
}

// Mirrors store.CalendarSources so chip hues are right before the feed's registry arrives.
const fallbackSources: CalendarFeedSource[] = [
  { id: 'events', label: 'Club events', icon: 'calendar', hue: 'blue' },
  { id: 'design', label: 'Design deadlines', icon: 'pen', hue: 'amber' },
  { id: 'purchase-deadlines', label: 'Purchase deadlines', icon: 'wallet', hue: 'green' },
  { id: 'buildspace-absences', label: 'Buildspace + Launchpad absences', icon: 'user', hue: 'coral' },
  { id: 'buildspace-weekly', label: 'Buildspace + Launchpad weekly check-ins', icon: 'check', hue: 'teal' },
  { id: 'buildspace-mentor', label: 'Buildspace + Launchpad mentor check-ins', icon: 'user', hue: 'violet' },
];

/** "Nov 9 – 15", "Nov 30 – Dec 6". */
function weekRange(monday: string) {
  const sunday = addDays(monday, 6);
  return monthOf(monday) === monthOf(sunday) ? `${shortDay(monday)} – ${dayNumber(sunday)}` : `${shortDay(monday)} – ${shortDay(sunday)}`;
}
/** Six weeks starting on the Monday on or before the 1st. */
function sixWeeks(month: string) {
  const first = monthGrid(month)[0]!.date;
  return Array.from({ length: 42 }, (_, i) => addDays(first, i));
}

/** Bucket a feed once for the visible [start, end) dates instead of rescanning it per day cell. */
export function bucketCalendarItems(items: CalendarFeedItem[], start: string, end: string): Map<string, CalendarFeedItem[]> {
  const buckets = new Map<string, CalendarFeedItem[]>();
  const rangeEnd = addDays(end, -1);
  for (const item of items) {
    // Every selected-zone date it overlaps; an exclusive midnight end stays on the previous day.
    const last = item.startsAt && item.endsAt ? pacificParts(new Date(new Date(item.endsAt).getTime() - 1)).date : item.date;
    for (let date = item.date < start ? start : item.date; date <= last && date <= rangeEnd; date = addDays(date, 1)) {
      const bucket = buckets.get(date);
      if (bucket) bucket.push(item);
      else buckets.set(date, [item]);
    }
  }
  for (const list of buckets.values()) list.sort((a, b) => (a.startsAt ?? '').localeCompare(b.startsAt ?? ''));
  return buckets;
}

/**
 * Home calendar (notes § Calendar): no controls. 2×1 and 2×2 show this week, 4×2 this month. The
 * month range is fetched once; the week view filters it, so a resize never refetches or blanks it.
 */
export default function CalendarWidget({ me, size }: WidgetProps) {
  const [zone, setZone] = useState(effectiveTimezone);
  const [today, setToday] = useState(pacificToday);
  const [feed, setFeed] = useState<Feed | null>(null);
  const [error, setError] = useState(false);
  const [tick, setTick] = useState(0);
  useEffect(() => {
    const update = () => { if (document.visibilityState === 'hidden') return; setZone(effectiveTimezone()); setToday(pacificToday()); setTick((t) => t + 1); };
    window.addEventListener('cseed-timezone', update);
    window.addEventListener('focus', update);
    document.addEventListener('visibilitychange', update);
    const timer = setInterval(update, 60_000);
    return () => { clearInterval(timer); window.removeEventListener('cseed-timezone', update); window.removeEventListener('focus', update); document.removeEventListener('visibilitychange', update); };
  }, []);
  const month = monthOf(today);
  const dates = sixWeeks(month);
  const start = dates[0]!;
  const end = addDays(dates[dates.length - 1]!, 1);
  useEffect(() => {
    let alive = true;
    setError(false);
    // Keep showing the current feed while a new range or zone loads.
    loadFeed(start, end, zone, me.role).then((d) => alive && setFeed(d), () => alive && setError(true));
    return () => { alive = false; };
  }, [start, end, zone, me.role, tick]);

  const sources = useMemo(() => new Map((feed?.sources ?? fallbackSources).map((source) => [source.id, source])), [feed?.sources]);
  const items = useMemo(() => (feed?.items ?? []).filter((item) => item.type !== 'cancelled'), [feed?.items]);
  const buckets = useMemo(() => bucketCalendarItems(items, start, end), [items, start, end, zone]);
  const itemsOn = useCallback((date: string) => buckets.get(date) ?? [], [buckets]);

  const monthView = size === '4x2';
  const monday = mondayOf(today);
  const range = monthView ? dates.filter((d) => monthOf(d) === month) : Array.from({ length: 7 }, (_, i) => addDays(monday, i));
  const inRange = items.filter((item) => item.date >= range[0]! && item.date <= range[range.length - 1]!);
  const nowIso = new Date().toISOString();
  const next = inRange.filter((item) => (item.startsAt ? item.startsAt > nowIso : item.date >= today)).sort((a, b) => (a.startsAt ?? a.date).localeCompare(b.startsAt ?? b.date))[0];
  const period = monthView ? 'this month' : 'this week';
  const noun = inRange.every((item) => item.source === 'events') ? (inRange.length === 1 ? 'event' : 'events') : inRange.length === 1 ? 'item' : 'items';
  // A one-line summary when the calendar is sparse (a few items) or empty.
  const summary = error
    ? <>Couldn’t load the calendar. <button type="button" class="btn btn--small" onClick={() => { cache.delete(JSON.stringify([start, end, zone, me.role])); setTick((t) => t + 1); }}>Retry</button></>
    : !feed ? null
      : !inRange.length ? `Nothing scheduled ${period}`
        : inRange.length <= (monthView ? 6 : 4) ? `${inRange.length} ${noun} ${period}${next ? ` · next: ${next.title}, ${weekdayShort(next.date)} ${shortTime(next.startsAt)}` : ''}` : null;
  const label = monthView ? monthLabel(month) : weekLabel(monday);
  return (
    <WidgetCard id="calendar" me={me} class={`cw cw--${monthView ? 'month' : 'week'}`} busy={!feed && !error}
      aside={monthView ? `${monthLabel(month)}${feed && inRange.length <= 6 ? ` · ${inRange.length ? `${inRange.length} ${noun}` : 'nothing scheduled'}` : ''}` : weekRange(monday)}
      foot={(error || !monthView) && summary && <span class="cal-summary" role={error ? 'alert' : undefined}>{summary}</span>}>
      {monthView
        ? <MonthView dates={dates} month={month} today={today} itemsOn={itemsOn} sources={sources} label={label} />
        : <WeekView monday={monday} today={today} tall={size !== '2x1'} itemsOn={itemsOn} sources={sources} label={label} />}
    </WidgetCard>
  );
}
