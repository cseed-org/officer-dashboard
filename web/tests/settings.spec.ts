import { expect, test } from '@playwright/test';
import { devLogin, settle } from './helpers';

test('the theme setting persists across a reload with no flash', async ({ page }) => {
  await devLogin(page, 'director');
  // Light is the default.
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  await page.goto('/settings');
  await settle(page);
  await page.getByLabel('Dark').check();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');

  // Reload with every module script blocked: only the inline pre-paint script can set the
  // theme, so a dark first paint proves there is no light flash before hydration.
  await page.route(/\.(m?js|tsx?|css)(\?.*)?$|\/@vite\/|\/@id\/|\/node_modules\//, (r) => (r.request().resourceType() === 'document' ? r.continue() : r.abort()));
  await page.reload({ waitUntil: 'domcontentloaded' });
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await expect(page.locator('html')).toHaveAttribute('data-theme-pref', 'dark');
  await page.unrouteAll({ behavior: 'ignoreErrors' });

  await page.reload();
  await settle(page);
  await expect(page.getByLabel('Dark')).toBeChecked();
  const bg = await page.evaluate(() => getComputedStyle(document.body).backgroundColor);
  const [r, g, b] = bg.match(/\d+/g)!.map(Number) as [number, number, number];
  expect(r + g + b, `dark body background, got ${bg}`).toBeLessThan(200);

  // System follows the device.
  await page.emulateMedia({ colorScheme: 'light' });
  await page.getByLabel('System').check();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  await page.emulateMedia({ colorScheme: 'dark' });
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');

  // Density persists too.
  await page.getByLabel('Compact').check();
  await page.reload();
  await expect(page.locator('html')).toHaveAttribute('data-density', 'compact');
  await page.getByLabel('Comfortable').check();
  await page.getByLabel('Light').check();
});

test('signed-out visitors are sent to /login, which links to Discord and back to where they were', async ({ page }) => {
  await page.goto('/finance?id=req-0005');
  await expect(page).toHaveURL(/\/login\?next=/);
  await expect(page.getByRole('link', { name: 'Sign in with Discord' })).toHaveAttribute('href', /^\/auth\/login\?next=%2Ffinance/);
  // Dev builds also show dev sign-in links carrying the same next=; production builds (Go)
  // don't, so sign in through /auth/dev directly there.
  const devLink = page.locator('[data-dev-login="officer"]');
  if (await devLink.count()) {
    await expect(devLink).toHaveAttribute('href', /next=%2Ffinance%3Fid%3Dreq-0005/);
    await devLink.click();
  } else {
    await page.goto(`/auth/dev?as=officer&next=${encodeURIComponent('/finance?id=req-0005')}`);
  }
  await expect(page).toHaveURL(/\/finance\?id=req-0005/);
});
