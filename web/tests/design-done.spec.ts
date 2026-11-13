import { expect, test, type Page } from '@playwright/test';
import { devSignIn, settle } from './helpers';

/**
 * V0.10 review (#70): every way into Done gets the Done moment (confetti + Undo), Undo returns
 * the request to the stage it came from on the server, and a failed Undo shows the server's
 * truth instead of a stale lane.
 */
const lane = (page: Page, l: 'todo' | 'doing' | 'review' | 'done') => page.locator(`.lane[data-lane="${l}"]`);
const stage = async (page: Page, id: string) => (await (await page.request.get(`/api/design-requests/${id}`)).json()).request.stage as string;
async function create(page: Page, title: string) {
  const res = await page.request.post('/api/design-requests', { data: { title, type: 'social', brief: 'Done moment brief.' } });
  expect(res.ok()).toBe(true);
  return (await res.json()).request.id as string;
}
async function moveTo(page: Page, id: string, to: string) {
  expect((await page.request.post(`/api/design-requests/${id}/move`, { data: { stage: to } })).ok()).toBe(true);
}
/** Phones show one lane at a time. */
async function showLane(page: Page, l: 'todo' | 'doing' | 'review' | 'done') {
  const tab = page.locator(`.board-tab[data-lane="${l}"]`);
  if (await tab.isVisible()) await tab.click();
}

test('a rejected Undo keeps the card where the server has it, with an error', async ({ page }) => {
  await devSignIn(page, { as: 'director', user: 'user-director' }, '/design');
  const title = `Undo rejected ${Date.now()}`;
  const id = await create(page, title);
  try {
    await moveTo(page, id, 'accepted');
    await page.goto('/design?view=board');
    await settle(page);
    const card = page.locator(`[data-request="${id}"]`);
    await card.getByRole('button', { name: `Actions for ${title}` }).click();
    await page.getByRole('menuitem', { name: 'Move to In progress', exact: true }).click();
    await expect(lane(page, 'doing')).toContainText(title);
    await expect.poll(() => stage(page, id)).toBe('in-progress');
    // Let the post-move refetch land, then reject only the Undo.
    await expect(page.locator('[data-board]')).toHaveAttribute('aria-busy', 'false');
    await page.route(`**/api/design-requests/${id}/move`, (route) =>
      route.fulfill({ status: 403, contentType: 'application/json', body: JSON.stringify({ error: { code: 'forbidden', message: 'Permission changed' } }) }),
    );
    await page.locator('.design-toast').getByRole('button', { name: 'Undo' }).click();
    await expect(page.locator('.design-toast')).toContainText('Permission changed');
    await page.unroute(`**/api/design-requests/${id}/move`);
    await showLane(page, 'doing');
    await expect(lane(page, 'doing').locator(`[data-request="${id}"]`)).toBeVisible();
    await expect(lane(page, 'todo').locator(`[data-request="${id}"]`)).toHaveCount(0);
    expect(await stage(page, id)).toBe('in-progress');
  } finally {
    await page.request.delete(`/api/design-requests/${id}`);
  }
});

for (const how of ['menu', 'keyboard'] as const) {
  test(`Review → Done on the board (${how}) celebrates and Undo puts it back in review`, async ({ page }) => {
    await devSignIn(page, { as: 'leadership' }, '/design');
    const title = `Done moment ${how} ${Date.now()}`;
    const id = await create(page, title);
    try {
      await moveTo(page, id, 'in-review');
      await page.goto('/design?view=board&lane=review');
      await settle(page);
      const card = page.locator(`[data-request="${id}"]`);
      page.once('dialog', (d) => d.accept()); // the leadership delivery override
      if (how === 'menu') {
        await card.getByRole('button', { name: `Actions for ${title}` }).click();
        await page.getByRole('menuitem', { name: 'Move to Delivered', exact: true }).click();
      } else {
        await card.focus();
        await page.keyboard.press('Space');
        // Arrows cycle through the allowed lanes; stop on Done.
        for (let i = 0; i < 4 && !/is-over/.test((await lane(page, 'done').getAttribute('class')) ?? ''); i++) await page.keyboard.press('ArrowRight');
        await expect(lane(page, 'done')).toHaveClass(/is-over/);
        await page.keyboard.press('Space');
      }
      await expect(lane(page, 'done')).toContainText(title);
      await expect.poll(() => stage(page, id)).toBe('delivered');
      await expect(page.locator('[data-confetti]').first()).toBeAttached();
      const toast = page.locator('.design-toast');
      await expect(toast).toContainText('delivered');
      await toast.getByRole('button', { name: 'Undo' }).click();
      await expect.poll(() => stage(page, id)).toBe('in-review');
      await expect(lane(page, 'review')).toContainText(title);
      await expect(lane(page, 'done').locator(`[data-request="${id}"]`)).toHaveCount(0);
    } finally {
      await page.request.delete(`/api/design-requests/${id}`);
    }
  });
}

test('the requester approving in the detail gets the Done moment, and Undo returns it to review', async ({ page }) => {
  await devSignIn(page, { as: 'officer', user: 'user-priya' }, '/design');
  const title = `Approve undo ${Date.now()}`;
  const id = await create(page, title);
  try {
    await devSignIn(page, { as: 'director', user: 'user-director' }, '/design');
    await moveTo(page, id, 'in-review');
    await devSignIn(page, { as: 'officer', user: 'user-priya' }, '/design');
    await page.goto(`/design?id=${id}`);
    await settle(page);
    const detail = page.locator('dialog[open] [data-design-detail]');
    await detail.getByRole('button', { name: 'Approve this version' }).click();
    await expect(detail).toHaveAttribute('data-stage', 'delivered');
    const moment = detail.locator('[data-done-moment]');
    await expect(moment).toBeVisible();
    await expect(moment.locator('[data-confetti]')).toBeAttached();
    // Approver-only Undo is not designer work: no Designer actions panel.
    await expect(detail.locator('[data-designer-actions]')).toHaveCount(0);
    await moment.getByRole('button', { name: 'Undo' }).click();
    await expect(detail).toHaveAttribute('data-stage', 'in-review');
    expect(await stage(page, id)).toBe('in-review');
  } finally {
    await devSignIn(page, { as: 'director', user: 'user-director' }, '/design');
    await page.request.delete(`/api/design-requests/${id}`);
  }
});

// Leadership may deliver straight from To do; Undo must restore that exact stage on the server.
for (const [from, how] of [['accepted', 'keyboard'], ['new', 'drag']] as const) {
  test(`${from} → Done on the board (${how}) and Undo restores ${from}`, async ({ page }, info) => {
    await devSignIn(page, { as: 'leadership' }, '/design');
    const title = `To do Done ${from} ${Date.now()}`;
    const id = await create(page, title);
    try {
      if (from === 'accepted') await moveTo(page, id, 'accepted');
      await page.goto('/design?view=board');
      await settle(page);
      const card = page.locator(`[data-request="${id}"]`);
      page.once('dialog', (d) => d.accept()); // the leadership delivery override
      if (how === 'keyboard') {
        await card.focus();
        await page.keyboard.press('Space');
        for (let i = 0; i < 4 && !/is-over/.test((await lane(page, 'done').getAttribute('class')) ?? ''); i++) await page.keyboard.press('ArrowRight');
        await expect(lane(page, 'done')).toHaveClass(/is-over/);
        await page.keyboard.press('Space');
      } else if (info.project.name === 'desktop') {
        await card.dragTo(lane(page, 'done'), { targetPosition: { x: 80, y: 60 } });
      } else {
        // Touch: the ⋯ menu (Done is another tab on phones).
        await card.getByRole('button', { name: `Actions for ${title}` }).click();
        await page.getByRole('menuitem', { name: 'Move to Delivered', exact: true }).click();
      }
      await expect.poll(() => stage(page, id)).toBe('delivered');
      await expect(lane(page, 'done')).toContainText(title);
      await expect(page.locator('[data-confetti]').first()).toBeAttached();
      const toast = page.locator('.design-toast');
      await expect(toast).toContainText('delivered');
      await toast.getByRole('button', { name: 'Undo' }).click();
      await expect.poll(() => stage(page, id)).toBe(from);
      await expect(lane(page, 'todo')).toContainText(title);
      await expect(lane(page, 'done').locator(`[data-request="${id}"]`)).toHaveCount(0);
      await expect(card.locator('.approval-chip')).toHaveCount(from === 'new' ? 1 : 0);
    } finally {
      await page.request.delete(`/api/design-requests/${id}`);
    }
  });
}

test('leadership delivering a New request from the detail gets the Done moment, and Undo restores New', async ({ page }) => {
  await devSignIn(page, { as: 'leadership' }, '/design');
  const title = `Detail New Done ${Date.now()}`;
  const id = await create(page, title);
  try {
    await page.goto(`/design?id=${id}`);
    await settle(page);
    const detail = page.locator('dialog[open] [data-design-detail]');
    const actions = detail.locator('[data-designer-actions]');
    await actions.getByLabel('Move to', { exact: true }).selectOption('delivered');
    page.once('dialog', (d) => d.accept());
    await actions.getByRole('button', { name: 'Update request' }).click();
    await expect(detail).toHaveAttribute('data-stage', 'delivered');
    const moment = detail.locator('[data-done-moment]');
    await expect(moment.locator('[data-confetti]')).toBeAttached();
    await moment.getByRole('button', { name: 'Undo' }).click();
    await expect(detail).toHaveAttribute('data-stage', 'new');
    expect(await stage(page, id)).toBe('new');
  } finally {
    await page.request.delete(`/api/design-requests/${id}`);
  }
});
