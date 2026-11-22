import { useEffect, useState } from 'preact/hooks';
import { effectiveTimezone } from '../../../lib/timezone';
import {
  addDays,
  dayNumber,
  diffDays,
  mondayOf,
  pacificParts,
  pacificToday,
  timeRange,
  weekDays,
  weekLabel,
  weekdayShort,
  weekSlot,
} from '../../../lib/calendar';
import type { CalEntry, TimedSlot } from './layout';
import { clipSpan, entryClass, hourRange, hueStyle, isSpan, overlapLanes } from './layout';

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

export function WeekView({
  monday,
  entries,
  onOpen,
  onWeek,
  title,
  onSlotClick,
  nowLine = false,
}: {
  monday: string;
  entries: CalEntry[];
  onOpen: (e: CalEntry) => void;
  /** Provided when the view carries its own ‹ › Today navigation (the Events page). */
  onWeek?: (monday: string) => void;
  /** Heading above the grid when onWeek is set (Events: week + time zone). */
  title?: string;
  /** Clicking an empty slot starts a new item at that day and 30-minute step. */
  onSlotClick?: (date: string, minutes: number) => void;
  /** Draw the current-time line on today's column. */
  nowLine?: boolean;
}) {
  const today = pacificToday();
  const days = weekDays(monday);
  const timedByDay = new Map<string, CalEntry[]>();
  for (const e of entries) {
    if (isSpan(e)) continue;
    const d = pacificParts(e.startsAt!).date;
    timedByDay.set(d, [...(timedByDay.get(d) ?? []), e]);
  }
  for (const [d, list] of timedByDay) timedByDay.set(d, list.sort((a, b) => Date.parse(a.startsAt!) - Date.parse(b.startsAt!)));
  const spans = entries
    .filter(isSpan)
    .map((e) => ({ e, clip: clipSpan(e.from!, e.to ?? e.from!, monday) }))
    .filter((x): x is { e: CalEntry; clip: { start: number; len: number } } => !!x.clip)
    .sort((a, b) => a.clip.start - b.clip.start || b.clip.len - a.clip.len || a.e.title.localeCompare(b.e.title));
  const band = overlapLanes(spans.map((x) => x.clip));
  const bandRows = band.reduce((n, b) => Math.max(n, b.lane + 1), 0);

  const inWeek = days.flatMap((d) => timedByDay.get(d) ?? []);
  const [first, last] = hourRange(inWeek);
  const rows = (last - first) * 2;
  const label = title ?? `${weekLabel(monday)} · ${effectiveTimezone()}`;

  // The now-line's row within the visible hours, live to the minute.
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!nowLine) return;
    const t = setInterval(() => setNow(Date.now()), 60_000);
    return () => clearInterval(t);
  }, [nowLine]);
  const nowParts = pacificParts(new Date(now));
  const nowRow = (nowParts.minutes - first * 60) / 30;

  return (
    <section class="cal" id="view-week" aria-label={`Events in the week of ${weekLabel(monday)}`}>
      {onWeek && (
        <div class="cal-head">
          <h3 class="cal-title">{label}</h3>
          <Nav label="week" onPrev={() => onWeek(addDays(monday, -7))} onToday={() => onWeek(mondayOf(today))} onNext={() => onWeek(addDays(monday, 7))} />
        </div>
      )}
      <div class="cal-week">
        <div class="cal-week-corner" />
        {days.map((d) => (
          <div class={`cal-week-day${d === today ? ' is-today' : ''}`} key={d}>
            {weekdayShort(d)}
            <strong>{dayNumber(d)}</strong>
          </div>
        ))}
        {spans.length > 0 && (
          <>
            <div class="cal-allday-corner">all-day</div>
            <div class="cal-allday" style={{ gridRow: 2 }}>
              {spans.map(({ e, clip }, i) => (
                <button
                  type="button"
                  class={`cal-span${entryClass(e)}`}
                  style={{ ...hueStyle(e.hue), gridColumn: `${clip.start + 1} / span ${clip.len}`, gridRow: band[i]!.lane + 1 }}
                  key={e.id}
                  data-cal-event={e.id}
                  data-locked={e.locked || undefined}
                  onClick={() => onOpen(e)}
                >
                  {e.tentative && <span class="cal-span-tag">Tentative · </span>}
                  {e.title}
                  {e.locked && <span class="visually-hidden"> (read-only)</span>}
                </button>
              ))}
            </div>
          </>
        )}
        <div class="cal-gutter" style={{ '--rows': rows, gridRow: spans.length > 0 ? 3 : 2 } as Record<string, string | number>}>
          {Array.from({ length: last - first }, (_, i) => (
            <span class="cal-hour" style={{ gridRow: i * 2 + 1 }} key={i}>
              {first + i}:00
            </span>
          ))}
        </div>
        {days.map((d) => {
          const dayEntries = timedByDay.get(d) ?? [];
          const slots: TimedSlot[] = [];
          for (const e of dayEntries) {
            const slot = weekSlot(e.startsAt!, e.endsAt, first, last);
            if (slot) slots.push({ entry: e, ...slot });
          }
          const lanes = overlapLanes(slots);
          return (
            <div
              class="cal-col"
              style={{ '--rows': rows, gridRow: spans.length > 0 ? 3 : 2 } as Record<string, string | number>}
              key={d}
              data-date={d}
              onClick={
                onSlotClick
                  ? (ev) => {
                      if ((ev.target as HTMLElement).closest('button')) return;
                      const rect = (ev.currentTarget as HTMLElement).getBoundingClientRect();
                      const row = Math.floor(((ev.clientY - rect.top) / rect.height) * rows);
                      onSlotClick(d, first * 60 + Math.max(0, Math.min(rows - 1, row)) * 30);
                    }
                  : undefined
              }
            >
              {slots.map((s, i) => (
                <button
                  type="button"
                  class={`cal-block${entryClass(s.entry)}`}
                  style={{ ...hueStyle(s.entry.hue), '--start': s.start, '--len': s.len, '--lane': lanes[i]!.lane, '--lanes': lanes[i]!.lanes } as Record<string, string | number>}
                  key={s.entry.id}
                  data-cal-event={s.entry.id}
                  data-locked={s.entry.locked || undefined}
                  onClick={() => onOpen(s.entry)}
                >
                  <span class="cal-block-title">
                    {s.entry.title}
                    {s.entry.note ?? ''}
                    {s.entry.locked && <span class="visually-hidden"> (read-only)</span>}
                  </span>
                  <span class="cal-block-time">{timeRange(s.entry.startsAt!, s.entry.endsAt)}</span>
                </button>
              ))}
              {nowLine && d === nowParts.date && nowRow >= 0 && nowRow <= rows && <div class="cal-now" style={{ '--now': nowRow } as Record<string, string | number>} aria-hidden="true" />}
            </div>
          );
        })}
      </div>
      <ul class="cal-agenda" aria-label={`Agenda for ${weekLabel(monday)}`}>
        {inWeek.length === 0 && spans.length === 0 && <li class="cal-agenda-day">No events this week</li>}
        {(() => {
          // A multi-day span is listed once, on its first visible day.
          const seen = new Set<string>();
          return days.flatMap((d) => {
          const daySpans = spans.filter((x) => d >= (x.e.from! < monday ? monday : x.e.from!) && d <= addDays((x.e.from! < monday ? monday : x.e.from!), x.clip.len - 1));
          const list: CalEntry[] = [...daySpans.map((x) => x.e), ...(timedByDay.get(d) ?? [])];
          return list
            .filter((e) => {
              const k = e.startsAt ? `${e.id}@${d}` : e.id;
              if (seen.has(k)) return false;
              seen.add(k);
              return true;
            })
            .map((e) => (
              <li key={e.startsAt ? `${e.id}@${d}` : e.id}>
                <p class="cal-agenda-day">
                  {e.startsAt ? `${weekdayShort(d)} ` : ''}
                  {e.agendaWhen}
                </p>
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
            ));
          });
        })()}
      </ul>
    </section>
  );
}
