import { useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks';
import { ACCESSIBLE_GREETING, GREETINGS, type Greeting } from './greetings';
import { LogoImages } from './LogoMark';
import { buildPlan, retargetPlan, sampleAt, skipPlan, type Box, type Input, type Plan, type Sample, type Target } from './introPlan';
import { startKey, type EarlyInput } from './introEarly';
import './intro.css';

/**
 * The hello screen's background video, found at build time in `public/media/` (helloMedia.ts,
 * docs/frontend.md). Without it the stand-in gradient plays in the same layer.
 */
export interface HelloMedia {
  /** `/media/hello-loop.mp4`: one short, silent, landscape loop, cropped to fill (object-fit: cover). */
  video?: string;
  /** Its first frame as a still, shown until the video plays (and instead of it with reduced motion). */
  poster?: string;
}
const NO_MEDIA: HelloMedia = {};

/** How long each language stays, and its fades; with reduced motion, a slow cross-fade instead. */
const CYCLE_MS = 2100;
const FADE_IN = 560;
const FADE_OUT = 380;
const REDUCED_CYCLE_MS = 4200;
const REDUCED_FADE = 900;
const EASE = 'cubic-bezier(.2, .8, .2, 1)';
/**
 * The melt: the sentence (all but cseed) splits into a few wide drips, each sagging and stretching a
 * little later than its neighbours, so it reads as a liquid curtain rather than broken strips.
 * `DRIP_AT` is when each starts (a share of the melt), `DRIP_DROP` how far it falls (a share of the band).
 */
const DRIP_AT = [0.12, 0.02, 0.2, 0.06, 0.24];
const DRIP_DROP = [0.44, 0.52, 0.38, 0.5, 0.42];
const DRIPS = DRIP_AT.length;
const DRIP_SPREAD = 0.3;
/** The share of the melt over which the glass (with its depth) hands over to the drips. */
const DRIP_HANDOVER = 0.22;
/** A click that follows a handled pointerup within this many ms is the same press. */
const SAME_PRESS_MS = 700;
/**
 * After the press that starts it, presses within this many ms are the same gesture (a double-click,
 * a bounce, a tap and a click) and are ignored (R-1). Only a clearly separate press fast-forwards.
 */
const SECOND_PRESS_MS = 650;

const reducedMotion = () => typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches;

function Words({ g, cls }: { g: Greeting; cls: string }) {
  const [before = '', after = ''] = g.text.split('{cseed}');
  return (
    <span class={cls}>
      {before}
      <span class="wi-cs" lang="en" dir="ltr">
        cseed
      </span>
      {after}
    </span>
  );
}

/**
 * One language in liquid glass: the glass face (a translucent fill that carries the backdrop's colours,
 * bent as through a lens, with a lit rim, an inner bevel and a cast shadow), and over it a band of light
 * that glides across the letters. The light is a window that moves while the copy of the words inside
 * it moves back by the same amount, so the words stay put and only the light travels, on the compositor.
 */
function Phrase({ g, front }: { g: Greeting; front: boolean }) {
  return (
    <p class="wi-slot" lang={g.lang} dir={g.dir} data-lang={g.lang} data-front={front ? '' : undefined} data-wi-phrase>
      <span class="wi-text">
        <span class="wi-face">
          <Words g={g} cls="wi-glass" />
        </span>
        <span class="wi-sheen" aria-hidden="true">
          <span class="wi-sheen-in">
            <Words g={g} cls="wi-shine" />
          </span>
        </span>
      </span>
    </p>
  );
}

interface Props {
  /** Called once the intro has finished: its logo stands exactly on the page's logo, which takes over in the same frame. */
  onDone: () => void;
  /** CSS selector of the page's logo, the destination (it may carry `data-kind="mark"`: the rail's sapling). */
  targetSelector?: string;
  media?: HelloMedia;
  /** The page decided there is no intro after all (a resumed setup): fade out, or finish at once if it is playing. */
  leaving?: boolean;
  /** Shown after the account loaded rather than from the first paint (R-4): it fades in. */
  late?: boolean;
  /** Measures the destination itself instead of `targetSelector` (the Settings replay lands on the real shell logo). */
  target?: () => Target | null;
}

type Phase = 'greeting' | 'playing' | 'done';
type Slots = { a: number; b: number; front: 0 | 1; n: number };

/** Style writes that skip values already set (B-23): most elements change on only a few frames. */
function makeWriter() {
  const seen = new WeakMap<HTMLElement, Record<string, string>>();
  return (el: HTMLElement, prop: string, value: string) => {
    let m = seen.get(el);
    if (!m) seen.set(el, (m = {}));
    if (m[prop] === value) return;
    m[prop] = value;
    el.style.setProperty(prop, value);
  };
}
const px = (n: number) => `${Math.round(n * 100) / 100}px`;
const num = (n: number) => String(Math.round(n * 1000) / 1000);
const clamp01 = (n: number) => Math.max(0, Math.min(1, n));

/**
 * The first screen of onboarding (V0.12, revision 2). "Hello, welcome to cseed" in liquid glass over a
 * looping video (or a calm stand-in), cycling through 15 languages, with nothing else on screen. A press
 * anywhere (a click, a tap, a swipe or a key; Escape skips) melts the greeting away and the timeline
 * (`introPlan.ts`) takes over. The screen draws only what `sampleAt(plan, ms)` returns, so playback, a
 * second press (fast-forward), Escape and a resize all go through the same sampler.
 *
 * Interim (D-29): the snake is gone and the glass word "cseed" is not drawn yet. The sentence melts,
 * the backdrop fades and the real logo fades in at its box. Lifting and gliding the word is Codex's to wire.
 */
export function WelcomeIntro({ onDone, targetSelector = '[data-onb-logo]', media = NO_MEDIA, leaving = false, late = false, target: findTarget }: Props) {
  const root = useRef<HTMLDivElement>(null);
  const greet = useRef<HTMLDivElement>(null);
  const backdrop = useRef<HTMLDivElement>(null);
  const paper = useRef<HTMLDivElement>(null);
  const video = useRef<HTMLVideoElement>(null);
  const logo = useRef<HTMLDivElement>(null);

  const [slots, setSlots] = useState<Slots>({ a: 0, b: 1, front: 0, n: 0 });
  const [phase, setPhase] = useState<Phase>('greeting');
  const [videoOn, setVideoOn] = useState(false);
  const phaseRef = useRef<Phase>('greeting');
  const reduced = useRef(reducedMotion());
  const doneRef = useRef(onDone);
  doneRef.current = onDone;
  /** The running playback: its plan, its clock origin and the pieces it draws. */
  const run = useRef<{ plan: Plan; t0: number; retargeted: boolean; raf: number; draw: (s: Sample) => void; place: (t: Target) => void } | null>(null);
  /** When the press that started it came (R-1: a second press must be clearly separate). */
  const startedAt = useRef(0);
  const lastPointer = useRef(0);
  const cycleTimer = useRef(0);

  /* ── Greeting cycle: two slots take turns; the next language is rendered into the hidden one first. ── */
  useEffect(() => {
    const step = () => {
      if (phaseRef.current !== 'greeting') return;
      setSlots((s) => {
        const shown = s.front === 0 ? s.a : s.b;
        const next = (shown + 1) % GREETINGS.length;
        return s.front === 0 ? { a: s.a, b: next, front: 1, n: s.n + 1 } : { a: next, b: s.b, front: 0, n: s.n + 1 };
      });
      cycleTimer.current = window.setTimeout(step, reduced.current ? REDUCED_CYCLE_MS : CYCLE_MS);
    };
    cycleTimer.current = window.setTimeout(step, reduced.current ? REDUCED_CYCLE_MS : CYCLE_MS);
    return () => clearTimeout(cycleTimer.current);
  }, []);
  useLayoutEffect(() => {
    if (slots.n === 0 || phaseRef.current !== 'greeting') return;
    const els = greet.current?.querySelectorAll<HTMLElement>(':scope > .wi-slot');
    const incoming = els?.[slots.front];
    const outgoing = els?.[1 - slots.front];
    if (!incoming || !outgoing) return;
    if (reduced.current) {
      incoming.animate([{ opacity: 0 }, { opacity: 1 }], { duration: REDUCED_FADE, easing: 'ease-in-out' });
      outgoing.animate([{ opacity: 1 }, { opacity: 0 }], { duration: REDUCED_FADE, easing: 'ease-in-out' });
      return;
    }
    incoming.animate([{ opacity: 0, transform: 'translateY(14px) scale(.98)' }, { opacity: 1, transform: 'none' }], { duration: FADE_IN, easing: EASE });
    outgoing.animate([{ opacity: 1, transform: 'none' }, { opacity: 0, transform: 'translateY(-10px) scale(.98)' }], { duration: FADE_OUT, easing: 'ease-in' });
  }, [slots.n]);

  /* ── Video: added after hydration only when it should play (not with reduced motion or Save-Data), and
     shown only once it is actually playing, over the poster or the stand-in, so there is never a black frame. ── */
  useEffect(() => {
    const saveData = (navigator as Navigator & { connection?: { saveData?: boolean } }).connection?.saveData;
    if (media.video && !reduced.current && !saveData) setVideoOn(true);
  }, []);
  useEffect(() => {
    const v = video.current;
    if (!videoOn || !v) return;
    const show = () => v.animate([{ opacity: 0 }, { opacity: 1 }], { duration: 400, easing: 'ease-out', fill: 'forwards' });
    if (!v.paused && v.readyState >= 3) show();
    else v.addEventListener('playing', show, { once: true });
    // Autoplay can be refused (iOS Low Power Mode): the poster or the stand-in simply stays.
    v.play().catch(() => {});
    return () => v.removeEventListener('playing', show);
  }, [videoOn]);

  /** The page logo's box, or null when it isn't on the page (then the intro fades away without landing, R-7). */
  const measureTarget = (): Target | null => {
    if (findTarget) return findTarget();
    const host = document.querySelector<HTMLElement>(targetSelector);
    // The image (or the rail's mark) the page shows, not its wrapper: the box the timeline lands on.
    const el = host && (Array.from(host.querySelectorAll<HTMLElement>('.wordmark-logo, .wordmark-mark')).find((e) => e.getBoundingClientRect().width > 0) ?? host);
    const r = el?.getBoundingClientRect();
    if (!host || !el || !r || r.width <= 0 || r.height <= 0) return null;
    return { x: r.left, y: r.top, w: r.width, h: r.height, kind: host.dataset.kind === 'mark' || el.classList.contains('wordmark-mark') ? 'mark' : 'wordmark' };
  };
  const NOWHERE: Target = { x: 0, y: 0, w: 1, h: 1, kind: 'wordmark' };
  const input = (wordBox: Box, target: Target): Input => {
    const el = root.current!;
    return { viewport: { w: el.clientWidth, h: el.clientHeight, dpr: window.devicePixelRatio || 1 }, word: wordBox, target, motion: reduced.current ? 'reduced' : 'full' };
  };

  const finish = () => {
    const r = run.current;
    if (r) cancelAnimationFrame(r.raf);
    if (phaseRef.current === 'done') return;
    phaseRef.current = 'done';
    setPhase('done');
    doneRef.current();
  };

  /** Builds the drawing for one press: the greeting is split into drips, then everything is only moved and faded. */
  const setup = (landing: boolean) => {
    const write = makeWriter();
    const g = greet.current!;
    const gr = g.getBoundingClientRect();
    // A press can come in the middle of a language change (R-3): freeze both phrases where they are
    // (their current opacity and position become inline styles) and let both melt from there.
    for (const a of g.getAnimations({ subtree: true })) {
      if (a.effect instanceof KeyframeEffect && a.effect.target instanceof HTMLElement && a.effect.target.matches('.wi-slot')) {
        try {
          a.commitStyles();
        } catch {
          /* not rendered: nothing to keep */
        }
      }
      a.cancel();
    }
    const phrases = Array.from(g.querySelectorAll<HTMLElement>(':scope > .wi-slot'))
      .map((el) => ({ el, base: +getComputedStyle(el).opacity }))
      .filter((x) => x.base > 0.01);
    // The phrase the word comes from is the more visible one; the other (mid-change) just fades from where it is.
    const main = phrases.reduce<(typeof phrases)[number] | null>((a, b) => (!a || b.base > a.base ? b : a), null);
    // The plan's word box: the glass "cseed" span of the phrase at the click (interim: its line box).
    const cs = main?.el.querySelector<HTMLElement>('.wi-face .wi-cs')?.getBoundingClientRect();
    const box: Box = cs && cs.width > 0 ? { x: cs.left, y: cs.top, w: cs.width, h: cs.height } : { x: gr.left + gr.width / 2 - 1, y: gr.top + gr.height / 2 - 1, w: 2, h: 2 };

    const drips: { el: HTMLElement; at: number; drop: number }[] = [];
    if (!reduced.current && main) {
      const w = g.clientWidth;
      const step = w / DRIPS;
      for (let i = 0; i < DRIPS; i++) {
        const d = document.createElement('div');
        d.className = 'wi-drip';
        d.style.left = `${(i * step).toFixed(2)}px`;
        d.style.width = `${(step + 0.5).toFixed(2)}px`;
        d.style.opacity = '0';
        const clone = main.el.cloneNode(true) as HTMLElement;
        clone.removeAttribute('data-wi-phrase');
        clone.removeAttribute('data-front');
        clone.style.left = `${(-i * step).toFixed(2)}px`;
        clone.style.width = `${w}px`;
        d.appendChild(clone);
        g.appendChild(d);
        drips.push({ el: d, at: DRIP_AT[i]! * (DRIP_SPREAD / 0.24), drop: DRIP_DROP[i]! });
      }
    }
    const greetH = g.clientHeight;

    // The landing logo: the page logo's own images in its exact box, faded in, never scaled.
    const place = (t: Target) => {
      const lg = logo.current!;
      lg.style.left = px(t.x);
      lg.style.top = px(t.y);
      lg.style.height = px(t.h);
      lg.dataset.kind = t.kind;
    };
    let videoPaused = false;

    const draw = (s: Sample) => {
      // Backdrop → the page's paper, by the end of the glide.
      write(backdrop.current!, 'opacity', num(s.backdropOpacity));
      if (s.backdropOpacity === 0 && !videoPaused) {
        videoPaused = true;
        video.current?.pause();
        backdrop.current!.setAttribute('data-still', '');
        write(backdrop.current!, 'visibility', 'hidden');
      }
      // The paper gives way to the page as the logo lands (the page around it shows in the same frames).
      write(paper.current!, 'opacity', num(1 - clamp01(s.logoOpacity)));

      // The sentence: the glass hands over to the drips, which sag, stretch and fade, each a little later.
      const p = 1 - s.sentenceOpacity;
      if (drips.length) {
        const f = Math.min(1, p / DRIP_HANDOVER);
        for (const { el, base: o } of phrases) write(el, 'opacity', num(o * (1 - (el === main!.el ? f : Math.min(1, p / (DRIP_HANDOVER * 1.5))))));
        for (const d of drips) {
          const k = clamp01((p - d.at) / (1 - DRIP_SPREAD));
          const e = k * k;
          write(d.el, 'transform', `translateY(${px(e * d.drop * greetH)}) scaleY(${num(1 + 0.55 * e)})`);
          write(d.el, 'opacity', num(f * (1 - Math.min(1, k * 1.15))));
        }
      } else for (const { el, base: o } of phrases) write(el, 'opacity', num(o * s.sentenceOpacity));

      write(logo.current!, 'opacity', landing ? num(s.logoOpacity) : '0');
    };
    return { box, draw, place };
  };

  /** Starts playback from 0 ms: the full timeline, or the short one (Escape, reduced motion, no logo). */
  const start = (short: boolean) => {
    phaseRef.current = 'playing';
    setPhase('playing');
    clearTimeout(cycleTimer.current);
    startedAt.current = performance.now();
    const target = measureTarget();
    const landing = !!target;
    const { box, draw, place } = setup(landing);
    const inp = input(box, target ?? NOWHERE);
    const plan = short || !target ? skipPlan(inp) : buildPlan(inp);
    place(plan.input.target);
    const r = { plan, t0: performance.now(), retargeted: !landing, raf: 0, draw, place };
    run.current = r;
    const frame = (now: number) => {
      if (run.current !== r) return;
      const ms = now - r.t0;
      // The page logo is measured again when the word starts to glide (D-15).
      if (!r.retargeted && ms >= r.plan.timeline.glideStart) retarget(r);
      const s = sampleAt(r.plan, ms);
      r.draw(s);
      // The end state (the landing logo fully in) is painted once before the overlay goes, so the
      // page's own logo takes over from a full-strength copy in the same box.
      if (s.stage === 'done') r.raf = requestAnimationFrame(finish);
      else r.raf = requestAnimationFrame(frame);
    };
    frame(performance.now());
  };
  /** Re-measures the page logo, keeping the last box if it has gone. */
  const retarget = (r: NonNullable<typeof run.current>) => {
    r.retargeted = true;
    const t = measureTarget();
    if (!t) return;
    r.plan = retargetPlan(r.plan, t);
    r.place(t);
  };

  /** A second press: jump to the start of the swap (the swap still plays). */
  const fastForward = () => {
    const r = run.current;
    if (!r || phaseRef.current !== 'playing') return;
    if (!r.retargeted) retarget(r);
    const ff = r.plan.ffTo;
    if (performance.now() - r.t0 < ff) r.t0 = performance.now() - ff;
  };

  const activate = () => {
    if (phaseRef.current === 'greeting') start(reduced.current);
    else if (performance.now() - startedAt.current >= SECOND_PRESS_MS) fastForward();
  };
  const skip = () => {
    if (phaseRef.current === 'greeting') start(true);
    else fastForward();
  };

  /* ── Input. Listeners go on before the first interactive paint, and a press made before hydration
     (kept by the inline script in onboarding.astro) is replayed, so no early press is lost. ── */
  useLayoutEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (phaseRef.current === 'done') return;
      if (e.key === 'Tab') {
        // Nothing inside is focusable but the dialog itself, and everything outside is inert.
        e.preventDefault();
        root.current?.focus({ preventScroll: true });
        return;
      }
      // A held key repeats: only its first keydown is a press (R-1).
      if (e.key === 'Escape') {
        e.preventDefault();
        if (!e.repeat) skip();
        return;
      }
      if (!startKey(e)) return; // F-keys, modifiers, media keys and shortcuts stay the browser's
      e.preventDefault();
      if (!e.repeat) activate();
    };
    window.addEventListener('keydown', onKey);
    window.__wiReady = true;
    const early: EarlyInput[] = window.__wiEarly ?? [];
    window.__wiEarly = [];
    if (early.some((x) => x.kind === 'key' && x.key === 'Escape')) skip();
    else if (early.length) activate();
    return () => {
      window.removeEventListener('keydown', onKey);
      window.__wiReady = false;
    };
  }, []);

  /* ── A real modal: the rest of the page is inert while it is up; focus returns afterwards. ── */
  useLayoutEffect(() => {
    const el = root.current;
    const before = document.activeElement as HTMLElement | null;
    const inerted: Element[] = [];
    for (let node: Element | null = el; node && node !== document.documentElement; node = node.parentElement) {
      for (const sib of Array.from(node.parentElement?.children ?? [])) {
        if (sib !== node && !sib.hasAttribute('inert') && sib.tagName !== 'SCRIPT') {
          sib.setAttribute('inert', '');
          inerted.push(sib);
        }
      }
    }
    el?.focus({ preventScroll: true });
    // Shown only once the account had loaded (someone else saw it in this browser, R-4): fade it in.
    if (late) el?.animate([{ opacity: 0 }, { opacity: 1 }], { duration: 280, easing: 'ease-out' });
    // With a poster, the glass bends the poster's colours (the video's first frame) instead of the stand-in's.
    if (media.poster) el?.style.setProperty('--wi-refract', `url("${media.poster}")`);
    // Start the fonts for every greeting now, while English shows, so no language swaps fonts mid-fade.
    document.fonts?.load?.('500 1em "cseed Hello"', GREETINGS.map((g) => g.text).join('')).catch(() => {});
    // The landing logo's images are decoded now, not on the frame they first show.
    el?.querySelectorAll<HTMLImageElement>('[data-wi-logo] img').forEach((i) => i.decode?.().catch(() => {}));
    // A rotation or resize mid-run: jump to the end state (done, not ffTo), onto the freshly measured logo (D-15).
    const onResize = () => {
      const r = run.current;
      if (!r || phaseRef.current !== 'playing') return;
      retarget(r);
      r.t0 = Math.min(r.t0, performance.now() - r.plan.done);
    };
    window.addEventListener('resize', onResize);
    return () => {
      window.removeEventListener('resize', onResize);
      inerted.forEach((n) => n.removeAttribute('inert'));
      if (before && before !== document.body && before.isConnected) before.focus({ preventScroll: true });
      clearTimeout(cycleTimer.current);
      if (run.current) cancelAnimationFrame(run.current.raf);
      run.current = null;
    };
  }, []);

  /* ── The page found there is no intro after all: fade out from the greeting, or finish now. ── */
  useEffect(() => {
    if (!leaving) return;
    if (phaseRef.current === 'playing') {
      fastForward();
      return;
    }
    if (phaseRef.current !== 'greeting') return;
    phaseRef.current = 'done';
    clearTimeout(cycleTimer.current);
    const a = root.current?.animate([{ opacity: 1 }, { opacity: 0 }], { duration: 280, easing: 'ease-out', fill: 'forwards' });
    if (a) a.finished.then(() => doneRef.current(), () => {});
    else doneRef.current();
  }, [leaving]);

  const front = slots.front;
  return (
    <div
      class="wi"
      ref={root}
      tabIndex={-1}
      role="dialog"
      aria-modal="true"
      aria-label="Welcome to cseed. Press any key, click or tap anywhere to begin."
      data-state={phase}
      data-video={videoOn ? 'on' : 'off'}
      onPointerUp={(e) => {
        if (e.button > 0) return;
        lastPointer.current = performance.now();
        activate();
      }}
      onClick={(e) => {
        // Assistive tech can click without pointer events; a real press has already been handled,
        // and the second click of a double-click is never a new press.
        if (e.detail > 1 || performance.now() - lastPointer.current < SAME_PRESS_MS) return;
        activate();
      }}
    >
      <div class="wi-paper" ref={paper} aria-hidden="true" />
      <div class="wi-backdrop" ref={backdrop} aria-hidden="true" data-wi-backdrop>
        <div class="wi-standin" data-wi-standin />
        {media.poster && <img class="wi-poster" src={media.poster} alt="" decoding="async" fetchpriority="high" data-wi-poster />}
        {videoOn && (
          <video
            class="wi-video"
            ref={video}
            src={media.video}
            poster={media.poster}
            muted
            autoplay
            loop
            playsInline
            preload="auto"
            disablePictureInPicture
            disableRemotePlayback
            tabIndex={-1}
            aria-hidden="true"
            data-wi-video
            onError={() => setVideoOn(false)}
          />
        )}
        <div class="wi-scrim" />
      </div>
      <p class="visually-hidden">{ACCESSIBLE_GREETING}</p>
      <div class="wi-greet" ref={greet} aria-hidden="true" data-wi-greeting>
        <Phrase key={`a${slots.a}`} g={GREETINGS[slots.a]!} front={front === 0} />
        <Phrase key={`b${slots.b}`} g={GREETINGS[slots.b]!} front={front === 1} />
      </div>
      <div class="wi-logo" ref={logo} data-wi-logo aria-hidden="true">
        <LogoImages />
      </div>
    </div>
  );
}
