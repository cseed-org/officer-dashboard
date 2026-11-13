import { test, expect, type Page } from '@playwright/test';
import { devLoginSkipTour, watchErrors } from './helpers';

/**
 * V0.10 sidebar People folder: order, open/closed remembered across reloads, the icons-only
 * flyout (keyboard) and the phone menu sheet.
 */
const toggle = (page: Page) => page.locator('#nav-people-toggle');
const panel = (page: Page) => page.locator('#nav-people-panel');
const reset = async (page: Page, path = '/') => {
  await page.evaluate(() => { try { localStorage.removeItem('cseed-people'); } catch { /* */ } });
  await page.goto(path);
};
const setRail = async (page: Page) => {
  await page.request.put('/api/me/prefs', { data: { sidebarWidth: 64 } });
  await page.evaluate(() => { try { localStorage.setItem('cseed-sidebar', '64'); } catch { /* */ } });
  await page.reload();
};
const resetWidth = async (page: Page) => {
  await page.request.put('/api/me/prefs', { data: { sidebarWidth: 0 } });
  await page.evaluate(() => { try { localStorage.removeItem('cseed-sidebar'); } catch { /* */ } });
};
async function openMenu(page: Page) {
  await page.locator('[data-nav-toggle]').click();
  await expect(page.locator('html')).toHaveClass(/nav-open/);
}

test.describe('sidebar People folder', () => {
  test.beforeEach(async ({ page }) => {
    await devLoginSkipTour(page, 'officer');
    await resetWidth(page);
  });
  test.afterEach(async ({ page }) => { await resetWidth(page).catch(() => {}); });

  test('order: Design sits directly above People, which holds the three pages', async ({ page }, info) => {
    const errors = watchErrors(page);
    await reset(page);
    if (info.project.name === 'phone') await openMenu(page);
    // Wide sidebar: top-level entries in order. Rail/phone expose the same DOM order.
    const top = await page.locator('#sidebar .nav-list > li > .nav-link').evaluateAll((els) => els.map((e) => e.getAttribute('data-nav')));
    expect(top).toEqual(['home', 'buildspace', 'events', 'finance', 'design', 'people']);
    const subs = await panel(page).locator('a').evaluateAll((els) => els.map((e) => [e.getAttribute('data-nav'), e.getAttribute('href')]));
    expect(subs).toEqual([['officers', '/officers'], ['members', '/members'], ['contacts', '/contacts']]);
    await expect(panel(page).locator('a').first()).toContainText('Board/Officers');
    expect(errors.filter((e) => !/\b(40[134])\b/.test(e))).toEqual([]);
  });

  test('open by default, collapse is remembered across reloads', async ({ page }, info) => {
    test.skip(info.project.name === 'tablet', 'tablet defaults to the icons-only rail');
    await reset(page);
    if (info.project.name === 'phone') await openMenu(page);
    await expect(toggle(page)).toHaveAttribute('aria-expanded', 'true');
    await expect(panel(page).getByRole('link', { name: 'Members' })).toBeVisible();

    await toggle(page).click();
    await expect(toggle(page)).toHaveAttribute('aria-expanded', 'false');
    await expect(panel(page).getByRole('link', { name: 'Members' })).toBeHidden();
    expect(await page.evaluate(() => localStorage.getItem('cseed-people'))).toBe('0');

    await page.reload();
    if (info.project.name === 'phone') await openMenu(page);
    await expect(toggle(page)).toHaveAttribute('aria-expanded', 'false');
    await expect(panel(page).getByRole('link', { name: 'Members' })).toBeHidden();

    await toggle(page).click();
    await expect(panel(page).getByRole('link', { name: 'Members' })).toBeVisible();
    await page.reload();
    if (info.project.name === 'phone') await openMenu(page);
    await expect(toggle(page)).toHaveAttribute('aria-expanded', 'true');
  });

  test('a collapsed folder auto-expands on one of its pages and marks the page current', async ({ page }, info) => {
    test.skip(info.project.name === 'tablet', 'rail flyout is covered below');
    await page.evaluate(() => localStorage.setItem('cseed-people', '0'));
    await page.goto('/members');
    if (info.project.name === 'phone') await openMenu(page);
    await expect(toggle(page)).toHaveAttribute('aria-expanded', 'true');
    await expect(panel(page).locator('[data-nav="members"]')).toHaveAttribute('aria-current', 'page');
    await expect(panel(page).locator('[data-nav="members"]')).toBeVisible();
    await page.goto('/events');
    if (info.project.name === 'phone') await openMenu(page);
    await expect(toggle(page)).toHaveAttribute('aria-expanded', 'false');
  });

  test('the tour targets still resolve', async ({ page }, info) => {
    await reset(page);
    await expect(page.locator('[data-tour="sidebar"]#sidebar')).toHaveCount(1);
    if (info.project.name === 'phone') await expect(page.locator('[data-tour="nav-toggle"]')).toBeVisible();
    else await expect(page.locator('[data-tour="sidebar-resize"]')).toBeVisible();
  });

  test('icons-only rail: one icon, flyout with keyboard support', async ({ page }, info) => {
    test.skip(info.project.name === 'phone', 'no rail on phones');
    await reset(page);
    await setRail(page);
    await expect(page.locator('#sidebar .nav-label').first()).toBeHidden();
    await expect(toggle(page)).toBeVisible();
    await expect(panel(page)).toBeHidden();
    await expect(toggle(page)).toHaveAttribute('aria-expanded', 'false');
    await expect(toggle(page)).toHaveAttribute('aria-controls', 'nav-people-panel');

    // Keyboard: ArrowDown opens and focuses the first page; arrows move; Esc closes and restores focus.
    await toggle(page).focus();
    await page.keyboard.press('ArrowDown');
    await expect(panel(page)).toBeVisible();
    await expect(toggle(page)).toHaveAttribute('aria-expanded', 'true');
    await expect(panel(page).locator('a').first()).toBeFocused();
    await page.keyboard.press('ArrowDown');
    await expect(panel(page).locator('a').nth(1)).toBeFocused();
    await page.keyboard.press('ArrowUp');
    await page.keyboard.press('ArrowUp'); // wraps to the last
    await expect(panel(page).locator('a').nth(2)).toBeFocused();
    await page.keyboard.press('Escape');
    await expect(panel(page)).toBeHidden();
    await expect(toggle(page)).toBeFocused();
    await expect(toggle(page)).toHaveAttribute('aria-expanded', 'false');

    // Click opens, a second click closes, a link navigates.
    await toggle(page).click();
    await expect(panel(page)).toBeVisible();
    const box = (await panel(page).boundingBox())!;
    expect(box.x).toBeGreaterThanOrEqual(60); // beside the rail, not inside it
    await toggle(page).click();
    await expect(panel(page)).toBeHidden();
    await toggle(page).click();
    await panel(page).getByRole('link', { name: 'External Contacts' }).click();
    await expect(page).toHaveURL(/\/contacts/);
    // On one of its pages the rail icon shows as current.
    await expect(page.locator('.nav-folder')).toHaveAttribute('data-current', '');
  });

  test('phone menu sheet: expandable group', async ({ page }, info) => {
    test.skip(info.project.name !== 'phone', 'phone only');
    await reset(page);
    await openMenu(page);
    await expect(toggle(page)).toBeVisible();
    await expect(toggle(page)).toHaveAttribute('aria-expanded', 'true');
    await toggle(page).click();
    await expect(toggle(page)).toHaveAttribute('aria-expanded', 'false');
    await expect(panel(page).getByRole('link', { name: 'Members' })).toBeHidden();
    await toggle(page).click();
    await panel(page).getByRole('link', { name: 'Members' }).click();
    await expect(page).toHaveURL(/\/members/);
    await expect(page.locator('html')).not.toHaveClass(/nav-open/);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  });
});
