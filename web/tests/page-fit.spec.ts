import { expect, test } from '@playwright/test';
import { devSignIn, settle } from './helpers';

// V0.10: every non-home page fits the viewport (no sideways scroll, nothing pushed past the edge),
// and the StatRow tiles are Home's widget cards. Runs in the phone / tablet / desktop projects.
const PAGES = ['/buildspace', '/events', '/finance', '/settings', '/design', '/officers', '/members', '/contacts'];

for (const path of PAGES) {
  test(`${path} fits the viewport`, async ({ page }) => {
    const res = await devSignIn(page, { as: 'director' }, path);
    expect(res?.ok(), 'dev login failed; is the Go server running with DEV_AUTH=1?').toBe(true);
    await settle(page);
    const m = await page.evaluate(() => ({ scroll: document.documentElement.scrollWidth, inner: window.innerWidth }));
    expect(m.scroll, `${path}: scrollWidth ${m.scroll} > innerWidth ${m.inner}`).toBeLessThanOrEqual(m.inner);
    // Nothing in the page's own flow sticks out of the right edge (scroll containers clip their own overflow).
    const clipped = await page.evaluate(() => {
      const inner = window.innerWidth;
      const out: string[] = [];
      const scrolls = (el: Element | null) => {
        for (; el && el !== document.body; el = el.parentElement) {
          const o = getComputedStyle(el).overflowX;
          if (o === 'auto' || o === 'scroll' || o === 'hidden' || o === 'clip') return true;
        }
        return false;
      };
      for (const el of document.querySelectorAll('main *')) {
        const r = el.getBoundingClientRect();
        if (r.width === 0 || r.height === 0 || r.right <= inner + 1) continue;
        const cs = getComputedStyle(el);
        if (cs.position === 'fixed' || cs.visibility === 'hidden') continue;
        if (!scrolls(el.parentElement)) out.push(`${el.tagName.toLowerCase()}.${String((el as HTMLElement).className).split(' ')[0]} right=${Math.round(r.right)}`);
      }
      return out.slice(0, 5);
    });
    expect(clipped, `${path}: elements past the right edge`).toEqual([]);
  });
}

test('StatRow tiles are styled like a Home widget card', async ({ page }) => {
  await devSignIn(page, { as: 'director' }, '/');
  await settle(page);
  const card = page.locator('[data-block].wc').first();
  await expect(card).toBeVisible();
  const style = (loc: typeof card) => loc.evaluate((el) => {
    const cs = getComputedStyle(el);
    return { radius: cs.borderRadius, border: cs.borderTopWidth + cs.borderTopStyle + cs.borderTopColor, shadow: cs.boxShadow, padding: cs.padding, title: getComputedStyle(el.querySelector('.wc-title')!).fontSize };
  });
  const home = await style(card);
  for (const path of ['/events', '/finance', '/buildspace', '/settings']) {
    await page.goto(path);
    await settle(page);
    const tiles = page.locator('[data-stat-row] .stat-tile');
    expect(await tiles.count()).toBeGreaterThanOrEqual(3);
    for (let i = 0; i < (await tiles.count()); i++) {
      const tile = tiles.nth(i);
      await expect(tile.locator('.wc-chip svg')).toBeVisible();
      expect(await style(tile), `${path} tile ${i}`).toEqual(home);
    }
  }
});
