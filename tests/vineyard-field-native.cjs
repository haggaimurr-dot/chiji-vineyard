'use strict';
// Chromium worker with real OffscreenCanvas/WebGL, mocked wx APIs and no DOM.
// This verifies our adapter and input flow, not a physical WeChat device.
const {chromium}=require('playwright');
const fs=require('node:fs');
const path=require('node:path');
const assert=require('node:assert/strict');
const {createHash}=require('node:crypto');
const model=require('../minigame/field/model');
const bundle=fs.readFileSync(path.resolve(__dirname,'../minigame/field/field-wechat.js'),'utf8');
let s=model.applyAction(model.createState(),{type:'selectCharacter',characterId:'lin'});
s=model.applyAction(s,{type:'interact',targetId:'seedling'});
const packet={schemaVersion:1,state:s,position:{x:-2,z:1.05},yaw:0};
let browser;
(async()=>{
  browser=await chromium.launch({headless:true,args:['--enable-unsafe-swiftshader']});
  const page=await browser.newPage();
  const result=await page.evaluate(async({source,packet,key})=>{
    const setup=`var module={exports:{}};const saved=new Map([[${JSON.stringify(key)},${JSON.stringify(JSON.stringify(packet))}]]);const events={};const frames=new Map();let frameId=0;self.requestAnimationFrame=fn=>{const id=++frameId;frames.set(id,setTimeout(()=>{frames.delete(id);fn(performance.now())},16));return id};self.cancelAnimationFrame=id=>{if(frames.has(id))clearTimeout(frames.get(id));frames.delete(id)};self.wx={createCanvas:()=>new OffscreenCanvas(390,844),getWindowInfo:()=>({windowWidth:390,windowHeight:844,pixelRatio:1,safeArea:{top:20,bottom:824}}),getStorageSync:k=>saved.get(k)||'',setStorageSync:(k,v)=>saved.set(k,v),onTouchStart:f=>events.start=f,onTouchMove:f=>events.move=f,onTouchEnd:f=>events.end=f,onTouchCancel:f=>events.cancel=f,onShow:f=>events.show=f,onHide:f=>events.hide=f,onWindowResize:f=>events.resize=f,offTouchStart:()=>events.start=null,offTouchMove:()=>events.move=null,offTouchEnd:()=>events.end=null,offTouchCancel:()=>events.cancel=null,offShow:()=>events.show=null,offHide:()=>events.hide=null,offWindowResize:()=>events.resize=null,showModal:x=>postMessage({modal:x.content})};`;
    const footer=`const game=module.exports.start();function respond(){postMessage({state:game.getState(),status:game.getStatus(),controls:game.getControls(),frames:frames.size,noDOM:typeof document==='undefined',native:game.getDebug&&game.getDebug().native})}function touch(phase,id,pointer){const c=game.getControls().find(c=>c.id===id);if(!c)throw new Error('Missing native control '+id);events[phase]({changedTouches:[{identifier:pointer||9,clientX:c.x+c.width/2,clientY:c.y+c.height/2}]})}onmessage=e=>{try{const m=e.data;if(m==='read')respond();else if(m==='start')touch('start','field-interact');else if(m==='end')touch('end','field-interact');else if(m==='extra-start')touch('start','field-interact',10);else if(m==='extra-end')touch('end','field-interact',10);else if(m==='hide'){events.hide();respond()}else if(m==='show'){events.show();respond()}else if(m==='destroy'){game.destroy();postMessage({destroyed:true,frames:frames.size,listeners:!!events.start||!!events.end||!!events.hide})}}catch(e){postMessage({error:e.stack})}};setTimeout(respond,900);`;
    const worker=new Worker(URL.createObjectURL(new Blob([setup,source,footer],{type:'application/javascript'})));
    let latest=null;const errors=[];
    worker.onmessage=e=>{if(e.data.error||e.data.modal)errors.push(e.data);else latest=e.data;};
    worker.onerror=e=>errors.push({error:e.message});
    const delay=ms=>new Promise(resolve=>setTimeout(resolve,ms));
    async function until(fn,timeout=20000){const end=performance.now()+timeout;while(performance.now()<end){if(errors.length)throw new Error(JSON.stringify(errors));worker.postMessage('read');await delay(120);if(latest&&fn(latest))return JSON.parse(JSON.stringify(latest));}throw new Error('Native check timed out: '+JSON.stringify(latest));}
    const checks=[];
    try{
      const start=await until(x=>x.controls&&x.controls.some(c=>c.id==='field-interact'));
      checks.push({name:'DOM-free native WebGL startup',value:start.noDOM&&start.native});
      checks.push({name:'safe-area HUD',value:start.controls.every(c=>c.x>=0&&c.y>=20&&c.x+c.width<=390.1&&c.y+c.height<=824.1)});
      worker.postMessage('start');await until(x=>x.status.busy);worker.postMessage('end');const cancelled=await until(x=>!x.status.busy);
      checks.push({name:'native short hold leaves planting step and resources intact',value:cancelled.state.nursery.step===1&&cancelled.state.coins===700});
      worker.postMessage('start');await until(x=>x.status.busy);worker.postMessage('extra-start');const dug=await until(x=>x.state.nursery.step===2&&!x.status.busy);
      worker.postMessage('extra-end');worker.postMessage('end');await delay(200);const released=await until(x=>!x.status.busy);
      checks.push({name:'native completed hold release and extra finger do not start the next step',value:dug.state.version===released.state.version&&released.state.nursery.step===2});
      worker.postMessage('start');await until(x=>x.status.busy);worker.postMessage('hide');const hidden=await until(x=>x.frames===0&&!x.status.busy);
      checks.push({name:'background cancels frames and unfinished work',value:hidden.state.nursery.step===2});
      worker.postMessage('show');await until(x=>x.frames>0);checks.push({name:'foreground resumes the same saved challenge',value:latest.state.nursery.step===2});
      worker.postMessage('destroy');for(let i=0;i<40&&!latest.destroyed;i++)await delay(50);
      checks.push({name:'native disposal removes frames and listeners',value:latest.destroyed&&latest.frames===0&&!latest.listeners});
      return {checks,errors};
    }finally{worker.terminate();}
  },{source:bundle,packet,key:model.STORAGE_KEY});
  for(const c of result.checks){assert.equal(c.value,true,c.name);console.log('PASS '+c.name);}assert.deepEqual(result.errors,[]);
  const dir=path.resolve(__dirname,'../docs/test-evidence/2026-10-12-vineyard-field');fs.mkdirSync(dir,{recursive:true});
  fs.writeFileSync(path.join(dir,'native-results.json'),JSON.stringify({runtime:'Chromium worker, real OffscreenCanvas WebGL, simulated wx, no physical device',bundleHash:createHash('sha256').update(bundle).digest('hex'),completed:true,...result},null,2));
})().catch(e=>{console.error(e);process.exitCode=1;}).finally(async()=>{if(browser)await browser.close();});
