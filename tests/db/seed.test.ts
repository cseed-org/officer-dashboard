import { describe, expect, it } from 'vitest';
import { buildSeedSql } from '../../scripts/seed/sql';
import { migratedDb } from './sqlite';

function seeded() {
  const db = migratedDb();
  db.exec(buildSeedSql());
  return db;
}
const one = (db: ReturnType<typeof migratedDb>, sql: string) => db.prepare(sql).get() as Record<string, number>;

describe('seed', () => {
  it('applies cleanly on top of the migrations', () => {
    const db = seeded();
    expect(one(db, `SELECT count(*) AS n FROM users WHERE role = 'director'`).n).toBe(1);
    expect(one(db, `SELECT count(*) AS n FROM users WHERE role = 'officer'`).n).toBe(3);
    expect(one(db, `SELECT count(*) AS n FROM events`).n).toBe(6);
    expect(one(db, `SELECT count(DISTINCT status) AS n FROM purchase_requests`).n).toBeGreaterThanOrEqual(5);
    expect(one(db, `SELECT count(*) AS n FROM funds WHERE restriction = 'with_donor'`).n).toBe(2);
  });

  it('posts every journal entry, each balanced', () => {
    const db = seeded();
    expect(one(db, `SELECT count(*) AS n FROM journal_entries WHERE status <> 'posted'`).n).toBe(0);
    const totals = one(db, `SELECT sum(debit_cents) AS d, sum(credit_cents) AS c FROM journal_lines`);
    expect(totals.d).toBe(totals.c);
  });

  it('keeps the books balanced: assets = liabilities + net assets + (revenue − expenses)', () => {
    const db = seeded();
    const bal = (type: string) =>
      one(db, `SELECT coalesce(sum(l.debit_cents - l.credit_cents), 0) AS v FROM journal_lines l JOIN accounts a ON a.id = l.account_id WHERE a.type = '${type}'`).v!;
    const assets = bal('asset');
    const liabilities = -bal('liability');
    const netAssets = -bal('net_asset');
    const surplus = -bal('revenue') - bal('expense');
    expect(assets).toBe(liabilities + netAssets + surplus);
    // Worked example: cash 3,200 + 5,000 + 1,500 + 300 − 405 − 186.40 = 9,408.60,
    // plus the 2,500 grant receivable = 11,908.60.
    expect(assets).toBe(1_190_860);
  });

  it('maps every revenue and expense account to a Form 990 line', () => {
    const db = seeded();
    expect(one(db, `SELECT count(*) AS n FROM accounts WHERE type IN ('revenue','expense') AND form990_line IS NULL`).n).toBe(0);
  });

  it('can be run twice without errors or duplicates', () => {
    const db = seeded();
    db.exec(buildSeedSql());
    expect(one(db, `SELECT count(*) AS n FROM journal_lines`).n).toBe(16);
    expect(one(db, `SELECT count(*) AS n FROM users`).n).toBe(4);
  });
});
