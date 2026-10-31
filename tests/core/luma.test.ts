import { expect, it } from 'vitest';
import { parseGuests, parseReflections } from '../../src/core/luma/csv';
it('handles quoted commas, BOM, CRLF, varied headers and normalized email', () => {
  const result = parseGuests(
    '\uFEFFFull Name,Email Address,Approval Status,Registered At,Checked In At\r\n"Lee, Ann",ANN@EXAMPLE.ORG,approved,2026-10-01T01:00:00Z,2026-10-02T01:00:00Z\r\n',
  );
  expect(result.errors).toEqual([]);
  expect(result.rows[0]).toMatchObject({
    name: 'Lee, Ann',
    email: 'ann@example.org',
    approvalStatus: 'approved',
  });
});
it('rejects malformed CSV, bad email and dates with row evidence', () => {
  expect(() => parseGuests('name,email\n"unfinished,x')).toThrow();
  expect(parseGuests('email,checked_in_at\nx,not a date').errors).toHaveLength(
    1,
  );
  expect(
    parseReflections('Email,Timestamp\na@b.org,2026-10-02T10:00:00Z').rows,
  ).toHaveLength(1);
  expect(() => parseReflections('Name\nAnn')).toThrow(/email/);
});

it('excludes pending approval and invitation statuses from registrations', async () => {
  const { isRegistered } = await import('../../src/core/luma/status');
  for (const status of [
    'pending_approval',
    'invited',
    'waitlisted',
    'declined',
  ])
    expect(isRegistered(status)).toBe(false);
  expect(isRegistered('invited', true)).toBe(true);
  expect(isRegistered('approved')).toBe(true);
});
it('interprets survey timestamps in Pacific time and tracks physical CSV rows', () => {
  expect(
    parseReflections('email,timestamp\na@b.org,10/31/2026 11:30:00 PM').rows[0]
      ?.submittedAt,
  ).toBe('2026-11-01T06:30:00.000Z');
  expect(
    parseReflections('email,timestamp\n\na@b.org,bad').errors[0]?.row,
  ).toBe(3);
  expect(
    parseReflections('email,timestamp\na@b.org,03/08/2026 02:30:00').errors,
  ).toHaveLength(1);
});
