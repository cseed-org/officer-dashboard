# V0.5 motion and performance draft

This slice makes navigation, content arrival, and modal dismissal consistent while reducing static transfer cost on the Raspberry Pi. It preserves full-page navigation and the existing authentication/island lifecycle. Home drag/resize and Buildspace internals are outside this change.

![Before and after static delivery](evidence/motion-perf/overview.png)

## Review revision: cause and fix

The first report's single samples were unreliable: it opened Search after `networkidle`, which can occur before deferred islands/data finish. First input ends LCP observation. CLS continues, but the probe excludes entries flagged by the browser as associated with recent input. The revised probe disables the browser cache, waits for island hydration, all busy indicators, network idle, and a further 750ms (including main's 600ms count-up) **before the first input**. It records the LCP element and every layout-shift source, not just a total.

The four primary comparison variants are main `ee0e7b2`, original implementation `08a9a75`, that implementation with only its added fades/view transitions disabled, and final runtime `58c9a79`. Commit `c33254d` changes documentation only, so its application code is identical to `08a9a75`. Each was measured three times on all six pages. Main was built from `git archive` into an isolated directory. The diagnostic variant disables only the original PR's added ready/row fades and native view transitions, retaining its JS and gzip; all three samples are retained. Disabling those effects did not remove Home's shift. Cross-document transitions cannot explain a cold navigation from a fresh context in any case.

Home's shift sources identify shared grid tracks growing after overview resolves: the KPI row grows about 35px and the two-row recent-events tile grows from 326px to 498px, moving lower widgets about 207px. The settled three-run baseline reproduces this on main too (CLS 0.0719 on all three runs). The earlier 0.0005 baseline missed that phase; it was not evidence of a branch-only regression. We nevertheless fix the real shift here:

- Home waits for its primary overview before mounting the grid, so final stats and the full table establish tracks together. Initial loading is accessible, and subsequent retries do not unmount an established layout.
- WidgetGrid initializes from the preferences already read by the auth/timezone gate, avoiding a second preference request and intermediate loading grid. Its drag/resize implementation is unchanged. Existing layout/editing/migration tests verify this boundary.
- Sidebar footer rows reserve the signed-in user slot before streaming/hydration, removing its smaller loading shift.

Members already had a two-stage connection-status/notice insertion. Nonzero-opacity entry made that existing change visible and measurable during loading: connection status changes width and the disconnected notice inserts above the painted directory. Members now shows its initial loading state until the first response, then commits connection status, notices and directory together. Reloads retain existing data and the page stays interactive. This fixes the state boundary rather than hiding its layout movement with opacity zero.

## Motion behavior

Main-column content rises 8px and fades from **0.65 to 1** in 180ms. The shell does not animate. Full-document snapshots have been removed. Primary content, async content, and table/list/card entries start at **nonzero opacity** so they can participate in LCP immediately. First-frame browser checks verify that property. Stagger is limited to the first eight items (16ms apart, 120ms each, ending within 232ms); later items appear directly. Animation styles expire rather than overriding later drag/hover transforms.

Drawers slide from the viewport edge in 240ms with a fading backdrop; dialogs/palette scale in, and the existing transform-only tab indicator slides in 240ms. Supporting browsers retain native modal top-layer membership during close; older browsers close immediately. Focus/Escape behavior remains native. Reduced motion cancels the effects and removes outgoing skeletons immediately.

`Async` retains the loading height as a minimum and fades its inert, aria-hidden skeleton out as an absolute overlay for 180ms, while paintable ready content owns layout. Removing the overlay does not resize the ready box. Events now uses this shared transition alongside Finance and Home's async widgets. A controlled delayed-response browser check verifies the box size before and after overlay removal; it does not claim arbitrary unknown response content can have its final height predicted before arrival.

KPI figures appear at their final value, avoiding frame-by-frame Preact updates and text layout. Skeleton shimmer uses transform without a blur filter.

## Repeatable client measurements

Serve each production Astro build with the seeded Go dev binary, then from `web/`:

```sh
WEB_ORIGIN=http://127.0.0.1:18135 PERF_RUNS=3 PERF_SHOTS=1 \
  PERF_OUT=tmp/verify/motion node scripts/performance.mjs
```

Omit `PERF_SHOTS=1` for measurement only. Each page/run gets a fresh authenticated browser context; authentication happens through the request context, outside page rendering. Chromium and Go both run on the Raspberry Pi: 1440×900, 4× CPU slowdown, 40ms network latency, 1.6Mbit/s down / 750Kbit/s up. This is reproducible stress testing, not a calibrated mid-tier phone. Runs execute serially, without our builds/tests in parallel. Other workstation activity is uncontrolled, so all raw samples and ranges are retained.

Each table cell is the **median of three samples**. Search is the maximum Event Timing duration from one open and one Escape close per sample, **not field INP**. CLS sums non-input shifts in this short observation period, rather than computing long-session windows. JS bytes are encoded response-body bytes, excluding headers. The 2s / 200ms / 0.05 targets come from the V0.5 brief.

| Page | Main median LCP (range) | Final median LCP (range) | CLS main → final | Search median main → final | JS bytes main → final |
| --- | ---: | ---: | ---: | ---: | ---: |
| / | 3.064s (3.064–3.344) | 1.800s (1.792–1.808) | 0.0719 → 0.0000 | 104ms → 80ms | 201,728 → 75,218 |
| /events | 2.992s (2.936–3.048) | 1.808s (1.800–1.844) | 0.0005 → 0.0000 | 72ms → 56ms | 224,840 → 83,895 |
| /members | 2.600s (2.588–3.868) | 1.572s (1.572–1.616) | 0.0005 → 0.0000 | 64ms → 48ms | 180,102 → 66,480 |
| /finance | 0.948s (0.932–0.948) | 0.488s (0.488–0.500) | 0.0005 → 0.0000 | 96ms → 56ms | 237,913 → 86,967 |
| /design | 3.004s (3.000–3.072) | 1.832s (1.832–1.840) | 0.0005 → 0.0000 | 112ms → 80ms | 222,625 → 82,884 |
| /officers | 0.988s (0.980–1.008) | 0.504s (0.500–0.520) | 0.0005 → 0.0000 | 96ms → 72ms | 270,049 → 100,630 |

All six final medians improve on main, with median LCP below 2s, zero CLS, and the sampled Search interaction below 200ms. This is lab evidence for these routes/states, not field INP or a guarantee for every dataset/device. JavaScript transfer is approximately 63% smaller.

Raw samples and sources: [main](evidence/motion-perf/before.json), [original PR](evidence/motion-perf/original-pr.json), [original PR without added fades](evidence/motion-perf/without-added-fades.json), [final](evidence/motion-perf/after.json). The [supplemental comparison](evidence/motion-perf/comparison.md) lists all three LCP/CLS values for every variant/page, so variance is visible.

## Server results

`BENCH_LATENCY=1 go test -count=1 -run LatencyReport -v ./internal/api` runs 2,000 authenticated requests per endpoint through session lookup, SQL and JSON using the seeded test harness. It is in-process, not an HTTP load test. The existing repeatable benchmark now also includes Members; use `go test -run '^$' -bench BenchmarkReadEndpoints -benchmem ./internal/api` for allocations.

| Endpoint | Before p95 | After p95 |
| --- | ---: | ---: |
| Home `/api/overview` | 13.39ms | 10.19ms |
| Events `/api/events` | 7.43ms | 2.13ms |
| Members `/api/members` | Not sampled | 2.67ms |
| Finance `/api/requests?scope=all` | 4.28ms | 0.63ms |
| Finance `/api/finance/summary` | 6.09ms | 1.14ms |

All sampled API p95 values are below 100ms. Query code is unchanged; these runs do not isolate the cause of the timing differences or demonstrate a SQL speedup. Raw reports: [before](evidence/motion-perf/api-before.txt), [after](evidence/motion-perf/api-after.txt). The small seed has six events and six purchase requests; Members may be disconnected/empty. These results do not establish large-directory scaling.

Query review: purchase lists load their reference catalog once outside the item loop; event metrics aggregate by event; Members uses count plus paginated reads. Existing indexes cover member join-date ordering and campus/grad-year filters, purchase requester/status, and event/email lookups. No speculative migration was added for the small seed. Larger connected-members and concurrent HTTP measurements remain follow-up work.

## Client audit and remaining work

Page islands are already separate Astro chunks, with small SVG charts rather than an external charting library. Auth-gated page islands, UserChip and Search use `client:load`; deferring them would delay required navigation/auth controls. Fonts use the system stack and logos have explicit dimensions. The shared contract/Zod runtime is a significant common payload; this slice preserves validation rather than removing it to reduce bytes.

The authentication gate waits for `/api/me`, then timezone initialization, before page data fetches. This preserves correct timezone display but creates a fetch chain worth profiling separately. Finance and Officers still eagerly import their secondary views; splitting those views is a potential next optimization. Do not interpret gzip transfer reductions as equivalent reductions in JavaScript parse work.

The repeatable lab interaction is not field INP; a representative-device/field check and larger connected-member/concurrent-HTTP workloads remain useful follow-up validation. Secondary Finance/Officers view splitting remains outside this revision.

Static serving retains startup gzip and weak content ETags for text files up to 2MiB, existing immutable hashed-asset/HTML revalidation policies, identity byte ranges, and live-disk fallback after a rebuild. It excludes API/session responses. Brotli was considered: Go has no standard-library encoder, and adding build-side encoded artifacts would introduce another deployment contract. Gzip already provides approximately 63% less JS transfer here; this revision keeps that simpler path and adds no compression dependency.

## Verification and visual evidence

Runtime commit `58c9a79`: Go tests (including dev-tag auth), Astro check/build (0 errors/warnings, 2 hints), and contract check (199 responses / 97 endpoints, 0 schema errors) passed. The combined Chromium motion, Home and Members suite passed 33 checks at desktop/phone widths; 9 viewport-specific checks were intentionally skipped. Go vet also passed. Motion checks cover reduced motion, transform-only tab movement, Escape dismissal, paintable first frames, the eight-item cap, stable skeleton overlay removal, and one-read/atomic Home loading. Desktop/phone Home regression checks also cover resize, add/remove, keyboard reorder, pointer drag, migration, and layout. Static-serving tests cover decompression, ETags/304, gzip rejection, HEAD, ranges, and rebuild fallback.

Screenshots are captured from the built site, not the Astro dev server. [Events desktop light](evidence/motion-perf/events-1440-light.png), [desktop dark](evidence/motion-perf/events-1440-dark.png), [phone light](evidence/motion-perf/events-390-light.png), [phone dark](evidence/motion-perf/events-390-dark.png). The evidence directory includes the same four captures for Home, Members, Finance, Design and Officers. Static screenshots establish layout and theme appearance; they do not prove animation timing.

Short Chromium recordings exercise Events entry/drawer open/close, Finance tab movement, and Members entry: [desktop light](evidence/motion-perf/motion-1440-light.webm), [desktop dark](evidence/motion-perf/motion-1440-dark.webm), [phone light](evidence/motion-perf/motion-390-light.webm), [phone dark](evidence/motion-perf/motion-390-dark.webm). These use normal CPU speed for visual review, separate from the throttled measurements. Firefox/Safari were not exercised.
