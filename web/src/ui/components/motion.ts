import { useLayoutEffect, useRef } from 'preact/hooks';
import type { RefObject } from 'preact';

/**
 * Shared list/view motion on the V0.5 tokens (docs/motion-performance-v05.md).
 * Transform and opacity only; prefers-reduced-motion disables both helpers.
 */
const reduced = () => typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches;
/** Above this many keyed items FLIP is skipped: measuring would cost more than it adds. */
const FLIP_LIMIT = 60;
const FLIP_MS = 180;

/**
 * FLIP: items marked `data-flip="<stable key>"` inside `root` glide from where they were
 * on the previous render to where they are now (filter reflow, board stage moves). Rects
 * are read once per commit relative to `root`, so page scroll does not count as movement.
 * New items keep the CSS stagger enter; removed items simply leave. Pass a new `scope`
 * when the whole view is replaced (tab/layout switch) so nothing glides across views.
 */
export function useFlip(root: RefObject<HTMLElement | null>, scope: unknown) {
  const rects = useRef(new Map<string, { x: number; y: number }>());
  const lastScope = useRef(scope);
  useLayoutEffect(() => {
    const box = root.current;
    if (!box) return;
    const items = box.querySelectorAll<HTMLElement>('[data-flip]');
    if (items.length > FLIP_LIMIT) { rects.current.clear(); return; }
    const origin = box.getBoundingClientRect();
    const sameScope = lastScope.current === scope;
    lastScope.current = scope;
    const animate = sameScope && !reduced();
    const next = new Map<string, { x: number; y: number }>();
    const moves: [HTMLElement, number, number][] = [];
    items.forEach((el) => {
      const r = el.getBoundingClientRect();
      const at = { x: r.left - origin.left, y: r.top - origin.top };
      const old = rects.current.get(el.dataset.flip!);
      if (animate && old && (Math.abs(old.x - at.x) > 1 || Math.abs(old.y - at.y) > 1)) moves.push([el, old.x - at.x, old.y - at.y]);
      next.set(el.dataset.flip!, at);
    });
    rects.current = next;
    // Writes after all reads: one layout pass per commit.
    for (const [el, dx, dy] of moves) {
      el.animate([{ transform: `translate(${dx}px, ${dy}px)` }, { transform: 'none' }], { duration: FLIP_MS, easing: 'cubic-bezier(.2, .8, .2, 1)' });
    }
  });
}

/**
 * Crossfade between whole views. When `key` changes, the outgoing view is cloned (inert,
 * aria-hidden, ids stripped) into `host` and fades out as the shared `.async-outgoing`
 * overlay while the incoming view, which owns layout, enters with the usual stagger.
 * Call during render: the previous DOM is still in place until Preact commits.
 */
export function useCrossfade(content: RefObject<HTMLElement | null>, host: RefObject<HTMLElement | null>, key: string) {
  const last = useRef(key);
  const outgoing = useRef<HTMLElement | null>(null);
  if (last.current !== key) {
    last.current = key;
    const old = content.current;
    if (old && !reduced()) {
      const clone = old.cloneNode(true) as HTMLElement;
      clone.querySelectorAll('[id]').forEach((el) => el.removeAttribute('id'));
      clone.removeAttribute('id');
      outgoing.current = clone;
    }
  }
  useLayoutEffect(() => {
    const clone = outgoing.current, at = host.current;
    outgoing.current = null;
    if (!clone || !at) return;
    const overlay = document.createElement('div');
    overlay.className = 'async-outgoing';
    overlay.setAttribute('aria-hidden', 'true');
    overlay.inert = true;
    overlay.append(clone);
    at.replaceChildren(overlay);
    const timer = setTimeout(() => overlay.remove(), 180);
    return () => { clearTimeout(timer); overlay.remove(); };
  }, [key]);
}
