import { describe, expect, it } from 'vitest';
import { canTransition, isEditable, nextStatuses } from '../../src/core/purchase-lifecycle';

describe('purchase request lifecycle', () => {
  it('moves a draft to submitted and a submission to a decision', () => {
    expect(canTransition('club_purchase', 'draft', 'submitted')).toBe(true);
    expect(canTransition('club_purchase', 'submitted', 'approved')).toBe(true);
    expect(canTransition('club_purchase', 'submitted', 'denied')).toBe(true);
  });

  it('cannot skip approval', () => {
    expect(canTransition('club_purchase', 'draft', 'approved')).toBe(false);
    expect(canTransition('reimbursement', 'submitted', 'reimbursed')).toBe(false);
  });

  it('fulfils a club purchase but reimburses a reimbursement', () => {
    expect(nextStatuses('club_purchase', 'approved')).toEqual(['fulfilling', 'cancelled']);
    expect(nextStatuses('reimbursement', 'approved')).toEqual(['reimbursed', 'cancelled']);
    expect(canTransition('club_purchase', 'fulfilling', 'fulfilled')).toBe(true);
    expect(canTransition('reimbursement', 'approved', 'fulfilling')).toBe(false);
  });

  it('lets a failed fulfilment go back to approved for retry', () => {
    expect(canTransition('club_purchase', 'fulfilling', 'approved')).toBe(true);
  });

  it('closes after fulfilment or reimbursement', () => {
    expect(canTransition('club_purchase', 'fulfilled', 'closed')).toBe(true);
    expect(canTransition('reimbursement', 'reimbursed', 'closed')).toBe(true);
  });

  it('treats denied, closed and cancelled as final', () => {
    for (const s of ['denied', 'closed', 'cancelled'] as const) {
      expect(nextStatuses('club_purchase', s)).toEqual([]);
    }
  });

  it('only allows editing drafts', () => {
    expect(isEditable('draft')).toBe(true);
    expect(isEditable('submitted')).toBe(false);
  });
});
