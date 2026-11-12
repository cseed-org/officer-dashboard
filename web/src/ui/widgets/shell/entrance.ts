import { useEffect, useState } from 'preact/hooks';

/** sessionStorage key: the Home entrance has played in this browser session. */
export const ENTRANCE_KEY = 'home-entered';
/** Long enough for 10 staggered tiles (400ms) + the tile fade (420ms) + chart draws (480ms). */
const ENTRANCE_MS = 1400;

const reducedMotion = () => typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches;

/** True only on the first Home visit of a browser session, with motion allowed. Marks it played. */
function firstVisit(): boolean {
  if (reducedMotion()) return false;
  try {
    if (sessionStorage.getItem(ENTRANCE_KEY)) return false;
    sessionStorage.setItem(ENTRANCE_KEY, '1');
    return true;
  } catch {
    // Storage blocked: skip the entrance rather than play it on every visit.
    return false;
  }
}

/**
 * The Home load motion (notes § Load motion): while true, the grid wrapper carries `.is-entering`
 * and the CSS in shell/widget.css staggers tiles, draws sparklines, grows bars and sweeps the
 * donut; CountUp counts. The class comes off after the entrance so later content never replays it.
 */
export function useEntrance(ready = true): boolean {
  const [entering, setEntering] = useState(firstVisit);
  // The clock starts when the widgets first render (`ready`), not while Home is still loading.
  useEffect(() => {
    if (!entering || !ready) return;
    const timer = setTimeout(() => setEntering(false), ENTRANCE_MS);
    return () => clearTimeout(timer);
  }, [ready]);
  return entering;
}
