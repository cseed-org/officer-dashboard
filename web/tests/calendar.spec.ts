import { mkdirSync } from 'node:fs';
import { expect, test, type Page } from '@playwright/test';
import { devLogin, overflow, settle, THEMES, useTheme, watchErrors } from './helpers';

/**
 * Organization calendar (V0.15, /calendar): month, week and the phone agenda, creating, editing
 * and deleting an Internal meeting with invitees, tentative ranges, locked Events items and the
 * calendar/overlay toggles. Every test works in its own week in March 2027 and removes what it
 * creates, so it can run against a shared seeded database.
 *
 * EVIDENCE=1 also saves light and dark screenshots at 1440 and 390 to docs/evidence/org-calendar/.
 */
const WEEK = '2027-03-08';
const stamp = () => Date.now().toString(36);
const phone = (name: string) => name === 'phone';

type Created = { id: string; title: string };
async function meeting(page: Page, body: Record<string, unknown>): Promise<Created> {
  const r = await page.request.post('/api/orgcal/events', { data: { calendarKey: 'internal', ...body } });
  expect(r.ok(), `create meeting → ${r.status()} ${await r.text()}`).toBe(true);
  const { event } = await r.json();
  return { id: event.id, title: event.title };
}
async function removeMeeting(page: Page, id: string) {
  await page.request.delete(`/api/orgcal/events/${id}`);
}
async function eventsIn(page: Page, from: string, to: string) {
  const r = await page.request.get(`/api/orgcal/events?from=${from}&to=${to}`);
  expect(r.ok()).toBe(true);
  return (await r.json()).events as { id: string; title: string; rrule: string | null; startsAt: string | null; invitees: { kind: string; ref: string }[]; calendarKey: string; sourceHref: string | null }[];
}
async function freshToggles(page: Page) {
  await page.addInitScript(() => {
    if (!sessionStorage.getItem('orgcal-e2e')) {
      localStorage.removeItem('cseed-orgcal-toggles');
      sessionStorage.setItem('orgcal-e2e', '1');
    }
  });
}

test.beforeEach(async ({ page }) => {
  await freshToggles(page);
  await devLogin(page, 'officer');
});

test('calendar: month, week and agenda show meetings; toggles hide calendars and persist', async ({ page }, info) => {
  const errors = watchErrors(page);
  const m = await meeting(page, { title: `E2E standup ${stamp()}`, startsAt: '2027-03-10T10:00:00-08:00', endsAt: '2027-03-10T11:00:00-08:00' });
  try {
    await page.goto('/calendar?m=2027-03');
    await settle(page);
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('Calendar');
    await expect(page.locator('#orgcal-period')).toContainText('March 2027');
    const toggles = page.locator('.orgcal-toggle input');
    if (phone(info.project.name)) {
      // Phones fold the calendar list above the agenda.
      await expect(page.locator('details.orgcal-filters')).not.toHaveAttribute('open', '');
      await expect(page.locator('.cal-agenda')).toBeVisible();
      await expect(page.locator('.cal-agenda').getByRole('button', { name: m.title })).toBeVisible();
      await page.locator('.orgcal-filters-summary').click();
    } else {
      await expect(page.locator(`.cal-day[data-date="2027-03-10"] [data-cal-event^="${m.id}"]`)).toBeVisible();
    }
    // Everything is on by default, overlays included.
    await expect(toggles).toHaveCount(7);
    for (const box of await toggles.all()) await expect(box).toBeChecked();

    // Month → week (segmented), then ‹ › and Today keep the URL in step.
    await page.getByRole('group', { name: 'Calendar view' }).getByRole('button', { name: 'Week' }).click();
    await expect(page).toHaveURL(/view=week/);
    await page.goto(`/calendar?view=week&w=${WEEK}`);
    await settle(page);
    await expect(page.locator('#orgcal-period')).toContainText('March 8–14, 2027');
    const item = phone(info.project.name) ? page.locator('.cal-agenda').getByRole('button', { name: m.title }) : page.locator(`.cal-col[data-date="2027-03-10"] .cal-block`, { hasText: m.title });
    await expect(item).toBeVisible();
    await page.getByRole('button', { name: 'Next week' }).click();
    await expect(page).toHaveURL(/w=2027-03-15/);
    await page.getByRole('button', { name: 'Previous week' }).click();
    await expect(page).toHaveURL(/w=2027-03-08/);

    // Hide Internal Meetings: the meeting goes, and stays hidden after a reload.
    if (!(await page.locator('details.orgcal-filters').evaluate((d) => (d as HTMLDetailsElement).open))) await page.locator('.orgcal-filters-summary').click();
    await page.locator('[data-cal="internal"] input').uncheck();
    await expect(item).toHaveCount(0);
    await page.reload();
    await settle(page);
    await expect(page.locator('[data-cal="internal"] input')).not.toBeChecked();
    await expect(item).toHaveCount(0);
    if (!(await page.locator('details.orgcal-filters').evaluate((d) => (d as HTMLDetailsElement).open))) await page.locator('.orgcal-filters-summary').click();
    await page.locator('[data-cal="internal"] input').check();
    await expect(item).toBeVisible();
    expect(await overflow(page)).toEqual([]);
  } finally {
    await removeMeeting(page, m.id);
  }
  expect(errors).toEqual([]);
});

test('calendar: create, edit and delete an internal meeting with invitees', async ({ page }, info) => {
  const errors = watchErrors(page);
  const title = `E2E planning ${info.project.name} ${stamp()}`;
  await page.goto(`/calendar?view=week&w=${WEEK}`);
  await settle(page);
  const editor = page.locator('dialog#orgcal-editor');
  if (phone(info.project.name)) {
    await page.getByRole('button', { name: 'Create' }).click();
    await expect(editor).toBeVisible();
    await editor.getByLabel('Date', { exact: true }).fill('2027-03-10');
    await editor.getByLabel(/^Starts/).fill('14:00');
    await editor.getByLabel('Ends').fill('15:00');
  } else {
    // Click an empty slot on Wednesday: the drawer opens on that day and half hour.
    const col = page.locator('.cal-col[data-date="2027-03-10"]');
    const box = (await col.boundingBox())!;
    const rows = await col.evaluate((el) => Number(getComputedStyle(el).getPropertyValue('--rows')));
    await page.mouse.click(box.x + box.width / 2, box.y + (box.height / rows) * 10 + 3); // row 10 = 14:00 from 9:00
    await expect(editor).toBeVisible();
    await expect(page).toHaveURL(/new=2027-03-10T14%3A00|new=2027-03-10T14:00/);
    await expect(editor.getByLabel('Date', { exact: true })).toHaveValue('2027-03-10');
    await expect(editor.getByLabel(/^Starts/)).toHaveValue('14:00');
  }
  await expect(editor.getByLabel('Calendar')).toHaveValue('internal');
  await expect(editor.getByLabel('Send invites')).not.toBeChecked();
  // Required title.
  await editor.getByRole('button', { name: 'Create meeting' }).click();
  await expect(editor.getByText('Give the meeting a title.')).toBeVisible();
  await editor.getByLabel('Title').fill(title);
  // Invitees: a subteam, an officer and a typed email.
  await editor.getByLabel('Add a subteam').selectOption({ index: 1 });
  await editor.getByLabel('Add an officer').selectOption({ index: 1 });
  await editor.getByLabel('Add an email address').fill('not-an-email');
  await editor.getByRole('button', { name: 'Add', exact: true }).click();
  await expect(editor.getByText('Enter an email address like name@example.org.')).toBeVisible();
  await editor.getByLabel('Add an email address').fill('guest@example.org');
  await editor.getByRole('button', { name: 'Add', exact: true }).click();
  await expect(editor.locator('.orgcal-invitee')).toHaveCount(3);
  await editor.getByLabel('Location').fill('Room 250');
  // Every two weeks until the end of April.
  await editor.getByLabel('Repeat').selectOption({ label: 'Every 2 weeks' });
  await editor.getByLabel(/^Until/).fill('2027-04-30');
  await editor.getByRole('button', { name: 'Create meeting' }).click();
  await expect(editor).toBeHidden();
  await expect(page.getByText(`“${title}” added to Internal Meetings.`)).toBeVisible();

  const saved = (await eventsIn(page, '2027-03-08', '2027-03-15')).find((e) => e.title === title)!;
  expect(saved, 'meeting saved').toBeTruthy();
  expect(saved.calendarKey).toBe('internal');
  expect(saved.invitees.map((i) => i.kind).sort()).toEqual(['email', 'subteam', 'user']);
  expect(saved.rrule).toMatch(/^RRULE:FREQ=WEEKLY;INTERVAL=2;UNTIL=20270501T065959Z$/);
  // The next occurrence is two weeks later, not one.
  expect((await eventsIn(page, '2027-03-15', '2027-03-22')).some((e) => e.id === saved.id)).toBe(false);
  expect((await eventsIn(page, '2027-03-22', '2027-03-29')).find((e) => e.id === saved.id)?.startsAt).toMatch(/^2027-03-24T14:00/);
  try {
    // Open → Edit → rename.
    const block = phone(info.project.name) ? page.locator('.cal-agenda').getByRole('button', { name: title }) : page.locator('.cal-col[data-date="2027-03-10"] .cal-block', { hasText: title });
    await block.click();
    const card = page.locator('dialog#orgcal-item');
    await expect(card.getByRole('heading', { name: title })).toBeVisible();
    await expect(card).toContainText('guest@example.org');
    await card.getByRole('button', { name: 'Edit' }).click();
    await expect(editor.getByLabel('Title')).toHaveValue(title);
    await expect(editor.getByLabel('Calendar')).toBeDisabled();
    await expect(editor).toContainText('Changes apply to every occurrence.');
    await expect(editor.getByLabel('Repeat')).toHaveValue('biweekly');
    await editor.getByLabel('Title').fill(`${title} v2`);
    await editor.getByRole('button', { name: 'Save changes' }).click();
    await expect(editor).toBeHidden();
    await expect(page.getByText(`“${title} v2” saved.`)).toBeVisible();
    // Renaming from one occurrence keeps the series where it was.
    expect((await eventsIn(page, '2027-03-22', '2027-03-29')).find((e) => e.id === saved.id)?.title).toBe(`${title} v2`);
    expect((await eventsIn(page, '2027-03-08', '2027-03-15')).find((e) => e.id === saved.id)?.startsAt).toMatch(/^2027-03-10T14:00/);

    // Open → Delete → confirm.
    const renamed = phone(info.project.name) ? page.locator('.cal-agenda').getByRole('button', { name: `${title} v2` }) : page.locator('.cal-block', { hasText: `${title} v2` });
    await renamed.click();
    await card.getByRole('button', { name: 'Delete' }).click();
    const confirm = page.locator('dialog#orgcal-delete');
    await expect(confirm).toContainText(`${title} v2`);
    await expect(confirm).toContainText('every occurrence included');
    await confirm.getByRole('button', { name: 'Delete meeting' }).click();
    await expect(confirm).toBeHidden();
    await expect(renamed).toHaveCount(0);
    expect((await eventsIn(page, '2027-03-08', '2027-03-15')).some((e) => e.id === saved.id)).toBe(false);
  } finally {
    await removeMeeting(page, saved.id);
  }
  expect(await overflow(page)).toEqual([]);
  expect(errors).toEqual([]);
});

test('calendar: a tentative range spans days in the all-day band', async ({ page }, info) => {
  const errors = watchErrors(page);
  const m = await meeting(page, { title: `E2E retreat ${stamp()}`, tentativeFrom: '2027-03-09', tentativeTo: '2027-03-12' });
  try {
    await page.goto(`/calendar?view=week&w=${WEEK}`);
    await settle(page);
    if (phone(info.project.name)) {
      const row = page.locator('.cal-agenda li', { hasText: m.title });
      await expect(row.first()).toContainText('Tentative');
      await expect(row.first().locator('.cal-event.is-tentative')).toBeVisible();
    } else {
      const span = page.locator('.cal-allday .cal-span', { hasText: m.title });
      await expect(span).toHaveCount(1);
      await expect(span).toHaveClass(/is-tentative/);
      await expect(span).toContainText('Tentative');
      // Tuesday to Friday: four columns from the second.
      expect(await span.evaluate((el) => (el as HTMLElement).style.gridColumn)).toMatch(/^2 \/ span 4$/);
      // Month view: dashed chips on each day of the range.
      await page.goto('/calendar?m=2027-03');
      await settle(page);
      for (const d of ['2027-03-09', '2027-03-12']) await expect(page.locator(`.cal-day[data-date="${d}"] .cal-event.is-tentative`, { hasText: m.title })).toBeVisible();
    }
    // The card says it has no set date yet.
    await page.goto(`/calendar?view=week&w=${WEEK}&id=${m.id}`);
    await settle(page);
    await expect(page.locator('dialog#orgcal-item')).toContainText('Tentative · no set date yet');
  } finally {
    await removeMeeting(page, m.id);
  }
  expect(errors).toEqual([]);
});

test('calendar: Events items are locked and link to their event', async ({ page }, info) => {
  const errors = watchErrors(page);
  const name = `E2E locked social ${stamp()}`;
  const r = await page.request.post('/api/events', { data: { name, startsAt: '2027-03-11T18:00:00-08:00', endsAt: '2027-03-11T20:00:00-08:00' } });
  expect(r.ok(), `create event → ${r.status()}`).toBe(true);
  const { event } = await r.json();
  try {
    await page.goto(`/calendar?view=week&w=${WEEK}`);
    await settle(page);
    const item = phone(info.project.name) ? page.locator('.cal-agenda').getByRole('button', { name: new RegExp(name) }) : page.locator('.cal-col[data-date="2027-03-11"] .cal-block', { hasText: name });
    await expect(item).toHaveAttribute('data-locked', 'true');
    await item.click();
    const card = page.locator('dialog#orgcal-item');
    await expect(card).toBeVisible();
    await expect(card).toHaveAttribute('aria-labelledby', 'orgcal-item-title');
    await expect(card.locator('[data-item-kind="locked"]')).toBeVisible();
    await expect(card.getByRole('button', { name: 'Edit' })).toHaveCount(0);
    await expect(card.getByRole('button', { name: 'Delete' })).toHaveCount(0);
    const link = card.getByRole('link', { name: 'Open event →' });
    await expect(link).toHaveAttribute('href', `/events?id=${event.id}`);
    // A locked item can't be opened in the editor through the URL either.
    await page.goto(`/calendar?view=week&w=${WEEK}&edit=${event.id}`);
    await settle(page);
    await expect(page.locator('dialog#orgcal-editor')).toBeHidden();
  } finally {
    await page.request.delete(`/api/events/${event.id}`);
  }
  expect(errors).toEqual([]);
});

test('calendar: overlays toggle off and on', async ({ page }, info) => {
  const errors = watchErrors(page);
  // Seeded Buildspace check-ins fall on Sundays; read the feed for a month that has them.
  const feed = await page.request.get('/api/calendar/feed?start=2026-11-01&end=2026-12-01&sources=buildspace-weekly');
  const items = (await feed.json()).items as { date: string }[];
  test.skip(items.length === 0, 'no seeded check-ins in this database');
  const day = items[0]!.date;
  const monday = (() => {
    const d = new Date(`${day}T12:00:00Z`);
    d.setUTCDate(d.getUTCDate() - ((d.getUTCDay() + 6) % 7));
    return d.toISOString().slice(0, 10);
  })();
  await page.goto(`/calendar?view=week&w=${monday}`);
  await settle(page);
  const overlay = phone(info.project.name) ? page.locator('.cal-agenda .cal-event', { hasText: 'Weekly check-in due' }) : page.locator('.cal-allday .cal-span', { hasText: 'Weekly check-in due' });
  await expect(overlay.first()).toBeVisible();
  if (!(await page.locator('details.orgcal-filters').evaluate((d) => (d as HTMLDetailsElement).open))) await page.locator('.orgcal-filters-summary').click();
  await page.locator('[data-overlay="buildspace"] input').uncheck();
  await expect(overlay).toHaveCount(0);
  await page.locator('[data-overlay="buildspace"] input').check();
  await expect(overlay.first()).toBeVisible();
  // Grouped: one entry per day, its card lists every check-in with a link.
  await overlay.first().click();
  const card = page.locator('dialog#orgcal-item');
  await expect(card.locator('[data-item-kind="overlay"]')).toBeVisible();
  await expect(card.getByRole('link').first()).toHaveAttribute('href', /^\/buildspace-launchpad/);
  expect(errors).toEqual([]);
});

test('calendar: evidence screenshots (EVIDENCE=1)', async ({ page }, info) => {
  test.skip(!process.env.EVIDENCE || info.project.name === 'tablet', 'evidence runs only on request, at 1440 and 390');
  const dir = '../docs/evidence/org-calendar';
  mkdirSync(dir, { recursive: true });
  const made: string[] = [];
  const add = async (body: Record<string, unknown>) => made.push((await meeting(page, body)).id);
  await add({ title: 'Officer sync', startsAt: '2027-03-09T18:00:00-08:00', endsAt: '2027-03-09T19:00:00-08:00', rrule: 'RRULE:FREQ=WEEKLY;UNTIL=20270401T065959Z', location: 'Zoom' });
  await add({ title: 'Budget review', startsAt: '2027-03-09T18:30:00-08:00', endsAt: '2027-03-09T20:00:00-08:00' });
  await add({ calendarKey: 'external', title: 'Sponsor call: Acme', startsAt: '2027-03-10T10:00:00-08:00', endsAt: '2027-03-10T10:45:00-08:00', invitees: [{ kind: 'email', ref: 'partner@example.org' }] });
  await add({ title: 'Spring retreat planning', tentativeFrom: '2027-03-09', tentativeTo: '2027-03-12' });
  await add({ calendarKey: 'external', title: 'Career fair', startsAt: '2027-03-12', endsAt: '2027-03-13', allDay: true });
  const prog = await (await page.request.post('/api/programs', { data: { trackId: 'buildspace', name: 'buildspace build night', weekday: 1, startTime: '17:00', durationMin: 90, location: 'Room 101', startsOn: '2027-03-01', endsOn: '2027-04-30' } })).json();
  const ev = await (await page.request.post('/api/events', { data: { name: 'Spring Social', startsAt: '2027-03-11T18:00:00-08:00', endsAt: '2027-03-11T20:00:00-08:00' } })).json();
  try {
    for (const theme of THEMES) {
      await useTheme(page, theme);
      await page.evaluate((t) => localStorage.setItem('cseed-theme', t), theme);
      for (const [view, q] of [
        ['month', '?m=2027-03'],
        ['week', `?view=week&w=${WEEK}`],
      ] as const) {
        await page.goto(`/calendar${q}`);
        await settle(page);
        await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
        await page.screenshot({ path: `${dir}/calendar-${view}-${info.project.name === 'phone' ? 390 : 1440}-${theme}.png`, fullPage: true });
      }
      await page.goto(`/calendar?view=week&w=${WEEK}&new=2027-03-10T14:00`);
      await settle(page);
      await expect(page.locator('dialog#orgcal-editor')).toBeVisible();
      await page.locator('dialog#orgcal-editor').getByLabel('Title').fill('Design sync');
      await page.screenshot({ path: `${dir}/calendar-create-${info.project.name === 'phone' ? 390 : 1440}-${theme}.png` });
    }
  } finally {
    for (const id of made) await removeMeeting(page, id);
    await page.request.delete(`/api/events/${ev.event.id}`);
    if (prog.program) await page.request.delete(`/api/programs/${prog.program.id}`);
  }
});
