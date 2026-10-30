<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { api, dateTime } from '../api'
import { inventoryKinds } from '../inventory'
import type { InventoryAlert } from '../types'
import type { ManagedBatch } from '../product-management'
type Warehouse={id:string;name:string;active:boolean;version:number}
type Lot={skuId:string;warehouseId:string;batchId:string;quantity:number;version:number}
const emit=defineEmits<{saved:[]}>()
const warehouses=ref<Warehouse[]>([]),skus=ref<InventoryAlert[]>([]),batches=ref<ManagedBatch[]>([]),lots=ref<Lot[]>([]),skuId=ref(''),warehouseId=ref(''),batchId=ref(''),stock=ref(0),reserved=ref(0),unallocated=ref(0)
const busy=ref(false),error=ref(''),success=ref(''),warehouse=ref<Warehouse|null>(null),operation=ref('receive'),quantity=ref(1),reference=ref(''),reason=ref(''),history=ref<any[]>([]),total=ref(0),offset=ref(0),batchTotal=ref(0)
const selected=computed(()=>lots.value.find(l=>l.warehouseId===warehouseId.value&&l.batchId===batchId.value))
const batchName=(id:string)=>batches.value.find(b=>b.id===id)?.number||id
const warehouseName=(id:string)=>warehouses.value.find(w=>w.id===id)?.name||id
type ScanResult={batchId:string;batchNumber:string;productId:string;productName:string;skus:Array<{skuId:string;skuName:string}>}
const scanCode=ref(''),scanInfo=ref<ScanResult|null>(null),scanError=ref('')
async function scan(){const code=scanCode.value.trim();if(!code||busy.value)return;busy.value=true;scanError.value='';try{
 const r=await api<ScanResult>('admin.inventory.resolveScan',{code});scanInfo.value=r;scanCode.value=''
 if(r.skus.length){skuId.value=r.skus[0].skuId;await load();batchId.value=r.batchId}
 else{scanError.value='该批次商品暂无可用规格'}
}catch(e){scanError.value=(e as Error).message;scanInfo.value=null}finally{busy.value=false}}
let request=0
async function load(next=0){const id=++request;busy.value=true;error.value='';try{
 const [w,a]=await Promise.all([api<{items:Warehouse[]}>('admin.inventory.locations'),api<{items:InventoryAlert[]}>('admin.inventory.alerts')]);if(id!==request)return;warehouses.value=w.items;skus.value=a.items
 if(!skuId.value)skuId.value=a.items[0]?.skuId||''
 if(skuId.value){const productId=a.items.find(s=>s.skuId===skuId.value)?.productId;const [b,l,h]=await Promise.all([api<{items:ManagedBatch[];total:number}>('admin.batch.list',{productId,limit:100}),api<{items:Lot[];stock:number;reserved:number;unallocated:number}>('admin.inventory.lots',{skuId:skuId.value}),api<{items:any[];total:number}>('admin.inventory.lot.movements',{skuId:skuId.value,warehouseId:warehouseId.value,batchId:batchId.value,offset:next})]);if(id!==request)return;batches.value=b.items;batchTotal.value=b.total;lots.value=l.items;stock.value=l.stock;reserved.value=l.reserved;unallocated.value=l.unallocated;history.value=h.items;total.value=h.total;offset.value=next}
 }catch(e){if(id===request)error.value=(e as Error).message}finally{if(id===request)busy.value=false}}
onMounted(()=>load());watch(skuId,()=>{warehouseId.value='';batchId.value='';load()})
async function moreBatches(){busy.value=true;error.value='';try{const b=await api<{items:ManagedBatch[]}>('admin.batch.list',{productId:skus.value.find(s=>s.skuId===skuId.value)?.productId,offset:batches.value.length,limit:100});batches.value.push(...b.items)}catch(e){error.value=(e as Error).message}finally{busy.value=false}}
async function saveWarehouse(){if(!warehouse.value)return;busy.value=true;error.value='';try{await api('admin.inventory.warehouse.save',warehouse.value);warehouse.value=null;await load();success.value='仓库已保存'}catch(e){error.value=(e as Error).message}finally{busy.value=false}}
async function submit(){busy.value=true;error.value='';success.value='';try{await api(`admin.inventory.${operation.value}`,{skuId:skuId.value,warehouseId:warehouseId.value,batchId:batchId.value,quantity:quantity.value,version:selected.value?.version||0,expectedStock:stock.value,reference:reference.value,reason:reason.value});success.value='已登记，库存与流水已保存';reference.value='';reason.value='';emit('saved');await load()}catch(e){error.value=(e as Error).message}finally{busy.value=false}}
</script>
<template>
<section class="panel lot-panel">
 <div class="toolbar"><h3>批次库存与多仓库</h3><button class="button secondary" :disabled="busy" @click="load(offset)">刷新</button><button v-permission="'inventory.write'" class="button primary" :disabled="busy" @click="warehouse={id:'',name:'',active:true,version:0}">新增仓库</button></div>
 <form class="scan-form" @submit.prevent="scan"><label>扫码出入库<input v-model.trim="scanCode" placeholder="扫描批次二维码后回车" :disabled="busy" /></label><button class="button secondary" :disabled="busy||!scanCode.trim()">识别</button><span v-if="scanInfo" class="scan-info">已识别：{{scanInfo.productName}} / 批次 {{scanInfo.batchNumber}}，请选仓库并确认数量</span></form>
 <p v-if="scanError" class="form-error" role="alert">{{scanError}}</p>
 <p>采购入库增加可售库存；损耗扣减指定仓库批次，不能扣减预占库存。期初分配将已有库存登记到仓库批次，总库存不变。扫码枪即键盘输入，回车自动识别批次并填充商品与批次。</p>
 <p>分仓数量为可售与预占的实物合计。付款和盘点减少实物时先扣未分配数量，再按仓库、批次编号顺序扣减；退款回补到未分配库存。流水仅记录启用后的服务内操作，云数据库仍需复验。</p>
 <p v-if="error" class="form-error" role="alert">{{error}}</p><p v-if="success" role="status">{{success}}</p>
 <div class="row-actions"><span v-for="w in warehouses" :key="w.id">{{w.name}}（{{w.active?'启用':'停用'}}）<button v-permission="'inventory.write'" class="text-button" :disabled="busy" @click="warehouse={...w}">编辑</button></span></div>
 <label>商品 / 规格<select v-model="skuId" :disabled="busy"><option value="">请选择</option><option v-for="s in skus" :key="s.skuId" :value="s.skuId">{{s.productName}} / {{s.skuName}}</option></select></label>
 <p>可售 {{stock}} · 预占 {{reserved}} · 未分配 {{unallocated}}</p>
 <div class="table-panel"><table><thead><tr><th>仓库</th><th>批次</th><th>实物数量</th></tr></thead><tbody><tr v-for="l in lots" :key="l.warehouseId+l.batchId"><td>{{warehouseName(l.warehouseId)}}</td><td>{{batchName(l.batchId)}}</td><td>{{l.quantity}}</td></tr><tr v-if="!lots.length"><td colspan="3">尚无分仓批次库存</td></tr></tbody></table></div>
 <form v-permission="'inventory.write'" class="form-grid" @submit.prevent="submit">
 <label>操作<select v-model="operation" :disabled="busy"><option value="receive">采购入库</option><option value="loss">损耗出库</option><option value="allocate">期初分配</option></select></label>
 <label>仓库<select v-model="warehouseId" required :disabled="busy"><option value="">请选择</option><option v-for="w in warehouses.filter(w=>w.active)" :key="w.id" :value="w.id">{{w.name}}</option></select></label>
 <label>批次<select v-model="batchId" required :disabled="busy"><option value="">请选择</option><option v-for="b in batches.filter(b=>operation==='loss'||b.status==='ACTIVE')" :key="b.id" :value="b.id">{{b.number}}（{{b.status==='ACTIVE'?'使用中':'归档'}}）</option></select></label>
 <label>数量<input v-model.number="quantity" type="number" min="1" max="2147483647" step="1" required :disabled="busy"/></label>
 <label>采购单 / 损耗单 / 盘点单号<input v-model.trim="reference" maxlength="128" required :disabled="busy"/></label><label>原因<textarea v-model.trim="reason" maxlength="200" required :disabled="busy"/></label>
 <p class="full">所选仓库批次数量：{{selected?.quantity||0}}。如无批次，请先在「批次管理」创建该商品的批次。</p>
 <button v-if="batches.length<batchTotal" type="button" class="button secondary" :disabled="busy" @click="moreBatches">加载更多批次</button><button class="button primary" :disabled="busy||!skuId">{{busy?'处理中…':'确认登记'}}</button>
 </form>
 <h3>分仓批次流水</h3><div class="row-actions"><button class="button secondary" :disabled="busy" @click="load()">按所选仓库 / 批次查询</button><button class="button secondary" :disabled="busy" @click="warehouseId='';batchId='';load()">全部仓库 / 批次</button></div>
 <div class="table-panel"><table><thead><tr><th>时间 / 操作人</th><th>仓库 / 批次</th><th>操作 / 单号</th><th>数量</th><th>原因</th></tr></thead><tbody><tr v-for="m in history" :key="m.id"><td>{{dateTime(m.createdAt)}}<small class="block">{{m.username}}</small></td><td>{{warehouseName(m.warehouseId)}} / {{batchName(m.batchId)}}</td><td>{{inventoryKinds[m.kind]||m.kind}}<small class="block">{{m.reference}}</small></td><td>{{m.beforeQuantity}} → {{m.afterQuantity}}</td><td>{{m.reason}}</td></tr><tr v-if="!history.length"><td colspan="5">暂无流水</td></tr></tbody></table></div>
 <div class="row-actions"><span>共 {{total}} 条</span><button class="button secondary" :disabled="busy||offset===0" @click="load(offset-20)">上一页</button><button class="button secondary" :disabled="busy||offset+20>=total" @click="load(offset+20)">下一页</button></div>
</section>
<div v-if="warehouse" class="modal-backdrop"><form class="drawer" @submit.prevent="saveWarehouse"><div class="drawer-head"><h2>{{warehouse.id?'编辑仓库':'新增仓库'}}</h2><button type="button" class="close" :disabled="busy" @click="warehouse=null">×</button></div><div class="drawer-body form-grid"><label>仓库名称<input v-model.trim="warehouse.name" maxlength="80" required :disabled="busy"/></label><label>状态<select v-model="warehouse.active" :disabled="busy"><option :value="true">启用</option><option :value="false">停用</option></select></label><p>停用保留库存和历史，暂停该仓库的手工入出库；订单自动扣减仍可能涉及已有库存。</p><p v-if="error" class="form-error" role="alert">{{error}}</p></div><div class="drawer-foot"><button class="button primary" :disabled="busy">保存仓库</button></div></form></div>
</template>
<style scoped>
.lot-panel{padding:24px}
.scan-form{display:flex;align-items:end;gap:12px;flex-wrap:wrap;background:var(--purple-soft);border-radius:10px;padding:12px 16px}
.scan-form label{display:grid;gap:6px;font-size:12px}
.scan-info{font-size:12px;color:var(--purple)}.lot-panel p{line-height:1.7;margin:16px 0}.row-actions{flex-wrap:wrap;margin:16px 0}.table-panel{overflow:auto;margin:20px 0}select,input,textarea{padding:10px;border:1px solid var(--border);border-radius:8px}table{min-width:600px}h3{margin-top:20px}
</style>
