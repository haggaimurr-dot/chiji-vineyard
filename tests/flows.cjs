const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const assert = require('node:assert/strict')
const ts = require('typescript')
const root = path.resolve(__dirname, '..')

function runtime(globals = {}) {
  const cache = new Map()
  return function load(relative) {
    const filename = path.resolve(root, relative)
    if (cache.has(filename)) return cache.get(filename).exports
    const module = { exports: {} }
    cache.set(filename, module)
    const source = ts.transpileModule(fs.readFileSync(filename, 'utf8'), {
      compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS },
    }).outputText
    const context = vm.createContext({ module, exports: module.exports, console, structuredClone, Error, setTimeout, clearTimeout, ...globals,
      require: (name) => load(path.resolve(path.dirname(filename), `${name}.ts`)),
    })
    vm.runInContext(source, context, { filename })
    return module.exports
  }
}

async function adminFlows() {
  const { mockCall } = runtime()('admin/src/mock.ts')
  const products = await mockCall('admin.product.list')
  const a = products[0].skus[0], b = products[1].skus[0]
  await assert.rejects(() => mockCall('admin.inventory.batch', { reason: '盘点', items: [
    { skuId: a.id, stock: 0, expectedStock: a.stock },
    { skuId: b.id, stock: 0, expectedStock: b.stock - 1 },
  ] }), /库存已变化/)
  assert.equal((await mockCall('admin.product.list'))[0].skus[0].stock, a.stock)
  await mockCall('admin.inventory.batch', { reason: '盘点', items: [
    { skuId: a.id, stock: 0, expectedStock: a.stock },
    { skuId: b.id, stock: 50, expectedStock: b.stock },
  ] })
  assert.equal((await mockCall('admin.product.list'))[0].skus[0].stock, 0)
  const config = await mockCall('admin.config.get')
  config.home.banners[0].title = '新的运营位'
  config.home.productIds = [products[1].id, products[0].id]
  await mockCall('admin.home.save', { home: config.home })
  assert.equal((await mockCall('admin.config.get')).home.activities[0].title, config.home.activities[0].title)
  assert.equal((await mockCall('admin.config.get')).home.launchCalendar.length, 2)
  assert.equal((await mockCall('admin.config.get')).home.banners[0].title, '新的运营位')
  await mockCall('admin.config.save', { shipping: { freight: 100, freeShippingThreshold: 999, blockedProvinces: [] } })
  assert.equal((await mockCall('admin.config.get')).home.banners[0].title, '新的运营位')
  config.privacy.published = true
  config.privacy.operator = ''
  await assert.rejects(() => mockCall('admin.privacy.save', { privacy: config.privacy }), /主体/)
  Object.assign(config.privacy, { operator: '测试主体', contact: 'privacy@example.com', address: '测试地址' })
  await mockCall('admin.privacy.save', { privacy: config.privacy })
  assert.equal((await mockCall('admin.config.get')).privacy.published, true)
  const orders = await mockCall('admin.order.list')
  await assert.rejects(() => mockCall('admin.order.tracking', { orderId: orders[0].id }), /尚未发货/)
  assert.equal((await mockCall('admin.order.tracking', { orderId: orders[2].id })).status, 'DEMO')
}

async function miniFlows() {
  let page, logins = 0, destination, modal, homeSections = {}
  const storage = new Map()
  const wx = {
    getStorageSync: key => storage.get(key), setStorageSync: (key, value) => storage.set(key, value), removeStorageSync: key => storage.delete(key),
    login: async () => { logins++; return { code: 'test' } },
    showToast: () => {}, navigateTo: args => { destination = args.url }, switchTab: args => { destination = args.url },
    showModal: args => { modal = args },
    cloud: { callContainer: async ({ data }) => {
      let result
      if (data.action === 'privacy.get') result = { title: '测试政策', version: '1.0', published: true, content: ['完整正文'] }
      else if (data.action === 'activity.reservation.list') result = []
      else if (data.action === 'home.get') result = { banners: [{ title: '上线活动', target: 'product', targetId: 'p2' }], productIds: ['p2', 'p1', 'unpublished'], articleIds: ['a2', 'a1'], ...homeSections }
      else if (data.action === 'product.list') result = [{ id: 'p1', skus: [{ price: 100 }] }, { id: 'p2', skus: [{ price: 200 }] }]
      else if (data.action === 'article.list') result = [{ id: 'a1' }, { id: 'a2' }]
      else throw new Error(`Unexpected action ${data.action}`)
      return { statusCode: 200, data: { ok: true, data: result } }
    } },
  }
  const load = runtime({ wx, getApp: () => ({ globalData: { demoMode: false, cloudEnv: 'test', cloudService: 'test' } }),
    Page: spec => { page = spec; page.setData = updates => Object.assign(page.data, updates) },
  })
  // Reading a policy must not itself force a login.
  const { callApi } = load('miniprogram/utils/api.ts')
  assert.equal((await callApi('privacy.get')).published, true)
  assert.equal(logins, 0)
  storage.set('chiji_mini_token', 'test-token')
  load('miniprogram/pages/home/index.ts')
  await page.onShow()
  assert.equal(page.data.home.banners[0].title, '上线活动')
  assert.equal(page.data.products.map(p => p.id).join(','), 'p2,p1')
  assert.equal(page.data.articles.map(a => a.id).join(','), 'a2,a1')
  page.openBanner({ currentTarget: { dataset: { index: 0 } } })
  assert.equal(destination, '/pages/product/index?id=p2')
  assert.equal(page.data.activities.length, 0)
  assert.equal(page.data.announcements.length, 0)
  const today = new Date(Date.now() + 8 * 3600000).toISOString().slice(0, 10)
  homeSections = {
    activities: [
      { title: '今日活动', content: '活动规则', startDate: today, endDate: today, productId: 'p2' },
      { title: '失效目标', content: '规则', startDate: today, endDate: today, productId: 'unpublished' },
      { title: '往期活动', content: '规则', startDate: '2020-01-01', endDate: '2020-01-02', productId: 'p2' },
    ],
    announcements: [{ title: '未来公告', content: '尚未发布', publishedAt: '2099-01-01' }, { title: '今日公告', content: '公告全文', publishedAt: today }],
    launchCalendar: [{ variety: '跨年品种', startDate: '2026-11-20', endDate: '2027-02-10', productId: 'p2' }, { variety: '夏季品种', startDate: '2026-06-01', endDate: '2026-07-31', productId: 'unpublished' }],
  }
  await page.onShow()
  assert.equal(page.data.activities[0].status, '进行中')
  page.openActivity({ currentTarget: { dataset: { index: 0 } } })
  assert.equal(destination, `/pages/activity/index?index=0&title=${encodeURIComponent('今日活动')}`)
  page.openActivity({ currentTarget: { dataset: { index: 1 } } })
  assert.equal(destination, `/pages/activity/index?index=1&title=${encodeURIComponent('失效目标')}`)
  page.openActivity({ currentTarget: { dataset: { index: 2 } } })
  assert.equal(destination, `/pages/activity/index?index=2&title=${encodeURIComponent('往期活动')}`)
  assert.equal(page.data.announcements.length, 1)
  page.openAnnouncement({ currentTarget: { dataset: { index: 0 } } })
  assert.ok(modal.content.includes('公告全文'))
  page.chooseMonth({ currentTarget: { dataset: { index: 1 } } })
  assert.equal(page.data.calendarItems.map(x => x.variety).join(','), '跨年品种')
  page.chooseMonth({ currentTarget: { dataset: { index: 3 } } })
  assert.equal(page.data.calendarItems.length, 0)
  page.chooseMonth({ currentTarget: { dataset: { index: 0 } } })
  assert.equal(page.data.calendarItems.length, 2)
  load('miniprogram/pages/profile/index.ts')
  page.policy()
  assert.equal(destination, '/pages/privacy/index')
}

async function productFlows() {
  let page, response, apiError, updates = 0, navigations = 0, toast = ''
  const storage = new Map([['chiji_mini_token', 'test']])
  const wx = {
    getStorageSync: key => storage.get(key), setStorageSync: (key, value) => storage.set(key, value),
    showToast: ({ title }) => { toast = title }, setNavigationBarTitle: () => {},
    navigateTo: () => { navigations++ },
    cloud: { callContainer: async ({ data }) => {
      if (data.action === 'cart.update') updates++
      if (apiError) throw new Error(apiError)
      if (data.action === 'product.search') {
        const { filterProducts, sortProducts } = load('miniprogram/utils/product.ts')
        const items = sortProducts(filterProducts(response, data.data.category, data.data.keyword), data.data.sort)
        return { statusCode: 200, data: { ok: true, data: { items: items.slice(data.data.offset, data.data.offset + data.data.limit), total: items.length, offset: data.data.offset, limit: data.data.limit } } }
      }
      return { statusCode: 200, data: { ok: true, data: response } }
    } },
  }
  const load = runtime({ wx, getApp: () => ({ globalData: { demoMode: false } }),
    Page: spec => {
      page = spec
      page.setData = values => {
        for (const [key, value] of Object.entries(values)) {
          const match = key.match(/^(\w+)\[(\d+)\]\.(\w+)$/)
          if (match) page.data[match[1]][Number(match[2])][match[3]] = value
          else page.data[key] = value
        }
      }
    },
  })
  const products = load('miniprogram/data/demo.ts').demoProducts
  const { filterProducts, sortProducts, salesCount } = load('miniprogram/utils/product.ts')
  assert.equal(sortProducts(products, 'priceAsc').map(p => p.id).join(','), 'p-kyoho,p-shine,p-gift')
  assert.equal(sortProducts(products, 'priceDesc').map(p => p.id).join(','), 'p-gift,p-shine,p-kyoho')
  assert.equal(sortProducts(products, 'sales')[0].id, 'p-kyoho')
  assert.equal(salesCount(products[0]), 363)
  assert.equal(products[0].id, 'p-shine') // Sorting must not mutate source order.
  const noSku = { ...products[0], id: 'empty', skus: [] }
  assert.equal(sortProducts([noSku, ...products], 'priceDesc')[3].id, 'empty')
  assert.equal(filterProducts(products, 'all', ' 阳光 宁夏 ').length, 1)
  assert.equal(filterProducts(products, 'gift', '阳光玫瑰').length, 0)
  assert.equal(filterProducts(products, 'all', '2kg')[0].id, 'p-shine')
  for (const product of products) assert.ok(fs.existsSync(path.join(root, 'miniprogram', product.cover)))

  load('miniprogram/pages/shop/index.ts')
  response = products
  await page.onShow()
  page.search({ detail: { value: '不存在的品种' } })
  await page.submitSearch()
  assert.equal(page.data.products.length, 0)
  assert.equal(page.data.error, '')
  await page.clearSearch()
  assert.equal(page.data.products.length, 3)
  await page.chooseSort({ currentTarget: { dataset: { value: 'price' } } })
  assert.equal(page.data.products[0].id, 'p-kyoho')
  await page.chooseSort({ currentTarget: { dataset: { value: 'price' } } })
  assert.equal(page.data.products[0].id, 'p-gift')
  page.search({ detail: { value: '阳光' } })
  await page.submitSearch()
  assert.equal(page.data.products.length, 1)
  await page.resetFilters()
  assert.equal(page.data.sort, 'default')
  apiError = '网络不可用'
  await page.loadProducts()
  assert.equal(page.data.error, '网络不可用')
  assert.equal(page.data.loading, false)
  apiError = ''
  await page.loadProducts()
  assert.equal(page.data.error, '')
  page.imageError({ currentTarget: { dataset: { id: products[0].id } } })
  assert.equal(page.data.products[0].imageFailed, true)
  const callContainer = wx.cloud.callContainer
  const pending = []
  wx.cloud.callContainer = () => new Promise(resolve => pending.push(resolve))
  const older = page.loadProducts(), newer = page.loadProducts()
  pending[1]({ statusCode: 200, data: { ok: true, data: { items: [products[1]], total: 1, offset: 0, limit: 20 } } })
  await newer
  pending[0]({ statusCode: 200, data: { ok: true, data: { items: products, total: 3, offset: 0, limit: 20 } } })
  await older
  assert.equal(page.data.products.length, 1)
  assert.equal(page.data.products[0].id, 'p-kyoho')
  wx.cloud.callContainer = callContainer

  load('miniprogram/pages/product/index.ts')
  response = structuredClone(products[0])
  response.skus[0].stock = 0
  response.skus[1].stock = 2
  await page.onLoad({ id: response.id })
  assert.equal(page.data.skuIndex, 1)
  page.chooseSku({ currentTarget: { dataset: { index: 0 } } })
  assert.equal(page.data.skuIndex, 1)
  page.plus(); page.plus()
  assert.equal(page.data.quantity, 2)
  apiError = '库存不足'
  await page.addCart()
  assert.equal(toast, '库存不足')
  assert.equal(page.data.submitting, false)
  apiError = ''
  const initialUpdates = updates
  await Promise.all([page.addCart(), page.addCart()])
  assert.equal(updates, initialUpdates + 1)
  response.skus[1].stock = 0
  await page.loadProduct()
  assert.equal(page.data.soldOut, true)
  const beforeSoldOut = updates
  await page.addCart(); page.buyNow()
  assert.equal(updates, beforeSoldOut)
  assert.equal(navigations, 0)
  response.skus = []
  await page.loadProduct()
  assert.equal(page.data.maxQuantity, 0)
  assert.equal(page.data.soldOut, true)
  response = null
  await page.loadProduct()
  assert.match(page.data.error, /已下架/)
  assert.equal(page.data.product, null)
  response = products[0]
  await page.loadProduct()
  assert.equal(page.data.error, '')
  assert.equal(page.data.images.length, 1)
  page.imageError({ currentTarget: { dataset: { index: 0 } } })
  assert.equal(page.data.images[0].failed, true)
}

async function attentionFlows() {
  const storage = new Map()
  let page
  const wx = { getStorageSync: key => storage.get(key), setStorageSync: (key, value) => storage.set(key, value), showToast: () => {}, setNavigationBarTitle: () => {} }
  const globals = { wx, getApp: () => ({ globalData: { demoMode: true } }), Page: spec => { page = spec; page.setData = values => Object.assign(page.data, values) } }
  const load = runtime(globals)
  const { callApi } = load('miniprogram/utils/api.ts')
  const products = load('miniprogram/data/demo.ts').demoProducts
  const set = (kind, enabled) => callApi('product.attention.set', { productId: products[0].id, kind, enabled })
  await Promise.all([set('favorite', true), set('favorite', true)])
  assert.equal((await callApi('product.attention.list', { kind: 'favorite' })).length, 1)
  assert.equal((await callApi('product.detail', { id: products[0].id })).favorite, true)
  // A fresh runtime retains storage, as when reopening the app.
  assert.equal((await runtime(globals)('miniprogram/utils/api.ts').callApi('product.detail', { id: products[0].id })).favorite, true)
  await assert.rejects(() => set('restock', true), /已有货/)
  const stocks = products[0].skus.map(s => s.stock)
  products[0].skus.forEach(s => { s.stock = 0 })
  await set('restock', true)
  load('miniprogram/pages/attention/index.ts')
  page.onLoad({ kind: 'restock' })
  await page.onShow()
  assert.equal(page.data.products[0].ready, false)
  products[0].skus.forEach((s, i) => { s.stock = stocks[i] })
  await page.onShow()
  assert.equal(page.data.products[0].ready, true)
  await page.remove({ currentTarget: { dataset: { id: products[0].id } } })
  assert.equal(page.data.products.length, 0)
  assert.equal((await callApi('product.detail', { id: products[0].id })).favorite, true)
  assert.equal((await callApi('product.detail', { id: products[0].id })).restockReminder, false)
  await set('favorite', false)
  assert.equal((await callApi('product.attention.list', { kind: 'favorite' })).length, 0)
  await assert.rejects(() => set('invalid', true), /参数无效/)
}

async function addressFlows() {
  let page, emitted, privacyAllowed = true, importCancelled = false, privacyRequested = false
  const storage = new Map([['chiji_default_address', { name: '旧地址', phone: '13800000000', province: '浙江省', city: '杭州市', district: '西湖区', detail: '1号' }]])
  const wx = {
    getStorageSync: key => storage.get(key), setStorageSync: (key, value) => storage.set(key, structuredClone(value)), removeStorageSync: key => storage.delete(key),
    showToast: () => {}, navigateBack: () => {}, showModal: () => {},
    requirePrivacyAuthorize: ({ success, fail }) => { setTimeout(() => { privacyRequested = privacyAllowed; privacyAllowed ? success({}) : fail({ errMsg: 'authorize:fail' }) }, 5) },
    chooseAddress: ({ success, fail }) => { assert.equal(privacyRequested, true); if (importCancelled) fail({ errMsg: 'chooseAddress:fail cancel' }); else success({ userName: '微信用户', telNumber: '+86 139-0000-0000', provinceName: '浙江省', cityName: '杭州市', countyName: '西湖区', detailInfo: '2号' }) },
  }
  const load = runtime({ wx, getApp: () => ({ globalData: { demoMode: true } }), Page: spec => {
    page = spec; page.setData = (values, callback) => { Object.assign(page.data, values); callback?.() }; page.getOpenerEventChannel = () => ({ emit: (name, value) => { emitted = value } })
  } })
  const { callApi } = load('miniprogram/utils/api.ts')
  let list = await callApi('address.list')
  assert.equal(list[0].id, 'legacy'); assert.equal(list[0].isDefault, true)
  assert.equal(storage.has('chiji_default_address'), false)
  const added = await callApi('address.save', { address: { ...list[0], id: undefined, isDefault: false, detail: '新增地址' } })
  assert.equal(added.isDefault, false)
  await callApi('address.default', { id: added.id })
  assert.equal((await callApi('address.get')).id, added.id)
  await assert.rejects(() => callApi('address.delete', { id: 'unknown' }), /地址不存在/)
  load('miniprogram/pages/address/index.ts')
  await page.loadAddresses(); await page.importWechat()
  assert.equal(page.data.form.phone, '13900000000')
  assert.equal((await callApi('address.list')).length, 2)
  await page.save(); assert.equal(page.data.addresses.length, 3)
  page.data.selecting = true
  page.select({ currentTarget: { dataset: { id: 'legacy' } } })
  assert.equal(emitted.id, 'legacy')
  assert.equal((await callApi('address.get')).id, added.id)
  page.data.editing = false; importCancelled = true
  await page.importWechat(); assert.equal(page.data.editing, false)
  privacyAllowed = false; await page.importWechat(); assert.equal(page.data.busy, false)
  await callApi('address.delete', { id: added.id })
  list = await callApi('address.list'); assert.equal(list.filter(a => a.isDefault).length, 1)
  for (const address of [...list]) await callApi('address.delete', { id: address.id })
  assert.equal(await callApi('address.get'), null)
  load('miniprogram/pages/checkout/index.ts')
  const a = { id: 'a', isDefault: true }, b = { id: 'b', isDefault: false }
  storage.set('chiji_demo_addresses', [a, b]); page.data.address = b
  await page.onShow(); assert.equal(page.data.address.id, 'b')
  storage.set('chiji_demo_addresses', [a]); await page.onShow(); assert.equal(page.data.address.id, 'a')
}

async function cartBatchFlows() {
  let page, confirm = true, failFavorite = '', calls = [], toasts = [], quoteError = false, quoteOverride = {}, failDelete = false, checkoutItems, destination
  const seed = () => [
    { productId: 'p1', product: { name: '葡萄' }, skuId: 's1', quantity: 2, selected: true, sku: { price: 100, stock: 20, limitPerOrder: 10 } },
    { productId: 'p1', product: { name: '葡萄' }, skuId: 's2', quantity: 1, selected: true, sku: { price: 200, stock: 20, limitPerOrder: 10 } },
    { productId: 'p2', product: { name: '葡萄' }, skuId: 's3', quantity: 1, selected: false, sku: { price: 300, stock: 20, limitPerOrder: 10 } },
  ]
  let cart = seed()
  const wx = {
    getStorageSync: key => key === 'chiji_mini_token' ? 'token' : undefined, showModal: async () => ({ confirm }), showToast: args => toasts.push(args.title),
    setStorageSync: (_, value) => { checkoutItems = value }, navigateTo: args => { destination = args.url },
    cloud: { callContainer: async ({ data: { action, data } }) => {
      calls.push({ action, ...data })
      if (action === 'address.list') return { statusCode: 200, data: { ok: true, data: [{ isDefault: true, province: '浙江省' }] } }
      if (action === 'order.quote') {
        if (quoteError) throw new Error('报价失败')
        const subtotal = data.items.reduce((n, x) => n + cart.find(c => c.skuId === x.skuId).sku.price * x.quantity, 0)
        return { statusCode: 200, data: { ok: true, data: { subtotal, freight: subtotal < 500 ? 100 : 0, freeShippingThreshold: 500, ...quoteOverride } } }
      }
      if (action === 'cart.batch') {
        if (failDelete && data.operation === 'delete') throw new Error('清理失败')
        if (data.operation === 'favorite' && cart.some(item => data.skuIds.includes(item.skuId) && item.productId === failFavorite)) throw new Error('收藏失败')
        cart = data.operation === 'select'
          ? cart.map(item => data.skuIds.includes(item.skuId) ? { ...item, selected: data.selected } : item)
          : cart.filter(item => !data.skuIds.includes(item.skuId))
      }
      return { statusCode: 200, data: { ok: true, data: action === 'product.attention.set' ? { enabled: true } : structuredClone(cart) } }
    } },
  }
  runtime({ wx, getApp: () => ({ globalData: { demoMode: false } }), Page: spec => {
    page = spec; page.setData = values => Object.assign(page.data, values)
  } })('miniprogram/pages/cart/index.ts')
  await page.onShow()
  assert.equal(page.data.selectedCount, 2)
  calls = []
  await page.toggleAll()
  assert.equal(page.data.allSelected, true)
  assert.equal(calls.filter(x => x.action === 'cart.batch').length, 1)
  assert.equal(calls.find(x => x.action === 'cart.batch').operation, 'select')
  await page.toggleAll()
  assert.equal(page.data.selectedCount, 0)
  cart = seed(); page.render(cart)
  confirm = false
  await page.deleteSelected()
  assert.equal(cart.length, 3)
  assert.equal(page.data.busy, false)
  confirm = true
  await page.deleteSelected()
  assert.equal(cart.map(x => x.skuId).join(','), 's3')
  assert.equal(page.data.totalText, '0.00')
  cart = seed(); page.render(cart); calls = []
  await Promise.all([page.moveToFavorites(), page.moveToFavorites()])
  assert.equal(calls.filter(x => x.action === 'cart.batch').length, 1)
  assert.equal(calls[0].operation, 'favorite')
  assert.equal(cart.map(x => x.skuId).join(','), 's3')
  cart = seed(); cart[2].selected = true; page.render(cart); failFavorite = 'p2'
  await page.moveToFavorites()
  assert.equal(cart.map(x => x.skuId).join(','), 's1,s2,s3')
  assert.equal(page.data.items.length, 3)
  assert.equal(page.data.busy, false)
  assert.equal(toasts.at(-1), '收藏失败')
  failFavorite = ''; await page.moveToFavorites()
  assert.equal(page.data.items.length, 0)
  assert.equal(page.data.allSelected, false)
  await page.deleteSelected()
  assert.equal(toasts.at(-1), '请先选择商品')

  const invalid = [
    { productId: 'missing', skuId: 'missing', selected: true, quantity: 1 },
    { productId: 'p1', product: { name: '葡萄' }, skuId: 'removed', selected: true, quantity: 1 },
    { ...seed()[0], skuId: 'sold-out', sku: { ...seed()[0].sku, stock: 0 } },
    { ...seed()[0], skuId: 'disabled', sku: { ...seed()[0].sku, limitPerOrder: 0 } },
  ]
  cart = [...seed(), ...invalid]; page.render(cart)
  assert.equal(page.data.invalidItems.length, 4)
  assert.equal(page.data.totalText, '4.00')
  assert.equal(page.data.selectedCount, 2)
  await page.refreshHint()
  assert.match(page.data.hint, /再购 ¥1.00/)
  assert.equal(page.data.canTopUp, true)
  assert.equal(calls.findLast(x => x.action === 'order.quote').address.province, '浙江省')
  page.checkout()
  assert.equal(checkoutItems.length, 2)
  assert.equal(destination, '/pages/checkout/index')
  confirm = false; await page.clearInvalid()
  assert.equal(cart.length, 7)
  confirm = true; failDelete = true; await page.clearInvalid()
  assert.equal(page.data.invalidItems.length, 4)
  assert.equal(page.data.busy, false)
  failDelete = false; calls = []
  await Promise.all([page.clearInvalid(), page.clearInvalid()])
  assert.equal(calls.filter(x => x.action === 'cart.batch').length, 1)
  assert.equal(cart.length, 3)
  assert.equal(page.data.invalidItems.length, 0)
  await page.toggleAll(); await page.refreshHint()
  assert.match(page.data.hint, /已免基础运费/)
  assert.equal(page.data.canTopUp, false)
  quoteOverride = { surcharge: 30, freight: 30 }; await page.refreshHint()
  assert.match(page.data.hint, /附加运费仍需支付/)
  quoteOverride = { blockedReason: '该地区暂不支持生鲜配送' }; await page.refreshHint()
  assert.match(page.data.hint, /暂不支持/)
  assert.equal(page.data.canTopUp, false)
  quoteOverride = {}; quoteError = true; await page.refreshHint()
  assert.match(page.data.hint, /暂不可用/)
  quoteError = false
  // A quote finishing after deselection must not restore an old hint.
  const pendingHint = page.refreshHint()
  cart = cart.map(x => ({ ...x, selected: false })); page.render(cart)
  await pendingHint
  assert.equal(page.data.hint, '')
  cart = invalid; page.render(cart)
  assert.equal(page.data.items.length, 0)
  assert.equal(page.data.allSelected, false)
  await page.clearInvalid()
  assert.equal(cart.length, 0)
  cart = [{ ...seed()[0], quantity: 25 }]; page.render(cart)
  page.checkout()
  assert.equal(toasts.at(-1), '请先调整商品数量')

  const storage = new Map()
  const demoWx = { getStorageSync: key => storage.get(key), setStorageSync: (key, value) => storage.set(key, structuredClone(value)) }
  const { callApi } = runtime({ wx: demoWx, getApp: () => ({ globalData: { demoMode: true } }) })('miniprogram/utils/api.ts')
  const products = await callApi('product.list')
  const demoSeed = products[0].skus.map(s => ({ productId: products[0].id, skuId: s.id, quantity: 1, selected: true }))
  storage.set('chiji_demo_cart', demoSeed)
  const ids = demoSeed.map(item => item.skuId)
  for (const data of [
    { operation: 'unknown', skuIds: ids }, { operation: 'delete', skuIds: [] },
    { operation: 'delete', skuIds: [ids[0], ids[0]] }, { operation: 'select', skuIds: ids },
    { operation: 'delete', skuIds: [1] }, { operation: 'delete', skuIds: ['x'.repeat(81)] },
    { operation: 'delete', skuIds: Array.from({length: 101}, (_, i) => String(i)) },
  ]) {
    await assert.rejects(() => callApi('cart.batch', data))
    assert.equal(storage.get('chiji_demo_cart').length, demoSeed.length)
  }
  let result = await callApi('cart.batch', { operation: 'select', skuIds: ids, selected: false })
  assert.equal(result.every(item => !item.selected && item.quantity === 1), true)
  result = await callApi('cart.batch', { operation: 'favorite', skuIds: ids })
  assert.equal(result.length, 0)
  assert.equal((await callApi('product.attention.list', {kind: 'favorite'})).length, 1)
  await callApi('cart.batch', { operation: 'favorite', skuIds: ids })
  assert.equal((await callApi('product.attention.list', {kind: 'favorite'})).length, 1)
  storage.set('chiji_demo_cart', [...demoSeed, {productId: 'missing', skuId: 'missing', quantity: 1}])
  assert.equal((await callApi('cart.get')).length, demoSeed.length + 1)
  await assert.rejects(() => callApi('cart.batch', {operation: 'favorite', skuIds: [...ids, 'missing']}), /下架/)
  assert.equal(storage.get('chiji_demo_cart').length, demoSeed.length + 1)
  await callApi('cart.batch', {operation: 'delete', skuIds: ['missing']})
  assert.equal(storage.get('chiji_demo_cart').length, demoSeed.length)
}

async function checkoutOptionsFlows() {
  const storage = new Map()
  const wx = {getStorageSync:k=>storage.get(k),setStorageSync:(k,v)=>storage.set(k,v),removeStorageSync:k=>storage.delete(k)}
  const load=runtime({wx,getApp:()=>({globalData:{demoMode:true}})})
  const {callApi}=load('miniprogram/utils/api.ts')
  const {chinaDate}=load('miniprogram/utils/checkout.ts')
  const products=await callApi('product.list')
  const product=products.find(p=>p.saleMode==='spot' && p.skus.some(s=>s.stock>=2 && s.limitPerOrder>=2))
  const sku=product.skus.find(s=>s.stock>=2 && s.limitPerOrder>=2)
  const items=[{productId:product.id,skuId:sku.id,quantity:2,selected:true}]
  const address={name:'测试',phone:'13800000000',province:'浙江省',city:'杭州市',district:'西湖区',detail:'测试地址'}
  await assert.rejects(()=>callApi('order.quote',{items:[{...items[0],quantity:1}],address,couponCode:'UNKNOWN'}),/不存在/)
  const base=await callApi('order.quote',{items,address})
  const quote=await callApi('order.quote',{items,address,couponCode:' demo10 '})
  assert.equal(quote.total,base.total-1000);assert.equal(quote.freight,base.freight)
  await assert.rejects(()=>callApi('order.create',{idempotencyKey:'flow_checkout_0001',items,address,deliveryDate:chinaDate(-1)}),/配送日期/)
  const order=await callApi('order.create',{idempotencyKey:'flow_checkout_0002',items,address,couponCode:'DEMO10',deliveryDate:chinaDate(3)})
  assert.equal(order.deliveryDate,chinaDate(3));assert.equal(order.quote.total,quote.total)
  await assert.rejects(()=>callApi('order.create',{idempotencyKey:'flow_checkout_0003',items,address,couponCode:'DEMO10'}),/占用/)
  const invoice={kind:'company',title:'测试企业',taxId:'91310000DEMO123456',email:'test@example.com'}
  await assert.rejects(()=>callApi('invoice.apply',{orderId:order.id,invoice}),/已支付/)
  await callApi('order.cancel',{id:order.id})
  assert.equal((await callApi('order.quote',{items,address,couponCode:'DEMO10'})).discount,1000)
  const next=await callApi('order.create',{idempotencyKey:'flow_checkout_0004',items,address,couponCode:'DEMO10'})
  await callApi('payment.create',{orderId:next.id})
  await assert.rejects(()=>callApi('invoice.apply',{orderId:next.id,invoice:{...invoice,taxId:''}}),/税号/)
  assert.equal((await callApi('invoice.apply',{orderId:next.id,invoice})).amount,next.quote.total)
  await assert.rejects(()=>callApi('invoice.apply',{orderId:next.id,invoice}),/已申请/)
  const {mockCall}=runtime()('admin/src/mock.ts')
  const first=(await mockCall('admin.order.list'))[0]
  await assert.rejects(()=>mockCall('admin.invoice.update',{orderId:first.id,status:'ISSUED',number:''}),/号码/)
  await assert.rejects(()=>mockCall('admin.invoice.update',{orderId:first.id,status:'ISSUED',number:'TEST-001'}),/PDF/)
  const fileData = Buffer.from('%PDF-1.4\n%%EOF').toString('base64')
  assert.equal((await mockCall('admin.order.search',{filter:'invoice_pending'})).items.length,1)
  const issued = await mockCall('admin.invoice.update',{orderId:first.id,status:'ISSUED',number:'TEST-001',fileName:'invoice.pdf',fileData})
  assert.equal(issued.emailStatus,'SENT')
  assert.equal((await mockCall('admin.invoice.file',{orderId:first.id})).fileData,fileData)
  assert.equal((await mockCall('admin.invoice.retryEmail',{orderId:first.id})).sentAt,issued.sentAt)
  assert.equal((await mockCall('admin.order.search',{filter:'invoice_pending'})).items.length,0)
  await assert.rejects(()=>mockCall('admin.invoice.update',{orderId:first.id,status:'ISSUED',number:'TEST-002',fileName:'invoice.pdf',fileData}),/没有待处理/)
  assert.equal((await mockCall('admin.order.list'))[0].invoice.number,'TEST-001')
  await assert.rejects(()=>mockCall('admin.coupon.save',{items:[{code:'INVALID SPACE'}]}),/无效/)
}

async function checkoutQuoteRaceFlows() {
  let page; const pending=[]
  const wx={getStorageSync:()=> 'token',showToast:()=>{},cloud:{callContainer:({data})=>new Promise(resolve=>pending.push({data,resolve}))}}
  runtime({wx,getApp:()=>({globalData:{demoMode:false}}),Page:spec=>{page=spec;page.setData=(values,callback)=>{Object.assign(page.data,values);if(callback)callback()}}})('miniprogram/pages/checkout/index.ts')
  page.data.items=[{skuId:'s1'}]
  const first=page.refreshQuote();page.data.couponCode='SAVE10';const second=page.refreshQuote()
  assert.equal(page.data.quoteBusy,true)
  pending[1].resolve({statusCode:200,data:{ok:true,data:{subtotal:10000,freight:1200,discount:1000,total:10200}}});await second
  pending[0].resolve({statusCode:200,data:{ok:true,data:{subtotal:10000,freight:1200,total:11200}}});await first
  assert.equal(page.data.quote.total,10200);assert.equal(page.data.quoteBusy,false)
  const failure=page.refreshQuote();pending[2].resolve({statusCode:200,data:{ok:false,error:{message:'优惠券已失效'}}});await failure
  assert.equal(page.data.quote,null);assert.match(page.data.quoteError,/失效/)
}

async function refundProgressFlows() {
  let page, status = 'REJECTED', fail = false, refreshRequested = false
  const storage = new Map([['chiji_mini_token', 'token']])
  const record = { id: 'rf', orderId: 'order', orderNo: 'CJ1', amount: 100, reason: '重复下单', rejectReason: '请先核实订单信息', createdAt: '2026-10-04T00:00:00Z' }
  const wx = { getStorageSync: k => storage.get(k), setStorageSync: (k,v) => storage.set(k,v), showToast: () => {},
    cloud: { callContainer: async ({data}) => {
      if (data.action === 'refund.list') { refreshRequested = !!data.data.refresh; if(fail) throw new Error('查询失败'); return {statusCode:200,data:{ok:true,data:[{...record,status}]}} }
      if (data.action === 'order.get') return {statusCode:200,data:{ok:true,data:{id:'order',quote:{total:100},paymentStatus:status==='SUCCEEDED'?'REFUNDED':status==='APPROVED'||status==='FAILED'?'REFUNDING':'PAID',refundStatus:status,fulfillmentStatus:'PREPARING'}}}
      throw new Error(data.action)
    } }
  }
  const load = runtime({wx,getApp:()=>({globalData:{demoMode:false,cloudEnv:'test',cloudService:'test'}}),Page:spec=>{page=spec;page.setData=v=>Object.assign(page.data,v)}})
  load('miniprogram/pages/refund/index.ts'); page.onLoad({id:'order'}); await page.load()
  assert.equal(page.data.refunds[0].rejectReason, record.rejectReason)
  assert.equal(page.data.refunds[0].statusText, '退款未通过'); assert.equal(page.data.eligible,true)
  for (status of ['APPLIED','APPROVED','FAILED','SUCCEEDED']) {
    await page.load(true); assert.equal(refreshRequested,true); assert.equal(page.data.eligible,false)
    assert.equal(page.data.refunds[0].steps[2].done,status==='SUCCEEDED')
  }
  fail=true; await page.load(true)
  assert.match(page.data.error,/查询失败/); assert.equal(page.data.refunds[0].status,'SUCCEEDED')
  assert.equal(page.data.loading,false); assert.equal(page.data.eligible,false)
  const format=load('miniprogram/utils/format.ts')
  assert.equal(format.orderStatusText({refundStatus:'APPROVED'}),'退款处理中')
  assert.equal(format.orderStatusText({refundStatus:'FAILED'}),'退款异常待处理')
  // Demo approval/rejection is separate from the real mini-program transport.
  const {mockCall}=runtime()('admin/src/mock.ts')
  const refund=(await mockCall('admin.refund.list'))[0]
  await assert.rejects(()=>mockCall('admin.refund.reject',{refundId:refund.id,reason:'  '}),/拒绝原因/)
  await mockCall('admin.refund.reject',{refundId:refund.id,reason:' 地址需要核对 '})
  assert.equal((await mockCall('admin.refund.list'))[0].rejectReason,'地址需要核对')
  await assert.rejects(()=>mockCall('admin.refund.approve',{refundId:refund.id}),/状态已变化/)
}

;(async () => {
  if (process.argv.includes('--cart-only')) {
    await cartBatchFlows()
    console.log('Cart batch selection, deletion, favorites, validation, atomic failure, retry and duplicate submission flows passed.')
    return
  }
  if (process.argv.includes('--invoice-only')) {
    await checkoutOptionsFlows()
    console.log('Invoice application, PDF upload, email status, download and duplicate protection flows passed.')
    return
  }
  if (process.argv.includes('--products-only')) {
    await productFlows()
    console.log('Product search, sorting, sold-out, stock limits, image fallback and error recovery flows passed.')
    return
  }
  await refundProgressFlows()
  await adminFlows()
  await miniFlows()
  await productFlows()
  await addressFlows()
  await attentionFlows()
  await cartBatchFlows()
  await checkoutOptionsFlows()
  await checkoutQuoteRaceFlows()
  console.log('Coupon, delivery date, invoice, admin, privacy, tracking, home, product search, sold-out, stock limits, image fallback, sorting, favorites, restock reminders and error recovery flows passed.')
})().catch(error => { console.error(error); process.exitCode = 1 })
