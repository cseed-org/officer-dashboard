# Go backend

The Go API serves the static Astro front end over one SQLite database. Build with Go 1.24+ and a C compiler (cgo).

From this directory, `make dev` builds the dev-login binary, resets `data/dev.db`, and serves `../web/dist` on port 8090. Build the front end first using the [root quick start](../README.md#quick-start).

- [Development and checks](../docs/development.md)
- [Configuration](../docs/configuration.md)
- [Deployment and backups](../docs/deployment.md)
- [Architecture](../docs/architecture.md) and [historical performance measurements](../docs/performance.md)
- [Accounting](../docs/accounting.md), [integrations](../docs/integrations.md), [roles and ownership recovery](../docs/roles-and-permissions.md)
- [API reference](../docs/api.md) · [All docs](../docs/README.md)
