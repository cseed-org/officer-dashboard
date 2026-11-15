/** V0.12 animation contract. All boxes are viewport CSS pixels. No DOM or clock. */
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
const shortTimeline: Timeline = freeze({ meltStart: 0, meltEnd: 350, fruitIn: [350, 350], snakeEnter: 350, spelledAt: 350, sproutAt: 350, morphStart: 350, travelStart: 350, morphEnd: 350, swapAt: 350, done: 500, ffTo: 500 });
const fullTimeline: Timeline = freeze({ meltStart: 0, meltEnd: 450, fruitIn: [450, 700], snakeEnter: 700, spelledAt: 3600, sproutAt: 3600, morphStart: 3900, travelStart: 4200, morphEnd: 4780, swapAt: 4780, done: 4900, ffTo: 4900 });

/** Re-measure at morphStart; preceding geometry/timing is preserved. Resize: sample the returned plan at ffTo. */
export function retargetPlan(plan: Plan, target: Target): Plan {
  return freeze({ ...plan, input: { ...plan.input, target: { ...target } }, morph: mapBox(plan.logoBox, target), sproutMorph: mapBox(plan.sprout, target) });
}
/** Escape uses the same sampler on this short plan. No fruit, snake, or travel. */
export function skipPlan(input: Input): Plan { return stubPlan({ ...input, motion: 'reduced' }); }

/** Simple immutable renderer fixture; buildPlan replaces its geometry in the real engine. */
export function stubPlan(input: Input): Plan {
  const cell = 12;
  const logoBox = { x: input.viewport.w / 2 - 156, y: input.viewport.h * .4, w: 312, h: 72 };
  const sprout = { x: logoBox.x + 234, y: logoBox.y + 14, w: 76, h: 56 };
  return retargetPlan(freeze({ input: { ...input, viewport: { ...input.viewport }, target: { ...input.target } }, cell, origin: { x: logoBox.x, y: logoBox.y }, segment: 10, maxLength: 0, fruit: [], ticks: [], word: { cells: [] }, wordBox: logoBox, logoBox, sprout, timeline: input.motion === 'reduced' ? shortTimeline : fullTimeline, skipTimeline: shortTimeline, morph: identity, sproutMorph: identity }), input.target);
}

/** The screen applies transforms with origin 0 0, translate then scale.
 * Draw full wordmark asset in logoBox at wordmarkOpacity, body at wordOpacity;
 * draw a separate sapling in sprout.box (only for the rail during travel).
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
  const body = reduced ? [] : (tick?.body ?? []).map(p => ({ ...p, visible: p.visible && !(p.kind === 'join' && morph === 1), opacity: p.opacity * (p.kind === 'join' ? 1 - morph : 1) }));
  const stage = ms >= t.done ? 'done' : reduced ? 'melt' : ms >= t.swapAt ? 'swap' : ms >= t.travelStart ? 'landing' : ms >= t.morphStart ? 'morph' : ms >= t.spelledAt ? 'spelled' : ms >= t.snakeEnter ? 'snake' : ms >= t.fruitIn[0] ? 'fruit' : 'melt';
  const rail = plan.input.target.kind === 'mark';
  return freeze({ ms, stage, body,
    fruit: reduced ? [] : plan.fruit.map(f => ({ ...f, eaten: ms >= f.eatAt, visible: ms >= f.popAt && ms < f.eatAt, opacity: ms >= f.eatAt ? 0 : progress(ms, f.popAt, f.popAt + 120), scale: .6 + .4 * progress(ms, f.popAt, f.popAt + 120) })),
    greetingOpacity: 1 - progress(ms, t.meltStart, t.meltEnd), backdropOpacity: 1 - progress(ms, t.meltStart, t.meltEnd),
    overlayOpacity: reduced ? 1 - progress(ms, 0, t.done) : 1 - swap,
    sprout: { box: plan.sprout, opacity: reduced ? 0 : progress(ms, t.sproutAt, t.morphStart) * (rail ? 1 - swap : 1 - morph), scale: reduced ? 1 : progress(ms, t.sproutAt, t.morphStart), transform: rail ? interpolate(plan.sproutMorph, ease) : interpolate(plan.morph, ease) },
    transform: interpolate(plan.morph, ease), wordOpacity: reduced ? 0 : 1 - morph,
    wordmarkOpacity: reduced ? 0 : morph * (rail ? 1 - travel : 1) * (1 - swap), logoBox: plan.logoBox,
    target: plan.input.target, targetOpacity: reduced ? progress(ms, 0, t.done) : swap, done: ms >= t.done });
}
export function buildPlan(input: Input): Plan { return stubPlan(input); }
