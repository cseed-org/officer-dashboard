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
    await expect(widget(page, 'kpi-upcoming')).toContainText('upcoming events');
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

test('Default layout packs into a full rectangle: no ragged gaps', async ({ page }, info) => {
  await openHome(page);
  const box = await grid(page).boundingBox();
  const rects = await grid(page).locator(':scope > li[data-widget]').evaluateAll((els) => els.map((e) => e.getBoundingClientRect()).map((r) => ({ l: r.left, r: r.right, b: r.bottom })));
  const bottomAt = (edge: 'l' | 'r', x: number) => Math.max(...rects.filter((r) => Math.abs(r[edge] - x) < 2).map((r) => r.b));
  // The last block in the left column ends level with the last block in the right column.
  expect(Math.abs(bottomAt('l', box!.x) - bottomAt('r', box!.x + box!.width))).toBeLessThan(2);
});

test('Resize, remove and add widgets; Done persists, Cancel discards', async ({ page }) => {
  await openHome(page);
  await page.getByRole('button', { name: 'Edit home' }).click();
  // Only the sizes a widget supports are offered.
  const sizes = page.getByRole('group', { name: 'Size of Upcoming events' }).getByRole('button');
  await expect(sizes).toHaveText(['1×1', '2×1']);
  await page.getByRole('group', { name: 'Size of Upcoming events' }).getByRole('button', { name: '2 by 1 blocks' }).click();
  await expect(widget(page, 'kpi-upcoming')).toHaveAttribute('data-size', '2x1');
  await page.getByRole('group', { name: 'Size of Recent events' }).getByRole('button', { name: '4 by 2 blocks' }).click();
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
  await expect(widget(page, 'member-signups')).toContainText('Not connected');
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
  const grip = page.getByRole('button', { name: /^Move Pending requests/ });
  const from = (await grip.boundingBox())!;
  const to = (await widget(page, 'kpi-upcoming').boundingBox())!;
  await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2);
  await page.mouse.down();
  await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, { steps: 12 });
  const target = grid(page).locator('li.is-dragging');
  await expect(target).toHaveAttribute('data-widget', 'kpi-pending');
  const look = await target.evaluate((e) => { const s = getComputedStyle(e); return { border: s.borderTopStyle, bg: s.backgroundColor }; });
  expect(look.border).not.toBe('dashed');
  expect(look.bg).not.toBe('rgba(0, 0, 0, 0)');
  await expect(page.locator('.blk-float')).toContainText('Pending requests');
  await page.mouse.up();
  expect((await order(page))[0]).toBe('kpi-pending');
  await expect(page.locator('.blk-float')).toHaveCount(0);
  await done(page);
  expect((await savedLayout(page))[0]?.id).toBe('kpi-pending');

  // Escape during a drag puts everything back.
  await page.getByRole('button', { name: 'Edit home' }).click();
  const before = await order(page);
  const g2 = (await page.getByRole('button', { name: /^Move Budget vs actual/ }).boundingBox())!;
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

test('Phone: two columns, stat tiles pair up, tables take the full row', async ({ page }, info) => {
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
