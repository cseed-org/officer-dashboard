import { expect, test, type Page } from '@playwright/test';
import { devLoginSkipTour, devSignIn } from './helpers';

/**
 * V0.9 onboarding intro: the multilingual greeting, the snake that eats it and the apples
 * spelling "cseed" before becoming the logo, and the reduced-motion / Skip paths. The intro
 * opens on /onboarding?intro=1 for a brand-new invitee. Needs the Go server with DEV_AUTH=1.
 *
 * INTRO_FRAMES=<dir> also saves a frame sequence of the animation (used for the PR).
 */
const discordId = () => `9${String(Date.now()).padStart(13, '0')}${Math.floor(Math.random() * 1e4).toString().padStart(4, '0')}`;

async function openIntro(page: Page) {
  const id = discordId();
  await devLoginSkipTour(page, 'director');
  await expect(page.locator('.app')).toBeVisible();
  // The real app-shell logo (sidebar at desktop, top bar on phones): the snake's destination.
  const shell = await page.evaluate(() => {
    // Phone: top bar wordmark. 768–1099px: the rail's C mark. Wider: the sidebar wordmark.
    const sel = innerWidth < 768 ? '.topbar .wordmark-logo' : innerWidth < 1100 ? '.sidebar .wordmark-mark' : '.sidebar .wordmark-logo';
    const el = Array.from(document.querySelectorAll(sel)).find((e) => e.getBoundingClientRect().width > 0)!;
    const b = el.getBoundingClientRect();
    return [b.left, b.top, b.width, b.height];
  });
  const r = await page.request.post('/api/invites', { data: { name: 'Intro Tester', discordId: id, subteamId: 'team-events', method: 'link' } });
  expect(r.ok()).toBe(true);
  await page.goto('about:blank');
  await page.context().clearCookies();
  await devSignIn(page, { discord: id });
  await expect(page).toHaveURL(/\/onboarding$/);
  await page.goto('/onboarding?intro=1');
  await expect(page.locator('.wi')).toBeVisible();
  return shell;
}

const active = (page: Page) => page.locator('[data-wi-phrase][data-active="true"]');
const overlay = (page: Page) => page.locator('.wi');

test('the greeting cycles through languages and keeps cseed lowercase', async ({ page }) => {
  await openIntro(page);
  const langs = new Set<string>();
  await expect(active(page)).toHaveCount(1);
  for (let i = 0; i < 4; i++) {
    langs.add((await active(page).getAttribute('data-lang')) ?? '');
    await expect.poll(async () => (await active(page).getAttribute('data-lang')) ?? '', { timeout: 6000 }).not.toBe([...langs].pop());
  }
  expect(langs.size).toBeGreaterThanOrEqual(3);
  const all = page.locator('[data-wi-phrase]');
  expect(await all.count()).toBeGreaterThanOrEqual(12);
  // Every sentence carries the club name as lowercase Latin text, LTR even inside Arabic.
  const names = await all.evaluateAll((els) => els.map((e) => e.querySelector('.wi-cs')?.textContent));
  expect(new Set(names)).toEqual(new Set(['cseed']));
  const arabic = page.locator('[data-wi-phrase][lang="ar"]');
  await expect(arabic).toHaveAttribute('dir', 'rtl');
  await expect(arabic.locator('.wi-cs')).toHaveAttribute('dir', 'ltr');
  await expect(page.locator('.wi p.visually-hidden')).toHaveText('Hello, welcome to cseed');
  await expect(page.getByRole('button', { name: 'Skip' })).toBeVisible();
});

test('Enter sends the snake in, it lands on the logo, then onDone fires', async ({ page }) => {
  const shell = await openIntro(page);
  await page.keyboard.press('Enter');
  await expect(overlay(page)).toHaveAttribute('data-state', 'playing');
  await expect(page.locator('[data-wi-snake="head"]')).toHaveCount(1);
  expect(await page.locator('[data-wi-snake]').count()).toBeGreaterThan(10);
  await expect.poll(() => page.locator('[data-wi-apple]').count()).toBeGreaterThan(20);

  // The final frame: the intro's logo sits exactly where the page's logo does.
  // Measured inside the page, in the frame the logo animation ends (the overlay goes ~160ms later).
  const rects = await page.evaluate(
    () =>
      new Promise<{ intro: number[]; page: number[] }>((resolve) => {
        const box = (s: string) => {
          const b = document.querySelector(s)!.getBoundingClientRect();
          return [b.left, b.top, b.width, b.height];
        };
        // Keep the last measurement taken once the logo is fully in; resolve when the overlay goes
        // (a slow machine can skip frames between the logo finishing and the overlay unmounting).
        let last: { intro: number[]; page: number[] } | null = null;
        const tick = () => {
          const lg = document.querySelector('[data-wi-logo]');
          if (!lg) return last ? resolve(last) : requestAnimationFrame(tick);
          const anims = lg.getAnimations();
          if (anims.length && anims.every((a) => a.playState === 'finished')) last = { intro: box('[data-wi-logo]'), page: box('[data-onb-logo]') };
          requestAnimationFrame(tick);
        };
        tick();
      }),
  );
  // ...and that is where the real shell draws its logo (same images, same geometry).
  rects.intro.forEach((v, i) => expect(Math.abs(v - rects.page[i]!)).toBeLessThan(0.01));
  rects.intro.forEach((v, i) => expect(Math.abs(v - shell[i]!), `shell logo ${shell} vs intro ${rects.intro}`).toBeLessThan(0.6));

  await expect(overlay(page)).toHaveCount(0, { timeout: 20_000 }); // onDone → the page removes it
  await expect(page.locator('[data-onb-logo]')).toBeVisible();
  await expect(page.locator('#step-title')).toBeFocused();
});

test('it is a real modal: Tab stays inside, the setup behind is inert, focus returns after', async ({ page }) => {
  await openIntro(page);
  expect(await page.evaluate(() => !!document.querySelector('.onb')?.closest('[inert]'))).toBe(true);
  for (let i = 0; i < 8; i++) {
    await page.keyboard.press(i % 3 === 2 ? 'Shift+Tab' : 'Tab');
    expect(await page.evaluate(() => !!document.activeElement?.closest('.wi')), `focus escaped after press ${i + 1}`).toBe(true);
  }
  await page.getByRole('button', { name: 'Skip' }).click();
  await expect(overlay(page)).toHaveCount(0, { timeout: 3000 });
  expect(await page.evaluate(() => !!document.querySelector('[inert]'))).toBe(false);
  await expect(page.locator('#step-title')).toBeFocused();
});

test('a click dismisses; pressing again fast-forwards to the end', async ({ page }) => {
  await openIntro(page);
  await page.locator('.wi').click({ position: { x: 120, y: 300 } });
  await expect(overlay(page)).toHaveAttribute('data-state', 'playing');
  await page.waitForTimeout(600);
  await page.keyboard.press('Space');
  await expect(overlay(page)).toHaveCount(0, { timeout: 3000 });
});

test('Skip goes straight to the logo without the snake', async ({ page }) => {
  await openIntro(page);
  await page.getByRole('button', { name: 'Skip' }).click();
  await expect(page.locator('[data-wi-snake]')).toHaveCount(0);
  await expect(overlay(page)).toHaveCount(0, { timeout: 3000 });
  await expect(page.locator('[data-onb-logo]')).toBeVisible();
});

test('prefers-reduced-motion: no snake, a plain fade to the logo', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await openIntro(page);
  await expect(active(page)).toHaveCount(1);
  await page.keyboard.press('Space');
  await expect(page.locator('[data-wi-snake]')).toHaveCount(0);
  await expect(page.locator('[data-wi-apple]')).toHaveCount(0);
  await expect(overlay(page)).toHaveCount(0, { timeout: 3000 });
  await expect(page.locator('[data-onb-logo]')).toBeVisible();
});

test('the accessible Continue button works from the keyboard', async ({ page }) => {
  await openIntro(page);
  const btn = page.getByRole('button', { name: 'Continue to setup' });
  await page.keyboard.press('Tab'); // Skip first, then Continue
  await page.keyboard.press('Tab');
  await expect(btn).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(overlay(page)).toHaveAttribute('data-state', 'playing');
});

test('frame sequence for the PR', async ({ page }, info) => {
  const dir = process.env.INTRO_FRAMES;
  test.skip(!dir, 'set INTRO_FRAMES=<dir> to record frames');
  await openIntro(page);
  await page.waitForTimeout(1200);
  await page.screenshot({ path: `${dir}/${info.project.name}-00-greeting.png` });
  await page.keyboard.press('Enter');
  const t0 = Date.now();
  for (let i = 1; i <= 30; i++) {
    await page.waitForTimeout(Math.max(0, i * 450 - (Date.now() - t0)));
    await page.screenshot({ path: `${dir}/${info.project.name}-${String(i).padStart(2, '0')}.png` });
    if ((await overlay(page).count()) === 0) break;
  }
  await page.waitForTimeout(300);
  await page.screenshot({ path: `${dir}/${info.project.name}-99-handoff.png` });
});
