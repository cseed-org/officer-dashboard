import { useEffect, useState } from 'preact/hooks';
import { api } from '../../lib/api/client';
import {
  SIDEBAR_DEFAULT, SIDEBAR_MAX, SIDEBAR_MIN, SIDEBAR_SNAP, applySidebarWidth, currentWidth, lastWideWidth, saveSidebarWidth, setResizing, snapWidth,
} from '../../lib/sidebar';

/**
 * Drag handle on the sidebar's right edge (desktop and tablet; CSS hides it on phones, where
 * the menu is a sheet). A focusable vertical separator: Left/Right resize, Shift for bigger
 * steps, Home/End jump to icons only / widest, Enter or double-click toggles icons only.
 */
export default function SidebarResizer() {
  const [w, setW] = useState(SIDEBAR_DEFAULT);
  useEffect(() => {
    const sync = () => setW(currentWidth());
    sync();
    window.addEventListener('cseed-sidebar', sync);
    matchMedia('(max-width: 1099px)').addEventListener('change', sync);
    return () => window.removeEventListener('cseed-sidebar', sync);
  }, []);

  const commit = (n: number) => {
    applySidebarWidth(n);
    setW(n);
    saveSidebarWidth(n, (v) => api('prefsPut', { body: { sidebarWidth: v } }));
  };
  const toggle = () => commit(currentWidth() <= SIDEBAR_MIN ? lastWideWidth() : SIDEBAR_MIN);

  const onPointerDown = (e: PointerEvent) => {
    if (e.button !== 0) return;
    e.preventDefault();
    const el = e.currentTarget as HTMLElement;
    el.setPointerCapture(e.pointerId);
    setResizing(true);
    let last = currentWidth();
    const move = (ev: PointerEvent) => {
      last = snapWidth(ev.clientX);
      applySidebarWidth(last);
      setW(last);
    };
    const up = () => {
      el.removeEventListener('pointermove', move);
      el.removeEventListener('pointerup', up);
      el.removeEventListener('pointercancel', up);
      setResizing(false);
      commit(last);
    };
    el.addEventListener('pointermove', move);
    el.addEventListener('pointerup', up);
    el.addEventListener('pointercancel', up);
  };
  const onKeyDown = (e: KeyboardEvent) => {
    const step = e.shiftKey ? 48 : 16;
    const w = currentWidth(); // the rendered width, never a stale copy
    let n: number | null = null;
    if (e.key === 'ArrowRight' || e.key === 'ArrowLeft') {
      const dir = e.key === 'ArrowRight' ? 1 : -1;
      n = w + dir * step;
      if (n < SIDEBAR_SNAP) n = dir > 0 ? SIDEBAR_SNAP : SIDEBAR_MIN;
      n = Math.min(SIDEBAR_MAX, n);
    } else if (e.key === 'Home') n = SIDEBAR_MIN;
    else if (e.key === 'End') n = SIDEBAR_MAX;
    else if (e.key === 'Enter') n = w <= SIDEBAR_MIN ? lastWideWidth() : SIDEBAR_MIN;
    if (n === null) return;
    e.preventDefault();
    if (n !== w) commit(n);
  };

  return (
    <div
      class="sidebar-resizer"
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize sidebar"
      aria-controls="sidebar"
      aria-valuemin={SIDEBAR_MIN}
      aria-valuemax={SIDEBAR_MAX}
      aria-valuenow={w}
      aria-valuetext={w <= SIDEBAR_MIN ? 'Icons only' : `${w} pixels`}
      tabIndex={0}
      title="Drag to resize · double-click for icons only"
      onPointerDown={onPointerDown}
      onKeyDown={onKeyDown}
      onDblClick={toggle}
    />
  );
}
