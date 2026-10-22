const fs = require('node:fs'), path = require('node:path'), vm = require('node:vm'), assert = require('node:assert/strict'), ts = require('typescript')
const root = path.resolve(__dirname, '..'), values = new Map(), cache = new Map()
let app
const wx = { getStorageSync: key => values.get(key), setStorageSync: (key, value) => values.set(key, structuredClone(value)), removeStorageSync: key => values.delete(key) }
function load(filename) {
  filename = path.resolve(root, filename)
  if (cache.has(filename)) return cache.get(filename).exports
  const module = { exports: {} }; cache.set(filename, module)
  const source = ts.transpileModule(fs.readFileSync(filename, 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS } }).outputText
  vm.runInNewContext(source, { module, exports: module.exports, wx, getApp: () => app, App: value => { app = value }, Date, Map, Set, Error, structuredClone, console, setTimeout, clearTimeout, require: name => load(path.resolve(path.dirname(filename), name + '.ts')) })
  return module.exports
}
;(async () => {
  load('miniprogram/app.ts')
  const { callApi } = load('miniprogram/utils/api.ts')
  app.onShow()
  await new Promise(resolve => setTimeout(resolve, 170))
  const key = 'chiji_demo_commerce_activity'
  assert.equal(values.get(key).length, 1)
  assert.equal(values.get(key)[0].cartAddedAt, undefined)
  await callApi('analytics.visit'); assert.equal(values.get(key).length, 1)
  const product = (await callApi('product.list'))[0], sku = product.skus[0]
  const item = { productId: product.id, skuId: sku.id, quantity: 1 }
  await callApi('cart.update', item)
  assert.equal(values.get(key)[0].cartAddedAt, undefined, 'ordinary edits must not count as add-to-cart')
  await callApi('cart.update', { ...item, addToCart: true })
  const first = values.get(key)[0].cartAddedAt
  assert.ok(first); assert.ok(Date.parse(first) >= Date.parse(values.get(key)[0].visitedAt))
  await callApi('cart.update', { ...item, addToCart: true })
  assert.equal(values.get(key)[0].cartAddedAt, first, 'repeat additions inflated first event')
  await callApi('cart.update', { ...item, selected: false })
  await callApi('cart.update', { ...item, quantity: 0 })
  assert.equal(values.get(key)[0].cartAddedAt, first)
  const { clearPersonalStorage, closedKey } = load('miniprogram/utils/personal-data.ts')
  clearPersonalStorage('usage'); assert.equal(values.has(key), false)
  values.set(closedKey, true); app.onShow()
  await new Promise(resolve => setTimeout(resolve, 170)); assert.equal(values.has(key), false)
  values.delete(closedKey)
  app.globalData.demoMode = false
  let logins = 0
  wx.login = async () => { logins++; await new Promise(resolve => setTimeout(resolve, 10)); return { code: 'login-code' } }
  wx.cloud = { callContainer: async ({ data }) => ({ statusCode: 200, data: { ok: true, data: data.action === 'auth.login' ? { token: 'fixture-token' } : { recorded: true } } }) }
  await Promise.all([callApi('analytics.visit'), callApi('bootstrap')])
  assert.equal(logins, 1, 'foreground tracking and page request caused duplicate login')
  console.log('Commerce analytics passed: foreground UV, repeat visits/adds, edit/select/delete exclusion, personal data cleanup and closed-account guard.')
})().catch(error => { console.error(error); process.exitCode = 1 })
