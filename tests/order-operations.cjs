const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const assert = require('node:assert/strict')
const { webcrypto } = require('node:crypto')
const ts = require('typescript')
const root = path.resolve(__dirname, '..')

function runtime(globals = {}) {
  const cache = new Map()
  return function load(relative) {
    const filename = path.resolve(root, relative)
    if (cache.has(filename)) return cache.get(filename).exports
    const module = { exports: {} }; cache.set(filename, module)
    let source = fs.readFileSync(filename, 'utf8')
    // Keep fixture injection inside the test runtime, without a production API.
    if (filename.endsWith(path.join('admin', 'src', 'mock.ts'))) source += '\nexport function __setOrders(value: Order[]) { orders = structuredClone(value); waybills.clear() }\n'
    source = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS } }).outputText
    vm.runInNewContext(source, {
      module, exports: module.exports, console, Error, structuredClone, crypto: webcrypto, TextEncoder,
      setTimeout: (callback, delay) => setTimeout(callback, delay === 180 ? 0 : delay), clearTimeout, ...globals,
      require: name => load(path.resolve(path.dirname(filename), `${name}.ts`)),
    }, { filename })
    return module.exports
  }
}

function fixture(index, patch = {}) {
  return {
    id: `order-${index}`, orderNo: `CJ20261004${String(index).padStart(4, '0')}`, createdAt: '2026-10-04T00:00:00.000Z',
    orderStatus: 'CONFIRMED', paymentStatus: 'PAID', fulfillmentStatus: 'PREPARING', refundStatus: 'NONE', shippingNoticeStatus: 'READY',
    items: [{ name: '阳光玫瑰', skuName: '家庭装', quantity: 2, cover: '', unitPrice: 8990 }],
    address: { name: '测试收件人', phone: '013800000001', province: '浙江省', city: '杭州市', district: '西湖区', detail: '文三路1号' },
    quote: { subtotal: 17980, freight: 1200, discount: 1000, total: 18180 }, ...patch,
  }
}

function csvRows(content) {
  const rows = [], row = []; let cell = '', quoted = false
  const source = content.replace(/^\uFEFF/, '')
  for (let i = 0; i < source.length; i++) {
    const c = source[i]
    if (c === '"') {
      if (quoted && source[i + 1] === '"') { cell += '"'; i++ } else quoted = !quoted
    } else if (c === ',' && !quoted) { row.push(cell); cell = '' }
    else if (c === '\r' && !quoted && source[i + 1] === '\n') { row.push(cell); rows.push(row.splice(0)); cell = ''; i++ }
    else cell += c
  }
  assert.equal(quoted, false)
  return rows
}

async function operationsFlows() {
  const load = runtime(), helpers = load('admin/src/order-operations.ts')
  const { mockCall, __setOrders } = load('admin/src/mock.ts')
  const seed = Array.from({ length: 125 }, (_, index) => fixture(index))
  seed[1].refundStatus = 'APPLIED'
  seed[2].refundStatus = 'PARTIAL'
  seed[3].paymentStatus = 'REFUNDING'; seed[3].refundStatus = 'FAILED'
  seed[4].orderStatus = 'CLOSED'
  seed[5].paymentStatus = 'UNPAID'; seed[5].orderStatus = 'PENDING_PAYMENT'
  seed[6].fulfillmentStatus = 'UNFULFILLED'
  seed[10].fulfillmentStatus = 'RECEIVED'; seed[10].shippingNoticeStatus = 'FAILED'; seed[10].shippingNoticeError = '原通知错误'
  seed[10].shipment = { carrier: '顺丰速运', carrierCode: 'SF', trackingNo: 'SFTEST10', shippedAt: '2026-10-03T00:00:00Z', syncStatus: 'SUCCESS' }
  __setOrders(seed)
  const picking = load('admin/src/picking.ts')
  const sheet = await mockCall('admin.order.picking', { orderIds: ['order-0', 'order-2'] })
  assert.equal(sheet.length, 2)
  const summary = picking.pickingSummary(sheet)
  assert.equal(summary.length, 1)
  assert.equal(summary[0].quantity, 4)
  assert.equal(summary[0].orders.size, 2)
  assert.ok(picking.pickingCSV(sheet).content.includes('CJ202610040000'))
  await assert.rejects(mockCall('admin.order.picking', { orderIds: ['order-1'] }))
  await assert.rejects(mockCall('admin.order.picking', { orderIds: ['order-0', 'order-0'] }))
  await assert.rejects(mockCall('admin.order.picking', { orderIds: ['missing'] }))
  const exceptions = await mockCall('admin.order.search', { filter: 'exceptions' })
  assert.equal(exceptions.total, 2)
  assert.deepEqual(Array.from((await mockCall('admin.order.search', { filter: 'refund_failed' })).items, o => o.id), ['order-3'])
  assert.deepEqual(Array.from((await mockCall('admin.order.search', { filter: 'notice_exception' })).items, o => o.id), ['order-10'])
  assert.equal((await mockCall('admin.order.search', { filter: 'exceptions', keyword: 'not-found' })).total, 0)
  const item = (index, trackingNo = `SFTEST${index}`) => ({ orderId: seed[index].id, carrier: '顺丰速运', carrierCode: 'SF', trackingNo })
  assert.equal(helpers.orderFilter('shipping'), 'shipping_pending')
  assert.equal(helpers.orderFilter('shipped'), 'shipped')
  assert.equal(helpers.canShip(seed[0]), true)
  assert.equal(helpers.canShip(seed[1]), false)
  assert.equal(helpers.canShip(seed[2]), true)
  assert.equal(helpers.canShip(seed[3]), false)
  assert.equal(helpers.canShip(seed[4]), false)
  assert.equal(helpers.canShip(seed[5]), false)
  assert.equal(helpers.canShip(seed[6]), true)
  assert.equal(helpers.validateShipmentItems([item(0)]).length, 0)
  assert.match(helpers.validateShipmentItems([item(0), item(0, 'SFOTHER')]).join('；'), /订单重复/)
  assert.match(helpers.validateShipmentItems([item(0, 'SfSame'), item(2, 'sfsame')]).join('；'), /运单号重复/)
  assert.equal(helpers.validateShipmentItems([item(0, 'SF-SAME'), { ...item(2, 'SF-SAME'), carrierCode: 'ZTO' }]).length, 0)
  assert.match(helpers.validateShipmentItems([{ ...item(0), trackingNo: 'A'.repeat(51) }]).join('；'), /运单号/)
  assert.match(helpers.validateShipmentItems([{ ...item(0), carrier: '葡'.repeat(41) }]).join('；'), /快递公司名称/)
  assert.match(helpers.validateShipmentItems([{ ...item(0), carrier: '<script>' }]).join('；'), /快递公司名称/)
  assert.match(helpers.validateShipmentItems([{ ...item(0), orderId: '恶意订单' }]).join('；'), /订单ID/)
  assert.match(helpers.validateShipmentItems(Array.from({ length: 51 }, (_, i) => item(i))).join('；'), /50/)

  await assert.rejects(() => mockCall('admin.order.batchShip', { items: [item(0), item(0, 'OTHER123')] }), /重复/)
  await assert.rejects(() => mockCall('admin.order.batchShip', { items: [item(0, 'SFSAME'), item(2, 'sfsame')] }), /重复/)
  await assert.rejects(() => mockCall('admin.order.batchShip', { items: [item(0), null] }), /订单ID/)
  assert.equal((await mockCall('admin.order.list')).find(order => order.id === seed[0].id).fulfillmentStatus, 'PREPARING')
  const result = await mockCall('admin.order.batchShip', { items: [item(0), item(1)] })
  assert.equal(result.succeeded, 1); assert.equal(result.failed, 1)
  assert.equal(result.results[0].success, true); assert.match(result.results[1].error, /不能发货/)
  const firstShipment = (await mockCall('admin.order.list')).find(order => order.id === seed[0].id)
  assert.equal(firstShipment.fulfillmentStatus, 'SHIPPED'); assert.equal(firstShipment.shippingNoticeStatus, 'SENT')
  const retry = await mockCall('admin.order.batchShip', { items: [{ ...item(0), carrierCode: 'sf', trackingNo: 'sftest0' }, item(1)] })
  assert.equal(retry.succeeded, 1); assert.equal(retry.failed, 1)
  const afterRetry = (await mockCall('admin.order.list')).find(order => order.id === seed[0].id)
  assert.equal(afterRetry.shipment.shippedAt, firstShipment.shipment.shippedAt)
  assert.equal(afterRetry.shippingNoticeStatus, firstShipment.shippingNoticeStatus)
  await assert.rejects(() => mockCall('admin.order.ship', item(0, 'CHANGED123')), /不能覆盖/)
  const reused = await mockCall('admin.order.batchShip', { items: [item(2, 'SFTEST0'), item(6, 'SFVALID6')] })
  assert.equal(reused.succeeded, 1); assert.equal(reused.failed, 1); assert.match(reused.results[0].error, /其他订单/)
  const invalidRow = await mockCall('admin.order.batchShip', { items: [{ ...item(2), trackingNo: 'A'.repeat(51) }, item(7)] })
  assert.equal(invalidRow.succeeded, 1); assert.equal(invalidRow.failed, 1)
  const receivedRetry = await mockCall('admin.order.batchShip', { items: [item(10)] })
  assert.equal(receivedRetry.succeeded, 1)
  const receivedOrder = (await mockCall('admin.order.list')).find(order => order.id === seed[10].id)
  assert.equal(receivedOrder.shippingNoticeStatus, 'FAILED'); assert.equal(receivedOrder.shippingNoticeError, '原通知错误')
  assert.equal(receivedOrder.fulfillmentStatus, 'RECEIVED')

  const config = await mockCall('admin.waybill.config')
  assert.equal(config.configured, true); assert.ok(config.carriers.some(carrier => carrier.code === 'YD'))
  assert.equal(await mockCall('admin.waybill.get', { orderId: seed[8].id }), null)
  await assert.rejects(() => mockCall('admin.waybill.create', { orderId: seed[1].id, carrierCode: 'SF', weight: 2 }), /不能申请/)
  await assert.rejects(() => mockCall('admin.waybill.create', { orderId: seed[8].id, carrierCode: 'SF', weight: 0 }), /重量/)
  const [label, retryLabel] = await Promise.all([mockCall('admin.waybill.create', { orderId: seed[8].id, carrierCode: 'SF', weight: 2 }), mockCall('admin.waybill.create', { orderId: seed[8].id, carrierCode: 'SF', weight: 2 })])
  assert.equal(label.status, 'READY'); assert.equal(label.demo, true); assert.equal(label.labelUrl, '')
  assert.equal(label.trackingNo, retryLabel.trackingNo); assert.equal(label.id, retryLabel.id)
  assert.equal((await mockCall('admin.order.list')).find(order => order.id === seed[8].id).fulfillmentStatus, 'PREPARING')
  assert.equal((await mockCall('admin.waybill.label', { orderId: seed[8].id })).demo, true)
  await assert.rejects(() => mockCall('admin.order.ship', item(8)), /已生成电子面单/)
  await assert.rejects(() => mockCall('admin.waybill.create', { orderId: seed[8].id, carrierCode: 'YTO', weight: 2 }), /其他承运商/)
  const labelShipped = await mockCall('admin.order.batchShip', { items: [{ orderId: seed[8].id, carrier: label.carrier, carrierCode: label.carrierCode, trackingNo: label.trackingNo }] })
  assert.equal(labelShipped.succeeded, 1)
  const shippedLabelOrder = (await mockCall('admin.order.list')).find(order => order.id === seed[8].id)
  assert.equal(shippedLabelOrder.shipment.trackingNo, label.trackingNo)
  await assert.rejects(() => mockCall('admin.order.updateShipment', { ...item(8, 'CHANGEDLABEL8'), expectedShipment: shippedLabelOrder.shipment }), /已生成电子面单/)

  const firstPage = await mockCall('admin.order.list', { limit: 50 })
  assert.equal(firstPage.length, 50)
  const allExport = await mockCall('admin.order.export', { filter: 'all', limit: 50 })
  assert.equal(allExport.count, 125); assert.equal(csvRows(allExport.content).length, 126)
  assert.equal(allExport.content.charCodeAt(0), 0xFEFF)
  const pendingPage = await mockCall('admin.order.search', { filter: 'shipping_pending', limit: 50, keyword: ' 阳光 家庭 ' })
  const pendingExport = await mockCall('admin.order.export', { filter: 'shipping_pending', keyword: ' 阳光 家庭 ' })
  assert.equal(pendingExport.count, pendingPage.total); assert.ok(pendingExport.count > pendingPage.items.length)
  const advanced = await mockCall('admin.order.export', { refundStatus: 'PARTIAL', keyword: '测试收件人' })
  assert.equal(advanced.count, 1)
  const selected = await mockCall('admin.order.export', { orderIds: [seed[0].id, seed[124].id], filter: 'closed', keyword: '不存在' })
  assert.equal(selected.count, 2)
  await assert.rejects(() => mockCall('admin.order.export', { orderIds: [] }), /1至500/)
  await assert.rejects(() => mockCall('admin.order.export', { orderIds: [seed[0].id, seed[0].id] }), /不同/)
  await assert.rejects(() => mockCall('admin.order.export', { orderIds: ['unknown'] }), /不存在/)
  await assert.rejects(() => mockCall('admin.order.export', { filter: 'unknown' }), /筛选/)
  __setOrders(Array.from({ length: 10001 }, (_, index) => fixture(index)))
  await assert.rejects(() => mockCall('admin.order.export', {}), /10000/)
}

function csvSafetyFlows() {
  const { exportOrders, csvCell } = runtime()('admin/src/order-operations.ts')
  const order = fixture(1, {
    orderNo: '000001', note: '\t=2+2',
    address: { name: ' =HYPERLINK("https://unsafe.example")', phone: '0013800000000', province: '浙江省', city: '杭州市', district: '西湖区', detail: 'a,"b"\nc' },
    invoice: { kind: 'company', title: '@SUM(1,2)', taxId: '0012345', email: 'test@example.com', status: 'APPLIED', number: '000009', amount: 18180 },
    shipment: { carrier: '顺丰速运', carrierCode: 'SF', trackingNo: '0000001234', shippedAt: '2026-10-04T01:00:00Z', syncStatus: 'SUCCESS' },
  })
  const result = exportOrders([order]), rows = csvRows(result.content), headers = rows[0], row = rows[1]
  assert.equal(headers.length, 32); assert.equal(row.length, 32)
  const column = name => row[headers.indexOf(name)]
  assert.equal(column('订单编号'), "'000001")
  assert.equal(column('手机号'), "'0013800000000")
  assert.equal(column('收件人'), "' =HYPERLINK(\"https://unsafe.example\")")
  assert.equal(column('详细地址'), "'a,\"b\"\nc")
  assert.equal(column('订单备注'), "'\t=2+2")
  assert.equal(column('税号'), "'0012345")
  assert.equal(column('发票抬头'), "'@SUM(1,2)")
  assert.equal(column('发票号码'), "'000009")
  assert.equal(column('发票状态'), '待开票')
  assert.equal(column('运单号'), "'0000001234")
  assert.equal(column('实付金额（元）'), '181.80')
  assert.equal(column('下单时间'), '2026-10-04 08:00:00')
  assert.equal(column('发货时间'), '2026-10-04 09:00:00')
  assert.equal(csvCell('\u0000=1+1'), '"\'=1+1"')
  assert.equal(csvCell('\uFEFF-1+2'), '"\'\uFEFF-1+2"')
  assert.equal(csvCell('2e10'), '"\'2e10"')
  assert.equal(csvCell('-1.00', false), '"-1.00"')
}

function downloadFlows() {
  const clicks = [], removed = [], scheduled = [], revoked = [], body = []
  const load = runtime({
    Blob: class { constructor(parts, options) { this.parts = parts; this.options = options } },
    URL: { createObjectURL: blob => { assert.equal(blob.options.type, 'text/csv;charset=utf-8'); return 'blob:test' }, revokeObjectURL: url => revoked.push(url) },
    document: { body: { appendChild: link => body.push(link) }, createElement: name => { assert.equal(name, 'a'); return { click() { clicks.push(this.href) }, remove() { removed.push(this.download) } } } },
    setTimeout: callback => scheduled.push(callback),
  })
  load('admin/src/order-operations.ts').downloadCSV({ filename: '订单导出.csv', content: '\uFEFFa,b\r\n', count: 0 })
  assert.deepEqual(clicks, ['blob:test']); assert.deepEqual(removed, ['订单导出.csv']); assert.equal(body[0].hidden, true)
  assert.equal(revoked.length, 0); scheduled[0](); assert.deepEqual(revoked, ['blob:test'])
}

;(async () => {
  await operationsFlows(); csvSafetyFlows(); downloadFlows()
  console.log('Batch shipping partial success/retries, refunds, electronic waybill reuse, filtered full export and CSV safety flows passed.')
})().catch(error => { console.error(error); process.exitCode = 1 })
