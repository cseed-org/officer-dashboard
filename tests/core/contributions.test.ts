import { describe, expect, it } from 'vitest';
import { contributionObligations } from '../../src/core/contributions';

describe('contributionObligations', () => {
  it('requires a written acknowledgment for a single gift of $250 or more', () => {
    expect(contributionObligations({ amountCents: 25000, quidProQuoCents: 0 }).acknowledgmentRequired).toBe(true);
    expect(contributionObligations({ amountCents: 24999, quidProQuoCents: 0 }).acknowledgmentRequired).toBe(false);
  });

  it('requires a quid pro quo disclosure for payments over $75 that buy goods or services', () => {
    expect(contributionObligations({ amountCents: 10000, quidProQuoCents: 3000 })).toEqual({
      acknowledgmentRequired: false,
      quidProQuoDisclosureRequired: true,
      deductibleCents: 7000,
    });
  });

  it('needs no disclosure at exactly $75 or with no goods or services', () => {
    expect(contributionObligations({ amountCents: 7500, quidProQuoCents: 2000 }).quidProQuoDisclosureRequired).toBe(false);
    expect(contributionObligations({ amountCents: 50000, quidProQuoCents: 0 }).quidProQuoDisclosureRequired).toBe(false);
  });
});
