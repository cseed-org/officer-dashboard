# Fresh install: Docker, D1 snapshots and approved updates

This is the current no-card path. The older R2/Litestream sections in the original
deployment guide describe the previous image and must not be used with this image.
Keep the existing remote-managed Tunnel and Access configuration from
[Cloudflare setup](cloudflare.md), steps 1–8 and 10. The backup sink is a separate hostname.

## Cloudflare and offline setup (requires operator approval)

1. On an offline recovery workstation install `age`, run `age-keygen -o dashboard.agekey`,
   and save the private identity in offline recovery storage. Copy only the printed
   `age1…` public recipient to `BACKUP_AGE_RECIPIENT`. Never paste the identity into
   GitHub, a dashboard secret, the host or a support log. Keep a second offline copy.
2. Cloudflare dashboard → Workers & Pages → D1 SQL Database → Create → name
   `dashboard-backups`. Confirm fewer than 10 account databases and total storage under
   4.5 GB excluding this backup DB. Copy its ID into
   `server/deploy/backup-sink/wrangler.toml`. This binding must name only this database.
3. Zero Trust → Access → Service auth → Service tokens → Create service token → name
   `dashboard-backups-host`, select an expiry and save both values in the host's protected
   `secrets/backup/access-client-id` and `secrets/backup/access-client-secret`; `backup.env` points to these files with `_FILE` keys.
4. Access → Applications → Add application → Self-hosted → hostname
   `backups.<your-domain>`, path all → add a **Service Auth** policy whose Include rule is
   **Service Token = dashboard-backups-host**. Do not add Bypass or public Allow policies.
   This is separate from the dashboard application and its MCP exceptions.
5. From `server/deploy/backup-sink`: `npm ci`,
   `npx wrangler d1 migrations apply BACKUPS --remote`. Generate a separate random bearer
   secret in a password manager; set it interactively with
   `npx wrangler secret put BACKUP_BEARER_SECRET`. Save the same value in `secrets/backup/bearer`; `BACKUP_BEARER_SECRET_FILE` points to it.
   Add a Worker custom-domain route for the protected hostname in `wrangler.toml`:
   `routes = [{ pattern = "backups.example.org", custom_domain = true }]` **before**
   `[[d1_databases]]`. Deploy with `npm run deploy` only after approval. Keep workers.dev
   and preview URLs disabled. In the Worker settings confirm only `BACKUPS` is bound.
6. Verify the hostname rejects requests without Access credentials, and with Access but
   without the bearer returns 401. With both, `/usage` and `/snapshots` return JSON.
7. For weekly membership exports: profile → API Tokens → Create custom token →
   **Account / D1 / Edit**, resources **Include / your account**, optionally restrict to
   the host egress IP and set an expiry. Save the value in `secrets/backup/membership-api-token`, referenced by `BACKUP_MEMBERSHIP_API_TOKEN_FILE`, with
   `BACKUP_MEMBERSHIP_ACCOUNT_ID` and the existing membership `BACKUP_MEMBERSHIP_DATABASE_ID`.
   Export's permission is account-scoped D1 Write/Edit: Cloudflare does not offer a
   single-database export permission. This token can modify other D1 databases in the
   account. It is never installed in the sink Worker; rotate/revoke it independently.
   Export briefly locks the membership DB; schedule its first run in a quiet period.
   See [D1 export API](https://developers.cloudflare.com/api/resources/d1/subresources/database/methods/export/)
   and [why export requires D1 Write](https://developers.cloudflare.com/d1/observability/debug-d1/#error-10000-and-token-permissions).

## Bootstrap the host

Use a Linux host with Docker Compose v2, buildx and Python 3.11+. Give its dedicated
deployment user a GHCR credential with **read:packages only**, through Docker's credential
helper. Do not put it in compose, the image or Git. Package visibility changes require
separate approval. The deployment user can control Docker and must be trusted accordingly.

Copy `.env.example` to `.env`, `dashboard.env.example` to `dashboard.env`, and `backup.env.example` to `backup.env` in `server/deploy`. Keep only paths to secrets in these files. Create `secrets/app` and `secrets/backup` owned by uid 10001 with mode 0700. Put each required credential in its named file (mode 0400, uid 10001). Comment out unused optional `_FILE` entries rather than making empty files. The tunnel token is a separate mode-0400 file owned by uid 65532; set its host path as `TUNNEL_TOKEN_FILE` in `.env`. Protect the env files with mode 0600. The dashboard gets only the app directory, the backup service only the backup directory, and cloudflared only the tunnel token file. Backup settings never belong in `dashboard.env`. Resolve the
approved image with `docker buildx imagetools inspect ghcr.io/cseed-org/officer-dashboard:prod`.
Write its immutable reference to `prod-image.env`:

```dotenv
DASHBOARD_IMAGE=ghcr.io/cseed-org/officer-dashboard@sha256:<approved digest>
```

Run compose with both env files on **every** invocation:

```sh
cd /opt/officer-dashboard/server/deploy
docker compose --env-file .env --env-file prod-image.env -f compose.cloudflare.yml pull
```

For a brand-new installation only, initialize the volume by running the approved image's
`/app/dashboard migrate` as its entrypoint, with backups disabled just for this command:

```sh
docker compose --env-file .env --env-file prod-image.env -f compose.cloudflare.yml \
  run --rm --no-deps --entrypoint /bin/sh dashboard -c 'umask 077; exec /app/dashboard migrate'
docker compose --env-file .env --env-file prod-image.env -f compose.cloudflare.yml up -d
```

If any previous installation existed, restore its DB instead. With backups configured,
the app refuses to start on a missing DB; the separate backup service downloads `<DB_PATH>.recovery.age`. **An unattended
plaintext restore cannot work while D5 keeps the private key off-box.** Recover off-box
with `scripts/restore-from-d1`, install the verified DB as uid 10001 in the stopped data
volume, then restart. The first-ever initialization above is deliberately explicit.

## GitHub approval gate (repository settings; requires approval)

Repository Settings → Environments → New environment → name exactly `production` →
Required reviewers → add the nominated human/review team → enable Prevent self-review →
disable administrator bypass where available → Deployment branches/tags → selected
branches → `main` only → Save. Verify these protections **before enabling promotion**;
an Environment without required reviewers provides no approval gate. Confirm the package
is private and the repository has write access to it. Do not change visibility implicitly.

Actions → Approve production image → Run workflow on `main` → enter the full commit SHA
whose main CI publish passed. The reviewer checks that SHA and approves the pending
`production` job. It verifies main ancestry, successful CI, and the image revision annotation,
then retags `sha-<first 7 characters>` by its digest as `:prod`.

Install `officer-dashboard-update.service` and `.timer` from `server/deploy` into
`/etc/systemd/system`; adjust `/opt/officer-dashboard` if necessary. Run `systemctl daemon-reload`
and `systemctl enable --now officer-dashboard-update.timer`. Authenticate GHCR as the service's
user (root in the supplied unit), using the read-only credential. Check the journal and run
the first update manually. Tunnel and Access remain remote-managed; neither is reprovisioned.

The app/backup containers run with all capabilities dropped, read-only root filesystems,
bounded tmpfs `/tmp` and memory/PID limits. Their shared `/data` is writable for SQLite
and private temporary snapshots. Mounted secrets are read-only. The app entrypoint sets
umask 0077 and removes any legacy backup environment keys before executing the app.
The backup helper reads file secrets without exporting their contents. Restart the relevant
container after changing a file. Docker-host administrators can still read mounted files;
these controls isolate the app from backup credentials, not the Docker administrator.
