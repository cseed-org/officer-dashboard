import { expect, test } from '@playwright/test';
import { devLogin, devLoginOwner, overflow, ownerLoginAvailable, settle, THEMES, useTheme } from './helpers';

for (const theme of THEMES) {
  test(`combined Officers controls and owner menu (${theme})`, async ({ page }, info) => {
    test.skip(!ownerLoginAvailable, 'owner sign-in needs a local dev server without DEV_AUTH_KEY');
    await useTheme(page, theme);
    await devLoginOwner(page);
    await page.goto('/officers');
    await settle(page);
    await expect(page.getByRole('tab', { name: 'Directory', exact: true })).toHaveAttribute('aria-selected', 'true');
    await expect(page.locator('#sidebar [data-nav="admin"]')).toHaveCount(0);
    if (process.env.SHOTS) await page.screenshot({ path: `design/shots/v04/officers-directory-${info.project.name}-${theme}.png`, fullPage: true });
    for (const tab of ['Invites', 'Access', 'Service tokens', 'Audit log']) {
      await page.getByRole('tab', { name: tab, exact: true }).click();
      await settle(page);
      expect(await overflow(page)).toEqual([]);
      if (process.env.SHOTS) await page.screenshot({ path: `design/shots/v04/officers-${tab.toLowerCase().replaceAll(' ', '-')}-${info.project.name}-${theme}.png`, fullPage: true });
    }
    await page.getByRole('tab', { name: 'Access', exact: true }).click();
    await settle(page);
    // The only owner can't remove themselves (the server refuses it), so their row has no menu.
    await expect(page.locator('tr[data-officer="Sam Rivera"] .row-menu-btn')).toHaveCount(0);
    const row = page.locator('tr[data-officer="Priya Shah"]');
    await expect(row.getByRole('button', { name: 'Make co-owner' })).toHaveCount(0);
    await row.getByRole('button', { name: 'Actions for Priya Shah' }).click();
    await expect(page.getByRole('menuitem', { name: 'Make co-owner' })).toBeVisible();
    await expect(page.getByRole('menuitem', { name: 'Transfer ownership' })).toBeVisible();
    const menu = await page.getByRole('menu').boundingBox();
    expect(menu!.x).toBeGreaterThanOrEqual(0);
    expect(menu!.x + menu!.width).toBeLessThanOrEqual(page.viewportSize()!.width);
    expect(await overflow(page)).toEqual([]);
    if (process.env.SHOTS) await page.screenshot({ path: `design/shots/v04/officers-owner-menu-${info.project.name}-${theme}.png`, fullPage: false });
    await page.keyboard.press('Escape');
    await expect(row.getByRole('button', { name: 'Actions for Priya Shah' })).toBeFocused();
  });
}

test('legacy Admin links map to Officers tabs', async ({ page }) => {
  await devLogin(page, 'director');
  for (const [url, tab] of [['/admin', 'Access'], ['/admin#users', 'Access'], ['/admin#audit', 'Audit log'], ['/admin?tab=tokens', 'Service tokens'], ['/admin/tokens', 'Service tokens'], ['/admin/audit', 'Audit log']]) {
    await page.goto(url!);
    await expect(page).toHaveURL(/\/officers\?tab=/);
    await settle(page);
    await expect(page.getByRole('tab', { name: tab!, exact: true })).toHaveAttribute('aria-selected', 'true');
  }
});

test('directors manage officers only and cannot change roles or ownership', async ({ page }) => {
  await devLogin(page, 'director');
  await page.goto('/officers?tab=access');
  await settle(page);
  await expect(page.getByRole('group', { name: 'Role for Priya Shah' }).getByRole('button').first()).toBeDisabled();
  await expect(page.locator('tr[data-officer="Sam Rivera"] .row-menu-btn')).toHaveCount(0);
  await page.locator('tr[data-officer="Priya Shah"] .row-menu-btn').click();
  await expect(page.getByRole('menuitem', { name: 'deactivate' })).toBeVisible();
  await expect(page.getByRole('menuitem', { name: 'Make co-owner' })).toHaveCount(0);
});
