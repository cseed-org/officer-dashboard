import type { APIRoute } from 'astro';
import { sql } from 'drizzle-orm';
import type { HealthResponse } from '../../core/contracts';
import { getDb } from '../../server/db/client';

export const GET: APIRoute = async () => {
  let db = false;
  try {
    await getDb().run(sql`SELECT 1`);
    db = true;
  } catch {}
  const body: HealthResponse = { ok: true, db, version: '0.1.0' };
  return Response.json(body);
};
