const fs = require('node:fs'), path = require('node:path'), vm = require('node:vm'), assert = require('node:assert/strict'), ts = require('typescript'), vue = require('vue')
const root = path.resolve(__dirname, '..')
function evaluate(source, imports) {
  const module = { exports: {} }
  vm.runInNewContext(ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText, { module, exports: module.exports, Date, Map, Error, console, require: name => { if (name in imports) return imports[name]; throw new Error(name) } })
  return module.exports
}
const dashboard = evaluate(fs.readFileSync(path.join(root, 'admin/src/dashboard.ts'), 'utf8'), {})
const now = new Date('2026-10-05T16:00:00Z')
assert.equal(dashboard.beijingDate(now), '2026-10-06')
assert.equal(dashboard.dashboardRange({}, now).startDate, '2026-09-07')
for (const range of [{ startDate: '2026-02-30' }, { endDate: '2026-10-7' }, { endDate: '2026-10-07' }, { startDate: '2026-10-06', endDate: '2026-10-05' }, { startDate: '2025-01-01' }]) assert.throws(() => dashboard.dashboardRange(range, now), /日期/)
const order = (id, buyerId, date, paymentStatus, fulfillmentStatus = 'RECEIVED') => ({ id, buyerId, createdAt: date, paymentStatus, fulfillmentStatus, quote: { total: 10000 } })
const orders = [
  order('a', 'buyer1', '2026-09-17T16:00:00Z', 'PAID'),
  order('b', 'buyer1', '2026-09-18T15:59:59Z', 'REFUNDING'),
  order('c', 'buyer2', '2026-09-18T16:00:00Z', 'REFUNDED'),
  order('d', 'buyer3', '2026-09-19T15:59:59Z', 'PAID', 'SHIPPED'),
  order('e', 'buyer4', '2026-09-19T16:00:00Z', 'PAID'),
  order('f', 'buyer4', '2026-09-18T16:00:00Z', 'UNPAID'),
]
const refunds = [{ orderId: 'b', amount: 1000, status: 'SUCCEEDED' }, { orderId: 'b', amount: 2000, status: 'SUCCEEDED' }, { orderId: 'b', amount: 7000, status: 'APPROVED' }, { orderId: 'c', amount: 10000, status: 'SUCCEEDED' }, { orderId: 'd', amount: 5000, status: 'FAILED' }]
const counts = { pendingShipment: 4, pendingRefund: 3, newLeads: 2, products: 1, articles: 1 }
const stats = dashboard.demoDashboard(orders, refunds, { startDate: '2026-09-18', endDate: '2026-09-19' }, counts)
assert.equal(stats.grossRevenue, 40000); assert.equal(stats.refundedAmount, 13000); assert.equal(stats.revenue, 27000)
assert.equal(stats.buyers, 2); assert.equal(stats.repeatBuyers, 1); assert.equal(stats.repurchaseRate, 50)
assert.equal(stats.funnel.created, 5); assert.equal(stats.funnel.paid, 4); assert.equal(stats.funnel.received, 3)
assert.equal(stats.trend[0].netRevenue, 17000); assert.equal(stats.trend[1].netRevenue, 10000)
assert.equal(stats.trend.reduce((sum, day) => sum + day.netRevenue, 0), stats.revenue)
assert.equal(stats.pendingShipment, 4)
const empty = dashboard.demoDashboard(orders, refunds, { startDate: '2026-09-21', endDate: '2026-09-23' }, counts)
assert.equal(empty.revenue, 0); assert.equal(empty.repurchaseRate, 0); assert.equal(empty.trend.length, 3)
const activity = [
  { buyerId: 'buyer1', visitedAt: '2026-09-17T16:00:00Z', cartAddedAt: '2026-09-17T16:00:00Z' },
  { buyerId: 'buyer1', visitedAt: '2026-09-18T10:00:00Z', cartAddedAt: '2026-09-18T10:01:00Z' },
  { buyerId: 'buyer2', visitedAt: '2026-09-18T12:00:00Z', cartAddedAt: '2026-09-18T12:01:00Z' },
  { buyerId: 'buyer3', visitedAt: '2026-09-19T15:59:59Z' }, // direct purchase excluded
  { buyerId: 'buyer4', visitedAt: '2026-09-19T16:00:00Z', cartAddedAt: '2026-09-19T16:00:00Z' }, // end excluded
  { buyerId: 'visitor', visitedAt: '2026-09-18T08:00:00Z' },
  { buyerId: 'cart-only', visitedAt: '2026-09-18T08:00:00Z', cartAddedAt: '2026-09-18T08:01:00Z' },
  { buyerId: 'before-cart', visitedAt: '2026-09-18T08:00:00Z', cartAddedAt: '2026-09-19T08:01:00Z' },
]
const behavioral = dashboard.demoDashboard([...orders, order('early', 'before-cart', '2026-09-18T09:00:00Z', 'PAID')], refunds, { startDate: '2026-09-18', endDate: '2026-09-19' }, counts, activity).behaviorFunnel
assert.equal(behavioral.visitors, 6); assert.equal(behavioral.cartUsers, 4); assert.equal(behavioral.createdUsers, 2); assert.equal(behavioral.paidUsers, 2); assert.equal(behavioral.receivedUsers, 2)
assert.equal(behavioral.partial, false)
assert.equal(empty.behaviorFunnel.visitors, 0); assert.equal(empty.behaviorFunnel.startedAt, null); assert.equal(empty.behaviorFunnel.partial, true)

async function testView() {
  const calls = [], unmounted = []
  const source = fs.readFileSync(path.join(root, 'admin/src/views/DashboardView.vue'), 'utf8').match(/<script setup lang="ts">([\s\S]*?)<\/script>/)[1]
  const { state } = evaluate(source + '\nexport const state = { load, startDate, endDate, stats, loading, error, loadRecent, recentError, orders, leads }', {
    vue: { ...vue, onMounted: () => {}, onUnmounted: f => unmounted.push(f) },
    '../api': { isDemo: true, api: (action, data) => new Promise((resolve, reject) => calls.push({ action, data, resolve, reject })) },
    '../dashboard': dashboard,
    '../permissions': { can: () => false, currentAdmin: () => ({}) },
  })
  state.startDate.value = '2026-09-18'; state.endDate.value = '2026-09-19'
  const first = state.load(); const second = state.load()
  calls[1].resolve(stats); await second
  calls[0].resolve(empty); await first
  assert.equal(state.stats.value.revenue, 27000, 'old response replaced current range')
  const failure = state.load(); calls[2].reject(new Error('加载失败')); await failure
  assert.equal(state.stats.value, null); assert.equal(state.error.value, '加载失败'); assert.equal(state.loading.value, false)
  const retry = state.load(); calls[3].resolve(stats); await retry
  assert.equal(state.stats.value.revenue, 27000); assert.equal(state.error.value, '')
  state.startDate.value = '2026-02-30'; await state.load(); assert.equal(calls.length, 4)
  await state.loadRecent(); assert.equal(calls.length, 4, 'dashboard-only role requested restricted details')
  state.startDate.value = '2026-09-18'; const disposed = state.load(); unmounted[0](); calls[4].resolve(stats); await disposed
  assert.equal(state.stats.value, null, 'unmounted request updated view')
}
testView().then(() => console.log('Dashboard passed: China date boundaries, successful/partial/full/pending/failed refunds, buyers, funnel, daily totals, zero days, stale requests, errors, retry, permission isolation.')).catch(error => { console.error(error); process.exitCode = 1 })
