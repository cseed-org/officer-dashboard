/** Placeholder origin used only to resolve and compare; never sent anywhere. */
const BASE = 'https://app.invalid';

/** Top-level page sections a post-login redirect may land in (besides `/`). */
export const REDIRECT_SECTIONS = ['events', 'requests', 'finance', 'admin'] as const;

/**
 * The post-login redirect target, or '/' unless it is one of the app's own pages.
 *
 * Layers, each enough on its own for the cases it covers:
 * 1. Refuse whitespace, control characters and backslashes. Browsers strip tabs and
 *    newlines and treat '\' as '/', so '/\t/evil.com' or '/\evil.com' become '//evil.com'.
 * 2. Refuse anything that isn't a single-slash path ('//host', 'https://…').
 * 3. Resolve against a fixed origin (normalising '.', '..' and '%2e' segments). The result
 *    must stay on that origin and must not itself start with '//': '/.//evil.com'
 *    normalises to '//evil.com', which a browser reads as another host.
 * 4. Allowlist: the first path segment must be one of REDIRECT_SECTIONS, so /api/* and
 *    /auth/* can never be a redirect target.
 * The returned value is the normalised path + query + hash.
 */
export function safeNext(next: string | null | undefined): string {
  if (!next || /[\s\x00-\x1f\x7f\\]/.test(next)) return '/';
  if (!next.startsWith('/') || next.startsWith('//')) return '/';
  let url: URL;
  try {
    url = new URL(next, BASE);
  } catch {
    return '/';
  }
  if (url.origin !== BASE) return '/';
  const path = url.pathname;
  if (path.startsWith('//') || path.startsWith('/\\')) return '/';
  if (path !== '/') {
    const section = path.split('/')[1] ?? '';
    if (!(REDIRECT_SECTIONS as readonly string[]).includes(section)) return '/';
  }
  return `${path}${url.search}${url.hash}`;
}
