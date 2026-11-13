import { useLayoutEffect, useRef } from 'preact/hooks';

/**
 * The app shell's logo: the same `cseed-wordmark` images and `.wordmark-logo` classes as
 * `AppLayout.astro` (ink on light, white on dark, swapped by the theme rules in
 * components.css). `ShellLogo` is the onboarding page's copy of it, and the intro's snake lands
 * on it (`data-onb-logo`). The intro overlay renders the same markup via `LogoImages`.
 *
 * Where it goes is measured, not hard-coded (V0.10, QA Q-1): the ghost is an invisible copy of
 * the shell's top bar and sidebar header with the same classes, laid out by the same CSS (the
 * breakpoints, the saved sidebar width, rail or wide). The visible logo copies whichever logo
 * the ghost shows (the wordmark, or the C mark on the rail) and its box. So the hand-off lines up
 * with the real shell at every width, even when the shell's CSS changes.
 */
export function LogoImages() {
  return (
    <>
      <img class="wordmark-logo wordmark-logo--ink" src="/images/cseed-wordmark.png" alt="" width="78" height="18" />
      <img class="wordmark-logo wordmark-logo--white" src="/images/cseed-wordmark-white.png" alt="" width="78" height="18" />
      {/* The rail (64px sidebar, the tablet default) shows only its C mark instead of the wordmark. */}
      <span class="wordmark-mark" aria-hidden="true">
        C
      </span>
    </>
  );
}

/**
 * Mirrors the shell's logo markup in `AppLayout.astro` (top bar for phones, sidebar header
 * otherwise). It is appended straight to <body>, outside the onboarding page, so no onboarding
 * style can change how it lays out; it holds no focusable element and is hidden from assistive tech.
 */
const WORDMARK_IMGS =
  '<img class="wordmark-logo wordmark-logo--ink" src="/images/cseed-wordmark.png" alt="" width="78" height="18">' +
  '<img class="wordmark-logo wordmark-logo--white" src="/images/cseed-wordmark-white.png" alt="" width="78" height="18">';
const GHOST_HTML =
  `<div class="topbar"><span class="icon-btn"></span><span class="wordmark wordmark--top">${WORDMARK_IMGS}</span><span class="icon-btn"></span></div>` +
  `<div class="sidebar"><span class="icon-btn sheet-close"></span><span class="wordmark"><span class="wordmark-mark">C</span>` +
  `<span class="wordmark-text">${WORDMARK_IMGS}<span class="wordmark-sub">officers</span></span></span></div>`;

function mountGhost(): HTMLDivElement {
  const g = document.createElement('div');
  g.className = 'app onb-ghost';
  g.setAttribute('aria-hidden', 'true');
  g.setAttribute('data-onb-ghost', '');
  g.innerHTML = GHOST_HTML;
  document.body.appendChild(g);
  return g;
}

export interface LogoBox {
  x: number;
  y: number;
  w: number;
  h: number;
  kind: 'wordmark' | 'mark';
}

/** The shell logo the ghost is showing right now (the one that is displayed, visible and on screen). */
export function measureGhost(ghost: HTMLElement): LogoBox | null {
  const shown = (el: Element) => {
    const r = el.getBoundingClientRect();
    const visible = typeof (el as HTMLElement).checkVisibility === 'function' ? (el as HTMLElement).checkVisibility({ visibilityProperty: true }) : true;
    return visible && r.width > 0 && r.height > 0 && r.right > 0 && r.left < innerWidth && r.bottom > 0 && r.top < innerHeight;
  };
  const el = Array.from(ghost.querySelectorAll('.wordmark-logo, .wordmark-mark')).find(shown);
  if (!el) return null;
  const r = el.getBoundingClientRect();
  return { x: r.left, y: r.top, w: r.width, h: r.height, kind: el.classList.contains('wordmark-mark') ? 'mark' : 'wordmark' };
}

export function ShellLogo() {
  const logo = useRef<HTMLSpanElement>(null);
  useLayoutEffect(() => {
    const g = mountGhost();
    const place = () => {
      const el = logo.current;
      if (!el) return;
      const b = measureGhost(g);
      if (!b) return;
      el.dataset.kind = b.kind;
      el.style.left = `${b.x}px`;
      el.style.top = `${b.y}px`;
      el.style.height = `${b.h}px`;
    };
    place();
    // Images or the font may settle the layout a moment later; the width can change any time.
    const ro = typeof ResizeObserver === 'function' ? new ResizeObserver(place) : null;
    if (ro) g.querySelectorAll('.wordmark, .wordmark-logo, .wordmark-mark').forEach((n) => ro.observe(n));
    addEventListener('resize', place);
    return () => {
      ro?.disconnect();
      removeEventListener('resize', place);
      g.remove();
    };
  }, []);
  return (
    <span class="onb-shell-logo" ref={logo} data-onb-logo role="img" aria-label="CSEED">
      <LogoImages />
    </span>
  );
}
