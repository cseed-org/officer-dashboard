# Research B: Home data path and server (V0.6 home speed)

Goal: median Home LCP 1.964s → ≤1.5s on the Pi probe profile (4× CPU, 40ms, 1.6Mbit/s down, cache
off; `web/scripts/performance.mjs`), with CLS <0.05, V0.5 behaviour and the #43 CSP unchanged.
This note is read-only research; prototypes ran on copies of `web/dist` under `tmp/research-b/`.

## Method

- Build `web/dist` from this branch (11:03). Dev Go binary seeded into `tmp/research-b/dev*.db`,
  served on ports 18210–18215 (stopped afterwards).
- `tmp/research-b/ab.mjs`: the same context setup, throttling, settle and LCP/CLS observers as
  `performance.mjs`, plus a CDP network log and long tasks. Variants are **interleaved run by
  run**, because other agents ran browsers/Go/npm builds on the Pi at the same time (load avg 3–8).
- **Caveat:** absolute values drift by ±500ms with Pi load. Use the interleaved deltas and the
  network-side timings, which are much less load-sensitive, rather than single LCP figures.

## Measured Home waterfall (baseline, 3 runs, director, tutorial seen)

Times in ms from navigation start (run 1; runs 2–3 within ~±60ms until `/api/me`):

| start | end | request | note |
| ---: | ---: | --- | --- |
| 0 | 75 | `GET /` (7.1KB gz) | |
| 67 | 435 | AppLayout.css 21KB, HomeView.css 6KB | CSS finishes late: bandwidth contention ↓ |
| 69 | 705 | `cseed-logo.png` 37KB + `cseed-logo-white.png` 33KB | 640×147 PNGs shown at 78×18; both always load |
| 504–658 | 565–1187 | level 1–2 JS (UserChip, client, HomePage, **session.js 45KB gz**, HomeView 18KB, preact…) | islands start only after CSS: the inline island loader waits for stylesheets |
| 982 | 1145 | level 3 JS (calendar, useApi, compat, registry, kit, columns) | third import level |
| ~1200 | ~1600 | — | **~560–650ms long task**: module evaluation + hydration (4× CPU) |
| 1597 | 1646 | `/api/me` | `session.ts:27` |
| 1667 | 1727 | `/api/me/prefs` ‖ `/api/club/settings` | `initTimezone` awaited inside `loadMe` (`session.ts:29`, `timezone.ts:33`) |
| 1789 | 1860 | `/api/overview` ‖ `/api/cloudflare/widgets` | `HomeView.tsx:47-54`: the grid is not committed until overview resolves |
| ~1860 | **2176** | — | grid render; **LCP = `.kpi-sub` budget text** (needs overview) |
| 2103 | 2310 | club/settings (**duplicate**), calendar-widget, calendar/feed, finance/summary, requests, audit | after LCP; `LinksWidget.tsx:7` refetches the club settings the timezone gate already holds |

LCP medians in this first sequence were 2.18/2.28/2.71s, with CLS 0.0056. The serial data chain
`/api/me` → prefs/club → overview costs **~260ms** of the critical path. JS arrival plus CPU costs
about 1.6s. No timezone `PUT` occurred (headless timezone matched the stored prefs). On a machine
where it doesn't match, the chain gains a 4th round trip (`timezone.ts:35`).

## Server timings (not the bottleneck)

`BENCH_LATENCY=1 go test -count=1 -run LatencyReport -v ./internal/api` (load ≈5): `/api/me` p50
0.12 / p95 0.33ms; `/api/overview` p50 2.33 / p95 7.21ms; `/api/finance/summary` p95 4.27ms.
HTTP time-to-first-byte via curl, 60 requests each (all Home endpoints): every p95 ≤5.5ms
(overview 2.9/5.5ms). Server work is <3% of the chain; the cost is round trips + client
fetch/Zod/render. **Parallel Go sub-queries for a bootstrap endpoint would save ~2–5ms: not
worth the complexity.**

EXPLAIN QUERY PLAN (seed copy): session lookup, invites, pending requests (`status` index), audit
(`audit_created_idx`) and settlement dates use indexes. Two queries scale with data size:
`ReportLines` (`store/catalog.go:156`) runs `SCAN contributions` as a correlated subquery per
journal line (no `contributions(journal_entry_id)` index), and overview loads the *whole* ledger,
all requests and all guests (`EventCounts` full scan + temp B-tree) on every Home load
(`api/finance.go:691`). These are fine at seed size (18 lines, 319 guests); add the index when the
ledger grows. They are not an LCP lever today.

## Prototypes (interleaved A/B, director, probe profile)

Quietest batch (load ≈3, 5 runs each, medians):

| Variant (copy of dist / proxy) | Median LCP | Δ vs base | CLS |
| --- | ---: | ---: | ---: |
| Baseline | 2.016s (1.972–2.172) | — | 0.0049 |
| A: `<link rel=preload as=fetch crossorigin=anonymous>` for me, prefs, club, overview, cloudflare | 1.916s (1.888–2.224) | −100ms | 0.0049 |
| B: inline JSON bootstrap (`<script type="application/json">` injected per request + fetch shim) | 1.860s (1.828–2.352) | −156ms | 0.0046 |
| A + logos resized to 156×36 (37+33KB → 7+5KB) | 1.816s (1.736–3.012) | −200ms | 0.0049 |

A noisier 7-run batch (load 5–8) kept the order. Its best runs were base 2.108s, A+logos 1.792s,
and A+logos+`modulepreload` of all 20 Home chunks 1.604s. Modulepreload of *all* chunks finished
the JS ~240ms earlier (997 vs 1237ms) but delayed CSS 440→745ms and FCP. Preload only the
critical chunks.

Verified: preloaded responses **are consumed** by `api()` (no duplicate requests, even with the
DevTools cache disabled). Credentials match because `crossorigin=anonymous` ⇒ `same-origin`
credentials, the same as `client.ts:96`. The inline JSON block raised **no CSP violations**.
Under #43 the page policy hashes only inline scripts present in the built file
(`web/csp.go:45`). A `type="application/json"` data block is never executed, so script-src does
not apply ([HTML spec: data blocks](https://html.spec.whatwg.org/multipage/scripting.html#data-block);
[MDN preload `as=fetch` needs crossorigin](https://developer.mozilla.org/en-US/docs/Web/HTML/Attributes/rel/preload#cors-enabled_fetches)).

**Conclusion:** removing the data round trips is worth **~100–160ms**. Data alone cannot reach
1.5s, because the remaining ~1.2s is JS arrival + a ~600ms module-eval/hydration task + a
~300–500ms grid render at 4× CPU. That share belongs to research A (JS); the data-path work
should land with it.

## Ranked ideas (Home LCP gain is rough, from the measurements above)

| # | Idea | Gain | Effort | Risk |
| --- | --- | ---: | --- | --- |
| 1 | Resize/serve logos at display size (2× PNG ~6KB or SVG), so CSS and islands start sooner | 100–250ms | XS | very low |
| 2 | Preload Home data in `<head>` (`rel=preload as=fetch`), no server change | ~100ms | S | low |
| 3 | `modulepreload` the critical island chunks only (session, HomeView, Gate, preact, client, hooks) | 100–250ms | S | low (watch CSS/FCP) |
| 4 | Inline per-request JSON bootstrap from the Go static handler (supersedes 2) | ~150ms (+ removes 5 requests) | M | medium (per-user HTML, gzip, caching) |
| 5 | Drop the duplicate `/api/club/settings` in LinksWidget; use the snapshot (`LinksWidget.tsx:7`) | 0 LCP, −1 request | XS | low |
| 6 | Startup gzip at level 9 instead of `BestSpeed` (`web/web.go:207`): −13KB on Home (session.js 43.3→38.1KB) | 25–60ms | XS | very low (one-time startup CPU) |
| 7 | Precompressed Brotli at build (Node `zlib` q11), served on `Accept-Encoding: br`: −25KB vs today | 50–120ms | M | medium (new build→deploy contract; V0.5 declined it) |
| 8 | `/api/me` includes prefs + club snapshot (one bootstrap endpoint, no HTML change) | ~50–60ms | M | medium (contract change) |
| 9 | gzip API JSON (overview 3.9KB) | ~10ms | S | low (BREACH: no secrets mixed with reflected input, but low value) |
| 10 | ETag/304 on API reads | ≈0 in probe (cache off; still a round trip) | M | low |
| 11 | Parallel Go sub-queries in a bootstrap handler | 2–5ms | M | low |
| 12 | `contributions(journal_entry_id)` index; scoped overview queries | 0 now; scaling | S | low |

## Top 3 recommended (data/server side), concretely

**1. Logos at display size (static bytes).** Replace `/images/cseed-logo{,-white}.png` (640×147)
with 156×36 versions (prototype: 6.6KB + 5.4KB) or one inline/`currentColor` SVG
(`web/src/layouts/AppLayout.astro:105-106,120-121`). Keep `width/height` (no CLS). This frees
~57KB in the first 700ms, so CSS finishes earlier, and the island loader and FCP follow. Check:
screenshots light/dark at both widths.

**2. Get Home's first data before JS asks for it.** Do (a) now and (b) if A's JS work leaves the
data chain on the critical path.
- (a) In `AppLayout.astro` `<head>`, right after `<title>`, add
  `<link rel="preload" as="fetch" crossorigin="anonymous" href="/api/me">` plus `/api/me/prefs`
  and `/api/club/settings`. For `page="home"` also add `/api/overview` and `/api/cloudflare/widgets`.
  The URLs must match `endpointUrl()` exactly. `api()` stays unchanged, so Zod validation, 401 →
  login and the timezone PUT still apply. No CSP change. Cost: ~6KB of early requests, plus a
  harmless "preload not used" warning on logged-out loads.
- (b) Inline bootstrap: add `Options.Bootstrap func(*http.Request, name string) []byte` to
  `web.Handler`. The API server supplies it, using the principal that `withAuth` already attaches
  to page requests (`api/session.go:91`). For `index.html` with a session, render me + prefs +
  club + overview + cloudflare into `<script type="application/json" id="cseed-boot">`, replacing
  a `<!--cseed-boot-->` marker in AppLayout. Encode with `json.Marshal` (it escapes `<>&`). Do
  **not** use `writeJSON`, which sets `SetEscapeHTML(false)` (`api/errors.go:48`). Also: gzip per
  request, drop the asset ETag, send `Cache-Control: private, no-store` and `Vary: Cookie`, leave
  the CSP untouched (no new executable inline script). Client: `api()` checks a one-shot map from
  the data block for its URL (GET only, deleted after first use) and still runs `validate()`. In
  the prototype the proxy took ~5–60ms to build the block, which delays HTML TTFB by that much.
  Keep it under ~10ms by reading in one `db.Read()` and leaving out slow parts (overview p95 7ms).

**3. Make the remaining requests cheap and fewer.** Gzip level 9 at startup (`web.go:207`,
`gzip.BestCompression`; the startup cost happens once per binary/rebuild). Remove the duplicate
club-settings fetch (`LinksWidget.tsx:7`: use `timezoneSnapshot().club` and refetch only on
focus). Optionally fold prefs + club into `/api/me` (idea 8) if (b) is not done. Pair with
research A's critical-chunk `modulepreload`. The measured best case for logos + data preload +
modulepreload was **1.60s** under load, so ≤1.5s on a quiet Pi looks reachable only together
with A's eval/hydration cut (session.js is 134KB raw with the full Zod contract).

## Not recommended now

Bootstrap endpoint with Go goroutines (server time is ~3ms); API ETags (probe disables cache; a
304 still costs the RTT); Brotli before gzip -9 and logo fixes are measured (it adds a build
artifact contract that V0.5 deliberately avoided).

Raw data: `tmp/research-b/waterfall-base.json`, `ab2.json` (quiet 4-way), `ab5.json` (noisy
7-run), prototype dists `tmp/research-b/dist-*`, proxy `tmp/research-b/proxy.mjs`.
