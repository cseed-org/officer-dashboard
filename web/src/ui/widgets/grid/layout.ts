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

/** Coordinates are a local preview, never part of the persisted {id,size} contract. */
export type GridCell = Placement & { x: number; y: number; w: number; h: number };

/** Existing desktop blocks are 226–266px at 1280–1440px viewport widths.
 * Pick the nearest count to 240px plus the 16px gap; CSS caps the container at
 * ten such blocks. Two-column phones and the existing four-column minimum remain.
 */
export function gridColumns(width: number): number {
  return width < 600 ? 2 : Math.max(4, Math.min(10, Math.round((width + 16) / 256)));
}

export function footprint(size: BlockSize, width: number) {
  let { w, h } = blocks(size);
  const cols = gridColumns(width);
  if (cols === 2) { if (w === 4 || h === 2) w = 2; }
  else if (width < 960) {
    if (w === 2 && h === 2) w = 4;
    else if (w === 1 && h === 2) { w = 2; h = 1; }
  }
  return { w, h, cols };
}
export type Pin = { id: string; x: number; y: number };

/**
 * Dense packing, the one rule every Home layout follows (view, edit, reload). Scan cells top to
 * bottom, left to right; each empty cell takes the next widget, in `layout` order, whose footprint
 * fits there. Later widgets may overtake earlier ones to fill a gap; a gap stays only where no
 * remaining widget fits. Deterministic: the same order and width always pack the same way.
 * `pin` holds one widget at a chosen top-left (the one being dragged or resized) and packs the
 * others around it. Returns cells in `layout` order.
 */
export function pack(layout: Placement[], width: number, pin?: Pin): GridCell[] {
  const { cols } = footprint('1x1', width);
  const taken: Uint8Array[] = [];
  const row = (y: number) => (taken[y] ??= new Uint8Array(cols));
  const free = (x: number, y: number, w: number, h: number) => {
    if (x + w > cols) return false;
    for (let dy = 0; dy < h; dy++) for (let dx = 0; dx < w; dx++) if (row(y + dy)[x + dx]) return false;
    return true;
  };
  const out = new Map<string, GridCell>();
  const put = (c: GridCell) => {
    for (let dy = 0; dy < c.h; dy++) row(c.y + dy).fill(1, c.x, c.x + c.w);
    out.set(c.id, c);
  };
  const pinned = pin && layout.find(p => p.id === pin.id);
  if (pinned) {
    const { w, h } = footprint(pinned.size, width);
    put({ ...pinned, w, h, x: Math.max(0, Math.min(pin.x, cols - w)), y: Math.max(0, pin.y) });
  }
  const remaining = layout.filter(p => !out.has(p.id)).map(p => ({ ...p, ...footprint(p.size, width) }));
  for (let at = 0; remaining.length; at++) {
    const x = at % cols, y = Math.floor(at / cols);
    if (row(y)[x]) continue;
    const i = remaining.findIndex(r => free(x, y, r.w, r.h));
    if (i < 0) continue;
    const [{ id, size, w, h }] = remaining.splice(i, 1) as [typeof remaining[number]];
    put({ id, size, w, h, x, y });
  }
  return layout.map(p => out.get(p.id)!);
}

/** Cells in reading order (top to bottom, left to right) as a saved {id,size} order. */
export const readingOrder = (cells: GridCell[]): Placement[] =>
  [...cells].sort((a, b) => a.y - b.y || a.x - b.x).map(({ id, size }) => ({ id, size }));

/**
 * The order to keep after an edit: pack (holding `pin` where the person put it), then take the
 * reading order. Packing that order unpinned gives the same cells wherever the pinned widget
 * could stay, and otherwise lets it rise into the gap it fits, so what Home shows during a drag
 * is what the drop, a save and a reload give. The persisted format stays {id,size}[].
 */
export const settle = (layout: Placement[], width: number, pin?: Pin): Placement[] =>
  readingOrder(pack(layout, width, pin));

/**
 * Keyboard move: `id` to index `to` of the reading order. Dense packing can pack a swap back into
 * the same picture (a 1×1 that fills a gap fills it from any later index), so keep stepping the
 * same way until the picture changes. Returns `layout` itself when nothing can move.
 */
export function moveInOrder(layout: Placement[], id: string, to: number, width: number): Placement[] {
  const from = layout.findIndex(p => p.id === id);
  const step = Math.sign(to - from);
  if (from < 0 || !step) return layout;
  const before = pack(layout, width);
  for (let i = to; i >= 0 && i < layout.length; i += step) {
    const next = settle(moveTo(layout, id, i), width);
    const cells = pack(next, width);
    if (cells.some(c => { const b = before.find(o => o.id === c.id)!; return b.x !== c.x || b.y !== c.y; })) return next;
  }
  return layout;
}

/**
 * Where the edit-mode Add tile goes: the cell right after the last occupied cell in reading
 * order, so it always follows the last widget and never fills a gap above one. It is placed
 * explicitly because auto-placed items (even without `dense`) start from the top and would
 * take the first hole the explicitly placed widgets leave.
 */
export function addSlot(cells: GridCell[], width: number): { x: number; y: number } {
  const { cols } = footprint('1x1', width);
  // Only tiles that reach the bottom row cover it; the Add tile goes just past the furthest one.
  const bottom = cells.reduce((m, c) => Math.max(m, c.y + c.h), 0);
  const end = cells.filter(c => c.y + c.h === bottom).reduce((m, c) => Math.max(m, c.x + c.w), 0);
  const next = bottom === 0 ? 0 : end < cols ? (bottom - 1) * cols + end : bottom * cols;
  return { x: next % cols, y: Math.floor(next / cols) };
}

/**
 * Which track an offset (px from the grid's content edge) falls in, given the grid's resolved
 * track sizes and gap. Each gap is split between its two neighbours. `extra` tracks past the last
 * one (sized like it) also count, so the row just below the grid is a target. Null outside.
 */
export function trackAt(sizes: number[], gap: number, offset: number, extra = 0): number | null {
  if (offset < -gap / 2 || !sizes.length) return null;
  let start = 0;
  for (let i = 0; i < sizes.length + extra; i++) {
    const size = sizes[i] ?? sizes[sizes.length - 1]!;
    if (offset < start + size + gap / 2) return i;
    start += size + gap;
  }
  return null;
}

/** Whether `size`'s footprint fits with its top-left at `at`: inside the columns, never above row 0. */
export function fitsAt(size: BlockSize, at: { x: number; y: number }, width: number) {
  const { w, cols } = footprint(size, width);
  return at.x >= 0 && at.y >= 0 && at.x + w <= cols;
}
