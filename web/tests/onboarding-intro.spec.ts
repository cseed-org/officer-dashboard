import { expect, test, type Page } from '@playwright/test';
import { front, openIntro, overlay, pressAnywhere } from './intro-helpers';
import { GREETINGS } from '../src/ui/onboarding/greetings';

/**
 * V0.12 hello screen (WelcomeIntro.tsx; the motion comes from introPlan.ts, unit-tested in
 * onboarding-snake.spec.ts). A liquid-glass greeting over the video slot (or its stand-in) in 15
 * languages with nothing else on screen; one press anywhere starts it, even before the page's
 * script has run; it melts onto the page's paper, fruit and the snake play from the plan, and the
 * wordmark lands exactly on the page logo with an opacity-only swap and no double draw.
 * Needs the Go server with DEV_AUTH=1 behind WEB_ORIGIN. INTRO_FRAMES=<dir> also saves frame strips.
 */
type Row = { t: number; state: string; shellVisible: boolean; logo: number; logoBox: number[]; mark: number; markBox: number[]; sprout: number; shellBox: number[] };

/** Records, on every animation frame of /onboarding, what the hand-off depends on. */
const RECORDER = () => {
  if (location.pathname !== '/onboarding') return;
  type Row = { t: number; state: string; shellVisible: boolean; logo: number; logoBox: number[]; mark: number; markBox: number[]; sprout: number; shellBox: number[] };
  const w = window as unknown as { __rows: Row[]; __dt: number[] };
  w.__rows = [];
  w.__dt = [];
  let prev = 0;
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
    const sh = document.querySelector<HTMLElement>('[data-onb-logo]');
    const lg = document.querySelector<HTMLElement>('[data-wi-logo]');
    const mk = document.querySelector<HTMLElement>('[data-wi-word]');
    const sp = document.querySelector<HTMLElement>('[data-wi-sprout]');
    w.__rows.push({
      t: now,
      state: wi?.dataset.state ?? 'gone',
      shellVisible: !!sh && getComputedStyle(sh).visibility === 'visible',
      logo: lg ? +getComputedStyle(lg).opacity : -1,
      logoBox: rect(shown(lg)),
      mark: mk ? +getComputedStyle(mk).opacity : -1,
      markBox: rect(mk),
      sprout: sp ? +getComputedStyle(sp).opacity : -1,
      shellBox: rect(shown(sh)),
    });
    requestAnimationFrame(tick);
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
    const allowed = ['wi-paper', 'wi-backdrop', 'wi-standin', 'wi-poster', 'wi-video', 'wi-scrim', 'wi-greet', 'wi-slot', 'wi-text', 'wi-face', 'wi-glass', 'wi-sheen', 'wi-sheen-in', 'wi-shine', 'wi-cs', 'wi-layer', 'wi-eyes', 'wi-mark', 'wi-sprout', 'wi-logo', 'wordmark-logo', 'wordmark-mark', 'visually-hidden'];
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

test('one press starts it, a second fast-forwards to the swap, and Escape skips without fruit or snake', async ({ page }, info) => {
  const shell = await openIntro(page, { before: (p) => p.addInitScript(RECORDER) });
  await pressAnywhere(page, info);
  await expect(overlay(page)).toHaveAttribute('data-state', 'playing');
  await expect(page.locator('[data-wi-fruit]').first()).toBeVisible();
  await page.waitForTimeout(800); // clearly a separate press, not the first one's double (R-1)
  const t1 = await page.evaluate(() => performance.now());
  await pressAnywhere(page, info);
  await expect(overlay(page)).toHaveCount(0, { timeout: 1500 });
  // It jumped to the swap and the swap still played: frames with the landing logo part-way in, in its box.
  const rows = (await page.evaluate(() => (window as unknown as { __rows: Row[] }).__rows)).filter((r) => r.t >= t1 && r.state !== 'gone');
  const swap = rows.filter((r) => r.logo > 0 && r.logo < 1);
  expect(swap.length, 'frames of the swap after the second press').toBeGreaterThan(2);
  for (const r of swap) r.logoBox.forEach((v, i) => expect(Math.abs(v - shell.box[i]!)).toBeLessThan(0.6));
  expect(rows.at(-1)!.logo).toBeGreaterThan(0.95);

  await page.goto('about:blank');
  await openIntro(page);
  // Escape starts the short plan at once (in the same task): it never builds fruit or a snake.
  const built = await page.evaluate(() => {
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
    return { state: document.querySelector<HTMLElement>('.wi')!.dataset.state, parts: document.querySelectorAll('[data-wi-fruit], [data-wi-snake]').length };
  });
  expect(built.parts).toBe(0);
  await expect(overlay(page)).toHaveCount(0, { timeout: 1500 });
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

test('video slot with a file: muted looping video fades in once playing, and pauses after the melt', async ({ page }, info) => {
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
  await page.keyboard.press('Enter');
  await expect.poll(() => v.evaluate((e: HTMLVideoElement) => e.paused), { timeout: 3000 }).toBe(true);
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
    // One sapling only: the travelling wordmark's own sapling is cropped off; the separate one stands for it (R-2).
    expect(await page.locator('[data-wi-word] img').first().evaluate((e) => getComputedStyle(e).clipPath)).toMatch(/inset/);
    const t0 = await page.evaluate(() => performance.now());
    await pressAnywhere(page, info);
    await expect(overlay(page)).toHaveCount(0, { timeout: 15_000 });
    await page.waitForTimeout(200);
    const rows = (await page.evaluate(() => (window as unknown as { __rows: Row[] }).__rows)).filter((r) => r.t >= t0);
    const gone = rows.findIndex((r) => r.state === 'gone');
    expect(gone).toBeGreaterThan(0);
    // About 5 s from the press to the page (D-7).
    expect(rows[gone]!.t - t0).toBeLessThan(5600);
    // Never two logos: the page logo stays hidden for as long as the overlay draws one.
    for (const r of rows) if (r.state !== 'gone') expect(r.shellVisible, `frame at ${Math.round(r.t - t0)} ms`).toBe(false);
    for (const r of rows.slice(gone)) expect(r.shellVisible).toBe(true);
    // The last overlay frame shows its logo fully, in the page logo's exact box; the next frame is the page's own.
    const last = rows[gone - 1]!;
    expect(last.logo).toBeGreaterThan(0.95);
    last.logoBox.forEach((v, i) => expect(Math.abs(v - shell.box[i]!), `intro ${last.logoBox} vs shell ${shell.box}`).toBeLessThan(0.6));
    rows[gone]!.shellBox.forEach((v, i) => expect(Math.abs(v - shell.box[i]!)).toBeLessThan(0.6));
    // The swap is opacity only: while the travelling wordmark and the landing logo cross-fade, both sit in the
    // same box (the full wordmark onto a wordmark; on the rail the wordmark has faded out by then).
    const swap = rows.filter((r) => r.logo > 0 && r.logo < 1 && r.state !== 'gone');
    expect(swap.length).toBeGreaterThan(0);
    expect(swap.at(-1)!.t - swap[0]!.t).toBeLessThan(160);
    for (const r of swap) {
      r.logoBox.forEach((v, i) => expect(Math.abs(v - shell.box[i]!)).toBeLessThan(0.6));
      if (shell.kind === 'wordmark' && r.mark > 0.01) r.markBox.forEach((v, i) => expect(Math.abs(v - shell.box[i]!), `mark ${r.markBox}`).toBeLessThan(1));
      // Coverage stays full: what is landing is drawn over what was there, which stays opaque (no dimming).
      expect(r.sprout, `sapling at ${Math.round(r.t - t0)} ms`).toBeGreaterThan(0.97);
      if (shell.kind === 'wordmark') expect(r.mark, `wordmark at ${Math.round(r.t - t0)} ms`).toBeGreaterThan(0.97);
    }
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

// One test per theme: each gets a fresh browser context, so the saved theme preference applies.
for (const theme of ['light', 'dark'] as const) {
  test(`frame strips for the PR (${theme})`, async ({ page }, info) => {
    const dir = process.env.INTRO_FRAMES;
    test.skip(!dir, 'set INTRO_FRAMES=<dir> to record frames');
    await openIntro(page, { theme });
    await page.waitForTimeout(1200);
    await page.screenshot({ path: `${dir}/${info.project.name}-${theme}-00-greeting.png` });
    await pressAnywhere(page, info);
    const t0 = Date.now();
    for (let i = 1; i <= 40; i++) {
      await page.waitForTimeout(Math.max(0, i * 160 - (Date.now() - t0)));
      if ((await overlay(page).count()) === 0) break;
      await page.screenshot({ path: `${dir}/${info.project.name}-${theme}-${String(i).padStart(2, '0')}.png` });
    }
    await page.waitForTimeout(400);
    await page.screenshot({ path: `${dir}/${info.project.name}-${theme}-99-landed.png` });
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
      new Promise<{ before: Record<string, number>; after: Record<string, number>; dripLangs: string[] }>((resolve) => {
        const slots = () => Array.from(document.querySelectorAll<HTMLElement>('.wi-greet > .wi-slot'));
        const read = () => Object.fromEntries(slots().map((e) => [e.dataset.lang!, +getComputedStyle(e).opacity]));
        const poll = () => {
          const a = slots().flatMap((e) => e.getAnimations()).find((x) => (x.currentTime as number) > 60 && (x.currentTime as number) < 200);
          if (!a) return requestAnimationFrame(poll);
          const before = read();
          document.querySelector('.wi')!.dispatchEvent(new PointerEvent('pointerup', { bubbles: true, button: 0 }));
          const dripLangs = [...new Set(Array.from(document.querySelectorAll<HTMLElement>('.wi-drip > .wi-slot')).map((e) => e.dataset.lang!))];
          requestAnimationFrame(() => resolve({ before, after: read(), dripLangs }));
        };
        poll();
      }),
  );
  const shown = Object.entries(r.before).filter(([, o]) => o > 0.05);
  expect(shown.length, `two phrases part-way through the change ${JSON.stringify(r.before)}`).toBe(2);
  // The more visible phrase melts (the drips are cloned from it), the other fades from where it was, and neither
  // jumps on the press frame.
  const top = shown.reduce((a, b) => (b[1] > a[1] ? b : a))[0];
  expect(r.dripLangs).toEqual([top]);
  for (const [lang, o] of shown) expect(Math.abs(r.after[lang]! - o), `${lang}: ${o} → ${r.after[lang]}`).toBeLessThan(0.2);
  await expect(overlay(page)).toHaveAttribute('data-state', 'playing');
});

test('a resize mid-run jumps to the end on the re-measured page logo (D-15)', async ({ page }, info) => {
  await openIntro(page, { before: (p) => p.addInitScript(RECORDER) });
  const vp = page.viewportSize()!;
  await pressAnywhere(page, info);
  await page.waitForTimeout(1500);
  await page.setViewportSize({ width: vp.height > vp.width ? vp.height : Math.round(vp.width * 0.7), height: vp.height > vp.width ? vp.width : vp.height });
  await expect(overlay(page)).toHaveCount(0, { timeout: 2000 });
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
