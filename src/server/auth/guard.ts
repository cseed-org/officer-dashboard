import { hasRole } from '../../core/auth';
import type { ApiError } from '../../core/contracts';
import type { CurrentUser } from '../../core/contracts';
import type { Role } from '../../core/domain';

export function apiError(status: number, code: ApiError['error']['code'], message: string, details?: unknown) {
  const body: ApiError = { error: { code, message, ...(details === undefined ? {} : { details }) } };
  return Response.json(body, { status });
}

export class HttpError extends Error {
  constructor(
    readonly response: Response,
  ) {
    super(`HTTP ${response.status}`);
  }
}

/**
 * For API handlers: returns the user if they hold `role`, otherwise throws an HttpError
 * carrying a 401/403 ApiError response. The middleware converts thrown HttpErrors.
 *
 *   const user = requireRole(Astro.locals, 'director');
 */
export function requireRole(locals: App.Locals, role: Role): CurrentUser {
  const user = locals.user;
  if (!user) throw new HttpError(apiError(401, 'unauthenticated', 'Sign in first'));
  if (!hasRole(user.role, role)) throw new HttpError(apiError(403, 'forbidden', `Requires the ${role} role`));
  return user;
}
