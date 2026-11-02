import { expect, test, type Page } from '@playwright/test';
import { devLogin, openDialog, overflow, settle, THEMES, useTheme, watchErrors } from './helpers';

/**
 * Speakers & Mentors against the real API: an officer adds a contact, edits it, logs outreach
 * through the nested dialog (and comes back to the contact with focus restored), sees
 * "Last reached out by" follow the latest *dated* entry, filters the directory, keeps drafts
 * when a save fails, and deletes the contact. Each run creates contacts with a unique marker.
 * SHOTS=1 also writes review screenshots to tmp/shots/.
 */
const shot = async (page: Page, name: string, project: string, theme: string) => {
  if (!process.env.SHOTS) return;
  await page.waitForTimeout(400);
  await page.screenshot({ path: `tmp/shots/speakers-${project}-${theme}-${name}.png`, fullPage: !(await openDialog(page).count()) });
};

/** The directory row (desktop table) or card (phone) for a contact. */
const entry = (page: Page, name: string) =>
  page.locator('main').locator('.speakers-desktop tr, .speakers-mobile article').filter({ visible: true, hasText: name });

async function addContact(page: Page, c: { name: string; org?: string; type?: string; topics?: string; email?: string }) {
  await page.getByRole('button', { name: 'Add contact', exact: true }).click();
  const d = openDialog(page);
  await expect(d.getByRole('heading', { level: 2, name: 'Add contact' })).toBeFocused();
  await d.getByLabel('Name').fill(c.name);
  if (c.org) await d.getByLabel('Organization').fill(c.org);
  if (c.type) await d.getByLabel('Type', { exact: true }).selectOption(c.type);
  if (c.topics) await d.getByLabel('Topics').fill(c.topics);
  if (c.email) await d.getByLabel('Email').fill(c.email);
  await d.getByRole('button', { name: 'Add contact' }).click();
  await expect(page).toHaveURL(/\/speakers\?id=/);
  await expect(openDialog(page).getByRole('heading', { level: 2, name: c.name })).toBeVisible();
}

test('contacts: field-named validation error, error styling, add draft kept on close, detail heading focused after create', async ({ page }) => {
  const errors = watchErrors(page);
  const name = `Draft ${Date.now().toString(36)}`;
  await devLogin(page, 'officer');
  await page.goto('/speakers');
  await settle(page);

  // A whitespace-only name passes `required` but the API rejects it: the alert names the field.
  await page.getByRole('button', { name: 'Add contact', exact: true }).click();
  let d = openDialog(page);
  await d.getByLabel('Name').fill('   ');
  await d.getByRole('button', { name: 'Add contact' }).click();
  const alert = d.getByRole('alert');
  await expect(alert).toContainText(/Name:/);
  await expect(alert).not.toHaveText('Validation failed');
  // Styled as an error, not body text.
  await expect(alert).toHaveClass(/field-error/);
  const [alertColor, bodyColor] = await Promise.all([
    alert.evaluate((el) => getComputedStyle(el).color),
    d.getByLabel('Name').evaluate((el) => getComputedStyle(el).color),
  ]);
  expect(alertColor).not.toBe(bodyColor);

  // Closing the Add drawer by accident keeps the draft for the next "Add contact".
  await d.getByLabel('Name').fill(name);
  await d.getByLabel('Organization').fill('Draft Org');
  await page.keyboard.press('Escape');
  await expect(openDialog(page)).toHaveCount(0);
  await page.getByRole('button', { name: 'Add contact', exact: true }).click();
  d = openDialog(page);
  await expect(d.getByLabel('Name')).toHaveValue(name);
  await expect(d.getByLabel('Organization')).toHaveValue('Draft Org');

  // After a successful create the detail heading takes focus, and the next Add starts blank.
  await d.getByRole('button', { name: 'Add contact' }).click();
  await expect(page).toHaveURL(/\/speakers\?id=/);
  d = openDialog(page);
  await expect(d.getByRole('heading', { level: 2, name })).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(openDialog(page)).toHaveCount(0);
  await page.getByRole('button', { name: 'Add contact', exact: true }).click();
  await expect(openDialog(page).getByLabel('Name')).toHaveValue('');
  await page.keyboard.press('Escape');

  // Clean up.
  const row = page.locator('main').locator('.speakers-desktop tr, .speakers-mobile article').filter({ visible: true, hasText: name });
  await row.getByRole('button', { name: `View contact ${name}` }).click();
  d = openDialog(page);
  await d.getByRole('button', { name: 'Delete contact' }).click();
  await d.getByRole('button', { name: 'Confirm delete contact and outreach history' }).click();
  await expect(row).toHaveCount(0);
  expect(errors.filter((e) => !/\/api\/contacts → 400$/.test(e))).toEqual([]);
});

for (const theme of THEMES) {
  test(`contacts: add, edit, nested outreach, latest outreach, filters, errors, delete (${theme})`, async ({ page }, info) => {
    const errors = watchErrors(page);
    const m = `qa${Date.now().toString(36)}${theme[0]}`;
    const name = `Priya ${m}`;
    await useTheme(page, theme);
    await devLogin(page, 'officer');
    await page.goto('/speakers');
    await settle(page);
    expect(await overflow(page)).toEqual([]);

    // Add: name is required (native validation keeps the drawer open).
    await page.getByRole('button', { name: 'Add contact', exact: true }).click();
    let d = openDialog(page);
    await d.getByRole('button', { name: 'Add contact' }).click();
    await expect(d.getByLabel('Name')).toBeFocused();
    await page.keyboard.press('Escape');
    await expect(openDialog(page)).toHaveCount(0);

    await addContact(page, { name, org: 'North Shore', type: 'both', topics: `Fundraising, ${m}-topic`, email: `priya.${m}@example.com` });
    await expect(page.getByRole('status').filter({ hasText: 'Contact added.' })).toBeAttached();
    d = openDialog(page);
    await expect(d.getByText('No outreach yet.')).toBeVisible();
    await shot(page, 'detail', info.project.name, theme);

    // Edit with a failing save: the error shows and the draft is kept, then the retry saves it.
    await d.getByLabel('Role').fill('Partner');
    await d.getByLabel('General notes').fill('Prefers a small Q&A.');
    await page.route('**/api/contacts/*', (r) => (r.request().method() === 'PATCH' ? r.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"boom"}' }) : r.fallback()));
    await d.getByRole('button', { name: 'Save contact' }).click();
    await expect(d.getByRole('alert')).toBeVisible();
    await expect(d.getByLabel('Role')).toHaveValue('Partner');
    await expect(d.getByLabel('General notes')).toHaveValue('Prefers a small Q&A.');
    await shot(page, 'save-error', info.project.name, theme);
    await page.unrouteAll({ behavior: 'ignoreErrors' });
    await d.getByRole('button', { name: 'Save contact' }).click();
    await expect(page.getByRole('status').filter({ hasText: 'Contact saved.' })).toBeAttached();
    await expect(d.getByRole('alert')).toHaveCount(0);

    // An unsaved edit survives the nested outreach round trip.
    await d.getByLabel('Organization').fill('North Shore Ventures');

    // Nested outreach: the contact drawer gives way to the outreach dialog; Escape returns to
    // the contact with focus on "Log outreach".
    await d.getByRole('button', { name: 'Log outreach' }).click();
    d = openDialog(page);
    await expect(page.locator('dialog[open]')).toHaveCount(1);
    await expect(d.getByRole('heading', { level: 2, name: 'Log outreach' })).toBeFocused();
    await expect(d.getByText(name)).toBeVisible();
    expect(await overflow(page)).toEqual([]);
    await shot(page, 'outreach', info.project.name, theme);
    await page.keyboard.press('Escape');
    d = openDialog(page);
    await expect(d.getByRole('heading', { level: 2, name })).toBeVisible();
    await expect(d.getByRole('button', { name: 'Log outreach' })).toBeFocused();
    await expect(d.getByLabel('Organization')).toHaveValue('North Shore Ventures');

    // The Close button does the same. Wait for the outreach heading first so dialog[open] is
    // not resolved against the parent drawer while it closes.
    await d.getByRole('button', { name: 'Log outreach' }).click();
    await expect(page.locator('#outreach-title')).toBeFocused();
    await openDialog(page).getByRole('button', { name: 'Close' }).click();
    d = openDialog(page);
    await expect(d.getByRole('heading', { level: 2, name })).toBeVisible();
    await expect(d.getByRole('button', { name: 'Log outreach' })).toBeFocused();

    // Outreach save fails: the error shows inside the outreach dialog and the note is kept.
    await d.getByRole('button', { name: 'Log outreach' }).click();
    d = openDialog(page);
    await d.getByLabel('When').fill('2026-10-30T10:00');
    await d.getByLabel('Channel', { exact: true }).selectOption('linkedin');
    await d.getByLabel('Outcome', { exact: true }).selectOption('interested');
    await d.getByLabel('Notes').fill('Follow up Nov 5');
    await page.route('**/api/contacts/*/outreach', (r) => r.fulfill({ status: 503, contentType: 'application/json', body: '{"error":"unavailable"}' }));
    await d.getByRole('button', { name: 'Save outreach' }).click();
    await expect(d.getByRole('alert')).toBeVisible();
    await expect(d.getByLabel('Notes')).toHaveValue('Follow up Nov 5');
    await expect(d.getByLabel('Channel', { exact: true })).toHaveValue('linkedin');
    await shot(page, 'outreach-error', info.project.name, theme);
    await page.unrouteAll({ behavior: 'ignoreErrors' });
    await d.getByRole('button', { name: 'Save outreach' }).click();
    d = openDialog(page);
    await expect(d.getByRole('heading', { level: 2, name })).toBeVisible();
    await expect(d.getByRole('button', { name: 'Log outreach' })).toBeFocused();
    await expect(d.locator('.timeline li')).toHaveCount(1);
    await expect(d.locator('.timeline')).toContainText('LinkedIn · Interested');
    await expect(d.getByLabel('Organization')).toHaveValue('North Shore Ventures');

    // An older entry logged later does not replace "Last reached out by".
    await d.getByRole('button', { name: 'Log outreach' }).click();
    d = openDialog(page);
    await d.getByLabel('When').fill('2026-10-01T09:00');
    await d.getByLabel('Channel', { exact: true }).selectOption('email');
    await d.getByLabel('Outcome', { exact: true }).selectOption('awaiting_reply');
    await d.getByRole('button', { name: 'Save outreach' }).click();
    d = openDialog(page);
    await expect(d.locator('.timeline li')).toHaveCount(2);
    await d.getByRole('button', { name: 'Save contact' }).click();
    await expect(page.getByRole('status').filter({ hasText: 'Contact saved.' })).toBeAttached();
    await shot(page, 'detail-with-outreach', info.project.name, theme);
    await page.keyboard.press('Escape');
    await expect(openDialog(page)).toHaveCount(0);
    await expect(page).toHaveURL(/\/speakers$/);
    const row = entry(page, name);
    await expect(row).toContainText('Ava Romero · Oct 30, 2026');
    await expect(row).toContainText('LinkedIn · Interested');
    await expect(row).toContainText('North Shore Ventures');

    // Filters: a mentor with another topic, then type/topic/search narrowing.
    const mentor = `Sofia ${m}`;
    await addContact(page, { name: mentor, type: 'mentor', topics: `Climate ${m}` });
    await page.keyboard.press('Escape');
    await page.getByLabel('Find a contact').fill(m);
    await expect(entry(page, name)).toHaveCount(1);
    await expect(entry(page, mentor)).toHaveCount(1);
    await page.getByLabel('Type', { exact: true }).selectOption('speaker');
    await expect(entry(page, name)).toHaveCount(1); // "both" counts as a speaker
    await expect(entry(page, mentor)).toHaveCount(0);
    await page.getByLabel('Type', { exact: true }).selectOption('');
    await page.getByLabel('Topic', { exact: true }).selectOption(`Climate ${m}`);
    await expect(entry(page, name)).toHaveCount(0);
    await expect(entry(page, mentor)).toHaveCount(1);
    expect(await overflow(page)).toEqual([]);
    await shot(page, 'filtered', info.project.name, theme);
    await page.getByLabel('Find a contact').fill(`${m}-nobody`);
    await expect(page.getByText('No matching contacts')).toBeVisible();
    await page.getByRole('button', { name: 'Clear filters' }).click();
    await expect(page.getByLabel('Find a contact')).toHaveValue('');
    await expect(entry(page, name)).toHaveCount(1);
    await page.getByLabel('Find a contact').fill(m);
    await shot(page, 'list', info.project.name, theme);

    // Delete both (two-step confirm).
    for (const n of [name, mentor]) {
      await entry(page, n).getByRole('button', { name: `View contact ${n}` }).click();
      d = openDialog(page);
      await expect(d.getByRole('heading', { level: 2, name: n })).toBeVisible();
      await d.getByRole('button', { name: 'Delete contact' }).click();
      await d.getByRole('button', { name: 'Confirm delete contact and outreach history' }).click();
      await expect(openDialog(page)).toHaveCount(0);
      await expect(entry(page, n)).toHaveCount(0);
    }

    expect(errors.filter((e) => !/→ 50[03]$/.test(e))).toEqual([]);
  });
}
