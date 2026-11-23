# Fresh install

Use this runbook when nothing exists yet: no dashboard database, no Litestream bucket, no
tunnel. If some of it exists, start at the [index](README.md#which-path-am-i-on).

The repository supports two targets. Pick one and follow only its sections, start to finish.

| | **Docker** (`compose.cloudflare.yml`) | **Pi 5** (systemd) |
| --- | --- | --- |
| Runs | the dashboard image plus a `cloudflared` container | the `dashboard` binary under `deploy/dashboard.service` |
| Database | `/data/dashboard.db` in the `dashboard-data` volume | `/var/lib/officer-dashboard/dashboard.db` |
| Off-box copy | **Litestream** to R2, continuous (needs the bucket below) | nightly `scripts/backup` via `deploy/dashboard-backup.timer`; off-box only if you set `BACKUP_REMOTE` |
| Reached by | `cloudflared` container → `http://dashboard:8090` | `cloudflared` installed on the Pi → `http://127.0.0.1:8090` |
| Sections | 0, 1, 2, [3D](#3d-docker-env-files), [4D](#4d-docker-first-boot), 5, 6 | 0, 2, [3P](#3p-pi-build-install-env-file), [4P](#4p-pi-first-boot-and-nightly-backup), 5, 6 |

Litestream runs only inside the container (`server/deploy/entrypoint.sh`); the systemd unit does
not run it. Docker on a Pi is fine too: that is the Docker column.

**Build on the server itself.** On a Pi, both targets build natively for arm64. Building on
another machine and shipping the image or binary is not covered here (see
[Deployment](../deployment.md#cloudflare-containers--docker) for the multi-platform build,
which its own note says was not working when written). The first image build compiles
Litestream and runs its tests; on a Pi expect it to take a while (not measured here).

## 0. What you need

- A domain on Cloudflare, and a Zero Trust account (the free plan is fine).
- **Docker target:** Docker with Compose v2, Node ≥ 22.23.4 and npm (the image embeds
  `web/dist`, built on the host), and `git`.
- **Pi target:** Debian / Raspberry Pi OS with `sudo apt install git build-essential sqlite3`
  (gcc and make), Go 1.27.4 from [go.dev/dl](https://go.dev/dl/) (`linux-arm64`; the module
  pins `toolchain go1.27.4`, and Debian's packaged Go is older), and Node ≥ 22.23.4 (not in
  Debian's apt; use [nodejs.org](https://nodejs.org/en/download) or a version manager).
- A Discord application (Discord developer portal) with redirect URI
  `https://officers.example.org/auth/callback`. Note its client ID and secret.
- Your own Discord user ID (Discord → Settings → Advanced → **Developer Mode** on, then
  right-click yourself → **Copy User ID**). It becomes the dashboard owner.
- Optional now, needed for the org calendar: a Google OAuth client
  ([org calendar setup](org-calendar-setup.md), step 1).

Clone the repository on the server. If it is private, authenticate first (for example
`gh auth login`, or a read-only deploy key):

```sh
git clone https://github.com/cseed-org/officer-dashboard.git
cd officer-dashboard
git checkout main      # the release you deploy; note it with: git rev-parse --short HEAD
```

## 1. Create the R2 bucket for Litestream (Docker only)

**Not verified here:** Cloudflare dashboard steps; menu names change.

1. Cloudflare dashboard → **R2 Object Storage** → **Create bucket**. Name `cseed-dashboard-db`
   (any name; it becomes `LITESTREAM_BUCKET`). Location: Automatic. Public access off.
2. Note your **account ID** (R2 overview page). The endpoint is
   `https://<account-id>.r2.cloudflarestorage.com`.
3. Create a token for that one bucket: R2 overview → **Manage API tokens** (or **API Tokens**)
   → **Create API token**:
   - Permissions: **Object Read & Write**.
   - Buckets: **Apply to specific buckets only** → `cseed-dashboard-db`.
   - **Create**, then copy the **Access Key ID** and **Secret Access Key** into your password
     manager. The secret is shown once; if you lose it, create a new token the same way and
     delete the old one.

Do not use an account-wide token: anyone with this one can read or overwrite the database.

## 2. Cloudflare Tunnel and Access

Do these steps of [Deployment → Cloudflare Tunnel + Access](../deployment.md#cloudflare-tunnel--access),
with these differences:

| Step there | Do | Differences |
| --- | --- | --- |
| [1](../deployment.md#1-create-the-tunnel-and-start-the-containers), items 1 to 3 | Create tunnel `officer-dashboard`, add the published route | **Docker:** pick Docker, do not run Cloudflare's command, keep only the token; Service URL `http://dashboard:8090`. **Pi:** pick **Debian** and run Cloudflare's install command on the Pi (**not verified here**); Service URL `http://127.0.0.1:8090`. |
| 1, item 4 | **Skip.** | Sections 3 and 4 below replace it (same files, plus the owner and Litestream settings it leaves out). |
| [2](../deployment.md#2-one-time-pin-and-the-two-groups) to [8](../deployment.md#8-rocket-loader-and-email-obfuscation-off-for-the-consent-page) | All of them | Same for both targets. |
| [9](../deployment.md#9-check-it-from-outside) | Later | In section 6. |

**Before your first sign-in,** add your own email to the `Officers` group (step 2). Otherwise
Access never sends you a PIN.

## 3D. Docker: env files

Every Docker command in this runbook runs in `server/deploy`:

```sh
cd server/deploy
cp .env.example .env                    # compose settings
cp dashboard.env.example dashboard.env  # the app's settings
chmod 600 .env dashboard.env
```

In `.env` (read by compose, not by the app):

```sh
APP_URL=https://officers.example.org
TUNNEL_TOKEN=<the tunnel token from section 2>
DASHBOARD_IMAGE=officer-dashboard:v0.15   # a named tag, so a later upgrade can keep this one
```

In `dashboard.env`: the [settings below](#the-apps-settings), plus Litestream (uncomment and set
all six; set `PATH` and `REGION` even though the entrypoint has defaults, because maintenance
commands that bypass the entrypoint read them too):

```sh
LITESTREAM_BUCKET=cseed-dashboard-db
LITESTREAM_ENDPOINT=https://<account-id>.r2.cloudflarestorage.com
LITESTREAM_REGION=auto
LITESTREAM_PATH=dashboard
LITESTREAM_ACCESS_KEY_ID=<from section 1>
LITESTREAM_SECRET_ACCESS_KEY=<from section 1>
```

Compose sets `APP_URL`, `ADDR` and `TRUSTED_PROXIES` itself; values for them in `dashboard.env`
are ignored.

## 3P. Pi: build, install, env file

From the repository root:

```sh
(cd web && npm ci && npm run build)
(cd server && make build-release)        # embeds web/dist into server/bin/dashboard
cd server                                # every Pi command below runs here
./bin/dashboard version                  # the commit, from git describe
```

Install, following the header of `deploy/dashboard.service`:

```sh
sudo useradd --system --home /var/lib/officer-dashboard --shell /usr/sbin/nologin dashboard
sudo install -d -m 0755 /opt/officer-dashboard          # the unit header assumes it exists
sudo install -m 0755 bin/dashboard /opt/officer-dashboard/dashboard
sudo install -d -m 0750 -o root -g dashboard /etc/officer-dashboard
sudo install -m 0640 -g dashboard deploy/dashboard.env.example /etc/officer-dashboard/env
sudoedit /etc/officer-dashboard/env
```

In `/etc/officer-dashboard/env`: `APP_URL=https://officers.example.org` (the example holds
the production host; change it if yours differs) and the [settings below](#the-apps-settings).
Leave `LITESTREAM_*` commented out: the unit does not run Litestream. `TRUSTED_PROXIES` can
stay unset: `cloudflared` on the Pi connects from loopback, which is trusted by default.

### The app's settings

Same on both targets ([Configuration](../configuration.md) has every variable).

| Setting | Needed? |
| --- | --- |
| `DISCORD_CLIENT_ID`, `DISCORD_CLIENT_SECRET` | **Required**: sign-in. |
| `BOOTSTRAP_OWNER_DISCORD_ID`, `BOOTSTRAP_OWNER_NAME` | **Required for the first boot**: your Discord user ID (17 to 20 digits; anything else stops the server from starting) and name. |
| Receipts: `GOOGLE_SERVICE_ACCOUNT_JSON` + `DRIVE_ROOT_FOLDER_ID` | Needed for receipt uploads (503 without). The JSON variable takes the key file's **contents** on one line (`jq -c . key.json`), not a path. On a Pi, `RECEIPTS_DIR=/var/lib/officer-dashboard/receipts` works instead, and you back it up yourself; in Docker it would be lost with the container. |
| `GOOGLE_OAUTH_CLIENT_ID`, `GOOGLE_OAUTH_CLIENT_SECRET` | Needed for per-officer Google sync and the org calendar ([setup](org-calendar-setup.md)). Can be added later. |
| `DISCORD_WEBHOOK_URL`, `DISCORD_BOT_TOKEN` | Optional: notifications (logged without) and invite DMs (copy-link without). |
| `SMTP_*` | Optional: `.ics` invites for per-officer delivery and Launchpad invite emails. The org calendar does **not** need it: Google sends its invites. |
| `CLOUDFLARE_*`, `D1_DATABASE_ID` | Optional: the membership mirror and analytics widgets. They read an existing D1 database; nothing is created. |

## 4D. Docker: first boot

The server creates the database and applies every migration before it listens.

```sh
(cd ../../web && npm ci && npm run build)    # the image embeds web/dist
docker compose -f compose.cloudflare.yml build --build-arg VERSION=$(git describe --always --dirty) dashboard
docker compose -f compose.cloudflare.yml up -d
docker compose -f compose.cloudflare.yml ps                   # dashboard: (healthy)
docker compose -f compose.cloudflare.yml logs dashboard | grep listening
docker compose -f compose.cloudflare.yml logs cloudflared | grep "Registered tunnel connection"
```

Without `--build-arg VERSION=…` the image reports its version as `dev`. On the first boot the
bucket is empty, so the entrypoint's `litestream restore -if-db-not-exists -if-replica-exists`
does nothing; the server creates the file and Litestream starts replicating it.

**Do not run `dashboard seed` on a production database.** It loads fictional sample data
(users, events, FY2027 journal entries). It refuses a database with real users or journal
entries, but on a brand-new empty one it succeeds, and journal entries cannot be deleted.
`make dev` and `make seed` are development commands too.

## 4P. Pi: first boot and nightly backup

In `server/`:

```sh
sudo install -m 0644 deploy/dashboard.service /etc/systemd/system/
sudo systemctl daemon-reload && sudo systemctl enable --now dashboard
sudo journalctl -u dashboard -n 50 --no-pager     # look for: msg=listening … version=…
```

The server creates the database and applies every migration before it listens. The `seed`
warning in [4D](#4d-docker-first-boot) applies here too.

The nightly backup (`dashboard-backup.service` runs `/opt/officer-dashboard/backup`, which the
unit header does not install):

```sh
sudo install -m 0755 scripts/backup /opt/officer-dashboard/backup
sudo install -m 0644 deploy/dashboard-backup.service deploy/dashboard-backup.timer /etc/systemd/system/
sudo systemctl daemon-reload && sudo systemctl enable --now dashboard-backup.timer
sudo systemctl start dashboard-backup.service      # one backup now
sudo journalctl -u dashboard-backup -n 20 --no-pager   # backup written: /var/lib/officer-dashboard/backups/dashboard-….db.gz
```

**These backups stay on the Pi's own disk.** For an off-box copy, set `BACKUP_REMOTE` in
`/etc/officer-dashboard/env` to a command that receives the file (`{}` is its path), as in the
header of `scripts/backup`, for example with `rclone` and an R2 bucket of its own
(`BACKUP_REMOTE=rclone copyto {} r2:<bucket>/$(basename {})`). **Not verified here:** installing
and configuring `rclone` for R2; the backup unit runs as `dashboard` with
`ProtectHome=true`, so its rclone config must live under `/var/lib/officer-dashboard`.

## 5. Create the owner account

With `BOOTSTRAP_OWNER_DISCORD_ID` set, the first start made that Discord account active
**leadership with owner access** and recorded an `owner.bootstrap` audit entry. No log line
says so; you confirm it by signing in:

1. Open `https://officers.example.org`, pass the Access PIN (your email must be in `Officers`).
2. **Sign in with Discord** as that account. You land in the dashboard; **Officers → Access**
   shows you as owner.
3. Still in **Officers → Access**, set your **account email** (see below).
4. Add the other officers from **Officers → Access**. Add each person's email to the Access
   `Officers` group **before** sending the invite ([why](../deployment.md#2-one-time-pin-and-the-two-groups)).

**Which email is which.** Discord sign-in gives the dashboard no email. Three can be involved,
and using the same address for all three is simplest:

| Email | Where it is set | Used for |
| --- | --- | --- |
| Access PIN email | Cloudflare, `Officers` group | getting past Access |
| **Account email** | dashboard, **Officers → Access** (leadership) | org calendar sharing and invites: it must be a Google account to see the calendars |
| Personal/school email | the person's profile | contact details only |

If the owner ID was missing or wrong: fix it and restart; it applies while no owner exists. To
promote an account that already exists, see
[Roles and permissions](../roles-and-permissions.md#dashboard-ownership-and-recovery)
(`dashboard provision-leadership --owner --discord-id …`).

## 6. Smoke checks

| Check | Docker (in `server/deploy`) | Pi (in `server/`) |
| --- | --- | --- |
| Health | `docker compose -f compose.cloudflare.yml ps` → `(healthy)` | `curl -s http://127.0.0.1:8090/api/health` → `{"db":true,"ok":true}` |
| Migration count | `docker compose -f compose.cloudflare.yml exec dashboard /app/dashboard migrate` → `/data/dashboard.db: 37 migrations applied` | `sudo -u dashboard env DB_PATH=/var/lib/officer-dashboard/dashboard.db /opt/officer-dashboard/dashboard migrate` → `… 37 migrations applied` |
| Version | `docker compose -f compose.cloudflare.yml exec dashboard /app/dashboard version` → the commit | `/opt/officer-dashboard/dashboard version` |
| Replica / backup | `docker compose -f compose.cloudflare.yml exec dashboard litestream generations -config /etc/litestream.yml /data/dashboard.db` → one row, updated moments ago; R2 shows objects under `dashboard/generations/` | `sudo ls -l /var/lib/officer-dashboard/backups` → a `.db.gz` |
| From outside | [Deployment step 9](../deployment.md#9-check-it-from-outside): `mcp-check` ends `0 failed` (its `read -rs` line needs bash) | same, from any machine with Docker or the binary |
| Sign-in | PIN, then Discord, as the owner (section 5) | same |

`dashboard migrate` applies anything pending (nothing, on a running server) and prints the
count; it does not start a server. 37 is the count for the org-calendar release (0000 to 0036).

Then do the one-time [org calendar setup](org-calendar-setup.md).

## Never

- Run a second dashboard (another `docker compose up` elsewhere, a copy on a laptop) against the
  same database or R2 prefix. Litestream is single-writer: two writers corrupt the replica.
- `docker compose run` the `dashboard` service **without** `--entrypoint`: the default
  entrypoint starts a second server and a second Litestream writer.
- Copy `dashboard.db` alone while the server runs (you would miss the `-wal`). Use
  `scripts/backup` or Litestream.
- Rotate `GOOGLE_OAUTH_CLIENT_SECRET` casually: it seals the stored Google refresh tokens, so
  every Google connection would have to be made again ([Configuration](../configuration.md)).
