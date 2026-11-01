// Read-only fixture checks, including the flows not covered by capture.mjs.
import { chromium } from '../node_modules/@playwright/test/index.mjs';
import { pathToFileURL, fileURLToPath } from 'node:url';
import path from 'node:path';
const dir=path.dirname(fileURLToPath(import.meta.url));
const browser=await chromium.launch({executablePath:'/usr/bin/chromium',args:['--no-sandbox']});
let checks=0;
try {
for(const width of [1440,390]) for(const theme of ['light','dark']){
 const page=await browser.newPage({viewport:{width,height:1000},reducedMotion:'reduce'});
 for(const name of ['speakers-v03','design-requests-v03']){
  await page.goto(pathToFileURL(path.join(dir,'mockups',name+'.html')).href+'?theme='+theme);
  for(const role of ['Officer','Director','Leadership','Owner']){
   await page.getByLabel('Your role',{exact:true}).selectOption(role);
   if(await page.locator('.user-chip-role').textContent()!==role)throw Error('Role preview failed');checks++;
  }
  for(const state of ['Empty','Loading','Error','Ready']){
   await page.getByLabel('Data state',{exact:true}).selectOption(state);
   await page.locator('#'+state.toLowerCase()).waitFor({state:'visible'});checks++;
  }
  const opens=name==='speakers-v03'?['Add contact','View contact','Log outreach']:['New design request','Founder stories cover'];
  for(const label of opens){
   if(label==='Log outreach')await page.getByRole('button',{name:'View contact',exact:true}).first().click();
   if(label==='Founder stories cover')await page.getByRole('link',{name:label,exact:true}).click();
   else await page.getByRole('button',{name:label,exact:true}).first().click();
   const dlg=page.locator('dialog[open]');await dlg.waitFor({state:'visible'});
   if(await dlg.evaluate(e=>e.scrollWidth>e.clientWidth))throw Error('Drawer overflow');
   await page.screenshot({path:path.join(dir,'shots',`${name}-${label.toLowerCase().replaceAll(' ','-')}-${width}-${theme}.png`)});
   if(label==='Founder stories cover'){
    await page.getByLabel('Acting as',{exact:true}).selectOption({index:1});
    if(!await page.getByRole('button',{name:'Approve this version'}).isDisabled())throw Error('Designer approval enabled');
    await page.getByLabel('Acting as',{exact:true}).selectOption({index:0});
    await page.getByRole('button',{name:'Approve this version'}).click();
    await page.getByRole('button',{name:'Approved ✓'}).waitFor();checks++;
   }
   await page.keyboard.press('Escape');checks++;
  }
  if(name==='design-requests-v03'){
   await page.getByRole('button',{name:'List',exact:true}).click();
   await page.locator('#request-list').waitFor({state:'visible'});
   if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth))throw Error('List overflow');checks++;
  }
 }
 await page.close();
}
console.log(`Passed ${checks} role, state, drawer, approval and list checks.`);
} finally {await browser.close()}
