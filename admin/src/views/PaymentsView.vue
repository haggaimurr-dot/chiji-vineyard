<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api, isDemo } from '../api'

type Run = { billDate: string; status: string; checked?: number; differences?: number; finishedAt?: string; message?: string }
type FinanceRun = Run & { kind: string; totals?: Record<string, number>; details?: { refundNo: string; message: string }[]; emptyBill?: boolean }
type Alert = { id: string; kind: string; target: string; message: string; status: string; occurrences: number; lastSeen: string; note: string }
const data = ref<{ runs: Run[]; alerts: Alert[]; openAlerts: number; financeRuns?: FinanceRun[] }>({ runs: [], alerts: [], openAlerts: 0 })
const yesterday = new Date(Date.now() - 86400000).toLocaleDateString('en-CA', { timeZone: 'Asia/Shanghai' })
const billDate = ref(yesterday), busy = ref(false), error = ref(''), message = ref('')
const financeKind = ref('REFUND'), financeFrom = ref(''), financeTo = ref('')
const financeLabels: Record<string, string> = { REFUND: '退款对账', BASIC: '基本账户', OPERATION: '运营账户', FEES: '手续费账户' }
const financeRuns = computed(() => (data.value.financeRuns || []).filter(run => run.kind === financeKind.value && (!financeFrom.value || run.billDate >= financeFrom.value) && (!financeTo.value || run.billDate <= financeTo.value)))
const financeSummary = computed(() => {
  const total: Record<string, number> = {}
  for (const run of financeRuns.value) if (run.status === 'MATCHED' || run.status === 'DIFFERENCES' || run.status === 'COMPLETED') for (const [key, value] of Object.entries(run.totals || {})) total[key] = (total[key] || 0) + value
  return total
})
const money = (value?: number) => value === undefined ? '—' : `¥${(value / 100).toFixed(2)}`
async function reconcileFinance() {
  if (busy.value) return
  busy.value = true; error.value = ''; message.value = ''
  try { const run = await api<FinanceRun>('admin.payment.financeReconcile', { billDate: billDate.value, kind: financeKind.value }); message.value = `${run.billDate} · ${financeLabels[run.kind]}：${labels[run.status] || run.status}${run.message ? ' · ' + run.message : ''}`; await refresh() }
  catch (e) { error.value = (e as Error).message } finally { busy.value = false }
}
const notes = ref<Record<string, string>>({})
type Thresholds = { refundStuckHours: number; invoiceStuckHours: number; openAlertCount: number; reminderFailureCount: number }
const thresholds = ref<Thresholds>({ refundStuckHours: 24, invoiceStuckHours: 48, openAlertCount: 50, reminderFailureCount: 100 })
const thresholdBusy = ref(false), thresholdMsg = ref('')
async function loadThresholds() { try { thresholds.value = await api<Thresholds>('admin.alert.thresholds', {}) } catch {} }
async function saveThresholds() {
  if (thresholdBusy.value) return
  thresholdBusy.value = true; thresholdMsg.value = ''
  try { await api('admin.alert.thresholds.save', thresholds.value); thresholdMsg.value = '告警阈值已保存，定时扫描将按新阈值执行' }
  catch (e) { thresholdMsg.value = (e as Error).message }
  finally { thresholdBusy.value = false }
}
const labels: Record<string, string> = { COMPLETED: '统计完成', REFUND_BILL_DIFFERENCE: '退款账单差异', FINANCE_RECONCILIATION_FAILED: '退款 / 资金账单失败', RUNNING: '对账中', MATCHED: '一致', DIFFERENCES: '存在差异', FAILED: '失败', OPEN: '待处理', RESOLVED: '已处理', QUERY_FAILED: '查单失败', CALLBACK_ORDER_MISSING: '回调订单异常', BILL_DIFFERENCE: '账单差异', RECONCILIATION_FAILED: '对账失败', PREPAY_FAILED: '下单失败', CALLBACK_INVALID: '回调校验失败', PAYMENT_MISMATCH: '支付结果异常', PAYMENT_RECORD_FAILED: '入账失败', THRESHOLD_REFUND_STUCK: '退款超时', THRESHOLD_INVOICE_STUCK: '发票超时', THRESHOLD_ALERT_BACKLOG: '告警积压', THRESHOLD_REMINDER_FAILURES: '提醒失败积压' }
async function refresh() { data.value = await api('admin.payment.overview') }
async function load() { if (busy.value) return; busy.value = true; error.value = ''; try { await refresh() } catch (e) { error.value = (e as Error).message } finally { busy.value = false } }
async function reconcile() {
  if (busy.value) return
  busy.value = true; error.value = ''; message.value = ''
  try { const run = await api<Run>('admin.payment.reconcile', { billDate: billDate.value }); message.value = `${run.billDate}：${labels[run.status] || run.status}${run.message ? ' · ' + run.message : ''}`; await refresh() }
  catch (e) { error.value = (e as Error).message } finally { busy.value = false }
}
async function resolve(alert: Alert) {
  if (busy.value) return
  busy.value = true; error.value = ''; message.value = ''
  try { await api('admin.payment.resolve', { id: alert.id, note: notes.value[alert.id] }); await refresh(); notes.value[alert.id] = '' }
  catch (e) { error.value = (e as Error).message } finally { busy.value = false }
}
onMounted(() => { load(); loadThresholds() })
</script>

<template>
  <section class="panel payment-panel">
    <div class="section-head"><div><h2>自动对账</h2><p class="subtle">每天北京时间 10:00 后核对前一天的微信成功交易账单；失败每 30 分钟重试。</p></div><button class="button" :disabled="busy" @click="load">刷新</button></div>
    <p v-if="isDemo" class="demo-note">演示模式：结果为模拟数据，不连接微信商户账户。</p>
    <form class="payment-actions" @submit.prevent="reconcile"><label>账单日期<input v-model="billDate" type="date" :max="yesterday" required /></label><button class="button primary" :disabled="busy">{{ busy ? '处理中…' : '手动对账 / 重试' }}</button></form>
    <p v-if="error" class="form-error" role="alert">{{ error }}</p><p v-if="message" role="status">{{ message }}</p>
    <div class="table-wrap"><table><thead><tr><th>账单日期</th><th>状态</th><th>核对交易</th><th>差异</th><th>完成时间</th></tr></thead><tbody><tr v-for="run in data.runs" :key="run.billDate"><td>{{ run.billDate }}</td><td>{{ labels[run.status] || run.status }}</td><td>{{ run.checked ?? '—' }}</td><td>{{ run.differences ?? '—' }}</td><td>{{ run.finishedAt ? new Date(run.finishedAt).toLocaleString('zh-CN') : '—' }}</td></tr><tr v-if="!data.runs.length"><td colspan="5">暂无对账记录</td></tr></tbody></table></div>
  </section>
  <section class="panel payment-panel">
    <div class="section-head"><div><h2>退款对账、结算与手续费统计</h2><p class="subtle">每天自动获取前一天账单；支持按日期和账户重试。金额以微信账单为准。</p></div></div>
    <form class="payment-actions" @submit.prevent="reconcileFinance">
      <label>账单类型<select v-model="financeKind"><option v-for="(label, kind) in financeLabels" :key="kind" :value="kind">{{ label }}</option></select></label>
      <label>账单日期<input v-model="billDate" type="date" :max="yesterday" required /></label>
      <button class="button primary" :disabled="busy">{{ busy ? '处理中…' : '执行 / 重试账单' }}</button>
    </form>
    <p class="subtle">退款账单按微信受理日核对申请金额、订单、退款单号及成功状态；处理中快照允许本地状态后续推进。结算净额为账户交易收入 − 退款支出 − 净手续费，资金净变动包含其他收支，均不代表银行到账。</p>
    <div class="payment-actions"><label>统计开始<input v-model="financeFrom" type="date" :max="financeTo || yesterday" /></label><label>统计结束<input v-model="financeTo" type="date" :min="financeFrom" :max="yesterday" /></label><button class="button" @click="financeFrom = ''; financeTo = ''">清除筛选</button></div>
    <p v-if="financeFrom && financeTo && financeFrom > financeTo" class="form-error">开始日期不能晚于结束日期</p>
    <p class="subtle">统计仅汇总当前账户已成功读取的 {{ financeRuns.filter(r => r.totals).length }} 份账单（最多展示最近 124 份退款及账户账单）。未执行、处理中和失败日期不计入统计。</p>
    <div v-if="financeRuns.some(r => r.totals)" class="finance-summary">
      <template v-if="financeKind === 'REFUND'"><div>申请退款金额<strong>{{ money(financeSummary.refundRequested) }}</strong></div><div>账单应结退款金额<strong>{{ money(financeSummary.refundSettled) }}</strong></div><div>账单成功退款笔数<strong>{{ financeSummary.refundSuccessful || 0 }}</strong></div></template>
      <template v-else><div>交易结算净额<strong>{{ money(financeSummary.settlementNet) }}</strong></div><div>资金净变动<strong>{{ money(financeSummary.net) }}</strong></div></template>
      <div>扣除手续费<strong>{{ money(financeSummary.feesCharged) }}</strong></div><div>退还手续费<strong>{{ money(financeSummary.feesReturned) }}</strong></div><div>净手续费<strong>{{ money(financeSummary.netFees) }}</strong></div>
    </div>
    <div class="table-wrap"><table><thead><tr><th>日期</th><th>状态</th><th>笔数 / 差异</th><th>{{ financeKind === 'REFUND' ? '申请退款' : '资金收入' }}</th><th>{{ financeKind === 'REFUND' ? '应结退款' : '资金支出' }}</th><th>净手续费</th><th>完成时间</th></tr></thead><tbody>
      <tr v-for="run in financeRuns" :key="run.billDate + run.kind"><td>{{ run.billDate }}</td><td>{{ labels[run.status] || run.status }}{{ run.emptyBill ? '（无账单）' : '' }}<p v-if="run.message" class="subtle">{{ run.message }}</p></td><td>{{ run.checked ?? '—' }} / {{ run.differences ?? '—' }}</td><td>{{ money(run.totals?.[financeKind === 'REFUND' ? 'refundRequested' : 'income']) }}</td><td>{{ money(run.totals?.[financeKind === 'REFUND' ? 'refundSettled' : 'expense']) }}</td><td>{{ money(run.totals?.netFees) }}</td><td>{{ run.finishedAt ? new Date(run.finishedAt).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai' }) : '—' }}</td></tr>
      <tr v-if="!financeRuns.length"><td colspan="7">当前筛选暂无账单，请选择日期执行</td></tr>
    </tbody></table></div>
    <details v-for="run in financeRuns.filter(r => r.details?.length)" :key="run.billDate"><summary>{{ run.billDate }} · {{ run.differences }} 条退款差异（最多展示100条）</summary><p v-for="item in run.details" :key="item.refundNo">{{ item.refundNo }} · {{ item.message }}</p></details>
  </section>
  <section class="panel payment-panel">
    <h2>支付异常告警 · {{ data.openAlerts }} 条待处理</h2><p class="subtle">相同异常合并计数。核查后填写处理备注；再次发生会重新打开告警。显示最近 100 条告警及最近 31 天账单。</p>
    <div v-for="alert in data.alerts" :key="alert.id" class="payment-alert">
      <strong>{{ labels[alert.kind] || alert.kind }} · {{ labels[alert.status] }} · {{ alert.occurrences }} 次</strong>
      <p>{{ alert.message }}</p><p class="subtle">{{ alert.target }} · 最近发生：{{ alert.lastSeen }}</p>
      <form v-if="alert.status === 'OPEN'" class="payment-actions" @submit.prevent="resolve(alert)"><input v-model.trim="notes[alert.id]" aria-label="告警处理备注" placeholder="填写核查结果与处理措施" maxlength="240" required /><button class="button" :disabled="busy">标记已处理</button></form>
      <p v-else>处理备注：{{ alert.note }}</p>
    </div><p v-if="!data.alerts.length" class="subtle">暂无支付异常</p>
  </section>
  <section class="panel payment-panel">
    <div class="section-head"><div><h2>关键指标告警阈值</h2><p class="subtle">定时扫描（每 15 分钟）按阈值检查并写入告警</p></div></div>
    <form class="payment-actions" @submit.prevent="saveThresholds">
      <label>退款超时（小时）<input v-model.number="thresholds.refundStuckHours" type="number" min="1" max="720" required /></label>
      <label>发票超时（小时）<input v-model.number="thresholds.invoiceStuckHours" type="number" min="1" max="720" required /></label>
      <label>告警积压（条）<input v-model.number="thresholds.openAlertCount" type="number" min="1" max="100000" required /></label>
      <label>提醒失败积压（条）<input v-model.number="thresholds.reminderFailureCount" type="number" min="1" max="100000" required /></label>
      <button class="button primary" :disabled="thresholdBusy">{{ thresholdBusy ? '保存中…' : '保存阈值' }}</button>
    </form>
    <p v-if="thresholdMsg" role="status">{{ thresholdMsg }}</p>
  </section>
</template>

<style scoped>
.payment-panel { padding: 24px; margin-bottom: 20px; }
.payment-actions { display: flex; align-items: end; gap: 12px; margin: 16px 0; flex-wrap: wrap; }
.payment-actions label { display: grid; gap: 6px; }
.payment-actions input, .payment-actions select { padding: 10px; border: 1px solid #ddd; border-radius: 8px; max-width: 100%; }
.payment-alert { padding: 18px 0; border-top: 1px solid #eee; overflow-wrap: anywhere; }
.payment-alert input { flex: 1; min-width: 180px; }
.finance-summary { display: flex; gap: 24px; flex-wrap: wrap; padding: 16px; background: #f6f8f7; border-radius: 10px; }
.finance-summary strong { display: block; margin-top: 8px; font-size: 20px; }
table { width: 100%; } th, td { padding: 12px; text-align: left; white-space: nowrap; }
.table-wrap { overflow-x: auto; }
</style>
