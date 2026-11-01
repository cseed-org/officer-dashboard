import { expect, test } from '@playwright/test';
import { devLogin, settle } from './helpers';

test("an officer can't see Admin", async ({ page }, info) => {
  await devLogin(page, 'director');
  await settle(page);
  if (info.project.name === 'phone') await page.locator('[data-nav-toggle]').click();
  await expect(page.locator('#sidebar [data-nav="admin"]')).toBeVisible();

  await devLogin(page, 'officer');
  await settle(page);
  if (info.project.name === 'phone') await page.locator('[data-nav-toggle]').click();
  await expect(page.locator('#sidebar [data-nav="events"]')).toBeVisible();
  await expect(page.locator('#sidebar [data-nav="admin"]')).toBeHidden();

  await page.goto('/admin');
  await settle(page);
  await expect(page.getByText('Directors only')).toBeVisible();
  await expect(page.getByRole('tab')).toHaveCount(0);

  // Director-only finance views are absent too.
  await page.goto('/finance');
  await settle(page);
  await expect(page.getByRole('tab', { name: 'fiscal periods' })).toHaveCount(0);
  await page.getByRole('tab', { name: 'reports' }).click();
  await expect(page.getByRole('button', { name: 'trial balance' })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'budget vs actual' })).toBeVisible();
});
