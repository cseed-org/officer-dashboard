import { drizzle } from 'drizzle-orm/d1';
import { getEnv } from '../env';
import * as schema from './schema';

export type Db = ReturnType<typeof createDb>;

export function createDb(d1: D1Database) {
  return drizzle(d1, { schema });
}

/** Drizzle over the D1 binding `DB`. */
export function getDb(): Db {
  return createDb(getEnv().DB);
}

export { schema };
