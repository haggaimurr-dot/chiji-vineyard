const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const assert = require('node:assert/strict')
const ts = require('typescript')
const root = path.resolve(__dirname, '..')

async function run() {
  const storage = new Map(), cache = new Map(), calls = []
  let page, answer = 'accept', prompts = 0, failSave = false, live = false, title = '', modalResult = { confirm: false }
  const wx = {
    getStorageSync: key => storage.get(key), setStorageSync: (key, value) => storage.set(key, value),
    requestSubscribeMessage: async ({ tmplIds }) => { prompts++; return { [tmplIds[0]]: answer } },
    showModal: async () => modalResult,
    showToast: () => {}, setNavigationBarTitle: ({ title: value }) => { title = value },
  }
  const callApi = async (action, data) => {
    if (!live) return api.callApi(action, data)
    calls.push({ action, data })
    if (action === 'product.priceDropSubscription.accept') {
      if (failSave) throw new Error('网络异常，请重试')
      return { status: 'READY' }
    }
    if (action === 'product.attention.set') return { enabled: data.enabled }
    throw new Error(`Unexpected action ${action}`)
  }
  function load(filename) {
    filename = path.resolve(root, filename)
    if (cache.has(filename)) return cache.get(filename).exports
    const module = { exports: {} }; cache.set(filename, module)
    const code = ts.transpileModule(fs.readFileSync(filename, 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS } }).outputText
    vm.runInNewContext(code, {
      module, exports: module.exports, wx, setTimeout, clearTimeout,
      getApp: () => ({ globalData: { demoMode: true } }),
      Page: spec => { page = spec; page.setData = patch => Object.assign(page.data, patch) },
      require: name => name.endsWith('/api') ? { callApi } : load(path.resolve(path.dirname(filename), `${name}.ts`)),
    }, { filename })
    return module.exports
  }
  const api = load('miniprogram/utils/api.ts')
  const products = load('miniprogram/data/demo.ts').demoProducts
  const product = products[0], sku = product.skus[0]
  // Old local storage remains usable after adding the new reminder kind.
  storage.set('chiji_demo_attention', { favorite: [product.id], restock: [] })
  assert.equal((await api.callApi('product.attention.list', { kind: 'pricedrop' })).length, 0)
  load('miniprogram/pages/product/index.ts')
  page.data.productId = product.id
  await page.loadProduct()
  const index = product.skus.length - 1
  page.data.skuIndex = index
  const watched = product.skus[index], baseline = watched.price
  await page.toggleAttention({ currentTarget: { dataset: { kind: 'pricedrop' } } })
  assert.equal(page.data.product.priceDrop.skuId, watched.id)
  assert.equal(page.data.product.priceDrop.baselinePrice, baseline)
  assert.equal(prompts, 0)
  const list = await api.callApi('product.attention.list', { kind: 'pricedrop' })
  assert.equal(list.length, 1)
  assert.equal(list[0].favorite, true)
  assert.equal(list[0].priceDrop.dropped, false)
  if (sku.id !== watched.id) {
    const old = sku.price; sku.price = 1
    assert.equal((await api.callApi('product.detail', { id: product.id })).priceDrop.dropped, false)
    sku.price = old
  }
  watched.price -= 100
  await api.callApi('product.attention.set', { productId: product.id, kind: 'pricedrop', skuId: watched.id, enabled: true })
  assert.equal((await api.callApi('product.detail', { id: product.id })).priceDrop.baselinePrice, baseline)
  load('miniprogram/pages/attention/index.ts')
  page.onLoad({ kind: 'pricedrop' }); await page.loadProducts()
  assert.equal(title, '降价提醒')
  assert.equal(page.data.products[0].priceDrop.dropped, true)
  assert.equal(page.data.products[0].dropText, '1.00')
  watched.stock = 0
  await page.loadProducts()
  assert.equal(page.data.products[0].priceDrop.available, false)
  await page.remove({ currentTarget: { dataset: { id: product.id } } })
  assert.equal(page.data.products.length, 0)
  assert.equal((await api.callApi('product.detail', { id: product.id })).favorite, true)
  await assert.rejects(() => api.callApi('product.attention.set', { productId: product.id, kind: 'pricedrop', skuId: 'bad', enabled: true }), /规格/)
  const { parseTargetPrice } = load('miniprogram/utils/reminder-target.ts')
  assert.equal(parseTargetPrice(' 79.01 '), 7901)
  for (const value of ['0', '-1', '1.001', 'NaN', '1e2', '', '1000000.01']) assert.throws(() => parseTargetPrice(value))
  watched.stock = 10
  watched.price = baseline
  const target = baseline - 200
  await api.callApi('product.attention.set', { productId: product.id, kind: 'pricedrop', skuId: watched.id, targetPrice: target, enabled: true })
  watched.price = target + 1
  assert.equal((await api.callApi('product.detail', { id: product.id })).priceDrop.dropped, false)
  watched.price = target
  assert.equal((await api.callApi('product.detail', { id: product.id })).priceDrop.dropped, true)
  const history = await api.callApi('product.attention.history', {})
  const count = history.items.length
  assert.ok(count >= 1)
  assert.equal(history.items[0].targetPrice, target)
  assert.equal(history.items[0].status, 'IN_APP')
  await api.callApi('product.detail', { id: product.id })
  assert.equal((await api.callApi('product.attention.history', {})).items.length, count)
  await page.loadProducts()
  modalResult = { confirm: false }
  await page.editTargetPrice({ currentTarget: { dataset: { id: product.id } } })
  assert.equal((await api.callApi('product.detail', { id: product.id })).priceDrop.targetPrice, target)
  modalResult = { confirm: true, content: String((target - 100) / 100) }
  await page.editTargetPrice({ currentTarget: { dataset: { id: product.id } } })
  assert.equal(page.data.products[0].priceDrop.targetPrice, target - 100)
  assert.equal(page.data.products[0].priceDrop.dropped, false)
  await page.editTargetPrice({ currentTarget: { dataset: { id: product.id, clear: true } } })
  assert.equal(page.data.products[0].priceDrop.targetPrice, 0)
  assert.equal(page.data.products[0].priceDrop.dropped, true)
  modalResult = { confirm: true, content: 'bad' }
  await page.editTargetPrice({ currentTarget: { dataset: { id: product.id } } })
  assert.equal(page.data.busy, false)
  await page.remove({ currentTarget: { dataset: { id: product.id } } })
  assert.equal((await api.callApi('product.attention.history', {})).items.length, count)
  await assert.rejects(() => api.callApi('product.priceDropTarget.set', { productId: product.id, targetPrice: 1 }), /先设置/)
  await assert.rejects(() => api.callApi('product.attention.set', { productId: product.id, kind: 'pricedrop', skuId: watched.id, targetPrice: watched.price + 1, enabled: true }), /目标价格/)
  page.onLoad({ kind: 'history' }); await page.loadProducts()
  assert.equal(title, '提醒历史')
  assert.equal(page.data.history.length, count)
  assert.equal(page.data.history[0].productName, product.name)
  assert.match(page.data.history[0].statusText, /演示/)
  // Pagination keeps older records visible; cancellation never removes snapshots.
  const flags = storage.get('chiji_demo_attention')
  for (let i = flags.history.length; i < 25; i++) flags.history.push({ ...flags.history[0], id: `older-${i}` })
  await page.loadProducts()
  assert.equal(page.data.history.length, 20)
  assert.equal(page.data.historyMore, true)
  await page.moreHistory()
  assert.equal(page.data.history.length, 25)
  assert.equal(page.data.historyMore, false)
  page.onLoad({ kind: 'pricedrop' })
  const config = { enabled: true, templateId: 'price-template' }
  live = true
  page.data.products = [{ id: product.id, priceDrop: { skuId: watched.id, baselinePrice: baseline, pushStatus: 'NONE' } }]
  page.data.priceDropConfig = config
  answer = 'reject'
  await page.subscribePriceDrop({ currentTarget: { dataset: { id: product.id } } })
  assert.equal(calls.length, 0)
  assert.match(page.data.subscriptionError, /未开启微信提醒/)
  answer = 'accept'; failSave = true
  await page.subscribePriceDrop({ currentTarget: { dataset: { id: product.id } } })
  assert.equal(page.data.products[0].priceDrop.pushStatus, 'NONE')
  assert.match(page.data.subscriptionError, /网络异常/)
  assert.equal(page.data.busy, false)
  failSave = false
  await page.subscribePriceDrop({ currentTarget: { dataset: { id: product.id } } })
  assert.equal(page.data.products[0].priceDrop.pushStatus, 'READY')
  assert.equal(calls.at(-1).data.templateId, 'price-template')
  page.data.busy = true
  const before = prompts
  await page.subscribePriceDrop({ currentTarget: { dataset: { id: product.id } } })
  assert.equal(prompts, before)
  cache.delete(path.resolve(root, 'miniprogram/pages/product/index.ts'))
  load('miniprogram/pages/product/index.ts')
  page.data.productId = product.id; page.data.product = list[0]; page.data.priceDropConfig = config
  await page.subscribePriceDrop()
  assert.equal(page.data.product.priceDrop.pushStatus, 'READY')
  await page.toggleAttention({ currentTarget: { dataset: { kind: 'favorite' } } })
  assert.equal(page.data.product.priceDrop.pushStatus, 'READY')
  console.log('Price drop target boundaries, edits, history retention/pagination, SKU baseline, authorization and failure checks passed')
}
run().catch(error => { console.error(error); process.exitCode = 1 })
