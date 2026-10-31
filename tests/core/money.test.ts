import { describe, expect, it } from 'vitest';
import { formatCents, parseMoneyToCents, purchaseTotalCents } from '../../src/core/money';

describe('parseMoneyToCents', () => {
  it.each([
    ['12.34', 1234],
    ['$12.34', 1234],
    ['1,234.5', 123450],
    ['  $0.07 ', 7],
    ['40', 4000],
    ['-3.10', -310],
    ['($3.10)', -310],
  ])('parses %s as %i cents', (input, cents) => {
    expect(parseMoneyToCents(input)).toBe(cents);
  });

  it.each(['', 'abc', '1.234', '12.3.4', '$', '1,23'])('rejects %j', (input) => {
    expect(parseMoneyToCents(input)).toBeNull();
  });
});

describe('formatCents', () => {
  it('formats USD with cents and thousands separators', () => {
    expect(formatCents(123450)).toBe('$1,234.50');
    expect(formatCents(7)).toBe('$0.07');
    expect(formatCents(0)).toBe('$0.00');
  });

  it('formats negatives with a minus sign', () => {
    expect(formatCents(-310)).toBe('-$3.10');
  });
});

describe('purchaseTotalCents', () => {
  it('is quantity × unit cost plus tax and shipping', () => {
    expect(purchaseTotalCents({ quantity: 3, unitCostCents: 1999, taxShipCents: 550 })).toBe(6547);
  });

  it('rejects non-integer or negative inputs', () => {
    expect(() => purchaseTotalCents({ quantity: 0, unitCostCents: 100, taxShipCents: 0 })).toThrow();
    expect(() => purchaseTotalCents({ quantity: 1, unitCostCents: 1.5, taxShipCents: 0 })).toThrow();
    expect(() => purchaseTotalCents({ quantity: 1, unitCostCents: 100, taxShipCents: -1 })).toThrow();
  });
});
