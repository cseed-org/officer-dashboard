import { describe, expect, it } from 'vitest';
import {
  postEntry,
  reverseEntry,
  closePeriod,
  releaseRestriction,
  recordContribution,
} from '../../../src/core/accounting/engine';
const accounts = [
  { id: 'cash', type: 'asset' as const, active: true },
  { id: 'rev', type: 'revenue' as const, active: true },
  { id: 'expense', type: 'expense' as const, active: true },
  {
    id: 'net',
    type: 'net_asset' as const,
    active: true,
    restriction: 'without_donor' as const,
  },
  {
    id: 'netr',
    type: 'net_asset' as const,
    active: true,
    restriction: 'with_donor' as const,
  },
];
const context = {
  accounts,
  funds: [
    { id: 'gen', restriction: 'without_donor' as const, active: true },
    { id: 'grant', restriction: 'with_donor' as const, active: true },
  ],
  periods: [
    {
      id: 'fy',
      startDate: '2026-07-01',
      endDate: '2027-06-30',
      status: 'open' as const,
    },
  ],
};
const input = {
  id: 'e',
  entryDate: '2026-09-01',
  memo: 'Gift',
  source: 'contribution' as const,
  lines: [
    { accountId: 'cash', fundId: 'gen', debitCents: 100, creditCents: 0 },
    { accountId: 'rev', fundId: 'gen', debitCents: 0, creditCents: 100 },
  ],
};
describe('posting engine', () => {
  it('balances, freezes posted data and reverses without editing', () => {
    const entry = postEntry(input, context);
    expect(Object.isFrozen(entry.lines[0])).toBe(true);
    const reverse = reverseEntry(
      entry,
      { id: 'r', entryDate: '2026-09-02' },
      context,
    );
    expect(reverse.reversesEntryId).toBe('e');
    expect(reverse.lines[0]?.creditCents).toBe(100);
    expect(entry.lines[0]?.debitCents).toBe(100);
  });
  it('rejects imbalance, fractions, unsafe cents, missing periods and closed periods', () => {
    for (const amount of [99, 0.5, Number.MAX_SAFE_INTEGER + 1])
      expect(() =>
        postEntry(
          {
            ...input,
            lines: [
              input.lines[0]!,
              { ...input.lines[1]!, creditCents: amount },
            ],
          },
          context,
        ),
      ).toThrow();
    expect(() => postEntry(input, { ...context, periods: [] })).toThrow(
      /open period/,
    );
    expect(() =>
      postEntry(input, {
        ...context,
        periods: [closePeriod(context.periods[0]!, 'closed')],
      }),
    ).toThrow();
  });
  it('requires an expense functional class', () => {
    expect(() =>
      postEntry(
        {
          ...input,
          lines: [
            { ...input.lines[0]!, accountId: 'expense' },
            input.lines[1]!,
          ],
        },
        context,
      ),
    ).toThrow(/functional/);
  });
  it('releases between donor classes and flags contributions', () => {
    const release = releaseRestriction(
      {
        id: 'release',
        entryDate: input.entryDate,
        memo: 'Satisfied purpose',
        amountCents: 50,
        fundId: 'grant',
        restrictedNetAssetAccountId: 'netr',
        unrestrictedNetAssetAccountId: 'net',
        availableRestrictedCents: 100,
      },
      context,
    );
    expect(release.lines.map((l) => l.accountId)).toEqual(['netr', 'net']);
    expect(release.lines.every((l) => l.fundId === 'grant')).toBe(true);
    const gift = recordContribution(
      {
        ...input,
        lines: input.lines.map((l) => ({
          ...l,
          debitCents: l.debitCents * 250,
          creditCents: l.creditCents * 250,
        })),
        amountCents: 25000,
        quidProQuoCents: 8000,
        kind: 'in_kind',
        inKindDescription: 'Equipment',
      },
      context,
    );
    expect(gift.acknowledgmentRequired).toBe(true);
    expect(gift.quidProQuoDisclosureRequired).toBe(true);
  });
});
