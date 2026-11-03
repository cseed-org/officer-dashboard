import { readFileSync } from 'node:fs';
import { expect, test } from '@playwright/test';
import { CONTRACTS, GENERATED, endpointsModule, extractEndpoints } from '../integrations/endpoints.mjs';
import { ENDPOINTS } from '../src/lib/contracts';
import { EARLY_PAGE, EARLY_SHARED } from '../src/lib/early';
import { devLogin, settle, watchErrors } from './helpers';

/** V0.6 Home speed: early reads, the zod contract off the first paint, module preloads. */

test('V0.6: the build-time endpoint table equals ENDPOINTS (method and path)', ({}, info) => {
  test.skip(info.project.name !== 'desktop', 'unit test runs once');
  const table = extractEndpoints(readFileSync(CONTRACTS, 'utf8'));
  expect(table).toEqual(Object.fromEntries(Object.entries(ENDPOINTS).map(([name, spec]) => [name, { method: spec.method, path: spec.path }])));
  // `npm run build` (or `astro dev`) rewrites the committed copy; commit it with the contract change.
  expect(readFileSync(GENERATED, 'utf8'), 'src/lib/endpoints.generated.ts is stale: run npm run build').toBe(endpointsModule(readFileSync(CONTRACTS, 'utf8')));
});

test('V0.6: every early path is a GET endpoint path exactly as api() builds it', ({}, info) => {
  test.skip(info.project.name !== 'desktop', 'unit test runs once');
  const gets = new Set<string>(Object.values(ENDPOINTS).filter((s) => s.method === 'GET').map((s) => s.path));
  for (const path of [...EARLY_SHARED, ...Object.values(EARLY_PAGE).flat()]) expect(gets.has(path!), path).toBe(true);
});

test.describe('Home', () => {
  test.beforeEach(async ({ page }) => {
    await page.request.post('/auth/dev', { form: { as: 'director', ...(process.env.E2E_DEV_AUTH_KEY ? { key: process.env.E2E_DEV_AUTH_KEY } : {}) }, maxRedirects: 0 });
    await page.request.put('/api/me/prefs', { data: { dashboardTutorialSeen: true } });
  });

  test('V0.6: Home starts its critical reads from the head and makes each one once', async ({ page }) => {
    const errors = watchErrors(page);
    const reads = new Map<string, number>();
    page.on('request', (r) => {
      const { pathname } = new URL(r.url());
      if (r.method() === 'GET' && pathname.startsWith('/api/')) reads.set(pathname, (reads.get(pathname) ?? 0) + 1);
    });
    await devLogin(page, 'director');
    await expect(page.getByRole('list', { name: 'Home widgets' })).toBeVisible();
    await settle(page);
    await page.waitForLoadState('networkidle');
    // The head script requests them (no duplicate from api() taking them over).
    expect(await page.evaluate(() => [...document.head.querySelectorAll('script:not([src])')].some((s) => s.textContent!.includes('__cseedEarly')))).toBe(true);
    for (const path of [...EARLY_SHARED, ...EARLY_PAGE.home!]) expect(reads.get(path), path).toBe(1);
    // LinksWidget uses the club settings the timezone gate read.
    expect(reads.get('/api/club/settings')).toBe(1);
    expect(errors).toEqual([]);
  });

  test('V0.6: a read rendered before the contract loaded still fails visibly when it breaks the contract', async ({ page }, info) => {
    test.skip(info.project.name !== 'desktop', 'one viewport is enough');
    // The early head-script read gets the broken body too: route() sees every request.
    await page.route('**/api/cloudflare/widgets', (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ unexpected: true }) }));
    // Hold the contract module until Home has rendered, so the broken read is used unvalidated first.
    let release!: () => void;
    const held = new Promise<void>((resolve) => { release = resolve; });
    await page.route(/\/contracts[.\w-]*\.(js|ts)(\?.*)?$/, async (route) => { await held; await route.continue(); });
    await devLogin(page, 'director');
    await expect(page.getByRole('list', { name: 'Home widgets' })).toBeVisible();
    release();
    const alert = page.getByRole('alert');
    await expect(alert).toContainText('did not match the API contract');
    await expect(alert.getByRole('button')).toBeVisible();
  });

  test('V0.6: built pages preload their island modules after the stylesheets', async ({ page }) => {
    test.skip(!process.env.WEB_ORIGIN, 'module preloads are added by the production build');
    const html = await (await page.request.get('/')).text();
    const preload = html.indexOf('rel="modulepreload"');
    expect(preload).toBeGreaterThan(html.lastIndexOf('rel="stylesheet"'));
    for (const m of html.matchAll(/component-url="([^"]+)"/g)) expect(html).toContain(`<link rel="modulepreload" fetchpriority="low" href="${m[1]}">`);
    // The zod contract is not among them: it loads after the first data.
    expect(html).not.toMatch(/modulepreload" fetchpriority="low" href="\/_astro\/contracts\./);
  });
});
