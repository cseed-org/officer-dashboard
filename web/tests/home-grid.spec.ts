import { test, expect, type Page } from '@playwright/test';
import { devLogin, overflow, THEMES, useTheme, watchErrors } from './helpers';
import { defaultWidgetLayout } from '../src/ui/widgets/registry';

/**
 * V0.4 Home block grid. Every test starts by writing a known layout, so a failed earlier run (or
 * another spec) can never leave the account in a state that breaks this one. The V0.3 tests
 * relied on cleanup in `finally`, which never ran once an earlier step used up the timeout.
 */
type P = { id: string; size: string };
const DEFAULT_IDS = defaultWidgetLayout.map((w) => w.id);

async function putLayout(page: Page, layout: P[]) {
  const r = await page.request.put('/api/me/prefs', { data: { widgetLayout: layout }, timeout: 15_000 });
  expect(r.ok(), `PUT /api/me/prefs → ${r.status()}`).toBe(true);
}
async function savedLayout(page: Page): Promise<P[]> {
  const r = await page.request.get('/api/me/prefs', { timeout: 15_000 });
  return (await r.json()).widgetLayout;
}
const grid = (page: Page) => page.getByRole('list', { name: 'Home widgets' });
const order = (page: Page) => grid(page).locator(':scope > li[data-widget]').evaluateAll((els) => els.map((e) => e.getAttribute('data-widget')));
const widget = (page: Page, id: string) => grid(page).locator(`:scope > li[data-widget="${id}"]`);
async function openHome(page: Page, layout: P[] = defaultWidgetLayout) {
  await page.request.put('/api/me/prefs', { data: { dashboardTutorialSeen: true } });
  await putLayout(page, layout);
  await page.goto('/');
  await expect(grid(page).locator(':scope > li[data-widget]').first()).toBeVisible();
}
async function done(page: Page) {
  const put = page.waitForResponse((r) => r.url().endsWith('/api/me/prefs') && r.request().method() === 'PUT');
  await page.getByRole('button', { name: 'Done' }).click();
  expect((await put).ok()).toBe(true);
  await expect(page.getByRole('button', { name: 'Edit home' })).toBeVisible();
}
/** Elements on Home drawn with a dashed border or outline. */
const dashed = (page: Page) =>
  page.locator('main').evaluate((main) =>
    [main, ...main.querySelectorAll('*')]
      .filter((e) => { const s = getComputedStyle(e); return [s.borderTopStyle, s.borderRightStyle, s.borderBottomStyle, s.borderLeftStyle, s.outlineStyle].includes('dashed'); })
      .map((e) => `${e.tagName.toLowerCase()}.${e.className}`),
  );

test.beforeEach(async ({ page }) => {
  await devLogin(page, 'director');
});
test.afterEach(async ({ page }) => {
  // Best effort: leave the shared dev account on the default layout for other specs.
  await page.request.put('/api/me/prefs', { data: { widgetLayout: defaultWidgetLayout }, timeout: 10_000 }).catch(() => {});
});

for (const theme of THEMES) {
  test(`Home is a block grid with no dashed borders, in view and edit mode · ${theme}`, async ({ page }, info) => {
    const errors = watchErrors(page);
    await useTheme(page, theme);
    await openHome(page);
    expect(await order(page)).toEqual(DEFAULT_IDS);
    // The old single Key figures row is now five 1×1 stat tiles.
    for (const id of ['kpi-upcoming', 'kpi-checkin', 'kpi-reflection', 'kpi-budget', 'kpi-pending']) await expect(widget(page, id)).toHaveAttribute('data-size', '1x1');
    await expect(widget(page, 'kpi-upcoming')).toContainText(/upcoming events/i);
    await expect(widget(page, 'web-analytics')).toContainText('Not connected');
    const cols = await grid(page).evaluate((g) => getComputedStyle(g).gridTemplateColumns.split(' ').length);
    expect(cols).toBe(info.project.name === 'phone' ? 2 : 4);
    expect(await dashed(page)).toEqual([]);

    await page.getByRole('button', { name: 'Edit home' }).click();
    await expect(page.getByRole('button', { name: /^Move Recent events/ })).toBeVisible();
    await expect(page.getByRole('button', { name: /Add widget/ }).last()).toBeVisible();
    expect(await dashed(page)).toEqual([]);
    expect(await overflow(page)).toEqual([]);
    await page.getByRole('button', { name: 'Cancel' }).click();
    expect(errors).toEqual([]);
  });
}

test('Links defaults to a compact card with accessible edit controls', async ({ page }) => {
  await openHome(page);
  const links = widget(page, 'links');
  await expect(links).toHaveAttribute('data-size', '2x1');
  for (const name of ['Club website', 'Notion', 'Google Drive']) {
    await expect(links.getByRole('link', { name: new RegExp(name) })).toBeVisible();
  }
  await page.getByRole('button', { name: 'Edit home' }).click();
  await expect(links.locator('.wc-head')).toBeVisible();
  await links.locator('.blk-handle').focus();
  await expect(links.locator('.blk-handle')).toBeFocused();
  const corner = (await links.locator('.blk-resize--both').boundingBox())!;
  expect(corner.width).toBeGreaterThanOrEqual(40);
  expect(corner.height).toBeGreaterThanOrEqual(40);
});

test('Resize, remove and add widgets; Done persists, Cancel discards', async ({ page }) => {
  await openHome(page);
  await page.getByRole('button', { name: 'Edit home' }).click();
  // Resize from a focused grip; only supported sizes are reached.
  await page.getByRole('button', { name: /^Move Upcoming events/ }).focus();
  await page.keyboard.press('Shift+ArrowRight');
  await expect(widget(page, 'kpi-upcoming')).toHaveAttribute('data-size', '2x1');
  await page.keyboard.press('Shift+ArrowDown');
  await expect(widget(page, 'kpi-upcoming')).toHaveAttribute('data-size', '2x1');
  await page.getByRole('button', { name: /^Move Recent events/ }).focus();
  await page.keyboard.press('Shift+ArrowRight');
  await page.getByRole('button', { name: 'Remove Web Analytics' }).click();
  await expect(widget(page, 'web-analytics')).toHaveCount(0);
  await page.getByRole('button', { name: 'Add widget' }).first().click();
  const gallery = page.getByRole('dialog', { name: 'Add a widget' });
  await gallery.getByRole('button', { name: 'Add Member signups' }).click();
  await expect(gallery.getByRole('button', { name: 'Add Member signups' })).toHaveCount(0);
  await expect(gallery.getByRole('button', { name: 'Add Web Analytics' })).toBeVisible();
  await gallery.getByRole('button', { name: 'Close' }).click();
  await expect(widget(page, 'member-signups')).toHaveAttribute('data-size', '1x1');
  await done(page);

  const expected = [...DEFAULT_IDS.filter((id) => id !== 'web-analytics'), 'member-signups'];
  await page.reload();
  await expect(widget(page, 'member-signups')).toContainText('No members yet');
  expect(await order(page)).toEqual(expected);
  await expect(widget(page, 'kpi-upcoming')).toHaveAttribute('data-size', '2x1');
  await expect(widget(page, 'recent-events')).toHaveAttribute('data-size', '4x2');
  const saved = await savedLayout(page);
  expect(saved.map((w) => w.id)).toEqual(expected);
  expect(saved.find((w) => w.id === 'recent-events')?.size).toBe('4x2');

  // Cancel throws edits away without saving.
  let puts = 0;
  page.on('request', (r) => { if (r.method() === 'PUT' && r.url().endsWith('/api/me/prefs')) puts++; });
  await page.getByRole('button', { name: 'Edit home' }).click();
  await page.getByRole('button', { name: 'Remove Budget vs actual' }).click();
  await page.getByRole('button', { name: 'Cancel' }).click();
  await expect(widget(page, 'budget')).toBeVisible();
  expect(puts).toBe(0);
});

test('Keyboard reordering on the grip, announced and saved', async ({ page }) => {
  await openHome(page);
  await page.getByRole('button', { name: 'Edit home' }).click();
  const grip = page.getByRole('button', { name: /^Move Recent events/ });
  await grip.focus();
  await page.keyboard.press('ArrowLeft');
  const i = DEFAULT_IDS.indexOf('recent-events');
  expect((await order(page)).indexOf('recent-events')).toBe(i - 1);
  await expect(grip).toBeFocused();
  await expect(page.locator('#blocks-live')).toHaveText(`Recent events, position ${i} of ${DEFAULT_IDS.length}`);
  await page.keyboard.press('Home');
  expect((await order(page))[0]).toBe('recent-events');
  await page.keyboard.press('ArrowDown');
  expect((await order(page))[1]).toBe('recent-events');
  await page.keyboard.press('End');
  expect((await order(page)).at(-1)).toBe('recent-events');
  await expect(grip).toBeFocused();
  await done(page);
  expect((await savedLayout(page)).at(-1)?.id).toBe('recent-events');
});

test('Pointer drag: the drop target is a soft fill, the move saves', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop', 'mouse drag runs once; touch uses the same pointer events');
  // Tall enough that the grip and the drop target are both on screen without scrolling.
  await page.setViewportSize({ width: 1440, height: 1600 });
  await openHome(page);
  await page.getByRole('button', { name: 'Edit home' }).click();
  const from = (await widget(page, 'kpi-pending').boundingBox())!;
  const to = (await widget(page, 'kpi-upcoming').boundingBox())!;
  await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2);
  await page.mouse.down();
  await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, { steps: 12 });
  const target = grid(page).locator('li.is-dragging');
  await expect(target).toHaveAttribute('data-widget', 'kpi-pending');
  const look = await target.evaluate((e) => { const s = getComputedStyle(e); return { border: s.borderTopStyle, bg: s.backgroundColor }; });
  expect(look.border).not.toBe('dashed');
  expect(look.bg).not.toBe('rgba(0, 0, 0, 0)');
  await expect(page.locator('.blk-float')).toContainText(/pending requests/i);
  await page.mouse.up();
  expect((await order(page)).indexOf('kpi-pending')).toBeLessThan(DEFAULT_IDS.indexOf('kpi-pending'));
  await expect(page.locator('.blk-float')).toHaveCount(0);
  await done(page);
  expect((await savedLayout(page)).map(w => w.id)).toEqual(await order(page));

  // Escape during a drag puts everything back.
  await page.getByRole('button', { name: 'Edit home' }).click();
  const before = await order(page);
  const g2 = (await widget(page, 'budget').boundingBox())!;
  const t2 = (await widget(page, 'kpi-checkin').boundingBox())!;
  await page.mouse.move(g2.x + 10, g2.y + 10);
  await page.mouse.down();
  await page.mouse.move(t2.x + t2.width / 2, t2.y + t2.height / 2, { steps: 12 });
  await page.keyboard.press('Escape');
  await page.mouse.up();
  expect(await order(page)).toEqual(before);
  await page.getByRole('button', { name: 'Cancel' }).click();
});

test('V0.3 S/M/L layouts migrate to blocks without losing anything', async ({ page }) => {
  await openHome(page, [
    { id: 'budget', size: 'S' },
    { id: 'kpis', size: 'L' },
    { id: 'web-analytics', size: 'L' },
    { id: 'activity', size: 'M' },
    { id: 'future-widget', size: 'M' },
  ]);
  expect(await order(page)).toEqual(['budget', 'kpi-upcoming', 'kpi-checkin', 'kpi-reflection', 'kpi-budget', 'kpi-pending', 'web-analytics', 'activity']);
  await expect(widget(page, 'budget')).toHaveAttribute('data-size', '2x2');
  await expect(widget(page, 'kpi-pending')).toHaveAttribute('data-size', '1x1');
  await expect(widget(page, 'web-analytics')).toHaveAttribute('data-size', '4x2');
  await expect(widget(page, 'activity')).toHaveAttribute('data-size', '2x2');
  await expect.poll(async () => (await savedLayout(page)).map((w) => w.size)).toEqual(['2x2', '1x1', '1x1', '1x1', '1x1', '1x1', '4x2', '2x2', '2x2']);
  let upgradeWrites = 0;
  page.on('request', (r) => { if (r.method() === 'PUT' && r.url().endsWith('/api/me/prefs')) upgradeWrites++; });
  await page.reload();
  await expect(page.getByRole('button', { name: 'Edit home' })).toBeEnabled();
  expect(upgradeWrites).toBe(0);
  // A widget from a feature that has not shipped stays saved and shows in edit mode.
  await page.getByRole('button', { name: 'Edit home' }).click();
  await expect(widget(page, 'future-widget')).toContainText('Unavailable widget (future-widget)');
  await done(page);
  const saved = await savedLayout(page);
  expect(saved.map((w) => w.size)).toEqual(['2x2', '1x1', '1x1', '1x1', '1x1', '1x1', '4x2', '2x2', '2x2']);
  expect(saved.at(-1)?.id).toBe('future-widget');
});

test('Phone: two columns, stat tiles pair up, large widgets take the full row', async ({ page }, info) => {
  test.skip(info.project.name !== 'phone', 'phone layout');
  await openHome(page);
  const g = (await grid(page).boundingBox())!;
  const a = (await widget(page, 'kpi-upcoming').boundingBox())!;
  const b = (await widget(page, 'kpi-checkin').boundingBox())!;
  expect(Math.abs(a.y - b.y)).toBeLessThan(2);
  expect(b.x).toBeGreaterThan(a.x + a.width);
  for (const id of ['my-action', 'recent-events', 'budget', 'activity']) {
    const r = (await widget(page, id).boundingBox())!;
    expect(Math.abs(r.width - g.width), id).toBeLessThan(2);
  }
  expect(await overflow(page)).toEqual([]);
  // Edit controls stay large enough to tap.
  await page.getByRole('button', { name: 'Edit home' }).click();
  const grip = (await page.getByRole('button', { name: /^Move Upcoming events/ }).boundingBox())!;
  expect(grip.height).toBeGreaterThanOrEqual(40);
  expect(await overflow(page)).toEqual([]);
  await page.getByRole('button', { name: 'Cancel' }).click();
});

test('Corner resize previews supported footprint, Escape restores, Done persists', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop', 'pointer geometry at desktop width');
  await openHome(page, [{ id: 'web-analytics', size: '1x1' }, { id: 'kpi-upcoming', size: '1x1' }]);
  await page.getByRole('button', { name: 'Edit home' }).click();
  const card = widget(page, 'web-analytics');
  const grip = card.locator('.blk-resize--both');
  const rect = (await card.boundingBox())!;
  const handle = (await grip.boundingBox())!;
  await page.mouse.move(handle.x + 20, handle.y + 20);
  await page.mouse.down();
  await page.mouse.move(handle.x + rect.width + 20, handle.y + rect.height + 20, { steps: 6 });
  await expect(card).toHaveAttribute('data-size', '2x2');
  await expect(card).toHaveClass(/is-resizing/);
  expect(await card.evaluate(el => getComputedStyle(el, '::after').content)).toContain('2 × 2');
  await page.keyboard.press('Escape');
  await page.mouse.up();
  await expect(card).toHaveAttribute('data-size', '1x1');
  await grip.focus();
  await page.keyboard.press('Shift+ArrowRight');
  await expect(card).toHaveAttribute('data-size', '2x2');
  await done(page);
  await page.reload();
  await expect(widget(page, 'web-analytics')).toHaveAttribute('data-size', '2x2');
});

test('Tutorial skips with Escape, stays seen on reload, and replays from Settings', async ({ page }) => {
  await page.request.put('/api/me/prefs', { data: { dashboardTutorialSeen: false } });
  await page.goto('/');
  const tour = page.getByRole('dialog', { name: 'Your home base' });
  await expect(tour).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(tour).not.toBeVisible();
  const prefs = await (await page.request.get('/api/me/prefs')).json();
  expect(prefs.dashboardTutorialSeen).toBe(true);
  await page.reload();
  await expect(page.locator('.dashboard-tour[open]')).toHaveCount(0);
  await page.goto('/settings');
  await page.getByRole('link', { name: 'Replay dashboard tutorial' }).click();
  await expect(page.getByRole('dialog', { name: 'Your home base' })).toBeVisible();
  await page.getByRole('button', { name: 'Next', exact: true }).click();
  await expect(page.getByRole('dialog', { name: 'Make it yours' })).toBeVisible();
  await page.getByRole('button', { name: 'Skip tour' }).click();
});


test('Coachmarks spotlight real controls and discard their temporary edit preview', async ({ page }) => {
  await openHome(page, [{ id: 'kpi-upcoming', size: '1x1' }]);
  const before = await savedLayout(page);
  await page.goto('/?tutorial=1');
  const tour = page.locator('.dashboard-tour');
  await expect(tour).toBeVisible();
  await expect(widget(page, 'links')).toBeVisible(); // temporary example, never saved
  await tour.getByRole('button', { name: 'Next', exact: true }).click();
  await expect(page.locator('[data-tour="edit"]')).toHaveClass(/is-tour-target/);
  await tour.getByRole('button', { name: 'Next', exact: true }).click();
  await expect(widget(page, 'links').locator('.blk-handle')).toHaveClass(/is-tour-target/);
  await tour.getByRole('button', { name: 'Next', exact: true }).click();
  const corner = widget(page, 'links').locator('.blk-resize--both');
  await expect(corner).toHaveClass(/is-tour-target/);
  const r = (await corner.boundingBox())!;
  const hole = await tour.locator('mask .tour-cutout').evaluate(e => ({ x: Number(e.getAttribute('x')), y: Number(e.getAttribute('y')), w: Number(e.getAttribute('width')), h: Number(e.getAttribute('height')) }));
  expect(hole.w).toBeGreaterThanOrEqual(r.width);
  expect(hole.h).toBeGreaterThanOrEqual(r.height);
  await tour.getByRole('button', { name: 'Next', exact: true }).click();
  await expect(page.locator('[data-tour="add"]')).toHaveClass(/is-tour-target/);
  await tour.getByRole('button', { name: 'Next', exact: true }).click();
  await expect(widget(page, 'links').locator('.club-links')).toHaveClass(/is-tour-target/);
  await tour.getByRole('button', { name: 'Finish', exact: true }).click();
  await expect(tour).not.toBeVisible();
  await expect(widget(page, 'links')).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Edit home' })).toBeVisible();
  expect(await savedLayout(page)).toEqual(before);
});

test('Leadership overrides club defaults; blank values restore the supplied URLs', async ({ page }) => {
  await devLogin(page, 'leadership');
  const settings = await (await page.request.get('/api/club/settings')).json();
  try {
    await page.request.put('/api/club/settings', { data: { ...settings, websiteUrl: 'https://example.org/club', notionUrl: '', driveUrl: '' } });
    await openHome(page);
    const links = widget(page, 'links');
    const site = links.getByRole('link', { name: /Club website/ });
    await expect(site).toHaveAttribute('href', 'https://example.org/club');
    await expect(site).toHaveAttribute('target', '_blank');
    await page.context().route('https://example.org/club', route => route.fulfill({ contentType: 'text/html', body: '<h1>Club destination</h1>' }));
    const opened = page.waitForEvent('popup');
    await site.click();
    const destination = await opened;
    await expect(destination).toHaveURL('https://example.org/club');
    await expect(destination.getByRole('heading', { name: 'Club destination' })).toBeVisible();
    await destination.close();
    await expect(links.getByRole('link', { name: /Add link/ })).toHaveCount(0);
    await expect(links.getByRole('link', { name: /Notion/ })).toHaveAttribute('href', 'https://app.notion.com/p/cseed/cseed-8c884a7f9dbe4bffa58718820e00cc10');
    await expect(links.getByRole('link', { name: /Google Drive/ })).toHaveAttribute('href', 'https://drive.google.com/drive/folders/0ABwfOblCw9EvUk9PVA?dmr=1&ec=wgc-drive-hero-goto');
  } finally { await page.request.put('/api/club/settings', { data: settings }); }
});


test('Default widgets fill a complete rectangle without an oversized actions card', async ({ page }) => {
  await openHome(page);
  await expect(widget(page, 'my-action')).toHaveAttribute('data-size', '2x1');
  await expect(widget(page, 'activity')).toHaveAttribute('data-size', '2x1');
  const box = (await grid(page).boundingBox())!;
  const rects = await grid(page).locator(':scope > li[data-widget]').evaluateAll(els => els.map(e => { const r = e.getBoundingClientRect(); return { x:r.x, y:r.y, right:r.right, bottom:r.bottom, area:r.width*r.height }; }));
  const leftBottom = Math.max(...rects.filter(r => Math.abs(r.x-box.x)<2).map(r=>r.bottom));
  const rightBottom = Math.max(...rects.filter(r => Math.abs(r.right-box.x-box.width)<2).map(r=>r.bottom));
  expect(Math.abs(leftBottom-rightBottom)).toBeLessThan(2);
  // Every row across the grid must be occupied, apart from the grid gap between cells.
  const gap = await grid(page).evaluate(el => parseFloat(getComputedStyle(el).columnGap));
  for(let y=box.y+2; y<box.y+box.height-2; y+=8) {
    const row = rects.filter(r=>r.y<=y && r.bottom>=y).sort((a,b)=>a.x-b.x);
    if (!row.length) continue;
    expect(Math.abs(row[0]!.x-box.x)).toBeLessThan(2);
    expect(Math.abs(row.at(-1)!.right-box.x-box.width)).toBeLessThan(2);
    for(let i=1;i<row.length;i++) expect(row[i]!.x-row[i-1]!.right).toBeLessThanOrEqual(gap+2);
  }
});

test('An onboarding landing starts the tour once, without forcing a replay', async ({ page }) => {
  await page.request.put('/api/me/prefs', { data: { dashboardTutorialSeen: false } });
  await page.goto('/?tutorial=onboarding');
  await expect(page.getByRole('dialog', { name: 'Your home base' })).toBeVisible();
  await page.getByRole('button', { name: 'Skip tour' }).click();
  await page.goto('/?tutorial=onboarding');
  await expect(page.getByRole('button', { name: 'Edit home' })).toBeEnabled();
  await expect(page.locator('.dashboard-tour[open]')).toHaveCount(0);
});


test('Calendar resize grips reach every supported footprint and persist', async ({ page }) => {
  await openHome(page, [{ id: 'calendar', size: '2x1' }, { id: 'links', size: '2x1' }]);
  const calendar = widget(page, 'calendar');
  await expect(calendar.locator('.cw--week')).toBeVisible();
  await expect(calendar.locator('.hcal > .hcal-day')).toHaveCount(7);
  await expect(calendar.getByRole('button')).toHaveCount(0);
  await page.getByRole('button', { name: 'Edit home' }).click();
  const grip = calendar.locator('.blk-resize--both');
  await grip.focus();
  await page.keyboard.press('Shift+ArrowDown');
  await expect(calendar).toHaveAttribute('data-size', '2x2');
  await expect(calendar.locator('.cw--week')).toBeVisible();
  await page.keyboard.press('Shift+ArrowRight');
  await expect(calendar).toHaveAttribute('data-size', '4x2');
  await expect(calendar.locator('.cw--month')).toBeVisible();
  await done(page);
  await page.reload();
  await expect(calendar).toHaveAttribute('data-size', '4x2');
  await page.getByRole('button', { name: 'Edit home' }).click();
  await calendar.locator('.blk-resize--both').focus();
  await page.keyboard.press('Shift+ArrowLeft');
  await expect(calendar).toHaveAttribute('data-size', '2x2');
  await done(page);
  await page.getByRole('button', { name: 'Edit home' }).click();
  await calendar.locator('.blk-resize--both').focus();
  await page.keyboard.press('Shift+ArrowUp');
  await expect(calendar).toHaveAttribute('data-size', '2x1');
  await done(page);
  expect((await savedLayout(page)).find(w => w.id === 'calendar')?.size).toBe('2x1');
});

test('Calendar corner drag previews its large footprint and Escape restores', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop', 'pointer geometry at desktop width');
  await openHome(page, [{ id: 'calendar', size: '2x1' }]);
  await page.getByRole('button', { name: 'Edit home' }).click();
  const calendar = widget(page, 'calendar');
  const box = (await calendar.boundingBox())!;
  const grip = (await calendar.locator('.blk-resize--both').boundingBox())!;
  await page.mouse.move(grip.x + grip.width / 2, grip.y + grip.height / 2);
  await page.mouse.down();
  await page.mouse.move(grip.x + grip.width / 2 + box.width, grip.y + grip.height / 2 + box.height, { steps: 8 });
  await expect(calendar).toHaveAttribute('data-size', '4x2');
  await page.keyboard.press('Escape');
  await page.mouse.up();
  await expect(calendar).toHaveAttribute('data-size', '2x1');
  await page.getByRole('button', { name: 'Cancel' }).click();
});

test('Resize commits only at snap boundaries and pushes neighbours down without reordering', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop', 'desktop snap geometry');
  await page.emulateMedia({ reducedMotion: 'reduce' });
  const initial = [
    { id: 'web-analytics', size: '1x1' },
    { id: 'kpi-upcoming', size: '1x1' },
    { id: 'kpi-checkin', size: '1x1' },
    { id: 'kpi-reflection', size: '1x1' },
  ];
  await openHome(page, initial);
  await page.getByRole('button', { name: 'Edit home' }).click();
  const card = widget(page, 'web-analytics');
  const neighbour = widget(page, 'kpi-upcoming');
  const before = (await neighbour.boundingBox())!;
  const cardBox = (await card.boundingBox())!;
  const grip = (await card.locator('.blk-resize--both').boundingBox())!;
  const x = grip.x + grip.width / 2, y = grip.y + grip.height / 2;
  const revision = () => grid(page).getAttribute('data-layout-revision');
  const originalRevision = await revision();
  await page.mouse.move(x, y);
  await page.mouse.down();
  for (let delta = 1; delta <= 12; delta++) await page.mouse.move(x + delta, y + delta);
  expect(await revision()).toBe(originalRevision);
  await page.mouse.move(x + cardBox.width, y + cardBox.height);
  await expect(card).toHaveAttribute('data-size', '2x2');
  expect(Number(await revision())).toBe(Number(originalRevision) + 1);
  const snappedRevision = await revision();
  for (let delta = 1; delta <= 12; delta++) await page.mouse.move(x + cardBox.width + delta, y + cardBox.height + delta);
  expect(await revision()).toBe(snappedRevision);
  const after = (await neighbour.boundingBox())!;
  expect(Math.abs(after.x - before.x)).toBeLessThan(2);
  expect(after.y).toBeGreaterThan(before.y + cardBox.height);
  expect(await order(page)).toEqual(initial.map(p => p.id));
  // Preview never writes preferences. Escape restores geometry as well as the size.
  expect(await savedLayout(page)).toEqual(initial);
  await page.keyboard.press('Escape');
  await page.mouse.up();
  await expect(card).toHaveAttribute('data-size', '1x1');
  const restored = (await neighbour.boundingBox())!;
  expect(Math.abs(restored.y - before.y)).toBeLessThan(2);
  expect(await card.evaluate(el => el.getAnimations().length)).toBe(0);
});

test('Whole-tile drag has a threshold, an inert static copy, and native cursors', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop', 'mouse cursor and drag geometry');
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await openHome(page, [{ id: 'calendar', size: '2x1' }, { id: 'links', size: '2x1' }]);
  await page.getByRole('button', { name: 'Edit home' }).click();
  const card = widget(page, 'calendar');
  const box = (await card.boundingBox())!;
  const x = box.x + box.width / 2, y = box.y + box.height / 2;
  expect(await card.evaluate(el => getComputedStyle(el).cursor)).toBe('grab');
  let feedRequests = 0;
  page.on('request', request => { if (request.url().includes('/api/calendar/feed')) feedRequests++; });
  await page.mouse.move(x, y);
  await page.mouse.down();
  await page.mouse.move(x + 3, y);
  await expect(page.locator('.blk-float')).toHaveCount(0);
  await page.mouse.move(x + 5, y);
  const clone = page.locator('.blk-float');
  await expect(clone).toBeVisible();
  await expect(clone.locator('[id], .blk-tools')).toHaveCount(0);
  expect(await clone.evaluate(el => (el as HTMLElement).inert)).toBe(true);
  expect(await clone.evaluate(el => getComputedStyle(el).transform)).toBe('none');
  expect(await page.locator('html').evaluate(el => getComputedStyle(el).cursor)).toBe('grabbing');
  expect(feedRequests).toBe(0);
  await page.keyboard.press('Escape');
  await page.mouse.up();
  await expect(clone).toHaveCount(0);
  await expect(page.locator('html')).not.toHaveClass(/is-dragging/);
  expect(await order(page)).toEqual(['calendar', 'links']);
  // A click below the threshold keeps the keyboard control's native focus behavior.
  await card.locator('.blk-handle').click();
  await expect(card.locator('.blk-handle')).toBeFocused();
  await expect(clone).toHaveCount(0);
});

test('Edit sway runs without replaying the entrance after Done', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  await openHome(page, [{ id: 'kpi-upcoming', size: '1x1' }]);
  await page.getByRole('button', { name: 'Edit home' }).click();
  const body = widget(page, 'kpi-upcoming').locator('.blk-body');
  expect(await body.evaluate(el => getComputedStyle(el).animationName)).toBe('blk-sway');
  await done(page);
  expect(await body.evaluate(el => getComputedStyle(el).animationName)).toBe('none');
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.getByRole('button', { name: 'Edit home' }).click();
  expect(await body.evaluate(el => getComputedStyle(el).animationName)).toBe('none');
});

test('Explicit edit positions adapt when Home changes from four columns to two', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop', 'resize one browser across breakpoints');
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await openHome(page, [
    { id: 'kpi-upcoming', size: '1x1' }, { id: 'kpi-checkin', size: '1x1' },
    { id: 'kpi-reflection', size: '1x1' }, { id: 'kpi-budget', size: '1x1' },
  ]);
  await page.getByRole('button', { name: 'Edit home' }).click();
  await page.setViewportSize({ width: 390, height: 844 });
  await expect.poll(() => grid(page).evaluate(el => getComputedStyle(el).gridTemplateColumns.split(' ').length)).toBe(2);
  await expect.poll(() => widget(page, 'kpi-reflection').evaluate(el => getComputedStyle(el).gridColumnStart)).toBe('1');
  const a = (await widget(page, 'kpi-upcoming').boundingBox())!;
  const b = (await widget(page, 'kpi-checkin').boundingBox())!;
  const c = (await widget(page, 'kpi-reflection').boundingBox())!;
  expect(Math.abs(a.y - b.y)).toBeLessThan(2);
  expect(c.y).toBeGreaterThan(a.y + a.height);
  expect(Math.abs(c.x - a.x)).toBeLessThan(2);
  expect(await overflow(page)).toEqual([]);
});


test('A whole-card click without dragging navigates in view mode', async ({ page }) => {
  await openHome(page, [{ id: 'calendar', size: '2x1' }]);
  const card = widget(page, 'calendar');
  await expect(card.locator('.cw--week')).toBeVisible();
  const box = (await card.boundingBox())!;
  // Padding belongs to the stretched card link, outside the individual event chips.
  await page.mouse.click(box.x + box.width / 2, box.y + box.height - 8);
  await expect(page).toHaveURL(/\/events(?:[?#]|$)/);
  await expect(page.locator('.blk-float')).toHaveCount(0);
});
