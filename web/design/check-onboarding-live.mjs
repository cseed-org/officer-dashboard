// Live onboarding captures against a dev build (DEV_AUTH=1), at 390 and 1440 px in light and dark:
//   intro       the full-screen welcome intro (Escape skips it, as any key or press starts it)
//   stage 1-5   the intake: basics (with the required-field errors), about you, your role,
//               calendar (with Finish later failing offline) and widgets
//   correction  the invitee's subteam comes from the invite; the director fixes it from
//               Officers → Invites → Edit details and the basics stage shows the fix
//   tour        Go to dashboard lands on Home with the guided tour; Skip tour ends it
// plus the shared-browser sign-in (someone else's invite link) and the unavailable-invite screen.
// The mockup counterpart is check-onboarding.mjs. Run from the repository root:
//   BASE_URL=http://127.0.0.1:8090 DEV_AUTH_KEY=… node web/design/check-onboarding-live.mjs
// Screenshots and checks.json go to OUT_DIR (default tmp/onboarding-live, git-ignored).
import {chromium} from '../node_modules/@playwright/test/index.mjs';
import {mkdir, writeFile} from 'node:fs/promises';
const base=process.env.BASE_URL||'http://127.0.0.1:8090';
const out=process.env.OUT_DIR||'tmp/onboarding-live';
await mkdir(out,{recursive:true});
const browser=await chromium.launch({executablePath:process.env.CHROMIUM_PATH||undefined});
const checks=[];const assert=(v,m)=>{if(!v)throw Error(m);checks.push(m)};
const discordId=()=>`9${String(Date.now()).padStart(13,'0')}${Math.floor(Math.random()*1e4).toString().padStart(4,'0')}`;
const noOverflow=p=>p.evaluate(()=>document.documentElement.scrollWidth<=document.documentElement.clientWidth+1);
const settle=async p=>{await p.waitForFunction(()=>!document.querySelector('astro-island[ssr]'));await p.waitForFunction(()=>!document.querySelector('main [aria-busy="true"]'));await p.evaluate(()=>document.fonts.ready)};
async function context(theme,width){
 const c=await browser.newContext({baseURL:base,viewport:{width,height:width<500?844:900},deviceScaleFactor:width<500?2:1,reducedMotion:'reduce',colorScheme:theme});
 await c.addInitScript(t=>{try{localStorage.setItem('cseed-theme',t)}catch{}},theme);
 return c;
}
/** Dev sign-in as web/tests/helpers.ts does it: POST /auth/dev (a GET only shows a confirmation page), then open the redirect. */
async function signIn(p,fields,what){
 if(p.url()!=='about:blank')await p.goto('about:blank'); // a late 401 from the old page must not redirect the new session
 const form={...fields,next:'/'};if(process.env.DEV_AUTH_KEY)form.key=process.env.DEV_AUTH_KEY;
 const r=await p.request.post('/auth/dev',{form,maxRedirects:0});
 assert([302,303].includes(r.status()),`${what}: dev sign-in (${r.status()}; is the server a dev build with DEV_AUTH=1?)`);
 await p.goto(r.headers()['location']??'/');await settle(p);
}
const watch=p=>{const errors=[];p.on('pageerror',e=>errors.push(e.message));
 p.on('console',m=>{if(m.type()==='error'&&!m.text().startsWith('Failed to load resource'))errors.push(m.text())});return errors};
const stage=(p,n)=>p.locator(`.onb[data-step="${n}"]`).waitFor();
const next=p=>p.locator('[data-onb-next]').click();
const subteamHint=p=>p.locator('.field-hint',{hasText:'come from your invitation'}).innerText();
try{
for(const theme of ['light','dark'])for(const width of [1440,390]){
 const tag=`${width}-${theme}`;
 // The director (tour marked seen, so it doesn't cover Officers) invites someone to Events.
 const dc=await context(theme,width);const dir=await dc.newPage();const dirErrors=watch(dir);
 await signIn(dir,{as:'director'},`${tag} director`);
 assert(new URL(dir.url()).pathname==='/',`${tag}: director lands on Home`);
 assert((await dir.request.put('/api/me/prefs',{data:{dashboardTutorialSeen:true}})).ok(),`${tag}: director tour marked seen`);
 const id=discordId();
 const inv=await dir.request.post('/api/invites',{data:{name:'Casey McKenzie',discordId:id,subteamId:'team-events',method:'link'}});
 assert(inv.ok(),`${tag}: invite created`);

 // The invitee signs in in their own browser and gets the intro first.
 const ic=await context(theme,width);const p=await ic.newPage();const errors=watch(p);
 const shot=async name=>{assert(await noOverflow(p),`${tag} ${name}: no horizontal overflow`);await p.screenshot({path:`${out}/${name}-${tag}.png`})};
 await signIn(p,{discord:id},`${tag} invitee`);
 assert(new URL(p.url()).pathname==='/onboarding',`${tag}: invitee lands on onboarding`);
 await p.locator('.wi').waitFor();
 const cover=await p.locator('.wi').evaluate(e=>{const b=e.getBoundingClientRect();return b.left<=0&&b.top<=0&&b.right>=innerWidth&&b.bottom>=innerHeight});
 assert(cover,`${tag}: the intro covers the whole screen`);
 await shot('intro');
 await p.keyboard.press('Escape');await p.locator('.wi').waitFor({state:'detached'});
 await stage(p,1);await p.waitForFunction(()=>document.activeElement?.id==='step-title');
 assert(await p.locator('.onb-progress li').count()===5,`${tag}: five stages`);
 assert((await subteamHint(p)).includes('(Events)'),`${tag}: basics shows the invite's subteam`);
 await shot('stage-1-basics');

 // Correction loop: the director fixes the subteam; the invitee's basics show the fix.
 await dir.goto('/officers?tab=invites');await settle(dir);
 await dir.locator(`[data-invite="${id}"] [data-invite-edit]`).click();
 await dir.locator('#inv-edit-team').selectOption({label:'Build'});
 assert(await dir.evaluate(()=>document.documentElement.scrollWidth<=document.documentElement.clientWidth+1),`${tag} invite edit: no horizontal overflow`);
 await dir.screenshot({path:`${out}/director-edit-invite-${tag}.png`});
 await dir.locator('[data-invite-edit-save]').click();
 assert((await dir.locator('[data-invite-note]').innerText()).startsWith('Saved Casey McKenzie'),`${tag}: director saved the fix`);
 await p.reload();await settle(p);await stage(p,1);
 assert(await p.locator('.wi').count()===0,`${tag}: the intro plays once`);
 assert((await subteamHint(p)).includes('(Build)'),`${tag}: basics shows the director's fix`);
 await shot('stage-1-after-fix');

 // Stage 1 requires phone and birthday.
 await next(p);await p.locator('#pf-phone-error').waitFor();await p.locator('#pf-birthday-error').waitFor();await stage(p,1);
 await shot('stage-1-errors');
 await p.fill('#pf-phone','+1 206 555 0188');await p.selectOption('#pf-month','11');await p.selectOption('#pf-day','03');
 await next(p);await stage(p,2);await shot('stage-2-about');
 await p.fill('#pf-pronouns','they/them');
 await next(p);await stage(p,3);
 assert(/Start term: (Spring|Summer|Fall) \d{4}/.test(await p.locator('[data-start-term]').innerText()),`${tag}: role stage shows the start term`);
 await shot('stage-3-role');
 await next(p);await stage(p,4);await settle(p);await shot('stage-4-calendar');
 // Finish later saves the place first; offline it keeps you signed in and says so.
 await ic.setOffline(true);await p.locator('[data-finish-later]').click();await p.locator('[data-step-error]').waitFor();
 await shot('stage-4-finish-later-offline');await ic.setOffline(false);
 await next(p);await stage(p,5);
 assert((await p.locator('[data-onb-next]').innerText()).includes('Go to dashboard'),`${tag}: last stage goes to the dashboard`);
 await shot('stage-5-widgets');

 // Home with the guided tour, once.
 await next(p);await p.waitForURL(u=>new URL(u).pathname==='/'&&new URL(u).searchParams.get('tutorial')==='onboarding');await settle(p);
 await p.locator('[data-home-tour]').waitFor();await shot('home-tour');
 await p.locator('[data-home-tour]').getByRole('button',{name:'Skip tour'}).click();await p.locator('[data-home-tour]').waitFor({state:'detached'});
 const profile=(await (await p.request.get('/api/me/profile')).json()).profile;
 assert(profile.birthday==='11-03'&&profile.pronouns==='they/them'&&profile.subteam?.name==='Build',`${tag}: profile saved with the corrected subteam`);
 await p.goto('/onboarding');await p.waitForURL(u=>new URL(u).pathname==='/');assert(true,`${tag}: setup done for good, /onboarding goes Home`);

 // Shared browser: the director's browser opens someone else's invite link.
 await dir.goto('/login?invite=someone-elses-token');await dir.locator('[data-invite-signed-in]').waitFor();
 assert(await dir.evaluate(()=>document.documentElement.scrollWidth<=document.documentElement.clientWidth+1),`${tag} shared login: no horizontal overflow`);
 await dir.screenshot({path:`${out}/login-signed-in-as-someone-else-${tag}.png`});
 // The unavailable-invite screen focuses its heading.
 await p.goto('/onboarding?unavailable=1');await p.locator('#step-title').waitFor();await p.waitForFunction(()=>document.activeElement?.id==='step-title');await shot('unavailable');

 const all=[...errors,...dirErrors].filter(e=>!/ERR_INTERNET_DISCONNECTED|Failed to fetch/.test(e)); // the offline step
 assert(all.length===0,`${tag}: no console errors (${all.join(' | ')})`);
 await ic.close();await dc.close();
}
await writeFile(`${out}/checks.json`,JSON.stringify({base,checks},null,2)+'\n');console.log(`${checks.length} live checks passed`);
}finally{await browser.close()}
