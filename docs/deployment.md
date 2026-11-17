# Deployment

## Raspberry Pi 5 (systemd)

Run Go build commands from `server/`.

```sh
make build-release           # needs ../web/dist; embeds it. `make build` for an API-only binary
```
then follow the header of `deploy/dashboard.service` (user, env file `deploy/dashboard.env.example`,
install, enable). Put it behind a Cloudflare Tunnel or Caddy for HTTPS; set `APP_URL` to the public
URL. With a tunnel and Cloudflare Access in front, the Access, bypass and bot settings in
[Cloudflare Tunnel + Access](#cloudflare-tunnel--access) apply here too (steps 2 to 7; `cloudflared`
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

> [!NOTE]
> The original Go-port branch did not build the image because `web/dist` did not yet exist. During this documentation update, the multi-platform command reached runtime setup but failed with `exec format error` on the amd64 stage of this ARM host. Configure a multi-platform builder before using that command. Live Litestream restore/replication still needs deployment credentials and verification.

## Cloudflare Tunnel + Access

The dashboard and `cloudflared` run as two containers
([`compose.cloudflare.yml`](../server/deploy/compose.cloudflare.yml)). The dashboard publishes no
port, so the tunnel is the only way in. Cloudflare Access asks people to sign in before they reach
the dashboard. Only the URLs that agents call bypass Access, so Claude Code, Claude.ai, ChatGPT and
Cursor can connect to `https://<dashboard host>/mcp` with no custom headers.

```
officer's browser ─▶ Cloudflare ─▶ Access sign-in ─▶ tunnel ─▶ cloudflared ─▶ dashboard:8090
agent (Claude Code, Claude.ai, ChatGPT, Cursor) ─▶ Cloudflare ─▶ Access bypass ─▶ tunnel ─▶ …
```

| Path | Who calls it | Access |
| --- | --- | --- |
| `/mcp` | the agent (Claude Code and Cursor from the officer's computer; Claude.ai and ChatGPT from their companies' servers) | **Bypass** |
| `/oauth/register`, `/oauth/token` | the agent | **Bypass** |
| `/.well-known/oauth-protected-resource` (and `/mcp` under it), `/.well-known/oauth-authorization-server` | the agent | **Bypass** |
| `/oauth/authorize` (the "Allow … ?" consent page) | the officer's browser | Access sign-in |
| everything else | browsers | Access sign-in |

**Why bypass rather than an Access service token:** Claude.ai and ChatGPT connectors cannot send
custom headers such as `CF-Access-Client-Id`. The bypassed paths are still protected by the
dashboard. `/mcp` needs an OAuth access token. Getting one needs an officer's own Allow on the
consent page, which stays behind Access. Registration only creates an unprivileged client: it is
limited per IP, and unused clients are removed after a week. Discovery is public metadata. Every
browser page, the API and sign-in stay behind Access.

The steps below use **`officers.example.org`** for the dashboard's hostname. Replace it everywhere,
including in the rule expressions. You apply every step in the Cloudflare dashboard yourself;
nothing here needs Cloudflare API credentials. Cloudflare sometimes renames menus. Where the old
name is still common, it is given in brackets.

### 0. Before you start

- The domain is an active zone on your Cloudflare account (its nameservers point at Cloudflare).
- The server has Docker with Compose v2 and can reach Cloudflare outbound on port 7844 (TCP and
  UDP). `cloudflared` prints a "CONNECTIVITY PRE-CHECKS" table at start that confirms this.
- In the Discord developer portal, the app's redirect URI is `https://officers.example.org/auth/callback`.

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
   `TRUSTED_PROXIES` itself. `TRUSTED_PROXIES` is exactly the `tunnel` network's fixed subnet
   (`TUNNEL_SUBNET`, default `10.250.89.0/28`), so per-IP limits see each real client through
   `X-Forwarded-For`, never `cloudflared`'s address. Litestream works as in the plain container: set
   `LITESTREAM_*` in `dashboard.env`.

   If `APP_URL` and the published hostname disagree, the dashboard logs once: "a request forwarded
   by a trusted proxy names a different host than APP_URL".

### 2. Protect the dashboard with an Access application

Zero Trust → **Access** → **Applications** (newer: **Access controls** → **Applications**) →
**Add an application** → **Self-hosted**:

- **Application name** `Officer Dashboard`. **Session duration** your choice (24 hours is typical).
- **Add public hostname**: Subdomain `officers`, Domain `example.org`, **Path empty**. This covers
  the whole host.
- **Add a policy** (Action **Allow**):

  > **⚠ YOUR CHOICE: who may sign in.** Pick the Include rule that matches your club. Examples:
  > *Emails ending in* `@<your club's domain>`, an *Emails* list of officers, a Google Workspace or
  > GitHub group, or a *Login method*. One-time PIN works with no identity provider.
  >
  > Everyone who uses the site must pass this policy, not only officers. That includes **newly
  > invited officers** opening their join link, and **Launchpad builders and mentors** using the
  > portal (`/launchpad/…`, `/api/portal/…`). If they cannot pass it, either add them to the policy
  > or add their paths to the Bypass application in step 3. Both are deliberate decisions.

- Leave the application's **CORS settings** off. The dashboard answers its own preflights.

People then sign in to Access first, and to the dashboard with Discord after that.

**Other non-browser callers.** Anything else that calls the dashboard from a server also meets
Access: the service-token APIs (`/api/ingest/*`, `/api/agent/*`) and the Cloudflare sync. If you
use one from outside, give it an Access service token (it can send headers), or a separate Bypass
for that one path.

### 3. Let the agent paths bypass Access

**Add an application** → **Self-hosted** again:

- **Application name** `Officer Dashboard: agents (bypass)`.
- **Add public hostname** once per path. Each entry uses Subdomain `officers`, Domain `example.org`,
  and one of these Paths (no leading slash):
  - `mcp`
  - `oauth/register`
  - `oauth/token`
  - `.well-known/oauth-protected-resource`
  - `.well-known/oauth-authorization-server`
- **Add a policy**: Action **Bypass**, Include **Everyone**.

**How Access chooses** ([docs](https://developers.cloudflare.com/cloudflare-one/policies/access/app-paths/)):
"When multiple rules are set for a common root path, the more specific rule takes precedence."
The step 2 application has no path, so it covers every path. These path applications are more
specific, so they win for their paths and for anything under them: `mcp` also covers `/mcp/`, and
`.well-known/oauth-protected-resource` also covers `…/mcp`. `oauth/authorize` is not listed, so it
stays behind the step 2 application.

**Do not** enter `oauth` or `oauth/*`: that would also bypass the consent page. A Bypass turns
Access off for those requests, and Cloudflare does not log them. Cloudflare evaluates Bypass first.

### 4. Bot Fight Mode, Browser Integrity Check and WAF

None of the agents can solve a JavaScript challenge. A challenge on an agent path shows up as a
vague "couldn't connect" in Claude.ai or ChatGPT. Every request on those paths is automated by
design: Claude Code and Cursor from Node or Electron on the officer's computer, Claude.ai and
ChatGPT from their companies' servers.

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
  User-Agent. Skip it on the agent paths with the same rule (any plan).

**The skip rule** (any plan): **Security** → **WAF** → **Custom rules** → **Create rule**.
- **Rule name**: `MCP agents: skip bot checks`.
- **Edit expression**:
  ```
  (http.host eq "officers.example.org" and (http.request.uri.path eq "/mcp" or starts_with(http.request.uri.path, "/mcp/") or http.request.uri.path in {"/oauth/register" "/oauth/token" "/.well-known/oauth-authorization-server"} or starts_with(http.request.uri.path, "/.well-known/oauth-protected-resource")))
  ```
- **Action: Skip**. Under **WAF components to skip**, tick **All Super Bot Fight Mode rules** (Pro
  and above), **Browser Integrity Check** and **Security Level**. Add **All rate limiting rules**
  if you have any: the dashboard limits each agent to 120 calls a minute itself. On Pro and above
  you may also tick **All managed rules**, because MCP bodies (JSON, base64 receipts) can trip
  managed WAF rules.
- Turn on **Log matching requests**, **Deploy**, and drag the rule to the **top** of the list.

**Also check:**
- **Under Attack mode** must be off for this host.
- If **Block AI bots** / **AI Crawl Control** is on (Security → Settings), look in
  **Security → Events** for blocks on the agent paths after connecting Claude.ai or ChatGPT.
  `mcp-check` from your own computer cannot see a block that only hits their servers.

### 5. Never cache the agent paths

Cloudflare does not cache JSON by default, and the dashboard sends `Cache-Control: no-store` on
every agent path. A broad "cache everything" rule elsewhere on the zone could still catch the
discovery documents. **Caching** → **Cache Rules** → **Create rule**:

- **Rule name**: `MCP agents: bypass cache`.
- **Edit expression**: the same expression as step 4.
- **Cache eligibility: Bypass cache**. **Deploy**, then put it **last** in the list so it wins
  over broader rules.

### 6. Rocket Loader and Email Obfuscation off for the consent page

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

### 7. Check it from outside

```sh
cd server/deploy
docker compose -f compose.cloudflare.yml run --rm --no-deps --entrypoint /app/dashboard dashboard \
  mcp-check https://officers.example.org
```
(Or, on any machine with the binary, `dashboard mcp-check https://officers.example.org`.) It
checks both discovery documents, the 401 challenge on `/mcp`, the CORS preflight, the token
endpoint and the consent page. It names each kind of Cloudflare interference: an Access sign-in,
a challenge, a WAF block, an edge-cached answer, or a tunnel that is down. It also says which fix
above applies. It exits non-zero if anything fails. A correct setup ends like this:

```
  PASS  GET /oauth/authorize: Cloudflare Access sign-in (<team>.cloudflareaccess.com): officers sign in to Access, then to the dashboard
  INFO  Cloudflare: responses came through Cloudflare (cf-ray …)

7 passed, 0 warnings, 0 failed
```

`-register` also checks `POST /oauth/register`. It creates one unused client, which the dashboard
removes after a week, so run it once after setup rather than routinely.

Then connect an agent ([MCP agents](mcp.md#connecting)), for example
`claude mcp add --transport http cseed https://officers.example.org/mcp`. The browser opens the
Access sign-in, then the dashboard's sign-in, then **Allow**.

### Rehearse on a quick tunnel

[`compose.quicktunnel.yml`](../server/deploy/compose.quicktunnel.yml) runs the same two
containers and network with a temporary `*.trycloudflare.com` URL. It needs no Cloudflare
account, and the dashboard is the seeded dev build with developer sign-in locked by
`DEV_AUTH_KEY`. Its header has the commands. A quick tunnel has **no Access and none of the zone
settings**, so it proves the containers, `TRUSTED_PROXIES`, `APP_URL`, OAuth and MCP through
Cloudflare's edge. It cannot prove steps 2 to 6. Quick tunnels allow 200 requests in flight and no
SSE, which the stateless `/mcp` does not use. Run `down -v` when you are done.

## HTTPS and restore

Terminate HTTPS at a Cloudflare Tunnel or Caddy and forward to the server's loopback listener (`127.0.0.1:8090` in the systemd unit). Set `APP_URL` to the public HTTPS origin so OAuth callbacks, Secure cookies and HSTS agree with the public site.

Install the service following [the unit header](../server/deploy/dashboard.service), with secrets based on [the env example](../server/deploy/dashboard.env.example). The example does not enumerate every optional V0.3 integration; use [configuration](configuration.md).

For a local backup restore: stop the service, decompress the chosen `.db.gz` into the configured database path, remove the matching stale `-wal` and `-shm` files, and restart (migrations are reapplied as needed). See [backup script instructions](../server/scripts/backup). Container recovery uses Litestream restore instead. Back up local receipts separately.
