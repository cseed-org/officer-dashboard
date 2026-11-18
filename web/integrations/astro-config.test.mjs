import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { resolve } from 'node:path';
import test from 'node:test';

test('M-096: Astro proxies to the Go listener by default and respects API_ORIGIN', () => {
  const root = resolve(import.meta.dirname, '../..');
  const check = (override) => {
    const env = { ...process.env };
    delete env.API_ORIGIN;
    if (override) env.API_ORIGIN = override;
    return execFileSync(process.execPath, ['--input-type=module', '-e', "const {default:config}=await import('./web/astro.config.mjs'); process.stdout.write(config.vite.server.proxy['/api'].target);"], { cwd: root, env, encoding: 'utf8' });
  };
  assert.equal(check(), 'http://127.0.0.1:8090');
  assert.equal(check('http://127.0.0.1:4690'), 'http://127.0.0.1:4690');
});
