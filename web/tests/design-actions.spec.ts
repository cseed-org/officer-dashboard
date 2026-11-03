import { expect, test, type Page } from '@playwright/test';
import { overflow, settle, THEMES, useTheme, watchErrors, devKeyParam } from './helpers';

async function login(page: Page, role: string, user?: string) {
  await page.goto(`/auth/dev?as=${role}${user ? `&user=${user}` : ''}&next=/design${devKeyParam}`);
  await settle(page);
}
async function create(page: Page, title: string) {
  const response = await page.request.post('/api/design-requests', { data: { title, type: 'poster', brief: 'A clear brief for the design team.' } });
  expect(response.ok()).toBe(true);
  return (await response.json()).request.id as string;
}

for (const role of ['leadership', 'director']) {
  test(`${role} moves unassigned requests by menu, keyboard and pointer`, async ({ page }, info) => {
    await login(page, role);
    const title = `${role} moves ${info.project.name} ${Date.now()}`;
    const id = await create(page, title);
    await page.goto('/design?view=board');
    await settle(page);
    const card = page.locator(`[data-request="${id}"]`);
    await card.getByRole('button', { name: `Actions for ${title}` }).click();
    await page.getByRole('menuitem', { name: 'Move to Accepted', exact: true }).click();
    await expect(page.locator('.kanban-col[data-stage="accepted"]')).toContainText(title);
    await expect(page.locator('[data-board]')).toHaveAttribute('aria-busy', 'false');
    await card.focus();
    await page.keyboard.press('Space');
    await expect(page.locator('.kanban-col[data-stage="delivered"]')).toHaveAttribute('data-drop-allowed', String(role === 'leadership'));
    await page.keyboard.press('ArrowRight'); // New
    await page.keyboard.press('ArrowRight'); // In progress
    const keyboardMove = page.waitForResponse(r => r.url().endsWith(`/api/design-requests/${id}/move`) && r.request().method() === 'POST');
    await page.keyboard.press('Space');
    expect((await keyboardMove).ok()).toBe(true);
    await expect(page.locator('.kanban-col[data-stage="in-progress"]')).toContainText(title);
    // Pointer capture supports touch too, independently of HTML's mouse-only native drag.
    await expect(card.getByRole('button', { name: `Drag ${title}`, exact: true })).toBeEnabled();
    await card.getByRole('button', { name: `Drag ${title}`, exact: true }).scrollIntoViewIfNeeded();
    const handle = (await card.getByRole('button', { name: `Drag ${title}`, exact: true }).boundingBox())!;
    await page.mouse.move(handle.x + handle.width / 2, handle.y + handle.height / 2);
    await page.mouse.down();
    const column = page.locator('.kanban-col[data-stage="in-review"]');
    await column.scrollIntoViewIfNeeded();
    const target = (await column.boundingBox())!;
    await page.mouse.move(target.x + target.width / 2, target.y + 35, { steps: 12 });
    await page.mouse.up();
    await expect(column).toContainText(title);
    if (role === 'leadership') {
      await card.getByRole('button', { name: `Actions for ${title}` }).click();
      page.once('dialog', (dialog) => dialog.dismiss());
      await page.getByRole('menuitem', { name: 'Move to Delivered', exact: true }).click();
      await expect(column).toContainText(title);
      await card.getByRole('button', { name: `Actions for ${title}` }).click();
      page.once('dialog', (dialog) => dialog.accept());
      await page.getByRole('menuitem', { name: 'Move to Delivered', exact: true }).click();
      await expect(page.locator('.kanban-col[data-stage="delivered"]')).toContainText(title);
    }
    if (info.project.name === 'desktop' && role === 'director') {
      const nativeMove = page.waitForResponse(r => r.url().endsWith(`/api/design-requests/${id}/move`) && r.request().method() === 'POST');
      await expect(page.locator('[data-board]')).toHaveAttribute('aria-busy', 'false');
      await card.dragTo(page.locator('.kanban-col[data-stage="accepted"]'), { targetPosition: { x: 60, y: 25 } });
      expect((await nativeMove).ok()).toBe(true);
      await expect(page.locator('.kanban-col[data-stage="accepted"]')).toContainText(title);
    }
    await page.request.delete(`/api/design-requests/${id}`);
  });
}

test('a failed optimistic move rolls back with a visible error', async ({ page }) => {
  await login(page, 'director');
  const id = await create(page, `Rollback ${Date.now()}`);
  await page.goto('/design?view=board');
  await settle(page);
  await page.route(`**/api/design-requests/${id}/move`, route => route.fulfill({ status: 403, contentType: 'application/json', body: JSON.stringify({ error: { code: 'forbidden', message: 'Permission changed' } }) }));
  await page.locator(`[data-request="${id}"] .card-actions`).click();
  await page.getByRole('menuitem', { name: 'Move to Accepted', exact: true }).click();
  await expect(page.locator('.toast')).toContainText('Permission changed');
  await expect(page.locator(`.kanban-col[data-stage="new"] [data-request="${id}"]`)).toContainText('Rollback');
  await page.request.delete(`/api/design-requests/${id}`);
});

for (const theme of THEMES) {
  for (const [role, user] of [['leadership', 'user-leadership'], ['director', 'user-director'], ['officer', 'user-ava'], ['officer', 'user-riley'], ['officer', 'user-priya']]) {
    test(`design flows ${user} ${theme}`, async ({ page }, info) => {
      const errors = watchErrors(page);
      await useTheme(page, theme);
      await login(page, role!, user);
      const title = `QA ${user} ${theme} ${info.project.name} ${Date.now()}`;
      await page.goto('/design?new=1');
      await settle(page);
      const form = page.locator('[data-design-form]');
      const heights = await form.locator('#dr-type, #dr-link, #dr-due').evaluateAll(els => els.map(el => el.getBoundingClientRect().height));
      expect(Math.max(...heights) - Math.min(...heights)).toBeLessThan(1);
      await form.getByLabel('Title', { exact: true }).fill(title);
      await form.getByLabel('Brief', { exact: true }).fill('Include date, place and RSVP.');
      await page.screenshot({ path: `test-results/design-form-${user}-${info.project.name}-${theme}.png`, fullPage: true });
      await form.getByRole('button', { name: 'Submit design request' }).click();
      const detail = page.locator('[data-design-detail]');
      await expect(detail).toBeVisible();
      const id = await detail.getAttribute('data-design-detail');
      expect(id).toBeTruthy();
      await detail.getByRole('button', { name: 'Edit request', exact: true }).click();
      await form.getByLabel('Sizes / formats').fill('A3 PDF');
      await form.getByRole('button', { name: 'Save changes' }).click();
      await expect(detail).toContainText('A3 PDF');
      await detail.getByLabel('Add a comment').fill('Please use our logo.');
      await detail.getByRole('button', { name: 'Post comment' }).click();
      await expect(detail.locator('.timeline')).toContainText('Please use our logo.');
      const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=', 'base64');
      if (await detail.locator('#dr-upload-kind').count()) await detail.locator('#dr-upload-kind').selectOption('reference');
      await detail.locator('#dr-upload-file').setInputFiles({ name: 'logo.png', mimeType: 'image/png', buffer: png });
      await expect(detail.getByRole('link', { name: 'logo.png' })).toBeVisible();
      if (user === 'user-ava' || user === 'user-priya') await expect(detail).toContainText('Only designers and directors can move requests');
      if (role !== 'officer' || user === 'user-riley') {
        const actions = detail.locator('[data-designer-actions]');
        if (role !== 'officer') {
          await actions.getByLabel('Move to', { exact: true }).selectOption('accepted');
          await actions.getByRole('button', { name: 'Update request' }).click();
          await expect(detail).toHaveAttribute('data-stage', 'accepted');
          await expect(actions.getByLabel('Assignee', { exact: true })).toHaveValue('');
          await actions.getByLabel('Move to', { exact: true }).selectOption('new');
          await actions.getByRole('button', { name: 'Update request' }).click();
          await expect(detail).toHaveAttribute('data-stage', 'new');
        }
        await actions.getByLabel('Assignee', { exact: true }).selectOption('user-riley');
        await actions.getByRole('button', { name: 'Update request' }).click();
        await expect(detail).toContainText('Riley Park');
        await actions.getByLabel('Move to', { exact: true }).selectOption('in-review');
        await actions.getByRole('button', { name: 'Update request' }).click();
        await expect(detail).toHaveAttribute('data-stage', 'in-review');
        await detail.locator('#dr-upload-kind').selectOption('review');
        await detail.locator('#dr-upload-file').setInputFiles({ name: 'review.png', mimeType: 'image/png', buffer: png });
        await expect(detail.getByRole('link', { name: 'review.png' })).toBeVisible();
        await detail.getByRole('button', { name: 'Request changes', exact: true }).click();
        await expect(detail).toHaveAttribute('data-stage', 'in-progress');
        await actions.getByLabel('Move to', { exact: true }).selectOption('in-review');
        await actions.getByRole('button', { name: 'Update request' }).click();
        await detail.getByRole('button', { name: 'Approve this version', exact: true }).click();
        await expect(detail).toHaveAttribute('data-stage', 'delivered');
        await detail.locator('#dr-upload-file').setInputFiles({ name: 'final.png', mimeType: 'image/png', buffer: png });
        await expect(detail.getByRole('link', { name: 'final.png' })).toBeVisible();
        await actions.getByLabel('Move to', { exact: true }).selectOption('in-progress');
        await actions.getByRole('button', { name: 'Update request' }).click();
        await actions.getByLabel('Move to', { exact: true }).selectOption('new');
        await actions.getByRole('button', { name: 'Update request' }).click();
        await expect(detail).toHaveAttribute('data-stage', 'new');
      }
      await page.screenshot({ path: `test-results/design-detail-${user}-${info.project.name}-${theme}.png`, fullPage: true });
      page.once('dialog', d => d.accept());
      await detail.getByRole('button', { name: 'Withdraw request' }).click();
      await expect(detail).toHaveCount(0);
      const eventResponse = await page.request.post('/api/events', { data: { name: `${title} event`, startsAt: new Date(Date.now() + 9 * 86400000).toISOString(), hostUserId: user } });
      expect(eventResponse.ok()).toBe(true);
      const eventId = (await eventResponse.json()).event.id;
      await page.goto('/design?view=board');
      await settle(page);
      const tile = page.locator(`[data-rail-event="${eventId}"]`);
      await tile.getByRole('button', { name: 'Mark not needed' }).click();
      await expect(tile.locator('[data-indicator]')).toHaveText('Not needed');
      await tile.getByRole('button', { name: 'Needs design after all' }).click();
      await tile.getByRole('button', { name: 'Request design' }).click();
      await expect(form.getByLabel('Title', { exact: true })).toHaveValue(`${title} event promo`);
      await form.getByRole('button', { name: 'Submit design request' }).click();
      await expect(detail).toBeVisible();
      const linkedId = await detail.getAttribute('data-design-detail');
      await page.keyboard.press('Escape');
      await tile.getByRole('button', { name: 'Request another' }).click();
      await expect(form.getByLabel('Linked event or program')).toHaveValue(eventId);
      await page.keyboard.press('Escape');
      await page.request.delete(`/api/design-requests/${linkedId}`);
      await page.request.delete(`/api/events/${eventId}`);
      if (user === 'user-ava') {
        await page.locator('.kanban-card .card-actions').first().click();
        await expect(page.getByRole('menuitem', { name: 'Only designers and directors can move requests' })).toHaveAttribute('aria-disabled', 'true');
        await page.keyboard.press('Escape');
      }
      for (const filter of ['mine', 'unassigned', 'due', 'all']) {
        await page.locator(`[data-filter="${filter}"]`).click();
        await expect(page.locator(`[data-filter="${filter}"]`)).toHaveAttribute('aria-pressed', 'true');
      }
      await page.locator('[data-type-filter]').selectOption('social');
      await page.locator('[data-type-filter]').selectOption('');
      for (const view of ['board', 'list', 'mine']) {
        await page.locator(`[data-view="${view}"]`).click();
        expect(await overflow(page)).toEqual([]);
        await page.screenshot({ path: `test-results/design-${view}-${user}-${info.project.name}-${theme}.png`, fullPage: true });
      }
      expect(errors).toEqual([]);
    });
  }
}
