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
  expect(p.fruit).toHaveLength(70);
  expect(new Set(p.fruit.map(f=>f.kind)).size).toBe(3);
  expect(p.fruit.map(f=>f.letter)).toEqual([...p.fruit.map(f=>f.letter)].sort());
  expect(new Set(p.fruit.map(f=>key(f.box))).size).toBe(70);
  expect(sampleAt(p,699).fruit.every(f=>f.visible&&!f.eaten&&f.opacity===1)).toBe(true);
  const eaten:number[]=[];
  for(let i=0;i<p.ticks.length;i++) {
   const t=p.ticks[i]!;
   expect(new Set(t.body.map(b=>key(b.box))).size,`tick ${i}`).toBe(t.body.length);
   expect(t.body.every(b=>b.visible&&b.opacity===1)).toBe(true);
   if(i) expect(t.body.length-p.ticks[i-1]!.body.length).toBe(t.ate===null?0:1);
   if(t.ate!==null){eaten.push(t.ate);const f=p.fruit[t.ate]!;expect(t.at).toBe(f.eatAt);expect(t.body[0]!.box).toEqual(f.box);expect(sampleAt(p,t.at-.001).fruit[t.ate]!.visible).toBe(true);expect(sampleAt(p,t.at).fruit[t.ate]!.eaten).toBe(true);}
   if(i){const a=p.ticks[i-1]!.body[0]!.box,b=t.body[0]!.box;expect(Math.abs(b.x-a.x)+Math.abs(b.y-a.y)).toBeCloseTo(p.cell,8);}
   for(const pose of t.body) for(const n of [pose.box.x,pose.box.y,pose.box.w,pose.box.h]) expect(n*input.viewport.dpr).toBeCloseTo(Math.round(n*input.viewport.dpr),7);
  }
  expect(eaten).toEqual(p.fruit.map(f=>f.id));
  const before=sampleAt(p,p.timeline.spelledAt);
  expect(before.body.some(b=>b.kind==='join'&&b.visible)).toBe(true);
  expect(before.body.some(b=>b.kind==='entry')).toBe(false);
  const final=sampleAt(p,p.timeline.travelStart);
  expect(new Set(final.body.filter(b=>b.visible).map(b=>key(b.box)))).toEqual(new Set(p.word.cells.map(key)));
  expect(final.body.filter(b=>b.visible)).toHaveLength(70);
 });
 test(`${input.viewport.w}: deterministic immutable five-second plan and exact landing`,()=>{
  const p=buildPlan(input),q=buildPlan(input),t=p.timeline;
  expect(p).toEqual(q);expect(Object.isFrozen(p.ticks[1]!.body[0]!.box)).toBe(true);
  const times=[t.meltStart,t.meltEnd,...t.fruitIn,t.snakeEnter,t.spelledAt,t.sproutAt,t.morphStart,t.travelStart,t.morphEnd,t.swapAt,t.done];
  expect(times).toEqual([...times].sort((a,b)=>a-b));expect(t.done).toBeLessThanOrEqual(5200);expect(t.done-t.swapAt).toBeLessThanOrEqual(120);
  for(const ms of [0,450,700,2000,3600,4050,4400,4780,4900]) expect(sampleAt(p,ms)).toEqual(sampleAt(q,ms));
  expect(sampleAt(p,t.ffTo)).toEqual(sampleAt(p,Infinity));expect(sampleAt(p,t.ffTo).done).toBe(true);
  const end=sampleAt(p,t.morphEnd),from=input.target.kind==='mark'?p.sprout:p.logoBox,tx=input.target.kind==='mark'?end.sprout.transform:end.transform;
  expect(from.x*tx.sx+tx.tx).toBeCloseTo(input.target.x,6);expect(from.y*tx.sy+tx.ty).toBeCloseTo(input.target.y,6);expect(from.w*tx.sx).toBeCloseTo(input.target.w,6);expect(from.h*tx.sy).toBeCloseTo(input.target.h,6);
  for(const at of [t.swapAt,t.swapAt+60,t.done]){const s=sampleAt(p,at);expect(s.target).toEqual(input.target);expect(s.transform).toEqual(end.transform);expect((input.target.kind==='mark'?s.sprout.opacity:s.wordmarkOpacity)+s.targetOpacity).toBeCloseTo(1,6);}
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
