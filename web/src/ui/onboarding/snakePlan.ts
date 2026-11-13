/**
 * Geometry and timing for the intro's snake. Pure functions, no DOM: `buildPlan` lays out
 * one route (across the greeting, off-screen, in over the apples that spell "cseed", then
 * up to the logo), a speed profile along it, and bakes per-segment Web Animations keyframes
 * (transform + opacity only).
 */

export interface P {
  x: number;
  y: number;
}
interface Rect {
  left: number;
  right: number;
  top: number;
  bottom: number;
}

export interface PlanInput {
  vw: number;
  vh: number;
  /** Where the greeting sits, for eating it left to right. */
  text: Rect;
  /** The logo's final centre and size; the snake collapses into this. */
  target: { cx: number; cy: number; size: number };
}

export interface Plan {
  D: number;
  appleSize: number;
  segCount: number;
  duration: number;
  apples: { x: number; y: number; eatAt: number; index: number }[];
  /** Vertical strips of the greeting with the time (ms) the head reaches each. */
  slices: { x0: number; x1: number; eatAt: number }[];
  /** When the apples start to pop in (ms) and how long the pause lasts. */
  holdStart: number;
  holdEnd: number;
  /** ms into the animation that the head is within reach of the logo. */
  collapseAt: number;
  /** Keyframes for segment `i` (0 = head). */
  segment(i: number): Keyframe[];
}

const RAD = Math.PI / 180;
const lerp = (a: number, b: number, t: number) => a + (b - a) * t;
const clamp = (v: number, a: number, b: number) => Math.min(b, Math.max(a, v));
const dist = (a: P, b: P) => Math.hypot(b.x - a.x, b.y - a.y);

function arc(cx: number, cy: number, rx: number, ry: number, a0: number, a1: number): P[] {
  const n = Math.max(2, Math.ceil(Math.abs(a1 - a0) / 4));
  const out: P[] = [];
  for (let i = 0; i <= n; i++) {
    const a = lerp(a0, a1, i / n) * RAD;
    out.push({ x: cx + rx * Math.cos(a), y: cy - ry * Math.sin(a) });
  }
  return out;
}
const seg = (a: P, b: P): P[] => {
  const n = Math.max(2, Math.ceil(dist(a, b) / 6));
  return Array.from({ length: n + 1 }, (_, i) => ({ x: lerp(a.x, b.x, i / n), y: lerp(a.y, b.y, i / n) }));
};
function bezier(p0: P, p1: P, p2: P, p3: P): P[] {
  const n = Math.max(8, Math.ceil((dist(p0, p1) + dist(p1, p2) + dist(p2, p3)) / 6));
  const out: P[] = [];
  for (let i = 0; i <= n; i++) {
    const t = i / n;
    const u = 1 - t;
    out.push({
      x: u * u * u * p0.x + 3 * u * u * t * p1.x + 3 * u * t * t * p2.x + t * t * t * p3.x,
      y: u * u * u * p0.y + 3 * u * u * t * p1.y + 3 * u * t * t * p2.y + t * t * t * p3.y,
    });
  }
  return out;
}
const unit = (a: P, b: P): P => {
  const d = Math.hypot(b.x - a.x, b.y - a.y) || 1;
  return { x: (b.x - a.x) / d, y: (b.y - a.y) / d };
};
const tail = (pts: P[]) => unit(pts[Math.max(0, pts.length - 4)]!, pts[pts.length - 1]!);
const head = (pts: P[]) => unit(pts[0]!, pts[Math.min(3, pts.length - 1)]!);

/** Letters in a unit box: u across (0..1), v down (0 = x-height line, 1 = baseline, -0.5 = ascender). */
function letterStrokes(ch: string): P[][] {
  switch (ch) {
    case 'c':
      return [arc(0.5, 0.5, 0.48, 0.5, 80, 315)];
    case 's':
      return [[...arc(0.5, 0.25, 0.42, 0.25, 25, 270), ...arc(0.5, 0.75, 0.42, 0.25, 90, -155).slice(1)]];
    case 'e':
      return [[...seg({ x: 0.04, y: 0.5 }, { x: 0.96, y: 0.5 }), ...arc(0.5, 0.5, 0.46, 0.5, 0, 320).slice(1)]];
    default: // d
      return [[...arc(0.38, 0.5, 0.38, 0.5, 0, 360), ...seg({ x: 0.76, y: 0.5 }, { x: 0.76, y: -0.5 }).slice(1)]];
  }
}

/** Resample a polyline at roughly even `step` spacing (always keeps the first point). */
function resample(pts: P[], step: number): P[] {
  const out: P[] = [pts[0]!];
  let acc = 0;
  for (let i = 1; i < pts.length; i++) {
    let a = pts[i - 1]!;
    const b = pts[i]!;
    let seglen = dist(a, b);
    while (acc + seglen >= step) {
      const t = (step - acc) / seglen;
      a = { x: lerp(a.x, b.x, t), y: lerp(a.y, b.y, t) };
      out.push(a);
      seglen = dist(a, b);
      acc = 0;
    }
    acc += seglen;
  }
  return out;
}

interface Sample {
  x: number;
  y: number;
  d: number;
}
interface Piece {
  /** Distance range [d0, d1] this piece covers. */
  v: number;
  rampIn: number;
  rampOut: number;
}

/** Time (s) for the head to cover `x` of a piece of length `len` (trapezoid: ramp up, cruise, ramp down). */
function pieceTime(p: Piece, len: number, x: number): number {
  const { v, rampIn: r1, rampOut: r2 } = p;
  x = clamp(x, 0, len);
  const a1 = r1 > 0 ? (v * v) / (2 * r1) : 0;
  const a2 = r2 > 0 ? (v * v) / (2 * r2) : 0;
  const t1 = r1 > 0 ? (2 * r1) / v : 0;
  const cruiseEnd = len - r2;
  const tCruiseEnd = t1 + Math.max(0, cruiseEnd - r1) / v;
  if (r1 > 0 && x < r1) return Math.sqrt((2 * x) / a1);
  if (x <= cruiseEnd) return t1 + (x - r1) / v;
  const xp = x - cruiseEnd;
  return tCruiseEnd + (v - Math.sqrt(Math.max(0, v * v - 2 * a2 * xp))) / a2;
}

export function buildPlan(input: PlanInput): Plan {
  const { vw, vh, text, target } = input;
  const H = Math.min(vw * 0.2, vh * 0.28, 170);
  const D = clamp(H * 0.17, 16, 28);
  const appleSize = D * 0.82;
  const segCount = 26;
  const sp = D * 0.52;
  const tailLen = segCount * sp;
  const pad = tailLen + D * 3;
  const cy = vh / 2;
  const speedA = clamp(vw * 1.0, 700, 1500);
  const speedB = clamp(H * 5.2, 420, 950);

  /* ── Route ───────────────────────────────────────────────────────────── */
  // Piece A: left to right across the greeting, with a gentle slither.
  const amp = D * 0.35;
  const lambda = D * 9;
  const A: P[] = [];
  const aSpan = vw + pad * 2;
  for (let x = 0; x <= aSpan; x += 8) {
    const ramp = clamp(x / (pad + 80), 0, 1);
    A.push({ x: x - pad, y: cy + amp * ramp * Math.sin((x / lambda) * Math.PI * 2) });
  }
  // Piece B: in from the left above the word, hook down into the "c", then trace cseed.
  const Wc = H * 0.62;
  const gap = H * 0.28;
  const wordW = 5 * Wc + 4 * gap;
  const X0 = (vw - wordW) / 2;
  const Y0 = cy - H * 0.25;
  const cStart = letterStrokes('c')[0]!.map((p) => ({ x: X0 + p.x * Wc, y: Y0 + p.y * H }))[0]!;
  const rho = H * 0.23;
  const laneY = cStart.y - 2 * rho;
  const B: P[] = [...seg({ x: -pad, y: laneY }, { x: cStart.x, y: laneY }), ...arc(cStart.x, laneY + rho, rho, rho, 90, -90).slice(1)];
  const strokes: { pts: P[]; from: number }[] = [];
  let prev: P[] | null = null;
  'cseed'.split('').forEach((ch, i) => {
    const lx = X0 + i * (Wc + gap);
    const pts = letterStrokes(ch)[0]!.map((p) => ({ x: lx + p.x * Wc, y: Y0 + p.y * H }));
    if (prev) {
      const a = prev[prev.length - 1]!;
      const b = pts[0]!;
      const h = Math.max(dist(a, b) * 0.4, H * 0.25);
      const t0 = tail(prev);
      const t3 = head(pts);
      B.push(...bezier(a, { x: a.x + t0.x * h, y: a.y + t0.y * h }, { x: b.x - t3.x * h, y: b.y - t3.y * h }, b).slice(1));
    }
    strokes.push({ pts, from: B.length });
    B.push(...pts.slice(prev ? 0 : 1));
    prev = pts;
  });
  // Piece C: from the top of the "d" up and across to the logo.
  const T: P = { x: target.cx, y: target.cy };
  const dEnd = B[B.length - 1]!;
  const dTan = tail(prev!);
  const span = dist(dEnd, T);
  const C = bezier(
    dEnd,
    { x: dEnd.x + dTan.x * span * 0.45, y: dEnd.y + dTan.y * span * 0.45 },
    { x: T.x + span * 0.38, y: T.y - span * 0.08 },
    T,
  ).slice(1);

  // Samples with cumulative distance; the A→B jump (off-screen both sides) has ~0 length.
  const samples: Sample[] = [];
  let d = 0;
  const push = (pts: P[], jump = false) => {
    pts.forEach((p, i) => {
      const last = samples[samples.length - 1];
      if (last) d += jump && i === 0 ? 0.01 : Math.hypot(p.x - last.x, p.y - last.y);
      samples.push({ x: p.x, y: p.y, d });
    });
  };
  push(A);
  const dA = d;
  push(B, true);
  const dB0 = dA + 0.01;
  const dBEnd = d;
  push(C);
  const dEndRoute = d;
  const dTotal = dEndRoute + tailLen; // the tail keeps going until it reaches the logo too

  const at = (dist: number): { x: number; y: number } => {
    if (dist <= 0) return samples[0]!;
    if (dist >= dEndRoute) return samples[samples.length - 1]!;
    let lo = 0;
    let hi = samples.length - 1;
    while (hi - lo > 1) {
      const mid = (lo + hi) >> 1;
      if (samples[mid]!.d <= dist) lo = mid;
      else hi = mid;
    }
    const a = samples[lo]!;
    const b = samples[hi]!;
    const t = b.d === a.d ? 0 : (dist - a.d) / (b.d - a.d);
    return { x: lerp(a.x, b.x, t), y: lerp(a.y, b.y, t) };
  };

  /* ── Speed profile ───────────────────────────────────────────────────── */
  const HOLD = 0.6; // s, off-screen: the apples pop in
  const pA: Piece = { v: speedA, rampIn: Math.min(380, dA * 0.2), rampOut: 0 };
  const pB: Piece = { v: speedB, rampIn: Math.min(260, (dBEnd - dB0) * 0.2), rampOut: 0 };
  const lenC = dTotal - dBEnd;
  const pC: Piece = { v: speedB * 1.1, rampIn: 0, rampOut: lenC * 0.7 };
  const tA = pieceTime(pA, dA, dA);
  const tB = pieceTime(pB, dBEnd - dB0, dBEnd - dB0);
  const tC = pieceTime(pC, lenC, lenC);
  const total = tA + HOLD + tB + tC;
  const timeAt = (dist: number): number => {
    if (dist <= dA) return pieceTime(pA, dA, dist);
    if (dist <= dBEnd) return tA + HOLD + pieceTime(pB, dBEnd - dB0, dist - dB0);
    return tA + HOLD + tB + pieceTime(pC, lenC, dist - dBEnd);
  };
  const distAt = (t: number): number => {
    if (t <= tA) return invert((x) => pieceTime(pA, dA, x), t, 0, dA);
    if (t <= tA + HOLD) return dA;
    if (t <= tA + HOLD + tB) return dB0 + invert((x) => pieceTime(pB, dBEnd - dB0, x), t - tA - HOLD, 0, dBEnd - dB0);
    return dBEnd + invert((x) => pieceTime(pC, lenC, x), t - tA - HOLD - tB, 0, lenC);
  };

  /* ── What gets eaten, and when ───────────────────────────────────────── */
  const nearest = (x: number) => {
    // First sample of piece A whose x reaches `x` (A is monotone in x).
    for (const s of samples) if (s.d <= dA && s.x >= x) return s.d;
    return dA;
  };
  const SLICE = 26;
  const slices: Plan['slices'] = [];
  const sliceStart = Math.floor(text.left - 6);
  const sliceEnd = Math.ceil(text.right + 6);
  for (let x = sliceStart; x < sliceEnd; x += SLICE) {
    slices.push({ x0: x, x1: Math.min(x + SLICE, sliceEnd), eatAt: Math.max(0, timeAt(nearest(x + SLICE * 0.5 - D * 0.2))) * 1000 });
  }

  const apples: Plan['apples'] = [];
  const spacing = appleSize * 1.4;
  // Apples sit on the letter strokes, so the dist of each is read off the route samples.
  let appleIndex = 0;
  for (const st of strokes) {
    for (const p of resample(st.pts, spacing)) {
      let best = dB0;
      let bestD = Infinity;
      for (const s of samples) {
        if (s.d < dB0 || s.d > dBEnd) continue;
        const dd = (s.x - p.x) ** 2 + (s.y - p.y) ** 2;
        if (dd < bestD) {
          bestD = dd;
          best = s.d;
        }
      }
      apples.push({ x: p.x, y: p.y, eatAt: timeAt(best) * 1000, index: appleIndex++ });
    }
  }

  /* ── Keyframes ───────────────────────────────────────────────────────── */
  const FRAME = 1000 / 60;
  const frames = Math.ceil((total * 1000) / FRAME) + 1;
  const dShrink = dEndRoute - Math.max(span, 240) * 0.9;
  const endScale = clamp((target.size * 0.78) / D, 0.5, 1);
  const scaleAt = (dd: number) => {
    const k = clamp((dd - dShrink) / (dEndRoute - dShrink), 0, 1);
    const e = k * k * (3 - 2 * k);
    return lerp(1, endScale, e);
  };
  const dists = Array.from({ length: frames }, (_, f) => distAt(Math.min(total, (f * FRAME) / 1000)));
  const margin = D * 1.5;

  const segment = (i: number): Keyframe[] => {
    const out: Keyframe[] = [];
    let angle = 0;
    let lastAngle = 0;
    for (let f = 0; f < frames; f++) {
      const dd = dists[f]! - i * sp;
      const p = at(dd);
      const sc = scaleAt(dd);
      const off = p.x < -margin || p.x > vw + margin || p.y < -margin || p.y > vh + margin || dd < 0;
      let tf = `translate(${p.x.toFixed(2)}px, ${p.y.toFixed(2)}px) translate(-50%, -50%) scale(${sc.toFixed(3)})`;
      if (i === 0) {
        const a = at(dd - D * 0.7);
        const b = at(dd + D * 0.7);
        if (dist(a, b) > 0.5) {
          let raw = Math.atan2(b.y - a.y, b.x - a.x) / RAD;
          // Unwrap so the head never spins the long way round between frames.
          while (raw - lastAngle > 180) raw -= 360;
          while (raw - lastAngle < -180) raw += 360;
          // Low-pass so the 90° corners of the letters read as turns, not snaps.
          angle = f === 0 ? raw : lerp(angle, raw, 0.35);
          lastAngle = angle;
        }
        tf += ` rotate(${angle.toFixed(1)}deg)`;
      }
      out.push({ transform: tf, opacity: off ? 0 : 1 });
    }
    return out;
  };

  return {
    D,
    appleSize,
    segCount,
    duration: total * 1000,
    apples,
    slices,
    holdStart: tA * 1000,
    holdEnd: (tA + HOLD) * 1000,
    collapseAt: timeAt(Math.max(dBEnd, dEndRoute - span * 0.2)) * 1000,
    segment,
  };
}

/** Invert a monotone increasing f on [lo, hi] by bisection. */
function invert(f: (x: number) => number, t: number, lo: number, hi: number): number {
  for (let i = 0; i < 28; i++) {
    const mid = (lo + hi) / 2;
    if (f(mid) < t) lo = mid;
    else hi = mid;
  }
  return (lo + hi) / 2;
}
