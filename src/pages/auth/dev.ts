import type { APIRoute } from 'astro';
import { and, asc, eq } from 'drizzle-orm';
import { isDevAuthEnabled } from '../../core/auth';
import { ROLES, type Role } from '../../core/domain';
import { safeNext } from '../../server/auth/next';
import { createSession, setSessionCookie } from '../../server/auth/session';
import { getDb } from '../../server/db/client';
import { users } from '../../server/db/schema';
import { getEnv } from '../../server/env';

/**
 * DEV ONLY: /auth/dev?as=director|officer signs in as the first seeded user with that
 * role (creating a placeholder if the DB is empty), so local dev and Playwright work
 * without Discord.
 *
 * Production safety: `import.meta.env.DEV` is replaced with `false` at build time, so the
 * whole body below the first check is dead code that the bundler drops, and the route
 * always 404s in a production build regardless of DEV_AUTH. `npm run build` runs
 * scripts/check-build.mjs, which fails the build if the log line below survives.
 */
export const GET: APIRoute = async ({ url, request, cookies, redirect }) => {
  if (!import.meta.env.DEV) return new Response('Not found', { status: 404 });
  if (!isDevAuthEnabled({ devBuild: import.meta.env.DEV, flag: getEnv().DEV_AUTH })) {
    return new Response('Dev login is off. Set DEV_AUTH=1 in .dev.vars.', { status: 404 });
  }

  const as = url.searchParams.get('as') as Role;
  if (!ROLES.includes(as)) return new Response('as must be director or officer', { status: 400 });

  const db = getDb();
  let user = await db
    .select({ id: users.id })
    .from(users)
    .where(and(eq(users.role, as), eq(users.active, true)))
    .orderBy(asc(users.createdAt), asc(users.id))
    .get();
  if (!user) {
    const id = `dev-${as}`;
    await db
      .insert(users)
      .values({ id, discordId: as === 'director' ? '000000000000000001' : '000000000000000002', name: `Dev ${as}`, role: as })
      .onConflictDoNothing();
    user = { id };
  }

  console.warn(`[dev-auth] signing in as ${as} (${user.id})`);
  const session = await createSession(db, user.id, request.headers.get('user-agent'));
  setSessionCookie(cookies, session.token, session.expiresAt);
  return redirect(safeNext(url.searchParams.get('next')), 302);
};
