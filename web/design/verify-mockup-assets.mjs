// Opens every mockup from disk (file: URLs, as the capture scripts do) and fails on any stylesheet,
// script, image or font that does not load, and on any brand font the page uses but did not load.
// Run from the repository root: node web/design/verify-mockup-assets.mjs [page …]
import { chromium } from '../node_modules/@playwright/test/index.mjs';
import { pathToFileURL, fileURLToPath } from 'node:url';
import { readdirSync } from 'node:fs';
import path from 'node:path';
const dir=path.dirname(fileURLToPath(import.meta.url));
const only=process.argv.slice(2);
const pages=(only.length?only:readdirSync(path.join(dir,'mockups')).filter(f=>f.endsWith('.html')).map(f=>f.slice(0,-5))).sort();
const BRAND=['Playfair Display','Poppins','Anonymous Pro'];
// Self-contained by design (no external assets, see its header): it relies on installed fonts.
const SELF_CONTAINED=new Set(['home-v07']);
const browser=await chromium.launch({executablePath:process.env.CHROMIUM_PATH||'/usr/bin/chromium',args:['--no-sandbox']});
const problems=[];let checks=0;
try {
 for(const theme of ['light','dark']){
  const page=await browser.newPage({viewport:{width:1440,height:1000},reducedMotion:'reduce'});
  for(const name of pages){
   const failed=[];
   const onFail=r=>failed.push(`${r.resourceType()} ${decodeURI(r.url())}`);
   page.on('requestfailed',onFail);
   await page.goto(pathToFileURL(path.join(dir,'mockups',name+'.html')).href+'?theme='+theme);
   await page.evaluate(()=>document.fonts.ready);
   const fonts=await page.evaluate(brand=>{
    const used=new Set();
    for(const el of document.querySelectorAll('body *')){
     if(!el.getClientRects().length)continue;
     const first=getComputedStyle(el).fontFamily.split(',')[0].trim().replace(/^["']|["']$/g,'');
     if(brand.includes(first))used.add(first);
    }
    const faces=[...document.fonts];
    return {used:[...used],loaded:faces.filter(f=>f.status==='loaded').map(f=>f.family.replace(/^["']|["']$/g,'')),
     errors:faces.filter(f=>f.status==='error').map(f=>`${f.family} ${f.weight} ${f.style}`)};
   },BRAND);
   page.off('requestfailed',onFail);
   for(const f of failed)problems.push(`${name} (${theme}): failed to load ${f}`);
   for(const f of fonts.errors)problems.push(`${name} (${theme}): font face failed: ${f}`);
   if(!SELF_CONTAINED.has(name))for(const f of fonts.used)if(!fonts.loaded.includes(f))problems.push(`${name} (${theme}): uses ${f} but it did not load`);
   checks++;
  }
  await page.close();
 }
} finally {await browser.close()}
if(problems.length){console.error(problems.join('\n'));process.exit(1)}
console.log(`All assets and brand fonts load in ${checks} page/theme checks.`);
