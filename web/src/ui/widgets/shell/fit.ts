import { useLayoutEffect, useRef, useState } from 'preact/hooks';

/**
 * The content box of an element, kept current with a ResizeObserver (never measured per render).
 * The first measurement happens in a layout effect, so the fitted content paints once.
 */
export function useBox<T extends HTMLElement = HTMLDivElement>() {
  const ref = useRef<T>(null);
  const [box, setBox] = useState({ width: 0, height: 0 });
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const set = (width: number, height: number) => {
      width = Math.round(width); height = Math.round(height);
      setBox((b) => (b.width === width && b.height === height ? b : { width, height }));
    };
    set(el.clientWidth, el.clientHeight);
    if (typeof ResizeObserver === 'undefined') return;
    const observer = new ResizeObserver(([entry]) => entry && set(entry.contentRect.width, entry.contentRect.height));
    observer.observe(el);
    return () => observer.disconnect();
  }, []);
  return [ref, box] as const;
}

/**
 * How many fixed-height rows fit in `height`, leaving room for a "+n more" line when not all of
 * `total` fit. `fallback` is used before the first measurement (and in no-layout environments).
 */
export function fitRows({ height, total, row, gap = 2, more = 22, columns = 1, fallback }: {
  height: number; total: number; row: number; gap?: number; more?: number; columns?: number; fallback: number;
}): number {
  if (!height) return Math.min(total, fallback);
  const per = (h: number) => Math.max(0, Math.floor((h + gap) / (row + gap)));
  if (per(height) * columns >= total) return total;
  return Math.min(total, Math.max(1, per(height - more) * columns));
}
