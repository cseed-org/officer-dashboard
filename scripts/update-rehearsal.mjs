// Isolated local registry/compose rehearsal: A -> B succeeds; unhealthy C restores B/data.
// Start local Wrangler on port 8791 with BACKUP_BEARER_SECRET=local-test-only first.
import assert from 'node:assert/strict';
import { execFileSync, spawn } from 'node:child_process';
import { copyFileSync, mkdirSync, mkdtempSync, writeFileSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';
const root=resolve(import.meta.dirname,'..');
const scratch=process.env.PANE_SCRATCH_DIR || resolve(root,'.pane/scratch');
mkdirSync(scratch,{recursive:true});
const work=mkdtempSync(resolve(scratch,'update-rehearsal-'));
const id='p3-rehearsal-'+Date.now();
const docker=(...args)=>execFileSync('docker',args,{encoding:'utf8',stdio:['ignore','pipe','pipe']}).trim();
const save=(name,body)=>writeFileSync(resolve(work,name),body);
const registry=id+'-registry';
let started=false;
try {
 docker('run','-d','--name',registry,'-p','127.0.0.1:5003:5000','registry:2');
 started=true;
 const recipient=docker('run','--rm','--entrypoint','/app/backup-test','-e','BACKUP_REHEARSAL_MODE=recipient','dashboard-backup-test','-test.run=^TestRehearsalHelper$').split('\n')[0];

 const images={};
 for(const version of ['A','B','C']) {
  save('dashboard-wrapper',`#!/bin/sh
set -eu
if [ "$1" = healthcheck ] && [ "${version}" = C ]; then exit 1; fi
if [ "$1" = serve ] || [ "$1" = migrate ]; then
  /app/dashboard-real migrate
  BACKUP_REHEARSAL_MODE=set BACKUP_REHEARSAL_VERSION=${version} /app/backup-test -test.run=^TestRehearsalHelper$
fi
exec /app/dashboard-real "$@"
`);
  save('Dockerfile',`FROM dashboard-backup-qa
USER root
RUN cp /app/dashboard /app/dashboard-real
COPY dashboard-wrapper /app/dashboard
COPY --from=dashboard-backup-test /app/backup-test /app/backup-test
RUN chmod 755 /app/dashboard
USER dashboard
LABEL rehearsal="${version}"
`);
  const tag=`localhost:5003/dashboard:${version}`;
  docker('build','-t',tag,work);
  docker('push',tag);
  images[version]=JSON.parse(docker('image','inspect',tag))[0].RepoDigests.find(s=>s.startsWith('localhost:5003/dashboard@'));
 }
 save('Dockerfile.driver','FROM docker:27-cli\nRUN apk add --no-cache python3\n');
 docker('build','-f',resolve(work,'Dockerfile.driver'),'-t',id+'-driver',work);
 save('.env','');
 save('prod-image.env','DASHBOARD_IMAGE='+images.A+'\n');
 save('dashboard.env',`APP_URL=https://rehearsal.example.org
DB_PATH=/data/dashboard.db
BACKUP_SINK_URL=http://host.docker.internal:8791
BACKUP_BEARER_SECRET=local-test-only
BACKUP_AGE_RECIPIENT=${recipient}
`);
 save('compose.cloudflare.yml',`name: ${id}
services:
  dashboard:
    image: \${DASHBOARD_IMAGE}
    env_file: dashboard.env
    extra_hosts: ["host.docker.internal:host-gateway"]
    volumes: ["data:/data"]
volumes:
  data:
`);
 copyFileSync(resolve(root,'scripts/update'),resolve(work,'update.py'));
 const compose=(...args)=>docker('compose','--env-file',resolve(work,'prod-image.env'),'-f',resolve(work,'compose.cloudflare.yml'),...args);
 compose('run','--rm','--no-deps','-e','BACKUP_REHEARSAL_MODE=prepare','-e','BACKUP_REHEARSAL_VERSION=A','--entrypoint','/app/backup-test','dashboard','-test.run=^TestRehearsalHelper$');
 compose('up','-d','dashboard');
 const update=(image)=>new Promise(resolveResult=>{
  const child=spawn('docker',['run','--rm','-v','/var/run/docker.sock:/var/run/docker.sock','-v',`${work}:/rehearsal`,id+'-driver','python3','/rehearsal/update.py','--deploy-dir','/rehearsal','--image',image],{stdio:['ignore','pipe','pipe']});
  let output=''; child.stdout.on('data',v=>output+=v); child.stderr.on('data',v=>output+=v);
  child.on('exit',code=>resolveResult({code,output}));
 });
 let result=await update(images.B);
 assert.equal(result.code,0,result.output);
 assert.match(readFileSync(resolve(work,'prod-image.env'),'utf8'),new RegExp(images.B.split('@')[1]));
 const probe=()=>compose('exec','-T','-e','BACKUP_REHEARSAL_MODE=probe','dashboard','/app/backup-test','-test.run=^TestRehearsalHelper$').split('\n')[0];
 assert.equal(probe(),'B');
 console.log('A -> B: healthy, data B, digest persisted');
 result=await update(images.C);
 assert.equal(result.code,1,result.output);
 assert.match(result.output,/previous image and pre-update database restored/);
 assert.equal(probe(),'B');
 assert.match(readFileSync(resolve(work,'prod-image.env'),'utf8'),new RegExp(images.B.split('@')[1]));
 console.log('broken C -> B: automatic rollback, pre-update data and digest intact');
 result=await update(images.C);
 assert.equal(result.code,0,result.output);
 assert.equal(probe(),'B');
 console.log('rejected C is skipped on subsequent polls');
} finally {
 if(started) {
  try { docker('compose','--env-file',resolve(work,'prod-image.env'),'-f',resolve(work,'compose.cloudflare.yml'),'down','-v'); } catch {}
  docker('rm','-f',registry);
 }
}
