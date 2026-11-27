# Troubleshooting

Run host commands from `server/deploy` using the same OS user used for registry
login. Every production Compose command needs both environment files:

```sh
docker compose --env-file .env --env-file prod-image.env -f compose.cloudflare.yml ps
docker compose --env-file .env --env-file prod-image.env -f compose.cloudflare.yml logs --tail 50 dashboard backup cloudflared
```

Keep credentials out of shared logs. Start with [Fresh install](fresh-install.md)
for setup or [Updates and recovery](upgrade-existing.md) for a failed update.

## Image download or configuration fails

| You see | What to check |
| --- | --- |
| `denied` or `unauthorized` when pulling | Package Read access, token expiry, SSO authorization, and the OS user's Docker credential store. Repeat [registry login](fresh-install.md#sign-in-to-the-private-image-registry). |
| `manifest unknown` or `not found` | The approved digest in `prod-image.env`. A source checkout or release tag alone does not approve production deployment. |
| `no matching manifest for linux/arm/v7` | Use a 64-bit OS. `uname -m` should report `aarch64` or `x86_64`. |
| Missing `DASHBOARD_IMAGE`, `APP_URL`, or `TUNNEL_TOKEN_FILE` | Check [deployment files](fresh-install.md#5-prepare-the-deployment-files) and pass both env files. |
| Docker socket permission denied | Use the intended deployment OS user and verify its Docker access. |
| `Pool overlaps` | Set `TUNNEL_NET` in `.env` to an unused private network prefix. |

## The dashboard does not start

| You see | What to do |
| --- | --- |
| `Database missing` | For a brand-new installation, run the explicit [initialization](fresh-install.md#6-initialize-and-start). For existing data, restore a verified database off-host; do not initialize an empty replacement. |
| Invalid public URL or forwarded host mismatch | Use the same public HTTPS hostname for `APP_URL` and the tunnel route. |
| Invalid bootstrap Discord ID | Copy the owner's 17–20 digit user ID again; see [Discord setup](fresh-install.md#3-create-the-discord-app). |
| Missing, empty, or unreadable secret file | Check the path, ownership, mode, and mount. App/backup files use UID 10001; the tunnel token uses UID 65532. Do not set both a direct secret and its `_FILE` variant. |
| Migration or health failure during an update | Keep logs, the previous pin, and checkpoint. Follow [interrupted update recovery](upgrade-existing.md#interrupted-update-recovery). |
| `update-recovery-required.json` blocks updates | Complete recovery before removing the marker. `--retry` does not override it. |

## The site does not open

| You see | What to check |
| --- | --- |
| Tunnel disconnected | Inspect `cloudflared` logs, the tunnel-token file, and outbound network access. The tunnel service waits for dashboard health. |
| Tunnel connected but origin unavailable | The dashboard must be healthy and the tunnel service URL must be `http://dashboard:8090`. |
| Hostname cannot be resolved | The domain must be active in Cloudflare and the tunnel's published hostname must exist. |

See [Cloudflare setup](cloudflare.md) for the tunnel and Access configuration.

## Signing in fails

| You see | What to check |
| --- | --- |
| No Cloudflare PIN email | Add the email to the correct [Access group](cloudflare.md#adding-someone-later) and check spam. |
| Discord rejects the redirect URI | Register exactly `https://<your-hostname>/auth/callback`, without a trailing slash. |
| Discord sign-in is not configured | Set `DISCORD_CLIENT_ID` and a readable `DISCORD_CLIENT_SECRET_FILE`, then recreate the dashboard service. |
| Signed in without owner access | Check [ownership and recovery](../roles-and-permissions.md#dashboard-ownership-and-recovery). Bootstrap settings do not replace an existing owner. |
| An invitation stops at Access | Add that person's email to the Access group before they open the invitation. |

## Agent or bot connections fail

Run [the outside check](cloudflare.md#10-check-it-from-outside). Each failure
names a Cloudflare setup step.

| Check fails on… | Where to look |
| --- | --- |
| Access sign-in on an agent path | [Exact agent bypass paths](cloudflare.md#4-let-ai-agents-through-on-their-own-paths) |
| A bot challenge or WAF block | [Bot checks](cloudflare.md#6-turn-off-bot-checks-for-agents) |
| Cached responses | [Cache rule](cloudflare.md#7-never-cache-the-agent-paths) |
| Unprotected consent page | [Whole-site Access application](cloudflare.md#3-protect-the-whole-site-with-an-access-application) |
| Service token refused | [Bot service-token application](cloudflare.md#5-create-a-service-token-for-bots), token expiry, and headers |

The backup hostname uses its own Access service token and bearer; dashboard
agent bypass rules must not expose it.

## Backups or recovery fail

| You see | What to check |
| --- | --- |
| Backup upload refused | Check backup URL, Access service-token expiry, and bearer secret. The Worker and host must use the same bearer. |
| Backup quota or CPU failure | Inspect usage and alerts using the [capacity reference](../deployment.md#measured-size-and-free-tier-budget) and [live rehearsal](../deployment.md#live-free-plan-rehearsal). Resolve the limit before relying on automatic updates. |
| Restore cannot decrypt | On the recovery workstation, use the offline age identity matching the configured public recipient. Never copy the identity to production. |
| Restore integrity or hash check fails | Keep the failed artifact for investigation; select another verified snapshot and repeat the [restore drill](upgrade-existing.md#monthly-restore-drill). Do not install an unverified database. |
| Drive copy is missing | Check the owner's Drive connection, root folder, quota, and backup alerts. See [Drive backups](../deployment.md#a-second-independent-copy-in-google-drive). |

## Google calendar

The owner can inspect the connection at `/setup` or in Settings.

| You see | What to check |
| --- | --- |
| Reconnect Google | Reconnect the org account and check the OAuth app's publishing status and account permissions. |
| No Connect button | Configure the Google OAuth Client ID and secret file; see [Google setup](google-calendar.md#2-add-the-client-to-the-dashboard). |
| `redirect_uri_mismatch` | Register `https://<your-hostname>/auth/org-google/callback` exactly. |
| Sync or calendar-sharing failures | Inspect app logs and the officer's Google email; see [Organization calendar](../org-calendar.md). |
