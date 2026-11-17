// Drives the dashboard's /mcp with the official MCP TypeScript SDK client: the SDK does
// discovery, dynamic client registration and PKCE itself; this script plays the officer's
// browser (dev sign-in, consent page, Allow) and then calls tools. docs/mcp.md.
//
//   npm i --no-save @modelcontextprotocol/sdk   # once, at the repository root
//   BASE=http://localhost:8090 node scripts/mcp-sdk-e2e.mjs [officer|director]
//
// Needs a dev server with DEV_AUTH=1 (cd server && make dev).
import { Client } from '@modelcontextprotocol/sdk/client/index.js';
import { StreamableHTTPClientTransport } from '@modelcontextprotocol/sdk/client/streamableHttp.js';
import { UnauthorizedError } from '@modelcontextprotocol/sdk/client/auth.js';

const BASE = process.env.BASE ?? 'http://localhost:8090';
const AS = process.argv[2] ?? 'officer';
const REDIRECT = 'http://127.0.0.1:53682/callback';

/** Decodes the HTML entities html/template emits, in one pass (no double unescaping). */
function decodeEntities(s) {
  const named = { amp: '&', lt: '<', gt: '>', quot: '"', apos: "'" };
  return s.replace(/&(#x[0-9a-f]+|#\d+|[a-z]+);/gi, (m, e) =>
    e[0] === '#' ? String.fromCodePoint(e[1] === 'x' || e[1] === 'X' ? parseInt(e.slice(2), 16) : Number(e.slice(1))) : (named[e.toLowerCase()] ?? m));
}

// ── the officer's browser ──
async function browserSession() {
  const r = await fetch(`${BASE}/auth/dev`, { method: 'POST', redirect: 'manual', body: new URLSearchParams({ as: AS, next: '/' }) });
  const ck = r.headers.getSetCookie().find((c) => c.startsWith('cseed_session='));
  if (!ck) throw new Error(`dev sign-in failed: ${r.status}`);
  return ck.split(';')[0];
}
const cookie = await browserSession();
let pendingCode = null;
const log = (...a) => console.log('·', ...a);

// ── OAuth client provider (in memory) ──
const state = {};
const provider = {
  get redirectUrl() { return REDIRECT; },
  get clientMetadata() {
    return { client_name: 'E2E Claude Code', redirect_uris: [REDIRECT], grant_types: ['authorization_code', 'refresh_token'], response_types: ['code'], token_endpoint_auth_method: 'none' };
  },
  clientInformation: () => state.client,
  saveClientInformation: (c) => { state.client = c; log('registered client', c.client_id); },
  tokens: () => state.tokens,
  saveTokens: (t) => { state.tokens = t; log('saved tokens, expires_in', t.expires_in); },
  codeVerifier: () => state.verifier,
  saveCodeVerifier: (v) => { state.verifier = v; },
  async redirectToAuthorization(url) {
    log('browser opens', url.pathname + '?client_id=…&code_challenge=…');
    // Signed out first: the dashboard sends us to sign in, returning to this URL.
    const signedOut = await fetch(url, { redirect: 'manual' });
    log('signed out →', signedOut.status, signedOut.headers.get('location')?.slice(0, 60) + '…');
    const page = await fetch(url, { headers: { cookie }, redirect: 'manual' });
    const html = await page.text();
    if (page.status !== 200) throw new Error(`consent page ${page.status}: ${html}`);
    // The <title> is plain text ("Connect <agent> · CSEED"): no tag stripping needed.
    log('consent page:', decodeEntities(html.match(/<title>([^<]*)<\/title>/)?.[1] ?? '?'));
    const form = new URLSearchParams();
    for (const m of html.matchAll(/<input type="hidden" name="([^"]+)" value="([^"]*)">/g)) form.set(m[1], decodeEntities(m[2]));
    form.set('decision', 'allow');
    const allow = await fetch(`${BASE}/oauth/authorize`, { method: 'POST', redirect: 'manual', headers: { cookie, origin: BASE }, body: form });
    const back = new URL(allow.headers.get('location'));
    log('Allow →', back.origin + back.pathname, 'code?', !!back.searchParams.get('code'), 'iss', back.searchParams.get('iss'));
    pendingCode = back.searchParams.get('code');
  },
};

// ── connect: first attempt triggers OAuth, then finish and reconnect ──
const url = new URL(`${BASE}/mcp`);
let transport = new StreamableHTTPClientTransport(url, { authProvider: provider });
const client = new Client({ name: 'e2e', version: '1.0.0' });
try {
  await client.connect(transport);
} catch (e) {
  if (!(e instanceof UnauthorizedError)) throw e;
  log('401 → OAuth done, finishing with code');
  await transport.finishAuth(pendingCode);
  transport = new StreamableHTTPClientTransport(url, { authProvider: provider });
  await client.connect(transport);
}
const info = client.getServerVersion();
log('connected to', info.name, info.version, '| protocol', transport.protocolVersion ?? '(negotiated)');

const { tools } = await client.listTools();
log(`${tools.length} tools offered to this ${AS}:`, tools.slice(0, 8).map((t) => t.name).join(', '), '…');
log('approve_purchase_request offered?', tools.some((t) => t.name === 'approve_purchase_request'));

const text = (r) => r.content?.[0]?.text ?? '';
const who = JSON.parse(text(await client.callTool({ name: 'whoami', arguments: {} })));
log('whoami →', who.user.name, `(${who.user.role})`);

const events = JSON.parse(text(await client.callTool({ name: 'list_events', arguments: { when: 'upcoming', limit: 3 } })));
log('list_events(upcoming) →', events.items.map((e) => e.name).join(' | '));

const created = await client.callTool({ name: 'create_event', arguments: { body: { name: 'Agent-planned Study Jam', startsAt: '2026-12-03T18:00:00-08:00', endsAt: '2026-12-03T20:00:00-08:00', location: 'Library Lab B' } } });
if (created.isError) throw new Error('create_event: ' + text(created));
const ev = JSON.parse(text(created)).event;
log('create_event →', ev.id, ev.name, ev.startsAt);

const need = await client.callTool({ name: 'set_event_design_need', arguments: { id: ev.id, body: { designNeed: 'needed' } } });
log('set_event_design_need →', need.isError ? 'ERROR ' + text(need) : 'ok');

const contact = await client.callTool({ name: 'create_contact', arguments: { body: { name: 'Dr. Agent Speaker', type: 'speaker', org: 'MCP Labs', topics: ['AI agents'] } } });
const cid = JSON.parse(text(contact)).contact.id;
const outreach = await client.callTool({ name: 'log_outreach', arguments: { id: cid, body: { date: '2026-11-15T10:00:00-08:00', channel: 'email', outcome: 'interested', note: 'Sent by agent' } } });
log('create_contact + log_outreach →', cid, outreach.isError ? 'ERROR ' + text(outreach) : 'ok');

const bad = await client.callTool({ name: 'create_event', arguments: { body: { name: '' } } });
log('validation error surfaces →', bad.isError, text(bad).slice(0, 110).replace(/\n/g, ' '));

const forbidden = await client.callTool({ name: 'approve_purchase_request', arguments: { id: 'req-0005', body: {} } });
log('approve_purchase_request →', forbidden.isError ? 'refused: ' + text(forbidden).slice(0, 90) : 'APPROVED');

const lp = JSON.parse(text(await client.callTool({ name: 'buildspace_overview', arguments: { cohort: 'cohort-fall-2026' } })));
log('buildspace_overview(fall cohort) →', lp.cohorts.length, 'cohorts,', lp.projects.length, 'projects,', lp.mentors.length, 'mentors');
const plan = await client.callTool({ name: 'preview_launchpad_mentor_assignment', arguments: { id: 'cohort-fall-2026' } });
if (plan.isError) throw new Error('preview_launchpad_mentor_assignment: ' + text(plan));
log('preview_launchpad_mentor_assignment →', JSON.parse(text(plan)).assignments.length, 'proposed');
if (AS === 'officer') {
  const del = await client.callTool({ name: 'delete_launchpad_cohort', arguments: { id: 'cohort-spring-2026' } });
  log('delete_launchpad_cohort (director-only) →', del.isError ? 'refused: ' + text(del).slice(0, 90) : 'DELETED');
  if (!del.isError) throw new Error('an officer agent deleted a cohort');
}

const search = JSON.parse(text(await client.callTool({ name: 'search', arguments: { q: 'Agent' } })));
log('search("Agent") →', (search.results ?? search.items ?? []).map((r) => `${r.kind}:${r.title ?? r.name}`).join(', '));

await client.close();
console.log('\nE2E OK');
