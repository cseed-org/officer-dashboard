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

export async function devLogin(page: Page, as: 'director' | 'officer') {
  const res = await page.goto(`/auth/dev?as=${as}`);
  expect(res?.ok(), `dev login as ${as} failed; is the API (API_ORIGIN) running with DEV_AUTH?`).toBe(true);
  await expect(page).toHaveURL(/\/$/);
}

/**
 * Expected HTTP failures: /api/search is 404 on the v1 backend (the palette then falls back
 * to the list endpoints), and /login probes /api/me while signed out.
 */
const EXPECTED_FAILURES = [/\/api\/search\b.*→ 404$/, /\/api\/me → 401$/];

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
