import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import {
  ENDPOINTS,
  PurchaseRequestCreateRequest,
  PurchaseRequestsListQuery,
  AdminUserCreateRequest,
  CsvImportFields,
  PurchaseTransitionRequest,
  AgentJobCompleteRequest,
  PurchaseRequestDetail,
  PurchaseRequestUpdateRequest,
} from '../../src/core/contracts';

describe('docs/api.md', () => {
  const doc = readFileSync(join(import.meta.dirname, '../../docs/api.md'), 'utf8');

  it.each(Object.entries(ENDPOINTS))('documents %s', (_name, e) => {
    const row = doc.split('\n').find((l) => l.includes(`| ${e.method} | \`${e.path}\` |`));
    expect(row, `${e.method} ${e.path} missing from docs/api.md`).toBeDefined();
    expect(row).toContain(`| ${e.auth} |`);
  });
});

describe('contract parsing', () => {
  it('defaults a new request to a draft program expense with no tax/shipping', () => {
    const parsed = PurchaseRequestCreateRequest.parse({
      type: 'club_purchase',
      title: 'Pizza',
      vendor: 'Dominos',
      quantity: 4,
      unitCostCents: 1599,
      expenseAccountId: 'acct',
      fundId: 'fund',
      justification: 'Kickoff meeting',
    });
    expect(parsed).toMatchObject({ submit: false, taxShipCents: 0, functionalClass: 'program' });
  });

  it('rejects fractional cents', () => {
    const r = PurchaseRequestCreateRequest.safeParse({
      type: 'club_purchase', title: 'x', vendor: 'y', quantity: 1, unitCostCents: 10.5,
      expenseAccountId: 'a', fundId: 'f', justification: 'z',
    });
    expect(r.success).toBe(false);
  });

  it('preserves omitted cost and functional class fields on a draft patch', () => {
    expect(PurchaseRequestUpdateRequest.parse({ title: 'New title' })).toEqual({ title: 'New title' });
  });

  it('coerces query-string numbers and applies list defaults', () => {
    expect(PurchaseRequestsListQuery.parse({ limit: '20' })).toEqual({ limit: 20, scope: 'mine' });
  });

  it('validates Discord snowflakes when allowlisting a user', () => {
    expect(AdminUserCreateRequest.safeParse({ discordId: '123', name: 'A' }).success).toBe(false);
    expect(AdminUserCreateRequest.parse({ discordId: '123456789012345678', name: 'A' }).role).toBe('officer');
  });

  it('turns the multipart dryRun field into a boolean', () => {
    expect(CsvImportFields.parse({ kind: 'luma_guests', dryRun: 'true' }).dryRun).toBe(true);
    expect(CsvImportFields.parse({ kind: 'reflections' }).dryRun).toBe(false);
  });
});

// Review finding 4: a $100 request bought for $95 must be recordable as $95.
describe('settling a purchase request', () => {
  it('requires the actual amount paid when marking fulfilled or reimbursed', () => {
    expect(PurchaseTransitionRequest.safeParse({ to: 'fulfilled' }).success).toBe(false);
    expect(PurchaseTransitionRequest.safeParse({ to: 'reimbursed' }).success).toBe(false);
  });

  it('carries the actual amount, receipts and notes through', () => {
    expect(
      PurchaseTransitionRequest.parse({ to: 'fulfilled', actualTotalCents: 9500, attachmentIds: ['att1'], comment: '5% discount' }),
    ).toEqual({ to: 'fulfilled', actualTotalCents: 9500, attachmentIds: ['att1'], comment: '5% discount' });
    expect(PurchaseTransitionRequest.parse({ to: 'reimbursed', actualTotalCents: 4120 })).toMatchObject({
      to: 'reimbursed',
      actualTotalCents: 4120,
      attachmentIds: [],
    });
  });

  it('rejects fractional or negative actual amounts', () => {
    expect(PurchaseTransitionRequest.safeParse({ to: 'reimbursed', actualTotalCents: 41.2 }).success).toBe(false);
    expect(PurchaseTransitionRequest.safeParse({ to: 'reimbursed', actualTotalCents: -1 }).success).toBe(false);
  });

  it('needs no amount for the non-posting moves', () => {
    for (const to of ['fulfilling', 'closed', 'cancelled'] as const) {
      expect(PurchaseTransitionRequest.parse({ to })).toEqual({ to });
    }
  });

  it('lets the fulfilment agent attach receipts with its actual amount', () => {
    expect(AgentJobCompleteRequest.parse({ outcome: 'done', actualTotalCents: 9500, attachmentIds: ['att1'] })).toMatchObject({
      actualTotalCents: 9500,
      attachmentIds: ['att1'],
    });
  });

  it('exposes the actual amount next to the approved estimate on a request', () => {
    expect(PurchaseRequestDetail.shape.actualTotalCents.safeParse(null).success).toBe(true);
    expect(PurchaseRequestDetail.shape.actualTotalCents.safeParse(9500).success).toBe(true);
  });
});
