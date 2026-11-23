# Deployment

> Step-by-step runbooks: [fresh install](deploy/fresh-install.md), [upgrade an existing
> install](deploy/upgrade-existing.md) and [org calendar setup](deploy/org-calendar-setup.md)
> ([which one?](deploy/README.md)). This page is the reference they link to.

## Raspberry Pi 5 (systemd)

Run Go build commands from `server/`.

```sh
make build-release           # needs ../web/dist; embeds it. `make build` for an API-only binary
```
then follow the header of `deploy/dashboard.service` (user, env file `deploy/dashboard.env.example`,
install, enable). Put it behind a Cloudflare Tunnel or Caddy for HTTPS; set `APP_URL` to the public
URL. With a tunnel and Cloudflare Access in front, the Access, bypass and bot settings in
[Cloudflare Tunnel + Access](#cloudflare-tunnel--access) apply here too (steps 2 to 9; `cloudflared`
on the Pi reaches `127.0.0.1:8090`, which is trusted by default). Nightly backups: `deploy/dashboard-backup.{service,timer}` run `scripts/backup`.

**Backups.** `scripts/backup` uses `sqlite3 .backup` (safe on a live WAL database; never copy the
`.db` file alone), checks `PRAGMA integrity_check`, gzips, prunes to the newest 30 and can push
off-box via `BACKUP_REMOTE` (`rclone`/`aws s3 cp`). Restore steps are in its header. Receipts are
not in the database: Google Drive holds them (or `RECEIPTS_DIR`, which you must back up too).

## Cloudflare Containers / Docker

Run the following from the repository root. The multi-platform build needs a builder capable of running both target architectures (native nodes or QEMU/binfmt); cgo compilation uses cross-gcc, but runtime package installation executes target binaries.

```sh
(cd web && npm ci && npm run build)
docker buildx build --platform linux/amd64,linux/arm64 -f server/Dockerfile -t officer-dashboard .
```
Multi-stage; cross-compiles cgo with an installed cross-gcc; the runtime is
`debian:bookworm-slim` plus the `litestream` binary, running as a non-root user with a
`HEALTHCHECK` (`dashboard healthcheck`). The image listens on `:8090`, so it **refuses to start
without `APP_URL`** (the public https origin). When the TLS proxy (Caddy, cloudflared) reaches the
container over the Docker network rather than loopback, set `TRUSTED_PROXIES` to that network
(for example `172.16.0.0/12`, or the proxy's address): otherwise every user shares the proxy's
rate-limit budget for sign-in and uploads. [`compose.cloudflare.yml`](#cloudflare-tunnel--access)
does this for you with a fixed subnet. **Containers have ephemeral disks**, so the image
replicates the SQLite file with Litestream when `LITESTREAM_BUCKET` is set:

| Variable | Example |
| --- | --- |
| `LITESTREAM_BUCKET` | `cseed-dashboard-db` |
| `LITESTREAM_ENDPOINT` | `https://<account-id>.r2.cloudflarestorage.com` (R2; empty for AWS S3) |
| `LITESTREAM_REGION` | `auto` (R2) or `us-west-2` |
| `LITESTREAM_PATH` | `dashboard` (key prefix; default) |
| `LITESTREAM_ACCESS_KEY_ID`, `LITESTREAM_SECRET_ACCESS_KEY` | an R2/S3 token scoped to the bucket |

On boot `deploy/entrypoint.sh` runs `litestream restore -if-db-not-exists -if-replica-exists`, then
`litestream replicate -exec "dashboard serve"`: up to ~1 s of writes can be lost if the container
dies hard, and a replaced container comes back with the data. `deploy/litestream.yml` keeps 72 h of
WAL and a snapshot every 6 h. Receipts must use Google Drive in a container (`RECEIPTS_DIR` would
be wiped). Run **one** instance: SQLite plus Litestream is single-writer.

The image builds [Litestream 0.3.14](https://github.com/benbjohnson/litestream/releases/tag/v0.3.14)
from source with the same pinned Go 1.27.4 toolchain as the dashboard, using upstream's static
linking and build tags. It retains the existing 0.3 replica format.
Vulnerable dependencies are raised to exact minimum fixed versions in `server/Dockerfile`,
including higher versions required by those dependencies. Before cross-compiling, the build
runs Litestream's core and CLI tests and compiles its S3 package (which has no test files);
no Litestream source code is patched.
The Security workflow checks its version and local snapshot replication in the release image,
then scans the shipped binary with `govulncheck -mode=binary`. That scan is report-only for
GO-2022-0635 and GO-2022-0646: AWS SDK v1 has no fixed release, and migrating to SDK v2 requires
Litestream code changes. Findings remain visible; build, test and smoke failures block CI.
A 0.5 upgrade needs a separate migration: restore and verify the old
replica, take a fresh snapshot with the new binary under a new bucket prefix, verify that
restore, then switch `LITESTREAM_PATH`. Do not point a 0.5 binary at the existing 0.3 prefix.

> [!NOTE]
> The original Go-port branch did not build the image because `web/dist` did not yet exist. During this documentation update, the multi-platform command reached runtime setup but failed with `exec format error` on the amd64 stage of this ARM host. Configure a multi-platform builder before using that command. Live Litestream restore/replication still needs deployment credentials and verification.

## Cloudflare Tunnel + Access

The dashboard and `cloudflared` run as two containers
([`compose.cloudflare.yml`](../server/deploy/compose.cloudflare.yml)). The dashboard publishes no
port, so the tunnel is the only way in. Cloudflare Access asks every person to sign in with a
**one-time PIN** sent to their email before they reach the dashboard. Only emails in the
**Officers** or **Launchpad** group get a PIN. Services (the Discord check-in bot, the Luma ingest,
the purchasing agent) pass Access with an **Access service token**. Only the URLs that MCP agents
call bypass Access, so Claude Code, Claude.ai, ChatGPT and Cursor can connect to
`https://<dashboard host>/mcp` with no custom headers.

```
officer's browser ──────▶ Cloudflare ─▶ Access: PIN, Officers group ─▶ tunnel ─▶ cloudflared ─▶ dashboard:8090
builder's/mentor's browser ─▶ Cloudflare ─▶ Access: PIN, Launchpad group ─▶ tunnel ─▶ …
bot, Luma ingest, purchasing agent ─▶ Cloudflare ─▶ Access: service token ─▶ tunnel ─▶ …
MCP agent (Claude Code, Claude.ai, ChatGPT, Cursor) ─▶ Cloudflare ─▶ Access bypass ─▶ tunnel ─▶ …
```

| Path | Who calls it | Access |
| --- | --- | --- |
| `/mcp` | the agent (Claude Code and Cursor from the officer's computer; Claude.ai and ChatGPT from their companies' servers) | **Bypass** (step 4) |
| `/oauth/register`, `/oauth/token` | the agent | **Bypass** |
| `/.well-known/oauth-protected-resource` (and `/mcp` under it), `/.well-known/oauth-authorization-server` | the agent | **Bypass** |
| `/api/ingest/*`, `/api/agent/*` | the Discord check-in bot, the Luma ingest, the purchasing agent | **Service token** (step 5) |
| `/oauth/authorize` (the "Allow … ?" consent page) | the officer's browser | PIN sign-in (step 3) |
| `/launchpad/*` (portal and join page), `/api/portal/*` | builders' and mentors' browsers | PIN sign-in |
| `/login?invite=…`, `/auth/callback` (Discord sign-in) | new officers, and everyone signing in | PIN sign-in |
| everything else | browsers | PIN sign-in |

**Why bypass rather than an Access service token for agents:** Claude.ai and ChatGPT connectors
cannot send custom headers such as `CF-Access-Client-Id`. The bypassed paths are still protected by
the dashboard. `/mcp` needs an OAuth access token. Getting one needs an officer's own Allow on the
consent page, which stays behind Access. Registration only creates an unprivileged client: it is
limited per IP, and unused clients are removed after a week. Discovery is public metadata. Every
browser page, the API, the portal and sign-in stay behind Access.

The steps below use **`officers.example.org`** for the dashboard's hostname. Replace it everywhere,
including in the rule expressions. You apply every step in the Cloudflare dashboard yourself;
nothing here needs Cloudflare API credentials. Cloudflare sometimes renames menus. Where the old
name is still common, it is given in brackets.

### 0. Before you start

- The domain is already on Cloudflare, so you start at the tunnel.
- The server has Docker with Compose v2 and can reach Cloudflare outbound on port 7844 (TCP and
  UDP). `cloudflared` prints a "CONNECTIVITY PRE-CHECKS" table at start that confirms this.
- In the Discord developer portal, the app's redirect URI is `https://officers.example.org/auth/callback`.
- Have ready the email of every officer, and of every current Launchpad builder and mentor.

### 1. Create the tunnel and start the containers

1. Go to [one.dash.cloudflare.com](https://one.dash.cloudflare.com) (Zero Trust) → **Networking** →
   **Tunnels** → **Create a tunnel** → **Cloudflared**. Name it `officer-dashboard`, then **Save tunnel**.
2. On "Install and run connectors", pick **Docker**. **Do not run the command shown.** Copy only
   the long token after `--token`. It is a secret: anyone with it can run your tunnel.
3. Open the tunnel's **Routes** tab → **Add route** → **Published application** (older dashboards:
   **Public Hostname** → **Add a public hostname**):
   - **Subdomain** `officers`, **Domain** `example.org`, **Path** empty.
   - **Service URL** `http://dashboard:8090` (older form: Type `HTTP`, URL `dashboard:8090`).
   - Leave **HTTP Host Header** (under additional application settings) empty, so the dashboard
     sees `officers.example.org`.

   Save. Cloudflare creates the DNS record itself.
4. On the server:
   ```sh
   cd server/deploy
   cp .env.example .env                    # APP_URL=https://officers.example.org, TUNNEL_TOKEN=<token>
   cp dashboard.env.example dashboard.env  # Discord, Drive, SMTP, LITESTREAM_* (see configuration.md)
   chmod 600 .env dashboard.env
   (cd ../../web && npm ci && npm run build)   # the image embeds web/dist
   docker compose -f compose.cloudflare.yml up -d --build
   docker compose -f compose.cloudflare.yml logs cloudflared | grep "Registered tunnel connection"
   ```
   The tunnel shows **Healthy** in the dashboard. Compose sets `APP_URL`, `ADDR` and
   `TRUSTED_PROXIES` itself. The `tunnel` network has a fixed subnet (`TUNNEL_NET`, default
   `10.250.89`, so `10.250.89.0/28`), and its containers get addresses only from `.8/29`.
   `TRUSTED_PROXIES` is exactly that range, so per-IP limits see each real client through
   `X-Forwarded-For`, never `cloudflared`'s address. The Docker gateway (`.1`), through which a
   port you publish on the host would arrive, is outside it and never trusted. If compose says the
   address pool overlaps, set another `TUNNEL_NET` in `.env`. Litestream works as in the plain container: set
   `LITESTREAM_*` in `dashboard.env`.

   If `APP_URL` and the published hostname disagree, the dashboard logs once: "a request forwarded
   by a trusted proxy names a different host than APP_URL".

### 2. One-time PIN and the two groups

1. **Turn on one-time PIN:** Zero Trust → **Integrations** → **Identity providers** → **Add new
   identity provider** → **One-time PIN** (older: **Settings** → **Authentication** → **Login
   methods** → **Add new**). There is nothing to fill in. A person types their email and gets a
   PIN at that address only. Each PIN works once and expires after 10 minutes. Cloudflare sends it
   only when the email passes a policy. Anyone else sees "A code has been emailed to you", but no
   code arrives ([docs](https://developers.cloudflare.com/cloudflare-one/integrations/identity-providers/one-time-pin/)).
2. **Create the groups:** Zero Trust → **Access controls** → **Policies** → **Rule groups** tab →
   **Add a group** (older: **Access** → **Access Groups**):
   - Name `Officers`. **Include** → Selector **Emails** → each officer's email. **Save**.
   - Name `Launchpad`. **Include** → Selector **Emails** → each builder's and mentor's email.
     **Save**.

**Adding someone later:** **Access controls** → **Policies** → **Rule groups** → open the group →
add the email to the **Emails** value → **Save**. It applies at that person's next sign-in. The
application and its policy do not change.

> **⚠ Add the email before you send the invite.** An invite link only opens after its recipient
> passes Access.
> - **Launchpad builder or mentor:** add their email to **Launchpad** first, then send the invite
>   from Buildspace + Launchpad ([invite flow](launchpad-cohorts.md#invite--bind--session)).
> - **New officer:** add their email to **Officers** first, then send the officer invite. Officer
>   invites go by Discord DM, so ask for the email they will type at the Access sign-in.
>
> Otherwise the link stops at the Access sign-in, and no PIN arrives.

Taking an email out of a group stops new sign-ins. A browser that already signed in keeps its Access
session until it expires (up to a month), so also disable the person in the dashboard (Officers, or the
Launchpad Invites tab). The dashboard checks its own accounts on every request.

### 3. Protect the dashboard with an Access application

Zero Trust → **Access controls** → **Applications** (older: **Access** → **Applications**) → **Add an
application** → **Self-hosted**:

- **Application name** `Officer Dashboard`.
- **Session Duration**: **1 month**, the longest Cloudflare offers ("Available session durations
  range from immediate timeout to one month",
  [docs](https://developers.cloudflare.com/cloudflare-one/access-controls/access-settings/session-management/)).
  Each browser asks for a PIN at most once a month.
- **Add public hostname**: Subdomain `officers`, Domain `example.org`, **Path empty**. This covers
  the whole host.
- **Login methods**: only **One-time PIN**. Untick any other identity provider, or turn off
  **Accept all available identity providers** if it is shown. Turn on **Apply instant
  authentication**, so people go straight to the email form.
- **Add a policy**: name `Officers and Launchpad`, Action **Allow**. **Include** → Selector **Rule
  group** (older: **Access groups**) → `Officers`. **Add include** → **Rule group** → `Launchpad`.
  Include rules are alternatives, so either group passes. Leave the policy's **Session Duration**
  at "Same as application session timeout".
- Leave the application's **CORS settings** off. The dashboard answers its own preflights.

Also set **Access controls** → **Access settings** → **Set your global session duration** →
**1 month**. That way the PIN sign-in itself is also remembered for a month.

Officers then enter a PIN once a month, and sign in to the dashboard with Discord.

Builders and mentors follow their invite email to `/launchpad/join`, and enter a PIN there (once a
month). Then they choose **Continue with Discord**.

The portal (`/launchpad/*`, `/api/portal/*`), the join page and the Discord sign-in callback stay
behind this application on purpose. Do not add a bypass for them.

### 4. Let the agent paths bypass Access

**Add an application** → **Self-hosted** again:

- **Application name** `Officer Dashboard: agents (bypass)`.
- **Add public hostname** once per path. Each entry uses Subdomain `officers`, Domain `example.org`,
  and one of these Paths (no leading slash):
  - `mcp`
  - `oauth/register`
  - `oauth/token`
  - `.well-known/oauth-protected-resource`
  - `.well-known/oauth-protected-resource/mcp` (the document agents fetch first)
  - `.well-known/oauth-authorization-server`
- **Add a policy**: Action **Bypass**, Include **Everyone**.

**How Access chooses** ([docs](https://developers.cloudflare.com/cloudflare-one/access-controls/policies/app-paths/)):
"When multiple rules are set for a common root path, the more specific rule takes precedence."
The step 3 application has no path, so it covers every path. These path applications are more
specific, so they win for their paths. Each path the agents use is listed exactly, rather than
relying on how far an entry reaches below itself. `oauth/authorize` is not listed, so it stays
behind the step 3 application. `mcp-check` (step 9) fetches every listed path and fails on any
that still meets the Access sign-in.

**Do not** enter `oauth` or `oauth/*`: that would also bypass the consent page. A Bypass turns
Access off for those requests, and Cloudflare does not log them. Within an application, Bypass
(and Service Auth) policies are evaluated before Allow and Block.

### 5. A service token for the bot, the Luma ingest and the purchasing agent

These callers run on servers and cannot type a PIN. They send an Access service token as two
headers, next to their usual dashboard token.

1. **Create the token:** Zero Trust → **Access controls** → **Service credentials** → **Service
   Tokens** → **Create Service Token**. Name it `officer-dashboard services`. For **Service Token
   Duration**, pick the longest offered, and note the expiry date: **Refresh** adds a year. Select
   **Generate token**, then copy the **Client ID** and the **Client Secret**. "This is the only time
   Cloudflare Access will display the Client Secret"
   ([docs](https://developers.cloudflare.com/cloudflare-one/access-controls/service-credentials/service-tokens/)).
   Store both wherever the callers keep the dashboard token.
2. **Create the application:** **Add an application** → **Self-hosted**:
   - **Application name** `Officer Dashboard: services (service token)`.
   - **Add public hostname** twice. Both use Subdomain `officers` and Domain `example.org`, with Path
     `api/ingest` and then `api/agent`. A path covers everything below it, so the two entries
     cover:
     - `/api/ingest/luma`, `/api/ingest/buildspace-launchpad` and `/api/ingest/buildspace`;
     - `/api/agent/jobs`, `/api/agent/jobs/<id>/claim` and `/api/agent/jobs/<id>/complete`.

     They are more specific than the step 3 application, so only this application's policy applies
     to them.
   - **Add a policy**: name `Service token`. Action **Service Auth**, not Allow: with Allow, "Access
     will prompt for an identity provider login". **Include** → Selector **Service Token** →
     `officer-dashboard services`.
   - Turn on **401 Response for Service Auth policies** in the application's settings. A missing or
     wrong token then gets a plain 401, not a redirect to the sign-in page.
3. **Give each caller the two headers.** Every request carries them, plus the dashboard token:
   ```
   CF-Access-Client-Id: <Client ID>
   CF-Access-Client-Secret: <Client Secret>
   Authorization: Bearer <the dashboard's service token for this caller>
   ```

   | Caller | Routes | Where it lives | What to change |
   | --- | --- | --- | --- |
   | Discord check-in bot | `POST /api/ingest/buildspace-launchpad` (and the older `/api/ingest/buildspace`) | the adapter is in this repo, [`scripts/buildspace-discord-stub.mjs`](../scripts/buildspace-discord-stub.mjs); the bot itself is outside | Set `CF_ACCESS_CLIENT_ID` and `CF_ACCESS_CLIENT_SECRET` in the bot's environment. The adapter then adds both headers (none while they are unset), and it reports an Access redirect instead of following it. Use `accessHeaders()` from the same file for any other call the bot makes. |
   | Luma ingest | `POST /api/ingest/luma` | outside this repo | Add both headers to its request. |
   | Purchasing (fulfillment) agent | `GET /api/agent/jobs`, `POST /api/agent/jobs/<id>/claim`, `…/complete` | outside this repo | Add both headers to every request. Treat a 401 or a redirect from Cloudflare (no JSON `error`) as "Access refused the token", not as a job failure. |

   For example, a Luma ingest with curl:
   ```sh
   curl -X POST https://officers.example.org/api/ingest/luma \
     -H "CF-Access-Client-Id: $CF_ACCESS_CLIENT_ID" \
     -H "CF-Access-Client-Secret: $CF_ACCESS_CLIENT_SECRET" \
     -H "Authorization: Bearer $LUMA_INGEST_TOKEN" \
     -H "Content-Type: application/json" --data @guests.json
   ```

The dashboard itself never needs the service token. The Cloudflare sync needs nothing, because the
dashboard calls Cloudflare's API, not the other way round. If the secret leaks, delete the token
under **Service Tokens**, create a new one, add it to the policy, and update the callers.

### 6. Bot Fight Mode, Browser Integrity Check and WAF

None of the agents can solve a JavaScript challenge. A challenge on an agent path shows up as a
vague "couldn't connect" in Claude.ai or ChatGPT. Every request on those paths is automated by
design: Claude Code and Cursor from Node or Electron on the officer's computer, Claude.ai and
ChatGPT from their companies' servers. The step 5 callers are automated too.

- **Bot Fight Mode (Free plan): turn it off.** It runs on the whole zone and cannot be exempted
  for some paths. Cloudflare's
  [docs](https://developers.cloudflare.com/bots/get-started/bot-fight-mode/) say: "You cannot
  bypass or skip Bot Fight Mode using WAF custom rules or Page Rules", and the
  [Skip options](https://developers.cloudflare.com/waf/custom-rules/skip/options/) page says:
  "Currently, you cannot skip Bot Fight Mode, only Super Bot Fight Mode." To turn it off:
  **Security** → **Settings**, filter **Bot traffic**, then **Bot Fight Mode** → Off. Access still
  guards every human page.
- **Super Bot Fight Mode (Pro and above)** can stay on, with an exemption: the skip rule below
  with **All Super Bot Fight Mode rules** ticked.
- **Browser Integrity Check** is on by default and challenges requests with a missing or unusual
  User-Agent. Skip it on the agent and service paths with the same rule (any plan).

**The skip rule** (any plan): **Security** → **WAF** → **Custom rules** → **Create rule**.
- **Rule name**: `Agents and services: skip bot checks`.
- **Edit expression**:
  ```
  (http.host eq "officers.example.org" and (http.request.uri.path eq "/mcp" or starts_with(http.request.uri.path, "/mcp/") or http.request.uri.path in {"/oauth/register" "/oauth/token" "/.well-known/oauth-authorization-server"} or starts_with(http.request.uri.path, "/.well-known/oauth-protected-resource") or starts_with(http.request.uri.path, "/api/ingest/") or starts_with(http.request.uri.path, "/api/agent/")))
  ```
- **Action: Skip**. Under **WAF components to skip**, tick **All Super Bot Fight Mode rules** (Pro
  and above), **Browser Integrity Check** and **Security Level**. Add **All rate limiting rules**
  if you have any: the dashboard limits each agent to 120 calls a minute itself. On Pro and above
  you may also tick **All managed rules**, because MCP bodies (JSON, base64 receipts) can trip
  managed WAF rules.
- Turn on **Log matching requests**, **Deploy**, and drag the rule to the **top** of the list.

Access still checks the service token on `/api/ingest/` and `/api/agent/`. The skip rule turns off
only the bot checks.

**Also check:**
- **Under Attack mode** must be off for this host.
- If **Block AI bots** / **AI Crawl Control** is on (Security → Settings), look in
  **Security → Events** for blocks on the agent paths after connecting Claude.ai or ChatGPT.
  `mcp-check` from your own computer cannot see a block that only hits their servers.

### 7. Never cache the agent paths

Cloudflare does not cache JSON by default, and the dashboard sends `Cache-Control: no-store` on
every agent path. A broad "cache everything" rule elsewhere on the zone could still catch the
discovery documents. **Caching** → **Cache Rules** → **Create rule**:

- **Rule name**: `Agents and services: bypass cache`.
- **Edit expression**: the same expression as step 6.
- **Cache eligibility: Bypass cache**. **Deploy**, then put it **last** in the list so it wins
  over broader rules.

### 8. Rocket Loader and Email Obfuscation off for the consent page

Both rewrite HTML. The consent page has a strict policy (no scripts) and hidden form fields that
must arrive unchanged. **Rules** → **Configuration Rules** → **Create rule**:

- **Rule name**: `Consent page: no rewriting`.
- **Edit expression**:
  ```
  (http.host eq "officers.example.org" and http.request.uri.path eq "/oauth/authorize")
  ```
- Settings: **Rocket Loader** Off, **Email Obfuscation** Off. **Deploy**.

The rest of the dashboard also allows only its own scripts, so the simplest choice is to turn
Rocket Loader off for the zone (**Speed** → **Optimization** → **Content Optimization**).

### 9. Check it from outside

```sh
cd server/deploy
export CF_ACCESS_CLIENT_ID=<Client ID>              # optional: also checks step 5
read -rs CF_ACCESS_CLIENT_SECRET && export CF_ACCESS_CLIENT_SECRET   # paste it; stays out of history
docker compose -f compose.cloudflare.yml run --rm --no-deps \
  -e CF_ACCESS_CLIENT_ID -e CF_ACCESS_CLIENT_SECRET --entrypoint /app/dashboard dashboard \
  mcp-check https://officers.example.org
```
(Or, on any machine with the binary, `dashboard mcp-check https://officers.example.org` with the
same two variables.) It checks:
- both discovery documents, the 401 challenge on `/mcp`, the CORS preflight and the token endpoint;
- that the consent page meets the Access sign-in;
- that `POST /api/ingest/luma` with the service token and no dashboard token gets through Access
  to the dashboard's own 401.

It names each kind of Cloudflare interference: an Access sign-in, a challenge, a WAF block, an
answer the edge cached or would cache, or a tunnel that is down. It also says which fix above
applies. It never prints the secret, and it exits non-zero if anything fails. A correct setup ends
like this:

```
  PASS  GET /oauth/authorize: Cloudflare Access sign-in (<team>.cloudflareaccess.com): officers sign in to Access, then to the dashboard
  PASS  POST /api/ingest/luma (service call): Access let the service token through: the dashboard's own 401 (no dashboard token was sent)
  INFO  Cloudflare: responses came through Cloudflare (cf-ray …)

8 passed, 0 warnings, 0 failed
```

Without the two variables, the service line is `INFO … Cloudflare Access answered 401 …: service
callers need the Access service token`, which is expected, and the run ends `7 passed`.

`-register` also checks `POST /oauth/register`. It creates one unused client, which the dashboard
removes after a week, so run it once after setup rather than routinely.

Then connect an agent ([MCP agents](mcp.md#connecting)), for example
`claude mcp add --transport http cseed https://officers.example.org/mcp`. The browser opens the
Access sign-in (PIN), then the dashboard's sign-in, then **Allow**.

### Rehearse on a quick tunnel

[`compose.quicktunnel.yml`](../server/deploy/compose.quicktunnel.yml) runs the same two
containers and network with a temporary `*.trycloudflare.com` URL. It needs no Cloudflare
account, and the dashboard is the seeded dev build with developer sign-in locked by
`DEV_AUTH_KEY`. Its header has the commands. A quick tunnel has **no Access and none of the zone
settings**, so it proves the containers, `TRUSTED_PROXIES`, `APP_URL`, OAuth and MCP through
Cloudflare's edge. It cannot prove steps 2 to 8. Quick tunnels allow 200 requests in flight and no
SSE, which the stateless `/mcp` does not use. There `mcp-check` reports two warnings, as expected:
`GET /oauth/authorize`, and `POST /api/ingest/luma`, which came through Cloudflare without Access.
Run `down -v` when you are done.

## HTTPS and restore

Terminate HTTPS at a Cloudflare Tunnel or Caddy and forward to the server's loopback listener (`127.0.0.1:8090` in the systemd unit). Set `APP_URL` to the public HTTPS origin so OAuth callbacks, Secure cookies and HSTS agree with the public site.

Install the service following [the unit header](../server/deploy/dashboard.service), with secrets based on [the env example](../server/deploy/dashboard.env.example). The example does not enumerate every optional V0.3 integration; use [configuration](configuration.md).

For a local backup restore: stop the service, decompress the chosen `.db.gz` into the configured database path, remove the matching stale `-wal` and `-shm` files, and restart (migrations are reapplied as needed). See [backup script instructions](../server/scripts/backup). Container recovery uses Litestream restore instead. Back up local receipts separately.
