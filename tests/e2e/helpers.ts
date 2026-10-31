import { expect, type Page } from '@playwright/test';

export const SHOTS_DIR = 'tmp/screenshots';

export const shotName = (route: string) =>
  route === '/' ? 'overview' : route.replace(/^\//, '').replace(/[/?&=]+/g, '_').replace(/_$/, '');

export async function devLogin(page: Page, as: 'director' | 'officer') {
  const res = await page.goto(`/auth/dev?as=${as}`);
  expect(res?.status(), 'dev login failed; is DEV_AUTH=1 in .dev.vars?').toBe(200);
  await expect(page).toHaveURL('/');
}

/** Wait for islands to hydrate and finish loading (no aria-busy skeletons), fonts loaded. */
export async function settle(page: Page) {
  await page.waitForFunction(() => !document.querySelector('astro-island[ssr]'), null, { timeout: 15_000 });
  await expect(page.locator('[aria-busy="true"]')).toHaveCount(0, { timeout: 15_000 });
  await page.evaluate(() => document.fonts.ready);
  await page.addStyleTag({ content: 'astro-dev-toolbar { display: none !important; }' });
}

export async function shot(page: Page, project: string, scheme: string, routeOrName: string) {
  await page.screenshot({ path: `${SHOTS_DIR}/${project}/${scheme}/${shotName(routeOrName)}.png`, fullPage: true });
}
