# Architecture


Paths in the package layout are relative to `server/`; contract paths are relative to the repo root.

A single static-ish Go binary replaces the TypeScript Worker backend: the **same JSON API**
(`docs/api.md`, `src/core/contracts.ts`), Discord sign-in, receipts, notifications and the
static front end (`web/dist`), over one SQLite file. It runs on a Raspberry Pi 5 (arm64) or
in a Cloudflare Container.

```
server/
  cmd/dashboard        main: serve | seed | migrate | provision-leadership | healthcheck | mcp-check | version
  internal/api         HTTP handlers, middleware, sessions, routes
  internal/store       every SQL statement; embedded migrations 0000 onward
  internal/accounting  posting engine, journal validation, financial reports, CSV export
  internal/purchases   request lifecycle, agent settlement tolerance
  internal/luma        Luma/survey CSV parsing, Pacific-time handling
  internal/auth        roles, allowlist decision, safe redirects, Discord OAuth2 + PKCE
  internal/calendar    ICS invitations over SMTP, Google Calendar OAuth and sync
  internal/orgcal      Organization Google calendars, sharing, incremental pulls and recurrence expansion
  internal/cfsync      membership D1 mirror and synchronization jobs
  internal/cloudflare  Cloudflare D1 and GraphQL analytics client
  internal/mcpcheck    the mcp-check deployment reachability checks
  internal/files       receipts: Google Drive (shared drive) and local disk
  internal/driveexport hourly readable event/program snapshots in Drive; content hashes skip unchanged files
  internal/notify      Discord webhook, invitation bot DMs, console fallback
  internal/notion      placeholder Notion event-link sync
  internal/fiscal      fiscal years, monthly periods, Pacific dates
  internal/money       integer-cent parsing/formatting
  internal/validate    request validation with zod-shaped error details
  internal/seed        the FY2027 sample books (ported from the historical TypeScript seed)
  internal/web         static file server: directory (dev) or go:embed (release)
  deploy/              Dockerfile support, backup helper/updater, systemd units
  scripts/backup       online SQLite backup
```

## Readable Drive exports

`internal/driveexport` resolves the active Drive connection for each pass. It starts
after five minutes, waits an hour after each pass, and has a ten-minute pass limit.
Disconnected, revoked, and missing-root connections are quiet skips; the connection
flow owns revocation alerts. Other failures use the shared operational alerter.
Quota and rate errors stop a pass; an individual event or track error is collected
while later records continue. A pass timeout alerts through the still-active parent
context; shutdown cancellation stays quiet.

Event text, guest counts, linked requests, program data, and prior export state come
from one database snapshot. Explicit allow-list structs render `event.json` and
`summary.md` into the same event folder that design uploads use. Track exports use
`Programs/<unique track key>/program.json`, so duplicate display names and renames
do not change their destination. Expense summaries distinguish `estimated_total_cents`
from nullable `actual_total_cents`, with `actual_count` identifying how many requests
have recorded actuals. An event without a date or tentative window reads
`Date: to be decided`.

Every generated file has an exact-byte SHA-256 in `drive_export_state` (migration
0038). Render and compare before folder discovery or upload. The retained export
timestamp prevents the clock alone from changing the bytes. State is scoped to the
Drive root and stable entity IDs. A separate `cover.source` state marker fingerprints
`Event.CoverUpdatedAt`; only a changed cover version loads its bytes, in a small
version-checked read transaction. At most one cover is retained at a time. Identical
cover bytes skip upload even when the source version changes.

Each connected pass also performs privacy cleanup under `Events/`. It scans the
event folders together and trashes folders with a trailing `[event id]` whose event
no longer exists. It removes stale `cover.*` files when a cover is replaced, changes
extension, or is removed, retaining only the current exported cover. Unrelated files
and design subfolders of live events are preserved. Cleanup rechecks event existence
and cover version before deleting, so concurrent new events or changed covers wait
for the next snapshot. Successful trash operations clear the affected export state,
allowing identical content to be exported again after recreation. Drive's recoverable
trash behavior applies. These cleanup listings still run when file hashes are
unchanged; unchanged records need no per-event folder discovery or upload.

## Stack, and why each dependency

The module has **one** third-party dependency:

| Dependency | Why |
| --- | --- |
| `github.com/mattn/go-sqlite3` | SQLite through cgo. It is the fastest binding and tracks upstream SQLite closely; the pure-Go ports were slower in the benchmarks people publish and add their own bug surface. Cost: a C compiler at build time (the Dockerfile cross-installs one). |

Everything else is the standard library: `net/http` method+path routing (Go 1.22), `database/sql`,
`crypto/*` (sessions, tokens, RS256 for Google service accounts, PKCE), `encoding/json`,
`log/slog`, `embed`, `time/tzdata` (Pacific time works without OS zoneinfo). Discord OAuth2 is
a small standard-library implementation (`internal/auth/discord.go`) rather than an OAuth library.

### Database

One file, opened twice: a **single writer connection** (all writes are transactions on it, so
they serialise without `SQLITE_BUSY` storms) and a **read pool** of read-only connections.
`journal_mode=WAL`, `foreign_keys=ON`, `busy_timeout=5000`, `synchronous=NORMAL`. Statements use
placeholders throughout. The original schema and triggers are preserved, with later migrations from 0000 onward covering officers, onboarding, calendar, Launchpad and MCP (`internal/store/migrations`,
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

`GET /api/search` uses a plain case-insensitive `LIKE '%q%'` over events, requests, contacts
(all officers) and (directors) users and accounts. FTS5 was considered and rejected: the tables hold hundreds of rows, a query costs
0.27 ms p50, and FTS5 would need a build tag, sync triggers and a rebuild path to save microseconds.
