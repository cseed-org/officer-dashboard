import { expect, test, type Page } from '@playwright/test';
import { devLogin, openDialog, overflow, settle, THEMES, useTheme, watchErrors, devSignIn } from './helpers';

/**
 * Officers and onboarding against the Go server (API_ORIGIN, DEV_AUTH=1, seeded). No
 * Discord bot, SMTP or Google is configured there, so invites are copy-link only and
 * calendar delivery shows "not configured".
 */

// A fresh invitee per run and project, so reruns don't collide with an accepted invite.
const inviteeId = () => `9${String(Date.now()).padStart(13, '0')}${Math.floor(Math.random() * 1e4).toString().padStart(4, '0')}`;

async function invite(page: Page, name: string, discordId: string) {
  await page.goto('/officers');
  await settle(page);
  await page.locator('[data-open-invite]').click();
  const d = openDialog(page);
  await d.getByLabel('Name').fill(name);
  await d.getByLabel('Discord ID').fill(discordId);
  // No bot token: DM is unavailable and the dialog says so.
  await expect(d.locator('[data-invite-dm]')).toBeDisabled();
  await expect(d).toContainText('copy-link only');
  await d.locator('[data-send-invite]').click();
  await expect(d.locator('[data-invite-link] code')).toContainText('/login?invite=');
  await d.getByRole('button', { name: 'Done' }).click();
}

test('invite → sign in as the invitee → onboarding → home', async ({ page }) => {
  const errors = watchErrors(page);
  const id = inviteeId();
  await devLogin(page, 'director');
  await invite(page, 'Robin Park', id);
  await page.getByRole('tab', { name: 'Invites', exact: true }).click();
  await expect(page.locator(`[data-invite="${id}"]`)).toContainText('Awaiting sign-in');

  // Inviting an existing account is refused with a clear reason.
  await page.locator('[data-open-invite]').click();
  const d = openDialog(page);
  await d.getByLabel('Name').fill('Priya');
  await d.getByLabel('Discord ID').fill('100000000000000002');
  await d.locator('[data-send-invite]').click();
  await expect(d.getByRole('alert')).toContainText('already belongs to');
  await d.getByRole('button', { name: 'Cancel' }).click();

  // Sign in as the invitee: their account is created and onboarding starts.
  await page.context().clearCookies();
  await devSignIn(page, { discord: id });
  await expect(page).toHaveURL(/\/onboarding$/);
  // The welcome intro comes first; skipping it lands on the first intake stage.
  const skipIntro = async () => {
    await page.getByRole('button', { name: 'Skip' }).click();
    await expect(page.locator('.wi')).toHaveCount(0, { timeout: 5000 });
  };
  await skipIntro();
  await expect(page.locator('#step-title')).toContainText('Robin'); // names keep their capitals
  // Other pages send unfinished invitees back here.
  await page.goto('/finance');
  await expect(page).toHaveURL(/\/onboarding$/);
  await skipIntro();

  // Profile: inline errors, then a valid month-day birthday.
  await expect(page.locator('[data-finish-later]')).toBeVisible(); // phones too
  await page.locator('[data-onb-next]').click();
  await expect(page.locator('#pf-phone-error')).toContainText('phone');
  await expect(page.locator('#pf-birthday-error')).toBeVisible();
  await expect(page.locator('#pf-birthday-hint')).toHaveText('Visible to all officers. Year not collected.');
  await page.getByLabel('Phone · required').fill('+1 206 555 0177');
  await page.getByLabel('Birthday month').selectOption('04');
  await page.getByLabel('Birthday day').selectOption('21');
  await page.locator('[data-onb-next]').click();
  await expect(page.locator('.onb')).toHaveAttribute('data-step', '2');
  await page.locator('[data-onb-next]').click(); // about you: all optional
  await expect(page.locator('.onb')).toHaveAttribute('data-step', '3');
  await page.locator('[data-onb-next]').click(); // your role

  // Calendar: the custom picker, ".ics not configured", Google "not configured".
  const picker = page.locator('[data-email-picker]');
  await picker.getByRole('button').click();
  await picker.getByLabel('New calendar email').fill('robin@example.org');
  await picker.getByRole('button', { name: 'Save' }).click();
  await expect(picker.locator('.email-picker-value')).toHaveText('robin@example.org');
  await expect(picker.locator('[data-email-count]')).toHaveText('1 of 5 saved');
  // Saving an address updates the step: Continue (not "Set up later").
  await expect(page.locator('[data-onb-next]')).toHaveText('Continue →');
  await expect(page.locator('[data-ics-status]')).toContainText('not configured');
  await page.getByLabel('Google Calendar sync').check();
  await expect(page.locator('[data-google-status]')).toContainText('not configured');
  await page.getByLabel('.ics email invites').check();
  await page.locator('[data-onb-next]').click();

  // Widgets, then done.
  await page.getByLabel('Budget vs actual').uncheck();
  await page.locator('[data-onb-next]').click();
  await expect(page).toHaveURL(/\/\?tutorial=onboarding/); // Home, with the guided tour
  await page.getByRole('button', { name: 'Skip tour' }).click();
  await settle(page);

  // The director sees them as an officer with a phone; officers don't see phones.
  await page.context().clearCookies();
  await devLogin(page, 'director');
  await page.goto('/officers');
  await settle(page);
  await page.getByRole('button', { name: 'Robin Park' }).first().click();
  await expect(openDialog(page).locator('[data-phone]')).toHaveText('+1 206 555 0177');
  // Directors deactivate officers, but don't change roles (both live on the Manage tab).
  await openDialog(page).getByRole('tab', { name: 'Manage', exact: true }).click();
  await expect(openDialog(page).getByRole('button', { name: 'Deactivate officer' })).toBeVisible();
  await expect(openDialog(page).getByLabel('Role')).toHaveCount(0);
  await page.keyboard.press('Escape');
  // Only leadership deactivates leadership (or directors).
  await page.getByRole('button', { name: 'Sam Rivera' }).click();
  await expect(openDialog(page)).toContainText('Sam Rivera');
  await expect(openDialog(page).getByRole('tab', { name: 'Manage', exact: true })).toHaveCount(0);
  await expect(openDialog(page).getByRole('button', { name: 'Deactivate officer' })).toHaveCount(0);
  await page.keyboard.press('Escape');
  await page.context().clearCookies();
  await devLogin(page, 'officer');
  await page.goto('/officers');
  await settle(page);
  await page.getByRole('button', { name: 'Robin Park' }).first().click();
  await expect(openDialog(page)).toContainText('Visible to directors and leadership');
  await expect(openDialog(page)).toContainText('Apr 21');
  // The only failed request is the deliberate re-invite of an existing account.
  expect(errors).toEqual(['http: /api/invites → 409']);
});

test('an unknown account from an invite link sees the unavailable screen', async ({ page }) => {
  await devSignIn(page, { discord: inviteeId() });
  await expect(page).toHaveURL(/error=not_allowlisted/);
  await page.goto('/onboarding?unavailable=1');
  await expect(page.locator('#step-title')).toHaveText('Let’s get you a fresh start.');
});

test('Board Members shows Officers, Club Support Contacts and Ex-Officers', async ({ page }) => {
  await devLogin(page, 'director');
  const name = `Support ${Date.now()}`;
  const made = await page.request.post('/api/contacts', { data: { name, org: 'Dept. of Things', role: 'Advisor', type: 'mentor', email: 'adviser@example.org', clubSupport: true } });
  expect(made.ok()).toBe(true);
  const plain = await page.request.post('/api/contacts', { data: { name: `Plain ${Date.now()}`, type: 'speaker' } });
  const ids = [(await made.json()).contact.id as string, (await plain.json()).contact.id as string];
  try {
    // Ex-officers are the deactivated accounts; show one without touching shared seed data.
    await page.route('**/api/officers', async (route) => {
      const res = await route.fetch();
      const body = await res.json();
      const victim = body.officers.find((o: { name: string }) => o.name === 'Priya Shah');
      victim.active = false;
      await route.fulfill({ response: res, json: body });
    });
    await page.goto('/officers');
    await settle(page);
    const officers = page.locator('[data-section="officers"]');
    const support = page.locator('[data-section="club-support"]');
    const ex = page.locator('[data-section="ex-officers"]');
    await expect(officers.getByRole('heading', { name: 'Officers', exact: true })).toBeVisible();
    await expect(officers).toContainText('Sam Rivera');
    await expect(officers).not.toContainText('Priya Shah');
    await expect(support.getByRole('heading', { name: 'Club Support Contacts' })).toBeVisible();
    await expect(support).toContainText(name);
    await expect(support).not.toContainText('Plain ');
    // The contact links sit in a column that phones fold into the drawer, so check presence.
    await expect(support.locator('a[aria-label="adviser@example.org"]')).toHaveAttribute('href', 'mailto:adviser@example.org');
    await expect(ex.getByRole('heading', { name: 'Ex-Officers' })).toBeVisible();
    await expect(ex).toContainText('Priya Shah');
    // One search box narrows every section.
    await page.locator('[data-officer-search]').fill('Dept. of Things');
    await expect(support).toContainText(name);
    await expect(officers).not.toContainText('Sam Rivera');
    expect(await overflow(page)).toEqual([]);
  } finally {
    for (const id of ids) await page.request.delete(`/api/contacts/${id}`);
  }
});

test('a single officer account still shows its Club Support Contacts', async ({ page }) => {
  await devLogin(page, 'director');
  const name = `Solo ${Date.now()}`;
  const made = await page.request.post('/api/contacts', { data: { name, type: 'mentor', clubSupport: true } });
  const id = (await made.json()).contact.id as string;
  try {
    await page.route('**/api/officers', async (route) => {
      const res = await route.fetch();
      const body = await res.json();
      body.officers = body.officers.slice(0, 1);
      await route.fulfill({ response: res, json: body });
    });
    await page.goto('/officers');
    await settle(page);
    await expect(page.locator('[data-section="officers"] tbody tr')).toHaveCount(1);
    await expect(page.locator('[data-section="club-support"]')).toContainText(name);
  } finally {
    await page.request.delete(`/api/contacts/${id}`);
  }
});

for (const theme of THEMES) {
  test(`officers page and invite dialog fit the screen (${theme})`, async ({ page }, info) => {
    await useTheme(page, theme);
    await devLogin(page, 'director');
    await page.goto('/officers');
    await settle(page);
    await expect(page.locator('[data-directory]')).toBeVisible();
    expect(await overflow(page)).toEqual([]);
    await page.screenshot({ path: `test-results/officers-${info.project.name}-${theme}.png`, fullPage: true });
    await page.locator('[data-open-invite]').click();
    await expect(openDialog(page)).toBeVisible();
    expect(await overflow(page)).toEqual([]);
    await page.screenshot({ path: `test-results/officers-invite-${info.project.name}-${theme}.png` });
  });
}
