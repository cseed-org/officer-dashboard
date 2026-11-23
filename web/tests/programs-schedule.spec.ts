import { expect, test } from '@playwright/test';
import type { Program, ProgramException } from '../src/lib/contracts';
import { laIso, laNow, laParts, nextSession, programSessions } from '../src/ui/views/buildspace/programSchedule';

/**
 * Program sessions in date order (PR #99 review F-1): a session is listed and counted by the
 * date it actually happens on, so a move past the following week, a move of a past session to
 * a later day, and a session that already ended today all land in the right place.
 * Pure functions: no browser or server needed.
 */

// Wednesdays 18:00–19:30 PT, Nov 4 – Dec 30, 2026 (Sunday = 0, so Wednesday = 3).
const base: Program = {
  id: 'p1', trackId: 'buildspace', name: 'Build night', weekday: 3, startTime: '18:00', durationMin: 90,
  location: 'The Nest', startsOn: '2026-11-04', endsOn: '2026-12-30', description: '', createdAt: 0, updatedAt: 0, exceptions: [],
};
const move = (onDate: string, day: string, start: string, end: string, newLocation: string | null = null): ProgramException => ({
  programId: 'p1', onDate, kind: 'move', newStartsAt: laIso(day, start), newEndsAt: laIso(day, end), newLocation,
});
const skip = (onDate: string): ProgramException => ({ programId: 'p1', onDate, kind: 'skip', newStartsAt: null, newEndsAt: null, newLocation: null });
const at = (today: string, time = '12:00') => ({ today, minutes: Number(time.slice(0, 2)) * 60 + Number(time.slice(3)) });
const days = (p: Program, now: ReturnType<typeof at>, limit = 12) => programSessions(p, now, limit).map((s) => s.on);

test('sessions start at the first scheduled weekday and end at the last date', () => {
  expect(days(base, at('2026-11-01'))).toEqual(['2026-11-04', '2026-11-11', '2026-11-18', '2026-11-25', '2026-12-02', '2026-12-09', '2026-12-16', '2026-12-23', '2026-12-30']);
  expect(days(base, at('2026-12-31'))).toEqual([]);
});

test('a session moved past the following week is listed on its new date, and the next session is the real next one', () => {
  // Sunday Nov 22: the Nov 25 session moves to Dec 9 (two weeks on).
  const p = { ...base, exceptions: [move('2026-11-25', '2026-12-09', '19:00', '20:30', 'Room 250')] };
  const now = at('2026-11-22');
  const list = programSessions(p, now, 4);
  expect(list.map((s) => [s.on, s.date])).toEqual([
    ['2026-12-02', '2026-12-02'],
    ['2026-12-09', '2026-12-09'],
    ['2026-12-09', '2026-11-25'],
    ['2026-12-16', '2026-12-16'],
  ]);
  expect(list[2]).toMatchObject({ start: '19:00', end: '20:30', location: 'Room 250' });
  expect(nextSession(p, now)?.on).toBe('2026-12-02');
});

test('a past session moved to a later day stays listed (so it can be undone) and can be the next session', () => {
  // Thursday Nov 19: last Wednesday's (Nov 18) session was moved to Saturday Nov 21.
  const p = { ...base, exceptions: [move('2026-11-18', '2026-11-21', '10:00', '12:00')] };
  const now = at('2026-11-19');
  expect(days(p, now, 2)).toEqual(['2026-11-21', '2026-11-25']);
  expect(nextSession(p, now)).toMatchObject({ on: '2026-11-21', date: '2026-11-18' });
});

test('a session moved to a day that has passed is not upcoming', () => {
  const p = { ...base, exceptions: [move('2026-11-25', '2026-11-20', '18:00', '19:30')] };
  expect(days(p, at('2026-11-22'), 2)).toEqual(['2026-12-02', '2026-12-09']);
});

test('today counts until the session ends; skipped sessions stay listed but are never next', () => {
  expect(days(base, at('2026-11-25', '19:29'), 1)).toEqual(['2026-11-25']);
  expect(days(base, at('2026-11-25', '19:30'), 1)).toEqual(['2026-12-02']);
  const p = { ...base, exceptions: [skip('2026-11-25')] };
  expect(programSessions(p, at('2026-11-22'), 1)[0]).toMatchObject({ on: '2026-11-25', exception: { kind: 'skip' } });
  expect(nextSession(p, at('2026-11-22'))?.on).toBe('2026-12-02');
});

test('a session that crosses midnight counts until it ends the next day', () => {
  const late = { ...base, startTime: '23:00', durationMin: 120 };
  const s = programSessions(late, at('2026-11-25', '23:30'), 1)[0]!;
  expect(s).toMatchObject({ on: '2026-11-25', start: '23:00', end: '01:00' });
  // Still running just after midnight (review N-2), over once it ends at 01:00.
  expect(days(late, at('2026-11-26', '00:30'), 1)).toEqual(['2026-11-25']);
  expect(nextSession(late, at('2026-11-26', '00:59'))?.on).toBe('2026-11-25');
  expect(days(late, at('2026-11-26', '01:00'), 1)).toEqual(['2026-12-02']);
  // A move that ends after midnight behaves the same.
  const moved = { ...base, exceptions: [{ ...move('2026-11-25', '2026-11-25', '23:00', '00:30'), newEndsAt: laIso('2026-11-26', '00:30') }] };
  expect(days(moved, at('2026-11-26', '00:15'), 1)).toEqual(['2026-11-25']);
  expect(days(moved, at('2026-11-26', '00:30'), 1)).toEqual(['2026-12-02']);
});

test('club wall time round-trips through instants across the DST change', () => {
  expect(laIso('2026-11-25', '18:00')).toBe('2026-11-25T18:00:00-08:00');
  expect(laIso('2026-07-01', '18:00')).toBe('2026-07-01T18:00:00-07:00');
  expect(laParts('2026-11-26T02:00:00Z')).toEqual({ date: '2026-11-25', time: '18:00' });
  expect(laNow(new Date('2026-11-26T02:30:00Z'))).toEqual({ today: '2026-11-25', minutes: 18 * 60 + 30 });
});
