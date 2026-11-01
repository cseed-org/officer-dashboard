/**
 * Meaningful colour. Hues come from the categorical palette in styles/tokens.css
 * (--c-<hue>); markup carries `data-hue="<hue>"` and CSS paints the mark. Text never
 * takes the hue: it stays in text tokens beside a coloured dot or bar.
 */
import type { PurchaseStatus } from './domain';

export const HUES = ['blue', 'amber', 'teal', 'coral', 'violet', 'green'] as const;
export type Hue = (typeof HUES)[number];

/**
 * A budget category's colour follows the account, never its rank in a list: expense codes
 * are 5100, 5200, … so the hundreds digit picks the slot (5100 blue, 5200 amber, …), and
 * other codes fall back to a stable hash. The same account is the same colour everywhere.
 */
export function categoryHue(code: string): Hue {
  const m = /^\d(\d)\d\d$/.exec(code.trim());
  if (m) {
    const d = Number(m[1]);
    if (d >= 1 && d <= 6) return HUES[d - 1]!;
    if (d === 9) return 'green'; // x900 "other" takes the free sixth slot (no x600 account today)
  }
  let h = 0;
  for (const ch of code) h = (h * 31 + ch.charCodeAt(0)) >>> 0;
  return HUES[h % HUES.length]!;
}

/** Status dots: pending amber, approved blue, buying violet, settled/closed green, denied coral. */
export const STATUS_HUE: Record<PurchaseStatus, Hue | null> = {
  draft: null,
  submitted: 'amber',
  approved: 'blue',
  fulfilling: 'violet',
  fulfilled: 'green',
  reimbursed: 'green',
  closed: 'green',
  denied: 'coral',
  cancelled: null,
};

/** Budget use: green under 80%, amber 80–100%, coral over budget. */
export function usageHue(rate: number | null): Hue {
  if (rate === null || rate < 0.8) return 'green';
  return rate <= 1 ? 'amber' : 'coral';
}
