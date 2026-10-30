<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api, dateTime } from '../api'
import { confirmAction } from '../confirm'
import type { LogisticsTask, Order, Parcel } from '../types'
const props = defineProps<{ orders: Order[] }>()
const emit = defineEmits<{ changed: [] }>()
const tasks = ref<LogisticsTask[]>([]), busy = ref(false), error = ref(''), message = ref('')
const status = ref(''), orderId = ref(''), parcelKey = ref('')
const order = computed(() => props.orders.find(o => o.id === orderId.value))
const parcels = computed(() => order.value?.shipment ? [order.value.shipment, ...(order.value.shipment.parcels || [])] : [])
const parcel = computed(() => parcels.value.find(p => `${p.carrierCode}:${p.trackingNo}` === parcelKey.value) || parcels.value[0])
const form = ref({ carrier: '顺丰速运', carrierCode: 'SF', trackingNo: '' })
const kind = ref('EXCEPTION'), owner = ref(''), note = ref(''), editing = ref<LogisticsTask|null>(null), taskStatus = ref('OPEN')
const kinds: Record<string,string> = { DELAY:'长时间未更新', EXCEPTION:'运输异常', RETURN:'退签 / 退回', SYNC:'微信同步失败', NOTICE:'发货通知异常' }
const statuses: Record<string,string> = { OPEN:'待处理', PROCESSING:'处理中', RESOLVED:'已关闭' }
async function load() {
  busy.value = true; error.value = ''
  try { tasks.value = await api<LogisticsTask[]>('admin.logistics.tasks', { status: status.value }) }
  catch(e) { error.value = (e as Error).message }
  finally { busy.value = false }
}
onMounted(load)
function editTask(task: LogisticsTask) { editing.value = structuredClone(task); owner.value = task.owner; note.value = ''; taskStatus.value = task.status; kind.value = task.kind }
function reset() { editing.value = null; note.value = ''; owner.value = ''; taskStatus.value = 'OPEN' }
async function saveTask() {
  if (busy.value) return
  busy.value = true; error.value = ''; message.value = ''
  try {
    if (!editing.value && !parcel.value) throw new Error('请选择已发货订单及运单')
    await api('admin.logistics.task.save', { ...(editing.value || { orderId:orderId.value, carrierCode:parcel.value?.carrierCode, trackingNo:parcel.value?.trackingNo, kind:kind.value }), status:taskStatus.value, owner:owner.value, note:note.value })
    reset(); message.value = '异常任务已保存'; await load()
  } catch(e) { error.value = (e as Error).message }
  finally { busy.value = false }
}
async function addParcel() {
  if (!order.value || busy.value) return
  if (!(await confirmAction({ title: '确认交寄', message: '确认此包裹已交寄？保存后将把全部运单同步到微信。', confirmText: '确认已交寄' }))) return
  busy.value = true; error.value = ''; message.value = ''
  try {
    const result = await api<{shipment: NonNullable<Order['shipment']>}>('admin.order.addParcel', { orderId:orderId.value, ...form.value })
    message.value = result.shipment.syncStatus === 'FAILED' ? '包裹已保存，微信同步失败，请在订单列表重试同步' : '包裹已保存'
    form.value.trackingNo = ''; emit('changed')
  } catch(e) { error.value = (e as Error).message }
  finally { busy.value = false }
}
function key(p: Parcel) { return `${p.carrierCode}:${p.trackingNo}` }
const shippedOrders = computed(() => props.orders.filter(o => o.shipment))
const printSelected = ref<string[]>([])
const printBusy = ref(false)
function esc(s: string) { return s.replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c] as string)) }
async function printWaybills() {
  if (!printSelected.value.length || printBusy.value) return
  printBusy.value = true; error.value = ''; message.value = ''
  try {
    const items = await api<Array<{ orderId: string; orderNo: string; recipient: { name: string; phone: string; province: string; city: string; district: string; detail: string }; items: Array<{ name: string; skuName: string; quantity: number; weightLabel: string }>; shipment?: { carrier: string; carrierCode: string; trackingNo: string }; waybillUrl?: string }>>('admin.waybill.batchGet', { orderIds: printSelected.value })
    const pages = items.map(it => {
      const addr = [it.recipient.province, it.recipient.city, it.recipient.district, it.recipient.detail].filter(Boolean).join('')
      const goods = it.items.map(g => `<tr><td>${esc(g.name)}${g.skuName ? '（' + esc(g.skuName) + '）' : ''}</td><td>${g.quantity}</td><td>${esc(g.weightLabel || '')}</td></tr>`).join('')
      const ship = it.shipment ? `<p>承运商：${esc(it.shipment.carrier || '')}　运单号：<strong>${esc(it.shipment.trackingNo || '')}</strong></p>` : '<p>尚未发货</p>'
      const label = it.waybillUrl ? `<p>电子面单：<a href="${esc(it.waybillUrl)}" target="_blank">下载/打印电子面单</a></p>` : ''
      return `<div class="waybill-page"><h2>赤鸡葡萄园 · 发货面单</h2><p>订单号：<strong>${esc(it.orderNo)}</strong></p>${ship}${label}<h3>收件人</h3><p>${esc(it.recipient.name)}　${esc(it.recipient.phone)}</p><p>${esc(addr)}</p><h3>商品明细</h3><table><thead><tr><th>商品</th><th>数量</th><th>规格</th></tr></thead><tbody>${goods}</tbody></table></div>`
    }).join('')
    const win = window.open('', '_blank')
    if (!win) throw new Error('浏览器拦截了打印窗口，请允许弹窗后重试')
    win.document.write(`<!DOCTYPE html><html lang="zh-CN"><head><meta charset="utf-8"><title>面单批量打印（${items.length}单）</title><style>body{font-family:sans-serif;padding:16px}.waybill-page{border:1px solid #333;padding:16px;margin-bottom:16px}table{border-collapse:collapse;width:100%}th,td{border:1px solid #999;padding:4px 8px;text-align:left}@media print{.waybill-page{page-break-after:always;border:none}}</style></head><body>${pages}<script>window.onload=()=>window.print()<\/script></body></html>`)
    win.document.close()
    message.value = `已生成 ${items.length} 张面单打印页`
  } catch (e) { error.value = (e as Error).message }
  finally { printBusy.value = false }
}
</script>

<template>
  <section class="panel">
    <h2>多运单与物流异常任务</h2>
    <p class="subtle">选取当前订单页中的已发货订单；每单最多10个包裹。任务保存跟进历史，关闭时需填写处理结果。</p>
    <p v-if="error" class="form-error" role="alert">{{ error }}</p><p v-if="message" class="success-text" role="status">{{ message }}</p>
    <label>订单<select v-model="orderId" :disabled="busy || !!editing" @change="parcelKey = ''"><option value="">请选择已发货订单</option><option v-for="o in orders.filter(o => o.shipment)" :key="o.id" :value="o.id">{{ o.orderNo }}</option></select></label>
    <div v-if="order?.shipment">
      <p v-for="p in parcels" :key="key(p)">{{ p.carrier }} · {{ p.trackingNo }}</p>
      <form v-permission="'order.ship'" @submit.prevent="addParcel">
        <h3>追加运单</h3><label>承运商<input v-model="form.carrier" required maxlength="40" :disabled="busy" /></label><label>公司编码<input v-model="form.carrierCode" required maxlength="20" :disabled="busy" /></label><label>运单号<input v-model="form.trackingNo" required maxlength="50" :disabled="busy" /></label>
        <button class="button secondary" :disabled="busy || order.fulfillmentStatus !== 'SHIPPED'">确认交寄并追加运单</button>
      </form>
    </div>
    <form v-permission="'order.ship'" @submit.prevent="saveTask">
      <h3>{{ editing ? `跟进任务 · ${editing.orderNo} · ${editing.trackingNo}` : '创建异常任务' }}</h3>
      <label v-if="!editing">运单<select v-model="parcelKey" :disabled="busy"><option v-for="p in parcels" :key="key(p)" :value="key(p)">{{ p.carrier }} · {{ p.trackingNo }}</option></select></label>
      <label>异常类型<select v-model="kind" :disabled="busy || !!editing"><option v-for="(label,value) in kinds" :key="value" :value="value">{{ label }}</option></select></label>
      <label>负责人<input v-model="owner" maxlength="60" :disabled="busy" placeholder="姓名或账号" /></label>
      <label>状态<select v-model="taskStatus" :disabled="busy"><option v-for="(label,value) in statuses" :key="value" :value="value">{{ label }}</option></select></label>
      <label>跟进说明 / 处理结果<textarea v-model="note" maxlength="1000" required :disabled="busy" /></label>
      <button class="button primary" :disabled="busy || (!editing && !parcel)">保存任务</button> <button v-if="editing" class="button secondary" type="button" :disabled="busy" @click="reset">取消编辑</button>
    </form>
    <section v-permission="'order.ship'">
      <h3>面单批量打印</h3>
      <p class="subtle">勾选当前订单页中的已发货订单，一次生成多张面单打印页（只读，不改变订单状态）。面单纸尺寸如需调整请联系开发。</p>
      <p v-if="!shippedOrders.length" class="subtle">当前订单页暂无已发货订单</p>
      <div v-for="o in shippedOrders" :key="o.id"><label><input type="checkbox" :value="o.id" v-model="printSelected" :disabled="printBusy" /> {{ o.orderNo }}{{ o.shipment?.trackingNo ? ' · ' + o.shipment.trackingNo : '' }}</label></div>
      <p><button class="button primary" :disabled="printBusy || !printSelected.length" @click="printWaybills">打印预览（{{ printSelected.length }} 单）</button></p>
    </section>
    <h3>任务列表</h3><label>任务状态<select v-model="status" :disabled="busy" @change="load"><option value="">全部</option><option v-for="(label,value) in statuses" :key="value" :value="value">{{ label }}</option></select></label>
    <button class="button secondary" :disabled="busy" @click="load">刷新任务</button>
    <p v-if="!tasks.length && !busy" class="subtle">暂无任务</p>
    <article v-for="task in tasks" :key="task.id" class="panel">
      <strong>{{ task.orderNo }} · {{ statuses[task.status] }}</strong><p>{{ task.carrierCode }} · {{ task.trackingNo }} · {{ kinds[task.kind] }}</p><p>负责人：{{ task.owner || '未分配' }} · {{ dateTime(task.updatedAt) }}</p><p>{{ task.note }}</p>
      <button v-permission="'order.ship'" class="button secondary" :disabled="busy" @click="editTask(task)">分派 / 跟进 / 关闭</button>
      <details><summary>跟进历史（{{ task.history.length }}）</summary><p v-for="(entry,index) in task.history" :key="index">{{ dateTime(entry.at) }} · {{ entry.actor }} · {{ statuses[entry.status] }} · {{ entry.owner || '未分配' }}：{{ entry.note }}</p></details>
    </article>
    <p class="subtle">最多展示最近200项任务。物流异常由运营人员核实后创建。</p>
  </section>
</template>
