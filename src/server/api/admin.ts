import { eq, sql } from 'drizzle-orm';
import * as s from '../db/schema';
import * as C from '../../core/contracts';
import { randomToken, sha256Hex } from '../crypto';
import {
  type ApiContext,
  type BatchItem,
  director,
  found,
  iso,
  uuid,
  now,
  mutate,
  page,
  fail,
} from './context';
import { catalog } from './read-model';
const userView = (u: s.UserRow) => ({
  ...u,
  lastLoginAt: iso(u.lastLoginAt),
  createdAt: u.createdAt.toISOString(),
});
export async function admin(
  c: ApiContext,
  name: string,
  id: string | undefined,
  body: unknown,
  query: unknown,
) {
  const u = director(c);
  if (name === 'adminUsers')
    return { users: (await c.db.select().from(s.users)).map(userView) };
  if (name === 'adminUserCreate') {
    const b = C.AdminUserCreateRequest.parse(body),
      id = uuid();
    await mutate(
      c,
      sql`1`,
      [c.db.insert(s.users).values({ id, ...b })],
      'user.created',
      'user',
      id,
      null,
      b,
    );
    return {
      user: userView(
        found(
          await c.db.select().from(s.users).where(eq(s.users.id, id)).get(),
        ),
      ),
    };
  }
  if (name === 'adminUserUpdate') {
    const before = found(
        await c.db
          .select()
          .from(s.users)
          .where(eq(s.users.id, found(id)))
          .get(),
      ),
      b = C.AdminUserUpdateRequest.parse(body);
    const writes: BatchItem[] = [
      c.db
        .update(s.users)
        .set({ ...b, updatedAt: now() })
        .where(eq(s.users.id, before.id)),
    ];
    if (b.active === false || (b.role && b.role !== before.role))
      writes.push(
        c.db.delete(s.sessions).where(eq(s.sessions.userId, before.id)),
      );
    await mutate(
      c,
      sql`EXISTS (SELECT 1 FROM users WHERE id = ${before.id} AND updated_at = ${before.updatedAt.getTime()})`,
      writes,
      'user.updated',
      'user',
      before.id,
      before,
      b,
    );
    return {
      user: userView(
        found(
          await c.db
            .select()
            .from(s.users)
            .where(eq(s.users.id, before.id))
            .get(),
        ),
      ),
    };
  }
  const cat = await catalog(c.db);
  const tokenView = (t: typeof s.serviceTokens.$inferSelect) => ({
    id: t.id,
    name: t.name,
    prefix: t.prefix,
    scopes: t.scopes,
    createdBy: found(cat.users.find((u) => u.id === t.createdBy)),
    createdAt: t.createdAt.toISOString(),
    lastUsedAt: iso(t.lastUsedAt),
    expiresAt: iso(t.expiresAt),
    revokedAt: iso(t.revokedAt),
  });
  if (name === 'adminTokens')
    return {
      tokens: (await c.db.select().from(s.serviceTokens)).map(tokenView),
    };
  if (name === 'adminTokenCreate') {
    const b = C.ServiceTokenCreateRequest.parse(body),
      id = uuid(),
      plaintext = `cseed_${randomToken()}`;
    const value = {
      id,
      name: b.name,
      scopes: [...new Set(b.scopes)],
      tokenHash: await sha256Hex(plaintext),
      prefix: plaintext.slice(0, 12),
      createdBy: u.id,
      expiresAt:
        b.expiresInDays === null
          ? null
          : new Date(Date.now() + b.expiresInDays * 86400000),
    };
    await mutate(
      c,
      sql`1`,
      [c.db.insert(s.serviceTokens).values(value)],
      'token.created',
      'service_token',
      id,
      null,
      { name: b.name, scopes: value.scopes, prefix: value.prefix },
    );
    return {
      token: tokenView(
        found(
          await c.db
            .select()
            .from(s.serviceTokens)
            .where(eq(s.serviceTokens.id, id))
            .get(),
        ),
      ),
      plaintext,
    };
  }
  if (name === 'adminTokenRevoke') {
    const before = found(
      await c.db
        .select()
        .from(s.serviceTokens)
        .where(eq(s.serviceTokens.id, found(id)))
        .get(),
    );
    if (before.revokedAt) fail(409, 'conflict', 'Token already revoked');
    await mutate(
      c,
      sql`1`,
      [
        c.db
          .update(s.serviceTokens)
          .set({ revokedAt: now() })
          .where(eq(s.serviceTokens.id, before.id)),
      ],
      'token.revoked',
      'service_token',
      before.id,
      { name: before.name },
      { revoked: true },
    );
    return { ok: true };
  }
  const q = C.AuditLogQuery.parse(query);
  const tokens = await c.db.select().from(s.serviceTokens);
  const rows = (await c.db.select().from(s.auditLog))
    .filter(
      (a) =>
        (!q.entityType || a.entityType === q.entityType) &&
        (!q.entityId || a.entityId === q.entityId),
    )
    .map((a) => ({
      ...a,
      actor: cat.users.find((u) => u.id === a.actorUserId) ?? null,
      actorTokenName: tokens.find((t) => t.id === a.actorTokenId)?.name ?? null,
      before: a.beforeJson ? JSON.parse(a.beforeJson) : null,
      after: a.afterJson ? JSON.parse(a.afterJson) : null,
      createdAt: a.createdAt.toISOString(),
    }));
  return page(rows, q);
}
