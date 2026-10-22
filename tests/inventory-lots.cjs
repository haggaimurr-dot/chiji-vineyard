const fs=require('node:fs'),path=require('node:path'),vm=require('node:vm'),assert=require('node:assert/strict'),ts=require('typescript')
const root=path.resolve(__dirname,'..'),values=new Map(),sessionStorage={getItem:k=>values.get(k)||null,setItem:(k,v)=>values.set(k,v)}
function runtime(){const cache=new Map();return function load(relative){const filename=path.resolve(root,relative);if(cache.has(filename))return cache.get(filename).exports;const module={exports:{}};cache.set(filename,module);const code=ts.transpileModule(fs.readFileSync(filename,'utf8'),{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS}}).outputText;vm.runInNewContext(code,{module,exports:module.exports,structuredClone,sessionStorage,Error,console,setTimeout,clearTimeout,require:name=>load(path.resolve(path.dirname(filename),`${name}.ts`))},{filename});return module.exports}}
;(async()=>{
 let call=runtime()('admin/src/mock.ts').mockCall
 const products=await call('admin.product.list'),p=products[0],sku=p.skus[0]
 const wh=await call('admin.inventory.warehouse.save',{id:'',name:'验收主仓',active:true,version:0}),wh2=await call('admin.inventory.warehouse.save',{id:'',name:'验收冷库',active:true,version:0})
 const batch=await call('admin.batch.save',{batch:{id:'',productId:p.id,number:'批次验收1',orchard:'验收果园',harvestedAt:'2026-10-01',packedAt:'2026-10-02',expiresAt:'2026-10-20',note:'',status:'ACTIVE',version:0}})
 const oldStock=sku.stock
 assert.equal((await call('admin.inventory.lots',{skuId:sku.id})).unallocated,oldStock+(sku.reserved||0))
 const input={skuId:sku.id,warehouseId:wh.id,batchId:batch.id,quantity:10,version:0,expectedStock:oldStock,reference:'期初1',reason:'仓库盘点'}
 await call('admin.inventory.allocate',input)
 let state=await call('admin.inventory.lots',{skuId:sku.id});assert.equal(state.stock,oldStock);assert.equal(state.items[0].quantity,10)
 await assert.rejects(()=>call('admin.inventory.allocate',{...input,version:1}))
 await assert.rejects(()=>call('admin.inventory.allocate',{...input,reference:'超额',version:1,quantity:oldStock+100}))
 await assert.rejects(()=>call('admin.inventory.receive',{...input,reference:'小数',quantity:1.5}))
 await call('admin.inventory.receive',{...input,reference:'采购1',quantity:5,version:1})
 state=await call('admin.inventory.lots',{skuId:sku.id});assert.equal(state.stock,oldStock+5);assert.equal(state.items[0].quantity,15)
 await assert.rejects(()=>call('admin.inventory.receive',{...input,reference:'采购1',quantity:5,version:2,expectedStock:oldStock+5}))
 await assert.rejects(()=>call('admin.inventory.loss',{...input,reference:'损耗超额',quantity:16,version:2,expectedStock:oldStock+5}))
 await call('admin.inventory.loss',{...input,reference:'损耗1',quantity:3,version:2,expectedStock:oldStock+5})
 state=await call('admin.inventory.lots',{skuId:sku.id});assert.equal(state.stock,oldStock+2);assert.equal(state.items[0].quantity,12)
 await call('admin.inventory.receive',{...input,warehouseId:wh2.id,reference:'采购冷库',quantity:7,version:0,expectedStock:oldStock+2})
 state=await call('admin.inventory.lots',{skuId:sku.id});assert.equal(state.items.length,2);assert.equal(state.stock,oldStock+9)
 assert.equal((await call('admin.inventory.movements',{kind:'PURCHASE'})).total,2);assert.equal((await call('admin.inventory.movements',{kind:'LOSS'})).items[0].reference,'损耗1')
 await call('admin.inventory.batch',{reason:'降低总库存一致性',items:[{skuId:sku.id,expectedStock:oldStock+9,stock:2}]})
 state=await call('admin.inventory.lots',{skuId:sku.id});assert.equal(state.items.reduce((n,l)=>n+l.quantity,0),2+(sku.reserved||0));assert.equal(state.unallocated,0)
 const whOff=await call('admin.inventory.warehouse.save',{...wh,active:false});assert.equal(whOff.version,2)
 await assert.rejects(()=>call('admin.inventory.warehouse.save',{...wh,name:'旧页面'}))
 await assert.rejects(()=>call('admin.inventory.receive',{...input,reference:'停用',version:3,expectedStock:2}))
 call=runtime()('admin/src/mock.ts').mockCall
 state=await call('admin.inventory.lots',{skuId:sku.id});assert.equal(state.stock,2);assert.equal((await call('admin.inventory.locations')).items.find(w=>w.id===wh.id).active,false)
 const history=await call('admin.inventory.lot.movements',{skuId:sku.id});assert.ok(history.total>=5);assert.equal((await call('admin.inventory.lot.movements',{skuId:sku.id,warehouseId:wh2.id,batchId:batch.id})).items.every(m=>m.warehouseId===wh2.id),true)
 console.log('Inventory lots checks passed: opening allocation, multi-warehouse purchase/loss, duplicate documents, stale versions, bounds, conservation after adjustment, warehouse disable, ledger filters, reload persistence.')
})().catch(e=>{console.error(e);process.exitCode=1})
