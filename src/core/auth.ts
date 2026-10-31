import type { Role } from './domain';

const RANK: Record<Role, number> = { officer: 1, director: 2 };

/** Directors can do everything officers can. */
export function hasRole(actual: Role, required: Role): boolean {
  return RANK[actual] >= RANK[required];
}

const under = (path: string, prefix: string) => path === prefix || path.startsWith(`${prefix}/`);

/** Paths reachable without a session. */
const PUBLIC_PREFIXES = ['/login', '/auth', '/api/health'];
/** Paths authenticated by a service token (Authorization: Bearer) inside the handler. */
const SERVICE_TOKEN_PREFIXES = ['/api/ingest', '/api/agent'];
const DIRECTOR_PREFIXES = ['/admin', '/api/admin'];

/**
 * The minimum role a signed-in user needs for a path, or null when the session middleware
 * should not enforce one (public pages, service-token APIs). Finer rules, such as
 * "only directors approve", are enforced in the handler with requireRole().
 */
export function requiredRoleForPath(pathname: string): Role | null {
  if ([...PUBLIC_PREFIXES, ...SERVICE_TOKEN_PREFIXES].some((p) => under(pathname, p))) return null;
  if (DIRECTOR_PREFIXES.some((p) => under(pathname, p))) return 'director';
  return 'officer';
}

/**
 * The dev login bypass (/auth/dev) needs BOTH a development build and DEV_AUTH=1.
 * `devBuild` must come from `import.meta.env.DEV`, which Vite replaces with the literal
 * `false` in production builds, so the bypass is dead code there whatever env is set.
 */
export function isDevAuthEnabled(opts: { devBuild: boolean; flag: string | undefined }): boolean {
  return opts.devBuild === true && opts.flag === '1';
}

export type LoginDecision =
  | { kind: 'sign_in'; userId: string }
  | { kind: 'create_director' }
  | { kind: 'deny'; reason: 'not_allowlisted' | 'inactive' };

/**
 * Only users already in `users` (allowlisted by a director) may sign in. The one exception
 * is BOOTSTRAP_DIRECTOR_DISCORD_ID, which is created as a director on first login so a
 * fresh deployment has someone to allowlist everybody else.
 */
export function decideLogin(input: {
  discordId: string;
  user: { id: string; role: Role; active: boolean } | null;
  bootstrapDirectorId: string | undefined;
}): LoginDecision {
  const { discordId, user, bootstrapDirectorId } = input;
  if (user) return user.active ? { kind: 'sign_in', userId: user.id } : { kind: 'deny', reason: 'inactive' };
  if (bootstrapDirectorId && discordId === bootstrapDirectorId.trim()) return { kind: 'create_director' };
  return { kind: 'deny', reason: 'not_allowlisted' };
}
