# MCP agents: test evidence

Every MCP test leaves a screenshot here. They are produced by `web/tests/mcp-agents.spec.ts`
against a live, freshly seeded server (see [docs/mcp.md](../../mcp.md)):

```sh
cd server && make dev                      # or any dev build with DEV_AUTH=1, fresh seed
cd web && MCP_SHOTS=1 WEB_ORIGIN=http://localhost:8090 \
  npx playwright test tests/mcp-agents.spec.ts --project=desktop --project=phone
# optional real clients:
#   MCP_SDK_E2E=1     (after `npm i --no-save @modelcontextprotocol/sdk` at the root)
#   MCP_CLAUDE_CLI=1  (the `claude` CLI on PATH)
```

- **UI tests** capture the real page on phone and desktop, in light and dark.
- **Protocol tests** render every request, its answer and each check as an evidence card.
- **Go tests** in `server/internal/api/mcp_test.go` run with a fixed clock and a controlled client IP. That covers expiry, purge and rate limits, which a live server can't show. Each one is run and its `go test -v` output captured, together with what it asserts.

## Connect flow in the browser

Each step has four screenshots: `-{phone,desktop}-{light,dark}`.

| Step | Screenshot |
| --- | --- |
| A signed-out officer opening the authorize URL is sent to sign-in. The authorize URL is kept as `next`. | `flow-1-signin-*.png` |
| Consent page, showing the agent's name and the officer | `flow-2-consent-*.png` |
| Allow: the agent's local listener receives the code and state | `flow-3-allowed-*.png` |
| Cancel: the agent receives `access_denied` | `flow-4-cancelled-*.png` |
| Invalid request (no PKCE): an error page, never a redirect | `flow-5-error-page-*.png` |

## Settings and audit

| Test | Screenshot |
| --- | --- |
| Settings → Connected agents: URL and command (copy works), list including a long name | `settings-agents-*.png` |
| Disconnect: cancelling the confirm keeps the agent, accepting removes it | `settings-after-disconnect-*.png` |
| Audit log shows "Name via Agent" | `audit-via-agent.png` |
| Audit log "AI agents" filter | `audit-agents-filter.png` |

## Protocol, live

| Test | Screenshot |
| --- | --- |
| Discovery: the 401 challenge, RFC 9728 and RFC 8414 metadata, cookies refused, CORS | `discovery.png` |
| Registration: redirect URIs are validated, and client names lose invisible and bidi characters | `registration.png` |
| Authorize errors never redirect (open-redirect fix): PKCE, `resource`, `state`, cross-site Allow | `authorize-errors.png` |
| Tokens: PKCE, single-use codes, client binding, rotation, and refresh reuse revokes the connection | `tokens.png` |
| Acting as the officer: role-filtered tools, officer can't approve, director approves, no self-approval | `acting-as-officer.png` |
| Every tool, live, as leadership, director and officer | `all-tools-{leadership,director,officer}.png` |
| Wrong or missing arguments, `..` ids, oversized and wrong-type files | `arguments.png` |
| Transport: parse errors, batches (max 20), notifications, protocol header, `/mcp/` | `transport.png` |
| 120 parallel calls on one connection, then 429 (per-agent rate limit) | `concurrency.png` |
| Revocation: disconnect, and deactivation by a director | `revocation.png` |

## Real clients

| Test | Screenshot |
| --- | --- |
| Official MCP TypeScript SDK, end to end (discovery, registration, PKCE, tools) | `sdk-e2e-{officer,director}.png` |
| Claude Code CLI: `claude mcp add` / `claude mcp list` report "Needs authentication" | `claude-code-cli.png` |

## Go tests

One screenshot per test: `go-<TestName>.png`.

| Test | Screenshot |
| --- | --- |
| Discovery and unauthenticated calls | `go-TestMCPDiscoveryAndUnauthenticated.png` |
| Redirect URI validation | `go-TestOAuthRegisterValidatesRedirects.png` |
| Authorize flow and consent page (escaping, CSP, referrer policy) | `go-TestOAuthAuthorizeFlowAndConsent.png` |
| Code exchange, code expiry, access expiry, refresh | `go-TestOAuthTokenExchangeAndRefresh.png` |
| Tools act as the officer; permission parity | `go-TestMCPToolsActAsTheOfficer.png` |
| Base64 upload tool | `go-TestMCPUploadTool.png` |
| Connected agents API and revocation | `go-TestConnectedAgentsAndRevocation.png` |
| Every tool maps to a real route; nothing exposes admin, auth or ingest | `go-TestMCPToolsMatchRoutes.png` |
| Argument checks and size limits | `go-TestMCPArgumentChecks.png` |
| Transport details | `go-TestMCPTransportDetails.png` |
| OAuth hardening (`resource`, `state`, name cleaning) | `go-TestOAuthHardening.png` |
| Reconnecting replaces a grant, and expired grants drop off the list | `go-TestConnectedAgentsListStaysClean.png` |
| Hourly purge of codes and unused clients | `go-TestPurgeOAuth.png` |
| Security review findings (open redirect, batch cap, upload rate limit, ids, invites) | `go-TestMCPReviewFixes.png` |
| Refresh-token reuse revokes the grant | `go-TestOAuthRefreshReuseRevokes.png` |
