import { useEffect, useRef, useState } from 'preact/hooks';
import { api } from '../../lib/api/client';
import { timezoneSnapshot } from '../../lib/timezone';
import { SIDEBAR_RESIZE_TARGET, SIDEBAR_RESIZE_TEXT, TOUR_STEPS, type TourStep } from './steps';
import { TOUR_EVENT } from './startTour';
import './tour.css';

type Rect = { x: number; y: number; width: number; height: number };
const reduced = () => matchMedia('(prefers-reduced-motion: reduce)').matches;
const visible = (el: Element) => {
  const r = el.getBoundingClientRect();
  return r.width > 0 && r.height > 0 && r.right > 0 && r.left < innerWidth;
};
const resolve = (step: TourStep): HTMLElement | null => {
  for (const s of step.targets) {
    const el = Array.from(document.querySelectorAll<HTMLElement>(s)).find(visible);
    if (el) return el;
  }
  return null;
};
const ease = (t: number) => 1 - Math.pow(1 - t, 3);

/**
 * The guided tour of Home: greeting, widget grid, editing (drag, resize, add and remove), the
 * sidebar and the command palette. Opens after onboarding (`?tutorial=onboarding`, once), on
 * `startTour()` (Settings' replay) and `?tutorial=1`. Skipping or finishing marks it seen.
 * `onPreview` lets the grid show its edit controls behind the stops that need them.
 */
export default function HomeTour({ onPreview }: { onPreview: (editing: boolean, active: boolean) => void }) {
  const dialog = useRef<HTMLDialogElement>(null);
  const card = useRef<HTMLDivElement>(null);
  const heading = useRef<HTMLHeadingElement>(null);
  const previousFocus = useRef<HTMLElement | null>(null);
  const cutout = useRef<SVGRectElement | null>(null);
  const outline = useRef<SVGRectElement | null>(null);
  const shown = useRef<Rect | null>(null); // what the cutout currently draws
  const tween = useRef(0);
  const [step, setStep] = useState(0);
  const [active, setActive] = useState(false);
  const [error, setError] = useState('');
  const [hasTarget, setHasTarget] = useState(true);
  const [position, setPosition] = useState({ left: 16, top: 16 });
  const [saving, setSaving] = useState(false);
  const [resizable, setResizable] = useState(false);
  const [first, setFirst] = useState(true);
  const s = TOUR_STEPS[step]!;

  // Open: after onboarding (once), on a forced replay, from startTour().
  useEffect(() => {
    let alive = true;
    const forced = new URLSearchParams(location.search).get('tutorial') === '1';
    const prefs = timezoneSnapshot().prefs;
    (prefs ? Promise.resolve(prefs) : api('prefsGet'))
      .then((p) => alive && (forced || !p.dashboardTutorialSeen) && begin())
      .catch(() => {});
    const onStart = () => begin();
    window.addEventListener(TOUR_EVENT, onStart);
    return () => {
      alive = false;
      window.removeEventListener(TOUR_EVENT, onStart);
    };
  }, []);
  const begin = () => {
    setStep(0);
    setFirst(true);
    shown.current = null;
    setActive(true);
  };

  useEffect(() => {
    if (!active) return;
    previousFocus.current = document.activeElement as HTMLElement;
    dialog.current?.showModal();
    return () => {
      onPreview(false, false);
      previousFocus.current?.focus?.();
    };
  }, [active, onPreview]);

  useEffect(() => {
    if (!active) return;
    onPreview(!!s.edit, true);
    heading.current?.focus({ preventScroll: true });
    let frame = 0;
    let spotlight: HTMLElement | null = null;
    let scrolled = false;
    const draw = (r: Rect) => {
      for (const el of [cutout.current, outline.current]) {
        if (!el) continue;
        el.setAttribute('x', String(r.x));
        el.setAttribute('y', String(r.y));
        el.setAttribute('width', String(r.width));
        el.setAttribute('height', String(r.height));
      }
      shown.current = r;
    };
    const moveTo = (to: Rect) => {
      cancelAnimationFrame(tween.current);
      const from = shown.current;
      if (!from || reduced()) return draw(to);
      const t0 = performance.now();
      const run = (now: number) => {
        const k = Math.min(1, (now - t0) / 420);
        const e = ease(k);
        draw({ x: from.x + (to.x - from.x) * e, y: from.y + (to.y - from.y) * e, width: from.width + (to.width - from.width) * e, height: from.height + (to.height - from.height) * e });
        if (k < 1) tween.current = requestAnimationFrame(run);
      };
      tween.current = requestAnimationFrame(run);
    };
    const measure = () => {
      const el = resolve(s);
      setResizable(!!document.querySelector(SIDEBAR_RESIZE_TARGET));
      const cw = card.current?.offsetWidth ?? 380;
      const ch = card.current?.offsetHeight ?? 240;
      const narrow = innerWidth <= 640;
      if (!el) {
        setHasTarget(false);
        if (spotlight) spotlight.classList.remove('is-tour-target');
        spotlight = null;
        moveTo({ x: innerWidth / 2, y: innerHeight / 2, width: 0, height: 0 });
        setPosition({ left: Math.max(16, (innerWidth - cw) / 2), top: Math.max(16, (innerHeight - ch) / 2) });
        return;
      }
      setHasTarget(true);
      if (spotlight !== el) {
        spotlight?.classList.remove('is-tour-target');
        spotlight = el;
        el.classList.add('is-tour-target');
      }
      let rect = el.getBoundingClientRect();
      if (!scrolled) {
        scrolled = true;
        const bottomLimit = narrow ? innerHeight - ch - 28 : innerHeight - 24;
        if (rect.top < 72 || rect.bottom > bottomLimit) {
          el.scrollIntoView({ block: narrow ? 'start' : 'center', behavior: reduced() ? 'instant' : 'smooth' });
        }
      }
      const pad = 8;
      const x = Math.max(8, rect.left - pad);
      const y = Math.max(8, rect.top - pad);
      const width = Math.min(innerWidth - x - 8, rect.width + pad * 2);
      const height = Math.min(innerHeight - y - 8, rect.height + pad * 2);
      moveTo({ x, y, width, height });
      // Beside the spotlight when there is room; on phones, docked to the free edge.
      let left = x + width + 20;
      let top = y;
      if (narrow) {
        left = (innerWidth - cw) / 2;
        top = y + height / 2 < innerHeight / 2 ? innerHeight - ch - 14 : 14;
      } else if (left + cw > innerWidth - 16) {
        if (x - cw - 20 >= 16) left = x - cw - 20;
        else {
          left = Math.min(x, innerWidth - cw - 16);
          top = y + height + 20;
        }
      }
      if (top + ch > innerHeight - 16) top = y - ch - 20;
      setPosition({ left: Math.max(16, Math.min(left, innerWidth - cw - 16)), top: Math.max(14, Math.min(top, innerHeight - ch - 14)) });
    };
    const update = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(measure);
    };
    update();
    // The grid's edit controls render a beat after the preview is switched on.
    const mutations = new MutationObserver(update);
    const grid = document.querySelector('[data-tour="grid"]');
    if (grid) mutations.observe(grid, { childList: true, subtree: true });
    const observer = new ResizeObserver(update);
    if (card.current) observer.observe(card.current);
    window.addEventListener('resize', update);
    window.addEventListener('scroll', update, true);
    const settle = setTimeout(update, 450); // after a smooth scroll
    return () => {
      cancelAnimationFrame(frame);
      clearTimeout(settle);
      observer.disconnect();
      mutations.disconnect();
      spotlight?.classList.remove('is-tour-target');
      window.removeEventListener('resize', update);
      window.removeEventListener('scroll', update, true);
    };
  }, [active, step, onPreview]);
  useEffect(() => () => cancelAnimationFrame(tween.current), []);

  async function finish() {
    if (saving) return;
    setSaving(true);
    setError('');
    try {
      await api('prefsPut', { body: { dashboardTutorialSeen: true } });
      dialog.current?.close();
      setActive(false);
      const url = new URL(location.href);
      url.searchParams.delete('tutorial');
      history.replaceState(null, '', url);
    } catch (e) {
      setError(`Couldn’t save that you’ve seen the tour. ${(e as Error).message}`);
    } finally {
      setSaving(false);
    }
  }
  const go = (n: number) => {
    setFirst(false);
    setStep(n);
  };
  const last = step === TOUR_STEPS.length - 1;
  const text = s.id === 'sidebar' && resizable ? SIDEBAR_RESIZE_TEXT : s.text;

  if (!active) return null;
  return (
    <dialog
      ref={dialog}
      class="home-tour"
      aria-labelledby="home-tour-title"
      data-home-tour
      onCancel={(e) => {
        e.preventDefault();
        void finish();
      }}
      onKeyDown={(e) => {
        if (e.key === 'ArrowRight' && !last && !saving) go(step + 1);
        else if (e.key === 'ArrowLeft' && step > 0 && !saving) go(step - 1);
      }}
    >
      <svg class="home-tour-shade" aria-hidden="true" width="100%" height="100%">
        <defs>
          <mask id="home-tour-mask">
            <rect width="100%" height="100%" fill="white" />
            <rect ref={cutout} rx="12" fill="black" x="0" y="0" width="0" height="0" />
          </mask>
        </defs>
        <rect width="100%" height="100%" fill="rgb(0 0 0 / .62)" mask="url(#home-tour-mask)" />
        <rect ref={outline} rx="12" fill="none" stroke="var(--c-blue)" stroke-width="3" x="0" y="0" width="0" height="0" opacity={hasTarget ? 1 : 0} />
      </svg>
      <div ref={card} class={`home-tour-card${first ? ' is-first' : ''}`} style={{ transform: `translate(${position.left}px, ${position.top}px)` }} data-tour-step={step + 1} data-tour-id={s.id}>
        <div class="home-tour-body" key={step}>
          <p class="home-tour-count small">
            Home tour · {step + 1} of {TOUR_STEPS.length}
          </p>
          <h2 ref={heading} tabIndex={-1} id="home-tour-title">
            {s.title}
          </h2>
          <p>{text}</p>
        </div>
        <ol class="home-tour-dots" aria-hidden="true">
          {TOUR_STEPS.map((t, i) => (
            <li key={t.id} class={i < step ? 'is-done' : ''} aria-current={i === step ? 'step' : undefined} />
          ))}
        </ol>
        {error && (
          <p class="home-tour-error" role="alert">
            {error}
          </p>
        )}
        <div class="home-tour-actions">
          <button class="btn btn--text home-tour-skip" type="button" disabled={saving} onClick={() => void finish()}>
            Skip tour
          </button>
          <button class="btn" type="button" disabled={saving || step === 0} onClick={() => go(step - 1)}>
            Back
          </button>
          <button class="btn btn--primary" type="button" disabled={saving} onClick={() => (last ? void finish() : go(step + 1))}>
            {last ? 'Finish' : 'Next'}
          </button>
        </div>
      </div>
    </dialog>
  );
}
