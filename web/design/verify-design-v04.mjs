// Read-only checks for the V0.4 Design mockup's rail actions. Run from the repository root
// after build-design-v04.py: node web/design/verify-design-v04.mjs
import { chromium } from '../node_modules/@playwright/test/index.mjs';
import { pathToFileURL, fileURLToPath } from 'node:url';
import path from 'node:path';
const dir=path.dirname(fileURLToPath(import.meta.url));
const browser=await chromium.launch({executablePath:process.env.CHROMIUM_PATH||'/usr/bin/chromium',args:['--no-sandbox']});
const prefilled='Opens New design request, prefilled from the event (title, due a week before, brief, link).';
let checks=0;
const expect=(ok,msg)=>{if(!ok)throw Error(msg);checks++};
try {
for(const width of [1440,390]) for(const theme of ['light','dark']){
 const page=await browser.newPage({viewport:{width,height:1000},reducedMotion:'reduce'});
 const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto(pathToFileURL(path.join(dir,'mockups','design-v04.html')).href+'?theme='+theme);
 const pitch=page.locator('[data-event="pitch"]');
 const status=page.locator('#result');
 const button=name=>pitch.getByRole('button',{name,exact:true});

 // Designer: the rebuilt Request design button still works after a not-needed round trip.
 await button('Mark not needed').click();
 expect(await pitch.getAttribute('data-need')==='not-needed','Mark not needed did not apply');
 await button('Needs design after all').click();
 expect(await pitch.getAttribute('data-need')==='needs','Needs design after all did not apply');
 await button('Request design').click();
 expect(await status.textContent()===prefilled,`Request design after the round trip: status is "${await status.textContent()}"`);

 // Officer: the rebuilt actions keep Mark not needed hidden.
 await page.reload();
 await page.getByLabel('Viewer',{exact:true}).selectOption('Officer');
 expect(await button('Mark not needed').isHidden(),'Officer sees Mark not needed');
 await pitch.locator('[data-not-needed]').evaluate(b=>b.click()); // a hidden control, clicked as a script would
 await button('Needs design after all').click();
 expect(await button('Mark not needed').isHidden(),'Mark not needed reappeared for the Officer after the actions were rebuilt');
 await button('Request design').click();
 expect(await status.textContent()===prefilled,'Officer Request design after the round trip did nothing');

 expect(!errors.length,errors.join('\n'));
 await page.close();
}
console.log(`Passed ${checks} Design V0.4 rail checks.`);
} finally {await browser.close()}
