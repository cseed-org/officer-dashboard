/**
 * URL safety helpers. Everything that turns data (API fields, ?next=, search results) into
 * a link or a navigation goes through here.
 */

const currentOrigin = () => (typeof location === 'undefined' ? 'http://localhost' : location.origin);

/**
 * A same-origin path ("/finance?id=1") for `raw`, or null when it would leave the site.
 * Resolved with `new URL`, so "//evil", "/\evil", "\\evil", "https://evil" and
 * "javascript:…" are all rejected (browsers treat "\" like "/").
 */
export function internalPath(raw: string | null | undefined, origin = currentOrigin()): string | null {
  if (!raw) return null;
  const s = raw.trim();
  // Only path-absolute input: a relative "foo" or a scheme is never an internal route here.
  if (!s.startsWith('/') || s.startsWith('//') || s.includes('\\')) return null;
  let u: URL;
  try {
    u = new URL(s, origin);
  } catch {
    return null;
  }
  if (u.origin !== new URL(origin).origin) return null;
  return `${u.pathname}${u.search}${u.hash}`;
}

/** Internal pages the UI may navigate to from data (search results, ?next=). */
const INTERNAL_PAGES = ['/', '/events', '/finance', '/admin', '/settings'];

/** internalPath, further limited to the dashboard's own pages (not /api, /auth or assets). */
export function internalRoute(raw: string | null | undefined, origin = currentOrigin()): string | null {
  const p = internalPath(raw, origin);
  if (!p) return null;
  const path = p.split(/[?#]/)[0]!.replace(/\/+$/, '') || '/';
  return INTERNAL_PAGES.includes(path) ? p : null;
}

/**
 * A link target for data: same-origin paths (checked as above) or absolute http(s) URLs.
 * Anything else (javascript:, data:, protocol-relative, backslash tricks) → null.
 */
export function safeUrl(url: string | null | undefined, origin = currentOrigin()): string | null {
  if (!url) return null;
  const s = url.trim();
  if (s.startsWith('/') || s.startsWith('\\')) return internalPath(s, origin);
  try {
    const u = new URL(s);
    return u.protocol === 'https:' || u.protocol === 'http:' ? u.href : null;
  } catch {
    return null;
  }
}

/** Images from data (avatars): https only. */
export function safeImageUrl(url: string | null | undefined): string | null {
  const s = safeUrl(url);
  return s && s.startsWith('https://') ? s : null;
}
