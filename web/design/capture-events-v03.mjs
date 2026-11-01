// Supplemental review screenshots; run after capture.mjs events-v03.
import {chromium} from '../node_modules/@playwright/test/index.mjs';
import {pathToFileURL} from 'node:url';
import path from 'node:path';
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
 for(const id of ['editor','handoff','design','import']) {
  await page.locator(`[data-open=${id}]`).first().click();
  const overflow=await page.locator('#'+id).evaluate(d=>d.scrollWidth>d.clientWidth);
  if(overflow) throw Error(id+' dialog overflow');
  await page.screenshot({path:path.join(dir,`shots/events-v03-${id}-${width}-${theme}.png`)});
  await page.keyboard.press('Escape');
 }
 await page.close();
}
} finally {await browser.close()}
console.log('Captured 24 supplemental calendar and dialog screenshots in both themes.');
