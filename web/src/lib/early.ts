/**
 * Early API reads. Every signed-in page needs /api/me, then the preferences and club settings
 * (timezone), and Home needs its overview, before anything paints. Instead of waiting for the
 * islands' JavaScript to download, evaluate and then ask in turn, an inline <head> script starts
 * these GETs while the HTML is still parsing; `api()` takes each response once, in place of its
 * own fetch, and handles it exactly as before (status, 401 → login, contract validation).
 *
 * The script is static per page, so the CSP hashes it like the other inline scripts (#43).
 */

/** The reads every signed-in page makes first (session.ts → timezone.ts). */
export const EARLY_SHARED = ['/api/me', '/api/me/prefs', '/api/club/settings'] as const;

/** Paths each page starts early on top of EARLY_SHARED. Must equal `endpointUrl()` exactly. */
export const EARLY_PAGE: Partial<Record<string, readonly string[]>> = {
  home: ['/api/overview', '/api/cloudflare/widgets'],
};

const KEY = '__cseedEarly';

/** The inline script for a page: fetch each path now, keep the promises for `takeEarly`. */
export function earlyScript(paths: readonly string[]): string {
  return `(function(){var m=window.${KEY}={};${JSON.stringify(paths)}.forEach(function(u){var p=fetch(u,{credentials:'same-origin',headers:{accept:'application/json'}});p.catch(function(){});m[u]=p})})();`;
}

/** The early response for `url`, once; later calls (reloads, refreshes) fetch normally. */
export function takeEarly(url: string): Promise<Response> | undefined {
  if (typeof window === 'undefined') return undefined;
  const map = (window as unknown as Record<string, Record<string, Promise<Response>> | undefined>)[KEY];
  const p = map?.[url];
  if (p) delete map![url];
  return p;
}
