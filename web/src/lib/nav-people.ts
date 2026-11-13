import { savePeopleOpen } from './sidebar';

/**
 * Sidebar "People" folder. The markup is server-rendered (AppLayout) and `<html data-people>`
 * is set before paint, so this only wires behaviour:
 *  - wide sidebar / phone sheet: the button expands and collapses the group (remembered per
 *    device). A folder holding the current page starts open whatever was saved.
 *  - icons-only rail: the button opens a flyout beside the rail (hover, focus-visible or click;
 *    Arrow keys move, Esc closes and returns focus). The flyout is `position: fixed` so the
 *    sidebar's own scrolling never clips it.
 */
const folder = document.querySelector<HTMLElement>('[data-nav-folder]');
const toggle = folder?.querySelector<HTMLButtonElement>('.nav-folder-toggle');
const panel = folder?.querySelector<HTMLElement>('.nav-folder-panel');

if (folder && toggle && panel) {
  const links = () => Array.from(panel.querySelectorAll<HTMLAnchorElement>('a'));
  /** Icons only: the label is hidden (phone sheet and wide sidebar show it). */
  const isRail = () => getComputedStyle(toggle.querySelector('.nav-label')!).display === 'none';
  const hasCurrent = folder.hasAttribute('data-current');
  let pinned = false; // flyout opened by click/keyboard, so mouseleave keeps it
  let hideTimer = 0;
  let restoring = false; // focus returning to the button after Esc must not reopen the flyout

  const folderOpen = () => folder.dataset.state
    ? folder.dataset.state === 'open'
    : !!hasCurrent || document.documentElement.dataset.people !== 'closed';
  const flyoutOpen = () => folder.dataset.flyout === 'open';

  const sync = () => {
    toggle.setAttribute('aria-expanded', String(isRail() ? flyoutOpen() : folderOpen()));
  };
  const place = () => {
    const r = toggle.getBoundingClientRect();
    const sb = document.getElementById('sidebar')!.getBoundingClientRect();
    panel.style.left = `${sb.right + 4}px`;
    panel.style.top = `${Math.max(8, Math.min(r.top, innerHeight - panel.offsetHeight - 8))}px`;
  };
  const openFlyout = (pin: boolean, focusFirst = false) => {
    clearTimeout(hideTimer);
    if (pin) pinned = true;
    folder.dataset.flyout = 'open';
    place();
    sync();
    if (focusFirst) links()[0]?.focus();
  };
  const closeFlyout = (restore = false) => {
    clearTimeout(hideTimer);
    pinned = false;
    delete folder.dataset.flyout;
    panel.style.left = panel.style.top = '';
    sync();
    if (restore) { restoring = true; toggle.focus(); restoring = false; }
  };

  toggle.addEventListener('click', () => {
    if (isRail()) {
      if (flyoutOpen() && pinned) closeFlyout();
      else openFlyout(true);
      return;
    }
    const open = !folderOpen();
    folder.dataset.state = open ? 'open' : 'closed';
    savePeopleOpen(open);
    sync();
  });

  // Hover and focus open the flyout; leaving closes it unless it was pinned by a click.
  folder.addEventListener('mouseenter', () => { if (isRail()) openFlyout(false); });
  folder.addEventListener('mouseleave', () => {
    if (!isRail() || pinned) return;
    hideTimer = window.setTimeout(() => closeFlyout(), 150);
  });
  toggle.addEventListener('focus', () => { if (!restoring && isRail() && toggle.matches(':focus-visible')) openFlyout(true); });
  folder.addEventListener('focusout', (e) => {
    if (!isRail() || !flyoutOpen()) return;
    if (!folder.contains(e.relatedTarget as Node | null)) closeFlyout();
  });
  document.addEventListener('pointerdown', (e) => {
    if (flyoutOpen() && !folder.contains(e.target as Node)) closeFlyout();
  });

  folder.addEventListener('keydown', (e) => {
    if (!isRail()) return;
    const items = links();
    const i = items.indexOf(document.activeElement as HTMLAnchorElement);
    if (e.key === 'Escape' && flyoutOpen()) {
      e.preventDefault();
      e.stopPropagation();
      closeFlyout(true);
    } else if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      if (!flyoutOpen()) openFlyout(true);
      const d = e.key === 'ArrowDown' ? 1 : -1;
      const n = i < 0 ? (d > 0 ? 0 : items.length - 1) : (i + d + items.length) % items.length;
      items[n]?.focus();
    } else if (e.key === 'Home' || e.key === 'End') {
      if (i < 0) return;
      e.preventDefault();
      items[e.key === 'Home' ? 0 : items.length - 1]?.focus();
    }
  });

  // A width change can flip rail <-> wide: drop any open flyout and re-read the state.
  const reset = () => { if (flyoutOpen()) closeFlyout(); else sync(); };
  window.addEventListener('cseed-sidebar', reset);
  matchMedia('(max-width: 1099px)').addEventListener('change', reset);
  matchMedia('(max-width: 767px)').addEventListener('change', reset);
  addEventListener('resize', () => { if (flyoutOpen()) place(); });
  sync();
}
