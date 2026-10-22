const assert = require('node:assert/strict')
const fs = require('node:fs')
const vm = require('node:vm')
const ts = require('typescript')
const moduleObject = { exports: {} }
vm.runInNewContext(ts.transpileModule(fs.readFileSync('miniprogram/utils/order-progress.ts', 'utf8'), {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 },
}).outputText, { exports: moduleObject.exports })
const { logisticsAlerts, orderProgress } = moduleObject.exports
const now = Date.parse('2026-10-07T12:00:00+08:00')
const order = { createdAt: '2026-10-04T00:00:00Z', paidAt: '2026-10-04T00:01:00Z', orderStatus: 'CONFIRMED', paymentStatus: 'PAID', fulfillmentStatus: 'SHIPPED', refundStatus: 'NONE', shipment: { shippedAt: '2026-10-04T01:00:00Z' } }
const tracking = { status: 'SUCCESS', state: '0', message: '运输中', updatedAt: '2026-10-07T00:00:00Z', events: [{ time: '2026-10-05 12:00:00', context: '转运' }, { time: '2026-10-04 12:00:00', context: '揽收' }] }
assert.equal(logisticsAlerts(order, tracking, now).length, 1)
assert.equal(logisticsAlerts(order, tracking, now - 1).length, 0)
for (const state of ['2', '4', '6']) assert.ok(logisticsAlerts(order, { ...tracking, state }, now).length >= 1)
assert.equal(logisticsAlerts(order, { ...tracking, state: '3' }, now).length, 0)
assert.equal(logisticsAlerts(order, { ...tracking, status: 'UNAVAILABLE', events: [] }, now).length, 0)
assert.equal(logisticsAlerts(order, { ...tracking, status: 'DEMO' }, now).length, 0)
assert.equal(logisticsAlerts(order, { ...tracking, status: 'PENDING', events: [] }, now).length, 1)
assert.equal(logisticsAlerts({ ...order, fulfillmentStatus: 'RECEIVED' }, tracking, now).length, 0)
assert.equal(logisticsAlerts(order, null, now).length, 0)
assert.equal(logisticsAlerts(order, { ...tracking, events: [{ time: 'invalid', context: 'unknown' }] }, now).length, 0)
const nodes = orderProgress(order, tracking)
assert.equal(nodes[3].detail, '揽收')
assert.equal(nodes[4].detail, '转运')
assert.equal(nodes[5].pending, true)
assert.equal(tracking.events[0].context, '转运')
const cancelled = orderProgress({ ...order, orderStatus: 'CANCELLED', paymentStatus: 'UNPAID', paidAt: undefined, shipment: undefined })
assert.equal(cancelled.map(n => n.key).join(','), 'created,cancelled')
const completed = orderProgress({ ...order, orderStatus: 'COMPLETED', fulfillmentStatus: 'RECEIVED' })
assert.equal(completed.find(n => n.key === 'received').time, '')
assert.ok(!completed.some(n => n.pending))
console.log('Order progress and logistics alert tests passed')
