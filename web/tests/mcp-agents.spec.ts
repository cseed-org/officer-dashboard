import { spawnSync } from 'node:child_process';
import { createServer, type Server } from 'node:http';
import { createHash, randomBytes } from 'node:crypto';
import { mkdirSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { test, expect, request as pwRequest, type APIRequestContext, type Page } from '@playwright/test';
import { devSignIn, overflow, settle, useTheme, watchErrors, type Theme } from './helpers';

/**
 * MCP agents (docs/mcp.md), end to end against a live server: the OAuth connect flow in the
 * browser, every tool for every role, the transport and OAuth edge cases, the security
 * review's findings, and the Settings and audit UI. Every test leaves a screenshot:
 * UI tests capture the page; protocol tests render each request, its answer and the checks
 * as an evidence card; the Go tests (which need a fake clock: expiry, purge, rate limits)
 * are run and their `go test -v` output captured.
 *
 * Needs the Go server with DEV_AUTH=1 on a freshly seeded database (cd server && make dev),
 * reached directly (WEB_ORIGIN=http://127.0.0.1:8090) or through the dev server's proxy.
 * MCP_SHOTS=1 writes docs/evidence/mcp-agents/ (see its README).
 */
const SHOTS = !!process.env.MCP_SHOTS;
/** The repository root (run from web/, or point MCP_REPO_ROOT at it). */
const ROOT = process.env.MCP_REPO_ROOT ?? resolve(process.cwd(), '..');
const shotDir = `${ROOT}/docs/evidence/mcp-agents`;
if (SHOTS) mkdirSync(shotDir, { recursive: true });
const REDIRECT = 'http://127.0.0.1:53682/callback';

// ───────────────────────────── helpers ─────────────────────────────

/** Child processes print plain text (no colours) so the output can be matched and shown. */
const plainEnv = { ...process.env, NO_COLOR: '1', FORCE_COLOR: '0' };
const stripAnsi = (s: string) => s.replace(/\u001b\[[0-9;]*m/g, '');

type Step = { label: string; request?: string; status?: number | string; body?: unknown; ok?: boolean };
type Check = { text: string; ok: boolean };

/** Collects what a protocol test did, then renders it as one screenshot. */
class Evidence {
  steps: Step[] = [];
  checks: Check[] = [];
  /** Plain bullets: what a Go test asserts (its failure messages, cleaned up). */
  notes: string[] = [];
  constructor(readonly name: string, readonly title: string, readonly about: string) {}
  step(s: Step) {
    this.steps.push(s);
  }
  check(ok: boolean, text: string) {
    this.checks.push({ text, ok });
    expect(ok, text).toBe(true);
  }
  async render(page: Page) {
    if (!SHOTS) return;
    const esc = (v: unknown) => String(v).replace(/&/g, '&amp;').replace(/</g, '&lt;');
    const short = (b: unknown) => {
      if (b === undefined) return '';
      const s = typeof b === 'string' ? b : JSON.stringify(b, null, 1);
      return s.length > 1800 ? s.slice(0, 1800) + ' …' : s;
    };
    const passed = this.checks.every((c) => c.ok);
    const html = `<!doctype html><html><head><meta charset="utf-8"><style>
      body{margin:0;background:#f6f6f3;font:14px/1.45 system-ui,-apple-system,sans-serif;color:#16181d}
      main{max-width:1100px;margin:24px auto;background:#fff;border:1px solid #e3e3de;border-radius:14px;padding:24px 28px}
      .top{display:flex;justify-content:space-between;align-items:baseline;gap:16px}
      .eyebrow{font-size:11px;letter-spacing:.08em;text-transform:uppercase;color:#5d6370;margin:0}
      h1{font-size:20px;margin:4px 0 6px}
      .about{color:#5d6370;margin:0 0 16px}
      .badge{font-weight:700;border-radius:999px;padding:4px 12px;font-size:13px;white-space:nowrap}
      .pass{background:#e3f4e8;color:#1f7a4d}.fail{background:#fde8e8;color:#b42318}
      .step{border-top:1px solid #eee;padding:10px 0;display:grid;grid-template-columns:240px 1fr;gap:12px}
      .label{font-weight:600}
      code,pre{font-family:ui-monospace,Menlo,monospace;font-size:12px}
      .req{color:#3b4a6b;word-break:break-all}
      .st{display:inline-block;min-width:34px;text-align:center;border-radius:6px;padding:1px 6px;margin-right:6px;font-weight:700;font-size:12px;background:#eef0f4}
      .st.good{background:#e3f4e8;color:#1f7a4d}.st.bad{background:#fff1e0;color:#9a4d00}
      pre{margin:6px 0 0;background:#f7f7f5;border-radius:8px;padding:8px 10px;white-space:pre-wrap;word-break:break-all;max-height:440px;overflow:hidden}
      ul{margin:14px 0 0;padding:0;list-style:none;border-top:2px solid #16181d}
      li{padding:6px 0;border-bottom:1px solid #f0f0ec}
      ol.notes{margin:6px 0 0;padding-left:22px;color:#3b4250}ol.notes li{border:0;padding:2px 0}
      .ok::before{content:"✓ ";color:#1f7a4d;font-weight:700}.no::before{content:"✗ ";color:#b42318;font-weight:700}
    </style></head><body><main>
      <div class="top"><div><p class="eyebrow">MCP agents · ${esc(this.name)}</p><h1>${esc(this.title)}</h1></div>
      <span class="badge ${passed ? 'pass' : 'fail'}">${passed ? 'PASS' : 'FAIL'} · ${this.checks.filter((c) => c.ok).length}/${this.checks.length} checks</span></div>
      <p class="about">${esc(this.about)}</p>
      ${this.steps
        .map((s) => {
          const good = s.ok ?? (typeof s.status === 'number' ? s.status < 400 : true);
          return `<div class="step"><div class="label">${esc(s.label)}</div><div>${s.request ? `<div class="req">${s.status !== undefined ? `<span class="st ${good ? 'good' : 'bad'}">${esc(s.status)}</span>` : ''}<code>${esc(s.request)}</code></div>` : ''}${s.body !== undefined ? `<pre>${esc(short(s.body))}</pre>` : ''}</div></div>`;
        })
        .join('')}
      ${this.notes.length ? `<p class="eyebrow" style="margin-top:16px">What this test asserts (each line is a way it would fail)</p><ol class="notes">${this.notes.map((n) => `<li>${esc(n)}</li>`).join('')}</ol>` : ''}
      <ul>${this.checks.map((c) => `<li class="${c.ok ? 'ok' : 'no'}">${esc(c.text)}</li>`).join('')}</ul>
    </main></body></html>`;
    await page.setViewportSize({ width: 1200, height: 800 });
    await page.setContent(html);
    await page.screenshot({ path: `${shotDir}/${this.name}.png`, fullPage: true });
  }
}

/** Protocol tests render cards and need no device sizes: run them once, on desktop. */
const desktopOnly = () => test.skip(test.info().project.name !== 'desktop', 'protocol evidence runs once, on desktop');

/** An API client signed in as a role (its own cookie jar). */
async function session(baseURL: string, as: string): Promise<APIRequestContext> {
  const ctx = await pwRequest.newContext({ baseURL });
  const form: Record<string, string> = { as, next: '/' };
  if (process.env.E2E_DEV_AUTH_KEY) form.key = process.env.E2E_DEV_AUTH_KEY;
  const r = await ctx.post('/auth/dev', { form, maxRedirects: 0 });
  expect([302, 303]).toContain(r.status());
  return ctx;
}

const pkce = () => {
  const verifier = randomBytes(32).toString('base64url');
  return { verifier, challenge: createHash('sha256').update(verifier).digest('base64url') };
};

async function register(api: APIRequestContext, name: string, uris = [REDIRECT]) {
  const r = await api.post('/oauth/register', { data: { client_name: name, redirect_uris: uris } });
  return { status: r.status(), json: await r.json() };
}

/** The whole OAuth flow as `as`, without a browser; returns the tokens. */
async function connectAgent(baseURL: string, as: string, name: string, ev?: Evidence) {
  const browser = await session(baseURL, as);
  const reg = await register(browser, name);
  const { verifier, challenge } = pkce();
  const allow = await browser.post('/oauth/authorize', {
    form: { client_id: reg.json.client_id, redirect_uri: REDIRECT, response_type: 'code', code_challenge: challenge, code_challenge_method: 'S256', state: 'st', decision: 'allow' },
    maxRedirects: 0,
  });
  const code = new URL(allow.headers()['location'] ?? 'http://missing/').searchParams.get('code')!;
  const tok = await browser.post('/oauth/token', { form: { grant_type: 'authorization_code', code, code_verifier: verifier, client_id: reg.json.client_id } });
  const tokens = await tok.json();
  ev?.step({ label: `connect ${name} as ${as}`, request: 'POST /oauth/register → /oauth/authorize (Allow) → /oauth/token', status: tok.status(), body: { client_id: reg.json.client_id, token_type: tokens.token_type, expires_in: tokens.expires_in } });
  return { browser, clientId: reg.json.client_id as string, access: tokens.access_token as string, refresh: tokens.refresh_token as string };
}

/** JSON-RPC to /mcp with a bearer token. */
async function rpc(api: APIRequestContext, token: string, body: unknown, headers: Record<string, string> = {}) {
  const r = await api.post('/mcp', { headers: { authorization: `Bearer ${token}`, 'content-type': 'application/json', ...headers }, data: typeof body === 'string' ? body : JSON.stringify(body) });
  const text = await r.text();
  let json: any = null;
  try {
    json = JSON.parse(text);
  } catch {}
  return { status: r.status(), text, json, headers: r.headers() };
}

/** tools/call → { isError, text, json }. */
async function tool(api: APIRequestContext, token: string, name: string, args: Record<string, unknown> = {}) {
  const r = await rpc(api, token, { jsonrpc: '2.0', id: 1, method: 'tools/call', params: { name, arguments: args } });
  if (r.json?.error) return { isError: true, text: `JSON-RPC ${r.json.error.code}: ${r.json.error.message}`, json: null as any };
  const text: string = r.json.result.content[0].text;
  let json: any = null;
  try {
    json = JSON.parse(text);
  } catch {}
  return { isError: !!r.json.result.isError, text, json };
}

// ───────────────────────────── UI: connect flow and settings ─────────────────────────────

/**
 * The agent's loopback listener, as Claude Code runs one: the browser lands here after
 * Allow or Cancel, so the screenshot shows what the agent received.
 */
let callback: Server | undefined;
test.beforeAll(async () => {
  callback = createServer((req, res) => {
    const u = new URL(req.url ?? '/', REDIRECT);
    const code = u.searchParams.get('code');
    const got = code ? `code ${code.slice(0, 6)}… (state ${u.searchParams.get('state')})` : `error ${u.searchParams.get('error')}`;
    // Echoed into HTML: escape it, as any real callback page must.
    const safe = got.replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`);
    res.writeHead(200, { 'content-type': 'text/html; charset=utf-8', 'content-security-policy': "default-src 'none'; style-src 'unsafe-inline'" });
    res.end(`<!doctype html><body style="font:16px system-ui;padding:2rem"><p style="color:#666">Agent's local callback (127.0.0.1:53682)</p><h1>Connected?</h1><p>The agent received: <strong data-callback>${safe}</strong></p></body>`);
  });
  await new Promise<void>((ok) => callback!.listen(53682, '127.0.0.1', ok));
});
test.afterAll(() => new Promise<void>((ok) => (callback ? callback.close(() => ok()) : ok())));

for (const theme of ['light', 'dark'] as Theme[]) {
  test(`connect flow in the browser: sign-in, consent, Allow, Cancel (${theme})`, async ({ page, baseURL }) => {
    test.skip(test.info().project.name === 'tablet', 'phone and desktop evidence');
    const device = test.info().project.name;
    const errors = watchErrors(page);
    await useTheme(page, theme);
    // The consent page follows the system scheme; the app follows its saved theme.
    await page.emulateMedia({ colorScheme: theme });
    const anon = await pwRequest.newContext({ baseURL });
    const reg = await register(anon, 'Claude Code');
    const { challenge } = pkce();
    const authorize = '/oauth/authorize?' + new URLSearchParams({ response_type: 'code', client_id: reg.json.client_id, redirect_uri: REDIRECT, code_challenge: challenge, code_challenge_method: 'S256', state: 'abc' });

    // 1. Signed out: the dashboard asks for sign-in, keeping the authorize URL as `next`.
    await page.goto(authorize);
    await expect(page).toHaveURL(/\/login\?next=%2Foauth%2Fauthorize/);
    if (SHOTS) await page.screenshot({ path: `${shotDir}/flow-1-signin-${device}-${theme}.png` });

    // 2. Signed in: the consent page names the agent and the officer.
    await devSignIn(page, { as: 'officer' }, authorize);
    await expect(page.getByRole('heading', { name: /Allow Claude Code to work in the dashboard as you/ })).toBeVisible();
    await expect(page.getByText('signed in as officer')).toBeVisible();
    expect(await overflow(page)).toEqual([]);
    if (SHOTS) await page.screenshot({ path: `${shotDir}/flow-2-consent-${device}-${theme}.png`, fullPage: true });

    // 3. Allow: the browser goes back to the agent with a code.
    await page.getByRole('button', { name: 'Allow' }).click();
    await expect(page.locator('[data-callback]')).toContainText('code ');
    await expect(page.locator('[data-callback]')).toContainText('state abc');
    if (SHOTS) await page.screenshot({ path: `${shotDir}/flow-3-allowed-${device}-${theme}.png` });

    // 4. Cancel: the agent hears access_denied.
    await page.goto(authorize);
    await page.getByRole('button', { name: 'Cancel' }).click();
    await expect(page.locator('[data-callback]')).toContainText('error access_denied');
    if (SHOTS) await page.screenshot({ path: `${shotDir}/flow-4-cancelled-${device}-${theme}.png` });

    // 5. A bad request (no PKCE) is an error page, never a redirect.
    await page.goto(`/oauth/authorize?client_id=${reg.json.client_id}&response_type=code`);
    await expect(page.getByRole('heading', { name: 'Cannot connect this app' })).toBeVisible();
    await expect(page.getByText(/S256/)).toBeVisible();
    if (SHOTS) await page.screenshot({ path: `${shotDir}/flow-5-error-page-${device}-${theme}.png` });
    expect(errors.filter((e) => !/oauth\/authorize/.test(e))).toEqual([]);
  });

  test(`Settings → Connected agents: copy, list, disconnect, cancel (${theme})`, async ({ page, baseURL, context }) => {
    test.skip(test.info().project.name === 'tablet', 'phone and desktop evidence');
    const device = test.info().project.name;
    const errors = watchErrors(page);
    await useTheme(page, theme);
    await page.emulateMedia({ colorScheme: theme });
    await context.grantPermissions(['clipboard-read', 'clipboard-write']);
    const stamp = Date.now();
    await connectAgent(baseURL!, 'officer', `Claude Code ${stamp}`);
    await connectAgent(baseURL!, 'officer', `A long agent name that wraps on a phone ${stamp}`);
    await devSignIn(page, { as: 'officer' }, '/settings');
    await settle(page);
    const panel = page.locator('[data-agents-connect]').locator('xpath=ancestor::section[1]');
    await panel.scrollIntoViewIfNeeded();
    await expect(page.locator('[data-mcp-url]')).toHaveText(new URL('/mcp', page.url()).href);
    const rows = page.locator('[data-agents-list] li');
    await expect(rows.filter({ hasText: String(stamp) })).toHaveCount(2);
    expect(await overflow(page)).toEqual([]);
    if (SHOTS) await panel.screenshot({ path: `${shotDir}/settings-agents-${device}-${theme}.png` });

    // Copy the URL.
    await page.locator('[data-agents-connect] button').first().click();
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(new URL('/mcp', page.url()).href);
    await expect(page.locator('[data-agents-connect] button').first()).toHaveText('copied ✓');

    // Cancelling the confirm keeps the agent; accepting removes it.
    const target = rows.filter({ hasText: `Claude Code ${stamp}` });
    page.once('dialog', (d) => d.dismiss());
    await target.getByRole('button', { name: 'disconnect' }).click();
    await expect(target).toHaveCount(1);
    page.once('dialog', (d) => d.accept());
    await target.getByRole('button', { name: 'disconnect' }).click();
    await expect(target).toHaveCount(0);
    if (SHOTS) await panel.screenshot({ path: `${shotDir}/settings-after-disconnect-${device}-${theme}.png` });
    expect(errors).toEqual([]);
  });
}

test('audit log: agent actions read "Name via Agent", the who filter keeps them, and the AI agents filter', async ({ page, baseURL }) => {
  desktopOnly();
  const errors = watchErrors(page);
  const a = await connectAgent(baseURL!, 'director', 'Audit Agent');
  const created = await tool(a.browser, a.access, 'create_contact', { body: { name: `Audit contact ${Date.now()}`, type: 'speaker' } });
  expect(created.isError, created.text).toBe(false);
  const who = await tool(a.browser, a.access, 'whoami');
  await devSignIn(page, { as: 'director' }, '/officers?tab=audit');
  await settle(page);
  const row = page.locator('.audit-row').filter({ hasText: 'via Audit Agent' }).filter({ hasText: 'created contact' });
  await expect(row.first()).toBeVisible();
  if (SHOTS) await page.screenshot({ path: `${shotDir}/audit-via-agent.png` });
  // The "who" filter groups an officer with their agents: picking the director keeps the row.
  const options = await page.locator('#audit-actor option').allTextContents();
  expect(options.some((o) => / via /.test(o)), options.join(' | ')).toBe(false);
  await page.locator('#audit-actor').selectOption(who.json.user.name);
  await expect(row.first()).toBeVisible();
  await page.locator('#audit-actor').selectOption('');
  await page.locator('#audit-type').selectOption('agent');
  await expect(page.locator('.audit-row').first()).toContainText(/connected agent|disconnected agent/);
  const texts = await page.locator('.audit-row').allTextContents();
  expect(texts.every((t) => /agent/.test(t))).toBe(true);
  if (SHOTS) await page.screenshot({ path: `${shotDir}/audit-agents-filter.png` });
  expect(errors).toEqual([]);
});

// ───────────────────────────── protocol: discovery, OAuth, MCP ─────────────────────────────

test('discovery: 401 challenge, protected-resource and authorization-server metadata', async ({ page, baseURL }) => {
  desktopOnly();
  const ev = new Evidence('discovery', 'An agent finds how to sign in from a bare /mcp URL', 'What Claude Code, claude.ai or Cursor do first: call /mcp, follow the 401 to the metadata, find the endpoints.');
  const api = await pwRequest.newContext({ baseURL });
  try {
    const r = await api.post('/mcp', { data: { jsonrpc: '2.0', id: 1, method: 'initialize' } });
    const www = r.headers()['www-authenticate'] ?? '';
    ev.step({ label: 'no token', request: 'POST /mcp', status: r.status(), body: `WWW-Authenticate: ${www}` });
    ev.check(r.status() === 401, '/mcp without a token answers 401');
    ev.check(/resource_metadata=".*\/\.well-known\/oauth-protected-resource"/.test(www), 'the challenge points at the protected-resource metadata');
    const pr = await (await api.get('/.well-known/oauth-protected-resource')).json();
    ev.step({ label: 'protected resource (RFC 9728)', request: 'GET /.well-known/oauth-protected-resource', status: 200, body: pr });
    ev.check(pr.resource.endsWith('/mcp') && pr.authorization_servers.length === 1, 'names /mcp and this dashboard as its authorization server');
    const as = await (await api.get('/.well-known/oauth-authorization-server')).json();
    ev.step({ label: 'authorization server (RFC 8414)', request: 'GET /.well-known/oauth-authorization-server', status: 200, body: as });
    ev.check(as.code_challenge_methods_supported.join() === 'S256', 'PKCE S256 only');
    ev.check(as.token_endpoint_auth_methods_supported.join() === 'none' && !!as.registration_endpoint, 'public clients with dynamic registration');
    const cookie = await session(baseURL!, 'officer');
    const c = await cookie.post('/mcp', { data: { jsonrpc: '2.0', id: 1, method: 'ping' } });
    ev.step({ label: 'signed-in browser cookie', request: 'POST /mcp (cookie, no bearer)', status: c.status() });
    ev.check(c.status() === 401, 'a dashboard session cookie is not an MCP credential');
    const pre = await api.fetch('/mcp', { method: 'OPTIONS' });
    ev.step({ label: 'CORS preflight', request: 'OPTIONS /mcp', status: pre.status(), body: `Access-Control-Allow-Origin: ${pre.headers()['access-control-allow-origin']}` });
    ev.check(pre.status() === 204, 'browser-based clients can call it');
  } finally {
    await ev.render(page);
  }
});

test('registration: redirect URIs are validated; names are cleaned', async ({ page, baseURL }) => {
  desktopOnly();
  const ev = new Evidence('registration', 'Dynamic client registration accepts only safe redirect URIs', 'Anyone can register a client (RFC 7591). Script-capable schemes and non-loopback http are refused; names lose invisible characters.');
  const api = await pwRequest.newContext({ baseURL });
  try {
    for (const bad of ['javascript:alert(1)', 'JAVASCRIPT:alert(1)', 'data:text/html,x', 'http://evil.example/cb', 'https://ok.example/cb#frag', 'https:///nohost']) {
      const r = await register(api, 'x', [bad]);
      ev.step({ label: 'refused', request: bad, status: r.status, body: r.json.error });
      ev.check(r.status === 400, `refuses ${bad}`);
    }
    for (const good of ['https://claude.ai/api/mcp/auth_callback', 'http://localhost:6274/oauth/callback', 'http://[::1]:3000/cb', 'cursor://anysphere.cursor-retrieval/oauth/callback']) {
      const r = await register(api, 'ok', [good]);
      ev.step({ label: 'accepted', request: good, status: r.status });
      ev.check(r.status === 201, `accepts ${good}`);
    }
    const spoof = await register(api, 'Claude‮ edoC\u0000\n  Code​');
    ev.step({ label: 'name with RLO, NUL, newline, ZWSP', request: JSON.stringify('Claude‮ edoC\u0000\n  Code​'), status: spoof.status, body: { client_name: spoof.json.client_name } });
    ev.check(spoof.json.client_name === 'Claude edoC Code', 'stored name is visible characters only');
  } finally {
    await ev.render(page);
  }
});

test('authorize: open redirect closed, PKCE and resource enforced', async ({ page, baseURL }) => {
  desktopOnly();
  const ev = new Evidence('authorize-errors', 'Invalid authorize requests never redirect', 'Security review finding: any visitor can register https://evil.example and use the dashboard as an open redirect. Now every invalid request is an error page on the dashboard.');
  const anon = await pwRequest.newContext({ baseURL });
  const officer = await session(baseURL!, 'officer');
  try {
    const evil = await register(anon, 'x', ['https://evil.example/landing']);
    const { challenge } = pkce();
    const base = { client_id: evil.json.client_id, redirect_uri: 'https://evil.example/landing', response_type: 'code', code_challenge: challenge, code_challenge_method: 'S256' };
    const cases: [string, Record<string, string>, APIRequestContext][] = [
      ['response_type=token (signed out)', { ...base, response_type: 'token' }, anon],
      ['no PKCE', { ...base, code_challenge: '' }, officer],
      ['PKCE plain', { ...base, code_challenge_method: 'plain' }, officer],
      ['foreign resource (RFC 8707)', { ...base, resource: 'https://other.example/mcp' }, officer],
      ['state over 1 KB', { ...base, state: 's'.repeat(1025) }, officer],
      ['unregistered redirect_uri', { ...base, redirect_uri: 'https://elsewhere.example/cb' }, officer],
    ];
    for (const [label, q, who] of cases) {
      const r = await who.get('/oauth/authorize?' + new URLSearchParams(q), { maxRedirects: 0 });
      const loc = r.headers()['location'] ?? '';
      ev.step({ label, request: `GET /oauth/authorize?${label}`, status: r.status(), body: loc ? `Location: ${loc}` : (await r.text()).match(/<p>(.*?)<\/p>/s)?.[1] });
      ev.check(r.status() >= 400 && !loc, `${label}: error page, no redirect`);
    }
    const ok = await officer.get('/oauth/authorize?' + new URLSearchParams({ ...base, resource: new URL('/mcp', baseURL).href }), { maxRedirects: 0 });
    ev.step({ label: 'valid request, own resource', request: 'GET /oauth/authorize', status: ok.status() });
    ev.check(ok.status() === 200, 'a valid request shows the consent page');
    const xs = await officer.post('/oauth/authorize', { form: { ...base, decision: 'allow' }, headers: { origin: 'https://evil.example' }, maxRedirects: 0 });
    ev.step({ label: 'cross-site Allow', request: 'POST /oauth/authorize (Origin: evil.example)', status: xs.status() });
    ev.check(xs.status() === 403, 'a cross-site form post cannot authorize');
  } finally {
    await ev.render(page);
  }
});

test('tokens: PKCE, single-use codes, refresh rotation, reuse revokes', async ({ page, baseURL }) => {
  desktopOnly();
  const ev = new Evidence('tokens', 'Code exchange and refresh', 'Codes are single use (even after a failed try) and bound to the client and PKCE verifier; replaying a redeemed code revokes its tokens. Refresh rotates both tokens; replaying an old refresh token revokes the connection (RFC 9700), except within 10 seconds of the rotation (a concurrent refresh).');
  const officer = await session(baseURL!, 'officer');
  try {
    const reg = await register(officer, 'Token test');
    const cid = reg.json.client_id;
    const code = async (challenge: string) => {
      const r = await officer.post('/oauth/authorize', { form: { client_id: cid, redirect_uri: REDIRECT, response_type: 'code', code_challenge: challenge, code_challenge_method: 'S256', decision: 'allow' }, maxRedirects: 0 });
      return new URL(r.headers()['location'] ?? 'http://missing/').searchParams.get('code')!;
    };
    const exchange = (c: string, v: string, client = cid) => officer.post('/oauth/token', { form: { grant_type: 'authorization_code', code: c, code_verifier: v, client_id: client } });
    const p = pkce();
    const c1 = await code(p.challenge);
    const wrong = await exchange(c1, pkce().verifier);
    ev.step({ label: 'wrong code_verifier', request: 'POST /oauth/token', status: wrong.status(), body: await wrong.json() });
    ev.check(wrong.status() === 400, 'wrong verifier refused');
    const again = await exchange(c1, p.verifier);
    ev.step({ label: 'same code, right verifier', request: 'POST /oauth/token', status: again.status() });
    ev.check(again.status() === 400, 'a code is burned by the failed try');
    const c2 = await code(p.challenge);
    const otherClient = await exchange(c2, p.verifier, 'mcp_someone_else');
    ev.step({ label: 'another client_id', request: 'POST /oauth/token', status: otherClient.status() });
    ev.check(otherClient.status() === 400, 'codes are bound to their client');
    const good = await exchange(await code(p.challenge), p.verifier);
    const t = await good.json();
    ev.step({ label: 'valid exchange', request: 'POST /oauth/token', status: good.status(), body: { token_type: t.token_type, expires_in: t.expires_in, scope: t.scope, cache: good.headers()['cache-control'] } });
    ev.check(good.status() === 200 && t.expires_in === 3600 && good.headers()['cache-control'] === 'no-store', '1-hour bearer token, not cached');
    const refresh = (rt: string, extra: Record<string, string> = {}) => officer.post('/oauth/token', { form: { grant_type: 'refresh_token', refresh_token: rt, ...extra } });
    const r1 = await refresh(t.refresh_token, { client_id: cid });
    const t2 = await r1.json();
    ev.step({ label: 'refresh', request: 'POST /oauth/token grant_type=refresh_token', status: r1.status() });
    ev.check(r1.status() === 200 && t2.refresh_token !== t.refresh_token, 'refresh rotates the token');
    const oldAccess = await rpc(officer, t.access_token, { jsonrpc: '2.0', id: 1, method: 'ping' });
    ev.step({ label: 'old access token', request: 'POST /mcp', status: oldAccess.status });
    ev.check(oldAccess.status === 401, 'the replaced access token stops working');
    const noClient = await refresh(t2.refresh_token);
    const t3 = await noClient.json();
    ev.step({ label: 'refresh without client_id', request: 'POST /oauth/token', status: noClient.status() });
    ev.check(noClient.status() === 200, 'client_id is optional on refresh');
    const race = await refresh(t2.refresh_token);
    const during = await rpc(officer, t3.access_token, { jsonrpc: '2.0', id: 1, method: 'ping' });
    ev.step({ label: 'same token again at once (two windows refreshing)', request: 'POST /oauth/token, then POST /mcp', status: `${race.status()}, ${during.status}` });
    ev.check(race.status() === 400 && during.status === 200, 'a concurrent refresh loses the race but does not disconnect the agent');
    await new Promise((r) => setTimeout(r, 10_500)); // past the 10-second race window
    const replay = await refresh(t2.refresh_token);
    ev.step({ label: 'replay a rotated refresh token later', request: 'POST /oauth/token', status: replay.status(), body: await replay.json() });
    ev.check(replay.status() === 400, 'the replay is refused');
    const after = await rpc(officer, t3.access_token, { jsonrpc: '2.0', id: 1, method: 'ping' });
    ev.step({ label: 'current token after the replay', request: 'POST /mcp', status: after.status });
    ev.check(after.status === 401, 'the whole connection is revoked: someone else held a copy');
    const c4 = await code(p.challenge);
    const t4 = await (await exchange(c4, p.verifier)).json();
    const codeReplay = await exchange(c4, p.verifier);
    const afterCode = await rpc(officer, t4.access_token, { jsonrpc: '2.0', id: 1, method: 'ping' });
    ev.step({ label: 'redeem a code, then present it again', request: 'POST /oauth/token ×2, then POST /mcp', status: `${codeReplay.status()}, ${afterCode.status}` });
    ev.check(codeReplay.status() === 400 && afterCode.status === 401, 'a replayed code revokes the tokens it bought (RFC 6749 §4.1.2)');
  } finally {
    await ev.render(page);
  }
});

test('MCP session: initialize, role-filtered tools, acting as the officer', async ({ page, baseURL }) => {
  desktopOnly();
  const ev = new Evidence('acting-as-officer', 'Tools act as the officer, with exactly their permissions', 'Each tool call runs the dashboard API route as the signed-in officer. Officers are not offered director tools, and handler rules (no self-approval) still apply.');
  try {
    const o = await connectAgent(baseURL!, 'officer', 'Claude Code', ev);
    const d = await connectAgent(baseURL!, 'director', 'Cursor', ev);
    const init = await rpc(o.browser, o.access, { jsonrpc: '2.0', id: 1, method: 'initialize', params: { protocolVersion: '2025-06-18', capabilities: {}, clientInfo: { name: 'e2e', version: '1' } } });
    ev.step({ label: 'initialize', request: 'initialize (2025-06-18)', status: init.status, body: { protocolVersion: init.json.result.protocolVersion, serverInfo: init.json.result.serverInfo } });
    ev.check(init.json.result.protocolVersion === '2025-06-18', 'negotiates the client protocol version');
    const names = async (a: { browser: APIRequestContext; access: string }) => (await rpc(a.browser, a.access, { jsonrpc: '2.0', id: 2, method: 'tools/list' })).json.result.tools.map((t: { name: string }) => t.name) as string[];
    const ot = await names(o);
    const dt = await names(d);
    ev.step({ label: 'tools/list', request: 'officer vs director', body: { officer: ot.length, director: dt.length, onlyDirector: dt.filter((n) => !ot.includes(n)) } });
    ev.check(!ot.includes('approve_purchase_request') && dt.includes('approve_purchase_request'), 'approve is offered to directors only');
    const who = await tool(o.browser, o.access, 'whoami');
    ev.step({ label: 'whoami', request: 'tools/call whoami', body: who.json.user });
    ev.check(who.json.user.role === 'officer', 'the agent is the officer');
    const create = await tool(o.browser, o.access, 'create_purchase_request', { body: await purchaseBody(o.browser, 'E2E agent pizza', true) });
    const rid = create.json?.request?.id;
    ev.step({ label: 'officer agent files a request', request: 'tools/call create_purchase_request (submit)', body: { status: create.json?.request?.status, number: create.json?.request?.number } });
    ev.check(!create.isError && create.json.request.status === 'submitted', 'officer submits through the agent');
    const selfApprove = await tool(o.browser, o.access, 'approve_purchase_request', { id: rid, body: {} });
    ev.step({ label: 'officer agent approves', request: 'tools/call approve_purchase_request', ok: false, body: selfApprove.text });
    ev.check(selfApprove.isError, 'refused: officers cannot approve');
    const approve = await tool(d.browser, d.access, 'approve_purchase_request', { id: rid, body: { comment: 'ok' } });
    ev.step({ label: "director agent approves the officer's request", request: 'tools/call approve_purchase_request', body: { status: approve.json?.request?.status } });
    ev.check(!approve.isError && approve.json.request.status === 'approved', 'director approves through the agent');
    const dOwn = await tool(d.browser, d.access, 'create_purchase_request', { body: await purchaseBody(d.browser, 'E2E director own', true) });
    const self = await tool(d.browser, d.access, 'approve_purchase_request', { id: dOwn.json.request.id, body: {} });
    ev.step({ label: 'director agent approves its own', request: 'tools/call approve_purchase_request', ok: false, body: self.text });
    ev.check(self.isError, 'self-approval is still refused');
  } finally {
    await ev.render(page);
  }
});

async function purchaseBody(api: APIRequestContext, title: string, submit = false) {
  const lookups = await (await api.get('/api/lookups')).json();
  const account = lookups.expenseAccounts.find((a: { budgetable?: boolean }) => a.budgetable !== false) ?? lookups.expenseAccounts[0];
  return { type: 'club_purchase', title, vendor: 'E2E vendor', quantity: 1, unitCostCents: 1234, taxShipCents: 0, expenseAccountId: account.id, fundId: lookups.funds[0].id, justification: 'MCP e2e', submit };
}

for (const role of ['leadership', 'director', 'officer'] as const) {
  test(`every tool, live, as ${role}`, async ({ page, baseURL }) => {
    desktopOnly();
    const ev = new Evidence(`all-tools-${role}`, `All tools called live as ${role}`, `Each tool offered to a ${role} is called with realistic arguments. "REFUSED ✓" rows are refusals the dashboard makes for this role or this data too (role, Design team, state, local D1 not connected).`);
    try {
      const a = await connectAgent(baseURL!, role, `Sweep ${role}`);
      const offered: string[] = (await rpc(a.browser, a.access, { jsonrpc: '2.0', id: 1, method: 'tools/list' })).json.result.tools.map((t: { name: string }) => t.name);
      const results = await sweep(a.browser, a.access, role);
      const called = new Set(results.map((r) => r.name));
      for (const r of results) ev.step({ label: r.name, status: !r.isError ? 'OK' : r.allowed ? 'REFUSED ✓' : 'ERR', ok: !r.isError || r.allowed, request: r.args, body: r.isError ? r.text : undefined });
      ev.check(offered.every((n) => called.has(n)), `all ${offered.length} offered tools were called`);
      const unexpected = results.filter((r) => r.isError && !r.allowed);
      ev.check(unexpected.length === 0, `no unexpected failures${unexpected.length ? ': ' + unexpected.map((r) => `${r.name} ${r.text}`).join('; ') : ''}`);
    } finally {
      await ev.render(page);
    }
  });
}

/** Calls every tool with arguments that work on the seed data; `allowed` marks expected refusals. */
async function sweep(api: APIRequestContext, token: string, role: string) {
  const out: { name: string; args: string; isError: boolean; text: string; allowed: boolean }[] = [];
  const ROLE = { officer: 1, director: 2, leadership: 3 }[role]!;
  const call = async (name: string, args: Record<string, unknown> = {}, minRole = 1, alsoRefused?: RegExp) => {
    const r = await tool(api, token, name, args);
    const allowed = (ROLE < minRole && /Not allowed|Unknown tool/.test(r.text)) || (!!alsoRefused && alsoRefused.test(r.text));
    out.push({ name, args: JSON.stringify(args).slice(0, 110), isError: r.isError, text: r.text.slice(0, 140), allowed });
    return r;
  };
  const pdf = Buffer.from('%PDF-1.4\n1 0 obj<<>>endobj\ntrailer<<>>\n%%EOF\n').toString('base64');
  const png = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAAAASUVORK5CYII=';
  const stamp = Date.now();
  await call('whoami');
  await call('my_stats');
  await call('search', { q: 'hack' });
  await call('dashboard_overview');
  await call('lookups');
  await call('calendar_feed', { start: '2026-11-01', end: '2026-12-31' });
  await call('club_settings');
  await call('list_events', { when: 'all', limit: 5 });
  await call('get_event', { id: 'event-hack-night' });
  const ev = await call('create_event', { body: { name: `Sweep ${role} ${stamp}`, startsAt: '2026-12-05T18:00:00-08:00', endsAt: '2026-12-05T20:00:00-08:00', location: 'Room 1' } });
  const evId = ev.json?.event?.id;
  await call('update_event', { id: evId, body: { location: 'Room 2' } });
  await call('get_event_metrics', { id: 'event-kickoff' });
  await call('import_event_csv', { kind: 'luma_guests', eventId: evId, dryRun: 'true', filename: 'g.csv', content: 'name,email,approval_status,checked_in_at\nAda,ada@example.org,approved,\n' });
  await call('set_event_design_need', { id: evId, body: { designNeed: 'needed' } });
  await call('draft_design_request_for_event', { id: evId });
  const dfe = await call('create_design_request_for_event', { id: evId, body: {} });
  await call('list_purchase_requests', { scope: 'all', limit: 5 });
  await call('get_purchase_request', { id: 'req-0005' });
  const pr = await call('create_purchase_request', { body: await purchaseBody(api, `Sweep ${role} ${stamp}`) });
  const prId = pr.json?.request?.id;
  await call('update_purchase_request', { id: prId, body: { quantity: 3 } });
  await call('attach_receipt', { id: prId, kind: 'quote', filename: 'q.pdf', mimeType: 'application/pdf', contentBase64: pdf });
  await call('submit_purchase_request', { id: prId });
  await call('approve_purchase_request', { id: prId, body: { comment: 'ok' } }, 2, /own request|self|requester/i);
  await call('deny_purchase_request', { id: prId, body: { comment: 'no' } }, 2, /own request|self|requester/i);
  await call('transition_purchase_request', { id: 'req-0004', body: { to: 'fulfilling' } }, 2, /transition|state/i);
  const pr3 = await call('create_purchase_request', { body: await purchaseBody(api, `Sweep delete ${stamp}`) });
  await call('delete_purchase_request', { id: pr3.json?.request?.id });
  await call('finance_summary');
  await call('finance_report', { report: role === 'officer' ? 'fund-balances' : 'trial-balance' });
  const budgets = await call('get_budgets');
  await call('set_budgets', { body: { fiscalYear: budgets.json?.fiscalYear ?? 2027, lines: (budgets.json?.lines ?? []).map((l: any) => ({ accountId: l.account.id, fundId: l.fund?.id ?? null, amountCents: l.amountCents, notes: l.notes ?? '' })) } }, 2);
  const periods = await call('list_fiscal_periods', {}, 2);
  await call('create_fiscal_periods', { body: { fiscalYear: 2030 + ROLE } }, 2, /already has fiscal periods/);
  const open = periods.json?.periods?.find?.((p: any) => !p.closedAt && p.status !== 'closed');
  await call('close_fiscal_period', { id: open?.id ?? 'period-none', body: { to: 'closed' } }, 2, /not found|cannot|hasn't ended/i);
  await call('list_accounts', {}, 2);
  await call('list_design_requests');
  await call('get_design_request', { id: 'design-hack-poster' });
  const dr = await call('create_design_request', { body: { title: `Sweep poster ${stamp}`, type: 'poster', brief: 'A poster for the sweep.' } });
  const drId = dr.json?.request?.id;
  await call('update_design_request', { id: drId, body: { urgency: 'high' } });
  await call('assign_design_request', { id: drId, body: { assigneeId: 'user-riley' } }, 1, /design team/);
  await call('move_design_request', { id: drId, body: { stage: 'accepted' } }, 1, /design team/);
  await call('comment_on_design_request', { id: drId, body: { body: 'Looks good' } });
  await call('upload_design_file', { id: drId, kind: 'reference', filename: 'r.png', mimeType: 'image/png', contentBase64: png });
  await call('approve_design_request', { id: drId }, 1, /requester|review|stage/i);
  await call('delete_design_request', { id: dfe.json?.request?.id });
  await call('list_contacts', { q: 'Maya' });
  await call('get_contact', { id: 'bs-mentor-maya' });
  const c = await call('create_contact', { body: { name: `Sweep Contact ${stamp}`, type: 'mentor', topics: ['x'] } });
  await call('update_contact', { id: c.json?.contact?.id, body: { org: 'Org' } });
  await call('log_outreach', { id: c.json?.contact?.id, body: { date: '2026-11-15T09:00:00-08:00', channel: 'email', outcome: 'interested' } });
  await call('delete_contact', { id: c.json?.contact?.id });
  const members = await call('list_members', { limit: 2 });
  const mid = members.json?.items?.[0]?.id ?? 'no-such-member';
  await call('get_member', { id: mid }, 1, /not found|D1 is not connected/i);
  await call('update_member', { id: mid, body: { pronouns: 'they/them', auditNote: 'sweep' } }, 2, /not found|D1 is not connected/i);
  await call('export_members_csv', { limit: 2 }, 2);
  await call('list_officers');
  await call('get_officer', { id: 'user-priya' });
  await call('get_officer_activity', { id: 'user-priya' });
  await call('update_officer', { id: 'user-priya', body: { title: 'VP Events' } }, 2);
  await call('buildspace_overview');
  await call('save_buildspace_project', { id: `bs-sweep-${role}`, body: { name: 'Sweep Project', description: 'd', track: 'Software', stage: 'Building', links: [], mentorIds: [], cohortId: 'cohort-fall-2026' } });
  await call('save_buildspace_person', { id: `bs-sweep-person-${role}`, body: { name: 'Sweep Person', projectId: `bs-sweep-${role}`, joinedOn: '2026-11-01' } });
  await call('log_buildspace_activity', { body: { projectId: `bs-sweep-${role}`, personId: `bs-sweep-person-${role}`, kind: 'request', date: '2026-11-15', body: 'stuck', status: 'open' } });
  await call('resolve_buildspace_activity', { id: 'bs-a2' }, 1, /resolved|Only requests/);
  await call('set_buildspace_schedule', { body: { dueWeekday: 5, dueTime: '17:00' } }, 3);
  // Launchpad cohorts, accounts and mentor roster (#83). The dev server has no invite email,
  // so the invite tools answer "not configured": through an agent invites are email-only.
  const cohort = await call('create_launchpad_cohort', { body: { name: `Sweep ${role} ${stamp}`, startsOn: '2027-01-05' } }, 2);
  const cohortId = cohort.json?.cohort?.id ?? 'no-such-cohort';
  await call('update_launchpad_cohort', { id: cohortId, body: { endsOn: '2027-03-20' } }, 2);
  await call('invite_launchpad_builders', { id: 'cohort-fall-2026', body: { invites: [{ email: `sweep.${role}@uw.edu` }] } }, 1, /not configured/);
  await call('resend_launchpad_invite', { id: 'lp-invite-sky' }, 1, /not configured/);
  await call('revoke_launchpad_invite', { id: 'lp-invite-dev' }, 1, /not found/i);
  await call('disable_launchpad_account', { id: 'lp-account-maya' }, 2);
  await call('enable_launchpad_account', { id: 'lp-account-maya' }, 2);
  await call('invite_launchpad_mentor', { contactId: 'bs-mentor-maya', body: {} }, 2, /not configured/);
  await call('set_launchpad_mentor_settings', { contactId: 'bs-mentor-maya', body: { available: true, maxProjects: null } });
  const plan = await call('preview_launchpad_mentor_assignment', { id: 'cohort-fall-2026' });
  await call('apply_launchpad_mentor_assignment', { id: 'cohort-fall-2026', body: { assignments: plan.json?.assignments?.length ? plan.json.assignments : [{ projectId: `bs-sweep-${role}`, mentorId: 'bs-mentor-maya' }] } }, 1, /out of date/);
  await call('save_buildspace_person', { id: `bs-sweep-del-${role}`, body: { name: 'Sweep Delete', projectId: `bs-sweep-${role}`, joinedOn: '2026-11-01' } });
  await call('archive_launchpad_person', { id: `bs-sweep-person-${role}` });
  await call('restore_launchpad_person', { id: `bs-sweep-person-${role}` });
  await call('delete_launchpad_person', { id: `bs-sweep-del-${role}` }, 2);
  await call('archive_launchpad_project', { id: `bs-sweep-${role}` });
  await call('restore_launchpad_project', { id: `bs-sweep-${role}` });
  await call('save_buildspace_project', { id: `bs-sweep-del-${role}`, body: { name: 'Sweep Delete', description: 'd', track: 'Software', stage: 'Ideating', links: [], mentorIds: [], cohortId: 'cohort-fall-2026' } });
  await call('delete_launchpad_project', { id: `bs-sweep-del-${role}` }, 2);
  await call('delete_launchpad_cohort', { id: cohortId }, 2);
  const prof = await call('get_my_profile');
  await call('update_my_profile', { body: { phone: '+1 206 555 0100', birthday: '01-15', subteamId: prof.json?.profile?.subteamId ?? undefined } }, 1, /subteam/i);
  await call('delete_event', { id: evId });
  return out;
}

test('arguments: unknown or missing arguments are refused, never silently dropped', async ({ page, baseURL }) => {
  desktopOnly();
  const ev = new Evidence('arguments', 'Wrong arguments fail loudly', 'Before the fix, update_event with its fields next to `body` (not inside it) reported success and changed nothing. Now the agent is told what the tool takes.');
  try {
    const a = await connectAgent(baseURL!, 'officer', 'Args');
    // A request the officer owns, so uploads reach the size and type checks.
    const own = (await tool(a.browser, a.access, 'create_purchase_request', { body: await purchaseBody(a.browser, `Args upload ${Date.now()}`) })).json.request.id;
    const cases: [string, Record<string, unknown>, RegExp][] = [
      ['update_event', { id: 'event-hack-night', location: 'Moved' }, /Unknown: location.*inside `body`/s],
      ['update_event', { id: 'event-hack-night' }, /Missing: body/],
      ['calendar_feed', { start: '2026-11-01' }, /Missing: end/],
      ['get_event', { id: '..' }, /Invalid id/],
      ['get_event', { id: '../admin/users' }, /Invalid id/],
      ['create_event', { body: { name: '' } }, /HTTP 400.*name/s],
      ['attach_receipt', { id: own, filename: 'big.pdf', mimeType: 'application/pdf', contentBase64: Buffer.alloc(10 * 1024 * 1024 + 1).toString('base64') }, /maximum is 10 MB/],
      ['attach_receipt', { id: own, filename: 'x.exe', mimeType: 'application/x-msdownload', contentBase64: 'TVqQ' }, /Unsupported receipt MIME type/],
      ['finance_report', { report: 'trial-balance' }, /director/],
    ];
    for (const [name, args, want] of cases) {
      const r = await tool(a.browser, a.access, name, args);
      const shown = JSON.stringify(args).replace(/"contentBase64":"[^"]{40}[^"]*"/, '"contentBase64":"<10 MB+1>"').slice(0, 120);
      ev.step({ label: name, request: shown, ok: false, status: 'ERR', body: r.text.slice(0, 260) });
      ev.check(r.isError && want.test(r.text), `${name} ${shown.slice(0, 60)} → ${want}`);
    }
    const okUpdate = await tool(a.browser, a.access, 'update_event', { id: 'event-hack-night', body: { location: 'Engineering Atrium' } });
    ev.step({ label: 'update_event (correct)', request: '{"id":"event-hack-night","body":{"location":…}}', status: 'OK', body: { location: okUpdate.json?.event?.location } });
    ev.check(!okUpdate.isError && okUpdate.json.event.location === 'Engineering Atrium', 'fields inside body update the event');
  } finally {
    await ev.render(page);
  }
});

test('transport: JSON-RPC shapes, batches, protocol header, trailing slash', async ({ page, baseURL }) => {
  desktopOnly();
  const ev = new Evidence('transport', 'Streamable HTTP, stateless JSON-RPC', 'Every message shape a client might send, including the limits the security review asked for.');
  try {
    const a = await connectAgent(baseURL!, 'officer', 'Transport');
    const ping = { jsonrpc: '2.0', id: 1, method: 'ping' };
    const cases: [string, unknown, Record<string, string>, (r: Awaited<ReturnType<typeof rpc>>) => boolean, string][] = [
      ['invalid JSON', '{nope', {}, (r) => r.status === 400 && r.json?.error?.code === -32700, '400 parse error'],
      ['string id', { ...ping, id: 'abc' }, {}, (r) => r.json?.id === 'abc', 'echoes the id'],
      ['notification only', { jsonrpc: '2.0', method: 'notifications/initialized' }, {}, (r) => r.status === 202, '202, no body'],
      ['batch: request + notification', [ping, { jsonrpc: '2.0', method: 'notifications/initialized' }], {}, (r) => Array.isArray(r.json) && r.json.length === 1, 'one answer'],
      ['batch of 21', Array.from({ length: 21 }, (_, i) => ({ ...ping, id: i })), {}, (r) => r.status === 400, 'refused: at most 20'],
      ['unknown method', { ...ping, method: 'nope' }, {}, (r) => r.json?.error?.code === -32601, '-32601'],
      ['unknown tool', { ...ping, method: 'tools/call', params: { name: 'create_invite', arguments: {} } }, {}, (r) => r.json?.error?.code === -32602, 'invite tools do not exist'],
      ['MCP-Protocol-Version 1999-01-01', ping, { 'mcp-protocol-version': '1999-01-01' }, (r) => r.status === 400, '400'],
      ['MCP-Protocol-Version 2025-06-18', ping, { 'mcp-protocol-version': '2025-06-18' }, (r) => r.status === 200, '200'],
    ];
    for (const [label, body, headers, ok, want] of cases) {
      const r = await rpc(a.browser, a.access, body, headers);
      ev.step({ label, request: `POST /mcp ${typeof body === 'string' ? body : JSON.stringify(body).slice(0, 90)}`, status: r.status, body: r.text.slice(0, 200) || '(empty)' });
      ev.check(ok(r), `${label}: ${want}`);
    }
    const slash = await a.browser.post('/mcp/', { headers: { authorization: `Bearer ${a.access}`, 'content-type': 'application/json' }, data: JSON.stringify(ping) });
    ev.step({ label: 'trailing slash', request: 'POST /mcp/', status: slash.status() });
    ev.check(slash.status() === 200, 'a pasted URL ending in / works');
    const get = await a.browser.get('/mcp');
    ev.step({ label: 'GET /mcp', request: 'GET /mcp (SSE stream)', status: get.status() });
    ev.check(get.status() === 405, 'stateless: no stream, 405');
  } finally {
    await ev.render(page);
  }
});

test('concurrency and rate limit: 120 parallel calls, then 429 for that agent only', async ({ page, baseURL }) => {
  desktopOnly();
  const ev = new Evidence('concurrency', '120 parallel calls, then the per-agent rate limit', 'One agent fires its whole budget at once: 90 reads and 30 contact creations (120 calls, the per-connection limit per minute). Its next calls get 429 with Retry-After; another agent of the same officer is unaffected.');
  try {
    const a = await connectAgent(baseURL!, 'director', 'Stress');
    const other = await connectAgent(baseURL!, 'director', 'Stress (second agent)');
    const tag = `Stress ${Date.now()}`;
    const t0 = Date.now();
    const rs = await Promise.all(Array.from({ length: 120 }, (_, i) => (i % 4 === 0 ? tool(a.browser, a.access, 'create_contact', { body: { name: `${tag} ${i}`, type: 'speaker' } }) : tool(a.browser, a.access, ['list_events', 'whoami', 'search', 'list_design_requests'][i % 4]!, i % 4 === 2 ? { q: 'hack' } : {}))));
    const ms = Date.now() - t0;
    const errs = rs.filter((r) => r.isError);
    const over = await rpc(a.browser, a.access, { jsonrpc: '2.0', id: 9, method: 'tools/call', params: { name: 'whoami', arguments: {} } });
    const listed = await tool(other.browser, other.access, 'list_contacts', { q: tag });
    ev.step({ label: 'result', request: '120 × tools/call in parallel', body: { calls: rs.length, errors: errs.length, milliseconds: ms, contactsCreated: listed.json?.items?.length } });
    ev.step({ label: 'call 121', request: 'tools/call whoami', status: over.status, ok: over.status === 429, body: { 'retry-after': over.headers['retry-after'], error: over.json?.error } });
    ev.check(errs.length === 0, 'no call within the budget failed');
    ev.check(listed.json?.items?.length === 30, 'all 30 writes landed exactly once');
    ev.check(over.status === 429 && Number(over.headers['retry-after']) >= 1 && over.json?.error?.code === -32000 && over.json?.id === 9, 'call 121 gets 429, Retry-After and a JSON-RPC error');
    ev.check(!listed.isError, "the officer's second agent has its own budget");
  } finally {
    await ev.render(page);
  }
});

test('revocation: disconnect and deactivation cut the agent off at once', async ({ page, baseURL }) => {
  desktopOnly();
  const ev = new Evidence('revocation', 'Disconnecting or deactivating stops the agent immediately', 'The user is reloaded on every call; deactivation also revokes every connection the officer made.');
  try {
    const a = await connectAgent(baseURL!, 'officer', 'Revoke me', ev);
    const list = await (await a.browser.get('/api/me/agents')).json();
    const mine = list.agents.find((x: { clientName: string }) => x.clientName === 'Revoke me');
    ev.step({ label: 'Connected agents', request: 'GET /api/me/agents', status: 200, body: mine });
    ev.check(!!mine, 'the agent is listed');
    const director = await session(baseURL!, 'director');
    const foreign = await director.delete(`/api/me/agents/${mine.id}`);
    ev.step({ label: "another officer's disconnect", request: `DELETE /api/me/agents/${mine.id} (director)`, status: foreign.status() });
    ev.check(foreign.status() === 404, "nobody disconnects someone else's agent");
    const del = await a.browser.delete(`/api/me/agents/${mine.id}`);
    const after = await rpc(a.browser, a.access, { jsonrpc: '2.0', id: 1, method: 'ping' });
    ev.step({ label: 'disconnect, then call', request: 'DELETE /api/me/agents/:id → POST /mcp', status: `${del.status()} → ${after.status}` });
    ev.check(del.ok() && after.status === 401, 'disconnected agent gets 401 on its next call');
    // Deactivation: Marcus connects an agent, a director deactivates him, then restores him.
    const marcus = await pwRequest.newContext({ baseURL });
    const form: Record<string, string> = { as: 'officer', user: 'user-marcus', next: '/' };
    await marcus.post('/auth/dev', { form, maxRedirects: 0 });
    const reg = await register(marcus, 'Marcus agent');
    const p = pkce();
    const allow = await marcus.post('/oauth/authorize', { form: { client_id: reg.json.client_id, redirect_uri: REDIRECT, response_type: 'code', code_challenge: p.challenge, code_challenge_method: 'S256', decision: 'allow' }, maxRedirects: 0 });
    const code = new URL(allow.headers()['location'] ?? 'http://missing/').searchParams.get('code')!;
    const tok = await (await marcus.post('/oauth/token', { form: { grant_type: 'authorization_code', code, code_verifier: p.verifier, client_id: reg.json.client_id } })).json();
    const before = await rpc(marcus, tok.access_token, { jsonrpc: '2.0', id: 1, method: 'ping' });
    try {
      const off = await director.patch('/api/admin/users/user-marcus', { data: { active: false } });
      const gone = await rpc(marcus, tok.access_token, { jsonrpc: '2.0', id: 1, method: 'ping' });
      const refresh = await marcus.post('/oauth/token', { form: { grant_type: 'refresh_token', refresh_token: tok.refresh_token } });
      ev.step({ label: 'director deactivates Marcus', request: 'ping → PATCH /api/admin/users/user-marcus {active:false} → ping, refresh', status: `${before.status} → ${off.status()} → ${gone.status}, ${refresh.status()}` });
      ev.check(before.status === 200 && gone.status === 401 && refresh.status() === 400, "the deactivated officer's agent and refresh token stop at once");
    } finally {
      await director.patch('/api/admin/users/user-marcus', { data: { active: true } });
    }
  } finally {
    await ev.render(page);
  }
});

// ───────────────────────────── the Go tests ─────────────────────────────

/**
 * The Go tests fix the clock (code/token expiry, refresh expiry, purge) and control the
 * client IP (rate limits), which a live server can't. Run each and keep its output.
 */
const GO_TESTS = [
  'TestMCPDiscoveryAndUnauthenticated',
  'TestOAuthRegisterValidatesRedirects',
  'TestOAuthAuthorizeFlowAndConsent',
  'TestOAuthTokenExchangeAndRefresh',
  'TestMCPToolsActAsTheOfficer',
  'TestMCPUploadTool',
  'TestConnectedAgentsAndRevocation',
  'TestMCPToolsMatchRoutes',
  'TestMCPArgumentChecks',
  'TestMCPTransportDetails',
  'TestOAuthHardening',
  'TestConnectedAgentsListStaysClean',
  'TestPurgeOAuth',
  'TestMCPReviewFixes',
  'TestOAuthRefreshReuseRevokes',
  'TestMCPRateLimitPerAgent',
  'TestMCPLaunchpadTools',
  'TestMCPLaunchpadInviteNeedsMailer',
  'TestOAuthCodeReplayRevokes',
  'TestOAuthAuthorizeLongRequestWhileSignedOut',
];

for (const name of GO_TESTS) {
  test(`go test ${name}`, async ({ page }) => {
    desktopOnly();
    test.skip(!!process.env.MCP_SKIP_GO, 'MCP_SKIP_GO set');
    const src = readFileSync(`${ROOT}/server/internal/api/mcp_test.go`, 'utf8');
    const at = src.indexOf(`func ${name}(`);
    expect(at, `${name} exists in mcp_test.go`).toBeGreaterThan(-1);
    // The comment block right above the function says what it proves.
    const doc = src.slice(0, at).split('\n').reverse();
    const lines: string[] = [];
    for (const l of doc.slice(1)) {
      if (!l.startsWith('//')) break;
      lines.unshift(l.replace(/^\/\/ ?/, ''));
    }
    const body = src.slice(at, src.indexOf('\n}\n', at) + 2);
    const res = spawnSync('go', ['test', '-count=1', '-v', '-run', `^${name}$`, './internal/api'], { cwd: `${ROOT}/server`, encoding: 'utf8', env: plainEnv, timeout: 300_000 });
    const output = stripAnsi(res.stdout + res.stderr).trim();
    const ev = new Evidence(`go-${name}`, name, lines.join(' ') || 'Go test in server/internal/api/mcp_test.go.');
    ev.step({ label: 'command', request: `cd server && go test -count=1 -v -run '^${name}$' ./internal/api`, status: res.status === 0 ? 'PASS' : 'FAIL', ok: res.status === 0, body: output });
    // Its assertions, from the messages it would fail with: "reuse not audited: %v" → "reuse not audited".
    const asserts = [...body.matchAll(/(?:t\.Fatalf?\(|expectStatus\(t, )"([^"]{4,100})"/g)].map((m) => (m[1] ?? '').replace(/(%[vsdqT][\s:,]*)+/g, '… ').replace(/\s+/g, ' ').trim());
    ev.notes.push(...[...new Set(asserts)].slice(0, 30));
    try {
      ev.check(res.status === 0 && /--- PASS/.test(output), `${name} passes`);
    } finally {
      await ev.render(page);
    }
  });
}

// ───────────────────────────── real clients (optional) ─────────────────────────────

/** The official MCP TypeScript SDK client (scripts/mcp-sdk-e2e.mjs). MCP_SDK_E2E=1 after `npm i --no-save @modelcontextprotocol/sdk` at the root. */
for (const role of ['officer', 'director']) {
  test(`official MCP SDK client, end to end, as ${role}`, async ({ page, baseURL }) => {
    desktopOnly();
    test.skip(!process.env.MCP_SDK_E2E, 'set MCP_SDK_E2E=1 (needs @modelcontextprotocol/sdk at the root)');
    const res = spawnSync('node', [`${ROOT}/scripts/mcp-sdk-e2e.mjs`, role], { cwd: ROOT, encoding: 'utf8', env: { ...plainEnv, BASE: baseURL }, timeout: 120_000 });
    const out = stripAnsi(res.stdout + res.stderr).trim();
    const ev = new Evidence(`sdk-e2e-${role}`, `Official MCP TypeScript SDK as ${role}`, 'The SDK discovers the auth server from the 401, registers itself, runs PKCE, and after the officer allows it calls tools: the same path Claude Code and claude.ai take.');
    try {
      ev.step({ label: 'command', request: `BASE=${baseURL} node scripts/mcp-sdk-e2e.mjs ${role}`, status: res.status === 0 ? 'PASS' : 'FAIL', ok: res.status === 0, body: out });
      ev.check(/registered client mcp_/.test(out), 'the SDK registered itself (RFC 7591)');
      ev.check(/Allow → .*code\? true/.test(out), 'the consent page issued a code');
      ev.check(/saved tokens, expires_in 3600/.test(out), 'the SDK exchanged it with PKCE');
      ev.check(new RegExp(`whoami → .*\\(${role}\\)`).test(out), `tools run as the ${role}`);
      ev.check(role === 'officer' ? /approve_purchase_request → refused/.test(out) : /approve_purchase_request → APPROVED/.test(out), role === 'officer' ? 'approval refused for an officer' : 'a director approves');
      ev.check(res.status === 0 && /E2E OK/.test(out), 'the run completes');
    } finally {
      await ev.render(page);
    }
  });
}

/** The real Claude Code CLI: adds the server in a scratch project, lists it, removes it. MCP_CLAUDE_CLI=1. */
test('Claude Code CLI finds the server and asks to sign in', async ({ page, baseURL }) => {
  desktopOnly();
  test.skip(!process.env.MCP_CLAUDE_CLI, 'set MCP_CLAUDE_CLI=1 with `claude` on PATH');
  const dir = `${ROOT}/web/test-results/claude-cli-probe`;
  mkdirSync(dir, { recursive: true });
  const run = (args: string[]) => spawnSync('claude', args, { cwd: dir, encoding: 'utf8', env: plainEnv, timeout: 90_000 });
  const ev = new Evidence('claude-code-cli', 'Claude Code discovers the OAuth server', 'claude mcp add, then claude mcp list: Claude Code calls /mcp, follows the 401 to the metadata and reports that the server needs sign-in (the browser step is the consent flow shown in flow-*.png).');
  try {
    const version = run(['--version']);
    const add = run(['mcp', 'add', '--transport', 'http', 'cseed-evidence', `${baseURL}/mcp`, '-s', 'local']);
    const list = run(['mcp', 'list']);
    const line = list.stdout.split('\n').find((l) => l.includes('cseed-evidence')) ?? list.stdout;
    ev.step({ label: 'version', request: 'claude --version', body: version.stdout.trim() });
    ev.step({ label: 'add', request: `claude mcp add --transport http cseed-evidence ${baseURL}/mcp -s local`, status: add.status ?? 'ERR', ok: add.status === 0, body: add.stdout.trim().split('\n')[0] });
    ev.step({ label: 'list', request: 'claude mcp list', body: line.trim() });
    ev.check(add.status === 0, 'Claude Code accepts the URL');
    ev.check(/Needs authentication/i.test(line), 'Claude Code detects the OAuth requirement');
  } finally {
    run(['mcp', 'remove', 'cseed-evidence', '-s', 'local']);
    await ev.render(page);
  }
});
