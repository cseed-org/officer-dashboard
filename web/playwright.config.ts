import { defineConfig } from '@playwright/test';

/**
 * Runs against the web/ dev server on :4340, which proxies /api and /auth to API_ORIGIN
 * (v1 app on :4330 by default, the Go server on :8090 later). Seed data must be loaded
 * there. Phone, tablet and desktop; light and dark are covered inside the tests.
 */
const PORT = Number(process.env.WEB_PORT ?? 4340);
/** Where the UI under test is served: the dev server by default, or e.g. the Go server (WEB_ORIGIN=http://127.0.0.1:8090). */
const ORIGIN = process.env.WEB_ORIGIN ?? `http://localhost:${PORT}`;

export default defineConfig({
  testDir: 'tests',
  outputDir: 'test-results',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 120_000,
  expect: { timeout: 15_000 },
  reporter: [['list']],
  use: {
    baseURL: ORIGIN,
    trace: 'retain-on-failure',
    locale: 'en-US',
    timezoneId: 'America/Los_Angeles',
    launchOptions: { args: ['--lang=en-US'], env: { ...process.env, LANG: 'en_US.UTF-8', LC_ALL: 'en_US.UTF-8' } },
  },
  projects: [
    { name: 'phone', use: { browserName: 'chromium', viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, isMobile: true, hasTouch: true } },
    { name: 'tablet', use: { browserName: 'chromium', viewport: { width: 820, height: 1180 }, deviceScaleFactor: 2, isMobile: true, hasTouch: true } },
    { name: 'desktop', use: { browserName: 'chromium', viewport: { width: 1440, height: 900 }, deviceScaleFactor: 1 } },
  ],
  // With WEB_ORIGIN set, something else serves the UI: don't start the dev server.
  webServer: process.env.WEB_ORIGIN ? undefined : {
    command: `npx astro dev --port ${PORT} --ignore-lock`,
    url: `${ORIGIN}/login`,
    reuseExistingServer: true,
    timeout: 120_000,
    stdout: 'ignore',
    stderr: 'pipe',
  },
});
