# V0.6 Home speed

Home's median LCP on the Pi probe profile goes from **2.012s to 1.080s** (target ≤1.5s). Every other measured page is faster too, CLS is unchanged, and the Search interaction sample is unchanged. Drag/resize, the tour, the calendar, Links and the #43 CSP are untouched (no CSP policy change: the one new inline script is hashed like the others).

Research came first, in two read-only notes: [client and rendering](research/home-speed-client.md) and [data path and server](research/home-speed-data.md). Both found the same chain. Home's critical data waited on three API round trips (`/api/me` → prefs/club settings → overview). Those round trips started only after all island JavaScript had downloaded, one import level at a time, and been evaluated. About half of that JavaScript was the zod contract. Two 640×147 logos competed with all of it for the 1.6Mbit link. Server time is under 3% of the chain (overview p95 ~7ms), so no bootstrap endpoint or Go concurrency work was worth it.

## What changed

| Change | Where | Why |
| --- | --- | --- |
| Logos at 2× display size (156×36, 12KB for both instead of 69KB) | `web/public/images/cseed-wordmark*.png`, `AppLayout.astro` | Both theme variants always download; they were delaying CSS and JS. |
| Early API reads: an inline head script starts `/api/me`, prefs and club settings on every signed-in page, plus overview and Cloudflare on Home; `api()` takes each response once | `web/src/lib/early.ts`, `AppLayout.astro`, `api/client.ts` | Removes the serial round trips; the data is ready before the JavaScript is. Status handling, 401 → login, onboarding and validation are unchanged. |
| Links reuses the club settings the timezone gate already read | `LinksWidget.tsx` | One fewer request on Home. |
| zod contract off the first paint: `api()` sends from a generated method/path table; schemas load in the background | `integrations/endpoints.mjs`, `src/lib/endpoints.generated.ts`, `api/client.ts`, `Gate.tsx` | Home's `session` chunk drops from 134KB to 15KB raw. See "Contract validation" below. |
| `modulepreload` links for each page's island modules, after the stylesheets, at low priority | `integrations/modulepreload.mjs` | The browser fetches the whole island graph at once instead of level by level, and CSS keeps the bandwidth. High priority slowed first paint on shell-painted pages (Finance +50ms); low priority fixed that. |
| Static gzip at best compression (once, at startup) | `server/internal/web/web.go` | About 12% fewer JS/CSS bytes; startup stays under 100ms on the Pi. |
| The probe can interleave several builds run by run (`WEB_ORIGINS=a,b`) | `web/scripts/performance.mjs` | Other Panes share the Pi; alternating builds makes before/after comparable. |

Not done, from the ranked ideas: inline per-request JSON bootstrap from Go (it would save ~100ms more but needs per-user HTML and a new caching model), Brotli, `zod/mini`, splitting `AppLayout.css`, cheaper first-render FLIP measurement, and `client:idle` for the command palette. Each remains open if more budget is needed as widgets grow.

## Contract validation

Every response is still validated against its zod schema. What changed is when:

- **Writes** (non-GET) and **any read once the schemas have loaded** are validated before `api()` returns, exactly as before.
- **Reads that arrive before the schemas have loaded** (a page's first data) render at once and are validated when the schemas load, which `api()` starts at the first idle moment. A mismatch logs the issues and replaces the page with the same "did not match the API contract" error state, with a reload button.
- Responses are used as sent rather than as zod's parsed output. The response schemas that use defaults (`dashboardTutorialSeen`, club links) are always sent by the Go server, and `contract-check` in CI still checks every response.

`src/lib/endpoints.generated.ts` is rewritten from `ENDPOINTS` by every `npm run build` and `npm run dev`. Commit it with contract changes; `tests/home-speed.spec.ts` fails when the committed copy is stale. Endpoint entries must stay on one line as `name: { method: '…', path: '…', … }`, and the build stops with a message if one doesn't.

## Before and after

Chromium and Go on the Raspberry Pi, the existing probe profile: 1440×900, 4× CPU slowdown, 40ms latency, 1.6Mbit/s down / 750Kbit/s up, cache disabled, director, tour seen. `main` is `13a12d4` (#50 merged) and the branch is rebased on it. Both were served as production builds by seeded dev binaries and measured **interleaved run by run, 5 runs each**:

```sh
WEB_ORIGINS=http://127.0.0.1:18148,http://127.0.0.1:18147 PERF_RUNS=5 PERF_SHOTS=1 \
  PERF_ROUTES=/,/events,/members,/buildspace,/design,/finance,/officers \
  PERF_OUT=../docs/evidence/home-speed-v06 node scripts/performance.mjs
```

| Page | LCP main, median (range) | LCP branch, median (range) | Δ | CLS main → branch | Search main → branch | JS bytes main → branch |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| / (Home) | 2.012s (1.972–2.256) | **1.080s** (1.052–1.120) | −932ms | 0.0023 → 0.0023 | 128 → 120ms | 89,172 → 82,178 |
| /events | 1.876s (1.848–1.896) | 1.316s (1.292–1.352) | −560ms | 0 → 0 | 64 → 72ms | 86,127 → 79,219 |
| /members | 1.612s (1.596–1.620) | 0.860s (0.840–0.872) | −752ms | 0 → 0 | 56 → 64ms | 68,074 → 63,132 |
| /buildspace | 1.608s (1.592–1.632) | 1.088s (1.072–1.200) | −520ms | 0 → 0 | 160 → 152ms | 79,108 → 72,839 |
| /design | 1.888s (1.848–1.952) | 1.304s (1.272–1.308) | −584ms | 0 → 0 | 80 → 72ms | 84,621 → 77,809 |
| /finance | 0.496s (0.488–0.520) | 0.440s (0.416–0.456) | −56ms | 0 → 0 | 64 → 72ms | 91,452 → 83,460 |
| /officers | 0.520s (0.516–0.524) | 0.456s (0.448–0.484) | −64ms | 0 → 0 | 72 → 72ms | 114,312 → 103,644 |

No ranges overlap. JS bytes are encoded bytes, including the contract chunk that now loads after the first data. Search is the scripted Event Timing sample, not field INP; changes of ±8ms are within its 8ms resolution. Raw samples and LCP elements: [metrics.json](evidence/home-speed-v06/metrics.json).

### Step by step (Home)

Each step is cumulative on the pre-#49 base (`30b1a4b`), served from its own port and interleaved with the others, 5 runs each. Other Panes were running browser tests during these runs, so single-step LCP medians are noisy. The network-side times (when the overview response and the last JS file finished) are less sensitive to load.

| Step | LCP median | FCP | Overview finished | Last JS finished |
| --- | ---: | ---: | ---: | ---: |
| Base | 2.020s | 528ms | 1738ms | 1187ms |
| + logos at display size | 2.336s* | 424ms | 1930ms | 1099ms |
| + early API reads, no duplicate club settings | 1.880s | 452ms | **267ms** | 1122ms |
| + gzip best compression | 1.868s | 428ms | 265ms | 1055ms |
| + modulepreload (high priority) | 1.616s | 532ms | 769ms | **866ms** |
| + zod contract off the first paint | **1.116s** | 512ms | 643ms | 936ms |
| + modulepreload at low priority (separate run) | 1.088s vs 1.156s | 436ms vs 504ms | 236ms vs 642ms | 897ms vs 936ms |

\* A load spike: in a quiet two-way run the logo step alone measured 2.000s → 1.876s (FCP 504 → 400ms). Finance in the low-priority run: 544ms → 432ms (main 492ms).

## Validation

- `go vet ./...`, `go test ./...` and `go test -tags dev ./internal/api ./cmd/...` pass.
- `npm run build` and `astro check` (0 errors, 0 warnings, 1 existing hint) pass, and so does the contract check.
- Playwright against the Go server serving this build passed Home speed, motion, home grid (drag, resize, tour), CSP (every page and role), pages, Buildspace, events, members, search, roles, purchase deadlines, onboarding, settings, security, admin, dev mode, Officers, Design, lifecycle and import, at phone/tablet/desktop where they apply.
- New checks in `tests/home-speed.spec.ts`:
  - The endpoint table equals `ENDPOINTS` and the committed copy is current.
  - Early paths are real GET paths.
  - Home makes each critical read exactly once, from the head script.
  - A read rendered before the schemas loaded still shows the contract error.
  - Built pages preload their island modules after the CSS, without the contract.
- Fixed in passing: `search.spec.ts` looked for the first `option` anywhere on the page, which matched the Home calendar's view `<select>` after #49/#50 and failed on main. It now looks inside the palette.
- These fail on main too and are not touched here:
  - The unit test "URLs: stored links…" imports the root `src/core/contracts.ts`, which needs a root `npm ci`.
  - The unit test "V0.3 widget layouts migrate…" expects the pre-calendar default layout.
  - `officers.spec.ts` "invite → … → home" expects `/`, but onboarding now lands on `/?tutorial=onboarding`.
  - `speakers.spec.ts` "contacts: add, edit…" has a focus-return timing flake: 2 of 12 runs on main, 3 of 18 on this branch.

Screenshots of this build at 1440 and 390, light and dark, for every measured page are in [evidence/home-speed-v06](evidence/home-speed-v06/). For example: [Home desktop light](evidence/home-speed-v06/home-1440-light.png), [Home desktop dark](evidence/home-speed-v06/home-1440-dark.png), [Home phone light](evidence/home-speed-v06/home-390-light.png), [Home phone dark](evidence/home-speed-v06/home-390-dark.png).

These are lab samples on one device. They are not field data or a guarantee for larger datasets.
