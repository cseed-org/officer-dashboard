import { useEffect, useRef } from 'preact/hooks';

/** A static DOM snapshot: no widget mounts, fetches, duplicate IDs, or active controls. */
export function useGridDrag() {
  const floating = useRef<HTMLElement | null>(null);
  const clear = () => {
    floating.current?.remove();
    floating.current = null;
    document.documentElement.classList.remove('is-dragging');
  };
  useEffect(() => clear, []);
  return {
    clear,
    start(tile: HTMLElement) {
      clear();
      const rect = tile.getBoundingClientRect();
      const clone = tile.cloneNode(true) as HTMLElement;
      clone.removeAttribute('id');
      clone.removeAttribute('data-widget');
      clone.removeAttribute('data-flip');
      clone.querySelectorAll('[id]').forEach(el => el.removeAttribute('id'));
      clone.querySelectorAll('.blk-tools').forEach(el => el.remove());
      clone.className = 'blk-float';
      clone.setAttribute('aria-hidden', 'true');
      clone.inert = true;
      Object.assign(clone.style, { width: `${rect.width}px`, height: `${rect.height}px` });
      document.body.append(clone);
      floating.current = clone;
      document.documentElement.classList.add('is-dragging');
    },
    move(x: number, y: number) {
      if (floating.current) Object.assign(floating.current.style, { left: `${x + 14}px`, top: `${y + 14}px` });
    },
  };
}
