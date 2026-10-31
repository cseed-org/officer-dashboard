/**
 * Pre-flight validation for a journal entry's lines, mirroring the database triggers in
 * migrations/0001_integrity_triggers.sql so the API can return friendly errors before the
 * database refuses. The posting engine itself is Phase 1.
 */
export interface JournalLineInput {
  accountId: string;
  fundId: string;
  debitCents: number;
  creditCents: number;
}

export type JournalValidation = { ok: true; totalCents: number } | { ok: false; errors: string[] };

const isCents = (n: number) => Number.isSafeInteger(n) && n >= 0;

export function validateJournalLines(lines: readonly JournalLineInput[]): JournalValidation {
  const errors: string[] = [];
  if (lines.length < 2) errors.push('an entry needs at least two lines');
  let debits = 0;
  let credits = 0;
  lines.forEach((l, i) => {
    const n = i + 1;
    if (!isCents(l.debitCents) || !isCents(l.creditCents)) {
      errors.push(`line ${n}: amounts must be non-negative integer cents`);
      return;
    }
    if ((l.debitCents === 0) === (l.creditCents === 0)) {
      errors.push(`line ${n}: exactly one of debit or credit must be non-zero`);
    }
    debits += l.debitCents;
    credits += l.creditCents;
  });
  if (debits > credits) errors.push(`debits exceed credits by ${debits - credits} cents`);
  if (credits > debits) errors.push(`credits exceed debits by ${credits - debits} cents`);
  return errors.length ? { ok: false, errors } : { ok: true, totalCents: debits };
}
