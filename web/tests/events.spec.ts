import { expect, test } from '@playwright/test';
import { devLogin, openDialog, overflow, settle, THEMES, useTheme, watchErrors } from './helpers';

/**
 * Events V0.3: draft → Luma handoff kit → paste the Luma URL back → request design link → delete,
 * plus the month and week calendars. Creates its own event and deletes it, so it can run
 * against a shared seeded database.
 */
test('events: draft, handoff kit, Luma link, request design, delete', async ({ page, context }, info) => {
  const errors = watchErrors(page);
  await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  await devLogin(page, 'officer');
  await page.goto('/events');
  await settle(page);
  const name = `E2E founder stories ${info.project.name} ${Date.now().toString(36)}`;

  await page.locator('[data-new-event]').click();
  const editor = openDialog(page);
  await expect(editor.getByRole('heading', { name: 'New event' })).toBeVisible();
  await editor.getByRole('button', { name: 'Save draft' }).click();
  await expect(editor.getByText('Give the event a name.')).toBeVisible();
  await editor.getByLabel('Event name').fill(name);
  await editor.getByLabel(/^Starts/).fill('2026-11-20T18:00');
  await editor.getByLabel(/^Ends/).fill('2026-11-20T20:00');
  await editor.getByLabel(/^Location/).fill('The Nest · Room 250');
  await editor.getByLabel(/^Host team/).selectOption({ label: 'Events' });
  await editor.getByLabel(/^Description/).fill('An honest conversation with founders.');
  await editor.getByLabel(/^Capacity/).fill('100');
  await editor.getByRole('button', { name: 'Save draft' }).click();
  await expect(editor.locator('#ev-saved')).toContainText('Draft saved');
  await expect(page).toHaveURL(/[?&]edit=/);
  const id = new URL(page.url()).searchParams.get('edit')!;
  expect(await overflow(page)).toEqual([]);

  // Handoff kit: copy a field, refuse a Luma page that is not an event, save a real one.
  await editor.getByRole('button', { name: 'Open handoff kit' }).click();
  const kit = page.locator('dialog#handoff');
  await expect(kit).toBeVisible();
  await expect(kit.locator('[data-copy-field="time"] p')).toHaveText('November 20, 2026 · 6–8 PM · America/Vancouver');
  await kit.getByRole('button', { name: 'Copy name' }).click();
  await expect(kit.getByRole('button', { name: 'Copied name ✓' })).toBeVisible();
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(name);
  await expect(kit.getByRole('link', { name: /Open Luma create/ })).toHaveAttribute('href', 'https://lu.ma/create');
  await expect(kit.getByLabel('Notion page link')).toBeDisabled();
  const url = kit.getByLabel('Paste Luma URL');
  await url.fill('https://lu.ma/discover');
  await kit.getByRole('button', { name: 'Save Luma link' }).click();
  await expect(kit.getByRole('alert')).toContainText('Luma event page');
  await expect(url).toHaveAttribute('aria-invalid', 'true');
  const slug = `e2e-${info.project.name}-${Date.now().toString(36)}`;
  await url.fill(`https://lu.ma/${slug}?utm_source=x`);
  await kit.getByRole('button', { name: 'Save Luma link' }).click();
  await expect(kit.locator('#link-state')).toContainText(`Linked ✓ · https://lu.ma/${slug}`);
  expect(await overflow(page)).toEqual([]);
  await kit.getByRole('button', { name: 'Close' }).click();
  await expect(page.locator('dialog[open]')).toHaveCount(0);

  // The list shows it published and linked.
  const row = page.locator('tr', { has: page.locator(`[data-event-link="${id}"]`) });
  await expect(row.locator('[data-event-status]')).toHaveText('Published');
  await expect(row.getByText('Luma linked')).toBeVisible();

  // Request design opens the Design board's new-request form for this event.
  await page.goto(`/events?id=${id}`);
  await settle(page);
  await expect(openDialog(page).locator('[data-request-design]')).toHaveAttribute('href', `/design?new=1&eventId=${id}`);

  // Delete with confirmation; the note repeats the separate Luma step.
  await page.goto(`/events?delete=${id}`);
  await settle(page);
  await page.locator('[data-confirm-delete]').click();
  await expect(page.locator('#delete-note')).toContainText('Remove any published Luma event separately');
  await expect(page.locator('#period')).toBeFocused();
  await expect(page.locator(`[data-event-link="${id}"]`)).toHaveCount(0);
  expect(errors).toEqual([]);
});

test('events: month and week calendars place events on the Pacific wall clock', async ({ page }, info) => {
  const errors = watchErrors(page);
  await devLogin(page, 'officer');
  await page.goto('/events?view=month&m=2026-11');
  await settle(page);
  await expect(page.locator('#period')).toHaveText('November 2026');
  const phone = info.project.name === 'phone';
  const hack = page.locator(phone ? '.cal-agenda [data-cal-event="event-hack-night"]' : '.cal-day[data-date="2026-11-13"] [data-cal-event="event-hack-night"]');
  await expect(hack).toBeVisible();
  expect(await overflow(page)).toEqual([]);
  await page.getByRole('button', { name: /Next month/ }).click();
  await expect(page.locator('#period')).toHaveText('December 2026');

  await page.goto('/events?view=week&w=2026-11-12');
  await settle(page);
  await expect(page.locator('#period')).toHaveText('November 9–15, 2026');
  const block = page.locator(phone ? '.cal-agenda [data-cal-event="event-hack-night"]' : '.cal-col[data-date="2026-11-13"] [data-cal-event="event-hack-night"]');
  await expect(block).toBeVisible();
  if (!phone) await expect(block.locator('.cal-block-time')).toHaveText('6–11 PM');
  expect(await overflow(page)).toEqual([]);
  await block.click();
  await expect(page).toHaveURL(/[?&]id=event-hack-night/);
  await expect(openDialog(page).locator('[data-event-detail]')).toBeVisible();
  expect(errors).toEqual([]);
});

test.describe('events review screenshots', () => {
  test.skip(!process.env.SHOTS, 'set SHOTS=1 to capture review screenshots');
  for (const theme of THEMES) {
    test(`events screenshots, ${theme}`, async ({ page }, info) => {
      await useTheme(page, theme);
      await devLogin(page, 'officer');
      for (const [shot, path] of [
        ['list', '/events'],
        ['month', '/events?view=month&m=2026-11'],
        ['week', '/events?view=week&w=2026-11-09'],
        ['editor', '/events?new=1'],
        ['kit', '/events?kit=event-hack-night'],
        ['delete', '/events?delete=event-hack-night'],
      ] as const) {
        await page.goto(path);
        await settle(page);
        await page.waitForTimeout(700);
        const dialog = await page.locator('dialog[open]').count();
        await page.screenshot({ path: `tmp/shots/events-${info.project.name}-${theme}-${shot}.png`, fullPage: !dialog });
      }
    });
  }
});
