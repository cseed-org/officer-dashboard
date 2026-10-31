import { describe, expect, it } from 'vitest';
import { eventMetrics, averageRate } from '../../src/core/metrics';

describe('eventMetrics', () => {
  it('computes check-in rate over registered and reflection rate over checked-in', () => {
    expect(eventMetrics({ registered: 40, checkedIn: 30, reflections: 12 })).toEqual({
      registered: 40,
      checkedIn: 30,
      reflections: 12,
      checkInRate: 0.75,
      reflectionRate: 0.4,
    });
  });

  it('reports null rates when there is nothing to divide by', () => {
    const m = eventMetrics({ registered: 0, checkedIn: 0, reflections: 0 });
    expect(m.checkInRate).toBeNull();
    expect(m.reflectionRate).toBeNull();
  });
});

describe('averageRate', () => {
  it('averages only the defined rates', () => {
    expect(averageRate([0.5, null, 1])).toBe(0.75);
  });

  it('is null when no rate is defined', () => {
    expect(averageRate([null, null])).toBeNull();
    expect(averageRate([])).toBeNull();
  });
});
