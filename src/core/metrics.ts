/**
 * Event health ratios. A rate is a fraction in [0, 1], or null when the denominator is 0.
 *
 * Reflection rate = reflections from guests who checked in ÷ checked-in guests.
 * Reflections and check-ins are imported separately (Luma guest export vs survey export),
 * so some reflections may not match a checked-in guest (a missed door scan, a typo'd
 * email). Those are excluded from the rate, keeping it within 0..1, and reported as
 * `unmatchedReflections` so the data problem stays visible.
 */

export interface EventCountsInput {
  registered: number;
  checkedIn: number;
  /** All reflections received (distinct respondents). */
  reflections: number;
  /** Reflections whose normalized email matches a checked-in guest. */
  matchedReflections: number;
}

export interface EventMetrics extends EventCountsInput {
  /** reflections − matchedReflections */
  unmatchedReflections: number;
  /** checkedIn / registered */
  checkInRate: number | null;
  /** matchedReflections / checkedIn */
  reflectionRate: number | null;
}

/** How emails are compared across Luma and survey exports: trimmed, lower-cased. */
export function normalizeEmail(email: string): string {
  return email.trim().toLowerCase();
}

/** Count distinct reflection respondents that did / didn't check in. */
export function matchReflections(input: {
  checkedInEmails: Iterable<string>;
  reflectionEmails: Iterable<string>;
}): { matched: number; unmatched: number } {
  const checkedIn = new Set([...input.checkedInEmails].map(normalizeEmail));
  const respondents = new Set([...input.reflectionEmails].map(normalizeEmail));
  let matched = 0;
  for (const email of respondents) if (checkedIn.has(email)) matched++;
  return { matched, unmatched: respondents.size - matched };
}

const ratio = (num: number, den: number): number | null => (den > 0 ? num / den : null);

export function eventMetrics(c: EventCountsInput): EventMetrics {
  if (c.matchedReflections > c.checkedIn || c.matchedReflections > c.reflections) {
    throw new RangeError('matchedReflections cannot exceed checkedIn or reflections');
  }
  return {
    ...c,
    unmatchedReflections: c.reflections - c.matchedReflections,
    checkInRate: ratio(c.checkedIn, c.registered),
    reflectionRate: ratio(c.matchedReflections, c.checkedIn),
  };
}

/** Mean of the defined rates (unweighted, one vote per event). */
export function averageRate(rates: readonly (number | null)[]): number | null {
  const defined = rates.filter((r): r is number => r !== null);
  return defined.length ? defined.reduce((a, b) => a + b, 0) / defined.length : null;
}
