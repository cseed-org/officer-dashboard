import { test, expect, type Page } from '@playwright/test';
import { devLogin, devSignIn, overflow, settle, THEMES, useTheme, watchErrors } from './helpers';
import { defaultWidgetLayout } from '../src/ui/widgets/registry';

/**
 * V0.6 purchase "needed by" deadlines: the form, the Finance list (due hints and deadline
 * sort), the request detail's inline edit, the Home calendar source and the My actions hint.
 * Uses the seeded requests (BuildHer workshop snacks, approved, needed by 2026-11-06) and
 * creates its own `deadline-e2e-…` drafts, deleting them afterwards. Pages that show
 * "due in N days" run with the browser clock fixed at 2026-11-04 (America/Los_Angeles).
 * DEADLINE_SHOTS=1 also writes docs/evidence/purchase-deadlines/ screenshots.
 */
const SHOTS = !!process.env.DEADLINE_SHOTS;
const shotDir = '../docs/evidence/purchase-deadlines';
const FIXED = new Date('2026-11-04T10:00:00-08:00');

/** YYYY-MM-DD `days` after today in Los Angeles (the tests' time zone). */
const laDate = (days: number) => new Intl.DateTimeFormat('en-CA', { timeZone: 'America/Los_Angeles' }).format(new Date(Date.now() + days * 86_400_000));

async function draft(page: Page, title: string, neededBy: string | null) {
  const lookups = await (await page.request.get('/api/lookups')).json();
  const account = lookups.expenseAccounts.find((a: { budgetable?: boolean }) => a.budgetable !== false) ?? lookups.expenseAccounts[0];
  const res = await page.request.post('/api/requests', {
    data: { type: 'club_purchase', title, vendor: 'E2E vendor', quantity: 1, unitCostCents: 1234, taxShipCents: 0, expenseAccountId: account.id, fundId: lookups.funds[0].id, justification: 'Deadline e2e', neededBy },
  });
  expect(res.ok(), await res.text()).toBe(true);
  return (await res.json()).request as { id: string; number: string };
}
const remove = (page: Page, id: string) => page.request.delete(`/api/requests/${id}`).catch(() => {});

test('the form rejects a past needed-by date and offers today as the earliest', async ({ page }) => {
  const errors = watchErrors(page);
  await devLogin(page, 'officer');
  await page.goto('/finance?new=1');
  const input = page.getByLabel('needed by');
  await expect(input).toHaveAttribute('min', laDate(0));
  await page.getByLabel('what is it?').fill('deadline-e2e past');
  await input.fill(laDate(-1));
  await page.getByRole('button', { name: /save as draft|save draft/i }).click();
  await expect(page.locator('#rq-needed-error')).toContainText('today or later');
  await expect(input).toHaveAttribute('aria-invalid', 'true');
  expect(errors).toEqual([]);
});

test('Finance lists due hints, sorts by deadline, and the requester edits a draft deadline', async ({ page }) => {
  const errors = watchErrors(page);
  await devLogin(page, 'officer');
  const soon = await draft(page, `deadline-e2e soon ${Date.now()}`, laDate(3));
  const later = await draft(page, `deadline-e2e later ${Date.now()}`, laDate(20));
  try {
    await page.goto('/finance?scope=mine');
    const row = page.locator('tr, li').filter({ hasText: soon.number }).first();
    await expect(row).toContainText('due in 3 days');
    await page.getByLabel('sort').selectOption('deadline');
    await expect(page).toHaveURL(/sort=deadline/);
    const numbers = await page.locator('[data-request-link]').evaluateAll((els) => els.map((e) => e.getAttribute('data-request-link')));
    expect(numbers.indexOf(soon.number)).toBeLessThan(numbers.indexOf(later.number));

    await page.goto(`/finance?id=${soon.id}`);
    await page.getByRole('button', { name: 'change' }).click();
    const edit = page.getByLabel('needed by', { exact: true }).last();
    await expect(edit).toBeFocused();
    await edit.fill(laDate(-2));
    await page.getByRole('button', { name: 'save', exact: true }).click();
    await expect(page.getByRole('alert')).toContainText('today or later');
    await edit.fill(laDate(10));
    const patch = page.waitForResponse((r) => r.url().includes(`/api/requests/${soon.id}`) && r.request().method() === 'PATCH');
    await page.getByRole('button', { name: 'save', exact: true }).click();
    expect((await patch).ok()).toBe(true);
    await expect(page.locator('.due-fact')).toContainText('due in 10 days');
    expect(errors).toEqual([]);
  } finally {
    await remove(page, soon.id);
    await remove(page, later.id);
  }
});

test('another officer cannot edit the deadline of someone else’s draft', async ({ page }) => {
  await devLogin(page, 'officer');
  const mine = await draft(page, `deadline-e2e private ${Date.now()}`, laDate(5));
  try {
    await devSignIn(page, { as: 'officer', user: 'user-marcus' });
    await page.goto(`/finance?id=${mine.id}`);
    await expect(page.locator('.due-fact')).toContainText('due in 5 days');
    await expect(page.locator('.due-fact').getByRole('button')).toHaveCount(0);
  } finally {
    await devLogin(page, 'officer');
    await remove(page, mine.id);
  }
});

for (const theme of THEMES) {
  test(`Home calendar shows purchase deadlines for directors · ${theme}`, async ({ page }, info) => {
    const errors = watchErrors(page);
    await page.clock.setFixedTime(FIXED);
    await useTheme(page, theme);
    await devLogin(page, 'director');
    await page.request.put('/api/me/prefs', { data: { dashboardTutorialSeen: true } });
    // A known widget state: month view, default sources for a director plus the source toggle.
    await page.request.put('/api/me/calendar-widget', { data: { view: 'auto', colourBy: 'source', showWeekends: true, sources: ['events', 'design', 'purchase-deadlines'] } });
    await page.goto('/');
    await settle(page);
    const toggle = page.getByRole('group', { name: 'Calendar sources' }).getByRole('button', { name: /Purchase deadlines/ });
    await expect(toggle).toHaveAttribute('aria-pressed', 'true');
    const chip = page.locator('.cw-item-title', { hasText: 'BuildHer workshop snacks' }).first();
    await expect(chip).toBeVisible();
    await chip.focus();
    const popover = page.getByRole('group', { name: 'BuildHer workshop snacks details' });
    await expect(popover).toContainText('#2027-0004');
    await expect(popover).toContainText('approved');
    await expect(popover).toContainText('Purchase deadlines');
    await expect(popover.getByRole('link', { name: 'Open details →' })).toHaveAttribute('href', /\/finance\?id=req-0004/);
    expect(await overflow(page)).toEqual([]);
    if (SHOTS && info.project.name !== 'tablet') await page.screenshot({ path: `${shotDir}/calendar-${info.project.name}-${theme}.png` });

    // Toggling the source off hides the chips (and saves the choice).
    const put = page.waitForResponse((r) => r.url().endsWith('/api/me/calendar-widget') && r.request().method() === 'PUT');
    await page.keyboard.press('Escape');
    await toggle.click();
    expect((await put).ok()).toBe(true);
    await expect(page.locator('.cw-item-title', { hasText: 'BuildHer workshop snacks' })).toHaveCount(0);
    await page.request.put('/api/me/calendar-widget', { data: { view: 'auto', colourBy: 'source', showWeekends: true, sources: ['events', 'design', 'purchase-deadlines'] } });
    expect(errors).toEqual([]);
  });

  test(`Finance list and My actions show due hints · ${theme}`, async ({ page }, info) => {
    const errors = watchErrors(page);
    await page.clock.setFixedTime(FIXED);
    await useTheme(page, theme);
    // Ava requested the seeded BuildHer snacks (approved, needed by Nov 6).
    await devSignIn(page, { as: 'officer', user: 'user-ava' });
    await page.request.put('/api/me/prefs', { data: { dashboardTutorialSeen: true } });
    await page.goto('/');
    await settle(page);
    const actions = page.locator('li[data-widget="my-action"]');
    await expect(actions).toContainText('due in 2 days');
    await page.goto('/finance?scope=all&sort=deadline');
    await settle(page);
    await expect(page.locator('tr, li').filter({ hasText: '2027-0004' }).first()).toContainText('due in 2 days');
    await expect(page.locator('tr, li').filter({ hasText: '2027-0005' }).first()).toContainText('due in 8 days');
    expect(await overflow(page)).toEqual([]);
    if (SHOTS && info.project.name !== 'tablet') await page.screenshot({ path: `${shotDir}/finance-${info.project.name}-${theme}.png`, fullPage: info.project.name === 'phone' });
    if (SHOTS && info.project.name !== 'tablet') {
      await page.goto('/finance?id=req-0005');
      await settle(page);
      await expect(page.locator('.due-fact')).toContainText('due in 8 days');
      await page.screenshot({ path: `${shotDir}/detail-${info.project.name}-${theme}.png` });
    }
    // Four days later the approved snacks are overdue; fulfilled/closed rows never are.
    await page.clock.setFixedTime(new Date('2026-11-08T10:00:00-08:00'));
    await page.goto('/finance?scope=all&sort=deadline');
    await settle(page);
    const overdue = page.locator('tr, li').filter({ hasText: '2027-0004' }).first().locator('[data-due]');
    await expect(overdue).toHaveAttribute('data-due', 'overdue');
    await expect(overdue).toContainText('overdue by 2 days');
    await expect(page.locator('tr, li').filter({ hasText: '2027-0001' }).first().locator('[data-due]')).toHaveCount(0);
    if (SHOTS && info.project.name !== 'tablet') {
      await overdue.scrollIntoViewIfNeeded();
      await page.screenshot({ path: `${shotDir}/overdue-${info.project.name}-${theme}.png` });
    }
    expect(errors).toEqual([]);
  });
}

/** Rectangles of the legend's chips/summary and the time-zone label. */
const legendBoxes = (page: Page) =>
  page.locator('.cw-legend').evaluate((legend) => {
    const r = (el: Element) => { const b = el.getBoundingClientRect(); return { x: b.left, y: b.top, w: b.width, h: b.height }; };
    return {
      legend: r(legend),
      items: [...legend.querySelectorAll(':scope > .cw-sources > .cw-source-toggle, :scope > .cw-sources-menu > summary')].map(r),
      zone: legend.querySelector(':scope > .cw-zone') ? r(legend.querySelector(':scope > .cw-zone')!) : null,
    };
  });
const overlaps = (a: { x: number; y: number; w: number; h: number }, b: { x: number; y: number; w: number; h: number }) =>
  a.x < b.x + b.w - 0.5 && b.x < a.x + a.w - 0.5 && a.y < b.y + b.h - 0.5 && b.y < a.y + a.h - 0.5;

for (const theme of THEMES) {
  for (const size of ['4x2', '2x2', '2x1'] as const) {
    test(`calendar legend fits at ${size} without overlap or layout shift · ${theme}`, async ({ page }, info) => {
      const errors = watchErrors(page);
      await page.clock.setFixedTime(FIXED);
      await useTheme(page, theme);
      await devLogin(page, 'director');
      await page.request.put('/api/me/prefs', { data: { dashboardTutorialSeen: true, widgetLayout: [{ id: 'calendar', size }, { id: 'links', size: '2x1' }] } });
      await page.request.put('/api/me/calendar-widget', { data: { view: 'auto', colourBy: 'source', showWeekends: true, sources: ['events', 'design', 'purchase-deadlines'] } });
      // Hold the feed so the legend's first paint (before registry metadata) can be measured.
      let release!: () => void;
      const held = new Promise<void>((r) => (release = r));
      await page.route('**/api/calendar/feed**', async (route) => { await held; await route.continue(); });
      await page.goto('/');
      const widget = page.locator('section[data-block="calendar"]');
      await expect(widget.locator('.cw-legend')).toBeVisible();
      const before = await legendBoxes(page);
      release();
      await expect(widget.locator('.cw-grid, .cw-agenda, .cw-empty').first()).toBeVisible();
      await settle(page);
      const after = await legendBoxes(page);
      expect(after.legend.h, 'legend height is stable once sources load').toBeCloseTo(before.legend.h, 0);
      if (after.zone) for (const item of after.items) expect(overlaps(item, after.zone), 'time zone overlaps a source control').toBe(false);
      for (const item of after.items) expect(item.x + item.w).toBeLessThanOrEqual(after.legend.x + after.legend.w + 0.5);
      expect(await overflow(page)).toEqual([]);

      if (size === '4x2') {
        await expect(widget.locator('.cw-sources-menu')).toHaveCount(0);
        await expect(widget.getByRole('group', { name: 'Calendar sources' }).getByRole('button')).toHaveCount(6);
      } else {
        const summary = widget.locator('.cw-sources-menu summary');
        await expect(summary).toHaveText(/Sources \(3\/6\)/);
        const gridBox = await widget.locator('.cw-legend').evaluate((el) => el.nextElementSibling?.getBoundingClientRect().top ?? 0);
        await summary.focus();
        await page.keyboard.press('Enter');
        const toggle = widget.getByRole('group', { name: 'Calendar sources' }).getByRole('button', { name: /Purchase deadlines/ });
        await expect(toggle).toBeVisible();
        expect(await widget.locator('.cw-legend').evaluate((el) => el.nextElementSibling?.getBoundingClientRect().top ?? 0), 'opening the menu moves nothing').toBeCloseTo(gridBox, 0);
        // The floating panel sits above neighbouring widgets: its last toggle receives pointer hits.
        await widget.locator('.cw-sources-panel button').last().click({ trial: true, timeout: 5_000 });
        expect(await overflow(page)).toEqual([]);
        if (SHOTS && info.project.name !== 'tablet') await widget.screenshot({ path: `${shotDir}/legend-${size}-${info.project.name}-${theme}.png` });
        // Keyboard: Tab moves from the summary into the panel's toggles.
        await page.keyboard.press('Tab');
        await expect(widget.locator('.cw-sources-panel button').first()).toBeFocused();
        await toggle.focus();
        const put = page.waitForResponse((r) => r.url().endsWith('/api/me/calendar-widget') && r.request().method() === 'PUT');
        await page.keyboard.press('Enter');
        expect((await put).ok()).toBe(true);
        await expect(toggle).toHaveAttribute('aria-pressed', 'false');
        await expect(summary).toHaveText(/Sources \(2\/6\)/);
        await page.keyboard.press('Escape');
        await expect(toggle).toBeHidden();
        await expect(summary).toBeFocused();
      }
      if (SHOTS && info.project.name !== 'tablet' && size === '4x2') await widget.screenshot({ path: `${shotDir}/legend-${size}-${info.project.name}-${theme}.png` });
      await page.request.put('/api/me/prefs', { data: { widgetLayout: defaultWidgetLayout } });
      expect(errors).toEqual([]);
    });
  }
}
