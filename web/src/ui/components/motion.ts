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
export function useFlip(root: RefObject<HTMLElement | null>, scope: unknown, options: { skip?: (id: string) => boolean; revision?: unknown; size?: boolean } = {}) {
  type Rect = { x: number; y: number; width: number; height: number };
  const rects = useRef(new Map<string, Rect>());
  const animations = useRef(new Map<HTMLElement, Animation>());
  const lastScope = useRef(scope);
  const lastRevision = useRef<unknown>(Symbol());
  useLayoutEffect(() => {
    const box = root.current;
    if (!box || (options.revision !== undefined && lastRevision.current === options.revision && lastScope.current === scope)) return;
    lastRevision.current = options.revision;
    // Never measure an intermediate transform from our previous animation.
    animations.current.forEach(animation => animation.cancel());
    animations.current.clear();
    const items = box.querySelectorAll<HTMLElement>('[data-flip]');
    if (items.length > FLIP_LIMIT) { rects.current.clear(); return; }
    const origin = box.getBoundingClientRect();
    const animate = lastScope.current === scope && !reduced();
    lastScope.current = scope;
    const next = new Map<string, Rect>();
    const moves: [HTMLElement, number, number, number, number][] = [];
    items.forEach(el => {
      const r = el.getBoundingClientRect();
      const at = { x: r.left - origin.left, y: r.top - origin.top, width: r.width, height: r.height };
      const id = el.dataset.flip!;
      const old = rects.current.get(id);
      if (animate && old && !options.skip?.(id)) {
        const dx = old.x - at.x, dy = old.y - at.y;
        const sx = options.size && at.width ? old.width / at.width : 1;
        const sy = options.size && at.height ? old.height / at.height : 1;
        if (Math.abs(dx) > 1 || Math.abs(dy) > 1 || Math.abs(sx - 1) > .001 || Math.abs(sy - 1) > .001) moves.push([el, dx, dy, sx, sy]);
      }
      next.set(id, at);
    });
    rects.current = next;
    const easing = getComputedStyle(box).getPropertyValue('--ease-out').trim() || 'cubic-bezier(.2, .8, .2, 1)';
    for (const [el, dx, dy, sx, sy] of moves) {
      const animation = el.animate([
        { transformOrigin: 'top left', transform: `translate(${dx}px, ${dy}px) scale(${sx}, ${sy})` },
        { transformOrigin: 'top left', transform: 'none' },
      ], { duration: options.size ? 200 : FLIP_MS, easing });
      animations.current.set(el, animation);
    }
  });
  useLayoutEffect(() => () => {
    animations.current.forEach(animation => animation.cancel());
    animations.current.clear();
  }, []);
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
