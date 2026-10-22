const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const assert = require('node:assert/strict')
const ts = require('typescript')
const root = path.resolve(__dirname, '..')

async function run() {
  let spec, demoMode = true, failure = false, pending = null
  const storage = new Map(), requests = []
  const wx = {
    getStorageSync: key => structuredClone(storage.get(key)),
    setStorageSync: (key, value) => storage.set(key, structuredClone(value)),
    cloud: { callContainer: async ({ data: { action, data } }) => {
      assert.equal(action, 'product.reviews')
      requests.push(data)
      if (pending) return new Promise(resolve => pending.push(resolve))
      if (failure) throw new Error('网络失败')
      const items = reviews.slice((data.page - 1) * 20, data.page * 20)
      return { statusCode: 200, data: { ok: true, data: { items, total: 45, average: 4, page: data.page, pageSize: 20, hasMore: data.page < 3 } } }
    } },
  }
  const cache = new Map()
  function load(relative) {
    const filename = path.resolve(root, relative)
    if (cache.has(filename)) return cache.get(filename).exports
    const module = { exports: {} }; cache.set(filename, module)
    const source = ts.transpileModule(fs.readFileSync(filename, 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS } }).outputText
    vm.runInNewContext(source, { module, exports: module.exports, wx, Error, console, setTimeout, clearTimeout,
      getApp: () => ({ globalData: { demoMode } }), Page: value => { spec = value },
      require: name => load(path.resolve(path.dirname(filename), name + '.ts')) }, { filename })
    return module.exports
  }
  const reviews = Array.from({ length: 45 }, (_, i) => ({ id: `r-${String(45-i).padStart(2, '0')}`, productId: 'p-shine', rating: i % 5 + 1, content: `评价${i}`, createdAt: '2026-10-05T01:00:00Z' }))
  storage.set('chiji_demo_reviews', [...reviews].reverse())
  const { callApi } = load('miniprogram/utils/api.ts')
  const first = await callApi('product.reviews', { productId: 'p-shine' })
  const second = await callApi('product.reviews', { productId: 'p-shine', page: 2 })
  const third = await callApi('product.reviews', { productId: 'p-shine', page: 3 })
  assert.equal(first.items.length, 20); assert.equal(second.items.length, 20); assert.equal(third.items.length, 6)
  assert.equal(first.page, 1); assert.equal(first.pageSize, 20); assert.equal(first.hasMore, true); assert.equal(third.hasMore, false)
  assert.equal(first.total, 46); assert.equal(first.average, second.average)
  assert.equal(new Set([...first.items, ...second.items, ...third.items].map(r => r.id)).size, 46)
  assert.equal(first.items[0].id, 'r-45', 'same-time reviews have deterministic ordering')
  assert.equal((await callApi('product.reviews', { productId: 'p-shine', page: 4 })).items.length, 0)
  for (const input of [{ page: -1 }, { page: 1.5 }, { pageSize: 101 }, { pageSize: -1 }]) {
    await assert.rejects(callApi('product.reviews', { productId: 'p-shine', ...input }), /分页参数无效/)
  }
  load('miniprogram/pages/product/index.ts')
  const page = { ...spec, data: structuredClone(spec.data), setData(updates) { Object.assign(this.data, updates) } }
  page.data.productId = 'p-shine'
  demoMode = false; storage.set('chiji_mini_token', 'token')
  failure = true
  await page.loadReviews()
  assert.equal(page.data.reviewPage, 0); assert.match(page.data.reviewsError, /网络失败/)
  failure = false; await page.retryReviews()
  assert.equal(page.data.reviews.length, 20); assert.equal(page.data.reviewPage, 1)
  failure = true; await page.loadMoreReviews()
  assert.equal(page.data.reviews.length, 20); assert.equal(page.data.reviewPage, 1)
  failure = false
  const before = requests.length
  await Promise.all([page.retryReviews(), page.loadMoreReviews()])
  assert.equal(requests.length, before + 1, 'concurrent taps make one request')
  assert.equal(page.data.reviewPage, 2); assert.equal(page.data.reviews.length, 40); assert.equal(page.data.reviewsError, '')
  await page.loadMoreReviews()
  assert.equal(page.data.reviews.length, 45); assert.equal(page.data.reviewsHasMore, false)
  const count = requests.length; await page.loadMoreReviews(); assert.equal(requests.length, count)
  await page.loadReviews(); assert.equal(page.data.reviews.length, 20)
  pending = []
  const older = page.loadMoreReviews(), refresh = page.loadReviews()
  const response = (items, page, hasMore) => ({ statusCode: 200, data: { ok: true, data: { items, total: 45, average: 4, page, pageSize: 20, hasMore } } })
  pending[1](response(reviews.slice(0, 20), 1, true)); await refresh
  pending[0](response(reviews.slice(20, 40), 2, true)); await older
  assert.equal(page.data.reviewPage, 1); assert.equal(page.data.reviews.length, 20)
  pending = []
  const unloading = page.loadMoreReviews(); page.onUnload()
  pending[0](response(reviews.slice(20, 40), 2, true)); await unloading
  assert.equal(page.data.reviews.length, 20, 'late responses cannot update an unloaded page')
  console.log('Review pagination: multiple pages, sorting, totals, validation, retry, concurrent taps, refresh and stale responses passed.')
}
run().catch(error => { console.error(error); process.exitCode = 1 })
