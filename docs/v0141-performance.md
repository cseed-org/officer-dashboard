# V0.14.1 Web performance on phones

This change reduces first-load JavaScript on the heaviest pages and removes repeated client-side work on Home, Buildspace, Contacts and Settings. The largest first-load reductions are **55% on Launchpad, 49% on Finance, 45% on Buildspace and 34% on Onboarding**. Officers also loses 33.8 KB of first-load CSS.

There are no API, DTO or Go server changes in this PR. The improvements are in static import boundaries, data indexing, render isolation and deferred work.

## Built bytes per route

The baseline is `main` at `d78ccd4`; the after build contains the code at `f5017b5`. Both used the unchanged `docker/dev.Dockerfile` web-build stage (Node 26) and the same `route-assets.mjs` static-closure tool. JS and CSS include the page's statically imported island modules and styles, excluding lazy-only chunks. Gzip uses zlib level 9, as the Go static server does.

| Route | JS raw, before → after | Change | JS gzip, before → after | CSS raw, before → after |
| --- | ---: | ---: | ---: | ---: |
| `/launchpad` | 253,604 → **113,658** | **−55.2%** | 81,270 → **43,731** | 105,493 → 105,493 |
| `/finance` | 287,049 → **146,479** | **−49.0%** | 91,993 → **54,239** | 87,212 → 87,212 |
| `/buildspace-launchpad` | 308,461 → **168,381** | **−45.4%** | 100,627 → **63,121** | 110,388 → 110,388 |
| `/onboarding` | 147,796 → **96,962** | **−34.4%** | 56,955 → **37,493** | 86,222 → 86,222 |
| `/officers` | 374,184 → **304,966** | **−18.5%** | 125,246 → **100,308** | 123,247 → **89,472** (−27.4%) |

Home is not a byte-reduction target in this PR: its static JS is 147,666 → 148,717 raw (+0.7%), while its changes target calendar and drag work after loading. Contacts is 107,770 → 108,453 raw (+0.6%); its improvement is removing the event-list request from directory reads, not reducing bytes. Other routes have small static-graph shifts; the complete before and after inventories are linked below.

Raw evidence: [before](evidence/v0141-performance/route-assets-before.json) · [after](evidence/v0141-performance/route-assets.json).

## What changed

| Finding | Change | Expected effect |
| --- | --- | --- |
| M-029 | Moved shared constants into zod-free modules in both contract copies. Buildspace, Finance and builder portal constants use those modules; request-save schemas load on demand. | Keeps the zod contract chunk off Buildspace, Finance and Launchpad first paint without changing schema validation or exports. |
| M-031 | Added a `WeakMap` Buildspace index for projects, people, mentors, activities, attendance and archived state. Cards, directory search and the drawer share it. | Replaces repeated full-array filters with indexed lookups; a new response builds a new index. |
| M-084 | Split component-free widget metadata into `catalog.ts`; only Home imports the component registry. Audit log metadata no longer imports Home. | Removes Home widgets and their CSS from Officers and Onboarding. |
| M-086 | Buckets calendar items once per feed/range/zone and uses a source `Map`. | Avoids filtering the whole feed and formatting end dates separately for every day cell. Multi-day and exclusive-midnight behavior is unchanged. |
| M-088 | Memoized widget bodies separately from tile positioning. | Pointer movement updates tile styles without rerendering unchanged widget bodies. |
| M-138 | Reused the cached zoned formatter for today's date and wall-clock-to-ISO conversion. | Avoids constructing identical `Intl.DateTimeFormat` instances on repeated renders. |
| M-140 | Cached devices reveal the shell immediately, including devices that kept the default width. Only a new or cleared device waits, now capped at 300 ms. | A stalled preferences request cannot hide a returning user's shell for the old 1.5 s cap. Late widths apply on the next navigation. |
| M-141 | Moved linked-event loading into the contact editor. | `/api/events?limit=100&when=all` is absent from directory and deep-link reads and loads when Add/Manage opens. Cursor pagination is retained. |
| M-143 | Memoized the IANA zone list and isolated the 30-second clock. | Clock updates no longer rerender the whole timezone panel. |

## Timing measurements

No before/after LCP comparison is claimed. The baseline worker could not run its Chromium probe because host free memory fell below the 1.5 GB safety floor. The after probe was also skipped: repeated checks after the Docker builds remained below that floor (about 1.17–1.28 GB), and the host had previously dropped by roughly 1 GB as soon as Chromium started. Running it would have produced a resource-starved measurement and triggered the baseline memory guard.

The release image itself built successfully with the baseline's `server/Dockerfile`. Server timing benchmarks were not repeated for this web-only PR; the previously completed Go tests, vet checks and API contract check are listed below.

## Validation

Existing and new checks run against this branch:

- `npx astro check`: **0 errors, 0 warnings, 9 existing hints**.
- `npm run build`: passed on the host; the matching Linux web build and release-image build also passed.
- `npx playwright test tests/unit.spec.ts tests/buildspace-model.spec.ts --project=desktop`: **38 passed**. These are pure tests and do not launch Chromium.
- Focused browser regressions passed for the 20-step drag render count, cached and stalled sidebar behavior (5 tests), deferred contact event loading, and timezone formatter caching.
- Production import-graph guards passed for Buildspace, Finance, Officers and Onboarding. The baseline HTML was also inspected directly and contains the contract/Home preloads that these guards reject.
- `go test ./...`, `go test -tags dev ./internal/api`, `go vet ./...` and `go vet -tags dev ./...`: passed in Docker/Go 1.24.
- Root script tests: **4 passed**. `node scripts/gen-mcp-tools.mjs --check`: generated MCP tools are current.
- API contract check: **299 responses, 139/146 endpoints exercised, 0 schema errors**.

New regression coverage includes aggregate equivalence and scan counts on 200 builders, 100 projects, 10 mentors and 10,000 activities; a 200-item calendar comparison with exactly 180 end conversions; a real pointer drag with zero Links body renders; and repeated timezone calls with one formatter construction.

Not run:

- The full Playwright suite, because available host memory was below its 3 GB floor. The focused browser tests above were run individually against the isolated seeded stack.
- Web LCP at 1440 and 390, for the memory reason above. There is no baseline web LCP result to compare against in any case.
- A separate clean-`main` execution of each new regression test. Baseline built artifacts were inspected, but no empirical claim is made that every new test was run and failed on `main`.
