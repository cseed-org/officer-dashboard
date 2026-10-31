import type { FunctionalClass, PurchaseStatus, PurchaseType, Restriction, FundKind, FinanceReport } from '../core/domain';
import { formatCents as coreFormatCents } from '../core/money';

/** Like core formatCents, but never prints "-$0.00". */
export const formatCents = (cents: number) => coreFormatCents(cents === 0 ? 0 : cents);

/** 0.784 → "78%"; null → "—" (no data, not zero). */
export function pct(rate: number | null | undefined, digits = 0): string {
  if (rate === null || rate === undefined) return '—';
  return `${(rate * 100).toFixed(digits)}%`;
}

/** Whole dollars for tiles and axes: 123456 → "$1,235". */
const usd0 = new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', maximumFractionDigits: 0 });
export const dollars = (cents: number) => usd0.format(Math.round(cents / 100));

const tz = 'America/Los_Angeles';
const dateFmt = new Intl.DateTimeFormat('en-US', { month: 'short', day: 'numeric', year: 'numeric', timeZone: tz });
const shortFmt = new Intl.DateTimeFormat('en-US', { month: 'short', day: 'numeric', timeZone: tz });
const timeFmt = new Intl.DateTimeFormat('en-US', { hour: 'numeric', minute: '2-digit', timeZone: tz });
const weekdayFmt = new Intl.DateTimeFormat('en-US', { weekday: 'short', timeZone: tz });

const toDate = (v: string) => (v.length === 10 ? new Date(`${v}T12:00:00Z`) : new Date(v));
export const fmtDate = (v: string | null | undefined) => (v ? dateFmt.format(toDate(v)) : '—');
export const fmtShort = (v: string) => (v.length === 10 ? new Intl.DateTimeFormat('en-US', { month: 'short', day: 'numeric', timeZone: 'UTC' }) : shortFmt).format(toDate(v));
export const fmtTime = (v: string) => timeFmt.format(toDate(v));
export const fmtDateTime = (v: string | null | undefined) => (v ? `${dateFmt.format(toDate(v))}, ${timeFmt.format(toDate(v))}` : '—');
export const fmtWeekday = (v: string) => weekdayFmt.format(toDate(v));

export function relative(v: string | null | undefined, now = Date.now()): string {
  if (!v) return 'never';
  const diff = toDate(v).getTime() - now;
  const abs = Math.abs(diff);
  const rtf = new Intl.RelativeTimeFormat('en', { numeric: 'auto' });
  if (abs < 3_600_000) return rtf.format(Math.round(diff / 60_000), 'minute');
  if (abs < 86_400_000) return rtf.format(Math.round(diff / 3_600_000), 'hour');
  if (abs < 30 * 86_400_000) return rtf.format(Math.round(diff / 86_400_000), 'day');
  return fmtDate(v);
}

export const STATUS_LABEL: Record<PurchaseStatus, string> = {
  draft: 'draft',
  submitted: 'pending',
  approved: 'approved',
  denied: 'denied',
  fulfilling: 'buying',
  fulfilled: 'bought',
  reimbursed: 'reimbursed',
  closed: 'closed',
  cancelled: 'cancelled',
};
export type Tone = 'positive' | 'attention' | 'info' | 'negative' | 'neutral';
export const STATUS_TONE: Record<PurchaseStatus, Tone> = {
  draft: 'neutral',
  submitted: 'attention',
  approved: 'positive',
  denied: 'negative',
  fulfilling: 'info',
  fulfilled: 'positive',
  reimbursed: 'positive',
  closed: 'neutral',
  cancelled: 'neutral',
};
export const TYPE_LABEL: Record<PurchaseType, string> = { reimbursement: 'reimbursement', club_purchase: 'club purchase' };
export const FUNCTIONAL_LABEL: Record<FunctionalClass, string> = {
  program: 'program',
  mgmt_general: 'management & general',
  fundraising: 'fundraising',
};
export const RESTRICTION_LABEL: Record<Restriction, string> = { without_donor: 'unrestricted', with_donor: 'donor-restricted' };
export const FUND_KIND_LABEL: Record<FundKind, string> = { general: 'general', grant: 'grant', sponsor: 'sponsor', other: 'other' };
export const REPORT_LABEL: Record<FinanceReport, string> = {
  'budget-vs-actual': 'budget vs actual',
  'fund-balances': 'fund balances',
  'trial-balance': 'trial balance',
  'statement-of-activities': 'activities',
  'statement-of-financial-position': 'financial position',
  'statement-of-functional-expenses': 'functional expenses',
  'form-990': 'form 990 lines',
};

export const bytes = (n: number) => (n < 1024 ? `${n} B` : n < 1024 * 1024 ? `${(n / 1024).toFixed(0)} KB` : `${(n / 1024 / 1024).toFixed(1)} MB`);

/** "$12.50" / "12.5" → 1250 cents; '' → null; garbage → NaN. */
export function dollarsToCents(input: string): number | null {
  const s = input.trim().replace(/[$,\s]/g, '');
  if (!s) return null;
  if (!/^\d+(\.\d{0,2})?$/.test(s)) return Number.NaN;
  const [whole, frac = ''] = s.split('.');
  return Number(whole) * 100 + Number(frac.padEnd(2, '0'));
}
