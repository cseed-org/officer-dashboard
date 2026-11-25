# Cloudflare setup

> Current Free-plan image: use [Fresh install](fresh-install.md) and
> [Upgrade/restore](upgrade-existing.md). R2/Litestream commands below apply only
> to the previous image; Litestream is no longer shipped. Tunnel/Access steps remain useful.


You come here from [step 2 of the deploy guide](README.md#2-set-up-cloudflare). Do steps 0 to 9
in order, then go back. Step 10 runs later, once the dashboard is up.

**What Cloudflare does for the dashboard:**

- **Tunnel:** a program on your server (`cloudflared`) opens an outgoing connection to Cloudflare,
  and Cloudflare sends your hostname's visitors through it. Your server opens no ports.
- **Access:** a sign-in page Cloudflare shows before anyone reaches the dashboard. People type
  their email and get a one-time PIN. Only emails you list get one.
- **R2:** cloud file storage, where the database backup lives.

A few words you'll meet: an **Access application** is one protected address (or a set of paths
under it); a **policy** says who gets in, and how; a **rule group** is a reusable list of emails.
**WAF** (web application firewall) is Cloudflare's set of filters that block suspicious requests.

Throughout, `<your-hostname>` is your dashboard's address (for example `officers.example.org`),
`<subdomain>` its first part (`officers`) and `<your-domain>` the rest (`example.org`).

**Menu names.** Cloudflare renames menus often. The click paths below were checked against
Cloudflare's own docs on 2026-11-24; each step links the page. If a menu looks different, the
linked page has the current name. Nothing here was tried on a live account.

## 0. Put your domain on Cloudflare and open Zero Trust

The tunnel and the sign-in only work for a domain whose DNS (the internet's address book for your
domain) Cloudflare runs. That is why the domain's nameservers move to Cloudflare. Skip items 1 and
2 if your domain already shows as **Active** in your Cloudflare account.

1. **Add the domain** ([docs](https://developers.cloudflare.com/fundamentals/manage-domains/add-site/)).
   Sign in at [dash.cloudflare.com](https://dash.cloudflare.com) → **Domains** → **Onboard a
   domain**. Enter your domain without `www` (for example `example.org`) → **Continue**. Choose
   the **Free** plan. Cloudflare copies the domain's existing DNS records. Check that every
   record you use is in the list, especially `MX` records if the domain receives email, and add
   any that are missing: after the switch, only the records listed here work. **Continue**.
2. **Point the domain at Cloudflare.** Cloudflare shows two nameservers. At your registrar (where
   you bought the domain), replace the domain's nameservers with those two. If DNSSEC is on at the
   registrar, turn it off first. Cloudflare emails you when the domain is active; this can take
   from minutes to a day. Steps 1 and 3 to 8 need the domain **Active**: they use its name or
   its settings. Meanwhile you can do item 3, step 2 and step 9 here, and steps 1 and 3 of the
   deploy guide.
3. **Open Zero Trust** ([docs](https://developers.cloudflare.com/cloudflare-one/setup/)). In the
   Cloudflare dashboard, select **Zero Trust** in the left menu. On first use it asks for a
   **team name**. Pick something short, like your club's name: your sign-in page will be
   `<team>.cloudflareaccess.com`. Choose the **Free** plan. It asks for payment details even on
   the Free plan; you are not charged.

You should see the domain as **Active** under **Domains**, and the Zero Trust home page.

## 1. Create the tunnel

([docs](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/get-started/create-remote-tunnel/))

1. **Zero Trust** → **Networking** → **Tunnels** → **Create a tunnel**. If asked for a type,
   pick **Cloudflared**. Name it `officer-dashboard` → **Create Tunnel**.
2. Cloudflare shows install commands for several systems. Pick **Docker**. Don't run the
   command. It looks like this:
   ```
   docker run cloudflare/cloudflared:latest tunnel --no-autoupdate run --token eyJhIjoi…
   ```
   Copy only the long value after `--token` (it starts with `eyJ`) and save it as the **tunnel
   token**. It is a secret: anyone who has it can run your tunnel.
3. Select **Next** to reach the routes. (If the wizard ends without a routes page, open the tunnel
   from the **Tunnels** list; the same form is on its **Routes** tab.) A route tells Cloudflare where to send visitors: here, to
   `http://dashboard:8090`, the dashboard container's name and port on the private network Docker
   creates between the two containers. On the tunnel's **Routes** tab → **Add route** →
   **Published application**:
   - **Subdomain:** `<subdomain>`
   - **Domain:** `<your-domain>`
   - **Path:** leave empty
   - **Service URL:** `http://dashboard:8090` (if the form has separate fields instead: **Type**
     `HTTP`, **URL** `dashboard:8090`)

   **Add route**. Cloudflare creates the DNS record for `<your-hostname>` itself.

You should see the tunnel in the list. It shows as down until the dashboard starts in step 5 of
the deploy guide. That is expected.

**If the token leaks:** open the tunnel → **Overview** → **Refresh token**, put the new token in
`.env`, and run `docker compose -f compose.cloudflare.yml up -d`
([docs](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/configure-tunnels/remote-tunnel-permissions/)).

## 2. Turn on one-time PIN and create the groups

1. **One-time PIN** ([docs](https://developers.cloudflare.com/cloudflare-one/integrations/identity-providers/one-time-pin/)):
   **Zero Trust** → **Integrations** → **Identity providers** → **Add new identity provider** →
   **One-time PIN**. There is nothing to fill in. A PIN works once and expires after 10 minutes.
2. **Groups** ([docs](https://developers.cloudflare.com/cloudflare-one/access-controls/policies/groups/)):
   **Access controls** → **Policies** → **Rule groups** tab → **Add a group**:
   - Name `Officers`. Under **Include**, selector **Emails**, add every officer's email,
     **including your own**. **Save**.
   - Name `Launchpad`. Same, with the email of every current builder and mentor in Launchpad, the
     club's builder program (they sign in to its portal). No builders yet? Put just your own email
     in it for now. **Save**.

You should see both groups on the **Rule groups** tab.

Your own email must be in `Officers`. Cloudflare sends a PIN only to emails a policy allows;
anyone else sees "A code has been emailed to you", but no code arrives.

### Adding someone later

**Access controls** → **Policies** → **Rule groups** → open the group → add the email → **Save**.
It works at that person's next sign-in. Do this **before** you send them a dashboard or Launchpad
invite: the invite link only opens after they pass this sign-in.

Removing an email stops new sign-ins, but a browser already signed in stays in until its session
ends (up to a month). So also turn the person off in the dashboard (**Officers**, or the Launchpad
**Invites** tab): the dashboard checks its own accounts on every request.

## 3. Protect the whole site with an Access application

([docs](https://developers.cloudflare.com/cloudflare-one/access-controls/applications/http-apps/self-hosted-public-app/))

**Access controls** → **Applications** → **Create new application** → **Self-hosted and
private**:

1. **Application name:** `Officer Dashboard`.
2. **Add public hostname:** Subdomain `<subdomain>`, Domain `<your-domain>`, **Path empty**. An
   empty path covers every page.
3. **Session Duration:** **1 month**, the longest offered. Each browser then asks for a PIN at
   most once a month.
4. **Access policies:** create a new policy. Name `Officers and Launchpad`, Action **Allow**.
   **Include** → selector **Rule group** → `Officers`. Add a second include: **Rule group** →
   `Launchpad`. (Include rules are alternatives: either group gets in.) Leave the policy's session
   duration as the application's.
5. **Login methods:** only **One-time PIN**. Untick any other. Turn on **Apply instant
   authentication**, so people go straight to the email form.
6. Leave **CORS** settings off. **Create** (or **Save**).

Then make the PIN sign-in itself last a month too
([docs](https://developers.cloudflare.com/cloudflare-one/access-controls/access-settings/session-management/)):
**Access controls** → **Access settings** → **Set your global session duration** → **Edit** →
**1 month** → **Save**.

You should see `Officer Dashboard` in the applications list. Officers will type a PIN once a
month, then sign in with Discord.

## 4. Let AI agents through on their own paths

Officers can connect their own AI agents (Claude, ChatGPT, Cursor) to the dashboard at
`https://<your-hostname>/mcp`, using MCP, a standard way for agents to use an app.
They can't type a PIN, and Claude.ai and ChatGPT can't send extra headers either. So a few exact
paths skip Access. They stay safe: the dashboard itself requires an OAuth token there, and
getting one needs an officer to press **Allow** on a page that stays behind Access.

**Create new application** → **Self-hosted and private** again:

1. **Application name:** `Officer Dashboard: agents (bypass)`.
2. **Add public hostname** six times, each with Subdomain `<subdomain>`, Domain `<your-domain>`
   and one of these **Paths** (no slash at the start):
   - `mcp`
   - `oauth/register`
   - `oauth/token`
   - `.well-known/oauth-protected-resource`
   - `.well-known/oauth-protected-resource/mcp`
   - `.well-known/oauth-authorization-server`
3. **Access policies:** a new policy, Action **Bypass**, **Include** → **Everyone**.
4. **Create**.

You should see two applications now. A path that is listed in a more specific application uses
that application's policy instead of the whole-site one
([docs](https://developers.cloudflare.com/cloudflare-one/access-controls/policies/app-paths/)).

**Don't add `oauth` or `oauth/*`.** That would also open `oauth/authorize`, the consent page where
an officer presses **Allow**, which must stay behind the PIN sign-in. Cloudflare does not log
bypassed requests.

**Which paths go where.** Everything not listed here stays behind the PIN sign-in, including the
Discord and Google sign-in callbacks (`/auth/callback`, `/auth/google/callback`,
`/auth/org-google/callback`): they are browser redirects, and the browser is already signed in to
Access. The dashboard receives no webhooks.

| Path | Called by | Access |
| --- | --- | --- |
| the six paths above | AI agents | **Bypass** (this step) |
| `/api/ingest/…`, `/api/agent/…` | the Discord check-in bot, the Luma import, the purchasing agent | **Service token** (step 5) |
| everything else, including `/oauth/authorize`, `/launchpad/…`, `/auth/…` | people's browsers | **PIN sign-in** (step 3) |

## 5. Create a service token for bots

Three club programs call the dashboard automatically: the Discord check-in bot (records Buildspace
check-ins), the Luma import (guest lists from Luma, the events site) and the purchasing agent. They
run on servers and can't type a PIN. They pass Access with a Cloudflare **service token**: two
values they send as headers.

1. **Create the token** ([docs](https://developers.cloudflare.com/cloudflare-one/access-controls/service-credentials/service-tokens/)):
   **Access controls** → **Service credentials** → **Service Tokens** → **Create Service Token**.
   Name it `officer-dashboard services`. **Service Token Duration:** the longest offered; note the
   expiry date. **Generate token**. Copy the **Client ID** and **Client Secret** now: Cloudflare
   shows the secret only once.
2. **Create the application:** **Applications** → **Create new application** → **Self-hosted and
   private**:
   - **Application name:** `Officer Dashboard: services (service token)`.
   - **Add public hostname** twice, Subdomain `<subdomain>`, Domain `<your-domain>`, Paths
     `api/ingest` and `api/agent`. A path covers everything below it.
   - **Access policies:** a new policy named `Service token`, Action **Service Auth** (not
     **Allow**, which would show the bot the sign-in page). **Include** → selector **Service Token** →
     `officer-dashboard services`.
   - In the application's **Additional settings**, turn on **401 Response for Service Auth
     policies**. A wrong token then gets a plain error instead of the sign-in page.
   - **Create**.

You should see three applications now.

**Optional origin assertion verification:** after creating both protected applications, copy
their Application Audience (AUD) values into `dashboard.env`. Set
`CLOUDFLARE_ACCESS_TEAM_DOMAIN=<team>.cloudflareaccess.com`,
`CLOUDFLARE_ACCESS_AUD=<Officer Dashboard browser application AUD>` and
`CLOUDFLARE_ACCESS_SERVICE_AUD=<Officer Dashboard services application AUD>` together.
The service AUD is accepted only for `/api/ingest` and `/api/agent` and their descendants;
all other protected paths require the browser AUD. These are separate from the service
Client ID/Secret below. Leave all three unset until both Access applications work;
partial configuration refuses dashboard startup.

**Two kinds of token.** Each bot needs this Cloudflare service token, to get past Access, and its
own **dashboard token**, so the dashboard lets it in. A director or leadership member creates the
dashboard tokens later, in the dashboard at **Officers → Service tokens**, one per bot
([Integrations](../integrations.md#service-tokens-and-purchasing-agents)). Nothing to do for that
now. Each bot sends all three headers with every request:

```
CF-Access-Client-Id: <Client ID>
CF-Access-Client-Secret: <Client Secret>
Authorization: Bearer <the dashboard service token for that bot>
```

| Bot | Where it lives | What its maintainer sets |
| --- | --- | --- |
| Discord check-in bot | its adapter is [`scripts/buildspace-discord-stub.mjs`](../../scripts/buildspace-discord-stub.mjs) | `CF_ACCESS_CLIENT_ID` and `CF_ACCESS_CLIENT_SECRET` in the bot's environment |
| Luma import | outside this repository | add both headers to its request |
| Purchasing agent | outside this repository | add both headers to every request; treat a 401 from Cloudflare as "Access refused the token" |

The dashboard itself never uses this token, so it does not go in `dashboard.env`. If the secret
leaks: delete the token, create a new one, select it in the `Service token` policy, and update the
bots.

## 6. Turn off bot checks for agents

Agents and bots can't solve Cloudflare's "are you human?" challenges. A challenge shows up in
Claude.ai or ChatGPT as a vague "couldn't connect". These settings are on your domain, not in Zero
Trust: in the Cloudflare dashboard, open your domain first.

1. **Bot Fight Mode: off** ([docs](https://developers.cloudflare.com/bots/get-started/bot-fight-mode/)).
   **Security** → **Settings** → filter **Bot traffic** → **Bot Fight Mode** → **Off**. It
   applies to the whole domain and can't be turned off for some paths only ("You cannot bypass or
   skip Bot Fight Mode using WAF custom rules or Page Rules"). Access still guards every page.
2. **A skip rule for the agent and bot paths**
   ([create a rule](https://developers.cloudflare.com/waf/custom-rules/create-dashboard/),
   [skip options](https://developers.cloudflare.com/waf/custom-rules/skip/options/)):
   **Security** → **Security rules** → **Create rule** → **Custom rules**.
   - **Rule name:** `Agents and services: skip bot checks`.
   - **Edit expression**, and paste this, with your hostname:
     ```
     (http.host eq "<your-hostname>" and (http.request.uri.path eq "/mcp" or starts_with(http.request.uri.path, "/mcp/") or http.request.uri.path in {"/oauth/register" "/oauth/token" "/.well-known/oauth-authorization-server"} or starts_with(http.request.uri.path, "/.well-known/oauth-protected-resource") or starts_with(http.request.uri.path, "/api/ingest/") or starts_with(http.request.uri.path, "/api/agent/")))
     ```
   - **Action:** **Skip**. Tick **Browser Integrity Check** and **Security Level**. If you have
     them: **All Super Bot Fight Mode rules** (paid plans), **All rate limiting rules**, and on paid
     plans **All managed rules** (agent requests carry JSON and file uploads that can trip them).
   - Keep **Log matching requests** on. **Deploy**, then move the rule to the **top** of the list.
3. **Under Attack Mode: off** ([docs](https://developers.cloudflare.com/fundamentals/reference/under-attack-mode/)).
   On the domain's overview page, in **Quick Actions**, check that **Under Attack Mode** is off.
4. If **Block AI bots** or **AI Crawl Control** is on (**Security** → **Settings**), leave it for
   now. After an officer connects Claude.ai or ChatGPT, look in **Security** → **Events**: if it
   blocked their requests to the agent paths, turn it off. The step 10 check runs from your
   computer and can't see a block that only hits their servers.

You should see the skip rule first in the list. Access still checks the service token on
`/api/ingest/` and `/api/agent/`; the rule only turns off the bot checks.

## 7. Never cache the agent paths

([docs](https://developers.cloudflare.com/cache/how-to/cache-rules/create-dashboard/))

Cloudflare doesn't cache these answers by default, and the dashboard marks them `no-store`. This
rule makes sure a broader cache rule can never catch them. In your domain: **Caching** → **Cache
Rules** → **Create rule**:

- **Rule name:** `Agents and services: bypass cache`.
- **Custom filter expression** → **Edit expression**: the same expression as in step 6.
- **Cache eligibility:** **Bypass cache**.
- **Place at:** last. **Deploy**.

You should see the rule at the bottom of the cache rules.

## 8. Turn off Rocket Loader and Email Obfuscation

Both change the page's HTML on the way through. The consent page (`/oauth/authorize`) allows no
outside scripts and has hidden form fields that must arrive unchanged. Add this rule
([docs](https://developers.cloudflare.com/rules/configuration-rules/create-dashboard/)). In your
domain, **Rules** → **Overview** → **Create rule** → **Configuration Rule**:

- **Rule name:** `Consent page: no rewriting`.
- **Edit expression:**
  ```
  (http.host eq "<your-hostname>" and http.request.uri.path eq "/oauth/authorize")
  ```
- Add the settings **Rocket Loader**: off and **Email Obfuscation**: off. **Deploy**.

You should see the configuration rule deployed. The rule is enough on its own. If nothing else on
your domain needs the two features, you may also turn both off for the whole domain: Rocket Loader
under **Speed** → **Settings** → **Content Optimization**
([docs](https://developers.cloudflare.com/speed/optimization/content/rocket-loader/enable/)), and
Email Address Obfuscation under **Security** → **Settings**, filter **Client-side abuse**
([docs](https://developers.cloudflare.com/waf/tools/scrape-shield/email-address-obfuscation/)).

## 9. Create the R2 bucket for backups

Litestream, built into the dashboard image, copies the database to this bucket every second.

1. **Turn on R2** ([docs](https://developers.cloudflare.com/r2/get-started/)): in the Cloudflare
   dashboard, **Storage & databases** → **R2** → **Overview**. The first time, it asks you to add
   the R2 subscription through a checkout. The free tier (10 GB of storage a month) is far more
   than the dashboard needs.
2. **Create the bucket:** **Create bucket**. Name it `cseed-dashboard-db` (any name of lowercase
   letters, numbers and hyphens). Location: automatic. Leave public access off. Save the name.
3. **Save your account ID:** it is shown under **Account Details** on the R2 overview page (or
   press Ctrl+K and search for `Copy account ID`,
   [docs](https://developers.cloudflare.com/fundamentals/account/find-account-and-zone-ids/)).
   Your R2 endpoint is `https://<account ID>.r2.cloudflarestorage.com`.
4. **Create a key for this bucket only** ([docs](https://developers.cloudflare.com/r2/api/tokens/)):
   on the R2 overview, under **Account Details**, **Manage** next to **API Tokens** → **Create
   Account API token**.
   - **Permissions:** **Object Read & Write**.
   - Scope it to **specific buckets only** → your bucket.
   - **Create Account API token**. The next page shows several values. Copy the **Access Key ID**
     and the **Secret Access Key** now: the secret is shown only once. You don't need the others
     (the token value and the endpoints); your endpoint is the one from item 3. If you lose the
     secret, make a new token the same way and delete the old one.

You should have the bucket name, the account ID and the two keys.

Use a key for this bucket only, never an account-wide one: anyone with the key can read or
overwrite the database.

**← [Back to step 2 of the deploy guide](README.md#2-set-up-cloudflare)**

## 10. Check it from outside

**Do this later**, when [step 8 of the deploy guide](README.md#8-check-that-it-works) sends you
here. It needs the dashboard running.

`mcp-check`, built into the dashboard, calls your site the way agents and bots do and reports any
Cloudflare setting that gets in the way. On the server, in `~/officer-dashboard/server/deploy`:

```sh
export CF_ACCESS_CLIENT_ID=<service token Client ID>
read -rs CF_ACCESS_CLIENT_SECRET && export CF_ACCESS_CLIENT_SECRET   # paste the secret, press Enter
docker compose -f compose.cloudflare.yml run --rm --no-deps \
  -e CF_ACCESS_CLIENT_ID -e CF_ACCESS_CLIENT_SECRET --entrypoint /app/dashboard dashboard \
  mcp-check https://<your-hostname>
```

(The `read -rs` line needs the `bash` shell, the default on Raspberry Pi OS.) You should see it
end with:

```
8 passed, 0 warnings, 0 failed
```

It checks that the agent paths get through (step 4), the consent page meets the PIN sign-in
(step 3), and the service token gets through (step 5). Each failure names the step to fix. Without
the two service-token variables it reports `7 passed` and an `INFO` line about the token; that is
fine for a first check.

Then connect an agent as an officer, for example with Claude Code:
`claude mcp add --transport http cseed https://<your-hostname>/mcp`. The browser shows the PIN
sign-in, then the dashboard's sign-in, then **Allow** ([MCP agents](../mcp.md#connecting)).

**← [Back to step 8 of the deploy guide](README.md#8-check-that-it-works)**
