/**
 * Small hand-rolled SVG charts (no chart library). Colours are chart tokens only:
 * sky (--chart-2) for a single series, sky + sage (--chart-3) for spent / committed,
 * validated for CVD separation on paper and on #111. Text always uses text tokens; every
 * chart sits next to a table with the same numbers.
 */
import type { TargetedPointerEvent } from 'preact';
import { useMemo, useRef, useState } from 'preact/hooks';
import { dollars, fmtShort, formatCents, pct } from '../format';

/** Horizontal bars for the event funnel: registered → checked in → reflected. */
export function FunnelChart({ steps, label }: { steps: { label: string; value: number; rate?: number | null; note?: string }[]; label: string }) {
  const max = Math.max(1, ...steps.map((s) => s.value));
  return (
    <figure class="chart funnel" aria-label={label}>
      <ol class="funnel-list">
        {steps.map((s) => (
          <li class="funnel-row" key={s.label}>
            <span class="funnel-label">{s.label}</span>
            <span class="funnel-track" aria-hidden="true">
              <span class="funnel-bar" style={{ width: `${(s.value / max) * 100}%` }} title={`${s.label}: ${s.value}`} />
            </span>
            <span class="funnel-value num">
              {s.value}
              {s.rate !== undefined && <span class="funnel-rate"> · {pct(s.rate)}</span>}
            </span>
            {s.note && <span class="funnel-note">{s.note}</span>}
          </li>
        ))}
      </ol>
    </figure>
  );
}

/** Cumulative line with a crosshair tooltip. Single series: no legend, the title names it. */
export function LineChart({
  points,
  label,
  valueLabel,
}: {
  points: { date: string; value: number }[];
  label: string;
  valueLabel: string;
}) {
  const W = 640;
  const H = 220;
  const pad = { l: 40, r: 16, t: 14, b: 28 };
  const [hover, setHover] = useState<number | null>(null);
  const svgRef = useRef<SVGSVGElement>(null);
  const max = Math.max(1, ...points.map((p) => p.value));
  const niceMax = useMemo(() => {
    const step = max <= 10 ? 2 : max <= 50 ? 10 : max <= 100 ? 20 : max <= 250 ? 50 : 100;
    return Math.ceil(max / step) * step;
  }, [max]);
  const x = (i: number) => pad.l + (points.length < 2 ? 0 : (i / (points.length - 1)) * (W - pad.l - pad.r));
  const y = (v: number) => pad.t + (1 - v / niceMax) * (H - pad.t - pad.b);
  const ticks = [0, niceMax / 2, niceMax];
  const d = points.map((p, i) => `${i ? 'L' : 'M'}${x(i).toFixed(1)},${y(p.value).toFixed(1)}`).join('');
  const area = points.length ? `${d}L${x(points.length - 1)},${y(0)}L${x(0)},${y(0)}Z` : '';
  const xTicks = points.length ? [0, Math.floor((points.length - 1) / 2), points.length - 1] : [];

  const onMove = (e: TargetedPointerEvent<SVGSVGElement>) => {
    const box = svgRef.current!.getBoundingClientRect();
    const px = ((e.clientX - box.left) / box.width) * W;
    const i = Math.round(((px - pad.l) / (W - pad.l - pad.r)) * (points.length - 1));
    setHover(Math.max(0, Math.min(points.length - 1, i)));
  };
  const last = points[points.length - 1];
  const h = hover !== null ? points[hover] : null;

  return (
    <figure class="chart line-chart">
      <svg
        ref={svgRef}
        viewBox={`0 0 ${W} ${H}`}
        role="img"
        aria-label={`${label}. ${last ? `${last.value} ${valueLabel} by ${fmtShort(last.date)}.` : 'No data.'}`}
        onPointerMove={onMove}
        onPointerLeave={() => setHover(null)}
      >
        {ticks.map((t) => (
          <g key={t}>
            <line x1={pad.l} x2={W - pad.r} y1={y(t)} y2={y(t)} class="chart-grid" />
            <text x={pad.l - 8} y={y(t)} class="chart-axis" text-anchor="end" dominant-baseline="middle">
              {t}
            </text>
          </g>
        ))}
        {xTicks.map((i) => (
          <text key={i} x={x(i)} y={H - 8} class="chart-axis" text-anchor={i === 0 ? 'start' : i === points.length - 1 ? 'end' : 'middle'}>
            {fmtShort(points[i]!.date)}
          </text>
        ))}
        <path d={area} class="chart-area" />
        <path d={d} class="chart-line" />
        {last && <circle cx={x(points.length - 1)} cy={y(last.value)} r={4.5} class="chart-dot" />}
        {last && hover === null && (
          <text x={x(points.length - 1) - 10} y={y(last.value) - 10} class="chart-label" text-anchor="end">
            {last.value}
          </text>
        )}
        {h && hover !== null && (
          <g class="chart-hover">
            <line x1={x(hover)} x2={x(hover)} y1={pad.t} y2={H - pad.b} class="chart-crosshair" />
            <circle cx={x(hover)} cy={y(h.value)} r={5} class="chart-dot" />
          </g>
        )}
      </svg>
      {h && (
        <div class="chart-tooltip" style={{ left: `${(x(hover!) / W) * 100}%` }} role="status">
          <span class="num">{h.value}</span> {valueLabel} · {fmtShort(h.date)}
        </div>
      )}
    </figure>
  );
}

/** Columns for small daily counts (reflections after the event). */
export function ColumnChart({ points, label, valueLabel }: { points: { date: string; value: number }[]; label: string; valueLabel: string }) {
  const W = 640;
  const H = 160;
  const pad = { l: 8, r: 8, t: 18, b: 26 };
  const max = Math.max(1, ...points.map((p) => p.value));
  const slot = (W - pad.l - pad.r) / Math.max(1, points.length);
  const bw = Math.min(24, slot * 0.6);
  const y = (v: number) => pad.t + (1 - v / max) * (H - pad.t - pad.b);
  const peak = points.reduce((m, p, i) => (p.value > (points[m]?.value ?? -1) ? i : m), 0);
  return (
    <figure class="chart">
      <svg viewBox={`0 0 ${W} ${H}`} role="img" aria-label={`${label}: ${points.map((p) => `${fmtShort(p.date)} ${p.value}`).join(', ')}`}>
        <line x1={pad.l} x2={W - pad.r} y1={y(0)} y2={y(0)} class="chart-grid" />
        {points.map((p, i) => {
          const cx = pad.l + slot * i + slot / 2;
          const top = y(p.value);
          const hgt = y(0) - top;
          return (
            <g key={p.date}>
              <path
                class="chart-col"
                d={hgt > 0 ? `M${cx - bw / 2},${y(0)}V${top + Math.min(4, hgt)}q0,-4 4,-4h${bw - 8}q4,0 4,4V${y(0)}Z` : ''}
              >
                <title>{`${fmtShort(p.date)}: ${p.value} ${valueLabel}`}</title>
              </path>
              {i === peak && p.value > 0 && (
                <text x={cx} y={top - 6} class="chart-label" text-anchor="middle">
                  {p.value}
                </text>
              )}
              {(i === 0 || i === points.length - 1 || points.length <= 8) && (
                <text x={cx} y={H - 8} class="chart-axis" text-anchor="middle">
                  {fmtShort(p.date)}
                </text>
              )}
            </g>
          );
        })}
      </svg>
    </figure>
  );
}

/**
 * Budget bars: one row per category, spent (sky) + committed (sage) stacked inside a
 * budget track. Over budget is called out in text with a marker, not by colour alone.
 */
export function BudgetBars({
  rows,
  label,
}: {
  rows: { key: string; label: string; budgetCents: number; spentCents: number; committedCents: number }[];
  label: string;
}) {
  const max = Math.max(1, ...rows.map((r) => Math.max(r.budgetCents, r.spentCents + r.committedCents)));
  return (
    <figure class="chart budget-bars" aria-label={label}>
      <div class="legend" aria-hidden="true">
        <span class="legend-item">
          <span class="swatch swatch--spent" />
          spent
        </span>
        <span class="legend-item">
          <span class="swatch swatch--committed" />
          committed
        </span>
        <span class="legend-item">
          <span class="swatch swatch--budget" />
          budget
        </span>
      </div>
      <ul class="budget-list">
        {rows.map((r) => {
          const used = r.spentCents + r.committedCents;
          const over = used > r.budgetCents;
          const w = (c: number) => `${(c / max) * 100}%`;
          return (
            <li class="budget-row" key={r.key}>
              <span class="budget-label">{r.label}</span>
              <span class="budget-track" style={{ width: w(r.budgetCents) }} title={`${r.label}: ${formatCents(r.spentCents)} spent, ${formatCents(r.committedCents)} committed of ${formatCents(r.budgetCents)}`}>
                <span class="budget-spent" style={{ width: `${(r.spentCents / Math.max(r.budgetCents, used, 1)) * 100}%` }} />
                <span class="budget-committed" style={{ width: `${(r.committedCents / Math.max(r.budgetCents, used, 1)) * 100}%` }} />
              </span>
              <span class={`budget-value num${over ? ' is-over' : ''}`}>
                {over && <span aria-hidden="true">▲ </span>}
                {dollars(used)} <span class="budget-of">/ {dollars(r.budgetCents)}</span>
                {over && <span class="visually-hidden"> (over budget)</span>}
              </span>
            </li>
          );
        })}
      </ul>
    </figure>
  );
}
