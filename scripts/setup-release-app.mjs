#!/usr/bin/env node
// One-time repository setup. Private keys and short-lived tokens stay in memory.
import { spawn } from 'node:child_process';
import { randomBytes, sign, timingSafeEqual } from 'node:crypto';
import { createServer } from 'node:http';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { parseArgs } from 'node:util';
import { setTimeout as delay } from 'node:timers/promises';

const creationRulesetName = 'Version tags - owners create';
const usage = 'node scripts/setup-release-app.mjs --org ORG --repo REPO [--name cseed-release] [--port N] [--dry-run] [--force]';
const html = (value) => String(value).replace(/[&<>"']/g, (char) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[char]);

export function buildManifest({ org, repo, name = 'cseed-release', port }) {
  if (!/^[a-z\d](?:[a-z\d-]{0,37}[a-z\d])?$/i.test(org ?? '') ||
      !/^[\w.-]+$/.test(repo ?? '') || ['.', '..'].includes(repo)) {
    throw new Error('Provide a valid GitHub organization and repository name.');
  }
  if (!Number.isInteger(port) || port < 0 || port > 65535) throw new Error('Port must be an integer from 0 to 65535.');
  if (!name.trim() || /[\r\n]/.test(name)) throw new Error('Provide a nonempty App name on one line.');
  return {
    name, url: `https://github.com/${org}/${repo}`, public: false,
    hook_attributes: { active: false },
    redirect_url: `http://127.0.0.1:${port}/callback`,
    default_permissions: { contents: 'write', pull_requests: 'write', metadata: 'read' },
  };
}

export function mergeRulesetBypass(ruleset, appId) {
  if (!Number.isSafeInteger(appId) || appId <= 0) throw new Error('Invalid App ID.');
  // Never grant an App a bypass on the separate immutable-tag ruleset.
  if (ruleset.name !== creationRulesetName || ruleset.target !== 'tag' ||
      ruleset.rules?.length !== 1 || ruleset.rules[0].type !== 'creation') {
    throw new Error(`Refusing to modify anything other than the ${creationRulesetName} creation ruleset.`);
  }
  const bypass = structuredClone(ruleset.bypass_actors ?? []);
  const existing = bypass.find((actor) => actor.actor_type === 'Integration' && actor.actor_id === appId);
  if (existing) existing.bypass_mode = 'always';
  else bypass.push({ actor_id: appId, actor_type: 'Integration', bypass_mode: 'always' });
  return {
    name: ruleset.name, target: ruleset.target, enforcement: ruleset.enforcement,
    conditions: structuredClone(ruleset.conditions), rules: structuredClone(ruleset.rules),
    bypass_actors: bypass,
  };
}

export function jwtClaims(appId, now = Date.now()) {
  if (!Number.isSafeInteger(appId) || appId <= 0 || !Number.isFinite(now)) throw new Error('Invalid JWT App ID or clock.');
  const seconds = Math.floor(now / 1000);
  return { iat: seconds - 60, exp: seconds + 9 * 60, iss: String(appId) };
}

export function appJwt(appId, pem, now = Date.now()) {
  const encode = (value) => Buffer.from(JSON.stringify(value)).toString('base64url');
  const payload = `${encode({ alg: 'RS256', typ: 'JWT' })}.${encode(jwtClaims(appId, now))}`;
  return `${payload}.${sign('RSA-SHA256', Buffer.from(payload), pem).toString('base64url')}`;
}

async function gh(args, input) {
  return new Promise((accept, reject) => {
    const child = spawn('gh', args, { stdio: ['pipe', 'pipe', 'pipe'], windowsHide: true });
    let output = '';
    child.stdout.setEncoding('utf8').on('data', (chunk) => { output += chunk; });
    // Never include subprocess output in errors: secret input must not escape.
    child.stderr.resume();
    child.stdin.on('error', () => {});
    child.on('error', () => reject(new Error('Could not start gh. Install GitHub CLI and run gh auth login first.')));
    child.on('close', (code) => code === 0 ? accept(output) : reject(new Error(`gh ${args.slice(0, 2).join(' ')} failed (exit ${code}); check GitHub CLI authentication and repository administration access.`)));
    child.stdin.end(input);
  });
}

async function api(path, { token, method = 'GET', body } = {}) {
  let response;
  try {
    response = await fetch(`https://api.github.com${path}`, {
      method, redirect: 'error', signal: AbortSignal.timeout(15_000),
      headers: { Accept: 'application/vnd.github+json', 'X-GitHub-Api-Version': '2022-11-28', 'User-Agent': 'officer-dashboard-release-setup',
        ...(token && { Authorization: `Bearer ${token}` }), ...(body && { 'Content-Type': 'application/json' }) },
      ...(body && { body: JSON.stringify(body) }),
    });
  } catch {
    throw new Error('GitHub API request failed or timed out. No credentials were logged.');
  }
  if (!response.ok) throw new Error(`GitHub API request failed (HTTP ${response.status}). Check App installation and repository permissions.`);
  if (response.status === 204) return;
  try { return await response.json(); } catch { throw new Error('GitHub API returned invalid JSON; response content was not logged.'); }
}

export function setupServer({ state, manifest, org, onCode, status }) {
  let consumed = false;
  const server = createServer(async (req, res) => {
    res.setHeader('Cache-Control', 'no-store');
    res.setHeader('Referrer-Policy', 'no-referrer');
    res.setHeader('X-Content-Type-Options', 'nosniff');
    res.setHeader('Content-Type', 'text/html; charset=utf-8');
    const address = `127.0.0.1:${server.address().port}`;
    if (req.headers.host !== address || req.method !== 'GET') {
      res.writeHead(400).end('Invalid local request.');
      return;
    }
    const url = new URL(req.url, `http://${address}`);
    const send = (body) => res.end(`<!doctype html><html lang="en"><meta charset="utf-8"><title>Release App setup</title><body><h1>Release App setup</h1>${body}</body></html>`);
    if (url.pathname === '/callback') {
      const received = Buffer.from(url.searchParams.get('state') ?? '');
      const expected = Buffer.from(state);
      const code = url.searchParams.get('code');
      if (received.length !== expected.length || !timingSafeEqual(received, expected) || !code) {
        res.writeHead(400).end('Invalid callback state or missing code. Return to the original setup page.');
        return;
      }
      if (consumed) { res.writeHead(409).end('This callback has already been used.'); return; }
      consumed = true;
      try { await onCode(code); } catch { /* The main routine reports a credential-free error. */ }
      send(status());
    } else if (url.pathname === '/') {
      if (consumed) { send(status()); return; }
      const action = `https://github.com/organizations/${encodeURIComponent(org)}/settings/apps/new?state=${encodeURIComponent(state)}`;
      send(`<p>One-time repository setup. Review the permissions on GitHub.</p><form method="post" action="${html(action)}"><textarea name="manifest" hidden>${html(JSON.stringify(manifest()))}</textarea><button type="submit">Continue to GitHub to create the App</button></form><script>document.forms[0].submit()</script>`);
    } else res.writeHead(404).end('Not found.');
  });
  return server;
}

export async function waitForInstallation(app, org, repo, { request = api, pause = delay, now = Date.now } = {}) {
  const deadline = now() + 10 * 60_000;
  while (now() < deadline) {
    const token = appJwt(app.id, app.pem);
    let page = 1;
    while (now() < deadline) {
      const installations = await request(`/app/installations?per_page=100&page=${page}`, { token });
      for (const installation of installations) {
        if (installation.account?.login?.toLowerCase() !== org.toLowerCase() ||
            installation.repository_selection !== 'selected' || installation.suspended_at) continue;
        const access = await request(`/app/installations/${installation.id}/access_tokens`, {
          token, method: 'POST', body: { permissions: { metadata: 'read' } },
        });
        try {
          const selected = await request('/installation/repositories?per_page=100', { token: access.token });
          if (selected.total_count === 1 && selected.repositories[0]?.full_name?.toLowerCase() === `${org}/${repo}`.toLowerCase()) return;
        } finally {
          await request('/installation/token', { token: access.token, method: 'DELETE' });
        }
      }
      if (installations.length < 100) break;
      page++;
    }
    await pause(Math.min(5000, Math.max(0, deadline - now())));
  }
  throw new Error(`Timed out after 10 minutes waiting for installation on only ${org}/${repo}. Secrets were saved; the creation-ruleset bypass was not added. Install the App and finish that bypass manually; do not recreate it just to retry.`);
}

export async function main(args = process.argv.slice(2), { runGh = gh, request = api, wait = waitForInstallation, log = console.log } = {}) {
  const { values } = parseArgs({ args, options: {
    org: { type: 'string' }, repo: { type: 'string' }, name: { type: 'string', default: 'cseed-release' },
    port: { type: 'string' }, 'dry-run': { type: 'boolean' }, force: { type: 'boolean' }, help: { type: 'boolean' },
  } });
  if (values.help) { log(usage); return; }
  const { org, repo, name } = values;
  const portText = values.port ?? process.env.PANE_PORT ?? '0';
  if (!/^\d+$/.test(portText)) throw new Error('Port must be an integer from 0 to 65535.');
  const requestedPort = Number(portText);
  buildManifest({ org, repo, name, port: requestedPort });
  const repository = `${org}/${repo}`;
  if (values['dry-run']) {
    log(JSON.stringify(buildManifest({ org, repo, name, port: requestedPort }), null, 2));
    log(`Planned only (no server, API calls or writes; port 0 becomes a free port on a real run):
gh secret list --repo ${repository} --json name
POST https://api.github.com/app-manifests/<code>/conversions
gh secret set RELEASE_APP_ID --repo ${repository} < stdin
gh secret set RELEASE_APP_PRIVATE_KEY --repo ${repository} < stdin (memory only)
Open https://github.com/apps/<slug>/installations/new; select only ${repository}
GET /app/installations using an in-memory RS256 App JWT; verify selected repositories
gh api --paginate repos/${repository}/rulesets
gh api repos/${repository}/rulesets/<creation-ruleset-id>
gh api --method PUT repos/${repository}/rulesets/<creation-ruleset-id> --input -
Only merge the App bypass into '${creationRulesetName}'; preserve all other actors and the immutable ruleset.`);
    return;
  }
  const secrets = JSON.parse(await runGh(['secret', 'list', '--repo', repository, '--json', 'name']));
  if (!values.force && ['RELEASE_APP_ID', 'RELEASE_APP_PRIVATE_KEY'].every((key) => secrets.some((secret) => secret.name === key))) {
    log(`Both release App secrets already exist in ${repository}; no changes made. Use --force only to replace the App and its secrets intentionally.`);
    return;
  }
  if (secrets.some((secret) => ['RELEASE_APP_ID', 'RELEASE_APP_PRIVATE_KEY'].includes(secret.name)) && !values.force) {
    throw new Error('Only one release App secret exists. Finish the existing setup or use --force to intentionally replace it.');
  }
  let actualPort, statusText = '<p>Creating the App and saving its secrets…</p>';
  let acceptCode;
  const callback = new Promise((accept) => { acceptCode = accept; });
  const server = setupServer({
    state: randomBytes(32).toString('hex'), org,
    manifest: () => buildManifest({ org, repo, name, port: actualPort }),
    status: () => statusText,
    onCode: async (code) => {
      try {
        const app = await request(`/app-manifests/${encodeURIComponent(code)}/conversions`, { method: 'POST' });
        if (!Number.isSafeInteger(app.id) || !/^[\w-]+$/.test(app.slug ?? '') || typeof app.pem !== 'string') throw new Error('GitHub returned an incomplete App configuration.');
        await runGh(['secret', 'set', 'RELEASE_APP_ID', '--repo', repository], String(app.id));
        await runGh(['secret', 'set', 'RELEASE_APP_PRIVATE_KEY', '--repo', repository], app.pem);
        const install = `https://github.com/apps/${app.slug}/installations/new`;
        log(`Secrets saved. Install the App on ONLY ${repository}:\n${install}`);
        statusText = `<p>Secrets saved. <a href="${html(install)}">Install the App on only ${html(repository)}</a>. Select “Only select repositories”. Waiting up to 10 minutes for installation.</p>`;
        acceptCode({ app });
      } catch (error) {
        statusText = '<p>Setup failed. See the terminal for next steps. No private key was logged or written to disk.</p>';
        acceptCode({ error });
      }
    },
  });
  try {
    await new Promise((accept, reject) => {
      server.once('error', () => reject(new Error('Could not bind the loopback setup server. Choose a free --port, or --port 0.')));
      server.listen(requestedPort, '127.0.0.1', accept);
    });
    actualPort = server.address().port;
    log(`Open http://127.0.0.1:${actualPort}/ to create the release App for ${repository}.`);
    const result = await callback;
    if (result.error) throw result.error;
    await wait(result.app, org, repo);
    const lists = JSON.parse(await runGh(['api', '--paginate', '--slurp', `repos/${repository}/rulesets`]));
    const matches = lists.flat().filter((ruleset) => ruleset.name === creationRulesetName && ruleset.source_type === 'Repository');
    if (matches.length !== 1) throw new Error(`Expected exactly one repository '${creationRulesetName}' ruleset. Secrets are saved; finish the bypass manually.`);
    const endpoint = `repos/${repository}/rulesets/${matches[0].id}`;
    const ruleset = JSON.parse(await runGh(['api', endpoint]));
    await runGh(['api', '--method', 'PUT', endpoint, '--input', '-'], JSON.stringify(mergeRulesetBypass(ruleset, result.app.id)));
    log(`Release App setup complete for ${repository}. App installed, secrets saved, creation bypass added; immutable ruleset unchanged. This is not a per-server setup.`);
  } finally {
    server.closeAllConnections();
    await new Promise((accept) => server.close(accept));
  }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main().catch((error) => { console.error(`Release App setup failed: ${error.message}`); process.exitCode = 1; });
}
