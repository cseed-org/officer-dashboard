import { expect, test } from '@playwright/test';
import { devLogin, openDialog, settle, watchErrors } from './helpers';

/** CSV import from the Events dialog into a new event: dry run, then import, then the counts. */
test.beforeEach(({}, info) => test.skip(info.project.name !== 'desktop', 'creates an event; one viewport is enough'));

test('events: a Luma guest CSV creates a new event with the right guest counts', async ({ page }) => {
  const errors = watchErrors(page);
  const stamp = Date.now().toString(36);
  const name = `agent-e2e import ${stamp}`;
  await devLogin(page, 'director');
  await page.goto('/events?import=1');
  await settle(page);
  const dialog = openDialog(page);
  await expect(dialog.getByRole('heading', { name: 'import a CSV' })).toBeVisible();
  await dialog.getByLabel('event', { exact: true }).selectOption('__new');
  await dialog.getByLabel('event name').fill(name);
  await dialog.getByLabel('Luma event id').fill(`evt-e2e${stamp}`);
  await dialog.getByLabel('date').fill('2026-10-20');
  // Three registrations, two checked in.
  const csv = [
    'name,email,approval_status,created_at,checked_in_at',
    `Ada ${stamp},ada-${stamp}@example.org,approved,2026-10-10T18:00:00Z,2026-10-21T01:05:00Z`,
    `Grace ${stamp},grace-${stamp}@example.org,approved,2026-10-11T18:00:00Z,2026-10-21T01:10:00Z`,
    `Linus ${stamp},linus-${stamp}@example.org,approved,2026-10-12T18:00:00Z,`,
  ].join('\r\n');
  await dialog.getByLabel('CSV file').setInputFiles({ name: 'guests.csv', mimeType: 'text/csv', buffer: Buffer.from(csv) });

  await dialog.getByRole('button', { name: 'check file' }).click();
  const result = dialog.locator('[data-import-result]');
  await expect(result).toContainText('Checked — nothing saved yet.');
  await expect(result).toContainText('3 registered, 2 checked in (67%)');

  await dialog.getByRole('button', { name: 'import', exact: true }).click();
  await expect(result).toContainText('Imported.');
  await expect(result).toContainText('3 rows: 3 new, 0 updated, 0 skipped.');
  await expect(result).toContainText('3 registered, 2 checked in (67%)');

  // The new event opens with the same numbers.
  await result.getByRole('link', { name: 'open the event →' }).click();
  await expect(page).toHaveURL(/\/events\?id=/);
  await settle(page);
  const detail = openDialog(page);
  await expect(detail.getByRole('heading', { level: 2, name })).toBeVisible();
  // Exact tile label; the figure settles after the one-off count-up.
  const figure = (label: string) => detail.locator('.kpi').filter({ has: page.locator('.kpi-label', { hasText: new RegExp(`^${label}$`) }) }).locator('.kpi-value');
  await expect(figure('registered')).toHaveText('3');
  await expect(figure('checked in')).toHaveText('2');
  expect(errors).toEqual([]);
});
