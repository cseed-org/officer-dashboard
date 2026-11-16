import { expect, test, type Page } from '@playwright/test';
import { front, openIntro, overlay, pressAnywhere } from './intro-helpers';
import { GREETINGS } from '../src/ui/onboarding/greetings';

/**
 * V0.12 hello screen (WelcomeIntro.tsx; the motion comes from introPlan.ts, unit-tested in
 * onboarding-plan.spec.ts). A liquid-glass greeting over the video slot (or its stand-in) in 15
 * languages with nothing else on screen; one press anywhere starts it, even before the page's
 * script has run; the glass word holds in the centre and glides to the corner, and the
 * wordmark lands exactly on the page logo with an opacity-only swap and no double draw.
 * Needs the Go server with DEV_AUTH=1 behind WEB_ORIGIN. INTRO_FRAMES=<dir> also saves frame strips.
 */
type Row = { t: number; state: string; shellVisible: boolean; logo: number; logoBox: number[]; mark: number; markBox: number[]; sapling: number; backdrop: number; shellBox: number[] };

/** Records, on every animation frame of /onboarding, what the hand-off depends on. */
const RECORDER = () => {
  if (location.pathname !== '/onboarding') return;
  type Row = { t: number; state: string; shellVisible: boolean; logo: number; logoBox: number[]; mark: number; markBox: number[]; sapling: number; backdrop: number; shellBox: number[] };
  const w = window as unknown as { __rows: Row[]; __dt: number[] };
  w.__rows = [];
  w.__dt = [];
  let prev = 0;
  let started = false;
  const rect = (e: Element | null | undefined) => {
    if (!e) return [];
    const b = e.getBoundingClientRect();
    return [b.left, b.top, b.width, b.height];
  };
  const shown = (root: Element | null) => (root ? Array.from(root.querySelectorAll('img, .wordmark-mark')).find((e) => getComputedStyle(e).display !== 'none') : undefined);
  const tick = (now: number) => {
    if (prev) w.__dt.push(now - prev);
    prev = now;
    const wi = document.querySelector<HTMLElement>('.wi');
    if (!wi && !started) { requestAnimationFrame(tick); return; }
    started = true;
    const sh = document.querySelector<HTMLElement>('[data-onb-logo]');
    const lg = document.querySelector<HTMLElement>('[data-wi-logo]');
    const mk = document.querySelector<HTMLElement>('[data-wi-word]');
    const sp = document.querySelector<HTMLElement>('[data-wi-logo-sapling]');
    const bg = document.querySelector<HTMLElement>('[data-wi-backdrop]');
    w.__rows.push({
      t: now,
      state: wi?.dataset.state ?? 'gone',
      shellVisible: !!sh && getComputedStyle(sh).visibility === 'visible',
      logo: lg ? +getComputedStyle(lg).opacity : -1,
      logoBox: rect(shown(lg)),
      mark: mk ? +getComputedStyle(mk).opacity : -1,
      markBox: rect(mk),
      sapling: sp ? +getComputedStyle(sp).opacity : -1,
      backdrop: bg ? +getComputedStyle(bg).opacity : -1,
      shellBox: rect(shown(sh)),
    });
    if (wi) requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);
};

test('only the intro shows: the glass greeting over the backdrop, no buttons, hints or photo', async ({ page }) => {
  await openIntro(page);
  await expect(front(page)).toHaveAttribute('data-lang', 'en');
  // Nothing to press but the dialog itself, and none of the V0.11 extras.
  await expect(overlay(page).locator('button, a, input, [role="button"]')).toHaveCount(0);
  await expect(overlay(page).locator('.wi-skip, .wi-hint, .wi-photo, figure')).toHaveCount(0);
  // Every visible thing in the overlay is one of its layers.
  const odd = await overlay(page).evaluate((root) => {
    const allowed = ['wi-paper', 'wi-backdrop', 'wi-standin', 'wi-poster', 'wi-video', 'wi-scrim', 'wi-greet', 'wi-slot', 'wi-text', 'wi-face', 'wi-glass', 'wi-sheen', 'wi-sheen-in', 'wi-shine', 'wi-cs', 'wi-artwork', 'wi-word', 'wi-word-glass', 'wi-word-ink', 'wi-logo', 'wordmark-logo', 'wordmark-mark', 'visually-hidden'];
    return Array.from(root.querySelectorAll<HTMLElement>('*'))
      .filter((e) => {
        const b = e.getBoundingClientRect();
        const cs = getComputedStyle(e);
        return b.width > 0 && b.height > 0 && cs.visibility !== 'hidden' && +cs.opacity > 0 && !e.closest('.sapling');
      })
      .filter((e) => !allowed.some((c) => e.classList.contains(c)))
      .map((e) => `${e.tagName.toLowerCase()}.${e.className}`);
  });
  expect(odd).toEqual([]);
  // The only visible words are the greeting (the screen reader gets one sentence instead).
  await expect(overlay(page)).toHaveAccessibleName(/Welcome to cseed/);
  await expect(page.locator('.wi .visually-hidden')).toHaveText('Hello, welcome to cseed');
  // Two phrase slots, not one layer per language; the page logo waits hidden under the overlay.
  await expect(page.locator('.wi-slot')).toHaveCount(2);
  await expect(page.locator('[data-onb-logo]')).toHaveCSS('visibility', 'hidden');
  // It is a real modal: the setup behind is inert and focus sits on the dialog.
  await expect(overlay(page)).toBeFocused();
  expect(await page.evaluate(() => document.querySelector('.onb')?.closest('[inert]') !== null)).toBe(true);
});

test('the overlay is server-rendered: it is the first paint, not the loading state', async ({ page }) => {
  await openIntro(page, {
    noWait: true,
    before: async (p) => {
      // Hold the island's script back: what shows now is the server's HTML alone.
      await p.route(/OnboardingPage/, async (route) => {
        await new Promise((f) => setTimeout(f, 1500));
        await route.continue();
      });
    },
  });
  await expect(overlay(page)).toBeVisible();
  expect(await page.evaluate(() => (window as { __wiReady?: boolean }).__wiReady ?? false)).toBe(false);
  await expect(front(page)).toHaveAttribute('data-lang', 'en');
  expect(await front(page).evaluate((e) => +getComputedStyle(e).opacity)).toBe(1);
  // What the eye meets anywhere on the screen is the intro, not the loading state behind it.
  const hits = await page.evaluate(() => [[0.5, 0.5], [0.1, 0.1], [0.9, 0.9]].map(([x, y]) => !!document.elementFromPoint(innerWidth * x!, innerHeight * y!)?.closest('.wi')));
  expect(hits).toEqual([true, true, true]);
});

for (const how of ['key', 'press', 'escape'] as const) {
  test(`a ${how} made before the page's script runs is not lost`, async ({ page }, info) => {
    await openIntro(page, {
      noWait: true,
      before: async (p) =>
        p.route(/OnboardingPage/, async (route) => {
          await new Promise((f) => setTimeout(f, 1200));
          await route.continue();
        }),
    });
    await expect(overlay(page)).toBeVisible();
    expect(await page.evaluate(() => (window as { __wiReady?: boolean }).__wiReady ?? false)).toBe(false);
    if (how === 'key') await page.keyboard.press('k');
    else if (how === 'escape') await page.keyboard.press('Escape');
    else await pressAnywhere(page, info);
    // Replayed once the page is interactive: it plays (or skips) and ends on the logo.
    await expect(overlay(page)).toHaveCount(0, { timeout: 15_000 });
    await expect(page.locator('[data-onb-logo]')).toHaveCSS('visibility', 'visible');
  });
}

test('one press starts it, a second fast-forwards to the swap, and Escape takes the short fade', async ({ page }, info) => {
  const shell = await openIntro(page, { before: (p) => p.addInitScript(RECORDER) });
  await pressAnywhere(page, info);
  await expect(overlay(page)).toHaveAttribute('data-state', 'playing');
  await expect(page.locator('[data-wi-word]')).toBeVisible();
  await page.waitForTimeout(800); // clearly a separate press, not the first one's double (R-1)
  const t1 = await page.evaluate(() => performance.now());
  await pressAnywhere(page, info);
  await expect(overlay(page)).toHaveCount(0, { timeout: 1500 });
  // It jumped to the swap and the swap still played: frames with the landing logo part-way in, in its box.
  const rows = (await page.evaluate(() => (window as unknown as { __rows: Row[] }).__rows)).filter((r) => r.t >= t1 && r.state !== 'gone');
  const swap = rows.filter((r) => r.logo > 0 && r.logo < 1);
  if (shell.kind === 'wordmark') expect(swap.length, 'frames of the swap after the second press').toBeGreaterThan(0);
  for (const r of swap) r.logoBox.forEach((v, i) => expect(Math.abs(v - shell.box[i]!)).toBeLessThan(0.6));
  expect(rows.at(-1)!.logo).toBeGreaterThan(0.95);

  await page.goto('about:blank');
  await openIntro(page);
  // Escape starts the stationary short plan in the same task.
  const built = await page.evaluate(() => {
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
    return { state: document.querySelector<HTMLElement>('.wi')!.dataset.state, parts: document.querySelectorAll('[data-wi-fruit], [data-wi-snake]').length };
  });
  expect(built.parts).toBe(0);
  await expect(overlay(page)).toHaveCount(0, { timeout: 1500 });
});

test('the glass word lifts without reflow, holds centred and scales uniformly into the logo', async ({ page }) => {
  await openIntro(page, { before: p => p.clock.install() });
  await page.waitForFunction(() => window.__wiReady === true);
  await page.clock.pauseAt((await page.evaluate(() => Date.now())) + 50);
  const before = await front(page).locator('.wi-face .wi-cs').boundingBox();
  expect(before).not.toBeNull();
  await page.evaluate(() => document.querySelector('.wi')!.dispatchEvent(new PointerEvent('pointerup', { bubbles: true, button: 0 })));
  const word = page.locator('[data-wi-word]');
  const lifted = await word.boundingBox();
  for (const key of ['x', 'y', 'width', 'height'] as const) expect(Math.abs(lifted![key] - before![key]), key).toBeLessThan(.5);
  await expect(front(page).locator('.wi-cs').first()).toHaveCSS('visibility', 'hidden');
  await expect(word.locator('.wi-word-glass')).toHaveCSS('visibility', 'visible');
  await page.clock.runFor(700);
  const hold = (await word.boundingBox())!, vp = page.viewportSize()!;
  expect(Math.abs(hold.x + hold.width / 2 - vp.width / 2)).toBeLessThan(.5);
  expect(Math.abs(hold.y + hold.height / 2 - vp.height / 2)).toBeLessThan(.5);
  expect(hold.width).toBeCloseTo(before!.width, 1);
  expect(hold.height).toBeCloseTo(before!.height, 1);
  await page.clock.runFor(400);
  const glide = (await word.boundingBox())!;
  expect(glide.width / before!.width).toBeCloseTo(glide.height / before!.height, 4);
  expect(glide.width).toBeLessThan(hold.width);
  await page.clock.runFor(300); // includes the next scheduled animation frame
  await expect(page.locator('[data-wi-backdrop]')).toHaveCSS('opacity', '0');
  await page.clock.runFor(200);
  await expect(overlay(page)).toHaveCount(0);
});

test('Escape during the glide freezes the word and takes a short fade', async ({ page }) => {
  await openIntro(page, { before: p => p.clock.install() });
  await page.waitForFunction(() => window.__wiReady === true);
  await page.clock.pauseAt((await page.evaluate(() => Date.now())) + 50);
  await page.keyboard.press('Enter');
  await page.clock.runFor(1050);
  await page.keyboard.press('Escape');
  await page.clock.runFor(20);
  const held = await page.locator('[data-wi-word]').boundingBox();
  await page.clock.runFor(200);
  expect(await page.locator('[data-wi-word]').boundingBox()).toEqual(held);
  await page.clock.runFor(310);
  await expect(overlay(page)).toHaveCount(0);
});

test('without a destination the intro fades away without drawing a fake logo', async ({ page }) => {
  await openIntro(page);
  // The overlay is server-rendered; wait for hydration and account loading
  // before removing a destination that Preact would otherwise recreate.
  await expect(page.locator('.onb[data-step]')).toHaveAttribute('data-step', '1');
  await page.waitForFunction(() => window.__wiReady === true);
  await page.locator('[data-onb-logo]').evaluate(el => el.remove());
  await page.keyboard.press('Enter');
  await expect(page.locator('[data-wi-logo]')).toHaveCSS('opacity', '0');
  await expect(page.locator('[data-wi-logo-sapling]')).toHaveCSS('opacity', '0');
  await expect(overlay(page)).toHaveCount(0, { timeout: 1000 });
});

test('keys: letters, Enter and Space start it; F-keys, Tab and shortcuts are left to the browser', async ({ page }) => {
  await openIntro(page);
  const r = await page.evaluate(() =>
    [
      { key: 'F5' },
      { key: 'F11' },
      { key: 'r', ctrlKey: true },
      { key: 'l', metaKey: true },
      { key: 'Shift' },
      { key: 'MediaPlayPause' },
    ].map((k) => {
      const e = new KeyboardEvent('keydown', { ...k, cancelable: true, bubbles: true });
      window.dispatchEvent(e);
      return [k.key, e.defaultPrevented];
    }),
  );
  expect(r).toEqual([
    ['F5', false],
    ['F11', false],
    ['r', false],
    ['l', false],
    ['Shift', false],
    ['MediaPlayPause', false],
  ]);
  await expect(overlay(page)).toHaveAttribute('data-state', 'greeting');
  // Tab keeps focus on the dialog (everything behind it is inert).
  await page.keyboard.press('Tab');
  await expect(overlay(page)).toBeFocused();
  await page.keyboard.press('Space');
  await expect(overlay(page)).toHaveAttribute('data-state', 'playing');
  // Afterwards focus lands on the step heading.
  await page.waitForTimeout(800);
  await page.keyboard.press('Enter');
  await expect(overlay(page)).toHaveCount(0, { timeout: 2000 });
  await expect(page.locator('#step-title')).toBeFocused();
});

test('a swipe starts it on a touch screen', async ({ page }, info) => {
  test.skip(!info.project.use.hasTouch, 'touch only');
  await openIntro(page);
  const cdp = await page.context().newCDPSession(page);
  const at = (x: number, y: number) => [{ x, y, id: 1 }];
  await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: at(120, 600) });
  for (let y = 560; y >= 300; y -= 40) await cdp.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: at(130, y) });
  await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
  await expect(overlay(page)).toHaveAttribute('data-state', 'playing');
});

test('all 15 languages render in the glass, with no mark clipped', async ({ page }, info) => {
  test.skip(info.project.name === 'tablet', 'phone and desktop widths cover the line breaks');
  await openIntro(page, { before: (p) => p.clock.install() });
  // The cycle's timer starts once the island is interactive.
  await page.waitForFunction(() => (window as { __wiReady?: boolean }).__wiReady === true);
  // The installed clock still runs in real time: stop it, so each language stays until the next step.
  await page.clock.pauseAt((await page.evaluate(() => Date.now())) + 50);
  // Hold the stand-in still so the two screenshots of each language differ only in the type.
  await page.addStyleTag({ content: '.wi-standin {animation-play-state: paused !important;}' });
  const band = page.locator('[data-wi-greeting]');
  const seen: string[] = [];
  const bandH = await band.evaluate((e) => e.getBoundingClientRect().height);
  expect(bandH).toBeGreaterThan(0);
  for (let i = 0; i < GREETINGS.length; i++) {
    if (i > 0) {
      await page.clock.runFor(2100);
      await page.waitForTimeout(700); // the slot's own fade (Web Animations run on real time)
    }
    const lang = (await front(page).getAttribute('data-lang'))!;
    seen.push(lang);
    await expect(front(page)).toHaveCSS('opacity', '1');
    // Its script's subset font is in (the comparison needs the same glyphs in both screenshots).
    const text = GREETINGS.find((g) => g.lang === lang)!.text.replace('{cseed}', 'cseed');
    await expect.poll(() => page.evaluate((t) => document.fonts.check('500 40px "cseed Hello"', t), text)).toBe(true);
    const glass = await band.screenshot();
    const solid = await (async () => {
      const tag = await page.addStyleTag({ content: '.wi-glass {color: #fff !important; background: none !important;}' });
      const shot = await band.screenshot();
      await tag.evaluate((t) => (t as HTMLElement).remove());
      return shot;
    })();
    await info.attach(`${lang}.png`, { body: glass, contentType: 'image/png' });
    if (process.env.INTRO_FRAMES) await band.screenshot({ path: `${process.env.INTRO_FRAMES}/${info.project.name}-glass-${String(i).padStart(2, '0')}-${lang}.png` });
    // Every pixel that is ink in solid type must also be lit in the glass: a clipped mark would show the backdrop.
    const r = await page.evaluate(
      async ([g, s]) => {
        const load = (b64: string) =>
          new Promise<Uint8ClampedArray>((res, rej) => {
            const img = new Image();
            img.onload = () => {
              const c = document.createElement('canvas');
              c.width = img.width;
              c.height = img.height;
              const x = c.getContext('2d')!;
              x.drawImage(img, 0, 0);
              res(x.getImageData(0, 0, img.width, img.height).data);
            };
            img.onerror = rej;
            img.src = `data:image/png;base64,${b64}`;
          });
        const [a, b] = await Promise.all([load(g), load(s)]);
        const lum = (d: Uint8ClampedArray, i: number) => 0.2126 * d[i]! + 0.7152 * d[i + 1]! + 0.0722 * d[i + 2]!;
        let ink = 0;
        let lost = 0;
        for (let i = 0; i < b.length; i += 4) {
          if (lum(b, i) < 215) continue;
          ink++;
          if (lum(a, i) < 115) lost++;
        }
        return { ink, lost };
      },
      [glass.toString('base64'), solid.toString('base64')] as const,
    );
    expect(r.ink, `${lang}: ink found`).toBeGreaterThan(200);
    expect(r.lost / r.ink, `${lang}: ${r.lost} of ${r.ink} ink pixels missing from the glass`).toBeLessThan(0.01);
    // The band never changes size, whatever the language.
    expect(Math.abs((await band.evaluate((e) => e.getBoundingClientRect().height)) - bandH), `${lang}: band height`).toBeLessThan(0.5);
  }
  expect(seen).toEqual(GREETINGS.map((g) => g.lang));
});

test('video slot without a file: the stand-in plays, and no video is requested', async ({ page }) => {
  const asked: string[] = [];
  page.on('request', (r) => r.url().includes('/media/') && asked.push(r.url()));
  await openIntro(page);
  await expect(page.locator('[data-wi-standin]')).toBeVisible();
  await expect(page.locator('[data-wi-video]')).toHaveCount(0);
  await expect(overlay(page)).toHaveAttribute('data-video', 'off');
  expect(asked).toEqual([]);
});

/** A one-second silent WebM made in the browser (Playwright's Chromium has no H.264, and this needs no fixture file). */
async function makeClip(page: Page): Promise<Buffer> {
  await page.goto('about:blank');
  const b64 = await page.evaluate(
    () =>
      new Promise<string>((resolve) => {
        const c = document.createElement('canvas');
        c.width = 320;
        c.height = 180;
        const x = c.getContext('2d')!;
        const rec = new MediaRecorder(c.captureStream(30), { mimeType: 'video/webm' });
        const parts: Blob[] = [];
        rec.ondataavailable = (e) => parts.push(e.data);
        rec.onstop = async () => {
          const buf = new Uint8Array(await new Blob(parts).arrayBuffer());
          let s = '';
          for (const b of buf) s += String.fromCharCode(b);
          resolve(btoa(s));
        };
        let f = 0;
        const draw = () => {
          x.fillStyle = `hsl(${(f++ * 6) % 360} 60% 40%)`;
          x.fillRect(0, 0, 320, 180);
          if (rec.state === 'recording') requestAnimationFrame(draw);
        };
        rec.start();
        draw();
        setTimeout(() => rec.stop(), 1000);
      }),
  );
  return Buffer.from(b64, 'base64');
}

/**
 * Makes /onboarding behave as if `hello-loop.mp4` had been in public/media at build time (the island's
 * `media.video` prop is set before it hydrates), and answers the file with `body`. The page itself is not
 * fulfilled by the test: Chromium would then treat it as public and block everything it loads locally.
 */
const withVideo = (body: Buffer | null) => async (page: Page) => {
  await page.addInitScript(() => {
    if (location.pathname !== '/onboarding') return;
    new MutationObserver((_, obs) => {
      const island = document.querySelector('astro-island[component-url*="OnboardingPage"]');
      const props = island?.getAttribute('props');
      if (!island || !props) return;
      island.setAttribute('props', props.replace('"video":[0]', '"video":[0,"/media/hello-loop.mp4"]'));
      obs.disconnect();
    }).observe(document, { childList: true, subtree: true });
  });
  await page.route('**/media/hello-loop.mp4', (route) => (body ? route.fulfill({ body, contentType: 'video/webm' }) : route.fulfill({ status: 404, body: '' })));
};

test('video slot with a file: muted looping video fades in once playing, and pauses at the end of the glide', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop', 'one width is enough');
  const clip = await makeClip(page);
  await openIntro(page, { before: withVideo(clip) });
  const v = page.locator('[data-wi-video]');
  await expect(v).toHaveCount(1);
  const attrs = await v.evaluate((e: HTMLVideoElement) => ({ muted: e.muted, loop: e.loop, autoplay: e.autoplay, inline: e.playsInline, src: e.getAttribute('src') }));
  expect(attrs).toEqual({ muted: true, loop: true, autoplay: true, inline: true, src: '/media/hello-loop.mp4' });
  await expect.poll(() => v.evaluate((e: HTMLVideoElement) => !e.paused && +getComputedStyle(e).opacity)).toBe(1);
  // object-fit: cover crops the one landscape video to any screen.
  await expect(v).toHaveCSS('object-fit', 'cover');
  await v.evaluate((el: HTMLVideoElement) => {
    const w = window as unknown as { __videoPauseBackdrop?: number };
    el.addEventListener('pause', () => {
      const backdrop = document.querySelector('[data-wi-backdrop]');
      if (backdrop) w.__videoPauseBackdrop = +getComputedStyle(backdrop).opacity;
    }, { once: true });
  });
  await page.keyboard.press('Enter');
  // The paused video is removed only 150ms later. Observe the event instead of
  // polling an element whose entire paused lifetime can fall between polls.
  await expect.poll(() => page.evaluate(() => (window as unknown as { __videoPauseBackdrop?: number }).__videoPauseBackdrop), { timeout: 3000 }).toBe(0);
});

test('video slot with a broken file: it falls back to the stand-in', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop', 'one width is enough');
  await openIntro(page, { before: withVideo(null) });
  await expect(overlay(page)).toHaveAttribute('data-video', 'off', { timeout: 5000 });
  await expect(page.locator('[data-wi-video]')).toHaveCount(0);
  await expect(page.locator('[data-wi-standin]')).toBeVisible();
});

for (const theme of ['light', 'dark'] as const) {
  test(`${theme}: it lands exactly on the page logo, with an opacity-only swap and no double draw`, async ({ page }, info) => {
    const shell = await openIntro(page, { theme, before: (p) => p.addInitScript(RECORDER) });
    const pageLogo = await page.locator('[data-onb-logo]').evaluate((e) => {
      const b = e.getBoundingClientRect();
      return [b.left, b.top, b.width, b.height];
    });
    pageLogo.forEach((v, i) => expect(Math.abs(v - shell.box[i]!), `page ${pageLogo} vs shell ${shell.box}`).toBeLessThan(0.6));
    await expect(page.locator('[data-onb-logo]')).toHaveAttribute('data-kind', shell.kind);
    const t0 = await page.evaluate(() => performance.now());
    await pressAnywhere(page, info);
    await expect(overlay(page)).toHaveCount(0, { timeout: 15_000 });
    await page.waitForTimeout(200);
    const rows = (await page.evaluate(() => (window as unknown as { __rows: Row[] }).__rows)).filter((r) => r.t >= t0);
    const gone = rows.findIndex((r) => r.state === 'gone');
    expect(gone).toBeGreaterThan(0);
    // About 1.5 s from the press to the page (D-28), including input delivery and the last paint.
    expect(rows[gone]!.t - t0).toBeGreaterThanOrEqual(1400);
    expect(rows[gone]!.t - t0).toBeLessThan(1750);
    // Never two logos: the page logo stays hidden for as long as the overlay draws one.
    for (const r of rows) if (r.state !== 'gone') expect(r.shellVisible, `frame at ${Math.round(r.t - t0)} ms`).toBe(false);
    for (const r of rows.slice(gone)) expect(r.shellVisible).toBe(true);
    // The last overlay frame shows its logo fully, in the page logo's exact box; the next frame is the page's own.
    const last = rows[gone - 1]!;
    expect(last.logo).toBeGreaterThan(0.95);
    last.logoBox.forEach((v, i) => expect(Math.abs(v - shell.box[i]!), `intro ${last.logoBox} vs shell ${shell.box}`).toBeLessThan(0.6));
    rows[gone]!.shellBox.forEach((v, i) => expect(Math.abs(v - shell.box[i]!)).toBeLessThan(0.6));
    // Real logo copies never move or scale. The wordmark swap starts only after the
    // glass letters have landed; the rail deliberately fades during the glide.
    const swap = rows.filter((r) => r.logo > 0 && r.logo < 1 && r.state !== 'gone');
    expect(swap.length).toBeGreaterThan(0);
    if (shell.kind === 'wordmark') expect(swap.at(-1)!.t - swap[0]!.t).toBeLessThan(160);
    for (const r of swap) {
      r.logoBox.forEach((v, i) => expect(Math.abs(v - shell.box[i]!)).toBeLessThan(0.6));
      expect(r.mark + r.logo).toBeCloseTo(1, 2);
      if (shell.kind === 'wordmark') {
        const targetLetters = [shell.box[0]!, shell.box[1]!, shell.box[2]! * 471 / 640, shell.box[3]!];
        r.markBox.forEach((v, i) => expect(Math.abs(v - targetLetters[i]!), `glass ${r.markBox} vs letters ${targetLetters}`).toBeLessThan(0.5));
        expect(r.backdrop).toBe(0);
      }
    }
    if (shell.kind === 'wordmark') {
      for (const r of swap) expect(r.sapling).toBeCloseTo(r.logo * 120 / 150, 2);
      expect(last.sapling).toBe(1);
    }
    expect(last.backdrop).toBe(0);
  });
}

test('prefers-reduced-motion: the poster or stand-in stands still, languages cross-fade, and a press fades to the logo', async ({ page }, info) => {
  const shell = await openIntro(page, { reduced: true, before: (p) => p.clock.install() });
  await expect(page.locator('[data-wi-standin]')).toHaveCSS('animation-name', 'none');
  await expect(page.locator('[data-wi-video]')).toHaveCount(0);
  // The cycle's timer starts once the island is interactive.
  await page.waitForFunction(() => (window as { __wiReady?: boolean }).__wiReady === true);
  await page.clock.runFor(4200);
  // The language change is a plain cross-fade: opacity only, no movement.
  const frames = await page.locator('.wi-slot').evaluateAll((els) => els.flatMap((e) => e.getAnimations().map((a) => (a.effect as KeyframeEffect).getKeyframes())));
  expect(frames.length).toBeGreaterThan(0);
  for (const kf of frames) for (const f of kf) expect(f.transform).toBeUndefined();
  await page.clock.resume();
  const t0 = Date.now();
  await pressAnywhere(page, info);
  await expect(overlay(page)).toHaveCount(0, { timeout: 3000 });
  expect(Date.now() - t0).toBeLessThan(1500);
  await expect(page.locator('[data-wi-fruit], [data-wi-snake]')).toHaveCount(0);
  const p = await page.locator('[data-onb-logo]').evaluate((e) => {
    const b = e.getBoundingClientRect();
    return [b.left, b.top, b.width, b.height];
  });
  p.forEach((v, i) => expect(Math.abs(v - shell.box[i]!)).toBeLessThan(0.6));
});

// Six contact sheets: each screenshot freezes the actual screen at a known plan time.
for (const theme of ['light', 'dark'] as const) {
  test(`frame strips for the PR (${theme})`, async ({ page }, info) => {
    const dir = process.env.INTRO_FRAMES;
    test.skip(!dir, 'set INTRO_FRAMES=<dir> to record frames');
    await openIntro(page, { theme, before: p => p.clock.install() });
    await page.waitForFunction(() => window.__wiReady === true);
    await page.clock.pauseAt((await page.evaluate(() => Date.now())) + 50);
    await page.evaluate(() => document.fonts.ready);
    const width = page.viewportSize()!.width;
    const shots: { name: string; data: string }[] = [];
    const shot = async (name: string) => {
      const bytes = await page.screenshot({ path: `${dir}/${width}-${theme}-${name}.png` });
      shots.push({ name, data: bytes.toString('base64') });
    };
    await shot('00-greeting');
    await page.evaluate(() => document.querySelector('.wi')!.dispatchEvent(new PointerEvent('pointerup', { bubbles: true, button: 0 })));
    let previous = 0;
    for (const [at, name] of [[300, '01-melt'], [700, '02-hold'], [1100, '03-glide'], [1400, '04-swap'], [2100, '05-landed']] as const) {
      await page.clock.runFor(at - previous);
      previous = at;
      await shot(name);
    }
    const sheet = await page.evaluate(async (frames) => {
      const imgs = await Promise.all(frames.map(f => new Promise<HTMLImageElement>((resolve) => {
        const img = new Image(); img.onload = () => resolve(img); img.src = `data:image/png;base64,${f.data}`;
      })));
      const cellW = 360, cellH = Math.round(imgs[0]!.height * cellW / imgs[0]!.width), label = 28;
      const canvas = document.createElement('canvas'); canvas.width = cellW * 3; canvas.height = (cellH + label) * 2;
      const ctx = canvas.getContext('2d')!; ctx.fillStyle = '#202020'; ctx.fillRect(0, 0, canvas.width, canvas.height);
      ctx.font = '14px sans-serif'; ctx.fillStyle = '#ffffff';
      imgs.forEach((img, i) => { const x = (i % 3) * cellW, y = Math.floor(i / 3) * (cellH + label); ctx.fillText(frames[i]!.name, x + 8, y + 19); ctx.drawImage(img, x, y + label, cellW, cellH); });
      return canvas.toDataURL('image/png').split(',')[1]!;
    }, shots);
    const { writeFile } = await import('node:fs/promises');
    await writeFile(`${dir}/${width}-${theme}-strip.png`, Buffer.from(sheet, 'base64'));
  });
}

test('a double-click or a held key is one press: it does not skip the intro (R-1)', async ({ page }, info) => {
  test.skip(!!info.project.use.hasTouch, 'mouse and keyboard');
  await openIntro(page);
  await page.mouse.dblclick(400, 500);
  await page.waitForTimeout(1200);
  await expect(overlay(page)).toHaveAttribute('data-state', 'playing');

  await page.goto('about:blank');
  await openIntro(page);
  // Holding Enter: the browser sends repeats (repeat: true) after a moment.
  await page.keyboard.down('Enter');
  await page.waitForTimeout(500);
  await page.keyboard.down('Enter');
  await page.keyboard.down('Enter');
  await page.waitForTimeout(700);
  await page.keyboard.up('Enter');
  await expect(overlay(page)).toHaveAttribute('data-state', 'playing');
});

test('a press during a language change melts the greeting from where it is, with no jump (R-3)', async ({ page }, info) => {
  await openIntro(page);
  // Wait for a change to be under way, then press in the same task and look at the next frame.
  const r = await page.evaluate(
    () =>
      new Promise<{ before: Record<string, number>; after: Record<string, number>; liftedLangs: string[] }>((resolve) => {
        const slots = () => Array.from(document.querySelectorAll<HTMLElement>('.wi-greet > .wi-slot'));
        const read = () => Object.fromEntries(slots().map((e) => [e.dataset.lang!, +getComputedStyle(e).opacity]));
        const poll = () => {
          const a = slots().flatMap((e) => e.getAnimations()).find((x) => (x.currentTime as number) > 60 && (x.currentTime as number) < 200);
          if (!a) return requestAnimationFrame(poll);
          const before = read();
          document.querySelector('.wi')!.dispatchEvent(new PointerEvent('pointerup', { bubbles: true, button: 0 }));
          const liftedLangs = slots().filter(e => e.hasAttribute('data-lifted')).map(e => e.dataset.lang!);
          requestAnimationFrame(() => resolve({ before, after: read(), liftedLangs }));
        };
        poll();
      }),
  );
  const shown = Object.entries(r.before).filter(([, o]) => o > 0.05);
  expect(shown.length, `two phrases part-way through the change ${JSON.stringify(r.before)}`).toBe(2);
  // The more visible phrase supplies the lifted word; both phrases melt from where they were, and neither
  // jumps on the press frame.
  const top = shown.reduce((a, b) => (b[1] > a[1] ? b : a))[0];
  expect(r.liftedLangs).toEqual([top]);
  for (const [lang, o] of shown) expect(Math.abs(r.after[lang]! - o), `${lang}: ${o} → ${r.after[lang]}`).toBeLessThan(0.2);
  await expect(overlay(page)).toHaveAttribute('data-state', 'playing');
});

test('a resize mid-run jumps to the end on the re-measured page logo (D-15)', async ({ page }, info) => {
  await openIntro(page, { before: (p) => p.addInitScript(RECORDER) });
  const vp = page.viewportSize()!;
  await pressAnywhere(page, info);
  await page.waitForTimeout(500);
  await page.setViewportSize({ width: vp.height > vp.width ? vp.height : Math.round(vp.width * 0.7), height: vp.height > vp.width ? vp.width : vp.height });
  await expect(overlay(page)).toHaveCount(0, { timeout: 2000 });
  await page.waitForFunction(() => (window as unknown as { __rows: Row[] }).__rows.some(r => r.state === 'gone'));
  const rows = await page.evaluate(() => (window as unknown as { __rows: Row[] }).__rows);
  const gone = rows.findIndex((r) => r.state === 'gone');
  const last = rows[gone - 1]!;
  const box = await page.locator('[data-onb-logo]').evaluate((e) => {
    const shownLogo = Array.from(e.querySelectorAll('img, .wordmark-mark')).find((x) => getComputedStyle(x).display !== 'none')!;
    const b = shownLogo.getBoundingClientRect();
    return [b.left, b.top, b.width, b.height];
  });
  expect(last.logo).toBeGreaterThan(0.95);
  last.logoBox.forEach((v, i) => expect(Math.abs(v - box[i]!), `intro ${last.logoBox} vs page ${box}`).toBeLessThan(0.6));
});

test('the melt ends on the page\'s own colour: the paper matches the page behind (B-14)', async ({ page }, info) => {
  for (const theme of ['light', 'dark'] as const) {
    if (theme === 'dark') await page.goto('about:blank');
    await openIntro(page, { theme });
    const c = await page.evaluate(() => {
      const paper = getComputedStyle(document.querySelector('.wi-paper')!).backgroundColor;
      let el: Element | null = document.querySelector('.onb');
      let bg = 'rgba(0, 0, 0, 0)';
      while (el && (bg === 'rgba(0, 0, 0, 0)' || bg === 'transparent')) {
        bg = getComputedStyle(el).backgroundColor;
        el = el.parentElement;
      }
      return { paper, bg };
    });
    expect(c.paper, theme).toBe(c.bg);
    await pressAnywhere(page, info);
    await expect(overlay(page)).toHaveCount(0, { timeout: 15_000 });
  }
});

test('no hello screen flash for a Google return or an account that has seen it (R-4)', async ({ page }) => {
  // Record whether the overlay was ever drawable: its display when it enters the document, before any paint.
  const watch = () =>
    page.addInitScript(() => {
      if (location.pathname !== '/onboarding') return;
      const w = window as unknown as { __wiShown: boolean[] };
      w.__wiShown = [];
      new MutationObserver(() => {
        const wi = document.querySelector('.wi');
        if (wi && w.__wiShown.length === 0) w.__wiShown.push(getComputedStyle(wi).display !== 'none');
      }).observe(document, { childList: true, subtree: true });
    });
  await openIntro(page);
  await page.keyboard.press('Escape');
  await expect(overlay(page)).toHaveCount(0, { timeout: 3000 });
  await watch();
  // The same person comes back to /onboarding (a reload, or a return from Google's consent screen).
  for (const url of ['/onboarding', '/onboarding?google=denied']) {
    await page.goto(url);
    await expect(page.locator('#step-title')).toBeVisible();
    expect(await page.evaluate(() => (window as unknown as { __wiShown: boolean[] }).__wiShown), url).toEqual([false]);
    await expect(overlay(page)).toHaveCount(0);
    await expect(page.locator('[data-onb-logo]')).toHaveCSS('visibility', 'visible');
  }
});

test('another invitee on the same browser still gets the intro, faded in once their account loads', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop', 'one width is enough');
  await openIntro(page);
  await page.keyboard.press('Escape');
  await expect(overlay(page)).toHaveCount(0, { timeout: 3000 });
  await page.goto('about:blank');
  await openIntro(page);
  await expect(overlay(page)).toHaveAttribute('data-state', 'greeting');
  await pressAnywhere(page, info);
  await expect(overlay(page)).toHaveCount(0, { timeout: 15_000 });
});

for (const theme of ['light', 'dark'] as const) {
  test(`${theme}: lettering coverage stays constant through the opacity swap`, async ({ page }, info) => {
    test.skip(info.project.name === 'tablet', 'the rail fades between different shapes');
    await openIntro(page, { theme, before: p => p.clock.install() });
    await page.waitForFunction(() => window.__wiReady === true);
    await page.clock.pauseAt((await page.evaluate(() => Date.now())) + 50);
    const logo = await page.locator('[data-onb-logo]').boundingBox();
    const clip = { x: Math.floor(logo!.x), y: Math.floor(logo!.y), width: Math.ceil(logo!.width * 471 / 640), height: Math.ceil(logo!.height) };
    await page.evaluate(() => document.querySelector('.wi')!.dispatchEvent(new PointerEvent('pointerup', { bubbles: true, button: 0 })));
    await page.clock.runFor(1410);
    const opacity = await page.locator('[data-wi-logo]').evaluate(el => +getComputedStyle(el).opacity);
    expect(opacity).toBeGreaterThan(.15); expect(opacity).toBeLessThan(.85);
    const middle = await page.screenshot({ clip });
    await page.clock.runFor(220);
    await expect(overlay(page)).toHaveCount(0);
    const final = await page.screenshot({ clip });
    const result = await page.evaluate(async ({ a, b, theme }) => {
      const pixels = (data: string) => new Promise<Uint8ClampedArray>(resolve => {
        const image = new Image(); image.onload = () => {
          const canvas = document.createElement('canvas'); canvas.width = image.width; canvas.height = image.height;
          const ctx = canvas.getContext('2d')!; ctx.drawImage(image, 0, 0); resolve(ctx.getImageData(0, 0, image.width, image.height).data);
        }; image.src = `data:image/png;base64,${data}`;
      });
      const [mid, end] = await Promise.all([pixels(a), pixels(b)]);
      const lum = (px: Uint8ClampedArray, i: number) => .2126 * px[i]! + .7152 * px[i + 1]! + .0722 * px[i + 2]!;
      // Solid interiors of the delivered letters exclude antialiased edges and
      // the <0.5px resampling difference between the two logo asset resolutions.
      let count = 0, midContrast = 0, endContrast = 0;
      const bg = theme === 'light' ? Math.max(...Array.from({ length: end.length / 4 }, (_, i) => lum(end, i * 4)))
        : Math.min(...Array.from({ length: end.length / 4 }, (_, i) => lum(end, i * 4)));
      for (let i = 0; i < end.length; i += 4) {
        const e = lum(end, i), m = lum(mid, i);
        if (theme === 'light' ? e < 40 : e > 220) {
          count++; midContrast += Math.abs(m - bg); endContrast += Math.abs(e - bg);
        }
      }
      return { count, coverage: midContrast / endContrast };
    }, { a: middle.toString('base64'), b: final.toString('base64'), theme });
    expect(result.count).toBeGreaterThan(20);
    expect(result.coverage, JSON.stringify(result)).toBeGreaterThan(.92);
  });
}
