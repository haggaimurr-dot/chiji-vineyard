const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const assert = require('node:assert/strict')
const ts = require('typescript')
const vue = require('vue')
const root = path.resolve(__dirname, '..')

function loadTS(file, imports = {}) {
  const source = fs.readFileSync(file, 'utf8')
  const module = { exports: {} }
  vm.runInNewContext(ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText, {
    module, exports: module.exports, structuredClone, Date, console,
    require: name => { if (name in imports) return imports[name]; throw new Error(`Unexpected import ${name}`) },
  }, { filename: file })
  return module.exports
}

function harness() {
  const calls = [], mounted = []
  const module = { exports: {} }
  const file = path.join(root, 'admin/src/views/PaymentsView.vue')
  const source = fs.readFileSync(file, 'utf8').match(/<script setup lang="ts">([\s\S]*?)<\/script>/)[1] + '\nexport const tested = { data, billDate, busy, error, message, notes, load, reconcile, resolve, financeKind, financeFrom, financeTo, financeRuns, financeSummary, reconcileFinance }'
  vm.runInNewContext(ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText, {
    module, exports: module.exports, Date,
    require: name => name === 'vue' ? { ...vue, onMounted: f => mounted.push(f) } : { isDemo: true, api: (action, data) => new Promise((resolve, reject) => calls.push({ action, data, resolve, reject })) },
  }, { filename: file })
  return { state: module.exports.tested, calls, mounted }
}

async function run() {
  const { paymentDemo } = loadTS(path.join(root, 'admin/src/payment-demo.ts'))
  const { requirePermission } = loadTS(path.join(root, 'admin/src/permissions.ts'))
  for (const action of ['admin.payment.financeReconcile', 'admin.payment.overview', 'admin.payment.reconcile', 'admin.payment.resolve', 'admin.payment.alertCount']) {
    assert.throws(() => requirePermission({ role: 'OPERATOR', permissions: [] }, action), /超级管理员/)
    requirePermission({ role: 'SUPER_ADMIN' }, action)
  }
  const yesterday = new Date(Date.now() - 86400000).toLocaleDateString('en-CA', { timeZone: 'Asia/Shanghai' })
  paymentDemo('admin.payment.reconcile', { billDate: yesterday })
  paymentDemo('admin.payment.reconcile', { billDate: yesterday })
  assert.equal(paymentDemo('admin.payment.overview', {}).runs.length, 1, 'retry duplicated daily record')
  assert.throws(() => paymentDemo('admin.payment.reconcile', { billDate: '2026-02-30' }), /日期/)
  assert.throws(() => paymentDemo('admin.payment.resolve', { id: 'demo-payment-alert', note: ' ' }), /备注/)
  paymentDemo('admin.payment.resolve', { id: 'demo-payment-alert', note: '核查演示记录' })
  assert.equal(paymentDemo('admin.payment.alertCount', {}).count, 0)
  assert.throws(() => paymentDemo('admin.payment.resolve', { id: 'demo-payment-alert', note: '重复提交' }), /已处理/)

  for (const kind of ['REFUND', 'BASIC', 'FEES', 'OPERATION']) {
    paymentDemo('admin.payment.financeReconcile', { billDate: yesterday, kind })
    paymentDemo('admin.payment.financeReconcile', { billDate: yesterday, kind })
  }
  const finance = paymentDemo('admin.payment.overview', {}).financeRuns
  assert.equal(finance.length, 4, 'finance retry duplicated daily/account record')
  assert.equal(finance.find(r => r.kind === 'REFUND').totals.netFees, -54)
  assert.equal(finance.find(r => r.kind === 'BASIC').totals.settlementNet, 17818)
  assert.throws(() => paymentDemo('admin.payment.financeReconcile', { billDate: yesterday, kind: 'UNKNOWN' }), /账户/)

  const h = harness(), s = h.state
  const loading = s.load(); s.load()
  assert.equal(h.calls.length, 1, 'concurrent load sent another request')
  h.calls[0].reject(new Error('加载失败')); await loading
  assert.equal(s.error.value, '加载失败'); assert.equal(s.busy.value, false)
  const reconciling = s.reconcile(); s.reconcile()
  assert.equal(h.calls.length, 2, 'concurrent reconciliation sent another request')
  assert.equal(h.calls[1].action, 'admin.payment.reconcile')
  h.calls[1].resolve({ billDate: yesterday, status: 'FAILED', message: '下载失败' })
  await new Promise(resolve => setImmediate(resolve))
  assert.equal(h.calls[2].action, 'admin.payment.overview')
  h.calls[2].resolve({ runs: [{ billDate: yesterday, status: 'FAILED' }], alerts: [], openAlerts: 0 }); await reconciling
  assert.match(s.message.value, /失败.*下载失败/); assert.equal(s.data.value.runs[0].status, 'FAILED')
  s.notes.value.alert = '已核查'
  const resolving = s.resolve({ id: 'alert' })
  assert.equal(h.calls[3].data.note, '已核查')
  h.calls[3].reject(new Error('处理失败')); await resolving
  assert.equal(s.error.value, '处理失败'); assert.equal(s.notes.value.alert, '已核查', 'failure lost user note')
  s.financeKind.value = 'BASIC'
  const financeLoading = s.reconcileFinance(); s.reconcileFinance()
  assert.equal(h.calls.length, 5)
  assert.deepEqual(JSON.parse(JSON.stringify(h.calls[4].data)), { billDate: yesterday, kind: 'BASIC' })
  h.calls[4].resolve({ billDate: yesterday, kind: 'BASIC', status: 'COMPLETED' })
  await new Promise(resolve => setImmediate(resolve))
  h.calls[5].resolve({ runs: [], alerts: [], openAlerts: 0, financeRuns: [
    { billDate: yesterday, kind: 'BASIC', status: 'COMPLETED', totals: { netFees: 30, net: 4970 } },
    { billDate: yesterday, kind: 'FEES', status: 'COMPLETED', totals: { netFees: 60 } },
    { billDate: '2000-01-01', kind: 'BASIC', status: 'FAILED', totals: { netFees: 999 } },
  ] }); await financeLoading
  assert.equal(s.financeSummary.value.netFees, 30, 'included failed or other-account totals')
  assert.match(s.message.value, /基本账户.*统计完成/)
  s.financeFrom.value = yesterday
  assert.equal(s.financeRuns.value.length, 1)
  s.financeTo.value = '2000-01-01'
  assert.equal(s.financeRuns.value.length, 0)
  console.log('Payment operations: demo permissions, retry, resolution and component error/concurrency flows passed')
}
run().catch(error => { console.error(error); process.exitCode = 1 })
