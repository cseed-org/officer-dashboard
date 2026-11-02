# Architecture


Paths in the package layout are relative to `server/`; contract paths are relative to the repo root.

A single static-ish Go binary replaces the TypeScript Worker backend: the **same JSON API**
(`docs/api.md`, `src/core/contracts.ts`), Discord sign-in, receipts, notifications and the
static front end (`web/dist`), over one SQLite file. It runs on a Raspberry Pi 5 (arm64) or
in a Cloudflare Container.

```
server/
  cmd/dashboard        main: serve | seed | migrate | healthcheck | version
  internal/api         HTTP handlers, middleware, sessions, routes
  internal/store       every SQL statement; embedded migrations 0000–0010
  internal/accounting  posting engine, journal validation, financial reports, CSV export
  internal/purchases   request lifecycle, agent settlement tolerance
  internal/luma        Luma/survey CSV parsing, Pacific-time handling
  internal/auth        roles, allowlist decision, safe redirects, Discord OAuth2 + PKCE
  internal/files       receipts: Google Drive (shared drive) and local disk
  internal/notify      Discord webhook, console fallback
  internal/fiscal      fiscal years, monthly periods, Pacific dates
  internal/money       integer-cent parsing/formatting
  internal/validate    request validation with zod-shaped error details
  internal/seed        the FY2027 sample books (ported from the historical TypeScript seed)
  internal/web         static file server: directory (dev) or go:embed (release)
  deploy/              Dockerfile support, Litestream config, systemd units
  scripts/backup       online SQLite backup
```

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
placeholders throughout. The v1 schema and triggers are unchanged, plus later migrations through 0010 (officers and onboarding), including 0004 (a per-year request counter) (`internal/store/migrations`,
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
