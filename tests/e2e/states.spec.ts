import { expect, test, type Page, type Route } from '@playwright/test';
import { layoutFailures, measure } from './measure';
import { devLogin, shot } from './helpers';

/**
 * Empty, error and loading states. The real API is intercepted with page.route, so the app
 * itself carries no test switches. Empty bodies are valid contract responses.
 */
const EMPTY: Record<string, unknown> = {
  '/api/overview': {
    fiscalYear: 2027,
    kpis: {
      upcomingEvents: 0,
      avgCheckInRate: null,
      avgReflectionRate: null,
      budgetCents: 0,
      spentCents: 0,
      committedCents: 0,
      remainingCents: 0,
      pendingRequests: 0,
      pendingRequestsCents: 0,
    },
    recentEvents: [],
  },
  '/api/events': { items: [], nextCursor: null },
  '/api/requests': { items: [], nextCursor: null, counts: {} },
  '/api/finance/summary': {
    fiscalYear: 2027,
    asOf: '2026-10-31',
    totals: { budgetCents: 0, spentCents: 0, committedCents: 0, remainingCents: 0 },
    byCategory: [],
    funds: [],
  },
  '/api/admin/tokens': { tokens: [] },
};
const PAGES: Record<string, string> = {
  '/': '/api/overview',
  '/events': '/api/events',
  '/requests': '/api/requests',
  '/finance': '/api/finance/summary',
  '/admin/tokens': '/api/admin/tokens',
};

type State = 'empty' | 'error' | 'loading';

async function intercept(page: Page, endpoint: string, state: State) {
  await page.unrouteAll({ behavior: 'ignoreErrors' });
  await page.route(
    (url) => url.pathname === endpoint,
    async (route: Route) => {
      if (state === 'loading') return; // never answered: the skeleton stays up
      if (state === 'error')
        return route.fulfill({ status: 500, json: { error: { code: 'internal', message: 'The server had a problem loading this.' } } });
      return route.fulfill({ status: 200, json: EMPTY[endpoint] });
    },
  );
}

for (const scheme of ['light', 'dark'] as const) {
  test(`empty, error and loading states render cleanly (${scheme})`, async ({ page }, info) => {
    await devLogin(page, 'director');
    await page.emulateMedia({ colorScheme: scheme });
    const problems: string[] = [];
    for (const [route, endpoint] of Object.entries(PAGES)) {
      for (const state of ['empty', 'error', 'loading'] as const) {
        await intercept(page, endpoint, state);
        await page.goto(route, { waitUntil: 'domcontentloaded' });
        await page.evaluate(() => document.fonts.ready);
        if (state === 'loading') await expect(page.locator('[aria-busy="true"]').first()).toBeVisible();
        else await expect(page.locator(`[data-state="${state}"]`).first()).toBeVisible({ timeout: 15_000 });
        if (state === 'error') await expect(page.getByRole('button', { name: 'try again' }).first()).toBeVisible();
        problems.push(...layoutFailures(await page.evaluate(measure)).map((f) => `${route} ${state}: ${f}`));
        await shot(page, info.project.name, scheme, `states/${route === '/' ? 'overview' : route.slice(1).replaceAll('/', '_')}-${state}`);
      }
    }
    await page.unrouteAll({ behavior: 'ignoreErrors' });
    expect(problems, problems.join('\n')).toEqual([]);
  });
}

test('a receipt upload when Drive is not configured explains what to do', async ({ page }) => {
  await devLogin(page, 'officer');
  await page.route(/\/api\/requests\/[^/]+\/attachments$/, (route) =>
    route.fulfill({ status: 503, json: { error: { code: 'internal', message: 'Receipt storage is not configured' } } }),
  );
  await page.goto('/requests/req-0001');
  await expect(page.locator('[data-state="ready"]')).toBeVisible({ timeout: 15_000 });
  await page.getByLabel('add files').setInputFiles([{ name: 'receipt.pdf', mimeType: 'application/pdf', buffer: Buffer.from('%PDF-1.4') }]);
  await expect(page.getByText("Receipt storage isn't connected")).toBeVisible();
});

test('a request id that does not exist shows not found', async ({ page }) => {
  await devLogin(page, 'officer');
  await page.goto('/requests/req-nope');
  await expect(page.locator('[data-state="error"]')).toContainText('Not found');
  await page.goto('/events/event-nope');
  await expect(page.locator('[data-state="error"]')).toContainText('Not found');
});

test('a held agent settlement shows the attempted vs approved amounts', async ({ page }) => {
  await devLogin(page, 'director');
  await page.route(/\/api\/requests\/req-0004$/, async (route) => {
    const res = await route.fetch();
    const body = await res.json();
    body.request.status = 'fulfilling';
    body.request.settlementHold = { attemptedCents: 22_000, approvedCents: 20_350, reason: 'held for director review', heldAt: '2026-10-30T18:00:00.000Z' };
    await route.fulfill({ response: res, json: body });
  });
  await page.goto('/requests/req-0004');
  await expect(page.locator('[data-settlement-hold]')).toContainText('agent reported $220.00 vs $203.50 approved');
});
