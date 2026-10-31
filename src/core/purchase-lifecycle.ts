import type { PurchaseStatus, PurchaseType } from './domain';

/**
 * draft → submitted → approved | denied
 *   club_purchase:  approved → fulfilling → fulfilled → closed   (fulfilling → approved on a failed job)
 *   reimbursement:  approved → reimbursed → closed
 * draft, submitted and approved can be cancelled. denied, closed, cancelled are final.
 * Who may perform each move (requester, director, agent) is enforced by the API layer.
 */
const COMMON: Partial<Record<PurchaseStatus, PurchaseStatus[]>> = {
  draft: ['submitted', 'cancelled'],
  submitted: ['approved', 'denied', 'cancelled'],
};

const BY_TYPE: Record<PurchaseType, Partial<Record<PurchaseStatus, PurchaseStatus[]>>> = {
  club_purchase: {
    approved: ['fulfilling', 'cancelled'],
    fulfilling: ['fulfilled', 'approved'],
    fulfilled: ['closed'],
  },
  reimbursement: {
    approved: ['reimbursed', 'cancelled'],
    reimbursed: ['closed'],
  },
};

export function nextStatuses(type: PurchaseType, from: PurchaseStatus): PurchaseStatus[] {
  return [...(COMMON[from] ?? BY_TYPE[type][from] ?? [])];
}

export function canTransition(type: PurchaseType, from: PurchaseStatus, to: PurchaseStatus): boolean {
  return nextStatuses(type, from).includes(to);
}

/** Only drafts can be edited; after submission changes go through cancel + resubmit. */
export function isEditable(status: PurchaseStatus): boolean {
  return status === 'draft';
}
