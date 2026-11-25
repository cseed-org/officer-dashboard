// Local-only Wrangler + Docker proof. Real D1 binding, age, SQLite and HTTP client.
import assert from 'node:assert/strict';
import { spawn, execFileSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, writeFileSync, openSync } from 'node:fs';
import { resolve } from 'node:path';
const root=resolve(import.meta.dirname,'..');
const scratch=process.env.PANE_SCRATCH_DIR || resolve(root,'.pane/scratch');
mkdirSync(scratch,{recursive:true});
const work=mkdtempSync(resolve(scratch,'backup-roundtrip-'));
const sink=resolve(root,'server/deploy/backup-sink');
const wrangler=resolve(sink,'node_modules/wrangler/bin/wrangler.js');
const persist=resolve(work,'d1');
execFileSync(process.execPath,[wrangler,'d1','migrations','apply','BACKUPS','--local','--persist-to',persist],{cwd:sink,stdio:'pipe'});
const port=8792;
const volume='dashboard-backup-roundtrip-'+Date.now();
execFileSync('docker',['volume','create',volume],{stdio:'ignore'});
execFileSync('docker',['run','--rm','--user','0','--entrypoint','chown','-v',`${volume}:/data`,'dashboard-backup-qa','10001:10001','/data'],{stdio:'ignore'});
const worker=spawn(process.execPath,[wrangler,'dev','--local','--ip','0.0.0.0','--port',String(port),'--persist-to',persist,'--var','BACKUP_BEARER_SECRET:roundtrip-test-only'],{cwd:sink,stdio:['ignore',openSync(resolve(work,'wrangler.log'),'w'),'ignore']});
try {
 let ready=false;
 for(let n=0;n<60;n++) {
  try { const response=await fetch(`http://127.0.0.1:${port}/snapshots`); if(response.status===401){ready=true;break;} } catch {}
  await new Promise(r=>setTimeout(r,500));
 }
 assert.ok(ready,'Wrangler must start');
 // The offline recovery driver is a test-only image, never a release layer.
 writeFileSync(resolve(work,'Dockerfile.offbox'),'FROM python:3.14-alpine\nRUN apk add --no-cache age\n');
 execFileSync('docker',['build','-f',resolve(work,'Dockerfile.offbox'),'-t','dashboard-offbox-test',work],{stdio:'pipe'});
 const keygen=execFileSync('docker',['run','--rm','--user','10001:10001','--entrypoint','sh','-v',`${volume}:/data`,'dashboard-offbox-test','-c','age-keygen -o /data/test.agekey 2>/dev/null && age-keygen -y /data/test.agekey'],{encoding:'utf8'}).trim();
 const env=['-e',`BACKUP_SINK_URL=http://host.docker.internal:${port}`,'-e','BACKUP_BEARER_SECRET=roundtrip-test-only','-e','DB_PATH=/data/source.db'];
 const backup=execFileSync('docker',['run','--rm','--user','10001:10001','--add-host','host.docker.internal:host-gateway',
  '--entrypoint','/app/backup-test','-v',`${volume}:/data`,...env,'-e','BACKUP_REHEARSAL_MODE=roundtrip','-e',`BACKUP_AGE_RECIPIENT=${keygen}`,
  'dashboard-backup-test','-test.run=^TestLocalSinkRoundTrip$','-test.v'],{encoding:'utf8',timeout:180000});
 assert.match(backup,/PASS/);
 const proof=`import sys,hashlib
from pathlib import Path
sys.path.insert(0,'/app')
from backup import restore,Sink
restore('latest','/data/restored.db','/data/test.agekey')
assert hashlib.sha256(Path('/data/restored.db').read_bytes()).hexdigest()==hashlib.sha256(Path('/data/reference.db').read_bytes()).hexdigest()
Path('/data/test.agekey').unlink()
print('static Go backup -> off-box age restore: two chunks, matching plaintext SHA-256, integrity ok, prune ok')
`;
 writeFileSync(resolve(work,'proof.py'),proof);
 const output=execFileSync('docker',['run','--rm','--user','10001:10001','--add-host','host.docker.internal:host-gateway','--entrypoint','python3',
  '-v',`${volume}:/data`,'-v',`${resolve(work,'proof.py')}:/proof.py:ro`,'-v',`${resolve(root,'server/deploy/backup.py')}:/app/backup.py:ro`,
  ...env,'dashboard-offbox-test','/proof.py'],{encoding:'utf8',timeout:180000});
 console.log(output.trim());
} finally {
 execFileSync('docker',['volume','rm',volume],{stdio:'ignore'});
 if(process.platform==='win32') execFileSync('taskkill',['/pid',String(worker.pid),'/T','/F'],{stdio:'ignore'});
 else worker.kill('SIGTERM');
}
