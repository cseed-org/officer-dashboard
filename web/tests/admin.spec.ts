import { expect, test } from '@playwright/test';
import { devLogin, overflow, settle, watchErrors } from './helpers';

/** Admin flows (adopted from the integration QA run). Each run adds an officer and a revoked token. */
test.beforeEach(({}, info) => test.skip(info.project.name !== 'desktop', 'creates records; one viewport is enough'));
const stamp = () => Date.now().toString(36);

test('admin: add an officer, make them a director, then an officer again', async ({ page }) => {
  const errors = watchErrors(page);
  await devLogin(page, 'director');
  await page.goto('/admin');
  await settle(page);
  const name = `agent-e2e officer ${stamp()}`;
  const discordId = `9${Date.now()}${Math.floor(1000 + Math.random() * 9000)}`; // 18 digits
  await page.getByLabel('Discord user id').fill(discordId);
  await page.getByLabel('name', { exact: true }).fill(name);
  await page.getByRole('button', { name: 'add officer' }).click();
  await expect(page.getByText(`${name} can now sign in with Discord.`)).toBeVisible();
  const role = page.getByLabel(`Role for ${name}`);
  await role.selectOption('director');
  await expect(page.getByText(`${name} is now a director.`)).toBeVisible();
  await expect(role).toHaveValue('director');
  await role.selectOption('officer');
  await expect(page.getByText(`${name} is now an officer.`)).toBeVisible();
  await expect(role).toHaveValue('officer');
  expect(await overflow(page)).toEqual([]);
  expect(errors).toEqual([]);
});

test('admin: create a service token, see the secret once, revoke it', async ({ page }) => {
  const errors = watchErrors(page);
  await devLogin(page, 'director');
  await page.goto('/admin?tab=tokens');
  await settle(page);
  const name = `agent-e2e token ${stamp()}`;
  await page.getByLabel('name', { exact: true }).fill(name);
  await page.getByRole('checkbox', { name: /ingest:luma/ }).check();
  await page.getByRole('button', { name: 'create token' }).click();
  const secret = page.locator('[data-secret]');
  await expect(secret).toBeVisible();
  expect(((await secret.textContent()) ?? '').trim().length).toBeGreaterThan(20);
  await expect(page.getByRole('heading', { name: `copy “${name}” now` })).toBeFocused();
  const row = page.locator('tr', { hasText: name });
  await expect(row).toHaveCount(1);
  page.once('dialog', (d) => void d.accept());
  await row.getByRole('button', { name: 'revoke' }).click();
  await expect(row.getByText('revoked')).toBeVisible();
  // A revoked token's secret leaves the screen.
  await expect(page.locator('[data-secret]')).toHaveCount(0);
  expect(errors).toEqual([]);
});
