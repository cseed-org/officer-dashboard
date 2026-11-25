# Troubleshooting

> Current Free-plan image: use [Fresh install](fresh-install.md) and
> [Upgrade/restore](upgrade-existing.md). R2/Litestream commands below apply only
> to the previous image; Litestream is no longer shipped. Tunnel/Access steps remain useful.


Find what you see in the left column. Commands run on the server in
`~/officer-dashboard/server/deploy`. To read the dashboard's log:
`docker compose -f compose.cloudflare.yml logs dashboard`.

## The download fails

| You see | Why | Fix |
| --- | --- | --- |
| `denied` from `pull` | the image is private and this machine has no GitHub login, the token expired, or that account can't see the package | log in again ([deploy guide step 1, item 6](README.md#1-prepare-the-server)), with a token from an account that can open the package page |
| `manifest unknown` or `not found` | no image has that tag | compare `DASHBOARD_TAG` in `.env` with the [package page](https://github.com/cseed-org/officer-dashboard/pkgs/container/officer-dashboard). A tag appears there about 15 minutes after its push, and only if its checks passed |
| `no matching manifest for linux/arm/v7` | the system is 32-bit | install the 64-bit OS (`uname -m` must say `aarch64` or `x86_64`) |
| `set DASHBOARD_TAG in server/deploy/.env…` (or `APP_URL`, `TUNNEL_TOKEN`) | that line in `.env` is missing or empty | fill it in ([deploy guide step 4](README.md#4-fill-in-the-settings-files)) |
| `permission denied while trying to connect to the Docker daemon socket` | your user is not in the `docker` group yet | [deploy guide step 1, item 3](README.md#1-prepare-the-server), then log out and in |

## The dashboard doesn't start

`docker compose -f compose.cloudflare.yml ps` shows `dashboard` restarting or `(unhealthy)`. Read
the log's last lines: `docker compose -f compose.cloudflare.yml logs --tail 30 dashboard`.

| The log says | Why | Fix |
| --- | --- | --- |
| `APP_URL … must be https` | `APP_URL` in `.env` starts with `http://` | use `https://<your-hostname>` |
| `BOOTSTRAP_OWNER_DISCORD_ID must be a 17–20 digit Discord ID` | the ID has a typo, spaces or quotes | copy it again ([deploy guide step 3](README.md#3-create-the-discord-app)) |
| `fatal … migration <file> …` | a migration failed (usually during an upgrade) | stop it at once (`docker compose -f compose.cloudflare.yml stop dashboard`), keep the log, and [roll back](upgrade.md#rollback) |
| a Litestream error about the bucket, key or endpoint | a `LITESTREAM_` line is wrong | check all six against [Cloudflare step 9](cloudflare.md#9-create-the-r2-bucket-for-backups); the endpoint is `https://<account ID>.r2.cloudflarestorage.com` |
| `LITESTREAM_BUCKET is not set: the database lives only on this container's disk` | the Litestream lines are still commented out | remove the `#` from all six ([deploy guide step 4](README.md#4-fill-in-the-settings-files)). Without them there is no backup |
| `Pool overlaps with other one on this address space` (from `up`) | another network on this machine uses `10.250.89.x` | set `TUNNEL_NET` in `.env` to another private prefix, for example `TUNNEL_NET=10.251.90` |

## The site doesn't open

| You see | Why | Fix |
| --- | --- | --- |
| Cloudflare error **1033**, or the tunnel shows down | `cloudflared` isn't connected | `docker compose -f compose.cloudflare.yml logs cloudflared`. A token error: check `TUNNEL_TOKEN` (the whole value after `--token`). It waits for the dashboard to be `(healthy)` first. Outgoing port 7844 (TCP and UDP) must not be blocked ([docs](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/configure-tunnels/tunnel-with-firewall/)) |
| Cloudflare error **502** | the tunnel is up but can't reach the dashboard | the route's Service URL must be `http://dashboard:8090` ([Cloudflare step 1](cloudflare.md#1-create-the-tunnel)), and the dashboard must be running |
| the browser can't find the site at all | DNS isn't ready | the domain must be **Active** in Cloudflare ([step 0](cloudflare.md#0-put-your-domain-on-cloudflare-and-open-zero-trust)), and the tunnel's route must exist |
| the log says `a request forwarded by a trusted proxy names a different host than APP_URL` | the tunnel's hostname and `APP_URL` differ | make them the same hostname |

## Signing in fails

| You see | Why | Fix |
| --- | --- | --- |
| "A code has been emailed to you", but no email arrives | that email isn't in the `Officers` or `Launchpad` group | add it ([Cloudflare: adding someone](cloudflare.md#adding-someone-later)); check spam too |
| Cloudflare offers other login methods, or none | the Access application's login methods | only **One-time PIN**, with instant authentication ([Cloudflare step 3](cloudflare.md#3-protect-the-whole-site-with-an-access-application)) |
| Discord says the redirect URI is invalid | the Discord app's redirect doesn't match | exactly `https://<your-hostname>/auth/callback` ([deploy guide step 3](README.md#3-create-the-discord-app)) |
| "Sign in with Discord" fails, and the log says `Discord sign-in is not configured` | `DISCORD_CLIENT_ID` or `DISCORD_CLIENT_SECRET` is empty | fill both in `dashboard.env`, then `docker compose -f compose.cloudflare.yml up -d` |
| you sign in but have no owner access | `BOOTSTRAP_OWNER_DISCORD_ID` was wrong or empty on first start | [deploy guide step 6](README.md#6-sign-in-as-the-owner), last paragraph |
| an invited person's link stops at the Cloudflare sign-in | their email wasn't in the group before the invite | add it, then they open the link again |

## `mcp-check` fails

`mcp-check` ([Cloudflare step 10](cloudflare.md#10-check-it-from-outside)) names a step for each
problem. Its messages say "docs/deployment.md"; the step numbers are the same as in
[Cloudflare setup](cloudflare.md):

| It mentions | Fix |
| --- | --- |
| an Access sign-in on an agent path (step 4) | the bypass application is missing a path, or has a typo ([step 4](cloudflare.md#4-let-ai-agents-through-on-their-own-paths)) |
| a challenge (step 6) | Bot Fight Mode is on, or the skip rule is missing or not at the top ([step 6](cloudflare.md#6-turn-off-bot-checks-for-agents)) |
| a WAF block (step 6) | find the rule under **Security** → **Events** and add it to the skip rule ([step 6](cloudflare.md#6-turn-off-bot-checks-for-agents)) |
| a cached answer (step 7) | add the cache rule ([step 7](cloudflare.md#7-never-cache-the-agent-paths)) |
| the tunnel (step 1) | see [The site doesn't open](#the-site-doesnt-open) |
| the consent page came through without Access (step 3) | the whole-site application is missing ([step 3](cloudflare.md#3-protect-the-whole-site-with-an-access-application)) |
| the service token was refused (step 5) | check the service-token application covers `api/ingest` and `api/agent` with a **Service Auth** policy, the token hasn't expired, and you exported the right Client ID and Secret ([step 5](cloudflare.md#5-create-a-service-token-for-bots)) |

## Backups

| You see | Why | Fix |
| --- | --- | --- |
| `litestream generations` prints no row | Litestream isn't copying | check the log for Litestream errors, and the six `LITESTREAM_` lines |
| the restore test fails | the backup can't be read | same checks; fix this before any upgrade |
| R2 shows nothing under `dashboard/` | wrong bucket, or `LITESTREAM_PATH` was changed | compare `LITESTREAM_BUCKET` and `LITESTREAM_PATH` with what R2 shows (after a rollback the folder is `dashboard-rollback-…`) |

## Google calendar

The **Settings → org Google Calendar** card shows the state. Log lines:
`docker compose -f compose.cloudflare.yml logs dashboard | grep "org calendar"`. The card's
warnings reset when the dashboard restarts, until the next sync sets them again.

| You see | Why | Fix |
| --- | --- | --- |
| **Reconnect Google** on the card | Google revoked access: the app is still in Testing (7-day limit), the org Gmail's password changed, or its access was removed in the Google account | connect again with the same Gmail ([Google step 3](google-calendar.md#3-connect-the-org-gmail)). If it was Testing, publish the app ([Google step 1](google-calendar.md#1-create-the-google-oauth-client), item 7) |
| "The last sync with Google failed", and the log says `org calendar sync failed; retained for retry` | one sync pass failed | it retries on its own (1 minute to 1 hour). A `403` in the error usually means the Calendar API is off or the `calendar` scope is missing (Google step 1, items 4 and 6) |
| log: `org calendar ACL entry failed … email=…` | Google refused to share with that address: not a Google account, or a typo | fix that officer's account email in **Officers → Access** |
| log: `org calendar ACL sync incomplete; retrying with backoff` | the line above, for one or more officers | fix the addresses; it retries on its own |
| no **Connect** button, or the log says `Google Calendar sync is not configured` | the two `GOOGLE_OAUTH_` lines are empty | [Google step 2](google-calendar.md#2-add-the-client-to-the-dashboard) |
| `redirect_uri_mismatch` on Google's page | the org redirect URI is missing or misspelled | exactly `https://<your-hostname>/auth/org-google/callback` |
| an officer sees no calendars | no account email, or it isn't a Google account | set it in **Officers → Access**; sharing follows within a minute |
