import type { ComponentChildren } from 'preact';
import { CountUp } from './kit';

export interface StatTileData {
  label: string;
  value: string | number;
  sub?: ComponentChildren;
  /** Makes the tile a link (same-page links just update the query string). */
  href?: string;
  /** Shows a skeleton instead of the figure. */
  loading?: boolean;
  /** Draws the figure in the alert colour (overdue, waiting on me). */
  alert?: boolean;
  /** Marks a link tile whose filter is currently applied. */
  pressed?: boolean;
}

/** One summary figure, styled like the Home stat widgets. */
export function StatTile({ label, value, sub, href, loading, alert, pressed }: StatTileData) {
  const body = loading ? (
    <>
      <span class="stat-tile-label">{label}</span>
      <span class="skel skel--kpi" aria-hidden="true" />
      <span class="visually-hidden">Loading</span>
    </>
  ) : (
    <>
      <span class="stat-tile-label">{label}</span>
      <span class={`stat-tile-value${alert ? ' stat-tile-value--alert' : ''}`}>
        <CountUp value={value} />
      </span>
      {sub && <span class="stat-tile-sub">{sub}</span>}
    </>
  );
  const attrs = { 'data-stat': label.toLowerCase().replace(/\W+/g, '-'), 'aria-busy': loading || undefined };
  return href ? (
    <a class="stat-tile stat-tile--link" href={href} aria-current={pressed ? 'true' : undefined} data-pressed={pressed ? 'true' : undefined} {...attrs}>
      {body}
    </a>
  ) : (
    <div class="stat-tile" {...attrs}>
      {body}
    </div>
  );
}

/** A row of 3–4 summary tiles at the top of a page. Wraps to two columns on phones. */
export function StatRow({ tiles, label = 'Summary' }: { tiles: StatTileData[]; label?: string }) {
  return (
    <section class="stat-row" aria-label={label} data-stat-row>
      {tiles.map((t) => (
        <StatTile key={t.label} {...t} />
      ))}
    </section>
  );
}
