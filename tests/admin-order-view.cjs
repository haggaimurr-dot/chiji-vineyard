const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const assert = require('node:assert/strict')
const ts = require('typescript')
const vue = require('vue')
const root = path.resolve(__dirname, '..')

// Execute the component's own script setup with Vue's actual reactivity. Only the
// API transport and DOM focus targets are replaced; no component logic is copied.
function harness() {
  const calls = [], mounted = [], unmounted = [], cache = new Map()
  const document = { activeElement: null, querySelector: () => null }
  const makeNode = name => ({ name, isConnected: true, focusCount: 0, focus() { this.focusCount++; document.activeElement = this } })
  const trigger = makeNode('shipping trigger'), first = makeNode('close'), tracking = makeNode('tracking'), last = makeNode('save'), drawer = makeNode('drawer')
  let focusable = [first, tracking, last]
  drawer.querySelector = selector => selector === '#shipment-tracking' ? tracking : null
  drawer.querySelectorAll = () => focusable
  document.activeElement = trigger
  const api = (action, data = {}) => new Promise((resolve, reject) => calls.push({ action, data, resolve, reject }))
  const injectedVue = { ...vue, onMounted: callback => mounted.push(callback), onBeforeUnmount: callback => unmounted.push(callback) }
  function load(file) {
    if (cache.has(file)) return cache.get(file).exports
    const module = { exports: {} }; cache.set(file, module)
    let source = fs.readFileSync(file, 'utf8')
    if (file.endsWith('.vue')) {
      source = source.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)[1]
      source += '\nexport const tested = { orders, refunds, filter, keyword, appliedKeyword, currentPage, pageSize, total, loading, listError, error, success, refundError, refundsLoading, pages, range, selectedIds, allSelected, operationBusy, syncBusy, shippingOrder, shippingBusy, shippingError, shippingForm, carrierChoice, shippingDrawer, shippingEditing, shippingChanged, trajectory, trackingOrder, trackingBusy, trackingError, evidenceRefund, evidenceUrls, evidenceBusy, evidenceError, loadOrders, search, clearSearch, goPage, loadRefunds, refresh, openShipping, closeShipping, shippingKeydown, chooseCarrier, saveShipment, resync, track, closeTracking, viewEvidence, closeEvidence, canShip, canEditShipment, orderSummary, orderStateLabel, paymentStateLabel, fulfillmentStateLabel, refundStateLabel }'
    }
    const compiled = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS } }).outputText
    vm.runInNewContext(compiled, {
      module, exports: module.exports, console, Error, structuredClone, setTimeout, clearTimeout, document,
      confirm: () => true, prompt: () => null,
      require: name => name === 'vue' ? injectedVue : name === '../api' ? { api, money: cents => (cents / 100).toFixed(2), dateTime: value => value } : name.endsWith('.vue') ? {} : load(path.resolve(path.dirname(file), `${name}.ts`)),
    }, { filename: file })
    return module.exports
  }
  const scope = vue.effectScope()
  const state = scope.run(() => load(path.join(root, 'admin/src/views/OrdersView.vue')).tested)
  state.shippingDrawer.value = drawer
  return {
    state, calls, document, trigger, first, tracking, last, drawer,
    emptyFocusables: () => { focusable = [] },
    dispose() { unmounted.forEach(callback => callback()); scope.stop() },
  }
}

const flush = async () => { await vue.nextTick(); await Promise.resolve(); await Promise.resolve() }
const order = (id = 'o', patch = {}) => ({
  id, orderNo: `CJ-${id}`, createdAt: '2026-10-04T00:00:00Z', orderStatus: 'CONFIRMED', paymentStatus: 'PAID', fulfillmentStatus: 'PREPARING', refundStatus: 'NONE',
  items: [{ name: '阳光玫瑰', skuName: '家庭装', quantity: 1, cover: '' }], quote: { total: 10000 },
  address: { name: '测试', phone: '13800000000', province: '浙江省', city: '杭州市', district: '西湖区', detail: '测试地址' }, ...patch,
})
const shipment = (patch = {}) => ({ carrier: '顺丰速运', carrierCode: 'SF', trackingNo: 'SF12345', syncStatus: 'SUCCESS', ...patch })
const pageResult = (items, total, offset = 0, limit = 20) => ({ items, total, offset, limit })

async function searchAndPagination() {
  const h = harness(), s = h.state
  s.keyword.value = '  CJ-old  '; s.search()
  assert.equal(h.calls[0].data.keyword, 'CJ-old')
  s.keyword.value = 'CJ-new'; s.search()
  h.calls[1].resolve(pageResult([order('new')], 45)); await flush()
  h.calls[0].reject(new Error('old search failure')); await flush()
  assert.equal(s.orders.value[0].id, 'new'); assert.equal(s.listError.value, ''); assert.equal(s.loading.value, false)
  assert.equal(s.pages.value, 3); assert.equal(s.range.value, '1–20')
  s.keyword.value = 'unsubmitted draft'; s.goPage(2)
  assert.equal(h.calls[2].data.keyword, 'CJ-new'); assert.equal(h.calls[2].data.offset, 20)
  s.goPage(3); assert.equal(h.calls.length, 3, 'page click while loading sent another request')
  h.calls[2].reject(new Error('page failed')); await flush()
  assert.match(s.listError.value, /page failed/); assert.equal(s.currentPage.value, 2)
  const retry = s.loadOrders(); assert.equal(h.calls[3].data.offset, 20); assert.equal(h.calls[3].data.keyword, 'CJ-new')
  h.calls[3].resolve(pageResult([order('page-two')], 45, 20)); await retry
  s.selectedIds.value = ['page-two']; s.filter.value = 'cancelled'; await flush()
  assert.equal(h.calls[4].data.offset, 0); assert.equal(h.calls[4].data.filter, 'cancelled')
  assert.equal(h.calls[4].data.keyword, 'CJ-new'); assert.equal(s.selectedIds.value.length, 0)
  assert.equal(s.orders.value.length, 0, 'previous page remained under another status')
  h.calls[4].resolve(pageResult([order('cancelled', { orderStatus: 'CANCELLED', paymentStatus: 'UNPAID' })], 1)); await flush()
  s.pageSize.value = 50; await flush(); assert.equal(h.calls[5].data.limit, 50); assert.equal(s.currentPage.value, 1)
  h.calls[5].resolve(pageResult([], 0, 0, 50)); await flush()
  s.clearSearch(); assert.equal(s.appliedKeyword.value, ''); assert.equal(s.keyword.value, '')
  h.calls[6].resolve(pageResult([], 0, 0, 50)); await flush()
  h.dispose()

  const clamp = harness(), c = clamp.state
  c.total.value = 60; c.currentPage.value = 3
  const pending = c.loadOrders(); clamp.calls[0].resolve(pageResult([], 21, 40)); await flush()
  assert.equal(c.currentPage.value, 2); assert.equal(clamp.calls[1].data.offset, 20)
  clamp.calls[1].resolve(pageResult([order('last-page')], 21, 20)); await pending
  assert.equal(c.loading.value, false); assert.equal(c.orders.value[0].id, 'last-page')
  const gone = c.loadOrders(); clamp.dispose(); clamp.calls[2].resolve(pageResult([order('after-unmount')], 1)); await gone
  assert.equal(c.orders.value[0].id, 'last-page', 'unmounted component accepted a late response')
}

async function independentFailuresAndDialogs() {
  const h = harness(), s = h.state
  s.refunds.value = [{ id: 'existing-refund' }]
  const refresh = s.refresh()
  h.calls[0].resolve(pageResult([order('valid-order')], 1)); h.calls[1].reject(new Error('refund service offline'))
  await refresh
  assert.equal(s.orders.value[0].id, 'valid-order'); assert.equal(s.listError.value, '')
  assert.match(s.refundError.value, /refund service offline/); assert.equal(s.refunds.value[0].id, 'existing-refund')
  assert.equal(s.loading.value, false); assert.equal(s.refundsLoading.value, false)
  const inverse = s.refresh()
  h.calls[2].reject(new Error('orders offline')); h.calls[3].resolve([{ id: 'fresh-refund' }]); await inverse
  assert.match(s.listError.value, /orders offline/); assert.equal(s.refundError.value, ''); assert.equal(s.refunds.value[0].id, 'fresh-refund')
  const oldRefunds = s.loadRefunds(), newRefunds = s.loadRefunds()
  h.calls[5].resolve([{ id: 'latest-refund' }]); await newRefunds
  h.calls[4].reject(new Error('stale refund failure')); await oldRefunds
  assert.equal(s.refunds.value[0].id, 'latest-refund'); assert.equal(s.refundError.value, '')
  const oldTrack = s.track(order('old', { shipment: shipment() })), newTrack = s.track(order('new', { shipment: shipment() }))
  h.calls[7].resolve({ message: 'current track', events: [] }); await newTrack
  h.calls[6].reject(new Error('stale track')); await oldTrack
  assert.equal(s.trajectory.value.message, 'current track'); assert.equal(s.trackingError.value, '')
  const closedTrack = s.track(order('closed', { shipment: shipment() })); s.closeTracking()
  h.calls[8].resolve({ message: 'closed track', events: [] }); await closedTrack
  assert.equal(s.trackingOrder.value, null); assert.equal(s.trajectory.value, null)
  const oldEvidence = s.viewEvidence({ id: 'old', images: ['a'] }), newEvidence = s.viewEvidence({ id: 'new', images: ['b'] })
  h.calls[10].resolve({ dataUrl: 'new-image' }); await newEvidence; h.calls[9].reject(new Error('old evidence')); await oldEvidence
  assert.equal(s.evidenceUrls.value[0], 'new-image'); assert.equal(s.evidenceError.value, '')
  h.dispose()
}

async function shipmentEditingAndFeedback() {
  const h = harness(), s = h.state, original = order('existing', { fulfillmentStatus: 'SHIPPED', shipment: shipment() })
  await s.openShipping(original)
  assert.equal(h.document.activeElement, h.tracking, 'opening the editor did not focus the tracking field')
  assert.equal(s.shippingEditing.value, true); assert.equal(s.shippingChanged.value, false)
  await s.saveShipment(); assert.equal(h.calls.length, 0, 'unchanged shipment was submitted')
  s.shippingForm.value.trackingNo = 'bad!'; await s.saveShipment()
  assert.match(s.shippingError.value, /运单号/); assert.equal(h.calls.length, 0)
  s.shippingForm.value.trackingNo = 'SF-UPDATED'
  const saving = s.saveShipment(); await s.saveShipment()
  assert.equal(h.calls.length, 1, 'duplicate save sent another request'); assert.equal(s.shippingBusy.value, true)
  assert.equal(h.calls[0].action, 'admin.order.updateShipment')
  assert.deepEqual(JSON.parse(JSON.stringify(h.calls[0].data.expectedShipment)), { carrier: '顺丰速运', carrierCode: 'SF', trackingNo: 'SF12345' })
  s.closeShipping(); assert.equal(s.shippingOrder.value.id, 'existing', 'saving editor could be dismissed')
  await s.openShipping(order('other')); assert.equal(s.shippingOrder.value.id, 'existing', 'saving editor was replaced by another order')
  h.calls[0].reject(new Error('订单状态已变化')); await saving
  assert.equal(s.shippingForm.value.trackingNo, 'SF-UPDATED'); assert.equal(s.shippingOrder.value.id, 'existing')
  assert.match(s.shippingError.value, /订单状态已变化/); assert.equal(s.shippingBusy.value, false)
  const pendingSync = s.saveShipment()
  h.calls[1].resolve({ shipment: shipment({ trackingNo: 'SF-UPDATED', syncStatus: 'PENDING' }) }); await flush()
  assert.equal(s.shippingOrder.value, null); assert.match(s.success.value, /已更新.*待核实/); assert.equal(s.error.value, '')
  h.calls[2].resolve(pageResult([order('updated')], 1)); await pendingSync
  assert.equal(original.shipment.trackingNo, 'SF12345', 'form edits mutated the source order before a response')

  await s.openShipping(order('new'))
  s.shippingForm.value.trackingNo = 'NEW12345'; const failedSync = s.saveShipment()
  assert.equal(h.calls[3].action, 'admin.order.ship')
  h.calls[3].resolve({ shipment: shipment({ trackingNo: 'NEW12345', syncStatus: 'FAILED', syncError: '微信暂不可用' }) }); await flush()
  assert.equal(s.shippingOrder.value, null); assert.match(s.success.value, /已标记发货/)
  assert.match(s.error.value, /资料已保存.*微信暂不可用/)
  h.calls[4].reject(new Error('保存后刷新失败')); await failedSync
  assert.match(s.listError.value, /保存后刷新失败/); assert.match(s.error.value, /资料已保存/)
  await s.openShipping(order('custom'))
  s.carrierChoice.value = 'custom'; s.chooseCarrier(); assert.equal(s.shippingForm.value.carrier, ''); assert.equal(s.shippingForm.value.carrierCode, '')
  s.carrierChoice.value = 'JD'; s.chooseCarrier(); assert.equal(s.shippingForm.value.carrier, '京东物流'); assert.equal(s.shippingForm.value.carrierCode, 'JD')
  h.document.activeElement = h.last; let prevented = false
  s.shippingKeydown({ key: 'Tab', shiftKey: false, preventDefault: () => { prevented = true } })
  assert.equal(prevented, true); assert.equal(h.document.activeElement, h.first)
  h.document.activeElement = h.first; prevented = false
  s.shippingKeydown({ key: 'Tab', shiftKey: true, preventDefault: () => { prevented = true } })
  assert.equal(prevented, true); assert.equal(h.document.activeElement, h.last)
  s.shippingKeydown({ key: 'Escape', preventDefault: () => {} }); await flush()
  assert.equal(s.shippingOrder.value, null); assert.equal(h.document.activeElement, h.last, 'closing editor did not restore the opening element')
  h.dispose()
}

async function validStatesAndOperationGuards() {
  const h = harness(), s = h.state
  const invalid = [
    { orderStatus: 'CANCELLED', paymentStatus: 'UNPAID' }, { orderStatus: 'CLOSED', paymentStatus: 'UNPAID' },
    { orderStatus: 'COMPLETED', fulfillmentStatus: 'RECEIVED' }, { fulfillmentStatus: 'RECEIVED' },
    { refundStatus: 'APPLIED' }, { refundStatus: 'APPROVED', paymentStatus: 'REFUNDING' }, { refundStatus: 'SUCCEEDED', paymentStatus: 'REFUNDED' },
  ]
  for (const patch of invalid) {
    const candidate = order('invalid', patch)
    assert.equal(s.canShip(candidate), false); assert.equal(s.canEditShipment({ ...candidate, shipment: shipment() }), false)
    await s.openShipping(candidate); assert.equal(s.shippingOrder.value, null, `editor accepted ${JSON.stringify(patch)}`)
  }
  for (const patch of [{ orderStatus: 'CANCELLED' }, { orderStatus: 'CLOSED' }, { orderStatus: 'COMPLETED', fulfillmentStatus: 'RECEIVED' }, { fulfillmentStatus: 'RECEIVED' }]) {
    assert.doesNotMatch(s.orderSummary(order('state', patch)), /待付款|待收货|待发货/)
  }
  for (const refundStatus of ['NONE', 'APPLIED', 'APPROVED', 'REJECTED', 'SUCCEEDED', 'PARTIAL', 'FAILED']) assert.doesNotMatch(s.refundStateLabel(refundStatus), /状态待确认/)
  assert.equal(s.canEditShipment(order('syncing', { fulfillmentStatus: 'SHIPPED', shipment: shipment({ syncStatus: 'PENDING', syncStartedAt: new Date().toISOString() }) })), false)
  assert.equal(s.canEditShipment(order('retryable', { fulfillmentStatus: 'SHIPPED', shipment: shipment({ syncStatus: 'FAILED' }) })), true)
  h.dispose()
}

;(async () => {
  await searchAndPagination()
  await independentFailuresAndDialogs()
  await shipmentEditingAndFeedback()
  await validStatesAndOperationGuards()
  console.log('OrdersView component: submitted search, page retries/clamping, stale responses, independent refund failures, shipment drafts/CAS/guards, sync feedback and keyboard focus passed.')
})().catch(error => { console.error(error); process.exitCode = 1 })
