// Start a local seeded Go dev binary serving a production Astro build first.
import { chromium } from '@playwright/test';
import { mkdir, writeFile } from 'node:fs/promises';
const origin = process.env.WEB_ORIGIN ?? 'http://127.0.0.1:18135';
if (!['localhost', '127.0.0.1', '[::1]'].includes(new URL(origin).hostname)) throw new Error('Performance probe requires a local seeded server');
const out = process.env.PERF_OUT ?? 'tmp/verify/motion';
const runs = Number(process.env.PERF_RUNS ?? 3);
if (!Number.isInteger(runs) || runs < 1) throw new Error('PERF_RUNS must be a positive integer');
const routes = (process.env.PERF_ROUTES ?? '/,/events,/members,/finance,/design,/officers').split(',');
await mkdir(out, { recursive: true });
const browser = await chromium.launch({ headless: true });
const samples = [];
for (let run = 1; run <= runs; run++) for (const route of routes) {
 const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
 const login = await context.request.post(`${origin}/auth/dev`, { form: { as: 'director' }, maxRedirects: 0 });
 if (login.status() !== 303) throw new Error(`Dev sign-in failed: ${login.status()}`);
 // Measure the returning-user dashboard; first-run coachmarks have their own browser checks.
 const prefs = await context.request.put(`${origin}/api/me/prefs`, { data: { dashboardTutorialSeen: true } });
 if (!prefs.ok()) throw new Error(`Preference setup failed: ${prefs.status()}`);
 const page = await context.newPage();
 const cdp = await context.newCDPSession(page);
 await cdp.send('Emulation.setCPUThrottlingRate', { rate: 4 });
 await cdp.send('Network.enable');
 await cdp.send('Network.setCacheDisabled', { cacheDisabled: true });
 await cdp.send('Network.emulateNetworkConditions', { offline: false, latency: 40, downloadThroughput: 1_600_000 / 8, uploadThroughput: 750_000 / 8 });
 await page.addInitScript(() => {
  window.probe = { lcp: 0, cls: 0, interaction: 0, lcpElement: '', shifts: [] };
  new PerformanceObserver(list => { for (const e of list.getEntries()) { window.probe.lcp = e.startTime; window.probe.lcpElement = e.element?.outerHTML.slice(0, 250) ?? ''; } }).observe({ type: 'largest-contentful-paint', buffered: true });
  new PerformanceObserver(list => { for (const e of list.getEntries()) if (!e.hadRecentInput) { window.probe.cls += e.value; window.probe.shifts.push({ value: e.value, time: e.startTime, sources: e.sources?.map(s => ({ node: s.node?.outerHTML?.slice(0, 250), before: s.previousRect.toJSON(), after: s.currentRect.toJSON() })) }); } }).observe({ type: 'layout-shift', buffered: true });
  new PerformanceObserver(list => { for (const e of list.getEntries()) if (e.interactionId) window.probe.interaction = Math.max(window.probe.interaction, e.duration); }).observe({ type: 'event', buffered: true, durationThreshold: 16 });
 });
 await page.goto(origin + route);
 await page.waitForFunction(() => !document.querySelector('astro-island[ssr], main [aria-busy="true"]'));
 await page.waitForLoadState('networkidle');
 // Include the old 600ms KPI tween as well as all 120–240ms motion before first input.
 await page.waitForTimeout(750);
 await page.getByRole('button', { name: 'Search (Ctrl K)', exact: true }).click();
 await page.locator('.palette[open]').waitFor();
 await page.keyboard.press('Escape');
 await page.locator('.palette[open]').waitFor({ state: 'hidden' });
 await page.waitForTimeout(250);
 samples.push({ route, run, ...await page.evaluate(() => ({ ...window.probe, jsBytes: performance.getEntriesByType('resource').filter(e => e.name.endsWith('.js')).reduce((n,e) => n + e.encodedBodySize, 0) })) });
 if (run === runs && process.env.PERF_SHOTS === '1') for (const width of [1440, 390]) for (const theme of ['light', 'dark']) {
  await page.setViewportSize({ width, height: 900 });
  await page.evaluate(theme => { localStorage.setItem('cseed-theme', theme); document.documentElement.dataset.theme = theme; }, theme);
  await page.screenshot({ path: `${out}/${route.slice(1) || 'home'}-${width}-${theme}.png`, fullPage: true });
 }
 await context.close();
}
await browser.close();
const median = values => values.sort((a,b) => a-b)[Math.floor(values.length / 2)];
const medians = routes.map(route => ({ route, ...Object.fromEntries(['lcp','cls','interaction','jsBytes'].map(key => [key, median(samples.filter(s => s.route === route).map(s => s[key]))])) }));
await writeFile(`${out}/metrics.json`, JSON.stringify({ profile: { runs, cpuSlowdown: 4, latencyMs: 40, downBitsPerSecond: 1600000, upBitsPerSecond: 750000, cache: 'disabled', settleMs: 750 }, medians, samples }, null, 2));
console.table(medians);
