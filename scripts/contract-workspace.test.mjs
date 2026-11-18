import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { existsSync, mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { DatabaseSync } from 'node:sqlite';
import test from 'node:test';
import { setTimeout as delay } from 'node:timers/promises';
import { withContractWorkspace } from './contract-workspace.mjs';

async function fixture(t, { stubborn = false } = {}) {
  const work = mkdtempSync(join(process.env.PANE_SCRATCH_DIR ?? tmpdir(), 'contract-cleanup-test-'));
  const db = new DatabaseSync(join(work, 'check.db'));
  db.exec('CREATE TABLE example (id INTEGER)');
  const server = spawn(process.execPath, ['--input-type=module', '-e', `
    import { DatabaseSync } from 'node:sqlite';
    const db = new DatabaseSync(process.argv[1]);
    ${stubborn ? "process.on('SIGTERM', () => {});" : ''}
    setInterval(() => {}, 1000);
    process.stdout.write('ready');
  `, join(work, 'check.db')], { stdio: ['ignore', 'pipe', 'ignore'] });
  t.after(async () => {
    if (server.exitCode === null && server.signalCode === null) {
      const exited = once(server, 'exit');
      server.kill('SIGKILL');
      await exited;
    }
    if (db.isOpen) db.close();
    rmSync(work, { recursive: true, force: true });
  });
  await once(server.stdout, 'data');
  return { work, db, server };
}

function assertClosed({ work, db, server }, keep = false) {
  assert.equal(db.isOpen, false, 'SQLite must be closed before cleanup returns');
  assert.ok(server.exitCode !== null || server.signalCode !== null, 'child must have exited before cleanup returns');
  assert.equal(existsSync(work), keep, 'workspace removal must finish before cleanup returns');
}

for (const mode of ['success', 'error', 'SIGINT', 'SIGTERM', 'keep']) {
  test(`M-033/R-01: cleanup with open SQLite and running child on ${mode}`, { timeout: 15000 }, async (t) => {
    const f = await fixture(t);
    const shutdown = withContractWorkspace(f.work, { keep: mode === 'keep' }, async (resources, signal) => {
      Object.assign(resources, { db: f.db, server: f.server });
      if (mode === 'error') throw new Error('intentional check failure');
      if (mode.startsWith('SIG')) {
        // Exercise the actual process signal handler portably, including on Windows.
        process.emit(mode);
        await delay(10000, undefined, { signal });
      }
    });
    if (mode === 'error') await assert.rejects(shutdown, /intentional check failure/);
    else if (mode.startsWith('SIG')) await assert.rejects(shutdown, (error) => {
      assert.equal(error.exitCode, mode === 'SIGINT' ? 130 : 143);
      return true;
    });
    else await shutdown;
    assertClosed(f, mode === 'keep');
  });
}

test('M-033/R-01: kill fallback waits for a child that ignores SIGTERM', {
  skip: process.platform === 'win32' && 'Windows terminates the child immediately on SIGTERM', timeout: 15000,
}, async (t) => {
  const f = await fixture(t, { stubborn: true });
  await withContractWorkspace(f.work, { timeoutMs: 100 }, async (resources) => {
    Object.assign(resources, { db: f.db, server: f.server });
  });
  assertClosed(f);
  assert.equal(f.server.signalCode, 'SIGKILL');
});

test('M-033/R-01: server spawn errors abort work and clean up SQLite', { timeout: 15000 }, async (t) => {
  const work = mkdtempSync(join(process.env.PANE_SCRATCH_DIR ?? tmpdir(), 'contract-cleanup-test-'));
  const db = new DatabaseSync(join(work, 'check.db'));
  t.after(() => {
    if (db.isOpen) db.close();
    rmSync(work, { recursive: true, force: true });
  });
  await assert.rejects(withContractWorkspace(work, {}, async (resources, signal) => {
    resources.db = db;
    resources.server = spawn('nonexistent-dashboard-contract-check-command');
    resources.server.on('error', (error) => resources.abort(new Error(`server spawn failed: ${error.message}`)));
    await delay(10000, undefined, { signal });
  }), /server spawn failed: .*ENOENT/);
  assert.equal(db.isOpen, false);
  assert.equal(existsSync(work), false);
});
