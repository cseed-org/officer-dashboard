# Motion/performance evidence

Production Astro build served by the local Go dev binary, seeded director account.
Each of Home, Events, Members, Finance, Design and Officers has four captures:
1440px and 390px width, light and dark, height 900px with full-page capture.
Screenshots follow the Search open/close measurement; focus outlines may remain.
Cloudflare is intentionally unconfigured.

See [the report](../../motion-performance-v05.md) and [three-run comparison](comparison.md).
`before.json` / `after.json` contain the revised three-run main/final probe, medians,
LCP candidate elements and layout-shift sources. `original-pr.json` compares the original
runtime commit; `without-added-fades.json` is a CSS-only diagnostic ablation.
`paintable-before-members-fix.json` records the intermediate paintable-motion variant
that exposed Members' connection-notice layout shift. These replace the initial
single-sample report; see the report for why its input timing was unreliable.

`api-before.txt` / `api-after.txt` retain the original authenticated handler measurements:
API query code is unchanged by the review revision. The overview diagram explains
static delivery and is embedded in the report. Its editable Excalidraw source remains
outside the repository under `/tmp/codex-pr-diagrams/motion-perf/`.

The four named `motion-*.webm` recordings show Events/drawer, Finance tabs and Members
at normal speed on Chromium, separate from the throttled performance probe.
