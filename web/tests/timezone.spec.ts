import { expect, test } from '@playwright/test';
import { settle, overflow } from './helpers';
import { mkdirSync } from 'node:fs';

// Exercise the real preference API and browser detection; no mocked API responses.
test('automatic follows device on focus; manual and club persist independently of widgets', async ({ page }) => {
  await page.goto('/auth/dev?as=officer');
  await page.request.put('/api/me/prefs', { data: { timezoneMode: 'club', timezone: '' } });
  await page.goto('/settings');
  await settle(page);
  await page.getByRole('radio', { name: 'Automatic' }).check();
  await page.getByRole('button', { name: 'Save time zone', exact: true }).click();
  await expect.poll(async () => (await (await page.request.get('/api/me/prefs')).json()).timezone).toBe('America/Los_Angeles');
  await page.evaluate(() => {
    const original = Intl.DateTimeFormat.prototype.resolvedOptions;
    Intl.DateTimeFormat.prototype.resolvedOptions = function () { return { ...original.call(this), timeZone: 'Asia/Tokyo' }; };
    window.dispatchEvent(new Event('focus'));
  });
  await expect.poll(async () => (await (await page.request.get('/api/me/prefs')).json()).effectiveTimezone).toBe('Asia/Tokyo');
  await expect(page.locator('[data-timezone-preview]')).toContainText('Asia/Tokyo');
  await page.getByRole('radio', { name: 'Choose manually' }).check();
  const manual = page.getByLabel('Manual time zone', { exact: true });
  await manual.fill('Mars/Base');
  await page.getByRole('button', { name: 'Save time zone', exact: true }).click();
  await expect(manual).toHaveAttribute('aria-invalid', 'true');
  await expect(page.locator('#manual-timezone-error')).toHaveText('Choose an IANA time zone');
  await manual.fill('Europe/London');
  await page.getByRole('button', { name: 'Save time zone', exact: true }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Time zone saved' })).toBeVisible();
  await page.reload();
  await settle(page);
  await expect(page.getByRole('radio', { name: 'Choose manually' })).toBeChecked();
  await expect(page.locator('[data-timezone-preview]')).toContainText('Europe/London');
  await page.evaluate(() => window.dispatchEvent(new Event('focus')));
  const before = await (await page.request.get('/api/me/prefs')).json();
  expect(before.timezone).toBe('Europe/London');
  await page.request.put('/api/me/prefs', { data: { widgetLayout: before.widgetLayout } });
  expect((await (await page.request.get('/api/me/prefs')).json()).effectiveTimezone).toBe('Europe/London');
  await expect(page.getByRole('button', { name: 'Save club location' })).toHaveCount(0);
  expect((await page.request.put('/api/club/settings', { data: { location: 'X', clubTimezone: 'UTC' } })).status()).toBe(403);
  await page.getByRole('radio', { name: 'Club default' }).check();
  await page.getByRole('button', { name: 'Save time zone', exact: true }).click();
  await expect(page.locator('[data-timezone-preview]')).toContainText('America/Los_Angeles');
});

test('leadership manages club location; settings fit light and dark screens', async ({ page }, info) => {
  await page.goto('/auth/dev?as=leadership');
  await page.request.put('/api/me/prefs', { data: { timezoneMode: 'club', timezone: '' } });
  await page.goto('/settings');
  await settle(page);
  await page.getByRole('textbox', { name: 'Club location', exact: true }).fill('University of Washington, Seattle');
  await page.getByLabel('Club default time zone', { exact: true }).fill('Asia/Tokyo');
  await page.getByRole('button', { name: 'Save club location', exact: true }).click();
  await expect(page.locator('[data-timezone-preview]')).toContainText('Asia/Tokyo');
  await page.getByLabel('Club default time zone', { exact: true }).fill('America/Los_Angeles');
  await page.getByRole('button', { name: 'Save club location', exact: true }).click();
  await expect(page.locator('[data-timezone-preview]')).toContainText('America/Los_Angeles');
  if (info.project.name === 'tablet') return;
  mkdirSync('design/shots/v04', { recursive: true });
  for (const theme of ['light', 'dark'] as const) {
    await page.locator(`input[name=theme][value=${theme}]`).check();
    await page.evaluate(() => window.scrollTo(0, 0));
    expect(await overflow(page)).toEqual([]);
    await page.screenshot({ path: `design/shots/v04/timezone-${info.project.name === 'phone' ? '390' : '1440'}-${theme}.png`, fullPage: true });
  }
  await page.locator('input[name=theme][value=light]').check();
});

test('event wall time conversion handles selected zone, DST gaps, and date-only values', async ({ page }) => {
  await page.goto('/auth/dev?as=officer');
  // A JSON document avoids app hydration/focus refresh changing the test clock.
  await page.goto('/api/health');
  const result = await page.evaluate(async () => {
    const moduleAt = (name: string) => import(`/src/lib/${name}.ts`);
    const tz = await moduleAt('timezone');
    const cal = await moduleAt('calendar');
    const fmt = await moduleAt('format');
    const base = (await (await fetch('/api/me/prefs')).json());
    tz.applyTimezone({ ...base, effectiveTimezone: 'America/New_York' });
    const winter = cal.fromLocalInput('2026-01-15T18:00');
    const summer = cal.fromLocalInput('2026-07-15T18:00');
    const gap = cal.fromLocalInput('2026-03-08T02:30');
    tz.applyTimezone({ ...base, effectiveTimezone: 'Pacific/Kiritimati' });
    const date = fmt.fmtDate('2026-01-15');
    const instant = cal.toLocalInput('2026-01-15T18:00:00Z');
    return { winter, summer, gap, date, instant };
  });
  expect(result).toEqual({ winter: '2026-01-15T18:00:00-05:00', summer: '2026-07-15T18:00:00-04:00', gap: null, date: 'Jan 15, 2026', instant: '2026-01-16T08:00' });
});

test('automatic zone changes preserve an unsaved event and apply after saving', async ({ page }) => {
  await page.goto('/auth/dev?as=officer');
  await page.request.put('/api/me/prefs', { data: { timezoneMode: 'auto', timezone: 'America/Los_Angeles' } });
  await page.goto('/events');
  await settle(page);
  await page.locator('[data-new-event]').click();
  const editor = page.locator('dialog[open]');
  const name = `Zone change draft ${Date.now()}`;
  await editor.getByLabel('Event name').fill(name);
  await editor.getByLabel(/^Starts/).fill('2026-11-20T18:00');
  await page.evaluate(() => {
    const original = Intl.DateTimeFormat.prototype.resolvedOptions;
    Intl.DateTimeFormat.prototype.resolvedOptions = function () { return { ...original.call(this), timeZone: 'America/New_York' }; };
    window.dispatchEvent(new Event('focus'));
  });
  await expect.poll(async () => (await (await page.request.get('/api/me/prefs')).json()).effectiveTimezone).toBe('America/New_York');
  await expect(page.getByRole('status').filter({ hasText: 'Your time zone changed' })).toContainText('America/New_York');
  await expect(editor.getByLabel('Event name')).toHaveValue(name);
  await expect(editor.getByLabel(/^Starts/)).toHaveValue('2026-11-20T18:00');
  await editor.getByRole('button', { name: 'Save draft', exact: true }).click();
  await expect(page).toHaveURL(/[?&]edit=/);
  const id = new URL(page.url()).searchParams.get('edit')!;
  await expect(page.getByText('Your time zone changed', { exact: false })).toHaveCount(0);
  await expect(editor.getByLabel('Event name')).toHaveValue(name);
  await expect(editor.getByLabel(/^Starts/)).toHaveValue('2026-11-20T21:00');
  const saved = await (await page.request.get(`/api/events/${id}`)).json();
  expect(new Date(saved.event.startsAt).toISOString()).toBe('2026-11-21T02:00:00.000Z');
  await page.request.delete(`/api/events/${id}`);
  await page.request.put('/api/me/prefs', { data: { timezoneMode: 'club', timezone: '' } });
});

test('a failed save retains edits and the pending zone applies when the drawer closes', async ({ page }) => {
  await page.goto('/auth/dev?as=officer');
  await page.request.put('/api/me/prefs', { data: { timezoneMode: 'auto', timezone: 'America/Los_Angeles' } });
  await page.goto('/events');
  await settle(page);
  await page.locator('[data-new-event]').click();
  const editor = page.locator('dialog[open]');
  await editor.getByLabel('Event name').fill('Keep this failed draft');
  await editor.getByLabel(/^Starts/).fill('2026-11-20T18:00');
  await page.evaluate(() => {
    const original = Intl.DateTimeFormat.prototype.resolvedOptions;
    Intl.DateTimeFormat.prototype.resolvedOptions = function () { return { ...original.call(this), timeZone: 'America/New_York' }; };
    window.dispatchEvent(new Event('focus'));
  });
  const notice = page.getByRole('status').filter({ hasText: 'Your time zone changed' });
  await expect(notice).toBeVisible();
  await page.route('**/api/events', (route) => route.request().method() === 'POST'
    ? route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ error: { code: 'internal', message: 'Try again' } }) })
    : route.continue());
  await editor.getByRole('button', { name: 'Save draft', exact: true }).click();
  await expect(editor.getByText('Try again', { exact: true })).toBeVisible();
  await expect(editor.getByLabel('Event name')).toHaveValue('Keep this failed draft');
  await expect(notice).toBeVisible();
  await editor.getByRole('button', { name: 'Close', exact: true }).click();
  await expect(page.locator('dialog[open]')).toHaveCount(0);
  await expect(notice).toHaveCount(0);
  const zone = await page.evaluate(async () => { const path = '/src/lib/timezone.ts'; return (await import(path)).effectiveTimezone(); });
  expect(zone).toBe('America/New_York');
  await page.request.put('/api/me/prefs', { data: { timezoneMode: 'club', timezone: '' } });
});

test('saving time zone settings keeps another dirty profile form intact', async ({ page }) => {
  await page.goto('/auth/dev?as=officer');
  await page.request.put('/api/me/prefs', { data: { timezoneMode: 'club', timezone: '' } });
  await page.goto('/settings');
  await settle(page);
  const phone = page.locator('#pf-phone');
  const before = await phone.inputValue();
  await phone.fill('2065550199');
  await page.getByRole('radio', { name: 'Choose manually' }).check();
  await page.getByLabel('Manual time zone', { exact: true }).fill('America/New_York');
  await page.getByRole('button', { name: 'Save time zone', exact: true }).click();
  const notice = page.getByRole('status').filter({ hasText: 'Your time zone changed' });
  await expect(notice).toContainText('America/New_York');
  await expect(phone).toHaveValue('2065550199');
  await expect(page.locator('[data-timezone-preview]')).toContainText('America/Los_Angeles');
  await page.getByRole('button', { name: 'Save profile', exact: true }).click();
  await expect(notice).toHaveCount(0);
  await expect(page.locator('[data-timezone-preview]')).toContainText('America/New_York');
  await expect(phone).toHaveValue('2065550199');
  await phone.fill(before);
  await page.getByRole('button', { name: 'Save profile', exact: true }).click();
  await expect(page.getByText('Saved ✓', { exact: true })).toBeVisible();
  await page.request.put('/api/me/prefs', { data: { timezoneMode: 'club', timezone: '' } });
});
