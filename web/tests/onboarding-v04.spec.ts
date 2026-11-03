import { expect, test, type Page } from '@playwright/test';
import { devKeyParam, devLogin, openDialog, settle, watchErrors } from './helpers';

/**
 * V0.4 onboarding: the server gate, the correction loop (invitee → inviter edits → Confirm
 * updates), history and save reliability, and the shared-browser invite link. Runs against
 * the Go server with DEV_AUTH=1 (no Discord bot, so corrections aren't DMed).
 */
test.beforeEach(({}, info) => test.skip(info.project.name !== 'desktop', 'creates invitees; one viewport is enough'));

const discordId = () => `9${String(Date.now()).padStart(13, '0')}${Math.floor(Math.random() * 1e4).toString().padStart(4, '0')}`;

/** Invites someone as the director (API) and returns the invite id. */
async function inviteAsDirector(page: Page, name: string, id: string, subteamId = 'team-events') {
  await devLogin(page, 'director');
  const r = await page.request.post('/api/invites', { data: { name, discordId: id, subteamId, method: 'link' } });
  expect(r.ok()).toBe(true);
  const body = await r.json();
  await signOut(page);
  return body.invite.id as string;
}

/** Leave the page first: a signed-out Home would redirect to /login mid-navigation. */
async function signOut(page: Page) {
  await page.goto('about:blank');
  await page.context().clearCookies();
}

async function signInAs(page: Page, id: string) {
  await page.goto(`/auth/dev?discord=${id}${devKeyParam}`);
  await expect(page).toHaveURL(/\/onboarding$/);
  await settle(page);
}

const title = (page: Page) => page.locator('#step-title');
const next = (page: Page) => page.locator('[data-onb-next]');

test('gate, correction, inviter edit, history and finish', async ({ page, browser }) => {
  const errors = watchErrors(page);
  const id = discordId();
  await inviteAsDirector(page, 'Casey mcKenzie', id);
  await signInAs(page, id);

  // The API is closed until setup is done, and app pages send the invitee back here.
  const gated = await page.request.get('/api/events');
  expect(gated.status()).toBe(403);
  expect((await gated.json()).error.code).toBe('onboarding_required');
  await page.goto('/events');
  await expect(page).toHaveURL(/\/onboarding$/);
  await settle(page);
  await expect(title(page)).toContainText('hey Casey'); // not lower-cased

  // Confirm: report a problem, then the inviter fixes it and the step shows it.
  await next(page).click();
  await expect(page.locator('[data-prefill]')).toContainText('Casey mcKenzie');
  await page.locator('[data-correction-open]').click();
  await page.getByLabel(/What should .* fix\?/).fill('It is Casey McKenzie, and I am on Build');
  await page.getByRole('button', { name: /^Send to/ }).click();
  await expect(page.getByRole('alert')).toContainText('Discord DMs aren’t set up');
  await expect(page.locator('[data-correction]')).toHaveCount(0); // recorded; the form closes
  await expect(page.locator('[data-correction-pending]')).toBeVisible();
  await expect(next(page)).toBeEnabled(); // a notice, not a block

  const dirCtx = await browser.newContext();
  const dir = await dirCtx.newPage();
  await devLogin(dir, 'director');
  await dir.goto('/officers?tab=invites');
  await settle(dir);
  const row = dir.locator(`[data-invite="${id}"]`);
  await expect(row).toContainText('Accepted · setting up');
  await row.locator('[data-invite-edit]').click();
  const d = openDialog(dir);
  await d.getByLabel('Name').fill('Casey McKenzie');
  await d.getByLabel('Subteam').selectOption({ label: 'Build' });
  await d.locator('[data-invite-edit-save]').click();
  await expect(dir.locator('[data-invite-note]')).toContainText('Their setup shows the change now');
  await expect(row).toContainText('Build');
  await dirCtx.close();

  await page.reload();
  await settle(page);
  await expect(page.locator('[data-prefill]')).toContainText('Casey McKenzie');
  await expect(page.locator('[data-prefill]')).toContainText('Build');
  await expect(page.locator('[data-correction-pending]')).toHaveCount(0);

  // Profile: Back with entries that don't validate stays here with the errors showing.
  await next(page).click();
  await expect(page.locator('[data-profile-form]')).toBeVisible();
  await page.getByLabel('Phone · required').fill('12');
  await page.goBack();
  await expect(page.locator('[data-profile-form]')).toBeVisible();
  await expect(page.locator('#pf-phone-error')).toBeVisible();
  await expect(page.locator('[data-step-error]')).toContainText('aren’t saved yet');
  // A refresh resumes at the profile, never past it.
  await page.goto('/onboarding');
  await settle(page);
  await expect(page.locator('[data-profile-form]')).toBeVisible(); // resumed at step 3
  // Valid entries are saved on Back instead of being dropped.
  await page.getByLabel('Phone · required').fill('+1 206 555 0188');
  await page.getByLabel('Birthday month').selectOption('11');
  await page.getByLabel('Birthday day').selectOption('03');
  await page.locator('summary').click();
  await page.getByLabel('LinkedIn').fill('https://linkedin.com');
  await next(page).click(); // the server's LinkedIn rule: no trailing path needed
  await expect(page.locator('.onb[data-step="4"]')).toBeVisible();
  await page.goBack();
  await expect(page.locator('[data-profile-form]')).toBeVisible();
  await page.goBack();
  await expect(page.locator('[data-prefill]')).toBeVisible();
  await page.goForward();
  await page.goForward();
  await expect(page.locator('.onb[data-step="4"]')).toBeVisible();
  await page.reload();
  await settle(page);
  await expect(page.locator('.onb[data-step="4"]')).toBeVisible(); // refresh resumes here

  // Google consent returns to Calendar once the profile is saved.
  await page.goto('/onboarding?google=connected');
  await settle(page);
  await expect(page.locator('.onb[data-step="4"]')).toBeVisible();

  // Finish later while offline: stays signed in, says why.
  await page.context().setOffline(true);
  await page.locator('[data-finish-later]').click();
  await expect(page.locator('[data-step-error]')).toContainText('still signed in');
  await expect(page).toHaveURL(/\/onboarding/);
  await page.context().setOffline(false);

  await next(page).click(); // Set up later
  await expect(page.locator('.onb[data-step="5"]')).toBeVisible();
  await next(page).click();
  await expect(page.locator('[data-summary]')).toContainText('Casey McKenzie · Build');
  await expect(page.locator('[data-summary]')).toContainText('Phone and birthday saved');
  // After setup, Back leaves for Home instead of replaying the steps.
  await page.goBack();
  await expect(page).toHaveURL(/\/$/);
  await settle(page);
  expect((await page.request.get('/api/events')).status()).toBe(200);
  expect(errors.filter((e) => !/\/api\/events → 403$/.test(e))).toEqual([]);
});

test('Google return before the profile is saved goes to the profile step', async ({ page }) => {
  const id = discordId();
  await inviteAsDirector(page, 'Dana Ruiz', id);
  await signInAs(page, id);
  await page.goto('/onboarding?google=connected');
  await settle(page);
  await expect(page.locator('[data-profile-form]')).toBeVisible();
});

test('officers added from Access go through setup too', async ({ page }) => {
  const id = discordId();
  await devLogin(page, 'leadership');
  const r = await page.request.post('/api/admin/users', { data: { discordId: id, name: 'Eli Okafor', subteamId: 'team-design', role: 'director' } });
  expect(r.ok()).toBe(true);
  await signOut(page);
  await signInAs(page, id);
  await expect(title(page)).toContainText('hey Eli');
  await next(page).click();
  await expect(page.locator('[data-prefill]')).toContainText('Design');
  await expect(page.locator('[data-prefill]')).toContainText('Director');
});

test('an invite link in a browser where someone else is signed in', async ({ page }) => {
  await devLogin(page, 'officer');
  await page.goto('/login?invite=someone-elses-token');
  const box = page.locator('[data-invite-signed-in]');
  await expect(box).toBeVisible();
  await expect(box).toContainText('Signed in as');
  await expect(page.locator('[data-login]')).toBeHidden();
  await box.locator('[data-invite-sign-out]').click();
  await expect(page).toHaveURL(/\/login\?invite=someone-elses-token$/);
  await expect(page.locator('[data-login]')).toBeVisible();
  await expect(page.locator('[data-invite-note]')).toBeVisible();
  expect((await page.request.get('/api/me')).status()).toBe(401);
});
