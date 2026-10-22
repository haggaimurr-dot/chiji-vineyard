const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const assert = require('node:assert/strict')
const ts = require('typescript')
const root = path.resolve(__dirname, '..')
function runtime(globals = {}) {
  const cache = new Map()
  return function load(relative) {
    const file = path.resolve(root, relative)
    if (cache.has(file)) return cache.get(file).exports
    const module = { exports: {} }; cache.set(file, module)
    const source = ts.transpileModule(fs.readFileSync(file, 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS } }).outputText
    vm.runInNewContext(source, { module, exports: module.exports, console, setTimeout, clearTimeout, structuredClone, ...globals, require: name => load(path.resolve(path.dirname(file), `${name}.ts`)) }, { filename: file })
    return module.exports
  }
}
async function main() {
  const storage = new Map()
  const wx = { getStorageSync: key => structuredClone(storage.get(key)), setStorageSync: (key, value) => storage.set(key, structuredClone(value)) }
  const load = runtime({ wx, getApp: () => ({ globalData: { demoMode: true } }) })
  const { refundCents, canApplyRefund } = load('miniprogram/utils/refund.ts')
  assert.equal(refundCents('0.01', 100), 1)
  assert.equal(refundCents('12.30', 2000), 1230)
  for (const value of ['0', '-1', '1.001', '1e2', '', '20.01']) assert.throws(() => refundCents(value, 2000))
  const { callApi } = load('miniprogram/utils/api.ts')
  const order = { id: 'o', orderNo: 'CJ1', paymentStatus: 'PAID', fulfillmentStatus: 'SHIPPED', orderStatus: 'CONFIRMED', refundStatus: 'NONE', quote: { total: 10000 }, items: [] }
  wx.setStorageSync('chiji_demo_orders', [order, { ...order, id: 'other' }])
  assert.equal(canApplyRefund(order), true)
  await assert.rejects(() => callApi('refund.apply', { orderId: 'o', amount: 2000, reason: '坏果' }), /举证图片/)
  const image = await callApi('refund.evidence.upload', { orderId: 'o', dataUrl: 'data:image/png;base64,iVBORw0KGgo=' })
  assert.equal((await callApi('refund.evidence.get', { id: image.id })).dataUrl, 'data:image/png;base64,iVBORw0KGgo=')
  await assert.rejects(() => callApi('refund.apply', { orderId: 'other', amount: 2000, reason: '坏果', images: [image.id] }), /举证图片无效/)
  await assert.rejects(() => callApi('refund.apply', { orderId: 'o', amount: 10001, reason: '坏果', images: [image.id] }), /金额/)
  await assert.rejects(() => callApi('refund.apply', { orderId: 'o', amount: 2000, reason: '坏果', images: [image.id, image.id] }), /重复/)
  const refund = await callApi('refund.apply', { orderId: 'o', amount: 2000, reason: '坏果', images: [image.id] })
  assert.equal(refund.amount, 2000)
  assert.equal((await callApi('refund.list', { orderId: 'o' }))[0].images[0], image.id)
  await assert.rejects(() => callApi('refund.apply', { orderId: 'o', amount: 2000, reason: '坏果', images: [image.id] }), /不能申请/)
  order.refundStatus = 'PARTIAL'; order.refundedAmount = 2000; order.fulfillmentStatus = 'RECEIVED'
  wx.setStorageSync('chiji_demo_orders', [order])
  const secondImage = await callApi('refund.evidence.upload', { orderId: 'o', dataUrl: 'data:image/png;base64,iVBORw0KGgo=' })
  await assert.rejects(() => callApi('refund.apply', { orderId: 'o', amount: 8001, reason: '坏果', images: [secondImage.id] }), /金额/)
  await assert.rejects(() => callApi('refund.apply', { orderId: 'o', amount: 8000, reason: '坏果', images: [image.id] }), /举证图片无效/)
  assert.equal((await callApi('refund.apply', { orderId: 'o', amount: 8000, reason: '坏果', images: [secondImage.id] })).amount, 8000)

  let page
  const pageLoader = runtime({ wx, getApp: () => ({ globalData: { demoMode: true } }), Page: config => { page = config; page.setData = changes => Object.assign(page.data, changes) } })
  order.refundStatus='PARTIAL';order.refundedAmount=2000;wx.setStorageSync('chiji_demo_orders',[order])
  pageLoader('miniprogram/pages/refund/index.ts');page.onLoad({id:'o'});await page.load()
  page.amount({detail:{value:'12.30'}});await page.load();assert.equal(page.data.amount,'12.30','returning from photo picker overwrote partial amount')
  page.amount({detail:{value:''}});await page.load();assert.equal(page.data.amount,'','refresh overwrote cleared amount')
  page.data.imageBusy=true;await page.load();assert.equal(page.data.amount,'');page.data.imageBusy=false

  const { mockCall } = runtime()('admin/src/mock.ts')
  const shipped = (await mockCall('admin.refund.list')).find(r => r.orderId === 'o-3')
  assert.equal(shipped.amount, 2000)
  await mockCall('admin.refund.approve', { refundId: shipped.id })
  const updated = (await mockCall('admin.order.list')).find(o => o.id === shipped.orderId)
  assert.equal(updated.paymentStatus, 'PAID')
  assert.equal(updated.refundStatus, 'PARTIAL')
  assert.equal(updated.fulfillmentStatus, 'SHIPPED')
  assert.equal(updated.refundedAmount, 2000)
  await assert.rejects(() => mockCall('admin.refund.approve', { refundId: shipped.id }), /状态已变化/)
  console.log('After-sales evidence, shipped/received refunds, partial amounts, cumulative limits and duplicate submission flows passed.')
}
main().catch(e => { console.error(e); process.exitCode = 1 })
