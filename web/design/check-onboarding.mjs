// Design-only interaction checks and six-step captures. Run from the repository root.
import {chromium} from '../node_modules/@playwright/test/index.mjs';
import {pathToFileURL} from 'node:url';
import {writeFile} from 'node:fs/promises';
const browser=await chromium.launch({executablePath:process.env.CHROMIUM_PATH||'/usr/bin/chromium',args:['--no-sandbox']});
const url=pathToFileURL(`${process.cwd()}/web/design/mockups/onboarding-v03.html`).href;
const checks=[]; const assert=(v,m)=>{if(!v)throw Error(m);checks.push(m)};
try{
for(const theme of ['light','dark'])for(const width of [1440,390]){
 const p=await browser.newPage({viewport:{width,height:1000},reducedMotion:'reduce'});
 const errors=[];p.on('pageerror',e=>errors.push(e.message));
 for(let step=1;step<=6;step++){
  await p.goto(`${url}?theme=${theme}&step=${step}`);await p.evaluate(()=>document.fonts.ready);
  if(step===3)await p.locator('summary').click();
  assert(await p.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),`${theme} ${width} step ${step}: no horizontal overflow`);
  // Full shell screenshot plus an expanded scroll-region screenshot for long form content.
  await p.screenshot({path:`web/design/shots/onboarding-v03-step-${step}-${width}-${theme}.png`});
  if(step===3)await p.locator('.onb-step').screenshot({path:`web/design/shots/onboarding-v03-profile-fields-${width}-${theme}.png`});
 }
 for(const state of ['empty','loading','error','expired']){
  await p.goto(`${url}?theme=${theme}&step=${state==='empty'?4:3}&state=${state}`);await p.evaluate(()=>document.fonts.ready);
  assert(await p.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),`${theme} ${width} ${state}: no horizontal overflow`);
  await p.screenshot({path:`web/design/shots/onboarding-v03-${state}-${width}-${theme}.png`});
 }
 assert(errors.length===0,`${theme} ${width}: no script errors`);await p.close();
}
const p=await browser.newPage({viewport:{width:390,height:1000},reducedMotion:'reduce'});
await p.goto(`${url}?step=3`);await p.locator('#phone').fill('');await p.locator('#birthday').fill('');await p.locator('#next').click();
assert(await p.locator('#phone').getAttribute('aria-invalid')==='true','Missing profile values block Continue and mark the field');
await p.locator('#phone').fill('+1 206 555 0104');await p.locator('#birthday').fill('2004-02-10');await p.locator('#next').click();
assert(await p.locator('#step-select').inputValue()==='4','Valid profile advances to Calendar');await p.locator('#back').click();
assert(await p.locator('#phone').inputValue()==='+1 206 555 0104','Back retains entered profile values');
await p.locator('#next').click();
for(let i=2;i<=5;i++){await p.locator('#saved-email').selectOption('add');await p.locator('#new-email').fill(`maya${i}@example.org`);await p.getByRole('button',{name:'Save email',exact:true}).click()}
assert(await p.locator('#saved-email option[value=add]').isDisabled(),'Sixth calendar email is disabled at five saved');
await p.locator('[value=google]').check();await p.locator('#connect').click();await p.locator('#next').click();
assert(await p.locator('#step-select').inputValue()==='4','Unconnected Google requires an explicit deferral');await p.locator('#defer').click();
assert(await p.locator('#step-select').inputValue()==='5','Explicit Set up later advances');
await p.locator('#state-select').selectOption('empty');await p.locator('#next').click();assert(await p.locator('.onb-checks').innerText().then(t=>t.includes('0 Home widgets')),'Empty Home selection completes with zero widgets');
await p.locator('#step-select').selectOption('2');for(const role of ['officer','director','leadership','owner']){await p.locator('#role-select').selectOption(role);assert(await p.locator('.onb-prefill').innerText().then(t=>t.toLowerCase().includes(role)),`${role}: assigned access is visible`)}
await p.locator('#state-select').selectOption('loading');assert(await p.locator('#next').isDisabled(),'Loading blocks Continue');
await p.locator('#state-select').selectOption('expired');assert(await p.locator('#next').isDisabled(),'Invalid invite blocks Continue');
await p.locator('#state-select').selectOption('ready');await p.locator('#step-select').selectOption('4');await p.keyboard.press('Alt+ArrowLeft');assert(await p.locator('#step-select').inputValue()==='3','Alt+Left navigates Back');
assert(await p.locator('#step-title').evaluate(e=>e===document.activeElement),'Step navigation focuses the heading');
await p.close();await writeFile('web/design/shots/validation-onboarding-interactions.json',JSON.stringify({checks},null,2)+'\n');console.log(`${checks.length} design checks passed`);
}finally{await browser.close()}
