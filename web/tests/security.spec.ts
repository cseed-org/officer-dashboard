import { expect, test } from '@playwright/test';
import { devLogin, openDialog, settle } from './helpers';

test('P2-1: a 401 from any API call during a session goes to /login', async ({ page }) => {
  await devLogin(page, 'director');
  await page.goto('/events');
  await settle(page);
  // The session "expires": the next data call answers 401.
  await page.route(/\/api\/events\/[^/?]+$/, (r) => r.fulfill({ status: 401, contentType: 'application/json', body: JSON.stringify({ error: { code: 'unauthenticated', message: 'Sign in first' } }) }));
  await page.locator('[data-event-link]').first().click();
  await expect(page).toHaveURL(/\/login\?next=%2Fevents%3Fid%3D/);
});

test('P2-2: /login?next= never leaves the site', async ({ page }) => {
  await devLogin(page, 'director');
  for (const next of ['/%5Cevil.example', '//evil.example', '%5C%5Cevil.example', 'https://evil.example/']) {
    await page.goto(`/login?next=${next}`);
    await expect(page).toHaveURL(/^http:\/\/localhost:\d+\/$/);
  }
  await page.goto('/login?next=%2Fsettings');
  await expect(page).toHaveURL(/\/settings$/);
});

test('P2-3: search results with off-site or script hrefs are dropped', async ({ page }, info) => {
  await devLogin(page, 'director');
  await settle(page);
  const result = (title: string, href: string) => ({ kind: 'user', id: title, title, subtitle: 'x', href });
  await page.route(/\/api\/search\?/, (r) =>
    r.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        results: [
          result('Script', 'javascript:alert(document.cookie)'),
          result('Protocol relative', '//evil.example/x'),
          result('Backslash', '/\\evil.example'),
          result('External', 'https://evil.example/'),
          { kind: 'request', id: 'req-0005', title: 'Demo night banner printing', subtitle: '#2027-0005', href: '/requests/req-0005' },
        ],
      }),
    }),
  );
  await (info.project.name === 'phone' ? page.locator('.topbar [data-open-search]') : page.locator('#sidebar [data-open-search]')).click();
  const box = page.getByRole('combobox', { name: /Search/ });
  await box.fill('zz-anything');
  await expect(page.getByRole('option', { name: /Demo night banner printing/ })).toBeVisible();
  for (const t of ['Script', 'Protocol relative', 'Backslash', 'External']) await expect(page.getByRole('option', { name: new RegExp(`^${t}`) })).toHaveCount(0);
  await expect(page.locator('.palette [data-group="user"]')).toHaveCount(0);
  await box.press('Enter');
  await expect(page).toHaveURL(/\/finance\?id=req-0005$/);
});

test('P2-4: finance shows the lookups error with a retry instead of loading forever', async ({ page }) => {
  await devLogin(page, 'director');
  await page.route(/\/api\/lookups$/, (r) => r.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ error: { code: 'internal', message: 'Lookups are down.' } }) }));
  await page.goto('/finance?tab=reports');
  await expect(page.getByText("Couldn't load the fiscal year")).toBeVisible();
  await expect(page.getByText('Lookups are down.')).toBeVisible();
  await page.unroute(/\/api\/lookups$/);
  await page.getByRole('button', { name: 'try again' }).click();
  await settle(page);
  await expect(page.getByRole('button', { name: 'budget vs actual' })).toBeVisible();
});

test('P3-2: signing out forgets the cached role', async ({ page }) => {
  await devLogin(page, 'director');
  await page.goto('/settings');
  await settle(page);
  expect(await page.evaluate(() => localStorage.getItem('cseed-role'))).toBe('director');
  await page.locator('[data-sign-out]').click();
  await page.waitForURL((u) => !u.pathname.startsWith('/settings'));
  expect(await page.evaluate(() => localStorage.getItem('cseed-role'))).toBeNull();
});

test('P3-3: the phone menu sheet traps focus and Escape returns it to the menu button', async ({ page }, info) => {
  test.skip(info.project.name !== 'phone', 'the sheet exists on phones only');
  await devLogin(page, 'director');
  await settle(page);
  const toggle = page.locator('[data-nav-toggle]');
  await toggle.click();
  const sheet = page.locator('#sidebar[role="dialog"][aria-modal="true"]');
  await expect(sheet).toBeVisible();
  for (let i = 0; i < 15; i++) {
    await page.keyboard.press('Tab');
    expect(await page.evaluate(() => !!document.activeElement?.closest('#sidebar'))).toBe(true);
  }
  await page.keyboard.press('Shift+Tab');
  expect(await page.evaluate(() => !!document.activeElement?.closest('#sidebar'))).toBe(true);
  await page.keyboard.press('Escape');
  await expect(toggle).toBeFocused();
  await expect(page.locator('#sidebar')).not.toHaveAttribute('role', 'dialog');
  await expect(openDialog(page)).toHaveCount(0);
});
