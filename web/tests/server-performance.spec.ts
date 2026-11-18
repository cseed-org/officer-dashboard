import { expect, test } from '@playwright/test';
import { devLoginSkipTour, settle } from './helpers';

// Optional review artifacts stay outside the repository during local runs.
test.afterEach(async ({ page }, info) => {
  if (!process.env.SERVER_PERF_SHOTS || info.status !== 'passed') return;
  for (const theme of ['light', 'dark']) {
    await page.evaluate((theme) => {
      document.documentElement.dataset.theme = theme;
      document.documentElement.style.colorScheme = theme;
    }, theme);
    await page.screenshot({ path: `/artifacts/perf/${info.project.name}-${info.title.replace(/[^a-z0-9]+/gi, '-').toLowerCase()}-${theme}.png`, fullPage: true });
  }
});

test('delivered history loads another page without dropping active cards', async ({ page }) => {
  await devLoginSkipTour(page, 'director');
  const response = await page.request.get('/api/design-requests');
  expect(response.ok()).toBe(true);
  const data = await response.json();
  const sample = data.items[0];
  expect(sample).toBeTruthy();
  const active = { ...sample, id: 'perf-active', title: 'Active work stays', stage: 'new' };
  const delivered = (i: number) => ({ ...sample, id: `perf-done-${i}`, title: `Delivered history ${i}`, stage: 'delivered' });
  let pages = 0;
  await page.route('**/api/design-requests*', async (route) => {
    const url = new URL(route.request().url());
    const more = url.searchParams.get('deliveredOnly') === 'true';
    if (more) { expect(url.searchParams.get('deliveredCursor')).toBeTruthy(); pages++; }
    await route.fulfill({ json: { ...data, items: more ? [delivered(7)] : [active, ...Array.from({ length: 6 }, (_, i) => delivered(i + 1))], deliveredNextCursor: more ? null : '2026-10-01T00:00:00.000Z|perf-done-6' } });
  });
  await page.goto('/design?view=board&lane=done');
  await settle(page);
  await expect(page.locator('.kanban-card[data-request="perf-active"]')).toHaveCount(1);
  await page.getByRole('button', { name: 'Load more delivered work', exact: true }).click();
  await expect(page.locator('.kanban-card[data-request="perf-done-7"]')).toBeVisible();
  await expect(page.locator('.kanban-card[data-request="perf-active"]')).toHaveCount(1);
  expect(pages).toBe(1);
  await expect(page.getByRole('button', { name: 'Load more delivered work', exact: true })).toHaveCount(0);
});

test('a fixed-event CSV form opens without an event-picker lookup', async ({ page }) => {
  await devLoginSkipTour(page, 'director');
  const lookups: string[] = [];
  page.on('request', (request) => { if (new URL(request.url()).pathname === '/api/lookups') lookups.push(request.url()); });
  await page.goto('/events?id=event-kickoff');
  await settle(page);
  await expect(page.locator('dialog[open]').getByLabel('CSV file', { exact: true })).toBeVisible();
  expect(lookups).toEqual([]);
});

test('Buildspace exposes a cancellable older-activity page', async ({ page }) => {
  await devLoginSkipTour(page, 'director');
  const response = await page.request.get('/api/buildspace-launchpad');
  expect(response.ok()).toBe(true);
  const data = await response.json();
  let older = 0;
  await page.route('**/api/buildspace-launchpad?*', async (route) => {
    const url = new URL(route.request().url());
    if (!url.searchParams.has('activityCursor')) return route.continue();
    older++;
    await route.fulfill({ json: { ...data, activityNextCursor: null } });
  });
  await page.route('**/api/buildspace-launchpad', (route) => route.fulfill({ json: { ...data, activityNextCursor: '2026-10-20|1|history-row' } }));
  expect(data.projects.length).toBeGreaterThan(0);
  await page.goto(`/buildspace-launchpad?project=${data.projects[0].id}`);
  await settle(page);
  const drawer = page.locator('dialog[open]');
  await drawer.getByRole('button', { name: 'Load older activity', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Load older activity', exact: true })).toHaveCount(0);
  expect(older).toBe(1);
});
