/**
 * Fiscal years are named by the calendar year they end in (FY2027 = Jul 2026 – Jun 2027
 * with the default July start). CSEED's start month is an assumption until the
 * treasurer confirms it; change DEFAULT_FY_START_MONTH if it differs.
 */
export const DEFAULT_FY_START_MONTH = 7;

const ISO_DATE = /^(\d{4})-(\d{2})-(\d{2})$/;

export function fiscalYearFor(isoDate: string, startMonth = DEFAULT_FY_START_MONTH): number {
  const m = ISO_DATE.exec(isoDate);
  if (!m) throw new RangeError(`expected YYYY-MM-DD, got ${isoDate}`);
  const year = Number(m[1]);
  const month = Number(m[2]);
  return startMonth === 1 || month < startMonth ? year : year + 1;
}

const pad = (n: number) => String(n).padStart(2, '0');

export function fiscalYearRange(fy: number, startMonth = DEFAULT_FY_START_MONTH): { start: string; end: string } {
  const startYear = startMonth === 1 ? fy : fy - 1;
  const start = `${startYear}-${pad(startMonth)}-01`;
  // Day 0 of the start month, one year on = last day of the fiscal year.
  const endDate = new Date(Date.UTC(startYear + 1, startMonth - 1, 0));
  const end = endDate.toISOString().slice(0, 10);
  return { start, end };
}

/** The calendar-month accounting periods of a fiscal year, in order. */
export function monthlyPeriods(
  fy: number,
  startMonth = DEFAULT_FY_START_MONTH,
): { name: string; startDate: string; endDate: string }[] {
  const [y0, m0] = fiscalYearRange(fy, startMonth).start.split('-').map(Number) as [number, number];
  const iso = (dt: Date) => dt.toISOString().slice(0, 10);
  return Array.from({ length: 12 }, (_, i) => {
    const first = new Date(Date.UTC(y0, m0 - 1 + i, 1));
    const last = new Date(Date.UTC(y0, m0 + i, 0));
    return {
      name: first.toLocaleString('en-US', { month: 'short', year: 'numeric', timeZone: 'UTC' }),
      startDate: iso(first),
      endDate: iso(last),
    };
  });
}
