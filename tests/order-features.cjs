const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const assert = require('node:assert/strict')
const ts = require('typescript')
const root = path.resolve(__dirname, '..')

function runtime(globals) {
  const cache = new Map()
  return function load(relative) {
    const filename = path.resolve(root, relative)
    if (cache.has(filename)) return cache.get(filename).exports
    const module = { exports: {} }; cache.set(filename, module)
    const source = ts.transpileModule(fs.readFileSync(filename, 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS } }).outputText
    vm.runInNewContext(source, { module, exports: module.exports, console, Error, setTimeout, clearTimeout, structuredClone, ...globals, require: name => load(path.resolve(path.dirname(filename), `${name}.ts`)) }, { filename })
    return module.exports
  }
}

async function run() {
  let page, destination, confirmation = true, mode = true, rejected = false, subscribeCalls = 0
  const storage = new Map(), calls = []
  const wx = {
    getStorageSync: key => structuredClone(storage.get(key)), setStorageSync: (key, value) => storage.set(key, structuredClone(value)), removeStorageSync: key => storage.delete(key),
    showToast: () => {}, showModal: async () => ({ confirm: confirmation }),
    navigateTo: ({ url }) => { destination = url }, switchTab: ({ url }) => { destination = url },
    requestSubscribeMessage: async ({ tmplIds }) => { subscribeCalls++; return { [tmplIds[0]]: rejected ? 'reject' : 'accept' } },
    cloud: { callContainer: async ({ data: { action, data } }) => {
      calls.push({ action, data })
      if (action === 'order.shippingSubscription.accept') return { statusCode: 200, data: { ok: true, data: { enabled: true, templateId: 'real-template', status: 'READY' } } }
      if (action === 'order.search') {
        const delay = data.keyword === 'old' ? 40 : 1
        await new Promise(resolve => setTimeout(resolve, delay))
        const result = data.keyword === 'old' ? seed.slice(0,1) : seed.slice(1,2)
        return { statusCode: 200, data: { ok: true, data: { items: result, total: result.length, offset: data.offset, limit: data.limit } } }
      }
      throw new Error(`unexpected action ${action}`)
    } },
  }
  const load = runtime({ wx, getApp: () => ({ globalData: { demoMode: mode } }), Page: spec => {
    page = spec; page.setData = (updates, callback) => { Object.assign(page.data, updates); callback?.() }
  } })
  const { callApi } = load('miniprogram/utils/api.ts')
  const { demoProducts } = load('miniprogram/data/demo.ts')
  const product = demoProducts[0], sku = product.skus[0]
  const seed = Array.from({ length: 25 }, (_, i) => ({
    id: `order-${i}`, orderNo: `CJTEST${i}`, createdAt: '2026-10-04T00:00:00Z', orderStatus: 'CONFIRMED', paymentStatus: 'PAID', fulfillmentStatus: i === 24 ? 'SHIPPED' : 'PREPARING', refundStatus: 'NONE',
    items: [{ productId: product.id, skuId: sku.id, name: '阳光玫瑰', skuName: '家庭装', quantity: 2, selected: true, product: { ...product, skus: [] }, sku: { ...sku, price: 1 } }],
    address: { name: '测试', phone: '13800000000', province: '浙江省', city: '杭州市', detail: '1号' }, quote: { subtotal: sku.price * 2, total: sku.price * 2, freight: 0, freeShippingThreshold: 19900 },
    ...(i === 24 ? { shipment: { carrier: '演示快递', trackingNo: 'DEMOEXISTING', shippedAt: '2026-10-04T01:00:00Z' } } : {}),
  }))
  storage.set('chiji_demo_orders', seed)
  assert.equal((await callApi('order.list', { keyword: 'cjtest24', limit: 20 }))[0].id, 'order-24')
  assert.equal((await callApi('order.list', { keyword: '阳光 家庭', filter: 'shipping' })).length, 1)
  assert.equal((await callApi('order.list', { keyword: '%_' })).length, 0)
  assert.equal((await callApi('order.list', { offset: 20, limit: 20 })).length, 5)
  const searchPage = await callApi('order.search', { keyword: '阳光 家庭', limit: 20 })
  assert.equal(searchPage.total, 25); assert.equal(searchPage.items.length, 20); assert.equal(searchPage.offset, 0); assert.equal(searchPage.limit, 20)
  assert.equal((await callApi('order.search', { offset: 20, limit: 20 })).items.length, 5)
  assert.equal((await callApi('order.search', { limit: 1000 })).limit, 100)
  await assert.rejects(() => callApi('order.search', { filter: 'invalid' }), /筛选状态/)

  const { orderPresentation, orderStatusText } = load('miniprogram/utils/format.ts')
  const { matchesOrder } = load('miniprogram/utils/order-filter.ts')
  const states = [
    [{ orderStatus: 'CANCELLED', paymentStatus: 'UNPAID', fulfillmentStatus: 'UNFULFILLED' }, '已取消', 'cancelled'],
    [{ orderStatus: 'CLOSED', paymentStatus: 'UNPAID', fulfillmentStatus: 'UNFULFILLED' }, '已关闭', 'closed'],
    [{ orderStatus: 'CONFIRMED', fulfillmentStatus: 'RECEIVED' }, '已收货', 'received'],
    [{ orderStatus: 'COMPLETED', fulfillmentStatus: 'RECEIVED' }, '已完成', 'completed'],
    [{ paymentStatus: 'REFUNDING' }, '退款处理中', 'refund'],
    [{ refundStatus: 'SUCCEEDED' }, '已退款', 'refund'],
    [{ refundStatus: 'PARTIAL' }, '已部分退款', 'refund'],
    [{ refundStatus: 'APPLIED' }, '退款审核中', 'refund'],
    [{ refundStatus: 'APPROVED' }, '退款处理中', 'refund'],
    [{ refundStatus: 'REJECTED' }, '退款未通过', 'refund'],
    [{ refundStatus: 'FAILED' }, '退款异常待处理', 'refund'],
    [{ paymentStatus: 'REFUNDED' }, '已退款', 'refund'],
  ]
  for (const [patch, label, filter] of states) {
    const order = { ...seed[0], ...patch }
    assert.equal(orderStatusText(order), label)
    assert.equal(matchesOrder(order, '', filter), true)
    const presentation = orderPresentation(order)
    for (const key of ['orderStateText', 'paymentStateText', 'fulfillmentStateText', 'refundStateText']) assert.doesNotMatch(presentation[key], /待确认/, `${key} was missing for ${JSON.stringify(patch)}`)
    if (['CANCELLED', 'CLOSED', 'COMPLETED'].includes(order.orderStatus) || order.fulfillmentStatus === 'RECEIVED') {
      assert.equal(presentation.canPay, false); assert.equal(presentation.canConfirm, false)
      assert.equal(matchesOrder(order, '', 'paid'), false); assert.equal(matchesOrder(order, '', 'shipping'), false)
      assert.doesNotMatch(presentation.statusHint, /30 分钟|正在路上/)
    }
  }
  assert.equal(matchesOrder({ ...seed[0], refundStatus: 'APPLIED' }, '', 'paid'), false)
  assert.equal(matchesOrder({ ...seed[24], paymentStatus: 'REFUNDED', refundStatus: 'SUCCEEDED' }, '', 'shipping'), false)
  assert.equal(orderStatusText({ ...seed[24], orderStatus: 'CLOSED' }), '已关闭')
  assert.equal(orderStatusText({ ...seed[0], orderStatus: 'UNKNOWN', fulfillmentStatus: 'UNKNOWN' }), '订单状态待确认')

  const { buyAgain } = load('miniprogram/utils/orders.ts')
  await buyAgain(seed[0]); assert.equal(destination, '/pages/checkout/index')
  storage.set('chiji_demo_cart', [{ productId: product.id, skuId: sku.id, quantity: 5, selected: true }])
  load('miniprogram/pages/checkout/index.ts'); await page.onLoad()
  assert.equal(page.data.items[0].quantity, 2)
  assert.equal(page.data.items[0].sku.price, sku.price)
  assert.equal(page.data.couponCode, ''); assert.equal(page.data.deliveryDate, '')
  await new Promise(resolve => setTimeout(resolve, 150))

  const changed = structuredClone(seed[0]); changed.items[0].quantity = sku.limitPerOrder + 1
  changed.items.push({ productId: 'gone', skuId: 'gone', quantity: 1, name: '下架商品', selected: true })
  destination = ''; confirmation = false; await buyAgain(changed); assert.equal(destination, '')
  confirmation = true; await buyAgain(changed)
  assert.equal(storage.get('chiji_checkout_items').length, 1)
  assert.equal(storage.get('chiji_checkout_items')[0].quantity, Math.min(sku.stock, sku.limitPerOrder))
  await assert.rejects(() => buyAgain({ ...seed[0], items: [changed.items[1]] }), /均无法购买/)

  await callApi('order.shippingSubscription.accept', { orderId: 'order-0' })
  assert.equal((await callApi('order.shippingSubscription.get', { orderId: 'order-0' })).status, 'READY')
  await callApi('order.demoShip', { orderId: 'order-0' })
  assert.equal((await callApi('order.shippingSubscription.get', { orderId: 'order-0' })).status, 'SENT')
  assert.equal((await callApi('shippingNotice.list')).unread, 2)
  await callApi('shippingNotice.read', { orderId: 'order-0' }); await callApi('shippingNotice.read', { orderId: 'order-0' })
  assert.equal((await callApi('shippingNotice.list')).unread, 1)
  await assert.rejects(() => callApi('order.demoShip', { orderId: 'order-0' }), /不能演示发货/)
  load('miniprogram/pages/shipping-notices/index.ts'); await page.onShow()
  assert.equal(page.data.items.length, 2); assert.equal(page.data.unread, 1)
  page.open({ currentTarget: { dataset: { id: 'order-24' } } }); assert.equal(destination, '/pages/order-detail/index?id=order-24')

  mode = false; storage.set('chiji_mini_token', 'test-token')
  load('miniprogram/pages/orders/index.ts')
  page.data.keyword = 'old'; const old = page.load()
  page.data.keyword = 'new'; const current = page.load()
  await Promise.all([old, current]); assert.equal(page.data.orders[0].id, 'order-1')
  assert.equal(page.data.loading, false)
  load('miniprogram/pages/order-detail/index.ts')
  page.data.order = seed[1]; page.data.subscription = { enabled: true, templateId: 'real-template', status: 'NONE' }
  rejected = true; await page.subscribeShipping()
  assert.equal(calls.filter(c => c.action === 'order.shippingSubscription.accept').length, 0)
  assert.match(page.data.subscriptionError, /未开启/)
  rejected = false; await Promise.all([page.subscribeShipping(), page.subscribeShipping()])
  assert.equal(subscribeCalls, 2)
  assert.equal(calls.filter(c => c.action === 'order.shippingSubscription.accept').length, 1)
  assert.equal(page.data.subscription.status, 'READY')
  await paginationFlows(seed)
  await detailFlows(seed)
  await profileCountFlows(seed)
  console.log('Order search metadata, pagination, retry/query consistency, stale responses, complete state display, shipment refresh, rebuy, notices and subscription consent passed.')
}

async function paginationFlows(seed) {
  let page
  const pending = []
  const wx = {
    getStorageSync: () => 'token', stopPullDownRefresh: () => {},
    cloud: { callContainer: ({ data }) => new Promise((resolve, reject) => pending.push({ ...data, resolve, reject })) },
  }
  runtime({ wx, getApp: () => ({ globalData: { demoMode: false } }), Page: spec => { page = spec; page.setData = values => Object.assign(page.data, values) } })('miniprogram/pages/orders/index.ts')
  const respond = (index, items, total, offset = 0) => pending[index].resolve({ statusCode: 200, data: { ok: true, data: { items, total, offset, limit: 20 } } })
  page.onLoad({ filter: 'unknown' }); assert.equal(page.data.filter, 'all')
  page.input({ detail: { value: '阳光' } }); let job = page.search()
  respond(0, seed.slice(0,20), 25); await job
  assert.equal(page.data.total, 25); assert.equal(page.data.hasMore, true)
  page.input({ detail: { value: '尚未搜索的新输入' } }); job = page.loadMore()
  await page.loadMore(); assert.equal(pending.length, 2, 'duplicate bottom events requested the same page')
  assert.equal(pending[1].data.keyword, '阳光'); assert.equal(pending[1].data.offset, 20)
  respond(1, seed.slice(20), 25, 20); await job
  assert.equal(page.data.orders.length, 25); assert.equal(page.data.hasMore, false)
  await page.loadMore(); assert.equal(pending.length, 2)

  job = page.onShow(); assert.equal(pending[2].data.keyword, '阳光')
  respond(2, seed.slice(0,20), 20); await job
  assert.equal(page.data.hasMore, false, 'an exact full page triggered an unnecessary request')

  job = page.search(); pending[3].reject(new Error('首次搜索失败')); await job
  assert.equal(page.data.orders.length, 0); assert.equal(page.data.failedMore, false)
  page.input({ detail: { value: '其他草稿' } }); job = page.retry()
  assert.equal(pending[4].data.keyword, '尚未搜索的新输入')
  respond(4, seed.slice(0,20), 25); await job
  job = page.loadMore(); pending[5].reject(new Error('分页失败')); await job
  assert.equal(page.data.orders.length, 20); assert.equal(page.data.offset, 20); assert.equal(page.data.failedMore, true)
  job = page.retry(); assert.equal(pending[6].data.offset, 20); assert.equal(pending[6].data.keyword, '尚未搜索的新输入')
  respond(6, [seed[19], ...seed.slice(20)], 26, 20); await job
  assert.equal(page.data.orders.length, 25, 'overlapping page rows were duplicated'); assert.equal(page.data.hasMore, false)

  page.data.keyword = 'old'; const stale = page.search()
  page.data.keyword = 'new'; const current = page.tab({ currentTarget: { dataset: { key: 'shipping' } } })
  assert.equal(page.data.orders.length, 0, 'old rows remained under a new filter')
  respond(8, [seed[24]], 1); await current
  pending[7].reject(new Error('旧查询失败')); await stale
  assert.equal(page.data.error, ''); assert.equal(page.data.orders[0].id, seed[24].id); assert.equal(page.data.loading, false)
}

async function detailFlows(seed) {
  let page, order = structuredClone(seed[24]), fail = false
  const tracking = []
  const wx = {
    getStorageSync: () => 'token', showToast: () => {},
    cloud: { callContainer: async ({ data: { action } }) => {
      if (action === 'order.get') { if (fail) throw new Error('详情加载失败'); return { statusCode: 200, data: { ok: true, data: structuredClone(order) } } }
      if (action === 'order.tracking') return new Promise(resolve => tracking.push(resolve))
      return { statusCode: 200, data: { ok: true, data: { enabled: false, status: 'NONE' } } }
    } },
  }
  runtime({ wx, getApp: () => ({ globalData: { demoMode: false } }), Page: spec => { page = spec; page.setData = values => Object.assign(page.data, values) } })('miniprogram/pages/order-detail/index.ts')
  page.onLoad({ id: order.id }); await page.load()
  page.data.reviewContent = '尚未提交的评价'
  assert.equal(page.data.order.statusText, '运输中'); assert.equal(page.data.order.canConfirm, true)
  order.shipment.trackingNo = 'UPDATED'; await page.load()
  assert.equal(tracking.length, 2, 'changed shipment kept the old tracking query')
  const respond = (index, message) => tracking[index]({ statusCode: 200, data: { ok: true, data: { message, status: 'OK', events: [] } } })
  respond(1, '新运单'); await new Promise(resolve => setTimeout(resolve, 0))
  respond(0, '旧运单'); await new Promise(resolve => setTimeout(resolve, 0))
  assert.equal(page.data.tracking.message, '新运单'); assert.equal(page.data.trackingLoading, false)
  assert.equal(page.data.reviewContent, '尚未提交的评价')
  fail = true; await page.load(); assert.match(page.data.loadError, /详情加载失败/); assert.equal(page.data.loading, false)
  assert.equal(page.data.order.shipment.trackingNo, 'UPDATED')
  fail = false; order.orderStatus = 'CANCELLED'; await page.load()
  assert.equal(page.data.loadError, ''); assert.equal(page.data.order.canConfirm, false); assert.equal(page.data.order.canPay, false)
  assert.equal(page.data.order.statusText, '已取消'); assert.doesNotMatch(page.data.order.statusHint, /正在路上/)
}

async function profileCountFlows(seed) {
  let page
  const orders = [
    ...Array.from({ length: 125 }, (_, i) => ({ ...seed[0], id: `volume-${i}` })),
    ...Array.from({ length: 3 }, (_, i) => ({ ...seed[24], id: `shipping-${i}` })),
    { ...seed[0], id: 'pending', orderStatus: 'PENDING_PAYMENT', paymentStatus: 'UNPAID' },
    { ...seed[0], id: 'cancelled', orderStatus: 'CANCELLED', paymentStatus: 'UNPAID' },
    { ...seed[0], id: 'refund', refundStatus: 'APPLIED' },
  ]
  const storage = new Map([['chiji_demo_orders', orders]])
  const wx = { getStorageSync: key => structuredClone(storage.get(key)), setStorageSync: (key, value) => storage.set(key, structuredClone(value)) }
  runtime({ wx, getApp: () => ({ globalData: { demoMode: true } }), Page: spec => { page = spec; page.setData = values => Object.assign(page.data, values) } })('miniprogram/pages/profile/index.ts')
  await page.onShow()
  assert.equal(page.data.counts.paid, 125, 'profile counts were truncated to the first order page')
  assert.equal(page.data.counts.pending, 1); assert.equal(page.data.counts.shipping, 3); assert.equal(page.data.counts.refund, 1)
}

run().catch(error => { console.error(error); process.exitCode = 1 })
