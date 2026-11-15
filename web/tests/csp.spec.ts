import { expect, test, type Page } from '@playwright/test';
import { devSignIn, settle, watchErrors } from './helpers';

/**
 * Content-Security-Policy: every page runs under the server's policy (script hashes, no
 * inline handlers, no eval) without a single violation. Run against the Go server serving
 * the built site, since the Astro dev server sends no CSP:
 *   WEB_ORIGIN=http://127.0.0.1:<port> npx playwright test csp --project=desktop
 */
test.skip(!process.env.WEB_ORIGIN, 'needs the Go server (WEB_ORIGIN); astro dev sends no CSP');

const PAGES = ['/', '/events', '/finance', '/finance?tab=budgets', '/contacts', '/design', '/members', '/officers', '/settings', '/admin', '/onboarding', '/no-such-page'];

/** Collect CSP violations reported to the page (console messages and the DOM event). */
async function watchCSP(page: Page): Promise<string[]> {
  const violations: string[] = [];
  page.on('console', (m) => {
    if (/Content Security Policy|Refused to (load|execute|apply|evaluate|connect|frame)/i.test(m.text())) violations.push(`console: ${m.text()}`);
  });
  await page.exposeFunction('__cspViolation', (v: string) => violations.push(v));
  await page.addInitScript(() => {
    document.addEventListener('securitypolicyviolation', (e) => {
      (window as unknown as { __cspViolation: (v: string) => void }).__cspViolation(`${e.violatedDirective} blocked ${e.blockedURI || 'inline'} at ${e.sourceFile}:${e.lineNumber}`);
    });
  });
  return violations;
}

test('the login page renders under its CSP', async ({ page }) => {
  const violations = await watchCSP(page);
  const errors = watchErrors(page);
  const res = await page.goto('/login');
  expect(res?.headers()['content-security-policy']).toContain("script-src 'self' 'sha256-");
  await expect(page.getByRole('link', { name: 'Sign in with Discord' })).toBeVisible();
  // The inline script ran (it rewrites the link with next=).
  await page.goto('/login?next=%2Fevents');
  await expect(page.getByRole('link', { name: 'Sign in with Discord' })).toHaveAttribute('href', /next=%2Fevents/);
  expect(violations).toEqual([]);
  expect(errors).toEqual([]);
});

for (const as of ['officer', 'director', 'leadership'] as const) {
  test(`every page as ${as} runs without CSP violations`, async ({ page }) => {
    const violations = await watchCSP(page);
    const errors = watchErrors(page);
    await devSignIn(page, { as });
    for (const path of PAGES) {
      const res = await page.goto(path);
      expect(res?.headers()['content-security-policy'], path).toContain("frame-ancestors 'none'");
      if (path !== '/no-such-page') await settle(page);
      // Theme pre-paint (inline script) applied, so hashed inline scripts execute.
      expect(await page.evaluate(() => document.documentElement.dataset.theme ?? document.documentElement.className), path).toBeTruthy();
    }
    expect(violations).toEqual([]);
    expect(errors.filter((e) => !e.includes('/no-such-page'))).toEqual([]);
  });
}

// L-17: the hello intro (fonts, data: fruit sprites, the inline early-input script, the logo images)
// runs under the page policy too. Directors are sent past /onboarding, so this uses a fresh invitee.
test('an invitee on /onboarding plays the hello intro to the end without violations', async ({ page }) => {
  const violations = await watchCSP(page);
  const errors = watchErrors(page);
  await devSignIn(page, { as: 'director' });
  const id = `9${String(Date.now()).padStart(13, '0')}${Math.floor(Math.random() * 1e4).toString().padStart(4, '0')}`;
  const r = await page.request.post('/api/invites', { data: { name: 'CSP Intro', discordId: id, subteamId: 'team-events', method: 'link' } });
  expect(r.ok()).toBe(true);
  await page.goto('about:blank');
  await page.context().clearCookies();
  const res = await devSignIn(page, { discord: id });
  await expect(page).toHaveURL(/\/onboarding$/);
  expect(res?.headers()['content-security-policy']).toContain("script-src 'self' 'sha256-");
  await expect(page.locator('.wi')).toBeVisible();
  // The inline early-input script ran (its hash is in the policy).
  expect(await page.evaluate(() => Array.isArray((window as { __wiEarly?: unknown[] }).__wiEarly))).toBe(true);
  await page.waitForTimeout(800);
  await page.keyboard.press('Enter');
  await expect(page.locator('.wi')).toHaveCount(0, { timeout: 15_000 });
  await expect(page.locator('#step-title')).toBeVisible();
  expect(violations).toEqual([]);
  expect(errors).toEqual([]);
});

test('a Discord avatar loads under img-src', async ({ page }) => {
  const violations = await watchCSP(page);
  await devSignIn(page, { as: 'officer', user: 'user-priya' }, '/settings');
  await settle(page);
  const avatar = page.locator('img.avatar').first();
  test.skip((await avatar.count()) === 0, 'seeded user has no avatar_url');
  await expect(avatar).toHaveAttribute('src', /cdn\.discordapp\.com/);
  expect(violations).toEqual([]);
});

test('sign-out (form post) and the OAuth links leave without violations', async ({ page }) => {
  const violations = await watchCSP(page);
  await devSignIn(page, { as: 'officer' }, '/settings');
  await settle(page);
  // Google Calendar connect is a link: a top-level navigation to the server, which redirects
  // to Google when configured (otherwise it answers on this origin).
  const connect = page.locator('a[href*="/api/me/calendar/google/connect"]').first();
  if (await connect.count()) {
    const [res] = await Promise.all([page.waitForResponse((r) => r.url().includes('/calendar/google/connect')), connect.click()]);
    expect([302, 303, 503]).toContain(res.status());
    await page.goto('/settings');
    await settle(page);
  }
  await page.getByRole('button', { name: /sign out/i }).first().click();
  await expect(page).toHaveURL(/\/login/);
  // Discord sign-in is a link to /auth/login, which redirects to discord.com when configured.
  const [login] = await Promise.all([
    page.waitForResponse((r) => new URL(r.url()).pathname === '/auth/login'),
    page.getByRole('link', { name: 'Sign in with Discord' }).click(),
  ]);
  expect([302, 503]).toContain(login.status());
  expect(violations).toEqual([]);
});
