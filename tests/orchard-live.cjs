const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const assert = require('node:assert/strict')
const ts = require('typescript')
const root = path.resolve(__dirname, '..')
let page, fail = false, deferred, stopped = 0, timerId = 0
const timers = new Map(), cache = new Map()
const channel = {enabled:true,url:'https://live.example.com/orchard.m3u8?sign=test',hours:'每日 08:00–18:00'}
let home = {banners:[],live:{orchard:channel,stall:{enabled:false,url:'',hours:''}}}
const wx = {createVideoContext:()=>({stop(){stopped++}}),stopPullDownRefresh(){}}
function load(relative) {
 const file=path.resolve(root,relative)
 if(cache.has(file)) return cache.get(file).exports
 const module={exports:{}};cache.set(file,module)
 const source=ts.transpileModule(fs.readFileSync(file,'utf8'),{compilerOptions:{target:ts.ScriptTarget.ES2020,module:ts.ModuleKind.CommonJS}}).outputText
 vm.runInNewContext(source,{module,exports:module.exports,wx,
  setTimeout:callback=>{timers.set(++timerId,callback);return timerId},clearTimeout:id=>timers.delete(id),
  Page:value=>{page=value;page.setData=updates=>Object.assign(page.data,updates)},
  require:name=>name.endsWith('/api')?{callApi:async()=>{if(fail)throw Error('offline');if(deferred)return deferred;return home}}:load(path.resolve(path.dirname(file),name+'.ts')),
 },{filename:file})
 return module.exports
}
const event = () => ({currentTarget:{dataset:{id:page.data.selected,version:page.data.version,src:page.data.channel.url}}})
async function run(){
 const helper=load('miniprogram/utils/orchard-live.ts')
 const validation=load('admin/src/live-validation.ts')
 for(const url of ['http://live.example.com/x.m3u8','rtsp://camera/live','https:///x.m3u8','https://user:pass@host/x.m3u8','https://host/x.mp4','https://host/x.m3u8#x','https://host/x.m3u8?x=a b']){
  assert.equal(helper.validLiveUrl(url),false,url)
  assert.throws(()=>validation.validateLiveConfig({orchard:{...channel,url},stall:home.live.stall}))
 }
 validation.validateLiveConfig(home.live)
 assert.throws(()=>validation.validateLiveConfig({orchard:{...channel,url:''},stall:home.live.stall}))
 assert.throws(()=>validation.validateLiveConfig({orchard:{...channel,hours:'时'.repeat(101)},stall:home.live.stall}))
 load('miniprogram/pages/orchard-live/index.ts')
 await page.onShow()
 assert.equal(page.data.channel.enabled,true);assert.equal(page.data.playing,false,'stream must wait for a user click')
 page.watchLive();assert.equal(page.data.status,'connecting')
 const old=event();page.videoPlay(old);assert.equal(page.data.status,'playing');assert.equal(timers.size,0)
 page.videoWaiting(old);assert.equal(page.data.status,'connecting')
 page.selectView({currentTarget:{dataset:{id:'stall'}}})
 assert.equal(page.data.channel.enabled,false);assert.equal(page.data.playing,false);assert.equal(stopped,1)
 page.videoError(old);assert.equal(page.data.status,'ready','old camera callbacks are ignored')
 page.watchLive();assert.equal(page.data.playing,false,'disabled cameras cannot play')
 page.selectView({currentTarget:{dataset:{id:'orchard'}}});page.watchLive()
 const timeOut=[...timers.values()][0];timeOut();assert.equal(page.data.status,'failed');assert.equal(page.data.playing,false)
 page.watchLive();page.videoError(event());assert.equal(page.data.status,'failed')
 page.watchLive();const pendingEvent=event();page.onHide();page.videoPlay(pendingEvent)
 assert.equal(page.data.playing,false);assert.equal(timers.size,0)
 fail=true;await page.onShow();assert.ok(page.data.error);assert.equal(page.data.channel.enabled,false)
 fail=false;home={banners:[]};await page.loadConfig();assert.equal(page.data.channel.enabled,false,'old configs without live stay closed')
 let resolve;deferred=new Promise(r=>{resolve=r});const pending=page.loadConfig();page.onHide();resolve({banners:[],live:{orchard:channel}});await pending
 assert.equal(page.data.channel.enabled,false,'a hidden page ignores pending configuration responses')
 deferred=undefined;await page.onShow();assert.equal(page.data.loading,false)
 console.log('Orchard live: URL validation, two views, waiting, disconnect, retry, hidden/stale requests passed')
}
run().catch(error=>{console.error(error);process.exitCode=1})
