import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { resolve } from 'node:path';
import { readFileSync } from 'node:fs';
import test from 'node:test';

test('M-096: Astro proxies to the Go listener by default and respects API_ORIGIN', () => {
  const root = resolve(import.meta.dirname, '../..');
  const check = (override) => {
    const env = { ...process.env };
    delete env.API_ORIGIN;
    if (override) env.API_ORIGIN = override;
    return JSON.parse(execFileSync(process.execPath, ['--input-type=module', '-e', `
      const {default:config}=await import('./web/astro.config.mjs');
      const proxy=config.vite.server.proxy['/api'];
      const headers={origin:'http://127.0.0.1:4340'};
      proxy.configure({on(event, callback) {
        if(event==='proxyReq') callback({getHeader: name=>headers[name], setHeader: (name,value)=>headers[name]=value});
      }});
      process.stdout.write(JSON.stringify({target:proxy.target, origin:headers.origin}));
    `], { cwd: root, env, encoding: 'utf8' }));
  };
  const defaults = check();
  assert.deepEqual(defaults, { target: 'http://localhost:8090', origin: 'http://localhost:8090' });
  const compose = readFileSync(resolve(root, 'compose.dev.yaml'), 'utf8');
  assert.equal(compose.match(/^\s+APP_URL: (.+)$/m)?.[1], defaults.origin);
  assert.equal(compose.match(/^\s+API_ORIGIN: (.+)$/m)?.[1], defaults.target);
  assert.deepEqual(check('http://127.0.0.1:4690'), { target: 'http://127.0.0.1:4690', origin: 'http://127.0.0.1:4690' });
});
