import { expect, test } from '@playwright/test';
import { devLogin, openDialog, overflow, settle, THEMES, useTheme, watchErrors } from './helpers';

/**
 * Programs tab on Buildspace + Launchpad (V0.15): weekly programs with per-session skip/move
 * exceptions. Creates its own program and deletes it, so it can run against a shared seeded database.
 */

/** YYYY-MM-DD n days from today (browser/test timezone is America/Los_Angeles). */
const day = (n: number) => {
  const d = new Date();
  d.setDate(d.getDate() + n);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
};

test('programs: create, skip and move a session, edit, delete', async ({ page }, info) => {
  const errors = watchErrors(page);
  await devLogin(page, 'officer');
  await page.goto('/buildspace-launchpad?tab=programs');
  await settle(page);
  const name = `E2E program ${info.project.name} ${Date.now().toString(36)}`;

  // Create: the tab lists programs grouped by track.
  await page.locator('[data-new-program]').click();
  const editor = openDialog(page);
  await expect(editor.getByRole('heading', { name: 'New program' })).toBeVisible();
  const weekday = (new Date().getDay() + 2) % 7; // a session lands two days out
  await editor.getByLabel('Program name').fill(name);
  await editor.getByLabel('Track').selectOption({ label: 'buildspace' });
  await editor.getByLabel('Weekday').selectOption(String(weekday));
  await editor.getByLabel(/^Start time/).fill('18:00');
  await editor.getByLabel(/^Duration/).fill('90');
  await editor.getByLabel(/^Location/).fill('The Nest');
  await editor.getByLabel('First date').fill(day(-3));
  await editor.getByLabel('Last date').fill(day(45));
  await editor.getByLabel(/^Description/).fill('Weekly e2e program.');
  await editor.getByRole('button', { name: 'Save program' }).click();
  await expect(page.locator('dialog[open]')).toHaveCount(0);
  await expect(page.getByRole('status').filter({ hasText: 'Weekly Programs calendar' }).first()).toBeVisible();

  const row = page.locator('tr', { has: page.locator(`[data-program-open]`, { hasText: name }) });
  await expect(row).toContainText('The Nest');
  await expect(row).toContainText('6 PM–7:30 PM');
  expect(await overflow(page)).toEqual([]);

  // Track filter narrows the list.
  await page.getByLabel('Filter by track').selectOption({ label: 'buildHER' });
  await expect(page.locator(`[data-program-open]`, { hasText: name })).toHaveCount(0);
  await page.getByLabel('Filter by track').selectOption({ label: 'buildspace' });
  await expect(page.locator(`[data-program-open]`, { hasText: name })).toHaveCount(1);

  // Open the program; the first session can be skipped and the skip undone.
  await page.locator(`[data-program-open]`, { hasText: name }).click();
  const detail = openDialog(page);
  await expect(detail.getByRole('heading', { name })).toBeVisible();
  const first = detail.locator('[data-session]').first();
  await expect(first).toHaveAttribute('data-session-state', 'scheduled');
  await first.locator('[data-skip-session]').click();
  await expect(first).toHaveAttribute('data-session-state', 'skipped');
  await expect(first.getByText('Skipped')).toBeVisible();
  await first.locator('[data-undo-exception]').click();
  await expect(first).toHaveAttribute('data-session-state', 'scheduled');

  // Move the same session; the row shows the new time and place.
  await first.locator('[data-move-session]').click();
  const move = first.locator('[data-move-form]');
  await move.getByLabel('New date').fill(day(9));
  await move.getByLabel(/^Start/).fill('19:00');
  await move.getByLabel(/^End/).fill('20:30');
  await move.getByLabel(/^Location/).fill('Room 250');
  await move.getByRole('button', { name: 'Save move' }).click();
  await expect(first).toHaveAttribute('data-session-state', 'moved');
  await expect(first).toContainText('Room 250');
  await expect(first).toContainText('7 PM–8:30 PM');
  await expect(first.locator('time')).toHaveAttribute('datetime', day(9));
  await expect(first).toContainText('was ');
  await first.locator('[data-undo-exception]').click();
  await expect(first).toHaveAttribute('data-session-state', 'scheduled');
  expect(await overflow(page)).toEqual([]);

  // Edit renames the program.
  await detail.getByRole('button', { name: 'Edit' }).click();
  await detail.getByLabel('Program name').fill(`${name} v2`);
  await detail.getByRole('button', { name: 'Save program' }).click();
  await expect(page.locator('dialog[open]')).toHaveCount(0);
  await expect(page.locator('[data-program-open]', { hasText: `${name} v2` })).toHaveCount(1);

  // Delete asks for confirmation.
  await page.locator('[data-program-open]', { hasText: `${name} v2` }).click();
  await openDialog(page).locator('[data-delete-program]').click();
  await openDialog(page).locator('[data-confirm-delete-program]').click();
  await expect(page.locator('dialog[open]')).toHaveCount(0);
  await expect(page.locator('[data-program-open]', { hasText: `${name} v2` })).toHaveCount(0);
  expect(errors).toEqual([]);
});

// Tracks have no delete endpoint, so each run leaves one uniquely named track behind.
test('programs: leadership adds a track', async ({ page }, info) => {
  const errors = watchErrors(page);
  await devLogin(page, 'leadership');
  await page.goto('/buildspace-launchpad?tab=programs');
  await settle(page);
  await page.locator('[data-manage-tracks]').click();
  const dialog = openDialog(page);
  const key = `e2e-${Date.now().toString(36)}`;
  await dialog.getByLabel('New track name').fill(`E2E track ${info.project.name} ${key}`);
  await dialog.getByLabel('Key').fill(key);
  await dialog.getByLabel('Colour').selectOption('teal');
  await dialog.getByRole('button', { name: 'Add track' }).click();
  await expect(dialog.locator('.pg-tracks')).toContainText(key);
  await expect(page.getByLabel('Filter by track').locator('option', { hasText: `E2E track ${info.project.name} ${key}` })).toHaveCount(1);
  expect(errors).toEqual([]);
});

test.describe('programs review screenshots', () => {
  test.skip(!process.env.SHOTS, 'set SHOTS=1 to capture review screenshots');
  for (const theme of THEMES) {
    test(`programs + tentative events screenshots, ${theme}`, async ({ page }, info) => {
      await useTheme(page, theme);
      await devLogin(page, 'officer');
      // A program and a tentative event to look at, cleaned up afterwards.
      const prog = await page.request
        .post('/api/programs', {
          data: { trackId: 'buildspace', name: 'Robotics drop-in', weekday: 3, startTime: '18:00', durationMin: 90, location: 'The Nest', startsOn: day(-7), endsOn: day(60), description: 'Open shop time for robot teams.' },
        })
        .then((r) => r.json());
      const ev = await page.request
        .post('/api/events', { data: { name: 'Winter social (date TBD)', tentativeFrom: day(14), tentativeTo: day(21) } })
        .then((r) => r.json());
      const dir = '../docs/evidence/programs';
      const shot = async (name: string) => {
        await page.waitForTimeout(500);
        await page.screenshot({ path: `${dir}/programs-${info.project.name}-${theme}-${name}.png`, fullPage: !(await page.locator('dialog[open]').count()) });
      };
      await page.goto('/buildspace-launchpad?tab=programs');
      await settle(page);
      await shot('list');
      await page.locator('[data-program-open]', { hasText: 'Robotics drop-in' }).click();
      await expect(openDialog(page).locator('[data-session]').first()).toBeVisible();
      await shot('detail');
      await openDialog(page).getByRole('button', { name: 'Close' }).click();
      await page.locator('[data-new-program]').click();
      await shot('form');
      await openDialog(page).getByRole('button', { name: 'Close' }).click();
      await page.goto(`/events?edit=${ev.event.id}`);
      await settle(page);
      await shot('tentative-editor');
      await page.goto('/events');
      await settle(page);
      await expect(page.locator('[data-tentative-range]').first()).toBeVisible();
      await shot('tentative-list');
      await page.request.delete(`/api/programs/${prog.program.id}`);
      await page.request.delete(`/api/events/${ev.event.id}`);
    });
  }
});
