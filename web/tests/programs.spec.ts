import { expect, test, type Page } from '@playwright/test';
import { devLogin, openDialog, overflow, settle, THEMES, useTheme, watchErrors } from './helpers';

/**
 * Programs tab on Buildspace + Launchpad (V0.15): weekly programs with per-session skip/move
 * exceptions. Each test creates its own program and deletes it in `finally`, so a failed step
 * never leaves one behind in the shared seeded database. Schedule ordering itself is unit-tested
 * in programs-schedule.spec.ts.
 */

/** YYYY-MM-DD n days from today (the test machine and browser run in America/Los_Angeles). */
const day = (n: number) => {
  const d = new Date();
  d.setDate(d.getDate() + n);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
};
/** The weekday two days out, so sessions fall on day(2), day(9), day(16)… */
const weekday = (new Date().getDay() + 2) % 7;

/** Delete every program with this name (ignores ones already gone). */
async function dropPrograms(page: Page, name: RegExp) {
  const list = await page.request.get('/api/programs').then((r) => r.json());
  for (const p of list.programs ?? []) if (name.test(p.name)) await page.request.delete(`/api/programs/${p.id}`);
}

test('programs: create, skip and move a session, edit, delete', async ({ page }, info) => {
  const errors = watchErrors(page);
  await devLogin(page, 'officer');
  const name = `E2E program ${info.project.name} ${Date.now().toString(36)}`;
  try {
    await page.goto('/buildspace-launchpad?tab=programs');
    await settle(page);
    // Programs aren't cohort data: none of the cohort controls show on this tab (review F-3).
    await expect(page.locator('[data-programs-panel]')).toBeVisible();
    await expect(page.getByRole('button', { name: 'New project' })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Add builder' })).toHaveCount(0);
    // The tab strip draws before the register's counts arrive; the underline follows them.
    await expect(page.getByRole('tab', { name: /People\s*\d+/ })).toBeVisible();
    await expect
      .poll(() =>
        page.evaluate(() => {
          const tab = document.getElementById('bs-tab-programs');
          const bar = document.querySelector<HTMLElement>('.bs-toolbar .tab-indicator');
          return !!tab && !!bar && bar.style.transform.startsWith(`translateX(${tab.offsetLeft}px)`);
        }),
      )
      .toBe(true);

    // Create: the tab lists programs grouped by track.
    await page.locator('[data-new-program]').click();
    const editor = openDialog(page);
    await expect(editor.getByRole('heading', { name: 'New program' })).toBeVisible();
    await editor.getByLabel('Program name').fill(name);
    await editor.getByLabel('Track').selectOption({ label: 'buildspace' });
    await editor.getByLabel('Weekday').selectOption(String(weekday));
    await editor.getByLabel(/^Start time/).fill('18:00');
    await editor.getByLabel(/^Duration/).fill('90');
    await editor.getByLabel(/^Location/).fill('The Nest');
    await editor.getByLabel('First date').fill(day(-3));
    await editor.getByLabel('Last date').fill(day(45));
    await editor.getByLabel(/^Description/).fill('Weekly e2e program.');
    await editor.getByRole('button', { name: 'Save program' }).click();
    await expect(page.locator('dialog[open]')).toHaveCount(0);
    await expect(page.getByRole('status').filter({ hasText: 'Weekly Programs calendar' }).first()).toBeVisible();

    const row = page.locator('tr', { has: page.locator('[data-program-open]', { hasText: name }) });
    await expect(row).toContainText('The Nest');
    await expect(row).toContainText('6 PM–7:30 PM');
    await expect(row.locator('[data-next-session]')).toHaveAttribute('data-next-session', day(2));
    expect(await overflow(page)).toEqual([]);

    // Track filter narrows the list.
    await page.getByLabel('Filter by track').selectOption({ label: 'buildHER' });
    await expect(page.locator('[data-program-open]', { hasText: name })).toHaveCount(0);
    await page.getByLabel('Filter by track').selectOption({ label: 'buildspace' });
    await expect(page.locator('[data-program-open]', { hasText: name })).toHaveCount(1);

    // Open the program; skip the first session and undo it.
    await page.locator('[data-program-open]', { hasText: name }).click();
    const detail = openDialog(page);
    await expect(detail.getByRole('heading', { name })).toBeVisible();
    const first = detail.locator(`[data-session="${day(2)}"]`);
    await expect(detail.locator('[data-session]').first()).toHaveAttribute('data-session', day(2));
    await expect(first).toHaveAttribute('data-session-state', 'scheduled');
    await first.getByRole('button', { name: /^Skip the .* session$/ }).click();
    await expect(first).toHaveAttribute('data-session-state', 'skipped');
    await expect(first.getByText('Skipped')).toBeVisible();
    await first.locator('[data-undo-exception]').click();
    await expect(first).toHaveAttribute('data-session-state', 'scheduled');

    // Move it past the following week (review F-1): it is listed on its new date, after the
    // week-two session, which becomes the next session.
    await first.locator('[data-move-session]').click();
    await expect(first.locator('[data-move-session]')).toHaveAttribute('aria-expanded', 'true');
    const move = first.locator('[data-move-form]');
    await move.getByLabel('New date').fill(day(16));
    await move.getByLabel(/^Start/).fill('19:00');
    await move.getByLabel(/^End/).fill('20:30');
    await move.getByLabel(/^Location/).fill('Room 250');
    await move.getByRole('button', { name: 'Save move' }).click();
    await expect(first).toHaveAttribute('data-session-state', 'moved');
    await expect(first).toHaveAttribute('data-session-on', day(16));
    await expect(first).toContainText('Room 250');
    await expect(first).toContainText('7 PM–8:30 PM');
    await expect(first).toContainText('was ');
    const order = await detail.locator('[data-session]').evaluateAll((els) => els.slice(0, 4).map((e) => (e as HTMLElement).dataset.session));
    expect(order).toEqual([day(9), day(16), day(2), day(23)]);
    expect(await overflow(page)).toEqual([]);

    // Back on the list (closing steps back over the drawer's entry; Back doesn't reopen it).
    await detail.getByRole('button', { name: 'Close' }).click();
    await expect(page.locator('dialog[open]')).toHaveCount(0);
    await expect(page).not.toHaveURL(/program=/);
    await expect(row.locator('[data-next-session]')).toHaveAttribute('data-next-session', day(9));

    // Undo the move.
    await page.locator('[data-program-open]', { hasText: name }).click();
    await openDialog(page).locator(`[data-session="${day(2)}"] [data-undo-exception]`).click();
    await expect(openDialog(page).locator(`[data-session="${day(2)}"]`)).toHaveAttribute('data-session-state', 'scheduled');

    // Edit: skip a session, then change the weekday: the form warns that the skip will be dropped.
    await openDialog(page).locator(`[data-session="${day(9)}"] [data-skip-session]`).click();
    await expect(openDialog(page).locator(`[data-session="${day(9)}"]`)).toHaveAttribute('data-session-state', 'skipped');
    await openDialog(page).getByRole('button', { name: 'Edit' }).click();
    await expect(openDialog(page).locator('[data-stranded-exceptions]')).toHaveCount(0);
    await openDialog(page).getByLabel('Weekday').selectOption(String((weekday + 1) % 7));
    await expect(openDialog(page).locator('[data-stranded-exceptions="1"]')).toBeVisible();
    await openDialog(page).getByLabel('Weekday').selectOption(String(weekday));
    await expect(openDialog(page).locator('[data-stranded-exceptions]')).toHaveCount(0);
    // Cancel leaves the form without saving.
    await openDialog(page).getByRole('button', { name: 'Cancel' }).click();
    await expect(openDialog(page).locator('[data-program-detail]')).toBeVisible();
    await openDialog(page).getByRole('button', { name: 'Edit' }).click();
    await openDialog(page).getByLabel('Program name').fill(`${name} v2`);
    await openDialog(page).getByRole('button', { name: 'Save program' }).click();
    await expect(page.locator('dialog[open]')).toHaveCount(0);
    await expect(page.locator('[data-program-open]', { hasText: `${name} v2` })).toHaveCount(1);

    // Delete asks for confirmation.
    await page.locator('[data-program-open]', { hasText: `${name} v2` }).click();
    await openDialog(page).locator('[data-delete-program]').click();
    await openDialog(page).locator('[data-confirm-delete-program]').click();
    await expect(page.locator('dialog[open]')).toHaveCount(0);
    await expect(page.locator('[data-program-open]', { hasText: `${name} v2` })).toHaveCount(0);
    expect(errors).toEqual([]);
  } finally {
    await dropPrograms(page, new RegExp(`^${name}`));
  }
});

test('programs: a failed session write shows its error inside the drawer', async ({ page }, info) => {
  await devLogin(page, 'officer');
  const name = `E2E program errors ${info.project.name} ${Date.now().toString(36)}`;
  try {
    const created = await page.request
      .post('/api/programs', { data: { trackId: 'buildspace', name, weekday, startTime: '18:00', durationMin: 60, startsOn: day(0), endsOn: day(30) } })
      .then((r) => r.json());
    await page.goto(`/buildspace-launchpad?tab=programs&program=${created.program.id}`);
    await settle(page);
    // The server refuses the write: the drawer (a modal) shows why, next to the session.
    await page.route('**/api/programs/*/exceptions', (route) =>
      route.fulfill({ status: 409, contentType: 'application/json', body: JSON.stringify({ error: { code: 'conflict', message: 'Someone else changed this program.' } }) }),
    );
    await openDialog(page).locator(`[data-session="${day(2)}"] [data-skip-session]`).click();
    await expect(openDialog(page).locator('[data-program-error]')).toContainText('Someone else changed this program.');
    await expect(openDialog(page).locator(`[data-session="${day(2)}"]`)).toHaveAttribute('data-session-state', 'scheduled');
    // The next action clears it.
    await page.unroute('**/api/programs/*/exceptions');
    await openDialog(page).locator(`[data-session="${day(2)}"] [data-skip-session]`).click();
    await expect(openDialog(page).locator(`[data-session="${day(2)}"]`)).toHaveAttribute('data-session-state', 'skipped');
    await expect(openDialog(page).locator('[data-program-error]')).toHaveCount(0);
  } finally {
    await dropPrograms(page, new RegExp(`^${name}$`));
  }
});

// Tracks can't be deleted, so each viewport reuses one fixed track: the first run on a database
// adds it through the form; later runs check that a duplicate key is refused in the dialog.
test('programs: leadership adds a track', async ({ page }, info) => {
  const errors = watchErrors(page);
  await devLogin(page, 'leadership');
  const key = `e2e-track-${info.project.name}`;
  const trackName = `E2E track ${info.project.name}`;
  const exists = (await page.request.get('/api/program-tracks').then((r) => r.json())).tracks.some((t: { key: string }) => t.key === key);
  await page.goto('/buildspace-launchpad?tab=programs');
  await settle(page);
  await page.locator('[data-manage-tracks]').click();
  const dialog = openDialog(page);
  await dialog.getByLabel('New track name').fill(trackName);
  await dialog.getByLabel('Key').fill(key);
  await dialog.getByLabel('Colour').selectOption('teal');
  await dialog.getByRole('button', { name: 'Add track' }).click();
  if (!exists) {
    // Added: a confirmation, no error, and the form is cleared for the next one (review F-2).
    await expect(dialog.getByRole('status').filter({ hasText: `${trackName} added.` })).toBeVisible();
    await expect(dialog.locator('.notice--negative')).toHaveCount(0);
    await expect(dialog.getByLabel('New track name')).toHaveValue('');
    await expect(dialog.getByLabel('Key')).toHaveValue('');
  } else {
    await expect(dialog.locator('.notice--negative')).toBeVisible();
    errors.length = 0; // the refused POST is the expected failure here
  }
  await expect(dialog.locator('.pg-tracks li', { hasText: key })).toHaveCount(1);
  await expect(page.getByLabel('Filter by track').locator('option').filter({ hasText: new RegExp(`^${trackName}$`) })).toHaveCount(1);
  expect(errors).toEqual([]);
});

test.describe('programs review screenshots', () => {
  test.skip(!process.env.SHOTS, 'set SHOTS=1 to capture review screenshots');
  for (const theme of THEMES) {
    test(`programs + tentative events screenshots, ${theme}`, async ({ page }, info) => {
      await useTheme(page, theme);
      await devLogin(page, 'officer');
      // A program and a tentative event to look at, cleaned up afterwards.
      let eventId = '';
      try {
        await page.request.post('/api/programs', {
          data: { trackId: 'buildspace', name: 'Robotics drop-in', weekday: 3, startTime: '18:00', durationMin: 90, location: 'The Nest', startsOn: day(-7), endsOn: day(60), description: 'Open shop time for robot teams.' },
        });
        const ev = await page.request.post('/api/events', { data: { name: 'Winter social (date TBD)', tentativeFrom: day(14), tentativeTo: day(21) } }).then((r) => r.json());
        eventId = ev.event.id;
        const dir = '../docs/evidence/programs';
        const shot = async (name: string) => {
          await page.waitForTimeout(500);
          await page.screenshot({ path: `${dir}/programs-${info.project.name}-${theme}-${name}.png`, fullPage: !(await page.locator('dialog[open]').count()) });
        };
        await page.goto('/buildspace-launchpad?tab=programs');
        await settle(page);
        await shot('list');
        await page.locator('[data-program-open]', { hasText: 'Robotics drop-in' }).click();
        await expect(openDialog(page).locator('[data-session]').first()).toBeVisible();
        await shot('detail');
        // Closing steps back through history (buildspace/history.ts): wait for it before navigating.
        await openDialog(page).getByRole('button', { name: 'Close' }).click();
        await expect(page).not.toHaveURL(/program=/);
        await page.locator('[data-new-program]').click();
        await shot('form');
        await openDialog(page).getByRole('button', { name: 'Close' }).click();
        await expect(page).not.toHaveURL(/pnew=/);
        await page.goto(`/events?edit=${eventId}`);
        await settle(page);
        await shot('tentative-editor');
        await page.goto('/events');
        await settle(page);
        await expect(page.locator('[data-tentative-range]').first()).toBeVisible();
        await shot('tentative-list');
      } finally {
        await dropPrograms(page, /^Robotics drop-in$/);
        if (eventId) await page.request.delete(`/api/events/${eventId}`);
      }
    });
  }
});
