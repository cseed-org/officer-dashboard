/** Luma approval-state aliases are normalized independently from timestamp imports. */
export function isRegistered(
  status: string | null | undefined,
  checkedIn: unknown = false,
): boolean {
  if (checkedIn) return true;
  const normalized = status?.trim().toLowerCase().replace(/[ -]+/g, '_') ?? '';
  return ![
    'declined',
    'rejected',
    'waitlist',
    'waitlisted',
    'pending',
    'pending_approval',
    'invited',
    'cancelled',
    'canceled',
  ].includes(normalized);
}
