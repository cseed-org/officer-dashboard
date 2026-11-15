import { SAPLING_PATHS as P } from './saplingMarkup';

/**
 * The cseed sapling mark as an inline SVG (see saplingMarkup.ts). It renders from Astro pages too
 * (static, no hydration). It is decorative: put the accessible name on the link or heading around it.
 */
export function Sapling({ class: cls = 'sapling' }: { class?: string }) {
  return (
    <svg class={cls} viewBox={P.VB} aria-hidden="true" focusable="false">
      <g transform="translate(0 -2)">
        <g style={{ fill: 'var(--sapling-leaf, #82c61b)' }}>
          <path d={P.LEAF_L} />
          <path d={P.LEAF_R} />
        </g>
        <g fill="none" stroke-linecap="round" style={{ stroke: 'var(--sapling-stem, #358b20)' }}>
          <path d={P.VEINS} stroke-width="3.5" />
          <path d={P.STEM} stroke-width="5" />
        </g>
      </g>
    </svg>
  );
}
