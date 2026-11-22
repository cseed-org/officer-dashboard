/**
 * Shared calendar views: the entry shape both the Events page and the org Calendar page
 * render, plus the pure bucketing and placement helpers (unit-tested in tests/unit.spec.ts).
 *
 * An entry is either timed (`startsAt`/`endsAt` ISO instants, shown on the selected wall
 * clock) or a date span (`from`/`to`, inclusive YYYY-MM-DD): all-day meetings and
 * tentative ranges. Times use the helpers in lib/calendar.ts.
 */
import { addDays, diffDays, pacificParts } from '../../../lib/calendar';
import type { Hue } from '../../../lib/colors';

export interface CalEntry {
  id: string;
  title: string;
  /** Timed instance (ISO instant). Absent for date spans. */
  startsAt?: string;
  endsAt?: string | null;
  /** Inclusive date span (all-day or tentative range), YYYY-MM-DD. */
  from?: string;
  to?: string;
  /** Tentative range: dashed "Tentative" styling. */
  tentative?: boolean;
  /** Per-calendar colour (a tokens.css hue name); unset keeps the default blue. */
  hue?: Hue | string;
  /** Outlined draft styling (Events drafts). */
  draft?: boolean;
  /** Extra suffix after the title (" · Draft"); screen-reader only on month chips. */
  note?: string;
  /** Preformatted "when" line for the phone agenda. */
  agendaWhen?: string;
  /** Visible suffix inside the agenda button. */
  agendaSuffix?: string;
  /** Locked item: opens a read-only panel instead of an editor. */
  locked?: boolean;
  /** Overlay item: clicking navigates here instead of opening a panel. */
  href?: string;
}

export const isSpan = (e: CalEntry) => !e.startsAt;

/** Placement of a timed entry in 30-minute rows (see weekSlot in lib/calendar). */
export interface TimedSlot {
  entry: CalEntry;
  /** First 30-minute row. */
  start: number;
  /** Rows spanned. */
  len: number;
}

/**
 * Side-by-side columns for overlapping timed entries. Entries are clustered by transitive
 * overlap; each gets a `lane` (0-based column) and the cluster's width in `lanes`. With no
 * overlaps every entry is `{lane: 0, lanes: 1}`, a full-width block as before.
 */
export function overlapLanes(slots: { start: number; len: number }[]): { lane: number; lanes: number }[] {
  const order = slots.map((s, i) => ({ s, i })).sort((a, b) => a.s.start - b.s.start || b.s.len - a.s.len);
  const out: { lane: number; lanes: number }[] = slots.map(() => ({ lane: 0, lanes: 1 }));
  let active: { end: number; lane: number }[] = [];
  let cluster: number[] = [];
  let width = 0;
  const close = () => {
    for (const i of cluster) out[i]!.lanes = Math.max(1, width);
    cluster = [];
    width = 0;
  };
  for (const { s, i } of order) {
    const end = s.start + s.len;
    active = active.filter((a) => a.end > s.start);
    if (active.length === 0 && cluster.length > 0) close();
    const used = new Set(active.map((a) => a.lane));
    let lane = 0;
    while (used.has(lane)) lane++;
    active.push({ end, lane });
    cluster.push(i);
    width = Math.max(width, active.length);
    out[i] = { lane, lanes: 1 };
  }
  if (cluster.length > 0) close();
  return out;
}

/** Every date an entry appears on, on the selected wall clock: its start day, or each day of a span. */
export function entryDays(e: CalEntry): string[] {
  if (e.startsAt) return [pacificParts(e.startsAt).date];
  const from = e.from!;
  const to = e.to ?? e.from!;
  const days: string[] = [];
  for (let d = from; d <= to; d = addDays(d, 1)) days.push(d);
  return days;
}

/** Day → entries, spans first, then timed by start. Day keys in chronological order. */
export function byDay(entries: CalEntry[]): Map<string, CalEntry[]> {
  const sorted = [...entries].sort((a, b) => Number(!isSpan(a)) - Number(!isSpan(b)) || (a.startsAt ? Date.parse(a.startsAt) : 0) - (b.startsAt ? Date.parse(b.startsAt) : 0));
  const m = new Map<string, CalEntry[]>();
  for (const e of sorted) for (const d of entryDays(e)) m.set(d, [...(m.get(d) ?? []), e]);
  return new Map([...m.entries()].sort(([a], [b]) => (a < b ? -1 : 1)));
}

/** Hours shown: 9:00–21:00, widened to fit the week's timed entries. */
export function hourRange(entries: CalEntry[]): [number, number] {
  let lo = 9;
  let hi = 21;
  for (const e of entries) {
    if (!e.startsAt) continue;
    const s = pacificParts(e.startsAt);
    lo = Math.min(lo, Math.floor(s.minutes / 60));
    const end = e.endsAt ? pacificParts(e.endsAt) : null;
    const endMin = end && end.date === s.date ? end.minutes : s.minutes + 60;
    hi = Math.max(hi, Math.min(24, Math.ceil(endMin / 60)));
  }
  return [lo, hi];
}

/** A date span clipped to the week: 0-based start column and length in days, or null outside. */
export function clipSpan(from: string, to: string, monday: string): { start: number; len: number } | null {
  const sunday = addDays(monday, 6);
  const a = from < monday ? monday : from;
  const b = to > sunday ? sunday : to;
  if (b < a) return null;
  return { start: diffDays(monday, a), len: diffDays(a, b) + 1 };
}
