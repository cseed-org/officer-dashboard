/**
 * Calendar maths for the Events month and week views. Every event time is shown on the
 * club's wall clock (Pacific: Vancouver and Los Angeles share offsets), whatever the
 * browser's time zone. Dates are YYYY-MM-DD strings; weeks start on Monday.
 */
import { pacificIso } from './format';

const TZ = 'America/Los_Angeles';
export const TZ_LABEL = 'America/Vancouver';

const partsFmt = new Intl.DateTimeFormat('en-US', { timeZone: TZ, hourCycle: 'h23', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' });

/** Pacific calendar date and minutes after midnight for an ISO instant. */
export function pacificParts(iso: string | Date): { date: string; minutes: number } {
  const p = Object.fromEntries(partsFmt.formatToParts(typeof iso === 'string' ? new Date(iso) : iso).map((x) => [x.type, x.value]));
  const hour = Number(p.hour) % 24;
  return { date: `${p.year}-${p.month}-${p.day}`, minutes: hour * 60 + Number(p.minute) };
}

const pad = (n: number) => String(n).padStart(2, '0');
const toUtc = (date: string) => {
  const [y, m, d] = date.split('-').map(Number) as [number, number, number];
  return Date.UTC(y, m - 1, d);
};
const fromUtc = (ms: number) => {
  const d = new Date(ms);
  return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}`;
};

export const addDays = (date: string, n: number) => fromUtc(toUtc(date) + n * 86_400_000);
/** 0 = Monday … 6 = Sunday. */
export const weekdayIndex = (date: string) => (new Date(toUtc(date)).getUTCDay() + 6) % 7;
export const mondayOf = (date: string) => addDays(date, -weekdayIndex(date));
export const monthOf = (date: string) => date.slice(0, 7);
export function addMonths(ym: string, n: number): string {
  const [y, m] = ym.split('-').map(Number) as [number, number];
  const t = y * 12 + (m - 1) + n;
  return `${Math.floor(t / 12)}-${pad((t % 12) + 1)}`;
}

/** The Monday-first days covering a month: whole weeks, with outside days flagged. */
export function monthGrid(ym: string): { date: string; outside: boolean }[] {
  const first = `${ym}-01`;
  const start = mondayOf(first);
  const next = `${addMonths(ym, 1)}-01`;
  const end = addDays(mondayOf(addDays(next, -1)), 6);
  const out: { date: string; outside: boolean }[] = [];
  for (let d = start; d <= end; d = addDays(d, 1)) out.push({ date: d, outside: monthOf(d) !== ym });
  return out;
}

export const weekDays = (monday: string) => Array.from({ length: 7 }, (_, i) => addDays(monday, i));

const monthNameFmt = new Intl.DateTimeFormat('en-US', { month: 'long', year: 'numeric', timeZone: 'UTC' });
const monthOnlyFmt = new Intl.DateTimeFormat('en-US', { month: 'long', timeZone: 'UTC' });
const shortDayFmt = new Intl.DateTimeFormat('en-US', { month: 'short', day: 'numeric', timeZone: 'UTC' });
const longDayFmt = new Intl.DateTimeFormat('en-US', { month: 'long', day: 'numeric', year: 'numeric', timeZone: 'UTC' });
const weekdayFmt = new Intl.DateTimeFormat('en-US', { weekday: 'short', timeZone: 'UTC' });
const at = (date: string) => new Date(toUtc(date) + 12 * 3_600_000);

export const monthLabel = (ym: string) => monthNameFmt.format(at(`${ym}-01`));
export const shortDay = (date: string) => shortDayFmt.format(at(date));
export const longDay = (date: string) => longDayFmt.format(at(date));
export const weekdayShort = (date: string) => weekdayFmt.format(at(date));
export const dayNumber = (date: string) => Number(date.slice(8));

/** "November 9–15, 2026", "Nov 30 – Dec 6, 2026", "Dec 28, 2026 – Jan 3, 2027". */
export function weekLabel(monday: string): string {
  const sunday = addDays(monday, 6);
  const [y1, m1] = [monday.slice(0, 4), monday.slice(5, 7)];
  const [y2, m2] = [sunday.slice(0, 4), sunday.slice(5, 7)];
  if (y1 === y2 && m1 === m2) return `${monthOnlyFmt.format(at(monday))} ${dayNumber(monday)}–${dayNumber(sunday)}, ${y1}`;
  if (y1 === y2) return `${shortDay(monday)} – ${shortDay(sunday)}, ${y1}`;
  return `${shortDay(monday)}, ${y1} – ${shortDay(sunday)}, ${y2}`;
}

/** 18:00 → "6", 10:30 → "10:30"; with the meridiem when asked. */
function clock(minutes: number, meridiem: boolean): string {
  const h = Math.floor(minutes / 60) % 24;
  const m = minutes % 60;
  const h12 = h % 12 === 0 ? 12 : h % 12;
  return `${h12}${m ? `:${pad(m)}` : ''}${meridiem ? (h < 12 ? ' AM' : ' PM') : ''}`;
}

/** "6–8 PM", "10–11:30 AM", "11 AM–1 PM", or "6 PM" with no end (same Pacific day only). */
export function timeRange(startsAt: string, endsAt?: string | null): string {
  const s = pacificParts(startsAt);
  if (!endsAt) return clock(s.minutes, true);
  const e = pacificParts(endsAt);
  if (e.date !== s.date) return `${clock(s.minutes, true)} – ${shortDay(e.date)}, ${clock(e.minutes, true)}`;
  const samePeriod = s.minutes < 720 === e.minutes < 720;
  return `${clock(s.minutes, !samePeriod)}–${clock(e.minutes, true)}`;
}

/** "Nov 12 · 6–8 PM" */
export const whenShort = (startsAt: string, endsAt?: string | null) => `${shortDay(pacificParts(startsAt).date)} · ${timeRange(startsAt, endsAt)}`;
/** "November 12, 2026 · 6–8 PM · America/Vancouver" (the handoff kit's Time field). */
export const whenLong = (startsAt: string, endsAt?: string | null) => `${longDay(pacificParts(startsAt).date)} · ${timeRange(startsAt, endsAt)} · ${TZ_LABEL}`;

/** ISO instant → the value of a datetime-local input on the Pacific wall clock. */
export function toLocalInput(iso: string | null | undefined): string {
  if (!iso) return '';
  const p = pacificParts(iso);
  return `${p.date}T${pad(Math.floor(p.minutes / 60))}:${pad(p.minutes % 60)}`;
}

/** A datetime-local value read as Pacific wall time → ISO with offset; null when blank or malformed. */
export function fromLocalInput(value: string): string | null {
  const m = /^(\d{4}-\d{2}-\d{2})T(\d{2}:\d{2})/.exec(value.trim());
  return m ? pacificIso(m[1]!, m[2]!) : null;
}

/** Pacific date of an instant ("today" for the calendar highlight). */
export const pacificToday = (now = new Date()) => pacificParts(now).date;

/**
 * Week-view placement in 30-minute rows from `firstHour`: the row an event starts on and
 * how many rows it spans, clipped to the visible hours (null when it is wholly outside).
 */
export function weekSlot(startsAt: string, endsAt: string | null | undefined, firstHour: number, lastHour: number): { start: number; len: number } | null {
  const s = pacificParts(startsAt);
  let endMin = s.minutes + 60;
  if (endsAt) {
    const e = pacificParts(endsAt);
    endMin = e.date === s.date ? Math.max(e.minutes, s.minutes + 30) : 24 * 60;
  }
  const lo = firstHour * 60;
  const hi = lastHour * 60;
  const a = Math.max(s.minutes, lo);
  const b = Math.min(endMin, hi);
  if (b <= a) return null;
  return { start: Math.floor((a - lo) / 30), len: Math.max(1, Math.ceil((b - a) / 30)) };
}
