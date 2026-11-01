import { expect, test } from '@playwright/test';
import { devLogin, openDialog, overflow, settle, THEMES, useTheme, watchErrors } from './helpers';

const PAGES: { path: string; heading: string }[] = [
  { path: '/', heading: 'Home' },
  { path: '/events', heading: 'Events' },
  { path: '/finance', heading: 'Requests & Finance' },
  { path: '/finance?tab=budgets', heading: 'Requests & Finance' },
  { path: '/finance?tab=reports', heading: 'Requests & Finance' },
  { path: '/finance?tab=periods', heading: 'Requests & Finance' },
  { path: '/admin', heading: 'Admin' },
  { path: '/admin?tab=tokens', heading: 'Admin' },
  { path: '/admin?tab=audit', heading: 'Admin' },
  { path: '/settings', heading: 'Settings' },
];

for (const theme of THEMES) {
  test.describe(`${theme} theme`, () => {
    test('every page renders without errors or sideways scroll', async ({ page }) => {
      await useTheme(page, theme);
      const errors = watchErrors(page);
      await devLogin(page, 'director');
      for (const p of PAGES) {
        await page.goto(p.path);
        await settle(page);
        await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
        await expect(page.getByRole('heading', { level: 1 })).toHaveText(p.heading);
        await expect(page.locator('main [data-state="error"]'), `${p.path} shows an error`).toHaveCount(0);
        expect(await overflow(page), `${p.path} overflows`).toEqual([]);
      }
      expect(errors).toEqual([]);
    });

    test('event and request details open in a panel', async ({ page }) => {
      await useTheme(page, theme);
      const errors = watchErrors(page);
      await devLogin(page, 'director');

      await page.goto('/events');
      await settle(page);
      const firstEvent = page.locator('[data-event-link]').first();
      const name = (await firstEvent.textContent())!.trim();
      await firstEvent.click();
      await expect(page).toHaveURL(/\/events\?id=/);
      await expect(openDialog(page).getByRole('heading', { level: 2, name })).toBeVisible();
      await expect(openDialog(page).locator('[data-event-detail]')).toBeVisible();
      expect(await overflow(page)).toEqual([]);
      await page.keyboard.press('Escape');
      await expect(openDialog(page)).toHaveCount(0);
      await expect(page).toHaveURL(/\/events$/);

      // The CSV import dialog lives on the same page.
      await page.locator('[data-open-import]').click();
      await expect(openDialog(page).getByRole('heading', { name: 'import a CSV' })).toBeVisible();
      await page.keyboard.press('Escape');

      await page.goto('/finance?scope=all');
      await settle(page);
      const firstReq = page.locator('main [data-request-link]').first();
      const title = (await firstReq.textContent())!.trim();
      await firstReq.click();
      await expect(page).toHaveURL(/\/finance\?.*id=/);
      await expect(openDialog(page).getByRole('heading', { level: 2, name: title })).toBeVisible();
      await expect(openDialog(page).locator('[data-request-detail]')).toBeVisible();
      expect(await overflow(page)).toEqual([]);
      // Back closes the panel.
      await page.goBack();
      await expect(openDialog(page)).toHaveCount(0);
      expect(errors).toEqual([]);
    });
  });
}
