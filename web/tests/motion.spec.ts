import { expect, test } from '@playwright/test';
import { devLogin, settle } from './helpers';

test('reduced motion: no entry animations and KPI figures appear at their final value', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop', 'one viewport is enough');
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await devLogin(page, 'director');
  await settle(page);
  const kpi = page.locator('.kpi-value').first();
  await expect(kpi).toBeVisible();
  // No count-up: the visible figure is the final one straight away (no aria-hidden tween span).
  await expect(kpi.locator('[aria-hidden="true"]')).toHaveCount(0);
  const anim = await page.evaluate(() =>
    ['.page-enter', '.progress-fill', '.kpi'].map((s) => {
      const el = document.querySelector(s);
      return el ? getComputedStyle(el).animationName : 'none';
    }),
  );
  expect(anim.every((a) => a === 'none')).toBe(true);
});

test('motion on: the tab underline slides with transform only', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop', 'one viewport is enough');
  await devLogin(page, 'director');
  await page.goto('/finance');
  await settle(page);
  const bar = page.locator('.tabs .tab-indicator');
  const before = await bar.evaluate((el) => getComputedStyle(el).transform);
  await page.getByRole('tab', { name: 'budgets' }).click();
  await expect.poll(() => bar.evaluate((el) => getComputedStyle(el).transform)).not.toBe(before);
  expect(await bar.evaluate((el) => getComputedStyle(el).transitionProperty)).toBe('transform');
});

test('drawer closes with Escape without blocking the page', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop', 'shared modal behavior');
  await devLogin(page, 'director');
  await page.goto('/events?id=event-hack-night');
  await settle(page);
  await expect(page.locator('.drawer[open]')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.locator('.drawer[open]')).toHaveCount(0);
  await expect(page.locator('html')).not.toHaveClass(/has-modal/);
  await expect(page.getByLabel('Search events')).toBeEnabled();
});
