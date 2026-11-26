# Development

## Quick start

Needs Go matching [server/go.mod](../server/go.mod) (minimum 1.26, with the build
toolchain selected by its `toolchain` directive), a C compiler (cgo, for SQLite),
Make, a POSIX shell and Node ≥ 22.23.4. On Windows use Bash/WSL for these shell
commands, or use the Docker stack below to build Go and SQLite inside Linux.

```sh
(cd web && npm ci && npm run build)   # static front end -> web/dist
cd server && make dev                 # seeds ./data/dev.db, serves with the dev login
```

See [Deploy the dashboard, step by step](deploy/README.md) and [configuration](configuration.md) for deployment and settings, and
[front-end guide](frontend.md) for the front-end dev server.

## Run it in Docker

With Docker Desktop running Linux containers, from the repository root:

```sh
docker compose -f compose.dev.yaml up -d --build --wait
docker compose -f compose.dev.yaml run --rm --no-deps e2e npx playwright test tests/onboarding-intro.spec.ts tests/onboarding-plan.spec.ts --output=test-results/intro
```

The seeded Go dev build serves <http://localhost:8090/login>; Astro with hot reload
serves <http://localhost:4340/login>. For developer sign-in, append
`?key=local-docker-development-only` to `/login`. Override `DOCKER_DEV_AUTH_KEY`
in your environment to change this local-only key; the test runner uses the same value.
Both ports bind to IPv4 (`127.0.0.1`) and IPv6 (`::1`) host loopback, so
`localhost` also works in browsers that try IPv6 first. They are not exposed on
LAN interfaces. Apply changed port bindings on the next coordinated stack
recreation. Every app-container start runs `seed -reset`,
replacing the development database in the `dev_data` volume.

Source is bind-mounted directly (writable for Astro's endpoint generator); the former `Sapling.tsx` / `sapling.ts` case
clash was removed by renaming the markup module to `saplingMarkup.ts`.
Dependencies and Astro's cache use named volumes; polling supports
hot reload on Windows. After dependency changes, run
`docker compose -f compose.dev.yaml run --rm --no-deps web npm ci`, then rebuild
the stack. The Go server's static build updates on rebuild; Astro updates live.
The web service also mounts the current tests and TypeScript configuration. To
check them without restarting the running stack, use
`docker compose -f compose.dev.yaml run --rm --no-deps web npm run check`.
Playwright and its matching browser image (pinned in [compose.dev.yaml](../compose.dev.yaml)) write traces and failure artifacts
to `test-results/docker/`. Every Playwright run clears its output directory.
Use `run --rm --no-deps e2e` against the running stack so testing does not restart
or reseed the services. Concurrent runs must choose distinct paths, e.g. append
`--output=test-results/engine` or `--output=test-results/screen` after the test arguments.
To capture intro frames, add `-e INTRO_FRAMES=/artifacts/frames/intro` before `e2e`;
frames appear in `test-results/docker/frames/intro/`. Keep the Playwright output
in its own subdirectory (`--output=test-results/intro`) so cleanup does not remove
the frame directory. In Git Bash, prefix commands that pass container paths with
`MSYS_NO_PATHCONV=1` to prevent conversion of `/artifacts/...` to a Windows path.

The Compose default `WEB_ORIGIN=http://dashboard:4340` targets Astro, which sends
no Content-Security-Policy. CSP tests require the Go server's **rebuilt static site**:

```sh
docker compose -f compose.dev.yaml up -d --build --wait app web
docker compose -f compose.dev.yaml run --rm --no-deps -e WEB_ORIGIN=http://dashboard:8090 e2e npx playwright test tests/csp.spec.ts --project=desktop --output=test-results/csp
```

Rebuilding restarts and reseeds the development app; coordinate this with anyone
running tests. Recreate `web` alongside `app` because they share a network namespace.
Direct e2e traffic to `:8090` is not loopback and can hit the 30-per-minute sign-in
limit in long batches. The Astro proxy uses loopback and the existing development
exemption; production rate limits are unchanged. `DEV_ALLOWED_HOSTS` configures
Vite's public `server.allowedHosts` option for the internal `dashboard` hostname.

Stop with `docker compose -f compose.dev.yaml down`; add `-v` to also remove
the development database, dependencies and cache.

## Checks

| Check | Command |
| --- | --- |
| Go vet + tests (including dev sign-in) | `cd server && make vet && make test` |
| API contract | `npm ci && npm run contract-check` (builds and runs the Go server itself) |
| Front end build + types | `cd web && npm ci && npm run build && npx astro check` |
| Front end e2e | `cd web && npx playwright install chromium && API_ORIGIN=http://localhost:8090 npm run test:e2e` (full local Playwright suite) |

CI ([ci.yml](../.github/workflows/ci.yml)) runs Go, contract and web checks, plus release
smoke tests and a selected Compose Playwright suite, on pushes to `main` and pull requests.
Markdown changes also run [docs.yml](../.github/workflows/docs.yml): relative links must
pass offline; external checks are advisory and accept rate-limit responses.

## Run it

Use the Go toolchain, C compiler and POSIX shell described above. From `server/`:

```sh
make dev                 # seeds ./data/dev.db, serves on :8090 with the dev login
# or by hand:
CGO_ENABLED=1 DB_PATH=./data/dev.db go run -tags dev ./cmd/dashboard seed -reset
CGO_ENABLED=1 DEV_AUTH=1 DB_PATH=./data/dev.db STATIC_DIR=../web/dist go run -tags dev ./cmd/dashboard serve
```

Open <http://localhost:8090/auth/dev?as=director> (or `as=officer`, `as=leadership`) and press
**Continue** to sign in without Discord (a GET only shows that confirmation; the button posts the
sign-in form, so a link or image on another site cannot sign anyone in), or use the **Developer mode** panel that `/login` shows under the Discord button
("Skip Discord and continue as" Leadership / Director / Officer; never an owner account). The panel appears
whenever the server answers `GET /auth/dev/info` with `authorized: true`, so the same static
build looks like production everywhere else.

### Developer mode on a test deployment

A shared test deployment can run the dev build so testers skip Discord, gated by a secret:

```sh
make build-dev
APP_URL=https://test.example.org ADDR=:8090 DEV_AUTH=1 DEV_AUTH_KEY=$(openssl rand -hex 24) ./bin/dashboard-dev serve
```

On any non-loopback `APP_URL` the dev build refuses to start with `DEV_AUTH=1` unless
`DEV_AUTH_KEY` is set and at least 16 characters long.

With `DEV_AUTH_KEY` set, `/auth/dev` and the login panel need the key; without it both look
exactly like a production build (`/auth/dev` answers the site's 404 page, no hint). Share links
of the form

```text
https://<host>/auth/dev?as=leadership&key=<KEY>                 # confirm, then land on Home
https://<host>/auth/dev?as=officer&key=<KEY>&next=/events       # confirm, then land on Events
https://<host>/login?key=<KEY>                                  # show the Developer mode panel
```

`as` is `leadership`, `director` or `officer` for the dashboard, or `builder` or `mentor` for
the Launchpad portal; `next` must be a path on the same site (anything else lands on `/`). Every
sign-in carries the key (the confirmation page and the panel post it with the form); nothing
remembers it, so a signed-out visitor needs a keyed link again. A form posted from another site
is refused (403), and `discord=<id>` sign-ins cannot target leadership or owner accounts. On a
keyed deployment, dev login never signs in as a dashboard owner: `as=leadership` picks the oldest
non-owner leadership account (creating a "Dev leadership" placeholder if there is none), and
`user=<owner id>` is refused (403). `as=owner` works only without `DEV_AUTH_KEY`, i.e. on a
loopback-only local server. The key is never logged (request logs record the path only).

**Test deployments only.** Anyone holding the key can sign in as any non-owner role, leadership included.
Never point a dev build at real data, and never deploy one to production.

**The dev login cannot exist in production.** `/auth/dev` is compiled in only with `-tags dev`
*and* answers only when `DEV_AUTH=1`. The default build has no such route; setting `DEV_AUTH=1`
on it logs a warning and changes nothing (`TestDefaultBuildHasNoDevLogin` asserts a 404 for
every variant, including `POST /auth/dev`, `/auth/dev/info` and a correct `DEV_AUTH_KEY`).

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
unexpected status, or non-exempt endpoint left unexercised. Seven endpoints are exempt
(auth/Google redirects and the dev-build-only `/auth/dev` routes) and are covered by Go tests.

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
API_ORIGIN=http://localhost:8090 npm run dev
```

Open <http://127.0.0.1:4340/login>. The dev proxy forwards `/api` and `/auth`, rewrites Origin and redirects, and strips cookie Domain so dev sign-in works. Its default backend is `http://localhost:8090`, matching `APP_URL`. See [front-end reference](frontend.md).

## Browser checks

From `web/`, with a seeded backend:

```sh
npx playwright install chromium
API_ORIGIN=http://localhost:8090 npm run test:e2e
```

Playwright uses Chromium at phone 390×844, tablet 820×1180, desktop 1440×900, and covers light/dark themes. It reuses the server on :4340 or starts one. `WEB_ORIGIN` targets an existing server instead; `WEB_PORT` changes the test dev port. Lifecycle tests create `agent-e2e-…` purchases. Install Chromium with Playwright if it is absent. Screenshot instructions and source layout are in [front-end reference](frontend.md).

Historical test-porting notes above describe v1-to-Go parity; the v1 sources and seed scripts now live in git history. Current schema migrations also include V0.3 tables.
