import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { test } from 'node:test';

const guard = fileURLToPath(new URL('./check-release-source.sh', import.meta.url));

test('release tags accept main ancestors and refuse commits only on another branch', { skip: process.platform === 'win32' ? 'release guard runs in Linux CI; exercise it in Docker on Windows' : false }, () => {
  const dir = mkdtempSync(path.join(tmpdir(), 'release-policy-'));
  const git = (...args) => execFileSync('git', args, { cwd: dir, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim();
  try {
    git('init', '-b', 'main');
    git('config', 'user.name', 'Release fixture');
    git('config', 'user.email', 'release@example.test');
    writeFileSync(path.join(dir, 'source'), 'first');
    git('add', 'source'); git('commit', '-m', 'First reviewed commit');
    const first = git('rev-parse', 'HEAD');
    git('tag', '-a', 'v1.0.0', '-m', 'Reviewed ancestor');
    writeFileSync(path.join(dir, 'source'), 'second');
    git('commit', '-am', 'Second reviewed commit');
    const main = git('rev-parse', 'HEAD');
    git('update-ref', 'refs/remotes/origin/main', main);
    git('checkout', '-b', 'unreviewed', first);
    writeFileSync(path.join(dir, 'source'), 'unreviewed');
    git('commit', '-am', 'Unreviewed branch commit');
    const other = git('rev-parse', 'HEAD');
    for (const sha of [main, first, 'v1.0.0']) {
      const result = spawnSync('sh', [guard], { cwd: dir, env: { ...process.env, GITHUB_SHA: sha }, encoding: 'utf8' });
      assert.equal(result.status, 0, result.stderr);
    }
    const refused = spawnSync('sh', [guard], { cwd: dir, env: { ...process.env, GITHUB_SHA: other }, encoding: 'utf8' });
    assert.equal(refused.status, 1);
    assert.match(refused.stderr, /Release tags must point to a commit reachable from origin\/main/);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});
