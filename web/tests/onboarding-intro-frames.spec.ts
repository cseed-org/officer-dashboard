import { expect, test } from '@playwright/test';
import { front, openIntro, overlay, pressAnywhere } from './intro-helpers';

/**
 * V0.12 hello intro: about 60 fps while it plays, also with the CPU four times slower (B-11, B-23).
 * Separate from onboarding-intro.spec.ts because Playwright's trace records the page as it runs and
 * costs frames itself; it can only be switched off for a whole file.
 */
test.use({ trace: 'off' });

for (const throttle of [1, 4] as const) {
  test(`frame times while it plays${throttle > 1 ? ` (CPU ${throttle}× slower)` : ''}`, async ({ page }, info) => {
    test.skip(info.project.name !== 'desktop' && throttle > 1, 'the throttled run is desktop only');
    // Only frame intervals are recorded here (the hand-off recorder reads layout every frame and would itself cost frames).
    await openIntro(page, {
      before: (p) =>
        p.addInitScript(() => {
          if (location.pathname !== '/onboarding') return;
          const w = window as unknown as { __dt: number[]; __at: number[] };
          w.__dt = [];
          w.__at = [];
          // The rAF timestamp is vsync-aligned and hides frames up to about 30 ms (R-6): measure the wall clock
          // at the start of each callback instead, which sees how late a frame really was.
          let prev = 0;
          const tick = () => {
            const now = performance.now();
            if (document.querySelector('.wi')) {
              if (prev) {
                w.__dt.push(now - prev);
                w.__at.push(now);
              }
              prev = now;
            }
            requestAnimationFrame(tick);
          };
          requestAnimationFrame(tick);
        }),
    });
    const cdp = await page.context().newCDPSession(page);
    if (throttle > 1) await cdp.send('Emulation.setCPUThrottlingRate', { rate: throttle });
    // A visitor reads the first greeting before pressing (by then the plan is built in idle time).
    await expect(front(page)).not.toHaveAttribute('data-lang', 'en', { timeout: 5000 });
    const t0 = await page.evaluate(() => {
      const w = window as unknown as { __dt: number[]; __at: number[] };
      w.__dt.length = 0;
      w.__at.length = 0;
      return performance.now();
    });
    await pressAnywhere(page, info);
    await expect(overlay(page)).toHaveCount(0, { timeout: 20_000 });
    const { dt, at } = await page.evaluate(() => {
      const w = window as unknown as { __dt: number[]; __at: number[] };
      return { dt: w.__dt, at: w.__at };
    });
    const long = dt.flatMap((d, i) => (d > 20.5 ? [`${Math.round(at[i]! - t0)}ms:${Math.round(d)}`] : []));
    if (throttle > 1) await cdp.send('Emulation.setCPUThrottlingRate', { rate: 1 });
    const sorted = [...dt].sort((a, b) => a - b);
    const over = dt.filter((d) => d > 20.5).length;
    const p95 = sorted[Math.floor(sorted.length * 0.95)]!;
    info.annotations.push({ type: 'frames', description: `${dt.length} frames, ${over} over 20 ms (${long.join(', ')}), p95 ${p95.toFixed(1)} ms, max ${sorted.at(-1)!.toFixed(1)} ms` });
    expect(dt.length).toBeGreaterThan(200);
    // About 60 fps: 95% of frames within ~20 ms, at most one in twenty-five over it, and no long stall. These are
    // wall-clock gaps in headless Chromium with software compositing (no GPU), which shows a few 20–45 ms frames
    // around the press and the fruit even with the V0.12 screen as first committed (a50b705) on the current
    // engine: the budget leaves room for that noise, not for real stalls (a 200 ms one fails it).
    expect(over / dt.length, `${over} of ${dt.length} frames over 20 ms at ${long.join(', ')}`).toBeLessThan(0.04);
    expect(p95).toBeLessThan(20.5);
    expect(sorted.at(-1)!, `longest frame; long ones at ${long.join(', ')}`).toBeLessThan(throttle > 1 ? 70 : 50);
  });
}


test('while it plays, only transform, opacity and visibility change (no layout per frame)', async ({ page }, info) => {
  await openIntro(page);
  await expect(front(page)).not.toHaveAttribute('data-lang', 'en', { timeout: 5000 });
  // Watch every inline style change inside the overlay from the press to the end.
  await page.evaluate(() => {
    const w = window as unknown as { __changes: string[]; __t0: number };
    w.__changes = [];
    w.__t0 = performance.now();
    const props = (s: string | null) => {
      const d = document.createElement('div');
      d.setAttribute('style', s ?? '');
      return Object.fromEntries(Array.from(d.style).map((p) => [p, d.style.getPropertyValue(p)]));
    };
    new MutationObserver((list) => {
      for (const m of list) {
        if (m.type !== 'attributes') continue;
        const before = props(m.oldValue);
        const after = props((m.target as Element).getAttribute('style'));
        for (const p of new Set([...Object.keys(before), ...Object.keys(after)])) {
          if (before[p] === after[p] || ['transform', 'opacity', 'visibility'].includes(p)) continue;
          w.__changes.push(`${Math.round(performance.now() - w.__t0)}ms ${(m.target as Element).className} ${p}: ${before[p] ?? '-'} → ${after[p] ?? '-'}`);
        }
      }
    }).observe(document.querySelector('.wi')!, { subtree: true, attributes: true, attributeFilter: ['style'], attributeOldValue: true });
  });
  await pressAnywhere(page, info);
  await expect(overlay(page)).toHaveCount(0, { timeout: 15_000 });
  const changes = await page.evaluate(() => (window as unknown as { __changes: string[] }).__changes);
  // The first frames place things once (drips, the sprout, the landing box); after that, nothing but motion.
  const late = changes.filter((c) => parseInt(c, 10) > 120);
  expect(late, late.slice(0, 8).join('\n')).toEqual([]);
});
