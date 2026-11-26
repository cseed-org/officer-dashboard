# Fresh install: Docker, D1 snapshots and approved updates

This uses the $0 Free plan. Cloudflare Zero Trust onboarding asks for payment details
even on that plan. Under D-13, the operator will add a card and Access stays configured:
the backup hostname requires both its Access service token and the strong bearer secret.
Under D-11, the operator completes setup after merge. R2 remains unused.
The older R2/Litestream sections in the original
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

Prepare [Cloudflare Tunnel and Access](cloudflare.md), steps 1–8 and 10, and
[create the Discord app](README.md#3-create-the-discord-app). Set the redirect URI
to `${APP_URL}/auth/callback` and obtain the first owner's Discord ID as described
in [owner bootstrap](../configuration.md#core). The Discord client secret belongs
in `secrets/app/discord-client-secret`, referenced by `DISCORD_CLIENT_SECRET_FILE`.

Use a Linux host with Docker Compose v2, buildx and Python 3.11+. The deployment user
can control Docker and must be trusted accordingly.

### Sign in to the private image registry

The GHCR image is **private**. A public source checkout does not grant image access.
Use the same OS identity for registry login, Compose and the image updater. The supplied
`officer-dashboard-update.service` runs as **root**: for that default setup, enter a root
shell with `sudo -i` before configuring the helper, logging in and running the host
Compose commands below. This uses root's Docker configuration and credential store.
If you customize the unit to use a different user, configure that user's `HOME`,
`DOCKER_CONFIG` and helper `PATH` consistently before logging in.

1. **Create the token.** A package administrator grants **Read** access to the GitHub
   account used for deployment. A direct grant on this private organization package
   requires that account to be an organization member (or receive access through an
   organization team); see [package access settings](https://docs.github.com/en/packages/learn-github-packages/configuring-a-packages-access-control-and-visibility#configuring-access-to-packages-for-an-organization).
   The operator of that GitHub account opens **Settings →
   Developer settings → Personal access tokens → Tokens (classic) → Generate new token
   (classic)**. Select **`read:packages` only**, choose an expiry and record its renewal
   date. Do not select `repo`, `write:packages` or `delete:packages`. Authorize the token
   for the organization if it requires SSO. GHCR requires a classic token for this host
   login; see [GitHub's registry authentication instructions](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry#authenticating-with-a-personal-access-token-classic).
2. **Prepare the credential helper before logging in.** Follow Docker's
   [credential-store setup](https://docs.docker.com/reference/cli/docker/login/#credential-stores)
   to install and initialize a helper for that OS user. For example, `pass` needs an
   initialized GPG/password store and `docker-credential-pass` on `PATH`; its per-registry
   configuration in `~/.docker/config.json` is:
   ```json
   {"credHelpers": {"ghcr.io": "pass"}}
   ```
   If already logged in to GHCR, run `docker logout ghcr.io` before switching helpers.
   Merge this entry into the existing JSON; preserve other settings. If Docker Desktop
   already uses its configured system credential store, keep that working configuration.
   Confirm the chosen helper and its backing store are ready before the next step:
   without a helper Docker may store the token unencrypted in `config.json`.
3. **Log in as the deployment OS user**, using the GitHub username that owns the token:
   ```sh
   docker login ghcr.io --username YOUR_GITHUB_USERNAME
   ```
   At the **Password** prompt paste the token, not the GitHub account password. Docker
   stores it through the configured helper. Expect `Login Succeeded`. Run later pulls
   as this same OS user so Docker can find the saved login. Never put the token in
   Compose/env files, command arguments, the image or Git. If a pull returns `denied`
   or `unauthorized`, check package access, token expiry and SSO authorization, then log
   in again with a replacement read-only token. Renew before the recorded expiry date.
   An expiry does not stop the running app;
   it prevents the next image download.

### Prepare the deployment files

Clone the repository if needed, then enter `server/deploy` from its root. All host
Compose commands below run in that directory; use your actual checkout location.
Copy `.env.example` to `.env`, `dashboard.env.example` to `dashboard.env`, and `backup.env.example` to `backup.env`. Keep only paths to secrets in these files. Create `secrets/app` and `secrets/backup` owned by uid 10001 with mode 0700. Put each required credential in its named file (mode 0400, uid 10001). Comment out unused optional `_FILE` entries rather than making empty files. The tunnel token is a separate mode-0400 file owned by uid 65532; set its host path as `TUNNEL_TOKEN_FILE` in `.env`. Protect the env files with mode 0600. The dashboard gets only the app directory, the backup service only the backup directory, and cloudflared only the tunnel token file. Backup settings never belong in `dashboard.env`. Resolve the
approved image with `docker buildx imagetools inspect ghcr.io/cseed-org/officer-dashboard:prod`.
Write its immutable reference to `prod-image.env`:

```dotenv
DASHBOARD_IMAGE=ghcr.io/cseed-org/officer-dashboard@sha256:<approved digest>
```

Run compose with both env files on **every** invocation:

```sh
docker compose --env-file .env --env-file prod-image.env -f compose.cloudflare.yml pull
```

For a brand-new installation only, initialize the volume by running the approved image's
`/app/dashboard migrate` as its entrypoint, with backups disabled just for this command:

```sh
docker compose --env-file .env --env-file prod-image.env -f compose.cloudflare.yml \
  run --rm --no-deps --entrypoint /bin/sh dashboard -c 'umask 077; exec /app/dashboard migrate'
docker compose --env-file .env --env-file prod-image.env -f compose.cloudflare.yml up -d
```

Once healthy, open `APP_URL`, complete Cloudflare Access and sign in with Discord as
the configured bootstrap owner. Connect the [org Google calendar](google-calendar.md)
and complete the [Google Shared Drive setup](../deployment.md#file-storage-the-google-shared-drive-person-only-setup)
before expecting Google invites, file uploads or the independent Drive backup copy.

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
`/etc/systemd/system`; adjust `/opt/officer-dashboard` if necessary. Run `systemctl daemon-reload`.
The updater uses root in the supplied unit, matching the registry login above. Ensure its
helper and backing store work unattended in the service environment, including `HOME`,
`DOCKER_CONFIG` and helper `PATH`; an interactive login alone does not prove timer access.
Run `systemctl start officer-dashboard-update.service`, inspect its journal for a successful
first update, and only then run `systemctl enable --now officer-dashboard-update.timer`.
Tunnel and Access remain remote-managed; neither is reprovisioned.

The app/backup containers run with all capabilities dropped, read-only root filesystems,
bounded tmpfs `/tmp` and memory/PID limits. Their shared `/data` is writable for SQLite
and private temporary snapshots. Mounted secrets are read-only. The app entrypoint sets
umask 0077 and removes any legacy backup environment keys before executing the app.
The backup helper reads file secrets without exporting their contents. Restart the relevant
container after changing a file. Docker-host administrators can still read mounted files;
these controls isolate the app from backup credentials, not the Docker administrator.

Before enabling timers, complete the [live Free-plan CPU and usage rehearsal](../deployment.md#live-free-plan-rehearsal).
The backup Worker bearer secret must contain at least 32 bytes (generate 32 random bytes
and encode them, for example with `openssl rand -hex 32`). It authenticates independently
of Access using a constant-time comparison; short secrets fail closed. Keep `workers_dev`
and `preview_urls` disabled and use only the custom domain. Access stays configured
under D-13; deployed requests must pass both the Access service token and bearer checks.
