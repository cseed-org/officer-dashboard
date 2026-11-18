import { expect, test, type Page, type Route } from '@playwright/test';
import { ApiError as WebError } from '../src/lib/contracts';
import { ApiError as CoreError } from '../../src/core/contracts';
import { dollars } from '../src/lib/format';
import { weekSlot } from '../src/lib/calendar';
import * as profileRules from '../src/lib/profileRules';
import { parseInvites } from '../src/ui/views/buildspace/launchpad';
import { auditHref } from '../src/ui/widgets/activity/ActivityWidget';

// Components run in a real browser, with actual seeded response shapes. Only the
// requests relevant to a regression are held, failed, or changed by each test.
let data: Record<string, any>;
let me: any, lookups: any, b: any, request: any, design: any, event: any;
const base = process.env.WEB_ORIGIN ?? `http://localhost:${process.env.WEB_PORT ?? 4340}`;
const error = { error: { code: 'internal', message: 'Temporary failure' } };
const json = (route: Route, value: unknown, status = 200) => route.fulfill({ status, json: value });
const clone = <T,>(v: T): T => structuredClone(v);
const holds: (() => void)[] = [];
test.afterEach(() => { holds.splice(0).forEach((resolve) => resolve()); });
const gate = () => { let resolve!: () => void; const promise = new Promise<void>((r) => resolve = r); holds.push(resolve); return { promise, resolve }; };

test.beforeAll(async ({ playwright }) => {
  const api = await playwright.request.newContext({ baseURL: base });
  await api.post('/auth/dev', { form: { as: 'director', key: process.env.E2E_DEV_AUTH_KEY ?? 'local-docker-development-only' }, maxRedirects: 0 });
  data = {};
  for (const path of ['/api/me', '/api/me/prefs', '/api/club/settings', '/api/lookups', '/api/officers', '/api/invites', '/api/contacts', '/api/me/profile', '/api/me/calendar', '/api/me/calendar-emails', '/api/buildspace-launchpad', '/api/design-requests', '/api/events?when=all&limit=200', '/api/requests?scope=all&limit=200', '/api/finance/budgets?fiscalYear=2027', '/api/finance/summary?fiscalYear=2027', '/api/admin/users', '/api/overview']) {
    const r = await api.get(path); expect(r.ok(), path).toBeTruthy(); data[path.split('?')[0]!] = await r.json();
  }
  me = data['/api/me'].user; lookups = data['/api/lookups']; b = data['/api/buildspace-launchpad'];
  const items = data['/api/requests'].items;
  const selected = items.find((r: any) => r.type === 'reimbursement' && r.status === 'submitted') ?? items[0];
  request = (await (await api.get(`/api/requests/${selected.id}`)).json()).request;
  design = await (await api.get(`/api/design-requests/${data['/api/design-requests'].items[0].id}`)).json();
  event = await (await api.get(`/api/events/${data['/api/events'].items[0].id}`)).json();
  data['/api/requests/' + request.id] = { request }; data['/api/design-requests/' + design.request.id] = design;
  data['/api/events/a'] = event; data['/api/events/b'] = { ...event, event: { ...event.event, id: 'b', name: 'Resource B' } };
  for (const role of ['builder', 'mentor']) {
    await api.post('/auth/dev', { form: { as: role, key: process.env.E2E_DEV_AUTH_KEY ?? 'local-docker-development-only' }, maxRedirects: 0 });
    for (const path of ['/api/portal/me', `/api/portal/${role}`]) data[path + (path.endsWith('/me') ? ':' + role : '')] = await (await api.get(path)).json();
  }
  await api.dispose();
});

test.beforeEach(async ({ page }) => {
  await page.route(/^https?:\/\/[^/]+\/api\//, (route) => {
    const path = new URL(route.request().url()).pathname;
    const value = data[path];
    return value ? json(route, clone(value)) : json(route, { items: [], nextCursor: null, emails: [], ok: true });
  });
  await page.route('**/__regression*', (r) => r.fulfill({ contentType: 'text/html', body: '<!doctype html><html lang="en"><body><style>#fixture > .wc { width:640px; height:360px }</style><div id="fixture"></div></body></html>' }));
  page.on('dialog', (d) => void d.accept());
});

async function mount(page: Page, kind: string, props: Record<string, unknown> = {}, query = '') {
  await page.goto('/__regression' + query);
  await rerender(page, kind, props);
}
async function rerender(page: Page, kind: string, props: Record<string, unknown> = {}) {
  await page.evaluate(async ({ kind, props }) => {
    const path = '/tests/web-bugs.fixture.tsx'; const m = await import(/* @vite-ignore */ path);
    m.mount(kind, props);
  }, { kind, props });
}
async function moduleCall(page: Page, name: string, arg?: unknown) {
  return page.evaluate(async ({ name, arg }) => { const path = '/tests/web-bugs.fixture.tsx'; const m = await import(/* @vite-ignore */ path); return m[name](arg); }, { name, arg });
}
const widget = (id: string, size = '2x2') => ({ id, size, me, overview: { status: 'ready', data: { fiscalYear: 2027, kpis: { pendingRequests: 0, pendingRequestsCents: 0 } } }, cloudflare: { status: 'loading' } });

test('M-012: both ApiError schemas accept every intentional error code', () => {
  for (const code of ['bad_request', 'unauthenticated', 'forbidden', 'not_found', 'conflict', 'invalid_transition', 'import_failed', 'internal', 'onboarding_required', 'over_tolerance', 'archived', 'rate_limited', 'not_configured', 'too_large']) {
    const body = { error: { code, message: 'Representative server error', details: {} } };
    expect(WebError.safeParse(body).success, code).toBe(true); expect(CoreError.safeParse(body).success, code).toBe(true);
  }
});
test('M-099: whole dollars round negative magnitudes symmetrically without negative zero', () => {
  expect(dollars(-40)).toBe('$0'); expect(dollars(-150)).toBe('-$2'); expect(dollars(150)).toBe('$2');
});
test('M-126: week slots cover both rows at partial half-hour boundaries and clip', () => {
  const slot = (a: string, b: string) => weekSlot('2026-11-18T' + a + ':00-08:00', '2026-11-18T' + b + ':00-08:00', 9, 21);
  expect(slot('09:45', '10:15')).toEqual({ start: 1, len: 2 }); expect(slot('09:55', '10:05')).toEqual({ start: 1, len: 2 });
  expect(slot('08:45', '09:15')).toEqual({ start: 0, len: 1 }); expect(slot('20:45', '21:15')).toEqual({ start: 23, len: 1 });
  expect(slot('09:00', '10:00')).toEqual({ start: 0, len: 2 });
});
test('M-115: address-book lists preserve display names and quoted commas', () => {
  expect(parseInvites('Ada Lovelace <ada@uw.edu>\nA <a@uw.edu>; B <b@uw.edu>\n"Lovelace, Ada" <other@uw.edu>').map((r) => [r.email, r.name, r.state])).toEqual([
    ['ada@uw.edu', 'Ada Lovelace', 'valid'], ['a@uw.edu', 'A', 'valid'], ['b@uw.edu', 'B', 'valid'], ['other@uw.edu', 'Lovelace, Ada', 'valid'],
  ]);
});
test('M-120: profile email validation mirrors the Go email vocabulary', () => {
  for (const email of ['ada@uw.edu', 'first.last+tag@sub.uni.ac.uk', "o'neil@x.org", 'a_b@x.org']) expect(profileRules.isEmail(email), email).toBe(true);
  for (const email of ['jo!e@x.com', 'ana@uni.ac.k1', 'é@x.org', '.a@x.org', 'a..b@x.org', "a'@x.org"]) expect(profileRules.isEmail(email), email).toBe(false);
});
test('M-072: audit links distinguish purchase, design and child records', () => {
  expect(auditHref({ entityType: 'purchase_request', entityId: 'p' })).toBe('/finance?id=p');
  expect(auditHref({ entityType: 'design_request', entityId: 'd' })).toBe('/design?id=d');
  expect(auditHref({ entityType: 'design_request_comment', entityId: 'c' })).toBeNull();
  expect(auditHref({ entityType: 'design_request_file', entityId: 'f' })).toBeNull();
});

test('M-002: resource-key switches hide prior ready actions while same-key reloads retain data', async ({ page }) => {
  const hold = gate(); await page.route('**/api/events/b', async (r) => { await hold.promise; await json(r, data['/api/events/b']); });
  await mount(page, 'Hook'); await expect(page.locator('[data-hook]')).toHaveText(event.event.name);
  await page.getByText('Switch', { exact: true }).click(); await expect(page.locator('[data-hook]')).toHaveText('loading');
  hold.resolve(); await expect(page.locator('[data-hook]')).toHaveText('Resource B');
  const holdReload = gate(); await page.route('**/api/events/b', async (r) => { await holdReload.promise; await json(r, data['/api/events/b']); });
  await page.getByText('Reload', { exact: true }).click(); await expect(page.locator('[data-hook]')).toHaveText('Resource B'); holdReload.resolve();
});
test('M-041: Events makes one unfiltered read and never renders an idle placeholder during search', async ({ page }) => {
  let reads = 0; const hold = gate();
  await page.route('**/api/events?*', async (r) => { reads++; if (new URL(r.request().url()).searchParams.get('q')) await hold.promise; await json(r, { items: [data['/api/events'].items[0]], nextCursor: null }); });
  await mount(page, 'Events'); await expect(page.locator('#period')).toContainText('1 event'); expect(reads).toBe(1);
  await page.getByLabel('Search events', { exact: true }).fill('unmatched'); await expect(page.locator('#period')).toHaveText('Loading events…');
  await expect(page.locator('#period')).not.toContainText('matching'); hold.resolve();
});
test('M-014: Events summary follows a second page for attendance and current-month counters', async ({ page }) => {
  const first = Array.from({ length: 200 }, (_, i) => ({ ...data['/api/events'].items[0], id: `future-${i}`, startsAt: '2030-01-01T10:00:00Z', status: 'published', metrics: { ...data['/api/events'].items[0].metrics, checkedIn: 1 } }));
  const last = { ...first[0], id: 'last', startsAt: new Date().toISOString(), metrics: { ...first[0].metrics, checkedIn: 7 } };
  await page.route('**/api/events?*', (r) => json(r, new URL(r.request().url()).searchParams.has('cursor') ? { items: [last], nextCursor: null } : { items: first, nextCursor: 'second' }));
  await mount(page, 'Events'); const summary = page.getByRole('region', { name: 'Events summary' });
  await expect(summary).toContainText('207'); await expect(summary).toContainText('This month');
  await expect(summary.locator('a, div').filter({ hasText: /^This month/ }).first()).toContainText('1');
});
test('M-011: My profile recovers after a failed load, close and reopen', async ({ page }) => {
  let n = 0; await page.route('**/api/me/profile', (r) => ++n === 1 ? json(r, error, 503) : json(r, data['/api/me/profile']));
  await mount(page, 'Officers', { me }); await page.getByRole('button', { name: 'My profile', exact: true }).click();
  const drawer = page.locator('#own-profile'); await expect(drawer).toContainText('Temporary failure');
  await drawer.getByRole('button', { name: 'Close', exact: true }).click(); await page.getByRole('button', { name: 'My profile', exact: true }).click();
  await expect(drawer.locator('#pf-phone')).toBeVisible(); await expect(drawer).not.toContainText('Temporary failure');
});
test('M-016: invite and support failures show retry, never false empty data or Discord configuration', async ({ page }) => {
  const hold = gate(); let fail = true;
  await page.route('**/api/invites', async (r) => { await hold.promise; await json(r, fail ? error : data['/api/invites'], fail ? 500 : 200); });
  await mount(page, 'Officers', { me, section: 'invites' }); await expect(page.getByText('Loading invites', { exact: false })).toBeVisible(); hold.resolve();
  await expect(page.getByText('Temporary failure')).toBeVisible(); await expect(page.getByText('No invites yet.')).toHaveCount(0);
  await page.locator('[data-open-invite]').click(); await expect(page.locator('[data-invite-dm]')).toBeDisabled(); await expect(page.getByText('Couldn’t check Discord DM setup', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Cancel', exact: true }).click(); fail = false; await page.getByRole('button', { name: /try again/i }).click(); await expect(page.locator('[data-invites]')).toBeVisible();
  await page.route('**/api/contacts?*', (r) => json(r, error, 500)); await mount(page, 'Officers', { me }); await expect(page.getByText('Temporary failure')).toBeVisible();
});
test('M-017: request pagination appends 101 rows and labels partial money as shown', async ({ page }) => {
  const sample = data['/api/requests'].items[0]; const rows = Array.from({ length: 101 }, (_, i) => ({ ...sample, id: `r-${i}`, number: `2027-${i}`, title: `Purchase ${i}` }));
  await page.route('**/api/requests?*', (r) => json(r, { items: new URL(r.request().url()).searchParams.has('cursor') ? rows.slice(100) : rows.slice(0, 100), nextCursor: new URL(r.request().url()).searchParams.has('cursor') ? null : 'second', counts: { submitted: 101 } }));
  await mount(page, 'Finance', { me }); await expect(page.getByText(/first 100 of 101 requests.*shown/)).toBeVisible();
  await page.getByRole('button', { name: 'Show more', exact: true }).click(); await expect(page.getByText('Purchase 100', { exact: true })).toBeVisible(); await expect(page.getByText(/101 requests.*total/)).toBeVisible();
});

test('R-01: a failed cursor from the previous Finance search cannot show an old error', async ({ page }) => {
  const hold = gate(); let cursorStarted = false;
  const sample = data['/api/requests'].items[0];
  await page.route('**/api/requests?*', async (r) => {
    const query = new URL(r.request().url()).searchParams;
    if (query.has('cursor')) {
      cursorStarted = true; await hold.promise;
      return json(r, { error: { code: 'internal', message: 'Old page failed' } }, 500);
    }
    return json(r, { items: [{ ...sample, title: query.has('q') ? 'New search result' : 'Original result' }], nextCursor: query.has('q') ? null : 'old-page', counts: { submitted: 1 } });
  });
  await mount(page, 'Finance', { me });
  await page.getByRole('button', { name: 'Show more', exact: true }).click();
  await expect.poll(() => cursorStarted).toBe(true);
  await page.getByLabel('Search requests', { exact: true }).fill('New search');
  await expect(page.getByText('New search result', { exact: true })).toBeVisible();
  const failed = page.waitForResponse((r) => new URL(r.url()).searchParams.get('cursor') === 'old-page');
  hold.resolve(); await (await failed).finished();
  // Let the fetch continuation and Preact's render finish before checking absence.
  await page.evaluate(() => new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))));
  await expect(page.getByText('Old page failed', { exact: true })).toHaveCount(0);
  await expect(page.getByText('New search result', { exact: true })).toBeVisible();
});

test('R-01: Finance query changes clear pagination errors and release only their own busy state', async ({ page }) => {
  const oldPage = gate(), newPage = gate(); let oldCalls = 0, newStarted = false;
  const sample = data['/api/requests'].items[0];
  await page.route('**/api/requests?*', async (r) => {
    const query = new URL(r.request().url()).searchParams;
    if (query.get('cursor') === 'old-page') {
      if (++oldCalls === 1) return json(r, { error: { code: 'internal', message: 'First page failed' } }, 500);
      await oldPage.promise;
      return json(r, { items: [{ ...sample, id: 'stale-row', title: 'Stale cursor row' }], nextCursor: null, counts: { submitted: 2 } });
    }
    if (query.get('cursor') === 'new-page') {
      newStarted = true; await newPage.promise;
      return json(r, { items: [{ ...sample, id: 'new-row', title: 'Current cursor row' }], nextCursor: null, counts: { submitted: 2 } });
    }
    return json(r, { items: [{ ...sample, title: query.has('q') ? 'New search result' : 'Original result' }], nextCursor: query.has('q') ? 'new-page' : 'old-page', counts: { submitted: 2 } });
  });
  await mount(page, 'Finance', { me });
  await page.getByRole('button', { name: 'Show more', exact: true }).click();
  await expect(page.getByText('First page failed', { exact: true })).toBeVisible();
  await page.getByLabel('Search requests', { exact: true }).fill('New search');
  await expect(page.getByText('New search result', { exact: true })).toBeVisible();
  await expect(page.getByText('First page failed', { exact: true })).toHaveCount(0);
  // Return to the original query, retry its cursor, then switch while it is held.
  await page.getByLabel('Search requests', { exact: true }).fill('');
  await expect(page.getByText('Original result', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Show more', exact: true }).click();
  await expect.poll(() => oldCalls).toBe(2);
  await page.getByLabel('Search requests', { exact: true }).fill('New search');
  await expect(page.getByText('New search result', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Show more', exact: true }).click();
  await expect.poll(() => newStarted).toBe(true);
  const completed = page.waitForResponse((r) => new URL(r.url()).searchParams.get('cursor') === 'old-page');
  oldPage.resolve(); await (await completed).finished();
  await page.evaluate(() => new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))));
  await expect(page.getByText('Stale cursor row', { exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Loading…', exact: true })).toBeDisabled();
  newPage.resolve(); await expect(page.getByText('Current cursor row', { exact: true })).toBeVisible();
});

test('M-063 M-080 M-114: budget overlap validation, canonical saved drafts and refreshed summary', async ({ page }) => {
  const budget = clone(data['/api/finance/budgets']); budget.lines = budget.lines.slice(0, 1);
  let summaryReads = 0, writes = 0; let saved = false;
  await page.route('**/api/finance/summary?*', (r) => { summaryReads++; const d = clone(data['/api/finance/summary']); if (saved) d.totals.budgetCents = 60000; return json(r, d); });
  await page.route('**/api/finance/budgets*', (r) => {
    if (r.request().method() === 'PUT') { writes++; saved = true; const body = r.request().postDataJSON(); return json(r, { ...budget, lines: [{ ...budget.lines[0], amountCents: body.lines[0].amountCents, notes: body.lines[0].notes }] }); }
    return json(r, budget);
  });
  await mount(page, 'Finance', { me }, '?tab=budgets'); await page.getByLabel('Amount, line 1', { exact: true }).fill('600');
  await page.getByRole('button', { name: 'save budget', exact: true }).click(); await expect(page.getByRole('button', { name: 'save budget', exact: true })).toBeDisabled(); await expect(page.getByText('discard changes', { exact: true })).toHaveCount(0);
  await expect.poll(() => summaryReads).toBe(2); expect(writes).toBe(1);
  await page.getByRole('button', { name: 'add line', exact: false }).click(); await page.getByLabel('Category, line 2', { exact: true }).selectOption(budget.lines[0].account.id);
  await page.getByLabel('Fund, line 1', { exact: true }).selectOption(''); await page.getByLabel('Fund, line 2', { exact: true }).selectOption(lookups.funds[0].id);
  await page.getByRole('button', { name: 'save budget', exact: true }).click(); await expect(page.locator('.field-error').first()).toContainText('all-funds'); expect(writes).toBe(1);
});
test('M-002: changing fiscal year removes the previous budget editor until new data arrives', async ({ page }) => {
  const hold = gate(); await page.route('**/api/finance/budgets?fiscalYear=2026', async (r) => { await hold.promise; await json(r, { ...data['/api/finance/budgets'], fiscalYear: 2026 }); });
  await mount(page, 'Finance', { me }, '?tab=budgets'); await expect(page.locator('[data-budget-editor]')).toBeVisible();
  await page.locator('#fin-fy').selectOption('2026'); await expect(page.locator('[data-budget-editor]')).toHaveCount(0); hold.resolve(); await expect(page.locator('[data-budget-editor]')).toBeVisible();
});
test('M-010 M-035 M-119: uploads preserve a newer approval, reject overlapping batches, and settle at most 20 receipts', async ({ page }) => {
  const r = clone(request); r.status = 'submitted'; r.allowedTransitions = ['approved', 'denied']; r.attachments = [];
  const hold = gate(); let uploads = 0; let sent: any;
  await page.route('**/api/requests/' + r.id, (route) => json(route, { request: r }));
  await page.route('**/api/requests/*/attachments', async (route) => { uploads++; await hold.promise; await json(route, { attachment: { id: 'receipt', kind: 'receipt', filename: 'receipt.png', sizeBytes: 1, mimeType: 'image/png', storage: 'local', url: '/api/attachments/receipt', uploadedBy: r.requester, createdAt: new Date().toISOString() } }); });
  await page.route(/\/api\/requests\/[^/]+\/(?:transition|approve)$/, (route) => { sent = route.request().postDataJSON(); return json(route, { request: { ...r, status: 'approved', allowedTransitions: ['reimbursed'], attachments: r.attachments } }); });
  await mount(page, 'Request', { id: r.id, meId: me.id, director: true });
  await page.locator('#att-files').setInputFiles({ name: 'receipt.png', mimeType: 'image/png', buffer: Buffer.from('a') });
  await page.locator('.dropzone').evaluate((el) => { const dt = new DataTransfer(); dt.items.add(new File(['b'], 'second.png', { type: 'image/png' })); el.dispatchEvent(new DragEvent('drop', { bubbles: true, dataTransfer: dt })); });
  await page.getByRole('button', { name: /^approve \$/ }).click(); await expect(page.locator('[data-request-detail]')).toContainText('approved'); hold.resolve();
  await expect(page.getByText('receipt.png', { exact: true })).toBeVisible(); await expect(page.getByRole('button', { name: /^approve \$/ })).toHaveCount(0); expect(uploads).toBe(1);
  const lots = clone(r); lots.status = 'approved'; lots.allowedTransitions = ['reimbursed']; lots.attachments = Array.from({ length: 21 }, (_, i) => ({ id: 'att-' + i, kind: 'receipt', filename: 'file-' + i, mimeType: 'image/png', sizeBytes: 1, storage: 'local', url: null, uploadedBy: r.requester, createdAt: new Date().toISOString() }));
  await page.route('**/api/requests/' + r.id, (route) => json(route, { request: lots })); await mount(page, 'Request', { id: r.id, meId: me.id, director: true });
  await page.getByRole('button', { name: 'approve payout', exact: true }).click(); await expect.poll(() => sent.attachmentIds?.length).toBe(20);
});
for (const [own, director, permitted] of [[true, false, true], [false, false, false], [false, true, true]]) test(`M-119: upload permission own=${own} director=${director}`, async ({ page }) => {
  await mount(page, 'Request', { id: request.id, meId: own ? request.requester.id : 'other-officer', director });
  await expect(page.locator('[data-request-detail]')).toBeVisible(); await expect(page.locator('#att-files')).toHaveCount(permitted ? 1 : 0);
});

test('M-057: design drafts are account scoped and legacy drafts are removed', async ({ page }) => {
  await mount(page, 'DesignForm', { userId: 'a' }); await page.locator('#dr-title').fill('A private brief'); await page.locator('#dr-brief').fill('A private references');
  await expect.poll(() => page.evaluate(() => sessionStorage.getItem('cseed-design-draft:a'))).toContain('A private brief');
  await page.evaluate(() => sessionStorage.setItem('cseed-design-draft', JSON.stringify({ title: 'Legacy secret' })));
  await rerender(page, 'DesignForm', { userId: 'b' }); await expect(page.locator('#dr-title')).toHaveValue(''); expect(await page.evaluate(() => sessionStorage.getItem('cseed-design-draft'))).toBeNull();
  await rerender(page, 'DesignForm', { userId: 'a' }); await expect(page.locator('#dr-title')).toHaveValue('A private brief');
});
test('M-061: a committed write succeeds when its contract module cannot load', async ({ page }) => {
  await page.route('**/src/lib/contracts.ts*', (r) => r.abort());
  // client.ts alone has no eager zod consumer: emulate the lazy Contacts route.
  await page.goto('/__regression');
  let writes = 0; await page.route('**/api/contacts', (r) => { writes++; return json(r, { contact: { id: 'saved' } }); });
  const result = await page.evaluate(async () => { const path = '/src/lib/api/client.ts'; const { api } = await import(/* @vite-ignore */ path); return api('contactCreate', { body: { name: 'Saved once', type: 'speaker' } }).then(() => 'saved', () => 'failed'); });
  expect(result).toBe('saved'); expect(writes).toBe(1);
});
test('M-078: design comment and upload mutations share the stage-action busy state', async ({ page }) => {
  const d = clone(design); d.request.stage = 'in-review'; d.request.permissions.canApprove = true;
  const hold = gate(); await page.route('**/api/design-requests/' + d.request.id, (r) => json(r, d));
  await page.route('**/api/design-requests/*/comments', async (r) => { await hold.promise; await json(r, d); });
  await mount(page, 'DesignDetail', { id: d.request.id, me }); await page.getByLabel('Add a comment', { exact: true }).fill('Review note'); await page.getByRole('button', { name: 'Post comment', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Approve this version', exact: true })).toBeDisabled(); await expect(page.locator('#dr-upload-file')).toBeDisabled(); hold.resolve();
  await expect(page.getByRole('button', { name: 'Approve this version', exact: true })).toBeEnabled();
});
test('M-079: calendar delivery radios disable during a pending mode change', async ({ page }) => {
  const d = clone(data['/api/me/calendar']); d.google.connected = true; d.hasProfile = true; d.mode = 'ics';
  const hold = gate(); let writes = 0; await page.route('**/api/me/calendar', async (r) => { if (r.request().method() === 'PUT') { writes++; await hold.promise; return json(r, { ...d, mode: 'google' }); } return json(r, d); });
  await mount(page, 'CalendarPanel', { next: '/' }); await page.getByRole('radio', { name: 'Google Calendar sync', exact: false }).check();
  await expect(page.locator('input[name=cal-mode][value=ics]')).toBeDisabled(); await expect(page.locator('input[name=cal-mode][value=google]')).toBeDisabled(); expect(writes).toBe(1); hold.resolve();
});
test('M-081: a failed tour preference save never traps Home behind the dialog', async ({ page }) => {
  await page.route('**/api/me/prefs', (r) => r.request().method() === 'PUT' ? json(r, error, 500) : json(r, data['/api/me/prefs']));
  await mount(page, 'Tour', {}, '?tutorial=1'); await expect(page.locator('[data-home-tour]')).toBeVisible(); await page.keyboard.press('Escape'); await expect(page.locator('[data-home-tour]')).toHaveCount(0); await expect(page.getByRole('status')).toContainText('may show again');
});
test('M-066: Search close and drawer unmount retain scroll locking for the remaining dialog', async ({ page }) => {
  await mount(page, 'Drawers'); await expect(page.locator('#first[open]')).toBeVisible(); await expect(page.locator('#second[open]')).toBeVisible();
  await page.keyboard.press('Control+k'); await expect(page.locator('.palette[open]')).toBeVisible(); await page.keyboard.press('Escape'); await expect(page.locator('html')).toHaveClass(/has-modal/);
  await page.getByRole('button', { name: 'Unmount first', exact: true }).click(); await expect(page.locator('#first')).toHaveCount(0); await expect(page.locator('html')).toHaveClass(/has-modal/);
});
test('M-069: sidebar writes serialize and coalesce to the latest width', async ({ page }) => {
  const hold = gate(); const writes: number[] = []; let stored = 0;
  await page.route('**/api/me/prefs', async (r) => { const w = r.request().postDataJSON().sidebarWidth; writes.push(w); if (writes.length === 1) await hold.promise; stored = w; return json(r, { ...data['/api/me/prefs'], sidebarWidth: w }); });
  await mount(page, 'Sidebar'); await page.getByRole('button', { name: '200', exact: true }).click(); await expect.poll(() => writes.length).toBe(1);
  await page.getByRole('button', { name: '280', exact: true }).click(); await page.waitForTimeout(500); expect(writes).toEqual([200]); hold.resolve(); await expect.poll(() => stored).toBe(280); expect(writes).toEqual([200, 280]); await expect(page.locator('html')).toHaveAttribute('style', /280px/);
});
test('M-077: My open requests finds older pending work beyond 50 closed purchases and uses all counts', async ({ page }) => {
  const sample = data['/api/requests'].items[0]; let pending = 0;
  await page.route('**/api/requests?*', (r) => { const q = new URL(r.request().url()).searchParams; const status = q.get('status'); if (status) pending++; return json(r, { items: status ? [{ ...sample, status: 'submitted', title: 'Older outstanding request' }] : Array.from({ length: 50 }, (_, i) => ({ ...sample, id: 'closed-' + i, status: 'closed' })), nextCursor: status ? null : 'older', counts: { closed: 50, submitted: 4 } }); });
  await mount(page, 'MyAction', { ...widget('my-action'), me: { ...me, role: 'officer' } }); await expect(page.getByText('Older outstanding request', { exact: true })).toBeVisible(); await expect(page.locator('.wc-aside')).toContainText('4 open'); expect(pending).toBe(1); await expect(page.getByText('No open requests', { exact: true })).toHaveCount(0);
});
test('M-101: New cohort defaults to the selected-zone day in a Pacific evening', async ({ page }) => {
  await page.clock.setFixedTime('2026-11-18T03:30:00Z'); await mount(page, 'CohortBar', { cohorts: b.cohorts, cohortId: b.cohortId, canManage: true }); await page.getByRole('button', { name: 'New cohort', exact: true }).click(); await expect(page.locator('input[name=startsOn]')).toHaveValue('2026-11-17');
});
test('M-118: project and builder create retries keep their client-chosen IDs', async ({ page }) => {
  for (const kind of ['ProjectForm', 'PersonForm']) {
    const ids: string[] = []; await page.route('**/api/buildspace-launchpad/' + (kind === 'ProjectForm' ? 'projects' : 'people') + '/*', (r) => { ids.push(r.request().url()); return json(r, error, 503); });
    await mount(page, kind, { b }); await page.locator('input[name=name]').fill('Retry record'); if (kind === 'PersonForm') await page.locator('select[name=projectId]').selectOption(b.projects[0].id);
    const save = page.getByRole('button', { name: kind === 'ProjectForm' ? 'Save project' : 'Save builder', exact: true }); await save.click(); await expect(page.getByText('Temporary failure')).toBeVisible(); await save.click(); await expect.poll(() => ids.length).toBe(2); expect(ids[0]).toBe(ids[1]);
  }
});
test('M-062: archived cards and direct edit forms have no enabled edit path', async ({ page }) => {
  for (const kind of ['PersonCard', 'ProjectCard']) {
    const p = kind === 'PersonCard' ? b.people[0] : b.projects[0]; await mount(page, kind, { b, p, scope: '', readOnly: true }); await page.getByRole('button', { name: 'Actions for ' + p.name, exact: true }).click(); await expect(page.getByRole('menuitem', { name: /^Edit/ })).toHaveCount(0);
  }
  await mount(page, 'ProjectForm', { b, project: b.projects[0], readOnly: true }); await expect(page.getByRole('button', { name: 'Save project', exact: true })).toBeDisabled();
  await mount(page, 'PersonForm', { b, person: b.people[0], readOnly: true }); await expect(page.getByRole('button', { name: 'Save builder', exact: true })).toBeDisabled();
});
test('M-146: calendar popovers refuse unsafe server hrefs', async ({ page }) => {
  await mount(page, 'CalendarChip', { item: { id: 'unsafe', source: 'events', type: 'event', title: 'Unsafe link', date: '2026-11-18', startsAt: null, endsAt: null, location: null, href: 'javascript:alert(1)', lumaUrl: 'javascript:alert(1)' }, index: 0 });
  await page.locator('.cal-chip').hover(); await expect(page.locator('.cw-item-popover')).toBeVisible(); await expect(page.locator('a[href^="javascript:"]')).toHaveCount(0);
});
test('M-110 M-146: archived builder portal has no Past due prompt and filters unsafe project links', async ({ page }) => {
  const d = clone(data['/api/portal/builder']); d.readOnly = true; d.thisWeek = 'past-due'; d.project.links = ['javascript:alert(1)', 'https://example.com'];
  await page.route('**/api/portal/builder', (r) => json(r, d)); await mount(page, 'BuilderHome', { me: data['/api/portal/me:builder'] });
  await expect(page.locator('[data-builder-dashboard]')).toBeVisible(); await expect(page.getByText('Past due', { exact: true })).toHaveCount(0); await expect(page.locator('a[href^="javascript:"]')).toHaveCount(0); await expect(page.locator('a[href="https://example.com/"]')).toBeVisible();
});

test('M-037 M-064: calendar caches by role, refreshes on expired focus, and ignores resize', async ({ page }) => {
  let reads = 0;
  await page.route('**/api/calendar/feed?*', (r) => json(r, { items: [{ id: 'item', source: 'events', type: 'event', title: `Feed ${++reads}`, date: new Date().toISOString().slice(0, 10), startsAt: null, endsAt: null, href: '/events?id=item', lumaUrl: null, location: null }], sources: [], timezone: 'America/Los_Angeles' }));
  await mount(page, 'Calendar', { ...widget('calendar'), me: { ...me, role: 'officer' } }); await expect(page.locator('.cal-chip')).toContainText('Feed 1');
  await rerender(page, 'Calendar', widget('calendar')); await expect(page.locator('.cal-chip')).toContainText('Feed 2');
  await page.setViewportSize({ width: 1200, height: 900 }); expect(reads).toBe(2);
  await page.clock.install(); await page.clock.fastForward(61_000); await page.evaluate(() => dispatchEvent(new Event('focus'))); await expect(page.locator('.cal-chip')).toContainText('Feed 3');
  await rerender(page, 'Calendar', { ...widget('calendar'), me: { ...me, role: 'officer' } }); await expect.poll(() => reads).toBe(4);
});
test('M-045: Budget observers attach after full to compact to full and compact initial mount', async ({ page }) => {
  await page.addInitScript(() => {
    const Native = ResizeObserver; (window as any).observed = new WeakSet();
    window.ResizeObserver = class extends Native { observe(el: Element, options?: ResizeObserverOptions) { (window as any).observed.add(el); super.observe(el, options); } };
  });
  await mount(page, 'Budget', widget('budget')); await expect(page.locator('.donut-wrap:not(.donut-wrap--compact)')).toBeVisible();
  await rerender(page, 'Budget', widget('budget', '1x1')); await expect(page.locator('.donut-wrap--compact')).toBeVisible();
  await rerender(page, 'Budget', widget('budget')); await expect(page.locator('.donut-wrap:not(.donut-wrap--compact)')).toBeVisible();
  for (const selector of ['.donut-wrap', '.blegend-col']) expect(await page.locator(selector).evaluate((el) => (window as any).observed.has(el))).toBe(true);
  await mount(page, 'Budget', widget('budget', '1x1')); await rerender(page, 'Budget', widget('budget')); expect(await page.locator('.donut-wrap').evaluate((el) => (window as any).observed.has(el))).toBe(true);
});
test('M-046: recovered same-zone snapshots update clean settings and links while preserving edits', async ({ page }) => {
  const prefs = { ...data['/api/me/prefs'], timezoneMode: 'manual', timezone: 'America/Los_Angeles', effectiveTimezone: 'America/Los_Angeles' };
  let club = { ...data['/api/club/settings'], location: 'Recovered location', websiteUrl: 'https://first.example' };
  await page.route('**/api/me/prefs', (r) => json(r, prefs)); await page.route('**/api/club/settings', (r) => json(r, club));
  await mount(page, 'Settings', { ...widget('links'), me: { ...me, role: 'leadership' } }); await moduleCall(page, 'refreshTimezone');
  await expect(page.locator('#manual-timezone')).toHaveValue(prefs.timezone); await expect(page.locator('#club-location-label')).toHaveValue('Recovered location'); await expect(page.locator('a[href="https://first.example"]')).toBeVisible();
  await page.locator('#club-location-label').fill('My unsaved location'); club = { ...club, location: 'Other tab', websiteUrl: 'https://second.example' }; await moduleCall(page, 'refreshTimezone');
  await expect(page.locator('#club-location-label')).toHaveValue('My unsaved location'); await expect(page.locator('a[href="https://second.example"]')).toBeVisible();
});
test('M-048: failed auto-zone save still applies loaded preferences and is attempted only once', async ({ page }) => {
  let puts = 0; const p = { ...data['/api/me/prefs'], timezoneMode: 'auto', timezone: 'UTC', effectiveTimezone: 'UTC', sidebarWidth: 280 };
  await page.route('**/api/me/prefs', (r) => r.request().method() === 'PUT' ? (puts++, json(r, { error: { code: 'bad_request', message: 'Unsupported browser zone' } }, 400)) : json(r, p));
  await mount(page, 'TimezonePanel', { me }); await moduleCall(page, 'refreshTimezone'); await expect(page.locator('[data-timezone-preview]')).toContainText('UTC'); await expect(page.locator('html')).toHaveAttribute('style', /280px/);
  await moduleCall(page, 'refreshTimezone'); await moduleCall(page, 'refreshTimezone'); expect(puts).toBe(1);
});
test('M-065: closing a pending purchase form with Cancel or Escape never reopens its detail', async ({ page }) => {
  for (const close of ['cancel', 'escape']) {
    const hold = gate(); let started = false;
    await page.route('**/api/requests', async (r) => { started = true; await hold.promise; await json(r, { request }); });
    await mount(page, 'Finance', { me }, '?new=1');
    await page.locator('#rq-title').fill('Pending creation'); await page.locator('#rq-vendor').fill('Vendor'); await page.locator('#rq-unit').fill('10'); await page.locator('#rq-account').selectOption(lookups.expenseAccounts[0].id); await page.locator('#rq-why').fill('Club supplies needed for our event.');
    await page.getByRole('button', { name: 'save as draft', exact: true }).click(); await expect.poll(() => started).toBe(true);
    if (close === 'cancel') await page.getByRole('button', { name: 'cancel', exact: true }).click(); else await page.keyboard.press('Escape');
    hold.resolve(); await expect(page.locator('#new-request[open], #request-panel[open]')).toHaveCount(0); await expect(page).not.toHaveURL(/[?&]id=/);
  }
});
test('M-067: an invite reissue disables every action for that invite until completion', async ({ page }) => {
  const invite = { id: 'i', name: 'Invitee', discordId: '900000000000000001', subteam: lookups.subteams[0], invitedBy: me, method: 'link', status: 'pending', dmSentAt: null, dmError: null, createdAt: new Date().toISOString(), expiresAt: null, acceptedAt: null, onboardedAt: null, userId: null };
  const hold = gate(); let writes = 0;
  await page.route('**/api/invites', async (r) => { if (r.request().method() === 'GET') return json(r, { invites: [invite], dmConfigured: true }); writes++; await hold.promise; return json(r, { invite, token: 'new', link: base + '/login?invite=new', reinvited: true, dm: { status: 'skipped', sentAt: null, message: null } }); });
  await mount(page, 'Officers', { me, section: 'invites' }); const row = page.locator('[data-invite="900000000000000001"]'); await row.getByRole('button', { name: 'Copy link', exact: true }).click();
  await expect(row.getByRole('button', { name: 'Copy link', exact: true })).toBeDisabled(); await expect(row.getByRole('button', { name: 'Resend DM', exact: true })).toBeDisabled(); expect(writes).toBe(1); hold.resolve();
});
test('M-068 M-112: role writes disable other rows and Add officer retries failed subteams', async ({ page }) => {
  const hold = gate(); let writes = 0;
  await page.route('**/api/admin/users/*', async (r) => { writes++; await hold.promise; return json(r, { user: data['/api/admin/users'].users[0] }); });
  await mount(page, 'Admin', { meId: me.id, isOwner: false, canChangeRoles: true, adding: false });
  const toggles = page.locator('.role-toggle'); await expect(toggles.first()).toBeVisible(); const buttons = toggles.locator('button:not([disabled])[aria-pressed=false]'); await buttons.first().click();
  await expect(toggles.locator('button:not([disabled])')).toHaveCount(0); expect(writes).toBe(1); hold.resolve();
  let fail = true; await page.route('**/api/lookups', (r) => json(r, fail ? error : lookups, fail ? 500 : 200)); await mount(page, 'Admin', { meId: me.id, isOwner: false, canChangeRoles: true, adding: true });
  await expect(page.locator('dialog[open]')).toContainText('Temporary failure'); fail = false; await page.getByRole('button', { name: /try again/i }).click(); await expect(page.locator('dialog[open] select')).toBeVisible();
});
test('M-073: renaming an explicitly selected cohort refreshes its unchanged query', async ({ page }) => {
  const register = clone(b); let reads = 0;
  await page.route('**/api/buildspace-launchpad?*', (r) => { reads++; return json(r, register); });
  await page.route('**/api/buildspace-launchpad/cohorts/*', (r) => { const c = register.cohorts.find((c: any) => c.id === register.cohortId); c.name = 'Renamed current cohort'; return json(r, { cohort: c }); });
  await mount(page, 'Buildspace', { me }, '?cohort=' + b.cohortId); const current = register.cohorts.find((c: any) => c.id === b.cohortId);
  await page.getByRole('button', { name: current.name + ' settings', exact: true }).click(); await page.getByRole('menuitem', { name: 'Rename or change dates', exact: true }).click();
  await page.locator('input[name=name]').fill('Renamed current cohort'); await page.getByRole('button', { name: 'Save cohort', exact: true }).click();
  await expect(page.locator('[data-cohort-select] option:checked')).toHaveText('Renamed current cohort'); expect(reads).toBe(2);
});
test('M-074: failed mentor invite copy keeps its one-time URL visible', async ({ page }) => {
  await page.addInitScript(() => Object.defineProperty(navigator, 'clipboard', { value: { writeText: () => Promise.reject(new Error('denied')) } }));
  const mentor = { ...b.mentors[0], account: null };
  await page.route('**/api/buildspace-launchpad/mentors/*/invite', (r) => json(r, { account: b.accounts[0], link: { url: base + '/launchpad/join?token=one-time' }, email: { status: 'skipped' } }));
  await mount(page, 'MentorRoster', { b, mentors: [mentor], canDirector: true, readOnly: false }); await page.locator('[data-mentor-invite]').click(); await page.getByRole('radio', { name: 'Just give me the link', exact: true }).check(); await page.getByRole('button', { name: 'Send invite', exact: true }).click();
  await page.getByRole('button', { name: 'Copy link and close', exact: true }).click(); await expect(page.locator('[data-mentor-invite-result]')).toContainText('one-time'); await expect(page.locator('[data-mentor-invite-result]')).toContainText('Couldn’t copy');
});
test('M-102 M-124: palette page matches lead results and shortcuts track live permission changes', async ({ page }) => {
  let role = 'officer'; let version = 'officer'; let meReads = 0;
  await page.route('**/api/me', (r) => { meReads++; return r.fulfill({ json: { ...data['/api/me'], user: { ...me, role } }, headers: { 'x-perm-version': version } }); });
  await page.route('**/api/search?*', (r) => json(r, { results: [{ kind: 'request', id: 'unrelated', title: 'Design purchase', subtitle: 'Unrelated record', href: '/finance?id=unrelated' }] }));
  await mount(page, 'Search'); await expect.poll(() => meReads).toBe(1); await page.keyboard.press('Control+k'); await expect(page.locator('[data-href="/officers?tab=users"]')).toHaveCount(0);
  for (const [term, path] of [['Design', '/design'], ['Officers', '/officers'], ['Members', '/members']]) { await page.getByRole('combobox').fill(term!); await expect(page.locator('.palette-option').first()).toHaveAttribute('data-href', path!); }
  role = 'director'; version = 'director'; await page.evaluate(() => dispatchEvent(new Event('cseed-permissions'))); await page.getByRole('combobox').fill('Officer access'); await expect(page.getByRole('option', { name: /Officer access/ })).toBeVisible();
  role = 'officer'; version = 'officer'; await page.evaluate(() => dispatchEvent(new Event('cseed-permissions'))); await expect(page.getByRole('option', { name: /Officer access/ })).toHaveCount(0);
});
test('M-117: mixed-offset events sort by instant in both calendars and Event health', async ({ page }) => {
  const sample = data['/api/events'].items[0]; const later = { ...sample, id: 'later', name: 'Later instant', startsAt: '2026-11-10T09:00:00-08:00', endsAt: '2026-11-10T09:30:00-08:00', status: 'published' }; const earlier = { ...later, id: 'earlier', name: 'Earlier instant', startsAt: '2026-11-10T10:00:00-05:00', endsAt: '2026-11-10T10:30:00-05:00' };
  for (const kind of ['MonthView', 'WeekView']) { await mount(page, kind, { month: '2026-11', monday: '2026-11-09', events: [later, earlier] }); const text = await page.locator(kind === 'MonthView' ? '.cal-month' : '.cal-week').textContent(); expect(text!.indexOf('Earlier instant')).toBeLessThan(text!.indexOf('Later instant')); }
  await page.route('**/api/events?*', (r) => json(r, { items: [earlier, later], nextCursor: null })); await mount(page, 'Events'); await expect(page.locator('#health h2')).toContainText('Later instant');
});
test('M-128: Make co-owner confirmation says the promoted person stays signed in', async ({ page }) => {
  let text = ''; page.on('dialog', (d) => { text = d.message(); }); await mount(page, 'Admin', { meId: me.id, isOwner: true, canChangeRoles: true, adding: false });
  const row = page.locator('tr').filter({ has: page.locator('.role-toggle button[aria-pressed=true]').filter({ hasText: 'director' }) }).first(); await row.locator('[aria-haspopup=menu]').click(); await page.getByRole('menuitem', { name: 'Make co-owner', exact: true }).click();
  expect(text).toContain('They stay signed in'); expect(text).not.toContain('signs them out');
});

for (const field of ['mode', 'manual', 'clubZone', 'location'] as const) {
  test(`R-02: reverted ${field} follows the last applied same-zone snapshot`, async ({ page }) => {
    let prefs = { ...data['/api/me/prefs'], timezoneMode: 'manual', timezone: 'America/Los_Angeles', effectiveTimezone: 'America/Los_Angeles' };
    let club = { ...data['/api/club/settings'], clubTimezone: 'America/Los_Angeles', location: 'First' };
    await page.route('**/api/me/prefs', (r) => json(r, prefs));
    await page.route('**/api/club/settings', (r) => json(r, club));
    await mount(page, 'TimezonePanel', { me: { ...me, role: 'leadership' } });
    await moduleCall(page, 'refreshTimezone');
    const selector = { mode: 'input[name=timezone-mode]:checked', manual: '#manual-timezone', clubZone: '#club-timezone', location: '#club-location-label' }[field];
    const input = page.locator(selector);
    const original = field === 'mode' ? 'manual' : field === 'location' ? 'First' : 'America/Los_Angeles';
    const temporary = field === 'mode' ? 'club' : field === 'location' ? 'Temporary' : 'Asia/Tokyo';
    const next = field === 'mode' ? 'club' : field === 'location' ? 'Second' : 'UTC';
    const edit = async (value: string) => {
      if (field === 'mode') await page.locator(`input[name=timezone-mode][value=${value}]`).check();
      else await input.fill(value);
    };
    const refresh = async (value: string) => {
      if (field === 'mode') prefs = { ...prefs, timezoneMode: value };
      else if (field === 'manual') prefs = { ...prefs, timezone: value };
      else if (field === 'clubZone') club = { ...club, clubTimezone: value };
      else club = { ...club, location: value };
      await moduleCall(page, 'refreshTimezone');
      await expect(page.locator('[data-timezone-preview]')).toContainText('America/Los_Angeles');
    };
    await expect(input).toHaveValue(original);
    await edit(temporary); await edit(original);
    await refresh(next); await expect(input).toHaveValue(next);
    // An actual edit survives refresh; reverting to the new baseline is clean too.
    await edit(original); await refresh(next); await expect(input).toHaveValue(original);
    await edit(next); await refresh(original); await expect(input).toHaveValue(original);
  });
}

test('M-073: deleting the implicit default cohort reloads the fallback register', async ({ page }) => {
  const empty = { ...b.cohorts[0], id: 'empty', name: 'Empty default', counts: { projects: 0, builders: 0, invited: 0 } };
  let register = { ...clone(b), cohortId: empty.id, projects: [], people: [], activity: [], cohorts: [empty, ...b.cohorts] }; let reads = 0;
  await page.route('**/api/buildspace-launchpad', (r) => { reads++; return json(r, register); });
  await page.route('**/api/buildspace-launchpad/cohorts/empty', (r) => { register = clone(b); return json(r, { ok: true }); });
  await mount(page, 'Buildspace', { me }); await page.getByRole('button', { name: 'Empty default settings', exact: true }).click(); await page.getByRole('menuitem', { name: 'Delete cohort', exact: true }).click();
  await expect(page.locator('[data-cohort-select]')).toHaveValue(b.cohortId); expect(reads).toBe(2);
});
test('M-002: switching cohorts hides old forms and actions during the delayed new register', async ({ page }) => {
  const hold = gate(); const next = { ...clone(b), cohortId: 'next', cohorts: [...b.cohorts, { ...b.cohorts[0], id: 'next', name: 'Next' }] };
  await page.route('**/api/buildspace-launchpad*', async (r) => { if (new URL(r.request().url()).searchParams.get('cohort') === 'next') { await hold.promise; return json(r, next); } return json(r, { ...b, cohorts: next.cohorts }); });
  await mount(page, 'Buildspace', { me }); await page.locator('[data-cohort-select]').selectOption('next'); await expect(page.getByRole('button', { name: 'New project', exact: true })).toBeDisabled(); await expect(page.locator('[data-person-card], [data-project-card]')).toHaveCount(0);
  hold.resolve(); await expect(page.getByRole('button', { name: 'New project', exact: true })).toBeEnabled();
});
test('M-120: intake exposes server errors for fields in a hidden profile stage', async ({ page }) => {
  await page.route('**/api/me/profile', (r) => json(r, { error: { code: 'bad_request', message: 'Validation failed', details: { fieldErrors: { personalEmail: ['Enter a valid email'] } } } }, 400));
  await mount(page, 'ProfileForm', { data: data['/api/me/profile'], section: 'role' }); await page.getByRole('button', { name: 'Save profile', exact: true }).click(); await expect(page.getByRole('alert')).toContainText('personalEmail');
});
test('M-099: functional-expenses footer uses accounting parentheses in every negative column', async ({ page }) => {
  const account = lookups.expenseAccounts[0]; const totals = { programCents: -100, mgmtGeneralCents: -200, fundraisingCents: -300, totalCents: -600 };
  await page.route('**/api/finance/reports/*', (r) => json(r, { report: 'statement-of-functional-expenses', fiscalYear: 2027, startDate: '2026-07-01', endDate: '2027-06-30', asOf: '2026-11-18', rows: [{ account, ...totals }], totals }));
  await mount(page, 'Report', { name: 'statement-of-functional-expenses', query: { fiscalYear: 2027 } }); await expect(page.locator('tfoot td.num')).toHaveText(['($1.00)', '($2.00)', '($3.00)', '($6.00)']);
});
