import { expect, test, type Page } from '@playwright/test';
import { devLogin, overflow, settle, THEMES, useTheme, watchErrors } from './helpers';

// V0.6: Buildspace + Launchpad motion parity and the configurable check-in due day.
const SHOTS = 'tmp/verify/launchpad';

/** Record every FLIP (Element.animate on a data-flip item) before page scripts run. */
async function recordFlips(page: Page) {
  await page.addInitScript(() => {
    (window as any).flips = [];
    const animate = Element.prototype.animate;
    Element.prototype.animate = function (...args: Parameters<Element['animate']>) {
      const key = (this as HTMLElement).dataset?.flip;
      if (key) (window as any).flips.push(key);
      return animate.apply(this, args);
    };
  });
}
/** Watch the next crossfade overlay in the page itself: it only lives for 180ms. */
const watchCrossfade = (page: Page) => page.evaluate(() => {
  const view = document.querySelector('.bs-view')!;
  (window as any).crossfade = new Promise((resolve) => {
    const seen: any = {};
    new MutationObserver((_, obs) => {
      const o = view.querySelector<HTMLElement>('.bs-view-out .async-outgoing');
      if (o && !seen.added) Object.assign(seen, { added: true, hidden: o.getAttribute('aria-hidden'), inert: o.inert, position: getComputedStyle(o).position, ids: o.querySelectorAll('[id]').length, height: view.getBoundingClientRect().height });
      if (!o && seen.added) { obs.disconnect(); resolve({ ...seen, heightAfter: view.getBoundingClientRect().height }); }
    }).observe(view, { childList: true, subtree: true });
  });
});
const crossfade = (page: Page) => page.evaluate(() => (window as any).crossfade) as Promise<Record<string, unknown>>;
const flips = (page: Page) => page.evaluate(() => (window as any).flips as string[]);
const clearFlips = (page: Page) => page.evaluate(() => { (window as any).flips = []; });

async function open(page: Page, path = '/buildspace-launchpad') {
  await page.goto(path);
  await settle(page);
  await expect(page.locator('.bs-table, .bs-board, .bs-groups').first()).toBeVisible();
}

test.beforeEach(async ({ page }) => {
  await page.request.put('/api/me/prefs', { data: { dashboardTutorialSeen: true } }).catch(() => undefined);
});

test('tabs slide one indicator and views crossfade without resizing the page', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop', 'shared motion contract');
  const errors = watchErrors(page);
  await devLogin(page, 'director');
  await open(page);
  const tabs = page.getByRole('tablist', { name: 'Buildspace + Launchpad directory' });
  await expect(tabs.getByRole('tab')).toHaveCount(5); // People, Projects, Mentors, Programs, Invites
  await expect(tabs.getByRole('tab', { name: /People/ })).toHaveAttribute('aria-selected', 'true');
  const bar = page.locator('.bs-toolbar .tab-indicator');
  const before = await bar.evaluate((el) => getComputedStyle(el).transform);
  await watchCrossfade(page);
  await tabs.getByRole('tab', { name: /Projects/ }).click();
  // The outgoing view is an inert, aria-hidden overlay; the incoming view owns layout,
  // so removing the overlay does not resize the panel.
  const fade = await crossfade(page);
  expect(fade).toMatchObject({ hidden: 'true', inert: true, position: 'absolute', ids: 0 });
  expect(fade.heightAfter).toBe(fade.height);
  await expect.poll(() => bar.evaluate((el) => getComputedStyle(el).transform)).not.toBe(before);
  expect(await bar.evaluate((el) => getComputedStyle(el).transitionProperty)).toBe('transform');
  await expect(page).toHaveURL(/tab=projects/);
  await expect(page.getByRole('tabpanel')).toHaveAttribute('aria-labelledby', 'bs-tab-projects');
  // Keyboard: arrows move between tabs.
  await tabs.getByRole('tab', { name: /Projects/ }).focus();
  await page.keyboard.press('ArrowRight');
  await expect(page).toHaveURL(/tab=mentors/);
  // Layout switch crossfades too.
  await watchCrossfade(page);
  await page.getByRole('button', { name: 'Cards' }).click();
  expect(await crossfade(page)).toMatchObject({ added: true, hidden: 'true' });
  await expect(page.locator('.bs-cards.motion-list').first()).toBeVisible();
  expect(errors).toEqual([]);
});

test('arrow keys keep focus on the tab strip across the Programs tab', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop', 'keyboard contract');
  const errors = watchErrors(page);
  await devLogin(page, 'director');
  await open(page, '/buildspace-launchpad?tab=mentors');
  // Programs renders without the cohort register; the tab strip is one instance for every tab
  // (review N-1), so focus follows the arrow keys onto and off it.
  await page.locator('#bs-tab-mentors').focus();
  await page.keyboard.press('ArrowRight');
  await expect(page).toHaveURL(/tab=programs/);
  await expect(page.locator('#bs-tab-programs')).toBeFocused();
  await expect(page.locator('[data-programs-panel]')).toBeVisible();
  await page.keyboard.press('ArrowRight');
  await expect(page).toHaveURL(/tab=invites/);
  await expect(page.locator('#bs-tab-invites')).toBeFocused();
  await page.keyboard.press('ArrowLeft');
  await expect(page.locator('#bs-tab-programs')).toBeFocused();
  await page.keyboard.press('ArrowLeft');
  await expect(page).toHaveURL(/tab=mentors/);
  await expect(page.locator('#bs-tab-mentors')).toBeFocused();
  // The summary tiles stay above the tab strip on the register tabs.
  const tiles = await page.locator('.stat-tiles').first().boundingBox();
  const strip = await page.locator('.bs-toolbar').boundingBox();
  expect(tiles!.y).toBeLessThan(strip!.y);
  expect(errors).toEqual([]);
});

test('first frame is paintable and only the first eight rows stagger', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop', 'shared CSS contract');
  await devLogin(page, 'director');
  await open(page);
  await page.addStyleTag({ content: '.bs-table tbody tr {animation-play-state:paused!important;animation-delay:0s!important;}' });
  const r = await page.evaluate(() => {
    const body = document.querySelector('.bs-table tbody')!;
    const rows = [...body.children] as HTMLElement[];
    rows.forEach((row) => { row.style.animation = 'none'; });
    void (body as HTMLElement).offsetWidth;
    rows.forEach((row) => { row.style.animation = ''; });
    return { n: rows.length, opacity: rows.slice(0, 8).map((row) => Number(getComputedStyle(row).opacity)), beyond: rows.slice(8).map((row) => getComputedStyle(row).animationName) };
  });
  expect(r.n).toBeGreaterThan(8);
  expect(r.opacity.every((o) => o > 0 && o < 1)).toBe(true);
  expect(r.beyond.every((a) => a === 'none')).toBe(true);
});

test('filters reflow with FLIP and board stage moves glide between lanes', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop', 'one viewport is enough');
  await recordFlips(page);
  await devLogin(page, 'director');
  await open(page, '/buildspace-launchpad?tab=projects');
  await clearFlips(page);
  await page.getByLabel('Track').selectOption('Hardware');
  await expect(page.locator('.bs-table tbody tr')).toHaveCount(1);
  await expect.poll(() => flips(page)).toContain('bs-sprout');

  // Board: change a project's stage in the drawer; its card moves lanes with a FLIP.
  await page.getByLabel('Track').selectOption('');
  await page.getByRole('button', { name: 'Board' }).click();
  const lane = (stage: string) => page.locator('.bs-lane').filter({ has: page.getByRole('heading', { name: new RegExp(`^${stage}`) }) });
  await expect(lane('Building').getByRole('button', { name: 'Moss', exact: true })).toBeVisible();
  await lane('Building').getByRole('button', { name: 'Moss', exact: true }).click();
  await expect(page.locator('.drawer[open]')).toBeVisible();
  expect(await page.locator('.drawer[open]').evaluate((el) => getComputedStyle(el).animationName)).toBe('drawer-in');
  await page.getByRole('button', { name: 'Edit project' }).click();
  await page.getByLabel('Stage', { exact: true }).selectOption('Demo-ready');
  await clearFlips(page);
  await page.getByRole('button', { name: /Save/ }).click();
  await expect(page.locator('.drawer[open]')).toHaveCount(0);
  await expect(lane('Demo-ready').getByRole('button', { name: 'Moss', exact: true })).toBeVisible();
  await expect.poll(() => flips(page)).toContain('bs-moss');
  // Put it back for other tests.
  await lane('Demo-ready').getByRole('button', { name: 'Moss', exact: true }).click();
  await page.getByRole('button', { name: 'Edit project' }).click();
  await page.getByLabel('Stage', { exact: true }).selectOption('Building');
  await page.getByRole('button', { name: /Save/ }).click();
  await expect(lane('Building').getByRole('button', { name: 'Moss', exact: true })).toBeVisible();
});

test('reduced motion: no crossfade overlay, FLIP or entry animations', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop', 'one viewport is enough');
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await recordFlips(page);
  await devLogin(page, 'director');
  await open(page);
  await page.getByRole('tab', { name: /Projects/ }).click();
  await expect(page.locator('.bs-view-out .async-outgoing')).toHaveCount(0);
  await page.getByLabel('Track').selectOption('Hardware');
  await expect(page.locator('.bs-table tbody tr')).toHaveCount(1);
  expect(await flips(page)).toEqual([]);
  const names = await page.evaluate(() => ['.bs-table tbody tr', '.page-enter', '.async-content'].map((s) => getComputedStyle(document.querySelector(s)!).animationName));
  expect(names.every((n) => n === 'none')).toBe(true);
});

test('manual entry form fields enter with the shared stagger and conditional fields animate in', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop', 'one viewport is enough');
  await devLogin(page, 'director');
  await open(page, '/buildspace-launchpad?project=bs-moss');
  await page.getByRole('button', { name: 'Add entry' }).click();
  const form = page.locator('.drawer[open] form.bs-form');
  await expect(form).toHaveClass(/motion-list/);
  expect(await form.locator('> label').first().evaluate((el) => getComputedStyle(el).animationName)).toBe('item-enter');
  await page.getByLabel('Entry type').selectOption('mentor');
  const mentor = form.getByLabel('Mentor');
  await expect(mentor).toBeVisible();
  expect(await mentor.evaluate((el) => getComputedStyle(el.closest('label')!).animationName)).toBe('item-enter');
  await page.keyboard.press('Escape');
  await expect(page.locator('.drawer[open]')).toHaveCount(0);
});

test('leadership sets the due day from the Buildspace + Launchpad page; officers can only read it', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop', 'one viewport is enough');
  const errors = watchErrors(page);
  await devLogin(page, 'officer');
  await open(page);
  await page.getByText('Check-in schedule', { exact: true }).click();
  await expect(page.getByText('Leadership can change the due day.')).toBeVisible();
  await expect(page.getByLabel('Due day')).toHaveCount(0);
  await page.keyboard.press('Escape');

  await devLogin(page, 'leadership');
  await open(page);
  // Start from no pending change (an interrupted earlier run may have left one).
  let original = await page.request.get('/api/buildspace-launchpad').then((r) => r.json());
  if (original.schedule.pending) {
    const { dueWeekday, dueTime } = original.schedule.current;
    expect((await page.request.put('/api/buildspace-launchpad/schedule', { data: { dueWeekday, dueTime } })).ok()).toBe(true);
    await open(page);
    original = await page.request.get('/api/buildspace-launchpad').then((r) => r.json());
  }
  const summary = page.getByText('Check-in schedule', { exact: true });
  await summary.click();
  const panel = page.locator('.bs-schedule-panel');
  expect(await panel.evaluate((el) => getComputedStyle(el).animationName)).toBe('pop-in');
  const next = (original.schedule.current.dueWeekday + 3) % 7;
  const names = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];
  await page.getByLabel('Due day').selectOption(String(next));
  await page.getByLabel('Due time · optional').fill('18:00');
  await page.getByRole('button', { name: 'Save schedule' }).click();
  await expect(page.getByText('Check-in schedule saved.')).toBeVisible();
  // The current week keeps its deadline; the change is pending (checked in the panel below).
  const saved = await page.request.get('/api/buildspace-launchpad').then((r) => r.json());
  expect(saved.weekDue).toBe(original.weekDue);
  expect(saved.schedule.pending).toMatchObject({ dueWeekday: next, dueTime: '18:00' });
  await summary.click();
  await expect(panel.locator('.bs-schedule-pending')).toContainText(`${names[next]}s, 6:00 PM`);
  // Choosing the current setting again cancels the pending change.
  await page.getByLabel('Due day').selectOption(String(original.schedule.current.dueWeekday));
  await page.getByLabel('Due time · optional').fill(original.schedule.current.dueTime ?? '');
  await page.getByRole('button', { name: 'Save schedule' }).click();
  await expect.poll(() => page.request.get('/api/buildspace-launchpad').then((r) => r.json()).then((b) => b.schedule.pending)).toBeNull();
  expect(errors).toEqual([]);
});

for (const theme of THEMES) {
  test(`screenshots and layout: ${theme}`, async ({ page }, info) => {
    test.skip(info.project.name === 'tablet', 'desktop and phone evidence');
    const width = info.project.name === 'phone' ? 390 : 1440;
    await useTheme(page, theme);
    await devLogin(page, 'leadership');
    for (const [name, path] of [['people', '/buildspace-launchpad'], ['board', '/buildspace-launchpad?tab=projects&view=board'], ['drawer', '/buildspace-launchpad?person=bs-person-01']] as const) {
      await open(page, path);
      await page.waitForTimeout(400);
      expect(await overflow(page)).toEqual([]);
      await page.screenshot({ path: `../${SHOTS}/${name}-${width}-${theme}.png`, fullPage: name !== 'drawer' });
    }
    await open(page);
    await page.getByText('Check-in schedule', { exact: true }).click();
    await page.waitForTimeout(300);
    expect(await overflow(page)).toEqual([]);
    const box = await page.locator('.bs-schedule-panel').boundingBox();
    expect(box!.x).toBeGreaterThanOrEqual(0);
    expect(box!.x + box!.width).toBeLessThanOrEqual(width);
    await page.screenshot({ path: `../${SHOTS}/schedule-${width}-${theme}.png` });
  });
}

test('legacy Buildspace links preserve the selected person', async ({ page }) => {
  test.skip(!process.env.WEB_ORIGIN, 'the /buildspace 301 lives in the Go server; astro dev has no redirect');
  await devLogin(page, 'director');
  await page.goto('/buildspace?person=bs-person-01');
  await expect(page).toHaveURL(/\/buildspace-launchpad\?person=bs-person-01/);
  expect(await overflow(page)).toEqual([]);
  await expect(page.getByRole('heading', { name: 'Buildspace + Launchpad', exact: true })).toBeVisible();
});

test('builder opens the shared person drawer without navigating and keeps edit actions', async ({ page }) => {
  await devLogin(page, 'director');
  await open(page);
  await page.evaluate(() => { (window as any).drawerDocument = 'same-document'; });
  await page.locator('.bs-table').getByRole('button', { name: 'Alex Kim', exact: true }).click();
  await expect(page).toHaveURL(/person=bs-person-01/);
  const drawer = page.locator('#buildspace-person');
  await expect(drawer).toBeVisible();
  await expect(drawer.locator('.person-hero')).toContainText('Alex Kim');
  await expect(drawer.getByRole('button', { name: 'Edit builder', exact: true })).toBeVisible();
  expect(await page.evaluate(() => (window as any).drawerDocument)).toBe('same-document');
  await page.goBack();
  await expect(drawer).not.toBeVisible();
  await page.goForward();
  await expect(drawer).toBeVisible();
  await page.reload();
  await expect(drawer.locator('.person-hero')).toContainText('Alex Kim');
  await drawer.getByRole('button', { name: 'Edit builder', exact: true }).click();
  await expect(page.locator('#buildspace-detail')).toBeVisible();
  await expect(page).toHaveURL(/edit=person/);
});


test('project check-in authors open the person drawer in place', async ({ page }) => {
  await devLogin(page, 'director');
  const activity = await page.request.post('/api/buildspace-launchpad/activity', { data: { projectId: 'bs-moss', personId: 'bs-person-01', kind: 'weekly', date: '2026-11-13', body: 'Drawer check-in fixture', status: 'on-track' } });
  expect(activity.ok()).toBe(true);
  await open(page, '/buildspace-launchpad?project=bs-moss');
  const project = page.locator('#buildspace-detail');
  await project.getByRole('button', { name: 'Timeline', exact: true }).click();
  await project.getByRole('button', { name: 'Alex Kim', exact: true }).first().click();
  await expect(page.locator('#buildspace-person .person-hero')).toContainText('Alex Kim');
  await expect(page).toHaveURL(/person=bs-person-01/);
  await expect(project).not.toBeVisible();
});

test('switching partners preserves the drawer history entry', async ({ page }) => {
  await devLogin(page, 'director');
  await page.goto('/events');
  await open(page);
  await page.locator('.bs-table').getByRole('button', { name: 'Alex Kim', exact: true }).click();
  const drawer = page.locator('#buildspace-person');
  await drawer.locator('.bs-person-list button').first().click();
  await expect(page).not.toHaveURL(/person=bs-person-01/);
  await page.keyboard.press('Escape');
  await expect(drawer).not.toBeVisible();
  await expect(page).toHaveURL(/\/buildspace-launchpad$/);
  await page.goBack();
  await expect(page).toHaveURL(/\/events$/);
});

test('profile tabs, missing builder and edit cancellation use shared drawer behavior', async ({ page }) => {
  await devLogin(page, 'director');
  await open(page, '/buildspace-launchpad?person=bs-person-01');
  const drawer = page.locator('#buildspace-person');
  await expect(drawer.locator('[data-person-stats]')).toContainText('Joined');
  await drawer.getByRole('tab', { name: 'Timeline', exact: true }).click();
  await expect(drawer.getByRole('button', { name: 'Alex Kim', exact: true })).toHaveCount(0);
  await drawer.getByRole('button', { name: 'Edit builder', exact: true }).click();
  await expect(page.locator('#buildspace-detail')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(drawer).toBeVisible();
  await expect(page).not.toHaveURL(/edit=/);
  await page.goto('/buildspace-launchpad?person=nope');
  await expect(drawer).toContainText('This record could not be found');
  await expect(drawer.locator('.person-hero')).not.toContainText('Record not found');
});

test('Back after closing the Edit builder form does not reopen the form (R-4)', async ({ page }) => {
  await devLogin(page, 'director');
  await page.goto('/events');
  await open(page);
  await page.locator('.bs-table').getByRole('button', { name: 'Alex Kim', exact: true }).click();
  const drawer = page.locator('#buildspace-person');
  const form = page.locator('#buildspace-detail');
  await expect(drawer).toBeVisible();
  await drawer.getByRole('button', { name: 'Edit builder', exact: true }).click();
  await expect(form).toBeVisible();
  await expect(page).toHaveURL(/edit=person/);
  await page.keyboard.press('Escape');
  await expect(drawer).toBeVisible();
  await expect(page).not.toHaveURL(/edit=/);
  await page.keyboard.press('Escape');
  await expect(drawer).not.toBeVisible();
  await expect(page).toHaveURL(/\/buildspace-launchpad$/);
  await page.goBack();
  await expect(page).toHaveURL(/\/events$/);
  await expect(page.locator('dialog[open]')).toHaveCount(0);

  // A new-builder form closed with Esc doesn't come back either.
  await open(page);
  await page.getByRole('button', { name: 'Add builder' }).click();
  await expect(form).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(form).not.toBeVisible();
  await expect(page).toHaveURL(/\/buildspace-launchpad$/);
  await page.goBack();
  await expect(page).toHaveURL(/\/events$/);
});

test('drawer shows Joined as a formatted date and the check-in chip carries its due date (R-5, R-6)', async ({ page }) => {
  await devLogin(page, 'director');
  const data = await page.request.get('/api/buildspace-launchpad').then((r) => r.json());
  await open(page, '/buildspace-launchpad?person=bs-person-01');
  const drawer = page.locator('#buildspace-person');
  const joined = drawer.locator('[data-person-stats] div', { hasText: 'Joined' }).locator('dd');
  await expect(joined).not.toHaveText(/\d{4}-\d{2}-\d{2}/);
  await expect(joined).toHaveText(/^[A-Z][a-z]{2} \d{1,2}, \d{4}$/);
  const alex = data.people.find((p: any) => p.id === 'bs-person-01');
  await expect(joined.locator('time')).toHaveAttribute('datetime', alex.joinedOn);
  const chip = drawer.locator('[data-checkin]');
  await expect(chip).toHaveCount(1);
  if ((await chip.getAttribute('data-checkin')) === 'missed') {
    const due = new Date(`${data.weekDue}T12:00:00`).toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
    await expect(chip).toContainText(due);
  } else {
    await expect(chip).toHaveText('Checked in');
  }
});

test('Log check-in jumps to the Add entry tab, primary while the check-in is missing', async ({ page }) => {
  const errors = watchErrors(page);
  await devLogin(page, 'officer');
  await open(page, '/buildspace-launchpad?person=bs-person-01');
  const drawer = page.locator('#buildspace-person');
  const log = drawer.locator('[data-person-action="log-checkin"]');
  await expect(log).toHaveText('Log check-in');
  const missed = (await drawer.locator('[data-checkin]').getAttribute('data-checkin')) === 'missed';
  await expect(log).toHaveClass(missed ? /btn--primary/ : /^(?!.*btn--primary)/);
  await log.click();
  const tab = drawer.getByRole('tab', { name: 'Add entry' });
  await expect(tab).toHaveAttribute('aria-selected', 'true');
  await expect(tab).toBeInViewport();
  await expect(drawer.getByLabel('Entry type')).toHaveValue('weekly');
  expect(errors).toEqual([]);
});

test('cards use the officer card style in every grouping, open the drawer, and fit the page', async ({ page }) => {
  const errors = watchErrors(page);
  await devLogin(page, 'director');
  for (const group of ['track', 'mentor', 'project']) {
    await open(page, `/buildspace-launchpad?view=cards&group=${group}`);
    const cards = page.locator('.bs-groups [data-person-card]');
    expect(await cards.count()).toBeGreaterThan(0);
    const first = cards.first();
    await expect(first.locator('.bs-card-band')).toBeAttached();
    await expect(first.locator('.avatar')).toBeVisible();
    await expect(first.locator('.people-chip').first()).toBeVisible();
    await expect(first.locator('[data-signal="mentor"], [data-signal="requests"], [data-signal="activity"]')).toHaveCount(3);
    // Group headings carry a count and collapse.
    const head = page.locator('.bs-group-head').first();
    await expect(head.locator('.people-count')).toHaveText(/\d+/);
    await head.locator('.people-collapse').click();
    await expect(page.locator('.bs-group').first().locator('.bs-cards')).toBeHidden();
    await head.locator('.people-collapse').click();
    expect(await overflow(page)).toEqual([]);
  }
  // Clicking the card body (not only the name) opens the builder's drawer.
  const alex = page.locator('.bs-groups [data-person-card="bs-person-01"]').first();
  await alex.locator('[data-signal="activity"]').click();
  await expect(page).toHaveURL(/person=bs-person-01/);
  await expect(page.locator('#buildspace-person .person-hero')).toContainText('Alex Kim');
  await page.keyboard.press('Escape');
  // The card menu opens the edit form directly.
  await alex.hover();
  await alex.getByRole('button', { name: 'Actions for Alex Kim' }).click();
  await page.getByRole('menuitem', { name: 'Edit builder' }).click();
  await expect(page.locator('#buildspace-detail')).toBeVisible();
  await page.keyboard.press('Escape');
  // Closing pops history asynchronously; let it land before the next navigation.
  await expect(page).not.toHaveURL(/edit=person/);
  // Projects and mentors render the same card family; board lanes reuse the project card.
  await open(page, '/buildspace-launchpad?tab=projects&view=cards');
  await expect(page.locator('.bs-groups [data-project-card]').first().locator('.bs-mark')).toBeVisible();
  await open(page, '/buildspace-launchpad?tab=mentors&view=cards');
  await expect(page.locator('.bs-groups [data-mentor-card]').first().locator('.avatar')).toBeVisible();
  await open(page, '/buildspace-launchpad?tab=projects&view=board');
  await expect(page.locator('.bs-lane [data-project-card]').first()).toBeVisible();
  expect(await overflow(page)).toEqual([]);
  expect(errors).toEqual([]);
});

test('mentor cards count the builders on the projects they list, per group and filter (F-1)', async ({ page }) => {
  await devLogin(page, 'director');
  const data = await page.request.get('/api/buildspace-launchpad').then((r) => r.json());
  const builders = (names: string[]) =>
    data.people.filter((x: any) => data.projects.some((p: any) => names.includes(p.name) && p.id === x.projectId)).length;
  const check = async () => {
    const cards = page.locator('.bs-groups [data-mentor-card]');
    const n = await cards.count();
    expect(n).toBeGreaterThan(0);
    for (let i = 0; i < n; i++) {
      const card = cards.nth(i);
      const listed = await card.locator('[data-signal="projects"] button').allTextContents();
      const want = builders(listed);
      await expect(card.locator('[data-signal="builders"] dd')).toHaveText(want === 1 ? '1 builder' : `${want} builders`);
    }
  };
  await open(page, '/buildspace-launchpad?tab=mentors&view=cards&group=track');
  await check();
  // A mentor on projects in two tracks shows a smaller count in each group than in total.
  const split = data.mentors.find((m: any) => new Set(data.projects.filter((p: any) => p.mentorIds.includes(m.id)).map((p: any) => p.track)).size > 1);
  if (split) expect(await page.locator(`.bs-groups [data-mentor-card="${split.id}"]`).count()).toBeGreaterThan(1);
  await open(page, '/buildspace-launchpad?tab=mentors&view=cards&group=track&track=Software');
  await check();
});

test('closing drawers and saving forms leave no dead history entry (F-2)', async ({ page }) => {
  await devLogin(page, 'director');
  const backToEvents = async () => {
    await page.goBack();
    await expect(page).toHaveURL(/\/events$/);
    await expect(page.locator('dialog[open]')).toHaveCount(0);
  };
  const start = async (path: string) => {
    await page.goto('/events');
    await settle(page);
    await open(page, path);
  };
  const detail = page.locator('#buildspace-detail');
  const person = page.locator('#buildspace-person');

  // 1. A project opened from its card, closed with Esc: one Back leaves the page.
  await start('/buildspace-launchpad?tab=projects&view=cards');
  await page.locator('.bs-groups [data-project-card="bs-moss"]').getByRole('button', { name: 'Moss', exact: true }).click();
  await expect(detail).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(detail).not.toBeVisible();
  await expect(page).toHaveURL(/\/buildspace-launchpad\?tab=projects&view=cards$/);
  await backToEvents();

  // 2. Project → Edit project → Esc closes to the list; one Back leaves the page.
  await start('/buildspace-launchpad?tab=projects&view=cards');
  await page.locator('.bs-groups [data-project-card="bs-moss"]').getByRole('button', { name: 'Moss', exact: true }).click();
  await detail.getByRole('button', { name: 'Edit project' }).click();
  await expect(page).toHaveURL(/edit=project/);
  await page.keyboard.press('Escape');
  await expect(detail).not.toBeVisible();
  await expect(page).toHaveURL(/\/buildspace-launchpad\?tab=projects&view=cards$/);
  await backToEvents();

  // 3. Saving a builder edit opened from the profile closes both; one Back leaves the page.
  await start('/buildspace-launchpad');
  await page.locator('.bs-table').getByRole('button', { name: 'Alex Kim', exact: true }).click();
  await person.getByRole('button', { name: 'Edit builder', exact: true }).click();
  await detail.getByRole('button', { name: 'Save builder' }).click();
  await expect(page.getByText('Saved to Buildspace + Launchpad.')).toBeVisible();
  await expect(page.locator('dialog[open]')).toHaveCount(0);
  await expect(page).toHaveURL(/\/buildspace-launchpad$/);
  await backToEvents();

  // 4. Saving an edit opened straight from a card menu.
  await start('/buildspace-launchpad?view=cards');
  const alex = page.locator('.bs-groups [data-person-card="bs-person-01"]').first();
  await alex.hover();
  await alex.getByRole('button', { name: 'Actions for Alex Kim' }).click();
  await page.getByRole('menuitem', { name: 'Edit builder' }).click();
  await detail.getByRole('button', { name: 'Save builder' }).click();
  await expect(page.locator('dialog[open]')).toHaveCount(0);
  await expect(page).toHaveURL(/\/buildspace-launchpad\?view=cards$/);
  await backToEvents();

  // 5. Deep links have nothing to step back over: closing drops the parameters in place.
  await start('/buildspace-launchpad?project=bs-moss');
  await expect(detail).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page).toHaveURL(/\/buildspace-launchpad$/);
  await backToEvents();
  await start('/buildspace-launchpad?project=bs-moss');
  await detail.getByRole('button', { name: 'Edit project' }).click();
  await page.keyboard.press('Escape');
  await expect(page).toHaveURL(/\/buildspace-launchpad$/);
  await expect(page.locator('dialog[open]')).toHaveCount(0);
  await page.goBack();
  await expect(page).toHaveURL(/\/events$/);
});
