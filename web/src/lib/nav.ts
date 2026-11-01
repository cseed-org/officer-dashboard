/**
 * Tiny same-page router: detail panels and tabs live in the query string. Links to the
 * current page update history without a reload; other links navigate normally.
 */
import { useEffect, useState } from 'preact/hooks';

const EVT = 'cseed:locationchange';

export function navigate(url: string, opts: { replace?: boolean } = {}) {
  const next = new URL(url, location.href);
  if (next.pathname.replace(/\/+$/, '') !== location.pathname.replace(/\/+$/, '')) {
    location.assign(next.href);
    return;
  }
  history[opts.replace ? 'replaceState' : 'pushState'](null, '', next.pathname + next.search + next.hash);
  dispatchEvent(new Event(EVT));
}

/** Patch query params on the current URL (null removes one). */
export function setParams(patch: Record<string, string | number | null | undefined>, opts: { replace?: boolean } = {}) {
  const url = new URL(location.href);
  for (const [k, v] of Object.entries(patch)) {
    if (v === null || v === undefined || v === '') url.searchParams.delete(k);
    else url.searchParams.set(k, String(v));
  }
  navigate(url.pathname + url.search, opts);
}

/** The current query string, re-rendering on back/forward and navigate(). */
export function useSearch(): URLSearchParams {
  const [search, setSearch] = useState(() => (typeof location === 'undefined' ? '' : location.search));
  useEffect(() => {
    const on = () => setSearch(location.search);
    addEventListener('popstate', on);
    addEventListener(EVT, on);
    on();
    return () => {
      removeEventListener('popstate', on);
      removeEventListener(EVT, on);
    };
  }, []);
  return new URLSearchParams(search);
}

let installed = false;
/** Intercept plain clicks on links to the current page (installed once per page). */
export function interceptSamePageLinks() {
  if (installed || typeof document === 'undefined') return;
  installed = true;
  document.addEventListener('click', (e) => {
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    const a = (e.target as Element | null)?.closest?.('a');
    if (!a || !a.href || a.target || a.hasAttribute('download') || a.dataset.reload !== undefined) return;
    const url = new URL(a.href);
    if (url.origin !== location.origin) return;
    if (url.pathname.replace(/\/+$/, '') !== location.pathname.replace(/\/+$/, '')) return;
    if (url.search === location.search && url.hash) return; // in-page anchor
    e.preventDefault();
    navigate(url.pathname + url.search + url.hash);
  });
}
