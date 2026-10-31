import type { APIRoute } from 'astro';
import { eq } from 'drizzle-orm';
import { decideLogin } from '../../core/auth';
import { OAUTH_STATE_COOKIE, OAUTH_VERIFIER_COOKIE, avatarUrl, discordClient, fetchDiscordProfile } from '../../server/auth/discord';
import { safeNext } from '../../server/auth/next';
import { createSession, setSessionCookie } from '../../server/auth/session';
import { getDb } from '../../server/db/client';
import { auditLog, users } from '../../server/db/schema';
import { getEnv } from '../../server/env';

export const GET: APIRoute = async ({ request, url, cookies, redirect }) => {
  const code = url.searchParams.get('code');
  const state = url.searchParams.get('state');
  const storedState = cookies.get(OAUTH_STATE_COOKIE)?.value;
  const verifier = cookies.get(OAUTH_VERIFIER_COOKIE)?.value;
  const next = safeNext(cookies.get('cseed_next')?.value);
  for (const name of [OAUTH_STATE_COOKIE, OAUTH_VERIFIER_COOKIE, 'cseed_next']) cookies.delete(name, { path: '/' });

  if (!code || !state || !storedState || !verifier || state !== storedState) {
    return redirect('/login?error=state', 302);
  }

  let profile;
  try {
    const tokens = await discordClient(request).validateAuthorizationCode(code, verifier);
    profile = await fetchDiscordProfile(tokens.accessToken());
  } catch (error) {
    console.error('Discord OAuth failed', error);
    return redirect('/login?error=discord', 302);
  }

  const db = getDb();
  const existing = await db.select().from(users).where(eq(users.discordId, profile.id)).get();
  const decision = decideLogin({
    discordId: profile.id,
    user: existing ?? null,
    bootstrapDirectorId: getEnv().BOOTSTRAP_DIRECTOR_DISCORD_ID,
  });
  if (decision.kind === 'deny') return redirect(`/login?error=${decision.reason}`, 302);

  const name = profile.global_name ?? profile.username;
  let userId: string;
  if (decision.kind === 'create_director') {
    userId = crypto.randomUUID();
    await db.batch([
      db.insert(users).values({ id: userId, discordId: profile.id, name, avatarUrl: avatarUrl(profile), role: 'director' }),
      db.insert(auditLog).values({
        id: crypto.randomUUID(),
        actorUserId: userId,
        action: 'user.bootstrap_director',
        entityType: 'user',
        entityId: userId,
        afterJson: JSON.stringify({ discordId: profile.id, role: 'director' }),
      }),
    ]);
  } else {
    userId = decision.userId;
    await db.update(users).set({ avatarUrl: avatarUrl(profile) }).where(eq(users.id, userId));
  }

  const session = await createSession(db, userId, request.headers.get('user-agent'));
  setSessionCookie(cookies, session.token, session.expiresAt);
  return redirect(next, 302);
};
