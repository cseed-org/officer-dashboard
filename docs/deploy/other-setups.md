# Other setups



The [fresh-install guide](fresh-install.md) covers the recommended setup: the published image in Docker, behind
a Cloudflare Tunnel. This page covers the alternatives, briefly. Each one assumes you know the
main guide.

- [Pi under systemd, without Docker](#pi-under-systemd-without-docker)
- [Build the image yourself](#build-the-image-yourself)
- [What's in the image](#whats-in-the-image)

## Pi under systemd, without Docker

The dashboard runs as a normal program (`dashboard serve`) under systemd, the service manager
built into Linux. You build it on the Pi. Backups are encrypted nightly files on the host
unless you configure an off-site copy.

| | Docker (main guide) | systemd |
| --- | --- | --- |
| Gets the dashboard by | downloading the image | building it on the Pi |
| Database | `/data/dashboard.db` in the `dashboard-data` volume | `/var/lib/officer-dashboard/dashboard.db` |
| Backup | age-encrypted D1 snapshots, hourly | age-encrypted `scripts/backup` nightly; off-site with an executable `BACKUP_REMOTE` hook |
| Settings file | `server/deploy/dashboard.env` | `/etc/officer-dashboard/env` |
| Tunnel | a `cloudflared` container → `http://dashboard:8090` | `cloudflared` installed on the Pi → `http://127.0.0.1:8090` |

### Install

Replace `<install-dir>` with your absolute binary installation directory. Render
the unit templates with that path using the `sed` commands below.
For these manual rendering commands, choose a path without `&`, `|`, `\`, `%` or `"`; spaces are supported.

1. **Tools:** `sudo apt install git build-essential sqlite3`, Go matching
   [server/go.mod](../../server/go.mod) from
   [go.dev/dl](https://go.dev/dl/) (`linux-arm64`; Debian's Go is too old), and Node 24 LTS from [nodejs.org](https://nodejs.org/en/download).
2. **Cloudflare:** do [Cloudflare setup](cloudflare.md) with two differences in step 1: pick
   **Debian** and **run** Cloudflare's install command on the Pi, and set the Service URL to
   `http://127.0.0.1:8090`. Configure a separate off-site destination for `BACKUP_REMOTE`.
3. **Discord:** [Discord setup](fresh-install.md#3-create-the-discord-app).
4. **Build**, from the repository root, after checking out your reviewed release tag or commit:
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
   sudo install -d -m 0755 <install-dir>
   sudo install -m 0755 bin/dashboard <install-dir>/dashboard
   sudo install -d -m 0750 -o root -g dashboard /etc/officer-dashboard
   sudo install -m 0640 -g dashboard deploy/dashboard.env.example /etc/officer-dashboard/env
   sudoedit /etc/officer-dashboard/env
   ```
   Create a host secret file, then enter the Discord client secret with `sudoedit`:
   ```sh
   sudo install -d -m 0750 -o root -g dashboard /etc/officer-dashboard/secrets
   sudo install -m 0640 -o root -g dashboard /dev/null /etc/officer-dashboard/secrets/discord-client-secret
   sudoedit /etc/officer-dashboard/secrets/discord-client-secret
   ```
   In `/etc/officer-dashboard/env`, set `APP_URL=https://<your-hostname>`,
   `DISCORD_CLIENT_ID`, `BOOTSTRAP_OWNER_DISCORD_ID` and optionally `BOOTSTRAP_OWNER_NAME`.
   Replace the active container secret path with
   `DISCORD_CLIENT_SECRET_FILE=/etc/officer-dashboard/secrets/discord-client-secret`;
   do not also set `DISCORD_CLIENT_SECRET`. For each optional `_FILE` setting you enable,
   create a host secret file with the same ownership/mode and use its host path.
   `TRUSTED_PROXIES` can stay unset: `cloudflared` on the Pi connects from the Pi
   itself, which is trusted by default.
6. **Start:**
   ```sh
   sed 's|@INSTALL_DIR@|<install-dir>|g' deploy/dashboard.service | sudo tee /etc/systemd/system/dashboard.service > /dev/null
   sudo systemctl daemon-reload && sudo systemctl enable --now dashboard
   sudo journalctl -u dashboard -n 50 --no-pager
   ```
   You should see `msg=listening`. Then sign in as owner, connect services, and verify the installation
   using [Fresh install](fresh-install.md#7-sign-in-and-connect-services). The checks differ:
   `curl -s http://127.0.0.1:8090/api/health` should print `{"db":true,"ok":true}`, and the
   migration count is
   `sudo -u dashboard env DB_PATH=/var/lib/officer-dashboard/dashboard.db <install-dir>/dashboard migrate`.
   The outside check: `./bin/dashboard mcp-check https://<your-hostname>`, with the same two
   variables as [Cloudflare step 10](cloudflare.md#10-check-it-from-outside).
7. **Nightly encrypted backup:** install the age CLI, then set the public `BACKUP_AGE_RECIPIENT` and an absolute `BACKUP_DIR` in the service env file. Keep its private identity off-host.
   ```sh
   sudo install -m 0755 scripts/backup <install-dir>/backup
   sed 's|@INSTALL_DIR@|<install-dir>|g' deploy/dashboard-backup.service | sudo tee /etc/systemd/system/dashboard-backup.service > /dev/null
   sudo install -m 0644 deploy/dashboard-backup.timer /etc/systemd/system/
   sudo systemctl daemon-reload && sudo systemctl enable --now dashboard-backup.timer
   sudo systemctl start dashboard-backup.service
   sudo journalctl -u dashboard-backup -n 20 --no-pager
   ```
   You should see `Encrypted backup written`. It keeps the newest 30 (`KEEP`)
   `.db.gz.age` files. `BACKUP_REMOTE`, when set, is an executable hook receiving only
   the ciphertext filename as its single argument. Keep remote-copy configuration
   under `/var/lib/officer-dashboard` because the service has `ProtectHome=true`.

Receipts: Google Drive, or `RECEIPTS_DIR=/var/lib/officer-dashboard/receipts`, which you must back
up yourself (it is not in the database).

**Restore a nightly backup:** on a recovery workstation, decrypt with the off-box age
identity, gunzip and require SQLite integrity `ok`. Then stop the service and install
that verified database, with owner `dashboard` and mode 0600. Remove its stale WAL/SHM
while stopped and restart. Never copy the age private identity onto the service host.

### Upgrade under systemd

Use this order: build while the old one serves, test on a copy, stop,
back up, install, start. In `server/`:

1. **Build:**
   ```sh
   git fetch origin --tags
   git checkout <reviewed-release-tag-or-commit>
   (cd ../web && npm ci && npm run build)
   make build-release
   ./bin/dashboard version
   ```
2. **Test on an off-box restored copy:** trigger a fresh encrypted backup and transfer
   the `.db.gz.age` file to the recovery workstation. Decrypt there with the private
   identity, check integrity, and run candidate migrations on a disposable copy.
   Keep the encrypted rollback point and its off-box identity available.
3. **Stop, back up, keep the old files, install, start:**
   ```sh
   sudo systemctl stop dashboard
   sudo systemctl start dashboard-backup.service
   f=$(sudo sh -c 'ls -1t /var/lib/officer-dashboard/backups/dashboard-*.db.gz.age | head -1') && echo "$f"
   sudo mkdir -p <install-dir>/pre-upgrade
   sudo cp -p <install-dir>/dashboard <install-dir>/backup /etc/systemd/system/dashboard.service \
     /etc/systemd/system/dashboard-backup.service /etc/systemd/system/dashboard-backup.timer <install-dir>/pre-upgrade/
   sudo install -m 0755 bin/dashboard <install-dir>/dashboard
   sudo install -m 0755 scripts/backup <install-dir>/backup
   for unit in dashboard.service dashboard-backup.service; do
     sed 's|@INSTALL_DIR@|<install-dir>|g' "deploy/$unit" | sudo tee "/etc/systemd/system/$unit" > /dev/null
   done
   sudo install -m 0644 deploy/dashboard-backup.timer /etc/systemd/system/
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
# Decrypt/verify $f on the recovery workstation first, then transfer the verified DB.
sudo install -m 0600 -o dashboard -g dashboard /path/to/offbox-verified.db /var/lib/officer-dashboard/dashboard.db
sudo rm -f /var/lib/officer-dashboard/dashboard.db-wal /var/lib/officer-dashboard/dashboard.db-shm
sudo chown dashboard:dashboard /var/lib/officer-dashboard/dashboard.db
p=<install-dir>/pre-upgrade
sudo install -m 0755 $p/dashboard $p/backup <install-dir>/
sudo install -m 0644 $p/dashboard.service $p/dashboard-backup.service $p/dashboard-backup.timer /etc/systemd/system/
sudo systemctl daemon-reload && sudo systemctl start dashboard
```

## Build the image yourself

Production requires a registry digest even when you build the image yourself. Configure
`.env`, `dashboard.env`, `backup.env` and the separate secret directories using
[Fresh install](fresh-install.md) first. From the repository root, choose a registry
and tag you can push, then publish and resolve that exact build:

```sh
IMAGE=registry.example.org/your-org/officer-dashboard:reviewed-build
docker buildx build --platform linux/amd64,linux/arm64 -f server/Dockerfile \
  --build-arg VERSION="$(git describe --always --dirty)" -t "$IMAGE" --push .
DIGEST=$(docker buildx imagetools inspect "$IMAGE" --format '{{json .Manifest}}' | \
  python3 -c 'import json,sys; print(json.load(sys.stdin)["digest"])')
printf 'DASHBOARD_IMAGE=%s@%s\n' "$IMAGE" "$DIGEST" > server/deploy/prod-image.env
cd server/deploy
docker compose --env-file .env --env-file prod-image.env -f compose.cloudflare.yml pull
```

Use the explicit database initialization in [Fresh install](fresh-install.md) before
`up` on a new host. On an existing host, take the verified backup and follow
[Upgrade an existing host](upgrade-existing.md) before changing its pin. The updater
follows the repository in `prod-image.env` and expects a `:prod` tag there. Keep the
timer disabled unless your pipeline moves `:prod` only after approval.
Both platforms build without emulation.

After initialization or the upgrade procedure, start the stack:

```sh
docker compose --env-file .env --env-file prod-image.env -f compose.cloudflare.yml up -d
```

For a local build without publishing, use the development stack from the repository root:

```sh
docker compose -f compose.dev.yaml up -d --build
```

This stack is for development with sample data and developer sign-in. The optional
`compose.build.yml` override is build-only and cannot turn a local tag into a production pin.

## What's in the image

- **Platforms:** 64-bit x86 (`linux/amd64`) and 64-bit ARM (`linux/arm64`, a Pi 5).
- **Contents:** the static `dashboard` server with the web front end built in, and the
  `dashboard-backup` helper, on BusyBox musl. It runs as user `dashboard` (uid 10001)
  and keeps its data in `/data`. The runtime contains no Python interpreter.
- **Listens** on port 8090 inside the container (`ADDR=:8090`, `DB_PATH=/data/dashboard.db`).
  Because it listens beyond its own machine, it refuses to start without `APP_URL`.
- **Starts** through `server/deploy/entrypoint.sh`. With `BACKUP_REQUIRED=1`, a missing
  database stops startup; initialize a new installation or restore off-box first.
  The separate backup service stages encrypted recovery snapshots, never plaintext restores.
- **Health check:** `dashboard healthcheck`, every 30 seconds.
- **Behind another proxy** (Caddy, or `cloudflared` you run yourself): set `APP_URL` to the public
  `https` address and `TRUSTED_PROXIES` to the proxy's address or network, so rate limits see each
  visitor instead of the proxy. `compose.cloudflare.yml` does this for you.
- **Security build details:** both Go binaries are statically linked with SQLite extension
  loading disabled. Base images are digest-pinned. See the comments in
  `server/Dockerfile`.
