/**
 * Every URL the UI builds. v2 has five pages; details open in a panel on the list page and
 * are addressed with query strings, so the static build needs no server-side fallback.
 * See README.md "Routes" for the table the Go server mirrors.
 */
const enc = encodeURIComponent;
const qs = (params: Record<string, string | number | null | undefined | false>) => {
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) if (v !== undefined && v !== null && v !== '' && v !== false) p.set(k, String(v));
  const s = p.toString();
  return s ? `?${s}` : '';
};

export type FinanceTab = 'requests' | 'budgets' | 'reports' | 'periods';
export type AdminTab = 'users' | 'tokens' | 'audit';

export const href = {
  home: '/',
  login: (next?: string) => `/login${qs({ next: next && next !== '/' ? next : null })}`,
  settings: '/settings',

  events: (o: { when?: 'upcoming' | 'past'; q?: string } = {}) => `/events${qs(o)}`,
  event: (id: string) => `/events?id=${enc(id)}`,
  eventImport: (eventId?: string) => `/events${qs({ import: 1, id: eventId })}`,
  eventNew: () => '/events?new=1',
  eventEdit: (id: string) => `/events?edit=${enc(id)}`,
  eventKit: (id: string) => `/events?kit=${enc(id)}`,
  eventDelete: (id: string) => `/events?delete=${enc(id)}`,

  finance: (tab?: FinanceTab, extra: Record<string, string | number | null | undefined> = {}) => `/finance${qs({ tab: tab === 'requests' ? null : tab, ...extra })}`,
  requests: (o: { scope?: 'mine' | 'all'; status?: string } = {}) => `/finance${qs(o)}`,
  request: (id: string) => `/finance?id=${enc(id)}`,
  newRequest: (eventId?: string) => `/finance${qs({ new: 1, eventId })}`,
  report: (report: string) => `/finance?tab=reports&report=${enc(report)}`,

  admin: (tab?: AdminTab) => `/admin${qs({ tab: tab === 'users' ? null : tab })}`,
};

/**
 * Map a v1-style path (the Go server's search may return these) to its v2 home:
 * /requests/:id → /finance?id=, /events/:id → /events?id=, /admin/tokens → /admin?tab=tokens.
 * Anything else passes through.
 */
export function normalizeHref(raw: string): string {
  let u: URL;
  try {
    u = new URL(raw, 'http://x');
  } catch {
    return raw;
  }
  if (u.host !== 'x') return raw;
  const p = u.pathname.replace(/\/+$/, '') || '/';
  let m: RegExpExecArray | null;
  if (p === '/requests/new') return href.newRequest(u.searchParams.get('eventId') ?? undefined);
  if ((m = /^\/requests\/([^/]+)$/.exec(p))) return href.request(decodeURIComponent(m[1]!));
  if (p === '/requests') return `/finance${u.search}`;
  if (p === '/events/import') return href.eventImport();
  if ((m = /^\/events\/([^/]+)$/.exec(p))) return href.event(decodeURIComponent(m[1]!));
  if (p === '/admin/users') return href.admin('users');
  if (p === '/admin/tokens') return href.admin('tokens');
  if (p === '/admin/audit') return href.admin('audit');
  return `${p}${u.search}${u.hash}`;
}
