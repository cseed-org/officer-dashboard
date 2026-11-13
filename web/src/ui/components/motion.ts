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

type Box = { x: number; y: number; width: number; height: number };
/** The p in [0, 1] where c0 + c1·p > eps, as an interval (empty: lo >= hi). */
function above(c0: number, c1: number, eps: number): [number, number] {
  if (Math.abs(c1) < 1e-9) return c0 > eps ? [0, 1] : [1, 1];
  const edge = (eps - c0) / c1;
  return c1 > 0 ? [Math.max(0, edge), 1] : [0, Math.min(1, edge)];
}
/** Do two boxes that each glide linearly with the same progress p overlap for some p? (1px tolerance.) */
function collide(a0: Box, a1: Box, b0: Box, b1: Box): boolean {
  const eps = 1;
  const axis = (p: 'x' | 'y', s: 'width' | 'height'): [number, number] => {
    // Overlap on an axis needs a.end > b.start and b.end > a.start, both linear in p.
    const aStart0 = a0[p], aStart1 = a1[p], bStart0 = b0[p], bStart1 = b1[p];
    const aEnd0 = a0[p] + a0[s], aEnd1 = a1[p] + a1[s], bEnd0 = b0[p] + b0[s], bEnd1 = b1[p] + b1[s];
    const one = above(aEnd0 - bStart0, (aEnd1 - bStart1) - (aEnd0 - bStart0), eps);
    const two = above(bEnd0 - aStart0, (bEnd1 - aStart1) - (bEnd0 - aStart0), eps);
    return [Math.max(one[0], two[0]), Math.min(one[1], two[1])];
  };
  const x = axis('x', 'width'), y = axis('y', 'height');
  return Math.max(x[0], y[0]) < Math.min(x[1], y[1]);
}
/**
 * FLIP glides every item along a straight line, so two items can cross each other between two
 * collision-free layouts. Any glide that would overlap another item (gliding or still) at some
 * point is dropped: that item simply lands, and the rest are re-checked against its final box.
 * `float` items (a card riding the pointer) are not part of the layout and are ignored.
 */
function dropCollidingMoves(moves: [HTMLElement, number, number, number, number][], now: Map<string, Box>, float?: (id: string) => boolean) {
  const from = (el: HTMLElement, dx: number, dy: number, sx: number, sy: number): Box => {
    const at = now.get(el.dataset.flip!)!;
    return { x: at.x + dx, y: at.y + dy, width: at.width * sx, height: at.height * sy };
  };
  for (let again = true; again;) {
    again = false;
    const gliding = new Map(moves.map(m => [m[0].dataset.flip!, from(...m)]));
    for (let i = moves.length - 1; i >= 0; i--) {
      const id = moves[i]![0].dataset.flip!;
      const start = gliding.get(id)!, end = now.get(id)!;
      const hit = [...now].some(([other, box]) => {
        if (other === id || float?.(other)) return false;
        const otherStart = gliding.get(other) ?? box;
        return collide(start, end, otherStart, box);
      });
      if (hit) { moves.splice(i, 1); again = true; break; }
    }
  }
}

/**
 * FLIP: items marked `data-flip="<stable key>"` inside `root` glide from where they were
 * on the previous render to where they are now (filter reflow, board stage moves). Rects
 * are read once per commit relative to `root`, so page scroll does not count as movement.
 * New items keep the CSS stagger enter; removed items simply leave. Pass a new `scope`
 * when the whole view is replaced (tab/layout switch) so nothing glides across views.
 */
export function useFlip(root: RefObject<HTMLElement | null>, scope: unknown, options: { skip?: (id: string) => boolean; revision?: unknown; size?: boolean; collisionFree?: boolean; float?: (id: string) => boolean } = {}) {
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
    if (options.collisionFree) dropCollidingMoves(moves, next, options.float);
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
