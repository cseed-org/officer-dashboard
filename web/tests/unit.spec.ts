import { expect, test } from '@playwright/test';
import { ApiError as BrowserApiError } from '../src/lib/contracts';
import { ApiError as CanonicalApiError } from '../../src/core/contracts';
import { byDesigner, designMoveTargets, designStats, designTool, laneOf, laneTarget, thisWeek } from '../src/lib/design';
import type { CalendarFeedItem, DesignRequestItem } from '../src/lib/contracts';
import { isGradMonth, isLinkedIn } from '../src/lib/profileRules';
import { normalizeHref } from '../src/lib/routes';
import { sanitizeResults } from '../src/lib/search';
import { internalPath, internalRoute, safeImageUrl, safeUrl } from '../src/lib/url';
import { migrateLayout, moveTo, nearestSize } from '../src/ui/widgets/layout';
import { defaultWidgetLayout, layoutForChoices } from '../src/ui/widgets/catalog';
import { mergeActivity } from '../src/ui/components/LoadHistory';
import { byDay, clipSpan, entryDays, hourRange, overlapLanes, type CalEntry } from '../src/ui/components/calendar/layout';
import { buildRepeat, occurrenceKey, orgEntry, overlayGroups, overlayTitle, parseRepeat, prefill, readToggles, stepView, viewRange } from '../src/lib/orgcal';
import type { OrgCalendarEvent } from '../src/lib/contracts';

/** Pure-function tests (no browser); run once, in the desktop project. */
test.beforeEach(({}, info) => test.skip(info.project.name !== 'desktop', 'unit tests run once'));
const O = 'http://localhost:4340';

test('M-012: API failures including oversized MCP bodies match both contracts', () => {
  for (const code of ['bad_request', 'unauthenticated', 'forbidden', 'not_found', 'conflict', 'invalid_transition', 'import_failed', 'internal', 'onboarding_required', 'over_tolerance', 'archived', 'rate_limited', 'not_configured', 'too_large']) {
    const body = { error: { code, message: 'Server failure' } };
    expect(BrowserApiError.safeParse(body).success, code).toBe(true);
    expect(CanonicalApiError.safeParse(body).success, code).toBe(true);
  }
});

test('activity history merges in timeline order across page boundaries', () => {
  const old = [
    { id: 'live-old', date: '2026-01-01', createdAt: 50 },
    { id: 'same-b', date: '2026-10-20', createdAt: 10 },
  ];
  const more = [
    { id: 'newer', date: '2026-11-01', createdAt: 1 },
    { id: 'same-new', date: '2026-10-20', createdAt: 20 },
    { id: 'same-a', date: '2026-10-20', createdAt: 10 },
  ];
  expect(mergeActivity(old, more).map((a) => a.id)).toEqual(['newer', 'same-new', 'same-a', 'same-b', 'live-old']);
});

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
  const me = { id: 'u1', name: 'Me', avatarUrl: null };
  const item = (id: string, over: Partial<DesignRequestItem>) => ({ id, stage: 'new', dueDate: null, updatedAt: '2026-11-01T00:00:00Z', requester: { id: 'other', name: 'O', avatarUrl: null }, assignee: null, ...over }) as DesignRequestItem;
  const items = [
    item('both', { stage: 'in-review', requester: me, assignee: me }), // assigned to me AND to approve
    item('assigned', { stage: 'in-progress', assignee: me }),
    item('approve', { stage: 'in-review', requester: me, assignee: { id: 'x', name: 'X', avatarUrl: null } }),
    item('delivered', { stage: 'delivered', assignee: me }),
  ];
  expect(designStats(items, 'u1', '2026-11-13').mine).toBe(3);
});

test('V0.10: Design lanes map the five stages and only offer moves the viewer may make', () => {
  expect((['new', 'accepted', 'in-progress', 'in-review', 'delivered'] as const).map(laneOf)).toEqual(['todo', 'todo', 'doing', 'review', 'done']);
  const item = (stage: DesignRequestItem['stage'], requester = 'req') => ({ id: 'i', stage, requester: { id: requester, name: 'R', avatarUrl: null }, assignee: null }) as unknown as DesignRequestItem;
  const director = { id: 'd', isDesigner: false, isWorker: true, isLeadership: false };
  const leadership = { ...director, isLeadership: true };
  const officer = { id: 'req', isDesigner: false, isWorker: false, isLeadership: false };
  // To do prefers Accepted; a card never "moves" into its own lane.
  expect(laneTarget(item('in-progress'), 'todo', director)).toBe('accepted');
  expect(laneTarget(item('new'), 'todo', director)).toBeNull();
  // Only leadership drops into Done (as an override); everyone else waits for the requester.
  expect(laneTarget(item('in-review'), 'done', director)).toBeNull();
  expect(laneTarget(item('in-review'), 'done', leadership)).toBe('delivered');
  // The design team reopens delivered work into any earlier lane (the Done toast's Undo uses the exact stage).
  expect(laneTarget(item('delivered'), 'review', director)).toBe('in-review');
  expect(laneTarget(item('delivered'), 'doing', director)).toBe('in-progress');
  expect(laneTarget(item('delivered'), 'todo', director)).toBe('accepted');
  expect(designMoveTargets(item('delivered'), director)).toEqual(['new', 'accepted', 'in-progress', 'in-review']);
  // A requester outside the team can't reopen from the board (their approval Undo lives in the detail).
  expect(designMoveTargets(item('delivered', 'req'), officer)).toEqual([]);
  // A requester outside the design team may only send their review back to Doing.
  expect(laneTarget(item('in-review'), 'doing', officer)).toBe('in-progress');
  expect(laneTarget(item('new'), 'doing', officer)).toBeNull();
});

test('V0.10: DesignLinks picks Figma or Canva from the URL host', () => {
  expect(designTool('https://www.figma.com/design/abc/Poster')).toBe('figma');
  expect(designTool('https://figma.com/file/x')).toBe('figma');
  expect(designTool('https://www.canva.com/design/DAF/edit')).toBe('canva');
  expect(designTool('https://canva.link/abc')).toBe('canva');
  expect(designTool('https://evilfigma.com/x')).toBe('file');
  expect(designTool('https://drive.google.com/drive/folders/1')).toBe('file');
  expect(designTool('not a url')).toBe('file');
  expect(designTool(null)).toBe('file');
});

test('V0.10: This week and designer groups on the Design board', () => {
  const me = { id: 'u1', name: 'Me', avatarUrl: null };
  const item = (id: string, over: Partial<DesignRequestItem>) => ({ id, stage: 'accepted', dueDate: null, updatedAt: '2026-11-01T00:00:00Z', requester: { id: 'o', name: 'O', avatarUrl: null }, assignee: null, ...over }) as DesignRequestItem;
  const items = [
    item('late', { dueDate: '2026-11-10' }),
    item('soon', { dueDate: '2026-11-19' }),
    item('later', { dueDate: '2026-11-21' }),
    item('approve', { stage: 'in-review', requester: me }),
    item('done', { stage: 'delivered', dueDate: '2026-11-12' }),
  ];
  expect(thisWeek(items, 'u1', '2026-11-13').map((i) => i.id)).toEqual(['late', 'soon', 'approve']);
  const groups = byDesigner([item('a', { assignee: { id: 'z', name: 'Zed', avatarUrl: null } }), item('b', {}), item('c', { assignee: me }), item('d', { assignee: { id: 'y', name: 'Ann', avatarUrl: null } })], 'u1');
  expect(groups.map((g) => g.name)).toEqual(['You', 'Ann', 'Zed', 'Unassigned']);
});

const calendarItem = (id: string, date: string, startsAt: string | null = null, endsAt: string | null = null): CalendarFeedItem => ({
  id, source: 'events', type: 'event', title: id, date, startsAt, endsAt, lumaUrl: null, location: null, href: `/events?id=${id}`,
});

test('M-086: bucketed calendar items equal the old filter for 200 items with ≤ one end conversion each', async () => {
  const { bucketCalendarItems } = await import('../src/ui/widgets/calendar/CalendarWidget');
  const day = (offset: number) => new Date(Date.UTC(2026, 9, 26 + offset)).toISOString().slice(0, 10);
  const feed = Array.from({ length: 200 }, (_, i) => {
    const date = day(i % 42);
    const timed = i % 10 !== 0;
    return calendarItem(`item-${i}`, date, timed ? `${date}T20:00:00Z` : null, timed ? `${day(i % 42 + 2)}T20:00:00Z` : null);
  });
  const original = Intl.DateTimeFormat.prototype.formatToParts;
  let calls = 0;
  Intl.DateTimeFormat.prototype.formatToParts = function (date) {
    calls++;
    return original.call(this, date);
  };
  let buckets: Map<string, CalendarFeedItem[]>;
  try {
    buckets = bucketCalendarItems(feed, day(0), day(42));
  } finally {
    Intl.DateTimeFormat.prototype.formatToParts = original;
  }
  expect(calls, 'one exclusive-end conversion per timed item, not 42 per item').toBe(180);
  const oldItemsOn = (date: string) => feed.filter((item) => {
    if (!item.startsAt) return item.date === date;
    const last = item.endsAt ? new Date(new Date(item.endsAt).getTime() - 1) : null;
    const parts = last ? Object.fromEntries(new Intl.DateTimeFormat('en-US', { timeZone: 'America/Los_Angeles', hourCycle: 'h23', year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(last).map((x) => [x.type, x.value])) : null;
    const lastDate = parts ? `${parts.year}-${parts.month}-${parts.day}` : item.date;
    return item.date <= date && lastDate >= date;
  }).sort((a, b) => (a.startsAt ?? '').localeCompare(b.startsAt ?? ''));
  for (let i = 0; i < 42; i++) expect(buckets.get(day(i)) ?? [], day(i)).toEqual(oldItemsOn(day(i)));
});

test('M-086: multi-day spans and exclusive-midnight ends keep their dates', async () => {
  const { bucketCalendarItems } = await import('../src/ui/widgets/calendar/CalendarWidget');
  const items = [
    calendarItem('dst-span', '2026-10-31', '2026-11-01T06:00:00Z', '2026-11-02T10:00:00Z'),
    calendarItem('midnight', '2026-11-01', '2026-11-02T07:30:00Z', '2026-11-02T08:00:00Z'),
    calendarItem('after-midnight', '2026-11-01', '2026-11-02T07:30:00Z', '2026-11-02T08:00:00.001Z'),
    calendarItem('date-only', '2026-10-30'),
  ];
  const buckets = bucketCalendarItems(items, '2026-10-31', '2026-11-04');
  const dates = (id: string) => [...buckets.entries()].filter(([, list]) => list.some((item) => item.id === id)).map(([date]) => date);
  expect(dates('dst-span')).toEqual(['2026-10-31', '2026-11-01', '2026-11-02']);
  expect(dates('midnight')).toEqual(['2026-11-01']);
  expect(dates('after-midnight')).toEqual(['2026-11-01', '2026-11-02']);
  expect(dates('date-only')).toEqual([]); // clipped before the visible range
});

// ───────────── Org calendar (V0.15): shared calendar layout and /calendar helpers ─────────────

const orgEvent = (over: Partial<OrgCalendarEvent>): OrgCalendarEvent => ({
  id: 'm1', calendarKey: 'internal', title: 'Sync', description: '', location: '', startsAt: '2027-03-10T10:00:00-08:00', endsAt: '2027-03-10T11:00:00-08:00',
  allDay: false, tentativeFrom: null, tentativeTo: null, rrule: null, reminders: [], sendInvites: false, invitees: [], sourceKind: 'manual', sourceId: null, sourceHref: null,
  googleEventId: null, googleEtag: null, createdBy: null, updatedBy: null, createdAt: 0, updatedAt: 0, ...over,
});

test('calendar layout: overlapping blocks share columns; separate clusters stay full width', () => {
  // 10:00–11:00 and 10:30–11:30 overlap; 13:00 stands alone.
  expect(overlapLanes([{ start: 2, len: 2 }, { start: 3, len: 2 }, { start: 8, len: 2 }])).toEqual([{ lane: 0, lanes: 2 }, { lane: 1, lanes: 2 }, { lane: 0, lanes: 1 }]);
  // A long block with two later ones beside it: three in a chain need only two columns.
  expect(overlapLanes([{ start: 0, len: 6 }, { start: 1, len: 2 }, { start: 3, len: 2 }])).toEqual([{ lane: 0, lanes: 2 }, { lane: 1, lanes: 2 }, { lane: 1, lanes: 2 }]);
  // Touching (one ends as the next starts) is not overlapping.
  expect(overlapLanes([{ start: 0, len: 2 }, { start: 2, len: 2 }])).toEqual([{ lane: 0, lanes: 1 }, { lane: 0, lanes: 1 }]);
  expect(overlapLanes([])).toEqual([]);
});

test('calendar layout: spans clip to the week; days bucket spans first, then by start', () => {
  expect(clipSpan('2027-03-09', '2027-03-12', '2027-03-08')).toEqual({ start: 1, len: 4 });
  expect(clipSpan('2027-03-01', '2027-03-20', '2027-03-08')).toEqual({ start: 0, len: 7 });
  expect(clipSpan('2027-03-14', '2027-03-16', '2027-03-08')).toEqual({ start: 6, len: 1 });
  expect(clipSpan('2027-03-15', '2027-03-16', '2027-03-08')).toBeNull();
  const span: CalEntry = { id: 's', title: 'Retreat', from: '2027-03-09', to: '2027-03-11', tentative: true };
  const late: CalEntry = { id: 'b', title: 'Late', startsAt: '2027-03-10T18:00:00-08:00' };
  const early: CalEntry = { id: 'a', title: 'Early', startsAt: '2027-03-10T09:00:00-08:00' };
  expect(entryDays(span)).toEqual(['2027-03-09', '2027-03-10', '2027-03-11']);
  expect(entryDays(late)).toEqual(['2027-03-10']);
  const days = byDay([late, span, early]);
  expect([...days.keys()]).toEqual(['2027-03-09', '2027-03-10', '2027-03-11']);
  expect(days.get('2027-03-10')!.map((e) => e.id)).toEqual(['s', 'a', 'b']);
  // Hours widen from 9–21 to fit an early start and a late end.
  expect(hourRange([early, late])).toEqual([9, 21]);
  expect(hourRange([{ id: 'x', title: 'x', startsAt: '2027-03-10T07:30:00-08:00', endsAt: '2027-03-10T22:15:00-08:00' }])).toEqual([7, 23]);
});

test('org calendar: items become entries (timed, all-day, tentative, locked)', () => {
  const timed = orgEntry(orgEvent({}), 'blue', 'Internal Meetings');
  expect(timed).toMatchObject({ id: 'm1~2027-03-10T10:00:00-08:00', startsAt: '2027-03-10T10:00:00-08:00', hue: 'blue', locked: false });
  const allDay = orgEntry(orgEvent({ startsAt: '2027-03-12', endsAt: '2027-03-14', allDay: true }), 'teal', 'External Meetings');
  expect(allDay).toMatchObject({ from: '2027-03-12', to: '2027-03-13' });
  expect(allDay.startsAt).toBeUndefined();
  const tentative = orgEntry(orgEvent({ startsAt: null, endsAt: null, allDay: true, tentativeFrom: '2027-03-09', tentativeTo: '2027-03-12' }), 'blue', 'Internal Meetings');
  expect(tentative).toMatchObject({ from: '2027-03-09', to: '2027-03-12', tentative: true, agendaWhen: 'Tentative · Mar 9 – Mar 12' });
  const locked = orgEntry(orgEvent({ calendarKey: 'events', sourceKind: 'event', title: '[Draft] Social' }), 'coral', 'Events');
  expect(locked).toMatchObject({ locked: true, draft: true });
  // Recurring occurrences share the master id, so they are keyed by start too.
  expect(occurrenceKey({ id: 'm1', startsAt: null, tentativeFrom: '2027-03-09' })).toBe('m1~2027-03-09');
});

test('org calendar: recurrence round-trips through RRULE text', () => {
  expect(buildRepeat('none', '')).toBeNull();
  expect(buildRepeat('weekly', '')).toBe('RRULE:FREQ=WEEKLY');
  expect(buildRepeat('biweekly', '')).toBe('RRULE:FREQ=WEEKLY;INTERVAL=2');
  expect(buildRepeat('daily', '')).toBe('RRULE:FREQ=DAILY');
  expect(buildRepeat('monthly', '')).toBe('RRULE:FREQ=MONTHLY');
  // Until is the whole last day on the selected (Los Angeles) clock: 23:59:59 PDT → 06:59:59Z.
  expect(buildRepeat('weekly', '2027-06-01')).toBe('RRULE:FREQ=WEEKLY;UNTIL=20270602T065959Z');
  for (const r of ['daily', 'weekly', 'biweekly', 'monthly'] as const) expect(parseRepeat(buildRepeat(r, '2027-06-01'))).toEqual({ repeat: r, until: '2027-06-01' });
  expect(parseRepeat(null)).toEqual({ repeat: 'none', until: '' });
  // Rules the drawer can't express are kept untouched.
  const custom = 'RRULE:FREQ=WEEKLY;BYDAY=MO,WE';
  expect(parseRepeat(custom).repeat).toBe('custom');
  expect(buildRepeat('custom', '', custom)).toBe(custom);
});

test('org calendar: toggles, ranges and create prefill', () => {
  expect(readToggles(null)).toMatchObject({ internal: true, events: true, design: true, buildspace: true });
  expect(readToggles('{"internal":false,"design":false,"bogus":1}')).toMatchObject({ internal: false, external: true, design: false, purchase: true });
  expect(readToggles('not json').internal).toBe(true);
  expect(viewRange('week', '2027-03', '2027-03-08')).toEqual({ from: '2027-03-08', to: '2027-03-15' });
  expect(viewRange('month', '2027-03', '2027-03-08')).toEqual({ from: '2027-03-01', to: '2027-04-05' });
  expect(stepView('month', '2027-12', '2027-12-06', 1)).toEqual({ m: '2028-01', w: '2027-12-27' });
  expect(stepView('week', '2027-03', '2027-03-29', 1)).toEqual({ m: '2027-04', w: '2027-04-05' });
  expect(prefill('2027-03-10T14:30')).toEqual({ date: '2027-03-10', start: '14:30', end: '15:30' });
  expect(prefill('2027-03-10')).toEqual({ date: '2027-03-10', start: '10:00', end: '11:00' });
  expect(prefill('1', new Date('2027-03-10T17:20:00Z'))).toEqual({ date: '2027-03-10', start: '10:00', end: '11:00' });
});

test('org calendar: same-day overlay items group per source', () => {
  const item = (id: string, source: string, date: string, title: string, startsAt: string | null = null) => ({ id, source, type: 'x', title, date, startsAt, endsAt: null, lumaUrl: null, location: null, href: '/x' });
  const groups = overlayGroups([
    item('1', 'buildspace-weekly', '2026-11-08', 'Weekly check-in due · Alex'),
    item('2', 'buildspace-weekly', '2026-11-08', 'Weekly check-in due · Sam'),
    item('3', 'buildspace-weekly', '2026-11-15', 'Weekly check-in due · Alex'),
    item('4', 'design', '2026-11-08', 'Poster'),
    item('5', 'design', '2026-11-08', 'Slides', '2026-11-08T17:00:00Z'),
  ]);
  expect(groups.map((g) => g.map((i) => i.id))).toEqual([['1', '2'], ['3'], ['4'], ['5']]);
  expect(overlayTitle(groups[0]!)).toBe('Weekly check-in due (2)');
  expect(overlayTitle(groups[1]!)).toBe('Weekly check-in due · Alex');
});
