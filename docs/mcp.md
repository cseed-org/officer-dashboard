# MCP agents

Every officer can connect their own AI agent (Claude Code, Claude, Cursor, ChatGPT, or any
MCP client) to the dashboard and have it do real work: plan events, file purchase requests
and receipts, move design cards, log outreach, run finance reports.

There is one endpoint, `https://<dashboard>/mcp`. The agent signs in **as the officer** with
OAuth, so it can do exactly what that officer can do in the UI, nothing more.

## Connecting

```sh
claude mcp add --transport http cseed https://<dashboard>/mcp
```

In Claude, Cursor or ChatGPT, add the same URL as a custom connector / MCP server. The
browser opens the dashboard; sign in with Discord if asked, then choose **Allow**.
Settings → Connected agents shows the URL, the command, and every connected agent with a
**Disconnect** button.

## How it works

```
agent ── POST /mcp (no token) ─────────────────────▶ 401 WWW-Authenticate: resource_metadata=…
agent ── GET /.well-known/oauth-protected-resource ─▶ authorization server = the dashboard
agent ── GET /.well-known/oauth-authorization-server
agent ── POST /oauth/register (RFC 7591, public client) ─▶ client_id
browser ─ GET /oauth/authorize (PKCE S256 required)
            signed out → /login?next=… (Discord) → back here
            consent page: "Allow <agent> to work in the dashboard as you?" → Allow
          ─▶ redirect_uri?code=…&state=…&iss=…
agent ── POST /oauth/token ─▶ access token (1 h) + refresh token (30 days, rotates on use)
agent ── POST /mcp  Authorization: Bearer … ─▶ tools
```

- **Same permissions as the UI.** A tool call becomes an in-process request to the existing
  API route, run through the normal auth middleware with the officer as the principal. Every
  role check, validation rule, lifecycle rule (no self-approval, payout tolerance, …) and
  onboarding gate applies unchanged. `tools/list` hides tools above the officer's role.
- **Audit.** Every change is recorded as the officer, with the agent's grant in
  `audit_log.actor_agent_id`; the audit log shows "Priya Shah via Claude Code", and its
  "who" filter keeps an officer's agent actions under the officer.
  Connecting is logged as `agent.connected`, disconnecting as `agent.disconnected`.
- **Revocation.** The user is reloaded on every call: deactivation stops the agent at once,
  and also revokes all of the officer's grants (with their sessions;
  `agent.revoked_with_user` with the count). The operator CLI's leadership provisioning and
  owner bootstrap sign the person out the same way and note `agentsDisconnected` in their
  audit entry. Role changes apply on the next call. Officers disconnect agents from Settings.
- **Storage.** Only SHA-256 hashes of codes and tokens are stored (`oauth_clients`,
  `oauth_codes`, `oauth_grants`). Codes are single use (burned even by a failed attempt) and
  live 2 minutes. A redeemed code presented again revokes the grant it produced
  (`agent.revoked_code_reuse`, RFC 6749 §4.1.2); used codes are kept an hour past expiry for
  that. `redirect_uri` at the token endpoint is optional (OAuth 2.1; PKCE binds the code)
  but must match when sent.
- **Refresh-token reuse.** Presenting a refresh token that was already rotated away means a
  copy leaked: the grant is revoked at once and `agent.revoked_token_reuse` is audited
  (RFC 9700). Within 10 seconds of the rotation it is instead the same client refreshing
  twice at once (two windows sharing a sign-in): that request gets `invalid_grant` and the
  agent stays connected. Reconnecting the same client replaces its previous grant.
- **Sign-in round trip.** A signed-out officer goes to `/login?next=<authorize URL>`, which
  rides in a cookie; an authorize URL over 3,000 bytes gets a "sign in first, then connect
  again" page instead of being dropped silently.
- **No open redirect.** Any visitor can register a client, so an invalid authorize request
  (bad `response_type`, missing PKCE, foreign `resource`, overlong `state`) shows an error
  page instead of redirecting. Only the officer's own Allow or Cancel goes back to the client.
- **Arguments are checked.** Unknown or missing arguments are refused with the list of valid
  ones, so request fields put next to `body` instead of inside it can never "succeed" as a
  no-op. Ids `.`/`..` and API redirects are errors.
- **Redirect URIs.** https, http on a loopback host (any port, RFC 8252), or an app scheme
  such as `cursor://`. Never `javascript:`, `data:` or `file:`. A request with an
  unregistered redirect URI gets an error page, never a redirect.
- **Transport.** Streamable HTTP, stateless: each POST carries a JSON-RPC message or batch
  (at most 20 messages) and gets a JSON reply. An unsupported `MCP-Protocol-Version` header is
  a 400. `/mcp/` works too. No SSE stream, no sessions (`GET /mcp` is 405). CORS is open on the
  bearer-only endpoints (no cookies are accepted there).
- **Rate limits.** Each agent connection (one OAuth grant; a token refresh keeps it) may make
  120 calls a minute to `/mcp`, refilled continuously; every message in a batch counts. Over
  that, `/mcp` answers 429 with `Retry-After` (seconds) and a JSON-RPC error (code `-32000`,
  one per request in the message, ids echoed) whose message says when to try again; nothing in
  the message runs. One agent's budget never touches another's, the same officer's or not.
  Requests without a valid token get the usual 401 challenge before any budget is counted.
  POSTs to `/oauth/*` share the sign-in budget per IP; upload and import tools also draw on
  the same per-IP upload budget as the API routes.
- **Housekeeping.** The hourly janitor (its own transaction, apart from the session purge)
  drops codes an hour past expiry and clients that never got a grant within a week.

## Tools

85 tools, one per API endpoint: context (`whoami`, `search`, `lookups`,
`calendar_feed`…), events, purchase requests (including `attach_receipt` with base64
files, 10 MB max), finance reports and budgets, the design board, external contacts, members,
officers, Buildspace Launchpad (cohorts, builder and mentor invites, mentor roster and
availability, mentor auto-assign preview and apply, archive/restore/delete of projects and
people, activity with its portal visibility) and the officer's own profile. Deletes and
period closes are marked `destructiveHint` so clients ask before running them. A tool
whose endpoint needs a higher role (director-only cohort changes, account disable/enable,
mentor invites, deletes…) is left out of `tools/list` for an officer and refused if called.

Launchpad invites through an agent go **by email only**: `method` must be `email`, and no
response hands an agent a join link (`links` is empty; a resend returns `link: null`). An
invite link is a sign-up credential. When invite email is not configured, the invite tools
answer 409 and the officer invites from the dashboard. The dashboard itself is unchanged.

Not exposed: sign-in and onboarding, Google Calendar connection, file downloads,
`/api/admin/*` (owners, roles, accounts, service tokens: an agent never manages access
or credentials), officer invites (an invite link is a sign-up credential), the
builder/mentor portal (`/api/portal/*`: portal accounts have their own table, session and
cookie; a bearer token never authenticates there, and a portal session never reaches `/mcp`
or the consent page, which needs an officer sign-in), the dev-only `/auth/dev` forms, the
service-token APIs and Cloudflare sync. The generator and the server both refuse a tool
outside `/api/` or under `/api/portal`.

Tool names, descriptions and the endpoint list live in `scripts/gen-mcp-tools.mjs`; the
input schemas come from `src/core/contracts.ts`. After changing either, regenerate:

```sh
npm run mcp-tools        # writes server/internal/api/mcp_tools.json (embedded in the binary)
```

CI runs `node scripts/gen-mcp-tools.mjs --check`, and `TestMCPToolsMatchRoutes` fails if a
tool points at a route that does not exist.

## Deployment

Behind Cloudflare Tunnel / Access or another proxy, let `/mcp`, `/oauth/*` and
`/.well-known/oauth-*` through without a browser challenge, and set `APP_URL` to the public
origin (the metadata URLs are built from it).

## Trying it locally

```sh
cd server && make dev                       # seeded server on :8090 with dev sign-in
claude mcp add --transport http cseed-local http://localhost:8090/mcp
```

The consent flow uses the normal login page, so the dev sign-in panel works there too.

## Tests and evidence

- `server/internal/api/mcp_test.go` contains the Go tests: OAuth, transport, permissions, revocation, purge and the security-review fixes.
- `scripts/contract-check.mjs` runs the OAuth → MCP → Connected agents → audit flow.
- `web/tests/mcp-agents.spec.ts` runs everything end to end against a live server: the browser connect flow, every tool for every role, protocol edge cases and the real clients. It also runs each Go test.
- With `MCP_SHOTS=1`, every test saves a screenshot to [docs/evidence/mcp-agents](evidence/mcp-agents/README.md).
