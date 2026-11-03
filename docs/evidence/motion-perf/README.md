# Motion/performance evidence

Production Astro build served by the local Go dev binary, seeded director account.
Each of Home, Events, Members, Finance, Design and Officers has four captures:
1440px and 390px width, light and dark, height 900px with full-page capture.
Screenshots were taken after exercising Search and closing it; focus outlines may remain.
The Cloudflare connection is intentionally unconfigured.

See [the report](../../motion-performance-v05.md) for measurement method and limitations.
`before.json` / `after.json` are the corrected cold-navigation probe measurements;
`api-before.txt` / `api-after.txt` are the authenticated handler latency reports.
The overview diagram explains the static delivery path and is embedded in that report.
Its editable Excalidraw working source remains outside the repository under
`/tmp/codex-pr-diagrams/motion-perf/`.
