/* Offline design interactions. Save is simulated; production persists user_prefs server-side. */
(() => {
const $=id=>document.getElementById(id), params=new URLSearchParams(location.search);
const registry=[
 {id:'figures',name:'Key figures',size:'m',hue:'blue',cat:'Club',description:'Upcoming events and participation at a glance.'},
 {id:'actions',name:'My actions',size:'l',hue:'violet',cat:'Club',description:'Your requests, reviews and event follow-ups.'},
 {id:'analytics',name:'Web Analytics',size:'l',hue:'blue',cat:'Cloudflare',description:'Visitors, page views, top pages, referrers and countries. On by default. Needs a read-only token · Not connected yet.'},
 {id:'members',name:'Member signups',size:'s',hue:'green',cat:'Members',description:'People from cseed.co/join, mirrored daily from D1. Needs D1 credentials · Not connected yet.'},
 {id:'events',name:'Recent events',size:'m',hue:'amber',cat:'Club',description:'Your latest CSEED events and check-ins.'},
 {id:'budget',name:'Budget vs actual',size:'m',hue:'teal',cat:'Club',description:'Allocated and settled spending for FY 2026–27.'},
 {id:'activity',name:'Activity',size:'m',hue:'coral',cat:'Club',description:'Recent work across the club.'},
 {id:'traffic',name:'Traffic & security',size:'m',hue:'teal',cat:'Cloudflare',description:'Requests, bandwidth, cache hit rate and threats. Needs a read-only token · Not connected yet.'},
 {id:'performance',name:'Performance',size:'m',hue:'coral',cat:'Cloudflare',description:'Core Web Vitals: LCP, INP and CLS at p75. Needs a read-only token · Not connected yet.'},
 {id:'usage',name:'Workers / D1 usage',size:'m',hue:'violet',cat:'Cloudflare',description:'Worker requests, CPU time, D1 reads, writes and storage. Needs a read-only token · Not connected yet.'}
];
window.homeRegistry=registry;
let layout=registry.slice(0,7).map(w=>({id:w.id,size:w.size})), editing=false, removed, lifted=null, dragId=null, category='All', slotAt=null, undoTimer;
const selected=Object.fromEntries(registry.map(w=>[w.id,w.size]));
$('role').value=params.get('role')||'director';$('state').value=params.get('state')||'disconnected';
const role=()=>$('role').value, state=()=>$('state').value, isLead=()=>['leadership','owner'].includes(role());
const say=t=>$('feedback').textContent=t;
const SIZE_NAME={s:'Small',m:'Medium',l:'Large'}, SPAN={s:[1,1],m:[2,1],l:[2,2]};
function list(rows){return '<ul class="widget-list">'+rows.map(([a,b])=>`<li><span>${a}</span><span>${b}</span></li>`).join('')+'</ul>';}
function metrics(rows,cls=''){return `<div class="home-metrics ${cls}">`+rows.map(([a,b])=>`<p><strong>${a}</strong><span>${b}</span></p>`).join('')+'</div>';}
function group(title,rows){return `<section><h3>${title}</h3>${list(rows)}</section>`;}
function stateView(title,body,action=''){return `<div class="widget-state"><p class="widget-state-title">${title}</p><p class="widget-state-body">${body}</p>${action}</div>`;}
// Sync wording is shared by the page line, widget footers and S widgets.
const SYNC={connected:'Synced daily, last 7:02 AM',empty:'Synced daily, last 7:02 AM',disconnected:'Not connected · syncs daily once leadership connects cseed.co',loading:'Checking today’s sync…',error:'Sync failed 7:02 AM · no earlier snapshot',mixed:'Sync failed 7:02 AM · showing Oct 31, 7:02 AM'};
const FOOT={connected:'Synced daily, last 7:02 AM',empty:'Synced daily, last 7:02 AM',disconnected:'Syncs daily once connected',loading:'Checking today’s sync…',error:'Sync failed 7:02 AM',mixed:'Stale · Oct 31, 7:02 AM'};
const GHOST={analytics:['Visitors','Page views','Top pages','Referrers','Countries'],traffic:['Requests','Bandwidth','Cache hit','Threats'],performance:['LCP','INP','CLS','Per-page p75'],usage:['Worker requests','CPU time','D1 reads & writes','Storage'],members:['Total members','New this week','Daily signups']};
function connectDetails(w){
 const text=isLead()
  ?(w.id==='members'?'Set the D1 account, database ID and read-only API token in the deployment config. The next daily sync fills this in.':'Set CLOUDFLARE_API_TOKEN (read-only) in the deployment config. The next daily sync fills this in.')
  :'Leadership connects cseed.co. Ask them in #leadership; nothing to set up on your side.';
 return `<details class="home-connect"><summary>Connection details</summary><p>${text}</p></details>`;
}
function actionRows(){
 const r={
  officer:[['Approve Founder stories cover','Due Nov 4'],['Upload workshop receipt','$124.80'],['Reply to Priya Raman (speaker)','Today'],['Submit Design for people reflection','Due Nov 6']],
  director:[['Review catering request','$286.40'],['Review workshop materials','$124.80'],['Taylor Brooks invite','DM sent ✓ Nov 1'],['Approve Founder stories cover','Due Nov 4'],['Fix 2 member records','D1']],
  leadership:[['Approve Fall venture night budget','$2,400'],['Promote Maya Patel to director','1 pending'],['Review catering request','$286.40'],['Grant leadership to Jordan Lee','Requested'],['Review workshop materials','$124.80']],
  owner:[['Ownership · 1 owner','Add a co-owner'],['Approve Fall venture night budget','$2,400'],['Promote Maya Patel to director','1 pending'],['Grant leadership to Jordan Lee','Requested'],['Review catering request','$286.40']]
 }[role()];
 return r;
}
function skeleton(w,size){
 const rows={s:0,m:2,l:6}[size];
 return `<span class="visually-hidden">Loading ${w.name}</span><span class="skel skel--kpi" aria-hidden="true"></span>`+(size==='s'?'<span class="skel skel--label" aria-hidden="true"></span>':'')+'<span class="skel skel--row" aria-hidden="true"></span>'.repeat(rows);
}
function content(w,size,s=state()){
 const cloud=w.cat!=='Club';
 if(s==='loading')return skeleton(w,size);
 if(s==='error')return stateView(`<span class="badge badge--negative">Failed</span> ${cloud?'Sync failed':'Couldn’t load'}`,cloud?'Today’s 7:02 AM sync failed and no earlier snapshot exists.':'This widget could not load. Your layout is safe.','<button class="linkish" data-retry>Retry</button>');
 if(cloud&&s==='disconnected')return stateView('Not connected',w.id==='members'?'D1 membership sync is not configured yet.':'A read-only Cloudflare token is needed.',connectDetails(w))+(size==='l'?ghost(w):'');
 if(s==='empty')return stateView(cloud?'No data yet':({actions:'You’re all caught up',events:'No events yet',activity:'A quiet start',budget:'No budget set',figures:'A new term awaits'})[w.id],cloud?'Connected. No records in this period.':'New activity will appear here.','<button class="linkish" data-empty>What happens next?</button>');
 const stale=cloud&&s==='mixed'?`<p class="widget-sub"><span class="badge badge--negative">Stale</span> ${size==='s'?'Oct 31':'Today’s sync failed · showing Oct 31, 7:02 AM'}</p>`:'';
 return stale+body(w,size,cloud&&s!=='mixed'?' · 7:02 AM':'');
}
function ghost(w){return `<div class="home-ghost"><p class="widget-sub">Preview · once connected you’ll see</p><ul class="home-ghost-list">${GHOST[w.id].map(t=>`<li>${t}</li>`).join('')}</ul></div>`;}
function body(w,size,synced){
 switch(w.id){
 case 'figures':return size==='s'?'<div class="widget-figure">4</div><div class="widget-sub">upcoming events</div>':metrics([['4','upcoming events'],['84%','check-in rate']])+'<div class="home-details">'+list([['Reflections received','68%'],['Past events this term','8'],['Officers active this week','17']])+'</div>';
 case 'actions':{const rows=actionRows();return size==='s'?`<div class="widget-figure">${rows.length}</div><div class="widget-sub">waiting on you</div>`:list(size==='l'?rows:rows.slice(0,2))+(size==='l'?`<a class="small" href="${role()==='officer'?'finance.html':'finance.html#review'}">${role()==='officer'?'Your requests →':'Open requests →'}</a>`:`<p class="widget-sub home-detail-hint">${rows.length-2} more · choose L to see all.</p>`);}
 case 'analytics':return (size==='s'?`<div class="widget-figure">12.4k</div><div class="widget-sub">visitors · 7 days${synced}</div>`:metrics([['12,408','visitors · 7 days'],['28,630','page views']]))+'<div class="home-details"><div class="home-analytics">'+group('Top pages · views',[['/join','11,240'],['/buildspace','6,810'],['/','5,320']])+group('Referrers · visits',[['Direct','5,140'],['Instagram','3,220'],['LinkedIn','1,960']])+group('Countries · visitors',[['Canada','9,680'],['United States','1,440'],['India','310']])+'</div></div>'+(size==='m'?'<p class="widget-sub home-detail-hint">Choose L for pages, referrers & countries.</p>':'');
 case 'members':return (size==='s'?`<div class="widget-figure">1,086</div><div class="widget-sub">members · +23 this week${synced}</div>`:metrics([['1,086','members'],['+23','this week']]))+'<div class="home-details">'+list([['New today','6'],['Busiest day · Oct 28','11'],['Period','Oct 26–Nov 1']])+`<a class="small" href="members.html">${role()==='officer'?'View members →':'Edit member records →'}</a></div>`;
 case 'events':return size==='s'?'<div class="widget-figure">72</div><div class="widget-sub">Founder stories check-ins</div>':list([['Founder stories · Oct 28','72 / 86'],['Design for people · Oct 22','51 / 64']])+'<div class="home-details">'+list([['Fall venture night · Oct 15','97 / 112'],['Coffee & cofounders · Oct 9','32 / 38']])+'<a class="small" href="events.html">All events →</a></div>';
 case 'budget':return '<div class="widget-figure">$9,920</div><div class="widget-sub">available · $5,080 of $15,000 spent</div><div class="home-details">'+list([['Community','$1,480 / $4,000'],['Entrepreneurship','$2,150 / $5,500'],['Design','$860 / $3,000'],['Operations','$590 / $2,500']])+'</div>';
 case 'activity':return size==='s'?'<div class="widget-figure">3</div><div class="widget-sub">updates since yesterday</div>':list([['Maya submitted catering','24 min ago'],['You approved speaker travel','2 hr ago']])+'<div class="home-details">'+list([['Jordan imported 64 registrations','Yesterday'],['Sam added Priya Raman to Speakers','Yesterday']])+'</div>';
 case 'traffic':return size==='s'?`<div class="widget-figure">386k</div><div class="widget-sub">requests · 7 days${synced}</div>`:metrics([['386k','requests · 7 days'],['4.9 GB','bandwidth'],['86.2%','cache hit'],['124','threats blocked']])+'<div class="home-details"><div class="home-analytics home-analytics--2">'+group('Threats · by action',[['Bot challenge','96'],['WAF block','28']])+group('Countries · requests',[['Canada','291k'],['United States','52k']])+'</div></div>';
 case 'performance':return size==='s'?`<div class="widget-figure">1.8s</div><div class="widget-sub">LCP p75 · Good${synced}</div>`:metrics([['1.8s','LCP · Good'],['142ms','INP · Good'],['0.04','CLS · Good']],size==='l'?'home-metrics--3':'')+'<div class="home-details"><h3 class="home-sub-head">Per page · LCP / INP at p75</h3>'+list([['/','1.6s · 120ms'],['/join','1.9s · 150ms'],['/buildspace','2.2s · 168ms'],['/programs/mentors','Insufficient samples']])+'</div><p class="widget-sub home-detail-hint">p75 · real visits · 7 days.</p>';
 case 'usage':{const days=[['Oct 26',2.1],['Oct 27',2.4],['Oct 28',2.9],['Oct 29',3.6],['Oct 30',2.8],['Oct 31',2.2],['Nov 1',2.2]];
  const bars=`<svg class="home-bars" viewBox="0 0 140 40" preserveAspectRatio="none" aria-hidden="true">${days.map(([,v],i)=>`<rect class="chart-col" x="${i*20+3}" y="${40-v*10}" width="14" height="${v*10}" rx="2"></rect>`).join('')}</svg>`;
  return size==='s'?`<div class="widget-figure">18.2k</div><div class="widget-sub">Worker requests · 7 days${synced}</div>`:metrics([['18.2k','Worker requests'],['4ms','avg CPU time'],['92.4k','D1 rows read'],['128','D1 rows written']])+`<div class="home-details"><h3 class="home-sub-head">Worker requests per day</h3>${bars}<p class="visually-hidden">${days.map(([d,v])=>`${d}: ${v}k`).join(', ')}</p><p class="widget-sub">D1 storage 8.2 MB · daily snapshot, not live billing.</p></div>`;}
 }
}
function sizes(w,current,forGallery=false){return `<div class="widget-sizes" role="group" aria-label="${w.name} size">`+['s','m','l'].map(s=>`<button class="widget-size" aria-pressed="${s===current}" aria-label="${SIZE_NAME[s]}" data-size="${s}" data-id="${w.id}" ${forGallery?'data-gallery-size':''}>${s.toUpperCase()}</button>`).join('')+'</div>';}
function slot(size){const [c,r]=SPAN[size];return `<li class="widget-slot" aria-hidden="true" style="--span-c:${c};--span-r:${r}"></li>`;}
function widget(item,i){
 const w=registry.find(w=>w.id===item.id), s=state(), cloud=w.cat!=='Club';
 const failed=s==='error', busy=s==='loading';
 const foot=cloud?FOOT[s]:'Fall 2026';
 return `<li class="widget widget--${item.size}${failed?' state--error':''}${lifted===w.id?' is-lifted':''}" data-id="${w.id}" data-hue="${w.hue}" aria-label="${w.name}"${busy?' aria-busy="true"':''} style="--i:${i}"><div class="widget-edit"><button class="widget-handle" data-move="${w.id}" aria-label="Move ${w.name}; arrow keys move it, Enter drops it" draggable="true">⠿</button><span class="widget-label">${w.name}</span><button class="widget-remove" data-remove="${w.id}" aria-label="Remove ${w.name}">−</button>${sizes(w,item.size)}</div><div class="widget-head" ${editing?'inert':''}><span class="widget-icon" aria-hidden="true">${{blue:'↗',violet:'◇',green:'+',amber:'▦',teal:'◈',coral:'↗'}[w.hue]}</span><span class="widget-title">${w.name}</span></div><div class="widget-body" ${editing?'inert':''}>${content(w,item.size)}</div><div class="widget-foot" ${editing?'inert':''}><span>${foot}</span></div></li>`;
}
function render(){
 $('widgets').classList.toggle('is-editing',editing);$('edit-bar').hidden=!editing;$('edit').hidden=editing;$('gallery-open').hidden=editing;
 $('blank').hidden=layout.length>0;
 const items=layout.map(widget);
 if(editing&&lifted&&slotAt!=null)items.splice(slotAt,0,slot(layout.find(w=>w.id===lifted).size));
 $('widgets').innerHTML=items.join('')+(editing?'<li class="home-add-slot"><button class="widget-add" id="tile-add"><span class="widget-add-plus">+</span>Add widget<span class="widget-add-sub">Make room for more</span></button></li>':'');
 $('sync-line').textContent='Cloudflare and membership · '+SYNC[state()]+'. All figures are sample data.';
 $('role-note').textContent={officer:'Officer · club metrics and your own follow-ups. Purchase approvals belong to directors and leadership; member records are read-only.',director:'Director · purchase reviews for their scope, invites and member-record edits.',leadership:'Leadership · club-wide approvals, role changes and leadership grants.',owner:'Owner · leadership access plus ownership management. At least one owner must remain.'}[role()];
 document.documentElement.dataset.role=role()==='owner'?'leadership':role();
 document.querySelector('.user-chip-role').innerHTML=`<span class="role-badge" data-role="${role()==='owner'?'leadership':role()}">${role()==='owner'?'Leadership · Owner':role()[0].toUpperCase()+role().slice(1)}</span>`;
 if($('tile-add'))$('tile-add').onclick=openGallery;
 renderGallery();
}
function renderGallery(){
 $('gallery-items').innerHTML=registry.filter(w=>category==='All'||w.cat===category).map((w,i)=>{const added=layout.some(a=>a.id===w.id);return `<li class="gallery-item ${added?'is-added':''}" style="--tilt:${i%2?'.8deg':'-1deg'}"><div class="gallery-preview" aria-hidden="true"><div class="widget widget--m" data-hue="${w.hue}"><div class="widget-head"><span class="widget-title">${w.name}</span>${w.cat==='Club'?'':'<span class="widget-link">Sample</span>'}</div><div class="widget-body">${body(w,'s','')}</div></div></div><div><p class="gallery-name">${w.name}</p><p class="gallery-sub">${w.description}</p></div><div class="gallery-actions">${sizes(w,layout.find(item=>item.id===w.id)?.size||selected[w.id],true)}${added?'<span class="gallery-added" tabindex="-1">✓ On your home</span>':`<button class="btn btn--small" data-add="${w.id}" aria-label="Add ${w.name}">+ Add</button>`}</div></li>`;}).join('');
}
function openGallery(){renderGallery();$('gallery').showModal();}
function hideUndo(){clearTimeout(undoTimer);$('undo').hidden=true;}
$('gallery-open').onclick=openGallery;$('edit-add').onclick=openGallery;$('blank-add').onclick=openGallery;$('gallery-close').onclick=()=>$('gallery').close();
$('edit').onclick=()=>{editing=true;render();$('done').focus();};
$('done').onclick=()=>{editing=false;lifted=null;slotAt=null;hideUndo();render();say('Layout saved · preview only. Production saves to your account across devices.');$('edit').focus();};
$('state').onchange=render;$('role').onchange=render;
document.addEventListener('click',e=>{
 const b=e.target.closest('button,a');if(!b)return;
 if(b.dataset.size){const id=b.dataset.id;if(b.hasAttribute('data-gallery-size')){selected[id]=b.dataset.size;const item=layout.find(item=>item.id===id);if(item){item.size=b.dataset.size;render();}else renderGallery();document.querySelector(`#gallery-items [data-id="${id}"][data-size="${b.dataset.size}"]`).focus();}else{layout.find(a=>a.id===id).size=b.dataset.size;render();document.querySelector(`#widgets [data-id="${id}"] [data-size="${b.dataset.size}"]`).focus();say(`${registry.find(w=>w.id===id).name} resized to ${SIZE_NAME[b.dataset.size]}`);}}
 if(b.dataset.remove){const i=layout.findIndex(w=>w.id===b.dataset.remove);removed={item:layout[i],i};layout.splice(i,1);render();$('undo').hidden=false;$('undo').firstChild.textContent=`Removed ${registry.find(w=>w.id===removed.item.id).name} · `;clearTimeout(undoTimer);undoTimer=setTimeout(hideUndo,8000);$('done').focus();}
 if(b.dataset.add){const id=b.dataset.add;layout.push({id,size:selected[id]});render();say(`${registry.find(w=>w.id===id).name} added`);const items=[...document.querySelectorAll('#gallery-items .gallery-item')];const next=items.find(li=>li.querySelector('[data-add]'));(next?.querySelector('[data-add]')||items.find(li=>li.querySelector(`[data-id="${id}"]`))?.querySelector('.gallery-added')||$('gallery-close')).focus();}
 if(b.dataset.category){category=b.dataset.category;document.querySelectorAll('[data-category]').forEach(c=>c.setAttribute('aria-pressed',c===b));renderGallery();}
 if(b.hasAttribute('data-retry')){$('state').value='connected';render();say('Sample snapshot refreshed.');}
 if(b.hasAttribute('data-empty'))say('Events appear after an event is created. Budgets appear after allocation. Connected metrics appear after the next daily sync.');
});
$('undo-button').onclick=()=>{if(removed){layout.splice(removed.i,0,removed.item);removed=null;render();hideUndo();say('Widget restored.');}};
function move(id,delta){const i=layout.findIndex(w=>w.id===id),j=Math.max(0,Math.min(layout.length-1,i+delta));if(i===j)return;const [item]=layout.splice(i,1);layout.splice(j,0,item);render();document.querySelector(`[data-move="${id}"]`).focus();const before=layout[j-1]?registry.find(w=>w.id===layout[j-1].id).name:null;say(`${registry.find(w=>w.id===id).name}, position ${j+1} of ${layout.length}${before?`, after ${before}`:''}`);}
document.addEventListener('keydown',e=>{const h=e.target.closest('[data-move]');if(!h)return;const id=h.dataset.move;if(['ArrowLeft','ArrowRight','ArrowUp','ArrowDown'].includes(e.key)){e.preventDefault();lifted=id;move(id,['ArrowLeft','ArrowUp'].includes(e.key)?-1:1);}if(e.key==='Enter'||e.key===' '){e.preventDefault();lifted=lifted===id?null:id;say(lifted?'Widget lifted. Arrow keys move it.':'Widget dropped.');}document.querySelectorAll('#widgets .widget').forEach(w=>w.classList.toggle('is-lifted',w.dataset.id===lifted));});
// Pointer drag: the dragged widget lifts and a .widget-slot shows where it will land.
let slotEl=null;
function placeSlot(target,size){if(!slotEl){slotEl=document.createElement('li');slotEl.className='widget-slot';slotEl.setAttribute('aria-hidden','true');}const [c,r]=SPAN[size];slotEl.style.cssText=`--span-c:${c};--span-r:${r}`;if(target&&target.previousElementSibling!==slotEl)target.before(slotEl);}
function clearDrag(){slotEl?.remove();document.querySelectorAll('#widgets .is-lifted').forEach(w=>w.classList.remove('is-lifted'));}
document.addEventListener('dragstart',e=>{const h=e.target.closest('[data-move]');if(h){dragId=h.dataset.move;e.dataTransfer.setData('text/plain',dragId);h.closest('.widget').classList.add('is-lifted');}});
document.addEventListener('dragover',e=>{const t=e.target.closest('#widgets .widget');if(editing&&t&&dragId){e.preventDefault();if(t.dataset.id!==dragId)placeSlot(t,layout.find(w=>w.id===dragId).size);}});
document.addEventListener('drop',e=>{const target=e.target.closest('#widgets .widget');if(!target||!dragId)return;e.preventDefault();clearDrag();move(dragId,layout.findIndex(w=>w.id===target.dataset.id)-layout.findIndex(w=>w.id===dragId));dragId=null;});
document.addEventListener('dragend',()=>{dragId=null;clearDrag();});
let touchMove=null;
document.addEventListener('pointerdown',e=>{const h=e.target.closest('[data-move]');if(h&&e.pointerType==='touch'){touchMove={id:h.dataset.move,x:e.clientX,y:e.clientY};h.setPointerCapture(e.pointerId);h.closest('.widget').classList.add('is-lifted');}});
document.addEventListener('pointermove',e=>{if(!touchMove)return;const t=document.elementFromPoint(e.clientX,e.clientY)?.closest('#widgets .widget');if(t&&t.dataset.id!==touchMove.id)placeSlot(t,layout.find(w=>w.id===touchMove.id).size);});
document.addEventListener('pointerup',e=>{if(!touchMove)return;const start=touchMove;touchMove=null;clearDrag();const target=document.elementFromPoint(e.clientX,e.clientY)?.closest('#widgets .widget');if(target&&Math.hypot(e.clientX-start.x,e.clientY-start.y)>10)move(start.id,layout.findIndex(w=>w.id===target.dataset.id)-layout.findIndex(w=>w.id===start.id));});
document.addEventListener('pointercancel',()=>{touchMove=null;clearDrag();});
render();
if(params.get('mode')==='edit'){$('edit').click();if(params.get('lift')){lifted=params.get('lift');slotAt=+(params.get('slot')||0);render();}}
if(params.get('mode')==='gallery')openGallery();
})();
