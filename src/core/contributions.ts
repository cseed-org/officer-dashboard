/**
 * IRS donor-substantiation rules for a single contribution (IRC §170(f)(8) and §6115).
 * Not tax advice; have the CPA confirm.
 */
export const ACKNOWLEDGMENT_THRESHOLD_CENTS = 25_000; // $250
export const QUID_PRO_QUO_THRESHOLD_CENTS = 7_500; // $75

export function contributionObligations(c: { amountCents: number; quidProQuoCents: number }) {
  return {
    /** Contemporaneous written acknowledgment needed for a single gift of $250 or more. */
    acknowledgmentRequired: c.amountCents >= ACKNOWLEDGMENT_THRESHOLD_CENTS,
    /** Disclosure needed when a payment over $75 is partly for goods or services. */
    quidProQuoDisclosureRequired: c.amountCents > QUID_PRO_QUO_THRESHOLD_CENTS && c.quidProQuoCents > 0,
    deductibleCents: Math.max(0, c.amountCents - c.quidProQuoCents),
  };
}
