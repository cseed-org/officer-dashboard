import { useLayoutEffect } from 'preact/hooks';
import { createPortal } from 'preact/compat';
import { WelcomeIntro, type HelloMedia } from './WelcomeIntro';
import type { Target } from './introPlan';

/** The app shell's visible logo: the phone top bar's or the sidebar's wordmark, or the rail's sapling mark. */
export function shellLogoTarget(): Target | null {
  const els = document.querySelectorAll<HTMLElement>('.topbar .wordmark-logo, .sidebar .wordmark-logo, .sidebar .wordmark-mark');
  for (const el of Array.from(els)) {
    const r = el.getBoundingClientRect();
    if (r.width > 0 && r.height > 0 && r.right > 0 && r.left < innerWidth && r.bottom > 0 && r.top < innerHeight) {
      return { x: r.left, y: r.top, w: r.width, h: r.height, kind: el.classList.contains('wordmark-mark') ? 'mark' : 'wordmark' };
    }
  }
  return null;
}

/**
 * Settings › Advanced › Replay welcome intro (D-22). The full V0.12 hello screen over the page you
 * are on, landing on the real shell logo, then you are back where you were. It only plays: nothing
 * is saved and no account or onboarding data is read or changed. While it plays the shell's own logo
 * is hidden (intro.css, `html.wi-replay`), so the two are never drawn at once.
 */
export function ReplayIntro({ media, onClose }: { media?: HelloMedia; onClose: () => void }) {
  useLayoutEffect(() => {
    const html = document.documentElement;
    html.classList.add('wi-replay');
    return () => html.classList.remove('wi-replay');
  }, []);
  return createPortal(<WelcomeIntro onDone={onClose} media={media} target={shellLogoTarget} />, document.body);
}
