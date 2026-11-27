import assert from 'node:assert/strict';
import { generateKeyPairSync, verify } from 'node:crypto';
import { spawnSync } from 'node:child_process';
import { once } from 'node:events';
import { request } from 'node:http';
import { test } from 'node:test';
import { appJwt, buildManifest, jwtClaims, main, mergeRulesetBypass, setupServer, waitForInstallation } from './setup-release-app.mjs';

test('manifest requests only the release permissions and a loopback callback', () => {
  assert.deepEqual(buildManifest({ org: 'cseed-org', repo: 'officer-dashboard', port: 54321 }), {
    name: 'cseed-release', url: 'https://github.com/cseed-org/officer-dashboard', public: false,
    hook_attributes: { active: false }, redirect_url: 'http://127.0.0.1:54321/callback',
    default_permissions: { contents: 'write', pull_requests: 'write', metadata: 'read' },
  });
  assert.equal(buildManifest({ org: 'org', repo: 'repo', name: 'other-release', port: 0 }).name, 'other-release');
});

test('manifest rejects malformed repository paths and unsafe ports', () => {
  for (const options of [
    { org: '../other' }, { repo: 'repo/path' }, { repo: '..' }, { org: '' },
    { port: -1 }, { port: 65536 }, { port: 1.5 }, { port: NaN }, { name: 'bad\nname' },
  ]) assert.throws(() => buildManifest({ org: 'org', repo: 'repo', port: 1234, ...options }));
});

const owners = {
  id: 123, name: 'Version tags - owners create', target: 'tag', enforcement: 'active',
  conditions: { ref_name: { include: ['refs/tags/v*'], exclude: [] } }, rules: [{ type: 'creation' }],
  bypass_actors: [
    { actor_id: null, actor_type: 'OrganizationAdmin', bypass_mode: 'always' },
    { actor_id: 99, actor_type: 'Team', bypass_mode: 'always' },
  ],
};

test('ruleset merge preserves owners, other actors, conditions and enforcement without mutation', () => {
  const original = structuredClone(owners);
  const merged = mergeRulesetBypass(owners, 456);
  assert.deepEqual(owners, original);
  assert.deepEqual(merged.bypass_actors, [...owners.bypass_actors, { actor_id: 456, actor_type: 'Integration', bypass_mode: 'always' }]);
  assert.deepEqual(merged.conditions, owners.conditions);
  assert.deepEqual(merged.rules, owners.rules);
  assert.equal(merged.enforcement, 'active');
  assert.equal(merged.id, undefined, 'read-only API response fields must not enter PUT payload');
  assert.deepEqual(mergeRulesetBypass(merged, 456), merged, 'repeated merge adds no duplicate');
});

test('ruleset merge refuses immutable rules and updates only the matching Integration actor', () => {
  assert.throws(() => mergeRulesetBypass({ ...owners, name: 'Version tags - immutable' }, 456));
  assert.throws(() => mergeRulesetBypass({ ...owners, rules: [{ type: 'update' }, { type: 'deletion' }] }, 456));
  assert.throws(() => mergeRulesetBypass({ ...owners, target: 'branch' }, 456));
  assert.throws(() => mergeRulesetBypass(owners, 0));
  const ruleset = { ...owners, bypass_actors: [
    ...owners.bypass_actors,
    { actor_id: 456, actor_type: 'Team', bypass_mode: 'always' },
    { actor_id: 456, actor_type: 'Integration', bypass_mode: 'pull_request' },
  ] };
  assert.equal(mergeRulesetBypass(ruleset, 456).bypass_actors.length, 4);
  assert.equal(mergeRulesetBypass(ruleset, 456).bypass_actors[3].bypass_mode, 'always');
  assert.equal(ruleset.bypass_actors[3].bypass_mode, 'pull_request');
});

test('JWT claims allow clock skew and expire within GitHub’s ten-minute limit', () => {
  assert.deepEqual(jwtClaims(456, 1_800_000_000_123), { iss: '456', iat: 1_799_999_940, exp: 1_800_000_540 });
  assert.throws(() => jwtClaims(-1));
  assert.throws(() => jwtClaims(456, NaN));
});

test('App JWT has a verifiable RS256 signature, with no private key in its claims', () => {
  const { privateKey, publicKey } = generateKeyPairSync('rsa', { modulusLength: 2048 });
  const jwt = appJwt(456, privateKey, 1_800_000_000_000);
  const [header, claims, signature] = jwt.split('.');
  assert.deepEqual(JSON.parse(Buffer.from(header, 'base64url')), { alg: 'RS256', typ: 'JWT' });
  assert.deepEqual(JSON.parse(Buffer.from(claims, 'base64url')), jwtClaims(456, 1_800_000_000_000));
  assert.equal(verify('RSA-SHA256', Buffer.from(`${header}.${claims}`), publicKey, Buffer.from(signature, 'base64url')), true);
  assert.equal(verify('RSA-SHA256', Buffer.from(`${header}.${claims}x`), publicKey, Buffer.from(signature, 'base64url')), false);
});

test('loopback form has automatic POST and fallback; callback rejects bad state and replay', async (t) => {
  let calls = 0;
  const server = setupServer({ state: 'unpredictable-state-fixture', org: 'cseed-org',
    manifest: () => buildManifest({ org: 'cseed-org', repo: 'officer-dashboard', port: server.address().port }),
    onCode: async (code) => { calls++; assert.equal(code, 'temporary-code'); },
    status: () => '<p>Install the App on the repository.</p>',
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  t.after(() => { server.closeAllConnections(); server.close(); });
  const base = `http://127.0.0.1:${server.address().port}`;
  const page = await fetch(base);
  assert.equal(page.headers.get('cache-control'), 'no-store');
  assert.equal(page.headers.get('referrer-policy'), 'no-referrer');
  const form = await page.text();
  assert.match(form, /form method="post" action="https:\/\/github.com\/organizations\/cseed-org\/settings\/apps\/new\?state=/);
  assert.match(form, /textarea name="manifest" hidden/);
  assert.match(form, /button type="submit"/);
  assert.match(form, /document.forms\[0\].submit\(\)/);
  for (const query of ['code=temporary-code', 'code=temporary-code&state=wrong', 'state=unpredictable-state-fixture']) {
    assert.equal((await fetch(`${base}/callback?${query}`)).status, 400);
  }
  assert.equal(calls, 0);
  const callback = `${base}/callback?code=temporary-code&state=unpredictable-state-fixture`;
  const accepted = await fetch(callback);
  assert.equal(accepted.status, 200);
  assert.doesNotMatch(await accepted.text(), /temporary-code|unpredictable-state-fixture/);
  assert.equal((await fetch(callback)).status, 409);
  assert.equal(calls, 1);
  assert.equal((await fetch(base, { method: 'POST' })).status, 400);
  const rebound = await new Promise((accept, reject) => {
    const req = request(base, { headers: { Host: 'evil.example' } }, (res) => { res.resume(); accept(res.statusCode); });
    req.on('error', reject);
    req.end();
  });
  assert.equal(rebound, 400);
});

test('dry-run works without gh or credentials and prints only the plan', () => {
  const result = spawnSync(process.execPath, ['scripts/setup-release-app.mjs', '--org', 'cseed-org', '--repo', 'officer-dashboard', '--port', '0', '--dry-run'], {
    cwd: new URL('..', import.meta.url), encoding: 'utf8', env: { ...process.env, PATH: '', GH_TOKEN: '', GITHUB_TOKEN: '' },
  });
  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /"redirect_url": "http:\/\/127.0.0.1:0\/callback"/);
  assert.match(result.stdout, /no server, API calls or writes/);
  assert.match(result.stdout, /gh secret set RELEASE_APP_PRIVATE_KEY.*stdin/);
  assert.match(result.stdout, /immutable ruleset/);
  assert.doesNotMatch(result.stdout, /BEGIN .*PRIVATE KEY/);
});

test('existing secrets stop setup; partial secrets require intentional recovery', async () => {
  const calls = [];
  const messages = [];
  await main(['--org', 'org', '--repo', 'repo', '--port', '0'], {
    runGh: async (args) => { calls.push(args); return JSON.stringify([{ name: 'RELEASE_APP_ID' }, { name: 'RELEASE_APP_PRIVATE_KEY' }]); },
    request: () => assert.fail('Existing secrets must not create another App'), log: (message) => messages.push(message),
  });
  assert.equal(calls.length, 1);
  assert.match(messages.join('\n'), /Both release App secrets already exist/);
  await assert.rejects(main(['--org', 'org', '--repo', 'repo', '--port', '0'], {
    runGh: async () => JSON.stringify([{ name: 'RELEASE_APP_ID' }]),
  }), /Only one release App secret exists/);
});

test('installation polling accepts only the one intended repository and revokes its lookup token', async () => {
  const { privateKey } = generateKeyPairSync('rsa', { modulusLength: 2048 });
  const app = { id: 456, pem: privateKey };
  let clock = 0, lookups = 0, revocations = 0;
  await waitForInstallation(app, 'cseed-org', 'officer-dashboard', {
    now: () => clock, pause: async (ms) => { clock += ms; },
    request: async (path, options) => {
      assert.equal(options.token.split('.').length, path.startsWith('/app/') ? 3 : 1);
      if (path.startsWith('/app/installations?')) return [
        { id: 1, account: { login: 'someone-else' }, repository_selection: 'selected' },
        { id: 2, account: { login: 'cseed-org' }, repository_selection: 'all' },
        { id: 3, account: { login: 'cseed-org' }, repository_selection: 'selected' },
      ];
      if (path === '/app/installations/3/access_tokens') {
        assert.deepEqual(options.body, { permissions: { metadata: 'read' } });
        return { token: 'short-lived-lookup-token' };
      }
      if (path.startsWith('/installation/repositories?')) {
        lookups++;
        return lookups === 1 ? { total_count: 2, repositories: [] } :
          { total_count: 1, repositories: [{ full_name: 'cseed-org/officer-dashboard' }] };
      }
      if (path === '/installation/token') { revocations++; assert.equal(options.method, 'DELETE'); return; }
      assert.fail(`Unexpected fixture API path: ${path}`);
    },
  });
  assert.equal(lookups, 2);
  assert.equal(revocations, 2);
  let requests = 0;
  await assert.rejects(waitForInstallation(app, 'cseed-org', 'officer-dashboard', {
    now: () => clock, pause: async () => { clock += 600_000; },
    request: async () => { requests++; return []; },
  }), /Timed out after 10 minutes.*bypass was not added/);
  assert.equal(requests, 1);
});

test('mocked setup sends secrets only on stdin and modifies only the creation ruleset after installation', { timeout: 5000 }, async () => {
  const marker = 'private-key-memory-fixture';
  const calls = [], logs = [];
  let resolveUrl, finishInstallation;
  const ready = new Promise((accept) => { resolveUrl = accept; });
  const installed = new Promise((accept) => { finishInstallation = accept; });
  const task = main(['--org', 'cseed-org', '--repo', 'officer-dashboard', '--port', '0', '--force'], {
    log: (message) => { logs.push(message); if (message.startsWith('Open http://')) resolveUrl(message.match(/http:\/\/\S+/)[0]); },
    request: async (path, options) => {
      assert.equal(path, '/app-manifests/temporary-code/conversions');
      assert.equal(options.method, 'POST');
      return { id: 456, slug: 'cseed-release', pem: marker };
    },
    wait: async (app, org, repo) => {
      assert.deepEqual([app.id, app.pem, org, repo], [456, marker, 'cseed-org', 'officer-dashboard']);
      await installed;
    },
    runGh: async (args, input) => {
      calls.push({ args, input });
      assert.ok(!args.some((arg) => arg.includes(marker)));
      if (args[0] === 'secret' && args[1] === 'list') return JSON.stringify([{ name: 'RELEASE_APP_ID' }, { name: 'RELEASE_APP_PRIVATE_KEY' }]);
      if (args[0] === 'secret') return '';
      if (args.includes('--slurp')) return JSON.stringify([[{ ...owners, source_type: 'Repository' }], [{ id: 987, name: 'Version tags - immutable', source_type: 'Repository' }]]);
      if (args.includes('PUT')) { assert.deepEqual(JSON.parse(input), mergeRulesetBypass(owners, 456)); return '{}'; }
      assert.deepEqual(args, ['api', 'repos/cseed-org/officer-dashboard/rulesets/123']);
      return JSON.stringify(owners);
    },
  });
  const url = await ready;
  const form = await (await fetch(url)).text();
  const state = form.match(/new\?state=([a-f\d]+)/)[1];
  const response = await fetch(`${url}callback?code=temporary-code&state=${state}`);
  const page = await response.text();
  assert.match(page, /https:\/\/github.com\/apps\/cseed-release\/installations\/new/);
  assert.doesNotMatch(page, new RegExp(marker));
  assert.ok(!calls.some(({ args }) => args.includes('PUT')), 'installation must precede the ruleset mutation');
  finishInstallation();
  await task;
  assert.deepEqual(calls.filter(({ args }) => args[0] === 'secret' && args[1] === 'set').map(({ args, input }) => [args[2], input]), [
    ['RELEASE_APP_ID', '456'], ['RELEASE_APP_PRIVATE_KEY', marker],
  ]);
  assert.ok(!logs.join('\n').includes(marker));
  assert.equal(calls.filter(({ args }) => args.includes('PUT')).length, 1);
});
