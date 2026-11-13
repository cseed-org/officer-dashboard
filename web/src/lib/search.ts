import { normalizeHref } from './routes';
import { internalRoute } from './url';

/**
 * Search hrefs come from the API. Only same-origin dashboard routes survive (v1-style
 * paths are mapped to v2 first); anything else (javascript:, //host, https://elsewhere,
 * /api/…, backslash forms) is dropped and never navigated to.
 */
export function sanitizeResults<T extends { href: string }>(results: T[], origin?: string): T[] {
  const out: T[] = [];
  for (const r of results) {
    const safe = internalRoute(normalizeHref(r.href), origin);
    if (safe) out.push({ ...r, href: safe });
  }
  return out;
}
