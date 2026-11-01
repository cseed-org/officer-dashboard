# Go backend (v2)

A single static-ish Go binary replaces the TypeScript Worker backend: the **same JSON API**
(`docs/api.md`, `src/core/contracts.ts`), Discord sign-in, receipts, notifications and the
static front end (`web/dist`), over one SQLite file. It runs on a Raspberry Pi 5 (arm64) or
in a Cloudflare Container.

```
server/
  cmd/dashboard        main: serve | seed | migrate | healthcheck | version
  internal/api         HTTP handlers, middleware, sessions, routes
  internal/store       every SQL statement; embedded migrations 0000–0004
  internal/accounting  posting engine, journal validation, financial reports, CSV export
  internal/purchases   request lifecycle, agent settlement tolerance
  internal/luma        Luma/survey CSV parsing, Pacific-time handling
  internal/auth        roles, allowlist decision, safe redirects, Discord OAuth2 + PKCE
  internal/files       receipts: Google Drive (shared drive) and local disk
  internal/notify      Discord webhook, console fallback
  internal/fiscal      fiscal years, monthly periods, Pacific dates
  internal/money       integer-cent parsing/formatting
  internal/validate    request validation with zod-shaped error details
  internal/seed        the FY2027 sample books (reproduces scripts/seed/data.ts)
  internal/web         static file server: directory (dev) or go:embed (release)
  deploy/              Dockerfile support, Litestream config, systemd units
  scripts/backup       online SQLite backup
```

## Run it

Go 1.24+, a C compiler (cgo, for SQLite). From `server/`:

```sh
make dev                 # seeds ./data/dev.db, serves on :8090 with the dev login
# or by hand:
go run -tags dev ./cmd/dashboard seed -reset
DEV_AUTH=1 STATIC_DIR=../web/dist go run -tags dev ./cmd/dashboard serve
```

Open <http://localhost:8090/auth/dev?as=director> (or `as=officer`) to sign in without Discord.

**The dev login cannot exist in production.** `/auth/dev` is compiled in only with `-tags dev`
*and* answers only when `DEV_AUTH=1`. The default build has no such route; setting `DEV_AUTH=1`
on it logs a warning and changes nothing (`TestDefaultBuildHasNoDevLogin` asserts a 404 for
every variant).

### Configuration

Environment variables (flags override where shown):

| Variable | Default | Meaning |
| --- | --- | --- |
| `ADDR` (`-addr`) | `:8090` | listen address |
| `DB_PATH` (`-db`) | `./data/dashboard.db` | SQLite file (WAL mode; directory is created) |
| `APP_URL` (`-app-url`) | `http://localhost:<port>` | public origin: OAuth redirect URI, links in notifications; `https://` turns on `Secure` cookies, the `__Host-` cookie prefix and HSTS |
| `STATIC_DIR` (`-static`) | embedded build | serve the front end from this directory instead |
| `STATIC_FALLBACK`, `STATIC_FALLBACK_STATUS` | `404.html`, `404` | page for unknown non-API paths. The `redesign` front end can set an SPA shell (`200.html`, `200`) |
| `DISCORD_CLIENT_ID`, `DISCORD_CLIENT_SECRET` | – | Discord OAuth2 app; redirect URI `${APP_URL}/auth/callback` |
| `BOOTSTRAP_OWNER_DISCORD_ID` | – | initial owner Discord ID; provisioned on server startup only when no owner exists |
| `BOOTSTRAP_OWNER_NAME` | – | initial owner display name; existing name retained if blank, otherwise “Dashboard owner” for a new user |
| `BOOTSTRAP_DIRECTOR_DISCORD_ID` | – | deprecated ID alias; `BOOTSTRAP_OWNER_DISCORD_ID` takes precedence |
| `DISCORD_WEBHOOK_URL` | – | notifications; with none they are logged |
| `GOOGLE_SERVICE_ACCOUNT_JSON`, `DRIVE_ROOT_FOLDER_ID` | – | receipts in a Google shared drive |
| `RECEIPTS_DIR` | – | receipts on local disk (single box, no Drive). With neither set, uploads answer 503 |
| `AGENT_SETTLEMENT_TOLERANCE_PERCENT`, `_CENTS` | `10`, `2500` | agent hold ceiling: max(estimate × 1.10, estimate + $25) |
| `DEV_AUTH` | – | `1` enables `/auth/dev` in a `-tags dev` build only |

Sessions live in the database (`sessions`, keyed by sha256 of the cookie token, 30-day sliding
expiry). Service tokens are stored as sha256 hashes. Writes that arrive with a session cookie
and a foreign `Origin` header are refused (403), on top of `SameSite=Lax`.

Static files are served for every path outside `/api/` and `/auth/`, **without** a session:
the pages are shells and the data behind them is API-protected. (v1 redirected unauthenticated
page requests to `/login`; the new front end does that client-side from `GET /api/me`.)

## Stack, and why each dependency

The module has **one** third-party dependency:

| Dependency | Why |
| --- | --- |
| `github.com/mattn/go-sqlite3` | SQLite through cgo. It is the fastest binding and tracks upstream SQLite closely; the pure-Go ports were slower in the benchmarks people publish and add their own bug surface. Cost: a C compiler at build time (the Dockerfile cross-installs one). |

Everything else is the standard library: `net/http` method+path routing (Go 1.22), `database/sql`,
`crypto/*` (sessions, tokens, RS256 for Google service accounts, PKCE), `encoding/json`,
`log/slog`, `embed`, `time/tzdata` (Pacific time works without OS zoneinfo). Discord OAuth2 is
about 100 lines (`internal/auth/discord.go`) rather than an OAuth library.

### Database

One file, opened twice: a **single writer connection** (all writes are transactions on it, so
they serialise without `SQLITE_BUSY` storms) and a **read pool** of read-only connections.
`journal_mode=WAL`, `foreign_keys=ON`, `busy_timeout=5000`, `synchronous=NORMAL`. Statements use
placeholders throughout. The v1 schema and triggers are unchanged, plus migration 0004 (a per-year request counter) (`internal/store/migrations`,
applied by an embedded runner into `schema_migrations`), so the same integrity rules hold:
posted journal entries immutable, balanced posting, closed periods, append-only audit log,
last-director guard, one approval and one settlement per request.

v1 wrapped each mutation in a "guard row" batch because D1 has no interactive transactions. Here
each mutation is one `BEGIN IMMEDIATE` transaction that re-reads current state before it writes,
which is the same check made simpler (`TestReleasesRestrictedPurchases…` fires two concurrent
approvals and expects exactly one 200 and one 409).

All SQL is in `internal/store`; handlers call typed methods. Postgres would mean reimplementing
that package. It is not built and not needed for 15–40 users on one box.

### Search

`GET /api/search` uses a plain case-insensitive `LIKE '%q%'` over events, requests and (directors) users and
accounts. FTS5 was considered and rejected: the tables hold hundreds of rows, a query costs
0.27 ms p50, and FTS5 would need a build tag, sync triggers and a rebuild path to save microseconds.

## Tests

```sh
go vet ./... && go test ./...            # unit + integration (~10 s, most of it RSA key generation)
go test -tags dev ./internal/api          # the dev-login build
node ../scripts/contract-check.mjs        # every endpoint vs the zod contract (needs `npm ci` at the repo root)
```

Ported from v1: `tests/core/*` (money, fiscal, settlement, Luma CSV, metrics, lifecycle, auth,
contributions, journal, accounting engine and reports, safe redirects), `tests/db/*` (integrity
triggers, seed), `tests/server/api.test.ts` (every scenario, against a real seeded SQLite file with
a fixed clock) and `tests/server/drive.test.ts` (JWT, shared-drive corpus, folder races). New:
search, Discord sign-in against a fake Discord (PKCE, allowlist, bootstrap leadership), session
sliding, the Origin check, static serving, `healthcheck`.

`scripts/contract-check.mjs` bundles `src/core/contracts.ts`, starts the server on a seeded temp
database and validates **every request body, query and response** against `ENDPOINTS`:
all GET endpoints plus a purchase lifecycle (create → attach → submit → approve by a second
director → fulfil → close), a restricted-grant reimbursement, the purchasing-agent flow including an
over-tolerance hold, Luma ingest, CSV import, and the director writes. It fails on any schema error,
unexpected status, or endpoint left unexercised.

The seeded FY2027 statements foot (printed by the check and asserted in `TestSeededFY2027StatementsFoot`):

```
unrestricted  $1,800.00 − $591.40 + $186.40 released = $1,395.00
net assets ending $11,908.60 = total assets $11,908.60   (balanced)
```

The Go seed was also diffed against the TypeScript seed: all 15 tables are byte-identical.

## Performance

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

## Deploy

### Raspberry Pi 5 (systemd)

```sh
make build-release           # needs ../web/dist; embeds it. `make build` for an API-only binary
```
then follow the header of `deploy/dashboard.service` (user, env file `deploy/dashboard.env.example`,
install, enable). Put it behind a Cloudflare Tunnel or Caddy for HTTPS; set `APP_URL` to the public
URL. Nightly backups: `deploy/dashboard-backup.{service,timer}` run `scripts/backup`.

**Backups.** `scripts/backup` uses `sqlite3 .backup` (safe on a live WAL database; never copy the
`.db` file alone), checks `PRAGMA integrity_check`, gzips, prunes to the newest 30 and can push
off-box via `BACKUP_REMOTE` (`rclone`/`aws s3 cp`). Restore steps are in its header. Receipts are
not in the database: Google Drive holds them (or `RECEIPTS_DIR`, which you must back up too).

### Cloudflare Containers / Docker

```sh
(cd web && npm ci && npm run build)
docker buildx build --platform linux/amd64,linux/arm64 -f server/Dockerfile -t officer-dashboard .
```
Multi-stage; cross-compiles cgo with an installed cross-gcc (no emulation); the runtime is
`debian:bookworm-slim` plus the `litestream` binary, running as a non-root user with a
`HEALTHCHECK` (`dashboard healthcheck`). **Containers have ephemeral disks**, so the image
replicates the SQLite file with Litestream when `LITESTREAM_BUCKET` is set:

| Variable | Example |
| --- | --- |
| `LITESTREAM_BUCKET` | `cseed-dashboard-db` |
| `LITESTREAM_ENDPOINT` | `https://<account-id>.r2.cloudflarestorage.com` (R2; empty for AWS S3) |
| `LITESTREAM_REGION` | `auto` (R2) or `us-west-2` |
| `LITESTREAM_PATH` | `dashboard` (key prefix; default) |
| `LITESTREAM_ACCESS_KEY_ID`, `LITESTREAM_SECRET_ACCESS_KEY` | an R2/S3 token scoped to the bucket |

On boot `deploy/entrypoint.sh` runs `litestream restore -if-db-not-exists -if-replica-exists`, then
`litestream replicate -exec "dashboard serve"`: up to ~1 s of writes can be lost if the container
dies hard, and a replaced container comes back with the data. `deploy/litestream.yml` keeps 72 h of
WAL and a snapshot every 6 h. Receipts must use Google Drive in a container (`RECEIPTS_DIR` would
be wiped). Run **one** instance: SQLite plus Litestream is single-writer.

> The Dockerfile and Litestream wiring were written against the documented interfaces but not built
> or run in this branch: the Pi had no `web/dist` to embed (the front end is the `redesign` Pane's work).
> First run of `docker buildx build` is the check.

### Dashboard ownership and recovery

Set `BOOTSTRAP_OWNER_DISCORD_ID` and `BOOTSTRAP_OWNER_NAME` in the deployment
environment (see `deploy/dashboard.env.example`). At startup, if no owner exists,
the server creates or updates that Discord account as active leadership with owner
access and records an audit entry. Provisioning is idempotent. Once an owner exists,
configuration no longer changes ownership or names; a different configured ID logs a
warning. An ownership transfer therefore survives restarts even with the old env values.
The deprecated `BOOTSTRAP_DIRECTOR_DISCORD_ID` remains an alias for the ID.

Owners can add co-owners, remove owner access, or transfer their own ownership from
Admin. Non-owner leadership cannot manage ownership or demote/deactivate an owner.
Every owner must be active leadership, and the last owner cannot be removed. Transfer
keeps the caller active leadership and keeps any other co-owners.

Existing roles are preserved by migration. To explicitly recover an active account
as an owner, run `dashboard provision-leadership --owner --db /path/dashboard.db
--discord-id 123456789012345678`. Without `--owner`, the command grants leadership
only. Both operations are idempotent, end the target's sessions when changed, and write
an audit entry. Seed data includes a fictional leadership owner; no real deployment
identity is checked in.
