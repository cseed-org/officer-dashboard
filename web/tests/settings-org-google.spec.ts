import { mkdirSync } from 'node:fs';
import { expect, test, type Page } from '@playwright/test';
import { devLogin, overflow, settle, useTheme, watchErrors } from './helpers';

/**
 * Settings › org Google Calendar (V0.15, plan decision 1): leadership connects the dedicated org
 * Gmail once. Each state is driven by mocking GET /api/settings/org-google/status, because a dev
 * server has no real org account. Officers never see the card, and /calendar points each role at
 * the right next step. EVIDENCE=1 saves the card in each state to docs/evidence/org-calendar/.
 */
type Status = { configured: boolean; connected: boolean; email: string | null; needsReconnect: boolean; lastSyncError: string | null };
const STATES: Record<string, Status> = {
  'not-configured': { configured: false, connected: false, email: null, needsReconnect: false, lastSyncError: null },
  'not-connected': { configured: true, connected: false, email: null, needsReconnect: false, lastSyncError: null },
  connected: { configured: true, connected: true, email: 'calendar@cseed.example.org', needsReconnect: false, lastSyncError: null },
  'needs-reconnect': { configured: true, connected: true, email: 'calendar@cseed.example.org', needsReconnect: true, lastSyncError: 'oauth2: "invalid_grant" "Token has been expired or revoked."' },
  'sync-error': { configured: true, connected: true, email: 'calendar@cseed.example.org', needsReconnect: false, lastSyncError: 'googleapi: Error 403: Rate Limit Exceeded' },
};

async function mockStatus(page: Page, status: Status) {
  await page.route('**/api/settings/org-google/status', (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(status) }));
}
const card = (page: Page) => page.locator('[data-org-google-panel]');

async function evidence(page: Page, name: string, project: string) {
  if (!process.env.EVIDENCE || project === 'tablet') return;
  const dir = '../docs/evidence/org-calendar';
  mkdirSync(dir, { recursive: true });
  // A viewport shot centred on the card, so the phone's sticky top bar never covers its heading.
  await page.locator('h2#org-google').evaluate((h) => h.closest('section')!.scrollIntoView({ block: 'center' }));
  await page.screenshot({ path: `${dir}/settings-org-google-${name}-${project === 'phone' ? 390 : 1440}-light.png` });
}

test('org Google card: every state, as leadership', async ({ page }, info) => {
  const errors = watchErrors(page);
  await useTheme(page, 'light');
  await devLogin(page, 'leadership');

  // Not configured: names what an owner must set, and offers no Connect.
  await mockStatus(page, STATES['not-configured']!);
  await page.goto('/settings#org-google');
  await settle(page);
  await expect(page.getByRole('heading', { name: 'org Google Calendar' })).toBeVisible();
  await expect(card(page).locator('[data-org-google="not-configured"]')).toContainText('GOOGLE_OAUTH_CLIENT_ID');
  await expect(card(page)).toContainText('GOOGLE_OAUTH_CLIENT_SECRET');
  await expect(card(page)).toContainText('/auth/org-google/callback');
  await expect(card(page).locator('[data-org-google-connect]')).toHaveCount(0);
  expect(await overflow(page)).toEqual([]);
  await evidence(page, 'not-configured', info.project.name);

  // Not connected: Connect is a navigation to the server's consent redirect, not a fetch.
  await page.unroute('**/api/settings/org-google/status');
  await mockStatus(page, STATES['not-connected']!);
  await page.reload();
  await settle(page);
  const connect = card(page).locator('[data-org-google-connect]');
  await expect(connect).toHaveText('Connect org Google account');
  await expect(connect).toHaveAttribute('href', '/api/settings/org-google/connect');
  await expect(card(page).locator('[data-org-google-disconnect]')).toHaveCount(0);
  await evidence(page, 'not-connected', info.project.name);

  // Connected: the account, and Disconnect behind a confirmation.
  await page.unroute('**/api/settings/org-google/status');
  await mockStatus(page, STATES.connected!);
  await page.reload();
  await settle(page);
  await expect(card(page).locator('[data-org-google="connected"]')).toBeVisible();
  await expect(card(page).locator('[data-org-google-email]')).toHaveText('calendar@cseed.example.org');
  await expect(card(page).locator('[data-org-google-connect]')).toHaveCount(0);
  await evidence(page, 'connected', info.project.name);
  let deletes = 0;
  await page.route('**/api/settings/org-google', (route) => {
    if (route.request().method() !== 'DELETE') return route.fallback();
    deletes++;
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(STATES['not-connected']) });
  });
  await card(page).locator('[data-org-google-disconnect]').click();
  const confirm = page.locator('dialog#org-google-disconnect');
  await expect(confirm).toBeVisible();
  await expect(confirm).toContainText('calendar@cseed.example.org');
  await confirm.getByRole('button', { name: 'Keep connected' }).click();
  await expect(confirm).toBeHidden();
  expect(deletes).toBe(0);
  await card(page).locator('[data-org-google-disconnect]').click();
  await confirm.locator('[data-confirm-org-disconnect]').click();
  await expect(confirm).toBeHidden();
  expect(deletes).toBe(1);
  await expect(card(page).locator('[data-org-google="not-connected"]')).toBeVisible();
  await expect(card(page).locator('[data-org-google-note]')).toContainText('disconnected');

  // Revoked access or a failing sync: a warning and Reconnect.
  for (const name of ['needs-reconnect', 'sync-error'] as const) {
    await page.unroute('**/api/settings/org-google/status');
    await mockStatus(page, STATES[name]!);
    await page.reload();
    await settle(page);
    const warning = card(page).locator('[data-org-google-warning]');
    await expect(warning).toContainText(name === 'needs-reconnect' ? 'revoked' : 'The last sync with Google failed.');
    await expect(warning).toContainText(STATES[name]!.lastSyncError!);
    await expect(card(page).locator('[data-org-google-connect]')).toHaveText('Reconnect Google');
    await expect(card(page).locator('[data-org-google-connect]')).toHaveAttribute('href', '/api/settings/org-google/connect');
    await expect(card(page).locator('[data-org-google-disconnect]')).toBeVisible();
    expect(await overflow(page)).toEqual([]);
    await evidence(page, name, info.project.name);
  }

  // The result Google consent returns with.
  await page.goto('/settings?orgGoogle=scope_required#org-google');
  await settle(page);
  await expect(card(page).locator('[data-org-google-note]')).toContainText('did not grant calendar access');
  expect(errors).toEqual([]);
});

test('org Google card: the real status loads without errors', async ({ page }) => {
  const errors = watchErrors(page);
  await devLogin(page, 'leadership');
  await page.goto('/settings');
  await settle(page);
  // A dev server usually has no Google OAuth client (not configured); any real state is fine.
  await expect(card(page).locator('[data-org-google]')).toBeVisible();
  await expect(card(page).locator('[data-org-google="error"]')).toHaveCount(0);
  expect(errors).toEqual([]);
});

test('org Google card: officers do not see it; /calendar tells each role what to do', async ({ page }) => {
  const errors = watchErrors(page);
  // The calendar notice shows while no calendar has a Google ID (a fresh dev database).
  const cals = async () => (await (await page.request.get('/api/orgcal/calendars')).json()).calendars as { googleCalendarId: string | null }[];
  await devLogin(page, 'officer');
  test.skip((await cals()).some((c) => c.googleCalendarId), 'this database already has Google calendars');
  await page.goto('/settings');
  await settle(page);
  await expect(page.locator('h2#org-google')).toHaveCount(0);
  await expect(card(page)).toHaveCount(0);
  await page.goto('/calendar');
  await settle(page);
  await expect(page.locator('[data-google="not-connected"]')).toContainText('Ask leadership to connect Google Calendar.');
  await expect(page.locator('[data-google-settings]')).toHaveCount(0);

  await devLogin(page, 'leadership');
  await mockStatus(page, STATES['not-connected']!);
  await page.goto('/calendar');
  await settle(page);
  const link = page.locator('[data-google-settings]');
  await expect(link).toHaveAttribute('href', '/settings#org-google');
  await link.click();
  await expect(page).toHaveURL(/\/settings#org-google$/);
  await settle(page);
  await expect(card(page).locator('[data-org-google="not-connected"]')).toBeVisible();

  // Connected but failing: leadership is sent to the card from the calendar too.
  await page.unroute('**/api/settings/org-google/status');
  await mockStatus(page, STATES['needs-reconnect']!);
  await page.goto('/calendar');
  await settle(page);
  await expect(page.locator('[data-google="needs-attention"]')).toContainText('revoked');
  expect(errors).toEqual([]);
});
