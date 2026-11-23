/**
 * Organization calendar (V0.15): the pure parts of the /calendar page. API items become the
 * shared calendar's entries, the create/edit drawer's recurrence round-trips through RRULE
 * text, and the left rail's toggles persist in localStorage. Unit-tested in tests/unit.spec.ts.
 *
 * Times: timed meetings are ISO instants shown on the selected wall clock (lib/calendar.ts).
 * Fixed all-day meetings carry YYYY-MM-DD dates with an exclusive end; tentative items carry an
 * inclusive tentativeFrom/tentativeTo range and no start.
 */
import { addDays, addMonths, monthGrid, monthOf, mondayOf, pacificParts, weekdayIndex, whenShort } from './calendar';
import type { CalendarFeedItem, CalendarFeedSource, OrgCalendar, OrgCalendarEvent, OrgCalendarKey } from './contracts';
import { pacificIso } from './format';
import type { CalEntry } from '../ui/components/calendar/layout';

export const CAL_KEYS: OrgCalendarKey[] = ['internal', 'external', 'programs', 'events'];
/** Calendars people write to; the other two are projections of Events and Weekly Programs. */
export const MANUAL_KEYS: OrgCalendarKey[] = ['internal', 'external'];

/** Mirrors migration 0031, so the rail has names and hues before /api/orgcal/calendars answers. */
export const FALLBACK_CALENDARS: OrgCalendar[] = [
  { key: 'internal', name: 'Internal Meetings', googleCalendarId: null, hue: 'blue', locked: false },
  { key: 'external', name: 'External Meetings', googleCalendarId: null, hue: 'teal', locked: false },
  { key: 'programs', name: 'Weekly Programs', googleCalendarId: null, hue: 'violet', locked: true },
  { key: 'events', name: 'Events', googleCalendarId: null, hue: 'coral', locked: true },
];

/**
 * Dashboard overlays from /api/calendar/feed: one toggle each, several feed sources per toggle.
 * The four calendars use blue, teal, violet and coral, so overlays take the two hues left and
 * neutral ink (`ink`), and draw as outlined chips so they never read as a calendar.
 */
export const OVERLAYS = [
  { key: 'design', label: 'Design deadlines', sources: ['design'], hue: 'amber' },
  { key: 'purchase', label: 'Purchase deadlines', sources: ['purchase-deadlines'], hue: 'green' },
  { key: 'buildspace', label: 'Buildspace check-ins', sources: ['buildspace-weekly', 'buildspace-mentor'], hue: 'ink' },
] as const;

/** A hue name → its CSS colour: a tokens.css `--c-<hue>`, or the muted text colour for `ink`. */
export const hueColor = (hue: string) => (hue === 'ink' ? 'var(--muted)' : `var(--c-${hue})`);
export type OverlayKey = (typeof OVERLAYS)[number]['key'];
export const OVERLAY_SOURCES = OVERLAYS.flatMap((o) => o.sources as readonly string[]);

// ───────────────────────────── Toggles ─────────────────────────────

export type Toggles = Record<OrgCalendarKey | OverlayKey, boolean>;
export const DEFAULT_TOGGLES: Toggles = { internal: true, external: true, programs: true, events: true, design: true, purchase: true, buildspace: true };
const TOGGLES_KEY = 'cseed-orgcal-toggles';

/** Saved toggles merged over the defaults (everything on, overlays included). */
export function readToggles(raw: string | null | undefined): Toggles {
  if (!raw) return { ...DEFAULT_TOGGLES };
  try {
    const saved = JSON.parse(raw) as Record<string, unknown>;
    const out = { ...DEFAULT_TOGGLES };
    for (const k of Object.keys(out) as (keyof Toggles)[]) if (typeof saved?.[k] === 'boolean') out[k] = saved[k] as boolean;
    return out;
  } catch {
    return { ...DEFAULT_TOGGLES };
  }
}
export function loadToggles(): Toggles {
  try {
    return readToggles(localStorage.getItem(TOGGLES_KEY));
  } catch {
    return { ...DEFAULT_TOGGLES };
  }
}
export function saveToggles(t: Toggles) {
  try {
    localStorage.setItem(TOGGLES_KEY, JSON.stringify(t));
  } catch {
    /* private mode: toggles last for this page only */
  }
}

// ───────────────────────────── Ranges ─────────────────────────────

/**
 * The [from, to) dates a view reads: the month's whole Monday-first weeks, or one week, plus a day
 * on each side. The server reads these dates as Los Angeles midnights while the grid shows the
 * viewer's own zone, so a viewer east or west of Pacific time still gets items near the edges.
 * The grid only draws the days it shows.
 */
export function viewRange(view: 'month' | 'week', month: string, monday: string): { from: string; to: string } {
  if (view === 'week') return { from: addDays(monday, -1), to: addDays(monday, 8) };
  const grid = monthGrid(month);
  return { from: addDays(grid[0]!.date, -1), to: addDays(grid[grid.length - 1]!.date, 2) };
}

const isMonthParam = (s: string | null | undefined): s is string => !!s && /^\d{4}-\d{2}$/.test(s);
const isDateParam = (s: string | null | undefined): s is string => !!s && /^\d{4}-\d{2}-\d{2}$/.test(s);

/**
 * `?m=` and `?w=` → the month and the week (its Monday) the page shows. Navigation always writes
 * both (periodParams), so each view reads its own; a hand-written link with only one derives the
 * other: a month's week is its first (or today's, for this month), a week's month holds its Thursday.
 */
export function periodFromParams(m: string | null | undefined, w: string | null | undefined, today: string): { month: string; monday: string } {
  const month = isMonthParam(m) ? m : isDateParam(w) ? monthOf(addDays(mondayOf(w), 3)) : monthOf(today);
  const monday = isDateParam(w) ? mondayOf(w) : isMonthParam(m) && m !== monthOf(today) ? mondayOf(`${m}-01`) : mondayOf(today);
  return { month, monday };
}

/** The URL for a period: both parameters, or neither for today's (the page's default). */
export function periodParams(p: { m: string; w: string }, today: string): { m: string | null; w: string | null } {
  if (p.m === monthOf(today) && p.w === mondayOf(today)) return { m: null, w: null };
  return { m: p.m, w: p.w };
}

/** ‹ / › for the current view. A week's month is the one holding its Thursday. */
export function stepView(view: 'month' | 'week', month: string, monday: string, n: number): { m: string; w: string } {
  if (view === 'month') {
    const m = addMonths(month, n);
    return { m, w: mondayOf(`${m}-01`) };
  }
  const w = addDays(monday, 7 * n);
  return { m: monthOf(addDays(w, 3)), w };
}

// ───────────────────────────── Entries ─────────────────────────────

/** One displayed occurrence. Recurring meetings repeat their master id, so key by id + start. */
export const occurrenceKey = (e: Pick<OrgCalendarEvent, 'id' | 'startsAt' | 'tentativeFrom'>) => `${e.id}~${e.startsAt ?? e.tentativeFrom ?? ''}`;

export const isTentative = (e: Pick<OrgCalendarEvent, 'startsAt'>) => e.startsAt === null;
const isDateOnly = (s: string | null | undefined) => !!s && /^\d{4}-\d{2}-\d{2}$/.test(s);

/** The shared calendar's entry for an org calendar item. */
export function orgEntry(e: OrgCalendarEvent, hue: string, calendarName: string): CalEntry {
  const base = {
    id: occurrenceKey(e),
    title: e.title,
    hue,
    locked: e.sourceKind !== 'manual',
    draft: /^\[draft\]/i.test(e.title),
  };
  if (e.startsAt === null) {
    const from = e.tentativeFrom ?? '';
    const to = e.tentativeTo ?? from;
    return { ...base, from, to, tentative: true, note: ' · Tentative', agendaWhen: `Tentative · ${rangeText(from, to)}`, agendaSuffix: ` · ${calendarName}` };
  }
  if (e.allDay || isDateOnly(e.startsAt)) {
    const from = e.startsAt;
    const to = e.endsAt && isDateOnly(e.endsAt) ? addDays(e.endsAt, -1) : from;
    return { ...base, from, to: to < from ? from : to, agendaWhen: `All day · ${rangeText(from, to < from ? from : to)}`, agendaSuffix: ` · ${calendarName}` };
  }
  return { ...base, startsAt: e.startsAt, endsAt: e.endsAt, agendaWhen: whenShort(e.startsAt, e.endsAt), agendaSuffix: ` · ${calendarName}` };
}

/**
 * Dashboard overlays as entries. Date-only items from one source on one day become a single
 * entry ("Weekly check-in due (10)") so a busy Sunday doesn't flood the all-day band; timed items
 * stay one entry each. Each group keeps its items for the read-only card.
 */
export function overlayGroups(items: CalendarFeedItem[]): CalendarFeedItem[][] {
  const groups = new Map<string, CalendarFeedItem[]>();
  for (const it of items) {
    const key = it.startsAt ? `${it.source}~${it.id}` : `${it.source}~${it.date}`;
    const g = groups.get(key);
    if (g) g.push(it);
    else groups.set(key, [it]);
  }
  return [...groups.values()];
}

/** The text a group of overlay items shares before " · " (their kind), with the count. */
export function overlayTitle(group: CalendarFeedItem[]): string {
  if (group.length === 1) return group[0]!.title;
  const heads = new Set(group.map((i) => i.title.split(' · ')[0]));
  return `${heads.size === 1 ? [...heads][0] : 'Items'} (${group.length})`;
}

export function overlayEntry(group: CalendarFeedItem[], source: CalendarFeedSource | undefined): CalEntry {
  const item = group[0]!;
  const hue = OVERLAYS.find((o) => (o.sources as readonly string[]).includes(item.source))?.hue ?? 'amber';
  const id = group.length === 1 ? `feed~${item.source}~${item.id}` : `feed~${item.source}~${item.date}`;
  const base = { id, title: overlayTitle(group), hue, locked: true, overlay: true };
  const suffix = source ? ` · ${source.label}` : '';
  if (item.startsAt) return { ...base, startsAt: item.startsAt, endsAt: item.endsAt, agendaWhen: whenShort(item.startsAt, item.endsAt), agendaSuffix: suffix };
  return { ...base, from: item.date, to: item.date, agendaWhen: `All day · ${rangeText(item.date, item.date)}`, agendaSuffix: suffix };
}

const fmtShort = new Intl.DateTimeFormat('en-US', { month: 'short', day: 'numeric', timeZone: 'UTC' });
const shortOf = (d: string) => fmtShort.format(new Date(`${d}T12:00:00Z`));
/** "Nov 12" or "Nov 12 – Nov 20". */
export const rangeText = (from: string, to: string) => (from === to ? shortOf(from) : `${shortOf(from)} – ${shortOf(to)}`);

// ───────────────────────────── Recurrence ─────────────────────────────

export type Repeat = 'none' | 'daily' | 'weekly' | 'biweekly' | 'monthly' | 'custom';
export const REPEAT_LABEL: Record<Repeat, string> = {
  none: 'Does not repeat',
  daily: 'Daily',
  weekly: 'Weekly',
  biweekly: 'Every 2 weeks',
  monthly: 'Monthly',
  custom: 'Custom (kept as is)',
};

/** "RRULE:FREQ=WEEKLY;INTERVAL=2;UNTIL=…" → the drawer's choice and its until date (selected time zone). */
export function parseRepeat(rule: string | null | undefined, start?: string | null): { repeat: Repeat; until: string } {
  if (!rule) return { repeat: 'none', until: '' };
  const parts = Object.fromEntries(
    rule
      .replace(/^RRULE:/i, '')
      .split(';')
      .filter(Boolean)
      .map((p) => {
        const [k, v = ''] = p.split('=');
        return [k!.toUpperCase(), v];
      }),
  ) as Record<string, string>;
  const known = new Set(['FREQ', 'INTERVAL', 'UNTIL']);
  // Google writes a plain weekly repeat as FREQ=WEEKLY;BYDAY=TU (often with WKST). A single
  // BYDAY on the start's own weekday adds nothing, and with one day per period WKST can't move
  // an occurrence, so both read as plain weekly. Saving without touching Repeat keeps the rule.
  const startDay = start ? WEEKDAY_CODES[weekdayIndex(orgDateOf(start))] : null;
  if (parts.FREQ === 'WEEKLY' && (!parts.BYDAY || parts.BYDAY === startDay)) {
    if (parts.BYDAY) known.add('BYDAY');
    known.add('WKST');
  }
  const interval = Number(parts.INTERVAL ?? '1');
  let repeat: Repeat = 'custom';
  if (Object.keys(parts).every((k) => known.has(k))) {
    if (parts.FREQ === 'DAILY' && interval === 1) repeat = 'daily';
    else if (parts.FREQ === 'WEEKLY' && interval === 1) repeat = 'weekly';
    else if (parts.FREQ === 'WEEKLY' && interval === 2) repeat = 'biweekly';
    else if (parts.FREQ === 'MONTHLY' && interval === 1) repeat = 'monthly';
  }
  return { repeat, until: parts.UNTIL ? untilDate(parts.UNTIL) : '' };
}

const WEEKDAY_CODES = ['MO', 'TU', 'WE', 'TH', 'FR', 'SA', 'SU'];
const orgDayFmt = new Intl.DateTimeFormat('en-CA', { timeZone: 'America/Los_Angeles', year: 'numeric', month: '2-digit', day: '2-digit' });
/**
 * The organization's (Los Angeles) calendar date of a start: a date stays as is, an instant is
 * read on the org clock, whatever its offset ("…T02:00:00Z" on Thursday UTC is Wednesday 6 PM
 * PT). Google reads BYDAY in the event's zone, which is the org's.
 */
export function orgDateOf(start: string): string {
  if (/^\d{4}-\d{2}-\d{2}$/.test(start)) return start;
  return orgDayFmt.format(new Date(start));
}

/** 20261231T075959Z (or 20261231) → the selected-zone date it falls on. */
function untilDate(v: string): string {
  const m = /^(\d{4})(\d{2})(\d{2})(?:T(\d{2})(\d{2})(\d{2})Z?)?$/.exec(v);
  if (!m) return '';
  if (!m[4]) return `${m[1]}-${m[2]}-${m[3]}`;
  return pacificParts(new Date(Date.UTC(+m[1]!, +m[2]! - 1, +m[3]!, +m[4]!, +m[5]!, +m[6]!))).date;
}

/** The drawer's choice → RRULE text (null for no repeat). Until is the last day, inclusive. */
export function buildRepeat(repeat: Repeat, until: string, previous?: string | null, allDay = false): string | null {
  if (repeat === 'none') return null;
  if (repeat === 'custom') return previous ?? null;
  const freq = { daily: 'DAILY', weekly: 'WEEKLY', biweekly: 'WEEKLY', monthly: 'MONTHLY' }[repeat];
  const parts = [`FREQ=${freq}`];
  if (repeat === 'biweekly') parts.push('INTERVAL=2');
  if (until && allDay) {
    // An all-day series starts on a DATE, and RFC 5545 wants UNTIL of the same type.
    parts.push(`UNTIL=${until.replace(/-/g, '')}`);
  } else if (until) {
    // End of that day on the selected wall clock, in UTC as RFC 5545 asks.
    const end = new Date(pacificIso(until, '23:59')).getTime() + 59_000;
    parts.push(`UNTIL=${new Date(end).toISOString().replace(/[-:]/g, '').replace(/\.\d{3}/, '')}`);
  }
  return `RRULE:${parts.join(';')}`;
}

/** One line for the detail card: "Weekly until Dec 31". */
export function repeatText(rule: string | null | undefined, start?: string | null): string | null {
  if (!rule) return null;
  const { repeat, until } = parseRepeat(rule, start);
  if (repeat === 'none') return null;
  const label = repeat === 'custom' ? 'Repeats' : REPEAT_LABEL[repeat];
  return until ? `${label} until ${shortOf(until)}` : label;
}

// ───────────────────────────── Reminders ─────────────────────────────

export const REMINDER_MINUTES = [0, 5, 10, 15, 30, 60, 120, 1440, 2880, 10080];
export function reminderText(minutes: number): string {
  if (minutes === 0) return 'At start';
  if (minutes % 10080 === 0) return `${minutes / 10080} week${minutes === 10080 ? '' : 's'} before`;
  if (minutes % 1440 === 0) return `${minutes / 1440} day${minutes === 1440 ? '' : 's'} before`;
  if (minutes % 60 === 0) return `${minutes / 60} hour${minutes === 60 ? '' : 's'} before`;
  return `${minutes} minutes before`;
}

// ───────────────────────────── Create prefill ─────────────────────────────

/**
 * `?new=` → the drawer's starting date and time. `1` is blank (next whole hour today), a date
 * comes from a month day, and a date with a time from a week slot.
 */
export function prefill(param: string, now = new Date()): { date: string; start: string; end: string } {
  const pad = (n: number) => String(n).padStart(2, '0');
  const clock = (min: number) => `${pad(Math.floor(min / 60) % 24)}:${pad(min % 60)}`;
  const m = /^(\d{4}-\d{2}-\d{2})(?:T(\d{2}):(\d{2}))?$/.exec(param);
  if (m && m[2]) {
    const start = +m[2] * 60 + +m[3]!;
    return { date: m[1]!, start: clock(start), end: clock(Math.min(start + 60, 23 * 60 + 59)) };
  }
  if (m) return { date: m[1]!, start: '10:00', end: '11:00' };
  const p = pacificParts(now);
  const next = Math.min(Math.ceil((p.minutes + 1) / 60) * 60, 23 * 60);
  return { date: p.date, start: clock(next), end: clock(Math.min(next + 60, 23 * 60 + 59)) };
}
