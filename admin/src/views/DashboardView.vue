<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { api, isDemo, money } from '../api'
import { beijingDate, dashboardRange, shiftDate } from '../dashboard'
import { can, currentAdmin } from '../permissions'
import { downloadCSV } from '../order-operations'
import type { Dashboard, Lead, LeadListResult, Order } from '../types'

const today = beijingDate()
const startDate = ref(shiftDate(today, -29))
const endDate = ref(today)
const preset = ref('30')
const stats = ref<Dashboard | null>(null)
const orders = ref<Order[]>([])
const leads = ref<Lead[]>([])
const loading = ref(false)
const error = ref('')
const recentError = ref('')
const showOrders = can(currentAdmin(), 'order.read')
const showLeads = can(currentAdmin(), 'lead.read')
let requestId = 0
onUnmounted(() => { requestId++ })

async function load() {
  const id = ++requestId
  error.value = ''
  try {
    const range = dashboardRange({ startDate: startDate.value, endDate: endDate.value })
    loading.value = true
    const result = await api<Dashboard>('admin.dashboard', range)
    if (id === requestId) stats.value = result
  } catch (e) {
    if (id === requestId) { stats.value = null; error.value = e instanceof Error ? e.message : '概览加载失败' }
  } finally { if (id === requestId) loading.value = false }
}
function choosePreset(value: string) {
  preset.value = value
  endDate.value = beijingDate()
  startDate.value = value === 'month' ? `${endDate.value.slice(0, 7)}-01` : shiftDate(endDate.value, -(Number(value) - 1))
  void load()
}
const exporting = ref(false)
const exportError = ref('')
async function exportReport() {
  if (exporting.value) return
  exporting.value = true; exportError.value = ''
  try {
    const result = await api<{ filename: string; content: string }>('admin.dashboard.export', dashboardRange({ startDate: startDate.value, endDate: endDate.value }))
    downloadCSV(result)
  } catch (e) { exportError.value = e instanceof Error ? e.message : '导出失败' }
  finally { exporting.value = false }
}
async function loadRecent() {
  recentError.value = ''
  const results = await Promise.allSettled([
    showOrders ? api<Order[]>('admin.order.list', { limit: 5 }) : Promise.resolve([] as Order[]),
    showLeads ? api<LeadListResult>('admin.lead.list', { limit: 4 }) : Promise.resolve({ items: [] as Lead[] }),
  ])
  if (results[0].status === 'fulfilled') orders.value = results[0].value
  if (results[1].status === 'fulfilled') leads.value = results[1].value.items
  if (results.some(result => result.status === 'rejected')) recentError.value = '最近订单或合作线索加载失败，请重试。'
}
onMounted(() => { void load(); void loadRecent() })

const maxRevenue = computed(() => Math.max(1, ...stats.value?.trend.map(day => Math.abs(day.netRevenue)) || []))
const points = computed(() => stats.value?.trend.map((day, i, days) => `${days.length === 1 ? 450 : 30 + i / (days.length - 1) * 840},${150 - day.netRevenue / maxRevenue.value * 115}`).join(' ') || '')
const stages = computed(() => stats.value ? [
  { label: '提交订单', count: stats.value.funnel.created, previous: stats.value.funnel.created },
  { label: '完成支付', count: stats.value.funnel.paid, previous: stats.value.funnel.created },
  { label: '确认收货', count: stats.value.funnel.received, previous: stats.value.funnel.paid },
] : [])
const behaviorStages = computed(() => {
  const f = stats.value?.behaviorFunnel
  return f ? [
    { label: '访问小程序', count: f.visitors, previous: f.visitors },
    { label: '加入购物袋', count: f.cartUsers, previous: f.visitors },
    { label: '提交订单', count: f.createdUsers, previous: f.cartUsers },
    { label: '完成支付', count: f.paidUsers, previous: f.createdUsers },
    { label: '确认收货', count: f.receivedUsers, previous: f.paidUsers },
  ] : []
})
const coverageDate = computed(() => {
  const value = stats.value?.behaviorFunnel?.startedAt
  return value ? beijingDate(new Date(value)) : ''
})
const percentage = (count: number, total: number) => total ? `${(count / total * 100).toFixed(1)}%` : '—'
const deltaText = (current: number, previous: number) => {
  if (!previous) return current ? '上一周期无数据' : '—'
  const pct = (current - previous) / Math.abs(previous) * 100
  return `较上一周期 ${pct >= 0 ? '+' : ''}${pct.toFixed(1)}%`
}
const orderStatus = (order: Order) => order.refundStatus === 'APPLIED' ? '退款审核' : order.paymentStatus === 'REFUNDED' ? '已退款' : order.fulfillmentStatus === 'RECEIVED' ? '已收货' : order.fulfillmentStatus === 'SHIPPED' ? '运输中' : order.paymentStatus === 'PAID' ? '待发货' : ['CLOSED', 'CANCELLED'].includes(order.orderStatus) ? '已关闭' : '待付款'
</script>

<template>
  <div class="welcome-card">
    <div><p class="eyebrow">TODAY AT THE ORCHARD</p><h2>今天，也把每一串葡萄照顾好。</h2><p>从收入、购买习惯到订单转化，掌握经营变化。</p></div>
    <div class="welcome-grapes"><i></i><i></i><i></i><i></i></div>
  </div>
  <section class="panel overview-filters">
    <div class="overview-presets"><strong>统计日期</strong><button v-for="option in [{ value: '1', label: '今天' }, { value: '7', label: '近7天' }, { value: '30', label: '近30天' }, { value: 'month', label: '本月' }]" :key="option.value" :class="{ selected: preset === option.value }" :disabled="loading" @click="choosePreset(option.value)">{{ option.label }}</button></div>
    <form @submit.prevent="load"><label>开始日期<input v-model="startDate" type="date" :max="today" required :disabled="loading" @input="preset = 'custom'" /></label><span>至</span><label>结束日期<input v-model="endDate" type="date" :max="today" required :disabled="loading" @input="preset = 'custom'" /></label><button class="primary" :disabled="loading">{{ loading ? '加载中…' : '查询' }}</button><button type="button" class="secondary" :disabled="loading || exporting" @click="exportReport">{{ exporting ? '导出中…' : '导出报表' }}</button></form>
    <p v-if="exportError" class="form-error" role="alert">{{ exportError }}</p>
    <p>按北京时间下单日期统计，含首尾两天；最多366天。{{ isDemo ? '当前为演示数据。' : '' }}</p>
  </section>
  <div v-if="error" class="panel overview-error" role="alert">{{ error }} <button @click="load">重新加载</button></div>
  <p v-if="loading" class="overview-loading" role="status">正在更新经营数据…</p>
  <div v-if="stats" :aria-busy="loading" :class="{ 'overview-updating': loading }">
    <p class="overview-range">统计范围：{{ stats.startDate }} 至 {{ stats.endDate }} · 订单当前状态</p>
    <div class="metric-grid overview-metrics">
      <article class="metric"><span>退款后净收入</span><strong>{{ money(stats.revenue) }}</strong><small>实付 {{ money(stats.grossRevenue) }} − 成功退款 {{ money(stats.refundedAmount) }}</small><small class="delta">{{ deltaText(stats.revenue, stats.previous.netRevenue) }}</small></article>
      <article class="metric"><span>已支付订单</span><strong>{{ stats.paidOrders }}</strong><small>含退款中、部分退款和全额退款订单</small><small class="delta">{{ deltaText(stats.paidOrders, stats.previous.paidOrders) }}</small></article>
      <article class="metric"><span>期间复购率</span><strong>{{ stats.buyers ? stats.repurchaseRate.toFixed(1) + '%' : '—' }}</strong><small>{{ stats.repeatBuyers }} 位复购买家 / {{ stats.buyers }} 位有效买家</small></article>
    </div>
    <p class="overview-note">净收入为所选期间下单订单的实付总额（含运费）减截至当前已成功退款；待审核、处理中及失败退款不扣减。此数值不扣除成本，不代表利润或期间现金流水。</p>
    <section class="panel overview-trend">
      <div class="panel-head"><div><h3>销售趋势</h3><p>按下单日期查看每日净收入</p></div><strong>{{ money(stats.revenue) }}</strong></div>
      <div v-if="!stats.paidOrders" class="overview-empty">该期间暂无已支付订单。</div>
      <template v-else>
        <div class="overview-chart"><span>{{ money(maxRevenue) }}</span><svg viewBox="0 0 900 290" role="img" aria-label="每日退款后净收入趋势，具体金额可展开每日明细查看"><line x1="30" y1="150" x2="870" y2="150" class="chart-baseline" /><polyline :points="points" class="chart-line" /><circle v-if="stats.trend.length === 1" cx="450" :cy="150 - stats.trend[0]!.netRevenue / maxRevenue * 115" r="5" class="chart-dot" /></svg><div class="chart-dates"><span>{{ stats.startDate }}</span><span>{{ stats.endDate }}</span></div></div>
      </template>
      <details><summary>查看每日明细（{{ stats.trend.length }}天）</summary><div class="overview-table"><table><thead><tr><th>下单日期</th><th>已支付订单</th><th>实付金额</th><th>成功退款</th><th>净收入</th></tr></thead><tbody><tr v-for="day in stats.trend" :key="day.date"><td>{{ day.date }}</td><td>{{ day.paidOrders }}</td><td>{{ money(day.grossRevenue) }}</td><td>{{ money(day.refundedAmount) }}</td><td>{{ money(day.netRevenue) }}</td></tr></tbody></table></div></details>
    </section>
    <section class="panel overview-behavior" aria-label="访问与加购转化漏斗">
      <div class="panel-head"><div><h3>访问与加购转化漏斗</h3><p>按用户去重，查看访问后的购物袋购买转化</p></div><span v-if="isDemo" class="status">演示数据</span></div>
      <template v-if="stats.behaviorFunnel">
        <p v-if="stats.behaviorFunnel.partial" class="overview-note behavior-coverage" role="status">{{ coverageDate ? `采集开始于北京时间 ${coverageDate}，所选期间含未采集时段。` : '暂无访问及加购采集记录。' }}历史数据不补算；以下仅展示已采集数据。</p>
        <p v-if="!stats.behaviorFunnel.visitors" class="overview-empty">该期间暂无已采集的访问用户。</p>
        <div v-for="(stage, i) in behaviorStages" :key="stage.label" class="behavior-stage funnel-stage"><div><span>{{ stage.label }}</span><strong>{{ stage.count }} 人</strong><small>{{ i === 0 ? '起点' : '上一步转化 ' + percentage(stage.count, stage.previous) }}</small></div><div class="funnel-track"><i :style="{ width: stats.behaviorFunnel.visitors ? (stage.count / stats.behaviorFunnel.visitors * 100) + '%' : '0%' }"></i></div></div>
        <p class="overview-note">按登录用户在所选期间访问、成功加购后提交的订单逐步统计，每人每阶段只计一次；支付与收货查看这些订单的当前状态，已退款仍计入曾支付。直接购买及加购前订单不计入此漏斗，仍计入下方订单转化。重复访问、加购不增加人数；购物袋勾选、删除和数量编辑不计为加购。</p>
      </template>
      <p v-else class="overview-empty">访问及加购统计暂不可用，请更新统计服务。</p>
    </section>
    <div class="dashboard-columns overview-analysis">
      <section class="panel"><div class="panel-head"><div><h3>复购率</h3><p>期间至少购买两次的有效买家占比</p></div></div><div class="repeat-rate"><strong>{{ stats.buyers ? stats.repurchaseRate.toFixed(1) + '%' : '—' }}</strong><span>{{ stats.buyers ? `${stats.repeatBuyers} 位复购买家，共 ${stats.buyers} 位有效买家` : '该期间暂无有效买家' }}</span></div><p class="overview-note">按用户去重；所选期间内有已付款且剩余实付金额大于0的订单视为有效购买，至少2单视为复购。全额退款订单排除，部分退款订单保留。</p></section>
      <section class="panel"><div class="panel-head"><div><h3>订单转化漏斗</h3><p>所选期间全部下单订单的当前进度，含直接购买</p></div></div><div v-for="(stage, i) in stages" :key="stage.label" class="funnel-stage order-funnel-stage"><div><span>{{ stage.label }}</span><strong>{{ stage.count }} 单</strong><small>{{ i === 0 ? '起点' : '上一步转化 ' + percentage(stage.count, stage.previous) }}</small></div><div class="funnel-track"><i :style="{ width: stats.funnel.created ? (stage.count / stats.funnel.created * 100) + '%' : '0%' }"></i></div></div><p class="overview-note">支付率 {{ percentage(stats.funnel.paid, stats.funnel.created) }}。已退款订单仍计入曾完成支付；收货以订单确认收货状态为准。此处按订单计数，与上方按人数统计的漏斗分别计算。</p></section>
    </div>
    <div class="metric-grid overview-backlog"><article class="metric attention"><span>待发货</span><strong>{{ stats.pendingShipment }}</strong><small>当前全部待办，不受日期筛选影响</small></article><article class="metric warning"><span>待审核退款</span><strong>{{ stats.pendingRefund }}</strong><small>当前全部待办，不受日期筛选影响</small></article><article class="metric"><span>新合作线索</span><strong>{{ stats.newLeads }}</strong><small>当前全部待办，不受日期筛选影响</small></article></div>
  </div>
  <div v-if="recentError" class="overview-error" role="alert">{{ recentError }} <button @click="loadRecent">重试</button></div>
  <div v-if="showOrders || showLeads" class="dashboard-columns">
    <section v-if="showOrders" class="panel"><div class="panel-head"><div><h3>最近订单</h3><p>最新订单，不受日期筛选影响</p></div></div><p v-if="!orders.length" class="overview-empty">暂无订单</p><div class="mini-order" v-for="order in orders.slice(0, 5)" :key="order.id"><img :src="order.items[0]?.cover" alt="商品"/><div><strong>{{ order.items[0]?.name }}</strong><small>{{ order.orderNo }} · {{ order.address.city }}</small></div><b>{{ money(order.quote.total) }}</b><span class="status">{{ orderStatus(order) }}</span></div></section>
    <section v-if="showLeads" class="panel"><div class="panel-head"><div><h3>渠道机会</h3><p>最新批发与团购需求，不受日期筛选影响</p></div></div><p v-if="!leads.length" class="overview-empty">暂无合作线索</p><div class="mini-lead" v-for="lead in leads.slice(0, 4)" :key="lead.id"><span>{{ lead.organization.slice(0,1) }}</span><div><strong>{{ lead.organization }}</strong><small>{{ lead.channel }} · {{ lead.volume }}</small></div><em :class="lead.status.toLowerCase()">{{ lead.status === 'NEW' ? '新线索' : ['CONTACTED', 'QUALIFIED'].includes(lead.status) ? '跟进中' : lead.status === 'WON' ? '已合作' : '已关闭' }}</em></div></section>
  </div>
</template>

<style scoped>
.overview-filters { margin: 20px 0; padding: 20px; }
.overview-presets, .overview-filters form { display: flex; align-items: center; flex-wrap: wrap; gap: 10px; }
.overview-presets { margin-bottom: 14px; }
.overview-presets strong { margin-right: 12px; }
.overview-filters button, .overview-error button { border: 1px solid var(--border); border-radius: 8px; background: var(--paper); padding: 8px 14px; }
.overview-filters button.selected, .overview-filters button.primary { background: var(--purple); color: white; border-color: var(--purple); }
.overview-filters label { display: flex; align-items: center; gap: 8px; font-size: 12px; }
.overview-filters input { border: 1px solid var(--border); border-radius: 8px; background: white; padding: 8px; }
.overview-filters p, .overview-note, .overview-range { color: var(--muted); font-size: 12px; line-height: 1.8; }
.overview-filters p { margin-top: 12px; }
.overview-range { margin-bottom: 12px; }
.overview-metrics, .overview-backlog { grid-template-columns: repeat(3, minmax(0, 1fr)); }
.overview-metrics .metric small { line-height: 1.6; }
.overview-metrics .metric small.delta { color: var(--purple); }
.overview-note { margin: 14px 0; }
.overview-updating { opacity: .5; pointer-events: none; }
.overview-error { color: var(--danger); margin: 16px 0; }
.overview-loading { color: var(--purple); margin: 12px 0; }
.overview-trend { margin-top: 20px; }
.overview-chart { position: relative; }
.overview-chart > span { position: absolute; top: 4px; color: var(--muted); font-size: 11px; }
.overview-chart svg { display: block; width: 100%; max-height: 300px; }
.chart-baseline { stroke: var(--border); stroke-dasharray: 5 5; }
.chart-line { stroke: var(--purple); stroke-width: 3; fill: none; vector-effect: non-scaling-stroke; stroke-linecap: round; stroke-linejoin: round; }
.chart-dot { fill: var(--purple); }
.chart-dates { display: flex; justify-content: space-between; color: var(--muted); font-size: 11px; }
.overview-trend details { margin-top: 18px; font-size: 12px; }
.overview-trend summary { cursor: pointer; color: var(--purple); }
.overview-table { max-height: 340px; overflow: auto; margin-top: 12px; }
.overview-table table { width: 100%; }
.overview-table th { position: sticky; top: 0; background: var(--paper); }
.overview-empty { padding: 30px 0; color: var(--muted); text-align: center; }
.overview-analysis { grid-template-columns: 1fr 1fr; }
.overview-behavior { margin-top: 20px; }
.overview-behavior > .funnel-stage, .overview-behavior > .overview-note { margin-left: 24px; margin-right: 24px; }
.behavior-coverage { padding: 10px 14px; background: var(--purple-soft); border-radius: 8px; }
.overview-analysis .repeat-rate, .overview-analysis > .panel > .overview-note, .overview-analysis .funnel-stage { margin-left: 24px; margin-right: 24px; }
.repeat-rate { display: flex; flex-direction: column; gap: 12px; padding: 18px 0; }
.repeat-rate strong { font-size: 46px; color: var(--purple); }
.repeat-rate span { color: var(--muted); font-size: 13px; }
.funnel-stage { margin: 16px 0; }
.funnel-stage > div:first-child { display: flex; align-items: center; gap: 14px; font-size: 13px; }
.funnel-stage small { margin-left: auto; color: var(--muted); }
.funnel-track { background: var(--purple-soft); height: 10px; border-radius: 6px; margin-top: 9px; overflow: hidden; }
.funnel-track i { display: block; height: 100%; background: var(--purple); border-radius: inherit; }
.overview-backlog { margin-top: 20px; }
button:disabled { cursor: wait; opacity: .55; }
@media (max-width: 1000px) { .overview-analysis, .overview-metrics, .overview-backlog { grid-template-columns: 1fr; } }
</style>
