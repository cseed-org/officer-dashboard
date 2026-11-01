// Design QA: layout, role variants, edit lifecycle and gallery; uses the same browser as capture.mjs.
import {chromium} from '../node_modules/@playwright/test/index.mjs';
import {pathToFileURL,fileURLToPath} from 'node:url';
import path from 'node:path';
import {writeFile} from 'node:fs/promises';
const dir=path.dirname(fileURLToPath(import.meta.url));
const browser=await chromium.launch({executablePath:process.env.CHROMIUM_PATH||'/usr/bin/chromium',args:['--no-sandbox','--disable-gpu']});
const results=[];
function check(ok,message){if(!ok)throw Error(message);}
try {
 for(const theme of ['light','dark'])for(const width of [1440,390]){
  const page=await browser.newPage({viewport:{width,height:1000},colorScheme:theme,reducedMotion:'reduce',hasTouch:width===390});
  const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.goto(pathToFileURL(path.join(dir,'mockups/home-v03.html')).href+`?theme=${theme}`);
  await page.evaluate(()=>document.fonts.ready);
  const total=await page.evaluate(()=>window.homeRegistry.length), cloudCount=await page.evaluate(()=>window.homeRegistry.filter(w=>w.cat!=='Club').length);
  const firstAction={};
  for(const role of ['officer','director','leadership','owner']){
   await page.locator('#role').selectOption(role);
   check((await page.locator('#role-note').textContent()).toLowerCase().startsWith(role),'Role explanation missing');
   firstAction[role]=await page.locator('#widgets [data-id="actions"] .widget-list li').first().textContent();
   if(role!=='director'){await page.locator('#state').selectOption('connected');await page.evaluate(()=>scrollTo(0,0));await page.screenshot({path:path.join(dir,`shots/home-v03-role-${role}-${width}-${theme}.png`),fullPage:true});await page.locator('#state').selectOption('disconnected');}
  }
  check(new Set(Object.values(firstAction)).size===4,'My actions does not differ by role');
  await page.locator('#role').selectOption('director');
  await page.locator('#edit').click();
  await page.locator('[data-move="figures"]').focus();
  await page.keyboard.press('ArrowRight');await page.keyboard.press('Enter');
  check(await page.locator('#widgets > li').first().getAttribute('data-id')==='actions','Keyboard reorder failed');
  await page.locator('#widgets [data-id="figures"] [data-size="s"]').click();
  check(await page.locator('#widgets > [data-id="figures"]').evaluate(e=>e.classList.contains('widget--s')),'Resize failed');
  await page.locator('[data-remove="members"]').click();
  check(await page.locator('#widgets > [data-id="members"]').count()===0,'Remove failed');
  await page.locator('#undo-button').click();
  check(await page.locator('#widgets > [data-id="members"]').count()===1,'Undo failed');
  await page.evaluate(()=>scrollTo(0,0));
  await page.screenshot({path:path.join(dir,`shots/home-v03-edit-${width}-${theme}.png`),fullPage:true});
  const lift=await browser.newPage({viewport:{width,height:1000},colorScheme:theme,reducedMotion:'reduce'});
  await lift.goto(pathToFileURL(path.join(dir,'mockups/home-v03.html')).href+`?theme=${theme}&mode=edit&lift=analytics&slot=0`);
  check(await lift.locator('#widgets .widget.is-lifted').count()===1&&await lift.locator('#widgets .widget-slot').count()===1,'Lift and drop slot missing');
  await lift.screenshot({path:path.join(dir,`shots/home-v03-lift-${width}-${theme}.png`),fullPage:true});
  await lift.close();
  await page.locator('#edit-add').click();
  check(await page.locator('#gallery').evaluate(e=>e.open),'Gallery did not open');
  await page.evaluate(()=>scrollTo(0,0));
  await page.screenshot({path:path.join(dir,`shots/home-v03-gallery-${width}-${theme}.png`)});
  await page.locator('[data-category="Cloudflare"]').click();
  check(await page.locator('#gallery-items > li').count()===cloudCount-1,'Category failed');
  for(const id of ['traffic','performance','usage'])await page.locator(`[data-add="${id}"]`).click();
  await page.keyboard.press('Escape');
  check(await page.locator('#edit-add').evaluate(e=>e===document.activeElement),'Gallery focus return failed');
  await page.locator('#done').click();
  check(await page.locator('#feedback').textContent().then(t=>t.includes('Layout saved')),'Save feedback missing');
  await page.locator('#edit').click();
  for(const id of ['traffic','performance','usage'])await page.locator(`#widgets [data-id="${id}"] [data-size="l"]`).click();
  await page.locator('#done').click();
  for(const state of ['connected','empty','loading','error','mixed','disconnected']){
   await page.locator('#state').selectOption(state);
   check(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'Page overflow');
   check(await page.locator('#widgets > li.widget').count()===total,'Registry coverage missing');
   if(state==='disconnected')check(await page.locator('#widgets .widget-state-title').filter({hasText:'Not connected'}).count()===cloudCount,'Missing disconnected states');
   if(state==='loading')check(await page.locator('#widgets [aria-busy="true"]').count()===total,'Loading not aria-busy');
   if(state==='error')check(await page.locator('#widgets .state--error').count()===total,'Error boundary missing');
   if(state==='mixed')check(await page.locator('#widgets .badge--negative').count()===cloudCount,'Mixed stale badges missing');
   check((await page.locator('#sync-line').textContent()).length>0,'Sync line missing');
   if(state==='connected'){
    check((await page.locator('#widgets').textContent()).includes('United States'),'Analytics breakdown missing');
    check((await page.locator('#widgets').textContent()).includes('INP'),'Core Web Vitals missing');
   }
   await page.evaluate(()=>scrollTo(0,0));
  await page.screenshot({path:path.join(dir,`shots/home-v03-${state}-${width}-${theme}.png`),fullPage:true});
   results.push({width,theme,state,widgets:total,overflow:false});
  }
  await page.locator('#state').selectOption('error');
  await page.locator('[data-retry]').first().click();
  check(await page.locator('#state').inputValue()==='connected','Retry failed');
  await page.locator('#edit').click();
  while(await page.locator('[data-remove]').count())await page.locator('[data-remove]').first().click();
  check(await page.locator('#blank').isVisible(),'Empty home missing');
  check(errors.length===0,errors.join('\n'));
  console.log(`Passed ${width}px ${theme}`);
  await page.close();
 }
 await writeFile(path.join(dir,'shots/validation-home-v03-interactions.json'),JSON.stringify({checks:['Four role explanations','Keyboard reorder','S/M/L resize','Remove and undo','Gallery category and add','Escape and trigger focus restore','Done feedback','Role-specific My actions + role captures','Lifted widget and drop slot','Loading aria-busy, error boundary, mixed stale','All widgets across six states','No horizontal overflow','Retry','Empty home','No script errors'],results},null,2)+'\n');
 console.log('Home V0.3 interactions and 24 state layouts passed.');
}finally{await browser.close();}
