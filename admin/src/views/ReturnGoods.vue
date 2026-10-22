<script setup lang="ts">
import { ref, watch } from 'vue'
import { api, dateTime } from '../api'
import { can, currentAdmin } from '../permissions'
import { returnStatuses, type AfterSalesTicket, type ReturnItem } from '../after-sales'
import type { Order } from '../types'
const props=defineProps<{ticket:AfterSalesTicket; disabled?:boolean}>()
const emit=defineEmits<{updated:[ticket:AfterSalesTicket]; editing:[value:boolean]}>()
const busy=ref(false), editing=ref(false), error=ref(''), inspection=ref('')
const address=ref({name:'',phone:'',province:'',city:'',district:'',detail:''})
const items=ref<ReturnItem[]>([]), tracking=ref<{message:string; events:Array<{time:string;context:string}>}|null>(null)
watch(()=>props.ticket.id,()=>{editing.value=false; emit('editing',false); tracking.value=null; error.value=''; items.value=[]; inspection.value=''}, {immediate:true})
async function begin() {
 if(busy.value || props.disabled) return
 busy.value=true; error.value=''
 try {
  if(props.ticket.return) items.value=props.ticket.return.items.map(i=>({...i,restock:0}))
  else {
   const result=await api<{items:Order[]}>('admin.order.search',{keyword:props.ticket.orderNo,limit:20})
   const order=result.items.find(o=>o.id===props.ticket.orderId)
   if(!order) throw new Error('未找到关联订单，请刷新后重试')
   if(order.items.some(i=>!i.skuId)) throw new Error('订单规格信息不完整，无法办理退货')
   items.value=order.items.map(i=>({skuId:i.skuId!,name:i.name+' / '+i.skuName,quantity:i.quantity,restock:0}))
  }
  editing.value=true; emit('editing',true)
 } catch(e){error.value=(e as Error).message} finally {busy.value=false}
}
function cancel(){editing.value=false;emit('editing',false)}
async function save() {
 if(busy.value || props.disabled) return
 busy.value=true;error.value=''
 try {
  const receiving=!!props.ticket.return
  const result=await api<AfterSalesTicket>('admin.ticket.update',{id:props.ticket.id,version:props.ticket.version,return:{action:receiving?'RECEIVE':'AUTHORIZE',address:address.value,items:items.value.filter(i=>receiving || i.quantity>0),inspection:inspection.value}})
  cancel(); emit('updated',result)
 } catch(e){error.value=(e as Error).message} finally {busy.value=false}
}
async function trace() {
 if(busy.value || props.disabled) return
 busy.value=true; error.value=''
 try {tracking.value=await api('admin.ticket.returnTracking',{id:props.ticket.id})} catch(e){error.value=(e as Error).message} finally{busy.value=false}
}
</script>
<template>
 <section class="return-section"><h4>退货物流与验收入库</h4><p v-if="error" role="alert" class="alert error">{{error}}</p>
  <template v-if="ticket.return"><p>退货进度：{{returnStatuses[ticket.return.status]}}</p><p>退货地址：{{ticket.return.address.name}} · {{ticket.return.address.phone}}<br/>{{ticket.return.address.province}}{{ticket.return.address.city}}{{ticket.return.address.district}} {{ticket.return.address.detail}}</p>
   <ul><li v-for="item in ticket.return.items" :key="item.skuId">{{item.name || item.skuId}} · 退回 {{item.quantity}} 件<span v-if="ticket.return.status==='RECEIVED'"> · 可售入库 {{item.restock}} 件 · 隔离 {{item.quantity-item.restock}} 件</span></li></ul>
   <p v-if="ticket.return.shipment">寄件：{{ticket.return.shipment.carrier}} · {{ticket.return.shipment.trackingNo}} · {{dateTime(ticket.return.shippedAt || '')}} <button class="button small secondary" :disabled="busy || disabled" @click="trace">查询退货物流</button></p>
   <p v-if="ticket.return.receivedAt">验收：{{dateTime(ticket.return.receivedAt)}} · {{ticket.return.receivedBy}}<br/>{{ticket.return.inspection}}</p>
  </template><p v-else>尚未安排实物退货。确认地址和明细后，用户可登记寄件。</p>
  <div v-if="tracking" role="status"><p>{{tracking.message}}</p><p v-for="(event,index) in tracking.events" :key="index">{{event.time}} · {{event.context}}</p></div>
  <button v-if="!editing && ticket.status!=='CLOSED' && can(currentAdmin(),'refund.approve') && (!ticket.return || ticket.return.status==='SHIPPED')" class="button small primary" :disabled="busy || disabled" @click="begin">{{ticket.return ? '登记验收入库' : '确认退货地址与明细'}}</button>
  <form v-if="editing" class="return-form" @submit.prevent="save">
   <template v-if="!ticket.return"><label>收件人<input v-model="address.name" required maxlength="30" :disabled="busy || disabled"/></label><label>联系电话<input v-model="address.phone" required maxlength="11" pattern="1[3-9][0-9]{9}" :disabled="busy || disabled"/></label><label>省<input v-model="address.province" required maxlength="30" :disabled="busy || disabled"/></label><label>市<input v-model="address.city" required maxlength="30" :disabled="busy || disabled"/></label><label>区 / 县<input v-model="address.district" maxlength="30" :disabled="busy || disabled"/></label><label>详细地址<input v-model="address.detail" required maxlength="120" :disabled="busy || disabled"/></label><p>无需退回的规格请填0；退货明细确认后不可覆盖。</p></template>
   <label v-for="item in items" :key="item.skuId">{{item.name || item.skuId}} · {{ticket.return ? '可售入库数量（退回 '+item.quantity+' 件）' : '退货数量'}}<input v-if="!ticket.return" v-model.number="item.quantity" type="number" min="0" step="1" required :disabled="busy || disabled"/><input v-else v-model.number="item.restock" type="number" min="0" :max="item.quantity" step="1" required :disabled="busy || disabled"/></label>
   <template v-if="ticket.return"><p>可售数量默认0。损坏或不适合销售的鲜果隔离处理，不增加可售库存。入库后可在库存管理中分配仓库与批次。</p><label>验收结果<textarea v-model="inspection" required maxlength="1000" :disabled="busy || disabled"/></label></template>
   <div class="row-actions"><button class="button small primary" :disabled="busy || disabled">{{busy ? '正在保存…' : ticket.return ? '确认验收入库' : '确认退货'}}</button><button type="button" class="button small secondary" :disabled="busy" @click="cancel">取消</button></div>
  </form>
 </section>
</template>
<style scoped>
.return-section{margin:16px 0;padding:16px;border:1px solid var(--border);border-radius:10px;overflow-wrap:anywhere}.return-form{display:grid;gap:12px}.return-form label{display:grid;gap:6px}input,textarea{padding:10px;border:1px solid var(--border);border-radius:8px;background:var(--paper);max-width:100%;box-sizing:border-box}textarea{min-height:90px}
</style>
