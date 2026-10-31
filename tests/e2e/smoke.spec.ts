import { expect, test, type Page } from '@playwright/test';
import { layoutFailures, measure } from './measure';
import { devLogin, settle, shot } from './helpers';

/** Every route in the plan. Dynamic ids come from the seed (scripts/seed/data.ts). */
const ROUTES = [
  '/',
  '/events?when=past',
  '/events',
  '/events/event-kickoff',
  '/events/import',
  '/requests',
  '/requests/new',
  '/requests/req-0001',
  '/finance',
  '/finance?tab=budget-vs-actual',
  '/finance?tab=fund-balances',
  '/finance?tab=statement-of-activities',
  '/finance?tab=statement-of-financial-position',
  '/finance?tab=statement-of-functional-expenses',
  '/finance?tab=trial-balance',
  '/finance?tab=form-990',
  '/requests?scope=all',
  '/requests/req-0004',
  '/admin/users',
  '/admin/tokens',
];
const SCHEMES = ['light', 'dark'] as const;

async function checkPage(page: Page, route: string, errors: string[]) {
  const res = await page.goto(route, { waitUntil: 'load' });
  expect(res?.status(), `${route} status`).toBe(200);
  await settle(page);
  // Every page's data loaded: no error state, no skeleton left.
  await expect(page.locator('[data-state="error"]'), `${route} shows an error state`).toHaveCount(0);
  const failures = layoutFailures(await page.evaluate(measure));
  if (errors.length) failures.push(`script error: ${errors.join(' | ')}`);
  return failures;
}

// One test per route (both themes) so a slow machine doesn't time out the whole sweep.
for (const route of ROUTES) {
  test(`director: ${route} renders without layout problems, light and dark`, async ({ page }, info) => {
    const errors: string[] = [];
    page.on('pageerror', (e) => errors.push(e.message.split('\n')[0]!));
    await devLogin(page, 'director');
    const problems: string[] = [];
    for (const scheme of SCHEMES) {
      await page.emulateMedia({ colorScheme: scheme });
      errors.length = 0;
      const failures = await checkPage(page, route, errors);
      await expect(page.locator('h1')).toBeVisible();
      await shot(page, info.project.name, scheme, route);
      problems.push(...failures.map((f) => `${scheme} ${route}: ${f}`));
    }
    expect(problems, problems.join('\n')).toEqual([]);
  });
}

// Officers see the summary-only finance page and their own requests.
for (const route of ['/', '/requests', '/finance', '/finance?tab=fund-balances']) {
  test(`officer: ${route} renders without layout problems`, async ({ page }, info) => {
    await devLogin(page, 'officer');
    const problems: string[] = [];
    for (const scheme of SCHEMES) {
      await page.emulateMedia({ colorScheme: scheme });
      problems.push(...(await checkPage(page, route, [])).map((f) => `${scheme} ${route}: ${f}`));
      await shot(page, info.project.name, scheme, `officer/${route}`);
    }
    if (route === '/finance') {
      await expect(page.getByRole('tab', { name: 'trial balance' })).toHaveCount(0);
      await expect(page.getByRole('tab', { name: 'fund balances' })).toBeVisible();
    }
    expect(problems, problems.join('\n')).toEqual([]);
  });
}

test('the nav reaches every section', async ({ page }, info) => {
  await devLogin(page, 'director');
  const nav = page.getByRole('navigation', { name: info.project.name === 'phone' ? 'Main (mobile)' : 'Main', exact: true });
  await expect(nav).toBeVisible();
  // Labels stay whole: one line box each, nothing broken inside a word.
  const lines = await nav.locator('a').evaluateAll((links) => links.map((l) => [l.textContent!.trim(), l.querySelector('span')?.getClientRects().length ?? 1] as const));
  for (const [label, n] of lines) expect(n, `"${label}" wraps`).toBe(1);
  for (const [label, path] of [
    ['events', '/events'],
    ['requests', '/requests'],
    ['finance', '/finance'],
    ['admin', '/admin/users'],
    ['overview', '/'],
  ] as const) {
    await nav.getByRole('link', { name: label }).click();
    await expect(page).toHaveURL(path);
    await expect(nav.getByRole('link', { name: label })).toHaveAttribute('aria-current', 'page');
  }
});

test('an officer is refused /admin/* and has no admin link', async ({ page, request }, info) => {
  await devLogin(page, 'officer');
  const nav = page.getByRole('navigation', { name: info.project.name === 'phone' ? 'Main (mobile)' : 'Main', exact: true });
  await expect(nav.getByRole('link', { name: 'admin' })).toHaveCount(0);
  for (const route of ['/admin/users', '/admin/tokens']) {
    const res = await page.goto(route);
    expect(res?.status()).toBe(403);
    await expect(page.locator('h1')).toHaveText('not allowed');
  }
  const api = await page.request.get('/api/admin/users');
  expect(api.status()).toBe(403);
  void request;
});

test('signed-out visitors are sent to the login page', async ({ page }) => {
  await page.goto('/requests');
  await expect(page).toHaveURL(/\/login\?next=%2Frequests$/);
  const failures = layoutFailures(await page.evaluate(measure));
  expect(failures).toEqual([]);
  await shot(page, test.info().project.name, 'light', 'login');
  await page.emulateMedia({ colorScheme: 'dark' });
  await shot(page, test.info().project.name, 'dark', 'login');
});

// Review finding 2: Chromium strips the tab and followed /<TAB>/example.com off-site.
// Re-review: '/.//example.com' normalised to a protocol-relative Location.
test('a crafted next= cannot send a signed-in user off-site', async ({ page, baseURL }) => {
  for (const next of [
    '%2F%09%2Fexample.com',
    '%2F%5Cexample.com',
    '%2F%2Fexample.com',
    '%2F%0A%2Fexample.com',
    // re-review: dot segments that normalise to //example.com
    '%2F.%2F%2Fexample.com',
    '%2Fa%2F..%2F%2Fexample.com',
    '%2F%252e%2F%2Fexample.com',
    '%2F%2Fapp.invalid%2F%2Fexample.com',
  ]) {
    await page.goto(`/auth/dev?as=officer&next=${next}`);
    expect(new URL(page.url()).origin).toBe(new URL(baseURL!).origin);
    await expect(page).toHaveURL('/');
  }
  await page.goto('/auth/dev?as=officer&next=%2Frequests%3Fscope%3Dall');
  await expect(page).toHaveURL('/requests?scope=all');
});
