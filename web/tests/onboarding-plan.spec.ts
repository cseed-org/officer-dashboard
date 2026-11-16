import { test, expect } from '@playwright/test';
import { buildPlan, sampleAt, retargetPlan, skipPlan, LETTERING_WIDTH, type Box, type Input, type Plan } from '../src/ui/onboarding/introPlan';

const fixtures: Input[] = [
  { viewport: { w: 1440, h: 900, dpr: 1 }, word: { x: 820, y: 420, w: 240, h: 240 * 36 / (156 * LETTERING_WIDTH) }, target: { x: 24, y: 24, w: 78, h: 18, kind: 'wordmark' }, motion: 'full' },
  { viewport: { w: 820, h: 1180, dpr: 2 }, word: { x: 500, y: 560, w: 220, h: 68 }, target: { x: 20, y: 24, w: 24, h: 24, kind: 'mark' }, motion: 'full' },
  { viewport: { w: 390, h: 844, dpr: 3 }, word: { x: 140, y: 410, w: 130, h: 130 * 36 / (156 * LETTERING_WIDTH) }, target: { x: 156, y: 23, w: 78, h: 18, kind: 'wordmark' }, motion: 'full' },
];
function rendered(plan: Plan, ms: number): Box {
  const s = sampleAt(plan, ms).word, b = plan.input.word;
  return { x: b.x + s.tx, y: b.y + s.ty, w: b.w * s.s, h: b.h * s.s };
}
function near(a: number, b: number) { expect(Math.abs(a - b)).toBeLessThanOrEqual(.5); }

for (const input of fixtures) test.describe(`${input.viewport.w}x${input.viewport.h}`, () => {
  test('centres the letters unscaled throughout the hold', () => {
    const p = buildPlan(input);
    for (const ms of [600, 725, 850]) {
      const b = rendered(p, ms);
      near(b.x + b.w / 2, input.viewport.w / 2);
      near(b.y + b.h / 2, input.viewport.h / 2);
      expect(sampleAt(p, ms).word.s).toBe(1);
    }
    expect(sampleAt(p, 0).word).toEqual({ tx: 0, ty: 0, s: 1, opacity: 1 });
    expect(sampleAt(p, 350).sentenceOpacity).toBe(0);
    expect(sampleAt(p, 725).stage).toBe('hold');
  });

  test('uniform glide lands on lettering (or rail mark) without moving during swap', () => {
    const p = buildPlan(input), target = input.target;
    const width = target.w * (target.kind === 'wordmark' ? LETTERING_WIDTH : 1);
    for (let ms = 0; ms <= p.done; ms += 10) {
      const b = rendered(p, ms);
      expect(b.w / input.word.w).toBeCloseTo(b.h / input.word.h, 10);
    }
    const b = rendered(p, 1350);
    near(b.x, target.x); near(b.w, width);
    near(b.y + b.h / 2, target.y + target.h / 2);
    if (target.kind === 'wordmark') { near(b.y, target.y); near(b.h, target.h); }
    for (const ms of [1350, 1380, 1410, 1470, 1500]) {
      expect(rendered(p, ms)).toEqual(b);
      const sample = sampleAt(p, ms);
      expect(sample.word.opacity + sample.logoOpacity).toBeCloseTo(1, 10);
    }
    expect(sampleAt(p, p.done).logoOpacity).toBe(1);
    expect(sampleAt(p, p.done).saplingOpacity).toBe(target.kind === 'mark' ? 0 : 1);
    if (target.kind === 'mark') {
      expect(sampleAt(p, 1100).word.opacity).toBeCloseTo(.5);
      expect(sampleAt(p, 1100).logoOpacity).toBeCloseTo(.5);
    } else {
      expect(sampleAt(p, 1349).word.opacity).toBe(1);
      expect(sampleAt(p, 1349).logoOpacity).toBe(0);
      expect(sampleAt(p, 1350).saplingOpacity).toBe(0);
    }
  });

  test('stages and fades are monotone, continuous and finish in 1.5 seconds', () => {
    const p = buildPlan(input), stages = ['melt', 'center', 'hold', 'glide', 'swap', 'done'];
    const t = p.timeline;
    const ordered = [t.meltStart, t.centerStart, t.meltEnd, t.centerEnd, t.glideStart, t.glideEnd, t.swapStart, t.swapEnd, t.done];
    expect(ordered).toEqual([...ordered].sort((a, b) => a - b));
    expect(p.done).toBeGreaterThanOrEqual(1400); expect(p.done).toBeLessThanOrEqual(1600);
    let before = sampleAt(p, 0);
    for (let ms = 1; ms <= p.done; ms++) {
      const now = sampleAt(p, ms);
      expect(stages.indexOf(now.stage)).toBeGreaterThanOrEqual(stages.indexOf(before.stage));
      for (const key of ['sentenceOpacity', 'backdropOpacity'] as const) expect(now[key]).toBeLessThanOrEqual(before[key]);
      expect(now.word.opacity).toBeLessThanOrEqual(before.word.opacity);
      expect(now.logoOpacity).toBeGreaterThanOrEqual(before.logoOpacity);
      expect(now.saplingOpacity).toBeGreaterThanOrEqual(before.saplingOpacity);
      for (const alpha of [now.sentenceOpacity, now.backdropOpacity, now.word.opacity, now.logoOpacity, now.saplingOpacity]) {
        expect(alpha).toBeGreaterThanOrEqual(0); expect(alpha).toBeLessThanOrEqual(1);
      }
      before = now;
    }
    for (const ms of ordered.filter(ms => ms > 0)) {
      const a = rendered(p, ms - .001), b = rendered(p, ms);
      for (const key of ['x', 'y', 'w', 'h'] as const) expect(Math.abs(a[key] - b[key])).toBeLessThan(.01);
    }
    expect(sampleAt(p, p.done).stage).toBe('done');
    expect(sampleAt(p, p.done).backdropOpacity).toBe(0);
    expect(sampleAt(p, Infinity)).toEqual(sampleAt(p, p.done));
    expect(sampleAt(p, -Infinity)).toEqual(sampleAt(p, 0));
    expect(sampleAt(p, NaN)).toEqual(sampleAt(p, 0));
  });

  test('retarget preserves the pre-glide path and ffTo seeks to the fixed swap', () => {
    const p = buildPlan(input), target = { x: 33.25, y: 17.5, w: 91, h: 21, kind: 'wordmark' as const };
    const next = retargetPlan(p, target);
    for (const ms of [0, 200, 600, 849, 850]) expect(sampleAt(next, ms)).toEqual(sampleAt(p, ms));
    expect(p.input.target).toEqual(input.target);
    const b = rendered(next, next.ffTo);
    near(b.x, target.x); near(b.w, target.w * LETTERING_WIDTH);
    near(b.y + b.h / 2, target.y + target.h / 2);
    expect(next.ffTo).toBe(next.timeline.swapStart);
    expect(sampleAt(next, next.ffTo).stage).toBe('swap');
    expect(sampleAt(next, next.ffTo).word.opacity).toBe(1);
    expect(sampleAt(next, next.ffTo).logoOpacity).toBe(0);
    expect(next.done - next.ffTo).toBeLessThanOrEqual(150);
  });

  test('reduced motion and Escape stay stationary and fade within 600ms', () => {
    const p = buildPlan({ ...input, motion: 'reduced' });
    expect(skipPlan(input)).toEqual(p);
    expect(p.done).toBeLessThanOrEqual(600);
    expect(p.ffTo).toBe(p.timeline.swapStart);
    for (let ms = 0; ms <= p.done; ms += 25) {
      const s = sampleAt(p, ms);
      expect(rendered(p, ms)).toEqual(input.word);
      expect(s.word.opacity + s.logoOpacity).toBeCloseTo(1, 10);
    }
    expect(sampleAt(p, p.done)).toMatchObject({ stage: 'done', sentenceOpacity: 0, backdropOpacity: 0, logoOpacity: 1, word: { opacity: 0 } });
  });
});

test('plans are deterministic, immutable snapshots independent of DPR and sampling order', () => {
  const input = structuredClone(fixtures[0]!);
  const p = buildPlan(input), original = structuredClone(p);
  expect(buildPlan(input)).toEqual(p);
  const samples = [1500, 0, 1100, 600, 1410].map(ms => sampleAt(p, ms));
  expect([1500, 0, 1100, 600, 1410].map(ms => sampleAt(buildPlan(input), ms))).toEqual(samples);
  const hiDpi = buildPlan({ ...input, viewport: { ...input.viewport, dpr: 3 } });
  expect(sampleAt(hiDpi, 1100)).toEqual(sampleAt(p, 1100));
  Object.assign(input.word, { x: 999 }); Object.assign(input.target, { x: 999 });
  expect(p).toEqual(original);
  expect(Object.isFrozen(p.input.word)).toBe(true);
  expect(Object.isFrozen(input.word)).toBe(false);
});
