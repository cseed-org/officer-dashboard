import { test, expect } from '@playwright/test';
import { devSignIn, ownerLoginAvailable } from './helpers';

test('owner can inspect integrations; other officers cannot open Setup', async ({ page }) => {
  test.skip(!ownerLoginAvailable, 'owner dev login requires an unkeyed loopback server');
  await devSignIn(page, { as: 'owner' }, '/setup');
  await expect(page.getByRole('heading', { name: 'Google Drive', exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Discord login', exact: true })).toBeVisible();
  await expect(page.locator('html')).toHaveAttribute('data-owner', 'true');
  await expect(page.locator('[data-nav="setup"]')).toHaveCount(1);
  const response = await page.request.get('/api/setup/status');
  expect(response.status()).toBe(200);
  expect((await response.json()).integrations).toHaveLength(10);
  await devSignIn(page, { as: 'officer' }, '/setup');
  await expect(page).toHaveURL(/\/$/);
  await expect(page.locator('[data-nav="setup"]')).toBeHidden();
  expect((await page.request.get('/api/setup/status')).status()).toBe(403);
});
