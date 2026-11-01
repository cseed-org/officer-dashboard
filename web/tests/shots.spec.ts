import { test } from '@playwright/test';
import { devLogin, settle, THEMES, useTheme } from './helpers';

/** Review screenshots (SHOTS=1 npx playwright test shots --project=desktop --project=phone) → tmp/shots/. */
const PAGES = [
  ['home', '/'],
  ['events', '/events'],
  ['event-detail', '/events?id=event-hack-night'],
  ['finance', '/finance'],
  ['request-detail', '/finance?id=req-0005'],
  ['finance-budgets', '/finance?tab=budgets'],
  ['finance-reports', '/finance?tab=reports'],
  ['admin', '/admin'],
  ['settings', '/settings'],
] as const;

test.skip(!process.env.SHOTS, 'set SHOTS=1 to capture review screenshots');

for (const theme of THEMES) {
  test(`screenshots, ${theme}`, async ({ page }, info) => {
    await useTheme(page, theme);
    await devLogin(page, 'director');
    for (const [name, path] of PAGES) {
      await page.goto(path);
      await settle(page);
      await page.waitForTimeout(250); // drawer slide-in
      const dialog = await page.locator('dialog[open]').count();
      await page.screenshot({ path: `tmp/shots/${info.project.name}-${theme}-${name}.png`, fullPage: !dialog });
    }
  });
}
