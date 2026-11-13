import { test, expect, type Page } from '@playwright/test';
import { devLoginSkipTour, watchErrors } from './helpers';

/**
 * V0.9 resizable sidebar. Desktop and tablet only: on a phone the menu is a sheet and there is
 * no handle. Each test resets the saved width first so an earlier run cannot leak in.
 */
const sidebar = (page: Page) => page.locator('#sidebar');
const handle = (page: Page) => page.getByRole('separator', { name: 'Resize sidebar' });
const width = async (page: Page) => Math.round((await sidebar(page).boundingBox())!.width);
const savedWidth = async (page: Page) => (await (await page.request.get('/api/me/prefs')).json()).sidebarWidth as number;
async function resetWidth(page: Page) {
  const r = await page.request.put('/api/me/prefs', { data: { sidebarWidth: 0 } });
  expect(r.ok()).toBe(true);
  await page.evaluate(() => { try { localStorage.removeItem('cseed-sidebar'); localStorage.removeItem('cseed-sidebar-last'); } catch { /* */ } });
  await page.reload();
}
/** Drag the handle to viewport x. */
async function dragTo(page: Page, x: number) {
  const b = (await handle(page).boundingBox())!;
  const y = b.y + 200;
  await page.mouse.move(b.x + b.width / 2, y);
  await page.mouse.down();
  await page.mouse.move(x, y, { steps: 8 });
  await page.mouse.up();
}

test.describe('resizable sidebar', () => {
  test.beforeEach(async ({ page }, info) => {
    test.skip(info.project.name === 'phone', 'The phone menu is a sheet; resizing is desktop and tablet only');
    await devLoginSkipTour(page, 'officer');
    await resetWidth(page);
  });

  test('dragging resizes freely and the width follows the user to a new browser profile', async ({ page }) => {
    const errors = watchErrors(page);
    await dragTo(page, 300);
    expect(await width(page)).toBe(300);
    await expect(handle(page)).toHaveAttribute('aria-valuenow', '300');
    await expect(page.locator('.nav-label').first()).toBeVisible();
    await expect.poll(() => savedWidth(page)).toBe(300);
    // Never beyond the maximum.
    await dragTo(page, 700);
    expect(await width(page)).toBe(320);
    await expect.poll(() => savedWidth(page)).toBe(320);

    // Reload with no local cache: the server preference applies, and the width is set before
    // first paint (the DOM shows it as soon as the document is parsed).
    await page.evaluate(() => localStorage.removeItem('cseed-sidebar'));
    await page.reload();
    await expect.poll(() => width(page)).toBe(320);
    expect(errors.filter((e) => !/\b(40[134]|404)\b/.test(e))).toEqual([]);
  });

  test('dragged to the minimum, only icons show and stay clickable with tooltips', async ({ page }) => {
    await dragTo(page, 10);
    expect(await width(page)).toBe(64);
    await expect(handle(page)).toHaveAttribute('aria-valuenow', '64');
    await expect(page.locator('#sidebar .nav-label').first()).toBeHidden();
    const links = page.locator('#sidebar .nav-list .nav-link');
    expect(await links.count()).toBeGreaterThan(3);
    for (let i = 0; i < (await links.count()); i++) {
      const l = links.nth(i);
      await expect(l.locator('svg').first()).toBeVisible();
      await expect(l).toHaveAttribute('title', /.+/);
    }
    await expect(page.locator('#sidebar .search-trigger')).toBeVisible();
    await page.locator('#sidebar [data-nav="events"]').click();
    await expect(page).toHaveURL(/\/events/);
    expect(await width(page)).toBe(64);
    // Below the snap threshold counts as icons only; just above it keeps labels.
    await dragTo(page, 110);
    expect(await width(page)).toBe(64);
    await dragTo(page, 130);
    expect(await width(page)).toBe(130);
  });

  test('the width survives a reload and applies before the page paints', async ({ page }) => {
    await dragTo(page, 10);
    await expect.poll(() => savedWidth(page)).toBe(64);
    await page.reload({ waitUntil: 'commit' });
    // Inline script: the preference is on <html> before the sidebar's first layout.
    await page.waitForFunction(() => document.documentElement.dataset.sidebar === 'rail');
    await expect.poll(() => width(page)).toBe(64);
    await expect(page.locator('#sidebar .nav-label').first()).toBeHidden();
  });

  test('keyboard: focusable separator, arrows resize, Home/End/Enter jump', async ({ page }) => {
    const h = handle(page);
    await expect(h).toHaveAttribute('aria-orientation', 'vertical');
    await expect(h).toHaveAttribute('aria-valuemin', '64');
    await expect(h).toHaveAttribute('aria-valuemax', '320');
    await h.focus();
    const start = await width(page);
    await page.keyboard.press(start >= 320 ? 'ArrowLeft' : 'ArrowRight');
    // From icons only (the tablet default) the first step opens to 120; otherwise it is 16px.
    expect(await width(page)).toBe(start >= 320 ? start - 16 : start <= 64 ? 120 : start + 16);
    await page.keyboard.press('Home');
    expect(await width(page)).toBe(64);
    await expect(h).toHaveAttribute('aria-valuenow', '64');
    await page.keyboard.press('ArrowRight');
    expect(await width(page)).toBe(120); // out of icons-only straight to a width that fits labels
    await page.keyboard.press('ArrowLeft');
    expect(await width(page)).toBe(64);
    await page.keyboard.press('End');
    expect(await width(page)).toBe(320);
    await page.keyboard.press('Shift+ArrowLeft');
    expect(await width(page)).toBe(272);
    await expect.poll(() => savedWidth(page)).toBe(272);
    await page.keyboard.press('Enter');
    expect(await width(page)).toBe(64);
    await page.keyboard.press('Enter');
    expect(await width(page)).toBe(272);
  });

  test('the handle sits on the sidebar edge at rest, resized and after a breakpoint change', async ({ page }) => {
    const gap = async () => {
      const h = (await handle(page).boundingBox())!;
      const edge = (await sidebar(page).boundingBox())!.width;
      return Math.abs(h.x + h.width / 2 - edge);
    };
    expect(await gap()).toBeLessThanOrEqual(1);
    await dragTo(page, 260);
    expect(await gap()).toBeLessThanOrEqual(1);
    await dragTo(page, 10);
    expect(await gap()).toBeLessThanOrEqual(1);
    // Back to the stylesheet default across a breakpoint (tablet rail <-> desktop 232px).
    await page.request.put('/api/me/prefs', { data: { sidebarWidth: 0 } });
    await page.evaluate(() => localStorage.removeItem('cseed-sidebar'));
    await page.reload();
    expect(await gap()).toBeLessThanOrEqual(1);
    const vp = page.viewportSize()!;
    await page.setViewportSize({ width: vp.width >= 1100 ? 900 : 1300, height: vp.height });
    expect(await gap()).toBeLessThanOrEqual(1);
  });

  for (const cache of ['none', 'stale', 'matching'] as const) {
    test(`a server-saved width is the first thing painted (local cache: ${cache})`, async ({ page }) => {
      await page.request.put('/api/me/prefs', { data: { sidebarWidth: 300 } });
      await page.addInitScript((c) => {
        try {
          if (c === 'none') localStorage.removeItem('cseed-sidebar');
          else localStorage.setItem('cseed-sidebar', c === 'stale' ? '150' : '300');
        } catch { /* */ }
        // Every frame in which the shell is visible: its sidebar width and where the content starts.
        (window as any).__frames = [];
        const tick = () => {
          const app = document.querySelector('.app') as HTMLElement | null;
          const sb = document.getElementById('sidebar');
          if (app && sb && getComputedStyle(app).visibility !== 'hidden') (window as any).__frames.push(Math.round(sb.getBoundingClientRect().width));
          requestAnimationFrame(tick);
        };
        requestAnimationFrame(tick);
      }, cache);
      // Hold the preferences back so a late jump would be visible.
      await page.route('**/api/me/prefs', async (route) => {
        if (route.request().method() !== 'GET') return route.continue();
        await new Promise((r) => setTimeout(r, 700));
        await route.continue();
      });
      await page.goto('/events');
      await expect(page.locator('.app')).toBeVisible(); // held hidden until the preferences answer
      await expect.poll(() => width(page)).toBe(300);
      await page.waitForTimeout(300);
      const frames: number[] = await page.evaluate(() => (window as any).__frames);
      expect(frames.length).toBeGreaterThan(0);
      expect([...new Set(frames)], 'every visible frame uses the saved width').toEqual([300]);
    });
  }

  test('a prefs response after the shell was revealed does not resize this page; the next navigation paints it', async ({ page }) => {
    await page.request.put('/api/me/prefs', { data: { sidebarWidth: 300 } });
    await page.addInitScript(() => {
      try { localStorage.setItem('cseed-sidebar', '150'); } catch { /* */ }
      (window as any).__frames = [];
      const tick = () => {
        const app = document.querySelector('.app') as HTMLElement | null;
        const sb = document.getElementById('sidebar');
        if (app && sb && getComputedStyle(app).visibility !== 'hidden') (window as any).__frames.push(Math.round(sb.getBoundingClientRect().width));
        requestAnimationFrame(tick);
      };
      requestAnimationFrame(tick);
    });
    let hold = true;
    await page.route('**/api/me/prefs', async (route) => {
      if (route.request().method() !== 'GET' || !hold) return route.continue();
      await new Promise((r) => setTimeout(r, 2500)); // past the 1.5s reveal timeout
      await route.continue();
    });
    await page.goto('/events');
    await expect(page.locator('.app')).toBeVisible();
    const revealed = await width(page);
    expect(revealed).toBe(150); // the cached width, shown at the timeout
    await page.waitForTimeout(2500); // the response lands now
    expect(await width(page), 'not resized under the user').toBe(150);
    const frames: number[] = await page.evaluate(() => (window as any).__frames);
    expect([...new Set(frames)]).toEqual([150]);
    expect(await page.evaluate(() => localStorage.getItem('cseed-sidebar'))).toBe('300');
    // The next navigation paints the server width from the first frame.
    hold = false;
    await page.addInitScript(() => { (window as any).__frames = []; });
    await page.goto('/events');
    await expect(page.locator('.app')).toBeVisible();
    expect(await width(page)).toBe(300);
    const next: number[] = await page.evaluate(() => (window as any).__frames);
    expect([...new Set(next)]).toEqual([300]);
  });

  test('a server reset updates the hydrated handle, its ARIA value and the next key press', async ({ page }, info) => {
    test.skip(info.project.name !== 'desktop', 'tablet defaults to the rail, so a reset is not a visible change there');
    await dragTo(page, 10);
    await expect.poll(() => savedWidth(page)).toBe(64);
    await expect(handle(page)).toHaveAttribute('aria-valuenow', '64');
    // Another device reset the preference: the next refresh (window focus) brings 0.
    await page.route('**/api/me/prefs', async (route) => {
      if (route.request().method() !== 'GET') return route.continue();
      const res = await route.fetch();
      await route.fulfill({ response: res, json: { ...(await res.json()), sidebarWidth: 0 } });
    });
    await page.evaluate(() => window.dispatchEvent(new Event('focus')));
    await expect.poll(() => width(page)).toBe(232);
    await expect(handle(page)).toHaveAttribute('aria-valuenow', '232');
    await handle(page).focus();
    await page.keyboard.press('ArrowRight');
    expect(await width(page)).toBe(248);
  });

  test('double-click toggles icons only and the last width', async ({ page }) => {
    await dragTo(page, 260);
    expect(await width(page)).toBe(260);
    await handle(page).dblclick();
    expect(await width(page)).toBe(64);
    await handle(page).dblclick();
    expect(await width(page)).toBe(260);
  });

  test('page content never overflows at the extremes', async ({ page }) => {
    for (const x of [10, 400]) {
      await dragTo(page, x);
      const over = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      expect(over).toBeLessThanOrEqual(0);
    }
  });
});

test('phone: no resize handle and the menu sheet is unchanged', async ({ page }, info) => {
  test.skip(info.project.name !== 'phone', 'phone only');
  await devLoginSkipTour(page, 'officer');
  await expect(handle(page)).toBeHidden();
  await page.getByRole('button', { name: 'Menu' }).click();
  await expect(sidebar(page)).toBeVisible();
  expect(Math.round((await sidebar(page).boundingBox())!.width)).toBeLessThanOrEqual(300);
  await expect(page.locator('#sidebar .nav-label').first()).toBeVisible();
});
