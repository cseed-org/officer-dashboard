# Deploying the dashboard: runbooks

Step-by-step runbooks for putting the server on a Raspberry Pi 5 or in Docker behind a
Cloudflare Tunnel, and for upgrading to the V0.15 org-calendar release. They link to
[Deployment](../deployment.md) and [Configuration](../configuration.md) for the full
reference and do not repeat them.

## Which path am I on?

Answer these on the machine that runs (or will run) the dashboard.

| Question | How to check |
| --- | --- |
| Is there a dashboard database? | Pi: `sudo ls -l /var/lib/officer-dashboard/dashboard.db`. Docker: `docker volume ls` shows `officer-dashboard_dashboard-data`. |
| Is there a Litestream bucket (the "Cloudflare DB")? | Cloudflare dashboard → **R2 Object Storage**: a bucket such as `cseed-dashboard-db` with objects under `dashboard/`. |
| Is there a tunnel? | Zero Trust → **Networking** → **Tunnels**: `officer-dashboard` shows **Healthy**. |

Then:

| What exists | Follow |
| --- | --- |
| Nothing: no database, no bucket, no tunnel | [Fresh install](fresh-install.md), then [org calendar setup](org-calendar-setup.md) |
| A running dashboard (database, and the bucket and tunnel if you use Docker) from an earlier release ([how to tell](upgrade-existing.md#before-you-start)) | [Upgrade an existing install](upgrade-existing.md), then [org calendar setup](org-calendar-setup.md) |
| A bucket with a replica but no running server (for example, a new machine) | [Fresh install](fresh-install.md) with the **existing** bucket (skip section 1; if you lost the R2 secret, create a new token for that bucket). The first boot restores the replica and migrates it, so first follow [A replica but no running server](upgrade-existing.md#a-replica-but-no-running-server) |
| The tunnel exists but there is no database or bucket | [Fresh install](fresh-install.md), skipping the tunnel steps you have already done |

## The runbooks

| Runbook | When |
| --- | --- |
| [Fresh install](fresh-install.md) | First deployment: R2 bucket and token, Tunnel and Access, env files, first boot, owner account, smoke checks. Pi (systemd) and Docker (`compose.cloudflare.yml`). |
| [Upgrade an existing install](upgrade-existing.md) | Upgrading to the org-calendar release: backup and a tested restore, new image or binary, migrations 0031 to 0036, smoke checks, rollback. |
| [Org calendar setup](org-calendar-setup.md) | Once, after either path: Google OAuth redirect and scope, connecting the org Gmail, sharing, service tokens and MCP, live checks, troubleshooting. |

## Conventions

- `officers.example.org` stands for your dashboard's hostname, and `APP_URL` for
  `https://officers.example.org`. Replace both everywhere.
- Commands run in a checkout of this repository on the server. Each section says its
  directory: Docker commands run in `server/deploy`, Pi commands in `server/`.
- Placeholders are in angle brackets: `<account-id>`. Replace the brackets too.
- **One writer.** Never run two dashboard servers against one database or one Litestream
  replica. Both runbooks say where this bites.
- Steps marked **Not verified here** need live credentials (Cloudflare, Google, R2) and were
  not run while writing these runbooks. The commands in them are checked against the
  repository; the menus and live results are not.
- Covered setups: `compose.cloudflare.yml`, and systemd on a Pi. Plain `docker run` and
  Cloudflare Containers are described in [Deployment](../deployment.md) but not here.
- The "Cloudflare DB" here is the R2 bucket Litestream replicates to. The optional
  `D1_DATABASE_ID` is a different thing: the existing membership database, which the dashboard
  only mirrors. These runbooks never create or change it.
