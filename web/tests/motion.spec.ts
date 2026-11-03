import { expect, test } from '@playwright/test';
import { devLogin, settle } from './helpers';

test('reduced motion: no entry animations and KPI figures appear at their final value', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop', 'one viewport is enough');
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await devLogin(page, 'director');
  await settle(page);
  const kpi = page.locator('.kpi-value').first();
  await expect(kpi).toBeVisible();
  // No count-up: the visible figure is the final one straight away (no aria-hidden tween span).
  await expect(kpi.locator('[aria-hidden="true"]')).toHaveCount(0);
  const anim = await page.evaluate(() =>
    ['.page-enter', '.progress-fill', '.kpi', '.async-content', '.dt tbody tr', '.notice'].map((s) => {
      const el = document.querySelector(s);
      return el ? getComputedStyle(el).animationName : 'none';
    }),
  );
  expect(anim.every((a) => a === 'none')).toBe(true);
});

test('motion on: the tab underline slides with transform only', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop', 'one viewport is enough');
  await devLogin(page, 'director');
  await page.goto('/finance');
  await settle(page);
  const bar = page.locator('.tabs .tab-indicator');
  const before = await bar.evaluate((el) => getComputedStyle(el).transform);
  await page.getByRole('tab', { name: 'budgets' }).click();
  await expect.poll(() => bar.evaluate((el) => getComputedStyle(el).transform)).not.toBe(before);
  expect(await bar.evaluate((el) => getComputedStyle(el).transitionProperty)).toBe('transform');
});

test('drawer closes with Escape without blocking the page', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop', 'shared modal behavior');
  await devLogin(page, 'director');
  await page.goto('/events?id=event-hack-night');
  await settle(page);
  await expect(page.locator('.drawer[open]')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.locator('.drawer[open]')).toHaveCount(0);
  await expect(page.locator('html')).not.toHaveClass(/has-modal/);
  await expect(page.getByLabel('Search events')).toBeEnabled();
});

test('first content frame stays paintable, shell holds still, and item stagger stops at eight', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop', 'shared CSS contract');
  await devLogin(page, 'director');
  await page.goto('/events');
  await settle(page);
  await page.addStyleTag({ content: '.page-enter,.async-content,.dt tbody tr {animation-play-state:paused!important;animation-delay:0s!important;}' });
  // Reinsert the animation to inspect its first frame rather than its settled end state.
  const result = await page.evaluate(() => {
    const panel = document.querySelector<HTMLElement>('.page-enter')!;
    panel.style.animation = 'none'; void panel.offsetWidth; panel.style.animation = '';
    const body = document.querySelector('.dt tbody')!;
    while (body.children.length < 10) body.append(body.firstElementChild!.cloneNode(true));
    const rows = [...body.children] as HTMLElement[];
    rows.forEach(row => { row.style.animation = 'none'; });
    void (body as HTMLElement).offsetWidth;
    rows.forEach(row => { row.style.animation = ''; });
    return {
      pageOpacity: Number(getComputedStyle(panel).opacity),
      pageTransform: getComputedStyle(panel).transform,
      shellAnimation: getComputedStyle(document.querySelector('.sidebar')!).animationName,
      rowOpacity: rows.slice(0, 8).map(row => Number(getComputedStyle(row).opacity)),
      beyondCap: rows.slice(8).map(row => getComputedStyle(row).animationName),
    };
  });
  expect(result.pageOpacity).toBeGreaterThan(0);
  expect(result.pageOpacity).toBeLessThan(1);
  expect(result.pageTransform).not.toBe('none');
  expect(result.shellAnimation).toBe('none');
  expect(result.rowOpacity.every(opacity => opacity > 0 && opacity < 1)).toBe(true);
  expect(result.beyondCap.every(name => name === 'none')).toBe(true);
});

test('skeleton exits over content without changing the ready box size', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop', 'shared Async boundary');
  await devLogin(page, 'director');
  await settle(page);
  let release!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; });
  await page.route('**/api/requests?*', async route => {
    const response = await route.fetch();
    await gate;
    await route.fulfill({ response });
  });
  await page.goto('/finance');
  await expect(page.locator('.async-state[data-state="loading"]').first()).toBeVisible();
  // Install before release so the short transition cannot be missed by test polling.
  await page.evaluate(() => {
    const values: { height: number; inert: boolean; hidden: string | null }[] = [];
    (window as any).crossfadeValues = values;
    new MutationObserver(() => {
      const overlay = document.querySelector<HTMLElement>('.async-outgoing');
      if (overlay) values.push({ height: overlay.parentElement!.getBoundingClientRect().height, inert: overlay.inert, hidden: overlay.getAttribute('aria-hidden') });
    }).observe(document.querySelector('main')!, { childList: true, subtree: true });
  });
  release();
  await settle(page);
  const samples = await page.evaluate(() => (window as any).crossfadeValues as { height: number; inert: boolean; hidden: string }[]);
  expect(samples.length).toBeGreaterThan(0);
  expect(samples.every(sample => sample.inert && sample.hidden === 'true')).toBe(true);
  const finalHeight = await page.locator('.async-state[data-state="ready"]').first().evaluate(el => el.getBoundingClientRect().height);
  expect(Math.abs(finalHeight - samples[0]!.height)).toBeLessThan(1);
  await expect(page.locator('.async-outgoing')).toHaveCount(0);
});

test('Home commits primary widgets together and reuses the auth preference read', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop', 'shared loading boundary');
  let release!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; });
  let preferenceReads = 0;
  page.on('request', request => {
    if (new URL(request.url()).pathname === '/api/me/prefs' && request.method() === 'GET') preferenceReads++;
  });
  await page.route('**/api/overview', async route => {
    const response = await route.fetch();
    await gate;
    await route.fulfill({ response });
  });
  await devLogin(page, 'director');
  await expect(page.getByRole('status').filter({ hasText: 'Loading home' })).toBeVisible();
  const widgets = page.getByRole('list', { name: 'Home widgets' });
  await expect(widgets).toHaveCount(0);
  release();
  await expect(widgets).toBeVisible();
  const positions = () => widgets.locator(':scope > li').evaluateAll(els => els.map(el => ({ top: (el as HTMLElement).offsetTop, height: (el as HTMLElement).offsetHeight })));
  const first = await positions();
  await settle(page);
  await page.waitForLoadState('networkidle');
  expect(await positions()).toEqual(first);
  expect(preferenceReads).toBe(1);
});

test('Members commits connection notices with the initial directory', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop', 'shared loading boundary');
  await devLogin(page, 'director');
  await settle(page);
  let release!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; });
  await page.route('**/api/members?*', async route => {
    const response = await route.fetch();
    await gate;
    await route.fulfill({ response });
  });
  await page.goto('/members');
  await expect(page.getByRole('status').filter({ hasText: 'Loading members' })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Member directory' })).toHaveCount(0);
  release();
  const directory = page.getByRole('region', { name: 'Member directory' });
  await expect(directory).toBeVisible();
  await expect(page.getByText('Cloudflare is not connected.', { exact: false })).toBeVisible();
  await settle(page);
});
