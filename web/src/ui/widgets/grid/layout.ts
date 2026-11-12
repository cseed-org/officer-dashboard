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
const overlaps = (a: GridCell, b: GridCell) =>
  a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h;

/**
 * The free spot closest to the top that fits `item`: rows top to bottom, then left to right.
 * Below the last row every spot is free, so this always finds one. Nothing in `placed` moves.
 */
export function firstFit(placed: GridCell[], item: Placement, width: number, from = 0): GridCell {
  const { w, h, cols } = footprint(item.size, width);
  let at = from;
  let cell: GridCell;
  do {
    cell = { ...item, w, h, x: at % cols, y: Math.floor(at / cols) };
    at++;
  } while (cell.x + w > cols || placed.some(p => overlaps(p, cell)));
  return cell;
}

/** CSS's initial dense packing; ordered packing is used after an explicit move. */
export function packLayout(layout: Placement[], width: number, dense = true): GridCell[] {
  const placed: GridCell[] = [];
  let cursor = 0;
  for (const item of layout) {
    const cell = firstFit(placed, item, width, dense ? 0 : cursor);
    placed.push(cell);
    cursor = cell.y * footprint(item.size, width).cols + cell.x + 1;
  }
  return placed;
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

/** Reserve the changed tile, then push intersecting neighbours down in visual order. */
export function placePushDown(layout: Placement[], previous: GridCell[], changedId: string, width: number): GridCell[] {
  const fallback = packLayout(layout, width);
  const cells = layout.map(item => {
    const old = previous.find(p => p.id === item.id) ?? fallback.find(p => p.id === item.id)!;
    const { w, h, cols } = footprint(item.size, width);
    return { ...item, w, h, x: Math.min(old.x, cols - w), y: old.y };
  });
  const ordered = [...cells].sort((a, b) =>
    a.id === changedId ? -1 : b.id === changedId ? 1 : a.y - b.y || a.x - b.x);
  const placed: GridCell[] = [];
  for (const cell of ordered) {
    const next = { ...cell };
    while (placed.some(p => overlaps(p, next))) next.y++;
    placed.push(next);
  }
  return cells.map(cell => placed.find(p => p.id === cell.id)!);
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

/**
 * Put `id` with its top-left at `at`, push intersecting neighbours down from `previous`, and
 * return the layout in reading order with the matching cells (so the order and the explicit
 * positions agree, and the Add tile still follows the last widget).
 */
export function moveToCell(layout: Placement[], previous: GridCell[], id: string, at: { x: number; y: number }, width: number) {
  const base = previous.map(c => (c.id === id ? { ...c, x: at.x, y: at.y } : c));
  const cells = placePushDown(layout, base, id, width).sort((a, b) => a.y - b.y || a.x - b.x);
  return { layout: cells.map(({ id, size }) => ({ id, size })), cells };
}
