# Go backend

The Go API serves the static Astro front end over one SQLite database. Build with Go matching [go.mod](go.mod) (minimum 1.26, with the build toolchain selected by its `toolchain` directive) and a C compiler (cgo). Targets require GNU Make and a POSIX shell.

From this directory, `make dev` builds the dev-login binary, resets `data/dev.db`, and serves `../web/dist` on port 8090. Build the front end first using the [root quick start](../README.md#quick-start).

- [Development and checks](../docs/development.md)
- [Configuration](../docs/configuration.md)
- [Fresh install](../docs/deploy/fresh-install.md) and [Upgrade/restore](../docs/deploy/upgrade-existing.md) (deployment, backups, upgrades)
- [Architecture](../docs/architecture.md) and [historical performance measurements](../docs/performance.md)
- [Accounting](../docs/accounting.md), [integrations](../docs/integrations.md), [roles and ownership recovery](../docs/roles-and-permissions.md)
- [API reference](../docs/api.md) · [All docs](../docs/README.md)
