import { expect, test, type Page } from '@playwright/test';
import { devLogin, settle } from './helpers';

/** Phase 2 QA findings (tmp/qa-report.md): F1, F2, F3, F4, F8. Runs on the real API + local D1. */

async function createRequest(page: Page, type: 'club_purchase' | 'reimbursement', title: string) {
  const res = await page.request.post('/api/requests', {
    data: {
      type,
      title,
      vendor: 'QA Vendor',
      quantity: 2,
      unitCostCents: 1250,
      taxShipCents: 100,
      expenseAccountId: 'acct-5100',
      fundId: 'fund-general',
      functionalClass: 'program',
      justification: 'QA fixes e2e',
      submit: true,
    },
  });
  expect(res.status(), await res.text()).toBe(200);
  return ((await res.json()) as { request: { id: string } }).request.id;
}

test('F3 + F4: approving asks for confirmation with the amount, then focus lands on the result', async ({ page }) => {
  await devLogin(page, 'officer');
  const id = await createRequest(page, 'club_purchase', `QA confirm approve agent-e2e-${Date.now()}`);
  await devLogin(page, 'director');
  await page.goto(`/requests/${id}`);
  await settle(page);

  const messages: string[] = [];
  // First dialog: cancel. Nothing changes.
  page.once('dialog', (d) => {
    messages.push(d.message());
    void d.dismiss();
  });
  await page.getByRole('button', { name: 'approve $26.00' }).click();
  await expect.poll(() => messages.length).toBe(1);
  expect(messages[0]).toContain('$26.00');
  await expect(page.locator('.page-title .badge')).toHaveText('pending');

  // Second: accept, using the keyboard like the QA journey.
  page.once('dialog', (d) => void d.accept());
  await page.getByRole('button', { name: 'approve $26.00' }).focus();
  await page.keyboard.press('Enter');
  await expect(page.locator('.page-title .badge')).toHaveText('approved');
  await expect(page.locator('[data-status-region]')).toBeFocused();
  await expect(page.locator('[data-status-region]')).toContainText('Approved.');
});

test('F1: a request being bought shows no approve button', async ({ page }) => {
  await devLogin(page, 'officer');
  const id = await createRequest(page, 'club_purchase', `QA fulfilling agent-e2e-${Date.now()}`);
  await devLogin(page, 'director');
  await page.goto(`/requests/${id}`);
  await settle(page);
  page.once('dialog', (d) => void d.accept());
  await page.getByRole('button', { name: /^approve/ }).click();
  await expect(page.locator('.page-title .badge')).toHaveText('approved');
  await page.getByRole('button', { name: 'mark as buying' }).click();
  await expect(page.locator('.page-title .badge')).toHaveText('buying');
  await expect(page.getByRole('button', { name: /^approve/ })).toHaveCount(0);
  await expect(page.getByRole('heading', { name: 'your decision' })).toHaveCount(0);
  await expect(page.getByText('If the purchase fails, the purchasing agent puts the request back')).toBeVisible();
  // The settle form is still there.
  await expect(page.getByRole('heading', { name: 'record the purchase' })).toBeVisible();
  await page.reload();
  await settle(page);
  await expect(page.getByRole('button', { name: /^approve/ })).toHaveCount(0);
});

test('F2: a director viewing their own pending request is told another director must approve it', async ({ page }) => {
  await devLogin(page, 'director');
  const id = await createRequest(page, 'reimbursement', `QA own request agent-e2e-${Date.now()}`);
  await page.goto(`/requests/${id}`);
  await settle(page);
  await expect(page.locator('[data-self-approval]')).toHaveText(/Another director must approve your own requests/);
  await expect(page.getByRole('button', { name: /^approve/ })).toHaveCount(0);
});

test('F8: revoking a just-created token closes its secret panel', async ({ page }) => {
  await devLogin(page, 'director');
  await page.goto('/admin/tokens');
  await settle(page);
  const name = `QA token agent-e2e-${Date.now()}`;
  await page.getByLabel('name').fill(name);
  await page.getByLabel(/ingest:luma/).check();
  await page.getByRole('button', { name: 'create token' }).click();
  await expect(page.locator('[data-secret]')).toBeVisible();

  const row = page.getByRole('row', { name: new RegExp(name) });
  page.once('dialog', (d) => void d.accept());
  await row.getByRole('button', { name: 'revoke' }).click();
  await expect(page.getByText(`“${name}” is revoked.`)).toBeVisible();
  await expect(page.locator('[data-secret]')).toHaveCount(0);
  await expect(page.getByText(/copy “/)).toHaveCount(0);
});

test('F10: the first Tab stop is a skip link that moves focus to the page content', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop', 'keyboard journey');
  await devLogin(page, 'officer');
  await page.goto('/requests/new');
  await settle(page);
  await page.keyboard.press('Tab');
  const skip = page.getByRole('link', { name: 'skip to content' });
  await expect(skip).toBeFocused();
  await expect(skip).toBeInViewport();
  await page.keyboard.press('Enter');
  await expect(page.locator('main')).toBeFocused();
  await page.keyboard.press('Tab');
  await expect(page.locator('main a, main input, main button').first()).toBeFocused();
});
