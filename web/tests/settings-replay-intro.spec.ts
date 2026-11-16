import { expect, test } from '@playwright/test';
import { devLoginSkipTour } from './helpers';
import { overlay, pressAnywhere, shellLogo } from './intro-helpers';

/**
 * Settings › Advanced › Replay welcome intro (D-22, ReplayIntro.tsx). Every signed-in role has it; it
 * plays the full V0.12 hello screen over Settings, lands on the real shell logo, and leaves the account
 * and its onboarding state exactly as they were.
 */
for (const role of ['officer', 'director', 'leadership'] as const) {
  test(`${role}: Advanced has "Replay welcome intro"`, async ({ page }) => {
    await devLoginSkipTour(page, role);
    await page.goto('/settings');
    await expect(page.getByText('Advanced', { exact: true })).toBeVisible();
    await expect(page.locator('[data-replay-intro]')).toBeVisible();
    await expect(page.locator('[data-replay-intro]')).toHaveText('Replay welcome intro');
  });
}

test('the replay plays the full intro, lands on the shell logo and changes nothing', async ({ page }, info) => {
  await devLoginSkipTour(page, 'officer');
  await page.goto('/settings');
  const state = async () => {
    const me = await (await page.request.get('/api/me')).json();
    const prefs = await (await page.request.get('/api/me/prefs')).json();
    return JSON.stringify({ onboarding: me.onboarding ?? null, prefs });
  };
  const before = await state();
  const shell = await shellLogo(page);
  const saves: string[] = [];
  page.on('request', (r) => r.method() !== 'GET' && r.url().includes('/api/') && saves.push(`${r.method()} ${r.url()}`));

  await page.locator('[data-replay-intro]').click();
  await expect(overlay(page)).toBeVisible();
  await expect(overlay(page)).toHaveAttribute('data-state', 'greeting');
  // The greeting cycles in the glass; the shell's own logo waits hidden under it.
  await expect(page.locator('.wi-slot[data-front]')).toHaveAttribute('data-lang', 'en');
  // Record the hand-off on every frame from the press on.
  await page.evaluate(() => {
    const w = window as unknown as { __rows: { state: string; logo: number; box: number[]; shellHidden: boolean }[] };
    w.__rows = [];
    const tick = () => {
      const wi = document.querySelector<HTMLElement>('.wi');
      const lg = document.querySelector<HTMLElement>('[data-wi-logo]');
      const img = lg && Array.from(lg.querySelectorAll('img, .wordmark-mark')).find((e) => getComputedStyle(e).display !== 'none');
      const b = img?.getBoundingClientRect();
      const shellEls = Array.from(document.querySelectorAll<HTMLElement>('.topbar .wordmark-logo, .sidebar .wordmark-logo, .sidebar .wordmark-mark')).filter((e) => e.getBoundingClientRect().width > 0);
      w.__rows.push({ state: wi?.dataset.state ?? 'gone', logo: lg ? +getComputedStyle(lg).opacity : -1, box: b ? [b.left, b.top, b.width, b.height] : [], shellHidden: shellEls.every((e) => getComputedStyle(e).visibility === 'hidden') });
      if (wi) requestAnimationFrame(tick);
      else w.__rows.push({ state: 'gone', logo: -1, box: [], shellHidden: shellEls.every((e) => getComputedStyle(e).visibility === 'hidden') });
    };
    requestAnimationFrame(tick);
  });
  await page.waitForTimeout(400);
  await pressAnywhere(page, info);
  await expect(overlay(page)).toHaveAttribute('data-state', 'playing');
  await expect(overlay(page)).toHaveCount(0, { timeout: 15_000 });

  // Back on Settings, same page, the shell logo visible again.
  await expect(page).toHaveURL(/\/settings$/);
  const rows = await page.evaluate(() => (window as unknown as { __rows: { state: string; logo: number; box: number[]; shellHidden: boolean }[] }).__rows);
  const gone = rows.findIndex((r) => r.state === 'gone');
  expect(gone).toBeGreaterThan(0);
  for (const r of rows.slice(0, gone)) expect(r.shellHidden, 'the shell logo stays hidden while the intro draws its own').toBe(true);
  expect(rows[gone]!.shellHidden).toBe(false);
  const last = rows[gone - 1]!;
  expect(last.logo).toBeGreaterThan(0.95);
  last.box.forEach((v, i) => expect(Math.abs(v - shell.box[i]!), `intro ${last.box} vs shell ${shell.box}`).toBeLessThan(0.6));

  // Nothing was saved, and the account is exactly as it was.
  expect(saves).toEqual([]);
  expect(await state()).toBe(before);
});

test('replay: Escape skips it and reduced motion fades straight to the logo', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await devLoginSkipTour(page, 'director');
  await page.goto('/settings');
  await page.locator('[data-replay-intro]').click();
  await expect(overlay(page)).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(overlay(page)).toHaveCount(0, { timeout: 3000 });
  // Focus returns to the button.
  await expect(page.locator('[data-replay-intro]')).toBeFocused();
});
