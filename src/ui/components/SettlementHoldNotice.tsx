import type { PurchaseRequestDetail } from '../../core/contracts';
import { formatCents } from '../format';
import { Notice } from './states';

/** Banner for a request whose agent settlement is held for director review (PR #6, M1). */
export function SettlementHoldNotice({ hold }: { hold: PurchaseRequestDetail['settlementHold'] }) {
  if (!hold) return null;
  return (
    <div data-settlement-hold>
      <Notice tone="attention">
        <strong>Held for director review:</strong> the purchasing agent reported {formatCents(hold.attemptedCents)} vs{' '}
        {formatCents(hold.approvedCents)} approved. Settle it below with the actual amount.
      </Notice>
    </div>
  );
}
