import { expect, test, type Page } from '@playwright/test';
import { devLogin, devSignIn } from './helpers';

/**
 * V0.9 first-login flow: welcome intro → five-stage intake (basics, about you, your role,
 * calendar, widgets) → Home with the guided tour. Covers a brand-new invitee, an existing
 * officer seeing it once, the start term (automatic; only admins edit it), the tour's skip and
 * replay, and the reduced-motion path. Needs the Go server with DEV_AUTH=1.
 */
const discordId = () => `8${String(Date.now()).padStart(13, '0')}${Math.floor(Math.random() * 1e4).toString().padStart(4, '0')}`;
const TERM = /^(Spring|Summer|Fall) \d{4}$/;

/** Invites a fresh officer as a director, then signs in as them. Lands on /onboarding. */
async function newInvitee(page: Page) {
  const id = discordId();
  await devLogin(page, 'director');
  const r = await page.request.post('/api/invites', { data: { name: 'Quinn Intake', discordId: id, subteamId: 'team-events', method: 'link' } });
  expect(r.ok()).toBe(true);
  await page.goto('about:blank');
  await page.context().clearCookies();
  await devSignIn(page, { discord: id });
  await expect(page).toHaveURL(/\/onboarding$/);
  return id;
}

const stage = (page: Page, n: number) => expect(page.locator('.onb')).toHaveAttribute('data-step', String(n));
const next = (page: Page) => page.locator('[data-onb-next]').click();
const logoBox = (page: Page) => page.locator('[data-onb-logo]').evaluate((e) => JSON.stringify(e.getBoundingClientRect()));

async function skipIntro(page: Page) {
  await expect(page.locator('.wi')).toBeVisible();
  await page.getByRole('button', { name: 'Skip' }).click();
  await expect(page.locator('.wi')).toHaveCount(0, { timeout: 5000 });
}

test('a new officer: intro, then the intake slides in under the same logo, then the tour', async ({ page }) => {
  await newInvitee(page);
  const before = await logoBox(page);
  await expect(page.locator('.onb-main')).toHaveCount(0); // nothing of the form shows under the intro
  // V0.10: no start button, only Skip; any click, tap or key starts the intro.
  await expect(page.locator('.wi').getByRole('button')).toHaveCount(1);
  await skipIntro(page);
  // Same logo, same place; the form is there.
  expect(await logoBox(page)).toBe(before);
  await stage(page, 1);
  await expect(page.locator('#step-title')).toBeFocused();
  await expect(page.locator('.onb-progress li')).toHaveCount(5);

  // Stage 1: phone and birthday are required and checked inline.
  await next(page);
  await expect(page.locator('#pf-phone-error')).toBeVisible();
  await expect(page.locator('#pf-birthday-error')).toBeVisible();
  await stage(page, 1);
  await page.fill('#pf-phone', '+1 206 555 0142');
  await page.selectOption('#pf-month', '04');
  await page.selectOption('#pf-day', '21');
  await next(page);

  // Stage 2: optional details.
  await stage(page, 2);
  await page.fill('#pf-personalEmail', 'nope');
  await next(page);
  await expect(page.locator('#pf-personalEmail-error')).toBeVisible();
  await stage(page, 2);
  await page.fill('#pf-personalEmail', 'quinn@example.org');
  await page.fill('#pf-pronouns', 'they/them');
  await next(page);

  // Stage 3: team or committee; the start term is shown, never an input.
  await stage(page, 3);
  await page.selectOption('#pf-team', 'Marketing and outreach');
  await page.fill('#pf-title', 'Outreach lead');
  await expect(page.locator('[data-start-term]')).toContainText(/Start term: (Spring|Summer|Fall) \d{4}/);
  await expect(page.locator('#pf-startTerm')).toHaveCount(0);
  await next(page);

  // Back and next keep what was typed.
  await stage(page, 4);
  await page.locator('[data-onb-back]').click();
  await stage(page, 3);
  await expect(page.locator('#pf-team')).toHaveValue('Marketing and outreach');
  await next(page);
  await stage(page, 4);
  await next(page); // calendar is optional
  await stage(page, 5);
  await expect(page.locator('[data-onb-next]')).toContainText('Go to dashboard');
  await next(page);

  await expect(page).toHaveURL(/\/\?tutorial=onboarding/);
  await expect(page.locator('[data-home-tour]')).toBeVisible();
  // Saved per stage, with the start term set for them.
  const p = await (await page.request.get('/api/me/profile')).json();
  expect(p.profile.team).toBe('Marketing and outreach');
  expect(p.profile.pronouns).toBe('they/them');
  expect(p.profile.birthday).toBe('04-21');
  expect(p.profile.startTerm).toMatch(TERM);
  await page.getByRole('button', { name: 'Skip tour' }).click();
  await expect(page.locator('[data-home-tour]')).toHaveCount(0);
  // Done for good: the flow no longer opens.
  await page.goto('/onboarding');
  await expect(page).toHaveURL(/\/$/);
});

test('the flow resumes where it stopped, without the intro', async ({ page }) => {
  const id = await newInvitee(page);
  await skipIntro(page);
  await page.fill('#pf-phone', '+1 206 555 0143');
  await page.selectOption('#pf-month', '09');
  await page.selectOption('#pf-day', '02');
  await next(page);
  await stage(page, 2);
  await page.fill('#pf-pronouns', 'she/her');
  await page.locator('[data-finish-later]').click();
  await expect(page).toHaveURL(/\/login/);
  await devSignIn(page, { discord: id });
  await expect(page).toHaveURL(/\/onboarding$/);
  await stage(page, 2);
  await expect(page.locator('.wi')).toHaveCount(0);
});

test('an existing officer sees the intro and intake once, with their details filled in', async ({ page }) => {
  await devSignIn(page, { as: 'officer', user: 'user-priya', onboarding: 'reset' });
  await expect(page).toHaveURL(/\/onboarding$/);
  const before = (await (await page.request.get('/api/me/profile')).json()).profile;
  // Their own Home arrangement: it must come through the flow untouched.
  const layout = [{ id: 'calendar', size: '2x1' }, { id: 'kpi-upcoming', size: '1x1' }, { id: 'links', size: '2x1' }];
  expect((await page.request.put('/api/me/prefs', { data: { widgetLayout: layout } })).ok()).toBe(true);
  await skipIntro(page);
  await stage(page, 1);
  await expect(page.locator('#pf-phone')).toHaveValue(before.phone);
  await next(page);
  await stage(page, 2);
  await next(page);
  await stage(page, 3);
  await next(page);
  await next(page);
  await next(page);
  await expect(page).toHaveURL(/\/\?tutorial=onboarding/);
  await expect(page.locator('[data-home-tour]')).toBeVisible();
  await page.getByRole('button', { name: 'Skip tour' }).click();
  expect((await (await page.request.get('/api/me/prefs')).json()).widgetLayout).toEqual(layout);
  // Their start term did not change, and the next sign-in goes straight to Home.
  const after = (await (await page.request.get('/api/me/profile')).json()).profile;
  expect(after.startTerm).toBe(before.startTerm);
  await devSignIn(page, { as: 'officer', user: 'user-priya' });
  await expect(page).toHaveURL(/\/$/);
});

test('start term: officers cannot edit it, admins can', async ({ page }) => {
  await devLogin(page, 'officer');
  await page.goto('/settings');
  await expect(page.locator('#pf-startTerm')).toHaveCount(0);
  const me = await (await page.request.get('/api/me')).json();
  const own = await page.request.patch(`/api/officers/${me.user.id}`, { data: { startTerm: 'Fall 2020' } });
  expect(own.status()).toBe(403);
  await devLogin(page, 'director');
  const list = await (await page.request.get('/api/officers')).json();
  const target = list.officers.find((o: { id: string; profileComplete: boolean; role: string }) => o.profileComplete && o.role === 'officer');
  const ok = await page.request.patch(`/api/officers/${target.id}`, { data: { startTerm: 'Spring 2025' } });
  expect(ok.ok()).toBe(true);
  expect((await ok.json()).officer.startTerm).toBe('Spring 2025');
});

async function openTour(page: Page, seen: boolean, url = '/?tutorial=onboarding') {
  await devLogin(page, 'officer');
  expect((await page.request.put('/api/me/prefs', { data: { dashboardTutorialSeen: seen } })).ok()).toBe(true);
  await page.goto(url);
}
const tour = (page: Page) => page.locator('[data-home-tour]');
const nextStop = (page: Page) => tour(page).getByRole('button', { name: 'Next', exact: true }).click();

test('the tour opens once, can be skipped (Escape too), and replays from Settings and startTour()', async ({ page }) => {
  await openTour(page, false);
  await expect(tour(page)).toBeVisible();
  await expect(tour(page).locator('h2')).toHaveText('This is Home');
  await page.keyboard.press('Escape');
  await expect(tour(page)).toHaveCount(0);
  expect((await (await page.request.get('/api/me/prefs')).json()).dashboardTutorialSeen).toBe(true);
  await page.goto('/?tutorial=onboarding');
  await expect(tour(page)).toHaveCount(0); // seen: the landing doesn't force it again

  // Replay from Settings (the link there goes to ?tutorial=1) …
  await page.goto('/settings');
  await page.getByRole('link', { name: /Replay dashboard tutorial|Replay dashboard tour/ }).click();
  await expect(tour(page)).toBeVisible();
  await tour(page).getByRole('button', { name: 'Skip tour' }).click();
  await expect(tour(page)).toHaveCount(0);
  // … and startTour() on Home.
  await page.evaluate(() => window.dispatchEvent(new Event('cseed:start-tour')));
  await expect(tour(page)).toBeVisible();
  await expect(tour(page).locator('h2')).toHaveText('This is Home');
});

test('the tour walks every stop, spotlighting its target and previewing edit mode', async ({ page }) => {
  await openTour(page, false);
  const phone = (page.viewportSize()?.width ?? 1440) <= 640;
  const stops: [string, string | null][] = [
    ['This is Home', '[data-tour="greeting"]'],
    ['Your widgets', '[data-tour="grid"]'],
    ['Make it yours', '[data-tour="edit"]'],
    ['Drag to move', '[data-tour="drag"]'],
    ['Resize from the corner', '[data-tour="resize"]'],
    ['Add and remove', '[data-tour="add"]'],
    ['Find your way', phone ? '[data-tour="nav-toggle"]' : '[data-tour="sidebar"]'],
    // The resizable sidebar exists on wide screens only; the stop is skipped elsewhere.
    ...((page.viewportSize()?.width ?? 1440) >= 768 ? ([['Resize the sidebar', '[data-tour="sidebar-resize"]']] as [string, string][]) : []),
    ['Jump anywhere', null],
  ];
  for (const [i, [title, target]] of stops.entries()) {
    await expect(tour(page).locator('h2')).toHaveText(title);
    await expect(tour(page).locator('.home-tour-card')).toHaveAttribute('data-tour-step', String(i + 1));
    if (target) await expect(page.locator(target).first()).toHaveClass(/is-tour-target/);
    if (i < stops.length - 1) await nextStop(page);
  }
  await expect(page.locator('[data-tour="search"].is-tour-target')).toHaveCount(1);
  await tour(page).getByRole('button', { name: 'Back' }).click();
  await expect(tour(page).locator('h2')).toHaveText(stops[stops.length - 2]![0]);
  await nextStop(page);
  await tour(page).getByRole('button', { name: 'Finish' }).click();
  await expect(tour(page)).toHaveCount(0);
  // The edit preview is gone and nothing was saved: the grid is not in edit mode.
  await expect(page.locator('.blocks.is-editing')).toHaveCount(0);
});

test('reduced motion: the intro just fades and the tour spotlight jumps in place', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await newInvitee(page);
  await expect(page.locator('.wi')).toBeVisible();
  await page.keyboard.press('Space');
  // V0.10: the group photo still shows, then a plain fade; no snake.
  await expect(page.locator('[data-wi-photo]')).toBeVisible();
  await expect(page.locator('[data-wi-snake]')).toHaveCount(0);
  await expect(page.locator('.wi')).toHaveCount(0, { timeout: 5000 });
  await stage(page, 1);

  await openTour(page, false);
  await expect(tour(page)).toBeVisible();
  await nextStop(page);
  // No tween: the cutout is already on the grid.
  const hole = await tour(page).locator('mask rect').nth(1).evaluate((e) => ({ x: +e.getAttribute('x')!, y: +e.getAttribute('y')!, w: +e.getAttribute('width')! }));
  const grid = await page.locator('[data-tour="grid"]').boundingBox();
  expect(hole.w).toBeGreaterThan(100);
  expect(Math.abs(hole.x - (grid!.x - 8))).toBeLessThan(10);
  const transition = await tour(page).locator('.home-tour-card').evaluate((e) => getComputedStyle(e).transitionDuration);
  expect(transition === '0s' || transition === '').toBe(true);
});
