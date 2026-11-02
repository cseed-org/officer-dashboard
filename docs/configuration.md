# Configuration

Environment variables (flags override where shown):

| Variable | Default | Meaning |
| --- | --- | --- |
| `ADDR` (`-addr`) | `:8090` | listen address |
| `DB_PATH` (`-db`) | `./data/dashboard.db` | SQLite file (WAL mode; directory is created) |
| `APP_URL` (`-app-url`) | `http://localhost:<port>` | public origin: OAuth redirect URI, links in notifications; `https://` turns on `Secure` cookies, the `__Host-` cookie prefix and HSTS |
| `STATIC_DIR` (`-static`) | embedded build | serve the front end from this directory instead |
| `STATIC_FALLBACK` (`-static-fallback`), `STATIC_FALLBACK_STATUS` (`-static-fallback-status`) | `404.html`, `404` | page for unknown non-API paths. An alternative SPA front end can set a shell (`200.html`, `200`) |
| `DISCORD_CLIENT_ID`, `DISCORD_CLIENT_SECRET` | – | Discord OAuth2 app; redirect URI `${APP_URL}/auth/callback` |
| `BOOTSTRAP_OWNER_DISCORD_ID` | – | initial owner Discord ID; provisioned on server startup only when no owner exists |
| `BOOTSTRAP_OWNER_NAME` | – | initial owner display name; existing name retained if blank, otherwise “Dashboard owner” for a new user |
| `BOOTSTRAP_DIRECTOR_DISCORD_ID` | – | deprecated ID alias; `BOOTSTRAP_OWNER_DISCORD_ID` takes precedence |
| `DISCORD_WEBHOOK_URL` | – | notifications; with none they are logged |
| `GOOGLE_SERVICE_ACCOUNT_JSON`, `DRIVE_ROOT_FOLDER_ID` | – | receipts in a Google shared drive |
| `RECEIPTS_DIR` | – | receipts on local disk (single box, no Drive). With neither set, uploads answer 503 |
| `AGENT_SETTLEMENT_TOLERANCE_PERCENT`, `_CENTS` | `10`, `2500` | agent hold ceiling: max(estimate × 1.10, estimate + $25) |
| `DEV_AUTH` | – | `1` enables `/auth/dev` in a `-tags dev` build only |

Sessions live in the database (`sessions`, keyed by sha256 of the cookie token, 30-day sliding
expiry). Service tokens are stored as sha256 hashes. Writes that arrive with a session cookie
and a foreign `Origin` header are refused (403), on top of `SameSite=Lax`.

Static files are served for every path outside `/api/` and `/auth/`, **without** a session:
the pages are shells and the data behind them is API-protected. (v1 redirected unauthenticated
page requests to `/login`; the new front end does that client-side from `GET /api/me`.)

## Optional services

All values default to unset unless specified. Source: `server/cmd/dashboard/main.go` and `server/internal/cloudflare/client.go`.

| Variable | Meaning |
| --- | --- |
| `GOOGLE_DRIVE_FOLDER_ID` | Legacy receipt-root alias; `DRIVE_ROOT_FOLDER_ID` takes precedence. |
| `DISCORD_BOT_TOKEN` | Bot token for invitation DMs; separate from OAuth and webhook credentials. |
| `SMTP_HOST` | SMTP server for calendar email delivery. |
| `SMTP_PORT` | SMTP port, default `587`. |
| `SMTP_USERNAME`, `SMTP_PASSWORD` | SMTP authentication credentials. |
| `SMTP_FROM` | Calendar invitation sender. |
| `GOOGLE_OAUTH_CLIENT_ID`, `GOOGLE_OAUTH_CLIENT_SECRET` | Per-officer Calendar OAuth; callback `${APP_URL}/auth/google/callback`. Separate from the Drive service account. |
| `CLOUDFLARE_API_TOKEN` | Cloudflare API bearer token. |
| `CLOUDFLARE_ACCOUNT_ID` | Account for D1 and GraphQL analytics. |
| `CLOUDFLARE_ZONE_ID` | Optional zone analytics scope. |
| `D1_DATABASE_ID` | Membership database to mirror. |

The settlement variables are spelled `AGENT_SETTLEMENT_TOLERANCE_PERCENT` and `AGENT_SETTLEMENT_TOLERANCE_CENTS` (defaults `10` and `2500`). Drive credentials take precedence over local storage. Setting the service-account JSON without a root folder disables receipts (503), even when `RECEIPTS_DIR` is set. Otherwise `RECEIPTS_DIR` selects local storage; enabled dev auth falls back to a `receipts/` directory beside the database. With none configured, storage is unavailable (503). `STATIC_DIR` defaults to the embedded site only in an `embedweb` release build; API-only builds do not embed it.

## Containers and backups

These are consumed by deployment scripts, rather than Go settings:

| Variable | Default | Meaning |
| --- | --- | --- |
| `LITESTREAM_BUCKET` | unset | Enables restore and continuous replication to this bucket. |
| `LITESTREAM_ENDPOINT` | empty | S3-compatible endpoint, e.g. R2. |
| `LITESTREAM_REGION` | `auto` | Replica region. |
| `LITESTREAM_PATH` | `dashboard` | Object key prefix. |
| `LITESTREAM_ACCESS_KEY_ID`, `LITESTREAM_SECRET_ACCESS_KEY` | unset | Replica bucket credentials. |
| `BACKUP_DIR` | `./backups` | Local backup output. |
| `KEEP` | `30` | Number of backups retained. |
| `BACKUP_REMOTE` | unset | Off-box copy command; `{}` is replaced by the backup path. |

The container sets `DB_PATH=/data/dashboard.db` and `ADDR=:8090`; systemd sets `DB_PATH=/var/lib/officer-dashboard/dashboard.db` and `ADDR=127.0.0.1:8090`. See [deployment](deployment.md).

## Development and diagnostics

| Variable | Default | Meaning |
| --- | --- | --- |
| `API_ORIGIN` | `http://127.0.0.1:4330` | Web dev proxy backend; set to `http://127.0.0.1:8090` for Go. |
| `WEB_PORT` | `4340` | Playwright's dev-server port. |
| `WEB_ORIGIN` | `http://localhost:${WEB_PORT}` | Existing UI origin for Playwright; setting it disables automatic dev-server startup. |
| `SHOTS` | unset | Enables screenshot capture in the shots tests. |
| `BENCH_LATENCY` | unset | Enables the Go latency-report test. |

`GO`, `VERSION` are Make overrides (default `go` and `git describe --always --dirty`); `CGO_ENABLED=1` is required to build SQLite support. These are build/tooling settings, not application secrets.
