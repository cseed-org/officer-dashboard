import { expect, test } from '@playwright/test';
import { devLogin, openDialog, overflow, settle, watchErrors } from './helpers';

/** Admin flows (adopted from the integration QA run). Each run adds an officer and a revoked token. */
test.beforeEach(({}, info) => test.skip(info.project.name !== 'desktop', 'creates records; one viewport is enough'));
const stamp = () => Date.now().toString(36);

test('admin: add an officer in the drawer, change their role, deactivate and reactivate them', async ({ page }) => {
  const errors = watchErrors(page);
  await devLogin(page, 'director');
  await page.goto('/admin');
  await settle(page);
  await expect(page.locator('[data-officer-count]')).toContainText(/active officers?/);

  await page.locator('[data-admin-create]').click();
  const drawer = openDialog(page);
  await expect(drawer.getByRole('heading', { name: 'add an officer' })).toBeVisible();
  const name = `agent-e2e officer ${stamp()}`;
  const discordId = `9${Date.now()}${Math.floor(1000 + Math.random() * 9000)}`; // 18 digits
  // Empty submit: the first field is flagged and focused.
  await drawer.getByRole('button', { name: 'add officer' }).click();
  await expect(drawer.getByLabel('Discord user id')).toBeFocused();
  await drawer.getByLabel('Discord user id').fill(discordId);
  await drawer.getByLabel('name', { exact: true }).fill(name);
  await drawer.getByLabel('title').fill('QA');
  // One column at the standard control height.
  const heights = await drawer.locator('.input').evaluateAll((els) => els.map((el) => Math.round(el.getBoundingClientRect().height)));
  expect(new Set(heights)).toEqual(new Set([36]));
  await drawer.getByRole('button', { name: 'add officer' }).click();
  await expect(openDialog(page)).toHaveCount(0);
  await expect(page.getByText(`${name} can now sign in with Discord.`)).toBeVisible();

  const row = page.locator(`tr[data-officer="${name}"]`);
  const toDirector = row.getByRole('group', { name: `Role for ${name}` }).getByRole('button', { name: 'director' });
  // Cancelling the confirmation changes nothing.
  page.once('dialog', (d) => void d.dismiss());
  await toDirector.click();
  await expect(toDirector).toHaveAttribute('aria-pressed', 'false');
  page.once('dialog', (d) => void d.accept());
  await toDirector.click();
  await expect(page.getByText(`${name} is now a director.`)).toBeVisible();
  await expect(toDirector).toHaveAttribute('aria-pressed', 'true');
  page.once('dialog', (d) => void d.accept());
  await row.getByRole('group', { name: `Role for ${name}` }).getByRole('button', { name: 'officer', exact: true }).click();
  await expect(page.getByText(`${name} is now an officer.`)).toBeVisible();

  // Deactivate from the ⋯ menu (keyboard), then it is hidden until "show deactivated".
  await row.getByRole('button', { name: `Actions for ${name}` }).click();
  await expect(page.getByRole('menuitem', { name: 'deactivate' })).toBeFocused();
  page.once('dialog', (d) => void d.accept());
  await page.keyboard.press('Enter');
  await expect(page.getByText(`${name} is deactivated and signed out.`)).toBeVisible();
  await expect(row).toHaveCount(0);
  await page.getByLabel('show deactivated').check();
  await expect(row).toHaveClass(/is-inactive/);
  await row.getByRole('button', { name: `Actions for ${name}` }).click();
  page.once('dialog', (d) => void d.accept());
  await page.getByRole('menuitem', { name: 'reactivate' }).click();
  await expect(page.getByText(`${name} is active again.`)).toBeVisible();
  expect(await overflow(page)).toEqual([]);
  expect(errors).toEqual([]);
});

test('admin: create a service token in the drawer, see the secret once, revoke it from the menu', async ({ page }) => {
  const errors = watchErrors(page);
  await devLogin(page, 'director');
  await page.goto('/admin?tab=tokens');
  await settle(page);
  await page.locator('[data-admin-create]').click();
  const drawer = openDialog(page);
  const name = `agent-e2e token ${stamp()}`;
  await drawer.getByLabel('name', { exact: true }).fill(name);
  await drawer.getByRole('checkbox', { name: /ingest:luma/ }).check();
  await drawer.getByRole('button', { name: 'create token' }).click();
  await expect(openDialog(page)).toHaveCount(0);
  const secret = page.locator('[data-secret]');
  await expect(secret).toBeVisible();
  expect(((await secret.textContent()) ?? '').trim().length).toBeGreaterThan(20);
  await expect(page.getByRole('heading', { name: `copy “${name}” now` })).toBeFocused();
  const row = page.locator(`tr[data-token="${name}"]`);
  await expect(row.locator('.dot-label')).toContainText('ingest:luma');
  await row.getByRole('button', { name: `Actions for ${name}` }).click();
  page.once('dialog', (d) => void d.accept());
  await page.getByRole('menuitem', { name: 'revoke' }).click();
  await expect(row.getByText('revoked')).toBeVisible();
  // A revoked token's secret leaves the screen.
  await expect(page.locator('[data-secret]')).toHaveCount(0);
  expect(errors).toEqual([]);
});

test('admin: audit log rows filter by actor, action and date', async ({ page }) => {
  await devLogin(page, 'director');
  await page.goto('/admin?tab=audit');
  await settle(page);
  const rows = page.locator('.audit-row');
  await expect(rows.first()).toBeVisible();
  const total = await rows.count();
  // Exact time on hover.
  await expect(rows.first().locator('time')).toHaveAttribute('title', /\d{4}/);
  const actor = (await page.locator('#audit-actor option').nth(1).textContent())!.trim();
  await page.locator('#audit-actor').selectOption(actor);
  await expect(page.locator('[data-audit-count]')).toContainText(`of ${total} loaded`);
  for (const t of await rows.locator('strong').allTextContents()) expect(t).toBe(actor);
  await page.locator('#audit-since').fill('2099-01-01');
  await expect(page.getByText('No entries match')).toBeVisible();
  await page.getByRole('button', { name: 'clear' }).click();
  await expect(rows).toHaveCount(total);
});
