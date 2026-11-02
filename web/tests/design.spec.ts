import { expect, test, type Page } from '@playwright/test';
import { overflow, settle, THEMES, useTheme, watchErrors } from './helpers';

/**
 * Design requests against the seeded Go server (DEV_AUTH=1): Priya (officer) requests,
 * Riley (Design subteam) takes it and moves it to review, only Priya approves it.
 */
async function signIn(page: Page, user: 'user-priya' | 'user-riley' | 'user-director') {
  const as = user === 'user-director' ? 'director' : 'officer';
  const res = await page.goto(`/auth/dev?as=${as}&user=${user}&next=/design`);
  expect(res?.ok(), 'dev login failed; is the Go server running with DEV_AUTH=1 and the dev build?').toBe(true);
  await settle(page);
}

const card = (page: Page, title: string) => page.locator('.kanban-card', { hasText: title });

test('a design request moves across the board', async ({ page }, info) => {
  const errors = watchErrors(page);
  const title = `E2E poster ${info.project.name} ${Date.now()}`;
  const phone = info.project.name === 'phone';

  // Request from an event: the form arrives prefilled.
  await signIn(page, 'user-priya');
  await page.goto('/design?new=1&eventId=event-hack-night');
  await settle(page);
  const form = page.locator('dialog[open] [data-design-form]');
  await expect(form.getByLabel('Title')).toHaveValue('Hack Night promo');
  // Due a week before Hack Night (Nov 13, 2026), never before today, none once it has passed.
  const today = new Intl.DateTimeFormat('en-CA', { timeZone: 'America/Vancouver' }).format(new Date());
  await expect(form.getByLabel('Due date')).toHaveValue(today >= '2026-11-13' ? '' : today > '2026-11-06' ? today : '2026-11-06');
  await form.getByLabel('Title').fill(title);
  await form.getByLabel('Type').selectOption('poster');
  await form.getByLabel('Sizes / formats').fill('A3 PDF');
  await form.getByRole('button', { name: 'Submit design request' }).click();
  const detail = page.locator('dialog[open] [data-design-detail]');
  await expect(detail).toHaveAttribute('data-stage', 'new');
  await expect(detail.getByText('Unassigned')).toBeVisible();
  // Officers outside the design team get no designer actions.
  await expect(detail.locator('[data-designer-actions]')).toHaveCount(0);

  // The designer takes it and moves it to review (the ⋯ menu path on the board).
  await signIn(page, 'user-riley');
  await page.goto('/design?view=board');
  await settle(page);
  await card(page, title).getByRole('button', { name: `Actions for ${title}` }).click();
  await page.getByRole('menuitem', { name: 'Move to Accepted' }).click();
  await expect(page.locator('.kanban-col[data-stage="accepted"]').locator('.kanban-card', { hasText: title })).toBeVisible();
  await expect(card(page, title).getByText('Riley Park')).toBeVisible();
  // Then via the drawer's Designer actions (the keyboard/touch path).
  await card(page, title).getByRole('link', { name: title }).click();
  const actions = page.locator('dialog[open] [data-designer-actions]');
  await actions.getByLabel('Move to').selectOption('in-review');
  await actions.getByRole('button', { name: 'Update request' }).click();
  await expect(page.locator('dialog[open] [data-design-detail]')).toHaveAttribute('data-stage', 'in-review');
  await expect(page.locator('dialog[open] [data-approval]')).toContainText('Waiting for Priya');
  await page.getByLabel('Add a comment').fill('Ready for your review.');
  await page.getByRole('button', { name: 'Post comment' }).click();
  await expect(page.locator('dialog[open] .timeline')).toContainText('Ready for your review.');

  // A director has no approval shortcut.
  await signIn(page, 'user-director');
  await page.goto('/design');
  await settle(page);
  await page.getByRole('link', { name: title }).first().click();
  await expect(page.locator('dialog[open] [data-design-detail]')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Approve this version' })).toHaveCount(0);

  // The requester approves; that delivers it.
  await signIn(page, 'user-priya');
  await page.goto('/design');
  await settle(page);
  await page.getByRole('link', { name: title }).first().click();
  await page.getByRole('button', { name: 'Approve this version' }).click();
  await expect(page.locator('dialog[open] [data-design-detail]')).toHaveAttribute('data-stage', 'delivered');
  await page.keyboard.press('Escape');
  await page.goto('/design?view=board');
  await settle(page);
  await expect(page.locator('.kanban-col[data-stage="delivered"]').locator('.kanban-card', { hasText: title })).toBeVisible();

  if (phone) {
    // Phones open on the list.
    await page.goto('/design');
    await settle(page);
    await expect(page.getByRole('table', { name: 'Design requests list' })).toBeVisible();
  }
  expect(errors).toEqual([]);
});

for (const theme of THEMES) {
  test(`design board renders without overflow (${theme})`, async ({ page }, info) => {
    await useTheme(page, theme);
    const errors = watchErrors(page);
    await signIn(page, 'user-riley');
    for (const view of ['board', 'list']) {
      await page.goto(`/design?view=${view}`);
      await settle(page);
      expect(await overflow(page)).toEqual([]);
      await page.screenshot({ path: `test-results/design-${view}-${info.project.name}-${theme}.png`, fullPage: true });
    }
    await page.goto('/design?id=design-kickoff-recap');
    await settle(page);
    await expect(page.locator('dialog[open] [data-design-detail]')).toBeVisible();
    expect(errors).toEqual([]);
  });
}
