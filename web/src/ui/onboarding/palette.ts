/** The hello screen's colour washes (pure, no DOM). */

/** A vivid random palette whose main hue jumps 70–220° from the last one, so consecutive colours never look alike. */
export function nextPalette(prevHue: number, rnd: () => number = Math.random) {
  const hue = (prevHue + 70 + rnd() * 150) % 360;
  const h2 = (hue + 25 + rnd() * 40) % 360;
  const h3 = (hue + 150 + rnd() * 60) % 360;
  const c = (h: number, s: number, l: number) => `hsl(${h.toFixed(0)} ${s}% ${l}%)`;
  return {
    hue,
    css:
      `radial-gradient(38% 48% at 18% 22%, ${c(hue, 88, 56)}, transparent 70%), radial-gradient(42% 52% at 82% 28%, ${c(h2, 92, 62)}, transparent 70%), ` +
      `radial-gradient(52% 50% at 55% 88%, ${c(h3, 85, 52)}, transparent 72%), radial-gradient(30% 40% at 40% 50%, ${c(h2, 92, 62)}, transparent 70%), ` +
      `hsl(${((hue + 300) % 360).toFixed(0)} 70% 42% / .78)`,
  };
}
