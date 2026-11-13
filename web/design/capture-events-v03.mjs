// Supplemental review screenshots and checks; run after capture.mjs events-v03.
// Contrast is measured on rendered elements: every visible text node (and form
// control value/placeholder) in each captured state is compared with its
// composited background. Disabled controls are exempt (WCAG 1.4.3 incidental).
import {chromium} from '../node_modules/@playwright/test/index.mjs';
import {pathToFileURL} from 'node:url';
import path from 'node:path';
import {writeFile} from 'node:fs/promises';
const dir=path.dirname(new URL(import.meta.url).pathname);
const browser=await chromium.launch({executablePath:process.env.CHROMIUM_PATH||'/usr/bin/chromium',args:['--no-sandbox']});
const contrast=[], failures=[], behaviour=[];

function audit(label){
 const c=document.createElement('canvas').getContext('2d',{willReadFrequently:true});
 const rgba=v=>{c.clearRect(0,0,1,1);c.fillStyle='#000';c.fillStyle=v;c.fillRect(0,0,1,1);const d=c.getImageData(0,0,1,1).data;return [d[0],d[1],d[2],d[3]/255]};
 const over=(top,bot)=>top.slice(0,3).map((x,i)=>x*top[3]+bot[i]*(1-top[3])).concat(1);
 const lum=a=>a.slice(0,3).map(x=>{x/=255;return x<=.04045?x/12.92:((x+.055)/1.055)**2.4}).reduce((s,v,i)=>s+v*[.2126,.7152,.0722][i],0);
 const ratio=(a,b)=>{const x=lum(a),y=lum(b);return (Math.max(x,y)+.05)/(Math.min(x,y)+.05)};
 function bg(el){const layers=[];let opacity=1;for(let e=el;e;e=e.parentElement){const s=getComputedStyle(e);opacity*=+s.opacity;const b=rgba(s.backgroundColor);if(b[3]>0)layers.push(b);if(b[3]===1)break}
  let out=[255,255,255,1];for(const l of layers.reverse())out=over(l,out);return {color:out,opacity}}
 const dialogs=[...document.querySelectorAll('dialog[open]')];const root=dialogs.at(-1)||document.body;
 const seen=new Set(), results=[];
 function check(el,colorValue,text){
  if(el.closest('[disabled],[aria-hidden=true],.visually-hidden,fieldset:disabled')||(!dialogs.length&&el.closest('dialog')))return;
  if(!el.checkVisibility({opacityProperty:true,visibilityProperty:true}))return;const r=el.getBoundingClientRect();if(!r.width||!r.height)return;
  const s=getComputedStyle(el), b=bg(el), fg=rgba(colorValue);fg[3]*=b.opacity;
  const size=parseFloat(s.fontSize), large=size>=24||(size>=18.66&&+s.fontWeight>=700), need=large?3:4.5;
  const v=ratio(over(fg,b.color),b.color);const key=el.tagName+'|'+el.className+'|'+colorValue+'|'+b.color;
  if(seen.has(key))return;seen.add(key);
  results.push({label,el:el.tagName.toLowerCase()+(el.className&&typeof el.className==='string'?'.'+el.className.trim().split(/\s+/).join('.'):''),text:text.trim().slice(0,40),size,ratio:+v.toFixed(2),need,pass:v>=need});
 }
 const w=document.createTreeWalker(root,NodeFilter.SHOW_TEXT,{acceptNode:n=>n.textContent.trim()?1:3});
 for(let n;(n=w.nextNode());){const el=n.parentElement;if(['SCRIPT','STYLE','OPTION','TITLE'].includes(el.tagName))continue;check(el,getComputedStyle(el).color,n.textContent)}
 root.querySelectorAll('input:not([type=file]):not([type=checkbox]):not([type=radio]),textarea,select').forEach(el=>{
  if(el.value)check(el,getComputedStyle(el).color,el.value);else if(el.placeholder)check(el,getComputedStyle(el,'::placeholder').color,el.placeholder)});
 return results;
}

async function record(page,label,meta){
 await page.evaluate(()=>document.fonts.ready);
 const r=await page.evaluate(audit,label);
 contrast.push({...meta,label,checked:r.length,min:r.length?Math.min(...r.map(x=>x.ratio)):null});
 for(const x of r)if(!x.pass)failures.push({...meta,...x});
}
const top=page=>page.evaluate(()=>scrollTo(0,0));

try {
for(const theme of ['light','dark']) for(const width of [1440,390]) {
 const meta={theme,width};
 const page=await browser.newPage({viewport:{width,height:1000},reducedMotion:'reduce'});
 const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto(pathToFileURL(path.join(dir,'mockups/events-v03.html')).href+'?theme='+theme);
 await record(page,'list',meta);
 for(const view of ['month','week']) {
  await page.locator(`[data-view=${view}]`).click();
  if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth)) throw Error('Calendar overflow');
  await record(page,view,meta);await top(page);
  await page.screenshot({path:path.join(dir,`shots/events-v03-${view}-${width}-${theme}.png`),fullPage:true});
 }
 await page.locator('[data-view=list]').click();
 for(const id of ['editor','handoff','design','import','delete']) {
  if(['design','delete'].includes(id)) await page.locator('[data-open=editor]:not([data-new])').first().click();
  await page.locator(id==='editor'?'[data-open=editor]:not([data-new])':`[data-open=${id}]`).first().click();
  const overflow=await page.locator('#'+id).evaluate(d=>d.scrollWidth>d.clientWidth);
  if(overflow) throw Error(id+' dialog overflow');
  await record(page,id,meta);
  await page.screenshot({path:path.join(dir,`shots/events-v03-${id}-${width}-${theme}.png`)});
  if(id==='handoff'){
   await page.locator('#luma-url').fill('https://lu.ma/');await page.locator('#luma-form button[type=submit]').click();await record(page,'handoff-invalid',meta);
   await page.locator('#luma-url').fill('https://lu.ma/cseed-founders');await page.locator('#luma-form button[type=submit]').click();await record(page,'linked',meta);
   await page.screenshot({path:path.join(dir,`shots/events-v03-linked-${width}-${theme}.png`)});
   const copy=page.locator('[data-copy]').first();await copy.click();
   behaviour.push({...meta,check:'copy button keeps field name',text:await copy.textContent()});
  }
  if(id==='design'){await page.locator('#request-design').click();behaviour.push({...meta,check:'focus after design request',focus:await page.evaluate(()=>document.activeElement.id)});await record(page,'design-created',meta)}
  await page.keyboard.press('Escape');
  if(['design','delete'].includes(id)) await page.keyboard.press('Escape');
 }
 for(const state of ['empty','loading','error']) {
  await page.locator(`[data-state=${state}]`).first().click();
  behaviour.push({...meta,check:state+' hides health',healthHidden:await page.locator('#health').isHidden(),period:await page.locator('#period').textContent()});
  await record(page,state,meta);await top(page);
  await page.screenshot({path:path.join(dir,`shots/events-v03-${state}-${width}-${theme}.png`),fullPage:true});
 }
 await page.locator('[data-state=empty]').click();await page.locator('#state-empty [data-open=editor]').click();
 behaviour.push({...meta,check:'empty CTA opens blank editor',name:await page.locator('#event-form input').first().inputValue(),deleteHidden:await page.locator('#delete-event').isHidden()});
 await page.keyboard.press('Escape');
 await page.locator('[data-state=ready]').first().click();
 // Delete flow: focus must land on the period heading and every draft entry disappears.
 await page.locator('[data-open=editor]:not([data-new])').first().click();
 await page.locator('#delete-event').click();await page.locator('#confirm-delete').click();await page.waitForTimeout(50);
 behaviour.push({...meta,check:'delete',focus:await page.evaluate(()=>document.activeElement.id),visibleDraft:await page.locator('[data-draft]:not([hidden])').count(),period:await page.locator('#period').textContent()});
 await record(page,'deleted',meta);await top(page);
 await page.screenshot({path:path.join(dir,`shots/events-v03-deleted-${width}-${theme}.png`),fullPage:true});
 if(errors.length)throw Error('Page errors: '+errors.join('; '));
 await page.close();
}
} finally {await browser.close()}
const bad=behaviour.filter(b=>(b.check==='delete'&&(b.focus!=='period'||b.visibleDraft))||(b.check.endsWith('hides health')&&!b.healthHidden)||(b.check==='empty CTA opens blank editor'&&(b.name||!b.deleteHidden))||(b.check==='focus after design request'&&b.focus!=='design-status')||(b.check.startsWith('copy')&&!/name/.test(b.text)));
await writeFile(path.join(dir,'shots/validation-events-v03-details.json'),JSON.stringify({
 contrast:{method:'Computed colour of every visible text node and form value/placeholder vs. composited ancestor background, per captured state; 4.5:1 normal, 3:1 large; disabled controls exempt',states:contrast,failures},
 behaviour,
 checks:['Calendar and dialog overflow','No page script errors','Calendar, editor, handoff, linked URL, request, CSV, delete, deleted and states captured in both themes and sizes']},null,2)+'\n');
if(failures.length){console.error(failures);throw Error(failures.length+' contrast failures')}
if(bad.length){console.error(bad);throw Error('Behaviour checks failed')}
console.log(`Captured supplemental shots; ${contrast.reduce((a,c)=>a+c.checked,0)} rendered text samples pass contrast; behaviour checks pass.`);
