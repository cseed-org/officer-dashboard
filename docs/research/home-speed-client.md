# Home speed: client and rendering (Research A)

Goal: Home median LCP from 1.964s to ≤1.5s on the Pi probe profile, with CLS < 0.05, V0.5 behaviour and the #43 CSP unchanged. Research only. No source changes. Scratch files are in `tmp/research-a/`.

## What the page does today (built `web/dist`, branch v06-home-speed @ 30b1a4b)

- `web/src/pages/index.astro:7` mounts one `client:load` island (`HomePage`). `AppLayout.astro` adds two more `client:load` islands, `UserChip` and `CommandPalette`. The static HTML for main is only a skeleton.
- **LCP element:** the `kpi-budget` sub-line (`<span class="kpi-sub">$591 spent · …</span>`, `HomeView.tsx:97-99`). It cannot paint until `/api/overview` resolves, because `HomeView.tsx:74-76` holds the whole grid back until overview loads (the V0.5 CLS fix).
- **Request chain (all serial):** HTML → CSS → astro-island loader → 3 levels of ES-module imports, with no `modulepreload` in `dist/index.html` → JS eval and hydrate → `/api/me` (`session.ts:27`) → `initTimezone` → `/api/me/prefs` + `/api/club/settings` (`timezone.ts:33`) → Gate renders → `useApi('overview')` (`HomeView.tsx:69`) → render. That is three API round trips after all JS has run.
- **Logos:** `AppLayout.astro` loads `cseed-logo.png` and `cseed-logo-white.png`. Each is **640×147, 37KB and 32KB**, but they display at 78×18. The theme-hidden variant is `display:none` (`components.css:336-338`) and still downloads, because non-lazy hidden images are always fetched ([phpied](https://www.phpied.com/?p=2094)). That is 69KB on a 200KB/s link, competing with the JS.
- **Bundle:** Home loads 20 JS files, 233KB raw / **77KB gzip** (level 9). Sizes below are raw / gzip:
  - `session.CRI-c17b.js`: 134KB / **38KB**. This is zod 4 classic plus `contracts.ts`'s ~344 schemas, plus client, timezone and unsaved. I rebuilt `contracts.ts` alone with rolldown: 137KB / 36.6KB. A minimal zod-classic program is 24KB gzip; the same with `zod/mini` is 6.4KB. The rest of the chunk is schema definitions.
  - `HomeView`: 44KB / 15KB.
  - `preact`: 11KB / 4.7KB.
  - Everything else is under 3KB gzip each.
  - CSS: `AppLayout.css` 72KB / 15KB and `HomeView.css` 18KB / 4.3KB. Both are render-blocking.
  - HTML: 20KB / 5.5KB.
  - Fonts: no font requests on Home. Every `@font-face` uses `swap`. Fonts are not on the critical path.
  - View transitions: no `@view-transition` remains in `web/src/styles`. Nothing to gain.
- **Main-thread profile** (CDP sampling, 3 loads, variant D below, 4× CPU): `session` chunk 471ms self time (schema construction and parse), `preact` 373ms, `format` module top level 279ms (module-scope `Intl.NumberFormat`s, `format.ts:3,20`), HomeView 258ms (`useLayoutEffect` FLIP `getBoundingClientRect` on every widget, even on first mount, `WidgetGrid.tsx:100-111`), Gate chunk 227ms (`Async` layout-effect `getBoundingClientRect`, `states.tsx:90`, a forced layout), calendar 111ms. The absolute numbers are inflated by host load; use the proportions.
- **CSP (#43)** `server/internal/web/csp.go:22-60`: `script-src 'self'` plus the sha256 of **every inline `<script>` in the served HTML file, computed from the file itself and cached per size/mtime**. Any static `is:inline` script added at build time is allowed automatically, with no Go change. `connect-src 'self'` allows same-origin fetch. `<link rel=modulepreload>` and `<link rel=preload>` to same-origin files are allowed by `'self'`. Inline JSON data (`type=application/json`) is not script, so it is fine too, but there is no SSR, so there is nothing to inline.

## Measurements

Chromium on the Pi, Go dev binary (`-tags dev`, freshly seeded DB) serving copies of the built dist. Profile: 1440×900, 4× CPU, 40ms latency, 1.6Mbit/s down / 750Kbit/s up, cache disabled. Director account, tour seen. This is the same profile as `web/scripts/performance.mjs`, measured with my own probe, `tmp/research-a/probe.mjs`.

**Caveat:** the Pi load average was 6–8 during these runs because other agents were running tests. Absolute values sit about 300ms above the doc's 1.964s, and spread is large. The four variants ran **interleaved** (A,B,C,D,A,B,…), **5 runs each**, so they share the same noise. Compare the deltas, not the absolutes.

| Variant (cumulative) | LCP median | min–max | `/api/overview` responseEnd median |
|---|---:|---:|---:|
| A: current build | 2264 | 2012–4040 | 1964 |
| B: A + logos resized to 156×36 PNG (6.6KB + 5.4KB) | 1956 | 1860–2524 | 1699 |
| C: B + `<link rel=modulepreload>` for all 19 Home modules | 1936 | 1716–2360 | 1597 |
| D: C + inline head script that starts `/api/me`, `/api/me/prefs`, `/api/club/settings`, `/api/overview` at parse time | 1804 | 1640–3484 | **825** |

In variant D, every byte has arrived by about 1000ms; the last is the 45KB `session` chunk. After that, about 600ms of main-thread work runs before HomeView's second wave of fetches, and LCP follows about 200ms later. **The page is now CPU-bound.** The rest of the route to 1.5s is less JS to download and evaluate, and less first-render work.

In C, modulepreload placed ahead of the stylesheets pushed FCP from ~400 to ~770ms by competing with CSS. Put the preloads **after** the CSS links.

Raw output: `tmp/research-a/out-interleaved.txt`. Waterfalls for each variant are at the end of that file.

## Ranked ideas

| # | Idea | Home LCP gain (rough) | Effort | Risk |
|---|---|---:|---|---|
| 1 | Shrink the logos to 2× (156×36), or one inline SVG; stop downloading the hidden theme variant | **~250–300ms** (measured) | XS | Very low (visual check, light and dark) |
| 2 | Start Home's critical API calls at HTML parse (inline head script with a promise map) so they run in parallel instead of me → prefs/club → overview | **~100–150ms** now (measured); more once CPU drops, since overview already lands at 825ms | S | Low–med (401/onboarding/abort semantics; must stay one-shot) |
| 3 | Move zod + schemas off the critical path: validate after first render, or behind a dynamic import; keep a small `{name: {method, path}}` table for `api()` | **~150–300ms** (est.: about 33KB gzip ≈ 165ms on the 1.6Mbit link, plus most of the session chunk's eval cost) | M | Med (ContractError timing changes; CI contract-check still covers drift) |
| 4 | `modulepreload` of the island graph, placed after the CSS links | ~20–100ms (measured −20 LCP, −100 overview) | XS–S (small post-build step, or Astro/Vite config) | Low |
| 5 | Cheaper first render: skip FLIP `getBoundingClientRect` when `rects` is empty (`WidgetGrid.tsx:100`), avoid the forced layout in `Async` on mount, render the below-fold Calendar body after LCP (keep its block box, so no CLS) | ~50–150ms (est. from profile) | S | Low–med (FLIP and tour tests) |
| 6 | Migrate `contracts.ts` to `zod/mini` | ~80–120ms (24→6KB core gzip) | L (344 schemas, chained API) | Med |
| 7 | `CommandPalette` as `client:idle`; lazy-create `Intl` formatters in `format.ts` | ~20–50ms | XS | Low (probe clicks Search after idle) |
| 8 | Split `AppLayout.css` (72KB, every page's rules) so Home loads only its own | ~30–60ms | M | Med (style regressions) |
| — | Server islands (`server:defer`) | n/a: requires an Astro on-demand adapter ([docs](https://docs.astro.build/en/guides/server-islands/)); this is static output served by Go | — | — |
| — | One island per widget | ~0 for LCP; drag/resize/tour share WidgetGrid state | L | High |
| — | 103 Early Hints from Go | ~0 locally: Chromium ignores 103 over HTTP/1.1 ([Chromium doc](https://chromium.googlesource.com/chromium/src/+/d4afc97b7/docs/early-hints.md), [intent to ship](https://groups.google.com/a/chromium.org/g/blink-dev/c/8iIvkmZNUhU/m/c6a7OBvFAgAJ)); HTML TTFB is already ~140ms | S | Low |
| — | Fonts and view transitions | 0: no font requests on Home; no `@view-transition` left | — | — |

The top 4 together are estimated at about 2.0 → 1.4–1.5s on a quiet Pi. 1 + 2 + 4 alone measured −460ms under load. Re-measure after each step with `web/scripts/performance.mjs` (`PERF_ROUTES=/`, `PERF_RUNS≥5`) when the Pi is idle.

## Top 3: how to implement here

**1. Logos (do first, XS).**
- Replace `web/public/images/cseed-logo*.png` with 156×36 versions. PIL Lanczos + optimize gave 6.6KB and 5.4KB; WebP or an SVG would be smaller.
- Better: one SVG wordmark using `fill: currentColor`, used once, so the theme switch costs nothing.
- If the two PNGs stay, add `loading="lazy"` to both: a lazy `display:none` image isn't fetched in Chrome or Safari ([phpied](https://www.phpied.com/?p=2094), [Hoizey](https://nicolas-hoizey.com/notes/2022/10/14/1/)). Lazy-loading the visible top-left logo is harmless at this size, but resizing is the dependable fix.
- Keep `width`/`height` attributes for CLS. Check the light and dark screenshots.

**2. Parallel critical fetches (S).**
- In `index.astro` only (pass a prop or slot to `AppLayout` head), add an `is:inline` script before the stylesheets: `window.__cseedEarly = Object.fromEntries(['/api/me','/api/me/prefs','/api/club/settings','/api/overview'].map(u => [u, fetch(u, {credentials:'same-origin', headers:{accept:'application/json'}})]))`.
- In `lib/api/client.ts` `api()`: for a GET with no body whose built URL is in the map, take the promise once (`delete` it), and use it in place of `fetch(url, init)`. All existing `res.ok`, 401 → `redirectToLogin`, onboarding and `validate` handling stays unchanged.
- Do not monkeypatch `window.fetch`; my prototype did that only to measure.
- Ignore the AbortSignal for the taken promise; abort only drops the result.
- CSP: the hash is picked up automatically (`csp.go:44-60`). Add a CSP browser check that Home has no violations.
- `<link rel=preload as=fetch crossorigin>` is the declarative alternative, but its credentials/mode matching against `api()`'s request is easier to get wrong; the inline map is deterministic.
- Keep the "one preference read" test green: the early `/api/me/prefs` is that one read, consumed by `refreshTimezone`.

**3. Zod off the critical path (M).**
- Add `lib/endpoints.ts`: a plain `{name: {method, path}}` table. Test that it equals `ENDPOINTS` minus the schemas, or generate it. `client.ts` imports types only from `contracts.ts` (`import type`).
- `validate()`: return the data at once. Then `import('../contracts')` inside `requestIdleCallback`/`setTimeout`, run `safeParse`, and on failure `console.error` plus raise the same `ContractError` through a small event that pages show as an error state.
- Alternatively, keep synchronous validation for mutations and async validation for GETs.
- Check the other runtime importers of `contracts.ts` (enums and constants used by `domain.ts`/`format.ts`), or they pull zod back into the critical chunk. Inspect `dist/_astro` after the build: the Home graph should have no chunk containing `safeParse`.
- `scripts/contract-check.mjs` in CI still catches drift. State the change in `docs/`.

## Sources

- Astro server islands require an adapter: https://docs.astro.build/en/guides/server-islands/
- Chromium Early Hints, HTTP/2+ only: https://chromium.googlesource.com/chromium/src/+/d4afc97b7/docs/early-hints.md and https://groups.google.com/a/chromium.org/g/blink-dev/c/8iIvkmZNUhU/m/c6a7OBvFAgAJ
- Hidden and lazy image fetching: https://www.phpied.com/?p=2094 and https://nicolas-hoizey.com/notes/2022/10/14/1/ (community tests; they disagree on edge cases, so verify in this Chromium)
- Zod 4 classic vs mini size: https://zod.nodejs.cn/packages/mini (mirror of zod.dev). Local rolldown measurement: classic 24KB vs mini 6.4KB gzip for a small schema set.

**Confidence:** high on the chain and bundle facts, and on the direction of 1, 2 and 4 (measured). Medium on the size of the gains: the host was loaded, 5 runs each. The gain for 3 is an estimate.

**Open questions:** is production HTTP/2 behind a proxy (that would make Early Hints viable)? Does anything need synchronous ContractError on GET responses?
