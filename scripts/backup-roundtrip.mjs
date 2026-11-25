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
const worker=spawn(process.execPath,[wrangler,'dev','--local','--ip','0.0.0.0','--port',String(port),'--persist-to',persist,'--var','BACKUP_BEARER_SECRET:roundtrip-test-secret-at-least-32-bytes'],{cwd:sink,stdio:['ignore',openSync(resolve(work,'wrangler.log'),'w'),'ignore']});
try {
 let ready=false;
 for(let n=0;n<60;n++) {
  try { const response=await fetch(`http://127.0.0.1:${port}/snapshots`); if(response.status===401){ready=true;break;} } catch {}
  await new Promise(r=>setTimeout(r,500));
 }
 assert.ok(ready,'Wrangler must start');
 // The offline recovery driver is a test-only image, never a release layer.
 writeFileSync(resolve(work,'Dockerfile.offbox'),'FROM python:3.14-alpine\nRUN apk add --no-cache age sqlite\n');
 execFileSync('docker',['build','-f',resolve(work,'Dockerfile.offbox'),'-t','dashboard-offbox-test',work],{stdio:'pipe'});
 const keygen=execFileSync('docker',['run','--rm','--user','10001:10001','--entrypoint','sh','-v',`${volume}:/data`,'dashboard-offbox-test','-c','age-keygen -o /data/test.agekey 2>/dev/null && age-keygen -y /data/test.agekey'],{encoding:'utf8'}).trim();
 const env=['-e',`BACKUP_SINK_URL=http://host.docker.internal:${port}`,'-e','BACKUP_BEARER_SECRET=roundtrip-test-secret-at-least-32-bytes','-e','DB_PATH=/data/source.db'];
 const backup=execFileSync('docker',['run','--rm','--user','10001:10001','--add-host','host.docker.internal:host-gateway',
  '--entrypoint','/app/backup-test','-v',`${volume}:/data`,...env,'-e','BACKUP_REHEARSAL_MODE=roundtrip','-e',`BACKUP_AGE_RECIPIENT=${keygen}`,
  'dashboard-backup-test','-test.run=^TestLocalSinkRoundTrip$','-test.v'],{encoding:'utf8',timeout:180000});
 assert.match(backup,/PASS/);
 const proof=`import sys,hashlib,subprocess,os,gzip,sqlite3,stat
from pathlib import Path
sys.path.insert(0,'/app')
from backup import restore,Sink
restore('latest','/data/restored.db','/data/test.agekey')
# The decrypted snapshot's SHA-256 is verified inside restore(); afterwards the
# official restore adds the R2 re-seed marker, so compare logical content.
def dump(path,skip=()):
 with sqlite3.connect(path) as db:
  tables=[r[0] for r in db.execute("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")]
  return {t:(db.execute('SELECT sql FROM sqlite_master WHERE name=?',(t,)).fetchone()[0],sorted(db.execute(f'SELECT * FROM "{t}"').fetchall(),key=repr)) for t in tables if t not in skip}
assert dump('/data/restored.db',('r2_reseed',))==dump('/data/reference.db',('r2_reseed',))
with sqlite3.connect('/data/restored.db') as db:
 assert db.execute('PRAGMA integrity_check').fetchone()[0]=='ok'
 marker=db.execute('SELECT count(*) FROM r2_reseed WHERE fallback_at=0').fetchone()
 assert marker==(1,),'restore must mark the R2 ledger for re-seeding'
restore('latest','/data/restored.age',None,encrypted_only=True)
plain=gzip.decompress(subprocess.check_output(['age','-d','-i','/data/test.agekey','/data/restored.age']))
assert hashlib.sha256(plain).hexdigest()==hashlib.sha256(Path('/data/reference.db').read_bytes()).hexdigest()
legacy=Path('/data/legacy');legacy.mkdir()
recipient=subprocess.check_output(['age-keygen','-y','/data/test.agekey'],text=True).strip()
hook=Path('/data/copy-hook');hook.write_text('#!/bin/sh\\ncp "$1" /data/remote.age\\n');hook.chmod(0o700)
env=dict(os.environ,DB_PATH='/data/source.db',BACKUP_DIR=str(legacy),BACKUP_AGE_RECIPIENT=recipient,BACKUP_REMOTE=str(hook))
subprocess.run(['sh','/legacy-backup'],env=env,check=True,capture_output=True)
artifacts=list(legacy.iterdir());assert len(artifacts)==1 and artifacts[0].name.endswith('.db.gz.age')
assert stat.S_IMODE(artifacts[0].stat().st_mode)==0o600
assert Path('/data/remote.age').read_bytes()==artifacts[0].read_bytes()
missing=dict(env,BACKUP_DIR='/data/missing-key',BACKUP_AGE_RECIPIENT='')
assert subprocess.run(['sh','/legacy-backup'],env=missing,capture_output=True).returncode!=0
assert not Path('/data/missing-key').exists()
compressed=subprocess.check_output(['age','-d','-i','/data/test.agekey',str(artifacts[0])])
Path('/data/legacy-restored.db').write_bytes(gzip.decompress(compressed))
with sqlite3.connect('/data/legacy-restored.db') as db:
 assert db.execute('PRAGMA integrity_check').fetchone()[0]=='ok'
 assert db.execute('SELECT length(value) FROM records').fetchone()[0]==2100000
Path('/data/test.agekey').unlink()
print('legacy backup: ciphertext only, mode 0600, independent age decrypt and integrity ok')
print('static Go backup -> off-box age restore: two chunks, matching plaintext SHA-256, integrity ok, prune ok')
`;
 writeFileSync(resolve(work,'proof.py'),proof);
 const output=execFileSync('docker',['run','--rm','--user','10001:10001','--add-host','host.docker.internal:host-gateway','--entrypoint','python3',
  '-v',`${volume}:/data`,'-v',`${resolve(work,'proof.py')}:/proof.py:ro`,'-v',`${resolve(root,'server/deploy/backup.py')}:/app/backup.py:ro`,'-v',`${resolve(root,'server/scripts/backup')}:/legacy-backup:ro`,
  ...env,'dashboard-offbox-test','/proof.py'],{encoding:'utf8',timeout:180000});
 console.log(output.trim());
} finally {
 execFileSync('docker',['volume','rm',volume],{stdio:'ignore'});
 if(process.platform==='win32') execFileSync('taskkill',['/pid',String(worker.pid),'/T','/F'],{stdio:'ignore'});
 else worker.kill('SIGTERM');
}
