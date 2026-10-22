const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const assert = require('node:assert/strict')
const ts = require('typescript')
const root = path.resolve(__dirname, '..')

function runtime(orderSeed) {
  const cache = new Map()
  return function load(relative) {
    const file = path.resolve(root, relative)
    if (cache.has(file)) return cache.get(file).exports
    const module = { exports: {} }; cache.set(file, module)
    let source = ts.transpileModule(fs.readFileSync(file, 'utf8'), {
      compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS },
    }).outputText
    // Keep fixture injection inside the test VM; production has no seed setter.
    if (orderSeed && file === path.resolve(root, 'admin/src/mock.ts')) source += '\norders = __orderSeed;'
    vm.runInNewContext(source, {
      module, exports: module.exports, console, structuredClone, __orderSeed: orderSeed,
      setTimeout: callback => setTimeout(callback, 0), clearTimeout,
      require: name => {
        if (!name.startsWith('.')) return require(name)
        const base = path.resolve(path.dirname(file), name)
        const resolved = [base, `${base}.ts`, `${base}.js`, `${base}.mjs`].find(candidate => fs.existsSync(candidate) && fs.statSync(candidate).isFile())
        return load(resolved || `${base}.ts`)
      },
    }, { filename: file })
    return module.exports
  }
}

function order(id, states = {}) {
  return {
    id, orderNo: `CJ-${id}`, createdAt: '2026-10-04T00:00:00.000Z',
    orderStatus: 'CONFIRMED', paymentStatus: 'PAID', fulfillmentStatus: 'PREPARING', refundStatus: 'NONE',
    items: [{ name: '阳光玫瑰', skuName: '家庭装', quantity: 1, cover: '' }],
    address: { name: '王女士', phone: '13812345678', province: '浙江省', city: '杭州市', district: '西湖区', detail: '测试路1号' },
    quote: { total: 10000 }, ...states,
  }
}

const shipment = { carrier: '顺丰速运', carrierCode: 'SF', trackingNo: 'SF12345678', shippedAt: '2026-10-03T08:00:00.000Z', syncStatus: 'SUCCESS' }
const data = { carrier: ' 中通快递 ', carrierCode: ' zto ', trackingNo: ' ZTO987654321 ' }

async function main() {
  const helpers = runtime()('admin/src/orders.ts')
  const { orderSummary, orderStateLabel, paymentStateLabel, fulfillmentStateLabel, refundStateLabel, canShip, canEditShipment, validateShipment } = helpers
  assert.equal(orderSummary(order('pending', { orderStatus: 'PENDING_PAYMENT', paymentStatus: 'UNPAID', fulfillmentStatus: 'UNFULFILLED' })), '待付款')
  assert.equal(orderSummary(order('cancelled', { orderStatus: 'CANCELLED', paymentStatus: 'UNPAID' })), '已取消')
  assert.equal(orderSummary(order('closed', { orderStatus: 'CLOSED', paymentStatus: 'UNPAID' })), '已关闭')
  assert.equal(orderSummary(order('completed', { orderStatus: 'COMPLETED', fulfillmentStatus: 'RECEIVED' })), '已完成')
  assert.equal(orderSummary(order('received', { fulfillmentStatus: 'RECEIVED' })), '已收货')
  assert.equal(orderSummary(order('shipping', { fulfillmentStatus: 'SHIPPED' })), '待收货')
  assert.equal(orderSummary(order('unfulfilled', { fulfillmentStatus: 'UNFULFILLED' })), '待发货')
  assert.equal(orderSummary(order('unpaid', { paymentStatus: 'UNPAID' })), '未付款')
  assert.equal(orderSummary(order('refund', { refundStatus: 'SUCCEEDED' })), '已退款')
  assert.equal(orderSummary(order('refunding', { paymentStatus: 'REFUNDING' })), '退款处理中')
  assert.equal(orderSummary(order('partial', { fulfillmentStatus: 'SHIPPED', refundStatus: 'PARTIAL' })), '待收货 · 已部分退款')
  assert.equal(orderSummary(order('rejected', { refundStatus: 'REJECTED' })), '待发货 · 审核未通过')
  assert.equal(orderSummary(order('completed-refund', { orderStatus: 'COMPLETED', fulfillmentStatus: 'RECEIVED', refundStatus: 'SUCCEEDED' })), '已完成 · 退款成功')
  for (const state of ['APPLIED', 'APPROVED', 'FAILED']) assert.equal(orderSummary(order(state, { refundStatus: state })), refundStateLabel(state))
  for (const [label, states] of [
    [orderStateLabel, ['PENDING_PAYMENT', 'CONFIRMED', 'CANCELLED', 'CLOSED', 'COMPLETED']],
    [paymentStateLabel, ['UNPAID', 'PAID', 'REFUNDING', 'REFUNDED']],
    [fulfillmentStateLabel, ['UNFULFILLED', 'PREPARING', 'SHIPPED', 'RECEIVED']],
    [refundStateLabel, ['NONE', 'APPLIED', 'APPROVED', 'REJECTED', 'SUCCEEDED', 'PARTIAL', 'FAILED']],
  ]) {
    for (const state of states) assert.doesNotMatch(label(state), /待确认/)
    assert.equal(label('FUTURE_STATE'), '状态待确认（FUTURE_STATE）')
    assert.equal(label('toString'), '状态待确认（toString）')
    assert.equal(label(''), '状态待确认（未提供）')
  }
  for (const key of ['orderStatus', 'paymentStatus', 'fulfillmentStatus', 'refundStatus']) {
    assert.equal(orderSummary(order('unknown', { [key]: 'FUTURE_STATE' })), '状态待确认（FUTURE_STATE）')
  }
  for (const refundStatus of ['NONE', 'REJECTED', 'PARTIAL']) assert.equal(canShip(order('eligible', { refundStatus })), true)
  for (const refundStatus of ['APPLIED', 'APPROVED', 'FAILED', 'SUCCEEDED', 'FUTURE_STATE']) assert.equal(canShip(order('blocked', { refundStatus })), false)
  for (const orderStatus of ['PENDING_PAYMENT', 'CANCELLED', 'CLOSED', 'COMPLETED']) assert.equal(canShip(order('terminal', { orderStatus })), false)

  const shipped = order('shipped', { fulfillmentStatus: 'SHIPPED', shipment: { ...shipment } })
  assert.equal(canEditShipment(shipped), true)
  for (const states of [{ fulfillmentStatus: 'RECEIVED' }, { orderStatus: 'CLOSED' }, { paymentStatus: 'REFUNDED' }, { refundStatus: 'APPLIED' }, { refundStatus: 'FAILED' }, { shipment: undefined }]) {
    assert.equal(canEditShipment({ ...shipped, ...states }), false)
  }
  const pending = { ...shipment, syncStatus: 'PENDING', syncStartedAt: new Date().toISOString() }
  assert.equal(canEditShipment({ ...shipped, shipment: pending }), false)
  assert.equal(canEditShipment({ ...shipped, shipment: { ...pending, syncStartedAt: new Date(Date.now() - 121000).toISOString() } }), true)
  assert.equal(canEditShipment({ ...shipped, shipment: { ...pending, syncStartedAt: undefined } }), true)

  assert.deepEqual({ ...validateShipment(data) }, { carrier: '中通快递', carrierCode: 'ZTO', trackingNo: 'ZTO987654321' })
  assert.doesNotThrow(() => validateShipment({ carrier: '葡'.repeat(40), carrierCode: 'A'.repeat(20), trackingNo: '1'.repeat(50) }))
  assert.doesNotThrow(() => validateShipment({ carrier: '🍇'.repeat(40), carrierCode: 'SF', trackingNo: 'SF12' }))
  for (const invalid of [
    { carrier: '' }, { carrier: '葡'.repeat(41) }, { carrier: '<快递>' }, { carrier: '快递\n公司' }, { carrier: '快递\u0085公司' },
    { carrierCode: '' }, { carrierCode: 'A'.repeat(21) }, { carrierCode: '顺丰' }, { carrierCode: 'S F' },
    { trackingNo: '' }, { trackingNo: '123' }, { trackingNo: '1'.repeat(51) }, { trackingNo: 'SF 123' }, { trackingNo: 'SF/123' },
  ]) assert.throws(() => validateShipment({ ...data, ...invalid }))

  const seed = [
    order('01-pending', { orderStatus: 'PENDING_PAYMENT', paymentStatus: 'UNPAID', fulfillmentStatus: 'UNFULFILLED' }),
    order('02-preparing'), order('03-partial', { refundStatus: 'PARTIAL' }), order('04-applied', { refundStatus: 'APPLIED' }),
    order('05-shipped', { fulfillmentStatus: 'SHIPPED', shipment: { ...shipment } }),
    order('06-shipped-partial', { fulfillmentStatus: 'SHIPPED', refundStatus: 'PARTIAL' }),
    order('07-refunded', { fulfillmentStatus: 'SHIPPED', paymentStatus: 'REFUNDED', refundStatus: 'SUCCEEDED' }),
    order('08-received', { fulfillmentStatus: 'RECEIVED' }),
    order('09-completed', { orderStatus: 'COMPLETED', fulfillmentStatus: 'RECEIVED' }),
    order('10-cancelled', { orderStatus: 'CANCELLED', paymentStatus: 'UNPAID' }),
    order('11-closed', { orderStatus: 'CLOSED', paymentStatus: 'UNPAID' }),
    order('12-approved', { paymentStatus: 'REFUNDING', refundStatus: 'APPROVED' }),
    order('13-failed', { paymentStatus: 'REFUNDING', refundStatus: 'FAILED' }),
    order('14-closed-paid', { orderStatus: 'CLOSED' }),
    order('15-refunded-legacy', { paymentStatus: 'REFUNDED' }),
  ]
  const { mockCall } = runtime(seed)('admin/src/mock.ts')
  const expected = {
    pending: ['01-pending'], shipping_pending: ['03-partial', '02-preparing'], shipped: ['06-shipped-partial', '05-shipped'],
    received: ['09-completed', '08-received'], completed: ['09-completed'], cancelled: ['10-cancelled'], closed: ['14-closed-paid', '11-closed'],
    refund: ['15-refunded-legacy', '13-failed', '12-approved', '07-refunded', '06-shipped-partial', '04-applied', '03-partial'],
  }
  for (const [filter, ids] of Object.entries(expected)) {
    const page = await mockCall('admin.order.search', { filter })
    assert.deepEqual(Array.from(page.items, item => item.id), ids, filter)
    assert.equal(page.total, ids.length)
  }
  assert.equal((await mockCall('admin.order.search', { filter: 'paid' })).total, 2)
  assert.equal((await mockCall('admin.order.search', { filter: 'shipping' })).total, 2)
  const first = await mockCall('admin.order.search', { limit: 4 })
  const second = await mockCall('admin.order.search', { limit: 4, offset: 4 })
  assert.equal(first.total, seed.length); assert.equal(second.total, seed.length)
  assert.equal(first.limit, 4); assert.equal(second.offset, 4)
  assert.equal(new Set([...first.items, ...second.items].map(item => item.id)).size, 8)
  assert.equal((await mockCall('admin.order.search', { offset: 999 })).items.length, 0)
  assert.equal((await mockCall('admin.order.search', { offset: -1, limit: 999 })).offset, 0)
  assert.equal((await mockCall('admin.order.search', { limit: 999 })).limit, 100)
  assert.equal((await mockCall('admin.order.search', { limit: 0 })).limit, 20)
  assert.equal((await mockCall('admin.order.search', { keyword: ' cj-05 阳光 家庭 ' })).total, 1)
  assert.equal((await mockCall('admin.order.search', { keyword: '王女士 13812345678' })).total, seed.length)
  assert.equal((await mockCall('admin.order.search', { keyword: 'sf12345678' })).items[0].id, '05-shipped')
  assert.equal((await mockCall('admin.order.search', { keyword: '%_' })).total, 0)
  assert.equal((await mockCall('admin.order.search', { paymentStatus: 'paid', fulfillmentStatus: 'received' })).total, 2)
  assert.equal((await mockCall('admin.order.search', { filter: 'shipping_pending', refundStatus: 'APPLIED' })).total, 0)
  await assert.rejects(() => mockCall('admin.order.search', { filter: 'unexpected' }), /筛选/)
  await assert.rejects(() => mockCall('admin.order.search', { paymentStatus: 'unexpected' }), /筛选/)
  assert.equal(Array.isArray(await mockCall('admin.order.list')), true)

  const editable = order('editable', {
    fulfillmentStatus: 'SHIPPED', shipment: { ...shipment, syncStatus: 'FAILED', syncError: '旧错误' },
    shippingNoticeStatus: 'SENT', shippingNoticeError: '通知历史错误',
  })
  const unshipped = order('unshipped', { shippingNoticeStatus: 'READY' })
  const other = order('other')
  const shippingSeed = [editable, unshipped, other, order('cancelled', { orderStatus: 'CANCELLED' }), order('refunding', { refundStatus: 'APPLIED' })]
  const shippingCall = runtime(shippingSeed)('admin/src/mock.ts').mockCall
  await assert.rejects(() => shippingCall('admin.order.ship', { orderId: 'unshipped', ...data, trackingNo: 'bad no' }), /运单号/)
  assert.equal(unshipped.fulfillmentStatus, 'PREPARING'); assert.equal(unshipped.shipment, undefined)
  await assert.rejects(() => shippingCall('admin.order.ship', { orderId: 'cancelled', ...data }), /不能发货/)
  await assert.rejects(() => shippingCall('admin.order.ship', { orderId: 'refunding', ...data }), /不能发货/)
  const recorded = await shippingCall('admin.order.ship', { orderId: 'unshipped', ...data })
  assert.equal(recorded.shipment.carrierCode, 'ZTO'); assert.ok(recorded.shipment.shippedAt)
  assert.equal(unshipped.shippingNoticeStatus, 'SENT')
  const sentShipment = { ...unshipped.shipment }
  const repeated = await shippingCall('admin.order.ship', { orderId: 'unshipped', ...data })
  assert.deepEqual({ ...repeated.shipment }, sentShipment)
  assert.equal(unshipped.shippingNoticeStatus, 'SENT')
  await assert.rejects(() => shippingCall('admin.order.ship', { orderId: 'unshipped', ...data, trackingNo: 'ZTO000000001' }), /已发货|不能覆盖/)
  assert.deepEqual({ ...unshipped.shipment }, sentShipment)
  await assert.rejects(() => shippingCall('admin.order.ship', { orderId: 'other', ...data }), /其他订单|运单号/)
  assert.equal(other.fulfillmentStatus, 'PREPARING'); assert.equal(other.shipment, undefined)
  unshipped.fulfillmentStatus = 'RECEIVED'
  const receivedRetry = await shippingCall('admin.order.ship', { orderId: 'unshipped', ...data })
  assert.deepEqual({ ...receivedRetry.shipment }, sentShipment)
  assert.equal(unshipped.fulfillmentStatus, 'RECEIVED'); assert.equal(unshipped.shippingNoticeStatus, 'SENT')
  const editData = { ...data, trackingNo: ' ZTO444444444 ' }
  const original = { ...editable.shipment }
  await assert.rejects(() => shippingCall('admin.order.updateShipment', { orderId: 'editable', ...data }), /信息已变化/)
  await assert.rejects(() => shippingCall('admin.order.updateShipment', { orderId: 'editable', ...data, expectedShipment: { ...original, carrierCode: 'sf' } }), /信息已变化/)
  await assert.rejects(() => shippingCall('admin.order.updateShipment', { orderId: 'editable', ...data, trackingNo: 'bad no', expectedShipment: original }), /运单号/)
  await assert.rejects(() => shippingCall('admin.order.updateShipment', { orderId: 'editable', ...data, expectedShipment: original }), /其他订单|运单号/)
  assert.deepEqual(editable.shipment, original)
  const saved = await shippingCall('admin.order.updateShipment', { orderId: 'editable', ...editData, expectedShipment: original })
  assert.equal(saved.shipment.trackingNo, 'ZTO444444444'); assert.equal(saved.shipment.syncStatus, 'SUCCESS')
  assert.equal(saved.shipment.shippedAt, original.shippedAt); assert.equal(saved.shipment.syncError, undefined)
  assert.equal(editable.shippingNoticeStatus, 'SENT'); assert.equal(editable.shippingNoticeError, '通知历史错误')
  await assert.rejects(() => shippingCall('admin.order.updateShipment', { orderId: 'editable', ...data, expectedShipment: original }), /信息已变化/)
  const snapshot = { ...editable.shipment }
  const concurrent = await Promise.allSettled([
    shippingCall('admin.order.updateShipment', { orderId: 'editable', ...data, trackingNo: 'ZTO11111111', expectedShipment: snapshot }),
    shippingCall('admin.order.updateShipment', { orderId: 'editable', ...data, trackingNo: 'ZTO22222222', expectedShipment: snapshot }),
  ])
  assert.equal(concurrent.filter(result => result.status === 'fulfilled').length, 1)
  assert.equal(concurrent.filter(result => result.status === 'rejected').length, 1)
  editable.fulfillmentStatus = 'RECEIVED'
  await assert.rejects(() => shippingCall('admin.order.updateShipment', { orderId: 'editable', ...data, expectedShipment: editable.shipment }), /不能修改/)
  editable.fulfillmentStatus = 'SHIPPED'; editable.shipment.syncStatus = 'PENDING'; editable.shipment.syncStartedAt = new Date().toISOString()
  await assert.rejects(() => shippingCall('admin.order.updateShipment', { orderId: 'editable', ...data, expectedShipment: editable.shipment }), /不能修改/)
  await assert.rejects(() => shippingCall('admin.order.resyncShipment', { orderId: 'editable' }), /正在同步/)
  editable.shipment.syncStartedAt = new Date(Date.now() - 121000).toISOString()
  await shippingCall('admin.order.resyncShipment', { orderId: 'editable' })
  assert.equal(editable.shipment.syncStatus, 'SUCCESS'); assert.equal(editable.shippingNoticeStatus, 'SENT')
  await assert.rejects(() => shippingCall('admin.order.resyncShipment', { orderId: 'missing' }), /尚未发货/)
  console.log('Admin order filtering, pagination, complete state labels, shipment validation, correction conflicts and notice preservation passed.')
}

main().catch(error => { console.error(error); process.exitCode = 1 })
