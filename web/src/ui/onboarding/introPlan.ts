/** Pure V0.12 glide timeline. Geometry is in viewport CSS pixels. */
export type Box = Readonly<{ x: number; y: number; w: number; h: number }>;
export type Target = Box & Readonly<{ kind: 'wordmark' | 'mark' }>;
export type Input = Readonly<{
  viewport: Readonly<{ w: number; h: number; dpr: number }>;
  /** Measured letters, not the sentence or its line box. */
  word: Box;
  /** Full shell image box (including sapling), or the rail mark box. */
  target: Target;
  motion: 'full' | 'reduced';
}>;
export type Transform = Readonly<{ tx: number; ty: number; s: number }>;
export type Sample = Readonly<{
  stage: 'melt' | 'center' | 'hold' | 'glide' | 'swap' | 'done';
  sentenceOpacity: number;
  backdropOpacity: number;
  word: Transform & Readonly<{ opacity: number }>;
  logoOpacity: number;
  saplingOpacity: number;
}>;
export type Timeline = Readonly<{
  meltStart: number; meltEnd: number; centerStart: number; centerEnd: number;
  glideStart: number; glideEnd: number; swapStart: number; swapEnd: number; done: number;
}>;
export type Plan = Readonly<{
  input: Input;
  timeline: Timeline;
  center: Transform;
  landing: Transform;
  ffTo: number;
  done: number;
}>;

/** Lettering occupies x0..471 in the 640-wide source artwork. The checked-in
 * 156x36 derivative has lettering over its full height. Keep the normalized
 * width here, independently of the resolution used to deliver the image.
 */
export const LETTERING_WIDTH = 471 / 640;
const fullTimeline: Timeline = Object.freeze({
  meltStart: 0, meltEnd: 350, centerStart: 150, centerEnd: 600,
  glideStart: 850, glideEnd: 1350, swapStart: 1350, swapEnd: 1470, done: 1500,
});
const shortTimeline: Timeline = Object.freeze({
  meltStart: 0, meltEnd: 350, centerStart: 0, centerEnd: 0,
  glideStart: 350, glideEnd: 350, swapStart: 350, swapEnd: 500, done: 500,
});
const identity: Transform = Object.freeze({ tx: 0, ty: 0, s: 1 });
const clamp = (n: number) => Math.max(0, Math.min(1, n));
const progress = (ms: number, start: number, end: number) => clamp((ms - start) / (end - start));
const ease = (t: number) => t * t * (3 - 2 * t);
const mix = (a: number, b: number, t: number) => a + (b - a) * t;
const blend = (a: Transform, b: Transform, t: number): Transform => ({
  tx: mix(a.tx, b.tx, t), ty: mix(a.ty, b.ty, t), s: mix(a.s, b.s, t),
});

function landing(word: Box, target: Target): Transform {
  // Match lettering width and vertical centre. Arbitrary source aspect ratios
  // cannot match both target dimensions without distortion. The renderer must
  // supply matching lettering proportions for an exact four-edge handoff.
  const w = target.kind === 'wordmark' ? target.w * LETTERING_WIDTH : target.w;
  const s = w / word.w;
  return Object.freeze({ tx: target.x - word.x, ty: target.y + (target.h - word.h * s) / 2 - word.y, s });
}

export function buildPlan(input: Input): Plan {
  // Copy before freezing so the caller may reuse and update measured boxes.
  const copy: Input = Object.freeze({
    ...input, viewport: Object.freeze({ ...input.viewport }),
    word: Object.freeze({ ...input.word }), target: Object.freeze({ ...input.target }),
  });
  const timeline = copy.motion === 'reduced' ? shortTimeline : fullTimeline;
  return Object.freeze({
    input: copy, timeline,
    center: Object.freeze({
      tx: (copy.viewport.w - copy.word.w) / 2 - copy.word.x,
      ty: (copy.viewport.h - copy.word.h) / 2 - copy.word.y, s: 1,
    }),
    landing: landing(copy.word, copy.target), ffTo: timeline.swapStart, done: timeline.done,
  });
}

/** Call at glideStart after re-measuring. Earlier samples stay identical.
 * Retargeting during the glide changes the path; the screen finishes on resize.
 */
export function retargetPlan(plan: Plan, target: Target): Plan {
  const copy = Object.freeze({ ...target });
  return Object.freeze({ ...plan, input: Object.freeze({ ...plan.input, target: copy }), landing: landing(plan.input.word, copy) });
}

/** Escape takes the same stationary, short fade as reduced motion. */
export function skipPlan(input: Input): Plan { return buildPlan({ ...input, motion: 'reduced' }); }

/** Apply translate(tx, ty) scale(s), origin 0 0, to an element positioned at
 * input.word.x/y. Thus its rendered left is word.x + tx, never word.x * s + tx.
 * Draw the real logo at input.target with no transform. For wordmark targets,
 * logoOpacity applies to letters only; saplingOpacity independently reveals
 * the sapling. Keep the actual page logo hidden until this overlay is removed.
 */
export function sampleAt(plan: Plan, time: number): Sample {
  const t = plan.timeline;
  const ms = Number.isNaN(time) ? 0 : Math.max(0, Math.min(plan.done, time));
  if (plan.input.motion === 'reduced') {
    const fade = progress(ms, 0, plan.done);
    return {
      stage: ms >= plan.done ? 'done' : ms >= plan.ffTo ? 'swap' : 'melt',
      sentenceOpacity: 1 - progress(ms, 0, t.meltEnd), backdropOpacity: 1 - fade,
      word: { ...identity, opacity: 1 - fade }, logoOpacity: fade,
      saplingOpacity: plan.input.target.kind === 'wordmark' ? fade : 0,
    };
  }
  const center = ease(progress(ms, t.centerStart, t.centerEnd));
  const glide = ease(progress(ms, t.glideStart, t.glideEnd));
  const swap = progress(ms, t.swapStart, t.swapEnd);
  const rail = plan.input.target.kind === 'mark';
  const transform = ms < t.glideStart ? blend(identity, plan.center, center) : blend(plan.center, plan.landing, glide);
  // A rail has no lettering to swap into: fade the travelling word and reveal
  // its fixed mark over the glide. Wordmark targets swap only after arrival.
  const handoff = rail ? glide : swap;
  return {
    stage: ms >= plan.done ? 'done' : ms >= t.swapStart ? 'swap' : ms >= t.glideStart ? 'glide'
      : ms >= t.centerEnd ? 'hold' : ms >= t.centerStart ? 'center' : 'melt',
    sentenceOpacity: 1 - progress(ms, t.meltStart, t.meltEnd),
    backdropOpacity: 1 - glide,
    word: { ...transform, opacity: 1 - handoff }, logoOpacity: handoff,
    saplingOpacity: rail ? 0 : progress(ms, t.swapStart, t.done),
  };
}
