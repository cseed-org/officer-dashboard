import { expect, test } from '@playwright/test';
import { layoutFailures, measure } from './measure';
import { devLogin, shot } from './helpers';

/**
 * Empty, error and loading states, driven by the UI mocks' ?mock= switch
 * (src/ui/mocks/handlers.ts). Skipped when the mocks are off.
 */
test.skip(process.env.PUBLIC_UI_MOCKS === 'off', 'needs the UI mocks');

const PAGES = ['/', '/events', '/requests', '/finance', '/admin/tokens'];

for (const scheme of ['light', 'dark'] as const) {
  test(`empty, error and loading states render cleanly (${scheme})`, async ({ page }, info) => {
    await devLogin(page, 'director');
    await page.emulateMedia({ colorScheme: scheme });
    const problems: string[] = [];
    for (const route of PAGES) {
      for (const state of ['empty', 'error', 'loading'] as const) {
        const sep = route.includes('?') ? '&' : '?';
        await page.goto(`${route}${sep}mock=${state}`);
        await page.evaluate(() => document.fonts.ready);
        if (state === 'loading') await expect(page.locator('[aria-busy="true"]').first()).toBeVisible();
        else await expect(page.locator(`[data-state="${state}"]`).first()).toBeVisible({ timeout: 10_000 });
        if (state === 'error') await expect(page.getByRole('button', { name: 'try again' }).first()).toBeVisible();
        problems.push(...layoutFailures(await page.evaluate(measure)).map((f) => `${route} ${state}: ${f}`));
        await shot(page, info.project.name, scheme, `states/${route === '/' ? 'overview' : route.slice(1).replaceAll('/', '_')}-${state}`);
      }
    }
    expect(problems, problems.join('\n')).toEqual([]);
  });
}

test('a request id that does not exist shows not found', async ({ page }) => {
  await devLogin(page, 'officer');
  await page.goto('/requests/req-nope');
  await expect(page.locator('[data-state="error"]')).toContainText('Not found');
  await page.goto('/events/event-nope');
  await expect(page.locator('[data-state="error"]')).toContainText('Not found');
});
