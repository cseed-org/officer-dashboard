/**
 * In-page layout checks, adapted from cseed.co's scripts/check-mobile.mjs: sideways
 * scroll, clipped text, overlapping text, and nav items that collide with the logo, each
 * other, or the screen edge. Runs inside the browser via page.evaluate(measure).
 */
export interface Measurement {
  width: number;
  scrollWidth: number;
  panned: number;
  clipped: string[];
  overlaps: string[];
  nav: string[];
}

export function measure(): Measurement {
  const root = document.documentElement;
  const width = root.clientWidth;
  scrollTo(5000, 0);
  const panned = scrollX;
  scrollTo(0, 0);

  type Line = { el: Element; text: string; left: number; right: number; top: number; bottom: number };
  const lines: Line[] = [];
  const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
  while (walker.nextNode()) {
    const node = walker.currentNode as Text;
    const el = node.parentElement;
    if (!node.data.trim() || !el) continue;
    if (el.closest('astro-dev-toolbar, script, style, noscript, [hidden], [aria-hidden="true"], .visually-hidden')) continue;
    if (!el.checkVisibility({ opacityProperty: true, visibilityProperty: true })) continue;
    const range = document.createRange();
    range.selectNodeContents(node);
    for (const r of range.getClientRects()) {
      if (r.width < 1 || r.height < 1) continue;
      const inset = r.height / 4; // glyphs fill about the middle half of a line box
      lines.push({ el, text: node.data.trim().replace(/\s+/g, ' ').slice(0, 40), left: r.left, right: r.right, top: r.top + inset, bottom: r.bottom - inset });
    }
  }

  const clipped: string[] = [];
  for (const line of lines) {
    let left = 0;
    let right = width;
    for (let a = line.el.parentElement; a && a !== document.body; a = a.parentElement) {
      if (getComputedStyle(a).overflowX === 'visible') continue;
      const box = a.getBoundingClientRect();
      left = Math.max(left, box.left);
      right = Math.min(right, box.right);
    }
    // Ellipsis truncation is intentional, not clipping.
    if (getComputedStyle(line.el).textOverflow === 'ellipsis') continue;
    const over = Math.max(left - line.left, line.right - right);
    if (over > 2) clipped.push(`"${line.text}" by ${Math.round(over)}px`);
  }

  const overlaps = new Set<string>();
  const area = (l: Line) => (l.right - l.left) * (l.bottom - l.top);
  // Fixed / sticky bars legitimately sit over scrolled content; compare like with like.
  const layer = (el: Element) => (el.closest('.nav-bar, .tab-bar') ? 'chrome' : 'page');
  for (let i = 0; i < lines.length; i++) {
    for (let j = i + 1; j < lines.length; j++) {
      const a = lines[i]!;
      const b = lines[j]!;
      if (a.el.contains(b.el) || b.el.contains(a.el) || layer(a.el) !== layer(b.el)) continue;
      const w = Math.min(a.right, b.right) - Math.max(a.left, b.left);
      const h = Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top);
      if (w > 0 && h > 0 && (w * h) / Math.min(area(a), area(b)) > 0.3) overlaps.add(`"${a.text}" and "${b.text}"`);
    }
  }

  const nav: string[] = [];
  const visible = (el: Element) => getComputedStyle(el).display !== 'none' && el.getBoundingClientRect().width > 0;
  const mark = [...document.querySelectorAll('.nav-bar-mark')].find(visible)?.getBoundingClientRect();
  for (const selector of ['.nav-bar-link', '.tab-bar-link']) {
    const links = [...document.querySelectorAll(selector)].filter(visible).map((a) => ({ text: a.textContent!.trim(), box: a.getBoundingClientRect() }));
    links.forEach((link, i) => {
      if (selector === '.nav-bar-link' && mark && link.box.left < mark.right) nav.push(`"${link.text}" runs into the logo`);
      if (link.box.right > width + 1 || link.box.left < -1) nav.push(`"${link.text}" runs off the screen`);
      if (i && link.box.left < links[i - 1]!.box.right - 1) nav.push(`"${links[i - 1]!.text}" and "${link.text}" overlap`);
      if (link.box.height < 24) nav.push(`"${link.text}" tap target is ${Math.round(link.box.height)}px tall`);
    });
  }

  return { width, scrollWidth: document.scrollingElement!.scrollWidth, panned, clipped, overlaps: [...overlaps], nav };
}

export function layoutFailures(m: Measurement): string[] {
  const failures: string[] = [];
  if (m.panned > 0 || m.scrollWidth > m.width + 1) failures.push(`sideways scroll: the page is ${m.scrollWidth}px wide on a ${m.width}px screen`);
  if (m.clipped.length) failures.push(`clipped text: ${m.clipped.slice(0, 6).join('; ')}`);
  if (m.overlaps.length) failures.push(`overlapping text: ${m.overlaps.slice(0, 6).join('; ')}`);
  if (m.nav.length) failures.push(`nav: ${m.nav.join('; ')}`);
  return failures;
}
