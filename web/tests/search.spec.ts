import { expect, test } from '@playwright/test';
import { devLogin, openDialog, settle, watchErrors } from './helpers';

test('sidebar search finds a seeded request and opens it', async ({ page }, info) => {
  const errors = watchErrors(page);
  await devLogin(page, 'officer');
  await settle(page);

  // Phone: the search button is in the top bar; tablet/desktop: in the sidebar.
  const trigger = info.project.name === 'phone' ? page.locator('.topbar [data-open-search]') : page.locator('#sidebar [data-open-search]');
  await trigger.click();
  const box = page.getByRole('combobox', { name: /Search/ });
  await expect(box).toBeFocused();
  await box.fill('banner');
  const option = page.getByRole('option', { name: /Demo night banner printing/ });
  await expect(option).toBeVisible();
  await expect(page.locator('.palette [data-group="request"]')).toBeVisible();
  // Keyboard: the first result is active; Enter opens it.
  await expect(page.getByRole('option').first()).toHaveAttribute('aria-selected', 'true');
  await box.press('Enter');
  await expect(page).toHaveURL(/\/finance\?id=req-0005/);
  await settle(page);
  await expect(openDialog(page).getByRole('heading', { level: 2, name: 'Demo night banner printing' })).toBeVisible();
  expect(errors).toEqual([]);
});

test('Ctrl+K opens the palette anywhere and arrow keys move through results', async ({ page }, info) => {
  test.skip(info.project.name === 'phone', 'no hardware keyboard on phones');
  await devLogin(page, 'director');
  await page.goto('/settings');
  await settle(page);
  await page.keyboard.press('Control+k');
  const box = page.getByRole('combobox', { name: /Search/ });
  await expect(box).toBeFocused();
  await box.fill('hack');
  await expect(page.locator('.palette [data-group="request"]')).toBeVisible();
  const options = page.getByRole('option');
  await box.press('ArrowDown');
  await expect(options.nth(1)).toHaveAttribute('aria-selected', 'true');
  await box.press('ArrowUp');
  await expect(options.nth(0)).toHaveAttribute('aria-selected', 'true');
  await page.keyboard.press('Escape');
  await expect(box).toBeHidden();
});
