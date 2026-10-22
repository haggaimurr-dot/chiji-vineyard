import { externalTraceError } from './product-management'
import type { ManagedBatch } from './product-management'
import { productBatchError } from './product-content'
import type { InventoryAlert, InventoryMovement, InventoryPage, Product } from './types'

export const inventoryKinds: Record<string, string> = { PRODUCT_SAVE: '商品保存', ADJUST: '库存调整', RESERVE: '下单预占', PAY: '付款确认', RELEASE: '取消 / 超时释放', RETURN_RECEIPT: '退货验收入库', REFUND: '退款完成', PURCHASE: '采购入库', LOSS: '损耗出库', ALLOCATE: '期初分配' }
const validCount = (value: unknown): value is number => Number.isInteger(value) && Number(value) >= 0 && Number(value) <= 2147483647

export function createDemoInventory(initial: Product[]) {
  let batches: ManagedBatch[] = []
  let warehouses: Array<{id:string;name:string;active:boolean;version:number}> = []
  let lots: Array<{skuId:string;warehouseId:string;batchId:string;quantity:number;version:number}> = []
  let lotMovements: any[] = []
  const lotRecord = (l: typeof lots[number], before:number, kind:string, reference:string, username:string, reason:string) => lotMovements.unshift({...l,id:`lot-${Date.now()}-${lotMovements.length}`,beforeQuantity:before,afterQuantity:l.quantity,kind,reference,username,reason,createdAt:new Date().toISOString()})

  let products = initial, movements: InventoryMovement[] = [], settings: Record<string, { threshold: number; enabled: boolean }> = {}
  const key = 'chiji_admin_demo_inventory_v1'
  try { const saved = typeof sessionStorage !== 'undefined' && sessionStorage.getItem(key); if (saved) { const data = JSON.parse(saved); products = data.products; movements = data.movements; settings = data.settings; batches = data.batches || []; warehouses=data.warehouses||[]; lots=data.lots||[];lotMovements=data.lotMovements||[] } } catch { /* Use initial data if storage is unavailable. */ }
  const persist = () => { try { if (typeof sessionStorage !== 'undefined') sessionStorage.setItem(key, JSON.stringify({ products, movements, settings, batches, warehouses, lots, lotMovements })) } catch { /* Current session remains usable. */ } }
  function record(product: Product, sku: Product['skus'][number], before: Product['skus'][number] | undefined, kind: string, username: string, reason: string) {
    if (before && before.stock === sku.stock && (before.reserved || 0) === (sku.reserved || 0) && (before.sold || 0) === (sku.sold || 0)) return
    if(kind!=='LOSS' && before && sku.stock+(sku.reserved||0)<before.stock+(before.reserved||0)) {
      const allocated=lots.filter(l=>l.skuId===sku.id).sort((a,b)=>a.warehouseId.localeCompare(b.warehouseId)||a.batchId.localeCompare(b.batchId))
      let excess=allocated.reduce((n,l)=>n+l.quantity,0)-sku.stock-(sku.reserved||0)
      for(const l of allocated){if(excess<=0)break;const delta=Math.min(excess,l.quantity);if(!delta)continue;const old=l.quantity;l.quantity-=delta;l.version++;excess-=delta;lotRecord(l,old,kind,kind==='PRODUCT_SAVE'?product.id:'',username,reason)}
    }
    movements.unshift({ id: `inv-${Date.now()}-${movements.length}`, productId: product.id, productName: product.name, skuId: sku.id, skuName: sku.name, kind, reference: kind === 'PRODUCT_SAVE' ? product.id : '', username, reason, createdAt: new Date().toISOString(), beforeStock: before?.stock || 0, afterStock: sku.stock, beforeReserved: before?.reserved || 0, afterReserved: sku.reserved || 0, beforeSold: before?.sold || 0, afterSold: sku.sold || 0 })
  }
  return {
    returnReceipt(items:Array<{skuId:string;restock:number}>, reference:string, username:string, reason:string) {
      const changes=items.filter(i=>i.restock>0).map(i=>{const product=products.find(p=>p.skus.some(s=>s.id===i.skuId));return {i,product,sku:product?.skus.find(s=>s.id===i.skuId)}})
      if(changes.some(({i,sku})=>!sku || !validCount(i.restock) || !validCount(sku.stock+i.restock))) throw new Error('库存超出上限或规格不存在')
      for(const {i,product,sku} of changes){const before={...sku!};sku!.stock+=i.restock;record(product!,sku!,before,'RETURN_RECEIPT',username,reason);movements[0].reference=reference}
      persist()
    },
    products: () => products,
    locations: () => ({items:structuredClone(warehouses)}),
    saveWarehouse(input:any) {
      const w=structuredClone(input);w.name=w.name?.trim();if(!w.name||w.name.length>80||typeof w.active!=='boolean'||warehouses.some(x=>x.id!==w.id&&x.name===w.name))throw new Error('仓库名称或状态无效 / 名称重复')
      const old=warehouses.find(x=>x.id===w.id);if(w.id&&(!old||old.version!==w.version))throw new Error('仓库已变化，请刷新重试')
      w.id=w.id||`wh-${Date.now()}-${warehouses.length}`;w.version=(old?.version||0)+1;warehouses=[w,...warehouses.filter(x=>x.id!==w.id)];persist();return w
    },
    lots(data:any) {
      const sku=products.flatMap(p=>p.skus).find(s=>s.id===data.skuId);if(!sku)throw new Error('请选择规格')
      const items=lots.filter(l=>l.skuId===sku.id);return {items:structuredClone(items),stock:sku.stock,reserved:sku.reserved||0,unallocated:sku.stock+(sku.reserved||0)-items.reduce((n,l)=>n+l.quantity,0)}
    },
    lotHistory(data:any) {
      const offset=data.offset??0;if(!Number.isInteger(offset)||offset<0)throw new Error('分页参数无效')
      const items=lotMovements.filter(m=>(!data.skuId||m.skuId===data.skuId)&&(!data.warehouseId||m.warehouseId===data.warehouseId)&&(!data.batchId||m.batchId===data.batchId));return {items:structuredClone(items.slice(offset,offset+20)),total:items.length,offset,limit:20}
    },
    changeLot(action:string,d:any,username:string) {
      const product=products.find(p=>p.skus.some(s=>s.id===d.skuId)),sku=product?.skus.find(s=>s.id===d.skuId&&s.active!==false),wh=warehouses.find(w=>w.id===d.warehouseId),batch=batches.find(b=>b.id===d.batchId)
      const kind=action==='admin.inventory.receive'?'PURCHASE':action==='admin.inventory.loss'?'LOSS':'ALLOCATE'
      if(!sku||!product||!wh?.active||!batch||batch.productId!==product.id||(batch.status!=='ACTIVE'&&kind!=='LOSS')||!validCount(d.quantity)||!d.quantity||!validCount(d.version)||!d.reference?.trim()||d.reference.trim().length>128||!d.reason?.trim()||d.reason.trim().length>200)throw new Error('规格、仓库、批次或数量 / 单号 / 原因无效')
      if(lotMovements.some(m=>m.kind===kind&&m.reference===d.reference.trim()&&m.skuId===d.skuId&&m.warehouseId===d.warehouseId&&m.batchId===d.batchId))throw new Error('该单号已登记，请核对流水')
      const existing=lots.find(l=>l.skuId===sku.id&&l.warehouseId===wh.id&&l.batchId===batch.id)
      if(sku.stock!==d.expectedStock||(existing?.version||0)!==d.version)throw new Error('库存已变化，请刷新重试')
      const quantity=existing?.quantity||0,delta=kind==='LOSS'?-d.quantity:d.quantity
      if(kind==='LOSS'&&(quantity<d.quantity||sku.stock<d.quantity))throw new Error('可售库存或批次库存不足')
      if(quantity+delta>2147483647||(kind==='PURCHASE'&&sku.stock+delta>2147483647))throw new Error('库存超出上限')
      if(kind==='ALLOCATE'&&this.lots(d).unallocated<d.quantity)throw new Error('未分配库存不足')
      const l=existing||{skuId:sku.id,warehouseId:wh.id,batchId:batch.id,quantity:0,version:0};if(!existing)lots.push(l);l.quantity+=delta;l.version++
      if(kind!=='ALLOCATE'){const before={...sku};sku.stock+=delta;record(product,sku,before,kind,username,d.reason.trim());movements[0].reference=d.reference.trim()}
      lotRecord(l,quantity,kind,d.reference.trim(),username,d.reason.trim());persist();return structuredClone(l)
    },
    importProducts(incoming: Product[], username:string) {
      const next=incoming.map((p,index)=>({...structuredClone(p),id:`import-${Date.now()}-${index}`}))
      next.forEach(p=>p.skus.forEach((s,i)=>s.id=p.id+'-sku-'+i))
      if(next.some(p=>!p.name||!p.cover||!p.skus.length||p.skus.some(s=>!validCount(s.stock)||!Number.isInteger(s.price)||s.price<1)))throw new Error('导入数据无效')
      next.forEach(p=>p.skus.forEach(s=>record(p,s,undefined,'PRODUCT_SAVE',username,'批量导入草稿')))
      products=[...next,...products];persist()
    },
    listBatches(data:any){const items=batches.filter(b=>(!data.productId||b.productId===data.productId)&&(!data.status||b.status===data.status)&&(!data.keyword||[b.number,b.orchard,b.note].join(' ').toLowerCase().includes(data.keyword.toLowerCase())));const limit=data.limit||20,offset=data.offset||0;return {items:items.slice(offset,offset+limit),total:items.length,limit,offset}},
    saveBatch(input:ManagedBatch){const b=structuredClone(input);const old=batches.find(x=>x.id===b.id)
      if(b.externalTrace){b.externalTrace.platform=b.externalTrace.platform.trim();b.externalTrace.url=b.externalTrace.url.trim();b.externalTrace.code=b.externalTrace.code.trim()}
      const traceError=externalTraceError(b.externalTrace);if(traceError)throw new Error(traceError)
      if(!products.some(p=>p.id===b.productId)||!b.number.trim()||b.number.length>120||!b.orchard.trim()||b.orchard.length>200||b.note.length>1000||!['ACTIVE','ARCHIVED'].includes(b.status))throw new Error('批次资料无效')
      const invalid=productBatchError({orchard:b.orchard,batch:b.number,harvestedAt:b.harvestedAt,packedAt:b.packedAt,packing:'',steps:[]});if(invalid)throw new Error(invalid)
      if(b.expiresAt&&(!/^\d{4}-\d{2}-\d{2}$/.test(b.expiresAt)||!Number.isFinite(Date.parse(b.expiresAt))||new Date(b.expiresAt).toISOString().slice(0,10)!==b.expiresAt||b.expiresAt<b.harvestedAt||b.expiresAt<b.packedAt))throw new Error('有效期日期无效')
      b.number=b.number.trim();if(batches.some(x=>x.id!==b.id&&x.productId===b.productId&&x.number===b.number))throw new Error('批次号重复')
      if(b.id&&(!old||old.version!==b.version||old.productId!==b.productId))throw new Error('批次已变化，请刷新；商品归属不可修改')
      b.id=b.id||`batch-${Date.now()}-${batches.length}`;b.code=b.id;b.version=(old?.version||0)+1;batches=[b,...batches.filter(x=>x.id!==b.id)];persist();return b
    },
    saveProduct(product: Product, username: string) {
      if (product.skus.some(s => !validCount(s.stock))) throw new Error('库存须为非负整数')
      const old = products.find(p => p.id === product.id)
      if(new Set(product.skus.map(s=>s.id)).size!==product.skus.length)throw new Error('规格编号重复')
      for(const sku of product.skus){const owner=products.find(p=>p.skus.some(s=>s.id===sku.id));if(owner&&owner.id!==product.id)throw new Error('规格属于其他商品');const before=old?.skus.find(s=>s.id===sku.id);if(before){if(sku.expectedStock!==undefined&&sku.expectedStock!==before.stock)throw new Error('库存已变化，请刷新后重新编辑；商品未保存');if(sku.expectedStock===undefined)sku.stock=before.stock}}

      for (const sku of product.skus) { const before = old?.skus.find(s => s.id === sku.id); if (before) { sku.reserved = before.reserved; sku.sold = before.sold }; record(product, sku, before, 'PRODUCT_SAVE', username, '商品保存') }
      products = [structuredClone(product), ...products.filter(p => p.id !== product.id)]; persist()
    },
    adjust(data: any, username: string) {
      if (!Array.isArray(data.items) || !data.items.length || data.items.length > 200 || !data.reason?.trim() || data.reason.trim().length > 200 || new Set(data.items.map((i: any) => i.skuId)).size !== data.items.length) throw new Error('请选择规格并填写 1–200 字调整原因')
      const changes = data.items.map((i: any) => { const product = products.find(p => p.skus.some(s => s.id === i.skuId)); return { i, product, sku: product?.skus.find(s => s.id === i.skuId && s.active !== false) } })
      if (changes.some(({ i, sku }: any) => !sku || !validCount(i.stock) || sku.stock !== i.expectedStock)) throw new Error('库存已变化或参数无效，请刷新重试；本批次未保存')
      for (const { i, product, sku } of changes) { const before = { ...sku }; sku.stock = i.stock; record(product, sku, before, 'ADJUST', username, data.reason.trim()) }
      persist(); return { updated: changes.length }
    },
    alerts(): { items: InventoryAlert[] } {
      return { items: products.flatMap(p => p.skus.filter(s => s.active !== false).map(s => { const setting = settings[s.id] || { threshold: 10, enabled: true }; return { skuId: s.id, productId: p.id, productName: p.name, skuName: s.name, status: p.status, stock: s.stock, reserved: s.reserved || 0, ...setting, alert: setting.enabled && p.status === 'PUBLISHED' && s.stock <= setting.threshold } })).sort((a,b) => a.stock-b.stock || a.skuId.localeCompare(b.skuId)) }
    },
    saveAlert(data: any) {
      if (!products.some(p => p.skus.some(s => s.id === data.skuId && s.active !== false)) || !validCount(data.threshold) || typeof data.enabled !== 'boolean') throw new Error('规格或告警参数无效')
      settings[data.skuId] = { threshold: data.threshold, enabled: data.enabled }; persist(); return { saved: true }
    },
    list(data: any): InventoryPage {
      const date = (value: string) => { const result = new Date(`${value}T00:00:00+08:00`); if (!/^\d{4}-\d{2}-\d{2}$/.test(value) || !Number.isFinite(result.getTime()) || new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit' }).format(result) !== value) throw new Error('日期无效'); return result.getTime() }
      const start = data.startDate ? date(data.startDate) : -Infinity, end = data.endDate ? date(data.endDate) + 86400000 : Infinity
      if (start >= end) throw new Error('结束日期不能早于开始日期')
      const limit = Number.isInteger(data.limit) && data.limit > 0 && data.limit <= 100 ? data.limit : 20, offset = data.offset ?? 0
      if (!Number.isInteger(offset) || offset < 0) throw new Error('分页参数无效')
      const items = movements.filter(m => (!data.skuId || m.skuId === data.skuId) && (!data.kind || m.kind === data.kind) && (!data.reference || m.reference === data.reference.trim()) && Date.parse(m.createdAt) >= start && Date.parse(m.createdAt) < end)
      return { items: items.slice(offset, offset + limit), total: items.length, limit, offset }
    },
  }
}
