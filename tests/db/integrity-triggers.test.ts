/**
 * Database-level integrity rules (migrations/0001_integrity_triggers.sql and the CHECKs in
 * 0000_init.sql), run against node:sqlite AND real local D1 (workerd), because D1's
 * settings (e.g. recursive_triggers = 0) change how REPLACE interacts with triggers.
 *
 * One database per engine; every test namespaces its rows with a fresh key.
 */
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { ENGINES, type TestDb } from './engines';

describe.each(ENGINES)('integrity rules on $name', (engine) => {
  let db: TestDb;
  let n = 0;
  /** Fresh key per test so rows from other tests never collide. */
  const key = () => `t${++n}`;

  beforeAll(async () => {
    db = await engine.open();
    await db.run(`INSERT INTO accounts (id, code, name, type, normal_balance) VALUES ('cash', '1000', 'Checking', 'asset', 'debit')`);
    await db.run(`INSERT INTO accounts (id, code, name, type, normal_balance) VALUES ('rev', '4000', 'Contributions', 'revenue', 'credit')`);
    await db.run(`INSERT INTO funds (id, code, name, kind, restriction) VALUES ('gen', 'GEN', 'General', 'general', 'without_donor')`);
    await db.run(`INSERT INTO users (id, discord_id, name, role) VALUES ('u1', '100000000000000001', 'Officer', 'officer')`);
    await db.run(`INSERT INTO donors (id, name, type) VALUES ('d1', 'Donor', 'individual')`);
  }, 60_000);
  afterAll(() => db?.close());

  const draftEntry = (id: string, date = '2026-09-15') =>
    db.run(`INSERT INTO journal_entries (id, entry_date, memo) VALUES (?, ?, 'test')`, id, date);
  const line = (entry: string, no: number, account: string, debit: number, credit: number, id = `${entry}-${no}`) =>
    db.run(
      `INSERT INTO journal_lines (id, entry_id, line_no, account_id, fund_id, debit_cents, credit_cents) VALUES (?, ?, ?, ?, 'gen', ?, ?)`,
      id, entry, no, account, debit, credit,
    );
  const post = (id: string) => db.run(`UPDATE journal_entries SET status = 'posted', posted_at = 1 WHERE id = ?`, id);
  async function postedEntry(id: string, cents = 5000) {
    await draftEntry(id);
    await line(id, 1, 'cash', cents, 0);
    await line(id, 2, 'rev', 0, cents);
    await post(id);
  }
  const entry = (id: string) => db.get<{ status: string; memo: string }>(`SELECT status, memo FROM journal_entries WHERE id = ?`, id);
  const imbalance = async (id: string) =>
    (await db.get<{ v: number }>(`SELECT coalesce(sum(debit_cents) - sum(credit_cents), 0) AS v FROM journal_lines WHERE entry_id = ?`, id))!.v;
  const lineCount = async (id: string) =>
    (await db.get<{ n: number }>(`SELECT count(*) AS n FROM journal_lines WHERE entry_id = ?`, id))!.n;

  describe('posting a journal entry', () => {
    it('posts a balanced entry', async () => {
      const k = key();
      await postedEntry(k);
      expect((await entry(k))?.status).toBe('posted');
    });

    it('refuses to post an unbalanced entry', async () => {
      const k = key();
      await draftEntry(k);
      await line(k, 1, 'cash', 5000, 0);
      await line(k, 2, 'rev', 0, 4999);
      await expect(post(k)).rejects.toThrow(/unbalanced/);
    });

    it('refuses to post an entry with no lines', async () => {
      const k = key();
      await draftEntry(k);
      await expect(post(k)).rejects.toThrow(/at least two lines/);
    });

    it('refuses to insert an entry directly as posted', async () => {
      await expect(
        db.run(`INSERT INTO journal_entries (id, entry_date, memo, status, posted_at) VALUES (?, '2026-09-01', 'm', 'posted', 1)`, key()),
      ).rejects.toThrow(/must be created as draft/);
    });

    it('refuses to post into a closed or locked fiscal period', async () => {
      const k = key();
      await db.run(`INSERT INTO fiscal_periods (id, fiscal_year, name, start_date, end_date, status) VALUES (?, 2031, 'Sep', '2030-09-01', '2030-09-30', 'closed')`, k);
      await draftEntry(k, '2030-09-15');
      await line(k, 1, 'cash', 100, 0);
      await line(k, 2, 'rev', 0, 100);
      await expect(post(k)).rejects.toThrow(/period is closed/);
    });

    it('allows draft entries to be edited and deleted', async () => {
      const k = key();
      await draftEntry(k);
      await line(k, 1, 'cash', 100, 0);
      await db.run(`UPDATE journal_lines SET debit_cents = 200 WHERE entry_id = ?`, k);
      await db.run(`DELETE FROM journal_lines WHERE entry_id = ?`, k);
      await db.run(`DELETE FROM journal_entries WHERE id = ?`, k);
      expect(await entry(k)).toBeUndefined();
    });
  });

  describe('posted journal entries are immutable', () => {
    it('blocks UPDATE of a posted entry', async () => {
      const k = key();
      await postedEntry(k);
      await expect(db.run(`UPDATE journal_entries SET memo = 'edited' WHERE id = ?`, k)).rejects.toThrow(/immutable/);
    });

    it('blocks un-posting a posted entry', async () => {
      const k = key();
      await postedEntry(k);
      await expect(db.run(`UPDATE journal_entries SET status = 'draft', posted_at = NULL WHERE id = ?`, k)).rejects.toThrow(/immutable/);
    });

    it('blocks DELETE of a posted entry', async () => {
      const k = key();
      await postedEntry(k);
      await expect(db.run(`DELETE FROM journal_entries WHERE id = ?`, k)).rejects.toThrow(/immutable/);
    });

    it('blocks UPDATE, DELETE and moving of a posted line', async () => {
      const k = key();
      await postedEntry(k);
      await draftEntry(`${k}-other`);
      await expect(db.run(`UPDATE journal_lines SET debit_cents = 1 WHERE id = ?`, `${k}-1`)).rejects.toThrow(/immutable/);
      await expect(db.run(`UPDATE journal_lines SET entry_id = ? WHERE id = ?`, `${k}-other`, `${k}-1`)).rejects.toThrow(/immutable/);
      await expect(db.run(`DELETE FROM journal_lines WHERE id = ?`, `${k}-1`)).rejects.toThrow(/immutable/);
    });

    it('blocks adding a line to a posted entry', async () => {
      const k = key();
      await postedEntry(k);
      await expect(line(k, 3, 'cash', 1, 0)).rejects.toThrow(/immutable/);
    });

    it('allows a reversing entry that references it', async () => {
      const k = key();
      await postedEntry(k);
      const r = `${k}-rev`;
      await db.run(
        `INSERT INTO journal_entries (id, entry_date, memo, source, reverses_entry_id) VALUES (?, '2026-09-16', 'reverse', 'reversal', ?)`,
        r, k,
      );
      await line(r, 1, 'rev', 5000, 0);
      await line(r, 2, 'cash', 0, 5000);
      await post(r);
      expect((await entry(r))?.status).toBe('posted');
    });
  });

  // Review finding 1: REPLACE deletes the conflicting row without firing DELETE triggers
  // when recursive_triggers is off (as on D1), so INSERT guards must look at existing rows.
  describe('REPLACE and UPSERT cannot rewrite posted books or the audit log', () => {
    it('blocks INSERT OR REPLACE over a posted entry', async () => {
      const k = key();
      await postedEntry(k);
      await expect(
        db.run(`INSERT OR REPLACE INTO journal_entries (id, entry_date, memo) VALUES (?, '2026-10-15', 'Overwritten')`, k),
      ).rejects.toThrow(/immutable/);
      expect(await entry(k)).toEqual({ status: 'posted', memo: 'test' });
    });

    it('blocks an UPSERT over a posted entry', async () => {
      const k = key();
      await postedEntry(k);
      await expect(
        db.run(
          `INSERT INTO journal_entries (id, entry_date, memo) VALUES (?, '2026-10-15', 'x') ON CONFLICT (id) DO UPDATE SET memo = excluded.memo`,
          k,
        ),
      ).rejects.toThrow(/immutable/);
      expect((await entry(k))?.memo).toBe('test');
    });

    it('blocks INSERT OR REPLACE of a posted line by id, even into a draft entry', async () => {
      const k = key();
      await postedEntry(k, 100);
      await draftEntry(`${k}-draft`);
      await expect(
        db.run(
          `INSERT OR REPLACE INTO journal_lines (id, entry_id, line_no, account_id, fund_id, debit_cents, credit_cents) VALUES (?, ?, 1, 'cash', 'gen', 100, 0)`,
          `${k}-1`, `${k}-draft`,
        ),
      ).rejects.toThrow(/immutable/);
      expect(await imbalance(k)).toBe(0);
      expect(await lineCount(k)).toBe(2);
    });

    it('blocks INSERT OR REPLACE of a posted line by (entry, line number)', async () => {
      const k = key();
      await postedEntry(k, 100);
      await expect(line(k, 1, 'cash', 999, 0, `${k}-new`).then(() => undefined)).rejects.toThrow(/immutable/);
      await expect(
        db.run(
          `INSERT OR REPLACE INTO journal_lines (id, entry_id, line_no, account_id, fund_id, debit_cents, credit_cents) VALUES (?, ?, 1, 'cash', 'gen', 999, 0)`,
          `${k}-new`, k,
        ),
      ).rejects.toThrow(/immutable/);
      expect(await imbalance(k)).toBe(0);
    });

    it('blocks UPDATE OR REPLACE that moves a draft onto a posted entry or line', async () => {
      const k = key();
      await postedEntry(k, 100);
      await draftEntry(`${k}-d`);
      await line(`${k}-d`, 1, 'cash', 7, 0);
      await expect(db.run(`UPDATE OR REPLACE journal_entries SET id = ? WHERE id = ?`, k, `${k}-d`)).rejects.toThrow(/immutable/);
      await expect(db.run(`UPDATE OR REPLACE journal_lines SET id = ? WHERE id = ?`, `${k}-1`, `${k}-d-1`)).rejects.toThrow(/immutable/);
      expect(await entry(k)).toEqual({ status: 'posted', memo: 'test' });
      expect(await imbalance(k)).toBe(0);
      expect(await lineCount(k)).toBe(2);
    });

    it('blocks INSERT OR REPLACE over an audit record', async () => {
      const k = key();
      await db.run(`INSERT INTO audit_log (id, action, entity_type) VALUES (?, 'original', 'test')`, k);
      await expect(
        db.run(`INSERT OR REPLACE INTO audit_log (id, action, entity_type) VALUES (?, 'rewritten', 'test')`, k),
      ).rejects.toThrow(/append-only/);
      expect(await db.get(`SELECT action FROM audit_log WHERE id = ?`, k)).toEqual({ action: 'original' });
    });

    it('blocks INSERT OR REPLACE that would reopen a closed fiscal period', async () => {
      const k = key();
      await db.run(`INSERT INTO fiscal_periods (id, fiscal_year, name, start_date, end_date, status) VALUES (?, 2041, 'Jan', '2041-01-01', '2041-01-31', 'closed')`, k);
      await expect(
        db.run(`INSERT OR REPLACE INTO fiscal_periods (id, fiscal_year, name, start_date, end_date, status) VALUES (?, 2041, 'Jan', '2041-01-01', '2041-01-31', 'open')`, `${k}-new`),
      ).rejects.toThrow(/closed fiscal period/);
      await db.run(`INSERT INTO fiscal_periods (id, fiscal_year, name, start_date, end_date) VALUES (?, 2041, 'Feb', '2041-02-01', '2041-02-28')`, `${k}-feb`);
      await expect(
        db.run(`UPDATE OR REPLACE fiscal_periods SET id = ? WHERE id = ?`, k, `${k}-feb`),
      ).rejects.toThrow(/closed fiscal period/);
      await expect(
        db.run(`UPDATE fiscal_periods SET status = 'open' WHERE id = ?`, k),
      ).resolves.toBeUndefined(); // reopening by UPDATE is an explicit, audited admin action
    });
  });

  describe('audit_log is append-only', () => {
    it('allows inserts and blocks UPDATE and DELETE', async () => {
      const k = key();
      await db.run(`INSERT INTO audit_log (id, action, entity_type, entity_id) VALUES (?, 'request.approve', 'purchase_request', 'r1')`, k);
      await expect(db.run(`UPDATE audit_log SET action = 'x' WHERE id = ?`, k)).rejects.toThrow(/append-only/);
      await expect(db.run(`DELETE FROM audit_log WHERE id = ?`, k)).rejects.toThrow(/append-only/);
      expect(await db.get(`SELECT action FROM audit_log WHERE id = ?`, k)).toEqual({ action: 'request.approve' });
    });
  });

  describe('journal line amounts', () => {
    it('rejects negative, two-sided and zero lines', async () => {
      const k = key();
      await draftEntry(k);
      await expect(line(k, 1, 'cash', -5, 0)).rejects.toThrow(/CHECK/);
      await expect(line(k, 2, 'cash', 5, 5)).rejects.toThrow(/CHECK/);
      await expect(line(k, 3, 'cash', 0, 0)).rejects.toThrow(/CHECK/);
    });
  });

  // Review finding 3: INTEGER affinity stores 0.5 as REAL; money must be whole cents.
  describe('money columns hold integer cents only', () => {
    it('rejects fractional journal amounts', async () => {
      const k = key();
      await draftEntry(k);
      await expect(line(k, 1, 'cash', 0.5, 0)).rejects.toThrow(/CHECK/);
      await expect(line(k, 2, 'rev', 0, 0.5)).rejects.toThrow(/CHECK/);
    });

    it('accepts whole cents', async () => {
      const k = key();
      await draftEntry(k);
      await line(k, 1, 'cash', 50, 0);
      expect(await db.get(`SELECT typeof(debit_cents) AS t FROM journal_lines WHERE id = ?`, `${k}-1`)).toEqual({ t: 'integer' });
    });

    it('rejects fractional purchase request amounts', async () => {
      const insert = (unit: number, tax: number, total: number) =>
        db.run(
          `INSERT INTO purchase_requests (id, number, requester_id, type, title, vendor, quantity, unit_cost_cents, tax_ship_cents, total_cents, expense_account_id, fund_id, justification)
           VALUES (?, ?, 'u1', 'club_purchase', 't', 'v', 1, ?, ?, ?, 'cash', 'gen', 'j')`,
          key(), key(), unit, tax, total,
        );
      await expect(insert(10.5, 0, 10.5)).rejects.toThrow(/CHECK/);
      await expect(insert(10, 0.25, 10.25)).rejects.toThrow(/CHECK/);
      await expect(insert(10, 5, 15)).resolves.toBeUndefined();
    });

    it('rejects fractional budget, contribution and grant amounts', async () => {
      await expect(
        db.run(`INSERT INTO budgets (id, fiscal_year, account_id, amount_cents) VALUES (?, 2050, 'cash', 99.9)`, key()),
      ).rejects.toThrow(/CHECK/);
      await expect(
        db.run(`INSERT INTO contributions (id, donor_id, fund_id, received_date, kind, amount_cents) VALUES (?, 'd1', 'gen', '2026-09-01', 'cash', 100.5)`, key()),
      ).rejects.toThrow(/CHECK/);
      await expect(
        db.run(`INSERT INTO funds (id, code, name, kind, restriction, amount_awarded_cents) VALUES (?, ?, 'g', 'grant', 'with_donor', 1.5)`, key(), key()),
      ).rejects.toThrow(/CHECK/);
    });
  });

  describe('budgets', () => {
    it('allows one all-funds budget per account per fiscal year', async () => {
      const insert = (id: string) =>
        db.run(`INSERT INTO budgets (id, fiscal_year, account_id, fund_id, amount_cents) VALUES (?, 2060, 'cash', NULL, 100)`, id);
      await insert(key());
      await expect(insert(key())).rejects.toThrow(/UNIQUE/);
    });
  });
});
