import { expect, type Page, type TestInfo } from '@playwright/test';
import { devLoginSkipTour, devSignIn, useTheme, type Theme } from './helpers';

/** Shared by the hello-intro specs (onboarding-intro.spec.ts, onboarding-intro-frames.spec.ts). */
export const discordId = () => `9${String(Date.now()).padStart(13, '0')}${Math.floor(Math.random() * 1e4).toString().padStart(4, '0')}`;

/** The real app shell's logo (sidebar wordmark, the rail's sapling mark, or the phone top bar), measured on Home. */
export const shellLogo = (page: Page) =>
  page.evaluate(() => {
    const el = Array.from(document.querySelectorAll('.topbar .wordmark-logo, .sidebar .wordmark-logo, .sidebar .wordmark-mark')).find((e) => {
      const b = e.getBoundingClientRect();
      return b.width > 0 && b.right > 0 && b.left < innerWidth && getComputedStyle(e).visibility !== 'hidden';
    })!;
    const b = el.getBoundingClientRect();
    return { box: [b.left, b.top, b.width, b.height], kind: el.classList.contains('wordmark-mark') ? 'mark' : 'wordmark' };
  });

export interface Open {
  theme?: Theme;
  reduced?: boolean;
  /** Runs after sign-in, before the invitee's first visit to /onboarding (routes, init scripts, the clock). */
  before?: (page: Page) => Promise<unknown>;
  /** Don't wait for the overlay (for presses made the instant it appears). */
  noWait?: boolean;
}

/** Signs in a fresh invitee, whose first page is the intro; returns the shell's logo box measured on Home. */
export async function openIntro(page: Page, o: Open = {}) {
  const theme = o.theme ?? 'light';
  await useTheme(page, theme);
  await page.emulateMedia({ colorScheme: theme, reducedMotion: o.reduced ? 'reduce' : 'no-preference' });
  const id = discordId();
  await devLoginSkipTour(page, 'director');
  await expect(page.locator('.app')).toBeVisible();
  const shell = await shellLogo(page);
  const r = await page.request.post('/api/invites', { data: { name: 'Intro Tester', discordId: id, subteamId: 'team-events', method: 'link' } });
  expect(r.ok()).toBe(true);
  await page.goto('about:blank');
  await page.context().clearCookies();
  await o.before?.(page);
  await devSignIn(page, { discord: id });
  await expect(page).toHaveURL(/\/onboarding$/);
  if (!o.noWait) await expect(page.locator('.wi')).toBeVisible();
  return shell;
}

export const overlay = (page: Page) => page.locator('.wi');
export const front = (page: Page) => page.locator('.wi-slot[data-front]');
export const pressAnywhere = async (page: Page, info: TestInfo) => {
  const vp = page.viewportSize()!;
  if (info.project.use.hasTouch) await page.touchscreen.tap(vp.width * 0.3, vp.height * 0.8);
  else await page.mouse.click(vp.width * 0.3, vp.height * 0.8);
};
