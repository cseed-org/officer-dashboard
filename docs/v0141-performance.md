# V0.14.1 performance

Release-wide performance evidence for V0.14.1, with web bundle and client work in PR #90 and server results reserved for PR 4.

## Web (PR #90)

This change reduces first-load JavaScript on the heaviest pages and removes repeated client-side work on Home, Buildspace, Contacts and Settings. The largest first-load reductions are **55% on Launchpad, 49% on Finance, 45% on Buildspace and 34% on Onboarding**. Officers also loses 33.8 KB of first-load CSS. The improvements are in static import boundaries, data indexing, render isolation and deferred work.

### What changed

| Finding | Change | Expected effect | Measured |
| --- | --- | --- | --- |
| M-029 | Moved shared constants into zod-free modules in both contract copies. Buildspace, Finance and builder portal constants use those modules; request-save schemas load on demand through the shared loader, behind a synchronous busy guard. | Keeps the zod contract chunk off Buildspace, Finance and Launchpad first paint without changing schema validation or exports. | Route-byte reductions are below. Production import guards and RequestForm double-submit/import-failure tests cover behavior; not timed. |
| M-031 | Added a `WeakMap` Buildspace index for projects, people, mentors, activities, attendance and archived state. Cards, directory search and the drawer share it. | Replaces repeated full-array filters with indexed lookups; a new response builds a new index. | One scan per array when building each response index (proxy-count test), not timed. |
| M-084 | Split component-free widget metadata into `catalog.ts`; only Home imports the component registry. Audit log metadata no longer imports Home. Officers therefore no longer loads Home's `widget.css`: its user-chip avatar changes from the leaked 24 px rule to the app-wide 28 px style used by every other non-Home page. | Removes Home widgets and their CSS from Officers and Onboarding. | Officers loses 69,218 raw JS bytes and 33,775 raw CSS bytes; Onboarding loses 50,834 raw JS bytes. Production import guards cover the removed chunks; not timed. |
| M-086 | Buckets calendar items once per feed/range/zone and uses a source `Map`. | Avoids filtering the whole feed and formatting end dates separately for every day cell. Multi-day and exclusive-midnight behavior is unchanged. | Exactly 180 end-date conversions for the 200-item equivalence fixture (test), not timed. |
| M-088 | Memoized widget bodies separately from tile positioning. | Pointer movement updates tile styles without rerendering unchanged widget bodies. | Zero Links body renders over a 20-step pointer drag (browser test), not timed. |
| M-138 | Reused the cached zoned formatter for today's date and wall-clock-to-ISO conversion. | Avoids constructing identical `Intl.DateTimeFormat` instances on repeated renders. | One formatter construction across repeated calls in a zone; the cache follows time-zone changes (test), not timed. |
| M-140 | Cached devices reveal the shell immediately, including devices that kept the default width. Only a new or cleared device waits, now capped at 300 ms. | A stalled preferences request cannot hide a returning user's shell for the old 1.5 s cap. Late widths apply on the next navigation. | Five cached/stalled sidebar browser cases passed, including visibility before the stalled response is released; no timing improvement claimed. |
| M-141 | Moved linked-event loading into the contact editor. | `/api/events?limit=100&when=all` is absent from directory and deep-link reads and loads when Add/Manage opens. Cursor pagination is retained. | Browser request log confirms no events request before the editor opens; request timing not measured. |
| M-143 | Memoized the IANA zone list and isolated the 30-second clock. | Clock updates no longer rerender the whole timezone panel. | Memoization is code-reviewed; not timed. |

### Built bytes per route

The baseline is `main` at `d78ccd4`; the after build contains the code at `f5017b5`. Both used the unchanged `docker/dev.Dockerfile` web-build stage (Node 26) and the same `scripts/route-assets.mjs` static-closure tool. JS and CSS include the page's statically imported island modules and styles, excluding lazy-only chunks. Gzip uses zlib level 9, as the Go static server does.

| Route | JS raw, before → after | Change | JS gzip, before → after | CSS raw, before → after |
| --- | ---: | ---: | ---: | ---: |
| `/launchpad` | 253,604 → **113,658** | **−55.2%** | 81,270 → **43,731** | 105,493 → 105,493 |
| `/finance` | 287,049 → **146,479** | **−49.0%** | 91,993 → **54,239** | 87,212 → 87,212 |
| `/buildspace-launchpad` | 308,461 → **168,381** | **−45.4%** | 100,627 → **63,121** | 110,388 → 110,388 |
| `/onboarding` | 147,796 → **96,962** | **−34.4%** | 56,955 → **37,493** | 86,222 → 86,222 |
| `/officers` | 374,184 → **304,966** | **−18.5%** | 125,246 → **100,308** | 123,247 → **89,472** (−27.4%) |

Home is not a byte-reduction target in this PR: its static JS is 147,666 → 148,717 raw (+0.7%), while its changes target calendar and drag work after loading. Contacts is 107,770 → 108,453 raw (+0.6%); its improvement is removing the event-list request from directory reads, not reducing bytes. Other routes have small static-graph shifts; the complete before and after inventories are linked below.

Raw evidence: [before](evidence/v0141-performance/route-assets-before.json) · [after](evidence/v0141-performance/route-assets.json).

### Timing measurements

No before/after LCP comparison is claimed. The baseline worker could not run its Chromium probe because host free memory fell below the 1.5 GB safety floor. The after probe was also skipped: repeated checks after the Docker builds remained below that floor (about 1.17–1.28 GB), and the host had previously dropped by roughly 1 GB as soon as Chromium started. Running it would have produced a resource-starved measurement and triggered the baseline memory guard.

The release image itself built successfully with the baseline's `server/Dockerfile`. Go tests, vet checks and the API contract check are listed below.

### Validation

Existing and new checks run against this branch:

- `npx astro check`: **0 errors, 0 warnings, 9 existing hints**.
- `npm run build`: passed on the host; the matching Linux web build and release-image build also passed.
- `npx playwright test tests/unit.spec.ts tests/buildspace-model.spec.ts --project=desktop`: **38 passed**. These are pure tests and do not launch Chromium.
- Focused browser regressions passed for the 20-step drag render count, cached and stalled sidebar behavior (5 tests), deferred contact event loading, and timezone formatter caching. The fix-up desktop lifecycle and sidebar specs passed **17 tests**, with one phone-only case skipped, including RequestForm duplicate-submit and import-failure coverage.
- Production import-graph guards passed for Buildspace, Finance, Officers and Onboarding. The baseline HTML was also inspected directly and contains the contract/Home preloads that these guards reject.
- `go test ./...`, `go test -tags dev ./internal/api`, `go vet ./...` and `go vet -tags dev ./...`: passed in Docker/Go 1.24.
- `npm run test:scripts`: **29 passed, 1 existing Windows-only skip**, including the route-assets fixture. `node scripts/gen-mcp-tools.mjs --check`: generated MCP tools are current.
- API contract check: **299 responses, 139/146 endpoints exercised, 0 schema errors**.

New regression coverage includes aggregate equivalence and scan counts on 200 builders, 100 projects, 10 mentors and 10,000 activities; a 200-item calendar comparison with exactly 180 end conversions; a real pointer drag with zero Links body renders; repeated timezone calls with one formatter construction; and RequestForm duplicate-submit and contract-import-failure handling.

Not run in the original measurement pass:

- The full Playwright suite, because available host memory was below its 3 GB floor. The focused browser tests above were run individually against the isolated seeded stack.
- Web LCP at 1440 and 390, for the memory reason above. There is no baseline web LCP result to compare against in any case.
- A separate clean-`main` execution of each new regression test. Baseline built artifacts were inspected, but no empirical claim is made that every new test was run and failed on `main`.

## Server (PR 4)

Server performance results will be added by PR 4.

## How to reproduce

The original measurements ran on Windows 11 Home 10.0.26300, an Intel Core i7-12700H (14 cores / 20 threads) with 15.68 GB RAM, Docker Desktop 29.9.3 and WSL2. Server and browser containers use the Pi-like limits `--cpuset-cpus 0-3 --memory 2g`; the static-closure measurement itself only reads build output on the host. Web builds use the Node 26 `web-build` stage of `docker/dev.Dockerfile`.

Compare `d78ccd4` (before) with `f5017b5` (after). Run from a clean checkout containing `scripts/route-assets.mjs`; the copy in `TOOL` remains available while the before checkout is built. These are the exact build and measurement commands:

```sh
set -e
TOOL_DIR="$(mktemp -d)"
cp scripts/route-assets.mjs "$TOOL_DIR/route-assets.mjs"
TOOL="$TOOL_DIR/route-assets.mjs"
trap 'rm -rf "$TOOL_DIR"' EXIT

build_route_assets() {
  docker build --target web-build -f docker/dev.Dockerfile -t v0141-web-perf-web .
  rm -rf web/dist
  id=$(docker create v0141-web-perf-web)
  docker cp "$id:/app/web/dist" web/dist
  docker rm "$id"
  mkdir -p "$(dirname "$1")"
  node "$TOOL" web/dist "$1"
}

git switch --detach d78ccd4
build_route_assets route-assets-before.json

git switch --detach f5017b5
build_route_assets docs/evidence/v0141-performance/route-assets.json
```

For each HTML route, the tool follows scripts, stylesheets, island component/renderer URLs and their static `import`/`export` edges. It excludes dynamic-import-only chunks from route totals and lists them separately as `lazy`. Raw and gzip byte counts are summed per asset, with gzip at zlib level 9.
