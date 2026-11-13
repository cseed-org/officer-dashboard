import { expect, test } from '@playwright/test';
import { designStats } from '../src/lib/design';
import type { DesignRequestItem } from '../src/lib/contracts';
import { isGradMonth, isLinkedIn } from '../src/lib/profileRules';
import { normalizeHref } from '../src/lib/routes';
import { sanitizeResults } from '../src/lib/search';
import { internalPath, internalRoute, safeImageUrl, safeUrl } from '../src/lib/url';
import { migrateLayout, moveTo, nearestSize } from '../src/ui/widgets/layout';
import { defaultWidgetLayout, layoutForChoices } from '../src/ui/widgets/registry';

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
      r('/contacts?id=contact-1'),
    ],
    O,
  ).map((x) => x.href);
  expect(out).toEqual(['/finance?id=2027-0007', '/events?id=event-hack-night', '/officers?tab=tokens', '/contacts?id=contact-1']);
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
  expect(c.whenLong('2026-11-12T18:00:00-08:00', '2026-11-12T20:00:00-08:00')).toBe('November 12, 2026 · 6–8 PM · America/Los_Angeles');
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
  for (const bad of ['http://lu.ma/x1', 'https://lu.ma/', 'https://lu.ma', 'https://lu.ma/create', 'https://lu.ma/discover', 'https://lu.ma/calendar', 'https://lu.ma/a/b', 'https://evil.example/x1', 'https://lu.ma:444/x1', 'https://lu.ma:443/x1', 'https://lu.ma\\x1', 'https:lu.ma/x1', 'nope'])
    expect(lumaEventUrlProblem(bad), bad).not.toBeNull();
});

test('URLs: stored links accept http(s) only, like the server (validate.IsURL)', async () => {
  const { EventUpsertRequest, LumaIngestRequest, PurchaseRequestUpdateRequest } = await import('../src/lib/contracts');
  const core = await import('../../src/core/contracts');
  const schemas = [
    EventUpsertRequest.shape.url,
    LumaIngestRequest.shape.event.shape.url,
    PurchaseRequestUpdateRequest.shape.itemUrl,
    core.EventUpsertRequest.shape.url,
    core.LumaIngestRequest.shape.event.shape.url,
    core.PurchaseRequestUpdateRequest.shape.itemUrl,
  ];
  for (const schema of schemas) {
    for (const ok of ['https://www.adafruit.com/', 'http://x.y/path?q=1']) expect(schema.safeParse(ok).success, ok).toBe(true);
    for (const bad of ['javascript:alert(1)', 'data:text/html,<b>x</b>', 'ftp://x.y/file', 'mailto:a@b.co', 'file:///etc/passwd'])
      expect(schema.safeParse(bad).success, bad).toBe(false);
  }
});

test('V0.4: V0.3 widget layouts migrate to block sizes in place', () => {
  const { layout, changed } = migrateLayout([
    { id: 'activity', size: 'L' },
    { id: 'kpis', size: 'M' },
    { id: 'my-action', size: 'S' },
    { id: 'member-signups', size: 'M' },
    { id: 'kpi-budget', size: '1x1' }, // already produced by kpis: dropped, not duplicated
    { id: 'unknown', size: 'S' },
  ]);
  expect(changed).toBe(true);
  expect(layout).toEqual([
    { id: 'activity', size: '4x2' },
    { id: 'kpi-upcoming', size: '1x1' }, { id: 'kpi-checkin', size: '1x1' }, { id: 'kpi-reflection', size: '1x1' }, { id: 'kpi-budget', size: '1x1' }, { id: 'kpi-pending', size: '1x1' },
    { id: 'my-action', size: '2x1' },
    { id: 'member-signups', size: '2x2' },
    { id: 'unknown', size: '1x1' },
  ]);
  // A saved V0.4 layout passes through untouched; an unsupported size snaps to the nearest.
  expect(migrateLayout(defaultWidgetLayout)).toEqual({ layout: defaultWidgetLayout, changed: false });
  expect(migrateLayout([{ id: 'kpi-pending', size: '4x2' }]).layout).toEqual([{ id: 'kpi-pending', size: '2x1' }]);
  expect(nearestSize(['1x1', '2x2'], '2x1')).toBe('1x1');
  expect(nearestSize(['2x2', '4x2'], '4x1')).toBe('2x2');
});

test('V0.4: moveTo clamps and keeps the array when nothing moves', () => {
  const l = [{ id: 'a', size: '1x1' as const }, { id: 'b', size: '1x1' as const }, { id: 'c', size: '1x1' as const }];
  expect(moveTo(l, 'c', 0).map((w) => w.id)).toEqual(['c', 'a', 'b']);
  expect(moveTo(l, 'a', 99).map((w) => w.id)).toEqual(['b', 'c', 'a']);
  expect(moveTo(l, 'a', -5)).toBe(l);
  expect(moveTo(l, 'zzz', 1)).toBe(l);
});

test('V0.4: onboarding Key figures expands to the five stat tiles in default order', () => {
  const l = layoutForChoices(new Set(['kpis', 'member-signups']));
  expect(l.map((w) => w.id)).toEqual(['kpi-upcoming', 'kpi-checkin', 'kpi-reflection', 'kpi-budget', 'kpi-pending', 'member-signups']);
});

test('V0.4: profile rules match the server (isLinkedIn, YYYY-MM graduation month)', () => {
  for (const ok of ['https://linkedin.com', 'https://www.linkedin.com/in/casey', 'https://LinkedIn.com/in/x', 'https://uk.linkedin.com/in/x'])
    expect(isLinkedIn(ok), ok).toBe(true);
  for (const bad of ['http://linkedin.com/in/x', 'https://evil.example/in/x', 'https://linkedin.com.evil.example/', 'https://notlinkedin.com/in/x', 'javascript:alert(1)', 'linkedin.com/in/x'])
    expect(isLinkedIn(bad), bad).toBe(false);
  for (const ok of ['2028-05', '2030-12']) expect(isGradMonth(ok), ok).toBe(true);
  for (const bad of ['2028-13', '2028-00', '28-05', '2028-5', 'May 2028']) expect(isGradMonth(bad), bad).toBe(false);
});

// Greeting dates are constructed in the runtime's local calendar, never parsed as UTC.
import { localDay, pickGreeting, readState, writeState } from '../src/ui/views/home/greeting';
const greetingDate = (hour: number, minute = 0) => new Date(2026, 10, 11, hour, minute);
for (const [hour, minute, message] of [
  [4, 59, 'Go sleep, bro'], [5, 0, 'Good morning, Ada'],
  [11, 59, 'Good morning, Ada'], [12, 0, 'Good afternoon, Ada'],
  [16, 59, 'Good afternoon, Ada'], [17, 0, 'Good evening, Ada'],
  [23, 59, 'Good evening, Ada'], [0, 0, 'Go sleep, bro'],
] as const) {
  test(`greeting boundary ${hour}:${minute}`, () => {
    expect(pickGreeting(greetingDate(hour, minute), 'Ada', null, () => 1)).toBe(message);
  });
}
test('greeting precedence, randomness, signed-out state and empty name', () => {
  const today = { day: localDay(greetingDate(10)), signedOut: true };
  expect(pickGreeting(greetingDate(10), 'Ada', null, () => 1 / 365 - 0.000001)).toBe('Lenny was here');
  expect(pickGreeting(greetingDate(10), 'Ada', null, () => 1 / 365)).toBe('Good morning, Ada');
  expect(pickGreeting(greetingDate(2), 'Ada', today, () => 0)).toBe('Go sleep, bro');
  expect(pickGreeting(greetingDate(10), 'Ada', today, () => 0)).toBe('Lenny was here');
  expect(pickGreeting(greetingDate(10), 'Ada', today, () => 1)).toBe('Welcome back, Ada');
  expect(pickGreeting(greetingDate(10), 'Ada', { ...today, signedOut: false }, () => 0)).toBeNull();
  expect(pickGreeting(greetingDate(10), 'Ada', { day: '2026-11-10', signedOut: true }, () => 1)).toBe('Good morning, Ada');
  expect(pickGreeting(greetingDate(10), '  ', null, () => 1)).toBe('Good morning');
});
test('greeting storage catches read/write failures and claims fallback once per tab', () => {
  const values = new Map<string, string>();
  const session = () => ({ getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => { values.set(key, value); } });
  const blocked = (): Storage => { throw new Error('blocked'); };
  const state = { day: '2026-11-11', signedOut: false };
  expect(readState('a', blocked, blocked).available).toBe(false);
  expect(writeState('a', state, blocked, blocked)).toBe(false);
  expect(readState('a', blocked, session)).toEqual({ state: null, fallback: true, available: true });
  expect(writeState('a', state, blocked, session)).toBe(true);
  expect(writeState('a', { ...state, day: '2026-11-12' }, blocked, session)).toBe(false);
  expect(readState('a', blocked, session).state).toEqual(state);
  expect(readState('b', blocked, session).state).toBeNull();
  expect(writeState('b', state, session, blocked)).toBe(true);
  expect(readState('b', session, blocked).state).toEqual(state);
  values.set('home-greeting:b', '{invalid');
  expect(readState('b', session, blocked).state).toBeNull();
  const brokenReads = () => ({ getItem: () => { throw new Error('read denied'); }, setItem: () => {} });
  const memory = readState('d', brokenReads, session);
  expect(writeState('d', state, brokenReads, session, memory.fallback)).toBe(true);
  expect(readState('d', brokenReads, session).state).toEqual(state);
  const readOnly = () => ({ getItem: () => null, setItem: () => { throw new Error('quota'); } });
  expect(writeState('c', state, readOnly, session)).toBe(true);
  expect(writeState('c', state, readOnly, session)).toBe(false);
});

import { addSlot, footprint, gridColumns, pack, readingOrder, settle, type GridCell, type Placement } from '../src/ui/widgets/grid/layout';
for (const [width, columns] of [[390, 2], [650, 4], [960, 4], [1200, 5], [1450, 6], [1700, 7], [1950, 8], [2200, 9], [2500, 10], [5000, 10]]) {
  test(`Home dense packing stays bounded and fills the top at ${columns} columns (${width}px container)`, () => {
    expect(gridColumns(width!)).toBe(columns);
    const layout: Placement[] = [
      { id: 'a', size: '1x1' }, { id: 'b', size: '4x2' }, { id: 'c', size: '2x2' },
      { id: 'd', size: '1x2' }, { id: 'e', size: '2x1' }, { id: 'f', size: '1x1' },
    ];
    const covers = (cells: GridCell[], x: number, y: number) => cells.some(c => x >= c.x && x < c.x + c.w && y >= c.y && y < c.y + c.h);
    const bounded = (cells: GridCell[]) => {
      for (const cell of cells) {
        expect(cell.x).toBeGreaterThanOrEqual(0);
        expect(cell.x + cell.w).toBeLessThanOrEqual(columns!);
        expect(cells.filter(other => other.id !== cell.id && cell.x < other.x + other.w && other.x < cell.x + cell.w && cell.y < other.y + other.h && other.y < cell.y + cell.h)).toEqual([]);
      }
    };
    /** A gap is left only where no widget placed later (in reading order) could have started. */
    const dense = (cells: GridCell[]) => {
      const bottom = Math.max(...cells.map(c => c.y + c.h));
      for (let y = 0; y < bottom; y++) for (let x = 0; x < columns!; x++) {
        if (covers(cells, x, y)) continue;
        for (const later of cells.filter(c => c.y > y || (c.y === y && c.x > x))) {
          const others = cells.filter(c => c.id !== later.id && (c.y < y || (c.y === y && c.x < x)));
          let fits = x + later.w <= columns!;
          for (let dy = 0; fits && dy < later.h; dy++) for (let dx = 0; fits && dx < later.w; dx++) if (covers(others, x + dx, y + dy)) fits = false;
          expect(fits, `${later.id} fits the gap at (${x},${y})`).toBe(false);
        }
      }
    };
    const packed = pack(layout, width!);
    bounded(packed); dense(packed);
    expect(pack(layout, width!)).toEqual(packed); // deterministic
    if (columns! >= 4) expect(packed.find(c => c.id === 'b')!.w).toBe(4);
    // The settled (reading) order packs to the same picture, so a save and reload match.
    expect(pack(settle(layout, width!), width!).map(({ id, x, y }) => ({ id, x, y })).sort((p, q) => p.id.localeCompare(q.id)))
      .toEqual(packed.map(({ id, x, y }) => ({ id, x, y })).sort((p, q) => p.id.localeCompare(q.id)));
    // Growing a widget pinned at its top-left: it keeps the spot; the rest pack around it.
    const a = packed.find(c => c.id === 'a')!;
    const grown = settle(layout.map(p => p.id === 'a' ? { ...p, size: '2x2' as const } : p), width!, { id: 'a', x: a.x, y: a.y });
    const after = pack(grown, width!);
    bounded(after); dense(after);
    if (a.x + footprint('2x2', width!).w <= columns!) expect(after.find(c => c.id === 'a')).toMatchObject({ x: a.x, y: a.y });
    expect(readingOrder(after)).toEqual(grown);
    // Removing a widget never leaves a gap something later fits.
    const removed = pack(settle(layout.filter(p => p.id !== 'c'), width!), width!);
    bounded(removed); dense(removed);
    // The Add tile follows the last widget; a new 1×1 takes the first free cell.
    const slot = addSlot(after, width!);
    const last = Math.max(...after.map(c => (c.y + c.h - 1) * columns! + c.x + c.w - 1));
    expect(slot.y * columns! + slot.x).toBe(last + 1);
    const added = pack([...grown, { id: 'new', size: '1x1' }], width!);
    const fresh = added.find(c => c.id === 'new')!;
    bounded(added);
    for (let at = 0; at < fresh.y * columns! + fresh.x; at++) expect(covers(added, at % columns!, Math.floor(at / columns!))).toBe(true);
  });
}

test('V0.9: Design "Waiting on me" counts a request once when it is both assigned to me and awaiting my approval', () => {
  const me = { id: 'u1', name: 'Me' };
  const item = (id: string, over: Partial<DesignRequestItem>) => ({ id, stage: 'new', dueDate: null, updatedAt: '2026-11-01T00:00:00Z', requester: { id: 'other', name: 'O' }, assignee: null, ...over }) as DesignRequestItem;
  const items = [
    item('both', { stage: 'in-review', requester: me, assignee: me }), // assigned to me AND to approve
    item('assigned', { stage: 'in-progress', assignee: me }),
    item('approve', { stage: 'in-review', requester: me, assignee: { id: 'x', name: 'X' } }),
    item('delivered', { stage: 'delivered', assignee: me }),
  ];
  expect(designStats(items, 'u1', '2026-11-13').mine).toBe(3);
});
