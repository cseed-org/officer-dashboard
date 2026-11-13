/**
 * The app shell's logo: the same `cseed-wordmark` images and `.wordmark-logo` classes as
 * `AppLayout.astro` (ink on light, white on dark, swapped by the theme rules in
 * components.css). `ShellLogo` is the onboarding page's copy of it, placed (see
 * `.onb-shell-logo` in v03.css) where the shell draws its own at desktop and phone widths,
 * and it is where the intro's snake lands (`data-onb-logo`). At 768–1099px the shell shows its C rail
 * mark instead, so that is what lands there. The intro overlay renders the same markup via `LogoImages`.
 */
export function LogoImages() {
  return (
    <>
      <img class="wordmark-logo wordmark-logo--ink" src="/images/cseed-wordmark.png" alt="" width="78" height="18" />
      <img class="wordmark-logo wordmark-logo--white" src="/images/cseed-wordmark-white.png" alt="" width="78" height="18" />
      {/* At 768–1099px the shell shows only its C rail mark instead of the wordmark. */}
      <span class="wordmark-mark" aria-hidden="true">
        C
      </span>
    </>
  );
}

export function ShellLogo() {
  return (
    <span class="onb-shell-logo" data-onb-logo role="img" aria-label="CSEED">
      <LogoImages />
    </span>
  );
}
