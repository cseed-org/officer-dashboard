import { defineConfig } from '@playwright/test';

/** 4321 is the UI's dev port; the backend Pane uses 4322. Override with E2E_PORT. */
const PORT = Number(process.env.E2E_PORT ?? 4321);

/**
 * Three viewports from the CSEED brief (mirrors cseed.co's scripts/check-mobile.mjs):
 * phone and tablet are emulated as touch devices, desktop as a mouse device.
 * Light and dark are covered inside the tests via emulateMedia.
 */
export default defineConfig({
  testDir: 'tests/e2e',
  outputDir: 'test-results',
  fullyParallel: false,
  workers: 1,
  retries: process.env.CI ? 1 : 0,
  timeout: 120_000,
  reporter: [['list'], ['html', { open: 'never', outputFolder: 'playwright-report' }]],
  use: {
    baseURL: `http://localhost:${PORT}`,
    trace: 'retain-on-failure',
  },
  projects: [
    {
      name: 'phone',
      use: { browserName: 'chromium', viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, isMobile: true, hasTouch: true },
    },
    {
      name: 'tablet',
      use: { browserName: 'chromium', viewport: { width: 820, height: 1180 }, deviceScaleFactor: 2, isMobile: true, hasTouch: true },
    },
    {
      name: 'desktop',
      use: { browserName: 'chromium', viewport: { width: 1440, height: 900 }, deviceScaleFactor: 1 },
    },
  ],
  webServer: {
    // Migrate + seed local D1, then run the dev server in the foreground
    // (`--ignore-lock` stops Astro auto-backgrounding when it detects an agent).
    command: `npm run db:migrate:local && npm run db:seed:local && node scripts/ensure-dev-vars.mjs && npx astro dev --port ${PORT} --ignore-lock`,
    url: `http://localhost:${PORT}/api/health`,
    reuseExistingServer: !process.env.CI,
    timeout: 240_000,
    stdout: 'ignore',
    stderr: 'pipe',
  },
});
