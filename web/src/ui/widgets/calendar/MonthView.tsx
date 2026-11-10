import type { CalendarFeedItem, CalendarFeedSource } from '../../../lib/contracts';
import { dayNumber, monthOf, shortDay, weekdayIndex, weekdayShort } from '../../../lib/calendar';
import { useBox } from '../shell/fit';
import { CalendarChip, safeHue } from './CalendarChip';

/** Month cell metrics (calendar.css): 14px date line, 15px chips with 2px gaps, 2+2px padding. */
const DOW = 16;
const GAP = 3;
const DATE = 14;
const CHIP = 15;

/** Six weeks starting Monday: real chips while they fit a cell, then "+n" (phones: dots). */
export function MonthView({ dates, month, today, itemsOn, sources, label }: {
  dates: string[];
  month: string;
  today: string;
  itemsOn: (date: string) => CalendarFeedItem[];
  sources: CalendarFeedSource[];
  label: string;
}) {
  const [ref, box] = useBox();
  const weeks = Math.ceil(dates.length / 7);
  const cell = box.height ? (box.height - DOW - GAP * weeks) / weeks : 40;
  const fit = Math.max(0, Math.floor((cell - 4 - DATE + 2) / (CHIP + 2)));
  let index = 0;
  return (
    <div class="calm" ref={ref} role="group" aria-label={label} style={{ '--weeks': weeks }}>
      {['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'].map((d) => <span class="calm-dow" key={d} aria-hidden="true">{d}</span>)}
      {dates.map((date) => {
        const list = itemsOn(date);
        // "+n" sits in the date line, so every chip slot shows a real item.
        const shown = list.slice(0, fit);
        const more = list.length - shown.length;
        return <div key={date} role="group"
          class={`calm-day${weekdayIndex(date) > 4 ? ' is-weekend' : ''}${monthOf(date) !== month ? ' is-outside' : ''}${date === today ? ' is-today' : ''}`}
          aria-label={`${weekdayShort(date)}, ${shortDay(date)}${date === today ? ', today' : ''}: ${list.length ? `${list.length} ${list.length === 1 ? 'item' : 'items'}` : 'nothing scheduled'}`}>
          <span class="calm-dh" aria-hidden="true"><b>{dayNumber(date)}</b>
            {list.map((item) => <i key={item.source + item.id} class="cal-dot" data-hue={safeHue(sources.find((s) => s.id === item.source)?.hue)} />)}
            {more > 0 && <span class="hcal-more">+{more}</span>}
          </span>
          {shown.map((item) => <CalendarChip key={item.source + item.id} item={item} source={sources.find((s) => s.id === item.source)} index={index++} />)}
        </div>;
      })}
    </div>
  );
}
