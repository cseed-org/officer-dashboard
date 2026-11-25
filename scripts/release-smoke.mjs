#!/usr/bin/env node
// Checks the real release binary without STATIC_DIR, dev authentication or seeded users.
import assert from 'node:assert/strict';
import { spawn, spawnSync } from 'node:child_process';
import { once } from 'node:events';
import { existsSync, mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { createServer } from 'node:net';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';

const bin = process.argv[process.argv.indexOf('--bin') + 1];
if (!process.argv.includes('--bin') || !bin) throw new Error('usage: node scripts/release-smoke.mjs --bin server/bin/dashboard');
const root = resolve(import.meta.dirname, '..');
const work = mkdtempSync(join(process.env.PANE_SCRATCH_DIR ?? tmpdir(), 'release-smoke-'));
const listener = createServer().listen(0, '127.0.0.1');
await once(listener, 'listening');
const port = listener.address().port;
await new Promise((done) => listener.close(done));
const origin = `http://127.0.0.1:${port}`;
const env = { ...process.env, ADDR: `127.0.0.1:${port}`, APP_URL: origin, DB_PATH: join(work, 'smoke.db') };
for (const key of ['STATIC_DIR', 'DEV_AUTH', 'DEV_AUTH_KEY']) delete env[key];
// A release must refuse fixtures even when asked to overwrite a database.
const seed = spawnSync(resolve(bin), ['seed', '-force', '-reset'], { env, encoding: 'utf8' });
assert.ifError(seed.error);
assert.notEqual(seed.status, 0, 'release excludes seed fixtures');
assert.equal(existsSync(env.DB_PATH), false, 'release seed cannot create a database');
const server = spawn(resolve(bin), ['serve'], { env, stdio: ['ignore', 'ignore', 'inherit'] });
let spawnError;
server.on('error', (error) => { spawnError = error; });
try {
  for (let attempt = 0; ; attempt++) {
    if (spawnError) throw spawnError;
    if (server.exitCode !== null) throw new Error(`release exited: ${server.exitCode}`);
    try { if ((await fetch(`${origin}/api/health`)).ok) break; } catch {}
    if (attempt > 100) throw new Error('release server did not start');
    await new Promise((done) => setTimeout(done, 50));
  }
  const page = await fetch(`${origin}/login/`);
  assert.equal(page.status, 200, 'embedded login page');
  const html = await page.text();
  assert.equal(html, readFileSync(join(root, 'web/dist/login/index.html'), 'utf8'));
  const asset = html.match(/(?:src|href)="(\/_astro\/[^"?#]+\.(?:js|css))"/)?.[1];
  assert.ok(asset, 'login references a built JS or CSS asset');
  const response = await fetch(origin + asset);
  assert.equal(response.status, 200, `embedded asset ${asset}`);
  assert.deepEqual(Buffer.from(await response.arrayBuffer()), readFileSync(join(root, 'web/dist', asset)));
  assert.equal((await fetch(`${origin}/auth/dev`, { redirect: 'manual' })).status, 404, 'release excludes dev login');
  console.log(`Embedded release serves /login/ and ${asset} without STATIC_DIR or dev auth.`);
} finally {
  if (server.pid && server.exitCode === null) {
    const exited = once(server, 'exit');
    server.kill('SIGTERM');
    await exited;
  }
  rmSync(work, { recursive: true, force: true });
}
