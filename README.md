# cseed officer dashboard

Internal dashboard for CSEED officers: event health (Luma registrations → check-ins →
reflection surveys), budget status, a purchase-request flow (officer submits → director
approves → fulfilment), and a nonprofit-grade double-entry ledger that can later produce
Form 990 / Washington filings and financial statements.

One Astro app (SSR) on Cloudflare Workers with D1 (SQLite) through Drizzle ORM. Sign-in
is Discord OAuth, limited to an allowlist of officers. The design follows
[cseed.co](https://cseed.co): paper and ink, Playfair Display, Poppins, Anonymous Pro.

> **Status: Phase 0 (foundation).** Schema, migrations, API contract, auth, seed data, the
> app shell and test harness are in place. Pages render `TODO` placeholders; the UI and
> the API handlers are Phase 1.

## Quick start

Needs Node ≥ 22.23 (tested on Node 26, Raspberry Pi 5 / linux arm64).

```sh
npm install
npm run db:migrate:local   # create the local D1 database (.wrangler/state)
npm run db:seed:local      # sample officers, events, requests, budget, journal
npm run dev                # http://localhost:4321
```

`npm run dev` creates `.dev.vars` from `.dev.vars.example` on first run, which turns on
the **dev login**: open <http://localhost:4321/login> and choose *director* or *officer*
(or go straight to `/auth/dev?as=director`). You don't need a Discord app for local work.

> When Astro detects a coding agent it starts the dev server in the background
> (`astro dev stop|status|logs`). Add `--ignore-lock` to keep it in the foreground.

Reset the local database: `npm run db:reset:local`. Do this after pulling a change
that rewrites existing migrations (as PR #1's review fixes did, before anything was deployed).

## Scripts

| Script | What it does |
| --- | --- |
| `dev` | Astro dev server, running the Worker in workerd with local D1 |
| `build` | Production build to `dist/`, then `scripts/check-build.mjs` fails if the dev login survived |
| `preview` | Build, then serve `dist/` with `wrangler dev` |
| `typecheck` | `wrangler types` + `astro check` + `tsc` for tests and scripts |
| `test` | Vitest: core logic, the contract, migrations/triggers, seed |
| `test:e2e` | Playwright smoke test on phone 390×844, tablet 820×1180, desktop 1440×900 |
| `db:generate` | Diff `src/server/db/schema.ts` into a new SQL migration in `migrations/` |
| `db:migrate:local` / `db:migrate:remote` | Apply `migrations/*.sql` to local / production D1 |
| `db:seed:local` | Generate `scripts/seed` SQL and run it on local D1 (safe to re-run) |
| `db:reset:local` | Delete local D1, migrate, seed |
| `deploy` | Build, migrate remote D1, `wrangler deploy` |

Playwright needs a browser once: `npx playwright install chromium`. The e2e run starts
its own dev server on port 4322 (migrating and seeding first) and saves a screenshot of
every page, light and dark, to `tmp/e2e-shots/<viewport>/<scheme>/`.

## Environment and secrets

Local values go in `.dev.vars` (git-ignored). In production, set secrets with
`wrangler secret put <NAME>`; non-secret vars live in `wrangler.jsonc`.

| Name | Kind | Needed for |
| --- | --- | --- |
| `DB` | D1 binding | Everything. `wrangler d1 create cseed-officer-dashboard`, then put the id in `wrangler.jsonc` |
| `APP_URL` | var | Public origin used for the OAuth redirect, e.g. `https://officers.cseed.co` |
| `DISCORD_CLIENT_ID` | secret | Discord sign-in |
| `DISCORD_CLIENT_SECRET` | secret | Discord sign-in |
| `BOOTSTRAP_DIRECTOR_DISCORD_ID` | secret/var | Your Discord user id; created as the first director on first sign-in |
| `GOOGLE_SERVICE_ACCOUNT_JSON` | secret | Phase 1: receipt uploads to Google Drive (service-account key JSON) |
| `GOOGLE_DRIVE_FOLDER_ID` | secret/var | Phase 1: shared-drive folder receipts are filed under |
| `DISCORD_WEBHOOK_URL` | secret | Phase 1: request notifications to a Discord channel |
| `DEV_AUTH` | `.dev.vars` only | `1` enables `/auth/dev` in `astro dev`. Production builds compile it out |

### Discord app

1. <https://discord.com/developers/applications> → New Application → OAuth2.
2. Add redirects `http://localhost:4321/auth/callback` and `https://<your domain>/auth/callback`.
3. Copy the client id and secret into `.dev.vars` / `wrangler secret put`.
4. Only the `identify` scope is requested. Sign-in is allowed only for Discord ids in the
   `users` table (directors add officers on `/admin/users`), plus the bootstrap director.

## Deploy (Cloudflare)

```sh
npx wrangler login
npx wrangler d1 create cseed-officer-dashboard      # copy database_id into wrangler.jsonc
npx wrangler secret put DISCORD_CLIENT_ID           # repeat for each secret above
npm run deploy                                      # build → migrate remote D1 → deploy
```

Then add a custom domain to the Worker and set `APP_URL` to it.

## Architecture

```
src/
  core/            framework-free domain logic: no Astro, Cloudflare or Drizzle imports
    contracts.ts   zod schemas + types for every API request/response (the UI ↔ backend contract)
    domain.ts      enums (roles, statuses, account types, functional classes, …)
    auth.ts        role rules, path → required role, dev-login guard, login allowlist decision
    ports.ts       FileStore and Notifier interfaces
    money.ts, metrics.ts, journal.ts, purchase-lifecycle.ts, fiscal.ts, contributions.ts
  server/          adapters
    db/            Drizzle schema (all tables) and D1 client
    auth/          sessions (hashed tokens in D1), Discord OAuth (arctic), requireRole()
    files/         FileStore: in-memory stub now, Google Drive in Phase 1
    notify/        Notifier: console stub now, Discord webhook in Phase 1
    env.ts         the one place that reads Worker bindings
  middleware.ts    attaches locals.user, enforces roles per path (403 for officers on /admin/*)
  pages/api/       thin JSON handlers (parse with contracts → call core → respond)
  pages/           UI routes (Phase 0: placeholders + app shell)
  styles/tokens/   CSEED colour (light + dark), type and layout tokens
migrations/        SQL migrations applied by wrangler (0001 = integrity triggers)
scripts/seed/      seed data and SQL generator
tests/core, tests/db   Vitest     tests/e2e   Playwright
docs/api.md        endpoint table: method, path, role, schema names
```

`src/core` stays portable: a later Raspberry Pi / Node deployment swaps the adapters
(`@astrojs/node`, better-sqlite3 with the same Drizzle schema, a filesystem FileStore)
without touching domain code.

### Data integrity

- Money is stored as integer cents everywhere. CHECK `typeof(col) = 'integer'` rejects
  fractional values, which SQLite's INTEGER affinity would otherwise store as REAL.
- Enum columns have CHECK constraints; journal lines must be non-negative with exactly one
  of debit or credit; purchase totals must equal quantity × unit cost + tax/shipping.
- Triggers (`migrations/0001_integrity_triggers.sql`): journal entries are created as
  drafts and can only be posted when balanced, with ≥ 2 lines, outside closed fiscal
  periods. **Posted entries and their lines can't be updated or deleted**; corrections
  are reversing entries. `audit_log` is append-only.
- D1 runs with `recursive_triggers` off, so `INSERT OR REPLACE` / `UPDATE OR REPLACE`
  delete conflicting rows without firing DELETE triggers. Extra guards therefore refuse any
  insert or key change that collides with a posted entry or line, an audit row, or a
  closed fiscal period. `tests/db/integrity-triggers.test.ts` runs every rule against both
  `node:sqlite` and real local D1 (workerd via `getPlatformProxy`).
- Sessions store only a SHA-256 of the cookie token. Service tokens likewise.

### Accounting note

The seeded chart of accounts, its Form 990 Part VIII/IX/X line mapping, the July–June
fiscal year, and the donor-acknowledgment rules in `src/core/contributions.ts` are a
reasonable starting point for a small 501(c)(3), **not tax or accounting advice. Have
CSEED's CPA review them** before real books are kept or filings are prepared.

## Design

Colours, fonts and the nav come from the cseed.co landing site
(`cseed-org/cseed-landing-page`). Tokens live in `src/styles/tokens/`: the verbatim
palette plus semantic tokens (surfaces, text, rules, status, charts) with a dark theme
that follows the system preference or the nav's theme toggle. Fonts load from the same
Google Fonts URL as the landing site. Status colours: sage = approved, sun = pending (ink
text), sky = info, brick = denied / over budget.

## CI

`.github/workflows/ci.yml` runs typecheck and unit tests on pushes and pull requests.
