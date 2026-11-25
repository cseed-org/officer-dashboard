import { timingSafeEqual, createHash } from 'node:crypto';

type Env = { BACKUPS: D1Database; BACKUP_BEARER_SECRET: string };
const CHUNK = 1_900_000;
const json = (data: unknown, status = 200) => Response.json(data, {status, headers:{'Cache-Control':'no-store'}});
const fail = (status: number, message: string): never => { throw Object.assign(new Error(message), {status}); };
const sha = (s: string) => createHash('sha256').update(s).digest();
async function bounded(request: Request, max: number) {
 const reader = request.body?.getReader();
 if (!reader) return new Uint8Array();
 const parts: Uint8Array[] = []; let size=0;
 while (true) {
  const {done,value} = await reader.read(); if (done) break;
  size += value.length;
  if (size > max) { await reader.cancel(); fail(413,'body too large'); }
  parts.push(value);
 }
 const result = new Uint8Array(size); let offset=0;
 for (const part of parts) { result.set(part,offset); offset+=part.length; }
 return result;
}
async function manifest(request: Request) {
 try { const m=JSON.parse(new TextDecoder().decode(await bounded(request,2048))); if (!m || typeof m!=='object' || Array.isArray(m)) fail(400,'invalid JSON object'); return m; }
 catch (e) { if ((e as any).status) throw e; fail(400,'invalid JSON'); }
}
async function usage(db: D1Database) {
 const q = await db.prepare(`SELECT COALESCE(sum(allocation),0) AS bytes, count(*) AS snapshots,
 COALESCE((SELECT writes FROM budgets WHERE day=date('now')),0) AS writes,
 COALESCE((SELECT uploads FROM budgets WHERE day=date('now')),0) AS uploads FROM snapshots`).all();
 return {...q.results[0], physical_bytes:q.meta.size_after, write_limit:40000, byte_limit:450000000};
}
export default {
 async fetch(request: Request, env: Env): Promise<Response> {
  const authorization = request.headers.get('Authorization') || '';
  if (!env.BACKUP_BEARER_SECRET || new TextEncoder().encode(env.BACKUP_BEARER_SECRET).length < 32 || !timingSafeEqual(sha(authorization),sha('Bearer '+env.BACKUP_BEARER_SECRET))) return json({error:'unauthorized'},401);
  try {
   const db = env.BACKUPS, path = new URL(request.url).pathname, method=request.method;
   if (method==='GET' && path==='/usage') return json(await usage(db));
   if (method==='GET' && path==='/snapshots') return json((await db.prepare('SELECT id,kind,created_at,sha256,size,chunks FROM snapshots WHERE committed=1 ORDER BY created_at DESC,id DESC LIMIT 100').all()).results);
   if (method==='POST' && path==='/snapshots') {
    const m = await manifest(request);
    if (!/^[a-zA-Z0-9_-]{1,80}$/.test(m.id) || !['hourly','daily','d1-weekly'].includes(m.kind) || !/^[a-f0-9]{64}$/.test(m.sha256)
      || !Number.isInteger(m.size) || m.size<1 || m.size>13*CHUNK || m.chunks!==Math.ceil(m.size/CHUNK)) fail(400,'invalid manifest');
    const allocation=2*(Math.ceil(m.size/4096)*4096 + m.chunks*8192 + 16384);
    // Check current physical usage without treating reusable deleted pages as a new allocation.
    // Logical lifetime reservations still enforce headroom atomically in the insert trigger.
    const u=await usage(db);
    if (Number(u.physical_bytes || 0)>450000000) fail(409,'physical storage budget exhausted');
    try {
     await db.prepare('INSERT INTO snapshots(id,kind,sha256,size,chunks,allocation,write_cost) VALUES(?,?,?,?,?,?,?)').bind(m.id,m.kind,m.sha256,m.size,m.chunks,allocation,100+20*m.chunks).run();
    } catch { fail(409,'snapshot exists or budget exhausted'); }
    return json({id:m.id,...await usage(db)},201);
   }
   if (method==='POST' && path==='/prune') {
    const m=await manifest(request), hourly=m.hourly===24?24:48;
    const reserve=m.reserve ?? 0;
    if (!Number.isSafeInteger(reserve) || reserve<0 || reserve>50000000) fail(400,'invalid reservation');
    // Bounded count/expiry cleanup; all deletion writes were reserved at creation.
    await db.prepare(`DELETE FROM snapshots WHERE (committed=0 AND created_at < strftime('%Y-%m-%dT%H:%M:%fZ','now','-2 hours'))
     OR id IN (SELECT id FROM (SELECT id,row_number() OVER(PARTITION BY kind ORDER BY created_at DESC,id DESC) AS rn,kind FROM snapshots WHERE committed=1)
       WHERE rn > CASE kind WHEN 'hourly' THEN ? WHEN 'daily' THEN 30 ELSE 4 END)`).bind(hourly).run();
    // Preserve the newest copy of each kind; evict older committed copies by
    // reservation as well as count so a growing database can keep backing up.
    if (reserve>0) await db.prepare(`WITH ranked AS (
      SELECT id,allocation,created_at,row_number() OVER(PARTITION BY kind ORDER BY created_at DESC,id DESC) AS rn
      FROM snapshots WHERE committed=1
    ), fitted AS (
      SELECT id,rn,sum(allocation) OVER(ORDER BY CASE WHEN rn=1 THEN 0 ELSE 1 END,created_at DESC,id DESC) AS used
      FROM ranked
    ) DELETE FROM snapshots WHERE id IN (SELECT id FROM fitted WHERE rn>1 AND used >
      450000000 - ? - COALESCE((SELECT sum(allocation) FROM snapshots WHERE committed=0),0))`).bind(reserve).run();
    return json(await usage(db));
   }
   const match=path.match(/^\/snapshots\/([a-zA-Z0-9_-]{1,80})(?:\/(commit|promote|chunks\/(\d{1,2})))?$/);
   if (!match) return json({error:'not found'},404);
   const [,id,action,num]=match;
   const s:any=await db.prepare('SELECT * FROM snapshots WHERE id=?').bind(id).first();
   if (!s) fail(404,'snapshot not found');
   if (action==='promote' && method==='POST' && s.committed && s.kind==='hourly') {
    await db.prepare("UPDATE snapshots SET kind='daily' WHERE id=? AND kind='hourly'").bind(id).run();
    return json({ok:true});
   }
   if (action?.startsWith('chunks/')) {
    const n=Number(num); if (n>=s.chunks) fail(400,'invalid chunk');
    if (method==='PUT') {
     if (s.committed) fail(409,'snapshot immutable');
     const body=await bounded(request,CHUNK);
     if (body.length!==Math.min(CHUNK,s.size-n*CHUNK)) fail(400,'chunk length mismatch');
     // Immutable chunks: retries must use a new snapshot id, never spend unreserved writes.
     try { await db.prepare('INSERT INTO chunks(snapshot_id,n,blob) VALUES(?,?,?)').bind(id,n,body).run(); }
     catch { fail(409,'chunk exists'); }
     return json({ok:true});
    }
    if (method==='GET' && s.committed) {
     const row:any=await db.prepare('SELECT blob FROM chunks WHERE snapshot_id=? AND n=?').bind(id,n).first();
     return new Response(new Uint8Array(row.blob),{headers:{'Content-Type':'application/octet-stream','Cache-Control':'no-store'}});
    }
   }
   if (action==='commit' && method==='POST') {
    if (s.committed) return json({id,sha256:s.sha256});
    // Bound memory to one chunk and queries to <= 17. Hash actual stored ciphertext.
    const hash=createHash('sha256'); let size=0;
    for (let n=0;n<s.chunks;n++) {
     const row:any=await db.prepare('SELECT blob FROM chunks WHERE snapshot_id=? AND n=?').bind(id,n).first();
     if (!row) fail(409,'missing chunk');
     const bytes=new Uint8Array(row.blob); hash.update(bytes); size+=bytes.length;
    }
    if (size!==s.size || hash.digest('hex')!==s.sha256) fail(422,'sha256 mismatch');
    await db.prepare('UPDATE snapshots SET committed=1 WHERE id=? AND committed=0').bind(id).run();
    return json({id,sha256:s.sha256});
   }
   return json({error:'not found'},404);
  } catch (e) { return json({error:(e as any).status ? (e as Error).message : 'sink unavailable'},(e as any).status || 503); }
 }
};
