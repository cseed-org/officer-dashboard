import { expect, test, type Page } from '@playwright/test';
import { devLogin, openDialog, overflow, settle, THEMES, useTheme, watchErrors } from './helpers';

/**
 * The Cloudflare sign-in email (access email): directors type it when inviting or adding
 * someone and edit it on the person; the UI says it changes nothing in Cloudflare yet;
 * officers never see it.
 */

const stamp = () => `${Date.now()}${Math.floor(Math.random() * 1e4)}`;
const inviteeId = () => `9${String(Date.now()).padStart(13, '0')}${Math.floor(Math.random() * 1e4).toString().padStart(4, '0')}`;
const NOT_YET = 'isn’t switched on yet';

async function openPerson(page: Page, id: string) {
  await page.goto(`/officers?person=${id}`);
  await expect(openDialog(page).getByRole('heading', { level: 2 })).toBeVisible();
}

test('a director invites with a Cloudflare sign-in email and edits it before acceptance', async ({ page }) => {
  const errors = watchErrors(page);
  const id = inviteeId();
  const email = `robin.${stamp()}@example.org`;
  await devLogin(page, 'director');
  await page.goto('/officers');
  await settle(page);
  await page.locator('[data-open-invite]').click();
  const d = openDialog(page);
  await d.getByLabel('Name').fill('Robin Access');
  await d.getByLabel('Discord ID').fill(id);
  const field = d.locator('[data-invite-access-email]');
  await expect(field).toHaveValue(''); // never filled in for them
  await expect(d.locator('#inv-access-hint')).toContainText(NOT_YET);
  await field.fill('not-an-email');
  await d.locator('[data-send-invite]').click();
  await expect(d.locator('#inv-accessEmail-error')).toBeVisible();
  await field.fill(`  ${email.toUpperCase()} `);
  await d.locator('[data-send-invite]').click();
  await expect(d.locator('[data-invite-link] code')).toContainText('/login?invite=');
  await d.getByRole('button', { name: 'Done' }).click();

  await page.getByRole('tab', { name: 'Invites', exact: true }).click();
  const row = page.locator(`[data-invite="${id}"]`);
  await expect(row.locator('[data-invite-access-email]')).toHaveText(`Cloudflare sign-in email: ${email}`);
  await row.locator('[data-invite-edit]').click();
  const edit = openDialog(page);
  const editField = edit.locator('[data-invite-edit-access-email]');
  await expect(editField).toHaveValue(email);
  await expect(edit.locator('#inv-edit-access-hint')).toContainText(NOT_YET);
  const changed = `robin.${stamp()}@example.org`;
  await editField.fill(changed);
  await edit.locator('[data-invite-edit-save]').click();
  await expect(openDialog(page)).toHaveCount(0);
  await expect(row.locator('[data-invite-access-email]')).toHaveText(`Cloudflare sign-in email: ${changed}`);
  expect(errors).toEqual([]);
});

test('a director sets, refuses a duplicate of, and clears an officer’s sign-in email in the drawer', async ({ page }) => {
  const errors = watchErrors(page);
  const email = `ava.${stamp()}@example.org`;
  await devLogin(page, 'director');
  await openPerson(page, 'user-ava');
  const d = openDialog(page);
  await d.getByRole('tab', { name: 'Manage', exact: true }).click();
  const section = d.locator('[data-access-email]');
  await expect(section.getByRole('heading', { name: 'Cloudflare sign-in email' })).toBeVisible();
  await expect(section.locator('#pd-access-hint')).toContainText(NOT_YET);
  const input = section.getByLabel('Email', { exact: true });
  await input.fill(email);
  await section.locator('[data-access-email-save]').click();
  await expect(section.getByRole('status')).toHaveText('Saved ✓ Nothing changed in Cloudflare.');
  await page.reload();
  await expect(openDialog(page).getByRole('heading', { level: 2, name: 'Ava Romero' })).toBeVisible();
  await openDialog(page).getByRole('tab', { name: 'Manage', exact: true }).click();
  await expect(openDialog(page).locator('[data-access-email]').getByLabel('Email', { exact: true })).toHaveValue(email);

  // The same email on someone else is refused, naming who has it.
  await openPerson(page, 'user-riley');
  await openDialog(page).getByRole('tab', { name: 'Manage', exact: true }).click();
  const riley = openDialog(page).locator('[data-access-email]');
  await riley.getByLabel('Email', { exact: true }).fill(email);
  await riley.locator('[data-access-email-save]').click();
  await expect(riley.getByRole('alert')).toContainText('Ava Romero');

  // Clearing it.
  await openPerson(page, 'user-ava');
  await openDialog(page).getByRole('tab', { name: 'Manage', exact: true }).click();
  const ava = openDialog(page).locator('[data-access-email]');
  await ava.getByLabel('Email', { exact: true }).fill('');
  await ava.locator('[data-access-email-save]').click();
  await expect(ava.getByRole('status')).toHaveText('Removed ✓ Nothing changed in Cloudflare.');
  // The duplicate's 409 is the refusal this test asked for; nothing else may fail.
  expect(errors.filter((e) => e !== 'http: /api/admin/users/user-riley → 409')).toEqual([]);
});

test('officers never see anyone’s sign-in email, their own included', async ({ page }) => {
  const email = `marcus.${stamp()}@example.org`;
  await devLogin(page, 'director');
  await openPerson(page, 'user-marcus');
  await openDialog(page).getByRole('tab', { name: 'Manage', exact: true }).click();
  const section = openDialog(page).locator('[data-access-email]');
  await section.getByLabel('Email', { exact: true }).fill(email);
  await section.locator('[data-access-email-save]').click();
  await expect(section.getByRole('status')).toContainText('Saved');

  await page.context().clearCookies();
  await devLogin(page, 'officer');
  const bodies: string[] = [];
  page.on('response', async (r) => {
    if (r.url().includes('/api/')) bodies.push(await r.text().catch(() => ''));
  });
  for (const id of ['user-marcus', 'user-priya', 'user-ava']) {
    await openPerson(page, id);
    await expect(openDialog(page).getByRole('tab', { name: 'Manage' })).toHaveCount(0);
    await expect(openDialog(page)).not.toContainText('Cloudflare sign-in email');
  }
  await page.goto('/officers');
  await settle(page);
  await expect(page.locator('body')).not.toContainText(email);
  expect(bodies.some((b) => b.includes('accessEmail') || b.includes(email))).toBe(false);
});

for (const theme of THEMES) {
  test(`the sign-in email fields fit and read clearly (${theme})`, async ({ page }, info) => {
    await useTheme(page, theme);
    await devLogin(page, 'director');
    await page.goto('/officers');
    await settle(page);
    await page.locator('[data-open-invite]').click();
    await expect(openDialog(page).locator('[data-invite-access-email]')).toBeVisible();
    expect(await overflow(page)).toEqual([]);
    if (process.env.SHOTS) await page.screenshot({ path: `test-results/shots/access-email/invite-${info.project.name}-${theme}.png`, fullPage: true });
    await openDialog(page).getByRole('button', { name: 'Cancel' }).click();
    await openPerson(page, 'user-priya');
    await openDialog(page).getByRole('tab', { name: 'Manage', exact: true }).click();
    await expect(openDialog(page).locator('[data-access-email]')).toBeVisible();
    expect(await overflow(page)).toEqual([]);
    if (process.env.SHOTS) await page.screenshot({ path: `test-results/shots/access-email/drawer-${info.project.name}-${theme}.png`, fullPage: true });
  });
}

test('add account: the calendar email is copied to the sign-in email only on request', async ({ page }) => {
  await devLogin(page, 'director');
  await page.goto('/officers?tab=access');
  await settle(page);
  await page.locator('[data-admin-create]').click();
  const d = openDialog(page);
  const email = `new.${stamp()}@example.org`;
  await d.locator('#au-email').fill(email);
  const access = d.locator('[data-access-email]');
  await expect(access).toHaveValue('');
  await expect(d.locator('#au-access-hint')).toContainText(NOT_YET);
  await d.locator('[data-access-email-copy]').click();
  await expect(access).toHaveValue(email);
  await expect(d.locator('[data-access-email-copy]')).toHaveCount(0);
});

test('re-inviting with the field empty keeps the stored email; clearing is done in Edit invite details', async ({ page }) => {
  const id = inviteeId();
  const email = `kept.${stamp()}@example.org`;
  await devLogin(page, 'director');
  await page.goto('/officers');
  await settle(page);
  const send = async (accessEmail: string) => {
    await page.locator('[data-open-invite]').click();
    const d = openDialog(page);
    await d.getByLabel('Name').fill('Kept Email');
    await d.getByLabel('Discord ID').fill(id);
    await d.locator('[data-invite-access-email]').fill(accessEmail);
    const req = page.waitForRequest((r) => r.url().endsWith('/api/invites') && r.method() === 'POST');
    await d.locator('[data-send-invite]').click();
    const body = (await req).postDataJSON() as Record<string, unknown>;
    await expect(d.locator('[data-invite-link] code')).toContainText('/login?invite=');
    return { d, body };
  };
  await send(email).then(({ d }) => d.getByRole('button', { name: 'Done' }).click());
  // The normal Invite button again, same Discord ID, field left empty: nothing is cleared.
  const { d, body } = await send('');
  expect('accessEmail' in body).toBe(false);
  await expect(d.locator('[data-invite-access-kept]')).toContainText(email);
  await d.getByRole('button', { name: 'Done' }).click();
  await page.getByRole('tab', { name: 'Invites', exact: true }).click();
  const row = page.locator(`[data-invite="${id}"]`);
  await expect(row.locator('[data-invite-access-email]')).toContainText(email);
  // Removing it is deliberate: clear the prefilled field in Edit invite details and save.
  await row.locator('[data-invite-edit]').click();
  const edit = openDialog(page);
  await expect(edit.locator('#inv-edit-access-hint')).toContainText('Clear the field and save to remove it');
  await edit.locator('[data-invite-edit-access-email]').fill('');
  const req = page.waitForRequest((r) => r.url().includes('/api/invites/') && r.method() === 'PATCH');
  await edit.locator('[data-invite-edit-save]').click();
  expect((await req).postDataJSON()).toMatchObject({ accessEmail: null });
  await expect(openDialog(page)).toHaveCount(0);
  await expect(row.locator('[data-invite-access-email]')).toHaveCount(0);
});

test('add account over an expired invite says where the carried-over email came from', async ({ page }) => {
  // The server carries an expired invite's email over (Go test TestAccessEmailAddAccountOverExpiredInvite);
  // here the response is shaped that way to check what the director is told.
  await page.route('**/api/admin/users', async (route) => {
    if (route.request().method() !== 'POST') return route.fallback();
    const res = await route.fetch();
    const json = await res.json();
    json.user.accessEmail = 'carried@example.org';
    await route.fulfill({ response: res, json });
  });
  await devLogin(page, 'director');
  await page.goto('/officers?tab=access');
  await settle(page);
  await page.locator('[data-admin-create]').click();
  const d = openDialog(page);
  await d.getByLabel('Discord user id').fill(`9${Date.now()}${Math.floor(1000 + Math.random() * 9000)}`);
  await d.getByLabel('name', { exact: true }).fill(`Carried ${stamp()}`);
  await d.getByLabel('subteam').selectOption({ label: 'Events' });
  await d.getByRole('button', { name: 'add officer' }).click();
  await expect(page.locator('[data-access-email-carried]')).toContainText('carried@example.org');
  await expect(page.locator('[data-access-email-carried]')).toContainText('expired invite');
});
