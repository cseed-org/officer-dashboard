import { expect, type Page } from '@playwright/test';

export type Theme = 'light' | 'dark';
export const THEMES: Theme[] = ['light', 'dark'];

/** Save a theme preference before any page script runs (as the Settings page would). */
export async function useTheme(page: Page, theme: Theme) {
  await page.addInitScript((t) => {
    try {
      if (!sessionStorage.getItem('e2e-theme-set')) {
        localStorage.setItem('cseed-theme', t);
        sessionStorage.setItem('e2e-theme-set', '1');
      }
    } catch {
      /* about:blank */
    }
  }, theme);
}

/** A test deployment's DEV_AUTH_KEY, when the server under test requires one. */
export const devKeyParam = process.env.E2E_DEV_AUTH_KEY ? `&key=${encodeURIComponent(process.env.E2E_DEV_AUTH_KEY)}` : '';

/**
 * Sign in through the dev login form (POST /auth/dev; a GET only shows a confirmation page)
 * with `as`/`user` or `discord`, then open where the server redirected. The key, when the
 * server needs one, goes with every sign-in.
 */
export async function devSignIn(page: Page, fields: { as?: string; user?: string; discord?: string }, next = '/') {
  // Session rotation invalidates the previous page's in-flight API calls. Dispose that
  // document first so a late 401 cannot redirect the newly authenticated page.
  if (page.url() !== 'about:blank') await page.goto('about:blank');
  const form: Record<string, string> = { ...fields, next } as Record<string, string>;
  if (process.env.E2E_DEV_AUTH_KEY) form.key = process.env.E2E_DEV_AUTH_KEY;
  const res = await page.request.post('/auth/dev', { form, maxRedirects: 0 });
  // 303 after a sign-in; 302 to /login?error=… when the account is refused.
  expect([302, 303], `dev login ${JSON.stringify(fields)} failed; is the API (API_ORIGIN) running with DEV_AUTH?`).toContain(res.status());
  return page.goto(res.headers()['location'] ?? '/');
}

/**
 * Sign in as the dashboard owner (owner controls). Dev login hands out ownership only on a
 * local server without DEV_AUTH_KEY, so tests that need it skip on keyed test deployments.
 */
export const ownerLoginAvailable = !process.env.E2E_DEV_AUTH_KEY;
export async function devLoginOwner(page: Page) {
  await devSignIn(page, { as: 'owner' });
  await expect(page).toHaveURL(/\/$/);
}

export async function devLogin(page: Page, as: 'director' | 'officer' | 'leadership') {
  await devSignIn(page, { as });
  await expect(page).toHaveURL(/\/$/);
}

/**
 * devLogin, with the first-visit dashboard tour marked seen. The tour's dialog covers the
 * top bar and sidebar, so tests that click the menu or search button sign in with this.
 */
export async function devLoginSkipTour(page: Page, as: 'director' | 'officer' | 'leadership') {
  await devLogin(page, as);
  const r = await page.request.put('/api/me/prefs', { data: { dashboardTutorialSeen: true }, timeout: 15_000 });
  expect(r.ok(), `PUT /api/me/prefs → ${r.status()}`).toBe(true);
  await page.reload();
}

/**
 * Expected HTTP failures: /api/search is 404 on the v1 backend (the palette then falls back
 * to the list endpoints), /login probes /api/me while signed out, and /login probes
 * /auth/dev/info, which only dev builds with DEV_AUTH=1 answer.
 */
const EXPECTED_FAILURES = [/\/api\/search\b.*→ 404$/, /\/api\/me → 401$/, /\/auth\/dev\/info\b.*→ 404$/];

/** Collect console errors, uncaught exceptions and unexpected failed requests. */
export function watchErrors(page: Page): string[] {
  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(`pageerror: ${e.message}`));
  page.on('console', (m) => {
    // The browser's "Failed to load resource" line carries no URL; responses are checked below.
    if (m.type() === 'error' && !m.text().startsWith('Failed to load resource')) errors.push(`console: ${m.text()}`);
  });
  page.on('response', (r) => {
    if (r.status() < 400) return;
    const line = `${new URL(r.url()).pathname}${new URL(r.url()).search} → ${r.status()}`;
    if (!EXPECTED_FAILURES.some((re) => re.test(line))) errors.push(`http: ${line}`);
  });
  return errors;
}

/** Islands hydrated and no loading skeletons left. */
export async function settle(page: Page) {
  await page.waitForFunction(() => !document.querySelector('astro-island[ssr]'), null, { timeout: 20_000 });
  await expect(page.locator('main [aria-busy="true"]')).toHaveCount(0, { timeout: 20_000 });
}

/** Elements wider than the viewport (the page must never scroll sideways). */
export async function overflow(page: Page) {
  return page.evaluate(() => {
    const vw = document.documentElement.clientWidth;
    const out: string[] = [];
    if (document.documentElement.scrollWidth > vw + 1) out.push(`document scrollWidth ${document.documentElement.scrollWidth} > ${vw}`);
    return out;
  });
}

/** The panel/drawer open on top of the page, if any. */
export const openDialog = (page: Page) => page.locator('dialog[open]');
