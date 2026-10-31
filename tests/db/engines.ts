import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { DatabaseSync } from 'node:sqlite';
import { getPlatformProxy } from 'wrangler';

const MIGRATIONS_DIR = join(import.meta.dirname, '../../migrations');
type Param = string | number | null;

/** The small async surface the integrity tests need, over node:sqlite or real local D1. */
export interface TestDb {
  run(sql: string, ...params: Param[]): Promise<void>;
  get<T = Record<string, unknown>>(sql: string, ...params: Param[]): Promise<T | undefined>;
  all<T = Record<string, unknown>>(sql: string, ...params: Param[]): Promise<T[]>;
  close(): Promise<void>;
}

export interface Engine {
  name: string;
  open(): Promise<TestDb>;
}

/** Migration files split the way wrangler applies them (drizzle statement breakpoints). */
export function migrationStatements(): string[] {
  return readdirSync(MIGRATIONS_DIR)
    .filter((f) => f.endsWith('.sql'))
    .sort()
    .flatMap((f) => readFileSync(join(MIGRATIONS_DIR, f), 'utf8').split('--> statement-breakpoint'))
    .map((s) => s.trim())
    .filter((s) => s.replace(/--.*$/gm, '').trim().length > 0);
}

export const nodeSqlite: Engine = {
  name: 'node:sqlite',
  async open() {
    const db = new DatabaseSync(':memory:');
    db.exec('PRAGMA foreign_keys = ON');
    for (const s of migrationStatements()) db.exec(s);
    return {
      async run(sql, ...p) {
        db.prepare(sql).run(...p);
      },
      async get(sql, ...p) {
        return db.prepare(sql).get(...p) as never;
      },
      async all(sql, ...p) {
        return db.prepare(sql).all(...p) as never;
      },
      async close() {
        db.close();
      },
    };
  },
};

/** Real workerd D1 (same engine and pragmas as `wrangler dev` / production), in memory. */
export const localD1: Engine = {
  name: 'local D1',
  async open() {
    const proxy = await getPlatformProxy<{ DB: D1Database }>({ persist: false });
    const d1 = proxy.env.DB;
    for (const s of migrationStatements()) await d1.prepare(s).run();
    return {
      async run(sql, ...p) {
        await d1.prepare(sql).bind(...p).run();
      },
      async get(sql, ...p) {
        return ((await d1.prepare(sql).bind(...p).first()) ?? undefined) as never;
      },
      async all(sql, ...p) {
        return (await d1.prepare(sql).bind(...p).all()).results as never;
      },
      async close() {
        await proxy.dispose();
      },
    };
  },
};

export const ENGINES = [nodeSqlite, localD1];
