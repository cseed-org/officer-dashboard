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
type Cell = { x: number; y: number; w: number; h: number };
/** A widget's explicit cell: 0-based column and row, and its span. */
const cellOf = (page: Page, id: string): Promise<Cell> => widget(page, id).evaluate((e) => {
  const s = getComputedStyle(e);
  return { x: +s.gridColumnStart - 1, y: +s.gridRowStart - 1, w: +s.gridColumnEnd.replace('span ', ''), h: +s.gridRowEnd.replace('span ', '') };
});
/** Every widget's cell, keyed by id. */
const cells = (page: Page) => grid(page).locator(':scope > li[data-widget]').evaluateAll((els) => Object.fromEntries(els.map((e) => {
  const s = getComputedStyle(e);
  return [e.getAttribute('data-widget')!, { x: +s.gridColumnStart - 1, y: +s.gridRowStart - 1, w: +s.gridColumnEnd.replace('span ', ''), h: +s.gridRowEnd.replace('span ', '') }];
})) as Record<string, Cell>);
/** Pairs of widgets whose cells overlap (always empty for a valid layout). */
const overlapping = (all: Record<string, Cell>) => {
  const e = Object.entries(all); const out: string[] = [];
  for (let i = 0; i < e.length; i++) for (let j = i + 1; j < e.length; j++) {
    const [a, b] = [e[i]![1], e[j]![1]];
    if (a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h) out.push(`${e[i]![0]}/${e[j]![0]}`);
  }
  return out;
};
/** The viewport point at the centre of grid cell (x, y), from the grid's own tracks; y may be one row past the last. */
const cellPoint = (page: Page, x: number, y: number) => grid(page).evaluate((g, [x, y]) => {
  const s = getComputedStyle(g); const r = g.getBoundingClientRect();
  const cols = s.gridTemplateColumns.split(' ').map(parseFloat); const rows = s.gridTemplateRows.split(' ').map(parseFloat);
  const at = (ts: number[], gap: number, i: number) => { let p = 0; for (let k = 0; k < i; k++) p += (ts[k] ?? ts.at(-1)!) + gap; return p + (ts[i] ?? ts.at(-1)!) / 2; };
  return { x: r.left + at(cols, parseFloat(s.columnGap), x), y: r.top + at(rows, parseFloat(s.rowGap), y) };
}, [x, y] as const);
/** Press on a widget; the returned function moves the pointer to the centre of a grid cell. */
async function dragOver(page: Page, id: string) {
  const box = (await widget(page, id).boundingBox())!;
  await page.mouse.move(box.x + 24, box.y + box.height / 2);
  await page.mouse.down();
  return async (x: number, y: number, steps = 6) => { const p = await cellPoint(page, x, y); await page.mouse.move(p.x, p.y, { steps }); };
}
const hint = (page: Page) => grid(page).locator('li.is-dragging.is-placeholder');
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
    await expect(page.locator('.blocks-bar')).not.toContainText('Your layout follows you across devices.');
    await expect(page.locator('.blocks-bar:not(.is-editing) .blocks-bar-text')).toHaveCount(0);
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
  // Dense packing may let a later widget fill a gap first, so the saved order is the reading order
  // Home showed, and a reload packs to the same picture.
  const expected = await order(page);
  expect([...expected].sort()).toEqual([...DEFAULT_IDS.filter((id) => id !== 'web-analytics'), 'member-signups'].sort());
  const shown = await cells(page);
  await done(page);
  await page.reload();
  await expect(widget(page, 'member-signups')).toContainText('No members yet');
  expect(await order(page)).toEqual(expected);
  expect(await cells(page)).toEqual(shown);
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
  // One step later; dense packing may place it further when it can't fit the next spot.
  const later = (await order(page)).indexOf('recent-events');
  expect(later).toBeGreaterThan(0);
  await expect(page.locator('#blocks-live')).toHaveText(`Recent events, position ${later + 1} of ${DEFAULT_IDS.length}`);
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
  const target = hint(page);
  await expect(target).toHaveCount(1);
  await expect(widget(page, 'kpi-pending')).toHaveClass(/is-lifted/);
  const look = await target.evaluate((e) => { const s = getComputedStyle(e); return { border: s.borderTopStyle, bg: s.backgroundColor }; });
  expect(look.border).not.toBe('dashed');
  expect(look.bg).not.toBe('rgba(0, 0, 0, 0)');
  await expect(widget(page, 'kpi-pending')).toContainText(/pending requests/i);
  await page.mouse.up();
  expect((await order(page)).indexOf('kpi-pending')).toBeLessThan(DEFAULT_IDS.indexOf('kpi-pending'));
  await expect(grid(page).locator('.is-lifted')).toHaveCount(0);
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
  // S is the smallest allowed size: budget gained 1×1 in V0.7.
  await expect(widget(page, 'budget')).toHaveAttribute('data-size', '1x1');
  await expect(widget(page, 'kpi-pending')).toHaveAttribute('data-size', '1x1');
  await expect(widget(page, 'web-analytics')).toHaveAttribute('data-size', '4x2');
  await expect(widget(page, 'activity')).toHaveAttribute('data-size', '2x2');
  await expect.poll(async () => (await savedLayout(page)).map((w) => w.size)).toEqual(['1x1', '1x1', '1x1', '1x1', '1x1', '1x1', '4x2', '2x2', '2x2']);
  let upgradeWrites = 0;
  page.on('request', (r) => { if (r.method() === 'PUT' && r.url().endsWith('/api/me/prefs')) upgradeWrites++; });
  await page.reload();
  await expect(page.getByRole('button', { name: 'Edit home' })).toBeEnabled();
  expect(upgradeWrites).toBe(0);
  // A widget from a feature that has not shipped stays saved and shows in edit mode.
  await page.getByRole('button', { name: 'Edit home' }).click();
  await expect(widget(page, 'future-widget')).toContainText('Unavailable widget (future-widget)');
  const arranged = await order(page);
  await done(page);
  const saved = await savedLayout(page);
  // Saved in the reading order edit mode showed (a 2×2 can fill a gap the 4×2 can't).
  expect(saved.map((w) => w.id)).toEqual(arranged);
  expect(saved.map((w) => w.size).sort()).toEqual(['1x1', '1x1', '1x1', '1x1', '1x1', '1x1', '2x2', '2x2', '4x2']);
  expect(saved).toContainEqual({ id: 'future-widget', size: '2x2' });
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
  const tour = page.getByRole('dialog', { name: 'This is Home' });
  await expect(tour).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(tour).not.toBeVisible();
  const prefs = await (await page.request.get('/api/me/prefs')).json();
  expect(prefs.dashboardTutorialSeen).toBe(true);
  await page.reload();
  await expect(page.locator('[data-home-tour][open]')).toHaveCount(0);
  await page.goto('/settings');
  await page.getByRole('link', { name: /Replay dashboard (tutorial|tour)/ }).click();
  await expect(page.getByRole('dialog', { name: 'This is Home' })).toBeVisible();
  await page.getByRole('button', { name: 'Next', exact: true }).click();
  await expect(page.getByRole('dialog', { name: 'Your widgets' })).toBeVisible();
  await page.getByRole('button', { name: 'Skip tour' }).click();
});


test('Coachmarks spotlight real controls and discard their temporary edit preview', async ({ page }) => {
  await openHome(page, [{ id: 'kpi-upcoming', size: '1x1' }]);
  const before = await savedLayout(page);
  await page.goto('/?tutorial=1');
  const tour = page.locator('[data-home-tour]');
  await expect(tour).toBeVisible();
  const nextStop = () => tour.getByRole('button', { name: 'Next', exact: true }).click();
  await nextStop(); // widgets
  await nextStop(); // edit
  await expect(page.locator('[data-tour="edit"]')).toHaveClass(/is-tour-target/);
  await nextStop();
  await expect(widget(page, 'links')).toBeVisible(); // temporary example, never saved
  await expect(page.locator('[data-tour="drag"].is-tour-target')).toHaveCount(1);
  await nextStop();
  const corner = page.locator('[data-tour="resize"].is-tour-target');
  await expect(corner).toHaveCount(1);
  const r = (await corner.boundingBox())!;
  const hole = await tour.locator('mask rect').nth(1).evaluate(e => ({ x: Number(e.getAttribute('x')), y: Number(e.getAttribute('y')), w: Number(e.getAttribute('width')), h: Number(e.getAttribute('height')) }));
  expect(hole.w).toBeGreaterThanOrEqual(r.width);
  expect(hole.h).toBeGreaterThanOrEqual(r.height);
  await nextStop();
  await expect(page.locator('[data-tour="add"]')).toHaveClass(/is-tour-target/);
  // sidebar, 'Resize the sidebar' (width >= 768 only), search
  for (let i = 0; i < ((page.viewportSize()?.width ?? 1440) >= 768 ? 3 : 2); i++) await nextStop();
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
  await expect(page.getByRole('dialog', { name: 'This is Home' })).toBeVisible();
  await page.getByRole('button', { name: 'Skip tour' }).click();
  await page.goto('/?tutorial=onboarding');
  await expect(page.getByRole('button', { name: 'Edit home' })).toBeEnabled();
  await expect(page.locator('[data-home-tour][open]')).toHaveCount(0);
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

test('Resize commits only at snap boundaries; neighbours pack around the resized widget', async ({ page }, info) => {
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
  // The resized widget keeps its top-left; the stat tiles pack into the free cells beside it.
  expect(await cellOf(page, 'web-analytics')).toEqual({ x: 0, y: 0, w: 2, h: 2 });
  expect(await cellOf(page, 'kpi-upcoming')).toEqual({ x: 2, y: 0, w: 1, h: 1 });
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

test('Whole-tile drag has a threshold, a lifted live card, and native cursors', async ({ page }, info) => {
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
  await expect(grid(page).locator('.is-lifted')).toHaveCount(0);
  await page.mouse.move(x + 5, y);
  const clone = grid(page).locator('.is-lifted');
  await expect(clone).toBeVisible();
  expect(await clone.evaluate(el => getComputedStyle(el).position)).toBe('fixed');
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

/** The Add tile follows every widget in reading order: no widget ends below its row or sits after it in that row. */
async function expectAddLast(page: Page) {
  await expect.poll(() => grid(page).evaluate((ul) => {
    const add = ul.querySelector(':scope > li.blk-add-cell')!.getBoundingClientRect();
    const late = [...ul.querySelectorAll<HTMLElement>(':scope > li[data-widget]')].filter((li) => {
      const r = li.getBoundingClientRect();
      const sameRow = r.bottom > add.top + 2 && r.top < add.bottom - 2;
      return r.bottom > add.bottom + 2 || (sameRow && r.left > add.left);
    }).map((li) => li.dataset.widget);
    return late;
  })).toEqual([]);
}

test('Add widget stays after the last widget through add, remove, resize and move', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  // A 1×1 then a full-width calendar leaves a hole beside the first tile at four and two columns.
  await openHome(page, [{ id: 'kpi-upcoming', size: '1x1' }, { id: 'calendar', size: '4x2' }, { id: 'kpi-checkin', size: '1x1' }]);
  await page.getByRole('button', { name: 'Edit home' }).click();
  await expectAddLast(page);
  await page.getByRole('button', { name: 'Remove Avg check-in rate' }).click();
  await expectAddLast(page);
  await page.getByRole('button', { name: /^Move Upcoming events/ }).focus();
  await page.keyboard.press('Shift+ArrowRight');
  await expect(widget(page, 'kpi-upcoming')).toHaveAttribute('data-size', '2x1');
  await expectAddLast(page);
  await page.getByRole('button', { name: 'Add widget' }).first().click();
  const gallery = page.getByRole('dialog', { name: 'Add a widget' });
  await gallery.getByRole('button', { name: 'Add Pending requests' }).click();
  await gallery.getByRole('button', { name: 'Close' }).click();
  await expectAddLast(page);
  // Keyboard move: the calendar to the end, then back to the front.
  await page.getByRole('button', { name: /^Move Calendar/ }).focus();
  await page.keyboard.press('End');
  await expectAddLast(page);
  await page.keyboard.press('Home');
  await expectAddLast(page);
});

test('A new widget starts at its smallest size in the highest free spot it fits', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  // A stat tile, then a full-width calendar: a hole beside the tile at four and at two columns.
  await openHome(page, [{ id: 'kpi-upcoming', size: '1x1' }, { id: 'calendar', size: '4x2' }]);
  await page.getByRole('button', { name: 'Edit home' }).click();
  const before = { kpi: (await widget(page, 'kpi-upcoming').boundingBox())!, cal: (await widget(page, 'calendar').boundingBox())! };
  await page.getByRole('button', { name: 'Add widget' }).first().click();
  const gallery = page.getByRole('dialog', { name: 'Add a widget' });
  // Budget's default is 2×2, but a new widget starts at its smallest size.
  await gallery.getByRole('button', { name: 'Add Budget vs actual' }).click();
  await gallery.getByRole('button', { name: 'Close' }).click();
  const budget = widget(page, 'budget');
  await expect(budget).toHaveAttribute('data-size', '1x1');
  const inGap = async () => {
    const kpi = (await widget(page, 'kpi-upcoming').boundingBox())!;
    const b = (await budget.boundingBox())!;
    return Math.abs(b.y - kpi.y) < 2 && b.x > kpi.x + kpi.width;
  };
  await expect.poll(inGap).toBe(true);
  expect((await widget(page, 'kpi-upcoming').boundingBox())!).toEqual(before.kpi);
  expect((await widget(page, 'calendar').boundingBox())!).toEqual(before.cal);
  await expectAddLast(page);
  await done(page);
  // Saved as {id,size} in reading order (the new widget sits in the gap); a reload packs it into the same spot.
  expect(await savedLayout(page)).toEqual([{ id: 'kpi-upcoming', size: '1x1' }, { id: 'budget', size: '1x1' }, { id: 'calendar', size: '4x2' }]);
  await page.reload();
  await expect(budget).toHaveAttribute('data-size', '1x1');
  await expect.poll(inGap).toBe(true);
  // A larger widget takes the highest spot it fits: a 2×2 fits beside the stat tiles at four
  // columns (the full-width calendar can't), so dense packing puts it there and the calendar
  // packs below it; at two columns it goes below everything. A reload packs the same.
  await page.getByRole('button', { name: 'Edit home' }).click();
  await page.getByRole('button', { name: 'Add widget' }).first().click();
  await gallery.getByRole('button', { name: 'Add Recent events' }).click();
  await gallery.getByRole('button', { name: 'Close' }).click();
  await expect(widget(page, 'recent-events')).toHaveAttribute('data-size', '2x2');
  const recent = await cellOf(page, 'recent-events');
  const cal = await cellOf(page, 'calendar');
  const cols = await grid(page).evaluate((g) => getComputedStyle(g).gridTemplateColumns.split(' ').length);
  // Phone (two columns) and tablet (a 2×2 is drawn four wide) have no such spot.
  if (recent.w + 2 > cols) expect(recent.y).toBeGreaterThanOrEqual(cal.y + cal.h);
  else { expect(recent).toEqual({ x: 2, y: 0, w: 2, h: 2 }); expect(cal.y).toBe(2); }
  expect(overlapping(await cells(page))).toEqual([]);
  await expectAddLast(page);
});

test('wide Home adds columns while blocks keep their size and widgets fit the screen', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop', 'one browser covers all widths');
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await openHome(page);
  const measurements: { viewport: number; columns: number; block: number }[] = [];
  for (const [width, columns] of [[1280, 4], [1920, 6], [2560, 9]]) {
    await page.setViewportSize({ width: width!, height: 1200 });
    await expect.poll(() => grid(page).evaluate(el => getComputedStyle(el).gridTemplateColumns.split(' ').length)).toBe(columns);
    const block = (await widget(page, 'kpi-upcoming').boundingBox())!.width;
    measurements.push({ viewport: width!, columns: columns!, block });
    expect(await overflow(page)).toEqual([]);
    expect(Math.abs(block / measurements[0]!.block - 1)).toBeLessThan(.15);
  }
  console.log('Home grid measurements:', JSON.stringify(measurements));
  await info.attach('wide-home-measurements', { body: JSON.stringify(measurements), contentType: 'application/json' });
  // Narrow/intermediate widths and the ten-column cap must also avoid document overflow.
  for (const width of [320, 390, 599, 600, 767, 768, 960, 1099, 1440, 1600, 2200, 2880, 3840]) {
    await page.setViewportSize({ width, height: 1200 });
    await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await expect.poll(() => grid(page).evaluate(el => [...el.querySelectorAll(':scope > li')].every(li => li.getBoundingClientRect().right <= el.getBoundingClientRect().right + 1))).toBe(true);
  }
});

for (const width of [1280, 1600, 1920, 2200, 2400, 2560, 2880]) {
  test(`editing and placement work at ${width}px`, async ({ page }, info) => {
    test.skip(info.project.name !== 'desktop', 'exercise each desktop column count');
    await page.setViewportSize({ width, height: 1200 });
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await openHome(page, [{ id: 'kpi-upcoming', size: '1x1' }, { id: 'calendar', size: '4x2' }, { id: 'kpi-checkin', size: '1x1' }]);
    await page.getByRole('button', { name: 'Edit home' }).click();
    await expect(widget(page, 'calendar')).toHaveCSS('grid-column-end', 'span 4');
    await expectAddLast(page);
    const before = await widget(page, 'calendar').boundingBox();
    await page.getByRole('button', { name: 'Add widget', exact: true }).click();
    const gallery = page.getByRole('dialog', { name: 'Add a widget' });
    await gallery.getByRole('button', { name: 'Add Budget vs actual' }).click();
    await gallery.getByRole('button', { name: 'Close' }).click();
    await expect(widget(page, 'budget')).toHaveAttribute('data-size', '1x1');
    expect(await widget(page, 'calendar').boundingBox()).toEqual(before);
    await expectAddLast(page);
    // Resize pushes neighbours without overlap; move works with the resulting positions.
    await page.getByRole('button', { name: /^Move Upcoming events/ }).focus();
    await page.keyboard.press('Shift+ArrowRight');
    await expect(widget(page, 'kpi-upcoming')).toHaveAttribute('data-size', '2x1');
    await expectAddLast(page);
    const source = widget(page, 'calendar').locator('.blk-handle');
    const target = widget(page, 'kpi-upcoming').locator('.blk-handle');
    await source.dragTo(target);
    // The calendar lands on the hovered tile's cell; the rest pack around it without overlap.
    await expect.poll(() => cellOf(page, 'calendar')).toEqual({ x: 0, y: 0, w: 4, h: 2 });
    expect(overlapping(await cells(page))).toEqual([]);
    expect((await order(page))[0]).toBe('calendar');
    await expectAddLast(page);
    await page.getByRole('button', { name: 'Remove Avg check-in rate' }).click();
    await expectAddLast(page);
    const arranged = await order(page);
    await done(page);
    const sizes: Record<string, string> = { calendar: '4x2', 'kpi-upcoming': '2x1', budget: '1x1' };
    expect(await savedLayout(page)).toEqual(arranged.map((id) => ({ id: id!, size: sizes[id!] })));
    await page.reload();
    await expect(widget(page, 'calendar')).toHaveCSS('grid-column-end', 'span 4');
    expect(await overflow(page)).toEqual([]);
    await page.getByRole('button', { name: 'Edit home' }).click();
    // Explicit coordinates and the cancel snapshot must adapt across 9 → 6 → 4 → 2 columns.
    for (const nextWidth of [2560, 1920, 1280, 390, width]) {
      await page.setViewportSize({ width: nextWidth, height: 1200 });
      await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      await expectAddLast(page);
    }
    await page.getByRole('button', { name: 'Cancel' }).click();
    expect(await overflow(page)).toEqual([]);
  });
}

// Dense packing: what the drag shows is where the drop lands, and a reload packs the same picture.
for (const [width, cols] of [[1280, 4], [1920, 6]] as const) {
  test(`the drop hint is the final position after packing · ${cols} columns`, async ({ page }, info) => {
    test.skip(info.project.name !== 'desktop', 'mouse drag geometry at desktop column counts');
    await page.setViewportSize({ width, height: 1400 });
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await openHome(page, [{ id: 'kpi-upcoming', size: '1x1' }, { id: 'kpi-checkin', size: '1x1' }, { id: 'calendar', size: '4x2' }, { id: 'my-action', size: '2x1' }]);
    await page.getByRole('button', { name: 'Edit home' }).click();
    await expect.poll(() => grid(page).evaluate((g) => getComputedStyle(g).gridTemplateColumns.split(' ').length)).toBe(cols);

    // A spot the widget can keep: it lands exactly where the pointer is, and the rest pack around it.
    let to = await dragOver(page, 'kpi-checkin');
    await to(0, 0);
    await expect.poll(() => cellOf(page, 'kpi-checkin')).toEqual({ x: 0, y: 0, w: 1, h: 1 });
    await expect(widget(page, 'kpi-checkin')).toHaveClass(/is-lifted/);
    expect(await hint(page).evaluate((e) => getComputedStyle(e).backgroundColor)).not.toBe('rgba(0, 0, 0, 0)');
    await page.mouse.up();
    expect(await cellOf(page, 'kpi-checkin')).toEqual({ x: 0, y: 0, w: 1, h: 1 });

    // Empty space, the last tile's row and the row past it: wherever the hint settles is where
    // the drop lands (a 1×1 below a gap it fits rises into that gap), with no overlap.
    const bottom = Math.max(...Object.values(await cells(page)).map((c) => c.y + c.h));
    for (const [id, x, y] of [['kpi-upcoming', cols - 1, 0], ['kpi-upcoming', 1, bottom], ['my-action', 0, bottom - 1], ['calendar', 0, bottom], ['my-action', cols - 2, 0]] as const) {
      to = await dragOver(page, id);
      await to(x, y);
      await page.waitForTimeout(120);
      const shown = await cellOf(page, id);
      const all = await cells(page);
      await page.mouse.up();
      await expect(hint(page)).toHaveCount(0);
      expect(await cellOf(page, id), `${id} dropped over (${x},${y})`).toEqual(shown);
      expect(await cells(page)).toEqual(all);
      expect(overlapping(all)).toEqual([]);
      await expectAddLast(page);
    }

    // A column the 4×2 can't start in shows nothing new, and dropping there keeps the last target.
    const cal = await cellOf(page, 'calendar');
    to = await dragOver(page, 'calendar');
    await to(0, cal.y);
    await expect.poll(() => cellOf(page, 'calendar')).toEqual(cal);
    await to(cols - 1, cal.y + 1, 1); // one jump: the columns in between would be valid targets
    await page.waitForTimeout(150);
    expect(await cellOf(page, 'calendar')).toEqual(cal);
    await page.mouse.up();
    expect(await cellOf(page, 'calendar')).toEqual(cal);

    const shown = await cells(page);
    const arranged = await order(page);
    await done(page);
    expect((await savedLayout(page)).map((w) => w.id)).toEqual(arranged);
    await page.reload();
    await expect(grid(page).locator(':scope > li[data-widget]').first()).toBeVisible();
    expect(await cells(page)).toEqual(shown);
  });
}

test('Removing a widget lets a 1×1 further down fill the gap near the top', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  // Links, two stat tiles, the calendar, then a stat tile below it (four columns; phone: two).
  await openHome(page, [{ id: 'links', size: '2x1' }, { id: 'kpi-upcoming', size: '1x1' }, { id: 'kpi-checkin', size: '1x1' }, { id: 'calendar', size: '4x2' }, { id: 'kpi-reflection', size: '1x1' }]);
  await page.getByRole('button', { name: 'Edit home' }).click();
  const gap = await cellOf(page, 'kpi-checkin');
  const cal = await cellOf(page, 'calendar');
  expect((await cellOf(page, 'kpi-reflection')).y).toBeGreaterThanOrEqual(cal.y + cal.h);
  await page.getByRole('button', { name: 'Remove Avg check-in rate' }).click();
  // The lower stat tile overtakes the calendar into the freed cell; the calendar stays put.
  await expect.poll(() => cellOf(page, 'kpi-reflection')).toEqual(gap);
  expect(await cellOf(page, 'calendar')).toEqual(cal);
  expect(await order(page)).toEqual(['links', 'kpi-upcoming', 'kpi-reflection', 'calendar']);
  await expectAddLast(page);
  const shown = await cells(page);
  await done(page);
  await page.reload();
  await expect(widget(page, 'kpi-reflection')).toBeVisible();
  expect(await cells(page)).toEqual(shown);
});

test('Shrinking a widget lets the widgets below rise into the space', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop', 'a 2×2 keeps two columns only at desktop widths');
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await openHome(page, [{ id: 'calendar', size: '4x2' }, { id: 'kpi-upcoming', size: '1x1' }, { id: 'kpi-checkin', size: '1x1' }]);
  await page.getByRole('button', { name: 'Edit home' }).click();
  expect(await cellOf(page, 'kpi-upcoming')).toEqual({ x: 0, y: 2, w: 1, h: 1 });
  await page.getByRole('button', { name: /^Move Calendar/ }).focus();
  await page.keyboard.press('Shift+ArrowLeft');
  await expect(widget(page, 'calendar')).toHaveAttribute('data-size', '2x2');
  // The calendar keeps its top-left; both stat tiles rise beside it.
  expect(await cellOf(page, 'calendar')).toEqual({ x: 0, y: 0, w: 2, h: 2 });
  await expect.poll(() => cellOf(page, 'kpi-upcoming')).toEqual({ x: 2, y: 0, w: 1, h: 1 });
  expect(await cellOf(page, 'kpi-checkin')).toEqual({ x: 3, y: 0, w: 1, h: 1 });
  await expectAddLast(page);
  await done(page);
  expect(await savedLayout(page)).toEqual([{ id: 'calendar', size: '2x2' }, { id: 'kpi-upcoming', size: '1x1' }, { id: 'kpi-checkin', size: '1x1' }]);
});

for (const theme of THEMES) {
  test(`dragging outlines only the cells a widget can land on; the target ring scales in · ${theme}`, async ({ page }, info) => {
    test.skip(info.project.name !== 'desktop', 'mouse drag geometry');
    await useTheme(page, theme);
    await page.setViewportSize({ width: 1280, height: 1400 });
    // Four columns: two stat tiles, a gap of two cells, the calendar below.
    await openHome(page, [{ id: 'kpi-upcoming', size: '1x1' }, { id: 'kpi-checkin', size: '1x1' }, { id: 'calendar', size: '4x2' }]);
    await page.getByRole('button', { name: 'Edit home' }).click();
    const spots = () => grid(page).locator(':scope > li.blk-spot').evaluateAll((els) => els.map((e) => {
      const s = getComputedStyle(e); return `${+s.gridColumnStart - 1},${+s.gridRowStart - 1}`;
    }));
    await expect(grid(page).locator('li.blk-spot')).toHaveCount(0);

    // A 1×1 dropped in the gap would be packed back beside the other tile, so the gap gets no outline.
    let to = await dragOver(page, 'kpi-upcoming');
    await to(0, 0);
    await expect.poll(spots).toEqual(['0,0', '1,0']);
    await page.mouse.up();
    await expect(grid(page).locator('li.blk-spot')).toHaveCount(0);

    // The calendar can land on the top row, so the empty gap cells are outlined and visible.
    to = await dragOver(page, 'calendar');
    await to(0, 1);
    await expect.poll(async () => (await spots()).includes('3,0')).toBe(true);
    const spot = grid(page).locator('li.blk-spot').nth((await spots()).indexOf('3,0'));
    const look = await spot.evaluate((e) => {
      const s = getComputedStyle(e); const r = e.getBoundingClientRect();
      return { bg: s.backgroundColor, ring: s.boxShadow, dashed: [s.borderTopStyle, s.outlineStyle].includes('dashed'), uncovered: !document.elementsFromPoint(r.left + r.width / 2, r.top + r.height / 2).some((el) => el.closest('li[data-widget]')) };
    });
    expect(look.bg).not.toBe('rgba(0, 0, 0, 0)');
    expect(look.ring).toContain('inset');
    expect(look.dashed).toBe(false);
    expect(look.uncovered).toBe(true); // in empty space, not under a tile
    // The target: a fill plus a stronger ring at the widget's exact size.
    const ring = await hint(page).evaluate((e) => getComputedStyle(e, '::after').boxShadow);
    expect(ring).toContain('inset');
    expect(await hint(page).evaluate((e) => [e.getBoundingClientRect().width, e.getBoundingClientRect().height])).toEqual(
      await grid(page).locator('li.is-lifted').evaluate((e) => [parseFloat((e as HTMLElement).style.width), parseFloat((e as HTMLElement).style.height)]));
    // Moving the target scales the ring in; reduced motion keeps it still.
    // Count ring animations as they start: the 220ms animation is too short to sample reliably.
    await page.evaluate(() => {
      const w = window as unknown as { ringStarts: number };
      w.ringStarts = 0;
      const animate = Element.prototype.animate;
      Element.prototype.animate = function (this: Element, ...args: Parameters<Element['animate']>) {
        if ((args[1] as KeyframeAnimationOptions | undefined)?.pseudoElement === '::after') w.ringStarts++;
        return animate.apply(this, args);
      };
    });
    await to(0, 0);
    await expect.poll(() => cellOf(page, 'calendar')).toEqual({ x: 0, y: 0, w: 4, h: 2 });
    expect(await page.evaluate(() => (window as unknown as { ringStarts: number }).ringStarts)).toBeGreaterThan(0);
    const starts = await page.evaluate(() => (window as unknown as { ringStarts: number }).ringStarts);
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await to(0, 1);
    await expect.poll(() => cellOf(page, 'calendar')).toEqual({ x: 0, y: 1, w: 4, h: 2 });
    expect(await page.evaluate(() => (window as unknown as { ringStarts: number }).ringStarts)).toBe(starts);
    await page.mouse.up();
    await expect(grid(page).locator('li.blk-spot')).toHaveCount(0);
    await page.getByRole('button', { name: 'Cancel' }).click();
  });
}

type Frame = { t: number; boxes: { id: string; x: number; y: number; w: number; h: number; leaves: number }[] };
/** Start sampling every card's real bounding box (transforms included) on every animation frame. */
const startFrames = (page: Page) => page.evaluate(() => {
  const w = window as unknown as { frames: unknown[]; sampling: boolean };
  w.frames = []; w.sampling = true;
  const tick = () => {
    if (!w.sampling) return;
    const boxes = [...document.querySelectorAll<HTMLElement>('ul.blocks > li.blk')].map(li => {
      const r = li.getBoundingClientRect();
      // Rendered text leaves in the body: a collapsed card keeps only its title.
      const shown = (e: HTMLElement) => {
        let r = e.getBoundingClientRect();
        let left = r.left, top = r.top, right = r.right, bottom = r.bottom;
        for (let a: HTMLElement | null = e; a; a = a.parentElement) {
          const s = getComputedStyle(a);
          if (s.display === 'none' || s.visibility !== 'visible' || parseFloat(s.opacity) <= 0) return false;
          // An ancestor that clips (overflow) cuts the visible area down to its own box.
          if (a !== e && (s.overflowX !== 'visible' || s.overflowY !== 'visible' || s.contain.includes('paint') || s.clipPath !== 'none')) {
            r = a.getBoundingClientRect();
            left = Math.max(left, r.left); top = Math.max(top, r.top); right = Math.min(right, r.right); bottom = Math.min(bottom, r.bottom);
          }
          if (a === li) break;
        }
        return right - left >= 1 && bottom - top >= 1;
      };
      const leaves = [...li.querySelectorAll<HTMLElement>('.blk-body *')].filter(e => !e.children.length && e.textContent?.trim() && shown(e)).length;
      return { id: li.dataset.widget ?? (li.classList.contains('is-placeholder') ? 'slot' : li.dataset.flip ?? 'add'), x: r.left, y: r.top, w: r.width, h: r.height, leaves };
    });
    w.frames.push({ t: performance.now(), boxes });
    requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);
});
const stopFrames = (page: Page) => page.evaluate(() => {
  const w = window as unknown as { frames: unknown[]; sampling: boolean };
  w.sampling = false; return w.frames;
}) as Promise<Frame[]>;
/** Pairs of boxes that intersect by more than 1px, ignoring `float` (the card riding the pointer). */
const hits = (frames: Frame[], float?: string, ...skip: string[]) => frames.flatMap((f, n) => {
  const boxes = f.boxes.filter(b => b.id !== float && !skip.includes(b.id) && b.w > 0);
  const out: string[] = [];
  for (let i = 0; i < boxes.length; i++) for (let j = i + 1; j < boxes.length; j++) {
    const a = boxes[i]!, b = boxes[j]!;
    if (a.x < b.x + b.w - 1 && b.x < a.x + a.w - 1 && a.y < b.y + b.h - 1 && b.y < a.y + a.h - 1) out.push(`frame ${n}: ${a.id}/${b.id}`);
  }
  return out;
});
/** The layout the grid shows: cards, the drop placeholder and the add tile. */
const MIN_LEAVES = 2;

for (const project of ['desktop', 'tablet']) {
  test(`Dragging: neighbours never overlap, the card keeps its size and content, and at rest it overlaps nothing · ${project}`, async ({ page }, info) => {
    test.skip(info.project.name !== project, 'one run per viewport');
    if (project === 'desktop') await page.setViewportSize({ width: 1440, height: 1600 });
    await openHome(page, [{ id: 'calendar', size: '2x1' }, { id: 'links', size: '2x1' }, { id: 'kpi-upcoming', size: '1x1' }, { id: 'kpi-pending', size: '1x1' }]);
    await page.getByRole('button', { name: 'Edit home' }).click();
    const card = widget(page, 'calendar');
    const before = (await card.boundingBox())!;
    const move = await dragOver(page, 'calendar');
    await startFrames(page);
    const now = () => page.evaluate(() => performance.now());
    const rests: [number, number][] = [];
    // Right across Links, then below, then back: reflows in every direction. The pointer pauses at each stop.
    for (const [x, y] of [[1, 0], [3, 0], [0, 1], [2, 1], [0, 0]] as const) {
      await move(x, y, 14);
      const from = await now();
      await page.waitForTimeout(700);
      rests.push([from, await now()]);
    }
    await page.mouse.up();
    await expect(grid(page).locator('.is-lifted')).toHaveCount(0);
    await page.waitForTimeout(400);
    const frames = await stopFrames(page);
    expect(frames.length).toBeGreaterThan(40);
    // (a) Cards other than the dragged one never intersect, in any frame (the drop placeholder counts).
    expect(hits(frames, 'calendar')).toEqual([]);
    // (b) Once the pointer pauses (from 250ms: the grid has reflowed and the card has docked), the dragged
    // card overlaps no other card; the same holds again after the drop.
    const resting = frames.filter(f => rests.some(([a, b]) => f.t >= a + 250 && f.t <= b) || f.t >= rests.at(-1)![1] + 200);
    expect(resting.length).toBeGreaterThan(20);
    // The card docks into its own slot, so the placeholder is the one box it is meant to cover.
    expect(hits(resting, undefined, 'slot')).toEqual([]);
    // (c) The dragged card keeps its size (only the ≤2% lift) and its content in every frame.
    for (const f of frames) {
      const c = f.boxes.find(b => b.id === 'calendar')!;
      expect(Math.abs(c.w / before.width - 1), 'width').toBeLessThan(0.03);
      expect(Math.abs(c.h / before.height - 1), 'height').toBeLessThan(0.03);
    }
    for (const f of frames) for (const b of f.boxes) if (b.id !== 'slot' && b.id !== 'add') expect(b.leaves, `${b.id} content`).toBeGreaterThanOrEqual(MIN_LEAVES);
    const after = (await card.boundingBox())!;
    expect(Math.abs(after.width - before.width)).toBeLessThan(1);
    expect(Math.abs(after.height - before.height)).toBeLessThan(1);
  });

  test(`Resizing keeps every card rendered and nothing overlaps in any frame · ${project}`, async ({ page }, info) => {
    test.skip(info.project.name !== project, 'one run per viewport');
    if (project === 'desktop') await page.setViewportSize({ width: 1440, height: 1600 });
    await openHome(page, [{ id: 'calendar', size: '2x1' }, { id: 'links', size: '2x1' }, { id: 'kpi-upcoming', size: '1x1' }, { id: 'kpi-pending', size: '1x1' }]);
    await page.getByRole('button', { name: 'Edit home' }).click();
    const card = widget(page, 'calendar');
    const grip = card.locator('.blk-resize--both');
    const g = (await grip.boundingBox())!;
    const rect = (await card.boundingBox())!;
    await page.mouse.move(g.x + 20, g.y + 20);
    await page.mouse.down();
    await startFrames(page);
    // Grow to 4×2, shrink back, grow again.
    for (const [dx, dy] of [[rect.width, rect.height], [0, 0], [rect.width, rect.height]] as [number, number][]) {
      await page.mouse.move(g.x + 20 + dx, g.y + 20 + dy, { steps: 12 });
      await page.waitForTimeout(300);
    }
    await page.mouse.up();
    await page.waitForTimeout(300);
    const frames = await stopFrames(page);
    expect(frames.length).toBeGreaterThan(20);
    expect(hits(frames)).toEqual([]);
    for (const f of frames) for (const b of f.boxes) if (b.id !== 'slot' && b.id !== 'add') expect(b.leaves, `${b.id} content`).toBeGreaterThanOrEqual(MIN_LEAVES);
  });
}
