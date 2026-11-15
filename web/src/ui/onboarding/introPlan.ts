/** V0.12 animation contract. All boxes are viewport CSS pixels. No DOM or clock. */
import { SAPLING_FILL, SAPLING_VIEWBOX } from '../brand/saplingMarkup';
export type Box = Readonly<{ x: number; y: number; w: number; h: number }>;
export type Target = Box & Readonly<{ kind: 'wordmark' | 'mark' }>;
export type Transform = Readonly<{ tx: number; ty: number; sx: number; sy: number }>;
export type Direction = 'u' | 'd' | 'l' | 'r';
export type FruitKind = 'apple' | 'cherry' | 'orange';
export type Input = Readonly<{
  viewport: Readonly<{ w: number; h: number; dpr: number }>;
  target: Target;
  motion: 'full' | 'reduced';
  seed?: number;
}>;
export type Pose = Readonly<{
  id: number; box: Box; dir: Direction; visible: boolean; opacity: number;
  kind: 'letter' | 'join' | 'entry'; letter: number | null;
}>;
export type Fruit = Readonly<{
  id: number; kind: FruitKind; letter: number; box: Box; popAt: number; eatAt: number;
}>;
export type Tick = Readonly<{ at: number; body: readonly Pose[]; ate: number | null }>;
export type Timeline = Readonly<{
  meltStart: number; meltEnd: number; fruitIn: readonly [number, number];
  snakeEnter: number; spelledAt: number; sproutAt: number;
  morphStart: number; travelStart: number; morphEnd: number; swapAt: number; done: number; ffTo: number;
}>;
export type Plan = Readonly<{
  input: Input; cell: number; origin: Readonly<{ x: number; y: number }>;
  segment: number; maxLength: number; fruit: readonly Fruit[]; ticks: readonly Tick[];
  word: Readonly<{ cells: readonly Box[] }>; wordBox: Box; logoBox: Box; sprout: Box;
  timeline: Timeline; skipTimeline: Timeline;
  morph: Transform; sproutMorph: Transform;
}>;
export type Sample = Readonly<{
  ms: number; stage: 'melt' | 'fruit' | 'snake' | 'spelled' | 'morph' | 'landing' | 'swap' | 'done';
  body: readonly Pose[];
  fruit: readonly (Fruit & { visible: boolean; eaten: boolean; opacity: number; scale: number })[];
  greetingOpacity: number; backdropOpacity: number; overlayOpacity: number;
  sprout: Readonly<{ box: Box; opacity: number; scale: number; transform: Transform }>;
  transform: Transform; wordOpacity: number; wordmarkOpacity: number;
  logoBox: Box; target: Target; targetOpacity: number; done: boolean;
}>;
const identity: Transform = Object.freeze({ tx: 0, ty: 0, sx: 1, sy: 1 });
const clamp = (n: number) => Math.max(0, Math.min(1, n));
const progress = (ms: number, start: number, end: number) => end === start ? +(ms >= end) : clamp((ms - start) / (end - start));
const mix = (a: number, b: number, t: number) => a + (b - a) * t;
const interpolate = (to: Transform, t: number): Transform => ({ tx: to.tx * t, ty: to.ty * t, sx: mix(1, to.sx, t), sy: mix(1, to.sy, t) });
const mapBox = (from: Box, to: Box): Transform => ({ sx: to.w / from.w, sy: to.h / from.h, tx: to.x - from.x * to.w / from.w, ty: to.y - from.y * to.h / from.h });
function freeze<T>(value: T): T {
  if (value && typeof value === 'object' && !Object.isFrozen(value)) {
    for (const child of Object.values(value)) freeze(child);
    Object.freeze(value);
  }
  return value;
}
const shortTimeline: Timeline = freeze({ meltStart: 0, meltEnd: 350, fruitIn: [350, 350], snakeEnter: 350, spelledAt: 350, sproutAt: 350, morphStart: 350, travelStart: 350, morphEnd: 350, swapAt: 380, done: 500, ffTo: 380 });
const fullTimeline: Timeline = freeze({ meltStart: 0, meltEnd: 450, fruitIn: [450, 700], snakeEnter: 700, spelledAt: 3600, sproutAt: 3600, morphStart: 3900, travelStart: 4200, morphEnd: 4780, swapAt: 4780, done: 4900, ffTo: 4780 });

/** Square SVG viewBox around the image sapling's painted x117–155, y7–35.
 * Use the same brand bounds in the stub and real engine (D-18).
 */
function sproutBox(logo: Box): Box {
  const imageScale = logo.w / 156;
  const svgScale = 38 * imageScale / (SAPLING_FILL * SAPLING_VIEWBOX.size);
  const size = SAPLING_VIEWBOX.size * svgScale;
  return { x: logo.x + 136 * imageScale - size / 2, y: logo.y + 21 * imageScale - size / 2, w: size, h: size };
}

/** Re-measure at morphStart; preceding geometry/timing is preserved. Resize: sample at done.
 * A second press seeks to ffTo, then continues sampling through the 120ms swap.
 */
export function retargetPlan(plan: Plan, target: Target): Plan {
  return freeze({ ...plan, input: { ...plan.input, target: { ...target } }, morph: mapBox(plan.logoBox, target), sproutMorph: mapBox(plan.sprout, target) });
}
/** Escape uses the same sampler on this short plan. No fruit, snake, or travel. */
export function skipPlan(input: Input): Plan { return stubPlan({ ...input, motion: 'reduced' }); }

/** Simple immutable renderer fixture; buildPlan replaces its geometry in the real engine. */
export function stubPlan(input: Input): Plan {
  const cell = 12;
  const logoBox = { x: input.viewport.w / 2 - 156, y: input.viewport.h * .4, w: 312, h: 72 };
  const sprout = sproutBox(logoBox);
  return retargetPlan(freeze({ input: { ...input, viewport: { ...input.viewport }, target: { ...input.target } }, cell, origin: { x: logoBox.x, y: logoBox.y }, segment: 10, maxLength: 0, fruit: [], ticks: [], word: { cells: [] }, wordBox: logoBox, logoBox, sprout, timeline: input.motion === 'reduced' ? shortTimeline : fullTimeline, skipTimeline: shortTimeline, morph: identity, sproutMorph: identity }), input.target);
}

/** The screen applies transforms with origin 0 0, translate then scale.
 * Draw full wordmark asset in logoBox at wordmarkOpacity, body at wordOpacity;
 * draw a separate sapling in sprout.box (only for the rail during travel).
 * overlayOpacity fades background/page-cover layers, NOT the root containing
 * the logo: the artwork has its own complementary opacity at the final swap.
 * targetOpacity is the fixed destination logo, with NO scale. Hide the page's
 * duplicate target until overlay removal. All opacity factors are supplied.
 */
export function sampleAt(plan: Plan, time: number): Sample {
  const t = plan.timeline;
  const ms = Number.isNaN(time) ? 0 : Math.max(0, Math.min(t.done, time));
  const reduced = plan.input.motion === 'reduced';
  const morph = progress(ms, t.morphStart, t.travelStart);
  const travel = progress(ms, t.travelStart, t.morphEnd);
  const ease = travel * travel * (3 - 2 * travel);
  const swap = progress(ms, t.swapAt, t.done);
  const tick = plan.ticks.findLast(k => k.at <= ms);
  const body = reduced ? [] : (tick?.body ?? []).map(p => {
    const inset = (plan.cell - plan.segment) / 2;
    return { ...p, box: p.kind === 'letter' ? { x: p.box.x - inset * morph, y: p.box.y - inset * morph, w: mix(plan.segment, plan.cell, morph), h: mix(plan.segment, plan.cell, morph) } : p.box,
      visible: p.visible && !(p.kind === 'join' && morph === 1), opacity: p.opacity * (p.kind === 'join' ? 1 - morph : 1) };
  });
  const stage = ms >= t.done ? 'done' : reduced ? 'melt' : ms >= t.swapAt ? 'swap' : ms >= t.travelStart ? 'landing' : ms >= t.morphStart ? 'morph' : ms >= t.spelledAt ? 'spelled' : ms >= t.snakeEnter ? 'snake' : ms >= t.fruitIn[0] ? 'fruit' : 'melt';
  const rail = plan.input.target.kind === 'mark';
  return freeze({ ms, stage, body,
    fruit: reduced ? [] : plan.fruit.map(f => ({ ...f, eaten: ms >= f.eatAt, visible: ms >= f.popAt && ms < f.eatAt, opacity: ms >= f.eatAt ? 0 : progress(ms, f.popAt, f.popAt + 120), scale: .6 + .4 * progress(ms, f.popAt, f.popAt + 120) })),
    greetingOpacity: 1 - progress(ms, t.meltStart, t.meltEnd), backdropOpacity: 1 - progress(ms, t.meltStart, t.meltEnd),
    overlayOpacity: reduced ? 1 - progress(ms, 0, t.done) : 1 - swap,
    sprout: { box: plan.sprout, opacity: reduced ? 0 : progress(ms, t.sproutAt, t.morphStart) * (1 - morph + (rail ? morph * travel : 0)) * (1 - swap), scale: reduced ? 1 : progress(ms, t.sproutAt, t.morphStart), transform: rail ? interpolate(plan.sproutMorph, ease) : interpolate(plan.morph, ease) },
    transform: interpolate(rail ? plan.sproutMorph : plan.morph, ease), wordOpacity: reduced ? 0 : 1 - morph,
    wordmarkOpacity: reduced ? 0 : morph * (rail ? 1 - travel : 1) * (1 - swap), logoBox: plan.logoBox,
    target: plan.input.target, targetOpacity: reduced ? progress(ms, 0, t.done) : swap, done: ms >= t.done });
}
type GridCell = { x: number; y: number; kind: Pose['kind']; letter: number | null };
// Each letter is a single self-avoiding stroke. Joins start on the baseline;
// ascent in the gutter brings the head to the next stroke's entry point.
const strokes: readonly (readonly [number, number][])[] = [
  [[3,1],[2,1],[1,1],[0,1],[0,2],[0,3],[0,4],[0,5],[1,5],[2,5],[3,5]],
  [[8,1],[7,1],[6,1],[5,1],[5,2],[5,3],[6,3],[7,3],[8,3],[8,4],[8,5],[7,5],[6,5],[5,5]],
  [[10,2],[10,1],[11,1],[12,1],[13,1],[13,2],[13,3],[12,3],[11,3],[10,3],[10,4],[10,5],[11,5],[12,5],[13,5]],
  [[15,2],[15,1],[16,1],[17,1],[18,1],[18,2],[18,3],[17,3],[16,3],[15,3],[15,4],[15,5],[16,5],[17,5],[18,5]],
  [[22,1],[21,1],[20,1],[20,2],[20,3],[20,4],[20,5],[21,5],[22,5],[23,5],[23,4],[23,3],[23,2],[23,1],[23,0]],
];
const joins: readonly (readonly [number, number][])[] = [
  [[4,5],[4,4],[4,3],[4,2],[4,1],[4,0],[5,0],[6,0],[7,0],[8,0]],
  [[5,6],[6,6],[7,6],[8,6],[9,6],[9,5],[9,4],[9,3],[9,2]],
  [[14,5],[14,4],[14,3],[14,2]],
  [[19,5],[19,4],[19,3],[19,2],[19,1],[19,0],[20,0],[21,0],[22,0]],
];
const wordRoute: readonly GridCell[] = freeze(strokes.flatMap((stroke, letter) => [
  ...stroke.map(([x,y]) => ({ x, y, kind: 'letter' as const, letter })),
  ...(joins[letter] ?? []).map(([x,y]) => ({ x, y, kind: 'join' as const, letter: null })),
]));
const direction = (a: GridCell, b: GridCell): Direction => b.x > a.x ? 'r' : b.x < a.x ? 'l' : b.y > a.y ? 'd' : 'u';

export function buildPlan(input: Input): Plan {
  if (![input.viewport.w,input.viewport.h,input.viewport.dpr,input.target.x,input.target.y,input.target.w,input.target.h].every(Number.isFinite) || input.viewport.w <= 0 || input.viewport.h <= 0 || input.viewport.dpr <= 0 || input.target.w <= 0 || input.target.h <= 0) throw new RangeError('Intro requires a finite positive viewport and target');
  if (input.motion === 'reduced') return stubPlan(input);
  const dpr = input.viewport.dpr;
  const snap = (n: number) => Math.round(n * dpr) / dpr;
  const unit = 115 / 24;
  const cell = Math.max(1 / dpr, Math.floor(Math.min((input.viewport.w - 32) / (156 / unit), input.viewport.h / 16, 30) * dpr) / dpr);
  // Snap both block edges: an even number of physical pixels in the gap.
  const inset = Math.max(0, Math.round(cell * .07 * dpr)) / dpr;
  const segment = cell - 2 * inset;
  const origin = { x: snap((input.viewport.w - 156 / unit * cell) / 2), y: snap(input.viewport.h * .43 - cell * 3) };
  const box = (c: GridCell, pad = 0): Box => ({ x: origin.x + c.x * cell + pad, y: origin.y + c.y * cell + pad, w: cell - 2 * pad, h: cell - 2 * pad });
  const wordBox = { x: origin.x, y: origin.y, w: 24 * cell, h: 6 * cell };
  const logoBox = { x: origin.x, y: origin.y + (6 - 36 / unit) * cell, w: 156 / unit * cell, h: 36 / unit * cell };
  const sprout = sproutBox(logoBox);
  const initialLength = joins.reduce((sum, j) => sum + j.length, 0);
  const entry: GridCell[] = [];
  for (let x = 3 - initialLength + 3; x <= 3; x++) entry.push({ x, y: -2, kind: 'entry', letter: null });
  entry.push({ x:3,y:-1,kind:'entry',letter:null }, { x:3,y:0,kind:'entry',letter:null });
  const route = [...entry, ...wordRoute];
  const fruit: Fruit[] = [];
  const ticks: Tick[] = [];
  let length = initialLength;
  let random = (input.seed ?? 1) >>> 0;
  const kinds: FruitKind[] = ['apple','cherry','orange'];
  for (let head = entry.length - 1; head < route.length; head++) {
    const step = head - entry.length + 1;
    const at = fullTimeline.snakeEnter + step / wordRoute.length * (fullTimeline.spelledAt - fullTimeline.snakeEnter);
    const c = route[head]!;
    let ate: number | null = null;
    if (c.kind === 'letter') {
      ate = fruit.length;
      random = (Math.imul(random, 1664525) + 1013904223) >>> 0;
      fruit.push({ id: ate, kind: kinds[random % kinds.length]!, letter: c.letter!, box: box(c, inset), popAt: fullTimeline.fruitIn[0] + ate / 70 * 120, eatAt: at });
      length++;
    }
    const body: Pose[] = [];
    for (let i = head; i > head - length; i--) {
      const p = route[i]!;
      body.push({ id: head - i, box: box(p, inset), dir: direction(route[i-1] ?? { ...p, x: p.x-1 }, p), visible: true, opacity: 1, kind: p.kind, letter: p.letter });
    }
    ticks.push({ at, body, ate });
  }
  return retargetPlan(freeze({ input: { ...input, viewport:{...input.viewport},target:{...input.target} }, cell, origin, segment, maxLength: length, fruit, ticks,
    word: { cells: wordRoute.filter(c=>c.kind==='letter').map(c=>box(c)) }, wordBox, logoBox, sprout,
    timeline: fullTimeline, skipTimeline:shortTimeline, morph:identity, sproutMorph:identity }), input.target);
}
