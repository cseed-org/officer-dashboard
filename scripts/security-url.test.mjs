import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';
import { buildSync, transformSync } from 'esbuild';
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

test('project detail omits script links while retaining HTTPS links', () => {
 const result = buildSync({stdin:{contents:`
  import { h } from 'preact';
  import { render } from 'preact-render-to-string';
  import { BuildspaceDetail } from './src/ui/views/buildspace/Detail';
  const project = {id:'fixture',name:'Project',track:'Software',stage:'Ideating',description:'Test',links:['javascript:alert(1)','https://example.org/project'],mentorIds:[],archivedAt:null};
  const b = {projects:[project],people:[],mentors:[],activity:[],cohorts:[],weekStart:'2026-01-01',today:'2026-01-02'};
  module.exports = render(h(BuildspaceDetail,{b,project,reload:()=>{},onPerson:()=>{},readOnly:true}));
 `,resolveDir:new URL('../web',import.meta.url).pathname.replace(/^\/([A-Za-z]:)/,'$1'),loader:'tsx'},loader:{'.css':'empty'},bundle:true,write:false,platform:'node',format:'cjs',jsx:'automatic',jsxImportSource:'preact'});
 const sandbox={module:{exports:{}},URL,console,setTimeout,clearTimeout};
 vm.runInNewContext(result.outputFiles[0].text,sandbox);
 assert.ok(!sandbox.module.exports.includes('href="javascript:'));
 assert.ok(sandbox.module.exports.includes('href="https://example.org/project"'));
});
