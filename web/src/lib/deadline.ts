import type { PurchaseStatus } from './domain';
import { pacificToday } from './calendar';

/**
 * Purchase "needed by" deadlines are date-only (YYYY-MM-DD). They are compared with
 * today's date on the officer's selected time zone, never shifted through an instant.
 */

/** Statuses whose deadline still matters: the purchase hasn't happened or been stopped. */
export const DEADLINE_OPEN: readonly PurchaseStatus[] = ['draft', 'submitted', 'approved', 'fulfilling'];
/** A deadline this many days away (or fewer) counts as "due soon". */
export const DUE_SOON_DAYS = 7;

const dayIndex = (date: string) => {
  const [y, m, d] = date.split('-').map(Number);
  return Date.UTC(y!, m! - 1, d!) / 86_400_000;
};

/** Whole calendar days from `today` to `date`; negative when overdue. */
export const daysUntil = (date: string, today = pacificToday()) => dayIndex(date) - dayIndex(today);

export type DueTone = 'overdue' | 'today' | 'soon' | 'later';

export function dueState(date: string, today = pacificToday()): { days: number; tone: DueTone; label: string } {
  const days = daysUntil(date, today);
  if (days < 0) return { days, tone: 'overdue', label: days === -1 ? 'overdue by 1 day' : `overdue by ${-days} days` };
  if (days === 0) return { days, tone: 'today', label: 'due today' };
  if (days === 1) return { days, tone: 'soon', label: 'due tomorrow' };
  return { days, tone: days <= DUE_SOON_DAYS ? 'soon' : 'later', label: `due in ${days} days` };
}

/** True when an open request's deadline is overdue or within DUE_SOON_DAYS. */
export const isDueSoon = (r: { neededBy: string | null; status: PurchaseStatus }, today = pacificToday()) =>
  !!r.neededBy && DEADLINE_OPEN.includes(r.status) && daysUntil(r.neededBy, today) <= DUE_SOON_DAYS;
