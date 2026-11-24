# Upgrade an existing install to the org-calendar release

Use this runbook when the dashboard already runs with `compose.cloudflare.yml` (database in
the `dashboard-data` volume, Litestream to R2, the tunnel) or under systemd on a Pi. It
upgrades to the org-calendar release (V0.15: migrations 0031 to 0036). Afterwards, do the
one-time [org calendar setup](org-calendar-setup.md). Installs run another way (plain
`docker run`, Cloudflare Containers) are not covered; adapt the Docker steps.

**The order:** get the new version (Docker: pull the image; Pi: build the binary) while the
old one serves; prove your backup restores and dry-run the migrations on that copy; stop; take
the final backup; start the new version; smoke-check. Downtime is the stop-to-healthy window, usually under a minute.

> [!WARNING]
> **One writer, always.** Never run two dashboard servers against one database or one
> Litestream replica: not the old and new container side by side, not a copy on another
> machine "just to test", not `docker compose run dashboard` without `--entrypoint` (that
> starts a second server and a second Litestream writer). Two writers corrupt the replica.
> Every command below that reads a copy restores it to a scratch path first.

## What you deploy

The org-calendar release is on `main` (server #97, Calendar page #98, Programs tab and
tentative dates #99). Its git tag is `v0.15.0` once a maintainer pushes it; CI then publishes
the image tag `0.15.0` ([the tags](../deployment.md#the-published-image)).

- **Docker:** set the fixed image tag `0.15.0`. If the
  [package page](https://github.com/cseed-org/officer-dashboard/pkgs/container/officer-dashboard)
  does not list it yet, use `sha-<commit>` for a `main` commit at or after #99 whose CI passed
  (the commit's first 7 characters, `git rev-parse --short=7 <commit>`). Never a moving tag
  (`latest`, `0.15`, `main`). `dashboard version` then reports `v0.15.0`, or that commit.
- **Pi:** build that tag or commit. Note `git rev-parse --short HEAD`: that is what
  `dashboard version` reports.

The counts in this runbook (31 before, 37 after) are for this release. For a later upgrade, the
steps are the same with other tags; the dry run (step 3) prints the new count, and
`git log --oneline v0.15.0..<new tag>` lists what changed.

## Before you start

**Which schema is running?** The version string is no help (older Docker builds report `dev`),
so count the migrations:

| | Command |
| --- | --- |
| Docker (in `server/deploy`) | `docker compose -f compose.cloudflare.yml exec dashboard /app/dashboard migrate` |
| Pi | `sudo -u dashboard env DB_PATH=/var/lib/officer-dashboard/dashboard.db /opt/officer-dashboard/dashboard migrate` |

- `31 migrations applied` (0000 to 0030): before the org calendar. This runbook applies all six.
- `37`: already on this release. Only the binary or image changes; the steps still apply.
- Anything else: an intermediate build ran. Nothing extra to do: the new server applies
  whatever is missing, in order.

**Docker:** make sure `dashboard.env` sets `LITESTREAM_PATH` and `LITESTREAM_REGION`
explicitly (older installs relied on the entrypoint's defaults, `dashboard` and `auto`). Add
them with those values if missing: the checks below call Litestream without the entrypoint.

Have ready: about 30 minutes (plus the build time on a Pi), and somewhere off this machine to
copy the backup (your laptop: `scp`). Tell officers about the short outage.

## What the migrations do

`dashboard serve` applies them automatically on start, before it listens: there is no separate
step. Each file runs in its own transaction, then a foreign-key check, then commit. **They are
forward-only**: no down migrations, and the old binary is not supported on a migrated database.
**Rollback means restoring the backup** you take below, losing any writes made after it.
For Docker that means: choosing the older image tag is never enough on its own. Start the older
image only on the restored backup ([Rollback](#rollback-docker)).

**Only 0033 is slow or risky.** It rebuilds the `events` table (copies every row, drops the old
table, renames, recreates five indexes) so that an Event can be tentative, without a start
time. Its time grows with the number of Events; the dry run measures it on your data. Rows that
point at Events (guests, contacts, design requests, assignments, outbox) are kept: foreign keys
are off during the rebuild, so the drop does not cascade, and the check fails the migration if
anything dangles. It also copies every non-cancelled Event with a start into the Events
calendar (drafts as `[Draft] …`, on purpose) and queues each for Google. Nothing goes to Google
until the [org account is connected](org-calendar-setup.md#2-connect-the-org-gmail); then the
queue is pushed once, without emailing anyone (`sendUpdates=none`).

| File | Change |
| --- | --- |
| `0031_org_calendar.sql` | New tables: the four org calendars, org Google account, meetings, invitees, outbox, sync and ACL state; triggers on `users` that bump an ACL generation. |
| `0032_programs.sql` | Weekly programs: tracks (`buildspace`, `buildHER`), programs, exceptions. |
| `0033_event_tentative.sql` | The `events` rebuild above, plus the Events-calendar backfill. |
| `0034_org_calendar_recurrence.sql` | Recurrence and time zone columns on meetings; Google instance overrides. |
| `0035_event_calendar_history.sql` | Last fixed time of each Event (backfilled, kept by triggers), to withdraw invites when an Event turns tentative. |
| `0036_org_calendar_attendee_status.sql` | Last Google RSVPs per meeting, sent back on push. |

**If a migration fails** the server logs `fatal … migration <file> statement <n>: …` (or
`foreign_key_check failed`) and exits. That migration is rolled back; earlier ones stay
applied. Compose (`restart: unless-stopped`) and systemd (`Restart=on-failure`) then restart it
in a loop. **Stop it at once** (`docker compose -f compose.cloudflare.yml stop dashboard`, or
`sudo systemctl stop dashboard`), keep the log, and roll back. The dry run below exists so that
this does not happen in production.

## Docker

Every command runs in `server/deploy`. In short: keep the old image, set the new tag, pull, dry
run, stop, back up, start.

### 1. Keep the old image

Give the running image a local name, so the rollback can start exactly it again. Do this
whatever the image is (built here, or a published tag: a moving tag such as `latest` would by
then point at the new release):

```sh
cd server/deploy
docker compose -f compose.cloudflare.yml images dashboard     # REPOSITORY and TAG of the running image
docker tag officer-dashboard:latest officer-dashboard:pre-v0.15   # use the REPOSITORY:TAG shown
```

### 2. Pull the new version (the old one keeps serving)

First the checkout, so the compose files match the release:

```sh
git fetch origin --tags && git checkout v0.15.0   # or the commit of your sha- tag
```

Then, in `server/deploy/.env`, delete any `DASHBOARD_IMAGE=` line (it names a locally built
image and wins over the tag) and set the new tag:

```sh
DASHBOARD_TAG=0.15.0          # or sha-<commit>: see "What you deploy"
```

```sh
docker compose -f compose.cloudflare.yml config --images      # ghcr.io/cseed-org/officer-dashboard:0.15.0 and cloudflared
docker compose -f compose.cloudflare.yml pull dashboard
```

If the package is private, `pull` needs a GHCR login on this machine first: `denied` means
there is none (or it expired). Do the `docker login ghcr.io` in
[fresh install, 4D](fresh-install.md#4d-docker-first-boot), then pull again. The steps below
need the pulled image: without it, `run` and `up` stop with an error. `pull` does not touch
the running container.

**An image you build yourself** (`DASHBOARD_IMAGE=officer-dashboard:local`, see
[Building the image yourself](../deployment.md#building-the-image-yourself)): keep that line,
and instead of `pull`, build the new checkout under the same name:
`docker compose -f compose.cloudflare.yml -f compose.build.yml build --build-arg VERSION=$(git describe --always --dirty) dashboard`.

### 3. Prove the replica restores, and dry-run the migrations on it

Litestream 0.3.14 has no "snapshot now" command: it snapshots every 6 h and ships WAL every
second (`deploy/litestream.yml`). Restoring to a scratch path is both a point-in-time copy and
the proof that the replica works. This runs the **new** image with its entrypoint replaced, so
it starts no server and no replication, and never touches `/data`:

```sh
docker compose -f compose.cloudflare.yml run --rm --no-deps --entrypoint sh dashboard -c '
  litestream restore -config /etc/litestream.yml -o /tmp/dry.db /data/dashboard.db &&
  date && DB_PATH=/tmp/dry.db /app/dashboard migrate && date'
```

Expect `/tmp/dry.db: 37 migrations applied` between two timestamps.

- **A restore error** means the replica is not usable: fix it (credentials, `LITESTREAM_*`,
  bucket) before upgrading.
- **A migration error** is the error the real start would hit. Stop here and report it.
- **The time between the timestamps** is roughly how long the real start migrates. Up to about
  a minute needs nothing special. If it is longer, start the dashboard alone in step 5 (shown
  there): the healthcheck can mark it unhealthy while it migrates, and `cloudflared` waits for
  it to be healthy.

**Not verified here:** needs your live R2 credentials.

### 4. Stop, and take the final backup

```sh
docker compose -f compose.cloudflare.yml stop dashboard   # up to 45 s: the server drains, Litestream flushes
mkdir -p ~/dashboard-backups
b=~/dashboard-backups/pre-v0.15-$(date -u +%Y%m%d-%H%M%S).tgz && echo "$b"
docker run --rm -v officer-dashboard_dashboard-data:/data:ro -v ~/dashboard-backups:/backup \
  debian:bookworm-slim tar czf "/backup/$(basename "$b")" -C /data .
```

`officer-dashboard_dashboard-data` is the volume compose created (`docker volume ls`). The server
is stopped, so the files are consistent. `cloudflared` keeps running; visitors see a tunnel
error page until step 5.

Check that the backup opens (needs `sqlite3` on the host; on Debian `sudo apt install sqlite3`),
then copy it off this machine:

```sh
v=$(mktemp -d) && tar xzf "$b" -C "$v"
sqlite3 "$v/dashboard.db" 'PRAGMA integrity_check;'                    # ok
sqlite3 "$v/dashboard.db" 'SELECT count(*) FROM schema_migrations;'    # 31 before the upgrade
rm -rf "$v"
```

Keep `$b` in this shell: the rollback names it. If you open a new shell, `ls ~/dashboard-backups`.

### 5. Start the new version

```sh
docker compose -f compose.cloudflare.yml up -d
docker compose -f compose.cloudflare.yml logs -f dashboard   # until "msg=listening … version=v0.15.0" (or the commit); Ctrl-C
docker compose -f compose.cloudflare.yml ps                  # dashboard: (healthy)
```

If the dry run took more than about a minute, start the dashboard alone first, wait for
`(healthy)`, then the rest:

```sh
docker compose -f compose.cloudflare.yml up -d --no-deps dashboard
docker compose -f compose.cloudflare.yml ps        # repeat until (healthy)
docker compose -f compose.cloudflare.yml up -d
```

Then the [smoke checks](#smoke-checks).

### Rollback (Docker)

> [!WARNING]
> **Restore first, then the older tag.** Migrations are forward-only: the new image has already
> migrated the database (and its replica), and the old image does not support that schema.
> Pointing `.env` back at the old tag without restoring the backup leaves the old server on a
> database it does not understand. The steps below restore, then start the old image.

**Not verified here** (needs a live replica). Roll back only soon after the upgrade: every write
since the backup is lost. If the org account was already connected, see
[After a rollback](#after-a-rollback) too.

The R2 prefix now holds the migrated database, so the restored one replicates to a **new**
prefix; the old prefix stays as a record.

```sh
docker compose -f compose.cloudflare.yml stop dashboard
docker run --rm -v officer-dashboard_dashboard-data:/data -v ~/dashboard-backups:/backup debian:bookworm-slim \
  sh -c "find /data -mindepth 1 -delete && tar xzf /backup/$(basename "$b") -C /data &&
         rm -rf /data/.dashboard.db-litestream && chown -R 10001:10001 /data"
```

(`10001` is the image's `dashboard` user. `.dashboard.db-litestream` is Litestream's local
state; removing it makes Litestream start a fresh generation.) Then set
`LITESTREAM_PATH=dashboard-rollback-<today>` in `dashboard.env` (keep it from then on: later
upgrades and checks use this prefix instead of `dashboard/`), and the old image from step 1 in
`.env`: `DASHBOARD_IMAGE=officer-dashboard:pre-v0.15`. The compose files can stay as they are.
Check that compose names that image and that it exists, then start it:

```sh
docker compose -f compose.cloudflare.yml config --images | grep pre-v0.15   # officer-dashboard:pre-v0.15
docker image inspect officer-dashboard:pre-v0.15 --format '{{.Id}}'          # an id, not "No such image"
docker compose -f compose.cloudflare.yml up -d
docker compose -f compose.cloudflare.yml exec dashboard /app/dashboard migrate   # 31 migrations applied
```

## Pi

Every command runs in `server/` of the checkout.

### 1. Build the new binary (the old one keeps serving)

```sh
git fetch origin --tags && git checkout v0.15.0   # or the commit (see "What you deploy")
(cd ../web && npm ci && npm run build)
make build-release                            # server/bin/dashboard
./bin/dashboard version                       # the commit you are deploying
```

### 2. Prove a backup restores, and dry-run the migrations on it

If the backup timer is not installed yet, do [fresh install, 4P](fresh-install.md#4p-pi-first-boot-and-nightly-backup)
(the backup part) first. Then:

```sh
sudo systemctl start dashboard-backup.service
f=$(sudo sh -c 'ls -1t /var/lib/officer-dashboard/backups/dashboard-*.db.gz | head -1') && echo "$f"
v=$(mktemp -d) && sudo gunzip -c "$f" > "$v/dry.db"
sqlite3 "$v/dry.db" 'PRAGMA integrity_check;'               # ok
date; DB_PATH="$v/dry.db" ./bin/dashboard migrate; date      # the NEW binary: 37 migrations applied
rm -rf "$v"
```

A migration error here is the error the real start would hit: stop and report it. The time
between the `date` lines is roughly how long the real start migrates (systemd does not time out
a starting `Type=simple` service).

### 3. Stop, final backup, install, start

```sh
sudo systemctl stop dashboard
sudo systemctl start dashboard-backup.service && sudo journalctl -u dashboard-backup -n 3 --no-pager
f=$(sudo sh -c 'ls -1t /var/lib/officer-dashboard/backups/dashboard-*.db.gz | head -1') && echo "$f"   # the rollback point
sudo mkdir -p /opt/officer-dashboard/pre-v0.15
sudo cp -p /opt/officer-dashboard/dashboard /opt/officer-dashboard/backup /etc/systemd/system/dashboard.service \
  /etc/systemd/system/dashboard-backup.service /etc/systemd/system/dashboard-backup.timer /opt/officer-dashboard/pre-v0.15/
sudo install -m 0755 bin/dashboard /opt/officer-dashboard/dashboard
sudo install -m 0755 scripts/backup /opt/officer-dashboard/backup
sudo install -m 0644 deploy/dashboard.service deploy/dashboard-backup.service deploy/dashboard-backup.timer /etc/systemd/system/
sudo systemctl daemon-reload && sudo systemctl start dashboard
sudo journalctl -u dashboard -n 50 --no-pager      # "msg=listening … version=<commit>", no "fatal"
```

Copy `$f` off the Pi too (`scp`). If you had edited the installed unit files yourself, `diff`
them against `deploy/` before the `install` line.

Then the [smoke checks](#smoke-checks).

### Rollback (Pi)

Roll back only soon after the upgrade: every write since the backup is lost. `$f` is the
rollback point from step 3 (in a new shell: `sudo ls -lt /var/lib/officer-dashboard/backups`).

```sh
sudo systemctl stop dashboard
sudo sh -c "gunzip -c $f > /var/lib/officer-dashboard/dashboard.db"
sudo rm -f /var/lib/officer-dashboard/dashboard.db-wal /var/lib/officer-dashboard/dashboard.db-shm
sudo chown dashboard:dashboard /var/lib/officer-dashboard/dashboard.db
p=/opt/officer-dashboard/pre-v0.15
sudo install -m 0755 $p/dashboard $p/backup /opt/officer-dashboard/
sudo install -m 0644 $p/dashboard.service $p/dashboard-backup.service $p/dashboard-backup.timer /etc/systemd/system/
sudo systemctl daemon-reload && sudo systemctl start dashboard
```

## After a rollback

If the org account was connected before you rolled back, Google keeps the four calendars, the
pushed Events and any invites already sent; the old version ignores them. The restored database
does not know those calendars, so upgrading again and connecting would create four new ones.
Before you connect again, delete the four old calendars in the org Gmail (Google Calendar →
each calendar → **Settings and sharing** → **Delete**).

## Smoke checks

| Check | How | Expect |
| --- | --- | --- |
| Healthy | Docker: `docker compose -f compose.cloudflare.yml ps`. Pi: `curl -s http://127.0.0.1:8090/api/health` | `(healthy)`; `{"db":true,"ok":true}` |
| Migrated | `dashboard migrate`, as in [Before you start](#before-you-start) | `37 migrations applied` |
| Version | `dashboard version` (Docker: `docker compose -f compose.cloudflare.yml exec dashboard /app/dashboard version`) | Docker: `v0.15.0`, or the commit of the `sha-` tag. Pi: the commit you noted |
| Errors | Docker: `docker compose -f compose.cloudflare.yml logs dashboard \| grep -v level=INFO`. Pi: `sudo journalctl -u dashboard -n 100 --no-pager \| grep -v level=INFO` | Only "not configured" warnings for integrations you do not use |
| Replica (Docker) | `docker compose -f compose.cloudflare.yml exec dashboard litestream generations -config /etc/litestream.yml /data/dashboard.db` | a row whose end time is a moment ago |
| From outside | [Deployment step 9](../deployment.md#9-check-it-from-outside), `mcp-check` | `0 failed` |
| Data | Sign in; open Events, Purchases, Home | your data, unchanged |
| Org calendar API | Signed in, open `https://officers.example.org/api/orgcal/calendars` | four `calendars`, `"googleConnected":false` until the [setup](org-calendar-setup.md) |
| Events projected | Signed in, open `https://officers.example.org/api/orgcal/events?from=2026-09-01&to=2027-06-30&cal=events` (any range up to 366 days) | your Events in that range |

Then do the one-time [org calendar setup](org-calendar-setup.md).

## A replica but no running server

For a new machine with an existing bucket (the [index](README.md#which-path-am-i-on) sends you
here): the new image's first boot restores the replica and migrates it at once. Before that,
keep a copy of the replica as it is now. On the new machine, after
[fresh install 3D](fresh-install.md#3d-docker-env-files) (same bucket and `LITESTREAM_*`; if you
lost the R2 secret, create a new token for the bucket) and the image pull
(`docker compose -f compose.cloudflare.yml pull`, as in [4D](fresh-install.md#4d-docker-first-boot)):

```sh
mkdir -p ~/dashboard-backups
docker compose -f compose.cloudflare.yml run --rm --no-deps --user root -v ~/dashboard-backups:/backup \
  --entrypoint litestream dashboard restore -config /etc/litestream.yml -o /backup/replica-before-upgrade.db /data/dashboard.db
sqlite3 ~/dashboard-backups/replica-before-upgrade.db 'SELECT count(*) FROM schema_migrations;'
```

Then run [step 3](#3-prove-the-replica-restores-and-dry-run-the-migrations-on-it) (the dry run)
and continue with [fresh install 4D](fresh-install.md#4d-docker-first-boot). Make sure the old
server is gone for good first: one writer. **Not verified here.**
