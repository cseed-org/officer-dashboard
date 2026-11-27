# Configuration

Every setting the dashboard reads, with whether you need it and its default. To deploy, start with
[Fresh install](deploy/fresh-install.md); it tells you which of these to set and
when. Configure the required core and backup settings first. Google services,
notifications, analytics, and membership sync are enabled separately as needed.

**Where settings go.** With Docker, the dashboard's settings go in `server/deploy/dashboard.env`
(copy [`dashboard.env.example`](../server/deploy/dashboard.env.example)), and compose's own
settings in `server/deploy/.env` (copy [`.env.example`](../server/deploy/.env.example)).
Backup-only settings belong in `backup.env` (copy
[`backup.env.example`](../server/deploy/backup.env.example)); the image pin belongs
in `prod-image.env`. Pass both `.env` and `prod-image.env` to production Compose commands. Under
systemd, the dashboard's settings go in `/etc/officer-dashboard/env`. Command-line flags, where
shown, override the variable.

## Compose settings (`server/deploy/.env`)

Read by `compose.cloudflare.yml`, not by the dashboard.

| Variable | Needed? | Default | Meaning |
| --- | --- | --- | --- |
| `APP_URL` | **required** | – | the public `https` address, no slash at the end. Passed to the dashboard; wins over `dashboard.env` |
| `TUNNEL_TOKEN_FILE` | **required** | – | host file containing the Cloudflare Tunnel's token ([Cloudflare step 1](deploy/cloudflare.md#1-create-the-tunnel)) |
| `DASHBOARD_IMAGE` | **required** | empty | approved immutable image reference in `prod-image.env`; local tags belong in the separate development stack |
| `COMPOSE_PROJECT_NAME` | optional | `officer-dashboard` | Compose resource namespace; use a distinct name and a distinct `TUNNEL_NET` for each install sharing a host |
| `TUNNEL_NET` | optional | `10.250.89` | first three numbers of the private network between the two containers: subnet `.0/28`, containers `.8/29` (also the dashboard's `TRUSTED_PROXIES`), gateway `.1`. Change it if compose says the pool overlaps |

Compose also sets the dashboard's `APP_URL`, `ADDR` (`:8090`) and `TRUSTED_PROXIES`
(`${TUNNEL_NET}.8/29`) itself; values for them in `dashboard.env` are ignored.

## Dashboard settings

### Core

| Variable (flag) | Needed? | Default | Meaning |
| --- | --- | --- | --- |
| `APP_URL` (`-app-url`) | **required** in production | `http://localhost:<port>` | public origin, used for OAuth redirects, links and cookies. Must be `https` (plain `http` only for localhost), or the server refuses to start. Listening beyond loopback without it also refuses to start |
| `DISCORD_CLIENT_ID`, `DISCORD_CLIENT_SECRET` | **required** to sign in | – | the Discord app; redirect URI `${APP_URL}/auth/callback`. Without them the server starts but nobody can sign in |
| `BOOTSTRAP_OWNER_DISCORD_ID` | **required** on first start | – | the first owner's Discord user ID, 17 to 20 digits (anything else stops startup). Applied only while the dashboard has no owner |
| `BOOTSTRAP_OWNER_NAME` | optional | “Dashboard owner” | the first owner's display name; an existing name is kept if blank |
| `BOOTSTRAP_DIRECTOR_DISCORD_ID` | deprecated | – | old name for `BOOTSTRAP_OWNER_DISCORD_ID`, which wins |
| `ADDR` (`-addr`) | set for you | `127.0.0.1:8090` | listen address. The container sets `:8090`, the systemd unit `127.0.0.1:8090` |
| `SETUP_DOCS_URL` | optional | CSEED repository configuration guide | setup page documentation link; override for your own documentation |
| `DB_PATH` (`-db`) | set for you | `./data/dashboard.db` | the SQLite file (its folder is created); resolved to an absolute path and logged at server startup. The container sets `/data/dashboard.db`, the systemd unit `/var/lib/officer-dashboard/dashboard.db` |
| `TRUSTED_PROXIES` | set for you in compose | loopback (`127.0.0.0/8`, `::1`) | comma-separated addresses or ranges of reverse proxies whose `X-Forwarded-For` names the visitor, for per-visitor rate limits. Read right to left, skipping trusted hops, so a visitor can't fake it. A malformed entry stops startup. Set it when your proxy is not on the same machine's loopback (a Docker network, for example `172.16.0.0/12`). A request a trusted proxy forwards with a host other than `APP_URL`'s is logged once as a warning |
| `CLOUDFLARE_ACCESS_TEAM_DOMAIN`, `CLOUDFLARE_ACCESS_AUD`, `CLOUDFLARE_ACCESS_SERVICE_AUD` | optional, set all three together | unset | enables Cloudflare Access RS256 assertion verification on browser/API paths. Use the bare team hostname (`team.cloudflareaccess.com`) and the browser application audience in `CLOUDFLARE_ACCESS_AUD`. Set `CLOUDFLARE_ACCESS_SERVICE_AUD` to the separate service application audience for `/api/ingest`, `/api/agent` and their descendants; other protected paths use the browser audience. Missing, invalid, expired or wrong-audience assertions return 401. Team HTTPS signing keys are cached; redirects and token-provided key URLs are refused. OAuth discovery, registration, token and MCP paths retain their own authentication; `/api/health` stays open for probes. Dashboard sessions and service/OAuth tokens remain required. Assertions for one application cannot authorize the other route group. Existing installations enabling verification must add the service audience before restarting; partial configuration refuses startup. Live ingress and Access policy were not verified |

### Optional features

Each is off until set; the log says "not configured" at startup for each one that is off.

| Variable | Default | Meaning |
| --- | --- | --- |
| `GOOGLE_OAUTH_CLIENT_ID`, `GOOGLE_OAUTH_CLIENT_SECRET` | – | Google sign-in for calendars, per-officer sync (callback `${APP_URL}/auth/google/callback`) and the org calendar (callback `${APP_URL}/auth/org-google/callback`), set up in [Google calendar setup](deploy/google-calendar.md); and the Google account the dashboard stores files as (callback `${APP_URL}/auth/drive-google/callback`, connected by an owner in Setup → Google Drive; see [Integrations](integrations.md#files-in-a-google-shared-drive)). The secret also seals stored Google tokens: changing it means every Google connection must be made again |
| `DRIVE_ROOT_FOLDER_ID` | – | the Shared Drive folder the dashboard files everything under. Usually chosen in Settings instead; when set here it wins and Settings shows it read-only |
| `DRIVE_QUOTA_BYTES` | `100e9` (100 GB) | the storage the daily Drive check measures usage against; owners get a Discord alert (through `DISCORD_WEBHOOK_URL`, else the log) at 70 %. A plain byte count or `e` notation |
| `GOOGLE_SERVICE_ACCOUNT_JSON` | – | the alternative to a connected account: a Google service-account key, accepted **only** as `GOOGLE_SERVICE_ACCOUNT_JSON_FILE` (a path to the key file). A direct value stops startup. It needs `DRIVE_ROOT_FOLDER_ID`, and is used only while no account is connected in Settings |
| `GOOGLE_DRIVE_FOLDER_ID` | – | old name for `DRIVE_ROOT_FOLDER_ID`, which wins |
| `RECEIPTS_DIR` | – | receipts and design files on local disk instead of Drive, for a single machine (not in a container: the files would be lost). Back the folder up yourself. With no file storage, uploads answer 503 |
| `DISCORD_WEBHOOK_URL` | – | purchase and other notifications to a Discord channel; without it they only go to the log |
| `DISCORD_BOT_TOKEN` | – | officer invitation DMs; without it, invites are links you copy |
| `SMTP_HOST`, `SMTP_FROM` | – | email delivery of `.ics` calendar invites and Launchpad invites. Both are needed; without them invites are copyable links. The org calendar doesn't need SMTP: Google sends its invites |
| `SMTP_PORT` | `587` | SMTP port (STARTTLS) |
| `SMTP_USERNAME`, `SMTP_PASSWORD` | – | SMTP sign-in |
| `CLOUDFLARE_API_TOKEN`, `CLOUDFLARE_ACCOUNT_ID` | – | the membership mirror and analytics widgets, from Cloudflare's API. Only for these widgets: backups don't use them |
| `CLOUDFLARE_ZONE_ID` | – | limits analytics to one domain |
| `D1_DATABASE_ID` | – | the existing membership D1 database, separate from backups. Sync reads it; director-authorized member edits update its `People` rows. The dashboard does not create this database. Use a token with D1 Edit permission when enabling member edits |
| `AGENT_SETTLEMENT_TOLERANCE_PERCENT`, `AGENT_SETTLEMENT_TOLERANCE_CENTS` | `10`, `2500` | the purchasing agent's hold ceiling: the larger of estimate × 1.10 and estimate + $25 |
| `STATIC_DIR` (`-static`) | the built-in front end | serve the front end from this folder instead. Only release builds (`embedweb`, as in the image) have one built in |
| `STATIC_FALLBACK` (`-static-fallback`), `STATIC_FALLBACK_STATUS` (`-static-fallback-status`) | `404.html`, `404` | page for unknown non-API paths. An alternative single-page front end can set `200.html`, `200` |

File storage (receipts and design files) is chosen on every upload, in this order:

1. the Google account an owner connected in Settings, once a root folder is chosen (in
   Settings, or `DRIVE_ROOT_FOLDER_ID`);
2. the service account (`GOOGLE_SERVICE_ACCOUNT_JSON_FILE` plus `DRIVE_ROOT_FOLDER_ID`);
3. `RECEIPTS_DIR`;
4. in a development build with `DEV_AUTH=1`, a `receipts/` folder beside the database.

Otherwise uploads answer 503 `not_configured` ("Google Drive is not connected"), and after
Google revokes the connected account's access, 503 asking an owner to reconnect. A full or
throttling Drive answers 507 `storage_quota`. A file is always downloaded from the backend
its row records; one stored in another backend answers 503 rather than being looked up in
the wrong place.

### Backups (encrypted D1 snapshots)

Read by the separate backup container and the static `dashboard-backup` command, from `backup.env`. The app does not receive these credentials. `backup.py` is an off-box restore client only. Without `BACKUP_SINK_URL`, only the
local data volume stores the database. Set all backup credentials before enabling.

| Variable | Default | Meaning |
| --- | --- | --- |
| `BACKUP_SINK_URL` | empty | HTTPS URL of the dedicated D1 backup Worker; enables hourly encrypted backups |
| `BACKUP_BEARER_SECRET` | empty | same bearer secret configured on the sink Worker; at least 32 bytes |
| `BACKUP_ACCESS_CLIENT_ID`, `BACKUP_ACCESS_CLIENT_SECRET` | empty | Access service token for the backup hostname, separate from dashboard callers |
| `BACKUP_AGE_RECIPIENT` | empty | public age recipient; private identity remains offline |
| `BACKUP_MEMBERSHIP_ACCOUNT_ID`, `BACKUP_MEMBERSHIP_DATABASE_ID` | empty | account and membership D1 database for weekly SQL export |
| `BACKUP_MEMBERSHIP_API_TOKEN` | empty | account-scoped D1 Edit token; can modify all account D1 databases, never given to the sink |
| `DB_PATH` | `/data/dashboard.db` | database read by the backup helper; Compose gives app and backup the same path and volume |
| `DISCORD_WEBHOOK_URL` | empty | backup failure alerts; configure separately in `backup.env` (normally via `_FILE`) |

The same ciphertext also goes to Google Drive: once per UTC day the sidecar publishes
`dashboard-<date>.db.gz.age` plus a sha256 manifest to `drive-outbox/` beside the database,
and the dashboard uploads it to `Backups/daily` (plus a first-of-month `Backups/monthly`
copy) using the app's Drive connection, keeping 30 daily and 12 monthly copies and alerting
on stuck or failed uploads. `dashboard drive-backup list` and `dashboard drive-backup restore`
fetch a verified copy back, still ciphertext; `backup.py restore --input <file>` decrypts it
offline with the age identity. This adds no settings: the uploader reuses the Drive sign-in
and `DB_PATH`.

The image uses BusyBox musl with static dashboard and backup binaries. SQLite, gzip and age encryption are compiled into `dashboard-backup`; Python, an age executable and Debian packages are not shipped. The off-box restore workstation still needs Python 3 and age.
See [fresh install](deploy/fresh-install.md) and [upgrade/restore](deploy/upgrade-existing.md).
Missing DB with backups enabled stops startup after downloading ciphertext for off-box recovery.
`DB_PATH` defaults to `/data/dashboard.db`. Restore refuses to overwrite an existing output.
Compose requires `DASHBOARD_IMAGE` pinned to an approved digest in `prod-image.env`; the host
updater writes that pin after health succeeds. `DASHBOARD_TAG` is no longer consumed.
### Backups (systemd)

Read by `scripts/backup` and its timer.

| Variable | Default | Meaning |
| --- | --- | --- |
| `BACKUP_DIR` | required absolute path | where backups are written (the unit sets `/var/lib/officer-dashboard/backups`) |
| `DB_PATH` | `./data/dashboard.db` | SQLite source; the unit sets `/var/lib/officer-dashboard/dashboard.db` |
| `BACKUP_AGE_RECIPIENT` | required | public age recipient used to encrypt each backup; keep the private identity off-host |
| `KEEP` | `30` | how many backups to keep |
| `BACKUP_REMOTE` | – | an executable hook receiving the encrypted `.age` path as its single argument |

### For callers of the dashboard

`CF_ACCESS_CLIENT_ID` and `CF_ACCESS_CLIENT_SECRET` are a Cloudflare Access service token
([Cloudflare step 5](deploy/cloudflare.md#5-create-a-service-token-for-bots)) for programs that
*call* the dashboard: the Discord bot adapter, and `dashboard mcp-check`, which then also checks
that service calls pass Access. Set both or neither. The dashboard server never reads them, so they
don't belong in `dashboard.env`.

### Development and test deployments

| Variable | Default | Meaning |
| --- | --- | --- |
| `DEV_AUTH` | – | `1` turns on the developer sign-in `/auth/dev`, in a `-tags dev` build only (a release build logs that it ignores it) |
| `DEV_AUTH_KEY` | – | `-tags dev` builds only. When set, every developer sign-in (`POST /auth/dev`), its confirmation page and the login page's Developer mode panel need `key=<DEV_AUTH_KEY>`; a wrong or missing key gets the site's 404. Shareable link: `https://<host>/auth/dev?as=leadership\|director\|officer\|builder\|mentor&key=<KEY>[&next=/events]` (`builder` and `mentor` open the Launchpad portal). **Required, at least 16 characters, whenever `APP_URL` or the listen address is not loopback**: the server refuses to start otherwise. **Test deployments only**: the key signs anyone in as any role except owner, leadership included. `as=owner` works only without a key on a loopback-only local server. Use `openssl rand -hex 24`; see [development](development.md#developer-mode-on-a-test-deployment) |
| `API_ORIGIN` | `http://localhost:8090` | the web dev server's API backend; must match the Go server's `APP_URL`, including scheme, hostname and port |
| `WEB_PORT` | `4340` | Playwright's dev-server port |
| `WEB_ORIGIN` | `http://localhost:${WEB_PORT}` | an existing UI for Playwright; setting it skips starting a dev server |
| `DOCKER_DEV_AUTH_KEY` | `local-docker-development-only` | Compose-only override for the app's `DEV_AUTH_KEY` and the test runner's `E2E_DEV_AUTH_KEY` |
| `E2E_DEV_AUTH_KEY` | empty | Playwright's developer-login key for a keyed test deployment |
| `DEV_ALLOWED_HOSTS` | empty | comma-separated extra hostnames for Astro/Vite's dev server; Compose sets `dashboard` |
| `INTRO_FRAMES` | empty | output directory for the onboarding intro frame captures |
| `SHOTS` | – | turns on screenshot capture in the shots tests |
| `BENCH_LATENCY` | – | turns on the Go latency-report test |

`GO` and `VERSION` are Make overrides (defaults `go` and `git describe --always --dirty`);
`CGO_ENABLED=1` is required to build SQLite support.

## Sessions and security headers

Sessions live in the database (`sessions`, keyed by sha256 of the cookie token). Every session
ends 30 days after sign-in however active the person is: the cookie and stored expiry never pass
`created_at` + 30 days, and the server refuses (and deletes) any older session. Signing in again replaces the browser's previous session. Service
tokens are stored as sha256 hashes. Writes that arrive with a session cookie and a foreign
`Origin` header are refused (403), on top of `SameSite=Lax`.

Every response carries `X-Content-Type-Options: nosniff`, a `Content-Type`, `Permissions-Policy`
and a `Content-Security-Policy`. HTML pages get a policy that allows same-origin scripts plus
the page's own inline scripts by sha256 (computed from the built files when served, so a
rebuild needs no configuration), inline styles, images from this origin, `data:`/`blob:` and
`cdn.discordapp.com` (avatars), and form posts to this origin, Discord and Google; framing,
plugins and `<base>` are refused. Every other response (JSON, files, redirects) gets
`default-src 'none'`.

**Not yet separate:** Google Calendar refresh tokens are sealed with a key derived from
`GOOGLE_OAUTH_CLIENT_SECRET`. A dedicated `TOKEN_SEAL_KEY` (so rotating the OAuth secret does
not orphan stored tokens, and one leaked secret does not expose both) is planned, not built.

Static files are served for every path outside `/api/` and `/auth/`, **without** a session:
the pages are shells and the data behind them is API-protected. The frontend redirects unauthenticated users after `GET /api/me`.

## Club location and time zones

In Settings, leadership and owners set the club location label and IANA default time zone. These live in SQLite (`club_settings`, migration 0012), not environment variables. The initial location is University of Washington, Seattle and the zone is `America/Los_Angeles`. Directors and officers see the values read-only; every club change is audited.

Each officer can choose Club default (initial preference), Automatic, or a manual IANA zone in Settings. Automatic reads the browser’s device zone on page load/focus/visibility return and saves it for server emails; if detection is unavailable, the club zone applies. Manual preferences remain fixed when travelling. The current time preview shows the saved effective zone. Club changes affect everyone using Club default immediately on their next page load or focus.

Event times, calendar/editor inputs, outreach, audit logs and widgets share the effective zone. Date-only deadlines and accounting dates retain their original date. Instants remain stored in UTC; changing a preference never moves an event. Fiscal accounting boundaries retain the established Washington rules. ICS invitations contain UTC instants and display-zone metadata; calendar applications control their own display preferences. Go embeds IANA tzdata so minimal containers can resolve saved zones without system zoneinfo.

Automatic zone changes are saved immediately. If a form has unsaved edits, a notice
appears and display times update after the form is saved or closed. Guest and reflection
CSV imports interpret offset-less timestamps in the club default zone, with a Pacific
fallback; personal display preferences do not affect imports.

## Organization Google calendars

The org calendar reuses `GOOGLE_OAUTH_CLIENT_ID` and `GOOGLE_OAUTH_CLIENT_SECRET` and needs no
other settings. Setup: [Google calendar setup](deploy/google-calendar.md). How it syncs:
[Organization calendar](org-calendar.md).

Cloudflared and build base images are pinned to multi-platform manifest digests in Compose and the Dockerfiles. Update the tag and digest together; `CLOUDFLARED_VERSION` is no longer an override. Application release selection remains separate.

Notion and Google Drive links default to empty. Leadership configures them in Settings → Club links; these values are stored in `club_settings`. Clearing either removes the link. The public club website retains its default.

## Container secret files and isolation

Keep app credentials in `server/deploy/secrets/app`, backup credentials in `secrets/backup`,
and the tunnel token in the host file selected by `TUNNEL_TOKEN_FILE`. These are read-only
mounts in different containers. They are excluded from Git and the Docker build context.
Files must be readable by the consumer UID: 10001 for app/backup, 65532 for cloudflared.
Use mode 0400 on files and 0700 on their host directories with the corresponding owner.
The read-only filesystem still permits SQLite and temporary snapshot work in `/data`,
and bounded scratch work in tmpfs `/tmp`. Limits: app 512 MB/128 PIDs, backup 256 MB/64
PIDs, cloudflared 128 MB/64 PIDs. Adjust deliberately if workload measurements justify it.

Use file paths in env files, not secret values. The binary resolves app file credentials
in memory under both systemd and Docker,
without exporting their contents. Supported app entrypoint keys are
`DISCORD_CLIENT_SECRET_FILE`, `DISCORD_WEBHOOK_URL_FILE`, `DISCORD_BOT_TOKEN_FILE`,
`SMTP_USERNAME_FILE`, `SMTP_PASSWORD_FILE`, `GOOGLE_OAUTH_CLIENT_SECRET_FILE`,
`GOOGLE_SERVICE_ACCOUNT_JSON_FILE`, `CLOUDFLARE_API_TOKEN_FILE` and `DEV_AUTH_KEY_FILE`.
`GOOGLE_SERVICE_ACCOUNT_JSON` is file-only: a private key pasted into the environment
leaks through process listings and compose files, so a direct value stops startup.
The app reads its own values at startup; backup values and their paths are stripped before
app execution. The backup helper reads `BACKUP_BEARER_SECRET_FILE`,
`BACKUP_ACCESS_CLIENT_ID_FILE`, `BACKUP_ACCESS_CLIENT_SECRET_FILE`,
`BACKUP_MEMBERSHIP_API_TOKEN_FILE` and `DISCORD_WEBHOOK_URL_FILE` into process memory,
without exporting their values. Off-box restore accepts the same sink `_FILE` keys.
A missing/empty/unreadable file, or a direct credential plus `_FILE`, fails closed.
Direct values remain compatible for isolated tests and non-container configurations;
the production templates use files. Every `dashboard` subcommand resolves app `_FILE` keys
itself; the image ships no shell-side resolver.

Compose sets `BACKUP_REQUIRED=1`: the app refuses to create a missing production DB.
The backup service stages its encrypted recovery snapshot for off-box decryption.
Compose also sets `DEPLOY_IMAGE` for both processes; startup and the host updater reject
moving tags and require `image@sha256:<64 lowercase hex characters>`. Bootstrap with a
reviewed immutable digest, and use the protected production promotion workflow afterward.

Cloudflared uses its documented [`TUNNEL_TOKEN_FILE`](https://developers.cloudflare.com/tunnel/reference/run-parameters/#token-file) support. Runtime restrictions use the [Compose service controls](https://docs.docker.com/reference/compose-file/services/).

## Owner integration setup

Owners open `/setup` to connect Google Drive and the org Google Calendar, check the
Drive root folder and quota, and see which host integrations are configured.
Settings links to this page. Other officers are redirected home and cannot read
`GET /api/setup/status` (403). Status responses are not cached and contain no
credentials, webhook URLs or secret-file paths.

For Discord login, set `DISCORD_CLIENT_ID` and `DISCORD_CLIENT_SECRET_FILE`.
For invite DMs set `DISCORD_BOT_TOKEN_FILE`; for notifications set
`DISCORD_WEBHOOK_URL_FILE`. SMTP needs `SMTP_HOST` and `SMTP_FROM`, with
`SMTP_USERNAME_FILE` and `SMTP_PASSWORD_FILE` for authentication. Cloudflare
uses `CLOUDFLARE_API_TOKEN_FILE` plus its account/resource IDs. All secret
values stay in host files; Setup has no credential inputs.

The OAuth sealing key is derived from `GOOGLE_OAUTH_CLIENT_SECRET_FILE`;
keep that secret stable so saved Google grants remain readable.
`BOOTSTRAP_OWNER_DISCORD_ID` and `BOOTSTRAP_OWNER_NAME` provision the initial
owner only; absence does not remove an existing owner.

Export status is read when the optional `drive_export_state` table exists.
The backup container owns `BACKUP_*` configuration; the app cannot inspect it.
Backup uploads and outbox age show "not installed" until the backup integration
publishes dashboard-readable status. A configured indicator confirms the
application has configuration, not that delivery or provider authentication
has succeeded. Use the last-run times and provider checks for operational proof.
