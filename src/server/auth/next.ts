/** Placeholder origin used only to resolve and compare; never sent anywhere. */
const BASE = 'https://app.invalid';

/**
 * The post-login redirect target, or '/' unless it is a same-origin path.
 *
 * Browsers strip tabs/newlines and treat '\' like '/' when parsing URLs, so
 * '/\t/evil.com' or '/\evil.com' become '//evil.com'. Anything with whitespace, control
 * characters or backslashes is refused outright; the rest is resolved against a fixed
 * origin and must stay on it. The returned value is the normalised path + query + hash.
 */
export function safeNext(next: string | null | undefined): string {
  if (!next || !next.startsWith('/') || /[\s\x00-\x1f\x7f\\]/.test(next)) return '/';
  let url: URL;
  try {
    url = new URL(next, BASE);
  } catch {
    return '/';
  }
  if (url.origin !== BASE) return '/';
  return `${url.pathname}${url.search}${url.hash}`;
}
