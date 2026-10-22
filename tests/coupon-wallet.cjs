const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const assert = require('node:assert/strict')
const ts = require('typescript')
const root = path.resolve(__dirname, '..')
const storage = new Map()
let page, destination, selected
const wx = {
  getStorageSync: key => structuredClone(storage.get(key)),
  setStorageSync: (key, value) => storage.set(key, structuredClone(value)),
  removeStorageSync: key => storage.delete(key), showToast() {}, stopPullDownRefresh() {},
  navigateTo: options => { destination = options }, navigateBack() {},
}
const cache = new Map()
function load(relative) {
  const filename = path.resolve(root, relative)
  if (cache.has(filename)) return cache.get(filename).exports
  const module = { exports: {} }; cache.set(filename, module)
  const source = ts.transpileModule(fs.readFileSync(filename, 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS } }).outputText
  vm.runInNewContext(source, { module, exports: module.exports, console, Error, structuredClone, setTimeout, clearTimeout, wx, getApp: () => ({ globalData: { demoMode: true } }),
    Page: spec => { page = spec; page.setData = (updates, callback) => { Object.assign(page.data, updates); callback?.() }; page.getOpenerEventChannel = () => ({ emit: (_, value) => { selected = value } }) },
    require: name => load(path.resolve(path.dirname(filename), `${name}.ts`)),
  }, { filename })
  return module.exports
}
async function run() {
  const { callApi } = load('miniprogram/utils/api.ts')
  const { bestCoupon, demoCoupons } = load('miniprogram/utils/coupons.ts')
  assert.equal((await callApi('coupon.wallet')).length, 0)
  await assert.rejects(() => callApi('coupon.redeem', { code: 'UNKNOWN' }), /不存在/)
  await Promise.all([callApi('coupon.redeem', { code: ' demo10 ' }), callApi('coupon.redeem', { code: 'DEMO10' })])
  await callApi('coupon.redeem', { code: 'DEMO20' })
  assert.equal((await callApi('coupon.wallet')).length, 2)
  const p = (await callApi('product.list'))[0]
  const items = [{ productId: p.id, skuId: p.skus[0].id, quantity: 4, selected: true }]
  const address = { name: '测试', phone: '13800000000', province: '浙江省', city: '杭州市', detail: '1号' }
  const q = await callApi('order.quote', { items, address, autoCoupon: true })
  assert.ok(q.subtotal >= 19900)
  assert.equal(q.couponCode, 'DEMO20'); assert.equal(q.discount, 2000)
  const noCoupon = await callApi('order.quote', { items, address })
  assert.equal(q.freight, noCoupon.freight); assert.equal(q.total, noCoupon.total - 2000)
  const manual = await callApi('order.quote', { items, address, couponCode: 'DEMO10' })
  assert.equal(manual.discount, 1000)
  const order = await callApi('order.create', { items, address, couponCode: q.couponCode, idempotencyKey: 'wallet_checkout_0001' })
  assert.equal((await callApi('coupon.wallet')).find(c => c.code === 'DEMO20').status, 'RESERVED')
  assert.equal((await callApi('order.quote', { items, address, autoCoupon: true })).couponCode, 'DEMO10')
  await assert.rejects(() => callApi('order.create', { items, address, couponCode: q.couponCode, idempotencyKey: 'wallet_checkout_0002' }), /占用/)
  await callApi('order.cancel', { id: order.id })
  assert.equal((await callApi('coupon.wallet')).find(c => c.code === 'DEMO20').status, 'AVAILABLE')
  const paid = await callApi('order.create', { items, address, couponCode: 'DEMO20', idempotencyKey: 'wallet_checkout_0003' })
  await callApi('payment.create', { orderId: paid.id })
  assert.equal((await callApi('coupon.wallet')).find(c => c.code === 'DEMO20').status, 'USED')
  const wallet = [{ ...demoCoupons[0], minimum: 0, endDate: '2099-01-01', status: 'AVAILABLE' }, { ...demoCoupons[1], minimum: 0, status: 'AVAILABLE' }]
  assert.equal(bestCoupon(wallet, 500), 'DEMO10') // Both capped to 499; earlier expiry wins.
  assert.equal(bestCoupon(wallet, 1), '')
  wallet[0].status = 'RESERVED'; wallet[1].status = 'USED'
  assert.equal(bestCoupon(wallet, 500), '')
  load('miniprogram/pages/coupons/index.ts')
  page.onLoad({ select: '1', subtotal: String(q.subtotal) }); await page.load()
  assert.equal(page.data.visible.length, 1)
  page.choose({ currentTarget: { dataset: { code: 'DEMO20' } } }); assert.equal(selected, undefined)
  page.choose({ currentTarget: { dataset: { code: 'DEMO10' } } }); assert.equal(selected.code, 'DEMO10')
  page.filter({ currentTarget: { dataset: { filter: 'other' } } }); assert.equal(page.data.visible.length, 1)
  // A new page instance reads persisted ownership and live order status.
  cache.delete(path.resolve(root, 'miniprogram/pages/coupons/index.ts'))
  load('miniprogram/pages/coupons/index.ts'); await page.load(); assert.equal(page.data.coupons.length, 2)
  load('miniprogram/pages/checkout/index.ts')
  page.setData({ items, address }); await page.refreshQuote()
  assert.equal(page.data.couponCode, 'DEMO10')
  page.clearCoupon(); await page.refreshQuote(); assert.equal(page.data.couponCode, '')
  page.matchCoupon(); await page.refreshQuote(); assert.equal(page.data.couponCode, 'DEMO10')
  page.wallet(); destination.events.couponSelected({ code: 'DEMO10' }); await page.refreshQuote()
  assert.equal(page.data.autoCoupon, false)
  page.setData({ couponInput: 'DEMO20' }); await page.useCoupon(); assert.match(page.data.quoteError, /占用/)
  page.matchCoupon(); await page.refreshQuote(); assert.equal(page.data.couponCode, 'DEMO10')
  // Older quotes must not overwrite the latest explicit choice.
  page.setData({ autoCoupon: true }); const older = page.refreshQuote()
  page.setData({ autoCoupon: false, couponCode: '' }); await page.refreshQuote(); await older
  assert.equal(page.data.quote.couponCode, '')
  console.log('Coupon wallet: redemption, persistence, occupancy/release, actual savings, manual/automatic selection and stale quotes passed')
}
run().catch(error => { console.error(error); process.exitCode = 1 })
