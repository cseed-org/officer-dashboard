import type { PurchaseStatus } from '../../lib/domain';
import { DEADLINE_OPEN, dueState } from '../../lib/deadline';
import { fmtDate, fmtShort } from '../../lib/format';

/**
 * A purchase "needed by" date. Open requests add "due in N days" / "overdue by N days"
 * (text, not colour alone, carries the urgency); finished ones show just the date.
 */
export function DueDate({ date, status, short, hintOnly }: { date: string | null; status: PurchaseStatus; short?: boolean; hintOnly?: boolean }) {
  if (!date) return hintOnly ? null : <span class="muted">—</span>;
  const text = short ? fmtShort(date) : fmtDate(date);
  if (!DEADLINE_OPEN.includes(status)) return hintOnly ? null : <time dateTime={date}>{text}</time>;
  const due = dueState(date);
  if (hintOnly) return <span class={`due-hint due--${due.tone}`} data-due={due.tone}>{due.label}</span>;
  return (
    <span class={`due due--${due.tone}`} data-due={due.tone}>
      <time dateTime={date}>{text}</time>
      <span class="due-hint">{due.label}</span>
    </span>
  );
}
