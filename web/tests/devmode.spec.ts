import { expect, test, type Page } from '@playwright/test';
import { settle, watchErrors } from './helpers';

/**
 * Developer mode on a test deployment: a dev build (`-tags dev`) with DEV_AUTH=1 and
 * DEV_AUTH_KEY set. Run against the Go server serving the built site, e.g.
 *   DEV_AUTH=1 DEV_AUTH_KEY=$KEY STATIC_DIR=../web/dist ./bin/dashboard-dev serve
 *   E2E_DEV_AUTH_KEY=$KEY WEB_ORIGIN=http://127.0.0.1:<port> npx playwright test devmode
 * Skipped unless E2E_DEV_AUTH_KEY is set (the key must match the server's).
 */
const KEY = process.env.E2E_DEV_AUTH_KEY ?? '';
test.skip(!KEY, 'set E2E_DEV_AUTH_KEY to the server’s DEV_AUTH_KEY');

const panel = (page: Page) => page.getByRole('region', { name: 'Developer mode' });

async function role(page: Page) {
  const r = await page.request.get('/api/me');
  return r.ok() ? ((await r.json()) as { user: { role: string } }).user.role : null;
}

test('the login page looks like production without the key', async ({ page }) => {
  const errors = watchErrors(page);
  for (const url of ['/login', '/login?key=wrong']) {
    // Wait for the /auth/dev/info probe to answer, then make sure nothing appeared.
    const probe = page.waitForResponse((r) => r.url().includes('/auth/dev/info'));
    await page.goto(url);
    const info = await (await probe).json();
    expect(info).toEqual({ enabled: true, keyRequired: true, authorized: false });
    await expect(page.getByRole('link', { name: 'Sign in with Discord' })).toBeVisible();
    await expect(panel(page)).toHaveCount(0);
  }
  expect(await page.request.get('/auth/dev?as=director&key=wrong').then((r) => r.status())).toBe(404);
  expect(errors).toEqual([]);
});

test('with the key, the panel skips Discord: Director lands on Home', async ({ page }) => {
  const errors = watchErrors(page);
  await page.goto(`/login?key=${encodeURIComponent(KEY)}`);
  await expect(panel(page)).toBeVisible();
  await expect(panel(page).getByText('Skip Discord and continue as')).toBeVisible();
  await expect(panel(page).getByText(/test build/)).toBeVisible();
  // The key leaves the address bar once the panel is built.
  await expect(page).not.toHaveURL(/key=/);
  await panel(page).getByRole('link', { name: 'Director' }).click();
  // A link only reaches the confirmation page; its button posts the sign-in.
  await page.getByRole('button', { name: 'Continue' }).click();
  await expect(page).toHaveURL(/\/$/);
  await settle(page);
  expect(await role(page)).toBe('director');
  expect(errors).toEqual([]);
});

test('a keyed deep link confirms, signs in and keeps the destination; nothing remembers the key', async ({ page, context }) => {
  const errors = watchErrors(page);
  await page.goto(`/auth/dev?as=officer&key=${encodeURIComponent(KEY)}&next=/events`);
  expect(await role(page)).toBeNull();
  await page.getByRole('button', { name: 'Continue' }).click();
  await expect(page).toHaveURL(/\/events$/);
  await settle(page);
  expect(await role(page)).toBe('officer');
  const cookies = await context.cookies();
  expect(cookies.find((c) => c.name.endsWith('dev_key'))).toBeUndefined();

  // Signed out, the login page shows no panel without the key, and a post without it fails.
  await context.clearCookies();
  await page.goto('/events');
  await expect(page).toHaveURL(/\/login\?next=%2Fevents/);
  await expect(page.getByRole('link', { name: 'Sign in with Discord' })).toBeVisible();
  await expect(panel(page)).toHaveCount(0);
  const refused = await page.request.post('/auth/dev', { form: { as: 'leadership' }, maxRedirects: 0 });
  expect(refused.status()).not.toBe(303);
  expect(await role(page)).toBeNull();
  expect(errors).toEqual([]);
});
