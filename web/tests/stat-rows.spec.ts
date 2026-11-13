import { expect, test } from '@playwright/test';
import { devLogin, devSignIn, settle, watchErrors } from './helpers';

// V0.9: Buildspace, Events, Design and Finance requests share one layout: a StatRow of
// summary tiles on top, the list underneath.
const PAGES = [
  { path: '/buildspace', label: 'Buildspace summary', tiles: ['Builders', 'Awaiting this week', 'Open requests', 'Unmentored'], list: '.bs-table, .bs-board, .bs-groups' },
  { path: '/events', label: 'Events summary', tiles: ['Upcoming', 'This month', 'Attendees', 'Drafts'], list: '#view-list' },
  { path: '/design', label: 'Design requests summary', tiles: ['Open', 'In progress', 'Waiting on me', 'Due soon'], list: '.kanban-card, .table-wrap, [data-state="empty"]' },
  { path: '/finance', label: 'Requests summary', tiles: ['Pending', 'Approved', 'Awaiting payment', 'Spend vs budget'], list: '#fin-panel table, #fin-panel [data-state="empty"]' },
];

for (const p of PAGES) {
  test(`${p.path} renders its stat row above the list`, async ({ page }) => {
    const errors = watchErrors(page);
    const res = await devSignIn(page, { as: 'director' }, p.path);
    expect(res?.ok(), 'dev login failed; is the Go server running with DEV_AUTH=1 and the dev build?').toBe(true);
    await settle(page);
    const row = page.locator(`[data-stat-row][aria-label="${p.label}"]`);
    await expect(row).toBeVisible();
    await expect(row.locator('.stat-tile')).toHaveCount(p.tiles.length);
    for (const t of p.tiles) await expect(row.locator('.stat-tile-label', { hasText: t })).toBeVisible();
    await expect(row.locator('[aria-busy="true"]')).toHaveCount(0);
    await expect(page.locator(p.list).first()).toBeVisible();
    // The row sits above the list.
    const rowBox = (await row.boundingBox())!;
    const listBox = (await page.locator(p.list).first().boundingBox())!;
    expect(rowBox.y + rowBox.height).toBeLessThanOrEqual(listBox.y);
    // The row fits the viewport (the Buildspace table below it is wider than a tablet on main too).
    expect(rowBox.x + rowBox.width).toBeLessThanOrEqual(page.viewportSize()!.width);
    expect(errors).toEqual([]);
  });
}

test('Buildspace tiles filter the register through the query string', async ({ page }) => {
  await devSignIn(page, { as: 'director' }, '/buildspace');
  await settle(page);
  await page.locator('[data-stat="open-requests"]').click();
  await expect(page).toHaveURL(/flag=requests/);
  await expect(page.locator('[data-stat="open-requests"]')).toHaveAttribute('aria-current', 'true');
});

const spendTile = (page: import('@playwright/test').Page) => page.locator('[data-stat="spend-vs-budget"]');

test('Finance: a failed budget summary says unavailable and retries instead of showing 0', async ({ page }) => {
  await devSignIn(page, { as: 'director' }, '/');
  let fail = true;
  await page.route('**/api/finance/summary*', (route) => (fail ? route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"boom"}' }) : route.continue()));
  await page.goto('/finance');
  await settle(page);
  await expect(spendTile(page)).toContainText('Unavailable');
  await expect(spendTile(page).locator('.stat-tile-value')).toHaveText('—');
  fail = false;
  await spendTile(page).getByRole('button', { name: 'Retry' }).click();
  await expect(spendTile(page)).not.toContainText('Unavailable');
  await expect(spendTile(page).locator('.stat-tile-value')).toContainText(/%|—/);
});

test('Finance: the budget tile refetches after a request in the drawer changes', async ({ page }) => {
  await devLogin(page, 'director');
  const lookups = await (await page.request.get('/api/lookups')).json();
  const res = await page.request.post('/api/requests', {
    data: { type: 'club_purchase', title: `stat-e2e ${Date.now()}`, vendor: 'E2E', quantity: 1, unitCostCents: 500, taxShipCents: 0, expenseAccountId: lookups.expenseAccounts[0].id, fundId: lookups.funds[0].id, justification: 'stat tile e2e', neededBy: null },
  });
  expect(res.ok(), await res.text()).toBe(true);
  const id = (await res.json()).request.id as string;
  let summaries = 0;
  page.on('request', (r) => r.url().includes('/api/finance/summary') && summaries++);
  page.on('dialog', (d) => d.accept());
  try {
    await page.goto(`/finance?id=${id}`);
    await settle(page);
    await expect(spendTile(page).locator('.stat-tile-value')).not.toHaveAttribute('aria-busy', 'true');
    const before = summaries;
    await page.getByRole('button', { name: /delete/i }).first().click();
    await expect.poll(() => summaries).toBeGreaterThan(before);
  } finally {
    await page.request.delete(`/api/requests/${id}`).catch(() => {});
  }
});
