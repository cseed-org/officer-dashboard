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

## Setup preview (`dashboard setup --dry-run`)

`dashboard setup --dry-run` reads your Cloudflare account and lists, for each piece
of [Cloudflare setup](deploy/cloudflare.md) and the backup Worker, whether it is
`ok`, would be created (`create`), differs from the docs (`drift`), cannot be read
(`unknowable`, for example a secret Cloudflare never returns), or needs a person
(`manual`). It sends only `GET` requests and writes no files. To create what it
lists, see [Setup apply](#setup-apply-scriptssetup-apply).

| Variable | Needed? | Default | Meaning |
| --- | --- | --- | --- |
| `CLOUDFLARE_SETUP_TOKEN_FILE` | optional | – | file holding a Cloudflare API token for setup. Unset: setup asks for the token without echo. Never a flag, never logged. Separate from `CLOUDFLARE_API_TOKEN` |
| `APP_URL` | **required** | – | the dashboard origin to check, also `--app-url` |
| `BACKUP_SINK_URL` | optional | `backups.<parent domain>` | its host is the backup Worker hostname, also `--backup-hostname`. Setup says when it falls back to the default |
| `TUNNEL_TOKEN_FILE` | optional | – | if readable, setup takes only the tunnel ID from it to find the tunnel, also `--tunnel-token-file` or `--tunnel-id` |

Setup finds the tunnel by the ID from the host's tunnel token, then by the tunnel the
dashboard hostname's DNS record points at, then by the name `officer-dashboard`, and
reports any of these that disagree.

Other flags: `--officers-group` and `--launchpad-group` (empty for no Launchpad
group), `--name-prefix` and `--json`. `--name-prefix` prefixes every resource
name, for a staging install beside the real one. A staging install also needs its
own `APP_URL`: on the real install's hostname, setup finds the real install's
applications, rules and tunnel, marks them as drift and prints a warning.

It exits 0 when it printed the plan, whatever the plan says; 1 when it could not
read the account (a bad token, two resources matching one name, a network failure,
or a Cloudflare error other than the two below); and 2 for a usage error. A missing permission (401 or 403) or a Cloudflare
server error (5xx or 429, after retries) on one resource makes only that line
`unknowable`.

Create a token for it under **Manage account** → **Account API tokens**, limited to
the dashboard's account and its one zone, with a short expiry and these read
permissions:

- Account: Cloudflare Tunnel, Access: Apps, Access: Policies, Access: Groups,
  Access: Identity Providers, Access: Organizations, Access: Service Tokens, D1,
  Workers Scripts.
- Zone: Zone, DNS, Zone WAF, Cache Rules, Config Rules, Bot Management.

A missing permission shows as `unknowable` on the lines it affects. Delete the
token and its file when you are done.

On the host, from `server/deploy`, Compose passes `APP_URL` and gives a terminal
for the token prompt. The `dashboard` service sees neither `backup.env` nor the
tunnel token file, so pass the backup hostname (the host in `BACKUP_SINK_URL`)
yourself. Without a tunnel ID, setup finds the tunnel through the hostname's DNS
record; add `--tunnel-id <id>` (Zero Trust → Networking → Tunnels) to match by ID.

```sh
docker compose --env-file .env --env-file prod-image.env -f compose.cloudflare.yml \
  run --rm --no-deps --entrypoint /app/dashboard dashboard \
  setup --dry-run --backup-hostname backups.example.org
```

## Setup apply (`scripts/setup apply`)

`dashboard setup` without `--dry-run` creates what the preview lists as `create`
for the tunnel, its route, the DNS record, the One-time PIN login, the two Access
groups, the `officer-dashboard services` service token, the three Access
applications and the three zone rules. It never deletes anything, never changes
what exists (`drift` is only reported), and never starts or restarts a service.
The backup Worker, D1 and the backup Access pieces are listed as `skipped`: they
stay manual in this version (fresh-install.md step 4).

Run it through the launcher, after `scripts/setup host --yes`:

```sh
<install-dir>/scripts/setup preview --token-file /root/setup/cloudflare-token
<install-dir>/scripts/setup apply --token-file /root/setup/cloudflare-token \
  --officers-email you@example.org --launchpad-email mentor@example.org
```

How it runs:

- **Order.** `host --yes` comes first: it pins the image and writes the env files
  and the `secrets/` directories with their owners. `apply` refuses to start
  without them, and without `TUNNEL_TOKEN_FILE=./secrets/tunnel-token` in `.env`.
  Start the stack only after `apply` reports that every step read back `ok`: it
  creates the Access applications before anything that publishes the hostname,
  but cloudflared must not run before they exist.
- **Confirmation.** It reads the account, prints the plan and the numbered
  steps, and asks you to type the dashboard hostname. `--yes` skips the question
  (the plan is still printed). Without a terminal and without `--yes` it changes
  nothing and exits 1. Just before the first write it reads the account again;
  if the plan, the steps or the warnings are not the ones you were shown, it
  stops without writing.
- **Container.** It runs as root with only `CHOWN`, `DAC_OVERRIDE` and `FOWNER`,
  with network, and with the deploy directory mounted read-write: the secrets
  Cloudflare hands out are written there before the next request. The token file
  is mounted read-only; the token is never in a command line, the environment or
  the output. Without a token file it is asked for in the terminal. A relative
  `--token-file` is relative to the current directory, and
  `CLOUDFLARE_SETUP_TOKEN_FILE` in `.env` to the deploy directory. A symbolic link
  is refused, and a file other users can read gets a warning. The launcher stops
  on rootless Docker, `userns-remap` and SELinux enforcing, as `host` does.
- **Options.** `--officers-email` and `--launchpad-email` (repeatable) are the
  members a group is created with; existing groups are never changed.
  `--name-prefix` works as in the preview, and refuses to apply on another
  install's hostname. Two zone- and account-wide settings stay manual unless you
  allow them: `--allow-bot-fight-mode-off` and `--allow-org-session-write`.

Where the secrets go (mode 0400; an existing file is never replaced):

| Secret | Host file | Owner |
| --- | --- | --- |
| tunnel token | `secrets/tunnel-token` | 65532 |
| services token client ID | `secrets/admin/services-access-client-id` | root |
| services token client secret | `secrets/admin/services-access-client-secret` | root |

The services token is for the Discord bot, the Luma import and the purchasing
agent; no container reads it. It does not expire (setup creates it with the
longest duration, `forever`), so rotating it is your job: Zero Trust → Access
controls → Service credentials → Service Tokens → `officer-dashboard services` →
Rotate, then give the bots the new secret.

Setup writes a secret file only when it creates or fetches the secret, so a new
value is always the one Cloudflare now holds for that tunnel or service token. If
its file already exists, the existing file is older: setup leaves it as it was,
saves the new value beside it as `secrets/admin/<name>.<UTC time>`, and stops,
because the install still reads the old file. The message names the tunnel or
token the new value belongs to and gives the command that puts it in place, for
example `mv secrets/admin/tunnel-token.<time> secrets/tunnel-token && chown
65532:65532 secrets/tunnel-token`. Every later run refuses to start, with the
same explanation, until the new file is in place.

If the services token's client secret is on the host but its client ID file
(`secrets/admin/services-access-client-id`) is not, every run lists it as
`missing`, with where to copy the client ID from, and exits 1 until the file is
there.

At the end it prints what it created, with IDs; what is left as `manual`,
`drift`, `unknowable` or `missing`, with the step for each; where each secret is
(paths only); and a reminder to check the cache rule (it must say **Bypass
cache**) and Bot Fight Mode in the Cloudflare dashboard. It says to start the
stack only when no `drift` or `unknowable` line concerns Access or the
hostname. It exits 0 only when every step was created and read back `ok` (or
there was nothing to create) and no secret file is missing; 1 when it stopped,
was not confirmed, or a secret file is missing; and 2 for a usage error
(including `--json`, which is for `--dry-run` only). A stopped run is safe to
start again: it reads the account first and creates no second copy.

The apply token needs Edit permissions; `dashboard setup -h` prints this list:

- Account: Cloudflare Tunnel Edit, Access: Apps Edit, Access: Policies Edit,
  Access: Groups Edit, Access: Identity Providers Edit, Access: Service Tokens
  Edit, Access: Organizations Read (Edit only with `--allow-org-session-write`),
  D1 Read, Workers Scripts Read.
- Zone: Zone Read, DNS Edit, Zone WAF Edit, Cache Rules Edit, Config Rules Edit,
  Bot Management Read (Edit only with `--allow-bot-fight-mode-off`).

When a permission is missing, Cloudflare answers 401 with code 1001; setup stops,
names the permission and prints the list. Limit the token to the dashboard's
account and zone, give it a short expiry, and delete it when setup is done.

## Host files (`scripts/setup`)

`scripts/setup` runs on the Linux host, as root, from any directory. It checks the
host, pins the image, and runs `dashboard setup` from that image with
`server/deploy` mounted. It needs only Python 3.11 and Docker on the host.

| Command | What it does |
| --- | --- |
| `scripts/setup check [--image IMAGE]` | checks the host: Linux, root, Docker with the Compose plugin, the deploy directory and the image pin. Stops on rootless Docker, `userns-remap` and SELinux enforcing, which break the UID 10001 and 65532 file owners the containers need |
| `scripts/setup host --dry-run` | prints what `host --yes` would write. The deploy directory is mounted read-only, and the container keeps only the read capability it needs to compare the secret files |
| `scripts/setup host --yes [--secret NAME=FILE]…` | writes `prod-image.env` when it is missing, then the env files and secret files listed below. Only this phase can write: it runs as root in the container with just `CHOWN`, `DAC_OVERRIDE` and `FOWNER`, and no network |
| `scripts/setup preview [--token-file FILE]` | the read-only [Cloudflare preview](#setup-preview-dashboard-setup---dry-run). It runs as cloudflared's UID 65532 with no capabilities, and sees only `APP_URL`, `BACKUP_SINK_URL` and the tunnel token file (read-only, for the tunnel ID); neither the deploy directory nor `secrets/` is mounted. The API token goes to it on standard input. Without a token file, setup asks for the token in a terminal |
| `scripts/setup apply [--yes] [--token-file FILE]` | the [Cloudflare apply](#setup-apply-scriptssetup-apply): prints the plan, then creates the missing Cloudflare pieces once you confirm. Runs as root with the `host --yes` capabilities and network, the deploy directory mounted read-write, and the token file read-only. Needs `host --yes` first |
| `scripts/setup discord [--dry-run \| --yes]`, `scripts/setup discord connect` | the [Discord setup](#discord-setup-scriptssetup-discord): checks the client ID, secret and bot token with Discord and stores them, then sets the first owner and the notification webhook from one Discord consent. Runs as root with network; the deploy directory is writable only with `--yes` and for `connect`. Needs `host --yes` first |
| `scripts/setup install-units [--enable-updates]` | installs the updater service, rendered for this checkout's path, and its timer. It does not turn on automatic updates unless you pass `--enable-updates` or answer yes |

`host` takes the values for the env files as flags: `--app-url`,
`--backup-hostname`, `--discord-client-id`, `--owner-discord-id`, `--owner-name`
and `--age-recipient`. It writes the existing keys `APP_URL`, `TUNNEL_TOKEN_FILE`,
`DISCORD_CLIENT_ID`, `BOOTSTRAP_OWNER_DISCORD_ID`, `BOOTSTRAP_OWNER_NAME`,
`BACKUP_SINK_URL`, `BACKUP_AGE_RECIPIENT` and the `*_FILE` paths. Each env file
is created from its `*.example` template when it is missing, with mode 0600; an
existing file keeps its mode and owner. A symlinked env file, secret file or
secret directory is reported and left alone.

Setup never chooses `COMPOSE_PROJECT_NAME` or `TUNNEL_NET`: the project name
selects the install's data volume. It writes them only when you pass
`--compose-project-name` and `--tunnel-net` for a new install (no `.env`, no
`prod-image.env`, nothing in Docker yet). On an existing install an unset key
means the Compose default, and a different value is reported as a conflict. If
Docker already uses the values in effect for something setup cannot prove is
this install (every container of the project started from this deploy
directory), setup stops before writing anything, lists what uses them, and asks
for both values.

Secrets are never flags. `--secret NAME=FILE` reads a file on the host, and
`--secret NAME=-` asks without echo. The values go to the container on standard
input.

| Name | Host file | Owner |
| --- | --- | --- |
| `discord-client-secret` | `secrets/app/discord-client-secret` | 10001 |
| `discord-webhook-url` (optional) | `secrets/app/discord-webhook-url` | 10001 |
| `discord-bot-token` (optional) | `secrets/app/discord-bot-token` | 10001 |
| `tunnel-token` | `secrets/tunnel-token` | 65532 |
| `backup-discord-webhook-url` (optional) | `secrets/backup/discord-webhook-url` | 10001 |
| `backup-bearer` | `secrets/backup/bearer` (generated: 32 random bytes, hex) | 10001 |
| `backup-access-client-id` | `secrets/backup/access-client-id` | 10001 |
| `backup-access-client-secret` | `secrets/backup/access-client-secret` | 10001 |

Secret files get mode 0400. `secrets/` gets 0711 and is owned by root.
`secrets/app` and `secrets/backup` get 0700 and are owned by 10001. An optional
secret also enables its `*_FILE` line in its env file (`dashboard.env`, or
`backup.env` for the backup copy of the webhook). A secret that no
container reads goes to `secrets/admin/` (root only), for you to pass on. The
token that bots and agents use is an example.

Setup never changes what you already have. A key with another value, or any
existing secret file, shows as `conflict` or `exists` and is left alone. Each
missing input is listed as `missing`, with the flag that supplies it. Run against
a complete install, `host` prints `nothing to do` and changes no file. Pass
`--rotate NAME` to replace one secret on purpose. It does not create the backup
age key: pass the public recipient from `age-keygen` on the recovery workstation
as `--age-recipient`.

## Discord setup (`scripts/setup discord`)

Two Discord steps stay manual, because Discord has no API for them: creating the
application, and registering its redirect `<APP_URL>/auth/callback` under
**OAuth2** → **Redirects**. `scripts/setup discord` prints that exact redirect.

| Command | What it does |
| --- | --- |
| `scripts/setup discord --dry-run` | checks the values with Discord and prints the plan. The deploy directory is mounted read-only |
| `scripts/setup discord [--yes]` | the same checks, then, with `--yes`, writes `DISCORD_CLIENT_ID`, `DISCORD_CLIENT_SECRET_FILE` and `DISCORD_BOT_TOKEN_FILE` (only with a bot token) to `dashboard.env`, and the two secret files. Without `--yes` it writes nothing |
| `scripts/setup discord connect [--dry-run]` | once the dashboard runs: prints one Discord link and reads back the setup code the dashboard shows. Then writes `BOOTSTRAP_OWNER_DISCORD_ID`, `BOOTSTRAP_OWNER_NAME` (when unset) and the webhook. `--dry-run` only says what it would ask for |

**Values.** The client ID, client secret and bot token come from
`--client-id-file`, `--client-secret-file` and `--bot-token-file`, which are files on
the host, mounted read-only into the container. A value without a file is typed at
the terminal, hidden. Values are never flags, and the launcher refuses any flag it
does not know. A value already in the host files counts as given. Run against a
complete install, `discord` checks the stored values again and changes no file.

**Checks, before anything is written.** The client ID and secret must pass a
client credentials grant at Discord's token endpoint. The bot token must open
`GET /applications/@me`, and that application must have the client ID. A refusal
stops the run with nothing written. With a bot token, setup also reads the app's
`redirect_uris` and says whether the redirect is registered. The bot invite link
asks for no permissions (`permissions=0`). The bot only opens direct messages,
which needs a shared server but no server permission.

**Adopt.** An existing client ID, secret file or bot token file with another value
is reported as `conflict` and left alone. To replace a secret on purpose, use
`scripts/setup host --yes --rotate discord-client-secret --secret discord-client-secret=FILE`.

**Connect.** The link asks for `identify` (your Discord user ID, for
`BOOTSTRAP_OWNER_DISCORD_ID`) and `webhook.incoming` (a webhook in a channel you
pick). It asks only for what is still missing, and does nothing when both are set.
Its state starts with `setup.`, and it uses PKCE. When Discord sends the browser
back to `/auth/callback` with such a state, the dashboard only shows the code: it
signs nobody in, sets and clears no cookie, writes nothing, and does not call
Discord. The page is `no-store`, `no-referrer`, allows no script, and cannot be
framed. The code is useless without the client secret and the PKCE verifier,
which only the terminal has. The page stays behind Cloudflare Access like the rest
of `/auth/`.

Paste the line the page shows, or the page's whole address. A code from another
link, a code already tried and a code Discord refuses are each turned down, and
setup asks again, up to three times. What one consent gives is written before setup
asks for more. If Discord leaves the webhook out of the answer to the combined
consent, setup prints a second link for `webhook.incoming` alone. Each approved
webhook link makes a new webhook in Discord. The webhook URL goes to
`secrets/app/discord-webhook-url` and `secrets/backup/discord-webhook-url`, with
`DISCORD_WEBHOOK_URL_FILE` in `dashboard.env` and `backup.env`. A
`DISCORD_WEBHOOK_URL_FILE` that already points at another file is left alone.

`connect` writes without `--yes`, since a code works only once, and it restarts
nothing. It prints the command that recreates the services whose files changed
(`docker compose … up -d --force-recreate dashboard backup`). At start, the
dashboard makes that Discord account the owner while it has no owner
(`BOOTSTRAP_OWNER_DISCORD_ID`; see
[ownership and recovery](roles-and-permissions.md#dashboard-ownership-and-recovery)).

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
