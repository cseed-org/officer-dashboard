import { describe, expect, it } from 'vitest';
import { validateJournalLines } from '../../src/core/journal';

const L = (debitCents: number, creditCents: number, accountId = 'a', fundId = 'f') => ({
  accountId,
  fundId,
  debitCents,
  creditCents,
});

describe('validateJournalLines', () => {
  it('accepts a balanced two-line entry', () => {
    expect(validateJournalLines([L(500, 0), L(0, 500)])).toEqual({ ok: true, totalCents: 500 });
  });

  it('accepts a balanced split entry', () => {
    expect(validateJournalLines([L(300, 0), L(200, 0), L(0, 500)])).toEqual({ ok: true, totalCents: 500 });
  });

  it('rejects an unbalanced entry and says by how much', () => {
    const r = validateJournalLines([L(500, 0), L(0, 499)]);
    expect(r.ok).toBe(false);
    expect(!r.ok && r.errors).toContain('debits exceed credits by 1 cents');
  });

  it('rejects fewer than two lines', () => {
    const r = validateJournalLines([L(500, 0)]);
    expect(!r.ok && r.errors).toContain('an entry needs at least two lines');
  });

  it('rejects lines with both sides, neither side, negatives or fractions', () => {
    const r = validateJournalLines([L(5, 5), L(0, 0), L(-1, 0), L(1.5, 0)]);
    expect(!r.ok && r.errors).toEqual(
      expect.arrayContaining([
        'line 1: exactly one of debit or credit must be non-zero',
        'line 2: exactly one of debit or credit must be non-zero',
        'line 3: amounts must be non-negative integer cents',
        'line 4: amounts must be non-negative integer cents',
      ]),
    );
  });
});
