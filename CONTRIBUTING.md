# Contributing

Start with [development setup](docs/development.md) for prerequisites, local services, seed data and sign-in. Follow our [Code of Conduct](CODE_OF_CONDUCT.md).

## Branches and pull requests

- Branch from the latest `origin/main`; keep each branch focused on one change.
- Open an issue for substantial behavior changes, then link it in a pull request targeting `main`.
- Explain the problem, resulting behavior and validation. Keep unrelated cleanup separate.
- Use a Conventional Commit PR title, such as `feat: add calendar export` or `fix: handle expired sign-in`. The squash merge subject drives release notes and version bumps; `pr-title` checks it.
- Wait for CI and maintainer review; address feedback before merging.

## Checks

These are the exact commands in [CI](.github/workflows/ci.yml). Run each block from the indicated directory. Go requires cgo and a C compiler for SQLite. CI uses Node 24 and the Go version in `server/go.mod`.

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
```

From the repository root:

```sh
npm ci
node scripts/gen-mcp-tools.mjs --check
npm run test:scripts
node scripts/contract-check.mjs
```

`npm run contract-check` is the root script alias for the last command. It builds the Go server, seeds a temporary database and validates API behavior against the zod contracts.

## Database and API changes

- Add the next zero-padded, numbered SQL file under `server/internal/store/migrations/` after the highest existing file. The filename `0026_<description>.sql` is an illustrative example only, not a fixed next number; calculate it from the migrations present in your branch. SQL migrations are embedded in the Go binary; preserve existing migrations and test both fresh databases and upgrades.
- Tests derive the migration count with `store.MigrationCount()`; do not hard-code a new count.
- Keep `src/core/contracts.ts` and `web/src/lib/contracts.ts` in sync for shared API schemas, along with their domain types. Preserve web-specific additions. Update the Go handlers and contract scenarios when behavior changes, then run `npm run contract-check`.
- After changing `ENDPOINTS` or `scripts/gen-mcp-tools.mjs`, run `npm run mcp-tools` and commit `server/internal/api/mcp_tools.json`.

## UI changes

Follow the [design specification](web/design/spec.md). Include screenshots at desktop 1440px and phone 390px in both light and dark themes.

Playwright lives in `web/tests/` and runs locally, outside CI. After following the development guide to start a seeded backend and configure the frontend proxy, run from `web/`:

```sh
npm run test:e2e
```

The config starts or reuses the web dev server; it does not start the backend. It covers phone, tablet and desktop; tests exercise light and dark themes. Use a disposable local database because lifecycle tests create records.

The CSEED name, logos and brand imagery are reserved. Forks must replace the brand assets; see [TRADEMARKS.md](TRADEMARKS.md).

## Good first areas

- Improve setup instructions or reproduce and document a bug.
- Fix keyboard navigation, accessible labels or empty states.
- Add focused API contract scenarios for existing behavior.
- Improve responsive layouts using the design specification.

For questions and reports, see [SUPPORT.md](SUPPORT.md).

## Releases and dependency updates

Every push to `main` runs release-please with a GitHub App token. It maintains one
release PR containing `CHANGELOG.md`, `version.txt`, the release manifest, and both
Node package versions and lockfiles. The first release is `0.15.0`; later versions
follow Conventional Commits (`feat`, `fix`, `perf`, `deps`, `deps-dev`, `docs`).
Review the proposed version and notes, wait for all required checks, then squash
merge the release PR with its generated `chore(main): release ...` title. Release
PRs require a person to merge them.

Release-please creates the immutable `vX.Y.Z` tag and GitHub Release. The release
image workflow waits up to 20 minutes for that commit's successful main CI run and
`publish` job, then promotes the existing image digest to semantic version tags.
It does not build another image. Failed or missing publication stops promotion;
after resolving CI, rerun the failed release-image workflow. See
[image tags and verification](docs/deployment.md#release-images-and-verification).

Dependabot uses `deps:` / `deps-dev:` titles. Minor and patch PRs enter GitHub's
auto-merge queue and still need every required check to pass. Major updates receive
`major-update` and require manual review and merging.

Maintainers must enable repository auto-merge, create the `major-update` label from
`.github/labels.yml`, and require the `pr-title` check alongside existing checks.
Install a repository-scoped GitHub App with contents, issues and pull requests write
access; save `RELEASE_APP_ID` and `RELEASE_APP_PRIVATE_KEY` as repository secrets.
Allow that App to create `v*` tags through the tag-creation ruleset, while preserving
the separate rules preventing tag updates/deletion. An App token lets release PRs
and GitHub Releases trigger CI; the default `GITHUB_TOKEN` would suppress those runs.
Missing App secrets fail explicitly. Repository settings, secrets, rulesets and
the protected `production` environment require maintainer setup; workflows do not
create them.
