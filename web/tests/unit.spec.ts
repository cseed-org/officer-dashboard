import { expect, test } from '@playwright/test';
import { normalizeHref } from '../src/lib/routes';
import { sanitizeResults } from '../src/lib/search';
import { internalPath, internalRoute, safeImageUrl, safeUrl } from '../src/lib/url';

/** Pure-function tests (no browser); run once, in the desktop project. */
test.beforeEach(({}, info) => test.skip(info.project.name !== 'desktop', 'unit tests run once'));
const O = 'http://localhost:4340';

test('P2-2: internalPath accepts same-origin paths and refuses every off-site form', () => {
  expect(internalPath('/finance?id=req-1', O)).toBe('/finance?id=req-1');
  expect(internalPath('/events#x', O)).toBe('/events#x');
  for (const bad of ['//evil.example', '/\\evil.example', '\\\\evil.example', '/\\/evil', 'https://evil.example/', 'javascript:alert(1)', 'data:text/html,x', 'evil', '', '  //evil']) {
    expect(internalPath(bad, O), bad).toBeNull();
  }
});

test('P2-2: safeUrl keeps http(s) and same-origin links, drops backslash and script URLs', () => {
  expect(safeUrl('https://example.com/pizza', O)).toBe('https://example.com/pizza');
  expect(safeUrl('/api/attachments/a1', O)).toBe('/api/attachments/a1');
  expect(safeUrl('/\\evil.example/x', O)).toBeNull();
  expect(safeUrl('\\\\evil.example', O)).toBeNull();
  expect(safeUrl('//evil.example', O)).toBeNull();
  expect(safeUrl('javascript:alert(1)', O)).toBeNull();
  expect(safeUrl(' JAVASCRIPT:alert(1)', O)).toBeNull();
  expect(safeImageUrl('http://cdn.example/a.png')).toBeNull();
  expect(safeImageUrl('https://cdn.discordapp.com/a.png')).toBe('https://cdn.discordapp.com/a.png');
});

test('P2-3: search results keep only internal dashboard routes', () => {
  const r = (href: string) => ({ kind: 'request', id: href, title: 't', subtitle: 's', href });
  const out = sanitizeResults(
    [
      r('javascript:alert(document.cookie)'),
      r('//evil.example/finance'),
      r('/\\evil.example'),
      r('https://evil.example/finance?id=1'),
      r('/api/me'),
      r('/auth/logout'),
      r('/requests/2027-0007'),
      r('/events?id=event-hack-night'),
      r('/admin/tokens'),
    ],
    O,
  ).map((x) => x.href);
  expect(out).toEqual(['/finance?id=2027-0007', '/events?id=event-hack-night', '/admin?tab=tokens']);
  expect(internalRoute('/settings', O)).toBe('/settings');
  expect(internalRoute('/favicon.svg', O)).toBeNull();
  expect(normalizeHref('/requests/new?eventId=e1')).toBe('/finance?new=1&eventId=e1');
});

test('events: calendar helpers keep the Pacific wall clock and Monday weeks', async () => {
  const c = await import('../src/lib/calendar');
  expect(c.pacificParts('2026-11-13T02:00:00Z')).toEqual({ date: '2026-11-12', minutes: 18 * 60 });
  expect(c.mondayOf('2026-11-15')).toBe('2026-11-09');
  expect(c.mondayOf('2026-11-09')).toBe('2026-11-09');
  const grid = c.monthGrid('2026-11');
  expect(grid[0]).toEqual({ date: '2026-10-26', outside: true });
  expect(grid.length % 7).toBe(0);
  expect(grid.filter((d) => !d.outside)).toHaveLength(30);
  expect(c.addMonths('2026-12', 1)).toBe('2027-01');
  expect(c.weekLabel('2026-11-09')).toBe('November 9–15, 2026');
  expect(c.weekLabel('2026-11-30')).toBe('Nov 30 – Dec 6, 2026');
  expect(c.timeRange('2026-11-12T18:00:00-08:00', '2026-11-12T20:00:00-08:00')).toBe('6–8 PM');
  expect(c.timeRange('2026-11-21T10:00:00-08:00', '2026-11-21T11:30:00-08:00')).toBe('10–11:30 AM');
  expect(c.timeRange('2026-11-21T11:00:00-08:00', '2026-11-21T13:00:00-08:00')).toBe('11 AM–1 PM');
  expect(c.whenLong('2026-11-12T18:00:00-08:00', '2026-11-12T20:00:00-08:00')).toBe('November 12, 2026 · 6–8 PM · America/Vancouver');
  // datetime-local round trip across the November DST change
  expect(c.fromLocalInput('2026-11-12T18:00')).toBe('2026-11-12T18:00:00-08:00');
  expect(c.fromLocalInput('2026-10-30T18:00')).toBe('2026-10-30T18:00:00-07:00');
  expect(c.toLocalInput('2026-11-12T18:00:00-08:00')).toBe('2026-11-12T18:00');
  expect(c.fromLocalInput('')).toBeNull();
  expect(c.weekSlot('2026-11-12T18:00:00-08:00', '2026-11-12T20:00:00-08:00', 9, 21)).toEqual({ start: 18, len: 4 });
  expect(c.weekSlot('2026-11-12T06:00:00-08:00', '2026-11-12T07:00:00-08:00', 9, 21)).toBeNull();
});

test('events: the Luma URL rule matches the server', async () => {
  const { lumaEventUrlProblem } = await import('../src/lib/contracts');
  for (const ok of ['https://lu.ma/founder-stories', 'https://www.luma.com/abc123xy/', 'https://lu.ma/event/evt-AbCd1234', 'https://lu.ma/x1?utm=1'])
    expect(lumaEventUrlProblem(ok), ok).toBeNull();
  for (const bad of ['http://lu.ma/x1', 'https://lu.ma/', 'https://lu.ma', 'https://lu.ma/create', 'https://lu.ma/discover', 'https://lu.ma/calendar', 'https://lu.ma/a/b', 'https://evil.example/x1', 'https://lu.ma:444/x1', 'nope'])
    expect(lumaEventUrlProblem(bad), bad).not.toBeNull();
});
