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
export const SIDEBAR_LAST_KEY = 'cseed-sidebar-last';

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
  if (!w) {
    delete root.dataset.sidebar;
    root.style.removeProperty('--sidebar');
    store.set(SIDEBAR_KEY, null);
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
  if (busy || timer) return;
  if (w !== (Number(store.get(SIDEBAR_KEY)) || 0)) applySidebarWidth(w);
}
export const setResizing = (on: boolean) => { busy = on; document.documentElement.classList.toggle('is-resizing', on); };

/** Save after the user stops adjusting. Failures keep the local width for this device. */
export function saveSidebarWidth(w: number, save: (w: number) => Promise<unknown>) {
  clearTimeout(timer);
  timer = window.setTimeout(() => {
    timer = 0;
    save(w).catch(() => {});
  }, 400);
}
