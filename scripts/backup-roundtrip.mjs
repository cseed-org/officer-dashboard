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
 const proof=`import sys,os,sqlite3,hashlib,subprocess
from pathlib import Path
sys.path.insert(0,'/app')
from backup import copy_database,encrypt,Sink,restore
os.umask(0o077)
key=Path('/data/test.agekey')
subprocess.run(['age-keygen','-o',str(key)],check=True,capture_output=True)
os.environ['BACKUP_AGE_RECIPIENT']=subprocess.check_output(['age-keygen','-y',str(key)],text=True).strip()
source=Path('/data/source.db')
with sqlite3.connect(source) as db:
 db.execute('CREATE TABLE records(id INTEGER PRIMARY KEY,value BLOB)')
 db.execute('INSERT INTO records(value) VALUES(?)',(os.urandom(2100000),))
copy_database(source,Path('/data/snapshot.db'))
expected=hashlib.sha256(Path('/data/snapshot.db').read_bytes()).hexdigest()
encrypt(Path('/data/snapshot.db'),Path('/data/snapshot.age'))
snapshot=Sink().upload(Path('/data/snapshot.age'))
assert snapshot['chunks']==2
restore(snapshot['id'],'/data/restored.db',str(key))
assert hashlib.sha256(Path('/data/restored.db').read_bytes()).hexdigest()==expected
Sink().request('/prune','POST',{'hourly':24})
assert any(s['id']==snapshot['id'] for s in Sink().list())
key.unlink()
print('two-chunk encrypted round trip: matching plaintext SHA-256; integrity ok; prune ok')
`;
 writeFileSync(resolve(work,'proof.py'),proof);
 const output=execFileSync('docker',['run','--rm','--user','10001:10001','--add-host','host.docker.internal:host-gateway','--entrypoint','python3',
  '-v',`${volume}:/data`,'-v',`${resolve(work,'proof.py')}:/proof.py:ro`,'-v',`${resolve(root,'server/deploy/backup.py')}:/app/backup.py:ro`,
  '-e',`BACKUP_SINK_URL=http://host.docker.internal:${port}`,'-e','BACKUP_BEARER_SECRET=roundtrip-test-only','-e','DB_PATH=/data/source.db',
  'dashboard-backup-qa','/proof.py'],{encoding:'utf8',timeout:180000});
 console.log(output.trim());
} finally {
 execFileSync('docker',['volume','rm',volume],{stdio:'ignore'});
 if(process.platform==='win32') execFileSync('taskkill',['/pid',String(worker.pid),'/T','/F'],{stdio:'ignore'});
 else worker.kill('SIGTERM');
}
