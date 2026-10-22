<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api, dateTime } from '../api'
import type { Order, OrderEvent, Tracking } from '../types'
import { can, currentAdmin } from '../permissions'
import { validateShipment } from '../orders'

const props = defineProps<{ order: Order }>()
const emit = defineEmits<{ close: []; changed: []; busy: [value: boolean] }>()
const order = ref(props.order), busy = ref(false), error = ref(''), events = ref<OrderEvent[]>([])
const plan = ref<number[][]>([]), forms = ref<Record<string, {carrier: string; carrierCode: string; trackingNo: string}>>({})
const tracking = ref<Record<string, Tracking>>({})
const editable = computed(() => order.value.orderStatus === 'CONFIRMED' && order.value.paymentStatus === 'PAID' && ['NONE','REJECTED','PARTIAL'].includes(order.value.refundStatus))
const canSplit = computed(() => editable.value && !order.value.shipment)
watch(() => props.order, value => {
  order.value = value; error.value = ''; tracking.value = {}
  plan.value = value.items.map(item => [item.quantity, 0])
  forms.value = Object.fromEntries((value.packages || []).map(p => [p.id, {carrier:'顺丰速运', carrierCode:'SF', trackingNo:''}]))
  void loadEvents()
}, { immediate: true })
async function loadEvents() {
  const id = order.value.id
  try { const result = await api<OrderEvent[]>('admin.order.events', {orderId:id}); if (id === order.value.id) events.value = result }
  catch(e) { if(id === order.value.id) error.value = (e as Error).message }
}
function addPackage() { if(plan.value[0]?.length < 10) plan.value.forEach(row => row.push(0)) }
function removePackage() { if(plan.value[0]?.length > 2) plan.value.forEach(row => row.pop()) }
async function mutate(action: string, data: Record<string,unknown>) {
  if(busy.value) return
  busy.value=true; emit('busy',true); error.value=''
  try { order.value=await api<Order>(action,{orderId:order.value.id,expectedVersion:order.value.packageVersion || 0,...data}); events.value=order.value.events || []; emit('changed') }
  catch(e) { error.value=(e as Error).message }
  finally { busy.value=false; emit('busy',false) }
}
async function split() {
  if(plan.value.some((row,i) => row.some(q => !Number.isSafeInteger(q) || q < 0) || row.reduce((a,b) => a+b,0) !== order.value.items[i].quantity)) {error.value='每行分配数量须为非负整数，合计须等于订单数量'; return}
  const packages=Array.from({length:plan.value[0].length},(_,column) => plan.value.map((row,itemIndex) => ({itemIndex,quantity:row[column]})).filter(x => x.quantity>0))
  if(packages.some(p => !p.length)) {error.value='每个包裹至少分配一件商品'; return}
  await mutate('admin.order.split',{packages})
}
async function ship(id: string) {
  try {const input=validateShipment(forms.value[id]); await mutate('admin.order.package.ship',{packageId:id,...input})}
  catch(e) {error.value=(e as Error).message}
}
async function track(id: string) {
  if(busy.value) return
  busy.value=true; emit('busy',true); error.value=''
  try {tracking.value[id]=await api<Tracking>('admin.order.tracking',{orderId:order.value.id,packageId:id})}
  catch(e) {error.value=(e as Error).message}
  finally {busy.value=false; emit('busy',false)}
}
</script>

<template>
  <section class="panel packages-panel" aria-label="拆单、包裹与订单事件">
    <div class="section-heading"><div><h2>拆单 / 多包裹 · {{ order.orderNo }}</h2><p>按商品数量分配履约子单，付款、退款及发票按原订单处理。全部包裹发出后可整单确认收货。</p></div><button class="button secondary" :disabled="busy" @click="emit('close')">关闭</button></div>
    <p v-if="error" role="alert" class="error">{{ error }}</p>
    <div v-if="canSplit && can(currentAdmin(),'order.ship')">
      <div class="table-scroll"><table><thead><tr><th>商品 / 订单数量</th><th v-for="(_,i) in plan[0]" :key="i">包裹 {{ i+1 }}</th></tr></thead><tbody><tr v-for="(item,i) in order.items" :key="i"><td>{{ item.name }} · {{ item.skuName }} ×{{ item.quantity }}</td><td v-for="(_,j) in plan[i]" :key="j"><input v-model.number="plan[i][j]" type="number" min="0" :max="item.quantity" step="1" :disabled="busy" :aria-label="item.name+' 包裹 '+(j+1)+' 数量'" /></td></tr></tbody></table></div>
      <div class="order-actions"><button class="button secondary" :disabled="busy || plan[0]?.length >= 10" @click="addPackage">增加包裹</button><button class="button secondary" :disabled="busy || plan[0]?.length <= 2" @click="removePackage">减少包裹</button><button class="button primary" :disabled="busy" @click="split">{{ order.packages?.length ? '重新分配包裹' : '确认拆单' }}</button></div>
    </div>
    <p v-if="!order.packages?.length">{{ order.shipment ? '此订单为整单发货，已发货后不能拆单。' : '尚未拆单，可按商品数量分配多个包裹。' }}</p>
    <div class="package-grid"><article v-for="p in order.packages" :key="p.id" class="package-card"><h3>{{ p.number }}</h3><p v-for="item in p.items" :key="item.itemIndex">{{ order.items[item.itemIndex]?.name }} · {{ order.items[item.itemIndex]?.skuName }} ×{{ item.quantity }}</p><template v-if="p.shipment"><p>{{ p.receivedAt ? '已收货' : '已发货' }} · {{ p.shipment.carrier }} {{ p.shipment.trackingNo }}</p><small>{{ dateTime(p.shipment.shippedAt || '') }} · 微信{{ p.shipment.syncStatus === 'SUCCESS' ? '已同步' : p.shipment.syncStatus === 'FAILED' ? '同步失败' : '待同步' }}</small><p v-if="p.shipment.syncError" class="error">{{ p.shipment.syncError }}</p><button class="button secondary" :disabled="busy" @click="track(p.id)">查询此包裹物流</button><div v-if="tracking[p.id]"><p>{{ tracking[p.id].message }}</p><p v-for="(event,i) in tracking[p.id].events" :key="i">{{ event.time }} · {{ event.context }}</p></div></template><template v-else><p>待发货</p><div v-if="editable && can(currentAdmin(),'order.ship') && forms[p.id]" class="package-form"><label>快递公司<input v-model="forms[p.id].carrier" :disabled="busy" maxlength="40" /></label><label>快递编码<input v-model="forms[p.id].carrierCode" :disabled="busy" maxlength="20" /></label><label>运单号<input v-model="forms[p.id].trackingNo" :disabled="busy" maxlength="50" /></label><button class="button primary" :disabled="busy" @click="ship(p.id)">发出此包裹</button></div></template></article></div>
    <button v-if="order.packages?.length && order.shipment && editable && can(currentAdmin(),'order.ship')" class="button secondary" :disabled="busy" @click="mutate('admin.order.packages.sync',{})">同步 / 重试全部已发包裹至微信</button>
    <h3>订单事件记录</h3><p v-if="!events.length">暂无事件记录（历史订单不会补造事件）</p><ol class="event-list"><li v-for="event in events" :key="event.id"><strong>{{ event.detail }}</strong><small>{{ dateTime(event.createdAt) }} · {{ event.actor }} · {{ event.kind }}</small></li></ol>
  </section>
</template>

<style scoped>
.packages-panel { margin: 16px 0; padding: 24px; }
.packages-panel input { width: 100%; min-width: 75px; }
.package-grid { display: grid; grid-template-columns: repeat(auto-fit,minmax(260px,1fr)); gap: 16px; margin: 20px 0; }
.package-card { border: 1px solid #deddd5; border-radius: 12px; padding: 16px; overflow-wrap: anywhere; }
.package-form { display: grid; gap: 10px; }
.package-form label, .event-list small { display: block; }
.event-list { padding-left: 20px; } .event-list li { margin: 14px 0; }
.error { color: #a32f39; }
</style>
