/** Event health ratios. A rate is a fraction in [0, 1], or null when the denominator is 0. */

export interface EventCounts {
  registered: number;
  checkedIn: number;
  reflections: number;
}

export interface EventMetrics extends EventCounts {
  /** checkedIn / registered */
  checkInRate: number | null;
  /** reflections / checkedIn */
  reflectionRate: number | null;
}

const ratio = (num: number, den: number): number | null => (den > 0 ? num / den : null);

export function eventMetrics(c: EventCounts): EventMetrics {
  return {
    ...c,
    checkInRate: ratio(c.checkedIn, c.registered),
    reflectionRate: ratio(c.reflections, c.checkedIn),
  };
}

/** Mean of the defined rates (unweighted, one vote per event). */
export function averageRate(rates: readonly (number | null)[]): number | null {
  const defined = rates.filter((r): r is number => r !== null);
  return defined.length ? defined.reduce((a, b) => a + b, 0) / defined.length : null;
}
