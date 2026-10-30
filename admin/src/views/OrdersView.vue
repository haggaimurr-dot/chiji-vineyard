<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { api, dateTime, money } from '../api'
import { confirmAction } from '../confirm'
import { can, currentAdmin } from '../permissions'
import { canEditShipment, canShip, fulfillmentStateLabel, orderStateLabel, orderSummary, paymentStateLabel, refundStateLabel, validateShipment } from '../orders'
import type { Order, OrderSearchResult, Refund, Tracking } from '../types'
import OrderOperations from './OrderOperations.vue'
import LogisticsPanel from './LogisticsPanel.vue'
import OrderPackages from './OrderPackages.vue'
const packageOrderId = ref('')
const packageOrder = computed(() => orders.value.find(o => o.id === packageOrderId.value))
import OrderBatches from './OrderBatches.vue'
import InvoiceActions from './InvoiceActions.vue'
import AfterSalesPanel from './AfterSalesPanel.vue'
import PickingSheet from './PickingSheet.vue'
import OrderAlertPanel from './OrderAlertPanel.vue'
import { orderExceptions } from '../picking'
import { logisticsAlerts, orderProgress } from '../order-progress'

const workspace = ref('orders')
function switchWorkspace(value: string) {
  if (operationBusy.value) return
  workspace.value = value
  filter.value = value === 'picking' ? 'shipping_pending' : value === 'exceptions' ? 'exceptions' : 'all'
}
const exceptionFilters = [
  { key: 'exceptions', label: '全部异常' }, { key: 'sync_failed', label: '微信同步失败' },
  { key: 'notice_exception', label: '发货提醒异常' }, { key: 'refund_failed', label: '退款失败' },
  { key: 'invoice_exception', label: '发票邮件异常' },
]
const visibleFilters = computed(() => workspace.value === 'exceptions' ? exceptionFilters : workspace.value === 'picking' ? filters.filter(item => item.key === 'shipping_pending') : filters)

const ticketOrderId = ref(''), ticketOrderNo = ref(''), ticketRefundId = ref('')
const selectedIds = ref<string[]>([]), operationBusy = ref(false), operations = ref<InstanceType<typeof OrderOperations>|null>(null)
const allSelected = computed(() => orders.value.length > 0 && orders.value.every(order => selectedIds.value.includes(order.id)))
function toggleAll(event: Event) { selectedIds.value = (event.target as HTMLInputElement).checked ? orders.value.map(order => order.id) : [] }

const orders = ref<Order[]>([]), refunds = ref<Refund[]>([])
const filter = ref('all'), keyword = ref(''), appliedKeyword = ref('')
const currentPage = ref(1), pageSize = ref(20), total = ref(0), loading = ref(false)
const listError = ref(''), error = ref(''), success = ref(''), refundError = ref(''), refundsLoading = ref(false)
const pages = computed(() => Math.max(1, Math.ceil(total.value / pageSize.value)))
const range = computed(() => total.value ? `${(currentPage.value - 1) * pageSize.value + 1}–${Math.min(currentPage.value * pageSize.value, total.value)}` : '0')
const filters = [
  { key: 'all', label: '全部' }, { key: 'pending', label: '待付款' }, { key: 'shipping_pending', label: '待发货' },
  { key: 'shipped', label: '待收货' }, { key: 'received', label: '已收货' }, { key: 'completed', label: '已完成' },
  { key: 'cancelled', label: '已取消' }, { key: 'closed', label: '已关闭' }, { key: 'refund', label: '退款 / 售后' },
  { key: 'invoice_pending', label: '待开票' }, { key: 'invoice_email', label: '发票邮件待处理' },
]
type SavedOrderView = { name: string; filter: string; keyword: string }
const savedViews = ref<SavedOrderView[]>([])
function orderViewKey() {
  try { return 'chiji_order_views_' + ((JSON.parse(localStorage.getItem('chiji_admin_user') || '{}') as { id?: string }).id || 'anon') }
  catch { return 'chiji_order_views_anon' }
}
function loadSavedViews() {
  try {
    const raw = JSON.parse(localStorage.getItem(orderViewKey()) || '[]')
    savedViews.value = Array.isArray(raw) ? raw.filter(v => v && typeof v.name === 'string').slice(0, 20) : []
  } catch { savedViews.value = [] }
}
function persistViews() { try { localStorage.setItem(orderViewKey(), JSON.stringify(savedViews.value)) } catch { /* quota */ } }
function saveCurrentView() {
  const name = window.prompt('为当前筛选（状态 + 关键词）命名', `${visibleFilters.value.find(f => f.key === filter.value)?.label || ''}${appliedKeyword.value ? ' · ' + appliedKeyword.value : ''}`.trim() || '我的筛选')
  if (!name || !name.trim()) return
  savedViews.value = [{ name: name.trim().slice(0, 24), filter: filter.value, keyword: appliedKeyword.value }, ...savedViews.value.filter(v => v.name !== name.trim())].slice(0, 20)
  persistViews()
}
function applySavedView(view: SavedOrderView) {
  keyword.value = view.keyword; appliedKeyword.value = view.keyword
  if (visibleFilters.value.some(f => f.key === view.filter)) filter.value = view.filter
  resetSearch()
}
function deleteSavedView(index: number) { savedViews.value.splice(index, 1); persistViews() }
loadSavedViews()
let listRequest = 0, refundRequest = 0
async function loadOrders() {
  const request = ++listRequest
  selectedIds.value = []
  const query = { keyword: appliedKeyword.value, filter: filter.value, offset: (currentPage.value - 1) * pageSize.value, limit: pageSize.value }
  loading.value = true; listError.value = ''
  try {
    const result = await api<OrderSearchResult>('admin.order.search', query)
    if (request !== listRequest) return
    if (query.offset > 0 && query.offset >= result.total) {
      currentPage.value = Math.max(1, Math.ceil(result.total / pageSize.value))
      await loadOrders(); return
    }
    orders.value = result.items; total.value = result.total
  } catch (e) { if (request === listRequest) listError.value = (e as Error).message }
  finally { if (request === listRequest) loading.value = false }
}
function resetSearch() { currentPage.value = 1; orders.value = []; total.value = 0; void loadOrders() }
function search() { appliedKeyword.value = keyword.value.trim(); resetSearch() }
function clearSearch() { keyword.value = ''; appliedKeyword.value = ''; resetSearch() }
function goPage(value: number) {
  if (operationBusy.value || loading.value || value < 1 || value > pages.value) return
  currentPage.value = value; orders.value = []; void loadOrders()
}
watch([filter, pageSize], resetSearch)
async function loadRefunds() {
  if (!can(currentAdmin(), 'refund.read')) { refunds.value = []; return }
  const request = ++refundRequest
  refundsLoading.value = true; refundError.value = ''
  try { const items = await api<Refund[]>('admin.refund.list'); if (request === refundRequest) refunds.value = items }
  catch (e) { if (request === refundRequest) refundError.value = (e as Error).message }
  finally { if (request === refundRequest) refundsLoading.value = false }
}
async function refresh() { await Promise.all([loadOrders(), loadRefunds()]) }
onMounted(refresh)
onBeforeUnmount(() => { listRequest++; refundRequest++; trackingRequest++; evidenceRequest++ })

const noticeLabel = (value?: string) => ({ NONE: '未订阅微信提醒', READY: '等待发送', SENDING: '发送结果待核实', SENT: '微信提醒已发送', FAILED: '微信提醒失败', BLOCKED: '用户未授权微信提醒', UNCERTAIN: '推送结果待核实' }[value || 'NONE'] || `状态待确认（${value}）`)
const syncLabel = (value: string) => ({ PENDING: '微信发货同步中 / 待重试', SUCCESS: '微信发货已同步', FAILED: '微信发货同步失败' }[value] || `微信同步状态待确认（${value || '未知'}）`)
const invoiceLabel = (value: string) => ({ APPLIED: '待处理', ISSUED: '已开票', REJECTED: '已退回' }[value] || `状态待确认（${value}）`)
const noticeBusy = ref(''), syncBusy = ref(''), refundBusy = ref('')
async function retryNotice(order: Order) {
  if (operationBusy.value || noticeBusy.value) return
  noticeBusy.value = order.id; error.value = ''; success.value = ''
  try {
    const result = await api<{ status: string; error?: string }>('admin.order.retryShippingNotice', { orderId: order.id })
    await loadOrders()
    if (result.error) error.value = result.error
    else success.value = noticeLabel(result.status)
  } catch (e) { error.value = (e as Error).message }
  finally { noticeBusy.value = '' }
}
async function resync(order: Order) {
  if (operationBusy.value || syncBusy.value) return
  syncBusy.value = order.id; error.value = ''; success.value = ''
  try {
    const result = await api<{ shipment: NonNullable<Order['shipment']> }>('admin.order.resyncShipment', { orderId: order.id })
    await loadOrders()
    if (result.shipment.syncStatus === 'FAILED') error.value = result.shipment.syncError || '微信发货同步失败，可重试同步'
    else success.value = result.shipment.syncStatus === 'SUCCESS' ? '微信发货信息已同步' : '发货信息已保存，同步结果待核实，请稍后刷新'
  } catch (e) { error.value = (e as Error).message }
  finally { syncBusy.value = '' }
}

const shippingOrder = ref<Order | null>(null), shippingBusy = ref(false), shippingError = ref('')
const shippingForm = ref({ carrier: '', carrierCode: '', trackingNo: '' }), carrierChoice = ref('SF')
const shippingDrawer = ref<HTMLElement | null>(null)
const carriers = [
  { code: 'SF', name: '顺丰速运' }, { code: 'JD', name: '京东物流' }, { code: 'ZTO', name: '中通快递' },
  { code: 'YTO', name: '圆通速递' }, { code: 'STO', name: '申通快递' }, { code: 'YD', name: '韵达快递' }, { code: 'EMS', name: '中国邮政 EMS' },
]
const shippingEditing = computed(() => !!shippingOrder.value?.shipment)
const shippingChanged = computed(() => {
  const old = shippingOrder.value?.shipment, form = shippingForm.value
  return !old || old.carrier !== form.carrier.trim() || old.carrierCode !== form.carrierCode.trim().toUpperCase() || old.trackingNo !== form.trackingNo.trim()
})
let shippingTrigger: HTMLElement | null = null
function chooseCarrier() {
  const carrier = carriers.find(c => c.code === carrierChoice.value)
  if (carrier) { shippingForm.value.carrier = carrier.name; shippingForm.value.carrierCode = carrier.code }
  else { shippingForm.value.carrier = ''; shippingForm.value.carrierCode = '' }
}
async function openShipping(order: Order) {
  if (operationBusy.value) return
  shippingTrigger = document.activeElement as HTMLElement | null
  shippingOrder.value = JSON.parse(JSON.stringify(order)) as Order; shippingError.value = ''
  const shipment = order.shipment
  shippingForm.value = shipment ? { carrier: shipment.carrier, carrierCode: shipment.carrierCode, trackingNo: shipment.trackingNo } : { carrier: '顺丰速运', carrierCode: 'SF', trackingNo: '' }
  carrierChoice.value = carriers.find(c => c.code === shippingForm.value.carrierCode && c.name === shippingForm.value.carrier)?.code || 'custom'
  await nextTick(); shippingDrawer.value?.querySelector<HTMLInputElement>('#shipment-tracking')?.focus()
}
function closeShipping() {
  if (shippingBusy.value) return
  shippingOrder.value = null; nextTick(() => shippingTrigger?.focus())
}
function shippingKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') { event.preventDefault(); closeShipping(); return }
  if (event.key !== 'Tab') return
  const elements = Array.from(shippingDrawer.value?.querySelectorAll<HTMLElement>('button:not(:disabled), input:not(:disabled), select:not(:disabled), [tabindex="0"]') || [])
  const first = elements[0], last = elements[elements.length - 1]
  if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus() }
  else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus() }
}
async function saveShipment() {
  const order = shippingOrder.value
  if (!order || shippingBusy.value || !shippingChanged.value) return
  shippingError.value = ''
  let values: { carrier: string; carrierCode: string; trackingNo: string }
  try { values = validateShipment(shippingForm.value) } catch (e) { shippingError.value = (e as Error).message; return }
  shippingBusy.value = true; error.value = ''; success.value = ''
  try {
    const old = order.shipment
    const result = await api<{ shipment: NonNullable<Order['shipment']> }>(old ? 'admin.order.updateShipment' : 'admin.order.ship', {
      orderId: order.id, ...values,
      ...(old ? { expectedShipment: { carrier: old.carrier, carrierCode: old.carrierCode, trackingNo: old.trackingNo } } : {}),
    })
    shippingBusy.value = false; closeShipping()
    success.value = old ? '发货信息已更新' : '订单已标记发货'
    if (result.shipment.syncStatus === 'PENDING') success.value += '，微信同步结果待核实，请稍后刷新'
    if (result.shipment.syncStatus === 'FAILED') error.value = `资料已保存，微信发货同步失败：${result.shipment.syncError || '请重试同步'}`
    await loadOrders()
  } catch (e) { shippingError.value = (e as Error).message }
  finally { shippingBusy.value = false }
}

const trajectory = ref<Tracking | null>(null), trackingOrder = ref<Order | null>(null), trackingBusy = ref(false), trackingError = ref('')
const progressNodes = computed(() => trackingOrder.value ? orderProgress(trackingOrder.value, trajectory.value) : [])
const trackingAlerts = computed(() => trackingOrder.value ? logisticsAlerts(trackingOrder.value, trajectory.value) : [])
let trackingRequest = 0
const trackingParcel = ref('')
async function track(order: Order, parcelKey = '') {
  trackingParcel.value = parcelKey
  const request = ++trackingRequest
  trackingOrder.value = order; trajectory.value = null; trackingBusy.value = true; trackingError.value = ''
  if (!order.shipment) { trackingBusy.value = false; return }
  try { const result = await api<Tracking>('admin.order.tracking', { orderId: order.id, carrierCode: parcelKey.split(':')[0] || '', trackingNo: parcelKey.split(':')[1] || '' }); if (request === trackingRequest) trajectory.value = result }
  catch (e) { if (request === trackingRequest) trackingError.value = (e as Error).message }
  finally { if (request === trackingRequest) trackingBusy.value = false }
}
function closeTracking() { trackingRequest++; trackingOrder.value = null }
async function refundAction(refund: Refund, action: string, data: Record<string, unknown> = {}) {
  if (operationBusy.value || refundBusy.value) return
  refundBusy.value = refund.id; error.value = ''
  try { await api(action, { refundId: refund.id, ...data }); await refresh() }
  catch (e) { error.value = (e as Error).message; await refresh() }
  finally { refundBusy.value = '' }
}
async function approve(refund: Refund) {
  if (!(await confirmAction({ title: '确认退款', message: `确认原路退回 ${money(refund.amount)}？处理中重试将复用原退款单号。`, danger: true, confirmText: '确认退款' }))) return
  await refundAction(refund, 'admin.refund.approve')
}
async function reject(refund: Refund) {
  const reason = prompt('请输入拒绝原因（必填，最多160字）')
  if (reason === null) return
  if (!reason.trim() || reason.trim().length > 160) { error.value = '拒绝原因须为1至160字'; return }
  await refundAction(refund, 'admin.refund.reject', { reason: reason.trim() })
}
const evidenceRefund = ref<Refund | null>(null), evidenceUrls = ref<string[]>([]), evidenceBusy = ref(false), evidenceError = ref('')
let evidenceRequest = 0
async function viewEvidence(refund: Refund) {
  const request = ++evidenceRequest
  evidenceRefund.value = refund; evidenceUrls.value = []; evidenceBusy.value = true; evidenceError.value = ''
  try {
    const results = await Promise.all((refund.images || []).map(id => api<{ dataUrl: string }>('admin.refund.evidence.get', { id })))
    if (request === evidenceRequest) evidenceUrls.value = results.map(x => x.dataUrl)
  } catch (e) { if (request === evidenceRequest) evidenceError.value = (e as Error).message }
  finally { if (request === evidenceRequest) evidenceBusy.value = false }
}
function closeEvidence() { evidenceRequest++; evidenceRefund.value = null }
</script>

<template>
  <LogisticsPanel v-if="workspace === 'logistics'" :orders="orders" @changed="loadOrders" />
  <div class="toolbar"><div><h2>订单与售后</h2><p>查找订单、管理发货与处理售后</p></div><button class="button secondary" :disabled="loading || refundsLoading || operationBusy" @click="refresh">刷新</button></div>
  <div class="segmented order-filters" aria-label="订单运营模块"><button v-for="item in [{ key: 'orders', label: '订单与售后' }, { key: 'picking', label: '拣货单' }, { key: 'logistics', label: '物流与异常任务' }, { key: 'exceptions', label: '异常订单工作台' }, { key: 'shipTimeout', label: '超时提醒' }]" :key="item.key" :class="{ active: workspace === item.key }" :aria-pressed="workspace === item.key" :disabled="operationBusy" @click="switchWorkspace(item.key)">{{ item.label }}</button></div>
  <p v-if="workspace === 'exceptions'" class="alert">集中处理微信发货同步失败、发货提醒失败或结果待核实、退款失败及已开票邮件异常。使用订单操作重试；退款与邮件结果待核实请先核对服务记录。</p>
  <div class="segmented order-filters" aria-label="订单状态筛选"><button v-for="item in visibleFilters" :key="item.key" :class="{ active: filter === item.key }" :aria-pressed="filter === item.key" :disabled="operationBusy" @click="filter = item.key">{{ item.label }}</button></div>
  <form class="order-search" @submit.prevent="search">
    <input v-model="keyword" :disabled="operationBusy" placeholder="订单号、商品、规格、收件人、手机或运单号" maxlength="80" aria-label="订单搜索" />
    <button v-permission="'order.read'" class="button primary" :disabled="operationBusy">搜索</button><button v-if="keyword || appliedKeyword" type="button" class="button secondary" :disabled="operationBusy" @click="clearSearch">清空</button>
  </form>
  <div class="saved-views" aria-label="已保存的筛选视图">
    <span class="saved-views-label">快捷筛选</span>
    <button v-for="(view, i) in savedViews" :key="view.name" type="button" class="chip" :title="`状态：${view.filter}；关键词：${view.keyword || '无'}`" @click="applySavedView(view)">{{ view.name }}<span class="chip-x" role="button" tabindex="0" aria-label="删除" @click.stop="deleteSavedView(i)" @keydown.enter.stop="deleteSavedView(i)">×</span></button>
    <button type="button" class="chip chip-add" @click="saveCurrentView">＋ 保存当前筛选</button>
  </div>
  <p v-if="appliedKeyword" class="order-search-caption">搜索「{{ appliedKeyword }}」 · {{ visibleFilters.find(f => f.key === filter)?.label }}</p>
  <p v-if="success" class="alert order-success" role="status">{{ success }}</p>
  <p v-if="error" class="alert error" role="alert">{{ error }}</p>
  <p v-if="listError" class="alert error" role="alert">订单加载失败：{{ listError }} <button class="text-button" @click="loadOrders">重试</button></p>
  <OrderOperations ref="operations" v-model:busy="operationBusy" :orders="orders" :selected-ids="selectedIds" :filter="filter" :keyword="appliedKeyword" :loading="loading || !!listError || shippingBusy || !!refundBusy || !!syncBusy" @changed="refresh"/>
  <OrderAlertPanel v-if="workspace === 'shipTimeout'" ref="alertPanel"/>
  <PickingSheet v-if="workspace === 'picking'" :selected-ids="selectedIds" :disabled="loading || operationBusy || !!listError" @update:busy="operationBusy = $event" />
  <OrderPackages v-if="packageOrder" :order="packageOrder" @close="packageOrderId = ''" @changed="loadOrders" @busy="operationBusy = $event"/>
  <section class="panel table-panel orders-table-panel" :aria-busy="loading">
    <table class="orders-table"><thead><tr><th class="order-checkbox"><input type="checkbox" aria-label="选择当前页订单" :checked="allSelected" :indeterminate="selectedIds.length > 0 && !allSelected" :disabled="loading || operationBusy || !orders.length" @change="toggleAll"/></th><th>订单 / 商品</th><th>收货人</th><th>金额</th><th>配送 / 发票</th><th>状态</th><th>物流</th><th>操作</th></tr></thead>
      <tbody>
        <tr v-if="loading"><td colspan="8" class="order-empty" role="status">正在加载订单…</td></tr>
        <tr v-else-if="!listError && !orders.length"><td colspan="8" class="order-empty">没有找到匹配的订单<span class="block">可调整关键词或切换状态筛选</span></td></tr>
        <template v-else-if="!listError"><tr v-for="order in orders" :key="order.id">
          <td class="order-checkbox"><input v-model="selectedIds" type="checkbox" :value="order.id" :aria-label="'选择订单 ' + order.orderNo" :disabled="loading || operationBusy"/></td>
          <td><strong>{{ order.orderNo }}</strong><small v-if="order.packages?.length" class="block">{{ order.packages.filter(p => p.shipment).length }}/{{ order.packages.length }} 包裹已发</small><small v-for="(item, index) in order.items" :key="index" class="block">{{ item.name }} · {{ item.skuName }} ×{{ item.quantity }}</small><small class="block">{{ dateTime(order.createdAt) }}</small></td>
          <td>{{ order.address.name }}<small class="block">{{ order.address.phone }}</small><small class="block">{{ order.address.province }} {{ order.address.city }} {{ order.address.district }}</small><small class="block">{{ order.address.detail }}</small></td>
          <td><strong>{{ money(order.quote.total) }}</strong><small v-if="order.refundedAmount" class="block">累计已退 {{ money(order.refundedAmount) }}</small><small v-if="order.quote.discount" class="block">券 {{ order.quote.couponCode }} −{{ money(order.quote.discount) }}</small></td>
          <td><small class="block">{{ order.deliveryDate || '尽快配送' }}</small><small v-if="order.deliveryDate" class="block subtle">期望送达日期</small>
            <template v-if="order.invoice"><strong class="block">{{ order.invoice.title }}</strong><small class="block">{{ order.invoice.kind === 'company' ? '企业 · ' + order.invoice.taxId : '个人' }}</small><small class="block">{{ order.invoice.email }}</small><small class="block">{{ money(order.invoice.amount) }} · {{ invoiceLabel(order.invoice.status) }}</small><small class="block">{{ order.invoice.number || order.invoice.reason }}</small>
              <InvoiceActions :order="order" :disabled="operationBusy" @changed="loadOrders" />
            </template><small v-else class="block">未申请发票</small>
          </td>
          <td class="order-state-cell"><button v-permission="'refund.read'" class="button small secondary" @click="ticketOrderId = ticketOrderId === order.id ? '' : order.id; ticketRefundId = ''; ticketOrderNo = order.orderNo">{{ ticketOrderId === order.id ? '查看全部工单' : '售后工单' }}</button><span class="status" :class="{ published: ['RECEIVED', 'COMPLETED'].includes(order.fulfillmentStatus) && order.refundStatus === 'NONE' }">{{ orderSummary(order) }}</span>
            <small v-for="issue in orderExceptions(order)" :key="issue" class="block sync-failed">{{ issue }}</small>
            <small class="block">订单：{{ orderStateLabel(order.orderStatus) }}</small><small class="block">支付：{{ paymentStateLabel(order.paymentStatus) }}</small><small class="block">履约：{{ fulfillmentStateLabel(order.fulfillmentStatus) }}</small><small class="block">退款：{{ refundStateLabel(order.refundStatus) }}</small>
          </td>
          <td><template v-if="order.shipment"><span>{{ order.shipment.carrier }} · {{ order.shipment.carrierCode }}</span><small class="block order-tracking-number">{{ order.shipment.trackingNo }}</small><small v-if="order.shipment.shippedAt" class="block">发货 {{ dateTime(order.shipment.shippedAt) }}</small><small class="block" :class="{ 'sync-failed': order.shipment.syncStatus === 'FAILED' }">{{ syncLabel(order.shipment.syncStatus) }}</small><small v-if="order.shipment.syncError" class="block sync-failed">{{ order.shipment.syncError }}</small><small class="block">{{ noticeLabel(order.shippingNoticeStatus) }}</small><small v-if="order.shippingNoticeError" class="block sync-failed">{{ order.shippingNoticeError }}</small></template><span v-else class="subtle">未录入发货信息</span></td>
          <td><div class="order-actions"><OrderBatches :order="order" :disabled="operationBusy" @changed="loadOrders" /><button class="button small secondary" :disabled="operationBusy || loading" @click="operations?.openLabel(order)">电子面单</button><button v-permission="'order.ship'" v-if="canShip(order) && !order.packages?.length" class="button small primary" :disabled="operationBusy" @click="openShipping(order)">录入发货</button><button v-permission="'order.ship'" v-if="canEditShipment(order) && !order.packages?.length" class="button small secondary" :disabled="!!syncBusy || operationBusy" @click="openShipping(order)">编辑发货信息</button><button class="button small secondary" :disabled="operationBusy" @click="packageOrderId = order.id">拆单 / 包裹 / 事件</button><button class="button small secondary" @click="track(order)">订单进度</button><button v-if="order.shipment" class="button small secondary" @click="track(order)">快递轨迹</button><button v-permission="'order.ship'" v-if="order.shipment && ['READY', 'FAILED'].includes(order.shippingNoticeStatus || '')" class="button small secondary" :disabled="!!noticeBusy || operationBusy" @click="retryNotice(order)">{{ noticeBusy === order.id ? '正在发送…' : '重试发货通知' }}</button><button v-permission="'order.ship'" v-if="order.shipment && ['PENDING', 'FAILED'].includes(order.shipment.syncStatus)" class="button small secondary" :disabled="!!syncBusy || operationBusy" @click="order.packages?.length ? packageOrderId = order.id : resync(order)">{{ syncBusy === order.id ? '正在同步…' : '重试同步' }}</button></div></td>
        </tr></template>
      </tbody>
    </table>
  </section>
  <nav class="order-pagination" aria-label="订单分页">
    <span aria-live="polite">{{ loading ? '正在加载…' : listError ? '加载失败，请重试' : `共 ${total} 笔 · 当前 ${range} 笔` }}</span>
    <div><label>每页 <select v-model.number="pageSize" :disabled="operationBusy" aria-label="每页订单数"><option :value="20">20 笔</option><option :value="50">50 笔</option><option :value="100">100 笔</option></select></label><button class="button small secondary" :disabled="loading || operationBusy || currentPage <= 1" @click="goPage(currentPage - 1)">上一页</button><span>第 {{ currentPage }} / {{ pages }} 页</span><button class="button small secondary" :disabled="loading || operationBusy || currentPage >= pages" @click="goPage(currentPage + 1)">下一页</button></div>
  </nav>
  <section v-if="can(currentAdmin(), 'refund.read')" class="panel refund-panel">
    <div class="panel-head"><div><h3>退款申请</h3><p>全部售后申请 · 查看审核原因与微信退款结果</p></div><button class="button small secondary" :disabled="!!refundBusy || refundsLoading" @click="loadRefunds">刷新售后</button><span class="count">{{ refunds.filter(r => r.status === 'APPLIED').length }} 待处理</span></div>
    <p v-if="refundError" class="alert error" role="alert">售后加载失败：{{ refundError }} <button class="text-button" @click="loadRefunds">重试</button></p>
    <p v-if="refundsLoading" class="order-empty">正在加载售后申请…</p><p v-else-if="!refunds.length && !refundError" class="order-empty">暂无退款申请</p>
    <div v-for="refund in refunds" :key="refund.id" class="refund-row">
      <div><strong>{{ refund.orderNo }}</strong><small>{{ dateTime(refund.createdAt) }}</small></div>
      <div><strong>{{ refund.reason }}</strong><small>{{ refund.note || '无补充说明' }}</small><small v-if="refund.rejectReason" class="sync-failed">拒绝原因：{{ refund.rejectReason }}</small><small v-if="refund.status === 'FAILED'" class="sync-failed">微信退款异常或关闭，需在商户平台核实；未确认到账。</small><small>退款单号：{{ refund.id }}</small><small v-if="refund.providerRefundId">微信退款单号：{{ refund.providerRefundId }}</small><small v-if="refund.status === 'SUCCEEDED'">微信已确认退款成功；到账核对请结合原支付账户凭证。</small><small v-if="refund.updatedAt">状态更新：{{ dateTime(refund.updatedAt) }}</small><small>{{ ['SHIPPED', 'RECEIVED', 'COMPLETED'].includes(refund.fulfillmentStatus || orders.find(o => o.id === refund.orderId)?.fulfillmentStatus || '') ? '已发货售后' : '未发货退款' }}</small><button v-if="refund.images?.length" class="button small secondary" @click="viewEvidence(refund)">查看举证（{{ refund.images.length }}）</button></div>
      <div><b>{{ money(refund.amount) }}</b><small v-if="refund.orderTotal">{{ refund.amount < refund.orderTotal ? '部分金额退款' : '整单金额退款' }}</small></div><span class="status">{{ refundStateLabel(refund.status) }}</span>
      <div class="row-actions"><button class="button small secondary" @click="ticketOrderId = refund.orderId; ticketRefundId = refund.id; ticketOrderNo = refund.orderNo">售后工单</button><button v-permission="'refund.approve'" v-if="refund.status === 'APPLIED'" class="button small secondary" :disabled="!!refundBusy || operationBusy" @click="reject(refund)">拒绝</button><button v-permission="'refund.approve'" v-if="refund.status === 'APPLIED'" class="button small primary" :disabled="!!refundBusy || operationBusy" @click="approve(refund)">同意退款</button><button v-permission="'refund.approve'" v-if="['APPROVED', 'FAILED'].includes(refund.status)" class="button small secondary" :disabled="!!refundBusy || operationBusy" @click="refundAction(refund, 'admin.refund.sync')">查询微信进度</button><button v-permission="'refund.approve'" v-if="refund.status === 'APPROVED'" class="button small secondary" :disabled="!!refundBusy || operationBusy" @click="approve(refund)">按原单重试</button></div>
    </div>
  </section>

  <AfterSalesPanel :orders="orders" :order-id="ticketOrderId" :order-no="ticketOrderNo" :refund-id="ticketRefundId" />

  <div v-if="shippingOrder" class="modal-backdrop" @click.self="closeShipping" @keydown="shippingKeydown">
    <form ref="shippingDrawer" class="drawer shipping-drawer" role="dialog" aria-modal="true" aria-labelledby="shipping-title" @submit.prevent="saveShipment">
      <div class="drawer-head"><h2 id="shipping-title">{{ shippingEditing ? '编辑发货信息' : '录入发货' }}</h2><button type="button" class="close" aria-label="关闭发货编辑" :disabled="shippingBusy" @click="closeShipping">×</button></div>
      <div class="drawer-body">
        <div class="shipment-summary"><strong>{{ shippingOrder.orderNo }}</strong><p>{{ shippingOrder.items.map(i => `${i.name} · ${i.skuName} ×${i.quantity}`).join('、') }}</p><p>{{ shippingOrder.address.name }} · {{ shippingOrder.address.phone }}</p><p>{{ shippingOrder.address.province }} {{ shippingOrder.address.city }} {{ shippingOrder.address.district }} {{ shippingOrder.address.detail }}</p></div>
        <p class="shipment-hint">{{ shippingEditing ? '更正承运商或运单号后会重新同步微信发货信息，请核对真实运单。' : '请核对收货地址及运单，确认后订单将进入待收货状态，并同步微信发货信息。' }}</p>
        <fieldset class="shipment-fields" :disabled="shippingBusy"><div class="form-grid">
          <label class="full">选择承运商<select v-model="carrierChoice" aria-label="选择承运商" @change="chooseCarrier"><option v-for="carrier in carriers" :key="carrier.code" :value="carrier.code">{{ carrier.name }}</option><option value="custom">其他承运商（手动填写）</option></select></label>
          <label>承运商名称<input v-model="shippingForm.carrier" maxlength="40" required aria-label="承运商名称" :readonly="carrierChoice !== 'custom'" placeholder="例如：顺丰速运" /></label>
          <label>快递公司编码<input v-model="shippingForm.carrierCode" maxlength="20" required aria-label="快递公司编码" :readonly="carrierChoice !== 'custom'" placeholder="例如：SF" /><small>按微信支持的快递公司编码填写</small></label>
          <label class="full" for="shipment-tracking">运单号<input id="shipment-tracking" v-model="shippingForm.trackingNo" maxlength="50" required autocomplete="off" spellcheck="false" aria-label="运单号" placeholder="请输入快递运单号" /><small>4–50位，支持英文字母、数字、下划线和连字符</small></label>
        </div></fieldset>
        <p v-if="shippingEditing && !shippingChanged" class="shipment-hint">资料尚未修改</p><p v-if="shippingError" class="alert error" role="alert">{{ shippingError }}</p>
      </div>
      <div class="drawer-foot"><button type="button" class="button secondary" :disabled="shippingBusy" @click="closeShipping">取消</button><button v-permission="'order.ship'" class="button primary" :disabled="shippingBusy || !shippingChanged">{{ shippingBusy ? '正在保存并同步…' : shippingEditing ? '保存更正并同步' : '确认发货' }}</button></div>
    </form>
  </div>
  <div v-if="trackingOrder" class="modal-backdrop" @click.self="closeTracking"><section class="drawer" role="dialog" aria-modal="true" aria-label="订单进度与物流" @keydown.esc="closeTracking"><div class="drawer-head"><h2>订单进度时间线</h2><button class="close" aria-label="关闭订单进度" @click="closeTracking">×</button></div><div class="drawer-body">
    <p>{{ trackingOrder.orderNo }}</p><select v-if="trackingOrder.shipment?.parcels?.length" :value="trackingParcel" :disabled="trackingBusy" aria-label="选择包裹物流" @change="track(trackingOrder, ($event.target as HTMLSelectElement).value)"><option value="">主运单</option><option v-for="p in trackingOrder.shipment.parcels" :key="`${p.carrierCode}:${p.trackingNo}`" :value="`${p.carrierCode}:${p.trackingNo}`">{{ p.carrier }} · {{ p.trackingNo }}</option></select><p v-if="trackingOrder.shipment">{{ trackingOrder.shipment.carrier }} · {{ trackingOrder.shipment.trackingNo }}</p>
    <h3 v-if="trackingAlerts.length">物流异常提醒</h3><p v-for="alert in trackingAlerts" :key="alert" class="alert error" role="alert">{{ alert }}</p>
    <p v-if="trackingBusy" role="status">正在查询物流…</p><p v-if="trackingError" class="alert error">{{ trackingError }} <button class="text-button" @click="track(trackingOrder, trackingParcel)">重试</button></p>
    <template v-if="trajectory"><p>{{ trajectory.message }}</p><small>查询时间：{{ dateTime(trajectory.updatedAt) }}（半小时内复用查询结果）</small><p v-if="!trajectory.events.length">暂无轨迹节点</p></template>
    <ol class="order-progress-timeline"><li v-for="node in progressNodes" :key="node.key" :class="{ pending: node.pending }"><strong>{{ node.label }}</strong><small>{{ node.time ? dateTime(node.time) : node.pending ? '待完成' : '时间未记录' }}</small><p v-if="node.detail">{{ node.detail }}</p></li></ol>
    <button v-if="trackingOrder.shipment" class="button secondary" :disabled="trackingBusy" @click="track(trackingOrder, trackingParcel)">刷新轨迹</button>
  </div></section></div>
  <div v-if="evidenceRefund" class="modal-backdrop" @click.self="closeEvidence"><section class="drawer" role="dialog" aria-modal="true" aria-label="售后举证"><div class="drawer-head"><h2>售后举证 · {{ evidenceRefund.orderNo }}</h2><button class="close" aria-label="关闭售后举证" @click="closeEvidence">×</button></div><div class="drawer-body"><p>{{ evidenceRefund.reason }} · 申请 {{ money(evidenceRefund.amount) }}</p><p v-if="evidenceBusy">正在加载图片…</p><p v-if="evidenceError" class="alert error">{{ evidenceError }} <button class="text-button" @click="viewEvidence(evidenceRefund)">重试</button></p><img v-for="(url, index) in evidenceUrls" :key="index" :src="url" :alt="'举证图片' + (index + 1)" class="evidence-image" /></div></section></div>
</template>

