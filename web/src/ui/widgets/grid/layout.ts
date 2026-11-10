import type { WidgetPlacement } from '../../../lib/contracts';
import { KPI_GROUP_ID, kpiIds, widgetDef, type BlockSize } from '../registry';

/** A placement as Home renders it: always a V0.4 block size. */
export type Placement = { id: string; size: BlockSize };

export const blocks = (size: BlockSize) => {
  const [w, h] = size.split('x').map(Number) as [number, number];
  return { w, h, area: w * h };
};
/** "2x2" → "2×2" for display; "2 by 2 blocks" for screen readers. */
export const sizeLabel = (size: BlockSize) => size.replace('x', '×');
export const sizeName = (size: BlockSize) => `${size.replace('x', ' by ')} blocks`;

/** The allowed size closest to `want` by area (ties go to the smaller). */
export function nearestSize(sizes: BlockSize[], want: BlockSize): BlockSize {
  if (sizes.includes(want)) return want;
  const a = blocks(want).area;
  return [...sizes].sort((x, y) => Math.abs(blocks(x).area - a) - Math.abs(blocks(y).area - a) || blocks(x).area - blocks(y).area)[0] ?? want;
}

/**
 * Migrate a saved layout to V0.4 block sizes, in place and in order. V0.3 layouts used S/M/L and one
 * "kpis" widget: kpis becomes the five 1×1 stat tiles at the same position, S the smallest allowed
 * size, M 2×2 (or nearest), L 4×2 (or largest). Sizes a widget does not support snap to the nearest
 * one. Unknown IDs keep their size (or 1×1) so another feature's widget survives until it ships.
 * Returns `changed` so callers can tell a migrated layout from a saved V0.4 one.
 */
export function migrateLayout(saved: WidgetPlacement[]): { layout: Placement[]; changed: boolean } {
  const out: Placement[] = [];
  const seen = new Set<string>();
  let changed = false;
  const push = (p: Placement) => {
    if (seen.has(p.id)) return void (changed = true);
    seen.add(p.id);
    out.push(p);
  };
  for (const p of saved) {
    if (p.id === KPI_GROUP_ID) {
      changed = true;
      for (const id of kpiIds) push({ id, size: '1x1' });
      continue;
    }
    const def = widgetDef(p.id);
    let size: BlockSize;
    if (p.size === 'S' || p.size === 'M' || p.size === 'L') {
      changed = true;
      if (!def) size = p.size === 'S' ? '1x1' : p.size === 'M' ? '2x2' : '4x2';
      else if (p.size === 'S') size = def.sizes[0]!;
      else if (p.size === 'L') size = def.sizes.includes('4x2') ? '4x2' : def.sizes[def.sizes.length - 1]!;
      else size = nearestSize(def.sizes, '2x2');
    } else {
      size = def ? nearestSize(def.sizes, p.size) : p.size;
      if (size !== p.size) changed = true;
    }
    push({ id: p.id, size });
  }
  return { layout: out, changed };
}

/** Move one widget to index `to` (clamped). Returns the same array when nothing moves. */
export function moveTo(layout: Placement[], id: string, to: number): Placement[] {
  const from = layout.findIndex((w) => w.id === id);
  const dest = Math.max(0, Math.min(to, layout.length - 1));
  if (from < 0 || from === dest) return layout;
  const next = [...layout];
  const [item] = next.splice(from, 1);
  next.splice(dest, 0, item!);
  return next;
}
