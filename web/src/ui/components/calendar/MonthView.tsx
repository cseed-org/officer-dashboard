import {
  addDays,
  addMonths,
  dayNumber,
  longDay,
  monthGrid,
  monthLabel,
  monthOf,
  mondayOf,
  pacificParts,
  pacificToday,
  timeRange,
} from '../../../lib/calendar';
import type { CalEntry } from './layout';
import { byDay, entryClass, hueStyle } from './layout';

const DOW = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'];
const PER_DAY = 3;

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

export function MonthView({
  month,
  entries,
  onOpen,
  onMonth,
  onWeek,
  onDayClick,
}: {
  month: string;
  entries: CalEntry[];
  onOpen: (e: CalEntry) => void;
  /** Provided when the view carries its own ‹ › Today navigation (the Events page). */
  onMonth?: (ym: string) => void;
  /** "+n more" jumps to that day's week. */
  onWeek: (monday: string) => void;
  /** Clicking an empty part of a day (or its number) starts a new item on it. */
  onDayClick?: (date: string) => void;
}) {
  const today = pacificToday();
  const days = byDay(entries);
  const inMonth = [...days.entries()].filter(([d]) => monthOf(d) === month);
  // The agenda lists each entry once (a multi-day span is not repeated per day); recurring
  // timed instances appear once per occurrence.
  const seen = new Set<string>();
  const agenda = inMonth.flatMap(([, list]) =>
    list.filter((e) => {
      const k = e.startsAt ? `${e.id}@${pacificParts(e.startsAt).date}` : e.id;
      if (seen.has(k)) return false;
      seen.add(k);
      return true;
    }),
  );
  return (
    <section class="cal" id="view-month" aria-label={`Events in ${monthLabel(month)}`}>
      {onMonth && (
        <div class="cal-head">
          <span />
          <Nav label="month" onPrev={() => onMonth(addMonths(month, -1))} onToday={() => onMonth(monthOf(today))} onNext={() => onMonth(addMonths(month, 1))} />
        </div>
      )}
      <div class="cal-month" role="group" aria-label={monthLabel(month)}>
        {DOW.map((d) => (
          <div class="cal-dow" key={d}>
            {d}
          </div>
        ))}
        {monthGrid(month).map(({ date, outside }) => {
          const list = outside ? [] : (days.get(date) ?? []);
          return (
            <div
              class={`cal-day${outside ? ' is-outside' : ''}${date === today ? ' is-today' : ''}${onDayClick ? ' is-create' : ''}`}
              key={date}
              data-date={date}
              onClick={onDayClick ? (e) => { if (!(e.target as HTMLElement).closest('button')) onDayClick(date); } : undefined}
            >
              {onDayClick && !outside ? (
                <button type="button" class="cal-date" onClick={() => onDayClick(date)} aria-label={`New meeting on ${longDay(date)}`}>
                  {dayNumber(date)}
                </button>
              ) : (
                <span class="cal-date">{outside ? '' : dayNumber(date)}</span>
              )}
              {list.slice(0, PER_DAY).map((e) => (
                <button
                  type="button"
                  class={`cal-event${entryClass(e)}`}
                  key={`${e.id}@${date}`}
                  title={e.tentative ? `Tentative: ${e.title}` : undefined}
                  data-cal-event={e.id}
                  data-locked={e.locked || undefined}
                  style={hueStyle(e.hue)}
                  onClick={() => onOpen(e)}
                >
                  {e.startsAt && <span class="cal-event-time">{timeRange(e.startsAt)}</span>}
                  <span class="cal-event-title">{e.title}</span>
                  <span class="visually-hidden">
                    {e.note ?? ''}
                    {e.locked ? ' (read-only)' : ''}
                  </span>
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
                    <span key={e.id} style={hueStyle(e.hue)} />
                  ))}
                </span>
              )}
            </div>
          );
        })}
      </div>
      <ul class="cal-agenda" aria-label={`${monthLabel(month)} agenda`}>
        {agenda.length === 0 && <li class="cal-agenda-day">No events this month</li>}
        {agenda.map((e) => (
            <li key={e.startsAt ? `${e.id}@${pacificParts(e.startsAt).date}` : e.id}>
              <p class="cal-agenda-day">{e.agendaWhen}</p>
              <button
                type="button"
                class={`cal-event${entryClass(e)}`}
                data-cal-event={e.id}
                data-locked={e.locked || undefined}
                style={hueStyle(e.hue)}
                onClick={() => onOpen(e)}
              >
                {e.title}
                {e.agendaSuffix ?? ''}
                {e.locked && <span class="visually-hidden"> (read-only)</span>}
              </button>
            </li>
          ))}
      </ul>
    </section>
  );
}
