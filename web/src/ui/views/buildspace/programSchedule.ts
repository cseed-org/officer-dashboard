import type { Program, ProgramException } from "../../../lib/contracts";

/**
 * Weekly program schedules in club wall time (America/Los_Angeles), whatever the viewer's
 * time zone. Pure functions, so tests/programs-schedule.spec.ts drives them with a fixed "now".
 */

export const WEEKDAYS = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];

const pad = (n: number) => String(n).padStart(2, "0");
/** "18:30" → 1110. */
export const minutesOf = (hhmm: string) => Number(hhmm.slice(0, 2)) * 60 + Number(hhmm.slice(3, 5));
/** "23:00" + 120 → "01:00" (wraps past midnight). */
export const addMinutes = (hhmm: string, minutes: number) => {
  const total = (minutesOf(hhmm) + minutes) % 1440;
  return `${pad(Math.floor(total / 60))}:${pad(total % 60)}`;
};
/** "18:00" → "6 PM", "18:30" → "6:30 PM". */
export const clock = (hhmm: string) => {
  const h = Number(hhmm.slice(0, 2)), m = Number(hhmm.slice(3, 5));
  return `${h % 12 === 0 ? 12 : h % 12}${m ? `:${pad(m)}` : ""} ${h < 12 ? "AM" : "PM"}`;
};
/** "2026-11-30" + 2 → "2026-12-02" (calendar days, no time zone involved). */
export const addDays = (date: string, n: number) => {
  const d = new Date(`${date}T12:00:00Z`);
  d.setUTCDate(d.getUTCDate() + n);
  return d.toISOString().slice(0, 10);
};
/** Sunday = 0, the same convention as the API (and Go's time.Weekday). */
export const weekdayOf = (date: string) => new Date(`${date}T12:00:00Z`).getUTCDay();

const laFormatter = new Intl.DateTimeFormat("en-US", {
  hourCycle: "h23",
  timeZone: "America/Los_Angeles",
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
});
const laFields = (instant: Date) => Object.fromEntries(laFormatter.formatToParts(instant).map((x) => [x.type, x.value]));

/** A club wall time ("2026-11-25", "18:00") as an ISO instant with its Los Angeles offset. */
export function laIso(date: string, time: string): string {
  const [y, m, d] = date.split("-").map(Number) as [number, number, number];
  const [hh, mm] = time.split(":").map(Number) as [number, number];
  const wall = Date.UTC(y, m - 1, d, hh, mm);
  const offsetAt = (instant: number) => {
    const p = laFields(new Date(instant));
    return Date.UTC(+p.year!, +p.month! - 1, +p.day!, +p.hour!, +p.minute!) - instant;
  };
  let offset = offsetAt(wall);
  offset = offsetAt(wall - offset); // settle around DST changes
  const mins = Math.round(offset / 60_000);
  const sign = mins < 0 ? "-" : "+";
  return `${date}T${pad(hh)}:${pad(mm)}:00${sign}${pad(Math.trunc(Math.abs(mins) / 60))}:${pad(Math.abs(mins) % 60)}`;
}

/** An instant as club wall time: "2026-11-26T02:00:00Z" → { date: "2026-11-25", time: "18:00" }. */
export function laParts(iso: string): { date: string; time: string } {
  const p = laFields(new Date(iso));
  return { date: `${p.year}-${p.month}-${p.day}`, time: `${p.hour}:${p.minute}` };
}

/** "Now" in club wall time: today's date and minutes past midnight. */
export type ClubNow = { today: string; minutes: number };
export function laNow(now = new Date()): ClubNow {
  const p = laParts(now.toISOString());
  return { today: p.date, minutes: minutesOf(p.time) };
}

export type Session = {
  /** The original occurrence date: the exception key. */
  date: string;
  /** The date it actually happens on: the original date, or a moved session's new date. */
  on: string;
  start: string;
  end: string;
  location: string;
  exception: ProgramException | null;
};

/** Every occurrence from the first scheduled weekday to the last date, with exceptions applied. */
function allSessions(p: Program): Session[] {
  const out: Session[] = [];
  const exceptions = new Map(p.exceptions.map((e) => [e.onDate, e]));
  let date = p.startsOn;
  while (weekdayOf(date) !== p.weekday) date = addDays(date, 1);
  for (; date <= p.endsOn; date = addDays(date, 7)) {
    const exception = exceptions.get(date) ?? null;
    if (exception?.kind === "move" && exception.newStartsAt && exception.newEndsAt) {
      const start = laParts(exception.newStartsAt);
      out.push({ date, on: start.date, start: start.time, end: laParts(exception.newEndsAt).time, location: exception.newLocation || p.location, exception });
    } else {
      out.push({ date, on: date, start: p.startTime, end: addMinutes(p.startTime, p.durationMin), location: p.location, exception: exception?.kind === "skip" ? exception : null });
    }
  }
  return out;
}

/**
 * True once the session is over. An end at or before its start is the next morning, so a
 * 23:00–01:00 session is still running at 00:30 the next day (review N-2).
 */
const ended = (s: Session, now: ClubNow) => {
  const endDay = minutesOf(s.end) <= minutesOf(s.start) ? addDays(s.on, 1) : s.on;
  if (endDay !== now.today) return endDay < now.today;
  return minutesOf(s.end) <= now.minutes;
};

/**
 * Sessions that haven't ended, in the order they happen (by the date they actually happen on,
 * then start time). Skipped sessions stay listed so the skip can be undone.
 */
export function programSessions(p: Program, now: ClubNow, limit = 12): Session[] {
  return allSessions(p)
    .filter((s) => !ended(s, now))
    .sort((a, b) => a.on.localeCompare(b.on) || a.start.localeCompare(b.start) || a.date.localeCompare(b.date))
    .slice(0, limit);
}

/** The next session that still happens (skips don't count). */
export const nextSession = (p: Program, now: ClubNow) => programSessions(p, now, Infinity).find((s) => s.exception?.kind !== "skip") ?? null;

/** Exceptions a saved weekday or date range would drop: their dates are no longer scheduled sessions. */
export function strandedExceptions(p: Program, next: { weekday: number; startsOn: string; endsOn: string }): ProgramException[] {
  return p.exceptions.filter((e) => e.onDate < next.startsOn || e.onDate > next.endsOn || weekdayOf(e.onDate) !== next.weekday);
}
