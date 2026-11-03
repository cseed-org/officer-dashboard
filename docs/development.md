# Development

## Quick start

Needs Go 1.24+ with a C compiler (cgo, for SQLite) and Node ≥ 22.23.4.

```sh
(cd web && npm ci && npm run build)   # static front end -> web/dist
cd server && make dev                 # seeds ./data/dev.db, serves with the dev login
```

See [deployment guide](deployment.md) for configuration, secrets and deployment, and
[front-end guide](frontend.md) for the front-end dev server.

## Checks

| Check | Command |
| --- | --- |
| Go vet + tests | `cd server && go vet ./... && go test ./...` |
| API contract | `npm ci && npm run contract-check` (builds and runs the Go server itself) |
| Front end build + types | `cd web && npm ci && npm run build && npx astro check` |
| Front end e2e (local only) | `cd web && API_ORIGIN=http://127.0.0.1:8090 npm run test:e2e` (Playwright; not run in CI) |

CI (`.github/workflows/ci.yml`) runs the `go`, `contract` and `web` jobs on every push to
`main` and every pull request.

## Run it

Go 1.24+, a C compiler (cgo, for SQLite). From `server/`:

```sh
make dev                 # seeds ./data/dev.db, serves on :8090 with the dev login
# or by hand:
go run -tags dev ./cmd/dashboard seed -reset
DEV_AUTH=1 STATIC_DIR=../web/dist go run -tags dev ./cmd/dashboard serve
```

Open <http://localhost:8090/auth/dev?as=director> (or `as=officer`, `as=leadership`) to sign in
without Discord, or use the **Developer mode** panel that `/login` shows under the Discord button
("Skip Discord and continue as" Leadership (owner) / Director / Officer). The panel appears
whenever the server answers `GET /auth/dev/info` with `authorized: true`, so the same static
build looks like production everywhere else.

### Developer mode on a test deployment

A shared test deployment can run the dev build so testers skip Discord, gated by a secret:

```sh
DEV_AUTH=1 DEV_AUTH_KEY=$(openssl rand -hex 24) ./bin/dashboard-dev serve
```

With `DEV_AUTH_KEY` set, `/auth/dev` and the login panel need the key; without it both look
exactly like a production build (`/auth/dev` answers the site's 404 page, no hint). Share links
of the form

```text
https://<host>/auth/dev?as=leadership&key=<KEY>                 # sign in, land on Home
https://<host>/auth/dev?as=officer&key=<KEY>&next=/events       # sign in, land on Events
https://<host>/login?key=<KEY>                                  # show the Developer mode panel
```

`as` is `leadership`, `director` or `officer`; `next` must be a path on the same site (anything
else lands on `/`). A valid key also sets a 30-day `dev_key` cookie (HttpOnly, SameSite=Lax,
Secure over HTTPS; it holds a hash of the key, not the key), so later the login page, including
the redirect a signed-out visitor gets from any page, shows the panel without the key in the
URL. Changing `DEV_AUTH_KEY` invalidates every such cookie. The key is never logged (request
logs record the path only).

**Test deployments only.** Anyone holding the key can sign in as any role, including the owner.
Never point a dev build at real data, and never deploy one to production.

**The dev login cannot exist in production.** `/auth/dev` is compiled in only with `-tags dev`
*and* answers only when `DEV_AUTH=1`. The default build has no such route; setting `DEV_AUTH=1`
on it logs a warning and changes nothing (`TestDefaultBuildHasNoDevLogin` asserts a 404 for
every variant, including `/auth/dev/info`, a correct `DEV_AUTH_KEY` and a `dev_key` cookie).

## Tests

```sh
go vet ./... && go test ./...            # unit + integration (~10 s, most of it RSA key generation)
go test -tags dev ./internal/api ./cmd/... # the dev-login build
node ../scripts/contract-check.mjs        # JSON endpoints vs the zod contract (needs `npm ci` at the repo root)
```

Ported from v1: `tests/core/*` (money, fiscal, settlement, Luma CSV, metrics, lifecycle, auth,
contributions, journal, accounting engine and reports, safe redirects), `tests/db/*` (integrity
triggers, seed), `tests/server/api.test.ts` (every scenario, against a real seeded SQLite file with
a fixed clock) and `tests/server/drive.test.ts` (JWT, shared-drive corpus, folder races). New:
search, Discord sign-in against a fake Discord (PKCE, allowlist, bootstrap leadership), session
sliding, the Origin check, static serving, `healthcheck`.

`scripts/contract-check.mjs` bundles `src/core/contracts.ts`, starts the server on a seeded temp
database and validates exercised request bodies, queries and responses against `ENDPOINTS`:
all GET endpoints plus a purchase lifecycle (create → attach → submit → approve by a second
director → fulfil → close), a restricted-grant reimbursement, the purchasing-agent flow including an
over-tolerance hold, Luma ingest, CSV import, and the director writes. It fails on any schema error,
unexpected status, or non-exempt endpoint left unexercised. Six auth/Google redirect endpoints are explicitly exempt and covered by Go tests. The documentation check validated 190 responses across 94 of 100 endpoint definitions.

The seeded FY2027 statements foot (printed by the check and asserted in `TestSeededFY2027StatementsFoot`):

```
unrestricted  $1,800.00 − $591.40 + $186.40 released = $1,395.00
net assets ending $11,908.60 = total assets $11,908.60   (balanced)
```

During the original Go port, the Go seed was also diffed against the TypeScript seed: all 15 tables are byte-identical.

## Live front-end development

Keep the seeded backend from `make dev` running. In a second terminal, from the repo root:

```sh
cd web
API_ORIGIN=http://127.0.0.1:8090 npm run dev
```

Open <http://127.0.0.1:4340/login>. The dev proxy forwards `/api` and `/auth`, rewrites Origin and redirects, and strips cookie Domain so dev sign-in works. Its unconfigured default `:4330` belongs to the removed v1 application. See [front-end reference](frontend.md).

## Browser checks

From `web/`, with a seeded backend:

```sh
API_ORIGIN=http://127.0.0.1:8090 npm run test:e2e
```

Playwright uses Chromium at phone 390×844, tablet 820×1180, desktop 1440×900, and covers light/dark themes. It reuses the server on :4340 or starts one. `WEB_ORIGIN` targets an existing server instead; `WEB_PORT` changes the test dev port. Lifecycle tests create `agent-e2e-…` purchases. Install Chromium with Playwright if it is absent. Screenshot instructions and source layout are in [front-end reference](frontend.md).

Historical test-porting notes above describe v1-to-Go parity; the v1 sources and seed scripts now live in git history. Current schema migrations also include V0.3 tables.
