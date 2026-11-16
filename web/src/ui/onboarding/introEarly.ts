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

/**
 * Who has seen the hello screen in this browser (their user ids). The inline script reads it before
 * the first paint: when anyone has, the server-rendered overlay is hidden (`html.wi-off`) until the
 * account is loaded, so a resumed setup or an onboarded account never gets a flash of it (R-4). A
 * different invitee on the same browser still gets the intro, faded in once their account loads.
 */
export const SEEN_KEY = 'cseed:hello-seen';
export function seenIntro(): string[] {
  try {
    const v: unknown = JSON.parse(localStorage.getItem(SEEN_KEY) ?? '[]');
    return Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : [];
  } catch {
    return [];
  }
}
export function rememberIntro(id: string) {
  try {
    localStorage.setItem(SEEN_KEY, JSON.stringify([...seenIntro().filter((x) => x !== id), id].slice(-20)));
  } catch {
    /* private mode or blocked storage: the overlay then just shows first, as before */
  }
}
/** Set before the first paint when the intro may not be wanted; the island confirms or clears it. */
export const OFF_CLASS = 'wi-off';

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
  // A Google consent return, the unavailable page, or anyone having seen the intro in this browser:
  // hide the overlay before it paints; the island shows it after all if this account still needs it.
  try {
    var s = new URLSearchParams(location.search);
    if (s.has('google') || s.has('unavailable') || JSON.parse(localStorage.getItem('${SEEN_KEY}') || '[]').length) document.documentElement.classList.add('${OFF_CLASS}');
  } catch (e) {}
  function off() { return document.documentElement.classList.contains('${OFF_CLASS}'); }
  function onIntro(t) { return !!(t && t.closest && t.closest('.wi')); }
  addEventListener('keydown', function (e) {
    if (window.__wiReady || off() || !document.querySelector('.wi')) return;
    if (e.key === 'Escape' || startKey(e)) q.push({ kind: 'key', key: e.key });
  }, true);
  addEventListener('pointerup', function (e) {
    if (window.__wiReady || off() || e.button > 0 || !onIntro(e.target)) return;
    q.push({ kind: 'pointer', x: e.clientX, y: e.clientY });
  }, true);
})();`;
