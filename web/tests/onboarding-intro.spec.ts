import { expect, test, type Page } from '@playwright/test';
import { devLoginSkipTour, devSignIn } from './helpers';
import { letterCells, WORD_PATH } from '../src/ui/onboarding/snakePlan';

/**
 * V0.10 hello screen: the multilingual greeting in glass letters over changing colour, no start
 * button (a click, a tap or any key starts), the group-photo slot, the arcade snake whose body
 * ends up spelling "cseed", the hand-off onto the shell logo at every width, the reduced-motion
 * and Skip paths, and the media fallbacks. The planner itself is unit-tested in
 * onboarding-snake.spec.ts. Needs the Go server with DEV_AUTH=1.
 *
 * INTRO_FRAMES=<dir> also saves a frame sequence of the animation (used for the PR).
 */
const discordId = () => `9${String(Date.now()).padStart(13, '0')}${Math.floor(Math.random() * 1e4).toString().padStart(4, '0')}`;

/** The real app shell's logo (sidebar wordmark, the rail's C mark, or the phone top bar), measured on Home. */
const shellLogo = (page: Page) =>
  page.evaluate(() => {
    const el = Array.from(document.querySelectorAll('.topbar .wordmark-logo, .sidebar .wordmark-logo, .sidebar .wordmark-mark')).find((e) => {
      const b = e.getBoundingClientRect();
      return b.width > 0 && b.right > 0 && b.left < innerWidth && getComputedStyle(e).visibility !== 'hidden';
    })!;
    const b = el.getBoundingClientRect();
    return { box: [b.left, b.top, b.width, b.height], kind: el.classList.contains('wordmark-mark') ? 'mark' : 'wordmark' };
  });

async function openIntro(page: Page) {
  const id = discordId();
  await devLoginSkipTour(page, 'director');
  await expect(page.locator('.app')).toBeVisible();
  const shell = await shellLogo(page);
  const r = await page.request.post('/api/invites', { data: { name: 'Intro Tester', discordId: id, subteamId: 'team-events', method: 'link' } });
  expect(r.ok()).toBe(true);
  await page.goto('about:blank');
  await page.context().clearCookies();
  await devSignIn(page, { discord: id });
  await expect(page).toHaveURL(/\/onboarding$/);
  await expect(page.locator('.wi')).toBeVisible(); // the intro is the first step, no switch needed
  return shell;
}

const active = (page: Page) => page.locator('[data-wi-phrase][data-active="true"]');
const overlay = (page: Page) => page.locator('.wi');
const box = (page: Page, sel: string) => page.locator(sel).evaluate((e) => { const b = e.getBoundingClientRect(); return [b.left, b.top, b.width, b.height]; });

/**
 * Where the intro's logo ends up: its layout box (it is placed by left/top/height, and its own
 * animation ends at scale(1) with no translate), next to the page logo's box. Read from layout,
 * not from a frame, so a slow machine cannot miss the moment.
 */
const landing = (page: Page) =>
  page.locator('[data-wi-logo]').evaluate((lg: HTMLElement) => {
    const frames = lg.getAnimations().map((a) => (a.effect as KeyframeEffect).getKeyframes());
    const last = frames.at(-1)?.at(-1);
    const p = document.querySelector('[data-onb-logo]')!.getBoundingClientRect();
    // Untransformed box: left/top/height as placed, width from the current box over the current scale.
    const m = new DOMMatrix(getComputedStyle(lg).transform === 'none' ? undefined : getComputedStyle(lg).transform);
    const w = lg.getBoundingClientRect().width / (m.a || 1);
    return { intro: [parseFloat(lg.style.left), parseFloat(lg.style.top), w, parseFloat(lg.style.height)], page: [p.left, p.top, p.width, p.height], kind: lg.dataset.kind, end: last?.transform, opacity: last?.opacity };
  });

test('the greeting cycles through languages in glass letters, with no start button', async ({ page }) => {
  await openIntro(page);
  const langs = new Set<string>();
  await expect(active(page)).toHaveCount(1);
  for (let i = 0; i < 3; i++) {
    langs.add((await active(page).getAttribute('data-lang')) ?? '');
    await expect.poll(async () => (await active(page).getAttribute('data-lang')) ?? '', { timeout: 6000 }).not.toBe([...langs].pop());
  }
  expect(langs.size).toBeGreaterThanOrEqual(3);
  const all = page.locator('[data-wi-phrase]');
  expect(await all.count()).toBeGreaterThanOrEqual(12);
  const names = await all.evaluateAll((els) => els.map((e) => e.querySelector('.wi-cs')?.textContent));
  expect(new Set(names)).toEqual(new Set(['cseed']));
  const arabic = page.locator('[data-wi-phrase][lang="ar"]');
  await expect(arabic).toHaveAttribute('dir', 'rtl');
  await expect(arabic.locator('.wi-cs')).toHaveAttribute('dir', 'ltr');
  await expect(page.locator('.wi p.visually-hidden')).toHaveText('Hello, welcome to cseed');
  // Glass type: a translucent fill clipped to the letters, never a solid colour.
  const fill = await active(page).locator('.wi-text').evaluate((e) => ({ color: getComputedStyle(e).color, clip: getComputedStyle(e).backgroundClip }));
  expect(fill.color).toBe('rgba(0, 0, 0, 0)');
  expect(fill.clip).toBe('text');
  // The only button is Skip; the overlay itself is the labelled control.
  await expect(overlay(page).getByRole('button')).toHaveCount(1);
  await expect(page.getByRole('button', { name: 'Skip' })).toBeVisible();
  await expect(page.getByRole('button', { name: /continue|start|begin/i })).toHaveCount(0);
  await expect(page.getByRole('dialog', { name: /press any key, click or tap anywhere/i })).toBeVisible();
  // The colour changes: a new wash fades in about every 1.2s.
  const washes = async () => page.locator('.wi-wash').evaluateAll((els) => els.map((e) => (e as HTMLElement).style.background).join('|'));
  const w0 = await washes();
  await expect.poll(washes, { timeout: 4000 }).not.toBe(w0);
});

test('a click shows the group photo and starts the snake, whose body ends up spelling "cseed", then it lands on the logo', async ({ page }) => {
  const shell = await openIntro(page);
  await overlay(page).click({ position: { x: 200, y: 300 } });
  await expect(overlay(page)).toHaveAttribute('data-state', 'playing');
  // The photo slot: the placeholder until the real photo is dropped in.
  const photo = page.locator('[data-wi-photo]');
  await expect(photo).toHaveAttribute('data-wi-photo', 'placeholder');
  await expect(photo.locator('img')).toHaveAttribute('src', '/media/hello-group-placeholder.svg');
  await expect.poll(() => photo.evaluate((e) => +getComputedStyle(e).opacity)).toBeGreaterThan(0.95);
  await expect(page.locator('[data-wi-snake="head"]')).toHaveCount(1);
  await expect.poll(() => page.locator('[data-wi-apple]').count()).toBe(WORD_PATH.length - 6);

  await expect(overlay(page)).toHaveAttribute('data-state', 'spelled', { timeout: 30_000 });
  // The body's cells, tail to head, are exactly the planned word path; letter cells vs joins as planned.
  const cells = await page.locator('[data-wi-snake]').evaluateAll((els) =>
    (els as HTMLElement[]).filter((e) => e.style.opacity === '1').map((e) => ({ cell: e.dataset.cell!, kind: e.dataset.kind!, head: e.dataset.wiSnake === 'head' })),
  );
  expect(cells).toHaveLength(WORD_PATH.length);
  const want = new Set(WORD_PATH.map((c) => `${c.x},${c.y}`));
  expect(new Set(cells.map((c) => c.cell))).toEqual(want);
  const letters = letterCells();
  for (const c of cells) expect(c.kind, c.cell).toBe(letters.has(c.cell) ? 'letter' : 'join');
  const last = WORD_PATH[WORD_PATH.length - 1]!;
  expect(cells.find((c) => c.head)!.cell).toBe(`${last.x},${last.y}`);
  // Joins are dimmed so the word reads.
  const join = page.locator('[data-wi-snake][data-kind="join"]').first();
  await expect.poll(() => join.evaluate((e) => getComputedStyle(e).backgroundColor)).toMatch(/rgba\(20, 20, 20, 0\.2\d/);

  const rects = await landing(page);
  expect(rects.end).toBe('scale(1)');
  expect(String(rects.opacity)).toBe('1');
  rects.intro.forEach((v, i) => expect(Math.abs(v - rects.page[i]!)).toBeLessThan(0.01));
  rects.intro.forEach((v, i) => expect(Math.abs(v - shell.box[i]!), `shell logo ${shell.box} vs intro ${rects.intro}`).toBeLessThan(0.6));
  await expect(overlay(page)).toHaveCount(0, { timeout: 20_000 }); // onDone → the page removes it
  await expect(page.locator('[data-onb-logo]')).toBeVisible();
  await expect(page.locator('#step-title')).toBeFocused();
});

test('any key starts it; Tab moves focus and Shift does nothing', async ({ page }) => {
  await openIntro(page);
  await page.keyboard.press('Shift');
  await page.keyboard.press('Tab');
  await expect(page.getByRole('button', { name: 'Skip' })).toBeFocused();
  await page.keyboard.press('Shift+Tab');
  await expect(overlay(page)).toBeFocused();
  await expect(overlay(page)).toHaveAttribute('data-state', 'greeting');
  await page.keyboard.press('k');
  await expect(overlay(page)).toHaveAttribute('data-state', 'playing');
  await expect(page.locator('[data-wi-snake="head"]')).toHaveCount(1);
});

test('it is a real modal: Tab stays inside, the setup behind is inert, focus returns after', async ({ page }) => {
  await openIntro(page);
  expect(await page.evaluate(() => !!document.querySelector('.onb')?.closest('[inert]'))).toBe(true);
  for (let i = 0; i < 8; i++) {
    await page.keyboard.press(i % 3 === 2 ? 'Shift+Tab' : 'Tab');
    expect(await page.evaluate(() => !!document.activeElement?.closest('.wi')), `focus escaped after press ${i + 1}`).toBe(true);
  }
  await expect(overlay(page)).toHaveAttribute('data-state', 'greeting');
  await page.getByRole('button', { name: 'Skip' }).click();
  await expect(overlay(page)).toHaveCount(0, { timeout: 3000 });
  expect(await page.evaluate(() => !!document.querySelector('[inert]'))).toBe(false);
  await expect(page.locator('#step-title')).toBeFocused();
});

test('pressing again fast-forwards to the end; Escape skips', async ({ page }) => {
  await openIntro(page);
  await overlay(page).click({ position: { x: 120, y: 300 } });
  await expect(overlay(page)).toHaveAttribute('data-state', 'playing');
  await page.waitForTimeout(500);
  await page.keyboard.press('Space');
  await expect(overlay(page)).toHaveCount(0, { timeout: 3000 });

  // Escape, on a fresh invitee.
  await openIntro(page);
  await page.keyboard.press('Escape');
  await expect(page.locator('[data-wi-snake]')).toHaveCount(0);
  await expect(overlay(page)).toHaveCount(0, { timeout: 3000 });
});

test('Skip goes straight to the logo without the snake or the photo', async ({ page }) => {
  await openIntro(page);
  await page.getByRole('button', { name: 'Skip' }).click();
  await expect(page.locator('[data-wi-snake]')).toHaveCount(0);
  await expect(overlay(page)).toHaveCount(0, { timeout: 3000 });
  await expect(page.locator('[data-onb-logo]')).toBeVisible();
});

test('prefers-reduced-motion: no snake and no colour changes; the photo shows, then a plain fade to the logo', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await openIntro(page);
  await expect(active(page)).toHaveCount(1);
  const washes = () => page.locator('.wi-wash').evaluateAll((els) => els.map((e) => (e as HTMLElement).style.background).join('|'));
  const w0 = await washes();
  await page.waitForTimeout(2600);
  expect(await washes()).toBe(w0);
  await page.keyboard.press('Space');
  await expect.poll(() => page.locator('[data-wi-photo]').evaluate((e) => +getComputedStyle(e).opacity)).toBeGreaterThan(0.95);
  await expect(page.locator('[data-wi-snake]')).toHaveCount(0);
  await expect(page.locator('[data-wi-apple]')).toHaveCount(0);
  await expect(overlay(page)).toHaveCount(0, { timeout: 5000 });
  await expect(page.locator('[data-onb-logo]')).toBeVisible();
});

test('missing media: no video element, the animated gradient instead, and the photo placeholder', async ({ page }) => {
  const media: string[] = [];
  page.on('request', (r) => r.url().includes('/media/') && media.push(new URL(r.url()).pathname));
  await openIntro(page);
  await expect(overlay(page)).toHaveAttribute('data-video', 'off');
  await expect(page.locator('[data-wi-video]')).toHaveCount(0);
  await expect(page.locator('[data-wi-fallback]')).toBeVisible();
  await expect(overlay(page)).toHaveAttribute('data-photo', 'placeholder');
  // Nothing asks for the missing files (no 404s): only the placeholder is fetched.
  expect(media.filter((m) => !m.endsWith('/hello-group-placeholder.svg'))).toEqual([]);
  // Glass that does not need refraction is still glass: a blurred backdrop on Skip.
  const skip = await page.getByRole('button', { name: 'Skip' }).evaluate((e) => getComputedStyle(e).backdropFilter);
  expect(skip).toContain('blur(');
});

// QA Q-1: the logo hand-off lines up with the real shell at the tablet edges and on phones.
for (const [w, h] of [[820, 1180], [900, 1180], [1099, 900], [1100, 900], [390, 844]] as const) {
  test(`${w}px: the intro logo lands exactly on the shell logo`, async ({ page }, info) => {
    test.skip(info.project.name !== 'desktop', 'widths are set explicitly; run once');
    await page.setViewportSize({ width: w, height: h });
    const shell = await openIntro(page);
    const pageLogo = await box(page, '[data-onb-logo]');
    pageLogo.forEach((v, i) => expect(Math.abs(v - shell.box[i]!), `page ${pageLogo} vs shell ${shell.box}`).toBeLessThan(0.6));
    await expect(page.locator('[data-onb-logo]')).toHaveAttribute('data-kind', shell.kind);
    await page.keyboard.press('Enter');
    await expect(overlay(page)).toHaveAttribute('data-state', 'playing');
    const r = await landing(page);
    expect(r.end).toBe('scale(1)');
    expect(r.kind).toBe(shell.kind);
    r.intro.forEach((v, i) => expect(Math.abs(v - shell.box[i]!), `intro ${r.intro} vs shell ${shell.box}`).toBeLessThan(0.6));
  });
}

test('frame sequence for the PR', async ({ page }, info) => {
  const dir = process.env.INTRO_FRAMES;
  test.skip(!dir, 'set INTRO_FRAMES=<dir> to record frames');
  await openIntro(page);
  await page.waitForTimeout(1600);
  await page.screenshot({ path: `${dir}/${info.project.name}-00-greeting.png` });
  await page.keyboard.press('Enter');
  const t0 = Date.now();
  for (let i = 1; i <= 30; i++) {
    await page.waitForTimeout(Math.max(0, i * 400 - (Date.now() - t0)));
    await page.screenshot({ path: `${dir}/${info.project.name}-${String(i).padStart(2, '0')}.png` });
    if ((await overlay(page).count()) === 0) break;
  }
  await page.waitForTimeout(300);
  await page.screenshot({ path: `${dir}/${info.project.name}-99-handoff.png` });
});
