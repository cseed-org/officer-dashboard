import { expect, test } from '@playwright/test';
import { layoutFailures, measure } from './measure';
import { devLogin, settle, shot } from './helpers';

/**
 * The purchase-request journey in dev-auth mode: an officer fills in /requests/new with a
 * receipt and submits, then a director approves it on /requests/:id.
 * Runs against the real API and the seeded local D1; each run creates new requests
 * (titles carry an agent-e2e marker).
 */
test('an officer submits a purchase request with receipts and a director approves it', async ({ page }, info) => {
  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(e.message));
  const marker = `agent-e2e-${Date.now()}`;

  await devLogin(page, 'officer');
  await page.goto('/requests/new');
  await settle(page);

  // Empty submit: contract validation shows field errors and focuses the first one.
  await page.getByRole('button', { name: 'submit for approval' }).click();
  await expect(page.getByText('fields need attention')).toBeVisible();
  await expect(page.getByLabel('what is it?')).toBeFocused();
  await expect(page.getByText('Say what you are buying.')).toBeVisible();
  await expect(page.getByLabel('what is it?')).toHaveAttribute('aria-invalid', 'true');
  await shot(page, info.project.name, 'light', 'requests_new_errors');

  await page.getByLabel('club buys it').check();
  await page.getByLabel('what is it?').fill(`Hack Night pizza ${marker}`);
  await page.getByLabel('vendor').fill('Pagliacci');
  await page.getByLabel('item link').fill('https://example.com/pizza');
  await page.getByLabel('quantity').fill('6');
  await page.getByLabel('unit cost').fill('24.50');
  await page.getByLabel('tax + shipping').fill('12.30');
  await expect(page.locator('[data-total]')).toHaveText('$159.30');
  await page.getByLabel('budget category').selectOption({ label: '5200 · Food & events' });
  await page.getByLabel('fund', { exact: true }).selectOption({ label: 'General operating' });
  await page.getByLabel('event').selectOption({ index: 1 });
  await page.getByLabel('functional class').selectOption('program');
  await page.getByLabel('justification').fill('Dinner for about 40 hackers at Hack Night.');
  await page.getByLabel(/quote or receipt files/).setInputFiles([
    { name: 'quote.pdf', mimeType: 'application/pdf', buffer: Buffer.from('%PDF-1.4 test quote') },
    { name: 'menu.png', mimeType: 'image/png', buffer: Buffer.from([0x89, 0x50, 0x4e, 0x47]) },
  ]);
  // A wrong file type is refused with a message, the good ones stay.
  await page.getByLabel(/quote or receipt files/).setInputFiles([{ name: 'notes.txt', mimeType: 'text/plain', buffer: Buffer.from('x') }]);
  await expect(page.getByText(/notes\.txt: use PDF/)).toBeVisible();
  await expect(page.getByRole('list', { name: 'Files to upload' }).getByRole('listitem')).toHaveCount(2);
  await shot(page, info.project.name, 'light', 'requests_new_filled');

  await page.getByRole('button', { name: 'submit for approval' }).click();
  await expect(page).toHaveURL(/\/requests\/(?!new)[^/?]+$/);
  await settle(page);
  await expect(page.getByText('Submitted. A director has been notified')).toBeVisible();
  await expect(page.locator('h1')).toHaveText(`Hack Night pizza ${marker}`);
  await expect(page.locator('.page-title .badge')).toHaveText('pending');
  await expect(page.getByText('quote.pdf')).toBeVisible();
  await expect(page.getByText('menu.png')).toBeVisible();
  await expect(page.getByText('$159.30').first()).toBeVisible();
  // An officer gets no decision buttons.
  await expect(page.getByRole('button', { name: /^approve/ })).toHaveCount(0);
  const requestUrl = page.url();

  // The request is in "mine".
  await page.goto('/requests');
  await settle(page);
  await expect(page.getByRole('link', { name: `Hack Night pizza ${marker}` })).toBeVisible();

  // Director: deny needs a comment, then approve.
  await devLogin(page, 'director');
  await page.goto(requestUrl);
  await settle(page);
  await page.getByRole('button', { name: 'deny' }).click();
  await expect(page.getByText('Say why it is denied')).toBeVisible();
  await page.getByLabel(/comment/).fill('Approved for Hack Night.');
  await page.getByRole('button', { name: 'approve $159.30' }).click();
  await expect(page.getByText('Approved. The requester has been notified.')).toBeVisible();
  await expect(page.locator('.page-title .badge')).toHaveText('approved');
  await expect(page.getByText('“Approved for Hack Night.”')).toBeVisible();
  expect(layoutFailures(await page.evaluate(measure))).toEqual([]);
  await shot(page, info.project.name, 'light', 'requests_detail_approved');
  expect(errors).toEqual([]);
});

test('a director records a reimbursement and confirms the payout', async ({ page }, info) => {
  await devLogin(page, 'officer');
  await page.goto('/requests/new');
  await settle(page);
  await page.getByLabel('what is it?').fill(`Workshop markers agent-e2e-${Date.now()}`);
  await page.getByLabel('vendor').fill('Target');
  await page.getByLabel('unit cost').fill('18');
  await page.getByLabel('budget category').selectOption({ label: '5100 · Program supplies' });
  await page.getByLabel('justification').fill('Markers for BuildHer.');
  await page.getByRole('button', { name: 'submit for approval' }).click();
  await expect(page).toHaveURL(/\/requests\/(?!new)[^/?]+$/);
  const url = page.url();

  await devLogin(page, 'director');
  await page.goto(url);
  await settle(page);
  await page.getByRole('button', { name: /^approve/ }).click();
  await expect(page.locator('.page-title .badge')).toHaveText('approved');
  await page.getByLabel('actual total').fill('16.40');
  await page.getByRole('button', { name: 'mark reimbursed' }).click();
  await expect(page.getByText('posted $16.40 to the ledger')).toBeVisible();
  page.once('dialog', (d) => void d.accept());
  await page.getByRole('button', { name: 'confirm reimbursement paid' }).click();
  await expect(page.locator('.page-title .badge')).toHaveText('closed');
  await expect(page.getByText('reimbursement paid · closed')).toBeVisible();
  await shot(page, info.project.name, 'light', 'requests_detail_closed');
});
