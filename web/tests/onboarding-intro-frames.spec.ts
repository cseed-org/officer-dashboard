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
          const w = window as unknown as { __dt: number[]; __at: number[]; __t0: number };
          w.__dt = [];
          w.__at = [];
          // The rAF timestamp is vsync-aligned and hides frames up to about 30 ms (R-6): measure the wall clock
          // at the start of each callback instead, which sees how late a frame really was.
          let prev = 0;
          let measuring = false;
          let ended = false;
          const begin = () => {
            if (measuring || ended || !document.querySelector('.wi')) return;
            measuring = true;
            prev = w.__t0 = performance.now();
          };
          // Capture the actual first press, before the screen handles it. This
          // includes setup cost without counting Playwright's input dispatch delay.
          window.addEventListener('pointerup', begin, true);
          window.addEventListener('keydown', begin, true);
          const tick = () => {
            const now = performance.now();
            if (measuring) {
              if (prev) {
                w.__dt.push(now - prev);
                w.__at.push(now);
              }
              prev = now;
              if (!document.querySelector('.wi')) { measuring = false; ended = true; }
            }
            if (!ended) requestAnimationFrame(tick);
          };
          requestAnimationFrame(tick);
        }),
    });
    const cdp = await page.context().newCDPSession(page);
    if (throttle > 1) await cdp.send('Emulation.setCPUThrottlingRate', { rate: throttle });
    // A visitor reads the first greeting before pressing (the fonts and logo are decoded by then).
    await expect(front(page)).not.toHaveAttribute('data-lang', 'en', { timeout: 5000 });
    await page.waitForTimeout(700); // finish the language transition before measuring the exit
    await cdp.send('Performance.enable');
    const metricsBefore = await cdp.send('Performance.getMetrics');
    await pressAnywhere(page, info);
    await expect(overlay(page)).toHaveCount(0, { timeout: 20_000 });
    const { dt, at, t0 } = await page.evaluate(() => {
      const w = window as unknown as { __dt: number[]; __at: number[]; __t0: number };
      return { dt: w.__dt, at: w.__at, t0: w.__t0 };
    });
    const metricsAfter = await cdp.send('Performance.getMetrics');
    const mainThread = Object.fromEntries(metricsAfter.metrics
      .filter(m => /Duration$|LayoutCount|RecalcStyleCount/.test(m.name))
      .map(m => [m.name, m.value - (metricsBefore.metrics.find(b => b.name === m.name)?.value ?? 0)]));
    const long = dt.flatMap((d, i) => (d > 20 ? [`${Math.round(at[i]! - t0)}ms:${Math.round(d)}`] : []));
    if (throttle > 1) await cdp.send('Emulation.setCPUThrottlingRate', { rate: 1 });
    const sorted = [...dt].sort((a, b) => a - b);
    const over = dt.filter((d) => d > 20).length;
    const p95 = sorted[Math.floor(sorted.length * 0.95)]!;
    info.annotations.push({ type: 'frames', description: `${dt.length} frames, ${over} over 20 ms (${long.join(', ')}), p95 ${p95.toFixed(1)} ms, max ${sorted.at(-1)!.toFixed(1)} ms` });
    // D-29 acceptance: measure wall-clock callback gaps, including the press,
    // under the same 20ms budget with normal and 4x CPU. Keep the raw intervals
    // in evidence so host scheduling stalls cannot be hidden by a percentile.
    const evidence = JSON.stringify({ throttle, dt, at, over, p95, long, mainThread });
    const { writeFile } = await import('node:fs/promises');
    await writeFile(info.outputPath('frame-times.json'), evidence);
    await info.attach('frame-times.json', { body: evidence, contentType: 'application/json' });
    expect(dt.length).toBeGreaterThan(60);
    expect(sorted.at(-1)!, `longest frame; long ones at ${long.join(', ')}`).toBeLessThanOrEqual(20);

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
  // The press places the word and landing boxes once; after that, only composited properties change.
  // D-15 explicitly re-measures the destination at glide start; only its fixed
  // logo boxes may be placed then. Every other late mutation must be composited.
  const late = changes.filter((c) => parseInt(c, 10) > 120 && !/^8\d\dms wi-logo (left|top|width|height):/.test(c));
  expect(late, late.slice(0, 8).join('\n')).toEqual([]);
});
