import { useId } from 'preact/hooks';
import type { Hue } from '../../../lib/colors';
import { CountUp } from '../../components/kit';

/**
 * Budget donut: one segment per category's used share of the whole budget, over a track. The
 * segments sit under an SVG mask whose circle sweeps in during the Home entrance (.donut-sweep).
 */
export function Donut({ segments, total: budget, size, label, center, sub }: {
  segments: { key: string; value: number; hue: Hue }[];
  /** Sum the segments are a share of (the total budget). */
  total: number;
  size: number;
  label: string;
  center: string;
  sub: string;
}) {
  const id = useId().replace(/[^a-zA-Z0-9_-]/g, '');
  const total = Math.max(1, budget, segments.reduce((a, s) => a + Math.max(0, s.value), 0));
  const r = 42;
  const sw = size > 140 ? 14 : 12;
  let at = 0;
  return (
    <div class="donut" style={{ width: `${size}px`, height: `${size}px` }} role="img" aria-label={label}>
      <svg viewBox="0 0 100 100" width={size} height={size} aria-hidden="true" focusable="false">
        <mask id={`donut-${id}`}><circle class="donut-sweep" cx="50" cy="50" r={r} fill="none" stroke="#fff" stroke-width={sw + 4} pathLength={100} stroke-dasharray="100 100" /></mask>
        <circle class="donut-track" cx="50" cy="50" r={r} stroke-width={sw} />
        <g mask={`url(#donut-${id})`}>
          {segments.filter((s) => s.value > 0).map((s) => {
            const p = (s.value / total) * 100;
            const len = Math.max(p - 0.6, 0.1);
            const el = <circle key={s.key} class="donut-seg" data-hue={s.hue} cx="50" cy="50" r={r} pathLength={100} stroke-width={sw} stroke-dasharray={`${len} ${100 - len}`} stroke-dashoffset={-at} />;
            at += p;
            return el;
          })}
        </g>
      </svg>
      <div class="donut-center" aria-hidden="true"><b><CountUp value={center} /></b><span>{sub}</span></div>
    </div>
  );
}
