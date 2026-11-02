import type { EventListItem } from '../../../lib/contracts';
import {
  addDays,
  addMonths,
  dayNumber,
  monthGrid,
  monthLabel,
  monthOf,
  mondayOf,
  pacificParts,
  pacificToday,
  timeRange,
  weekDays,
  weekLabel,
  weekSlot,
  weekdayShort,
  whenShort,
} from '../../../lib/calendar';
import { shownStatus, statusLabel } from './shared';

const DOW = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'];
const PER_DAY = 3;

type Open = (e: EventListItem) => void;

function byDay(events: EventListItem[]): Map<string, EventListItem[]> {
  const m = new Map<string, EventListItem[]>();
  for (const e of [...events].sort((a, b) => a.startsAt.localeCompare(b.startsAt))) {
    const d = pacificParts(e.startsAt).date;
    m.set(d, [...(m.get(d) ?? []), e]);
  }
  return m;
}

const draftSuffix = (e: EventListItem) => (e.status === 'draft' ? ' · Draft' : e.status === 'cancelled' ? ' · Cancelled' : '');

function Nav({ label, onPrev, onToday, onNext }: { label: string; onPrev: () => void; onToday: () => void; onNext: () => void }) {
  return (
    <div class="cal-nav" role="group" aria-label={`Change ${label}`}>
      <button type="button" class="btn btn--small" onClick={onPrev} aria-label={`Previous ${label}`}>
        ‹ <span class="visually-hidden">Previous {label}</span>
      </button>
      <button type="button" class="btn btn--small" onClick={onToday}>
        Today
      </button>
      <button type="button" class="btn btn--small" onClick={onNext} aria-label={`Next ${label}`}>
        › <span class="visually-hidden">Next {label}</span>
      </button>
    </div>
  );
}

export function MonthView({ month, events, onOpen, onMonth, onWeek }: { month: string; events: EventListItem[]; onOpen: Open; onMonth: (ym: string) => void; onWeek: (monday: string) => void }) {
  const today = pacificToday();
  const days = byDay(events);
  const inMonth = [...days.entries()].filter(([d]) => monthOf(d) === month);
  return (
    <section class="cal" id="view-month" aria-label={`Events in ${monthLabel(month)}`}>
      <div class="cal-head">
        <span />
        <Nav label="month" onPrev={() => onMonth(addMonths(month, -1))} onToday={() => onMonth(monthOf(today))} onNext={() => onMonth(addMonths(month, 1))} />
      </div>
      <div class="cal-month" role="group" aria-label={monthLabel(month)}>
        {DOW.map((d) => (
          <div class="cal-dow" key={d}>
            {d}
          </div>
        ))}
        {monthGrid(month).map(({ date, outside }) => {
          const list = outside ? [] : (days.get(date) ?? []);
          return (
            <div class={`cal-day${outside ? ' is-outside' : ''}${date === today ? ' is-today' : ''}`} key={date} data-date={date}>
              <span class="cal-date">{outside ? '' : dayNumber(date)}</span>
              {list.slice(0, PER_DAY).map((e) => (
                <button type="button" class={`cal-event${e.status === 'draft' ? ' is-draft' : ''}`} key={e.id} data-cal-event={e.id} onClick={() => onOpen(e)}>
                  <span class="cal-event-time">{timeRange(e.startsAt)}</span>
                  <span class="cal-event-title">{e.name}</span>
                  <span class="visually-hidden">{draftSuffix(e)}</span>
                </button>
              ))}
              {list.length > PER_DAY && (
                <button type="button" class="cal-more" onClick={() => onWeek(mondayOf(date))}>
                  +{list.length - PER_DAY} more
                </button>
              )}
              {list.length > 0 && (
                <span class="cal-dots" aria-hidden="true">
                  {list.slice(0, PER_DAY).map((e) => (
                    <span key={e.id} />
                  ))}
                </span>
              )}
            </div>
          );
        })}
      </div>
      <ul class="cal-agenda" aria-label={`${monthLabel(month)} agenda`}>
        {inMonth.length === 0 && <li class="cal-agenda-day">No events this month</li>}
        {inMonth.flatMap(([, list]) =>
          list.map((e) => (
            <li key={e.id}>
              <p class="cal-agenda-day">
                {whenShort(e.startsAt, e.endsAt)}
                {e.status === 'draft' ? ' · Draft' : ''}
              </p>
              <button type="button" class={`cal-event${e.status === 'draft' ? ' is-draft' : ''}`} data-cal-event={e.id} onClick={() => onOpen(e)}>
                {e.name}
              </button>
            </li>
          )),
        )}
      </ul>
    </section>
  );
}

/** Hours shown: 9:00–21:00, widened to fit the week's events. */
function hourRange(events: EventListItem[]): [number, number] {
  let lo = 9;
  let hi = 21;
  for (const e of events) {
    const s = pacificParts(e.startsAt);
    lo = Math.min(lo, Math.floor(s.minutes / 60));
    const end = e.endsAt ? pacificParts(e.endsAt) : null;
    const endMin = end && end.date === s.date ? end.minutes : s.minutes + 60;
    hi = Math.max(hi, Math.min(24, Math.ceil(endMin / 60)));
  }
  return [lo, hi];
}

export function WeekView({ monday, events, onOpen, onWeek }: { monday: string; events: EventListItem[]; onOpen: Open; onWeek: (monday: string) => void }) {
  const today = pacificToday();
  const days = weekDays(monday);
  const map = byDay(events);
  const inWeek = days.flatMap((d) => map.get(d) ?? []);
  const [first, last] = hourRange(inWeek);
  const rows = (last - first) * 2;
  const label = `${weekLabel(monday)} · Vancouver`;
  return (
    <section class="cal" id="view-week" aria-label={`Events in the week of ${weekLabel(monday)}`}>
      <div class="cal-head">
        <h3 class="cal-title">{label}</h3>
        <Nav label="week" onPrev={() => onWeek(addDays(monday, -7))} onToday={() => onWeek(mondayOf(today))} onNext={() => onWeek(addDays(monday, 7))} />
      </div>
      <div class="cal-week">
        <div class="cal-week-corner" />
        {days.map((d) => (
          <div class={`cal-week-day${d === today ? ' is-today' : ''}`} key={d}>
            {weekdayShort(d)}
            <strong>{dayNumber(d)}</strong>
          </div>
        ))}
        <div class="cal-gutter" style={{ '--rows': rows }}>
          {Array.from({ length: last - first }, (_, i) => (
            <span class="cal-hour" style={{ gridRow: i * 2 + 1 }} key={i}>
              {first + i}:00
            </span>
          ))}
        </div>
        {days.map((d) => (
          <div class="cal-col" style={{ '--rows': rows }} key={d} data-date={d}>
            {(map.get(d) ?? []).map((e) => {
              const slot = weekSlot(e.startsAt, e.endsAt, first, last);
              if (!slot) return null;
              const status = shownStatus(e);
              return (
                <button
                  type="button"
                  class={`cal-block${e.status === 'draft' ? ' is-draft' : ''}`}
                  style={{ '--start': slot.start, '--len': slot.len }}
                  key={e.id}
                  data-cal-event={e.id}
                  onClick={() => onOpen(e)}
                >
                  <span class="cal-block-title">
                    {e.name}
                    {status === 'draft' || status === 'cancelled' ? ` · ${statusLabel(status)}` : ''}
                  </span>
                  <span class="cal-block-time">{timeRange(e.startsAt, e.endsAt)}</span>
                </button>
              );
            })}
          </div>
        ))}
      </div>
      <ul class="cal-agenda" aria-label={`Agenda for ${weekLabel(monday)}`}>
        {inWeek.length === 0 && <li class="cal-agenda-day">No events this week</li>}
        {inWeek.map((e) => (
          <li key={e.id}>
            <p class="cal-agenda-day">
              {weekdayShort(pacificParts(e.startsAt).date)} {whenShort(e.startsAt, e.endsAt)}
            </p>
            <button type="button" class={`cal-event${e.status === 'draft' ? ' is-draft' : ''}`} data-cal-event={e.id} onClick={() => onOpen(e)}>
              {e.name}
              {draftSuffix(e)}
            </button>
          </li>
        ))}
      </ul>
    </section>
  );
}
