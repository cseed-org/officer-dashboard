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

## Server (Pi)

Historical finance for 300 requests and 6,000 audit rows falls from **614 ms to 14.4 ms**; the builder portal with 5,000 activity rows across three cohorts falls from **23.6 ms to 0.58 ms**. Listing 200 fulfillment jobs falls from **15.2 ms to 2.77 ms** and matching 5,000 members to officers from **8.21 ms to 0.078 ms**. These are medians of three benchmark runs on Go 1.27.4 with four pinned cores and a 2 GB container limit. The small seed HTTP workload remains broadly similar, with individual improvements and regressions below. Startup and whole-process memory do not improve measurably.

The original baseline is `d78ccd4` on Go 1.24.13. The paired BEFORE is `fa301df` (main at task start, including PRs #86 and #87) on Go 1.27.4; AFTER is `dc863d5` on the same toolchain. Main subsequently advanced to `4233621`; its server difference is CLI help text, with no HTTP/SQL change. Later commits add regression checks, client history-control fixes, and the standalone `EventSummary` store aggregate; no measured handler SQL changes. The embedded client bytes in the measured release are pinned to `dc863d5`; final client controls are covered by a fresh production build and browser tests. These are x86 Docker measurements using a Pi resource profile, not measurements on ARM hardware.

### Changes and evidence

| Finding | Change and behavior check | Measured result / limit |
| --- | --- | --- |
| M-005 | Select only relevant audit images and decode once into per-request history. `TestCommitmentHistoryInterleaved`, `TestHistoricalFinanceDecodeBudget`. | 614 → 14.4 ms; 123.3 → 6.29 MB allocated per historical summary. |
| M-030 | PK archive checks, reuse preloaded registers, scope portal activity in SQL. `TestScopedBuildspaceActivityParity`, `TestBuilderPreloadedRegisterNeedsNoQueries`, `TestPortalBuilderAllocationBudget`. | 23.6 → 0.58 ms; 13.51 MB → 69 KB allocated for the 5,000-row portal fixture. |
| M-032 | Add contribution journal-entry index. `TestPerformanceQueryPlans` explains the actual production SQL and recorded parameters. | Contribution subquery uses the covering index; isolated report time was not measured. |
| M-054 | Filter creator-owned jobs before detail loading; batch five detail queries with identical DTO construction. `TestJobDetailsConstantQueriesAndParity`, `TestAgentJobsAllocationBudget`. | 200 jobs: 15.2 → 2.77 ms; 47,514 → 12,541 allocations. Query count stays constant for 1/50/200 jobs. |
| M-055 | Tuple cursors and composite audit/request indexes. `TestAuditCursorParityAndFinanceHistory`, `TestRequestCursorParityAndPlan`. | Production plans avoid the cursor temp sort; equal-timestamp pages match the previous predicate. No isolated timing claim. |
| M-056 | Index audit type/time, job and activity purchase FKs, and OAuth client references. `TestPerformanceQueryPlans`. | Targeted growing-table lookups use indexes; no isolated timing claim. |
| M-085 | Admit at most four MCP requests globally and reserve a rate token before body reads; stream raw base64 into one decoded buffer. `TestMCPRejectsBeforeReadingBody`, `TestMCPInFlightBodyCap`, `TestStreamingBase64Parity`. | Rejected bodies read zero bytes. 10 MiB multipart fixture: 49.9 → 42.3 ms; 34.98 → 20.99 MB allocated. Early 429 has JSON-RPC id null because the body is unread. |
| M-087 | Default to 200 completed activity records, preserve current-week/open signals, and expose older pages in officer/builder/mentor views. Dense calendar days use cohort summaries above 20 details. `TestBuildspaceHistoryPagesBoundBodiesAndRetainSignals`, private-weekly and onboarding tests, `TestBuildspaceCalendarAggregatesBusyDaysWithCohortNavigation`, browser history test. | Growth fixture: old 617 rows / 818,802 bytes fails the new ≤225-row / 400 KB budget (200 historical plus live records). Portal benchmark includes this bound; calendar timing not isolated. |
| M-089 | Select event pages before metrics enrichment, provide the separate `EventSummary` SQL aggregate for UI counters, and omit event-picker reads for fixed-event imports. `TestEventSQLPageParityAcrossOffsetsAndDST`, `TestEventSmallPageAllocationBudget`, `TestEventSummaryIncludesHistoryBeyondPage`, fixed-event CSV browser test. | A one-event page with 1,000 historical events falls below 5,000 allocations (old 60,138). Small-seed event latency does not improve consistently. Event KPI/UI redesign belongs to M-014 in PR 5. |
| M-090 | Cache immutable officer-email matches until member-table generation changes. `TestMemberEmailCacheTracksCorrectionsReplacementsAndRollback`, `TestMemberOfficersAllEmailKinds`, allocation guard. | Warm 5,000-member reads: 8.21 → 0.078 ms; 30,654 → 607 allocations. Cold first read still builds the index. |
| M-091 | Hash all assets once; retain body/gzip only for small text files. `TestBinaryAssetValidators`. | Binary conditional GET returns 304 with no body; no timed transfer/RSS reduction claimed. |
| M-100 | Static extensions and `/_astro/` bypass session DB reads and cookie renewal. `TestStaticAssetSkipsSessionDatabase`. | Guard succeeds with no session database available. No isolated latency gain measured. |
| M-139 | Keep all active cards and six delivered cards by default, with cursor-based delivered history; bound rail summaries. `TestDesignDeliveredHistoryBoundedAndComplete`, delivered-history browser test. | Fixture has 125 delivered rail entries before, ≤6 after; cursor traversal retains all delivered cards. No isolated latency measurement. |
| M-142 | Slice unquoted UTF-8 CSV fields; build only escaped fields; read the upload into a string builder. `TestCSVUnquotedAllocationBudget`, Unicode/escape and existing line-number tests. | 26,000 guests (simple three-column 0.75 MB fixture): 34.1 → 30.2 ms; 21.07 → 17.02 MB; 286,104 → 182,101 allocations. This is not a measured 5 MB upload. |

The common regressions were also run on an unmodified production-code archive of `fa301df`; compatibility-only test copies compile against both revisions. They fail there for allocation growth, unbounded history, pre-body MCP reads, binary validation, static session access, and query plans, then pass after. Full logs are in [the evidence directory](evidence/v0141-performance/server/). No finding was skipped.

### Benchmarks

All values below are three-run medians. MB means decimal bytes/1,000,000. These additional large-history fixtures were not present in the old Go 1.24 baseline, so it has no comparable numbers for them.

| Scenario | ms/op before → after | MB/op before → after | allocations/op before → after |
| --- | ---: | ---: | ---: |
| Historical finance, R=300 H=6,000 | 614.205 → 14.420 | 123.301 → 6.294 | 5,624,175 → 117,800 |
| Builder portal, 5,000 rows / three cohorts | 23.551 → 0.575 | 13.509 → 0.069 | 172,823 → 1,385 |
| Fulfillment queue, 200 jobs | 15.219 → 2.769 | 3.008 → 1.738 | 47,514 → 12,541 |
| Officer matching, 5,000 members (warm) | 8.208 → 0.078 | 1.296 → 0.025 | 30,654 → 607 |
| MCP multipart, 10 MiB decoded | 49.882 → 42.303 | 34.977 → 20.986 | 41 → 39 |
| CSV, 26,000 guests | 34.056 → 30.194 | 21.069 → 17.018 | 286,104 → 182,101 |

### Release HTTP load and process costs

Each cell is **p50 / p95 in ms**, median of three eight-second runs at concurrency eight. The client is pinned to cores 4–7 with 512 MB; server to cores 0–3 with 2 GB. Every recorded response in all 48 runs per revision was HTTP 200. Seeded current-day endpoints do not exercise the large-history cases above; this table is a regression check, not a claim that every endpoint speeds up.

| Endpoint/page | Old Go 1.24 | Main Go 1.27 BEFORE | Go 1.27 AFTER | CPU ms / 1,000 requests, before → after |
| --- | ---: | ---: | ---: | ---: |
| `/api/members` | 1.4 / 3.0 | 1.5 / 3.3 | 1.5 / 3.2 | 681.8 → 684.1 |
| `/api/me` | 1.0 / 1.8 | 1.1 / 2.0 | 1.1 / 2.1 | 476.4 → 480.7 |
| `/api/lookups` | 1.4 / 3.0 | 1.5 / 3.2 | 1.5 / 3.3 | 676.6 → 690.8 |
| `/api/overview` | 3.5 / 7.1 | 3.6 / 7.4 | 3.7 / 7.4 | 1585.7 → 1624.8 |
| `/api/events` | 2.1 / 4.6 | 2.1 / 4.9 | 2.6 / 5.2 | 1018.8 → 1159.4 |
| `/api/events/event-kickoff` | 2.2 / 4.2 | 2.3 / 4.5 | 2.3 / 4.3 | 1026.2 → 1007.4 |
| `/api/events/event-kickoff/metrics` | 1.8 / 3.5 | 1.8 / 3.7 | 1.9 / 3.8 | 847.5 → 875.3 |
| `/api/requests?scope=all` | 1.5 / 3.1 | 1.6 / 3.5 | 1.6 / 3.5 | 740.2 → 748.7 |
| `/api/requests/req-0002` | 1.6 / 3.5 | 1.8 / 3.9 | 1.9 / 4.0 | 833.0 → 861.1 |
| `/api/finance/summary` | 2.0 / 4.3 | 2.1 / 4.6 | 2.2 / 4.8 | 948.5 → 990.9 |
| `/api/finance/budgets` | 1.3 / 2.8 | 1.5 / 3.4 | 1.5 / 3.2 | 690.7 → 665.6 |
| `/api/finance/reports/budget-vs-actual` | 1.8 / 3.9 | 2.0 / 4.4 | 2.2 / 4.8 | 906.4 → 995.9 |
| `/api/finance/reports/statement-of-activities` | 1.8 / 3.8 | 1.9 / 4.2 | 2.1 / 4.4 | 883.6 → 937.4 |
| `/api/search?q=demo` | 2.1 / 4.0 | 2.2 / 4.1 | 2.2 / 4.2 | 976.4 → 975.5 |
| `/` | 1.0 / 1.8 | 1.1 / 2.0 | 1.1 / 2.1 | 443.8 → 454.8 |
| `/login/` | 0.9 / 1.7 | 1.0 / 1.9 | 1.0 / 1.9 | 412.4 → 425.5 |

| Process measurement | Old Go 1.24 | Main Go 1.27 BEFORE | Go 1.27 AFTER |
| --- | ---: | ---: | ---: |
| Idle RSS after restart (MiB) | 24.1 | 24.7 | 24.3 |
| Peak RSS (MiB) | 39.4 | 40.7 | 41.2 |
| Settled RSS after 30 s (MiB) | 35.2 | 36.9 | 37.4 |
| Load CPU (s) | 1257.50 | 1241.87 | 1239.94 |
| Load wall time (s) | 450.8 | 455.8 | 455.6 |
| Server startup, median of five (ms) | 53 | 70 | 76 |
| Container StartedAt → healthy, median of five (ms) | 1997 | 1992 | 1996 |

Startup is 70 → 76 ms on the paired toolchain, and peak RSS 40.7 → 41.2 MiB: neither is a measured win. The approximately two-second container-to-health measurement is dominated by the polling/container harness. Total saturated-load CPU is nearly unchanged. Idle goroutines are 18 in all captures. Go 1.27 load snapshots contain 73 before / 42 after unique goroutine IDs; these single snapshots are not evidence of a sustained reduction. The original Go 1.24 load count was 36 using the legacy parser and is not directly comparable. The updated parser handles Go 1.27's additional gp/m fields, counts the last SIGQUIT dump only, and tolerates Docker log rotation.

Raw data: [original Go 1.24](evidence/v0141-performance/server/baseline-go124.json), [paired BEFORE](evidence/v0141-performance/server/before.json), [AFTER](evidence/v0141-performance/server/after.json), [before benchmark text](evidence/v0141-performance/server/before-go-bench.txt), [after benchmark text](evidence/v0141-performance/server/after-go-bench.txt). JSON retains all per-round observations; `before-hey/` and `after-hey/` retain the load client's text. Process samples, cold-start runs, image identities, resource limits, host-memory gates, query regressions and validation logs are adjacent.

### Reproducing the server comparison

Run from Git Bash on Windows with Docker Desktop. Keep temporary files under the Pane scratch directory. Check `(Get-CimInstance Win32_OperatingSystem).FreePhysicalMemory/1024` in PowerShell before each benchmark/load/cold-start phase; wait if below 1,500 MB. Run no other heavy jobs concurrently. The captures used the original hey v0.1.5 binary (SHA-256 recorded alongside the evidence), Go 1.27.4, Node 26, the existing release Dockerfile and the original baseline's seed/load settings. Only path/port isolation, Windows curl output redirection, and the Go stack parser were adjusted. `/dev/null` passed as a Windows curl output filename exits 23; shell redirection fixes that health-wait check.

```sh
set -e
export MSYS_NO_PATHCONV=1
S=.pane/scratch
T=docs/evidence/v0141-performance/server/tools
mkdir -p "$S/before" "$S/after"
git archive fa301df | tar -x -C "$S/before"
git archive dc863d5 | tar -x -C "$S/after"
# The common benchmark fixtures compile on the earlier production code.
cp "$T"/fixtures/api/*.go "$S/before/server/internal/api/"
cp "$T/fixtures/luma/csv_performance_test.go" "$S/before/server/internal/luma/"
docker volume create fix-server-perf-scratch
for label in before after; do
  B="$S/perf-$label"
  mkdir -p "$B/tools/bin" "$B/raw/server"
  cp "$T"/*.sh "$T"/*.py "$B/tools/"
  docker run --rm --cpuset-cpus 0-3 --memory 2g     -v "$(cygpath -w "$PWD/$S/$label"):/repo:ro"     -v "$(cygpath -w "$PWD/$B"):/results"     -v fix-server-perf-scratch:/scratch -e LABEL="$label"     -e PANE_SCRATCH_DIR=/scratch golang:1.27.4-bookworm     sh /results/tools/go-bench.sh > "$B/raw/go-bench.log" 2>&1
  # Run each following phase only after its predecessor fully exits.
  docker build --target web-build -f "$S/$label/docker/dev.Dockerfile"     -t "fix-server-perf-web-$label" "$S/$label"
  id=$(docker create "fix-server-perf-web-$label")
  docker cp "$id:/app/web/dist" "$S/$label/web/dist"
  docker rm "$id"
  docker build -f "$S/$label/server/Dockerfile"     -t "fix-server-perf-$label" "$S/$label"
  docker run --rm -v "$(cygpath -w "$PWD/$B/tools/bin"):/out"     golang:1.27.4-bookworm sh -c     'CGO_ENABLED=0 GOBIN=/out go install github.com/rakyll/hey@v0.1.5'
  B="$B" IMG="fix-server-perf-$label" bash "$T/perf-server.sh"
  B="$B" IMG="fix-server-perf-$label" bash "$T/perf-coldstart.sh"
  python -I "$T/summarize-server.py" "$B"
done
docker volume rm fix-server-perf-scratch
```

The recorded BEFORE release reused the original baseline's Linux-built web assets; the AFTER release built its own assets on Node 26. Rebuilding BEFORE assets above is functionally equivalent for the server comparison. Retain the common fixtures when testing main; implementation-specific regression tests deliberately use the branch APIs and are not blindly copied over old code. `go test -bench . -benchmem -run '^$' ./...` runs three times, followed by the existing in-process latency report; all report output is preserved in the benchmark text. The original Go 1.24 capture and toolchain details are retained in `baseline-go124.json`.

### Server validation

Full Go suite, dev API suite, both vet configurations, cgo release build, contract check (299 responses, 139/146 endpoints, zero schema errors), generated MCP tools check and root script tests passed. Final web build and Astro check passed (zero errors, zero warnings, nine existing hints). Native full-suite execution encountered Windows Smart App Control blocks; the required Linux checks ran in Docker without changing that protection. All six focused desktop/phone history and fixed-event CSV tests passed; [light/dark screenshots](evidence/v0141-performance/server/screenshots/) are linked in the PR. The standalone event-summary aggregate also passed the full store suite and both vet configurations. The full browser suite was skipped locally because free memory was below 3 GB. CI runs its configured unit and compose integration checks.

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
