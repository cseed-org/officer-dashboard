# Fresh install

This guide installs the production dashboard on a 64-bit Linux server using
Docker, Cloudflare Tunnel and Access, and encrypted backups. For a local trial,
use [Development](../development.md). For a server with existing data, use
[Updates and recovery](upgrade-existing.md); do not initialize a replacement database.

## 1. Before you start

You need:

- A 64-bit Linux host (`x86_64` or `aarch64`), such as a VPS or Raspberry Pi.
- Git, Docker with Compose v2 and buildx, and Python 3.11+ on the host.
- Administrator access to the host and permission to use Docker.
- A domain managed through Cloudflare and access to its Zero Trust settings.
- A Discord application and the first owner's Discord user ID (step 3).
- GitHub package access to download the production image.
- A separate recovery workstation with Python 3 and the `age` encryption CLI.

Keep credentials in a password manager. Review your provider's current plan limits
and billing settings before creating resources. Allow the outbound traffic needed
by the tunnel and integrations; Cloudflare Tunnel uses TCP/UDP port 7844.

The supplied updater runs as root. For the default setup, enter a root shell with
`sudo -i`. Any directory works: replace `<install-dir>` in these commands with
your absolute installation path, quoting it if it contains spaces. Use the same OS
identity for registry login, Compose, and the updater.

```sh
git clone https://github.com/cseed-org/officer-dashboard.git <install-dir>
cd <install-dir>/server/deploy
```

All host Compose commands below run from this directory. Render the systemd
updater template with the same installation path before enabling it.

## 2. Set up Cloudflare

Follow [Cloudflare setup](cloudflare.md), steps 0–8. It covers the domain,
outgoing tunnel, browser sign-in, and access for agents and bots. The tunnel's
service URL is `http://dashboard:8090`. Save its token privately; step 5 puts it
in a separate file.

The dashboard hostname, for example `officers.example.org`, must match
`APP_URL=https://officers.example.org`. The backup service uses a separate
hostname, such as `backups.example.org`, configured in step 4.

## 3. Create the Discord app

Officers sign in using Discord. In the
[Discord Developer Portal](https://discord.com/developers/applications):

1. Create an application for the dashboard.
2. Under OAuth2, register `https://<your-hostname>/auth/callback` as a redirect.
   Replace `<your-hostname>` with the dashboard hostname; do not include angle
   brackets or add a trailing slash.
3. Save the Client ID and Client Secret. The secret goes in a host secret file,
   not in the environment file.
4. In your Discord account, enable **User Settings → Advanced → Developer Mode**.
   Copy your user ID from your profile's context menu. This 17–20 digit number
   becomes `BOOTSTRAP_OWNER_DISCORD_ID`.

A bot token for invitation DMs is optional and separate from OAuth.
See [Discord integration](../integrations.md#discord-sign-in-and-officer-invitations).

## 4. Prepare encrypted backups

A snapshot is a consistent copy of the SQLite database. The backup service
compresses and encrypts it before uploading it to a dedicated Cloudflare D1
database through a Worker (a small hosted API). The private decryption key stays
on the recovery workstation, never on the production server.

1. On the recovery workstation, run `age-keygen -o dashboard.agekey`.
   Save the private identity in offline recovery storage with a second offline copy.
   Copy only its printed `age1…` public recipient into `BACKUP_AGE_RECIPIENT`.
2. Create a D1 database named `dashboard-backups`. Check account capacity using
   the [backup capacity reference](../deployment.md#measured-size-and-free-tier-budget).
   Put its ID in `server/deploy/backup-sink/wrangler.toml`. The Worker must bind
   only this backup database, not the membership database.
3. Create a Cloudflare Access service token named `dashboard-backups-host`.
   Save its Client ID and Client Secret for the backup secret files in step 5.
4. Create an Access application for `backups.<your-domain>` covering all paths.
   Add a **Service Auth** policy allowing only that service token. Do not add a
   public Allow or Bypass policy.
5. On an administration workstation with Node and Cloudflare deployment access,
   enter `server/deploy/backup-sink` and run:
   ```sh
   npm ci
   npx wrangler d1 migrations apply BACKUPS --remote
   npx wrangler secret put BACKUP_BEARER_SECRET
   ```
   At the secret prompt, enter a separate random bearer secret with at least
   32 bytes. Save the same value for the host backup service. For example,
   `openssl rand -hex 32` produces a suitable value; keep its output private.
6. Add the custom domain before `[[d1_databases]]` in `wrangler.toml`, replacing
   the example domain, then deploy:
   ```toml
   routes = [{ pattern = "backups.example.org", custom_domain = true }]
   ```
   ```sh
   npm run deploy
   ```
   Keep `workers_dev` and `preview_urls` disabled.
7. Check that the hostname rejects requests without Access credentials. With
   Access credentials but no bearer it must return 401. With both, `/usage`
   and `/snapshots` must return JSON.

The backup Access token and bearer are both required. Keep them separate from
the tokens for dashboard bots.

Weekly exports of an existing membership D1 database are optional. If enabled,
use the `BACKUP_MEMBERSHIP_*` settings from [Configuration](../configuration.md#backups-encrypted-d1-snapshots).
The export API requires an account-scoped D1 Edit token, which can modify other
D1 databases in that account; keep it only in the backup service. Schedule exports
during a quiet period because they briefly lock the membership database.

## Sign in to the private image registry

The GHCR image is private. The GitHub account used for deployment needs package
Read access and a personal access token (classic) with `read:packages`.
Authorize it for organization SSO if required and record its expiry date.

Before logging in, configure a Docker
[credential store](https://docs.docker.com/reference/cli/docker/login/#credential-stores)
for the deployment OS user. The helper and its backing store must also work in
the unattended root updater service. Without a helper, Docker may save the token
unencrypted in its configuration.

```sh
docker login ghcr.io --username YOUR_GITHUB_USERNAME
```

Paste the token at the Password prompt. Expect `Login Succeeded`.
Use this same OS user for subsequent pulls. Never put the token in env files,
command arguments, or Git. For access details, see
[GitHub's registry authentication guide](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry#authenticating-with-a-personal-access-token-classic).

## 5. Prepare the deployment files

On the host, from `<install-dir>/server/deploy`:

```sh
umask 077
cp .env.example .env
cp dashboard.env.example dashboard.env
cp backup.env.example backup.env
install -d -m 0700 -o 10001 -g 10001 secrets/app secrets/backup
chmod 0600 .env dashboard.env backup.env
```

| File | What to set |
| --- | --- |
| `.env` | Public `APP_URL` and host `TUNNEL_TOKEN_FILE` path |
| `dashboard.env` | Discord Client ID, bootstrap owner ID/name, and app secret-file paths |
| `backup.env` | Backup URL, public age recipient, and backup secret-file paths |
| `prod-image.env` | Approved immutable image reference, resolved below |

Use a protected editor to create the following files. App and backup files need
owner UID 10001 and mode 0400; the tunnel token file needs UID 65532 and mode 0400.
The root-owned `secrets` parent must permit directory traversal by UID 65532.

| Host secret file | Contents |
| --- | --- |
| `secrets/app/discord-client-secret` | Discord OAuth Client Secret |
| `secrets/backup/bearer` | Same bearer secret as the backup Worker |
| `secrets/backup/access-client-id` | Backup Access service-token Client ID |
| `secrets/backup/access-client-secret` | Backup Access service-token Client Secret |
| `secrets/tunnel-token` | Cloudflare Tunnel token |

After creating the files, set their ownership and permissions:

```sh
chown 10001:10001 secrets/app/discord-client-secret secrets/backup/bearer secrets/backup/access-client-id secrets/backup/access-client-secret
chmod 0400 secrets/app/discord-client-secret secrets/backup/bearer secrets/backup/access-client-id secrets/backup/access-client-secret
chown 65532:65532 secrets/tunnel-token
chmod 0400 secrets/tunnel-token
chmod 0711 secrets
```

The templates use container paths such as
`DISCORD_CLIENT_SECRET_FILE=/run/secrets/app/discord-client-secret`.
Leave optional integrations commented out unless configured. Do not create
empty credential files. App, backup, and tunnel credentials have separate mounts;
see [secret-file requirements](../configuration.md#container-secret-files-and-isolation).

A maintainer must first approve a production image using the
[approval workflow below](#github-approval-gate-repository-settings-requires-approval).
Resolve that approved image:

```sh
docker buildx imagetools inspect ghcr.io/cseed-org/officer-dashboard:prod
```

Copy the displayed multi-platform manifest digest into `prod-image.env`:

```dotenv
DASHBOARD_IMAGE=ghcr.io/cseed-org/officer-dashboard@sha256:<approved digest>
```

Replace the entire `<approved digest>` placeholder with the 64-character digest
after `sha256:`. Protect the file with `chmod 0600 prod-image.env`.

## 6. Initialize and start

Pass both environment files on every production Compose invocation:

```sh
docker compose --env-file .env --env-file prod-image.env -f compose.cloudflare.yml pull
```

**Brand-new installation only:** initialize the empty data volume explicitly.
If a previous installation existed, restore its verified database using
[Updates and recovery](upgrade-existing.md) instead.

```sh
docker compose --env-file .env --env-file prod-image.env -f compose.cloudflare.yml \
  run --rm --no-deps --entrypoint /bin/sh dashboard -c 'umask 077; exec /app/dashboard migrate'
docker compose --env-file .env --env-file prod-image.env -f compose.cloudflare.yml up -d
docker compose --env-file .env --env-file prod-image.env -f compose.cloudflare.yml ps
```

Expect the dashboard to become healthy and cloudflared to connect. Production
startup refuses a missing database. The backup service can stage encrypted
recovery data, but decryption must happen off-host.

## 7. Sign in and connect services

Open `APP_URL`, pass Cloudflare Access, and sign in with the Discord account whose
ID matches `BOOTSTRAP_OWNER_DISCORD_ID`. That account becomes the initial owner
only while no owner exists. For problems, see
[ownership and recovery](../roles-and-permissions.md#dashboard-ownership-and-recovery).

Open `/setup` as the owner to see integration status and connect Google services.
Follow [Google Calendar setup](google-calendar.md) and
[Google Shared Drive setup](../deployment.md#file-storage-the-google-shared-drive-person-only-setup)
for invites, file uploads, and the independent Drive backup copy. Optional
notifications, invitation DMs, membership sync, and analytics are listed in
[Configuration](../configuration.md).

## 8. Verify the installation

- Check app health:
  `docker compose --env-file .env --env-file prod-image.env -f compose.cloudflare.yml exec dashboard /app/dashboard healthcheck`.
- Check the owner can sign in and load the main pages.
- Trigger a backup:
  `docker compose --env-file .env --env-file prod-image.env -f compose.cloudflare.yml run --rm --no-deps backup backup`.
- Complete the [restore drill](upgrade-existing.md#monthly-restore-drill) on the
  recovery workstation; verify representative records in an isolated instance.
- Run [Cloudflare's outside check](cloudflare.md#10-check-it-from-outside)
  if agents or bots will connect.
- Complete the [live capacity rehearsal](../deployment.md#live-free-plan-rehearsal)
  before accepting the backup workload and enabling unattended updates.

See [Troubleshooting](troubleshooting.md) if a check fails.

## GitHub approval gate (repository settings; requires approval)

Repository administrators configure this once; server installation alone does
not create it. In repository Settings → Environments, create `production` with
required maintainer reviewers and a branch policy allowing `main` only. Verify
these protections before promotion. With one maintainer, self-review must remain
possible; with another reviewer, enable prevention of self-review and disable
administrator bypass where available.

In Actions → **Approve production image**, run on `main` with the full commit SHA
whose main CI publication passed. A maintainer reviews that SHA and approves the
pending `production` deployment. The workflow checks ancestry, CI, and image
revision, then publishes its digest as `:prod`. Release version tags alone do
not approve a production image.

Once installation, backups, and recovery checks pass, install the updater units:

```sh
sed 's|@INSTALL_DIR@|<install-dir>|g' officer-dashboard-update.service | tee /etc/systemd/system/officer-dashboard-update.service > /dev/null
install -m 0644 officer-dashboard-update.timer /etc/systemd/system/
systemctl daemon-reload
systemctl start officer-dashboard-update.service
journalctl -u officer-dashboard-update.service -n 50 --no-pager
```

Confirm the service can read the registry credential store unattended and completes
successfully. Then enable polling:

```sh
systemctl enable --now officer-dashboard-update.timer
```

The timer checks for approved updates every five minutes. The updater backs up,
tests migrations, checks health, and rolls back on failure. See
[Updates and recovery](upgrade-existing.md) before doing manual maintenance.
