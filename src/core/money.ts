/** All money is integer cents. These helpers are the only place dollars appear. */

const MONEY_RE = /^(-)?\$?(\d{1,3}(?:,\d{3})+|\d+)(?:\.(\d{1,2}))?$/;

/**
 * "$1,234.50" → 123450. Accepts an optional $, thousands separators, up to two decimals,
 * a leading minus or accounting parentheses. Returns null for anything else.
 */
export function parseMoneyToCents(input: string): number | null {
  let s = input.trim();
  let negative = false;
  const paren = /^\((.*)\)$/.exec(s);
  if (paren) {
    negative = true;
    s = paren[1]!.trim();
  }
  const m = MONEY_RE.exec(s);
  if (!m) return null;
  if (m[1]) negative = true;
  const dollars = Number(m[2]!.replaceAll(',', ''));
  const cents = Number((m[3] ?? '').padEnd(2, '0'));
  const total = dollars * 100 + cents;
  if (!Number.isSafeInteger(total)) return null;
  return negative ? -total : total;
}

const usd = new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD' });

/** 123450 → "$1,234.50"; -310 → "-$3.10". */
export function formatCents(cents: number): string {
  return usd.format(cents / 100);
}

const assertInt = (name: string, value: number, min: number) => {
  if (!Number.isSafeInteger(value) || value < min) {
    throw new RangeError(`${name} must be an integer ≥ ${min}, got ${value}`);
  }
};

/** Matches the purchase_requests_total CHECK constraint. */
export function purchaseTotalCents(p: { quantity: number; unitCostCents: number; taxShipCents: number }): number {
  assertInt('quantity', p.quantity, 1);
  assertInt('unitCostCents', p.unitCostCents, 0);
  assertInt('taxShipCents', p.taxShipCents, 0);
  return p.quantity * p.unitCostCents + p.taxShipCents;
}
