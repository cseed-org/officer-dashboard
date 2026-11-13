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
  await expect(tabs.getByRole('tab')).toHaveCount(3);
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
  await expect(lane('Building').getByRole('button', { name: 'Moss' })).toBeVisible();
  await lane('Building').getByRole('button', { name: 'Moss' }).click();
  await expect(page.locator('.drawer[open]')).toBeVisible();
  expect(await page.locator('.drawer[open]').evaluate((el) => getComputedStyle(el).animationName)).toBe('drawer-in');
  await page.getByRole('button', { name: 'Edit project' }).click();
  await page.getByLabel('Stage', { exact: true }).selectOption('Demo-ready');
  await clearFlips(page);
  await page.getByRole('button', { name: /Save/ }).click();
  await expect(page.locator('.drawer[open]')).toHaveCount(0);
  await expect(lane('Demo-ready').getByRole('button', { name: 'Moss' })).toBeVisible();
  await expect.poll(() => flips(page)).toContain('bs-moss');
  // Put it back for other tests.
  await lane('Demo-ready').getByRole('button', { name: 'Moss' }).click();
  await page.getByRole('button', { name: 'Edit project' }).click();
  await page.getByLabel('Stage', { exact: true }).selectOption('Building');
  await page.getByRole('button', { name: /Save/ }).click();
  await expect(lane('Building').getByRole('button', { name: 'Moss' })).toBeVisible();
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
});
