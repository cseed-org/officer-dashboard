import type { ComponentChildren } from 'preact';
import { CountUp } from './kit';
import { Icon, type IconName } from './Icon';
import '../widgets/shell/widget.css';

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
  /** Shows the value as plain text (no count-up), for titles that happen to contain digits. */
  plain?: boolean;
  /** Full text for a value that may be cut off. */
  title?: string;
  /** Chip icon, as on Home's cards (defaults to a neutral one). */
  icon?: IconName;
  /** Chip colour (defaults to blue). */
  hue?: 'blue' | 'amber' | 'teal' | 'coral' | 'violet' | 'green';
}

/** One summary figure, styled like the Home stat widgets. */
export function StatTile({ label, value, sub, href, loading, alert, pressed, plain, title, icon, hue }: StatTileData) {
  const head = (
    <header class="wc-head">
      <span class="wc-chip" aria-hidden="true"><Icon name={icon ?? 'blocks'} size={14} /></span>
      <h3 class="wc-title stat-tile-label">{label}</h3>
    </header>
  );
  const body = loading ? (
    <div class="stat-tile-body">
      <span class="skel skel--kpi" aria-hidden="true" />
      <span class="visually-hidden">Loading</span>
    </div>
  ) : (
    <div class="stat-tile-body">
      <span class={`stat-tile-value${alert ? ' stat-tile-value--alert' : ''}`} title={title}>
        {plain ? value : <CountUp value={value} />}
      </span>
      {sub && <span class="stat-tile-sub">{sub}</span>}
    </div>
  );
  const attrs = { 'data-hue': hue ?? 'blue', 'data-stat': label.toLowerCase().replace(/\W+/g, '-'), 'aria-busy': loading || undefined };
  return href ? (
    <a class="wc stat-tile stat-tile--link" href={href} aria-current={pressed ? 'true' : undefined} data-pressed={pressed ? 'true' : undefined} {...attrs}>
      {head}
      {body}
    </a>
  ) : (
    <div class="wc wc--nolink stat-tile" {...attrs}>
      {head}
      {body}
    </div>
  );
}

/** A row of 3–4 summary tiles at the top of a page. Wraps to two columns on phones. */
export function StatRow({ tiles, label = 'Summary' }: { tiles: StatTileData[]; label?: string }) {
  return (
    <section class="stat-tiles" aria-label={label} data-stat-row>
      {tiles.map((t) => (
        <StatTile key={t.label} {...t} />
      ))}
    </section>
  );
}
