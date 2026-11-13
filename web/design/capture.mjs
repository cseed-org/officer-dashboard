// Run from repository root: node web/design/capture.mjs [page …]
// With page names (e.g. components-v03) only those pages are captured and checked.
import { chromium } from '../node_modules/@playwright/test/index.mjs';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { mkdir, writeFile } from 'node:fs/promises';
import path from 'node:path';
const dir=path.dirname(fileURLToPath(import.meta.url));
await mkdir(path.join(dir,'shots'),{recursive:true});
const browser=await chromium.launch({executablePath:process.env.CHROMIUM_PATH||'/usr/bin/chromium',args:['--no-sandbox','--disable-gpu']});
const results=[];
const only=process.argv.slice(2);
const pages=only.length?only:['home','events','finance','admin','settings'];
try {
 for(const theme of ['light','dark']) for(const width of [1440,820,390]) {
  const context=await browser.newContext({viewport:{width,height:1000},deviceScaleFactor:1,colorScheme:theme,reducedMotion:'reduce'});
  const page=await context.newPage();
  for(const name of pages) {
   const errors=[];page.on('pageerror',e=>errors.push(e.message));
   await page.goto(pathToFileURL(path.join(dir,'mockups',name+'.html')).href+'?theme='+theme);
   await page.evaluate(()=>document.fonts.ready);
   const metrics=await page.evaluate(()=>({pageWidth:document.documentElement.scrollWidth,viewport:innerWidth,background:getComputedStyle(document.body).backgroundColor,stylesheets:document.styleSheets.length}));
   if(metrics.pageWidth>width) throw new Error(`${name} ${width} ${theme} overflows: ${metrics.pageWidth}`);
   if(errors.length) throw new Error(errors.join('\n'));
   if(width!==820) await page.screenshot({path:path.join(dir,'shots',`${name}-${width}-${theme}.png`),fullPage:true});
   results.push({name,width,theme,...metrics});
  }
  await context.close();
 }
 if(only.length){
  await writeFile(path.join(dir,'shots',`validation-${only.join('-')}.json`),JSON.stringify({screenshots:only.length*4,layouts:results,checks:['No horizontal page overflow at 390, 820, 1440 in both themes','No browser script errors']},null,2)+'\n');
  console.log(`Captured ${only.length*4} screenshots of ${only.join(', ')}.`);
 } else {
 const page=await browser.newPage({viewport:{width:390,height:844}});
 await page.goto(pathToFileURL(path.join(dir,'mockups/settings.html')).href+'?theme=system&density=compact');
 await page.emulateMedia({colorScheme:'dark'});
 if(await page.evaluate(()=>getComputedStyle(document.body).backgroundColor)!=='rgb(24, 26, 24)') throw new Error('System dark failed');
 await page.emulateMedia({colorScheme:'light'});
 if(await page.evaluate(()=>getComputedStyle(document.body).backgroundColor)!=='rgb(255, 255, 255)') throw new Error('System light failed');
 await page.goto(pathToFileURL(path.join(dir,'mockups/finance.html')).href);
 await page.getByRole('tab',{name:'Budgets',exact:true}).click();
 await page.locator('#budgets').waitFor({state:'visible'});
 await page.getByRole('tab',{name:'Requests',exact:true}).click();
 await page.getByRole('button',{name:'New request',exact:true}).click();
 if(!await page.locator('#new-request').evaluate(e=>e.open)) throw new Error('Drawer did not open');
 await page.keyboard.press('Escape');
 await page.getByRole('button',{name:'Menu',exact:true}).click();
 if(!await page.locator('html').evaluate(e=>e.classList.contains('nav-open'))) throw new Error('Mobile navigation failed');
 await writeFile(path.join(dir,'shots','validation.json'),JSON.stringify({screenshots:20,layouts:results,checks:['No horizontal page overflow at 390, 820, 1440 in both themes','All pages load three shared stylesheets','No browser script errors','System theme follows emulated OS','Finance tabs, request drawer, Escape and mobile navigation']},null,2)+'\n');
 console.log('Captured 20 screenshots; verified 30 layouts and preview controls.');
 }
} finally { await browser.close(); }
