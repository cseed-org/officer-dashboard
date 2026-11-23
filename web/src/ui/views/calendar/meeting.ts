/**
 * The meeting drawer's pure parts: the draft it edits, how an org calendar item becomes one,
 * validation, and the API body it sends (tests/unit.spec.ts covers meetingBody).
 */
import { addDays, fromLocalInput, pacificParts, weekdayIndex } from '../../../lib/calendar';
import type { OrgCalendarEvent, OrgEventWriteRequest, OrgInvitee } from '../../../lib/contracts';
import { pacificIso } from '../../../lib/format';
import { buildRepeat, parseRepeat, prefill, type Repeat } from '../../../lib/orgcal';

export type Key = 'internal' | 'external';
export type Reminder = { method: 'email' | 'popup'; minutes: number };
export interface Draft {
  calendarKey: Key;
  title: string;
  mode: 'fixed' | 'tentative';
  allDay: boolean;
  date: string;
  endDate: string;
  start: string;
  end: string;
  tentFrom: string;
  tentTo: string;
  repeat: Repeat;
  until: string;
  reminders: Reminder[];
  invitees: OrgInvitee[];
  sendInvites: boolean;
  location: string;
  description: string;
}

const pad = (n: number) => String(n).padStart(2, '0');
const clockOf = (iso: string) => {
  const m = pacificParts(iso).minutes;
  return `${pad(Math.floor(m / 60))}:${pad(m % 60)}`;
};
export const isDate = (s: string) => /^\d{4}-\d{2}-\d{2}$/.test(s);
const isTime = (s: string) => /^([01]\d|2[0-3]):[0-5]\d$/.test(s);

export function draftOf(event: OrgCalendarEvent | null, newParam: string, defaultKey: Key): Draft {
  if (!event) {
    const p = prefill(newParam);
    return {
      calendarKey: defaultKey,
      title: '',
      mode: 'fixed',
      allDay: false,
      date: p.date,
      endDate: p.date,
      start: p.start,
      end: p.end,
      tentFrom: p.date,
      tentTo: addDays(p.date, 6),
      repeat: 'none',
      until: '',
      reminders: [{ method: 'popup', minutes: 10 }],
      invitees: [],
      sendInvites: defaultKey === 'external',
      location: '',
      description: '',
    };
  }
  // A listed occurrence of a repeating meeting carries its own start; the drawer edits the
  // series, so its date and times come from the master (seriesStartsAt/seriesEndsAt).
  const { startsAt, endsAt } = seriesTimes(event);
  const { repeat, until } = parseRepeat(event.rrule, startsAt);
  const tentative = startsAt === null;
  const allDay = !tentative && (event.allDay || isDate(startsAt!));
  const date = tentative ? (event.tentativeFrom ?? '') : allDay ? startsAt! : pacificParts(startsAt!).date;
  const endDate = allDay && endsAt && isDate(endsAt) ? addDays(endsAt, -1) : date;
  return {
    calendarKey: event.calendarKey === 'external' ? 'external' : 'internal',
    title: event.title,
    mode: tentative ? 'tentative' : 'fixed',
    allDay,
    date: tentative ? (event.tentativeFrom ?? '') : date,
    endDate,
    start: !tentative && !allDay ? clockOf(startsAt!) : '10:00',
    end: !tentative && !allDay && endsAt ? clockOf(endsAt) : '11:00',
    tentFrom: event.tentativeFrom ?? date,
    tentTo: event.tentativeTo ?? event.tentativeFrom ?? date,
    repeat,
    until,
    reminders: event.reminders.map((r) => ({ ...r })),
    invitees: event.invitees.map((i) => ({ ...i })),
    sendInvites: event.sendInvites,
    location: event.location,
    description: event.description,
  };
}

/** The times the drawer edits: the series' for a repeating meeting, else the item's own. */
export function seriesTimes(e: OrgCalendarEvent): { startsAt: string | null; endsAt: string | null } {
  return e.rrule && e.seriesStartsAt ? { startsAt: e.seriesStartsAt, endsAt: e.seriesEndsAt } : { startsAt: e.startsAt, endsAt: e.endsAt };
}

export type PatchBody = Partial<OrgEventWriteRequest> & { moveSeriesStart?: boolean };

/** The draft as an API body: the whole meeting on create; on edit only what changed. */
export function meetingBody(d: Draft, original: OrgCalendarEvent | null, base: Draft | null, moveStart = false): OrgEventWriteRequest | PatchBody {
  const when: Partial<OrgEventWriteRequest> =
    d.mode === 'tentative'
      ? { startsAt: null, endsAt: null, tentativeFrom: d.tentFrom, tentativeTo: d.tentTo, rrule: null }
      : d.allDay
        ? { startsAt: d.date, endsAt: addDays(d.endDate, 1), allDay: true, tentativeFrom: null, tentativeTo: null }
        : { startsAt: pacificIso(d.date, d.start), endsAt: pacificIso(d.date, d.end), allDay: false, tentativeFrom: null, tentativeTo: null };
  const rrule = d.mode === 'tentative' ? null : buildRepeat(d.repeat, d.until, original?.rrule, d.allDay);
  const full: OrgEventWriteRequest = {
    calendarKey: d.calendarKey,
    title: d.title.trim(),
    description: d.description,
    location: d.location.trim(),
    ...when,
    rrule,
    reminders: d.reminders,
    invitees: d.invitees,
    sendInvites: d.sendInvites,
  };
  if (!original || !base) return full;
  // Edits send only what changed. The draft holds the series' own date (seriesTimes), and the
  // server keeps a repeating series on its first date unless moveSeriesStart says otherwise, so
  // a new time from any occurrence moves every occurrence and drops none. Moving the series'
  // first date is a separate, explicit action ("Move series start" → moveStart).
  const out: PatchBody = {};
  const changed = <K extends keyof Draft>(...keys: K[]) => keys.some((k) => JSON.stringify(d[k]) !== JSON.stringify(base[k]));
  if (changed('title')) out.title = full.title;
  if (changed('description')) out.description = full.description;
  if (changed('location')) out.location = full.location;
  if (changed('mode', 'allDay', 'date', 'endDate', 'start', 'end', 'tentFrom', 'tentTo')) Object.assign(out, when);
  if (moveStart && original.rrule && d.mode === 'fixed' && base.mode === 'fixed' && changed('date')) out.moveSeriesStart = true;
  // Resend the rule when the series moves to another weekday (a Google-style BYDAY would keep
  // the old day) or switches all-day (UNTIL must match the start's type). Custom rules stay.
  const weekdayMoved = !!out.moveSeriesStart && weekdayIndex(d.date) !== weekdayIndex(base.date);
  const rebuild = (weekdayMoved || changed('allDay')) && d.mode === 'fixed' && d.repeat !== 'none' && d.repeat !== 'custom';
  if (changed('repeat', 'until') || (d.mode === 'tentative' && original.rrule) || rebuild) out.rrule = rrule;
  if (changed('reminders')) out.reminders = full.reminders;
  if (changed('invitees')) out.invitees = full.invitees;
  if (changed('sendInvites')) out.sendInvites = full.sendInvites;
  return out;
}

export function check(d: Draft): Record<string, string> {
  const e: Record<string, string> = {};
  if (!d.title.trim()) e.title = 'Give the meeting a title.';
  if (d.mode === 'tentative') {
    if (!isDate(d.tentFrom)) e.tentFrom = 'Pick the first possible day.';
    if (!isDate(d.tentTo)) e.tentTo = 'Pick the last possible day.';
    else if (isDate(d.tentFrom) && d.tentTo < d.tentFrom) e.tentTo = 'The range ends before it starts.';
  } else if (d.allDay) {
    if (!isDate(d.date)) e.date = 'Pick a date.';
    if (!isDate(d.endDate)) e.endDate = 'Pick the last day.';
    else if (isDate(d.date) && d.endDate < d.date) e.endDate = 'The meeting ends before it starts.';
  } else {
    if (!isDate(d.date)) e.date = 'Pick a date.';
    if (!isTime(d.start)) e.start = 'Pick a start time.';
    if (!isTime(d.end)) e.end = 'Pick an end time.';
    else if (isTime(d.start) && d.end <= d.start) e.end = 'The meeting ends before it starts.';
    // The spring-forward hour doesn't exist on the wall clock: refuse it rather than shift it.
    if (isDate(d.date) && isTime(d.start) && !fromLocalInput(`${d.date}T${d.start}`)) e.start = "That time doesn't exist on this day (clocks spring forward). Pick another.";
    if (isDate(d.date) && isTime(d.end) && !e.end && !fromLocalInput(`${d.date}T${d.end}`)) e.end = "That time doesn't exist on this day (clocks spring forward). Pick another.";
  }
  if (d.mode === 'fixed' && d.repeat !== 'none' && d.until && isDate(d.date) && d.until < d.date) e.until = 'Repeat until a day after the first meeting.';
  return e;
}

export const EMAIL = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
