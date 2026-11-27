# Development

Use the Docker setup for the fewest tools to install. Native development is an
alternative if you want to run Go and Node directly. Both use sample data.

## Get the code

Install Git, then run:

```sh
git clone https://github.com/cseed-org/officer-dashboard.git
cd officer-dashboard
```

Unless a section says otherwise, run commands from this repository root.

## Run it in Docker

Install Docker with Compose v2. On Windows or macOS, start Docker Desktop; on
Windows, select Linux containers. You do not need Go, Node, or service credentials
on your computer for this setup.

**Every app-container start resets the sample database in the `dev_data` volume.**
Use disposable data only.

```sh
docker compose -f compose.dev.yaml up -d --build --wait
```

Open <http://localhost:4340/login?key=local-docker-development-only>. Choose a
role in **Developer mode** and continue. The app is ready when the services report
healthy. If startup fails, inspect it with:

```sh
docker compose -f compose.dev.yaml ps
docker compose -f compose.dev.yaml logs --tail 50 app web
```

| Address | Purpose |
| --- | --- |
| <http://localhost:4340> | Astro frontend with live updates when you edit web source |
| <http://localhost:8090> | Go backend serving the frontend built into the development image |

Both ports are bound to your computer's loopback interfaces. To change the local
login key, set `DOCKER_DEV_AUTH_KEY` before starting the stack and use that key in
the login URL.

Web source is mounted from your checkout. Rebuild after Go changes or to refresh
the Go-served static site; this restarts and resets the app. Recreate `web`
alongside `app` because they share a network namespace:

```sh
docker compose -f compose.dev.yaml up -d --build --wait app web
```

After changing web dependencies, refresh their volume and rebuild:

```sh
docker compose -f compose.dev.yaml run --rm --no-deps web npm ci
docker compose -f compose.dev.yaml up -d --build --wait app web
```

Stop the stack with `docker compose -f compose.dev.yaml down`. Adding `-v`
also deletes the sample database, installed dependencies, and caches.

### Checks in Docker

With the stack running:

```sh
docker compose -f compose.dev.yaml run --rm --no-deps web npm run check
docker compose -f compose.dev.yaml run --rm --no-deps e2e
```

The browser tests use Astro by default. To check the Content Security Policy
(CSP), which is sent by Go, rebuild the app and web services first, then run:

```sh
docker compose -f compose.dev.yaml run --rm --no-deps -e WEB_ORIGIN=http://dashboard:8090 e2e npx playwright test tests/csp.spec.ts --project=desktop --output=test-results/csp
```

Browser artifacts appear under `test-results/docker/`. Each run clears its output
directory; use separate `--output=test-results/<name>` paths for concurrent runs.
Tests create records, so run them only against disposable data.

## Native setup

Install:

- Node 24 and npm, matching the root and web package manifests.
- Go matching [server/go.mod](../server/go.mod): minimum 1.26, with toolchain
  `go1.27.4` selected by the module. Go may download that toolchain automatically.
- A C compiler for SQLite's cgo bindings, GNU Make, and a POSIX shell.

On Windows, use WSL for the native commands below. They use Bash-style
environment assignments. For an Ubuntu-based WSL environment, the compiler and
Make are available through `build-essential`; install Go and Node separately.

Build the frontend:

```sh
cd web
npm ci
npm run build
cd ..
```

Start the backend:

```sh
cd server
make dev
```

**Every `make dev` resets `server/data/dev.db`.** It starts a development build
on port 8090 and serves `web/dist`. Open
<http://localhost:8090/auth/dev?as=director> and press **Continue**.
You can also use `as=officer` or `as=leadership`.
Stop the process with Ctrl+C.

## Live front-end development

Keep the native backend running. In a second terminal, from the repository root:

```sh
cd web
API_ORIGIN=http://localhost:8090 npm run dev
```

Open <http://localhost:4340/login> and use **Developer mode**.
Astro forwards API and authentication requests to Go. Keep the backend's
`APP_URL` consistent with `API_ORIGIN` when changing the default address.
See [Frontend](frontend.md) for proxy behavior and source layout.

## Checks

Install the tooling dependencies at the repository root:

```sh
npm ci
```

Run checks from the directory shown. Each command assumes you start there.

| Directory | Check | Command |
| --- | --- | --- |
| Repository root | Script tests | `npm run test:scripts` |
| Repository root | Markdown links and anchors | `node scripts/check-doc-links.mjs` |
| Repository root | API contracts | `npm run contract-check` |
| Repository root | Generated MCP tools | `node scripts/gen-mcp-tools.mjs --check` |
| `server/` | Go analysis and tests, including dev login | `make vet && make test` |
| `web/` | Frontend build and types | `npm ci && npm run build && npm run check` |

The contract checker builds its own Go server and seeds a temporary database.
It requires the native Go and C compiler prerequisites.

### Browser checks

Install both root and web dependencies. With a seeded native backend running,
run from `web/`:

```sh
npm ci
npx playwright install chromium
API_ORIGIN=http://localhost:8090 npm run test:e2e
```

Playwright starts or reuses Astro on port 4340; it does not start the backend.
After building the frontend, also test the site served by Go:

```sh
WEB_ORIGIN=http://localhost:8090 npm run test:e2e
```

This second pass covers CSP, production assets, and Go redirects. Tests cover
phone, tablet, and desktop layouts. See [Contributing](../CONTRIBUTING.md) for
change-specific checks and screenshot requirements.

## Developer mode on a test deployment

This is an advanced option for a shared test server with disposable data.
Developer login requires both a `-tags dev` build and `DEV_AUTH=1`.
Production builds have no developer-login route.

From `server/`, build and run with a secret key:

```sh
make build-dev
export DEV_AUTH_KEY=$(openssl rand -hex 24)
APP_URL=https://test.example.org ADDR=:8090 DEV_AUTH=1 ./bin/dashboard-dev serve
```

A non-loopback test deployment requires a key of at least 16 characters. Keep the
generated key private and save it in your password manager when creating a test link. Share a link such as:

```text
https://test.example.org/login?key=<KEY>
https://test.example.org/auth/dev?as=officer&key=<KEY>&next=/events
```

Anyone with the key can sign in as a non-owner role, including leadership.
Keyed deployments refuse owner login. Never use a dev build with production data.
See [Configuration](configuration.md) for developer settings.
