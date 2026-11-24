# Other setups

The [deploy guide](README.md) covers the recommended setup: the published image in Docker, behind
a Cloudflare Tunnel. This page covers the alternatives, briefly. Each one assumes you know the
main guide.

- [Pi under systemd, without Docker](#pi-under-systemd-without-docker)
- [Build the image yourself](#build-the-image-yourself)
- [Practice run on a quick tunnel](#practice-run-on-a-quick-tunnel)
- [What's in the image](#whats-in-the-image)

## Pi under systemd, without Docker

The dashboard runs as a normal program (`dashboard serve`) under systemd, the service manager
built into Linux. You build it on the Pi. **There is no Litestream here**: backups are a nightly
file on the Pi's own disk unless you add an off-site copy yourself.

| | Docker (main guide) | systemd |
| --- | --- | --- |
| Gets the dashboard by | downloading the image | building it on the Pi |
| Database | `/data/dashboard.db` in the `dashboard-data` volume | `/var/lib/officer-dashboard/dashboard.db` |
| Backup | Litestream to R2, every second | `scripts/backup` nightly, on the Pi; off-site only with `BACKUP_REMOTE` |
| Settings file | `server/deploy/dashboard.env` | `/etc/officer-dashboard/env` |
| Tunnel | a `cloudflared` container → `http://dashboard:8090` | `cloudflared` installed on the Pi → `http://127.0.0.1:8090` |

### Install

1. **Tools:** `sudo apt install git build-essential sqlite3`, Go 1.27.4 from
   [go.dev/dl](https://go.dev/dl/) (`linux-arm64`; Debian's Go is too old), and Node 22.23.4 or
   newer from [nodejs.org](https://nodejs.org/en/download).
2. **Cloudflare:** do [Cloudflare setup](cloudflare.md) with two differences in step 1: pick
   **Debian** and **run** Cloudflare's install command on the Pi, and set the Service URL to
   `http://127.0.0.1:8090`. Skip step 9 (R2) unless you want it for `BACKUP_REMOTE`.
3. **Discord:** [deploy guide step 3](README.md#3-create-the-discord-app).
4. **Build**, from the repository root, after `git checkout v0.15.0` (or the commit):
   ```sh
   (cd web && npm ci && npm run build)
   (cd server && make build-release)
   cd server
   ./bin/dashboard version
   ```
   You should see the version or commit. Every command below runs in `server/`.
5. **Install** (this follows the header of `deploy/dashboard.service`):
   ```sh
   sudo useradd --system --home /var/lib/officer-dashboard --shell /usr/sbin/nologin dashboard
   sudo install -d -m 0755 /opt/officer-dashboard
   sudo install -m 0755 bin/dashboard /opt/officer-dashboard/dashboard
   sudo install -d -m 0750 -o root -g dashboard /etc/officer-dashboard
   sudo install -m 0640 -g dashboard deploy/dashboard.env.example /etc/officer-dashboard/env
   sudoedit /etc/officer-dashboard/env
   ```
   In the env file set `APP_URL=https://<your-hostname>` and the same Discord and owner lines as
   [deploy guide step 4](README.md#4-fill-in-the-settings-files). Leave the `LITESTREAM_` lines
   commented out. `TRUSTED_PROXIES` can stay unset: `cloudflared` on the Pi connects from the Pi
   itself, which is trusted by default.
6. **Start:**
   ```sh
   sudo install -m 0644 deploy/dashboard.service /etc/systemd/system/
   sudo systemctl daemon-reload && sudo systemctl enable --now dashboard
   sudo journalctl -u dashboard -n 50 --no-pager
   ```
   You should see `msg=listening`. Then do deploy guide steps 6 to 8. The checks differ:
   `curl -s http://127.0.0.1:8090/api/health` should print `{"db":true,"ok":true}`, and the
   migration count is
   `sudo -u dashboard env DB_PATH=/var/lib/officer-dashboard/dashboard.db /opt/officer-dashboard/dashboard migrate`.
   The outside check: `./bin/dashboard mcp-check https://<your-hostname>`, with the same two
   variables as [Cloudflare step 10](cloudflare.md#10-check-it-from-outside).
7. **Nightly backup:**
   ```sh
   sudo install -m 0755 scripts/backup /opt/officer-dashboard/backup
   sudo install -m 0644 deploy/dashboard-backup.service deploy/dashboard-backup.timer /etc/systemd/system/
   sudo systemctl daemon-reload && sudo systemctl enable --now dashboard-backup.timer
   sudo systemctl start dashboard-backup.service
   sudo journalctl -u dashboard-backup -n 20 --no-pager
   ```
   You should see `backup written: /var/lib/officer-dashboard/backups/dashboard-….db.gz`. It keeps
   the newest 30 (`KEEP`). For an off-site copy, set `BACKUP_REMOTE` in the env file to a command
   that receives the file (`{}` is its path), as described in the header of `scripts/backup`, for
   example `BACKUP_REMOTE=rclone copyto {} r2:<bucket>/$(basename {})`. Not verified here: the
   backup runs as `dashboard` with `ProtectHome=true`, so an `rclone` config must live under
   `/var/lib/officer-dashboard`.

Receipts: Google Drive, or `RECEIPTS_DIR=/var/lib/officer-dashboard/receipts`, which you must back
up yourself (it is not in the database).

**Restore a nightly backup:** stop the service, unzip the chosen `.db.gz` over
`/var/lib/officer-dashboard/dashboard.db`, delete the matching `-wal` and `-shm` files, fix the
owner, start. The commands are in [Rollback under systemd](#rollback-under-systemd).

### Upgrade under systemd

The same order as [Upgrade](upgrade.md): build while the old one serves, test on a copy, stop,
back up, install, start. In `server/`:

1. **Build:**
   ```sh
   git fetch origin --tags && git checkout v0.16.0
   (cd ../web && npm ci && npm run build)
   make build-release
   ./bin/dashboard version
   ```
2. **Test on a copy of last night's backup:**
   ```sh
   sudo systemctl start dashboard-backup.service
   f=$(sudo sh -c 'ls -1t /var/lib/officer-dashboard/backups/dashboard-*.db.gz | head -1') && echo "$f"
   v=$(mktemp -d) && sudo gunzip -c "$f" > "$v/dry.db"
   sqlite3 "$v/dry.db" 'PRAGMA integrity_check;'
   date; DB_PATH="$v/dry.db" ./bin/dashboard migrate; date
   rm -rf "$v"
   ```
   You should see `ok`, then the new migration count. A migration error here would happen for
   real too: stop and report it.
3. **Stop, back up, keep the old files, install, start:**
   ```sh
   sudo systemctl stop dashboard
   sudo systemctl start dashboard-backup.service
   f=$(sudo sh -c 'ls -1t /var/lib/officer-dashboard/backups/dashboard-*.db.gz | head -1') && echo "$f"
   sudo mkdir -p /opt/officer-dashboard/pre-upgrade
   sudo cp -p /opt/officer-dashboard/dashboard /opt/officer-dashboard/backup /etc/systemd/system/dashboard.service \
     /etc/systemd/system/dashboard-backup.service /etc/systemd/system/dashboard-backup.timer /opt/officer-dashboard/pre-upgrade/
   sudo install -m 0755 bin/dashboard /opt/officer-dashboard/dashboard
   sudo install -m 0755 scripts/backup /opt/officer-dashboard/backup
   sudo install -m 0644 deploy/dashboard.service deploy/dashboard-backup.service deploy/dashboard-backup.timer /etc/systemd/system/
   sudo systemctl daemon-reload && sudo systemctl start dashboard
   sudo journalctl -u dashboard -n 50 --no-pager
   ```
   You should see `msg=listening` and no `fatal`. `$f` is your rollback point: copy it off the Pi
   (`scp`). If you had edited the installed unit files, `diff` them against `deploy/` before the
   `install` lines.

### Rollback under systemd

Every change since the backup `$f` is lost. (In a new shell, find it with
`sudo ls -lt /var/lib/officer-dashboard/backups`.)

```sh
sudo systemctl stop dashboard
sudo sh -c "gunzip -c $f > /var/lib/officer-dashboard/dashboard.db"
sudo rm -f /var/lib/officer-dashboard/dashboard.db-wal /var/lib/officer-dashboard/dashboard.db-shm
sudo chown dashboard:dashboard /var/lib/officer-dashboard/dashboard.db
p=/opt/officer-dashboard/pre-upgrade
sudo install -m 0755 $p/dashboard $p/backup /opt/officer-dashboard/
sudo install -m 0644 $p/dashboard.service $p/dashboard-backup.service $p/dashboard-backup.timer /etc/systemd/system/
sudo systemctl daemon-reload && sudo systemctl start dashboard
```

## Build the image yourself

Use this to run a version that has no published image, or if you can't use GitHub's registry.
Nothing needs building first: the image build also builds the web front end. It takes about 3
minutes on a fast x86 machine; longer on a Pi (not measured).

**With compose**, in `server/deploy`:

```sh
echo DASHBOARD_IMAGE=officer-dashboard:local >> .env
docker compose -f compose.cloudflare.yml -f compose.build.yml build --build-arg VERSION=$(git describe --always --dirty) dashboard
docker compose -f compose.cloudflare.yml up -d
```

`officer-dashboard:local` is a local name: compose never downloads it and it can't be mistaken for
a release. To upgrade, check out the new version, build again the same way, and follow
[Upgrade](upgrade.md) from step 4. Without `--build-arg VERSION`, `dashboard version` prints `dev`.

**Without compose**, from the repository root:

```sh
docker build -f server/Dockerfile --build-arg VERSION=$(git describe --always --dirty) -t officer-dashboard:local .
docker buildx build --platform linux/amd64,linux/arm64 -f server/Dockerfile -t <registry>/officer-dashboard:<tag> --push .
```

The first builds for this machine. The second builds both platforms and pushes them to a
registry, because Docker can't load a two-platform image locally (or use
`--output type=oci,dest=image.tar`). Neither needs emulation.

## Practice run on a quick tunnel

[`compose.quicktunnel.yml`](../../server/deploy/compose.quicktunnel.yml) runs the same two
containers with a temporary `*.trycloudflare.com` address. It needs no Cloudflare account. The
dashboard is the development build with sample data and a developer sign-in locked by
`DEV_AUTH_KEY`. Its header has the commands; `down -v` removes everything when you are done.

It proves the containers, the tunnel, `APP_URL`, sign-in and MCP through Cloudflare. It has **no
Access and none of the domain settings**, so it can't test Cloudflare steps 2 to 8. There,
`mcp-check` shows two warnings, which are expected: `GET /oauth/authorize` and
`POST /api/ingest/luma` came through without Access. Never put real data in it: the address is
public.

## What's in the image

- **Platforms:** 64-bit x86 (`linux/amd64`) and 64-bit ARM (`linux/arm64`, a Pi 5).
- **Contents:** the `dashboard` server with the web front end built in, and Litestream 0.3.14,
  on `debian:bookworm-slim`. It runs as user `dashboard` (uid 10001, gid 999) and keeps its data
  in `/data`.
- **Listens** on port 8090 inside the container (`ADDR=:8090`, `DB_PATH=/data/dashboard.db`).
  Because it listens beyond its own machine, it refuses to start without `APP_URL`.
- **Starts** through `server/deploy/entrypoint.sh`: with `LITESTREAM_BUCKET` set, it restores the
  database from the bucket if there is no local file, then runs the dashboard under
  `litestream replicate`. Without it, the database lives only in the container's volume.
- **Health check:** `dashboard healthcheck`, every 30 seconds.
- **Behind another proxy** (Caddy, or `cloudflared` you run yourself): set `APP_URL` to the public
  `https` address and `TRUSTED_PROXIES` to the proxy's address or network, so rate limits see each
  visitor instead of the proxy. `compose.cloudflare.yml` does this for you.
- **Security build details:** Litestream is built from source with the dashboard's Go version and
  patched dependency versions; the Security workflow scans the shipped binary. See the comments in
  `server/Dockerfile`.
