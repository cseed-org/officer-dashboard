import { expect, test, type Page } from '@playwright/test';
import { devSignIn, settle } from './helpers';

/**
 * Regression (V0.10): dragging a design card straight across into another stage must move it
 * on the server. Columns used to be only as tall as their cards, and with the V0.9 summary
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
    const before = await serverStage(page, id);
    const target = before === 'in-progress' ? 'in-review' : 'in-progress';
    const from = (await card.locator('.kanban-card-meta').boundingBox())!;
    const column = page.locator(`.kanban-col[data-stage="${target}"]`);
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
