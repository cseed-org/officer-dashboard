import { expect, test } from '@playwright/test';
import { nextPalette } from '../src/ui/onboarding/palette';
import { buildPlan, cellSize, letterCells, picture, START_LENGTH, WORD_COLS, WORD_PATH, type Plan } from '../src/ui/onboarding/snakePlan';

/** V0.10 hello screen: the arcade snake's planner and the palette (pure functions, no browser; run once). */
test.beforeEach(({}, info) => test.skip(info.project.name !== 'desktop', 'unit tests run once'));

const VIEWPORTS: [number, number][] = [[1440, 900], [1100, 800], [1099, 800], [900, 1180], [820, 1180], [390, 844], [360, 640]];
const plan = (vw: number, vh: number): Plan =>
  buildPlan({ vw, vh, text: { left: vw * 0.15, right: vw * 0.85, top: vh * 0.45, bottom: vh * 0.55 }, target: { x: 20, y: 26.75, w: 78, h: 18 } });

/** The word as text: `#` = a letter cell, `+` = a join (dimmed on screen). */
const CSEED = [
  '......................#',
  '...................+++#',
  '###..###++###..###.+###',
  '#....#...+#.#.+#.#.+#.#',
  '#....###..###.+###.+#.#',
  '#......#..#...+#...+#.#',
  '###++###..###++###++###',
].join('\n');

test('the word path is one self-avoiding grid route of 90° steps that covers every letter cell', () => {
  const key = (c: { x: number; y: number }) => `${c.x},${c.y}`;
  expect(new Set(WORD_PATH.map(key)).size).toBe(WORD_PATH.length);
  for (let i = 1; i < WORD_PATH.length; i++) {
    const a = WORD_PATH[i - 1]!;
    const b = WORD_PATH[i]!;
    expect(Math.abs(a.x - b.x) + Math.abs(a.y - b.y), `step ${i}`).toBe(1);
  }
  const letters = letterCells();
  for (const c of letters) expect(WORD_PATH.some((p) => key(p) === c), c).toBe(true);
  expect(letters.size).toBe(58);
  expect(WORD_PATH.length).toBe(77);
});

for (const [vw, vh] of VIEWPORTS) {
  test(`${vw}×${vh}: the snake ends with its body spelling "cseed"`, () => {
    const p = plan(vw, vh);
    const last = p.ticks.length - 1;
    const body = p.body(last).map((i) => p.route[i]!);
    // Head first: the body is the word path reversed, tail on the top of the c, head on the top of the d.
    expect(body.map((c) => [c.x - p.gx0, c.y - p.gy0])).toEqual([...WORD_PATH].reverse().map((c) => [c.x, c.y]));
    expect(picture(body, p.gx0, p.gy0)).toBe(CSEED);
    // ...and the word is on screen with a margin.
    expect(p.word.x).toBeGreaterThanOrEqual(8);
    expect(p.word.x + p.word.w).toBeLessThanOrEqual(vw - 8);
    expect(p.word.y).toBeGreaterThan(0);
    expect(p.word.y + p.word.h).toBeLessThan(vh);
  });

  test(`${vw}×${vh}: classic arcade movement and growth`, () => {
    const p = plan(vw, vh);
    // One cell per tick with 90° turns, except the single wrap from the right edge to the left.
    for (let i = 1; i < p.route.length; i++) {
      const a = p.route[i - 1]!;
      const b = p.route[i]!;
      if (i === p.wrapAt) {
        expect(a.x).toBeGreaterThan(p.cols); // off the right edge...
        expect(b.x).toBeLessThan(0); // ...back in from the left
        continue;
      }
      expect(Math.abs(a.x - b.x) + Math.abs(a.y - b.y), `route step ${i}`).toBe(1);
    }
    let grew = 0;
    for (let t = 1; t < p.ticks.length; t++) {
      const before = p.body(t - 1);
      const after = p.body(t);
      expect(p.ticks[t]!.head).toBe(p.ticks[t - 1]!.head + 1);
      if (p.ticks[t]!.ate) {
        grew++;
        // Eating: the tail stays put for that step and the body is one longer.
        expect(after[after.length - 1], `tail at tick ${t}`).toBe(before[before.length - 1]);
        expect(after.length).toBe(before.length + 1);
      } else {
        // Moving: the tail follows (one cell along) and the length is unchanged.
        expect(after[after.length - 1]).toBe(before[before.length - 1]! + (t >= START_LENGTH ? 1 : 0));
        expect(after.length).toBe(Math.min(before.length + (t < START_LENGTH ? 1 : 0), START_LENGTH + grew));
      }
    }
    expect(grew).toBe(p.apples.length);
    expect(grew).toBe(WORD_PATH.length - START_LENGTH);
    // Every apple sits on a word cell and is eaten exactly when the head arrives.
    for (const a of p.apples) expect(['letter', 'join']).toContain(p.route[a]!.kind);
    // Never doubles back on itself.
    for (let t = 0; t < p.ticks.length; t++) {
      const cells = p.body(t).map((i) => p.route[i]!).filter((c) => c.x >= 0 && c.x < p.cols).map((c) => `${c.x},${c.y}`);
      expect(new Set(cells).size, `tick ${t}`).toBe(cells.length);
    }
  });

  test(`${vw}×${vh}: thick cells, sensible timing, and the word shrinks onto the logo box`, () => {
    const p = plan(vw, vh);
    expect(p.C).toBe(cellSize(vw));
    expect(p.C).toBeGreaterThanOrEqual(vw < 600 ? 13 : 18);
    expect(p.word.w).toBe(WORD_COLS * p.C);
    // About 1s on the spelled word, then the shrink; the whole run stays short.
    expect(p.morphAt - p.spelledAt).toBe(1000);
    expect(p.duration).toBeLessThan(9000);
    expect(p.at.every((t, i) => i === 0 || t > p.at[i - 1]!)).toBe(true);
    // The shrunk word is centred on the logo box and fits inside it.
    const { s, tx, ty } = p.morph;
    const cx = (p.word.x + p.word.w / 2) * s + tx;
    const cy = (p.word.y + p.word.h / 2) * s + ty;
    expect(Math.abs(cx - (20 + 39))).toBeLessThan(0.01);
    expect(Math.abs(cy - (26.75 + 9))).toBeLessThan(0.01);
    expect(p.word.w * s).toBeLessThanOrEqual(78.001);
    expect(p.word.h * s).toBeLessThanOrEqual(18.001);
    // The greeting strips go in order, left to right, before the wrap.
    for (let i = 1; i < p.slices.length; i++) expect(p.slices[i]!.eatAt).toBeGreaterThanOrEqual(p.slices[i - 1]!.eatAt);
    expect(p.slices[p.slices.length - 1]!.eatAt).toBeLessThan(p.at[p.wrapAt]!);
  });
}

test('the palette jumps at least 70° each time and stays vivid', () => {
  let hue = 10;
  for (let i = 0; i < 200; i++) {
    const r = i % 7 === 0 ? () => 0 : i % 7 === 1 ? () => 0.9999 : Math.random;
    const p = nextPalette(hue, r);
    const d = Math.abs(((p.hue - hue + 540) % 360) - 180);
    expect(d, `jump ${hue} → ${p.hue}`).toBeGreaterThanOrEqual(69.9);
    expect(p.css).toMatch(/hsl\(\d+ 8\d% 5\d%\)/);
    hue = p.hue;
  }
});
