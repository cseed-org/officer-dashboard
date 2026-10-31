import { expect, test, type Page } from '@playwright/test';
import { layoutFailures, measure } from './measure';

/** Every route in the plan. Dynamic ids come from the seed (scripts/seed/data.ts). */
const ROUTES = [
  '/',
  '/events',
  '/events/event-kickoff',
  '/events/import',
  '/requests',
  '/requests/new',
  '/requests/req-0001',
  '/finance',
  '/admin/users',
  '/admin/tokens',
];
const SCHEMES = ['light', 'dark'] as const;

const shotName = (route: string) => (route === '/' ? 'overview' : route.slice(1).replaceAll('/', '_'));

async function devLogin(page: Page, as: 'director' | 'officer') {
  const res = await page.goto(`/auth/dev?as=${as}`);
  expect(res?.status(), 'dev login failed; is DEV_AUTH=1 in .dev.vars?').toBe(200);
  await expect(page).toHaveURL('/');
}

async function checkPage(page: Page, route: string, errors: string[]) {
  const res = await page.goto(route, { waitUntil: 'load' });
  expect(res?.status(), `${route} status`).toBe(200);
  await page.evaluate(() => document.fonts.ready);
  await page.addStyleTag({ content: 'astro-dev-toolbar { display: none !important; }' });
  const failures = layoutFailures(await page.evaluate(measure));
  if (errors.length) failures.push(`script error: ${errors.join(' | ')}`);
  return failures;
}

test('director can reach every route without layout problems, light and dark', async ({ page }, info) => {
  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(e.message.split('\n')[0]!));
  await devLogin(page, 'director');

  const problems: string[] = [];
  for (const scheme of SCHEMES) {
    await page.emulateMedia({ colorScheme: scheme });
    for (const route of ROUTES) {
      errors.length = 0;
      const failures = await checkPage(page, route, errors);
      await expect(page.locator('h1')).toBeVisible();
      await page.screenshot({
        path: `tmp/e2e-shots/${info.project.name}/${scheme}/${shotName(route)}.png`,
        fullPage: true,
      });
      problems.push(...failures.map((f) => `${scheme} ${route}: ${f}`));
    }
  }
  expect(problems, problems.join('\n')).toEqual([]);
});

test('the nav reaches every section', async ({ page }, info) => {
  await devLogin(page, 'director');
  const nav = page.getByRole('navigation', { name: info.project.name === 'phone' ? 'Main (mobile)' : 'Main', exact: true });
  await expect(nav).toBeVisible();
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
  await page.screenshot({ path: `tmp/e2e-shots/${test.info().project.name}/light/login.png` });
});
