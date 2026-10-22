'use strict';
const {chromium}=require('playwright');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const {spawn}=require('node:child_process');
const {createHash}=require('node:crypto');
const model=require('../minigame/field/model');
const root=path.resolve(__dirname,'..');
const artifacts=path.join(root,'docs/test-evidence/2026-10-12-vineyard-field');
fs.mkdirSync(artifacts,{recursive:true});
const origin=process.env.VINEYARD_PREVIEW_URL||'http://127.0.0.1:5199';
const url=origin+'/field.html';
let browser,server;const contexts=[];
const results={previewURL:url,runtime:'Chromium WebGL, not WeChat device acceptance',bundleHash:createHash('sha256').update(fs.readFileSync(path.join(root,'minigame/preview/field-preview.js'))).digest('hex'),checks:[],screenshots:[],errors:[]};
const pause=ms=>new Promise(r=>setTimeout(r,ms));
function check(message){results.checks.push(message);console.log('PASS '+message);}
const state=p=>p.evaluate(()=>window.vineyardField.getState());
const status=p=>p.evaluate(()=>window.vineyardField.getStatus());
const button=(p,id)=>p.locator('[data-control="'+id+'"]');
async function click(p,id){await button(p,id).click();await p.waitForTimeout(80);}
async function fresh(name,viewport,fixture){
  const size=viewport||{width:390,height:844};
  const ctx=await browser.newContext({viewport:size,deviceScaleFactor:1,hasTouch:true,isMobile:size.width<740,timezoneId:'Asia/Shanghai'});contexts.push(ctx);
  if(fixture)await ctx.addInitScript(({key,packet})=>localStorage.setItem(key,JSON.stringify(packet)),{key:model.STORAGE_KEY,packet:fixture});
  const p=await ctx.newPage();p.on('pageerror',e=>results.errors.push({name,error:e.message}));
  p.on('console',e=>{if(e.type()==='error')results.errors.push({name,error:e.text()});});
  await p.goto(url);await p.waitForFunction(()=>window.vineyardField&&window.vineyardField.getState());await p.waitForTimeout(180);return p;
}
async function screenshot(p,name){await p.screenshot({path:path.join(artifacts,name)});results.screenshots.push(name);}
async function go(p,id){
  if(!(await status(p)).guide)await click(p,'field-guide');
  await click(p,'field-go-'+id);
  await p.waitForFunction(()=>!window.vineyardField.getStatus().navigating,null,{timeout:20000});
  const s=await status(p);assert.equal(s.error,'');assert.equal(s.nearest&&s.nearest.id,id,'physically reached '+id);assert.equal(s.nearest.enabled,true);
}
async function hold(p,done){
  const n=button(p,'field-interact');await n.waitFor({state:'visible'});assert.equal(await n.isDisabled(),false);
  const box=await n.boundingBox();await p.mouse.move(box.x+box.width/2,box.y+box.height/2);await p.mouse.down();
  try{await p.waitForFunction(()=>window.vineyardField.getStatus().busy,null,{timeout:2000});await p.waitForFunction(done,null,{timeout:15000});}finally{await p.mouse.up();}
  await p.waitForTimeout(100);assert.equal((await status(p)).error,'');
}
async function bounds(p){
  const v=p.viewportSize();const controls=await p.evaluate(()=>window.vineyardField.getControls());
  for(const c of controls){assert(c.x>=-0.1&&c.y>=-0.1,c.id+' starts in viewport');assert(c.x+c.width<=v.width+0.1&&c.y+c.height<=v.height+0.1,c.id+' fits viewport');}
}
function plantingState(){let s=model.applyAction(model.createState(),{type:'selectCharacter',characterId:'lin'});for(const step of model.PLANTING_STEPS)s=model.applyAction(s,{type:'interact',targetId:step.targetId});return s;}
function harvestState(){return model.applyAction(plantingState(),{type:'nextChapter'});}
function fixture(s,p){return {schemaVersion:1,state:s,position:p||{x:0,z:7},yaw:0};}
(async()=>{
  try{const response=await fetch(url,{signal:AbortSignal.timeout(2000)});if(!response.ok)throw new Error('not available');}
  catch(_){server=spawn(process.execPath,['minigame/scripts/preview.cjs'],{cwd:root,windowsHide:true,stdio:'ignore'});for(let i=0;i<30;i++){try{if((await fetch(url)).ok)break;}catch(_){}await pause(200);}}
  browser=await chromium.launch({headless:true,args:['--enable-unsafe-swiftshader']});
  const p=await fresh('complete-playthrough');
  await screenshot(p,'character-mobile.png');await bounds(p);
  for(const id of ['xia','qiao','chen','lin']){await click(p,'field-character-'+id);assert.equal((await status(p)).previewCharacter,id);await p.waitForTimeout(100);assert.equal((await p.evaluate(()=>window.vineyardField.getDebug())).world.characterId,id);}
  await click(p,'field-character-xia');await click(p,'field-choose');assert.equal((await state(p)).characterId,'xia');
  check('Four 3D character previews and persistent choice');
  const before=(await status(p)).position;
  await p.keyboard.down('KeyW');await p.waitForTimeout(500);await p.keyboard.up('KeyW');assert((await status(p)).position.z<before.z-0.2);
  const beforeJoystick=(await status(p)).position;
  await p.mouse.move(79,761);await p.mouse.down();await p.mouse.move(105,734,{steps:5});await p.waitForTimeout(250);await p.mouse.up();
  const afterJoystick=(await status(p)).position;assert(Math.hypot(afterJoystick.x-beforeJoystick.x,afterJoystick.z-beforeJoystick.z)>0.1);
  const body=await p.locator('#field-canvas').boundingBox();
  await p.mouse.move(body.width/2,body.height/2);await p.mouse.down();await p.mouse.move(body.width/2+60,body.height/2,{steps:8});await p.mouse.up();
  check('Keyboard, virtual joystick movement and camera drag use scene input');
  await go(p,'seedling');
  let box=await button(p,'field-interact').boundingBox();await p.mouse.move(box.x+box.width/2,box.y+box.height/2);await p.mouse.down();await p.waitForTimeout(260);await p.mouse.up();await p.waitForTimeout(200);
  assert.equal((await state(p)).nursery.step,0);assert.equal((await status(p)).busy,false);check('Releasing a long press interrupts work without state or resource changes');
  for(let i=0;i<model.PLANTING_STEPS.length;i++){
    const step=model.PLANTING_STEPS[i];await go(p,step.targetId);
    const expected=i+1;
    await hold(p,() => !window.vineyardField.getStatus().busy);
    // waitForFunction above can race with a render; assert actual persisted step.
    assert.equal((await state(p)).nursery.step,expected,step.label+' finishes once');
    if(expected===5){assert.equal((await state(p)).nursery.hasWater,true);await screenshot(p,'carry-water.png');}
    if(expected===6){assert.equal((await p.evaluate(()=>window.vineyardField.getDebug())).world.wetSoil,true);await screenshot(p,'wet-soil.png');}
  }
  assert.equal((await state(p)).stage,'complete');await screenshot(p,'planting-complete.png');check('Seven-step planting returns water to the roots and persists wet soil and chapter reward');
  const completed=await state(p);await p.reload();await p.waitForFunction(()=>window.vineyardField&&window.vineyardField.getState()?.stage==='complete');assert.deepEqual(await state(p),completed);
  await click(p,'field-next');assert.equal((await state(p)).chapter,'harvest');assert.equal((await state(p)).nursery.watered,true);
  await screenshot(p,'mature-vineyard.png');
  for(const id of ['drain','shelter','sun','creek']){
    await go(p,id);const v=(await state(p)).version;
    await hold(p,()=>!window.vineyardField.getStatus().busy);
    assert.equal((await state(p)).version,v+1);
  }
  assert.equal((await state(p)).rainChallenge.clock,6);assert.equal((await state(p)).rainChallenge.rainArrived,true);
  assert.equal((await state(p)).rainChallenge.plots.find(x=>x.id==='slope').damage,5);
  await screenshot(p,'rain-and-harvest.png');check('Drainage and shelter consume time and money, rain reaches unharvested grapes at its announced time');
  await go(p,'delivery');await click(p,'field-interact');await bounds(p);await screenshot(p,'delivery-choice.png');
  await click(p,'field-deliver-contract');await p.waitForFunction(()=>window.vineyardField.getState().rainChallenge.status==='passed');
  assert.equal((await state(p)).rainChallenge.deliveredKg,22);assert.equal((await state(p)).stats.rewards,350);await screenshot(p,'harvest-report.png');
  check('Grade-qualified delivery completes chapter and pays a single first-clear reward');
  const saved=await state(p);await p.reload();await p.waitForFunction(()=>window.vineyardField&&window.vineyardField.getState()?.rainChallenge.status==='passed');assert.deepEqual(await state(p),saved);
  await click(p,'field-retry');assert.equal((await state(p)).rainChallenge.attempt,2);assert.deepEqual((await state(p)).facilities,{drainage:true,shelter:true});assert.equal((await state(p)).stats.rewards,350);
  await click(p,'field-characters');await click(p,'field-character-chen');await click(p,'field-choose');assert.equal((await state(p)).characterId,'chen');assert.equal((await state(p)).rainChallenge.attempt,2);
  check('Reload, next mature season and character changes preserve farm assets');
  let failed=harvestState();failed=model.applyAction(failed,{type:'harvest',plotId:'sun'});while(failed.rainChallenge.status==='active')failed=model.applyAction(failed,{type:'wait'});
  const f=await fresh('failure-salvage',null,fixture(failed));await screenshot(f,'failure-report.png');await click(f,'field-menu-close');await go(f,'delivery');await click(f,'field-interact');assert.equal(await button(f,'field-deliver-contract').isDisabled(),true);await click(f,'field-deliver-wholesale');await f.waitForFunction(()=>!window.vineyardField.getStatus().busy&&window.vineyardField.getState().inventory.length===0);
  assert.equal((await state(f)).rainChallenge.status,'failed');check('A failed chapter keeps its farm and permits salvage sales');
  const ctx=await browser.newContext({viewport:{width:390,height:844}});contexts.push(ctx);await ctx.addInitScript(key=>localStorage.setItem(key,'{broken-json'),model.STORAGE_KEY);const bad=await ctx.newPage();bad.on('pageerror',e=>results.errors.push({name:'damaged-save',error:e.message}));await bad.goto(url);await bad.waitForFunction(()=>window.vineyardField?.getStatus().saveBlocked);await screenshot(bad,'damaged-save.png');assert.equal(await bad.evaluate(key=>localStorage.getItem(key),model.STORAGE_KEY),'{broken-json');await click(bad,'field-reset-confirm');assert.equal((await status(bad)).saveBlocked,false);assert.equal((await state(bad)).stage,'character');check('Damaged saves are preserved until an explicit reset');
  const disk=await browser.newContext({viewport:{width:390,height:844}});contexts.push(disk);
  await disk.addInitScript(({key,packet})=>{localStorage.setItem(key,JSON.stringify(packet));const original=Storage.prototype.setItem;let rejected=false;Storage.prototype.setItem=function(k,v){if(k===key&&!rejected&&JSON.parse(v).state.version>0){rejected=true;throw new Error('injected storage failure');}return original.call(this,k,v);};},{key:model.STORAGE_KEY,packet:fixture(model.createState())});
  const d=await disk.newPage();d.on('pageerror',e=>results.errors.push({name:'save-failure',error:e.message}));await d.goto(url);await d.waitForFunction(()=>window.vineyardField&&window.vineyardField.getState());await click(d,'field-choose');assert.equal((await state(d)).version,0);assert.equal((await state(d)).characterId,null);assert.match((await status(d)).error,/保存/);await screenshot(d,'save-failure.png');await click(d,'field-retry');assert.equal((await state(d)).version,1);assert.equal((await state(d)).characterId,'lin');assert.equal((await status(d)).error,'');check('Failed disk writes leave gameplay unchanged and retry the same action');
  for(const [w,h] of [[320,568],[1360,900]]){
    const q=await fresh('viewport-'+w,{width:w,height:h});await bounds(q);await screenshot(q,'character-'+w+'.png');await click(q,'field-choose');await bounds(q);await click(q,'field-guide');await bounds(q);await screenshot(q,'guide-'+w+'.png');
  }
  check('Character, task and navigation controls fit small phones and desktops');
  assert.deepEqual(results.errors,[]);check('No browser JavaScript or WebGL errors');
  results.completed=true;
})().catch(e=>{results.failure=e.stack;console.error(e);process.exitCode=1;}).finally(async()=>{
  fs.writeFileSync(path.join(artifacts,'ui-results.json'),JSON.stringify(results,null,2));
  for(const c of contexts)await c.close().catch(()=>{});if(browser)await browser.close();if(server)server.kill();
});
