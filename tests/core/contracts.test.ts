import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import {
  ENDPOINTS,
  PurchaseRequestCreateRequest,
  PurchaseRequestsListQuery,
  AdminUserCreateRequest,
  CsvImportFields,
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
