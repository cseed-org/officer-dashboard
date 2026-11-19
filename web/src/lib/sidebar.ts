/**
 * Resizable left nav (desktop/tablet). The width lives in the user's server prefs
 * (`sidebarWidth`, 0 = never resized) so it follows them across devices, and in localStorage so
 * the inline PREPAINT script in AppLayout can apply it before first paint (no layout jump).
 *
 * <html> state: no `data-sidebar` = the stylesheet defaults (232px, icons-only on tablets);
 * `rail` = icons only (64px); `wide` = labels shown at `--sidebar` px.
 */
export const SIDEBAR_MIN = 64;
export const SIDEBAR_MAX = 320;
/** Dragging below this snaps to icons only; keyboard steps jump between SIDEBAR_MIN and this. */
export const SIDEBAR_SNAP = 120;
export const SIDEBAR_DEFAULT = 232;
export const SIDEBAR_KEY = 'cseed-sidebar';
export const SIDEBAR_SEEN_KEY = 'cseed-sidebar-seen';
export const SIDEBAR_LAST_KEY = 'cseed-sidebar-last';

/** The sidebar's People folder: '0' = collapsed, anything else (or nothing) = open. Device-local, like the width cache. */
export const PEOPLE_KEY = 'cseed-people';

const store = {
  get(k: string) {
    try { return localStorage.getItem(k); } catch { return null; }
  },
  set(k: string, v: string | null) {
    try { v === null ? localStorage.removeItem(k) : localStorage.setItem(k, v); } catch { /* private mode */ }
  },
};

/** A free width, snapped: below SIDEBAR_SNAP is icons only, otherwise clamped to the range. */
export const snapWidth = (w: number) => (w < SIDEBAR_SNAP ? SIDEBAR_MIN : Math.min(SIDEBAR_MAX, Math.round(w)));

export function currentWidth(): number {
  const root = document.documentElement;
  if (root.dataset.sidebar === 'rail') return SIDEBAR_MIN;
  if (root.dataset.sidebar === 'wide') return parseInt(root.style.getPropertyValue('--sidebar'), 10) || SIDEBAR_DEFAULT;
  return matchMedia('(max-width: 1099px)').matches ? SIDEBAR_MIN : SIDEBAR_DEFAULT;
}

/** The width to reopen to after icons only (double-click). */
export const lastWideWidth = () => {
  const n = Number(store.get(SIDEBAR_LAST_KEY));
  return n >= SIDEBAR_SNAP && n <= SIDEBAR_MAX ? n : SIDEBAR_DEFAULT;
};

/** Show a width on the page and remember it on this device. 0 restores the defaults. */
export function applySidebarWidth(w: number) {
  const root = document.documentElement;
  store.set(SIDEBAR_SEEN_KEY, '1');
  if (!w) {
    delete root.dataset.sidebar;
    root.style.removeProperty('--sidebar');
    store.set(SIDEBAR_KEY, null);
    window.dispatchEvent(new Event('cseed-sidebar'));
    return;
  }
  const n = snapWidth(w);
  root.dataset.sidebar = n === SIDEBAR_MIN ? 'rail' : 'wide';
  root.style.setProperty('--sidebar', `${n}px`);
  store.set(SIDEBAR_KEY, String(n));
  if (n > SIDEBAR_MIN) store.set(SIDEBAR_LAST_KEY, String(n));
  window.dispatchEvent(new Event('cseed-sidebar'));
}

let busy = false;
let timer = 0;
/** The server's saved width wins once prefs load, unless the user is mid-resize or has an unsaved change. */
export function syncSidebarFromServer(w: number) {
  // The page's first prefs response arrived after AppLayout's head script revealed the shell
  // (slow network): never resize under the user; remember it so the next page paints it.
  const w2 = window as unknown as { __cseedSidebarLate?: boolean };
  if (w2.__cseedSidebarLate) {
    w2.__cseedSidebarLate = false;
    store.set(SIDEBAR_KEY, w ? String(w) : null);
    store.set(SIDEBAR_SEEN_KEY, '1');
    return;
  }
  if (busy || timer || inFlight || pending !== null) return;
  if (w !== (Number(store.get(SIDEBAR_KEY)) || 0)) applySidebarWidth(w);
}
export const setResizing = (on: boolean) => { busy = on; document.documentElement.classList.toggle('is-resizing', on); };

let pending: number | null = null;
let flushOnLeave = false;
let inFlight = false;
let leaving = false;
let saveLatest: ((w: number) => Promise<unknown>) | null = null;
async function drainSaves() {
  if (inFlight || pending === null || !saveLatest) return;
  const w = pending;
  pending = null;
  inFlight = true;
  try {
    if (leaving) await fetch('/api/me/prefs', { method: 'PUT', credentials: 'same-origin', keepalive: true, headers: { 'content-type': 'application/json' }, body: JSON.stringify({ sidebarWidth: w }) });
    else await saveLatest(w);
  } catch { /* the local width still applies */ }
  finally { inFlight = false; if (!timer) void drainSaves(); }
}
/**
 * Leaving the page inside the debounce window (resize, then click a nav link at once) would drop
 * the save, and the next page's SIDEBARWAIT would then restore the old server width. Send the
 * pending width with keepalive so it outlives this document.
 */
function flushPendingSave() {
  leaving = true;
  clearTimeout(timer);
  timer = 0;
  void drainSaves();
}

/** Save after the user stops adjusting. Failures keep the local width for this device. */
export function saveSidebarWidth(w: number, save: (w: number) => Promise<unknown>) {
  if (!flushOnLeave) {
    flushOnLeave = true;
    window.addEventListener('pagehide', flushPendingSave);
  }
  clearTimeout(timer);
  saveLatest = save;
  pending = w;
  timer = window.setTimeout(() => {
    timer = 0;
    void drainSaves();
  }, 400);
}

/** Remember the People folder open/closed on this device; `<html data-people="closed">` is what the PREPAINT script and CSS read. */
export function savePeopleOpen(open: boolean) {
  store.set(PEOPLE_KEY, open ? null : '0');
  if (open) delete document.documentElement.dataset.people;
  else document.documentElement.dataset.people = 'closed';
}
