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
    await route.fulfill({ json: { ...data, items: more ? [delivered(7)] : [active, ...Array.from({ length: 6 }, (_, i) => delivered(i + 1))], deliveredNextCursor: more ? null : '2026-10-01T00:00:00.000Z|perf-done-6', deliveredTotal: 120 } });
  });
  await page.goto('/design?view=board&lane=done');
  await settle(page);
  await expect(page.locator('[data-pipeline]')).toHaveAttribute('aria-label', /Done 120/);
  await expect(page.locator('section[data-lane="done"] .lane-count')).toHaveText('120 requests');
  await expect(page.locator('.kanban-card[data-request="perf-active"]')).toHaveCount(1);
  await page.getByRole('button', { name: 'Load more delivered work', exact: true }).click();
  await expect(page.locator('.kanban-card[data-request="perf-done-7"]')).toBeVisible();
  await expect(page.locator('.kanban-card[data-request="perf-active"]')).toHaveCount(1);
  expect(pages).toBe(1);
  await expect(page.getByRole('button', { name: 'Load more delivered work', exact: true })).toHaveCount(0);
});

test('filtered Done lane counts loaded matches while Pipeline keeps the delivered total', async ({ page }) => {
  await devLoginSkipTour(page, 'director');
  const response = await page.request.get('/api/design-requests');
  expect(response.ok()).toBe(true);
  const data = await response.json();
  const sample = data.items[0];
  expect(sample).toBeTruthy();
  const active = { ...sample, id: 'perf-filter-active', title: 'Unassigned active work', stage: 'new', type: 'social', assignee: null };
  const delivered = (i: number) => ({
    ...sample,
    id: `perf-filter-done-${i}`,
    title: `Assigned delivered work ${i}`,
    stage: 'delivered',
    type: 'poster',
    assignee: { id: 'perf-designer', name: 'Designer', avatarUrl: null },
  });
  await page.route('**/api/design-requests*', (route) => route.fulfill({ json: {
    ...data,
    items: [active, ...Array.from({ length: 6 }, (_, i) => delivered(i + 1))],
    deliveredNextCursor: 'more-delivered',
    deliveredTotal: 120,
  } }));
  for (const filter of ['filter=unassigned', 'type=social']) {
    await page.goto(`/design?view=board&lane=done&${filter}`);
    await settle(page);
    await expect(page.locator('[data-pipeline]')).toHaveAttribute('aria-label', /Done 120/);
    await expect(page.locator('section[data-lane="done"] .lane-count')).toHaveText('0+ requests');
    await expect(page.locator('.board-tab[data-lane="done"] b')).toHaveText('0+');
    await expect(page.locator('section[data-lane="done"] .kanban-card')).toHaveCount(0);
  }
  await page.goto('/design?view=board&lane=done');
  await settle(page);
  await expect(page.locator('section[data-lane="done"] .lane-count')).toHaveText('120 requests');
  await expect(page.locator('section[data-lane="done"] .kanban-card')).toHaveCount(6);
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

test('Buildspace cards use retained last-activity dates when history is not loaded', async ({ page }) => {
  await devLoginSkipTour(page, 'director');
  const response = await page.request.get('/api/buildspace-launchpad');
  expect(response.ok()).toBe(true);
  const data = await response.json();
  const person = data.people.find((p: any) => data.projects.some((project: any) => project.id === p.projectId));
  const project = data.projects.find((p: any) => p.id === person.projectId);
  expect(person).toBeTruthy();
  expect(project).toBeTruthy();
  await page.route('**/api/buildspace-launchpad', (route) => route.fulfill({ json: {
    ...data,
    projects: [{ ...project, lastActivityDate: '2026-08-03' }],
    people: [{ ...person, lastActivityDate: '2026-08-03' }],
    activity: [],
  } }));
  await page.goto('/buildspace-launchpad');
  await settle(page);
  await page.getByRole('button', { name: 'Cards', exact: true }).click();
  await expect(page.locator(`[data-person-card="${person.id}"] [data-signal="activity"]`)).toContainText('Active');
  await expect(page.locator(`[data-person-card="${person.id}"] [data-signal="activity"]`)).not.toContainText('No activity yet');
  await page.getByRole('tab', { name: /^Projects/ }).click();
  await expect(page.locator(`[data-project-card="${project.id}"] [data-signal="activity"]`)).toContainText('Active');
  await expect(page.locator(`[data-project-card="${project.id}"] [data-signal="activity"]`)).not.toContainText('No activity yet');
});

test('delivered history does not overwrite a newer board reload', async ({ page }) => {
  await devLoginSkipTour(page, 'director');
  const response = await page.request.get('/api/design-requests');
  expect(response.ok()).toBe(true);
  const data = await response.json();
  const sample = data.items[0];
  const detailResponse = await page.request.get(`/api/design-requests/${sample.id}`);
  expect(detailResponse.ok()).toBe(true);
  const detailData = await detailResponse.json();
  const active = { ...sample, id: 'perf-active', title: 'Active work stays', stage: 'new' };
  const fresh = { ...sample, id: 'perf-fresh', title: 'Fresh board reload', stage: 'new' };
  const delivered = { ...sample, id: 'perf-late-history', title: 'Late delivered history', stage: 'delivered' };
  let refreshed = false;
  let boardLoads = 0;
  let historyStarted = () => {};
  const started = new Promise<void>((resolve) => { historyStarted = resolve; });
  let releaseHistory = () => {};
  const release = new Promise<void>((resolve) => { releaseHistory = resolve; });
  await page.route('**/api/design-requests**', async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname.includes('/perf-active/')) {
      refreshed = true;
      await route.fulfill({ json: { ...detailData, request: { ...detailData.request, ...active } } });
      return;
    }
    if (url.searchParams.get('deliveredOnly') === 'true') {
      historyStarted();
      await release;
      await route.fulfill({ json: { ...data, items: [delivered], deliveredNextCursor: null, deliveredTotal: 120 } });
      return;
    }
    boardLoads++;
    await route.fulfill({ json: { ...data, items: [refreshed ? fresh : active], deliveredNextCursor: refreshed ? null : 'cursor-1', deliveredTotal: 120 } });
  });
  await page.goto('/design?view=board');
  await settle(page);
  await page.getByRole('button', { name: 'Load more delivered work', exact: true }).click();
  await started;
  await page.locator('[data-request="perf-active"]').getByRole('button', { name: 'Actions for Active work stays' }).click();
  await page.getByRole('menuitem', { name: 'Move to Accepted', exact: true }).click();
  await expect.poll(() => refreshed).toBe(true);
  await expect.poll(() => boardLoads).toBeGreaterThan(1);
  await expect(page.locator('[data-request="perf-fresh"]')).toBeVisible();
  releaseHistory();
  await expect(page.locator('[data-request="perf-late-history"]')).toBeVisible();
  await expect(page.locator('[data-request="perf-fresh"]')).toHaveCount(1);
});
