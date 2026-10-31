import type { APIRoute } from 'astro';
import { clearSessionCookie, invalidateSession } from '../../server/auth/session';
import { getDb } from '../../server/db/client';

/** POST only (a form button), so a cross-site link can't sign people out. Astro checkOrigin guards it. */
export const POST: APIRoute = async ({ locals, cookies, redirect }) => {
  if (locals.sessionId) await invalidateSession(getDb(), locals.sessionId);
  clearSessionCookie(cookies);
  return redirect('/login', 303);
};
