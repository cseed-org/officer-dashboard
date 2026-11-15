import { useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks';
import { ACCESSIBLE_GREETING, GREETINGS, type Greeting } from './greetings';
import { saplingSvg } from '../brand/saplingMarkup';
import { LogoImages } from './LogoMark';
import { buildPlan, retargetPlan, sampleAt, skipPlan, type Input, type Plan, type Sample, type Target, type Transform } from './introPlan';
import { FRUIT_ART } from './fruitArt';
import { startKey, type EarlyInput } from './introEarly';
import './intro.css';

/**
 * The hello screen's background video, found at build time in `public/media/` (onboarding.astro,
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
/** The melt: how many drips the phrase splits into, and the share of the melt their starts spread over. */
const DRIPS = 14;
const DRIP_SPREAD = 0.4;
/** A click that follows a handled pointerup within this many ms is the same press. */
const SAME_PRESS_MS = 700;

const reducedMotion = () => typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches;

function Phrase({ g, front }: { g: Greeting; front: boolean }) {
  const [before = '', after = ''] = g.text.split('{cseed}');
  return (
    <p class="wi-slot" lang={g.lang} dir={g.dir} data-lang={g.lang} data-front={front ? '' : undefined} data-wi-phrase>
      <span class="wi-text">
        <span class="wi-glass">
          {before}
          <span class="wi-cs" lang="en" dir="ltr">
            cseed
          </span>
          {after}
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
const tf = (t: Transform) => `translate(${px(t.tx)}, ${px(t.ty)}) scale(${num(t.sx)}, ${num(t.sy)})`;
const ROT = { r: 0, d: 90, l: 180, u: -90 } as const;

/**
 * The first screen of onboarding (V0.12). "Hello, welcome to cseed" in liquid glass over a looping
 * video (or a calm stand-in), cycling through 15 languages, with nothing else on screen. A press
 * anywhere (a click, a tap, a swipe or a key; Escape skips) melts the greeting onto the page's paper,
 * and the engine's plan (`introPlan.ts`) takes over: fruit spelling cseed, a pixel-block snake that
 * eats it, the word becoming the real wordmark, its travel onto the page logo and an opacity-only swap.
 * The screen draws only what `sampleAt(plan, ms)` returns, so playback, a second press (fast-forward),
 * Escape and a resize all go through the same sampler.
 */
export function WelcomeIntro({ onDone, targetSelector = '[data-onb-logo]', media = NO_MEDIA, leaving = false }: Props) {
  const root = useRef<HTMLDivElement>(null);
  const greet = useRef<HTMLDivElement>(null);
  const backdrop = useRef<HTMLDivElement>(null);
  const paper = useRef<HTMLDivElement>(null);
  const video = useRef<HTMLVideoElement>(null);
  const fruitLayer = useRef<HTMLDivElement>(null);
  const bodyLayer = useRef<HTMLDivElement>(null);
  const eyes = useRef<HTMLDivElement>(null);
  const markTravel = useRef<HTMLDivElement>(null);
  const mark = useRef<HTMLDivElement>(null);
  const sproutTravel = useRef<HTMLDivElement>(null);
  const sprout = useRef<HTMLDivElement>(null);
  const logo = useRef<HTMLDivElement>(null);

  const [slots, setSlots] = useState<Slots>({ a: 0, b: 1, front: 0, n: 0 });
  const [phase, setPhase] = useState<Phase>('greeting');
  const [videoOn, setVideoOn] = useState(false);
  const phaseRef = useRef<Phase>('greeting');
  const reduced = useRef(reducedMotion());
  const doneRef = useRef(onDone);
  doneRef.current = onDone;
  /** The running playback: its plan, its clock origin and the pieces it draws. */
  const run = useRef<{ plan: Plan; t0: number; retargeted: boolean; raf: number; draw: (s: Sample) => void } | null>(null);
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

  const measureTarget = (): Target => {
    const el = document.querySelector<HTMLElement>(targetSelector);
    const r = el?.getBoundingClientRect();
    const kind = el?.dataset.kind === 'mark' ? 'mark' : 'wordmark';
    return r && r.width > 0 ? { x: r.left, y: r.top, w: r.width, h: r.height, kind } : { x: 20, y: 26.75, w: 78, h: 18, kind };
  };
  const input = (): Input => {
    const el = root.current!;
    return { viewport: { w: el.clientWidth, h: el.clientHeight, dpr: window.devicePixelRatio || 1 }, target: measureTarget(), motion: reduced.current ? 'reduced' : 'full' };
  };

  const finish = () => {
    const r = run.current;
    if (r) cancelAnimationFrame(r.raf);
    if (phaseRef.current === 'done') return;
    phaseRef.current = 'done';
    setPhase('done');
    doneRef.current();
  };

  /** Builds the drawing for one plan: every element is created once, then only moved and faded. */
  const setup = (plan: Plan, melt: boolean) => {
    const write = makeWriter();
    const g = greet.current!;
    // The shown phrase splits into drips (full motion) or simply fades (reduced motion, Escape).
    const shown = g.querySelector<HTMLElement>(':scope > .wi-slot[data-front]');
    g.getAnimations({ subtree: true }).forEach((a) => a.cancel());
    const drips: { el: HTMLElement; at: number; drop: number }[] = [];
    if (melt && shown) {
      const w = g.clientWidth;
      const step = w / DRIPS;
      for (let i = 0; i < DRIPS; i++) {
        const d = document.createElement('div');
        d.className = 'wi-drip';
        d.style.left = `${(i * step).toFixed(2)}px`;
        d.style.width = `${(step + 0.5).toFixed(2)}px`;
        const clone = shown.cloneNode(true) as HTMLElement;
        clone.removeAttribute('data-wi-phrase');
        clone.style.left = `${(-i * step).toFixed(2)}px`;
        clone.style.width = `${w}px`;
        d.appendChild(clone);
        g.appendChild(d);
        // A fixed, irregular order (no randomness: the same every time).
        drips.push({ el: d, at: (((i * 7) % DRIPS) / (DRIPS - 1)) * DRIP_SPREAD, drop: 0.35 + ((i * 5) % 4) * 0.1 });
      }
      g.querySelectorAll<HTMLElement>(':scope > .wi-slot').forEach((s) => (s.style.visibility = 'hidden'));
    }
    const greetH = g.clientHeight;

    // Fruit, drawn from the plan's boxes and kinds.
    const fl = fruitLayer.current!;
    fl.textContent = '';
    const fruit = new Map<number, HTMLElement>();
    for (const f of plan.fruit) {
      const el = document.createElement('div');
      el.className = 'wi-fruit';
      el.dataset.kind = f.kind;
      el.setAttribute('data-wi-fruit', '');
      el.style.width = px(f.box.w);
      el.style.height = px(f.box.h);
      el.style.backgroundImage = FRUIT_ART[f.kind] ?? FRUIT_ART.apple;
      fl.appendChild(el);
      fruit.set(f.id, el);
    }

    // The snake: one pooled block per pose id, created when first needed.
    const bl = bodyLayer.current!;
    bl.querySelectorAll('.wi-seg').forEach((n) => n.remove());
    const segs = new Map<number, HTMLElement>();
    const seg = (id: number) => {
      let el = segs.get(id);
      if (!el) {
        el = document.createElement('div');
        el.className = 'wi-seg';
        el.setAttribute('data-wi-snake', '');
        bl.insertBefore(el, eyes.current);
        segs.set(id, el);
      }
      return el;
    };

    // The real wordmark (in the theme's ink), laid over the drawn word, and the separate sapling.
    const mk = mark.current!;
    mk.style.left = px(plan.logoBox.x);
    mk.style.top = px(plan.logoBox.y);
    mk.style.width = px(plan.logoBox.w);
    mk.style.height = px(plan.logoBox.h);
    const sp = sprout.current!;
    if (!sp.firstChild) sp.innerHTML = saplingSvg();

    const lg = logo.current!;
    let video_paused = false;

    return (s: Sample) => {
      // Backdrop → paper (the melt), then the paper hand-off.
      write(backdrop.current!, 'opacity', num(s.backdropOpacity));
      if (s.backdropOpacity === 0 && !video_paused) {
        video_paused = true;
        video.current?.pause();
        backdrop.current!.setAttribute('data-still', '');
        write(backdrop.current!, 'visibility', 'hidden');
      }
      write(paper.current!, 'opacity', num(s.overlayOpacity));

      // The greeting: drips stretch down and fade, each starting a little later than the last.
      const p = 1 - s.greetingOpacity;
      if (drips.length) {
        for (const d of drips) {
          const k = Math.max(0, Math.min(1, (p - d.at) / (1 - DRIP_SPREAD)));
          const e = k * k;
          write(d.el, 'transform', `translateY(${px(e * d.drop * greetH)}) scaleY(${num(1 + 0.6 * e)})`);
          write(d.el, 'opacity', num(1 - Math.min(1, k * 1.15)));
        }
      } else if (shown) write(shown, 'opacity', num(s.greetingOpacity));

      // Fruit pops in, then goes when the snake's head reaches it.
      for (const f of s.fruit) {
        const el = fruit.get(f.id);
        if (!el) continue;
        write(el, 'opacity', num(f.visible ? f.opacity : 0));
        write(el, 'transform', `translate(${px(f.box.x)}, ${px(f.box.y)}) scale(${num(f.scale)})`);
      }

      // The snake: head first; blocks not in this pose are hidden.
      write(bl, 'opacity', num(s.wordOpacity));
      const used = new Set<number>();
      for (const pose of s.body) {
        const el = seg(pose.id);
        used.add(pose.id);
        write(el, 'width', px(pose.box.w));
        write(el, 'height', px(pose.box.h));
        write(el, 'transform', `translate(${px(pose.box.x)}, ${px(pose.box.y)})`);
        write(el, 'opacity', num(pose.visible ? pose.opacity : 0));
      }
      for (const [id, el] of segs) if (!used.has(id)) write(el, 'opacity', '0');
      const head = s.body[0];
      const ey = eyes.current!;
      if (head) {
        write(ey, 'width', px(head.box.w));
        write(ey, 'height', px(head.box.h));
        write(ey, 'transform', `translate(${px(head.box.x)}, ${px(head.box.y)}) rotate(${ROT[head.dir]}deg)`);
        write(ey, 'opacity', num(head.visible ? head.opacity : 0));
      } else write(ey, 'opacity', '0');

      // The real wordmark fades in over the drawn word, then travels (origin 0 0, translate then scale).
      write(markTravel.current!, 'transform', tf(s.transform));
      write(mk, 'opacity', num(s.wordmarkOpacity));
      // The sapling sprouts from its stem, and on the rail travels on its own onto the sapling mark.
      write(sproutTravel.current!, 'transform', tf(s.sprout.transform));
      write(sp, 'left', px(s.sprout.box.x));
      write(sp, 'top', px(s.sprout.box.y));
      write(sp, 'width', px(s.sprout.box.w));
      write(sp, 'height', px(s.sprout.box.h));
      write(sp, 'opacity', num(s.sprout.opacity));
      write(sp, 'transform', `scale(${num(s.sprout.scale)})`);

      // The landing logo: the page logo's own images in its exact box, faded in, never scaled.
      write(lg, 'left', px(s.target.x));
      write(lg, 'top', px(s.target.y));
      write(lg, 'height', px(s.target.h));
      if (lg.dataset.kind !== s.target.kind) lg.dataset.kind = s.target.kind;
      write(lg, 'opacity', num(s.targetOpacity));
    };
  };

  /** Starts playback of `plan` from 0 ms. */
  const start = (plan: Plan, melt: boolean) => {
    phaseRef.current = 'playing';
    setPhase('playing');
    clearTimeout(cycleTimer.current);
    const draw = setup(plan, melt);
    const r = { plan, t0: performance.now(), retargeted: false, raf: 0, draw };
    run.current = r;
    const frame = (now: number) => {
      if (run.current !== r) return;
      const ms = now - r.t0;
      // The page logo is measured again when the word starts to travel (D-15).
      if (!r.retargeted && ms >= r.plan.timeline.morphStart) {
        r.retargeted = true;
        r.plan = retargetPlan(r.plan, measureTarget());
      }
      const s = sampleAt(r.plan, ms);
      r.draw(s);
      if (s.done) finish();
      else r.raf = requestAnimationFrame(frame);
    };
    frame(performance.now());
  };

  /** A second press: jump to the plan's fast-forward point (the swap still plays). */
  const fastForward = () => {
    const r = run.current;
    if (!r || phaseRef.current !== 'playing') return;
    if (!r.retargeted) {
      r.retargeted = true;
      r.plan = retargetPlan(r.plan, measureTarget());
    }
    const ff = r.plan.timeline.ffTo;
    if (performance.now() - r.t0 < ff) r.t0 = performance.now() - ff;
  };

  const activate = () => {
    if (phaseRef.current === 'greeting') start(buildPlan(input()), !reduced.current);
    else fastForward();
  };
  const skip = () => {
    if (phaseRef.current === 'greeting') start(skipPlan(input()), false);
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
      if (e.key === 'Escape') {
        e.preventDefault();
        skip();
        return;
      }
      if (!startKey(e)) return; // F-keys, modifiers, media keys and shortcuts stay the browser's
      e.preventDefault();
      activate();
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
    // Start the fonts for every greeting now, while English shows, so no language swaps fonts mid-fade.
    document.fonts?.load?.('500 1em "cseed Hello"', GREETINGS.map((g) => g.text).join('')).catch(() => {});
    // A rotation or resize mid-run: jump to the end state (timeline.done, not ffTo), onto the freshly measured logo (D-15).
    const onResize = () => {
      const r = run.current;
      if (!r || phaseRef.current !== 'playing') return;
      r.retargeted = true;
      r.plan = retargetPlan(r.plan, measureTarget());
      r.t0 = Math.min(r.t0, performance.now() - r.plan.timeline.done);
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
      onClick={() => {
        // Assistive tech can click without pointer events; a real press has already been handled.
        if (performance.now() - lastPointer.current < SAME_PRESS_MS) return;
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
      <div class="wi-layer" ref={fruitLayer} aria-hidden="true" />
      <div class="wi-layer" ref={bodyLayer} aria-hidden="true">
        <div class="wi-eyes" ref={eyes} />
      </div>
      <div class="wi-layer" ref={markTravel} aria-hidden="true">
        <div class="wi-mark" ref={mark} data-wi-word>
          {/* The same artwork as the page's 156px wordmark at 640px, so it stays sharp while it is large. */}
          <img class="wordmark-logo wordmark-logo--ink" src="/images/cseed-logo.png" alt="" width="640" height="147" decoding="async" />
          <img class="wordmark-logo wordmark-logo--white" src="/images/cseed-logo-white.png" alt="" width="640" height="147" decoding="async" />
        </div>
      </div>
      <div class="wi-layer" ref={sproutTravel} aria-hidden="true">
        <div class="wi-sprout" ref={sprout} data-wi-sprout />
      </div>
      <div class="wi-logo" ref={logo} data-wi-logo aria-hidden="true">
        <LogoImages />
      </div>
    </div>
  );
}
