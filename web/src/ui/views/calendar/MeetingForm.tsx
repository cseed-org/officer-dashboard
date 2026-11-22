import { useMemo, useState } from 'preact/hooks';
import { api } from '../../../lib/api/client';
import { useApi } from '../../../lib/api/useApi';
import { addDays, fromLocalInput, pacificParts } from '../../../lib/calendar';
import type { OrgCalendar, OrgCalendarEvent, OrgEventWriteRequest, OrgInvitee } from '../../../lib/contracts';
import { pacificIso } from '../../../lib/format';
import { MANUAL_KEYS, REMINDER_MINUTES, REPEAT_LABEL, buildRepeat, parseRepeat, prefill, reminderText, type Repeat } from '../../../lib/orgcal';
import { effectiveTimezone } from '../../../lib/timezone';
import { describe, Field } from '../../components/kit';
import { errorMessage, Notice } from '../../components/states';

type Key = 'internal' | 'external';
type Reminder = { method: 'email' | 'popup'; minutes: number };
interface Draft {
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
const isDate = (s: string) => /^\d{4}-\d{2}-\d{2}$/.test(s);
const isTime = (s: string) => /^([01]\d|2[0-3]):[0-5]\d$/.test(s);

function draftOf(event: OrgCalendarEvent | null, newParam: string, defaultKey: Key): Draft {
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
  const { repeat, until } = parseRepeat(event.rrule, event.startsAt);
  const tentative = event.startsAt === null;
  const allDay = !tentative && (event.allDay || isDate(event.startsAt!));
  const date = tentative ? (event.tentativeFrom ?? '') : allDay ? event.startsAt! : pacificParts(event.startsAt!).date;
  const endDate = allDay && event.endsAt && isDate(event.endsAt) ? addDays(event.endsAt, -1) : date;
  return {
    calendarKey: event.calendarKey === 'external' ? 'external' : 'internal',
    title: event.title,
    mode: tentative ? 'tentative' : 'fixed',
    allDay,
    date: tentative ? (event.tentativeFrom ?? '') : date,
    endDate,
    start: !tentative && !allDay ? clockOf(event.startsAt!) : '10:00',
    end: !tentative && !allDay && event.endsAt ? clockOf(event.endsAt) : '11:00',
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

/** The draft as an API body: the whole meeting on create; on edit only what changed. */
export function meetingBody(d: Draft, original: OrgCalendarEvent | null, base: Draft | null): OrgEventWriteRequest | Partial<OrgEventWriteRequest> {
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
  // A recurring meeting opened from one occurrence: keep the series start unless the time
  // fields were edited, so saving a title does not move the whole series to this day.
  const out: Partial<OrgEventWriteRequest> = {};
  const changed = <K extends keyof Draft>(...keys: K[]) => keys.some((k) => JSON.stringify(d[k]) !== JSON.stringify(base[k]));
  if (changed('title')) out.title = full.title;
  if (changed('description')) out.description = full.description;
  if (changed('location')) out.location = full.location;
  if (changed('mode', 'allDay', 'date', 'endDate', 'start', 'end', 'tentFrom', 'tentTo')) Object.assign(out, when);
  if (changed('repeat', 'until') || (d.mode === 'tentative' && original.rrule)) out.rrule = rrule;
  if (changed('reminders')) out.reminders = full.reminders;
  if (changed('invitees')) out.invitees = full.invitees;
  if (changed('sendInvites')) out.sendInvites = full.sendInvites;
  return out;
}

function check(d: Draft): Record<string, string> {
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

const EMAIL = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export function MeetingForm({
  event,
  newParam,
  calendars,
  defaultKey,
  onSaved,
  onCancel,
  onDelete,
}: {
  event: OrgCalendarEvent | null;
  /** `?new=` value: a date or date+time from the grid, or `1`. */
  newParam: string;
  calendars: OrgCalendar[];
  defaultKey: Key;
  onSaved: (e: OrgCalendarEvent, created: boolean) => void;
  onCancel: () => void;
  onDelete: (e: OrgCalendarEvent) => void;
}) {
  const base = useMemo(() => draftOf(event, newParam, defaultKey), [event?.id, newParam]);
  const [d, setD] = useState<Draft>(base);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [message, setMessage] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [sendTouched, setSendTouched] = useState(false);
  const [email, setEmail] = useState('');
  const [emailError, setEmailError] = useState<string | null>(null);
  const lookups = useApi('lookups');
  const subteams = lookups.status === 'ready' ? lookups.data.subteams : [];
  const officers = lookups.status === 'ready' ? lookups.data.hosts : [];
  const nameOf = (i: OrgInvitee) =>
    i.kind === 'subteam' ? (subteams.find((s) => s.id === i.ref)?.name ?? 'Subteam') : i.kind === 'user' ? (officers.find((u) => u.id === i.ref)?.name ?? 'Officer') : i.ref;

  const set =
    <K extends keyof Draft>(k: K) =>
    (ev: Event) => {
      const el = ev.currentTarget as HTMLInputElement;
      const v = (el.type === 'checkbox' ? el.checked : el.value) as Draft[K];
      setD((x) => ({ ...x, [k]: v }));
    };
  const recurring = event?.rrule != null;
  const tz = effectiveTimezone();
  const calName = (k: Key) => calendars.find((c) => c.key === k)?.name ?? (k === 'internal' ? 'Internal Meetings' : 'External Meetings');

  const addInvitee = (i: OrgInvitee) => {
    if (d.invitees.some((x) => x.kind === i.kind && x.ref.toLowerCase() === i.ref.toLowerCase())) return;
    setD((x) => ({ ...x, invitees: [...x.invitees, i] }));
  };
  const addEmail = () => {
    const v = email.trim();
    if (!v) return;
    if (!EMAIL.test(v)) {
      setEmailError('Enter an email address like name@example.org.');
      return;
    }
    setEmailError(null);
    addInvitee({ kind: 'email', ref: v });
    setEmail('');
  };

  async function save(ev: Event) {
    ev.preventDefault();
    // An address typed but not yet added with Add/Enter is added now, or stops the save.
    let draft = d;
    const pending = email.trim();
    if (pending) {
      if (!EMAIL.test(pending)) {
        setEmailError('Enter an email address like name@example.org, or clear the field.');
        setMessage('Fix the highlighted fields, then save again.');
        return;
      }
      setEmailError(null);
      setEmail('');
      if (!d.invitees.some((x) => x.kind === 'email' && x.ref.toLowerCase() === pending.toLowerCase())) draft = { ...d, invitees: [...d.invitees, { kind: 'email', ref: pending }] };
      setD(draft);
    }
    const found = check(draft);
    setErrors(found);
    if (Object.keys(found).length) {
      setMessage('Fix the highlighted fields, then save again.');
      return;
    }
    const body = meetingBody(draft, event, event ? base : null);
    setBusy(true);
    setMessage(null);
    try {
      if (event && Object.keys(body).length === 0) {
        onSaved(event, false);
        return;
      }
      const saved = event ? await api('orgEventUpdate', { params: { id: event.id }, body }) : await api('orgEventCreate', { body: body as OrgEventWriteRequest });
      onSaved(saved.event, !event);
    } catch (err) {
      setMessage(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form class="stack orgcal-form" onSubmit={save} noValidate data-meeting-form={event?.id ?? 'new'}>
      {recurring && <Notice tone="info">This meeting repeats. Changes apply to every occurrence.</Notice>}
      <div class="grid-fields">
        <Field id="mt-cal" label="Calendar" hint={event ? 'A meeting stays in its calendar.' : undefined}>
          <select
            class="select"
            value={d.calendarKey}
            disabled={!!event}
            onChange={(e) => {
              const k = (e.currentTarget as HTMLSelectElement).value as Key;
              setD((x) => ({ ...x, calendarKey: k, sendInvites: sendTouched ? x.sendInvites : k === 'external' }));
            }}
            {...describe('mt-cal', undefined, !!event)}
          >
            {MANUAL_KEYS.map((k) => (
              <option value={k} key={k}>
                {calName(k as Key)}
              </option>
            ))}
          </select>
        </Field>
        <Field id="mt-title" label="Title" error={errors.title}>
          <input class="input" type="text" maxLength={200} value={d.title} onInput={set('title')} {...describe('mt-title', errors.title)} />
        </Field>
      </div>

      <fieldset class="fieldset">
        <legend class="field-label">When</legend>
        <div class="segmented" role="group" aria-label="Fixed date or tentative range">
          <button type="button" class="segment" aria-pressed={d.mode === 'fixed'} onClick={() => setD((x) => ({ ...x, mode: 'fixed' }))}>
            Date and time
          </button>
          <button
            type="button"
            class="segment"
            aria-pressed={d.mode === 'tentative'}
            onClick={() => setD((x) => ({ ...x, mode: 'tentative', tentFrom: isDate(x.tentFrom) ? x.tentFrom : x.date, tentTo: isDate(x.tentTo) ? x.tentTo : x.date }))}
          >
            No set date yet
          </button>
        </div>
        {d.mode === 'tentative' ? (
          <>
            <p class="field-hint">Shown as a tentative all-day range until you pick the real date and time.</p>
            <div class="grid-fields">
              <Field id="mt-tfrom" label="Earliest day" error={errors.tentFrom}>
                <input class="input" type="date" value={d.tentFrom} onInput={set('tentFrom')} {...describe('mt-tfrom', errors.tentFrom)} />
              </Field>
              <Field id="mt-tto" label="Latest day" error={errors.tentTo}>
                <input class="input" type="date" value={d.tentTo} onInput={set('tentTo')} {...describe('mt-tto', errors.tentTo)} />
              </Field>
            </div>
          </>
        ) : (
          <>
            <label class="check">
              <input type="checkbox" checked={d.allDay} onChange={set('allDay')} />
              <span>All day</span>
            </label>
            {d.allDay ? (
              <div class="grid-fields">
                <Field id="mt-date" label="First day" error={errors.date}>
                  <input class="input" type="date" value={d.date} onInput={set('date')} {...describe('mt-date', errors.date)} />
                </Field>
                <Field id="mt-enddate" label="Last day" error={errors.endDate}>
                  <input class="input" type="date" value={d.endDate} onInput={set('endDate')} {...describe('mt-enddate', errors.endDate)} />
                </Field>
              </div>
            ) : (
              <div class="grid-fields grid-fields--3">
                <Field id="mt-date" label="Date" error={errors.date}>
                  <input class="input" type="date" value={d.date} onInput={set('date')} {...describe('mt-date', errors.date)} />
                </Field>
                <Field id="mt-start" label={`Starts · ${tz}`} error={errors.start}>
                  <input class="input" type="time" step={300} value={d.start} onInput={set('start')} {...describe('mt-start', errors.start)} />
                </Field>
                <Field id="mt-end" label="Ends" error={errors.end}>
                  <input class="input" type="time" step={300} value={d.end} onInput={set('end')} {...describe('mt-end', errors.end)} />
                </Field>
              </div>
            )}
            <div class="grid-fields">
              <Field id="mt-repeat" label="Repeat">
                <select class="select" value={d.repeat} onChange={set('repeat')} id="mt-repeat">
                  {(['none', 'daily', 'weekly', 'biweekly', 'monthly'] as Repeat[]).concat(d.repeat === 'custom' ? ['custom'] : []).map((r) => (
                    <option value={r} key={r}>
                      {REPEAT_LABEL[r]}
                    </option>
                  ))}
                </select>
              </Field>
              {d.repeat !== 'none' && d.repeat !== 'custom' && (
                <Field id="mt-until" label="Until" error={errors.until} optional>
                  <input class="input" type="date" value={d.until} onInput={set('until')} {...describe('mt-until', errors.until)} />
                </Field>
              )}
            </div>
          </>
        )}
      </fieldset>

      <fieldset class="fieldset">
        <legend class="field-label">Invitees</legend>
        {d.invitees.length > 0 ? (
          <ul class="orgcal-invitees" aria-label="Invited">
            {d.invitees.map((i) => (
              <li class="orgcal-invitee" key={`${i.kind}:${i.ref}`} data-invitee-kind={i.kind}>
                <span class="orgcal-invitee-kind">{i.kind === 'subteam' ? 'Subteam' : i.kind === 'user' ? 'Officer' : 'Email'}</span>
                <span class="orgcal-invitee-name">{nameOf(i)}</span>
                <button
                  type="button"
                  class="icon-btn icon-btn--small"
                  aria-label={`Remove ${nameOf(i)}`}
                  onClick={() => setD((x) => ({ ...x, invitees: x.invitees.filter((y) => !(y.kind === i.kind && y.ref === i.ref)) }))}
                >
                  ×
                </button>
              </li>
            ))}
          </ul>
        ) : (
          <p class="field-hint">Nobody yet. Add a subteam, an officer or any email address.</p>
        )}
        <div class="grid-fields">
          <Field id="mt-subteam" label="Add a subteam">
            <select
              class="select"
              id="mt-subteam"
              value=""
              onChange={(e) => {
                const v = (e.currentTarget as HTMLSelectElement).value;
                if (v) addInvitee({ kind: 'subteam', ref: v });
                (e.currentTarget as HTMLSelectElement).value = '';
              }}
            >
              <option value="">{lookups.status === 'loading' ? 'Loading…' : 'Choose a subteam'}</option>
              {subteams.map((s) => (
                <option value={s.id} key={s.id}>
                  {s.name}
                </option>
              ))}
            </select>
          </Field>
          <Field id="mt-officer" label="Add an officer">
            <select
              class="select"
              id="mt-officer"
              value=""
              onChange={(e) => {
                const v = (e.currentTarget as HTMLSelectElement).value;
                if (v) addInvitee({ kind: 'user', ref: v });
                (e.currentTarget as HTMLSelectElement).value = '';
              }}
            >
              <option value="">{lookups.status === 'loading' ? 'Loading…' : 'Choose an officer'}</option>
              {officers.map((u) => (
                <option value={u.id} key={u.id}>
                  {u.name}
                </option>
              ))}
            </select>
          </Field>
        </div>
        <Field id="mt-email" label="Add an email address" error={emailError ?? undefined}>
          <div class="orgcal-email-row">
            <input
              class="input"
              type="email"
              value={email}
              placeholder="name@example.org"
              onInput={(e) => setEmail((e.currentTarget as HTMLInputElement).value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault();
                  addEmail();
                }
              }}
              {...describe('mt-email', emailError ?? undefined)}
            />
            <button type="button" class="btn" onClick={addEmail}>
              Add
            </button>
          </div>
        </Field>
        <label class="check">
          <input
            type="checkbox"
            checked={d.sendInvites}
            onChange={(e) => {
              setSendTouched(true);
              setD((x) => ({ ...x, sendInvites: (e.currentTarget as HTMLInputElement).checked }));
            }}
          />
          <span>
            Send invites
            <span class="muted small check-sub">Google emails invitees about this meeting and its changes. On by default for External Meetings.</span>
          </span>
        </label>
      </fieldset>

      <fieldset class="fieldset">
        <legend class="field-label">Reminders</legend>
        {d.reminders.length === 0 && <p class="field-hint">No reminders.</p>}
        {d.reminders.map((r, i) => (
          <div class="orgcal-reminder" key={i}>
            <select
              class="select"
              aria-label={`Reminder ${i + 1} method`}
              value={r.method}
              onChange={(e) => {
                const method = (e.currentTarget as HTMLSelectElement).value as Reminder['method'];
                setD((x) => ({ ...x, reminders: x.reminders.map((y, j) => (j === i ? { ...y, method } : y)) }));
              }}
            >
              <option value="popup">Notification</option>
              <option value="email">Email</option>
            </select>
            <select
              class="select"
              aria-label={`Reminder ${i + 1} time`}
              value={String(r.minutes)}
              onChange={(e) => {
                const minutes = Number((e.currentTarget as HTMLSelectElement).value);
                setD((x) => ({ ...x, reminders: x.reminders.map((y, j) => (j === i ? { ...y, minutes } : y)) }));
              }}
            >
              {Array.from(new Set([...REMINDER_MINUTES, r.minutes])).sort((a, b) => a - b).map((m) => (
                <option value={String(m)} key={m}>
                  {reminderText(m)}
                </option>
              ))}
            </select>
            <button type="button" class="icon-btn icon-btn--small" aria-label={`Remove reminder ${i + 1}`} onClick={() => setD((x) => ({ ...x, reminders: x.reminders.filter((_, j) => j !== i) }))}>
              ×
            </button>
          </div>
        ))}
        {d.reminders.length < 5 && (
          <button type="button" class="btn btn--small orgcal-add" onClick={() => setD((x) => ({ ...x, reminders: [...x.reminders, { method: 'popup', minutes: 30 }] }))}>
            + Add reminder
          </button>
        )}
      </fieldset>

      <Field id="mt-location" label="Location" optional>
        <input class="input" type="text" maxLength={500} value={d.location} onInput={set('location')} id="mt-location" />
      </Field>
      <Field id="mt-description" label="Description" optional>
        <textarea class="textarea" rows={4} maxLength={10000} value={d.description} onInput={set('description')} id="mt-description" />
      </Field>

      {message && <Notice tone="negative">{message}</Notice>}
      <div class="action-row">
        <button type="submit" class="btn btn--primary" disabled={busy}>
          {busy ? 'Saving…' : event ? 'Save changes' : 'Create meeting'}
        </button>
        <button type="button" class="btn" onClick={onCancel} disabled={busy}>
          Cancel
        </button>
        {event && (
          <button type="button" class="btn btn--danger orgcal-delete" onClick={() => onDelete(event)} disabled={busy}>
            Delete
          </button>
        )}
      </div>
    </form>
  );
}
