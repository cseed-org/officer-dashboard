import { expect, test } from '@playwright/test';
import { devLogin, overflow, settle, THEMES, useTheme, watchErrors } from './helpers';
import { defaultWidgetLayout } from '../src/ui/widgets/registry';

/** V0.7 widget cards (web/design/mockups/home-v07-notes.md): icon chips, whole-card links, rows, calendar. */
test.beforeEach(async ({ page }) => {
  await page.request.post('/auth/dev', { form: { as: 'director', ...(process.env.E2E_DEV_AUTH_KEY ? { key: process.env.E2E_DEV_AUTH_KEY } : {}) }, maxRedirects: 0 });
  await page.request.put('/api/me/prefs', { data: { dashboardTutorialSeen: true, widgetLayout: defaultWidgetLayout } });
});

for (const theme of THEMES) {
  test(`every widget has an icon chip and a sentence-case title; nothing overflows · ${theme}`, async ({ page }) => {
    const errors = watchErrors(page);
    await useTheme(page, theme);
    await devLogin(page, 'director');
    await settle(page);
    const cards = page.locator('li[data-widget] section.wc');
    await expect(cards).toHaveCount(defaultWidgetLayout.length);
    for (const card of await cards.all()) {
      await expect(card.locator('.wc-chip svg')).toHaveCount(1);
      expect(await card.locator('.wc-title').textContent()).toMatch(/^[A-Z]/);
    }
    // Corner links ("all events →", "budgets →") are gone; tables are compact rows.
    await expect(page.locator('li[data-widget] .tile-link, li[data-widget] table')).toHaveCount(0);
    expect(await overflow(page)).toEqual([]);
    expect(errors).toEqual([]);
  });
}

test('calendar defaults to this week with no buttons, and loads one feed range', async ({ page }) => {
  const feeds: string[] = [];
  let optionReads = 0;
  page.on('request', (r) => {
    const path = new URL(r.url()).pathname;
    if (path === '/api/calendar/feed') feeds.push(r.url());
    if (path === '/api/me/calendar-widget') optionReads++;
  });
  await page.request.put('/api/me/prefs', { data: { widgetLayout: [{ id: 'calendar', size: '2x2' }, { id: 'links', size: '2x1' }] } });
  await devLogin(page, 'director');
  await settle(page);
  const calendar = page.locator('section[data-block="calendar"]');
  await expect(calendar).toHaveClass(/cw--week/);
  await expect(calendar.locator('.hcal-day')).toHaveCount(7);
  await expect(calendar.locator('.hcal-day').first()).toContainText('Mon');
  await expect(calendar.locator('.hcal-day').last()).toContainText('Sun');
  await expect(calendar.locator('.hcal-day.is-today')).toHaveCount(1);
  await expect(calendar.getByRole('button')).toHaveCount(0);
  await expect(calendar.locator('select, details, summary')).toHaveCount(0);
  await page.waitForLoadState('networkidle');
  expect(feeds).toHaveLength(1);
  // One month-wide range (six weeks), no saved source list.
  const q = new URL(feeds[0]!).searchParams;
  expect(q.has('sources')).toBe(false);
  expect((Date.parse(q.get('end')!) - Date.parse(q.get('start')!)) / 86_400_000).toBe(42);
  expect(optionReads).toBe(0);
});

test('4×2 calendar shows the month with real chips (dots on phones)', async ({ page }, info) => {
  await page.clock.setFixedTime(new Date('2026-11-04T10:00:00-08:00'));
  await page.request.put('/api/me/prefs', { data: { widgetLayout: [{ id: 'calendar', size: '4x2' }] } });
  await devLogin(page, 'director');
  await settle(page);
  const calendar = page.locator('section[data-block="calendar"]');
  await expect(calendar).toHaveClass(/cw--month/);
  await expect(calendar.locator('.calm-day')).toHaveCount(42);
  if (info.project.name === 'phone') await expect(calendar.locator('.calm-day .cal-dot').first()).toBeVisible();
  else await expect(calendar.locator('.calm-day a.cal-chip').first()).toBeVisible();
  await expect(calendar.locator('.wc-aside')).toContainText('November 2026');
});

test('clicking a card navigates to its page; a row opens its own item', async ({ page }, info) => {
  test.skip(info.project.name === 'tablet', 'phone and desktop cover pointer and touch');
  await devLogin(page, 'director');
  await settle(page);
  // Click empty card space (not the title) on the Budget remaining stat tile.
  const budget = page.locator('section[data-block="kpi-budget"]');
  const box = (await budget.boundingBox())!;
  await page.mouse.click(box.x + box.width - 12, box.y + box.height - 8);
  await expect(page).toHaveURL(/\/finance\?tab=budgets/);
  await page.goBack();
  await settle(page);
  const row = page.locator('section[data-block="recent-events"] a.row').first();
  const href = await row.getAttribute('href');
  expect(href).toMatch(/\/events\?id=/);
  await row.click();
  await expect(page).toHaveURL(new RegExp(href!.replace(/[?]/g, '\\?')));
});

test('Links: rows open the club links; no card link for directors', async ({ page }) => {
  await devLogin(page, 'director');
  await settle(page);
  const links = page.locator('section[data-block="links"]');
  await expect(links).toHaveClass(/wc--nolink/);
  await expect(links.locator('.wc-link')).toHaveCount(0);
  await expect(links.locator('a.link-row').first()).toHaveAttribute('target', '_blank');
});
