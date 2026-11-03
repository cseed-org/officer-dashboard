// Local production-build probe. Start the seeded dev Go server first; never target production.
import { chromium } from '@playwright/test';
import { mkdir, writeFile } from 'node:fs/promises';
const origin = process.env.WEB_ORIGIN ?? 'http://127.0.0.1:18135';
if (!['localhost', '127.0.0.1', '[::1]'].includes(new URL(origin).hostname)) throw new Error('Performance probe requires a local seeded server');
const out = process.env.PERF_OUT ?? 'tmp/verify/motion';
await mkdir(out, { recursive: true });
const browser = await chromium.launch({ headless: true });
const results = [];
for (const route of ['/', '/events', '/members', '/finance', '/design', '/officers']) {
 const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
 const page = await context.newPage();
 await context.request.get(`${origin}/auth/dev?as=director`, { maxRedirects: 0 });
 const cdp = await context.newCDPSession(page);
 await cdp.send('Emulation.setCPUThrottlingRate', { rate: 4 });
 await cdp.send('Network.enable');
 await cdp.send('Network.emulateNetworkConditions', { offline: false, latency: 40, downloadThroughput: 1_600_000 / 8, uploadThroughput: 750_000 / 8 });
 await page.addInitScript(() => {
  window.probe = { lcp: 0, cls: 0, interaction: 0 };
  new PerformanceObserver(list => { for (const e of list.getEntries()) window.probe.lcp = e.startTime; }).observe({ type: 'largest-contentful-paint', buffered: true });
  new PerformanceObserver(list => { for (const e of list.getEntries()) if (!e.hadRecentInput) window.probe.cls += e.value; }).observe({ type: 'layout-shift', buffered: true });
  new PerformanceObserver(list => { for (const e of list.getEntries()) if (e.interactionId) window.probe.interaction = Math.max(window.probe.interaction, e.duration); }).observe({ type: 'event', buffered: true, durationThreshold: 16 });
 });
 await page.goto(origin + route);
 await page.waitForLoadState('networkidle');
 await page.getByRole('button', { name: 'Search (Ctrl K)', exact: true }).click();
 await page.keyboard.press('Escape');
 await page.waitForTimeout(250);
 results.push({ route, ...await page.evaluate(() => ({ ...window.probe, jsBytes: performance.getEntriesByType('resource').filter(e => e.name.endsWith('.js')).reduce((n,e) => n + e.encodedBodySize, 0) })) });
 if (process.env.PERF_SHOTS !== '0') for (const width of [1440, 390]) for (const theme of ['light', 'dark']) {
  await page.setViewportSize({ width, height: 900 });
  await page.evaluate(theme => { localStorage.setItem('cseed-theme', theme); document.documentElement.dataset.theme = theme; }, theme);
  await page.screenshot({ path: `${out}/${route.slice(1) || 'home'}-${width}-${theme}.png`, fullPage: true });
 }
 await context.close();
}
await browser.close();
await writeFile(`${out}/metrics.json`, JSON.stringify(results, null, 2));
console.table(results);
