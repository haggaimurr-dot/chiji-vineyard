const fs=require('node:fs'),path=require('node:path'),vm=require('node:vm'),assert=require('node:assert/strict'),ts=require('typescript')
const root=path.resolve(__dirname,'..'),values=new Map(),sessionStorage={getItem:k=>values.get(k)||null,setItem:(k,v)=>values.set(k,v)}
function runtime(){const cache=new Map();return function load(relative){const filename=path.resolve(root,relative);if(cache.has(filename))return cache.get(filename).exports;const module={exports:{}};cache.set(filename,module);const code=ts.transpileModule(fs.readFileSync(filename,'utf8'),{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS}}).outputText;vm.runInNewContext(code,{module,exports:module.exports,structuredClone,console,sessionStorage,Error,setTimeout,clearTimeout,require:name=>load(path.resolve(path.dirname(filename),`${name}.ts`))},{filename});return module.exports}}
;(async()=>{
 let call=runtime()('admin/src/mock.ts').mockCall
 const products=await call('admin.product.list'),sku=products[0].skus[0],second=products[1].skus[0]
 const change=(stock,expectedStock)=>call('admin.inventory.batch',{reason:'盘点测试',items:[{skuId:sku.id,stock,expectedStock}]})
 assert.equal((await call('admin.inventory.movements')).total,0)
 await call('admin.inventory.alert.save',{skuId:sku.id,threshold:10,enabled:true});await change(10,sku.stock)
 assert.equal((await call('admin.inventory.alerts')).items.find(a=>a.skuId===sku.id).alert,true)
 const history=await call('admin.inventory.movements',{skuId:sku.id,kind:'ADJUST',limit:1})
 assert.equal(history.items[0].beforeStock,sku.stock);assert.equal(history.items[0].afterStock,10);assert.equal(history.items[0].beforeReserved,history.items[0].afterReserved)
 await change(10,10);assert.equal((await call('admin.inventory.movements')).total,1)
 await assert.rejects(()=>call('admin.inventory.batch',{reason:'整批回滚',items:[{skuId:sku.id,stock:1,expectedStock:10},{skuId:second.id,stock:0,expectedStock:second.stock-1}]}))
 assert.equal((await call('admin.product.list'))[0].skus[0].stock,10);assert.equal((await call('admin.inventory.movements')).total,1)
 await assert.rejects(()=>change(1.5,10));await assert.rejects(()=>change(-1,10))
 await change(11,10);assert.equal((await call('admin.inventory.alerts')).items.find(a=>a.skuId===sku.id).alert,false)
 await change(0,11);await call('admin.inventory.alert.save',{skuId:sku.id,threshold:0,enabled:false});assert.equal((await call('admin.inventory.alerts')).items.find(a=>a.skuId===sku.id).alert,false)
 await call('admin.inventory.alert.save',{skuId:sku.id,threshold:0,enabled:true});assert.equal((await call('admin.inventory.alerts')).items.find(a=>a.skuId===sku.id).alert,true)
 await assert.rejects(()=>call('admin.inventory.alert.save',{skuId:sku.id,threshold:-1,enabled:true}))
 await assert.rejects(()=>call('admin.inventory.movements',{startDate:'2026-02-30'}));await assert.rejects(()=>call('admin.inventory.movements',{startDate:'2026-10-05',endDate:'2026-10-04'}))
 const today=new Intl.DateTimeFormat('en-CA',{timeZone:'Asia/Shanghai',year:'numeric',month:'2-digit',day:'2-digit'}).format(new Date())
 assert.equal((await call('admin.inventory.movements',{startDate:today,endDate:today})).total,3)
 assert.equal((await call('admin.inventory.movements',{limit:2,offset:2})).items.length,1)
 call=runtime()('admin/src/mock.ts').mockCall
 assert.equal((await call('admin.inventory.movements')).total,3);assert.equal((await call('admin.inventory.alerts')).items.find(a=>a.skuId===sku.id).threshold,0)
 const edited=(await call('admin.product.list'))[0];edited.status='DRAFT';await call('admin.product.save',{product:edited});assert.equal((await call('admin.inventory.alerts')).items.find(a=>a.skuId===sku.id).alert,false)
 edited.skus[0].expectedStock=edited.skus[0].stock;edited.skus[0].stock=8;await call('admin.product.save',{product:edited});assert.equal((await call('admin.inventory.movements',{kind:'PRODUCT_SAVE'})).total,1)
 console.log('Inventory checks passed: ledger, no-op, atomic batch, threshold boundaries, recovery, disable, draft, dates, pagination, reload, product save.')
})().catch(e=>{console.error(e);process.exitCode=1})
