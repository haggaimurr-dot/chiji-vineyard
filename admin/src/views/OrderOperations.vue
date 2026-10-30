<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { api, isDemo } from '../api'
import { confirmAction } from '../confirm'
import { canShip, downloadCSV, orderFilter, validateShipmentItems } from '../order-operations'
import type { BatchShipmentResult, Order, OrderExportResult, ShipmentItem, Waybill, WaybillConfig, WaybillLabel, WaybillQuotes } from '../types'

const props = defineProps<{ orders: Order[]; selectedIds: string[]; filter: string; keyword: string; loading: boolean }>()
const emit = defineEmits<{ changed: []; 'update:busy': [boolean] }>()
const selected = computed(() => props.orders.filter(order => props.selectedIds.includes(order.id)))
const busy = ref(false), error = ref(''), message = ref('')
watch(busy, value => emit('update:busy', value))
const batchOpen = ref(false), batchError = ref(''), batchSummary = ref('')
const batchReady = ref(false), batchDrawer = ref<HTMLElement|null>(null), waybillDrawer = ref<HTMLElement|null>(null)
let dialogTrigger: HTMLElement|null = null
function restoreFocus() { void nextTick(() => dialogTrigger?.focus()) }
function closeBatch() { if (!busy.value) { batchOpen.value = false; restoreFocus() } }
function dialogKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') { event.preventDefault(); if (batchOpen.value) closeBatch(); else closeLabel(); return }
  if (event.key !== 'Tab') return
  const drawer = batchOpen.value ? batchDrawer.value : waybillDrawer.value
  const controls = Array.from(drawer?.querySelectorAll<HTMLElement>('button:not(:disabled), input:not(:disabled), select:not(:disabled), [tabindex="0"]') || []).filter(element => element.getClientRects().length > 0)
  const first = controls[0], last = controls[controls.length - 1]
  if (!first) { event.preventDefault(); drawer?.focus() }
  else if (event.shiftKey && (document.activeElement === first || document.activeElement === drawer)) { event.preventDefault(); last?.focus() }
  else if (!event.shiftKey && (document.activeElement === last || document.activeElement === drawer)) { event.preventDefault(); first.focus() }
}
type BatchRow = ShipmentItem & { orderNo: string; success?: boolean; error?: string; syncStatus?: string }
const batchRows = ref<BatchRow[]>([])
const remaining = computed(() => batchRows.value.filter(row => !row.success))
const labelOrder = ref<Order|null>(null), label = ref<Waybill|null>(null), labelConfig = ref<WaybillConfig|null>(null)
const labelBusy = ref(false), labelError = ref(''), labelCarrier = ref('SF'), labelWeight = ref(1)
const cancelReason = ref(''), history = ref<Array<{waybill: Waybill; reason: string; actor: string; at: string}>>([])
const labelPaperHeight = ref(150)
const quotes = ref<WaybillQuotes|null>(null)
watch(labelWeight, () => { quotes.value = null })
async function compareRates() {
  if (!labelOrder.value || busy.value || labelBusy.value) return
  labelBusy.value = true; labelError.value = ''; quotes.value = null
  try { quotes.value = await api<WaybillQuotes>('admin.waybill.quote', { orderId: labelOrder.value.id, weight: labelWeight.value }) }
  catch (e) { labelError.value = (e as Error).message }
  finally { labelBusy.value = false }
}
const labelStatus = (value: string) => ({ CREATING: '正在获取运单', READY: '面单已生成', FAILED: '获取失败', UNCERTAIN: '结果待核实', CANCELLING: '取消处理中', CANCEL_UNCERTAIN: '取消结果待核实', CANCELLED: '面单已取消' }[value] || value)
let labelRequest = 0
onBeforeUnmount(() => { ++labelRequest })

async function exportOrders(onlySelected: boolean) {
  if (busy.value || props.loading) return
  busy.value = true; error.value = ''; message.value = ''
  try {
    const result = await api<OrderExportResult>('admin.order.export', { keyword: props.keyword, filter: orderFilter(props.filter), ...(onlySelected ? { orderIds: selected.value.map(order => order.id) } : {}) })
    downloadCSV(result); message.value = `已导出 ${result.count} 笔订单`
  } catch (e) { error.value = (e as Error).message }
  finally { busy.value = false }
}

async function openBatch() {
  if (busy.value || props.loading) return
  error.value = ''; message.value = ''; batchError.value = ''; batchSummary.value = ''
  if (!selected.value.length || selected.value.length > 50) { error.value = '请选择1至50笔订单'; return }
  if (selected.value.some(order => (!canShip(order) || order.packages?.length))) { error.value = '所选订单包含不可发货订单，请只勾选已付款且无进行中售后的待发货订单'; return }
  batchRows.value = selected.value.map(order => ({ orderId: order.id, orderNo: order.orderNo, carrier: '顺丰速运', carrierCode: 'SF', trackingNo: '' }))
  dialogTrigger = document.activeElement as HTMLElement|null
  batchReady.value = false; batchOpen.value = true; busy.value = true
  await nextTick(); batchDrawer.value?.focus()
  try {
    const labels = await Promise.all(batchRows.value.map(row => api<Waybill|null>('admin.waybill.get', { orderId: row.orderId })))
    labels.forEach((saved, index) => {
      if (saved?.status === 'READY') Object.assign(batchRows.value[index], { carrier: saved.carrier, carrierCode: saved.carrierCode, trackingNo: saved.trackingNo })
      else if (saved && ['CREATING', 'UNCERTAIN'].includes(saved.status)) batchRows.value[index].error = '电子面单结果待核实，请先在面单中处理'
    })
    batchReady.value = true
  } catch (e) { batchError.value = `读取已有面单失败：${(e as Error).message}。请关闭后重试。` }
  finally { busy.value = false }
}

async function submitBatch() {
  if (busy.value || !batchReady.value || !remaining.value.length) return
  const items = remaining.value.map(({ orderId, carrier, carrierCode, trackingNo }) => ({ orderId, carrier: carrier.trim(), carrierCode: carrierCode.trim().toUpperCase(), trackingNo: trackingNo.trim() }))
  const issues = validateShipmentItems(items)
  if (issues.length) { batchError.value = issues.join('；'); return }
  if (!(await confirmAction({ title: '批量确认交寄', message: `确认这 ${items.length} 笔订单已交寄？提交后将标记发货并同步微信。`, confirmText: '确认发货' }))) return
  busy.value = true; batchError.value = ''; batchSummary.value = ''
  try {
    const result = await api<BatchShipmentResult>('admin.order.batchShip', { items })
    result.results.forEach(outcome => {
      const row = batchRows.value.find(item => item.orderId === outcome.orderId)
      if (row) Object.assign(row, { success: outcome.success, error: outcome.error, syncStatus: outcome.syncStatus || outcome.shipment?.syncStatus })
    })
    batchSummary.value = `本次成功 ${result.succeeded} 笔，失败 ${result.failed} 笔。${result.failed ? '检查失败原因后可仅重试未成功的订单。' : ''}`
    emit('changed')
  } catch (e) { batchError.value = `提交结果需核实：${(e as Error).message}。刷新后核对订单，使用相同运单重试会复用已发货结果。`; emit('changed') }
  finally { busy.value = false }
}

async function openLabel(order: Order) {
  if (busy.value) return
  dialogTrigger = document.activeElement as HTMLElement|null
  const request = ++labelRequest
  labelOrder.value = order; label.value = null; labelConfig.value = null; labelError.value = ''; labelBusy.value = true
  history.value = []; cancelReason.value = ''
  labelCarrier.value = order.shipment?.carrierCode || 'SF'; labelWeight.value = 1; quotes.value = null
  await nextTick(); waybillDrawer.value?.focus()
  try {
    const [saved, config] = await Promise.all([api<Waybill|null>('admin.waybill.get', { orderId: order.id }), api<WaybillConfig>('admin.waybill.config')])
    if (request !== labelRequest) return
    label.value = saved; labelConfig.value = config
    history.value = await api('admin.waybill.history', { orderId: order.id })
    if (saved) labelCarrier.value = saved.carrierCode
    else if (!config.carriers.some(carrier => carrier.code === labelCarrier.value)) labelCarrier.value = config.carriers[0]?.code || ''
  } catch (e) { if (request === labelRequest) labelError.value = (e as Error).message }
  finally { if (request === labelRequest) labelBusy.value = false }
}

async function createLabel() {
  if (!labelOrder.value || labelBusy.value || busy.value) return
  labelBusy.value = true; busy.value = true; labelError.value = ''
  try { label.value = await api<Waybill>('admin.waybill.create', { orderId: labelOrder.value.id, carrierCode: labelCarrier.value, weight: labelWeight.value }) }
  catch (e) {
    labelError.value = (e as Error).message
    try { label.value = await api<Waybill|null>('admin.waybill.get', { orderId: labelOrder.value.id }) } catch {}
  } finally { labelBusy.value = false; busy.value = false }
}

async function cancelLabel() {
  if (!labelOrder.value || !label.value || busy.value || labelBusy.value) return
  if (!cancelReason.value.trim()) { labelError.value = '请填写取消原因'; return }
  if (!(await confirmAction({ title: '取消电子面单', message: '确认包裹尚未交寄并取消此面单？取消成功后原面单不可使用。', danger: true, confirmText: '确认取消' }))) return
  busy.value = true; labelError.value = ''
  try {
    label.value = await api<Waybill>('admin.waybill.cancel', { orderId: labelOrder.value.id, waybillId: label.value.id, reason: cancelReason.value })
    history.value = await api('admin.waybill.history', { orderId: labelOrder.value.id })
  } catch(e) { labelError.value = (e as Error).message; try {label.value = await api('admin.waybill.get', {orderId:labelOrder.value.id})} catch {} }
  finally { busy.value = false }
}

async function syncLabel() {
  if (!labelOrder.value || busy.value) return
  busy.value = true; labelBusy.value = true; labelError.value = ''
  try { label.value = await api<Waybill>('admin.waybill.sync', { orderId: labelOrder.value.id }) }
  catch (e) { labelError.value = (e as Error).message }
  finally { busy.value = false; labelBusy.value = false }
}

async function shipLabel() {
  if (!labelOrder.value || !label.value || busy.value) return
  if (!(await confirmAction({ title: '面单发货', message: '确认包裹已交寄？将使用这张面单的运单号发货并同步微信。', confirmText: '确认发货' }))) return
  busy.value = true; labelError.value = ''
  try {
    const result = await api<{ shipment: NonNullable<Order['shipment']> }>('admin.order.ship', { orderId: labelOrder.value.id, carrier: label.value.carrier, carrierCode: label.value.carrierCode, trackingNo: label.value.trackingNo })
    labelOrder.value = { ...labelOrder.value, fulfillmentStatus: 'SHIPPED', shipment: result.shipment }
    message.value = result.shipment.syncStatus === 'FAILED' ? '订单已发货，微信同步失败，可在订单列表重试同步' : '订单已发货'
    emit('changed')
  } catch (e) { labelError.value = (e as Error).message; emit('changed') }
  finally { busy.value = false }
}

async function printLabel() {
  if (!labelOrder.value || !label.value || busy.value) return
  const printWindow = window.open('', '_blank', 'width=520,height=780')
  if (!printWindow) { labelError.value = '浏览器拦截了打印窗口，请允许弹出窗口后重试'; return }
  printWindow.document.title = `电子面单 · ${labelOrder.value.orderNo}`
  printWindow.document.body.textContent = '正在加载面单…'
  busy.value = true; labelError.value = ''
  try {
    const style = printWindow.document.createElement('style')
    const paperHeight = [100, 150, 180, 200].includes(labelPaperHeight.value) ? labelPaperHeight.value : 150
    style.textContent = `@page { size: 100mm ${paperHeight}mm; margin: 0; } body { margin: 0; font: 14px sans-serif; } img { display: block; width: 100mm; height: auto; } article { box-sizing: border-box; width: 100mm; padding: 8mm; border: 2px dashed #555; } h1 { font-size: 22px; } p { overflow-wrap: anywhere; line-height: 1.6; } .demo { border: 2px solid #a00; padding: 10px; color: #a00; font-weight: bold; }`
    printWindow.document.head.append(style)
    printWindow.document.body.replaceChildren()
    if (isDemo || label.value.demo) {
      const article = printWindow.document.createElement('article')
      for (const [tag, text] of [ ['h1', '迟记葡萄牙 · 示例面单'], ['p', '演示面单，不可交寄'], ['p', `${label.value.carrier} · ${label.value.trackingNo}`], ['p', `订单：${labelOrder.value.orderNo}`], ['p', `收件人：${labelOrder.value.address.name} ${labelOrder.value.address.phone}`], ['p', `${labelOrder.value.address.province}${labelOrder.value.address.city}${labelOrder.value.address.district}${labelOrder.value.address.detail}`], ['p', labelOrder.value.items.map(item => `${item.name} ${item.skuName} ×${item.quantity}`).join('、')] ]) {
        const node = printWindow.document.createElement(tag); node.textContent = text
        if (text.includes('不可交寄')) node.className = 'demo'
        article.append(node)
      }
      printWindow.document.body.append(article)
      printWindow.focus(); printWindow.print()
    } else {
      const image = await api<WaybillLabel>('admin.waybill.label', { orderId: labelOrder.value.id })
      if (!/^image\/(png|jpeg|webp)$/.test(image.mime) || !image.dataUrl.startsWith(`data:${image.mime};base64,`)) throw new Error('面单图片格式无效')
      const img = printWindow.document.createElement('img'); img.alt = '快递100电子面单'; img.src = image.dataUrl
      printWindow.document.body.append(img)
      await img.decode(); printWindow.focus(); printWindow.print()
    }
  } catch (e) { labelError.value = (e as Error).message; printWindow.close() }
  finally { busy.value = false }
}

function closeLabel() { if (!busy.value && !labelBusy.value) { ++labelRequest; labelOrder.value = null; restoreFocus() } }
defineExpose({ openLabel })
</script>

<template>
  <section class="panel order-operations">
    <div class="operations-summary"><strong>订单处理</strong><span class="subtle">已选 {{ selected.length }} 笔 · 单次发货最多50笔</span></div>
    <div class="operations-actions">
      <button v-permission="'order.ship'" class="button primary" :disabled="busy || loading || !selected.length" @click="openBatch">批量发货</button>
      <button v-permission="'order.export'" class="button secondary" :disabled="busy || loading || !selected.length" @click="exportOrders(true)">导出所选</button>
      <button v-permission="'order.export'" class="button secondary" :disabled="busy || loading" @click="exportOrders(false)">导出筛选结果</button>
    </div>
    <p class="operations-hint subtle">CSV 可用 Excel 打开，包含收货资料和商品明细；导出筛选结果包含全部匹配订单。电子面单可在每笔订单的操作中获取。</p>
    <p v-if="message" class="success-text" role="status">{{ message }}</p><p v-if="error" class="form-error" role="alert">{{ error }}</p>
  </section>

  <div v-if="batchOpen" class="modal-backdrop" @click.self="closeBatch" @keydown="dialogKeydown">
    <section ref="batchDrawer" class="drawer shipping-drawer" role="dialog" aria-modal="true" aria-label="批量发货" tabindex="-1">
      <div class="drawer-head"><div><h2>批量发货</h2><p class="subtle">逐单填写运单；已有电子面单自动带入。</p></div><button class="close" :disabled="busy" aria-label="关闭批量发货" @click="closeBatch">×</button></div>
      <div class="drawer-body">
        <p v-if="busy" class="subtle" role="status">正在处理，请稍候…</p>
        <p v-if="batchError" class="alert error" role="alert">{{ batchError }}</p><p v-if="batchSummary" class="alert" role="status">{{ batchSummary }}</p>
        <fieldset v-for="row in batchRows" :key="row.orderId" class="shipment-row" :disabled="busy || row.success">
          <legend>{{ row.orderNo }} <span v-if="row.success" class="success-text">已发货</span></legend>
          <div class="shipment-fields"><label>承运商<input v-model="row.carrier" maxlength="40" placeholder="例：顺丰速运"/></label><label>公司编码<input v-model="row.carrierCode" maxlength="20" placeholder="例：SF"/></label></div>
          <label>运单号<input v-model="row.trackingNo" maxlength="50" placeholder="填写真实运单号，或先获取电子面单"/></label>
          <p v-if="row.error" class="form-error">{{ row.error }}</p><p v-if="row.success && row.syncStatus === 'FAILED'" class="form-error">已发货，微信同步失败；请在订单列表重试同步。</p>
        </fieldset>
      </div>
      <div class="drawer-foot"><button class="button secondary" :disabled="busy" @click="closeBatch">{{ remaining.length ? '关闭' : '完成' }}</button><button v-permission="'order.ship'" v-if="remaining.length" class="button primary" :disabled="busy || !batchReady" @click="submitBatch">{{ busy ? '正在处理…' : batchSummary ? `重试未成功的 ${remaining.length} 笔` : `确认发货 ${remaining.length} 笔` }}</button></div>
    </section>
  </div>

  <div v-if="labelOrder" class="modal-backdrop" @click.self="closeLabel" @keydown="dialogKeydown">
    <section ref="waybillDrawer" class="drawer" role="dialog" aria-modal="true" aria-label="电子面单" tabindex="-1">
      <div class="drawer-head"><div><h2>电子面单</h2><p class="subtle">{{ labelOrder.orderNo }} · 快递100</p></div><button class="close" :disabled="busy || labelBusy" aria-label="关闭电子面单" @click="closeLabel">×</button></div>
      <div class="drawer-body waybill-body">
        <details v-if="history.length"><summary>面单取消历史</summary><p v-for="entry in history" :key="entry.at">{{ entry.waybill.trackingNo }} · {{ labelStatus(entry.waybill.status) }} · {{ entry.actor }}：{{ entry.reason }}</p></details>
        <p v-if="labelBusy" class="subtle" role="status">正在读取面单…</p><p v-if="labelError" class="alert error" role="alert">{{ labelError }}</p>
        <div class="waybill-address"><strong>{{ labelOrder.address.name }} · {{ labelOrder.address.phone }}</strong><p>{{ labelOrder.address.province }} {{ labelOrder.address.city }} {{ labelOrder.address.district }} {{ labelOrder.address.detail }}</p><p>{{ labelOrder.items.map(item => `${item.name} ${item.skuName} ×${item.quantity}`).join('、') }}</p></div>
        <template v-if="label">
          <p><span class="status">{{ labelStatus(label.status) }}</span></p><p v-if="label.demo" class="alert">演示面单不可交寄，不会向快递100创建真实运单。</p>
          <div class="waybill-number"><small>{{ label.carrier }}</small><strong>{{ label.trackingNo || '暂未取得运单号' }}</strong></div>
          <label v-permission="'order.ship'" v-if="label.status === 'READY' && !labelOrder.shipment">取消原因<input v-model="cancelReason" maxlength="100" :disabled="busy || labelBusy" /></label>
          <p v-if="label.error" class="form-error">{{ label.error }}</p>
          <p v-if="label.status === 'READY'" class="subtle">同一订单重复打印使用同一运单号。确认交寄后再发货。</p>
          <label v-if="label.status === 'READY'" class="waybill-paper">面单纸张<select v-model.number="labelPaperHeight" :disabled="busy || labelBusy"><option v-for="height in [100,150,180,200]" :key="height" :value="height">100 × {{ height }} 毫米</option></select><small class="block">与快递100模板及打印机纸张保持一致，打印时按原始尺寸输出。</small></label>
          <p v-if="['CREATING','UNCERTAIN'].includes(label.status)" class="subtle">请先核实快递100的下单结果。有运单号时可尝试重新获取面单图片；请勿重复下单。</p>
        </template>
        <template v-if="!label || ['FAILED','CANCELLED'].includes(label.status)">
          <p v-if="labelConfig && !labelConfig.configured" class="alert">{{ labelConfig.message || '尚未配置快递100面单账号及寄件资料。' }} 请由超级管理员在「运营配置 → 电子面单」配置。</p>
          <form v-if="labelConfig?.configured && canShip(labelOrder) && !labelOrder.packages?.length" id="create-waybill" class="waybill-form" @submit.prevent="createLabel">
            <label>承运商<select v-model="labelCarrier" aria-label="承运商" :disabled="busy || labelBusy || (!!label && label.status !== 'CANCELLED')" required><option v-for="carrier in labelConfig.carriers" :key="carrier.code" :value="carrier.code">{{ carrier.name }}</option></select></label>
            <label>包裹重量（千克）<input v-model.number="labelWeight" type="number" min="0.01" max="100" step="0.01" required :disabled="busy || labelBusy"/></label>
            <button type="button" class="button secondary" :disabled="busy || labelBusy" @click="compareRates">比较运费</button>
            <div v-if="quotes" class="waybill-quotes">
              <p>{{ quotes.province }} · {{ quotes.weight }} 公斤 · 协议价预估</p>
              <div v-for="quote in quotes.items" :key="quote.carrierCode" class="waybill-quote">
                <span><strong>{{ quote.carrier }}</strong><small class="block">{{ quote.delivery || quote.message || '时效未配置' }}</small></span>
                <span>{{ quote.available ? `¥${((quote.fee || 0) / 100).toFixed(2)}` : '暂无报价' }}<small v-if="quote.available" class="block">计费重量 {{ quote.billableWeight }} 公斤</small></span>
                <button type="button" class="button small secondary" :disabled="busy || labelBusy || (!!label && label.status !== 'CANCELLED')" @click="labelCarrier = quote.carrierCode">{{ labelCarrier === quote.carrierCode ? '已选择' : '选择' }}</button>
              </div><p class="subtle">{{ quotes.message }}</p>
              <p v-if="!quotes.items.length" class="subtle">没有已开通的承运商，请检查电子面单账号配置。</p>
            </div>
            <p class="subtle">使用服务端配置的寄件人及商家面单账号获取运单；生成面单不会自动发货。</p>
          </form>
          <p v-else-if="!labelBusy && !canShip(labelOrder) && !labelOrder.packages?.length" class="subtle">当前订单不满足获取面单条件：需已付款、待发货且无进行中售后。</p>
        </template>
      </div>
      <div class="drawer-foot"><button v-permission="'order.ship'" v-if="label?.status === 'READY' && !labelOrder.shipment" class="button secondary" :disabled="busy || labelBusy" @click="cancelLabel">取消面单</button><button class="button secondary" :disabled="busy || labelBusy" @click="closeLabel">关闭</button><button v-permission="'order.ship'" v-if="label && ['CREATING','UNCERTAIN'].includes(label.status) && label.trackingNo" class="button secondary" :disabled="busy || labelBusy" @click="syncLabel">重新获取面单图片</button><button v-if="label?.status === 'READY'" class="button secondary" :disabled="busy || labelBusy" @click="printLabel">打印面单</button><button v-permission="'order.ship'" v-if="label?.status === 'READY' && canShip(labelOrder) && !labelOrder.packages?.length" class="button primary" :disabled="busy || labelBusy" @click="shipLabel">确认交寄并发货</button><button v-permission="'order.ship'" v-if="(!label || ['FAILED','CANCELLED'].includes(label.status)) && labelConfig?.configured && canShip(labelOrder) && !labelOrder.packages?.length" class="button primary" form="create-waybill" :disabled="busy || labelBusy">{{ busy ? '正在获取…' : '获取电子面单' }}</button></div>
    </section>
  </div>
</template>
