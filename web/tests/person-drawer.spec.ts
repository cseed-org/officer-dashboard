import { expect, test, type Page } from '@playwright/test';
import { devLogin, openDialog, overflow, settle, watchErrors } from './helpers';

/**
 * V0.10 PersonDrawer, the one side drawer every people page opens: from a row on Board
 * Members, Members and External Contacts; deep-linked with ?person=<id> (old ?id= links land
 * on it too); Esc and Back close it, Forward reopens it, focus returns to the row; edits work
 * from its Manage tab; and on phones it fills the screen.
 */

/** On phones (< 768px) the drawer covers the whole viewport. */
async function expectFullScreenOnPhone(page: Page) {
  const vw = page.viewportSize()!.width;
  const box = await openDialog(page).boundingBox();
  expect(box).not.toBeNull();
  if (vw < 768) {
    expect(Math.round(box!.x)).toBe(0);
    expect(Math.round(box!.width)).toBe(vw);
  } else {
    expect(box!.width).toBeLessThan(vw);
  }
  expect(await overflow(page)).toEqual([]);
}

test('Board Members: open from the list, stats and activity, edit, Esc, Back and Forward', async ({ page }) => {
  const errors = watchErrors(page);
  await devLogin(page, 'director');
  await page.goto('/officers');
  await settle(page);

  const name = page.locator('[data-section="officers"]').getByRole('button', { name: 'Priya Shah', exact: true });
  await name.click();
  await expect(page).toHaveURL(/\/officers\?person=user-priya$/);
  const d = openDialog(page);
  await expect(d.getByRole('heading', { level: 2, name: 'Priya Shah' })).toBeFocused();
  await expectFullScreenOnPhone(page);
  // Hero stats come from existing rows: Priya requested two open design requests.
  const stats = d.locator('[data-person-stats]');
  await expect(stats).toContainText('Open design requests');
  await expect(stats.locator('div', { hasText: 'Open design requests' }).locator('dd')).toHaveText('2');
  await d.getByRole('tab', { name: 'Activity', exact: true }).click();
  await expect(d.locator('[data-person-activity]')).toContainText('Fall Kickoff recap slides');

  // Esc closes, drops ?person and gives focus back to the name in the row.
  await page.keyboard.press('Escape');
  await expect(openDialog(page)).toHaveCount(0);
  await expect(page).toHaveURL(/\/officers$/);
  await expect(name).toBeFocused();

  // Back closes a drawer opened from the list; Forward opens it again.
  await name.click();
  await expect(openDialog(page)).toHaveCount(1);
  await page.goBack();
  await expect(openDialog(page)).toHaveCount(0);
  await expect(page).toHaveURL(/\/officers$/);
  await page.goForward();
  await expect(openDialog(page).getByRole('heading', { level: 2, name: 'Priya Shah' })).toBeVisible();

  // Edit from the Manage tab (same rules as before: directors change subteam, title, term).
  await openDialog(page).getByRole('tab', { name: 'Manage', exact: true }).click();
  const title = openDialog(page).getByLabel('Title', { exact: true });
  await title.fill('VP Events (acting)');
  await openDialog(page).getByRole('button', { name: 'Save changes' }).click();
  await expect(openDialog(page).getByRole('status')).toContainText('Saved');
  await expect(page.locator('[data-section="officers"]')).toContainText('VP Events (acting)');
  await title.fill('VP Events');
  await openDialog(page).getByRole('button', { name: 'Save changes' }).click();
  await expect(openDialog(page).getByRole('status')).toContainText('Saved');
  expect(errors).toEqual([]);
});

test('Board Members: a deep link opens the drawer and closing keeps you on the page', async ({ page }) => {
  await devLogin(page, 'officer');
  await page.goto('/officers?person=user-riley');
  const d = openDialog(page);
  await expect(d.getByRole('heading', { level: 2, name: 'Riley Park' })).toBeVisible();
  // Officers see no Manage tab and no phone numbers.
  await expect(d.getByRole('tab', { name: 'Manage' })).toHaveCount(0);
  await expect(d).toContainText('Visible to directors and leadership');
  await d.getByRole('button', { name: 'Close' }).click();
  await expect(openDialog(page)).toHaveCount(0);
  await expect(page).toHaveURL(/\/officers$/);
});

test('Members: a row opens the drawer and old ?id= links land on ?person=', async ({ page }) => {
  const member = {
    person_id: 'person-7', first_name: 'Rowan', last_name: 'Ito', preferred_name: 'Ro', uw_email: 'rowan@uw.edu', cs_email: null, personal_email: null,
    major: 'Design', grad_year: '2028', campus: 'Seattle', pronouns: 'they/them', demographics: null, join_date: '2026-09-30', agreed_to_membership_agreement: 1, has_been_cseed_officer: 0,
  };
  await page.route('**/api/members?*', (route) =>
    route.fulfill({ json: { officers: [], officerRows: [], items: [member], total: 1, offset: 0, limit: 25, connected: true, lastSync: Date.now(), syncError: '', facets: { campus: ['Seattle'], grad_year: ['2028'] } } }),
  );
  await page.route('**/api/members/person-7', (route) => route.fulfill({ json: { member } }));
  await devLogin(page, 'officer');
  await page.goto('/members');
  await settle(page);
  // A click anywhere on the row (not only the name) opens it.
  await page.locator('[data-person-row="person-7"] td').nth(1).click();
  await expect(page).toHaveURL(/\/members\?person=person-7$/);
  const d = openDialog(page);
  await expect(d.getByRole('heading', { level: 2, name: 'Rowan Ito' })).toBeVisible();
  await expect(d).toContainText('Goes by Ro');
  await expectFullScreenOnPhone(page);
  await page.keyboard.press('Escape');
  await expect(page).toHaveURL(/\/members$/);

  await page.goto('/members?id=person-7');
  await expect(page).toHaveURL(/\/members\?person=person-7$/);
  await expect(openDialog(page).getByRole('heading', { level: 2, name: 'Rowan Ito' })).toBeVisible();
});

test('External Contacts: open, stats, edit from Manage, Back closes', async ({ page }) => {
  await devLogin(page, 'officer');
  const name = `Drawer ${Date.now().toString(36)}`;
  const made = await page.request.post('/api/contacts', { data: { name, org: 'Drawer Labs', type: 'mentor', email: 'drawer@example.org' } });
  const id = (await made.json()).contact.id as string;
  try {
    await page.goto('/contacts');
    await settle(page);
    await page.locator(`[data-person-row="${id}"]`).getByRole('button', { name, exact: true }).click();
    await expect(page).toHaveURL(new RegExp(`/contacts\\?person=${id}$`));
    const d = openDialog(page);
    await expect(d.getByRole('heading', { level: 2, name })).toBeFocused();
    await expect(d.locator('[data-person-stats]')).toContainText('Outreach logged');
    await expect(d.getByRole('link', { name: 'Email' })).toHaveAttribute('href', 'mailto:drawer@example.org');
    await expectFullScreenOnPhone(page);
    await d.getByRole('tab', { name: 'Manage', exact: true }).click();
    await d.getByLabel('Role').fill('Mentor lead');
    await d.getByRole('button', { name: 'Save contact' }).click();
    await expect(page.getByRole('status').filter({ hasText: 'Contact saved.' })).toBeAttached();
    await expect(d).toContainText('Mentor lead · Drawer Labs');
    await page.goBack();
    await expect(openDialog(page)).toHaveCount(0);
    await expect(page).toHaveURL(/\/contacts$/);
  } finally {
    await page.request.delete(`/api/contacts/${id}`);
  }
});
