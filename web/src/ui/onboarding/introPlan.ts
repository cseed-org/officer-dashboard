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
  sproutStart: Box; settled: readonly Pose[];
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
const blendBox = (from: Box, to: Box, t: number): Box => ({ x: mix(from.x, to.x, t), y: mix(from.y, to.y, t), w: mix(from.w, to.w, t), h: mix(from.h, to.h, t) });
function freeze<T>(value: T): T {
  if (value && typeof value === 'object' && !Object.isFrozen(value)) {
    for (const child of Object.values(value)) freeze(child);
    Object.freeze(value);
  }
  return value;
}
const shortTimeline: Timeline = freeze({ meltStart: 0, meltEnd: 350, fruitIn: [350, 350], snakeEnter: 350, spelledAt: 350, sproutAt: 350, morphStart: 350, travelStart: 350, morphEnd: 350, swapAt: 380, done: 500, ffTo: 380 });
const fullTimeline: Timeline = freeze({ meltStart: 0, meltEnd: 450, fruitIn: [300, 650], snakeEnter: 300, spelledAt: 3350, sproutAt: 3350, morphStart: 3650, travelStart: 4200, morphEnd: 4780, swapAt: 4780, done: 4900, ffTo: 4780 });

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
  return retargetPlan(freeze({ input: { ...input, viewport: { ...input.viewport }, target: { ...input.target } }, cell, origin: { x: logoBox.x, y: logoBox.y }, segment: 10, maxLength: 0, fruit: [], ticks: [], settled: [], word: { cells: [] }, wordBox: logoBox, logoBox, sprout, sproutStart: sprout, timeline: input.motion === 'reduced' ? shortTimeline : fullTimeline, skipTimeline: shortTimeline, morph: identity, sproutMorph: identity }), input.target);
}

/** The screen applies transforms with origin 0 0, translate then scale.
 * Draw the wordmark's LETTERS ONLY in logoBox at wordmarkOpacity, body at wordOpacity;
 * draw one separate sapling in sprout.box throughout growth, settling and travel.
 * overlayOpacity fades background/page-cover layers, NOT the root containing
 * the logo: the outgoing artwork stays opaque UNDER the destination during the
 * final swap. Complementary alpha on two source-over layers would dim the ink 25%.
 * targetOpacity is the fixed destination logo, with NO scale. Hide the page's
 * duplicate target until overlay removal. All opacity factors are supplied.
 */
export function sampleAt(plan: Plan, time: number): Sample {
  const t = plan.timeline;
  const ms = Number.isNaN(time) ? 0 : Math.max(0, Math.min(t.done, time));
  const reduced = plan.input.motion === 'reduced';
  const settle = progress(ms, t.morphStart, t.morphStart + (t.travelStart - t.morphStart) * .6);
  const morph = progress(ms, t.morphStart + (t.travelStart - t.morphStart) * .6, t.travelStart);
  const travel = progress(ms, t.travelStart, t.morphEnd);
  const ease = travel * travel * (3 - 2 * travel);
  const swap = progress(ms, t.swapAt, t.done);
  // Binary search works on older Safari too. Before the morph, reuse the frozen
  // tick and pose objects; sampling must not recursively freeze hundreds of copies.
  let lo = 0, hi = plan.ticks.length;
  while (lo < hi) { const mid = (lo + hi) >>> 1; if (plan.ticks[mid]!.at <= ms) lo = mid + 1; else hi = mid; }
  const tick = plan.ticks[lo - 1];
  const body = reduced ? [] : !settle ? tick?.body ?? [] : (tick?.body ?? []).map((p, i) => {
    const dest = plan.settled[i]!;
    return p.kind === 'letter' ? { ...p, box: blendBox(p.box, dest.box, settle) }
      : { ...p, visible: p.visible && settle < 1, opacity: p.opacity * (1 - settle) };
  });
  const stage = ms >= t.done ? 'done' : reduced ? 'melt' : ms >= t.swapAt ? 'swap' : ms >= t.travelStart ? 'landing' : ms >= t.morphStart ? 'morph' : ms >= t.spelledAt ? 'spelled' : ms >= t.snakeEnter ? 'snake' : ms >= t.fruitIn[0] ? 'fruit' : 'melt';
  const rail = plan.input.target.kind === 'mark';
  return { ms, stage, body,
    fruit: reduced ? [] : plan.fruit.map(f => ({ ...f, eaten: ms >= f.eatAt, visible: ms >= f.popAt && ms < f.eatAt, opacity: ms >= f.eatAt ? 0 : progress(ms, f.popAt, f.popAt + 120), scale: .6 + .4 * progress(ms, f.popAt, f.popAt + 120) })),
    greetingOpacity: 1 - progress(ms, t.meltStart, t.meltEnd), backdropOpacity: 1 - progress(ms, t.meltStart, t.meltEnd),
    overlayOpacity: reduced ? 1 - progress(ms, 0, t.done) : 1 - swap,
    sprout: { box: blendBox(plan.sproutStart, plan.sprout, settle), opacity: reduced ? 0 : progress(ms, t.sproutAt, t.morphStart), scale: reduced ? 1 : progress(ms, t.sproutAt, t.morphStart), transform: rail ? interpolate(plan.sproutMorph, ease) : interpolate(plan.morph, ease) },
    transform: interpolate(rail ? plan.sproutMorph : plan.morph, ease), wordOpacity: reduced ? 0 : 1 - morph,
    wordmarkOpacity: reduced ? 0 : morph * (rail ? 1 - travel : 1), logoBox: plan.logoBox,
    target: plan.input.target, targetOpacity: reduced ? progress(ms, 0, t.done) : swap, done: ms >= t.done };
}
type GridCell = { x: number; y: number; kind: Pose['kind']; letter: number | null };
type Point = readonly [number, number];
// Coarse two-cell strokes keep bottom entry/exit without closing the c.
// These are self-avoiding paths through the glyphs (111 painted cells total),
// rather than a doubled fine grid whose ticks would outrun a 60 Hz frame (D-21).
const GLYPH_WIDTHS = [3,4,4,4,3] as const;
const GLYPH_H = 8, PITCH = 7, WORD_W = PITCH * 4 + 3;
const c: readonly Point[] = [[1,7],[0,7],[0,6],[0,5],[0,4],[0,3],[0,2],[0,1],[0,0],[1,0],[2,0],[2,1],[1,1],[1,2],[1,3],[1,4],[1,5],[1,6],[2,6],[2,7]];
const s: readonly Point[] = [[0,7],[0,6],[1,6],[1,7],[2,7],[2,6],[2,5],[2,4],[1,4],[0,4],[0,3],[0,2],[0,1],[0,0],[1,0],[2,0],[3,0],[3,1],[2,1],[1,1],[1,2],[1,3],[2,3],[3,3],[3,4],[3,5],[3,6],[3,7]];
const e: readonly Point[] = [[0,7],[0,6],[0,5],[0,4],[0,3],[0,2],[0,1],[1,1],[2,1],[3,1],[3,2],[3,3],[3,4],[2,4],[1,4],[1,5],[1,6],[1,7],[2,7],[2,6],[3,6],[3,7]];
const d: readonly Point[] = [[0,7],[0,6],[0,5],[0,4],[0,3],[0,2],[1,2],[1,1],[1,0],[2,0],[2,1],[2,2],[2,3],[2,4],[2,5],[2,6],[1,6],[1,7],[2,7]];
const strokes = [c,s,e,e,d].map((stroke,i) => stroke.map(([x,y]): Point => [x+i*PITCH,y]));
const joins = strokes.slice(0,-1).map((stroke,i) => {
  const from = stroke[stroke.length-1]![0], to = strokes[i+1]![0]![0];
  return Array.from({length:to-from+1},(_,n): Point => [from+n,GLYPH_H]);
});
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
  const unit = 115 / WORD_W;
  const cell = Math.max(1 / dpr, Math.floor(Math.min((input.viewport.w - 32) / (156 / unit), input.viewport.h / 18, 26) * dpr) / dpr);
  // Snap both block edges: an even number of physical pixels in the gap.
  const inset = Math.max(0, Math.round(cell * .07 * dpr)) / dpr;
  const segment = cell - 2 * inset;
  const origin = { x: snap((input.viewport.w - 156 / unit * cell) / 2), y: snap(input.viewport.h * .43 - cell * 4) };
  const box = (c: GridCell, pad = 0): Box => ({ x: origin.x + c.x * cell + pad, y: origin.y + c.y * cell + pad, w: cell - 2 * pad, h: cell - 2 * pad });
  const wordBox = { x: origin.x, y: origin.y, w: WORD_W * cell, h: GLYPH_H * cell };
  const logoBox = { x: origin.x, y: origin.y + (GLYPH_H - 36 / unit) * cell, w: 156 / unit * cell, h: 36 / unit * cell };
  const sprout = sproutBox(logoBox);
  const sproutStart = { ...sprout, x: origin.x + (4*PITCH+2)*cell - sprout.w/2, y: origin.y - sprout.h*.86 };
  const initialLength = joins.reduce((sum, j) => sum + j.length, 0);
  const entry: GridCell[] = [];
  const firstHead = Math.floor(-origin.x/cell)-2;
  for (let x = firstHead-initialLength+1; x <= 1; x++) entry.push({ x, y: GLYPH_H, kind: 'entry', letter: null });
  const route = [...entry, ...wordRoute];
  const poses: Pose[] = route.map((p,id) => ({
    id, box: box(p,inset), dir: direction(route[id-1] ?? {...p,x:p.x-1},p),
    visible: true, opacity: p.kind === 'join' ? .4 : 1, kind: p.kind, letter: p.letter,
  }));
  const fruitCount = strokes.reduce((n,s)=>n+s.length,0);
  const fruit: Fruit[] = [];
  const ticks: Tick[] = [];
  let length = initialLength;
  let random = (input.seed ?? 1) >>> 0;
  const kinds: FruitKind[] = ['apple','cherry','orange'];
  const firstFruitAt = 700;
  for (let head = initialLength - 1; head < route.length; head++) {
    const at = head < entry.length
      ? mix(fullTimeline.snakeEnter, firstFruitAt, (head-initialLength+1)/(entry.length-initialLength+1))
      : mix(firstFruitAt, fullTimeline.spelledAt, (head-entry.length)/(wordRoute.length-1));
    const c = route[head]!;
    let ate: number | null = null;
    if (c.kind === 'letter') {
      ate = fruit.length;
      random = (Math.imul(random, 1664525) + 1013904223) >>> 0;
      let kind = kinds[random % kinds.length]!;
      if (fruit.length >= 2 && fruit[fruit.length-1]!.kind === kind && fruit[fruit.length-2]!.kind === kind) kind = kinds[(random % kinds.length+1)%kinds.length]!;
      fruit.push({ id: ate, kind, letter: c.letter!, box: poses[head]!.box, popAt: fullTimeline.fruitIn[0] + ate / fruitCount * 180, eatAt: at });
      length++;
    }
    const body: Pose[] = [];
    for (let i = head; i > head - length; i--) {
      // The route index belongs to the cell, not the distance from the head.
      // Renderers can recycle retired ids: surviving blocks never move per tick.
      body.push(poses[i]!);
    }
    ticks.push({ at, body, ate });
  }
  // Painted letter bounds in the 640x147 artwork (sapling is separate). Settle
  // each glyph into its own bounds before revealing that artwork, not a single
  // whole-word dissolve from the unrelated grid spacing.
  const ink: readonly Box[] = [
    {x:0,y:44,w:101,h:103},{x:101,y:44,w:69,h:103},
    {x:171,y:44,w:101,h:103},{x:273,y:44,w:100,h:103},{x:371,y:0,w:100,h:147},
  ];
  const settled = ticks[ticks.length-1]!.body.map(p => {
    if (p.letter === null) return p;
    const b = ink[p.letter]!, c = route[p.id]!;
    const dest = {x:logoBox.x+b.x/640*logoBox.w,y:logoBox.y+b.y/147*logoBox.h,w:b.w/640*logoBox.w,h:b.h/147*logoBox.h};
    return {...p,box:{x:dest.x+(c.x-p.letter*PITCH)/GLYPH_WIDTHS[p.letter]!*dest.w,y:dest.y+c.y/GLYPH_H*dest.h,w:dest.w/GLYPH_WIDTHS[p.letter]!,h:dest.h/GLYPH_H}};
  });
  return retargetPlan(freeze({ input: { ...input, viewport:{...input.viewport},target:{...input.target} }, cell, origin, segment, maxLength: length, fruit, ticks, settled, sproutStart,
    word: { cells: wordRoute.filter(c=>c.kind==='letter').map(c=>box(c)) }, wordBox, logoBox, sprout,
    timeline: fullTimeline, skipTimeline:shortTimeline, morph:identity, sproutMorph:identity }), input.target);
}
