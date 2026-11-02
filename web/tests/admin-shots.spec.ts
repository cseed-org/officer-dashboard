import { test } from '@playwright/test';
import { devLogin, settle, THEMES, useTheme } from './helpers';

/** Admin review screenshots at every viewport (SHOTS=1 npx playwright test admin-shots) → tmp/shots/. */
const VIEWS = [
  ['admin', '/officers?tab=access'],
  ['admin-add', '/officers?tab=access&new=1'],
  ['admin-tokens', '/officers?tab=tokens'],
  ['admin-token-new', '/officers?tab=tokens&new=1'],
  ['admin-audit', '/officers?tab=audit'],
] as const;

test.skip(!process.env.SHOTS, 'set SHOTS=1 to capture review screenshots');

for (const theme of THEMES) {
  test(`admin screenshots, ${theme}`, async ({ page }, info) => {
    await useTheme(page, theme);
    await devLogin(page, 'director');
    for (const [name, path] of VIEWS) {
      await page.goto(path);
      await settle(page);
      await page.waitForTimeout(500);
      const dialog = await page.locator('dialog[open]').count();
      await page.screenshot({ path: `tmp/shots/${info.project.name}-${theme}-${name}.png`, fullPage: !dialog });
    }
    // The open ⋯ menu on the first officer.
    await page.goto('/officers?tab=access');
    await settle(page);
    await page.locator('.row-menu-btn').first().click();
    await page.waitForTimeout(250);
    await page.screenshot({ path: `tmp/shots/${info.project.name}-${theme}-admin-menu.png` });
  });
}
