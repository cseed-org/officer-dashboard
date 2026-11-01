// Supplemental review screenshots; run after capture.mjs events-v03.
import {chromium} from '../node_modules/@playwright/test/index.mjs';
import {pathToFileURL} from 'node:url';
import path from 'node:path';
import {writeFile} from 'node:fs/promises';
const contrast=[];
const dir=path.dirname(new URL(import.meta.url).pathname);
const browser=await chromium.launch({executablePath:process.env.CHROMIUM_PATH||'/usr/bin/chromium',args:['--no-sandbox']});
try {
for(const theme of ['light','dark']) for(const width of [1440,390]) {
 const page=await browser.newPage({viewport:{width,height:1000},reducedMotion:'reduce'});
 await page.goto(pathToFileURL(path.join(dir,'mockups/events-v03.html')).href+'?theme='+theme);
 for(const view of ['month','week']) {
  await page.locator(`[data-view=${view}]`).click();
  if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth)) throw Error('Calendar overflow');
  await page.screenshot({path:path.join(dir,`shots/events-v03-${view}-${width}-${theme}.png`),fullPage:true});
 }
 await page.locator('[data-view=list]').click();
 for(const id of ['editor','handoff','design','import','delete']) {
  if(['design','delete'].includes(id)) await page.locator('[data-open=editor]:not([data-new])').first().click();
  await page.locator(id==='editor'?'[data-open=editor]:not([data-new])':`[data-open=${id}]`).first().click();
  const overflow=await page.locator('#'+id).evaluate(d=>d.scrollWidth>d.clientWidth);
  if(overflow) throw Error(id+' dialog overflow');
  await page.screenshot({path:path.join(dir,`shots/events-v03-${id}-${width}-${theme}.png`)});
  if(id==='handoff'){await page.locator('#luma-url').fill('https://lu.ma/cseed-founders');await page.locator('#luma-form button').click();await page.screenshot({path:path.join(dir,`shots/events-v03-linked-${width}-${theme}.png`)});}
  await page.keyboard.press('Escape');
  if(['design','delete'].includes(id)) await page.keyboard.press('Escape');
 }
 for(const state of ['empty','loading','error']) {await page.locator(`[data-state=${state}]`).first().click();await page.screenshot({path:path.join(dir,`shots/events-v03-${state}-${width}-${theme}.png`),fullPage:true});}
 const ratios=await page.evaluate(()=>{
 const c=document.createElement('canvas').getContext('2d');const style=getComputedStyle(document.documentElement);
 function rgb(token){c.clearRect(0,0,1,1);c.fillStyle=style.getPropertyValue(token).trim();c.fillRect(0,0,1,1);return [...c.getImageData(0,0,1,1).data].slice(0,3)}
 function l(a){return a.map(x=>{x/=255;return x<=.04045?x/12.92:((x+.055)/1.055)**2.4}).reduce((a,v,i)=>a+v*[.2126,.7152,.0722][i],0)}
 return ['--text','--muted','--positive','--negative'].map(t=>{const a=l(rgb(t)),b=l(rgb('--surface'));return {token:t,ratio:(Math.max(a,b)+.05)/(Math.min(a,b)+.05)}});
 });
 contrast.push({theme,width,ratios});if(ratios.some(r=>r.ratio<4.5))throw Error('Text contrast below 4.5');
 await page.close();
}
} finally {await browser.close()}
await writeFile(path.join(dir,'shots/validation-events-v03-details.json'),JSON.stringify({contrast,checks:['Calendar and dialog overflow','Calendar, editor, handoff, linked URL, request, CSV, delete and states captured in both themes and sizes']},null,2)+'\n');
console.log('Captured supplemental calendar, dialog and state screenshots; text token contrast passes.');
