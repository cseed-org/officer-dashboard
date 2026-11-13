import { useEffect, useRef, useState } from 'preact/hooks';
import { ACCESSIBLE_GREETING, GREETINGS, type Greeting } from './greetings';
import { LogoImages } from './LogoMark';
import { nextPalette } from './palette';
import { buildPlan, type Plan } from './snakePlan';
import './intro.css';

/** Background video and group photo. Built from what is in `public/media/` (see onboarding.astro and docs/frontend.md). */
export interface HelloMedia {
  /** `<source>`s for the looping background video, best first; empty = the animated gradient. */
  video: { src: string; type: string }[];
  poster?: string;
  /** The group photo; absent = the placeholder illustration. */
  photo?: string;
}
export const PHOTO_PLACEHOLDER = '/media/hello-group-placeholder.svg';
const NO_MEDIA: HelloMedia = { video: [] };

/** How long each language stays (ms), its fade, and the palette rhythm: a new colour every 1.2s, cross-faded over 0.9s. */
const CYCLE_MS = 2100;
const FADE_IN = 560;
const FADE_OUT = 380;
const PALETTE_MS = 1200;
const PALETTE_FADE = 900;
const EASE = 'cubic-bezier(.2, .8, .2, 1)';
/** Reduced motion: how long the photo stays before the fade to the logo. */
const REDUCED_HOLD = 1500;

const reducedMotion = () => typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches;
/** SVG filters in `backdrop-filter` (refraction) only render in Chromium; elsewhere the glass is blur only. */
const canRefract = () => typeof navigator !== 'undefined' && /\b(?:Chrome|Chromium)\/\d+/.test(navigator.userAgent);

function Phrase({ g }: { g: Greeting }) {
  const [before = '', after = ''] = g.text.split('{cseed}');
  return (
    <p class="wi-phrase" lang={g.lang} dir={g.dir} data-wi-phrase data-lang={g.lang} data-active="false">
      <span class="wi-text">
        {before}
        <span class="wi-cs" lang="en" dir="ltr">
          cseed
        </span>
        {after}
      </span>
    </p>
  );
}

interface Props {
  /** Called once the intro has finished and its logo sits exactly where the page's logo is. */
  onDone: () => void;
  /** CSS selector of the page's logo, the snake's destination (it may carry `data-kind="mark"`). */
  targetSelector?: string;
  media?: HelloMedia;
}

type State = 'greeting' | 'playing' | 'spelled' | 'done';

/**
 * The first screen of onboarding (V0.10). "Hello, welcome to cseed" cycles through 15 languages
 * in glass letters over a drifting mesh of colour that changes every 1.2s, with an optional
 * video behind. There is no start button. Any click, tap or key (Tab still moves focus, Escape
 * skips) brings up the group photo and starts an arcade snake. It eats the greeting, eats the
 * apples along the word, spells "cseed" with its body, holds, then shrinks into the top-left logo.
 * Pressing again fast-forwards. With reduced motion (or Skip) there is no snake.
 */
export function WelcomeIntro({ onDone, targetSelector = '[data-onb-logo]', media = NO_MEDIA }: Props) {
  const root = useRef<HTMLDivElement>(null);
  const stage = useRef<HTMLDivElement>(null);
  const fx = useRef<HTMLDivElement>(null);
  const board = useRef<HTMLDivElement>(null);
  const snake = useRef<HTMLDivElement>(null);
  const logo = useRef<HTMLDivElement>(null);
  const bg = useRef<HTMLDivElement>(null);
  const photo = useRef<HTMLElement>(null);
  const hint = useRef<HTMLParagraphElement>(null);
  const washes = useRef<HTMLDivElement>(null);
  const [state, setState] = useState<State>('greeting');
  const [videoOn, setVideoOn] = useState(media.video.length > 0);
  const [photoSrc, setPhotoSrc] = useState(media.photo ?? PHOTO_PLACEHOLDER);
  const stateRef = useRef<State>(state);
  const go = (s: State) => {
    stateRef.current = s;
    setState(s);
  };
  const anims = useRef<Animation[]>([]);
  const timer = useRef<number>(0);
  const raf = useRef<number>(0);
  const fastForward = useRef<(() => void) | null>(null);
  const activeIndex = useRef(-1);
  const doneRef = useRef(onDone);
  doneRef.current = onDone;
  const reduced = useRef(reducedMotion());
  const refract = useRef(canRefract());

  /* ── Colour: a new random palette every 1.2s, cross-faded (never cut) between two layers ── */
  useEffect(() => {
    const layers = Array.from(washes.current?.children ?? []) as HTMLElement[];
    let hue = Math.random() * 360;
    let i = 0;
    const paint = () => {
      const p = nextPalette(hue);
      hue = p.hue;
      const next = layers[i % 2];
      const prev = layers[(i + 1) % 2];
      if (!next || !prev) return;
      next.style.background = p.css;
      next.style.zIndex = '2';
      prev.style.zIndex = '1';
      next.animate([{ opacity: 0 }, { opacity: 1 }], { duration: i === 0 || reduced.current ? 1 : PALETTE_FADE, easing: 'cubic-bezier(.4, 0, .2, 1)', fill: 'forwards' });
      i++;
    };
    paint();
    // Reduced motion keeps one palette: no colour changes at all.
    const id = reduced.current ? 0 : window.setInterval(paint, PALETTE_MS);
    return () => clearInterval(id);
  }, []);

  /* ── Greeting cycle ── */
  useEffect(() => {
    const phrases = () => Array.from(stage.current?.querySelectorAll<HTMLElement>(':scope > [data-wi-phrase]') ?? []);
    const show = (i: number) => {
      const list = phrases();
      const prev = list[activeIndex.current];
      const next = list[i];
      if (!next) return;
      activeIndex.current = i;
      list.forEach((el) => el.setAttribute('data-active', el === next ? 'true' : 'false'));
      if (reduced.current) {
        if (prev) prev.style.opacity = '0';
        next.style.opacity = '1';
        return;
      }
      if (prev) prev.animate([{ opacity: 1, transform: 'translateY(0) scale(1)' }, { opacity: 0, transform: 'translateY(-10px) scale(.98)' }], { duration: FADE_OUT, easing: 'ease-in', fill: 'forwards' });
      next.animate([{ opacity: 0, transform: 'translateY(14px) scale(.98)' }, { opacity: 1, transform: 'translateY(0) scale(1)' }], { duration: FADE_IN, easing: EASE, fill: 'forwards' });
    };
    const tick = () => {
      show((activeIndex.current + 1) % GREETINGS.length);
      timer.current = window.setTimeout(tick, CYCLE_MS);
    };
    timer.current = window.setTimeout(tick, 150);
    const hintTimer = window.setTimeout(() => hint.current?.animate([{ opacity: 0 }, { opacity: 1 }], { duration: 600, fill: 'forwards' }), 1400);
    return () => {
      clearTimeout(timer.current);
      clearTimeout(hintTimer);
    };
  }, []);

  /* ── Start: photo + snake, then the hand-off ── */
  const finishNow = () => {
    fastForward.current?.();
    anims.current.forEach((a) => a.playState !== 'finished' && a.finish());
  };

  const play = (skip: boolean, from?: { x: number; y: number }) => {
    const el = root.current;
    const st = stage.current;
    const layer = fx.current;
    if (!el || !st || !layer || stateRef.current !== 'greeting') return;
    go('playing');
    clearTimeout(timer.current);
    const list = anims.current;
    const run = (target: Element, frames: Keyframe[], opts: KeyframeAnimationOptions) => {
      const a = target.animate(frames, { fill: 'both', ...opts });
      list.push(a);
      return a;
    };
    const phrases = Array.from(st.querySelectorAll<HTMLElement>(':scope > [data-wi-phrase]'));
    const current = phrases.find((p) => p.getAttribute('data-active') === 'true');
    st.getAnimations({ subtree: true }).forEach((a) => a.cancel());
    phrases.forEach((p) => (p.style.opacity = p === current ? '1' : '0'));
    if (hint.current) run(hint.current, [{ opacity: 0 }], { duration: 160 });
    run(el.querySelector('.wi-skip')!, [{ opacity: 1 }, { opacity: 0 }], { duration: 200 });

    // The page's logo is the destination; measured now (fixed overlay = viewport coordinates).
    const target = document.querySelector<HTMLElement>(targetSelector);
    const found = target?.getBoundingClientRect();
    const box = found && found.width ? { x: found.left, y: found.top, w: found.width, h: found.height } : { x: 20, y: 20, w: 78, h: 18 };
    const lg = logo.current!;
    lg.style.left = `${box.x}px`;
    lg.style.top = `${box.y}px`;
    lg.style.height = `${box.h}px`;
    if (target?.dataset.kind) lg.dataset.kind = target.dataset.kind;

    const vw = el.clientWidth;
    const vh = el.clientHeight;
    const ph = photo.current!;
    const revealPhoto = (delay = 0) => {
      if (reduced.current) {
        run(ph, [{ opacity: 0 }, { opacity: 1 }], { duration: 200, delay });
        return;
      }
      const r = ph.getBoundingClientRect();
      const o = from ?? { x: vw / 2, y: vh / 2 };
      const dx = o.x - (r.left + r.width / 2);
      const dy = o.y - (r.top + r.height / 2);
      run(
        ph,
        [
          { opacity: 0, transform: `translate(${dx}px, ${dy}px) scale(.08)` },
          { opacity: 1, transform: 'translate(0, 0) scale(1.03)', offset: 0.7 },
          { opacity: 1, transform: 'translate(0, 0) scale(1)' },
        ],
        { duration: 720, delay, easing: EASE },
      );
    };
    /**
     * Hand-off: everything but the logo fades over the page, whose logo is at the same spot. The
     * fade starts from whatever opacity each layer has then (one keyframe) and fills forwards only,
     * so during its delay it cannot override the layers' own reveals (or reveal what was never shown).
     */
    const end = (after: number, layers: Element[]) => {
      layers.forEach((n) => run(n, [{ opacity: 0 }], { duration: 240, delay: after, easing: 'ease-out', fill: 'forwards' }));
      return run(el, [{ opacity: 1 }, { opacity: 1 }], { duration: after + 240 });
    };

    let finished: Promise<unknown>;
    if (skip) {
      if (current) run(current, [{ opacity: 1 }, { opacity: 0 }], { duration: reduced.current ? 1 : 160 });
      run(lg, [{ opacity: 0 }, { opacity: 1 }], { duration: reduced.current ? 1 : 200, delay: reduced.current ? 0 : 80 });
      finished = end(reduced.current ? 60 : 200, [bg.current!, st]).finished; // the photo and grid never showed
    } else if (reduced.current) {
      // No snake: the photo fades in, stays a moment, then the logo fades in and the overlay goes.
      if (current) run(current, [{ opacity: 1 }, { opacity: 0 }], { duration: 200 });
      revealPhoto();
      run(lg, [{ opacity: 0 }, { opacity: 1 }], { duration: 200, delay: REDUCED_HOLD });
      finished = end(REDUCED_HOLD + 200, [bg.current!, ph, st]).finished;
    } else {
      revealPhoto();
      const textBox = (() => {
        const span = current?.querySelector<HTMLElement>('.wi-text');
        if (!span) return null;
        const r = document.createRange();
        r.selectNodeContents(span);
        const rects = Array.from(r.getClientRects());
        if (!rects.length) return span.getBoundingClientRect();
        return { left: Math.min(...rects.map((q) => q.left)), right: Math.max(...rects.map((q) => q.right)), top: Math.min(...rects.map((q) => q.top)), bottom: Math.max(...rects.map((q) => q.bottom)) };
      })();
      const plan = buildPlan({ vw, vh, text: textBox ?? { left: vw * 0.2, right: vw * 0.8, top: vh * 0.45, bottom: vh * 0.55 }, target: box });
      finished = runSnake(plan, current, run, (after) => end(after, [bg.current!, ph, board.current!, st]));
    }

    finished
      .then(() => {
        if (!root.current) return;
        go('done');
        doneRef.current();
      })
      .catch(() => {});
  };

  /** Lays out the board, the greeting strips and the apples, then drives the snake one grid tick at a time. */
  const runSnake = (plan: Plan, current: HTMLElement | undefined, run: (t: Element, f: Keyframe[], o: KeyframeAnimationOptions) => Animation, end: (after: number) => Animation) => {
    const layer = fx.current!;
    const bd = board.current!;
    const sn = snake.current!;
    const { C } = plan;
    const S = Math.round(C * 0.86);
    const inset = (C - S) / 2;
    bd.style.setProperty('--wi-c', `${C}px`);
    bd.style.backgroundPosition = `${plan.ox}px ${plan.oy}px`;
    root.current!.style.setProperty('--wi-r', `${Math.max(2, Math.round(C * 0.1))}px`);
    run(bd, [{ opacity: 0 }, { opacity: 1 }], { duration: 400 });

    // 1. The greeting becomes vertical strips; each vanishes as the head reaches it.
    if (current) {
      current.style.opacity = '0';
      const vw = root.current!.clientWidth;
      for (const s of plan.slices) {
        const strip = document.createElement('div');
        strip.className = 'wi-slice';
        strip.style.left = `${s.x0}px`;
        strip.style.width = `${s.x1 - s.x0}px`;
        const clone = current.cloneNode(true) as HTMLElement;
        clone.removeAttribute('data-wi-phrase');
        clone.removeAttribute('data-active');
        clone.setAttribute('aria-hidden', 'true');
        clone.style.left = `${-s.x0}px`;
        clone.style.width = `${vw}px`;
        clone.style.opacity = '1';
        strip.appendChild(clone);
        layer.appendChild(strip);
        run(strip, [{ opacity: 1, transform: 'scale(1)' }, { opacity: 0, transform: 'scale(.86)' }], { delay: s.eatAt, duration: 150, easing: 'ease-in' });
      }
    }

    // 2. Apples on the word, popping in while the snake is off-screen; each goes when the head lands on it.
    const wrapT = plan.at[plan.wrapAt]!;
    const stagger = Math.min(6, 320 / plan.apples.length);
    plan.apples.forEach((idx, j) => {
      const cell = plan.route[idx]!;
      const p = plan.px(cell);
      const a = document.createElement('div');
      a.className = 'wi-apple';
      a.setAttribute('data-wi-apple', '');
      a.dataset.kind = cell.kind;
      const size = Math.round(S * 0.7);
      a.style.width = a.style.height = `${size}px`;
      const pos = `translate(${(p.x + (C - size) / 2).toFixed(1)}px, ${(p.y + (C - size) / 2).toFixed(1)}px)`;
      layer.appendChild(a);
      run(a, [{ opacity: 0, transform: `${pos} scale(0)` }, { opacity: 1, transform: `${pos} scale(1.25)`, offset: 0.6 }, { opacity: 1, transform: `${pos} scale(1)` }], { delay: wrapT - 380 + j * stagger, duration: 300, easing: EASE });
      run(a, [{ opacity: 0, transform: `${pos} scale(1)` }], { delay: plan.at[idx]!, duration: 1, fill: 'forwards' });
    });

    // 3. The snake: one pooled square per body cell, moved by transform on each tick.
    const max = plan.ticks[plan.ticks.length - 1]!.len;
    const segs: HTMLElement[] = [];
    for (let i = 0; i < max; i++) {
      const d = document.createElement('div');
      d.className = i === 0 ? 'wi-seg wi-seg--head' : 'wi-seg';
      d.setAttribute('data-wi-snake', i === 0 ? 'head' : 'body');
      d.style.width = d.style.height = `${S}px`;
      if (i === 0) d.innerHTML = '<i class="wi-eye wi-eye--l"></i><i class="wi-eye wi-eye--r"></i>';
      segs.push(d);
    }
    for (let i = max - 1; i >= 0; i--) sn.appendChild(segs[i]!);
    const render = (t: number) => {
      const body = plan.body(t);
      segs.forEach((el, s) => {
        const ri = body[s];
        if (ri === undefined) {
          el.style.opacity = '0';
          return;
        }
        const c = plan.route[ri]!;
        const p = plan.px(c);
        const on = c.x >= -1 && c.x <= plan.cols && c.y >= -1 && c.y <= plan.rows;
        el.style.opacity = on ? '1' : '0';
        let rot = '';
        if (s === 0) {
          const b = plan.route[ri - 1] ?? c;
          const dx = c.x - b.x;
          const dy = c.y - b.y;
          rot = ` rotate(${dx === -1 ? 180 : dy === 1 ? 90 : dy === -1 ? -90 : 0}deg)`;
        }
        el.style.transform = `translate(${(p.x + inset).toFixed(1)}px, ${(p.y + inset).toFixed(1)}px)${rot}`;
        el.dataset.kind = c.kind;
        el.dataset.cell = `${c.x - plan.gx0},${c.y - plan.gy0}`;
      });
    };
    const last = plan.ticks.length - 1;
    let shown = -1;
    const t0 = performance.now();
    const spelled = () => {
      render(last);
      shown = last;
      // Dim the joins so "cseed" reads (opacity only, on the compositor).
      if (stateRef.current === 'playing') segs.forEach((el) => el.dataset.kind === 'join' && run(el, [{ opacity: 0.26 }], { duration: 300, fill: 'forwards' }));
      cancelAnimationFrame(raf.current);
      if (stateRef.current === 'playing') go('spelled');
    };
    fastForward.current = spelled;
    const frame = (now: number) => {
      const el = now - t0;
      let t = shown;
      while (t < last && plan.at[t + 1]! <= el) t++;
      if (t !== shown) {
        shown = t;
        render(t);
      }
      if (t >= last) spelled();
      else raf.current = requestAnimationFrame(frame);
    };
    render(0);
    shown = 0;
    raf.current = requestAnimationFrame(frame);

    // 4. Hold on "cseed", then the word shrinks into the logo, which takes its place.
    const { tx, ty, s } = plan.morph;
    run(bd, [{ opacity: 0 }], { delay: plan.morphAt - 300, duration: 300, fill: 'forwards' });
    run(sn, [{ transform: 'translate(0, 0) scale(1)' }, { transform: `translate(${tx.toFixed(2)}px, ${ty.toFixed(2)}px) scale(${s.toFixed(5)})` }], { delay: plan.morphAt, duration: plan.morphEnd - plan.morphAt, easing: 'cubic-bezier(.65, 0, .25, 1)' });
    run(sn, [{ opacity: 1 }, { opacity: 0 }], { delay: plan.morphEnd - 100, duration: 140 });
    run(logo.current!, [{ opacity: 0, transform: 'scale(.85)' }, { opacity: 1, transform: 'scale(1)' }], { delay: plan.morphEnd - 160, duration: 220, easing: EASE });
    return end(plan.duration + 80).finished;
  };

  /* ── Input: a click, a tap or any key starts; Tab still moves focus, Escape skips; a second press fast-forwards ── */
  const activate = (from?: { x: number; y: number }) => {
    if (stateRef.current === 'greeting') play(false, from);
    else if (stateRef.current === 'playing' || stateRef.current === 'spelled') finishNow();
  };
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Tab' || ['Shift', 'Control', 'Alt', 'Meta', 'CapsLock', 'Fn', 'OS'].includes(e.key) || e.isComposing) return;
      if (e.ctrlKey || e.metaKey || e.altKey) return; // leave browser shortcuts alone
      if (e.key === 'Escape') {
        e.preventDefault();
        if (stateRef.current === 'greeting') play(true);
        else finishNow();
        return;
      }
      // A focused button handles its own Enter/Space.
      if ((e.key === 'Enter' || e.key === ' ' || e.key === 'Spacebar') && (e.target as HTMLElement | null)?.closest('button, a')) return;
      e.preventDefault();
      activate();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);
  useEffect(() => {
    const el = root.current;
    const before = document.activeElement as HTMLElement | null;
    // A real modal: everything outside the overlay is inert while it is up.
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
    // Tab moves between the overlay itself and Skip, and never leaves.
    const trap = (e: KeyboardEvent) => {
      if (e.key !== 'Tab' || !el) return;
      const f = [el, ...Array.from(el.querySelectorAll<HTMLElement>('button'))];
      const i = f.indexOf(document.activeElement as HTMLElement);
      const next = e.shiftKey ? (i <= 0 ? f.length - 1 : i - 1) : i === f.length - 1 ? 0 : i + 1;
      e.preventDefault();
      f[next]?.focus();
    };
    el?.addEventListener('keydown', trap);
    return () => {
      el?.removeEventListener('keydown', trap);
      inerted.forEach((n) => n.removeAttribute('inert'));
      if (before && before !== document.body && before.isConnected) before.focus({ preventScroll: true });
      clearTimeout(timer.current);
      cancelAnimationFrame(raf.current);
      anims.current.forEach((a) => a.cancel());
    };
  }, []);

  const hasPhoto = photoSrc !== PHOTO_PLACEHOLDER;
  return (
    <div
      class={`wi${refract.current ? ' wi--refract' : ''}`}
      ref={root}
      tabIndex={-1}
      role="dialog"
      aria-modal="true"
      aria-label="Welcome to cseed. Press any key, click or tap anywhere to begin."
      data-state={state}
      data-video={videoOn ? 'on' : 'off'}
      data-photo={hasPhoto ? 'on' : 'placeholder'}
      onClick={(e) => activate({ x: e.clientX, y: e.clientY })}
    >
      <svg class="wi-svg" aria-hidden="true" focusable="false">
        <filter id="wi-refract" x="-10%" y="-10%" width="120%" height="120%">
          <feTurbulence type="fractalNoise" baseFrequency="0.012 0.018" numOctaves="2" seed="3" result="n" />
          <feGaussianBlur in="n" stdDeviation="1.5" result="nb" />
          <feDisplacementMap in="SourceGraphic" in2="nb" scale="22" xChannelSelector="R" yChannelSelector="G" />
        </filter>
        <filter id="wi-glass-text" x="-5%" y="-20%" width="110%" height="140%">
          <feGaussianBlur in="SourceAlpha" stdDeviation="2.2" result="b" />
          <feSpecularLighting in="b" surfaceScale="4" specularConstant=".95" specularExponent="22" lighting-color="#ffffff" result="s">
            <feDistantLight azimuth="235" elevation="50" />
          </feSpecularLighting>
          <feComposite in="s" in2="SourceAlpha" operator="in" result="lit" />
          <feMerge>
            <feMergeNode in="SourceGraphic" />
            <feMergeNode in="lit" />
          </feMerge>
        </filter>
      </svg>
      <div class="wi-bg" ref={bg} aria-hidden="true">
        {videoOn && (
          <video class="wi-video" data-wi-video autoplay muted loop playsInline preload="auto" poster={media.poster} onError={() => setVideoOn(false)}>
            {media.video.map((v, i) => (
              <source key={v.src} src={v.src} type={v.type} onError={i === media.video.length - 1 ? () => setVideoOn(false) : undefined} />
            ))}
          </video>
        )}
        <div class="wi-fallback" data-wi-fallback />
        <div class="wi-washes" ref={washes}>
          <div class="wi-wash" />
          <div class="wi-wash" />
        </div>
      </div>
      <figure class="wi-photo" ref={photo} data-wi-photo={hasPhoto ? 'photo' : 'placeholder'}>
        <img src={photoSrc} alt={hasPhoto ? 'The cseed team' : ''} decoding="async" onError={() => setPhotoSrc(PHOTO_PLACEHOLDER)} />
      </figure>
      <p class="visually-hidden">{ACCESSIBLE_GREETING}</p>
      <div class="wi-stage" ref={stage} aria-hidden="true">
        {GREETINGS.map((g) => (
          <Phrase key={g.lang} g={g} />
        ))}
      </div>
      <div class="wi-board" ref={board} aria-hidden="true" />
      <div class="wi-fx" ref={fx} aria-hidden="true" />
      <div class="wi-snake" ref={snake} aria-hidden="true" />
      <div class="wi-logo" ref={logo} data-wi-logo aria-hidden="true">
        <LogoImages />
      </div>
      <p class="wi-hint wi-glass-ui" ref={hint} aria-hidden="true">
        <span class="wi-hint-mouse">Click anywhere or press any key</span>
        <span class="wi-hint-touch">Tap anywhere to begin</span>
      </p>
      <button
        type="button"
        class="wi-skip wi-glass-ui"
        onClick={(e) => {
          e.stopPropagation();
          if (stateRef.current === 'greeting') play(true);
          else finishNow();
        }}
      >
        Skip
      </button>
    </div>
  );
}
