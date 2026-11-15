/**
 * The hello intro is server-rendered, so it shows from the first paint, before its script has run.
 * This inline script (onboarding.astro) keeps the first press made in that gap: a key, a click or a
 * tap anywhere on the intro. WelcomeIntro replays it as soon as it is interactive, so an early
 * press is never lost (B-9). It never calls preventDefault: it only listens.
 *
 * `startKey` is shared with WelcomeIntro so both agree on which keys start the intro: printable
 * keys, Enter, Space and the arrows. F-keys, Tab, modifiers, media keys and browser shortcuts are
 * left alone. Escape is the invisible skip.
 */
export function startKey(e: Pick<KeyboardEvent, 'key' | 'ctrlKey' | 'metaKey' | 'altKey' | 'isComposing'>): boolean {
  if (e.ctrlKey || e.metaKey || e.altKey || e.isComposing) return false;
  return e.key.length === 1 || ['Enter', 'Spacebar', 'ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight'].includes(e.key);
}

export type EarlyInput = { kind: 'key'; key: string } | { kind: 'pointer'; x: number; y: number };

declare global {
  interface Window {
    __wiEarly?: EarlyInput[];
    __wiReady?: boolean;
  }
}

export const EARLY_INPUT_SCRIPT = `(function () {
  var q = (window.__wiEarly = []);
  var startKey = ${startKey.toString()};
  function onIntro(t) { return !!(t && t.closest && t.closest('.wi')); }
  addEventListener('keydown', function (e) {
    if (window.__wiReady || !document.querySelector('.wi')) return;
    if (e.key === 'Escape' || startKey(e)) q.push({ kind: 'key', key: e.key });
  }, true);
  addEventListener('pointerup', function (e) {
    if (window.__wiReady || e.button > 0 || !onIntro(e.target)) return;
    q.push({ kind: 'pointer', x: e.clientX, y: e.clientY });
  }, true);
})();`;
