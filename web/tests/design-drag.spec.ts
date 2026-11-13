import { expect, test, type Page } from '@playwright/test';
import { devSignIn, settle } from './helpers';

/**
 * Regression (V0.10): dragging a design card straight across into another stage must move it
 * on the server. Lanes (columns) used to be only as tall as their cards, and with the V0.9 summary
 * tiles above the board, a card low in a busy column was dragged sideways into empty space
 * below (or, once scrolled, entirely below) the short target column, so the drop did nothing.
 */
async function create(page: Page, title: string) {
  const res = await page.request.post('/api/design-requests', { data: { title, type: 'poster', brief: 'Drag regression brief.' } });
  expect(res.ok()).toBe(true);
  return (await res.json()).request.id as string;
}
const serverStage = async (page: Page, id: string) =>
  (await (await page.request.get(`/api/design-requests/${id}`)).json()).request.stage as string;

test('dragging a design card straight across into another stage moves it on the server', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop', 'a mouse drag; touch and keyboard moves are covered in design-actions.spec.ts');
  await devSignIn(page, { as: 'director', user: 'user-director' }, '/design');
  const stamp = Date.now();
  // A busy first column, so the card we drag sits well below the empty target column's cards.
  const ids: string[] = [];
  for (let i = 0; i < 4; i++) ids.push(await create(page, `Drag ${i} ${stamp}`));
  try {
    await page.goto('/design?view=board');
    await settle(page);
    const card = page.locator('[data-board] .kanban-card', { hasText: `${stamp}` }).last();
    await card.scrollIntoViewIfNeeded();
    const id = (await card.getAttribute('data-request'))!;
    // A new request sits in To do; Doing is the lane to its right and sets In progress.
    expect(await serverStage(page, id)).toBe('new');
    const target = 'in-progress';
    const from = (await card.locator('.kanban-card-meta').boundingBox())!;
    const column = page.locator('.lane[data-lane="doing"]');
    const col = (await column.boundingBox())!;
    const moved = page.waitForResponse((r) => r.url().endsWith(`/api/design-requests/${id}/move`) && r.request().method() === 'POST');
    // Straight across: same height as the grabbed point, into the target column's lane.
    await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2);
    await page.mouse.down();
    await page.mouse.move(col.x + col.width / 2, from.y + from.height / 2, { steps: 16 });
    await page.mouse.up();
    expect((await moved).ok()).toBe(true);
    await expect.poll(() => serverStage(page, id)).toBe(target);
    await expect(column.locator(`[data-request="${id}"]`)).toBeVisible();
  } finally {
    for (const id of ids) await page.request.delete(`/api/design-requests/${id}`);
  }
});

test('design-file buttons say "Not linked yet" on cards and in the detail', async ({ page }, info) => {
  await devSignIn(page, { as: 'director', user: 'user-director' }, '/design');
  await page.goto('/design?view=board');
  await settle(page);
  const card = page.locator('.lane[data-lane="todo"] .kanban-card', { hasText: 'Hack Night poster' });
  const file = card.getByRole('button', { name: 'Design file for Hack Night poster: not linked yet' });
  const folder = card.getByRole('button', { name: 'Folder for Hack Night poster: not linked yet' });
  await expect(file).toHaveAttribute('aria-disabled', 'true');
  await expect(folder).toHaveAttribute('aria-disabled', 'true');
  // Hover (or a tap on touch) shows the tooltip; it describes the button.
  if (info.project.name === 'desktop') await folder.hover();
  else await folder.tap({ force: true });
  await expect(card.locator('.dlink-tip', { hasText: 'Folder · Not linked yet' })).toBeVisible();
  // A disabled button does nothing: no navigation, no drawer.
  await file.click({ force: true }); // aria-disabled still receives the click, which must do nothing
  await expect(page).toHaveURL(/\/design\?view=board$/);
  await card.getByRole('link', { name: 'Hack Night poster' }).click();
  const detail = page.locator('dialog[open] [data-design-detail]');
  await expect(detail.locator('[data-design-links] .dlink')).toHaveCount(2);
  await expect(detail.locator('[data-design-links]')).toContainText('Not linked yet');
  // The one-click next step (New → Accepted) is offered to a director.
  await expect(detail.getByRole('button', { name: /Accept request/ })).toBeVisible();
});
