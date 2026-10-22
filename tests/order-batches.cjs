const fs=require('node:fs'),path=require('node:path'),vm=require('node:vm'),assert=require('node:assert/strict'),ts=require('typescript')
const root=path.resolve(__dirname,'..')
function storage(){const m=new Map();return {getItem:k=>m.get(k)||null,setItem:(k,v)=>m.set(k,v),removeItem:k=>m.delete(k)}}
function runtime(globals){const cache=new Map();return function load(file){const filename=path.resolve(root,file);if(cache.has(filename))return cache.get(filename).exports;const module={exports:{}};cache.set(filename,module);const source=ts.transpileModule(fs.readFileSync(filename,'utf8'),{compilerOptions:{target:ts.ScriptTarget.ES2020,module:ts.ModuleKind.CommonJS}}).outputText;vm.runInNewContext(source,{module,exports:module.exports,console,crypto:require("node:crypto").webcrypto,btoa,atob,structuredClone,TextEncoder,URL,Error,setTimeout,clearTimeout,...globals,require:n=>load(path.resolve(path.dirname(filename),n+'.ts'))},{filename});return module.exports}}
;(async()=>{
 const sessionStorage=storage(),localStorage=storage(),load=runtime({sessionStorage,localStorage}),call=load('admin/src/mock.ts').mockCall
 const login=await call('admin.login',{username:'qa-batches',password:'Batch2026!'});localStorage.setItem('chiji_admin_token',login.token)
 const order=(await call('admin.order.search',{filter:'shipping_pending'})).items.find(o=>o.id==='o-1')
 const base={id:'',productId:'p-shine',number:'QA-01',orchard:'验收果园',harvestedAt:'2026-10-01',packedAt:'2026-10-02',expiresAt:'',note:'内部备注',status:'ACTIVE',version:0}
 const first=await call('admin.batch.save',{batch:base}),second=await call('admin.batch.save',{batch:{...base,number:'QA-02'}}),wrong=await call('admin.batch.save',{batch:{...base,productId:'p-kyoho',number:'OTHER'}})
 assert.equal(first.code,first.id)
 const externalTrace={platform:'验收平台',url:'https://trace.example.com/batch/QA-01?code=abc',code:'EXT-001'}
 const external=await call('admin.batch.save',{batch:{...base,number:'EXT-01',externalTrace}})
 assert.deepEqual(JSON.parse(JSON.stringify(external.externalTrace)),externalTrace)
 for(const url of ['javascript:alert(1)','http://example.com','https://user:secret@example.com','https://'])await assert.rejects(()=>call('admin.batch.save',{batch:{...base,number:'BAD',externalTrace:{...externalTrace,url}}}),/HTTPS/)
 await assert.rejects(()=>call('admin.batch.save',{batch:{...base,externalTrace:{...externalTrace,platform:''}}}),/平台名称/)
 const removed=await call('admin.batch.save',{batch:{...external,externalTrace:undefined}});assert.equal(removed.externalTrace,undefined)
 const ship={orderId:order.id,carrier:'顺丰速运',carrierCode:'SF',trackingNo:'SFQABATCH0001'}
 await assert.rejects(()=>call('admin.order.ship',ship),/先登记/)
 const a={skuId:order.items[0].skuId,batchId:first.id,quantity:1},b={...a,batchId:second.id}
 const save=items=>call('admin.order.batches.save',{orderId:order.id,expectedVersion:0,items})
 await assert.rejects(()=>save([a]),/合计/)
 await assert.rejects(()=>save([a,a]),/重复/)
 await assert.rejects(()=>save([{...a,batchId:wrong.id,quantity:2}]),/归属/)
 await assert.rejects(()=>save([{...a,quantity:1.5},b]),/数量/)
 const saved=await save([a,b]);assert.equal(saved.batchVersion,1);assert.equal(saved.batches.length,2)
 await assert.rejects(()=>save([a,b]),/已变化/)
 const changed=await call('admin.batch.save',{batch:{...first,orchard:'更新果园',status:'ARCHIVED'}})
 await assert.rejects(()=>call('admin.order.ship',ship),/归档/)
 await call('admin.batch.save',{batch:{...changed,status:'ACTIVE'}})
 await call('admin.order.ship',ship)
 await assert.rejects(()=>call('admin.order.batches.save',{orderId:order.id,expectedVersion:1,items:[a,b]}),/待发货/)
 const shipped=(await call('admin.order.search',{})).items.find(o=>o.id===order.id)
 assert.equal(shipped.batches[0].batch.orchard,'验收果园')
 await call('admin.order.batchShip',{items:[ship]})
 assert.equal(shipped.batches.length,2)
 let page,destination;const memory=new Map(),wx={getStorageSync:k=>memory.get(k),setStorageSync:(k,v)=>memory.set(k,v),navigateTo:({url})=>destination=url}
 const mini=runtime({wx,getApp:()=>({globalData:{demoMode:true}}),Page:p=>page=p});const api=mini('miniprogram/utils/api.ts').callApi,products=mini('miniprogram/data/demo.ts').demoProducts
 const sample=products.find(p=>p.traceability?.code);assert.ok(sample)
 const trace=await api('trace.query',{code:sample.traceability.code});assert.equal(trace.demo,true);assert.equal(trace.number,sample.traceability.batch)
 await assert.rejects(()=>api('trace.query',{code:'unknown-code'}),/未找到/)
 mini('miniprogram/pages/trace/index.ts');page.setData=function(p){Object.assign(this.data,p)};page.data.code='unknown-code';await page.query();assert.equal(page.data.result,null);assert.match(page.data.error,/未找到/)
 page.data.code=sample.traceability.code;await page.query();assert.equal(page.data.result.code,sample.traceability.code);assert.equal(page.data.error,'')
 console.log('Order batches passed: split quantities, product ownership, duplicate and stale edits, ship gating, archive blocking, frozen history, shipping retries, trace lookup and query page errors.')
})().catch(e=>{console.error(e);process.exitCode=1})
