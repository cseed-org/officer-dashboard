import { expect, test } from '@playwright/test';
import { devLogin, settle } from './helpers';

test.beforeEach(async ({ page }) => {
  await page.clock.setFixedTime(new Date('2026-11-11T10:00:00-08:00'));
  await page.addInitScript(() => { Math.random = () => 0.5; });
  await page.request.post('/auth/dev', { form: { as: 'director', ...(process.env.E2E_DEV_AUTH_KEY ? { key: process.env.E2E_DEV_AUTH_KEY } : {}) }, maxRedirects: 0 });
  await page.request.put('/api/me/prefs', { data: { dashboardTutorialSeen: true } });
});

test('first visit greets once, reload shows Home, next local day greets again', async ({ page }) => {
  await devLogin(page, 'director');
  await expect(page.locator('h1')).toContainText('Good morning,');
  await expect(page).toHaveTitle('Home · CSEED officers');
  await expect(page.locator('[data-home-greeting-announcement]')).toContainText('Good morning,');
  await page.reload();
  await settle(page);
  await expect(page.locator('h1')).toHaveText('Home');
  await page.clock.setFixedTime(new Date('2026-11-12T10:00:00-08:00'));
  await page.reload();
  await expect(page.locator('h1')).toContainText('Good morning,');
});

test('sign out and sign back in welcomes the officer back', async ({ page }) => {
  await devLogin(page, 'director');
  await expect(page.locator('h1')).toContainText('Good morning,');
  await page.goto('/settings');
  await settle(page);
  await page.locator('[data-sign-out]').click();
  await expect(page).toHaveURL(/\/login/);
  await devLogin(page, 'director');
  await expect(page.locator('h1')).toContainText('Welcome back,');
});

test('invite-page sign out also records a new sign-in', async ({ page }) => {
  await devLogin(page, 'director');
  await expect(page.locator('h1')).toContainText('Good morning,');
  await page.goto('/login?invite=greeting-test');
  await page.locator('[data-invite-sign-out]').click();
  await expect(page).toHaveURL(/\/login\?invite=/);
  await devLogin(page, 'director');
  await expect(page.locator('h1')).toContainText('Welcome back,');
});

test('02:00 overrides the rare greeting', async ({ page }) => {
  await page.clock.setFixedTime(new Date('2026-11-11T02:00:00-08:00'));
  await page.addInitScript(() => { Math.random = () => 0; });
  await devLogin(page, 'director');
  await expect(page.locator('h1')).toContainText('Go sleep, bro');
});

for (const both of [false, true]) {
  test(`blocked ${both ? 'both stores' : 'localStorage'} keeps Home working`, async ({ page }) => {
    const errors: string[] = [];
    page.on('pageerror', error => errors.push(error.message));
    await page.addInitScript((blockBoth) => {
      Object.defineProperty(window, 'localStorage', { get() { throw new Error('blocked'); } });
      if (blockBoth) Object.defineProperty(window, 'sessionStorage', { get() { throw new Error('blocked'); } });
    }, both);
    await devLogin(page, 'director');
    await settle(page);
    await expect(page.locator('h1')).toContainText(both ? 'Home' : 'Good morning,');
    await page.clock.setFixedTime(new Date('2026-11-12T10:00:00-08:00'));
    await page.reload();
    await settle(page);
    await expect(page.locator('h1')).toHaveText('Home');
    await page.goto('/settings');
    await settle(page);
    await page.locator('[data-sign-out]').click();
    await expect(page).toHaveURL(/\/login/);
    expect(errors).toEqual([]);
  });
}

test('reduced motion has no heading fade and long names do not move the grid', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.route('**/api/me', async route => {
    const response = await route.fetch();
    const body = await response.json();
    body.user.name = 'Alexandertheverylongfirstname Officer';
    await route.fulfill({ response, json: body });
  });
  await devLogin(page, 'director');
  await settle(page);
  await expect(page.locator('.home-heading-new')).toContainText('Alexandertheverylongfirstname');
  expect(await page.locator('.home-heading-new').evaluate(el => getComputedStyle(el).animationName)).toBe('none');
  const before = await page.locator('.page-header').boundingBox();
  await page.reload();
  await settle(page);
  const after = await page.locator('.page-header').boundingBox();
  expect(before?.height).toBe(after?.height);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});
