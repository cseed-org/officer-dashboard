import { expect, test } from '@playwright/test';
import { buildPlan, sampleAt, retargetPlan, skipPlan, stubPlan, type Input, type Box } from '../src/ui/onboarding/introPlan';
test.beforeEach(({}, info) => test.skip(info.project.name !== 'desktop', 'pure engine tests run once'));
const key = (b: Box) => `${b.x.toFixed(5)},${b.y.toFixed(5)}`;
const fixtures: Input[] = [
 {viewport:{w:1440,h:900,dpr:1},target:{x:20,y:26.75,w:78,h:18,kind:'wordmark'},motion:'full',seed:123},
 {viewport:{w:820,h:1180,dpr:2},target:{x:17.5,y:22,w:28,h:28,kind:'mark'},motion:'full',seed:123},
 {viewport:{w:390,h:844,dpr:3},target:{x:160.34375,y:17.5,w:69.328125,h:16,kind:'wordmark'},motion:'full',seed:123},
];
for (const input of fixtures) {
 test(`${input.viewport.w}: fruit, growth, visible joins and collision-free body`,()=>{
  const p=buildPlan(input);
  expect(p.fruit).toHaveLength(216);
  expect(new Set(p.fruit.map(f=>f.kind)).size).toBe(3);
  expect(p.fruit.map(f=>f.letter)).toEqual([...p.fruit.map(f=>f.letter)].sort());
  expect(new Set(p.fruit.map(f=>key(f.box))).size).toBe(216);
  expect(sampleAt(p,699).fruit.every(f=>f.visible&&!f.eaten&&f.opacity===1)).toBe(true);
  const eaten:number[]=[];
  for(let i=0;i<p.ticks.length;i++) {
   const t=p.ticks[i]!;
   expect(new Set(t.body.map(b=>key(b.box))).size,`tick ${i}`).toBe(t.body.length);
   expect(t.body.every(b=>b.visible&&b.opacity===(b.kind==='join'?.4:1))).toBe(true);
   if(i) expect(t.body.length-p.ticks[i-1]!.body.length).toBe(t.ate===null?0:1);
   if(t.ate!==null){eaten.push(t.ate);const f=p.fruit[t.ate]!;expect(t.at).toBe(f.eatAt);expect(t.body[0]!.box).toEqual(f.box);expect(sampleAt(p,t.at-.001).fruit[t.ate]!.visible).toBe(true);expect(sampleAt(p,t.at).fruit[t.ate]!.eaten).toBe(true);}
   if(i){const a=p.ticks[i-1]!.body[0]!.box,b=t.body[0]!.box;expect(Math.abs(b.x-a.x)+Math.abs(b.y-a.y)).toBeCloseTo(p.cell,8);}
   expect(t.body.every(pose=>[pose.box.x,pose.box.y,pose.box.w,pose.box.h].every(n=>Math.abs(n*input.viewport.dpr-Math.round(n*input.viewport.dpr))<1e-7)),`pixel snapping at tick ${i}`).toBe(true);
  }
  expect(eaten).toEqual(p.fruit.map(f=>f.id));
  const before=sampleAt(p,p.timeline.spelledAt);
  expect(before.body.some(b=>b.kind==='join'&&b.visible)).toBe(true);
  expect(before.body.some(b=>b.kind==='entry')).toBe(false);
  const inset=(p.cell-p.segment)/2;
  expect(new Set(before.body.filter(b=>b.kind==='letter').map(b=>key({...b.box,x:b.box.x-inset,y:b.box.y-inset})))).toEqual(new Set(p.word.cells.map(key)));
  const final=sampleAt(p,p.timeline.travelStart);
  expect(final.body.filter(b=>b.visible)).toHaveLength(216);
 });
 test(`${input.viewport.w}: deterministic immutable five-second plan and exact landing`,()=>{
  const p=buildPlan(input),q=buildPlan(input),t=p.timeline;
  expect(p).toEqual(q);expect(Object.isFrozen(p.ticks[1]!.body[0]!.box)).toBe(true);
  const times=[t.meltStart,t.snakeEnter,t.meltEnd,t.fruitIn[1],t.spelledAt,t.sproutAt,t.morphStart,t.travelStart,t.morphEnd,t.swapAt,t.done];
  expect(times).toEqual([...times].sort((a,b)=>a-b));expect(t.done).toBeLessThanOrEqual(5200);expect(t.done-t.swapAt).toBeLessThanOrEqual(120);
  for(const ms of [0,450,700,2000,3600,4050,4400,4780,4900]) expect(sampleAt(p,ms)).toEqual(sampleAt(q,ms));
  expect(t.ffTo).toBe(t.swapAt);
  expect(sampleAt(p,t.ffTo).done).toBe(false);
  expect(t.done-t.ffTo).toBe(120);
  expect(sampleAt(p,t.ffTo+120)).toEqual(sampleAt(p,Infinity));
  expect(sampleAt(p,t.ffTo).targetOpacity).toBe(0);
  expect(sampleAt(p,t.ffTo+60).targetOpacity).toBeCloseTo(.5,6);
  const end=sampleAt(p,t.morphEnd),from=input.target.kind==='mark'?p.sprout:p.logoBox,tx=input.target.kind==='mark'?end.sprout.transform:end.transform;
  expect(from.x*tx.sx+tx.tx).toBeCloseTo(input.target.x,6);expect(from.y*tx.sy+tx.ty).toBeCloseTo(input.target.y,6);expect(from.w*tx.sx).toBeCloseTo(input.target.w,6);expect(from.h*tx.sy).toBeCloseTo(input.target.h,6);
  for(const at of [t.swapAt,t.swapAt+30,t.swapAt+60,t.swapAt+90,t.done]){const s=sampleAt(p,at);expect(s.target).toEqual(input.target);expect(s.transform).toEqual(end.transform);const outgoing=input.target.kind==='mark'?s.sprout.opacity:s.wordmarkOpacity;expect(s.targetOpacity+outgoing*(1-s.targetOpacity)).toBeCloseTo(1,6);}
  const changed={x:44,y:16,w:28,h:28,kind:'mark' as const},next=retargetPlan(p,changed),dest=sampleAt(next,t.ffTo);
  expect(next.ticks).toBe(p.ticks);expect(sampleAt(next,t.morphStart).body).toEqual(sampleAt(p,t.morphStart).body);expect(dest.target).toEqual(changed);expect(p.sprout.x*dest.sprout.transform.sx+dest.sprout.transform.tx).toBeCloseTo(changed.x,6);
 });
 test(`${input.viewport.w}: reduced and skip have no fruit or snake, at most 600ms`,()=>{
  for(const p of [buildPlan({...input,motion:'reduced'}),skipPlan(input)]){expect(p.timeline.done).toBeLessThanOrEqual(600);for(const ms of [0,100,300,500]){const s=sampleAt(p,ms);expect(s.body).toEqual([]);expect(s.fruit).toEqual([]);expect(s.sprout.opacity).toBe(0);expect(s.target).toEqual(input.target);}expect(sampleAt(p,p.timeline.done).targetOpacity).toBe(1);}
  expect(sampleAt(stubPlan(input),4900).done).toBe(true);
 });
}
test('seed changes artwork only and invalid geometry fails explicitly',()=>{
 const a=buildPlan(fixtures[0]!),b=buildPlan({...fixtures[0]!,seed:456});expect(a.fruit.map(f=>f.kind)).not.toEqual(b.fruit.map(f=>f.kind));expect(a.ticks).toEqual(b.ticks);expect(()=>buildPlan({...fixtures[0]!,viewport:{w:0,h:900,dpr:1}})).toThrow(RangeError);
});

// Independent glyph expectation: a route that conserves fruit but closes the c
// or fuses two letters must fail even if its own plan.word agrees with it.
const glyphRows = [
 ['111','100','100','100','111'], ['111','100','111','001','111'],
 ['111','101','111','100','111'], ['111','101','111','100','111'],
 ['001','001','111','101','111'],
];
for (const input of fixtures) test(`${input.viewport.w}: readable glyphs, baseline joins, stable ids and visible entry`,()=>{
 const p=buildPlan(input),t=p.timeline,spelled=sampleAt(p,t.spelledAt);
 const inset=(p.cell-p.segment)/2;
 const grid=(b:Box)=>[Math.round((b.x-p.origin.x-inset)/p.cell),Math.round((b.y-p.origin.y-inset)/p.cell)];
 for(let letter=0;letter<5;letter++) {
  const expected:string[]=[];
  glyphRows[letter]!.forEach((row,y)=>[...row].forEach((v,x)=>{
   if(v==='1') for(let dy=0;dy<2;dy++) for(let dx=0;dx<2;dx++) expected.push(`${letter*9+x*2+dx},${y*2+dy}`);
  }));
  expect(new Set(spelled.body.filter(b=>b.letter===letter&&b.visible).map(b=>grid(b.box).join(',')))).toEqual(new Set(expected));
 }
 const letters=spelled.body.filter(b=>b.kind==='letter');
 for(const join of spelled.body.filter(b=>b.kind==='join')) {
  const [x,y]=grid(join.box);
  expect(y).toBe(10);
  expect(join.opacity).toBeLessThan(.5);
  const touching=new Set(letters.filter(l=>{const [lx,ly]=grid(l.box);return Math.abs(lx!-x!)+Math.abs(ly!-y!)===1;}).map(l=>l.letter));
  expect(touching.size).toBeLessThanOrEqual(1);
 }
 for(let i=1;i<p.ticks.length;i++) {
  const a=p.ticks[i-1]!.body,b=p.ticks[i]!.body,previous=new Map(a.map(pose=>[pose.id,pose]));
  expect(b.filter(pose=>!previous.has(pose.id))).toHaveLength(1);
  expect(b.filter(pose=>previous.has(pose.id))).toEqual(b.filter(pose=>previous.has(pose.id)).map(pose=>previous.get(pose.id)));
 }
 const first=p.ticks[0]!;
 expect(first.at).toBe(t.snakeEnter);
 expect(first.body[0]!.box.x+first.body[0]!.box.w).toBeLessThan(0);
 expect(t.snakeEnter).toBeLessThan(t.meltEnd);
 expect(p.ticks.some(k=>k.at<t.fruitIn[1]&&k.body[0]!.box.x>=0)).toBe(true);
 expect(t.fruitIn[0]).toBeLessThan(t.meltEnd);
 expect(t.morphStart-t.spelledAt).toBeGreaterThanOrEqual(300);
 expect(sampleAt(p,t.spelledAt+299).body).toBe(spelled.body);
 // The grown stem sits on the d before moving to the logo's separate sapling.
 const sprout=sampleAt(p,t.morphStart).sprout;
 expect(sprout.box.x+sprout.box.w/2).toBeCloseTo(p.origin.x+41*p.cell,6);
 const settled=sampleAt(p,t.morphStart+(t.travelStart-t.morphStart)*.6);
 expect(settled.wordOpacity).toBe(1);
 expect(settled.wordmarkOpacity).toBe(0);
 expect(settled.body.filter(b=>b.kind==='letter').some(b=>key(b.box)!==key(spelled.body.find(a=>a.id===b.id)!.box))).toBe(true);
 expect(settled.body.filter(b=>b.kind==='join').every(b=>!b.visible)).toBe(true);
 expect(settled.sprout.box).toEqual(p.sprout);
 for(let i=2;i<p.fruit.length;i++) expect(p.fruit[i]!.kind===p.fruit[i-1]!.kind&&p.fruit[i]!.kind===p.fruit[i-2]!.kind).toBe(false);
 // Sampling reuses frozen poses until geometry actually changes; returned
 // samples themselves are inexpensive values, not recursively frozen copies.
 expect(sampleAt(p,1000).body).toBe(sampleAt(p,1000).body);
 expect(Object.isFrozen(sampleAt(p,1000))).toBe(false);
});
test('D-18: stub and engine preserve sapling and wordmark aspect throughout travel',()=>{
 for(const input of fixtures) for(const build of [stubPlan,buildPlan]) {
  const p=build(input),t=p.timeline;
  expect(p.sprout.w).toBe(p.sprout.h);
  for(const at of [t.morphStart,t.travelStart,t.travelStart+100,(t.travelStart+t.morphEnd)/2,t.morphEnd,t.swapAt+60]) {
   const s=sampleAt(p,at);
   expect(Math.abs(s.transform.sx-s.transform.sy)).toBeLessThan(1e-3);
   if(input.target.kind==='mark') {
    expect(s.transform).toEqual(s.sprout.transform);
    expect(Math.abs(s.sprout.transform.sx-s.sprout.transform.sy)).toBeLessThan(1e-10);
   }
  }
  if(input.target.kind==='wordmark') expect(Math.abs(p.morph.sx-p.morph.sy)).toBeLessThan(1e-3);
  else {const landed=sampleAt(p,t.morphEnd).transform;for(const field of ['tx','ty','sx','sy'] as const) expect(landed[field]).toBeCloseTo(p.sproutMorph[field],10);}
 }
 for(const input of fixtures) for(const p of [skipPlan(input),buildPlan({...input,motion:'reduced'})]) {
  expect(p.timeline.ffTo).toBe(p.timeline.swapAt);
  expect(p.timeline.done-p.timeline.ffTo).toBe(120);
  expect(sampleAt(p,p.timeline.ffTo).done).toBe(false);
  expect(sampleAt(p,p.timeline.ffTo+120)).toEqual(sampleAt(p,Infinity));
 }
});
