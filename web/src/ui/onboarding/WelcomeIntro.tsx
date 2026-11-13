import { useEffect, useRef, useState } from 'preact/hooks';
import { ACCESSIBLE_GREETING, GREETINGS, type Greeting } from './greetings';
import { LogoImages } from './LogoMark';
import { buildPlan } from './snakePlan';
import './intro.css';

/** How long each language stays (ms), the crossfade, and the wait before the first one. */
const CYCLE_MS = 2100;
const FADE_IN = 520;
const FADE_OUT = 380;
const EASE = 'cubic-bezier(.2, .8, .2, 1)';
/** Where the shell logo sits (78×18 at 20,26.75; tablet: the 28px C mark at 17.5,30; phone: 69×16, centred at 17.5) if the page behind hasn't rendered it yet. */
const fallback = () => (innerWidth >= 768 && innerWidth < 1100 ? { x: 17.5, y: 30, w: 28, h: 28 } : innerWidth < 768 ? { x: innerWidth / 2 - 34.67, y: 17.5, w: 69.33, h: 16 } : { x: 20, y: 26.75, w: 78, h: 18 });

const reducedMotion = () => typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches;

function Phrase({ g, active }: { g: Greeting; active?: boolean }) {
  const [before = '', after = ''] = g.text.split('{cseed}');
  return (
    <p class="wi-phrase" lang={g.lang} dir={g.dir} data-wi-phrase data-lang={g.lang} data-active={active ? 'true' : 'false'}>
      <span>
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
  /** CSS selector of the page's logo, the snake's destination. */
  targetSelector?: string;
}

/**
 * The first screen of onboarding: "Hello, welcome to cseed" cycling through languages. Any
 * click, tap, Enter or Space sends in a snake that eats the greeting, eats apples spelling
 * "cseed", then shrinks into the top-left corner and becomes the logo. With reduced motion
 * (or Skip) it just fades to the logo.
 */
export function WelcomeIntro({ onDone, targetSelector = '[data-onb-logo]' }: Props) {
  const root = useRef<HTMLDivElement>(null);
  const stage = useRef<HTMLDivElement>(null);
  const fx = useRef<HTMLDivElement>(null);
  const logo = useRef<HTMLDivElement>(null);
  const bg = useRef<HTMLDivElement>(null);
  const hint = useRef<HTMLParagraphElement>(null);
  const [state, setState] = useState<'greeting' | 'playing' | 'done'>('greeting');
  const stateRef = useRef(state);
  stateRef.current = state;
  const anims = useRef<Animation[]>([]);
  const timer = useRef<number>(0);
  const activeIndex = useRef(-1);
  const doneRef = useRef(onDone);
  doneRef.current = onDone;
  const reduced = useRef(reducedMotion());

  /* ── Greeting cycle ───────────────────────────────────────────────── */
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
      if (prev) prev.animate([{ opacity: 1, transform: 'translateY(0)' }, { opacity: 0, transform: 'translateY(-10px)' }], { duration: FADE_OUT, easing: 'ease-in', fill: 'forwards' });
      next.animate([{ opacity: 0, transform: 'translateY(12px)' }, { opacity: 1, transform: 'translateY(0)' }], { duration: FADE_IN, easing: EASE, fill: 'forwards' });
    };
    const tick = () => {
      show((activeIndex.current + 1) % GREETINGS.length);
      timer.current = window.setTimeout(tick, CYCLE_MS);
    };
    timer.current = window.setTimeout(tick, 150);
    const hintTimer = window.setTimeout(() => hint.current?.animate([{ opacity: 0 }, { opacity: 0.75 }], { duration: 600, fill: 'forwards' }), 1400);
    return () => {
      clearTimeout(timer.current);
      clearTimeout(hintTimer);
    };
  }, []);

  /* ── Dismiss: play the hand-off ───────────────────────────────────── */
  const finishNow = () => anims.current.forEach((a) => a.playState !== 'finished' && a.finish());

  const play = (fast: boolean) => {
    const el = root.current;
    const st = stage.current;
    const layer = fx.current;
    if (!el || !st || !layer || stateRef.current !== 'greeting') return;
    setState('playing');
    stateRef.current = 'playing';
    clearTimeout(timer.current);
    const list: Animation[] = anims.current;
    const run = (target: Element, frames: Keyframe[], opts: KeyframeAnimationOptions) => {
      const a = target.animate(frames, { fill: 'both', ...opts });
      list.push(a);
      return a;
    };
    // Settle the greeting on whatever is showing now.
    const phrases = Array.from(st.querySelectorAll<HTMLElement>(':scope > [data-wi-phrase]'));
    const current = phrases.find((p) => p.getAttribute('data-active') === 'true');
    st.getAnimations({ subtree: true }).forEach((a) => a.cancel());
    phrases.forEach((p) => (p.style.opacity = p === current ? '1' : '0'));
    if (hint.current) run(hint.current, [{ opacity: 0 }], { duration: 200 });
    run(root.current!.querySelector('.wi-skip')!, [{ opacity: 1 }, { opacity: 0 }], { duration: 200 });

    // The page's own logo is the destination; measure it now (fixed overlay = viewport coords).
    const found = document.querySelector<HTMLElement>(targetSelector)?.getBoundingClientRect();
    const box = found && found.width ? { x: found.left, y: found.top, w: found.width, h: found.height } : fallback();
    const lg = logo.current!;
    lg.style.left = `${box.x}px`;
    lg.style.top = `${box.y}px`;
    lg.style.height = `${box.h}px`;

    const end = (after: number) => {
      // Hand off: the overlay's own background fades away over the page, whose logo is at the same spot.
      run(bg.current!, [{ opacity: 1 }, { opacity: 0 }], { duration: 240, delay: after, easing: 'ease-out' });
      const last = run(el, [{ opacity: 1 }, { opacity: 1 }], { duration: after + 240 });
      return last;
    };

    let finished: Promise<unknown>;
    if (fast || reduced.current) {
      // Simple path: the greeting fades and the logo fades in.
      if (current) run(current, [{ opacity: 1 }, { opacity: 0 }], { duration: reduced.current ? 1 : 160 });
      run(lg, [{ opacity: 0 }, { opacity: 1 }], { duration: reduced.current ? 1 : 200, delay: reduced.current ? 0 : 80 });
      finished = end(reduced.current ? 60 : 200).finished;
    } else {
      const textBox = (() => {
        const span = current?.firstElementChild as HTMLElement | null;
        if (!span) return null;
        const r = document.createRange();
        r.selectNodeContents(span);
        const rects = Array.from(r.getClientRects());
        if (!rects.length) return span.getBoundingClientRect();
        return {
          left: Math.min(...rects.map((q) => q.left)),
          right: Math.max(...rects.map((q) => q.right)),
          top: Math.min(...rects.map((q) => q.top)),
          bottom: Math.max(...rects.map((q) => q.bottom)),
        };
      })();
      const vw = el.clientWidth;
      const vh = el.clientHeight;
      const plan = buildPlan({
        vw,
        vh,
        text: textBox ?? { left: vw * 0.2, right: vw * 0.8, top: vh * 0.45, bottom: vh * 0.55 },
        target: { cx: box.x + box.w / 2, cy: box.y + box.h / 2, size: box.h },
      });

      // 1. The greeting becomes vertical strips; each vanishes as the head reaches it.
      if (current) {
        current.style.opacity = '0';
        for (const s of plan.slices) {
          const clone = current.cloneNode(true) as HTMLElement;
          clone.removeAttribute('data-wi-phrase');
          clone.removeAttribute('data-active');
          clone.setAttribute('aria-hidden', 'true');
          clone.className = 'wi-slice wi-phrase';
          clone.style.opacity = '1';
          clone.style.clipPath = `inset(0 ${Math.max(0, vw - s.x1)}px 0 ${Math.max(0, s.x0)}px)`;
          clone.style.transformOrigin = `${(s.x0 + s.x1) / 2}px 50%`;
          layer.appendChild(clone);
          run(clone, [{ opacity: 1, transform: 'scale(1)' }, { opacity: 0, transform: 'scale(.9)' }], { delay: s.eatAt, duration: 170, easing: 'ease-in' });
        }
      }

      // 2. The snake: segment 0 is the head, drawn last so it sits on top.
      const n = plan.segCount;
      for (let i = n - 1; i >= 0; i--) {
        const dot = document.createElement('div');
        dot.className = i === 0 ? 'wi-seg wi-seg--head' : 'wi-seg';
        dot.setAttribute('data-wi-snake', i === 0 ? 'head' : 'body');
        const size = plan.D * (i === 0 ? 1.18 : 1 - (i / n) * 0.32);
        dot.style.width = dot.style.height = `${size}px`;
        if (i === 0) {
          for (const side of ['l', 'r']) {
            const eye = document.createElement('i');
            eye.className = `wi-eye wi-eye--${side}`;
            dot.appendChild(eye);
          }
        }
        layer.appendChild(dot);
        run(dot, plan.segment(i), { duration: plan.duration, easing: 'linear' });
      }

      // 3. Apples appear while the snake is off-screen, then each is eaten as the head passes.
      for (const ap of plan.apples) {
        const a = document.createElement('div');
        a.className = 'wi-apple';
        a.setAttribute('data-wi-apple', '');
        a.style.width = a.style.height = `${plan.appleSize}px`;
        a.style.marginLeft = a.style.marginTop = `${-plan.appleSize / 2}px`;
        layer.appendChild(a);
        const pos = `translate(${ap.x.toFixed(1)}px, ${ap.y.toFixed(1)}px)`;
        run(
          a,
          [
            { opacity: 0, transform: `${pos} scale(0)`, offset: 0 },
            { opacity: 1, transform: `${pos} scale(1.18)`, offset: 0.55 },
            { opacity: 1, transform: `${pos} scale(1)`, offset: 0.8 },
            { opacity: 1, transform: `${pos} scale(1)`, offset: 1 },
          ],
          { delay: plan.holdStart + 40 + ap.index * 11, duration: 360, easing: EASE },
        );
        run(a, [{ opacity: 1, transform: `${pos} scale(1)` }, { opacity: 0, transform: `${pos} scale(0.2)` }], { delay: Math.max(ap.eatAt - 40, plan.holdEnd), duration: 140, easing: 'ease-in', fill: 'forwards' });
      }

      // 4. The snake collapses into the logo, which takes its place pixel for pixel.
      const crossAt = plan.duration - 140;
      run(lg, [{ opacity: 0, transform: 'scale(.8)' }, { opacity: 1, transform: 'scale(1)' }], { delay: crossAt, duration: 220, easing: EASE });
      list.push(
        ...Array.from(layer.querySelectorAll<HTMLElement>('.wi-seg')).map((s) =>
          s.animate([{ opacity: 1 }, { opacity: 0 }], { delay: crossAt + 60, duration: 140, fill: 'forwards' }),
        ),
      );
      finished = end(plan.duration + 120).finished;
    }

    finished
      .then(() => {
        if (!root.current) return;
        stateRef.current = 'done';
        setState('done');
        doneRef.current();
      })
      .catch(() => {});
  };

  /* ── Input: click / tap / Enter / Space; a second press fast-forwards ── */
  const activate = () => {
    if (stateRef.current === 'greeting') play(false);
    else if (stateRef.current === 'playing') finishNow();
  };
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Enter' && e.key !== ' ' && e.key !== 'Spacebar') return;
      if ((e.target as HTMLElement | null)?.closest('button, a')) return; // buttons handle their own keys
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
    // Tab stays on Skip and Continue.
    const trap = (e: KeyboardEvent) => {
      if (e.key !== 'Tab' || !el) return;
      const f = Array.from(el.querySelectorAll<HTMLElement>('button'));
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
      anims.current.forEach((a) => a.cancel());
    };
  }, []);

  return (
    <div class="wi" ref={root} tabIndex={-1} role="dialog" aria-modal="true" aria-label="Welcome to cseed" data-state={state} onClick={activate}>
      <div class="wi-bg" ref={bg} />
      <p class="visually-hidden">{ACCESSIBLE_GREETING}</p>
      <div class="wi-stage" ref={stage} aria-hidden="true">
        {GREETINGS.map((g) => (
          <Phrase key={g.lang} g={g} />
        ))}
      </div>
      <div class="wi-fx" ref={fx} aria-hidden="true" />
      <div class="wi-logo" ref={logo} data-wi-logo aria-hidden="true">
        <LogoImages />
      </div>
      <p class="wi-hint" ref={hint} aria-hidden="true">
        Press anywhere to continue
      </p>
      <button
        type="button"
        class="wi-skip"
        onClick={(e) => {
          e.stopPropagation();
          if (stateRef.current === 'greeting') play(true);
          else finishNow();
        }}
      >
        Skip
      </button>
      <button type="button" class="visually-hidden wi-continue">
        Continue to setup
      </button>
    </div>
  );
}
