/**
 * The cseed sapling: two leaves on a stem, the same drawing as `public/favicon.svg` and the
 * sapling in the `cseed-wordmark` images. It is the single-item brand mark (the rail, sign-in,
 * 404, the club-website link chip). The colours come from `--sapling-leaf` / `--sapling-stem`
 * in tokens.css, so it works in light and dark.
 *
 * The viewBox is square and centred on the painted sapling, which fills 90% of its width. The
 * hello intro relies on that to land its sprouted sapling exactly on the rail's mark.
 */
export const SAPLING_VIEWBOX = { x: 4, y: 4.75, size: 56 } as const;
/** The painted sapling's width as a share of the viewBox width. */
export const SAPLING_FILL = 50.6 / 56;

const LEAF_L = 'M32 37C24 38 8 36 7 24C6 15 16 12 23 16C29 20 32 29 32 37Z';
const LEAF_R = 'M32 37C40 38 56 36 57 24C58 15 48 12 41 16C35 20 32 29 32 37Z';
const VEINS = 'M15 23Q25 27 32 36M49 23Q39 27 32 36';
const STEM = 'M32 36V53';
const VB = `${SAPLING_VIEWBOX.x} ${SAPLING_VIEWBOX.y} ${SAPLING_VIEWBOX.size} ${SAPLING_VIEWBOX.size}`;

/** The sapling as an SVG string (for markup built outside Preact, like the onboarding ghost). */
export function saplingSvg(cls = 'sapling'): string {
  return (
    `<svg class="${cls}" viewBox="${VB}" aria-hidden="true" focusable="false">` +
    `<g transform="translate(0 -2)"><g style="fill:var(--sapling-leaf, #82c61b)"><path d="${LEAF_L}"/><path d="${LEAF_R}"/></g>` +
    `<g fill="none" stroke-linecap="round" style="stroke:var(--sapling-stem, #358b20)"><path d="${VEINS}" stroke-width="3.5"/><path d="${STEM}" stroke-width="5"/></g></g></svg>`
  );
}

export const SAPLING_PATHS = { LEAF_L, LEAF_R, VEINS, STEM, VB } as const;
