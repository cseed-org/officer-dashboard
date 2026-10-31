import { beforeEach, describe, expect, it } from 'vitest';
import type { DatabaseSync } from 'node:sqlite';
import { migratedDb } from './sqlite';

let db: DatabaseSync;

const run = (sql: string, ...params: (string | number | null)[]) => db.prepare(sql).run(...params);

function seedLedger() {
  run(`INSERT INTO accounts (id, code, name, type, normal_balance) VALUES ('cash', '1000', 'Checking', 'asset', 'debit')`);
  run(`INSERT INTO accounts (id, code, name, type, normal_balance) VALUES ('rev', '4000', 'Contributions', 'revenue', 'credit')`);
  run(`INSERT INTO funds (id, code, name, kind, restriction) VALUES ('gen', 'GEN', 'General', 'general', 'without_donor')`);
}

function draftEntry(id: string, date = '2026-09-15') {
  run(`INSERT INTO journal_entries (id, entry_date, memo) VALUES (?, ?, 'test')`, id, date);
}

function line(entry: string, no: number, account: string, debit: number, credit: number) {
  run(
    `INSERT INTO journal_lines (id, entry_id, line_no, account_id, fund_id, debit_cents, credit_cents)
     VALUES (?, ?, ?, ?, 'gen', ?, ?)`,
    `${entry}-${no}`, entry, no, account, debit, credit,
  );
}

const post = (id: string) =>
  run(`UPDATE journal_entries SET status = 'posted', posted_at = 1 WHERE id = ?`, id);

function postedEntry(id: string) {
  draftEntry(id);
  line(id, 1, 'cash', 5000, 0);
  line(id, 2, 'rev', 0, 5000);
  post(id);
}

beforeEach(() => {
  db = migratedDb();
  seedLedger();
});

describe('posting a journal entry', () => {
  it('posts a balanced entry', () => {
    postedEntry('je1');
    const row = db.prepare(`SELECT status FROM journal_entries WHERE id = 'je1'`).get();
    expect(row).toEqual({ status: 'posted' });
  });

  it('refuses to post an unbalanced entry', () => {
    draftEntry('je1');
    line('je1', 1, 'cash', 5000, 0);
    line('je1', 2, 'rev', 0, 4999);
    expect(() => post('je1')).toThrow(/unbalanced/);
  });

  it('refuses to post an entry with no lines', () => {
    draftEntry('je1');
    expect(() => post('je1')).toThrow(/at least two lines/);
  });

  it('refuses to insert an entry directly as posted', () => {
    expect(() =>
      run(`INSERT INTO journal_entries (id, entry_date, memo, status, posted_at) VALUES ('x', '2026-09-01', 'm', 'posted', 1)`),
    ).toThrow(/must be created as draft/);
  });

  it('refuses to post into a closed or locked fiscal period', () => {
    run(`INSERT INTO fiscal_periods (id, fiscal_year, name, start_date, end_date, status)
         VALUES ('p', 2026, 'Sep', '2026-09-01', '2026-09-30', 'closed')`);
    draftEntry('je1', '2026-09-15');
    line('je1', 1, 'cash', 100, 0);
    line('je1', 2, 'rev', 0, 100);
    expect(() => post('je1')).toThrow(/period is closed/);
  });

  it('allows draft entries to be edited and deleted', () => {
    draftEntry('je1');
    line('je1', 1, 'cash', 100, 0);
    run(`UPDATE journal_lines SET debit_cents = 200 WHERE entry_id = 'je1'`);
    run(`DELETE FROM journal_lines WHERE entry_id = 'je1'`);
    run(`DELETE FROM journal_entries WHERE id = 'je1'`);
    expect(db.prepare(`SELECT count(*) AS n FROM journal_entries`).get()).toEqual({ n: 0 });
  });
});

describe('posted journal entries are immutable', () => {
  beforeEach(() => postedEntry('je1'));

  it('blocks UPDATE of a posted entry', () => {
    expect(() => run(`UPDATE journal_entries SET memo = 'edited' WHERE id = 'je1'`)).toThrow(/immutable/);
  });

  it('blocks un-posting a posted entry', () => {
    expect(() => run(`UPDATE journal_entries SET status = 'draft', posted_at = NULL WHERE id = 'je1'`)).toThrow(/immutable/);
  });

  it('blocks DELETE of a posted entry', () => {
    expect(() => run(`DELETE FROM journal_entries WHERE id = 'je1'`)).toThrow(/immutable/);
  });

  it('blocks UPDATE of a posted line', () => {
    expect(() => run(`UPDATE journal_lines SET debit_cents = 1 WHERE id = 'je1-1'`)).toThrow(/immutable/);
  });

  it('blocks moving a line out of a posted entry', () => {
    draftEntry('je2');
    expect(() => run(`UPDATE journal_lines SET entry_id = 'je2' WHERE id = 'je1-1'`)).toThrow(/immutable/);
  });

  it('blocks DELETE of a posted line', () => {
    expect(() => run(`DELETE FROM journal_lines WHERE id = 'je1-1'`)).toThrow(/immutable/);
  });

  it('blocks adding a line to a posted entry', () => {
    expect(() => line('je1', 3, 'cash', 1, 0)).toThrow(/immutable/);
  });

  it('allows a reversing entry that references it', () => {
    run(`INSERT INTO journal_entries (id, entry_date, memo, source, reverses_entry_id)
         VALUES ('rev1', '2026-09-16', 'reverse je1', 'reversal', 'je1')`);
    line('rev1', 1, 'rev', 5000, 0);
    line('rev1', 2, 'cash', 0, 5000);
    post('rev1');
    expect(db.prepare(`SELECT count(*) AS n FROM journal_entries WHERE status = 'posted'`).get()).toEqual({ n: 2 });
  });
});

describe('journal line constraints', () => {
  beforeEach(() => draftEntry('je1'));

  it('rejects negative amounts', () => {
    expect(() => line('je1', 1, 'cash', -5, 0)).toThrow(/CHECK/);
  });

  it('rejects a line with both debit and credit', () => {
    expect(() => line('je1', 1, 'cash', 5, 5)).toThrow(/CHECK/);
  });

  it('rejects a zero line', () => {
    expect(() => line('je1', 1, 'cash', 0, 0)).toThrow(/CHECK/);
  });
});

describe('audit_log is append-only', () => {
  beforeEach(() => {
    run(`INSERT INTO audit_log (id, action, entity_type, entity_id) VALUES ('a1', 'request.approve', 'purchase_request', 'r1')`);
  });

  it('allows inserts', () => {
    expect(db.prepare(`SELECT action FROM audit_log`).all()).toEqual([{ action: 'request.approve' }]);
  });

  it('blocks UPDATE', () => {
    expect(() => run(`UPDATE audit_log SET action = 'x'`)).toThrow(/append-only/);
  });

  it('blocks DELETE', () => {
    expect(() => run(`DELETE FROM audit_log`)).toThrow(/append-only/);
  });
});

describe('budgets', () => {
  it('allows one all-funds budget per account per fiscal year', () => {
    const insert = (id: string) =>
      run(`INSERT INTO budgets (id, fiscal_year, account_id, fund_id, amount_cents) VALUES (?, 2027, 'cash', NULL, 100)`, id);
    insert('b1');
    expect(() => insert('b2')).toThrow(/UNIQUE/);
  });
});
