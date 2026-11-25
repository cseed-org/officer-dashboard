import { beforeAll, afterAll, expect, test } from 'vitest';
import { Miniflare } from 'miniflare';
import { build } from 'esbuild';
import { readFileSync } from 'node:fs';
let mf: Miniflare;
beforeAll(async () => {
 const bundle = await build({entryPoints:['src/index.ts'],bundle:true,write:false,format:'esm',platform:'node',external:['node:crypto']});
 mf = new Miniflare({modules:true,script:bundle.outputFiles[0].text,compatibilityDate:'2026-07-30',compatibilityFlags:['nodejs_compat'],d1Databases:['BACKUPS'],bindings:{BACKUP_BEARER_SECRET:'test-secret'}});
 await (await mf.getD1Database('BACKUPS')).exec(readFileSync('migrations/0001.sql','utf8').replace(/--[^\n]*/g,'').replace(/\s+/g,' '));
}, 60000);
afterAll(async () => { await mf?.dispose(); });
const call = (path:string, method='GET', body?:unknown) => mf.dispatchFetch('http://sink'+path,{method,headers:{Authorization:'Bearer test-secret','Content-Type':'application/json'},body:body === undefined ? undefined : JSON.stringify(body)});
test('unauthenticated callers cannot list snapshots', async () => {
 expect((await mf.dispatchFetch('http://sink/snapshots')).status).toBe(401);
});
test('commits only verified chunks and serves immutable ciphertext', async () => {
 const m={id:'roundtrip',kind:'hourly',size:3,chunks:1,sha256:'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad'};
 expect((await call('/snapshots','POST',m)).status).toBe(201);
 expect((await call('/snapshots/roundtrip/commit','POST')).status).toBe(409);
 expect((await mf.dispatchFetch('http://sink/snapshots/roundtrip/chunks/0',{method:'PUT',headers:{Authorization:'Bearer test-secret'},body:'abc'})).status).toBe(200);
 expect((await call('/snapshots/roundtrip/commit','POST')).status).toBe(200);
 expect(await (await call('/snapshots/roundtrip/chunks/0')).text()).toBe('abc');
 expect((await mf.dispatchFetch('http://sink/snapshots/roundtrip/chunks/0',{method:'PUT',headers:{Authorization:'Bearer test-secret'},body:'abc'})).status).toBe(409);
 expect((await call('/snapshots/roundtrip/promote','POST')).status).toBe(200);
 expect((await (await call('/snapshots')).json() as any[]).find(s=>s.id==='roundtrip').kind).toBe('daily');
});
test('rejects incorrect hashes without exposing uncommitted data', async () => {
 const m={id:'corrupt',kind:'hourly',size:3,chunks:1,sha256:'0'.repeat(64)};
 expect((await call('/snapshots','POST',m)).status).toBe(201);
 await mf.dispatchFetch('http://sink/snapshots/corrupt/chunks/0',{method:'PUT',headers:{Authorization:'Bearer test-secret'},body:'abc'});
 expect((await call('/snapshots/corrupt/commit','POST')).status).toBe(422);
 expect((await call('/snapshots/corrupt/chunks/0')).status).toBe(404);
 expect((await (await call('/snapshots')).json() as any[]).some(s=>s.id==='corrupt')).toBe(false);
});
test('rejects oversize and malformed manifests before writing', async () => {
 expect((await call('/snapshots','POST',{id:'too-big',kind:'hourly',size:24700001,chunks:14,sha256:'0'.repeat(64)})).status).toBe(400);
 expect((await call('/snapshots','POST',{id:'../../escape',kind:'hourly',size:1,chunks:1,sha256:'0'.repeat(64)})).status).toBe(400);
});
test('retention falls back to 24 hourly copies and keeps the daily snapshot', async () => {
 for (let n=0;n<26;n++) {
  const id='retention'+n;
  expect((await call('/snapshots','POST',{id,kind:'hourly',size:3,chunks:1,sha256:'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad'})).status).toBe(201);
  await mf.dispatchFetch(`http://sink/snapshots/${id}/chunks/0`,{method:'PUT',headers:{Authorization:'Bearer test-secret'},body:'abc'});
  expect((await call(`/snapshots/${id}/commit`,'POST')).status).toBe(200);
 }
 expect((await call('/prune','POST',{hourly:24})).status).toBe(200);
 const snapshots=await (await call('/snapshots')).json() as any[];
 expect(snapshots.filter(s=>s.kind==='hourly')).toHaveLength(24);
 expect(snapshots.filter(s=>s.kind==='daily')).toHaveLength(1);
});
test('daily and weekly retention preserves 30 and 4 committed snapshots', async () => {
 for (const [kind,total] of [['daily',32],['d1-weekly',6]] as const) {
  for (let n=0;n<total;n++) {
   const id=kind+n;
   expect((await call('/snapshots','POST',{id,kind,size:3,chunks:1,sha256:'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad'})).status).toBe(201);
   await mf.dispatchFetch(`http://sink/snapshots/${id}/chunks/0`,{method:'PUT',headers:{Authorization:'Bearer test-secret'},body:'abc'});
   expect((await call(`/snapshots/${id}/commit`,'POST')).status).toBe(200);
  }
 }
 await call('/prune','POST',{hourly:48});
 const snapshots=await (await call('/snapshots')).json() as any[];
 expect(snapshots.filter(s=>s.kind==='daily')).toHaveLength(30);
 expect(snapshots.filter(s=>s.kind==='d1-weekly')).toHaveLength(4);
});
test('concurrent reservations cannot overcommit storage', async () => {
 const responses=await Promise.all(Array.from({length:12},(_,n)=>call('/snapshots','POST',{id:'budget'+n,kind:'hourly',size:24700000,chunks:13,sha256:'0'.repeat(64)})));
 expect(responses.some(r=>r.status===409)).toBe(true);
 const usage=await (await call('/usage')).json() as any;
 expect(usage.bytes).toBeLessThanOrEqual(450000000);
});
