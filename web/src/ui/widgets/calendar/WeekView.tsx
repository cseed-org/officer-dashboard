import type { CalendarFeedItem, CalendarFeedSource } from '../../../lib/contracts';
import { dayNumber, weekdayIndex, weekdayShort, weekDays, shortDay } from '../../../lib/calendar';
import { useBox } from '../shell/fit';
import { CalendarChip } from './CalendarChip';

/** Chip heights (calendar.css): two-line chip 30px (2×1); 2×2 chips wrap the title to two lines, 44px. */
const CHIP = { short: 30, tall: 44 };
const DAY_HEAD = 18;
const PAD = 8;
const GAP = 3;

/** This week, Mon–Sun: as many chips a day as the cell has room for, then "+n" in the day header. */
export function WeekView({ monday, today, tall, itemsOn, sources, label }: {
  monday: string;
  today: string;
  tall: boolean;
  itemsOn: (date: string) => CalendarFeedItem[];
  sources: CalendarFeedSource[];
  label: string;
}) {
  const [ref, box] = useBox();
  const chip = tall ? CHIP.tall : CHIP.short;
  const fit = box.height ? Math.max(1, Math.floor((box.height - PAD - DAY_HEAD + GAP) / (chip + GAP))) : tall ? 4 : 2;
  let index = 0;
  return (
    <div class={`hcal${tall ? ' hcal--tall' : ''}`} ref={ref} role="group" aria-label={label}>
      {weekDays(monday).map((date) => {
        const list = itemsOn(date);
        const shown = list.slice(0, fit);
        const more = list.length - shown.length;
        return <div key={date} class={`hcal-day${weekdayIndex(date) > 4 ? ' is-weekend' : ''}${date === today ? ' is-today' : ''}`}
          aria-label={`${weekdayShort(date)}, ${shortDay(date)}${date === today ? ', today' : ''}: ${list.length ? `${list.length} ${list.length === 1 ? 'item' : 'items'}` : 'nothing scheduled'}`} role="group">
          <span class="cal-dh" aria-hidden="true">{weekdayShort(date)} <b>{dayNumber(date)}</b>{more > 0 && <span class="hcal-more">+{more}</span>}</span>
          {shown.map((item) => <CalendarChip key={item.source + item.id} item={item} source={sources.find((s) => s.id === item.source)} index={index++} twoLine={!tall} />)}
        </div>;
      })}
    </div>
  );
}
