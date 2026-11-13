/**
 * The intro's arcade snake (V0.10). Pure functions, no DOM: `buildPlan` lays a grid over the
 * viewport and plans, tick by tick, a classic snake. It moves one cell per tick with 90° turns
 * and grows by keeping its tail still on the tick it eats an apple. It crosses the greeting,
 * leaves the right edge, wraps to a lane above the word, drops into the "c" and eats the
 * apples laid along the word. When it stops, its body is exactly `WORD_PATH`, which spells
 * "cseed" on the grid.
 */

export interface Cell {
  x: number;
  y: number;
}
/** What a route cell is: the greeting pass, the way round to the word, a letter of the word, or a join between letters. */
export type CellKind = 'greet' | 'approach' | 'letter' | 'join';
export interface RouteCell extends Cell {
  kind: CellKind;
}

/** 3-wide glyphs; x-height letters are 5 rows, the "d" 7 (its stem rises two rows above the others). */
export const GLYPHS: Record<string, string[]> = {
  c: ['###', '#..', '#..', '#..', '###'],
  s: ['###', '#..', '###', '..#', '###'],
  e: ['###', '#.#', '###', '#..', '###'],
  d: ['..#', '..#', '###', '#.#', '#.#', '#.#', '###'],
};
export const WORD = 'cseed';
/** Letters are 3 cells wide with 2 empty columns between them. */
export const LETTER_PITCH = 5;
export const WORD_COLS = 23;
export const WORD_ROWS = 7;

/** The cells of each letter of "cseed", in word coordinates (row 0 = the top of the d). */
export function letterCells(): Set<string> {
  const out = new Set<string>();
  WORD.split('').forEach((ch, i) => {
    const g = GLYPHS[ch]!;
    const top = WORD_ROWS - g.length;
    g.forEach((row, r) => [...row].forEach((v, c) => v === '#' && out.add(`${i * LETTER_PITCH + c},${top + r}`)));
  });
  return out;
}

/**
 * The snake's final body, tail first: the shortest self-avoiding grid route that covers every
 * letter cell of "cseed". The e's and the d have T-junctions, so no single stroke can trace them
 * without help: 15 extra "join" cells link the letters (58 letter cells, 77 in all). The joins
 * are drawn dimmed at the end, like the blacked-out tube between letters of a neon sign.
 */
export const WORD_PATH: readonly Cell[] = (
  [
    [2, 2], [1, 2], [0, 2], [0, 3], [0, 4], [0, 5], [0, 6], [1, 6], [2, 6], [3, 6], [4, 6], [5, 6], [6, 6], [7, 6], [7, 5], [7, 4], [6, 4], [5, 4], [5, 3], [5, 2],
    [6, 2], [7, 2], [8, 2], [9, 2], [9, 3], [10, 3], [10, 2], [11, 2], [12, 2], [12, 3], [12, 4], [11, 4], [10, 4], [10, 5], [10, 6], [11, 6], [12, 6], [13, 6], [14, 6], [14, 5],
    [14, 4], [14, 3], [15, 3], [15, 2], [16, 2], [17, 2], [17, 3], [17, 4], [16, 4], [15, 4], [15, 5], [15, 6], [16, 6], [17, 6], [18, 6], [19, 6], [19, 5], [19, 4], [19, 3], [19, 2],
    [19, 1], [20, 1], [21, 1], [21, 2], [20, 2], [20, 3], [20, 4], [20, 5], [20, 6], [21, 6], [22, 6], [22, 5], [22, 4], [22, 3], [22, 2], [22, 1], [22, 0],
  ] as const
).map(([x, y]) => ({ x, y }));

/** Length of the snake when it first appears; it grows by one per apple from there. */
export const START_LENGTH = 6;

export interface PlanInput {
  vw: number;
  vh: number;
  /** Where the greeting sits, so its strips vanish as the head passes. */
  text: { left: number; right: number; top: number; bottom: number };
  /** The page logo's box; the spelled word shrinks into it. */
  target: { x: number; y: number; w: number; h: number };
}

export interface Plan {
  /** Cell size (px) and the grid: `cols × rows` cells, the first one's corner at (ox, oy). */
  C: number;
  cols: number;
  rows: number;
  ox: number;
  oy: number;
  /** Grid cell of the word's top-left corner. */
  gx0: number;
  gy0: number;
  /** Every cell the head visits, in order. */
  route: RouteCell[];
  /** Route index the head reaches at tick 0 / when it wraps (the pause while the apples pop in) / when it enters the word. */
  wrapAt: number;
  wordFrom: number;
  /** Route indices that hold an apple (each one: the tail stays put on that tick). */
  apples: number[];
  /** Per tick: the head's route index and the body length. Tick 0 is the snake's first frame. */
  ticks: { head: number; len: number; ate: boolean }[];
  /** ms at which each tick starts. */
  at: number[];
  /** ms: the snake is complete and spells the word. */
  spelledAt: number;
  /** ms: the word starts shrinking into the logo, and when it is there. */
  morphAt: number;
  morphEnd: number;
  /** Total ms until the hand-off can start. */
  duration: number;
  /** The word's box (px) and the transform that maps it onto the logo box. */
  word: { x: number; y: number; w: number; h: number };
  morph: { tx: number; ty: number; s: number };
  /** Vertical strips of the greeting with the ms at which the head reaches each. */
  slices: { x0: number; x1: number; eatAt: number }[];
  /** The body (route indices, head first) at tick `t`. */
  body(t: number): number[];
  /** Pixel corner of a grid cell. */
  px(c: Cell): { x: number; y: number };
}

const clamp = (v: number, a: number, b: number) => Math.min(b, Math.max(a, v));

/** ms per tick while crossing the greeting, and while writing the word (slower, so the growth reads). */
export const STEP_FAST = 22;
export const STEP_WORD = 34;
/** ms the snake waits off-screen while the apples pop in; then the hold on the spelled word; then the shrink. */
export const WRAP_HOLD = 420;
export const SPELL_HOLD = 1000;
export const MORPH_MS = 640;

/** Cell size: the 23-cell word takes about 58% of a wide screen and the full width (less a 16px gutter each side) on a phone. */
export function cellSize(vw: number): number {
  return clamp(Math.floor((vw < 600 ? vw - 32 : vw * 0.58) / WORD_COLS), 12, 38);
}

export function buildPlan(input: PlanInput): Plan {
  const { vw, vh, text, target } = input;
  const C = cellSize(vw);
  const cols = Math.ceil(vw / C) + 1;
  const rows = Math.ceil(vh / C) + 1;
  const ox = (vw - cols * C) / 2;
  const oy = (vh - rows * C) / 2;
  const gx0 = Math.floor((cols - WORD_COLS) / 2);
  // Keep a lane of 3 rows above the word for the approach, even on a short screen.
  const gy0 = Math.max(4, Math.floor((rows - WORD_ROWS) / 2));
  const L0 = START_LENGTH;
  const letters = letterCells();

  /* ── Route ── */
  const route: RouteCell[] = [];
  // 1. In from the left on the greeting's row, out past the right edge until the whole body is gone.
  const mid = gy0 + 3;
  for (let x = -1; x <= cols + L0; x++) route.push({ x, y: mid, kind: 'greet' });
  // 2. Wrap (arcade style) to a lane three rows above the word, run right to the c's column, drop down.
  const wrapAt = route.length;
  const lane = gy0 - 3;
  for (let x = -L0; x <= gx0 + 2; x++) route.push({ x, y: lane, kind: 'approach' });
  for (let y = lane + 1; y < gy0 + 2; y++) route.push({ x: gx0 + 2, y, kind: 'approach' });
  // 3. The word, tail cell first.
  const wordFrom = route.length;
  for (const c of WORD_PATH) route.push({ x: gx0 + c.x, y: gy0 + c.y, kind: letters.has(`${c.x},${c.y}`) ? 'letter' : 'join' });

  // Apples on every word cell after the first L0: arriving with length L0 and eating on every
  // tick from there leaves the tail on the word's first cell, so the final body is the word.
  const apples: number[] = [];
  for (let i = wordFrom + L0; i < route.length; i++) apples.push(i);
  const appleSet = new Set(apples);

  /* ── Ticks ── */
  const ticks: Plan['ticks'] = [{ head: 0, len: L0, ate: false }];
  const at: number[] = [0];
  let t = 0;
  for (let head = 1; head < route.length; head++) {
    const prev = ticks[ticks.length - 1]!;
    const ate = appleSet.has(head);
    t += head <= wordFrom - 3 ? STEP_FAST : STEP_WORD;
    if (head === wrapAt) t += WRAP_HOLD;
    ticks.push({ head, len: prev.len + (ate ? 1 : 0), ate });
    at.push(t);
  }
  const spelledAt = t;
  const morphAt = spelledAt + SPELL_HOLD;
  const morphEnd = morphAt + MORPH_MS;

  const px = (c: Cell) => ({ x: ox + c.x * C, y: oy + c.y * C });
  const w0 = px({ x: gx0, y: gy0 });
  const word = { x: w0.x, y: w0.y, w: WORD_COLS * C, h: WORD_ROWS * C };
  const s = Math.min(target.w / word.w, target.h / word.h);
  const morph = { s, tx: target.x + target.w / 2 - (word.x + word.w / 2) * s, ty: target.y + target.h / 2 - (word.y + word.h / 2) * s };

  // The greeting vanishes in vertical strips as the head's leading edge reaches each one.
  const SLICE = 24;
  const slices: Plan['slices'] = [];
  const sx0 = Math.floor(text.left - 8);
  const sx1 = Math.ceil(text.right + 8);
  for (let x = sx0; x < sx1; x += SLICE) {
    const col = Math.floor((x + SLICE / 2 - ox) / C); // the column whose cell covers the strip's middle
    const head = clamp(col + 1, 0, wrapAt - 1); // route index of that column (route starts at x = -1)
    slices.push({ x0: x, x1: Math.min(x + SLICE, sx1), eatAt: at[head]! });
  }

  const body = (tick: number) => {
    const { head, len } = ticks[clamp(tick, 0, ticks.length - 1)]!;
    const out: number[] = [];
    for (let i = head; i > head - len && i >= 0; i--) out.push(i);
    return out;
  };

  return {
    C,
    cols,
    rows,
    ox,
    oy,
    gx0,
    gy0,
    route,
    wrapAt,
    wordFrom,
    apples,
    ticks,
    at,
    spelledAt,
    morphAt,
    morphEnd,
    duration: morphEnd,
    word,
    morph,
    slices,
    body,
    px,
  };
}

/** Text picture of a set of word cells (`#` letter, `+` join, `.` empty), for tests and docs. */
export function picture(cells: readonly RouteCell[], gx0: number, gy0: number): string {
  const grid = Array.from({ length: WORD_ROWS }, () => Array<string>(WORD_COLS).fill('.'));
  for (const c of cells) {
    const x = c.x - gx0;
    const y = c.y - gy0;
    if (x >= 0 && x < WORD_COLS && y >= 0 && y < WORD_ROWS) grid[y]![x] = c.kind === 'join' ? '+' : '#';
  }
  return grid.map((r) => r.join('')).join('\n');
}
