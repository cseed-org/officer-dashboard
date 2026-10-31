import { describe, expect, it } from 'vitest';
import { averageRate, eventMetrics, matchReflections, normalizeEmail } from '../../src/core/metrics';
import { EventCounts } from '../../src/core/contracts';

describe('normalizeEmail', () => {
  it('trims and lower-cases', () => {
    expect(normalizeEmail('  Ada.Lovelace@Example.EDU \n')).toBe('ada.lovelace@example.edu');
  });
});

describe('matchReflections', () => {
  it('matches reflections to checked-in guests by normalized email', () => {
    expect(
      matchReflections({
        checkedInEmails: ['a@x.edu', 'B@x.edu', 'c@x.edu'],
        reflectionEmails: [' b@X.edu', 'a@x.edu', 'walkin@x.edu'],
      }),
    ).toEqual({ matched: 2, unmatched: 1 });
  });

  it('counts a person who submitted twice once', () => {
    expect(
      matchReflections({ checkedInEmails: ['a@x.edu'], reflectionEmails: ['a@x.edu', 'A@x.edu ', 'z@x.edu', 'Z@x.edu'] }),
    ).toEqual({ matched: 1, unmatched: 1 });
  });
});

describe('eventMetrics', () => {
  it('computes check-in rate over registered and reflection rate over checked-in', () => {
    expect(eventMetrics({ registered: 40, checkedIn: 30, reflections: 12, matchedReflections: 12 })).toEqual({
      registered: 40,
      checkedIn: 30,
      reflections: 12,
      matchedReflections: 12,
      unmatchedReflections: 0,
      checkInRate: 0.75,
      reflectionRate: 0.4,
    });
  });

  // Review finding 5: 3 reflections but only 2 check-ins (a missed door scan) gave 1.5.
  it('only counts reflections from checked-in guests, and reports the rest separately', () => {
    const m = eventMetrics({ registered: 10, checkedIn: 2, reflections: 3, matchedReflections: 2 });
    expect(m.reflectionRate).toBe(1);
    expect(m.unmatchedReflections).toBe(1);
    expect(EventCounts.safeParse(m).success).toBe(true);
  });

  it('rejects counts where more reflections matched than there were check-ins', () => {
    expect(() => eventMetrics({ registered: 10, checkedIn: 2, reflections: 3, matchedReflections: 3 })).toThrow();
  });

  it('reports null rates when there is nothing to divide by', () => {
    const m = eventMetrics({ registered: 0, checkedIn: 0, reflections: 4, matchedReflections: 0 });
    expect(m.checkInRate).toBeNull();
    expect(m.reflectionRate).toBeNull();
    expect(m.unmatchedReflections).toBe(4);
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
