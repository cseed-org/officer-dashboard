import { expect, test, type Page } from '@playwright/test';
import { devLogin, devSignIn, overflow, settle, THEMES, useTheme, watchErrors } from './helpers';
import { parseInvites, sendable } from '../src/ui/views/buildspace/launchpad';

// V0.12 Launchpad cohorts, invites, mentor roster and the builder/mentor portals
// (docs/launchpad-cohorts.md). Local only, against a seeded dev server with DEV_AUTH=1:
// `as=builder` / `as=mentor` sign in to the seeded demo portal accounts.
const SHOTS = 'tmp/verify/launchpad-v12';

async function open(page: Page, path = '/buildspace-launchpad') {
  await page.goto(path);
  await settle(page);
}

test.beforeEach(async ({ page }) => {
  await page.request.put('/api/me/prefs', { data: { dashboardTutorialSeen: true } }).catch(() => undefined);
});

test('invite parser: emails, name,email CSV, duplicates, invalid rows and existing accounts', () => {
  const rows = parseInvites(
    ['name,email', 'ada@uw.example', 'Grace Hopper, GRACE@uw.example', 'grace@uw.example', 'not-an-email', 'a@uw.example; b@uw.example', 'joined@uw.example', 'pending@uw.example'].join('\n'),
    [
      { email: 'joined@uw.example', status: 'active' },
      { email: 'pending@uw.example', status: 'invited' },
    ] as never,
  );
  expect(rows.map((r) => [r.email, r.state])).toEqual([
    ['ada@uw.example', 'valid'],
    ['grace@uw.example', 'valid'],
    ['grace@uw.example', 'duplicate'],
    ['not-an-email', 'invalid'],
    ['a@uw.example', 'valid'],
    ['b@uw.example', 'valid'],
    ['joined@uw.example', 'active'],
    ['pending@uw.example', 'pending'],
  ]);
  expect(rows[1]!.name).toBe('Grace Hopper');
  expect(sendable(rows).map((r) => r.email)).toEqual(['ada@uw.example', 'grace@uw.example', 'a@uw.example', 'b@uw.example', 'pending@uw.example']);
});

test('cohort switcher drives the register and keeps the URL', async ({ page }, info) => {
  const errors = watchErrors(page);
  await devLogin(page, 'director');
  await open(page);
  const select = page.locator('[data-cohort-select]');
  await expect(select).toBeVisible();
  const options = await select.locator('option').evaluateAll((os) => os.map((o) => (o as HTMLOptionElement).value));
  expect(options.length).toBeGreaterThan(0);
  await expect(page.locator('[data-new-cohort]')).toBeVisible();
  if (options.length > 1) {
    await select.selectOption(options[1]!);
    await expect(page).toHaveURL(new RegExp(`cohort=${encodeURIComponent(options[1]!)}`));
    await settle(page);
    await expect(select).toHaveValue(options[1]!);
  }
  // New cohort dialog opens and validates without saving.
  await page.locator('[data-new-cohort]').click();
  await expect(page.locator('#lp-cohort-dialog [data-cohort-form]')).toBeVisible();
  await page.keyboard.press('Escape');
  expect(await overflow(page)).toEqual([]);
  await page.screenshot({ path: `${SHOTS}/cohort-${info.project.name}.png` });
  expect(errors).toEqual([]);
});

test('officers cannot manage cohorts', async ({ page }) => {
  await devLogin(page, 'officer');
  await open(page);
  await expect(page.locator('[data-cohort-select]')).toBeVisible();
  await expect(page.locator('[data-new-cohort]')).toHaveCount(0);
});

test('invite dialog previews pasted rows before sending', async ({ page }, info) => {
  const errors = watchErrors(page);
  await devLogin(page, 'director');
  await open(page, '/buildspace-launchpad?tab=invites');
  await expect(page.locator('[data-invites-panel]')).toBeVisible();
  await page.locator('[data-invite-builders]').first().click();
  const input = page.locator('[data-invite-input]');
  const run = `${info.project.name}-${Date.now()}`;
  await input.fill(`ada.${run}@uw.example\nGrace Hopper, grace.${run}@uw.example\nada.${run}@uw.example\nnope`);
  const preview = page.locator('[data-invite-preview]');
  await expect(preview.locator('li[data-state="valid"]')).toHaveCount(2);
  await expect(preview.locator('li[data-state="duplicate"]')).toHaveCount(1);
  await expect(preview.locator('li[data-state="invalid"]')).toHaveCount(1);
  await expect(page.locator('[data-invite-send]')).toHaveText(/Invite 2 builders/);
  for (const theme of THEMES) {
    await page.evaluate((t) => { document.documentElement.dataset.theme = t; }, theme);
    await page.screenshot({ path: `${SHOTS}/invite-dialog-${info.project.name}-${theme}.png` });
  }
  // Send as links so nothing depends on SMTP; the result lists one copyable link per invite.
  await page.getByLabel('Just give me the links to share').check();
  await page.locator('[data-invite-send]').click();
  await expect(page.locator('[data-invite-result]')).toBeVisible();
  await expect(page.locator('[data-invite-links] li')).toHaveCount(2);
  await page.keyboard.press('Escape');
  await expect(page.locator('[data-invites-table] tr[data-status="invited"]').first()).toBeVisible();
  await expect(page.locator('[data-copy-link]').first()).toBeVisible();
  expect(errors).toEqual([]);
});

test('auto-assign shows a preview with shuffle and apply', async ({ page }, info) => {
  const errors = watchErrors(page);
  await devLogin(page, 'director');
  await open(page, '/buildspace-launchpad?tab=mentors');
  await expect(page.locator('[data-mentor-roster]')).toBeVisible();
  await page.locator('[data-auto-assign-open]').click();
  const dialog = page.locator('#lp-auto-assign');
  await expect(dialog.locator('[data-auto-assign]')).toBeVisible();
  await expect(dialog.locator('[aria-busy="true"]')).toHaveCount(0);
  const rows = dialog.locator('[data-auto-assign-table] tr[data-assignment]');
  if (await rows.count()) {
    await expect(dialog.locator('[data-auto-assign-apply]')).toBeEnabled();
    await dialog.locator('[data-auto-assign-shuffle]').click();
    await expect(rows.first()).toBeVisible();
  } else {
    await expect(dialog.locator('[data-auto-assign-apply]')).toBeDisabled();
  }
  await page.screenshot({ path: `${SHOTS}/auto-assign-${info.project.name}.png` });
  await page.keyboard.press('Escape');
  expect(errors).toEqual([]);
});

test('show archived toggles the query and archived rows are muted', async ({ page }) => {
  await devLogin(page, 'director');
  await open(page, '/buildspace-launchpad?tab=projects');
  const toggle = page.locator('[data-show-archived]');
  await toggle.check();
  await expect(page).toHaveURL(/archived=1/);
  await settle(page);
  for (const row of await page.locator('tr[data-archived]').all()) await expect(row).toHaveClass(/lp-muted-row/);
  await toggle.uncheck();
  await expect(page).not.toHaveURL(/archived=1/);
  await settle(page);
  await expect(page.locator('tr[data-archived]')).toHaveCount(0);
});

test('join page explains missing, invalid and errored links', async ({ page }, info) => {
  await page.goto('/launchpad/join');
  await expect(page.locator('[data-join-state="missing"]')).toBeVisible();
  await page.goto('/launchpad/join?token=not-a-real-token');
  await expect(page.locator('[data-join-error]')).toBeVisible();
  await expect(page.locator('[data-join-continue]')).toHaveCount(0);
  await page.goto('/launchpad/join?token=abc&error=discord_taken');
  await expect(page.locator('[data-join-error="discord_taken"]')).toContainText('already linked');
  await expect(page.locator('[data-join-continue]')).toHaveAttribute('href', '/auth/login?launchpad=abc');
  await page.screenshot({ path: `${SHOTS}/join-error-${info.project.name}.png` });
  await page.goto('/launchpad/login?error=not_found');
  await expect(page.locator('[data-login-error="not_found"]')).toBeVisible();
  await expect(page.locator('[data-portal-signin]')).toHaveAttribute('href', '/auth/login?portal=1');
  expect(await overflow(page)).toEqual([]);
});

test('signed-out portal visits go to the portal sign-in, not the officer one', async ({ page }) => {
  await page.context().clearCookies();
  await page.goto('/launchpad');
  await expect(page).toHaveURL(/\/launchpad\/login/);
});

test('builder onboarding then dashboard', async ({ page }, info) => {
  const errors = watchErrors(page);
  for (const theme of THEMES) {
    await useTheme(page, theme);
    await devSignIn(page, { as: 'builder' }, '/launchpad');
    await settle(page);
    const wizard = page.locator('[data-onboarding]');
    if (await wizard.isVisible()) {
      await page.screenshot({ path: `${SHOTS}/onboarding-${info.project.name}-${theme}.png` });
      await page.getByLabel('Your role on the team · optional').fill('Hardware lead');
      await page.locator('[data-onboarding-next]').click();
      await expect(wizard).toHaveAttribute('data-step', 'Project');
      const joinable = page.locator('[data-joinable] input');
      if (await joinable.count()) await joinable.first().check();
      else await page.getByLabel('Project name').fill('E2E project');
      await page.locator('[data-onboarding-next]').click();
      await expect(wizard).toHaveAttribute('data-step', 'Confirm');
      await page.locator('[data-onboarding-next]').click();
    }
    await expect(page.locator('[data-builder-dashboard]')).toBeVisible();
    await expect(page.locator('[data-card="week"] [data-week]')).toBeVisible();
    await expect(page.locator('[data-card="project"]')).toBeVisible();
    await expect(page.locator('[data-officer-link]')).toHaveCount(0);
    expect(await overflow(page)).toEqual([]);
    await page.screenshot({ path: `${SHOTS}/builder-${info.project.name}-${theme}.png`, fullPage: true });
  }
  expect(errors).toEqual([]);
});

test('mentor dashboard lists assigned projects and opens one', async ({ page }, info) => {
  const errors = watchErrors(page);
  for (const theme of THEMES) {
    await useTheme(page, theme);
    await devSignIn(page, { as: 'mentor' }, '/launchpad');
    await settle(page);
    await expect(page.locator('[data-mentor-dashboard]')).toBeVisible();
    const cards = page.locator('[data-mentor-project]');
    if (await cards.count()) {
      await cards.first().getByRole('button', { name: 'Open project' }).click();
      await expect(page).toHaveURL(/project=/);
      await expect(page.locator('#mentor-project [data-mentor-detail]')).toBeVisible();
      await expect(page.locator('[data-mentor-note]')).toBeVisible();
      await page.screenshot({ path: `${SHOTS}/mentor-detail-${info.project.name}-${theme}.png` });
      await page.keyboard.press('Escape');
    }
    expect(await overflow(page)).toEqual([]);
    await page.screenshot({ path: `${SHOTS}/mentor-${info.project.name}-${theme}.png`, fullPage: true });
  }
  expect(errors).toEqual([]);
});
