// Live onboarding captures: the real six steps, the correction loop and the shared-browser
// sign-in, at 390 and 1440 px in light and dark, against a dev build (DEV_AUTH=1).
// The mockup counterpart is check-onboarding.mjs. Run from the repository root:
//   BASE_URL=http://127.0.0.1:8197 DEV_AUTH_KEY=… node web/design/check-onboarding-live.mjs
import {chromium} from '../node_modules/@playwright/test/index.mjs';
import {mkdir, writeFile} from 'node:fs/promises';
const base=process.env.BASE_URL||'http://127.0.0.1:8090';
const key=process.env.DEV_AUTH_KEY?`&key=${encodeURIComponent(process.env.DEV_AUTH_KEY)}`:'';
const out=process.env.OUT_DIR||'docs/evidence/onboarding-v04';
await mkdir(out,{recursive:true});
const browser=await chromium.launch({executablePath:process.env.CHROMIUM_PATH||undefined});
const checks=[];const assert=(v,m)=>{if(!v)throw Error(m);checks.push(m)};
const discordId=()=>`9${String(Date.now()).padStart(13,'0')}${Math.floor(Math.random()*1e4).toString().padStart(4,'0')}`;
const noOverflow=p=>p.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1);
const settle=async p=>{await p.waitForFunction(()=>!document.querySelector('astro-island[ssr]'));await p.waitForFunction(()=>!document.querySelector('[aria-busy="true"]'));await p.evaluate(()=>document.fonts.ready)};
async function context(theme,width){
 const c=await browser.newContext({baseURL:base,viewport:{width,height:width<500?844:900},deviceScaleFactor:width<500?2:1,reducedMotion:'reduce',colorScheme:theme});
 await c.addInitScript(t=>{try{localStorage.setItem('cseed-theme',t)}catch{}},theme);
 return c;
}
async function asDirector(c){const p=await c.newPage();const r=await p.goto(`/auth/dev?as=director${key}`);assert(r.ok(),'director dev login');return p}
try{
for(const theme of ['light','dark'])for(const width of [1440,390]){
 const tag=`${width}-${theme}`;
 const c=await context(theme,width);const errors=[];
 // The director invites someone; the invitee signs in on a second context.
 const dir=await asDirector(c);const id=discordId();
 const inv=await dir.request.post('/api/invites',{data:{name:'Casey McKenzie',discordId:id,subteamId:'team-events',method:'link'}});assert(inv.ok(),`${tag}: invite created`);
 const ic=await context(theme,width);const p=await ic.newPage();
 p.on('pageerror',e=>errors.push(e.message));p.on('console',m=>{if(m.type()==='error'&&!m.text().startsWith('Failed to load resource'))errors.push(m.text())});
 await p.goto(`/auth/dev?discord=${id}${key}`);await settle(p);
 assert(p.url().endsWith('/onboarding'),`${tag}: invitee lands on onboarding`);
 const shot=async name=>{assert(await noOverflow(p),`${tag} ${name}: no horizontal overflow`);await p.screenshot({path:`${out}/${name}-${tag}.png`})};
 await shot('step-1-hello');
 await p.locator('[data-onb-next]').click();await p.locator('[data-prefill]').waitFor();await shot('step-2-confirm');
 await p.locator('[data-correction-open]').click();await p.locator('#correction').fill('My subteam should be Build.');
 await p.getByRole('button',{name:/^Send to/}).click();await p.locator('[data-correction-pending]').waitFor();await shot('step-2-correction-pending');
 // The director fixes it from Officers → Invites.
 await dir.goto('/officers?tab=invites');await settle(dir);await dir.locator(`[data-invite="${id}"] [data-invite-edit]`).click();
 await dir.locator('dialog[open]').getByLabel('Subteam').selectOption({label:'Build'});
 assert(await dir.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),`${tag} invite edit: no horizontal overflow`);
 await dir.screenshot({path:`${out}/director-edit-invite-${tag}.png`});
 await dir.locator('[data-invite-edit-save]').click();await dir.locator('[data-invite-note]').waitFor();
 await p.reload();await settle(p);await p.locator('[data-prefill]').waitFor();
 assert((await p.locator('[data-prefill]').innerText()).includes('Build'),`${tag}: Confirm shows the inviter's fix`);
 assert(await p.locator('[data-correction-pending]').count()===0,`${tag}: pending notice clears after the fix`);
 await shot('step-2-after-fix');
 await p.locator('[data-onb-next]').click();await p.locator('[data-profile-form]').waitFor();
 await p.locator('[data-onb-next]').click();await p.locator('#pf-phone-error').waitFor();await shot('step-3-errors');
 await p.getByLabel('Phone · required').fill('+1 206 555 0188');await p.getByLabel('Birthday month').selectOption('11');await p.getByLabel('Birthday day').selectOption('03');
 await p.locator('[data-onb-next]').click();await p.locator('.onb[data-step="4"]').waitFor();await settle(p);await shot('step-4-calendar');
 await ic.setOffline(true);await p.locator('[data-finish-later]').click();await p.locator('[data-step-error]').waitFor();await shot('step-4-finish-later-offline');await ic.setOffline(false);
 await p.locator('[data-onb-next]').click();await p.locator('.onb[data-step="5"]').waitFor();await shot('step-5-widgets');
 await p.locator('[data-onb-next]').click();await p.locator('[data-summary]').waitFor();await shot('step-6-done');
 await p.goBack();await p.waitForURL(u=>new URL(u).pathname==='/');assert(true,`${tag}: Back after setup goes Home`);
 // Shared browser: the director's browser opens someone else's invite link.
 await dir.goto('/login?invite=someone-elses-token');await dir.locator('[data-invite-signed-in]').waitFor();
 assert(await dir.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),`${tag} shared login: no horizontal overflow`);
 await dir.screenshot({path:`${out}/login-signed-in-as-someone-else-${tag}.png`});
 // Unavailable screen focuses its heading.
 await p.goto('/onboarding?unavailable=1');await p.locator('#step-title').waitFor();await p.waitForFunction(()=>document.activeElement?.id==='step-title');await shot('unavailable');
 assert(errors.length===0,`${tag}: no console errors (${errors.join(' | ')})`);
 await ic.close();await c.close();
}
await writeFile(`${out}/checks.json`,JSON.stringify({base,checks},null,2)+'\n');console.log(`${checks.length} live checks passed`);
}finally{await browser.close()}
