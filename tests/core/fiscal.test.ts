import { describe, expect, it } from 'vitest';
import { fiscalYearFor, fiscalYearRange } from '../../src/core/fiscal';

describe('fiscal year (July–June, named by the year it ends)', () => {
  it('puts July 2026 in FY2027', () => {
    expect(fiscalYearFor('2026-07-01')).toBe(2027);
  });

  it('puts June 2027 in FY2027', () => {
    expect(fiscalYearFor('2027-06-30')).toBe(2027);
  });

  it('puts January 2026 in FY2026', () => {
    expect(fiscalYearFor('2026-01-15')).toBe(2026);
  });

  it('gives the date range of a fiscal year', () => {
    expect(fiscalYearRange(2027)).toEqual({ start: '2026-07-01', end: '2027-06-30' });
  });

  it('supports a calendar fiscal year', () => {
    expect(fiscalYearFor('2026-07-01', 1)).toBe(2026);
    expect(fiscalYearRange(2026, 1)).toEqual({ start: '2026-01-01', end: '2026-12-31' });
  });

  it('rejects malformed dates', () => {
    expect(() => fiscalYearFor('7/1/2026')).toThrow();
  });
});
