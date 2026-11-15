import { useEffect, useRef, useState } from 'preact/hooks';
import { ACCESSIBLE_GREETING, GREETINGS, type Greeting } from './greetings';
import { saplingSvg } from '../brand/saplingMarkup';
import { LogoImages } from './LogoMark';
import { buildPlan, roundCorners, type Plan } from './snakePlan';
import './intro.css';

/** The group photo, built from what is in `public/media/` (see onboarding.astro and docs/frontend.md). */
export interface HelloMedia {
  /** The group photo; absent = the placeholder illustration. */
  photo?: string;
}
export const PHOTO_PLACEHOLDER = '/media/hello-group-placeholder.svg';
const NO_MEDIA: HelloMedia = {};

/** How long each language stays (ms) and its fades. */
const CYCLE_MS = 2100;
const FADE_IN = 560;
const FADE_OUT = 380;
const EASE = 'cubic-bezier(.2, .8, .2, 1)';
/** Reduced motion: how long the photo stays before the fade to the logo. */
const REDUCED_HOLD = 1500;

const reducedMotion = () => typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches;

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
  /** CSS selector of the page's logo, the snake's destination (it may carry `data-kind="mark"`: the rail's sapling). */
  targetSelector?: string;
  media?: HelloMedia;
}

type State = 'greeting' | 'playing' | 'spelled' | 'done';

/**
 * The first screen of onboarding (V0.10, calmer in V0.11). "Hello, welcome to cseed" cycles
 * through 15 languages on the app's plain background. There is no start button. Any click, tap or
 * key (Tab still moves focus, Escape skips) brings up the group photo and starts an arcade snake.
 * It eats the greeting, eats the seeds laid along the word, writes "cseed" with its body, a
 * sapling sprouts after the d, and the whole wordmark shrinks onto the shell's logo. Pressing
 * again fast-forwards. With reduced motion (or Skip) there is no snake.
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
  const [state, setState] = useState<State>('greeting');
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
    const hintTimer = window.setTimeout(() => stateRef.current === 'greeting' && hint.current?.animate([{ opacity: 0 }, { opacity: 1 }], { duration: 600, fill: 'forwards' }), 1400);
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
    const kind: 'mark' | 'wordmark' = target?.dataset.kind === 'mark' ? 'mark' : 'wordmark';
    const box = found && found.width ? { x: found.left, y: found.top, w: found.width, h: found.height, kind } : { x: 20, y: 20, w: 78, h: 18, kind };
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
      finished = runSnake(plan, box, current, run, (after) => end(after, [bg.current!, ph, board.current!, st]));
    }

    finished
      .then(() => {
        if (!root.current) return;
        go('done');
        doneRef.current();
      })
      .catch(() => {});
  };

  /** Lays out the board, the greeting strips and the seeds, then drives the snake one grid tick at a time. */
  const runSnake = (plan: Plan, box: { kind: 'mark' | 'wordmark' }, current: HTMLElement | undefined, run: (t: Element, f: Keyframe[], o: KeyframeAnimationOptions) => Animation, end: (after: number) => Animation) => {
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

    // 2. Seeds on the letters, popping in while the snake is off-screen; each goes when the head lands on it.
    //    The hidden lifts between letters hold seeds too (that is how the snake keeps growing), but they are never drawn.
    const wrapT = plan.at[plan.wrapAt]!;
    const seeds = plan.apples.filter((idx) => plan.route[idx]!.kind === 'letter');
    const stagger = Math.min(6, 320 / seeds.length);
    const seedSize = Math.max(4, Math.round(S * 0.56));
    seeds.forEach((idx, j) => {
      const p = plan.px(plan.route[idx]!);
      const a = document.createElement('div');
      a.className = 'wi-apple';
      a.setAttribute('data-wi-apple', '');
      a.style.width = `${seedSize}px`;
      a.style.height = `${Math.round(seedSize * 1.15)}px`;
      const pos = `translate(${(p.x + (C - seedSize) / 2).toFixed(1)}px, ${(p.y + (C - seedSize * 1.15) / 2).toFixed(1)}px)`;
      layer.appendChild(a);
      run(a, [{ opacity: 0, transform: `${pos} scale(0)` }, { opacity: 1, transform: `${pos} scale(1.25)`, offset: 0.6 }, { opacity: 1, transform: `${pos} scale(1)` }], { delay: wrapT - 380 + j * stagger, duration: 300, easing: EASE });
      run(a, [{ opacity: 0, transform: `${pos} scale(1)` }], { delay: plan.at[idx]!, duration: 1, fill: 'forwards' });
    });

    // 3. The snake: one pooled square per body cell, moved by transform on each tick. A cell on a
    //    hidden lift is never drawn, so the snake writes only the letters.
    const word = document.createElement('div');
    word.className = 'wi-word';
    sn.appendChild(word);
    // The snake is drawn in the wordmark's own ink (fixed, both themes) while the light photo card
    // is behind it. A copy of the written word in the theme's text colour sits on top, filled in
    // once the word is written, and fades in over the shrink, so in dark mode the word arrives on
    // the dark shell as light ink, the same as the white wordmark that takes over (opacity only).
    const themed = document.createElement('div');
    themed.className = 'wi-word wi-word--themed';
    themed.setAttribute('data-wi-word-themed', '');
    sn.appendChild(themed);
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
    for (let i = max - 1; i >= 0; i--) word.appendChild(segs[i]!);
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
        const on = c.kind !== 'lift' && c.x >= -1 && c.x <= plan.cols && c.y >= -1 && c.y <= plan.rows;
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

    // The sapling, waiting to sprout after the d.
    const sprout = document.createElement('div');
    sprout.className = 'wi-sprout';
    sprout.setAttribute('data-wi-sprout', '');
    sprout.innerHTML = saplingSvg();
    sprout.style.width = sprout.style.height = `${plan.sprout.size.toFixed(1)}px`;
    sprout.style.left = `${plan.sprout.x.toFixed(1)}px`;
    sprout.style.top = `${plan.sprout.y.toFixed(1)}px`;
    sn.insertBefore(sprout, word);

    const last = plan.ticks.length - 1;
    let shown = -1;
    const t0 = performance.now();
    const corners = roundCorners();
    const spelled = () => {
      render(last);
      shown = last;
      cancelAnimationFrame(raf.current);
      if (stateRef.current !== 'playing') return;
      // The cells close up into solid, round strokes (scale and opacity on the compositor; the
      // corners are a one-off restyle), the eyes close and the sapling sprouts.
      const grow = (C / S) * 1.06;
      segs.forEach((el) => {
        if (el.dataset.kind !== 'letter') return;
        // Square where cells meet, round only on the outer corner of a turn.
        const r = corners.get(el.dataset.cell!);
        el.style.borderRadius = '0';
        if (r) {
          el.dataset.round = r;
          el.style[({ tl: 'borderTopLeftRadius', tr: 'borderTopRightRadius', br: 'borderBottomRightRadius', bl: 'borderBottomLeftRadius' } as const)[r]] = '50%';
        }
        // Scale after the cell's own translate, so each cell grows about its own centre.
        const at = el.style.transform;
        run(el, [{ transform: `${at} scale(1)` }, { transform: `${at} scale(${grow.toFixed(3)})` }], { duration: 260, easing: EASE, fill: 'forwards' });
        const copy = document.createElement('div');
        copy.className = 'wi-seg';
        copy.style.cssText = el.style.cssText;
        copy.style.opacity = '1';
        copy.style.transform = `${at} scale(${grow.toFixed(3)})`;
        themed.appendChild(copy);
      });
      segs[0]!.querySelectorAll('.wi-eye').forEach((e) => run(e, [{ opacity: 0 }], { duration: 160, fill: 'forwards' }));
      go('spelled');
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

    // 4. The sapling sprouts from its stem, the wordmark holds, then shrinks onto the page logo,
    //    which takes its place. On the rail the sapling lands on the sapling mark and the word fades.
    run(sprout, [
      { opacity: 0, transform: 'scale(0, 0)' },
      { opacity: 1, transform: 'scale(.35, .7)', offset: 0.35 },
      { opacity: 1, transform: 'scale(1.08)', offset: 0.75 },
      { opacity: 1, transform: 'scale(1)' },
    ], { delay: plan.spelledAt + 120, duration: 620, easing: 'cubic-bezier(.3, .7, .3, 1)' });
    const { tx, ty, s } = plan.morph;
    run(bd, [{ opacity: 0 }], { delay: plan.morphAt - 300, duration: 300, fill: 'forwards' });
    run(sn, [{ transform: 'translate(0, 0) scale(1)' }, { transform: `translate(${tx.toFixed(2)}px, ${ty.toFixed(2)}px) scale(${s.toFixed(5)})` }], { delay: plan.morphAt, duration: plan.morphEnd - plan.morphAt, easing: 'cubic-bezier(.65, 0, .25, 1)' });
    // (On the rail the word fades out instead, so the themed copy never shows.)
    if (box.kind !== 'mark') run(themed, [{ opacity: 0 }, { opacity: 1 }], { delay: plan.morphAt, duration: plan.morphEnd - plan.morphAt, easing: 'cubic-bezier(.65, 0, .25, 1)' });
    if (box.kind === 'mark') run(word, [{ opacity: 1 }, { opacity: 0 }], { delay: plan.morphAt, duration: (plan.morphEnd - plan.morphAt) * 0.6, easing: 'ease-in' });
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
      class="wi"
      ref={root}
      tabIndex={-1}
      role="dialog"
      aria-modal="true"
      aria-label="Welcome to cseed. Press any key, click or tap anywhere to begin."
      data-state={state}
      data-photo={hasPhoto ? 'on' : 'placeholder'}
      onClick={(e) => activate({ x: e.clientX, y: e.clientY })}
    >
      <div class="wi-bg" ref={bg} aria-hidden="true" />
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
      <p class="wi-hint wi-ui" ref={hint} aria-hidden="true">
        <span class="wi-hint-mouse">Click anywhere or press any key</span>
        <span class="wi-hint-touch">Tap anywhere to begin</span>
      </p>
      <button
        type="button"
        class="wi-skip wi-ui"
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
