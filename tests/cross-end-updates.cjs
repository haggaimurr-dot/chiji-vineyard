// Client integration checks using the admin demo store and a simulated cloud
// transport. Formal MySQL + WeChat acceptance is documented separately.
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
      require: name => load(path.resolve(path.dirname(filename), `${name}.ts`)),
    })
    vm.runInContext(source, context, { filename })
    return module.exports
  }
}

async function run() {
  const { mockCall } = runtime()('admin/src/mock.ts')
  let page, destination = '', toast = '', modal, refreshes = 0, copied = '', logins = 0
  let failAction = '', deferred = false
  const pending = []
  const storage = new Map([['chiji_mini_token', 'fixture-token']])
  const response = async action => {
    if (action === failAction) throw new Error('验收：网络中断')
    if (action === 'home.get') return (await mockCall('admin.config.get')).home
    if (action === 'privacy.get') {
      const policy = (await mockCall('admin.config.get')).privacy
      if (!policy.published) throw new Error('隐私政策正在完善，请联系商家')
      return policy
    }
    if (action === 'product.list') return (await mockCall('admin.product.list')).filter(p => p.status === 'PUBLISHED')
    if (action === 'article.list') return (await mockCall('admin.article.list')).filter(a => a.status === 'PUBLISHED')
    throw new Error(`Unexpected action ${action}`)
  }
  const wx = {
    getStorageSync: key => storage.get(key), setStorageSync: (key, value) => storage.set(key, value), removeStorageSync: key => storage.delete(key),
    login: async () => { logins++; throw new Error('政策读取不得发起微信登录') },
    setNavigationBarTitle() {}, showToast: args => { toast = args.title }, showModal: args => { modal = args },
    navigateTo: args => { destination = args.url }, switchTab: args => { destination = args.url },
    stopPullDownRefresh: () => { refreshes++ }, setClipboardData: args => { copied = args.data },
    cloud: { callContainer: async ({ data }) => {
      if (data.action === 'activity.reservation.status') return { statusCode: 200, data: { ok: true, data: { items: [] } } }
      if (data.action === 'activity.reservation.list') return { statusCode: 200, data: { ok: true, data: [] } }
      if (deferred) return new Promise((resolve, reject) => pending.push({ action: data.action, resolve, reject }))
      return { statusCode: 200, data: { ok: true, data: await response(data.action) } }
    } },
  }
  const load = runtime({ wx, getApp: () => ({ globalData: { demoMode: false, cloudEnv: 'fixture-env', cloudService: 'fixture-service' } }),
    Page: spec => { page = spec; page.setData = updates => {
      for (const [key, value] of Object.entries(updates)) {
        const fields = key.replace(/\[(\d+)\]/g, '.$1').split('.')
        let target = spec.data
        for (const field of fields.slice(0, -1)) target = target[field]
        target[fields.at(-1)] = value
      }
    } },
  })
  const send = (request, result) => request.resolve({ statusCode: 200, data: { ok: true, data: structuredClone(result) } })
  const snapshot = async () => ({
    'home.get': await response('home.get'), 'product.list': await response('product.list'), 'article.list': await response('article.list'),
  })
  const products = await mockCall('admin.product.list'), articles = await mockCall('admin.article.list')
  let config = await mockCall('admin.config.get')
  for (const banner of config.home.banners) assert.ok(fs.existsSync(path.join(root, 'admin/public', banner.image)), 'packaged demo banner exists')
  const today = new Date(Date.now() + 8 * 3600000).toISOString().slice(0, 10)
  Object.assign(config.home, {
    banners: [{ title: '保存后的首页', copy: '跨端检查', image: 'https://fixture.example/home-v1.jpg', button: '查看商品', target: 'product', targetId: products[0].id }],
    productIds: [products[1].id, products[0].id], articleIds: [articles[1].id, articles[0].id],
    activities: [{ id: 'cross-end-activity', reservationEnabled: true, capacity: 2, waitlistEnabled: true, title: '保存后的活动', summary: '活动简介', content: '完整活动规则', startDate: today, endDate: today, productId: products[0].id }],
    announcements: [{ title: '保存后的公告', content: '完整公告', publishedAt: today }],
    launchCalendar: [{ variety: '保存后的品种', note: '跨年产期', startDate: '2026-11-20', endDate: '2027-02-10', productId: products[0].id }],
  })
  for (const capacity of [-1, 1.5, 100001]) await assert.rejects(() => mockCall('admin.home.save', { home: { ...config.home, activities: [{ ...config.home.activities[0], capacity }] } }), /容量/)
  await mockCall('admin.home.save', { home: config.home })
  const savedConfig = await mockCall('admin.config.get')
  assert.equal(savedConfig.home.activities[0].capacity, 2)
  assert.equal(savedConfig.home.activities[0].waitlistEnabled, true)
  load('miniprogram/pages/home/index.ts')
  const home = page
  await home.onShow()
  assert.equal(home.data.home.banners[0].title, '保存后的首页')
  assert.equal(home.data.home.banners[0].image, 'https://fixture.example/home-v1.jpg')
  assert.equal(home.data.products.map(p => p.id).join(','), `${products[1].id},${products[0].id}`)
  assert.equal(home.data.articles.map(a => a.id).join(','), `${articles[1].id},${articles[0].id}`)
  assert.equal(home.data.activities[0].title, '保存后的活动')
  assert.equal(home.data.reservationActivities[0].capacity, 2)
  assert.equal(home.data.reservationActivities[0].waitlistEnabled, true)
  assert.equal(home.data.reservationError, '')
  assert.equal(home.data.announcements[0].title, '保存后的公告')
  assert.equal(home.data.calendarItems[0].variety, '保存后的品种')
  const imageEvent = (kind, src, version = home.data.requestVersion) => ({ currentTarget: { dataset: { kind, index: 0, src, version } } })
  home.imageError(imageEvent('banner', home.data.home.banners[0].image))
  home.imageError(imageEvent('product', home.data.products[0].cover))
  home.imageError(imageEvent('article', home.data.articles[0].cover))
  assert.equal(home.data.bannerImageFailed[0], true)
  assert.equal(home.data.products[0].imageFailed, true)
  assert.equal(home.data.articles[0].imageFailed, true)
  const oldImageVersion = home.data.requestVersion
  const replacement = structuredClone(config.home)
  replacement.banners[0].image = 'https://fixture.example/home-v2.jpg'
  replacement.activities[0].capacity = 3
  replacement.activities[0].waitlistEnabled = false
  replacement.storyTitle = '更新后的果园介绍'
  replacement.cooperationCopy = '更新后的合作说明'
  replacement.announcements.push({ title: '未来公告不可见', content: '未来正文', publishedAt: '2099-01-01' })
  await mockCall('admin.home.save', { home: replacement })
  await home.onPullDownRefresh()
  assert.equal(home.data.home.banners[0].image, replacement.banners[0].image)
  assert.equal(home.data.home.storyTitle, replacement.storyTitle)
  assert.equal(home.data.reservationActivities[0].capacity, 3)
  assert.equal(home.data.reservationActivities[0].waitlistEnabled, false)
  assert.equal(home.data.home.cooperationCopy, replacement.cooperationCopy)
  assert.equal(home.data.announcements.length, 1)
  assert.equal(home.data.bannerImageFailed[0], false)
  assert.equal(home.data.products[0].imageFailed, false)
  home.imageError(imageEvent('banner', replacement.banners[0].image, oldImageVersion))
  home.imageError(imageEvent('banner', 'https://fixture.example/home-v1.jpg'))
  assert.equal(home.data.bannerImageFailed[0], false, 'obsolete image errors must not hide a replacement')
  home.chooseMonth({ currentTarget: { dataset: { index: 1 } } })
  assert.equal(home.data.calendarItems.length, 1, 'January includes the cross-year season')
  home.chooseMonth({ currentTarget: { dataset: { index: 6 } } })
  assert.equal(home.data.calendarItems.length, 0)
  home.chooseMonth({ currentTarget: { dataset: { index: 0 } } })
  const removed = { ...replacement, activities: [], announcements: [], launchCalendar: [] }
  await mockCall('admin.home.save', { home: removed })
  await home.onShow()
  for (const field of ['activities', 'announcements', 'calendarItems']) assert.equal(home.data[field].length, 0, `${field} removed on next show`)
  await mockCall('admin.home.save', { home: replacement })
  await home.onShow()
  home.openBanner({ currentTarget: { dataset: { index: 0 } } })
  assert.equal(destination, `/pages/product/index?id=${products[0].id}`)
  home.openActivity({ currentTarget: { dataset: { index: 0 } } })
  assert.match(destination, /^\/pages\/activity\/index\?/)
  load('miniprogram/pages/activity/index.ts')
  const activityPage = page
  activityPage.onLoad(home.data.activities[0].id ? { id: home.data.activities[0].id } : { index: '0', title: home.data.activities[0].title })
  await activityPage.onShow()
  assert.equal(activityPage.data.activity.title, '保存后的活动')
  assert.equal(activityPage.data.productAvailable, true)

  // Content taken down after a successful save disappears on the next show.
  await mockCall('admin.product.save', { product: { ...products[0], status: 'DRAFT' } })
  await mockCall('admin.article.save', { article: { ...articles[0], status: 'DRAFT' } })
  await home.onShow()
  assert.equal(home.data.products.map(p => p.id).join(','), products[1].id)
  assert.equal(home.data.articles.map(a => a.id).join(','), articles[1].id)
  assert.equal(home.data.bannerAvailable[0], false)
  assert.equal(home.data.activities[0].available, false)
  assert.equal(home.data.calendarItems[0].available, false)
  destination = ''
  home.openBanner({ currentTarget: { dataset: { index: 0 } } })
  assert.equal(destination, '')
  assert.match(toast, /下架|不可用/)
  await activityPage.onShow()
  assert.equal(activityPage.data.productAvailable, false)
  activityPage.openProduct()
  assert.equal(destination, '', 'activity detail must not navigate to an unpublished product')

  // A transport error clears the previous campaign and product data.
  failAction = 'home.get'
  await home.onShow()
  assert.equal(home.data.loading, false)
  assert.match(home.data.error, /网络中断/)
  for (const field of ['products', 'articles', 'activities', 'announcements', 'calendarItems']) assert.equal(home.data[field].length, 0)
  assert.equal(home.data.home.banners.length, 0)
  failAction = ''
  await home.onPullDownRefresh()
  assert.equal(home.data.error, '')
  assert.equal(refreshes, 2)

  // A delayed older request cannot replace a more recent saved campaign.
  const currentProduct = (await mockCall('admin.product.list')).find(p => p.id === products[0].id)
  const currentArticle = (await mockCall('admin.article.list')).find(a => a.id === articles[0].id)
  await mockCall('admin.product.save', { product: { ...currentProduct, status: 'PUBLISHED' } })
  await mockCall('admin.article.save', { article: { ...currentArticle, status: 'PUBLISHED' } })
  const oldSnapshot = await snapshot()
  deferred = true
  const older = home.onShow()
  config = await mockCall('admin.config.get')
  config.home.banners[0].title = '更新后的首页'
  await mockCall('admin.home.save', { home: config.home })
  const newSnapshot = await snapshot()
  const newer = home.onShow()
  assert.equal(pending.length, 6)
  for (const request of pending.splice(3)) send(request, newSnapshot[request.action])
  await newer
  for (const request of pending.splice(0)) send(request, oldSnapshot[request.action])
  await older
  assert.equal(home.data.home.banners[0].title, '更新后的首页')
  assert.equal(home.data.loading, false)
  const olderFailure = home.onShow(), newest = home.onShow()
  for (const request of pending.splice(3)) send(request, newSnapshot[request.action])
  await newest
  for (const request of pending.splice(0)) request.reject(new Error('较旧请求的错误'))
  await olderFailure
  assert.equal(home.data.error, '')
  const hidden = home.onShow()
  home.onHide()
  for (const request of pending.splice(0)) send(request, oldSnapshot[request.action])
  await hidden
  assert.equal(home.data.home.banners[0].title, '更新后的首页')
  deferred = false

  // Policy updates are fetched on every show without authenticating readers.
  storage.delete('chiji_mini_token')
  config = await mockCall('admin.config.get')
  Object.assign(config.privacy, { published: true, operator: '验收主体', address: '验收地址', contact: 'privacy@example.com', version: 'acceptance-v1' })
  await mockCall('admin.privacy.save', { privacy: config.privacy })
  load('miniprogram/pages/privacy/index.ts')
  const privacy = page
  await privacy.onShow()
  assert.equal(privacy.data.policy.version, 'acceptance-v1')
  assert.equal(logins, 0)
  config.privacy.version = 'acceptance-v2'
  config.privacy.content[0] = '更新后的政策正文'
  await mockCall('admin.privacy.save', { privacy: config.privacy })
  await privacy.onShow()
  assert.equal(privacy.data.policy.version, 'acceptance-v2')
  assert.equal(privacy.data.policy.content[0], '更新后的政策正文')
  privacy.copyContact()
  assert.equal(copied, 'privacy@example.com')
  config.privacy.published = false
  await mockCall('admin.privacy.save', { privacy: config.privacy })
  await privacy.onShow()
  assert.equal(privacy.data.policy, null)
  assert.match(privacy.data.error, /完善/)
  copied = ''
  privacy.copyContact()
  assert.equal(copied, '')
  config.privacy.published = true
  await mockCall('admin.privacy.save', { privacy: config.privacy })
  await privacy.onPullDownRefresh()
  assert.equal(refreshes, 3)
  assert.equal(privacy.data.error, '')
  deferred = true
  const stalePolicy = privacy.onShow(), latestPolicy = privacy.onShow()
  assert.equal(pending.length, 2)
  assert.equal(privacy.data.policy, null, 'old policy is hidden during a refresh')
  send(pending[1], { ...config.privacy, version: 'acceptance-v3' })
  await latestPolicy
  send(pending[0], { ...config.privacy, version: 'acceptance-v1' })
  await stalePolicy
  assert.equal(privacy.data.policy.version, 'acceptance-v3')
  pending.splice(0)
  const unpublishedResponse = privacy.onShow()
  send(pending.shift(), { ...config.privacy, published: false })
  await unpublishedResponse
  assert.equal(privacy.data.policy, null, 'formal mode must not display an unpublished policy, even if the transport returns it')
  assert.match(privacy.data.error, /完善/)
  const hiddenPolicy = privacy.onShow()
  privacy.onHide()
  send(pending.shift(), config.privacy)
  await hiddenPolicy
  assert.equal(privacy.data.policy, null)
  deferred = false
  await privacy.onShow()
  assert.equal(privacy.data.policy.version, 'acceptance-v2')
  assert.equal(logins, 0)
  console.log('跨端客户端流程通过：保存读取、再进入刷新、下架目标、异常清空、撤回政策、请求竞态、下拉刷新（模拟传输，非正式云环境验收）')
}

run().catch(error => { console.error(error); process.exitCode = 1 })
