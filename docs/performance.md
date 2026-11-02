# Historical performance measurements

These are preserved measurements from the original Go port, not a new V0.3 benchmark run. Binary size, dataset counts, and timings describe that build.

Raspberry Pi 5 (4 cores), seeded data (319 guests, 6 events, 6 requests), release build.

**Cold start:** 11–22 ms from process start to the first healthy `/api/health` (budget 300 ms).
Binary: 9.4 MB.

**Latency per request, no queueing** (`BENCH_LATENCY=1 go test -run LatencyReport -v ./internal/api`,
2,000 requests each through the whole handler stack: session lookup, SQL, JSON):

| Endpoint | p50 | p95 | p99 |
| --- | --- | --- | --- |
| `/api/me` | 0.05 ms | 0.15 ms | 0.37 ms |
| `/api/lookups` | 0.38 ms | 0.58 ms | 1.23 ms |
| `/api/overview` | 1.61 ms | 2.15 ms | 2.66 ms |
| `/api/events` | 0.82 ms | 1.01 ms | 1.42 ms |
| `/api/events/:id` | 0.61 ms | 0.78 ms | 1.15 ms |
| `/api/events/:id/metrics` | 0.47 ms | 0.56 ms | 0.77 ms |
| `/api/requests?scope=all` | 0.50 ms | 0.78 ms | 1.17 ms |
| `/api/requests/:id` | 0.53 ms | 0.64 ms | 1.05 ms |
| `/api/finance/summary` | 0.75 ms | 1.16 ms | 1.62 ms |
| `/api/finance/budgets` | 0.39 ms | 0.49 ms | 0.82 ms |
| `/api/finance/reports/budget-vs-actual` | 0.74 ms | 1.11 ms | 1.41 ms |
| `/api/finance/reports/statement-of-activities` | 0.72 ms | 0.96 ms | 1.23 ms |
| `/api/search?q=demo` | 0.27 ms | 0.33 ms | 0.53 ms |

**Over real HTTP under load** (`hey -z 8s -c 8`, 8 concurrent clients saturating the 4 cores, same
box, so this includes the load generator's own CPU):

| Endpoint | req/s | p95 | p99 |
| --- | --- | --- | --- |
| `/api/overview` | 1,127 | 14.4 ms | 19.4 ms |
| `/api/events` | 2,173 | 7.5 ms | 10.5 ms |
| `/api/requests?scope=all` | 3,015 | 5.6 ms | 7.9 ms |
| `/api/requests/:id` | 2,857 | 5.9 ms | 8.5 ms |
| `/api/finance/summary` | 2,021 | 8.4 ms | 12.8 ms |
| `/api/finance/reports/budget-vs-actual` | 2,147 | 8.1 ms | 11.5 ms |
| `/api/search?q=demo` | 4,550 | 3.7 ms | 5.4 ms |

Every list and read endpoint is under the 15 ms p95 target, in-process by an order of magnitude
and under saturating load by a margin (`/api/overview`, the heaviest, is the one close to it at
saturation; with 15–40 officers the box never queues). `go test -bench . -benchmem ./internal/api`
gives ns/op and allocations per endpoint.
