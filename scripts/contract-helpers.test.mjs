import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtempSync, readdirSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import test from 'node:test';
import { z } from 'zod';
import { droppedKeys, executableName, spawnFailure } from './contract-helpers.mjs';
import { compareEndpoints } from './contract-parity.mjs';

test('M-033: Windows executable names and actual spawn errors survive diagnostics', () => {
  assert.equal(executableName('dashboard', 'win32'), 'dashboard.exe');
  assert.equal(executableName('dashboard', 'linux'), 'dashboard');
  const result = spawnSync('nonexistent-dashboard-contract-check-command', [], { encoding: 'utf8' });
  assert.match(spawnFailure('seed', result), /seed failed: .*ENOENT/);
});

test('M-033: contract checker reports the underlying seed spawn failure', () => {
  const work = mkdtempSync(join(process.env.PANE_SCRATCH_DIR ?? tmpdir(), 'contract-error-test-'));
  try {
    const result = spawnSync(process.execPath, ['scripts/contract-check.mjs', '--bin', 'nonexistent-dashboard-contract-check-command'], {
      cwd: resolve(import.meta.dirname, '..'), encoding: 'utf8',
      env: { ...process.env, PANE_SCRATCH_DIR: work },
    });
    assert.equal(result.status, 1);
    assert.match(result.stderr, /seed failed: .*ENOENT/);
    assert.deepEqual(readdirSync(work), [], 'a setup error must not leak its workspace');
  } finally { rmSync(work, { recursive: true, force: true }); }
});

test('M-149: undeclared fields at every depth fail while declared arbitrary records survive', () => {
  const schema = z.object({ items: z.array(z.object({ id: z.string(), metadata: z.record(z.string(), z.unknown()) })) });
  const raw = { items: [{ id: 'officer', phone: 'private', metadata: { free: { nested: true } } }], token: 'secret' };
  assert.deepEqual(droppedKeys(raw, schema.parse(raw)), ['$.items[0].phone', '$.token']);
  assert.deepEqual(droppedKeys({ items: [] }, schema.parse({ items: [] })), []);
});

test('M-058: parity detects response, request, query, method, path and missing endpoint drift', () => {
  const endpoint = { method: 'GET', path: '/api/example', auth: 'officer', query: z.object({ q: z.string() }), body: z.object({ id: z.string() }), response: z.object({ id: z.string() }) };
  const compare = (browser) => compareEndpoints({ example: endpoint }, browser, z.toJSONSchema);
  assert.deepEqual(compare({ example: { ...endpoint } }), []);
  for (const key of ['method', 'path', 'auth', 'body', 'query', 'response']) {
    const changed = typeof endpoint[key] === 'string' ? 'changed' : z.object({ id: z.number() });
    assert.deepEqual(compare({ example: { ...endpoint, [key]: changed } }), [`example.${key}`]);
  }
  assert.deepEqual(compare({}), ['example: missing endpoint']);
  assert.deepEqual(compare({ example: endpoint, extra: endpoint }), ['extra: missing endpoint']);
  assert.deepEqual(compareEndpoints({ search: endpoint }, { search: { ...endpoint, response: z.string(), path: '/wrong' }, authDev: endpoint }, z.toJSONSchema), ['search.path']);
});
