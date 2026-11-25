# Upgrade

> Current no-card image: use [Fresh install](fresh-install.md) and
> [Upgrade/restore](upgrade-existing.md). R2/Litestream commands below apply only
> to the previous image; Litestream is no longer shipped. Tunnel/Access steps remain useful.


Use this page when the dashboard already runs with Docker (`compose.cloudflare.yml`, set up with
the [deploy guide](README.md)). For a Pi running the dashboard under systemd, see
[Other setups → Upgrade under systemd](other-setups.md#upgrade-under-systemd).

**The order:** keep the old image → choose the new tag → download it while the old one keeps
serving → test the new version on a copy of your data → stop → back up → start → check.
Officers lose access only between stop and start, usually under a minute. Allow about 30
minutes in total, and tell officers about the short outage.

> [!WARNING]
> **You can't go back by changing the tag.** Each new version may change the database's
> structure on its first start (a "migration"), and older versions can't read the changed
> database. Going back means restoring the backup you take in step 5, which loses every change
> made after it. See [Rollback](#rollback).

> [!WARNING]
> **One dashboard at a time.** Never run the old and new versions side by side, and never start
> a copy elsewhere "just to test". Two copies writing to one database or one R2 backup corrupt
> it. Every test below runs on a separate copy.

Run every command in `~/officer-dashboard/server/deploy`.

## 1. Find out what is running

```sh
docker compose -f compose.cloudflare.yml exec dashboard /app/dashboard migrate
docker compose -f compose.cloudflare.yml images dashboard
```

The first command prints how many migrations the database has, for example
`/data/dashboard.db: 31 migrations applied`. Note the number. (31 is the release before the org
calendar; V0.15 has 37.) The second shows the image's name (**REPOSITORY**) and **TAG**.

**Upgrading from before V0.15?** Older installs may not set `LITESTREAM_PATH` and
`LITESTREAM_REGION` in `dashboard.env`. Add them if missing (`LITESTREAM_PATH=dashboard`,
`LITESTREAM_REGION=auto`): the tests below need them.

## 2. Keep the old image

Give the running image a second, local name, so a rollback can start exactly this version again:

```sh
docker tag <REPOSITORY>:<TAG> officer-dashboard:pre-upgrade
```

Use the REPOSITORY and TAG from step 1 (for example
`docker tag ghcr.io/cseed-org/officer-dashboard:0.15.0 officer-dashboard:pre-upgrade`). You should
see no output. `docker image ls officer-dashboard` now lists `pre-upgrade`.

## 3. Choose and download the new version

1. Pick the new tag on the
   [package page](https://github.com/cseed-org/officer-dashboard/pkgs/container/officer-dashboard),
   as in [step 1, item 7 of the deploy guide](README.md#1-prepare-the-server): a release like `0.16.0`,
   or a `sha-` tag. Never `latest`, a two-part tag like `0.16`, or `main`.
2. Update the deploy files to the same version:
   ```sh
   git fetch origin --tags
   git checkout v0.16.0        # or the 7 characters of the sha- tag
   ```
3. In `.env`, set the new tag, and delete any `DASHBOARD_IMAGE=` line (it would win over the tag):
   ```sh
   DASHBOARD_TAG=0.16.0
   ```
4. Check and download. The running dashboard is not touched:
   ```sh
   docker compose -f compose.cloudflare.yml config --images
   docker compose -f compose.cloudflare.yml pull dashboard
   ```
   You should see `ghcr.io/cseed-org/officer-dashboard:<new tag>`, then `Pulled`. `denied` means
   the GitHub login expired: repeat [step 1, item 6 of the deploy guide](README.md#1-prepare-the-server).

**Built the image yourself** (`DASHBOARD_IMAGE=officer-dashboard:local` in `.env`)? Keep that
line and build instead of pulling: see [Other setups → Build the image yourself](other-setups.md#build-the-image-yourself).

To see what changed between two releases: `git log --oneline v0.15.0..v0.16.0`.

## 4. Test the new version on a copy

This restores the R2 backup into a temporary file and runs the new version's migrations on it.
It starts no dashboard and does not touch the live data:

```sh
docker compose -f compose.cloudflare.yml run --rm --no-deps --entrypoint sh dashboard -c '
  litestream restore -config /etc/litestream.yml -o /tmp/dry.db /data/dashboard.db &&
  date && DB_PATH=/tmp/dry.db /app/dashboard migrate && date'
```

You should see `/tmp/dry.db: <new count> migrations applied` between two times.

- **A restore error:** your backup is not usable. Fix it (the `LITESTREAM_` lines, the R2 key,
  the bucket) before you go on.
- **A migration error:** the real start would fail the same way. Stop here and report it to the
  maintainers.
- **The time between the two times** is about how long the real start will take. If it is more
  than a minute, use the slow start in step 6.

## 5. Stop and back up

```sh
docker compose -f compose.cloudflare.yml stop dashboard
mkdir -p ~/dashboard-backups
b=~/dashboard-backups/pre-upgrade-$(date -u +%Y%m%d-%H%M%S).tgz && echo "$b"
docker run --rm -v officer-dashboard_dashboard-data:/data:ro -v ~/dashboard-backups:/backup \
  debian:bookworm-slim tar czf "/backup/$(basename "$b")" -C /data .
```

`stop` can take up to 45 seconds: the dashboard finishes its requests and Litestream sends its
last changes. Visitors see a Cloudflare error page until step 6. You should see the backup's file
name printed. Keep this terminal open: the rollback uses `$b`. (In a new terminal, find it with
`ls ~/dashboard-backups`.)

Check that the backup opens (install the tool once with `sudo apt install -y sqlite3`):

```sh
v=$(mktemp -d) && tar xzf "$b" -C "$v"
sqlite3 "$v/dashboard.db" 'PRAGMA integrity_check;'
sqlite3 "$v/dashboard.db" 'SELECT count(*) FROM schema_migrations;'
rm -rf "$v"
```

You should see `ok`, then the old count from step 1. Copy the backup to another computer too, for
example from your laptop: `scp <user>@<server>:dashboard-backups/pre-upgrade-*.tgz .`

## 6. Start the new version

```sh
docker compose -f compose.cloudflare.yml up -d
docker compose -f compose.cloudflare.yml logs -f dashboard
```

Wait for a line with `msg=listening` and the new version, then press Ctrl+C. Then
`docker compose -f compose.cloudflare.yml ps` should show `dashboard` as `(healthy)`.

**Slow start** (the test in step 4 took more than a minute): the health check could give up while
the database is changing. Start the dashboard on its own, wait, then the rest:

```sh
docker compose -f compose.cloudflare.yml up -d --no-deps dashboard
docker compose -f compose.cloudflare.yml ps        # repeat until (healthy)
docker compose -f compose.cloudflare.yml up -d
```

**If it keeps restarting** with `fatal … migration` in the log: stop it at once
(`docker compose -f compose.cloudflare.yml stop dashboard`), save the log, and do the
[Rollback](#rollback).

## 7. Check it

Do the checks in [step 8 of the deploy guide](README.md#8-check-that-it-works). The migration
count should now be the new one. Also sign in and open Events, Purchases and Home: your data
should be there, unchanged.

Upgrading to V0.15 for the first time? Then do the one-time [Google calendar setup](google-calendar.md).

## Rollback

Roll back only soon after an upgrade: **every change made since the backup in step 5 is lost.**
Not tried on a live system.

The R2 backup already holds the upgraded database, so the restored one starts a new backup folder
in the bucket. The old folder stays as a record.

1. Stop the dashboard and put the backup back:
   ```sh
   docker compose -f compose.cloudflare.yml stop dashboard
   docker run --rm -v officer-dashboard_dashboard-data:/data -v ~/dashboard-backups:/backup debian:bookworm-slim \
     sh -c "find /data -mindepth 1 -delete && tar xzf /backup/$(basename "$b") -C /data &&
            rm -rf /data/.dashboard.db-litestream && chown -R 10001:999 /data"
   ```
   (`10001:999` is the image's `dashboard` user and group. `.dashboard.db-litestream` is
   Litestream's own bookkeeping; removing it makes Litestream start fresh.)
2. In `dashboard.env`, set a new backup folder, and keep it from now on:
   ```sh
   LITESTREAM_PATH=dashboard-rollback-<today's date>
   ```
3. In `.env`, point at the image you kept in step 2:
   ```sh
   DASHBOARD_IMAGE=officer-dashboard:pre-upgrade
   ```
4. Check, then start:
   ```sh
   docker compose -f compose.cloudflare.yml config --images | grep pre-upgrade
   docker image inspect officer-dashboard:pre-upgrade --format '{{.Id}}'
   docker compose -f compose.cloudflare.yml up -d
   docker compose -f compose.cloudflare.yml exec dashboard /app/dashboard migrate
   ```
   You should see `officer-dashboard:pre-upgrade`, an image ID (not `No such image`), and then
   the old migration count from step 1.

**Rolled back from V0.15 after connecting the org Gmail?** Google keeps the four calendars, but
the restored database doesn't know them, so connecting again later would make four new ones.
Before you connect again, delete the four old calendars in the org Gmail's Google Calendar (each
calendar → **Settings and sharing** → **Delete**).

## Move to a new machine

Use this when the old server is gone (or will be switched off for good) and its R2 bucket holds
the backup. The new machine's first start restores the database from R2.

1. **Make sure the old dashboard is stopped for good.** Two dashboards on one bucket corrupt it.
2. On the new machine, follow the deploy guide's steps [1](README.md#1-prepare-the-server) and
   [4](README.md#4-fill-in-the-settings-files), with the **same** tunnel token, bucket and
   `LITESTREAM_` values as before (lost the R2 secret? Make a new key for the same bucket,
   [Cloudflare step 9](cloudflare.md#9-create-the-r2-bucket-for-backups)). Skip steps 2 and 3:
   Cloudflare and Discord are already set up.
3. Download the image: `docker compose -f compose.cloudflare.yml pull`.
4. Keep a copy of the backup as it is now:
   ```sh
   mkdir -p ~/dashboard-backups
   docker compose -f compose.cloudflare.yml run --rm --no-deps --user root -v ~/dashboard-backups:/backup \
     --entrypoint litestream dashboard restore -config /etc/litestream.yml -o /backup/replica-before-move.db /data/dashboard.db
   ```
   You should see a new file in `~/dashboard-backups`.
5. If the image is newer than the old server's, do the test in [step 4](#4-test-the-new-version-on-a-copy).
6. Continue with the deploy guide from [step 5](README.md#5-start-the-dashboard). The first start
   restores the database, then the checks in step 8 should show your data.

Not tried on a live system.

## For maintainers: publish a release

Images are built by GitHub Actions (`.github/workflows/ci.yml`, job `publish`) for 64-bit x86
and ARM, only after every other check passes (about 15 minutes):

| Event | Tags published |
| --- | --- |
| a push to `main` | `main` (moves) and `sha-<7 characters>` (fixed) |
| a tag `v0.15.0` | `0.15.0` (fixed), and moves `0.15` and `latest` |
| a pre-release tag such as `v0.15.1-rc.1` | only `0.15.1-rc.1` |

To release, once the commit is on `main` and its checks are green:

```sh
git tag v0.15.0 <commit> && git push origin v0.15.0
```

Never move or reuse a tag; a fix is a new patch tag (`v0.15.1`). There are no release notes yet:
the merged pull requests since the previous tag are the record.

A new GitHub package starts **private**. Making it public (organisation → **Packages** →
`officer-dashboard` → **Package settings** → **Change visibility**) removes the login step for
every server; that is the organisation owners' decision.

**Litestream 0.5 later:** the image ships Litestream 0.3.14. Moving to 0.5 needs its own plan:
restore and check the old backup, write a fresh one with the new version under a new
`LITESTREAM_PATH`, check that, then switch. Never point 0.5 at the existing 0.3 folder.
