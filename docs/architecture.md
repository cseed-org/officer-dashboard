# Architecture


Paths in the package layout are relative to `server/`; contract paths are relative to the repo root.

The Go backend serves the JSON API and static Astro frontend over one SQLite
database. Production runs on a 64-bit Linux host, usually in Docker.

```
server/
  cmd/dashboard        main: serve | seed | migrate | provision-leadership | healthcheck | mcp-check | drive-backup | setup | version
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
  internal/setup       dashboard setup: Cloudflare preview and apply (plan, cfapi, cfprovision, cffake) and host files (hostfiles)
  internal/files       receipts: Google Drive (shared drive) and local disk
  internal/drivebackup uploads the sidecar's encrypted snapshots to Drive (daily + monthly) and restores them
  internal/driveexport hourly readable event/program snapshots in Drive; content hashes skip unchanged files
  internal/notify      Discord webhook, invitation bot DMs, console fallback
  internal/notion      placeholder Notion event-link sync
  internal/fiscal      fiscal years, monthly periods, Pacific dates
  internal/money       integer-cent parsing/formatting
  internal/validate    request validation with zod-shaped error details
  internal/seed        the FY2027 sample books for development
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
event folders together, but only touches a folder whose trailing `[event id]` has
`drive_export_state` under the current root, so a folder this export never wrote
(another instance's root, a dev or seeded database, a database restored from an
older backup, or a hand-made `Photos [2025]`) loses nothing. When such an owned
folder's event no longer exists, cleanup trashes only the files the export writes
there: `event.json`, `summary.md` and `cover.*`, found by their state ids or those
names. It then lists the folder again. An empty folder is trashed; a folder that
still holds anything else, such as `Design/<request id>/` uploads that
`design_request_files` rows still reference (an event delete sets
`design_requests.event_id` to NULL, and a re-linked request's earlier files stay in
the old event's folder), is renamed `<name> [<id>] (deleted)` and left in place.
Cleanup never trashes or recurses into a subfolder. The renamed folder no longer
ends in `[id]`, so it is never matched as an event folder again. If any exported
file cannot be trashed, the folder and its state are kept and the next pass retries.

For live events, cleanup removes stale `cover.*` files when a cover is replaced,
changes extension, or is removed, retaining only the current exported cover. It
lists an event's folder only when that event's cover state points anywhere but the
current cover (a replaced or removed cover, or an earlier trash that failed), plus
one full sweep of every owned folder per process start and per 24 hours, which also
catches stale covers that have no state. An unchanged pass therefore makes two
Drive listings (the root's `Events` lookup and the `Events` listing) regardless of
the number of events. Unrelated files and design subfolders of live events are
preserved; `cover.*` is reserved for generated cover files. Cleanup rechecks event
existence and cover version before deleting, so concurrent new events or changed
covers wait for the next snapshot. Successful trash operations clear the affected
export state, allowing identical content to be exported again after recreation.
Drive's recoverable trash behavior applies.
Removing a program rewrites its track's JSON. Cleanup of entire `Programs/` folders
is outside this event-and-cover cleanup.

## Stack, and why each dependency

The module has two direct third-party dependencies (see [go.mod](../server/go.mod)):

| Dependency | Why |
| --- | --- |
| `github.com/mattn/go-sqlite3` | SQLite through cgo; requires a C compiler at build time (the Dockerfile cross-installs one). |
| `filippo.io/age` | Encrypts the backup helper's snapshots to the offline recovery recipient; its cryptographic dependencies are recorded in `go.mod` and `go.sum`. |

The dashboard HTTP and application code otherwise uses the standard library: `net/http` method+path routing (Go 1.22), `database/sql`,
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

Each mutation uses a `BEGIN IMMEDIATE` transaction and re-reads current state
before writing, protecting against conflicting concurrent changes.

All SQL is in `internal/store`; handlers call typed methods.

### Search

`GET /api/search` uses a plain case-insensitive `LIKE '%q%'` over events, requests, contacts
(all officers) and (directors) users and accounts. Search uses SQLite without an additional full-text index.
