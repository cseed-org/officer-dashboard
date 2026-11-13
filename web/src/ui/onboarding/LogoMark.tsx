/**
 * The CSEED sprout from `public/favicon.svg`, without its paper tile so it sits on either
 * theme. The intro's snake turns into this mark and the onboarding header renders the same
 * component at the spot it lands (`data-onb-logo`), so the hand-off is pixel-exact.
 */
export function LogoMark({ size = 28, ...rest }: { size?: number; class?: string; [k: string]: unknown }) {
  return (
    <svg xmlns="http://www.w3.org/2000/svg" viewBox="4 4 56 56" width={size} height={size} aria-hidden="true" focusable="false" {...rest}>
      <g transform="translate(0 -2)">
        <g fill="#82c61b">
          <path d="M32 37C24 38 8 36 7 24C6 15 16 12 23 16C29 20 32 29 32 37Z" />
          <path d="M32 37C40 38 56 36 57 24C58 15 48 12 41 16C35 20 32 29 32 37Z" />
        </g>
        <g fill="none" stroke="#358b20" stroke-linecap="round">
          <path d="M15 23Q25 27 32 36M49 23Q39 27 32 36" stroke-width="3.5" />
          <path d="M32 36V53" stroke-width="5" />
        </g>
      </g>
    </svg>
  );
}
