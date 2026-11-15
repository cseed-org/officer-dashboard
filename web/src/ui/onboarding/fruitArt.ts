import type { FruitKind } from './introPlan';

/**
 * The hello intro's fruit (V0.12, D-3): original 9×9 pixel sprites in the same blocky arcade
 * style as the snake. Each one is drawn as an SVG of single-pixel rects with crisp edges, so it
 * stays sharp at any cell size, and shipped as a data: URI (img-src allows data:), so there is
 * no request and no flash while it loads. The engine picks the kind; the screen only draws it.
 */
const PALETTE: Record<string, string> = {
  r: '#e5362f', // apple red
  R: '#ff8a70', // apple highlight
  d: '#a91f22', // apple shade
  c: '#d6143f', // cherry red
  C: '#ff7b93', // cherry highlight
  e: '#8e0b2a', // cherry shade
  o: '#ff961a', // orange
  O: '#ffd27a', // orange highlight
  p: '#d96a07', // orange shade
  b: '#6b3d1c', // stem brown
  g: '#3fae3a', // leaf green
  G: '#2a7d2a', // leaf shade
};

/** Rows top to bottom; '.' is transparent. */
const SPRITES: Record<FruitKind, readonly string[]> = {
  apple: [
    '....bgg..',
    '....bGg..',
    '.rr.b.rr.',
    'rRRrrrrrr',
    'rRrrrrrrd',
    'rrrrrrrrd',
    'rrrrrrrdd',
    '.rrrrrdd.',
    '..rd.dd..',
  ],
  cherry: [
    '.....ggG.',
    '....g.g..',
    '...g...g.',
    '..g....g.',
    '.ccc..g..',
    'cCccecce.',
    'cCcecCcce',
    'ccee.ccee',
    '.ee...ee.',
  ],
  orange: [
    '....gG...',
    '...bgg...',
    '.oooboo..',
    'oOOoooooo',
    'oOooooooo',
    'ooooooooo',
    'oooooooop',
    '.oooooop.',
    '..oppp...',
  ],
};

function svg(rows: readonly string[]): string {
  let rects = '';
  rows.forEach((row, y) => {
    for (let x = 0; x < row.length; x++) {
      const fill = PALETTE[row[x]!];
      if (fill) rects += `<rect x="${x}" y="${y}" width="1" height="1" fill="${fill}"/>`;
    }
  });
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 9 9" shape-rendering="crispEdges">${rects}</svg>`;
}

/** `url("data:…")` values for each kind, ready for `background-image`. */
export const FRUIT_ART: Record<FruitKind, string> = Object.fromEntries(
  (Object.keys(SPRITES) as FruitKind[]).map((k) => [k, `url("data:image/svg+xml,${encodeURIComponent(svg(SPRITES[k]))}")`]),
) as Record<FruitKind, string>;
