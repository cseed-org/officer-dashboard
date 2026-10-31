import type { AstroCookies } from 'astro';
import { and, eq, gt } from 'drizzle-orm';
import type { CurrentUser } from '../../core/contracts';
import { randomToken, sha256Hex } from '../crypto';
import type { Db } from '../db/client';
import { sessions, users } from '../db/schema';

/**
 * Sessions: a random token in an httpOnly cookie; D1 stores only sha256(token), so a
 * leaked database can't be replayed as cookies. 30-day sliding expiry.
 */
const SESSION_TTL_MS = 30 * 24 * 60 * 60 * 1000;
const RENEW_WHEN_LEFT_MS = 15 * 24 * 60 * 60 * 1000;

/** `__Host-` pins the cookie to this origin over HTTPS; plain name in dev (http://localhost). */
export const SESSION_COOKIE = import.meta.env.DEV ? 'cseed_session' : '__Host-cseed_session';

export async function createSession(db: Db, userId: string, userAgent: string | null) {
  const token = randomToken();
  const id = await sha256Hex(token);
  const expiresAt = new Date(Date.now() + SESSION_TTL_MS);
  await db.insert(sessions).values({ id, userId, expiresAt, userAgent: userAgent?.slice(0, 300) ?? null });
  await db.update(users).set({ lastLoginAt: new Date() }).where(eq(users.id, userId));
  return { token, id, expiresAt };
}

/** The active user for a session token, or null. Renews the session when half-expired. */
export async function validateSession(
  db: Db,
  token: string,
): Promise<{ user: CurrentUser; sessionId: string; renewedExpiresAt: Date | null } | null> {
  const id = await sha256Hex(token);
  const now = new Date();
  const row = await db
    .select({ session: sessions, user: users })
    .from(sessions)
    .innerJoin(users, eq(users.id, sessions.userId))
    .where(and(eq(sessions.id, id), gt(sessions.expiresAt, now), eq(users.active, true)))
    .get();
  if (!row) return null;

  let renewedExpiresAt: Date | null = null;
  if (row.session.expiresAt.getTime() - now.getTime() < RENEW_WHEN_LEFT_MS) {
    renewedExpiresAt = new Date(now.getTime() + SESSION_TTL_MS);
    await db.update(sessions).set({ expiresAt: renewedExpiresAt }).where(eq(sessions.id, id));
  }
  const u = row.user;
  return {
    sessionId: id,
    renewedExpiresAt,
    user: { id: u.id, name: u.name, avatarUrl: u.avatarUrl, discordId: u.discordId, role: u.role, title: u.title },
  };
}

export async function invalidateSession(db: Db, sessionId: string) {
  await db.delete(sessions).where(eq(sessions.id, sessionId));
}

export function setSessionCookie(cookies: AstroCookies, token: string, expires: Date) {
  cookies.set(SESSION_COOKIE, token, {
    httpOnly: true,
    secure: !import.meta.env.DEV,
    sameSite: 'lax',
    path: '/',
    expires,
  });
}

export function clearSessionCookie(cookies: AstroCookies) {
  cookies.delete(SESSION_COOKIE, { path: '/' });
}
