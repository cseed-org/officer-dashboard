# Contributing

Start with [development setup](docs/development.md) for prerequisites, local services, seed data and sign-in. Follow our [Code of Conduct](CODE_OF_CONDUCT.md).

## Branches and pull requests

- Branch from the latest `origin/main`; keep each branch focused on one change.
- Open an issue for substantial behavior changes, then link it in a pull request targeting `main`.
- Explain the problem, resulting behavior and validation. Keep unrelated cleanup separate.
- Use a Conventional Commit PR title, such as `feat: add calendar export` or `fix: handle expired sign-in`. The squash merge subject drives release notes and version bumps; `pr-title` checks it.
- Wait for CI and maintainer review; address feedback before merging.

## Checks

These are the core checks in [CI](.github/workflows/ci.yml). Run each block from the indicated directory. Go requires cgo and a C compiler for SQLite. CI uses Node 24 and the Go version in `server/go.mod`.

From `server/` (Bash; set `GITHUB_SHA` to the commit SHA when running the release build locally):

```bash
test -z "$(gofmt -l .)" || { gofmt -d .; exit 1; }
go vet ./...
go vet -tags dev ./...
go test ./...
go test -tags dev ./internal/api ./cmd/...
CGO_ENABLED=1 go build -trimpath -ldflags "-s -w -X main.version=${GITHUB_SHA::7}" -o /tmp/dashboard ./cmd/dashboard
```

From `web/`:

```sh
npm ci
npm run build
npx astro check
node --test integrations/astro-config.test.mjs
```

From the repository root:

```sh
npm ci
node scripts/gen-mcp-tools.mjs --check
npm run test:scripts
node scripts/contract-check.mjs
node scripts/check-doc-links.mjs
```

`npm run contract-check` is the root script alias for `node scripts/contract-check.mjs`. It builds the Go server, seeds a temporary database and validates API behavior against the zod contracts.

For updater and offline-restore changes, install Python 3.11+ and also run the CI fault-regression suite from the root:

```sh
python3 -m unittest discover -s scripts -p "test_*.py" -v
```

## Database and API changes

- Add the next zero-padded, numbered SQL file under `server/internal/store/migrations/` after the highest existing file. The filename `NNNN_<description>.sql` is an illustrative example only; calculate the next number from the migrations present in your branch. SQL migrations are embedded in the Go binary; preserve existing migrations and test both fresh databases and upgrades.
- Tests derive the migration count with `store.MigrationCount()`; do not hard-code a new count.
- Keep `src/core/contracts.ts` and `web/src/lib/contracts.ts` in sync for shared API schemas, along with their domain types. Preserve web-specific additions. Update the Go handlers and contract scenarios when behavior changes, then run `npm run contract-check`.
- After changing `ENDPOINTS` or `scripts/gen-mcp-tools.mjs`, run `npm run mcp-tools` and commit `server/internal/api/mcp_tools.json`.

## UI changes

Follow the [design specification](web/design/spec.md). Include screenshots at desktop 1440px and phone 390px in both light and dark themes.

Playwright lives in `web/tests/`. CI runs browser-independent unit/security-link tests and selected Compose unit/Home speed tests; run the full suite locally. After following the development guide to start a seeded backend and configure the frontend proxy, run from `web/`:

```sh
npm ci --prefix ..
npm ci
npx playwright install chromium
npm run test:e2e
```

The config starts or reuses the web dev server; it does not start the backend. It covers phone, tablet and desktop; tests exercise light and dark themes. Use a disposable local database because lifecycle tests create records. The default Astro run skips CSP, production module-preload and Go redirect checks. After building `web/dist` and starting the seeded Go dev server, run a second pass from `web/`:

```sh
WEB_ORIGIN=http://localhost:8090 npm run test:e2e
```

This targets the built Go-served site. See the [Docker CSP recipe](docs/development.md#run-it-in-docker) for a focused container run.

The CSEED name, logos and brand imagery are reserved. Before deploying a fork, replace the brand assets; see [TRADEMARKS.md](TRADEMARKS.md).

## Good first areas

- Improve setup instructions or reproduce and document a bug.
- Fix keyboard navigation, accessible labels or empty states.
- Add focused API contract scenarios for existing behavior.
- Improve responsive layouts using the design specification.

For questions and reports, see [SUPPORT.md](SUPPORT.md).

## Releases

Maintainers: see [Releases and dependency updates](docs/releases.md) for release
approval, image publication, automation, and repository setup. Ordinary
contributions do not need release credentials.
