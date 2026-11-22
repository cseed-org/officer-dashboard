import { useEffect, useState } from 'preact/hooks';
import { addMonths, dayNumber, longDay, monthGrid, monthLabel, monthOf, pacificToday } from '../../../lib/calendar';

const DOW = ['M', 'T', 'W', 'T', 'F', 'S', 'S'];

/**
 * The rail's small month: browse with its own ‹ ›, pick a day to move the main view there.
 * Days inside the main view's period are tinted; today has the ink disc.
 */
export function MiniMonth({ month, inView, onPick }: { month: string; inView: (date: string) => boolean; onPick: (date: string) => void }) {
  const [shown, setShown] = useState(month);
  useEffect(() => setShown(month), [month]);
  const today = pacificToday();
  return (
    <section class="orgcal-mini" aria-label="Pick a date">
      <div class="orgcal-mini-head">
        <span class="orgcal-mini-title">{monthLabel(shown)}</span>
        <span class="orgcal-mini-nav">
          <button type="button" class="icon-btn icon-btn--small" aria-label="Previous month (date picker)" onClick={() => setShown(addMonths(shown, -1))}>
            ‹
          </button>
          <button type="button" class="icon-btn icon-btn--small" aria-label="Next month (date picker)" onClick={() => setShown(addMonths(shown, 1))}>
            ›
          </button>
        </span>
      </div>
      <div class="orgcal-mini-grid">
        {DOW.map((d, i) => (
          <span class="orgcal-mini-dow" key={i} aria-hidden="true">
            {d}
          </span>
        ))}
        {monthGrid(shown).map(({ date, outside }) => (
          <button
            type="button"
            key={date}
            class={`orgcal-mini-day${outside ? ' is-outside' : ''}${date === today ? ' is-today' : ''}${inView(date) ? ' is-in-view' : ''}`}
            aria-label={longDay(date)}
            aria-current={date === today ? 'date' : undefined}
            data-date={date}
            onClick={() => {
              if (monthOf(date) !== shown) setShown(monthOf(date));
              onPick(date);
            }}
          >
            {dayNumber(date)}
          </button>
        ))}
      </div>
    </section>
  );
}
