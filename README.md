# CSEED Officer Dashboard

Manage club events, people, purchases, and finances in one place. Officers sign in
with Discord; Launchpad builders and mentors use a separate portal.

[![CI](https://img.shields.io/github/actions/workflow/status/cseed-org/officer-dashboard/ci.yml?branch=main&label=CI)](https://github.com/cseed-org/officer-dashboard/actions/workflows/ci.yml)
[![Astro](https://img.shields.io/github/package-json/dependency-version/cseed-org/officer-dashboard/astro?filename=web%2Fpackage.json)](web/package.json)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)

## What it does

- Plan events, meetings, and weekly programs on a shared calendar.
- Manage officers, members, external contacts, and design requests.
- Submit purchases, attach receipts, review budgets, and report on finances.
- Run Launchpad cohorts, mentor assignments, and builder check-ins.
- Connect Google Drive, Google Calendar, and AI agents through MCP.

The Go server serves the Astro + Preact website and JSON API. SQLite stores the
application data; Cloudflare D1 provides the optional membership mirror and the
production backup store. See [architecture](docs/architecture.md) for details.

## Quick start

To try the app with sample data, install Git and Docker with Compose v2. On
Windows or macOS, start Docker Desktop; on Windows, use Linux containers.
Run these commands in a terminal:

```sh
git clone https://github.com/cseed-org/officer-dashboard.git
cd officer-dashboard
docker compose -f compose.dev.yaml up -d --build --wait
```

Open <http://localhost:4340/login?key=local-docker-development-only>, choose a
role in **Developer mode**, and continue. No Discord or Google credentials are
needed for this local setup. The first build downloads dependencies and may take
several minutes.

**Each app-container start replaces the sample database.** Use this stack only
with disposable development data. Stop it from the repository root:

```sh
docker compose -f compose.dev.yaml down
```

For native setup, live editing, and checks, read [Development](docs/development.md).

## Deploy

Follow [Fresh install](docs/deploy/fresh-install.md) to deploy on a 64-bit Linux
host, including a Raspberry Pi. It covers the published image, Cloudflare Tunnel
and Access, Discord sign-in, encrypted backups, and approved updates.

For an existing installation, use [Updates and recovery](docs/deploy/upgrade-existing.md).
Production setup uses different configuration and credentials from local development.

## Documentation

| You want to… | Start here |
| --- | --- |
| Find a guide | [Documentation index](docs/README.md) |
| Change settings or connect services | [Configuration](docs/configuration.md) |
| Contribute code or documentation | [Contributing](CONTRIBUTING.md) |
| Understand endpoints or permissions | [API](docs/api.md) · [Roles and permissions](docs/roles-and-permissions.md) |
| Get help or report a problem | [Support](SUPPORT.md) |

Accounting reports are preparatory rollups. Have a qualified accountant review
them before using them for real books or tax filings.

## Project layout

| Directory | Contents |
| --- | --- |
| [server/](server/README.md) | Go backend, SQLite migrations, and deployment files |
| [web/](web/README.md) | Astro + Preact frontend and browser tests |
| [src/core/](src/core/) | Shared API contracts and domain types |
| [scripts/](scripts/) | Contract checks, documentation checks, and release tooling |
| [docs/](docs/README.md) | Setup, operations, and feature guides |

## Security and license

Report vulnerabilities using [SECURITY.md](SECURITY.md). Follow the
[Code of Conduct](CODE_OF_CONDUCT.md) when contributing.

Code is licensed under [Apache-2.0](LICENSE); see [NOTICE](NOTICE).
The CSEED name and logos are reserved; see [TRADEMARKS.md](TRADEMARKS.md).
