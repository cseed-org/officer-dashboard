import { expect, test } from '@playwright/test';
import { devLogin, settle } from './helpers';

/** Creating an event from a Luma export needs name, Luma id and date (the API requires all three). */
test('a new event from a CSV import needs a name, Luma id and date', async ({ page }) => {
  await devLogin(page, 'officer');
  await page.goto('/events/import');
  await settle(page);
  let posted = false;
  await page.route('**/api/events/import', (route) => {
    posted = true;
    return route.continue();
  });

  await page.getByLabel('event', { exact: true }).selectOption('__new');
  await page.getByLabel('CSV file').setInputFiles({ name: 'guests.csv', mimeType: 'text/csv', buffer: Buffer.from('email,checked_in_at\na@example.com,\n') });
  await page.getByRole('button', { name: 'check file' }).click();

  await expect(page.getByText('Name the new event.')).toBeVisible();
  await expect(page.getByText('Enter the Luma event id')).toBeVisible();
  await expect(page.getByText('Pick the event date.')).toBeVisible();
  await expect(page.getByLabel('event name')).toHaveAttribute('aria-invalid', 'true');
  await expect(page.getByLabel('Luma event id')).toHaveAttribute('aria-invalid', 'true');
  await expect(page.getByLabel('date')).toHaveAttribute('aria-invalid', 'true');
  expect(posted, 'nothing is sent until the fields are filled').toBe(false);

  // Filled in, the dry run reaches the server and reports back without saving.
  await page.getByLabel('event name').fill('E2E Import Night');
  await page.getByLabel('Luma event id').fill(`evt-e2e-${Date.now()}`);
  await page.getByLabel('date').fill('2026-11-20');
  await page.getByRole('button', { name: 'check file' }).click();
  await expect(page.locator('[data-import-result]')).toContainText('Checked — nothing saved yet.');
  expect(posted).toBe(true);
});
