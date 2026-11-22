import { expect, test } from '@playwright/test';
import { devLogin, openDialog, overflow, settle, THEMES, useTheme, watchErrors } from './helpers';

/** YYYY-MM-DD n days from today (the browser runs in America/Los_Angeles). */
const day = (n: number) => {
  const d = new Date();
  d.setDate(d.getDate() + n);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
};
/** Next calendar month as YYYY-MM, its short name, and the Mondays of the weeks holding its 5th and 20th. */
const next = (() => {
  const t = new Date();
  const first = new Date(Date.UTC(t.getFullYear(), t.getMonth() + 1, 1, 12));
  const ym = first.toISOString().slice(0, 7);
  const mondayOf = (dd: number) => {
    const d = new Date(Date.UTC(first.getUTCFullYear(), first.getUTCMonth(), dd, 12));
    d.setUTCDate(d.getUTCDate() - ((d.getUTCDay() + 6) % 7));
    return d.toISOString().slice(0, 10);
  };
  const mon = new Intl.DateTimeFormat('en-US', { month: 'short', timeZone: 'UTC' }).format(first);
  return { ym, mon, week5: mondayOf(5), week20: mondayOf(20) };
})();

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
  await editor.getByLabel(/^Starts/).fill(`${day(7)}T18:00`);
  await editor.getByLabel(/^Ends/).fill(`${day(7)}T20:00`);
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
  await expect(kit.locator('[data-copy-field="time"] p')).toHaveText(`${new Intl.DateTimeFormat('en-US', { month: 'long', day: 'numeric', year: 'numeric', timeZone: 'UTC' }).format(new Date(`${day(7)}T12:00:00Z`))} · 6–8 PM · America/Los_Angeles`);
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

test('events: a new event is hosted by its creator, who may then rule out design', async ({ page }) => {
  const errors = watchErrors(page);
  await devLogin(page, 'officer');
  const me = await page.request.get('/api/me').then((r) => r.json());
  await page.goto('/events');
  await settle(page);
  await page.locator('[data-new-event]').click();
  const editor = openDialog(page);
  await expect(editor.getByLabel(/^Host officer/)).toHaveValue(me.user.id);
  await expect(editor.getByLabel('Needs design')).toBeEnabled();
  // Choosing "No one yet" sticks, and only a director or the host can rule out design.
  await editor.getByLabel(/^Host officer/).selectOption('');
  await expect(editor.getByLabel(/^Host officer/)).toHaveValue('');
  await expect(editor.getByLabel('Needs design')).toBeDisabled();
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

test('events: a tentative date range stands in for the date until it is set', async ({ page }, info) => {
  const errors = watchErrors(page);
  await devLogin(page, 'officer');
  await page.goto('/events');
  await settle(page);
  const name = `E2E tentative social ${info.project.name} ${Date.now().toString(36)}`;

  await page.locator('[data-new-event]').click();
  const editor = openDialog(page);
  await editor.getByLabel('Event name').fill(name);
  await editor.getByLabel('No set date yet').check();
  await editor.getByRole('button', { name: 'Save draft' }).click();
  await expect(editor.getByText('Pick the earliest date it could be.')).toBeVisible();
  await editor.getByLabel('Earliest date').fill(`${next.ym}-01`);
  await editor.getByLabel(/^Latest date/).fill(`${next.ym}-12`);
  await editor.getByRole('button', { name: 'Save draft' }).click();
  await expect(editor.locator('#ev-saved')).toContainText('Draft saved');
  const id = new URL(page.url()).searchParams.get('edit')!;
  const created = await page.request.get(`/api/events/${id}`).then((r) => r.json());
  expect(created.event.startsAt).toBe('');
  expect(created.event.tentativeFrom).toBe(`${next.ym}-01`);
  expect(created.event.tentativeTo).toBe(`${next.ym}-12`);

  // The list shows the range with a Tentative badge, not a time.
  await page.goto('/events');
  await settle(page);
  const row = page.locator('tr', { has: page.locator(`[data-event-edit="${id}"]`) });
  await expect(row.locator('[data-tentative-range]')).toContainText(`${next.mon} 1 – ${next.mon} 12`);
  await expect(row.locator('[data-tentative-range]')).toContainText('Tentative');
  expect(await overflow(page)).toEqual([]);

  // The month and week calendars list it once above the grid, as a draft, not on a day.
  await page.goto(`/events?view=month&m=${next.ym}`);
  await settle(page);
  const strip = page.locator('[data-tentative-strip]');
  await expect(strip.locator(`[data-tentative-event="${id}"]`)).toContainText(`${next.mon} 1 – ${next.mon} 12`);
  await expect(strip.locator(`[data-tentative-event="${id}"]`)).toContainText('Draft');
  await expect(page.locator(`.cal-month [data-cal-event="${id}"]`)).toHaveCount(0);
  expect(await overflow(page)).toEqual([]);
  await page.goto(`/events?view=week&w=${next.week5}`);
  await settle(page);
  await expect(page.locator(`[data-tentative-strip] [data-tentative-event="${id}"]`)).toBeVisible();
  await page.goto(`/events?view=week&w=${next.week20}`);
  await settle(page);
  await expect(page.locator(`[data-tentative-event="${id}"]`)).toHaveCount(0);
  await page.goto(`/events?view=month&m=${next.ym}`);
  await settle(page);
  await page.locator(`[data-tentative-event="${id}"]`).click();
  await expect(openDialog(page).getByLabel('No set date yet')).toBeChecked();

  // Setting the real date clears the range.
  await page.goto(`/events?edit=${id}`);
  await settle(page);
  const again = openDialog(page);
  await expect(again.getByLabel('No set date yet')).toBeChecked();
  await again.getByLabel('No set date yet').uncheck();
  await again.getByLabel(/^Starts/).fill(`${next.ym}-05T18:00`);
  await again.getByRole('button', { name: 'Save changes' }).click();
  await expect(again.locator('#ev-saved')).toContainText('Changes saved');
  const fixed = await page.request.get(`/api/events/${id}`).then((r) => r.json());
  expect(fixed.event.tentativeFrom).toBeNull();
  expect(fixed.event.tentativeTo).toBeNull();
  expect(fixed.event.startsAt).toContain(`${next.ym}-05`);

  await page.goto(`/events?delete=${id}`);
  await settle(page);
  await page.locator('[data-confirm-delete]').click();
  await expect(page.locator('#delete-note')).toBeVisible();
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
