import { env } from 'cloudflare:workers';

/** Worker bindings, vars and secrets. The only module that imports `cloudflare:workers` for env. */
export function getEnv(): Cloudflare.Env {
  return env;
}

/** Public origin, used for OAuth redirect URIs. Falls back to the request origin in dev. */
export function appUrl(request: Request): string {
  return import.meta.env.DEV ? new URL(request.url).origin : getEnv().APP_URL;
}
