const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const assert = require('node:assert/strict')
const ts = require('typescript')
const root = path.resolve(__dirname, '..')
const pending = [], patches = []
let page, calls = 0
const storage = new Map([['chiji_mini_token', 'test']])
const app = { globalData: { demoMode: false } }
const wx = {
  getStorageSync: k => storage.get(k), setStorageSync: (k, v) => storage.set(k, v),
  showToast() {}, stopPullDownRefresh() {},
  showModal(options) { options.success({ confirm: true }) },
  cloud: { callContainer: args => { calls++; return new Promise(resolve => pending.push({ resolve, data: args.data.data })) } },
}
const cache = new Map()
function load(file) {
  file = path.resolve(root, file)
  if (cache.has(file)) return cache.get(file).exports
  const module = { exports: {} }; cache.set(file, module)
  const code = ts.transpileModule(fs.readFileSync(file, 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS } }).outputText
  vm.runInNewContext(code, { module, exports: module.exports, wx, getApp: () => app, setTimeout, clearTimeout, console,
    require: name => load(path.resolve(path.dirname(file), name + '.ts')),
    Page: spec => { page = spec; page.setData = patch => {
      patches.push(patch)
      for (const [k, v] of Object.entries(patch)) {
        const m = k.match(/^products\[(\d+)\](?:\.(\w+))?$/)
        if (!m) page.data[k] = v
        else if (m[2]) page.data.products[+m[1]][m[2]] = v
        else page.data.products[+m[1]] = v
      }
    } },
  }, { filename: file })
  return module.exports
}
const template = load('miniprogram/data/demo.ts').demoProducts[0]
const products = Array.from({ length: 137 }, (_, i) => ({ ...template, id: `p-${i}`, name: `商品 ${i}` }))
function reply(request, items, total = 137) { request.resolve({ statusCode: 200, data: { ok: true, data: { items, total, offset: request.data.offset, limit: request.data.limit } } }) }
async function test() {
  load('miniprogram/pages/shop/index.ts')
  let work = page.onShow(); reply(pending.shift(), products.slice(0, 20)); await work
  assert.equal(page.data.products.length, 20); assert.equal(page.data.total, 137)
  page.imageError({ currentTarget: { dataset: { id: 'p-0' } } })
  const before = calls
  work = page.onReachBottom(); await page.onReachBottom()
  assert.equal(calls, before + 1)
  const failed = pending.shift(); assert.equal(failed.data.offset, 20)
  failed.resolve({ statusCode: 500, data: { ok: false, message: '加载失败' } }); await work
  assert.equal(page.data.products.length, 20); assert.ok(page.data.moreError)
  await page.onReachBottom(); assert.equal(calls, before + 1)
  work = page.retryMore(); reply(pending.shift(), products.slice(20, 40)); await work
  assert.equal(page.data.products.length, 40); assert.equal(page.data.products[0].imageFailed, true)
  assert.ok(patches.some(p => p['products[20]'] && !p.products))
  while (page.data.hasMore) {
    work = page.loadMore(); const r = pending.shift(); reply(r, products.slice(r.data.offset, r.data.offset + 20)); await work
  }
  assert.equal(page.data.products.length, 137); assert.equal(page.data.offset, 137)
  const endCalls = calls; await page.loadMore(); assert.equal(calls, endCalls)
  work = page.loadProducts(); const stale = pending.shift()
  page.search({ detail: { value: '旧关键词' } }); page.search({ detail: { value: '新关键词' } })
  reply(stale, products.slice(0, 20)); await work; assert.equal(page.data.products.length, 0)
  await new Promise(resolve => setTimeout(resolve, 350))
  assert.equal(pending.length, 1); const search = pending.shift(); assert.equal(search.data.keyword, '新关键词')
  reply(search, [], 0); await new Promise(resolve => setTimeout(resolve, 0)); assert.equal(page.data.loading, false)
  work = page.resetFilters(); reply(pending.shift(), products.slice(0, 20)); await work
  work = page.loadMore(); const oldMore = pending.shift()
  const switched = page.chooseCategory({ currentTarget: { dataset: { value: 'gift' } } })
  const current = pending.shift(); assert.equal(current.data.category, 'gift'); assert.equal(current.data.offset, 0)
  reply(current, [products[130]], 1); await switched; reply(oldMore, products.slice(20, 40)); await work
  assert.equal(page.data.products.length, 1); assert.equal(page.data.products[0].id, 'p-130')
  work = page.loadProducts(); const hidden = pending.shift(); page.onHide(); reply(hidden, products.slice(0, 20)); await work
  assert.equal(page.data.products.length, 0)
  page.openFilters()
  page.data.draftSaleMode = 'spot'; page.data.draftStock = 'available'
  page.data.draftMinPrice = '50.25'; page.data.draftMaxPrice = '40'
  const invalidCalls = calls; await page.applyFilters(); assert.equal(calls, invalidCalls); assert.equal(page.data.filtersOpen, true)
  page.data.draftMaxPrice = '100'
  work = page.applyFilters(); const filtered = pending.shift()
  assert.equal(filtered.data.minPrice, 5025); assert.equal(filtered.data.maxPrice, 10000)
  assert.equal(filtered.data.saleMode, 'spot'); assert.equal(filtered.data.stock, 'available'); assert.equal(filtered.data.offset, 0)
  reply(filtered, [], 0); await work; assert.equal(page.data.filterCount, 3)
  page.data.query = '葡萄'; page.data.category = 'gift'; page.data.sort = 'priceDesc'
  page.saveFilterFavorite()
  assert.equal(page.data.searchFavorites.length, 1)
  const favoriteId = page.data.searchFavorites[0].id
  page.saveFilterFavorite(); assert.equal(page.data.searchFavorites.length, 1)
  page.data.minPrice = '050.25'; page.saveFilterFavorite(); assert.equal(page.data.searchFavorites.length, 1)
  work = page.resetFilters(); reply(pending.shift(), [], 0); await work
  page.search({ detail: { value: '将被取消的搜索' } })
  work = page.applyFilterFavorite({ currentTarget: { dataset: { id: favoriteId } } })
  const restored = pending.shift()
  assert.equal(restored.data.keyword, '葡萄'); assert.equal(restored.data.category, 'gift'); assert.equal(restored.data.sort, 'priceDesc')
  assert.equal(restored.data.saleMode, 'spot'); assert.equal(restored.data.stock, 'available')
  assert.equal(restored.data.minPrice, 5025); assert.equal(restored.data.maxPrice, 10000); assert.equal(restored.data.offset, 0)
  reply(restored, [], 0); await work
  await new Promise(resolve => setTimeout(resolve, 350)); assert.equal(pending.length, 0)
  assert.equal(page.data.filterCount, 3)
  const favorites = load('miniprogram/utils/shop-search.ts')
  for (let i = 0; i < 9; i++) favorites.saveSearchFavorite({ ...page.data, query: `组合${i}` })
  assert.equal(favorites.readSearchFavorites().length, 10)
  assert.throws(() => favorites.saveSearchFavorite({ ...page.data, query: '第十一组' }), /最多收藏/)
  assert.equal(favorites.readSearchFavorites().length, 10)
  const originalSetStorage = wx.setStorageSync
  wx.setStorageSync = () => { throw new Error('写入失败') }
  const previousFavorites = page.data.searchFavorites
  page.deleteFilterFavorite({ currentTarget: { dataset: { id: favoriteId } } })
  assert.equal(page.data.searchFavorites, previousFavorites)
  wx.setStorageSync = originalSetStorage
  page.deleteFilterFavorite({ currentTarget: { dataset: { id: favoriteId } } })
  assert.equal(page.data.searchFavorites.length, 9)
  work = page.applyFilterFavorite({ currentTarget: { dataset: { id: favoriteId } } }); await work
  assert.equal(pending.length, 0)
  storage.set('chiji_live_search_favorites', [null, { query: 123 }, ...favorites.readSearchFavorites()])
  assert.equal(favorites.readSearchFavorites().length, 9)
  page.openFilters(); page.resetDraftFilters(); page.closeFilters(); assert.equal(page.data.stock, 'available')
  work = page.resetFilters(); reply(pending.shift(), [], 0); await work
  assert.equal(page.data.filterCount, 0); assert.equal(page.data.minPrice, '')
  const history = load('miniprogram/utils/shop-search.ts')
  for (let i = 0; i < 12; i++) history.rememberSearch(`搜索${i}`)
  assert.equal(history.readSearchHistory().length, 10)
  history.rememberSearch(' 搜索11 '); assert.equal(history.readSearchHistory()[0], '搜索11')
  work = page.chooseSearch({ currentTarget: { dataset: { word: '葡萄' } } }); const picked = pending.shift()
  assert.equal(picked.data.keyword, '葡萄'); reply(picked, [], 0); await work
  assert.equal(page.data.searchHistory[0], '葡萄'); assert.equal(page.data.suggestionsOpen, false)
  page.removeSearchHistory({ currentTarget: { dataset: { word: '葡萄' } } }); assert.ok(!history.readSearchHistory().includes('葡萄'))
  page.clearSearchHistory(); assert.equal(history.readSearchHistory().length, 0)
  work = page.showSuggestions(); const hot = pending.shift(); assert.equal(hot.data.sort, 'sales'); assert.equal(hot.data.limit, 6)
  hot.resolve({ statusCode: 500, data: { ok: false, message: '失败' } }); await work; assert.ok(page.data.hotError)
  work = page.loadHotSearches(); reply(pending.shift(), [products[0], products[0], products[1]], 2); await work
  assert.equal(page.data.hotSearches.length, 2); assert.equal(page.data.hotError, '')
  page.setData({ category: 'family', saleMode: 'presale', stock: 'available', minPrice: '20', maxPrice: '100', sort: 'sales' })
  work = page.chooseSearch({ currentTarget: { dataset: { word: page.data.hotSearches[0] } } })
  const recommendation = pending.shift()
  assert.equal(recommendation.data.keyword, products[0].name); assert.equal(recommendation.data.category, 'family')
  assert.equal(recommendation.data.saleMode, 'presale'); assert.equal(recommendation.data.stock, 'available')
  assert.equal(recommendation.data.minPrice, 2000); assert.equal(recommendation.data.maxPrice, 10000); assert.equal(recommendation.data.sort, 'sales')
  reply(recommendation, [], 0); await work
  work = page.showSuggestions(); const refreshedHot = pending.shift()
  assert.equal(refreshedHot.data.category, 'all'); assert.equal(refreshedHot.data.saleMode, undefined)
  reply(refreshedHot, [products[0], products[1]], 2); await work

  work = page.loadHotSearches(); const staleHot = pending.shift(); page.onHide(); reply(staleHot, [products[2]], 1); await work
  assert.equal(page.data.hotSearches[0], products[0].name)
  history.rememberSearch('正式历史')
  app.globalData.demoMode = true
  assert.equal(history.readSearchHistory().length, 0)
  assert.equal(favorites.readSearchFavorites().length, 0)
  favorites.saveSearchFavorite(page.data); assert.equal(favorites.readSearchFavorites().length, 1)
  app.globalData.demoMode = false; assert.equal(favorites.readSearchFavorites().length, 9)
  app.globalData.demoMode = true
  const demo = load('miniprogram/data/demo.ts').demoProducts
  demo.splice(0, demo.length, ...products)
  const { callApi } = load('miniprogram/utils/api.ts')
  const result = await callApi('product.search', { keyword: '商品 136', category: 'all', offset: 0, limit: 20 })
  assert.equal(result.total, 1); assert.equal(result.items[0].id, 'p-136')
  const last = await callApi('product.search', { offset: 120, limit: 20 })
  assert.equal(last.total, 137); assert.equal(last.items.length, 17)
  const fixtures = [
    { ...template, id: 'a', category: 'family', saleMode: 'spot', skus: [{ price: 5025, stock: 1, limitPerOrder: 1 }] },
    { ...template, id: 'b', category: 'family', saleMode: 'presale', skus: [{ price: 6000, stock: 1, limitPerOrder: 1 }] },
    { ...template, id: 'c', category: 'family', saleMode: 'spot', skus: [{ price: 5000, stock: 0, limitPerOrder: 1 }] },
    { ...template, id: 'd', category: 'family', saleMode: 'spot', skus: [] },
    { ...template, id: 'e', category: 'family', saleMode: 'spot', skus: [{ price: 5025, stock: 2, limitPerOrder: 0 }] },
  ]
  demo.splice(0, demo.length, ...fixtures)
  const combo = await callApi('product.search', { category: 'family', saleMode: 'spot', stock: 'available', minPrice: 5025, maxPrice: 5025 })
  assert.equal(combo.total, 1); assert.equal(combo.items[0].id, 'a')
  const soldOut = await callApi('product.search', { stock: 'soldOut' }); assert.deepEqual(Array.from(soldOut.items, p => p.id), ['c', 'd', 'e'])
  const bounded = await callApi('product.search', { maxPrice: 5025 }); assert.equal(bounded.total, 3)
  await assert.rejects(callApi('product.search', { minPrice: 10000, maxPrice: 1 }))
  console.log('Saved search combinations: restore all conditions, cancel debounce, deduplication, limit, deletion, failed storage, corrupt entries and mode isolation passed.')
  console.log('Shop filters, inclusive price boundaries, no-SKU and stock guards, history limits/isolation/removal, hot search retry and stale response tests passed.')
  console.log('Shop pagination: 137 products, append patches, duplicate request guard, retry, debounce, stale responses, filters, lifecycle and demo parity passed.')
}
test().catch(e => { console.error(e); process.exitCode = 1 })
