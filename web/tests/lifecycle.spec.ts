import { expect, test, type Page } from '@playwright/test';
import { devLogin, openDialog, overflow, settle, watchErrors } from './helpers';

/**
 * The full purchase lifecycle through the v2 UI against the real API: an officer creates a
 * club purchase with files in the new-request drawer, a director denies-without-comment
 * (blocked), approves, marks it bought with the actual amount, then closes it. A second run
 * covers a reimbursement through payout. Each run creates new requests (agent-e2e marker).
 */
const badge = (page: Page) => openDialog(page).locator('.detail-sub .badge');

test('club purchase: officer submits, director approves, records the purchase and closes it', async ({ page }) => {
  const errors = watchErrors(page);
  const marker = `agent-e2e-${Date.now()}`;
  await devLogin(page, 'officer');
  await page.goto('/finance');
  await settle(page);
  await page.locator('[data-new-request]').click();
  const form = openDialog(page);
  await expect(form.getByRole('heading', { name: 'new purchase request' })).toBeVisible();
  await expect(form.getByLabel('what is it?')).toBeVisible();

  // Empty submit: field errors, first field focused.
  await form.getByRole('button', { name: 'submit for approval' }).click();
  await expect(form.getByText('fields need attention')).toBeVisible();
  await expect(form.getByLabel('what is it?')).toBeFocused();

  await form.getByLabel('club buys it').check();
  await form.getByLabel('what is it?').fill(`Hack Night pizza ${marker}`);
  await form.getByLabel('vendor').fill('Pagliacci');
  await form.getByLabel('quantity').fill('6');
  await form.getByLabel('unit cost').fill('24.50');
  await form.getByLabel('tax + shipping').fill('12.30');
  await expect(form.locator('[data-total]')).toHaveText('$159.30');
  await form.getByLabel('budget category').selectOption({ label: '5200 · Food & events' });
  await form.getByLabel('fund', { exact: true }).selectOption({ label: 'General operating' });
  await form.getByLabel('justification').fill('Dinner for about 40 hackers at Hack Night.');
  await form.getByLabel(/quote or receipt files/).setInputFiles([{ name: 'quote.pdf', mimeType: 'application/pdf', buffer: Buffer.from('%PDF-1.4 test quote') }]);
  expect(await overflow(page)).toEqual([]);
  await form.getByRole('button', { name: 'submit for approval' }).click();

  // The drawer swaps to the new request, focus on the confirmation.
  await expect(page).toHaveURL(/\/finance\?id=/);
  const detail = openDialog(page);
  await expect(detail.getByRole('heading', { level: 2, name: `Hack Night pizza ${marker}` })).toBeVisible();
  await expect(detail.getByText('Submitted. A director has been notified')).toBeVisible();
  await expect(detail.locator('[data-status-region]')).toBeFocused();
  await expect(badge(page)).toHaveText('pending');
  await expect(detail.getByText('quote.pdf')).toBeVisible();
  await expect(detail.getByRole('button', { name: /^approve/ })).toHaveCount(0);
  const url = page.url();

  // The list behind the drawer has it.
  await page.keyboard.press('Escape');
  await expect(page.locator('main').getByRole('link', { name: `Hack Night pizza ${marker}` })).toBeVisible();

  // Director.
  await devLogin(page, 'director');
  await page.goto(url);
  await settle(page);
  const d = openDialog(page);
  await d.getByRole('button', { name: 'deny' }).click();
  await expect(d.getByText('Say why it is denied')).toBeVisible();
  await d.getByLabel(/comment/).fill('Approved for Hack Night.');
  page.once('dialog', (x) => void x.accept());
  await d.getByRole('button', { name: 'approve $159.30' }).click();
  await expect(d.getByText('Approved. The requester has been notified.')).toBeVisible();
  await expect(badge(page)).toHaveText('approved');
  await expect(d.getByText('“Approved for Hack Night.”')).toBeVisible();

  await d.getByRole('button', { name: 'mark as buying' }).click();
  await expect(badge(page)).toHaveText('buying');
  await d.getByLabel('actual total').fill('151.10');
  await d.getByRole('button', { name: 'mark bought' }).click();
  await expect(d.getByText('Marked bought and posted $151.10')).toBeVisible();
  await expect(badge(page)).toHaveText('bought');
  await d.getByRole('button', { name: 'close', exact: true }).click();
  await expect(badge(page)).toHaveText('closed');
  await expect(d.locator('ol.timeline')).toContainText('bought · $151.10');
  expect(errors).toEqual([]);
});

test('reimbursement: director approves, approves the payout and confirms it was paid', async ({ page }) => {
  const errors = watchErrors(page);
  const marker = `agent-e2e-${Date.now()}`;
  await devLogin(page, 'officer');
  await page.goto('/finance?new=1');
  await settle(page);
  const form = openDialog(page);
  await form.getByLabel('what is it?').fill(`Workshop markers ${marker}`);
  await form.getByLabel('vendor').fill('Target');
  await form.getByLabel('unit cost').fill('18');
  await form.getByLabel('budget category').selectOption({ label: '5100 · Program supplies' });
  await form.getByLabel('justification').fill('Markers for BuildHer.');
  await form.getByRole('button', { name: 'submit for approval' }).click();
  await expect(page).toHaveURL(/\/finance\?id=/);
  const url = page.url();

  await devLogin(page, 'director');
  await page.goto(url);
  await settle(page);
  const d = openDialog(page);
  page.once('dialog', (x) => void x.accept());
  await d.getByRole('button', { name: /^approve/ }).click();
  await expect(badge(page)).toHaveText('approved');
  await d.getByLabel('actual total').fill('16.40');
  await d.getByRole('button', { name: 'approve payout' }).click();
  await expect(d.getByText('Payout of $16.40')).toBeVisible();
  await expect(badge(page)).toHaveText('approved for payout — awaiting payment');
  page.once('dialog', (x) => void x.accept());
  await d.getByRole('button', { name: 'confirm reimbursement paid' }).click();
  await expect(badge(page)).toHaveText('closed');
  const timeline = d.locator('ol.timeline');
  await expect(timeline).toContainText('payout approved · $16.40');
  await expect(timeline).toContainText('reimbursement paid');
  expect(errors).toEqual([]);
});
