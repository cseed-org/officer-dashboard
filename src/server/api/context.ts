import { sql, eq, type SQL } from 'drizzle-orm';
import type { Db } from '../db/client';
import { auditLog, mutationGuards, serviceTokens } from '../db/schema';
import { apiError, HttpError } from '../auth/guard';
import { sha256Hex } from '../crypto';
import type { CurrentUser, ApiError } from '../../core/contracts';
export type BatchItem = Parameters<Db['batch']>[0][number];
export interface ApiContext {
  db: Db;
  user: CurrentUser | null;
  tokenId: string | null;
  request: Request;
  url: URL;
}
export function fail(
  status: number,
  code: ApiError['error']['code'],
  message: string,
): never {
  throw new HttpError(apiError(status, code, message));
}
export function found<T>(row: T | undefined | null): T {
  if (!row) fail(404, 'not_found', 'Record not found');
  return row;
}
export function director(c: ApiContext) {
  officer(c);
  if (c.user?.role !== 'director')
    fail(403, 'forbidden', 'Requires director role');
  return c.user!;
}
export function officer(c: ApiContext) {
  if (!c.user) fail(401, 'unauthenticated', 'Sign in first');
  return c.user;
}
export const now = () => new Date();
export const uuid = () => crypto.randomUUID();
export const iso = (d: Date | null) => d?.toISOString() ?? null;
export function audit(
  c: ApiContext,
  action: string,
  entityType: string,
  entityId: string,
  before: unknown,
  after: unknown,
): BatchItem {
  return c.db.insert(auditLog).values({
    id: uuid(),
    actorUserId: c.user?.id ?? null,
    actorTokenId: c.tokenId,
    action,
    entityType,
    entityId,
    beforeJson: before == null ? null : JSON.stringify(before),
    afterJson: after == null ? null : JSON.stringify(after),
  });
}
export async function mutate(
  c: ApiContext,
  condition: SQL,
  writes: BatchItem[],
  action: string,
  entity: string,
  id: string,
  before: unknown,
  after: unknown,
) {
  const guard = uuid();
  if (c.user)
    condition = sql`${condition} AND EXISTS (SELECT 1 FROM users WHERE id = ${c.user.id} AND active = 1 AND role = ${c.user.role})`;
  if (c.tokenId)
    condition = sql`${condition} AND EXISTS (SELECT 1 FROM service_tokens WHERE id = ${c.tokenId} AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ${Date.now()}))`;
  await c.db.batch([
    c.db
      .insert(mutationGuards)
      .values({ id: guard, ok: sql`CASE WHEN ${condition} THEN 1 ELSE 0 END` }),
    ...writes,
    audit(c, action, entity, id, before, after),
    c.db.delete(mutationGuards).where(eq(mutationGuards.id, guard)),
  ]);
}
export async function authenticateToken(c: ApiContext, scope: string) {
  const match = /^Bearer ([^\s]+)$/i.exec(
    c.request.headers.get('Authorization') ?? '',
  );
  if (!match) fail(401, 'unauthenticated', 'Service token required');
  const token = await c.db
    .select()
    .from(serviceTokens)
    .where(eq(serviceTokens.tokenHash, await sha256Hex(match[1]!)))
    .get();
  if (
    !token ||
    token.revokedAt ||
    (token.expiresAt && token.expiresAt <= now())
  )
    fail(401, 'unauthenticated', 'Invalid or expired service token');
  if (!token.scopes.includes(scope))
    fail(403, 'forbidden', 'Service token lacks required scope');
  c.tokenId = token.id;
  c.user = null;
  // Usage metadata is part of the endpoint mutation batch; reads leave token state unchanged.
  return token;
}
/** Composite date/id cursors stay stable when newer rows are inserted. */
export function page<T extends { id: string }>(
  rows: T[],
  query: { cursor?: string; limit: number },
) {
  const key = (r: T) => {
    const record = r as Record<string, unknown>;
    const value = record.startsAt ?? record.createdAt ?? record.updatedAt;
    const date =
      value instanceof Date
        ? value.toISOString()
        : typeof value === 'string' && Number.isFinite(Date.parse(value))
          ? new Date(value).toISOString()
          : '';
    return `${date}|${r.id}`;
  };
  const sorted = [...rows]
    .sort((a, b) => key(b).localeCompare(key(a)))
    .filter((r) => !query.cursor || key(r) < query.cursor);
  const items = sorted.slice(0, query.limit);
  return {
    items,
    nextCursor: sorted.length > query.limit ? key(items.at(-1)!) : null,
  };
}
