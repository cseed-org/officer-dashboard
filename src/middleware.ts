import { defineMiddleware } from 'astro:middleware';
import { hasRole, requiredRoleForPath } from './core/auth';
import { apiError, HttpError } from './server/auth/guard';
import { SESSION_COOKIE, clearSessionCookie, setSessionCookie, validateSession } from './server/auth/session';
import { getDb } from './server/db/client';

/**
 * 1. Attach locals.user from the session cookie.
 * 2. Enforce the path's minimum role (src/core/auth.ts#requiredRoleForPath):
 *    pages redirect to /login or render /403; APIs answer 401/403 ApiError JSON.
 * 3. Turn HttpErrors thrown by requireRole() in handlers into their responses.
 */
export const onRequest = defineMiddleware(async (context, next) => {
  context.locals.user = null;
  context.locals.sessionId = null;

  const token = context.cookies.get(SESSION_COOKIE)?.value;
  if (token) {
    const session = await validateSession(getDb(), token);
    if (session) {
      context.locals.user = session.user;
      context.locals.sessionId = session.sessionId;
      if (session.renewedExpiresAt) setSessionCookie(context.cookies, token, session.renewedExpiresAt);
    } else {
      clearSessionCookie(context.cookies);
    }
  }

  const { pathname, search } = context.url;
  const isApi = pathname.startsWith('/api/');
  const required = requiredRoleForPath(pathname);
  const user = context.locals.user;

  if (required && !user) {
    if (isApi) return apiError(401, 'unauthenticated', 'Sign in first');
    return context.redirect(`/login?next=${encodeURIComponent(pathname + search)}`);
  }
  if (required && user && !hasRole(user.role, required)) {
    if (isApi) return apiError(403, 'forbidden', `Requires the ${required} role`);
    return context.rewrite(new Request(new URL('/403', context.url), { headers: context.request.headers }));
  }

  try {
    const response = await next();
    if (!isApi && user) response.headers.set('Cache-Control', 'private, no-store');
    return response;
  } catch (error) {
    if (error instanceof HttpError) return error.response;
    throw error;
  }
});
