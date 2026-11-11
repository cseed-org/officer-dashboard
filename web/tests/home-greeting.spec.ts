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
  const line = await page.locator('.home-heading-new').evaluate(el => {
    const css = getComputedStyle(el);
    return { overflow: css.textOverflow, height: el.getBoundingClientRect().height, lineHeight: parseFloat(css.lineHeight) };
  });
  expect(line.overflow).toBe('ellipsis');
  expect(line.height).toBeCloseTo(line.lineHeight, 1);
  const before = await page.locator('.page-header').boundingBox();
  await page.reload();
  await settle(page);
  const after = await page.locator('.page-header').boundingBox();
  expect(before?.height).toBe(after?.height);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('larger greeting reserves its space and reload restores ordinary heading typography', async ({ page }, info) => {
  let release!: () => void;
  const ready = new Promise<void>(resolve => { release = resolve; });
  await page.route('**/api/me', async route => { await ready; await route.continue(); }, { times: 1 });
  await devLogin(page, 'director');
  const heading = page.locator('.home-heading');
  await expect(heading).toHaveText('Home');
  const headerBefore = (await page.locator('.page-header').boundingBox())!;
  const baseSize = await heading.evaluate(el => parseFloat(getComputedStyle(el).fontSize));
  release();
  await settle(page);
  const greeting = page.locator('.home-heading-new');
  await expect(greeting).toContainText('Good morning,');
  const largerSize = await greeting.evaluate(el => parseFloat(getComputedStyle(el).fontSize));
  expect(largerSize / baseSize).toBeCloseTo(info.project.name === 'phone' ? 1.3 : 1.6, 2);
  const headerAfter = (await page.locator('.page-header').boundingBox())!;
  expect(headerAfter.y + headerAfter.height).toBeCloseTo(headerBefore.y + headerBefore.height, 1);
  const gridBefore = (await page.locator('.blocks').boundingBox())!;
  await page.reload();
  await settle(page);
  await expect(heading).toHaveText('Home');
  await expect(greeting).toHaveCount(0);
  const styles = await heading.evaluate(el => {
    const reference = document.createElement('h1');
    reference.textContent = 'Home';
    document.querySelector('main')!.append(reference);
    const typography = (node: Element) => {
      const css = getComputedStyle(node);
      return [css.fontSize, css.fontFamily, css.fontWeight, css.lineHeight, css.letterSpacing, css.color];
    };
    const result = { actual: typography(el), original: typography(reference) };
    reference.remove();
    return result;
  });
  expect(styles.actual).toEqual(styles.original);
  expect((await page.locator('.blocks').boundingBox())!.y).toBeCloseTo(gridBefore.y, 1);
});

test('invite sign-in tolerates a missing user', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.route('**/api/me', route => route.fulfill({ json: { user: null, onboarding: null } }));
  await page.goto('/login?invite=greeting-test');
  await expect(page.locator('[data-invite-signed-in]')).toBeVisible();
  await expect(page.locator('[data-invite-signed-in-name]')).toHaveText('someone else');
  await expect(page.locator('[data-invite-signed-in] form')).toHaveAttribute('data-greeting-user-id', '');
  expect(errors).toEqual([]);
});
