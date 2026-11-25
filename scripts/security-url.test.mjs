import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';
import { transformSync } from 'esbuild';
const source = readFileSync(new URL('../web/src/lib/url.ts', import.meta.url), 'utf8');
const code = transformSync(source, { loader: 'ts', format: 'cjs' }).code;
const ctx = { module: { exports: {} }, URL };
vm.runInNewContext(code, ctx);
const { internalPath, safeUrl } = ctx.module.exports;
for (const raw of ['/.//evil.example', '/%2e//evil.example', '/x/..//evil.example']) {
 test(`resolved URL rejects ${raw}`, () => {
  assert.equal(internalPath(raw, 'https://dashboard.example'), null);
  assert.equal(safeUrl(raw, 'https://dashboard.example'), null);
 });
 test(`login rejects ${raw}`, () => {
  const page = readFileSync(new URL('../web/src/pages/login.astro', import.meta.url), 'utf8');
  const literal = page.match(/const SCRIPT = (`[^`]+`);/s)[1];
  const script = vm.runInNewContext(literal, { MESSAGES: {} });
  let destination;
  vm.runInNewContext(script, {
   URL, URLSearchParams,
   location: { origin: 'https://dashboard.example', href: 'https://dashboard.example/login', search: '?next='+encodeURIComponent(raw), replace: p => { destination = p; } },
   document: { querySelector: () => null, querySelectorAll: () => [] },
   fetch: () => Promise.resolve({ok: true}),
  });
  return new Promise(resolve => setImmediate(resolve)).then(() => assert.equal(destination, '/'));
 });
}
