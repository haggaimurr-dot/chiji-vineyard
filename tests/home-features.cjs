const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const assert = require('node:assert/strict')
const ts = require('typescript')
const root = path.resolve(__dirname, '..')

async function run() {
  let page, modal, failSet = false, deferList = false, failProducts = false, failNotices = false, failRead = false
  const pending = [], storage = new Map(), cache = new Map()
  const app = { globalData: { demoMode: true } }
  const wx = {
    getStorageSync: key => storage.get(key), setStorageSync: (key, value) => storage.set(key, value),
    setNavigationBarTitle() {}, stopPullDownRefresh() {}, switchTab() {}, showToast() {}, showModal: args => { modal = args },
    navigateTo: args => { wx.destination = args.url },
  }
  let api
  function load(filename) {
    filename = path.resolve(root, filename)
    if (cache.has(filename)) return cache.get(filename).exports
    const module = { exports: {} }; cache.set(filename, module)
    const source = ts.transpileModule(fs.readFileSync(filename, 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS } }).outputText
    vm.runInNewContext(source, { module, exports: module.exports, Date, setTimeout, clearTimeout, wx, getApp: () => app,
      Page: value => { page = value; value.setData = updates => Object.assign(value.data, updates) },
      require: name => name.endsWith('/api') && !filename.endsWith(path.join('utils', 'api.ts')) ? { callApi: async (action, data) => {
        if (action === 'product.list' && failProducts) throw new Error('商品断网')
        if (action === 'activity.notice.list' && failNotices) throw new Error('通知断网')
        if (action === 'activity.notice.read' && failRead) throw new Error('已读失败')
        if (action === 'activity.reservation.set' && failSet) throw new Error('模拟断网')
        if (action === 'activity.reservation.status' && deferList) return new Promise(resolve => pending.push(ids => resolve({ items: ids.map(activityId => ({ activityId, status: 'RESERVED', confirmed: 1, waiting: 0, position: 0 })) })))
        return api.callApi(action, data)
      } } : load(path.resolve(path.dirname(filename), `${name}.ts`)),
    }, { filename })
    return module.exports
  }
  api = load('miniprogram/utils/api.ts')
  const helper = load('miniprogram/utils/home-features.ts')
  const home = load('miniprogram/data/home.ts').demoHome
  // Future and ended activities avoid dependence on the machine's current date.
  home.activities[0].startDate = '2099-01-01'; home.activities[0].endDate = '2099-01-31'
  load('miniprogram/pages/home/index.ts')
  await page.onShow()
  assert.equal(page.data.detailsExpanded, false, 'home operational sections start collapsed')
  assert.equal(page.data.orchardImage, home.banners[0].image, 'quiet home uses first configured image')
  page.toggleDetails()
  assert.equal(page.data.detailsExpanded, true)
  page.toggleDetails()
  assert.equal(page.data.detailsExpanded, false)
  page.imageError({ currentTarget: { dataset: { kind: 'orchard', src: page.data.orchardImage, version: page.data.requestVersion - 1 } } })
  assert.equal(page.data.orchardImageFailed, false, 'old image failures cannot replace current photo')
  page.imageError({ currentTarget: { dataset: { kind: 'orchard', src: page.data.orchardImage, version: page.data.requestVersion } } })
  assert.equal(page.data.orchardImageFailed, true, 'current photo failure shows its fallback')
  await page.onShow()
  assert.equal(page.data.orchardImageFailed, false, 'refresh retries the orchard photo')
  assert.equal(page.data.reservationActivities.length, 1)
  const id = home.activities[0].id, event = { currentTarget: { dataset: { id } } }
  page.changeReservation(event)
  await modal.success({ confirm: false })
  assert.equal(page.data.reservations.length, 0)
  page.changeReservation(event)
  const submit = modal.success({ confirm: true })
  page.changeReservation(event)
  await submit
  assert.equal(page.data.reservations.length, 1)
  await api.callApi('activity.reservation.set', { activityId: id, enabled: true })
  assert.equal((await api.callApi('activity.reservation.list')).length, 1, 'duplicate request is idempotent')
  let notices = await api.callApi('activity.notice.list', {})
  assert.equal(notices.items.length, 1, 'duplicate reservation produces one success notice')
  assert.equal(notices.unread, 1)
  assert.equal(modal.title, '预约成功')
  await modal.success({ confirm: true })
  assert.equal(wx.destination, '/pages/activity-notices/index')
  await page.onShow()
  assert.equal(page.data.reservationActivities[0].reserved, true, 'refresh retains reservation')
  failSet = true
  page.changeReservation(event); await modal.success({ confirm: true })
  assert.equal(page.data.reservationActivities[0].reserved, true, 'failed cancellation retains state')
  assert.equal(page.data.reservationBusy, '')
  failSet = false
  const original = home.activities.splice(0)
  await page.onShow()
  assert.deepEqual(Array.from(page.data.unavailableReservations), [id], 'removed activity can still be cancelled')
  page.changeReservation(event); await modal.success({ confirm: true })
  assert.equal(page.data.reservations.length, 0)
  assert.equal((await api.callApi('activity.notice.list', {})).items.length, 1, 'cancellation keeps success snapshot')
  home.activities.push(...original)
  home.activities[0].endDate = '2000-01-01'
  await assert.rejects(() => api.callApi('activity.reservation.set', { activityId: id, enabled: true }), /已结束/)
  await assert.rejects(() => api.callApi('activity.reservation.set', { activityId: 'missing', enabled: true }), /未开放/)
  await assert.rejects(() => api.callApi('activity.reservation.set', { activityId: id, enabled: 'true' }), /参数/)

  const product = (id, category, favorite, stock = 1) => ({ id, category, favorite, skus: [{ price: 100, stock, limitPerOrder: 1 }] })
  const products = [product('family', 'family', false), product('gift', 'gift', true), product('sold-out', 'gift', true, 0), product('other-gift', 'gift', false)]
  let results = helper.recommendProducts(products, 'family', true, [])
  assert.equal(results[0].id, 'family')
  assert.match(results[0].reason, /偏好/)
  results = helper.recommendProducts(products, 'all', true, [])
  assert.equal(results[0].id, 'gift'); assert.match(results[0].reason, /收藏/)
  assert.equal(results[1].id, 'other-gift')
  assert.ok(results.every(p => p.id !== 'sold-out'))
  assert.equal(helper.recommendProducts(products, 'gift', false, ['family'])[0].id, 'family', 'disabled mode ignores personal signals')
  assert.equal(helper.recommendProducts(products, 'all', true, [], 1)[0].id, 'other-gift')
  assert.equal(helper.recommendProducts([], 'all', true, []).length, 0)
  page.choosePreference({ currentTarget: { dataset: { id: 'gift' } } })
  await page.onShow(); assert.equal(page.data.preference, 'gift')
  page.togglePersonalization({ detail: { value: false } })
  await page.onShow(); assert.equal(page.data.personalized, false)

  deferList = true
  const first = page.loadReservations(), second = page.loadReservations()
  pending[1]([]); await second
  pending[0]([id]); await first
  assert.equal(page.data.reservations.length, 0, 'older status response cannot overwrite latest')
  deferList = false
  const activities = load('miniprogram/utils/activity.ts')
  const sample = (id, startDate, endDate) => ({ id, title: id, startDate, endDate })
  const reminders = activities.activityReminders([
    sample('tomorrow', '2026-10-08', '2026-10-10'),
    sample('today', '2026-10-07', '2026-10-10'),
    sample('ongoing', '2026-10-01', '2026-10-07'),
    sample('ended', '2026-10-01', '2026-10-06'),
    sample('not-reserved', '2026-10-07', '2026-10-10'),
  ], ['tomorrow', 'today', 'ongoing', 'ended'], '2026-10-07')
  assert.deepEqual(Array.from(reminders, r => r.reminder), ['活动进行中', '今天开始', '明天开始'])
  assert.equal(activities.activityStatus(sample('x', '2026-10-07', '2026-10-07'), '2026-10-07'), '进行中')
  assert.equal(activities.activityReminders([sample('far', '2026-10-09', '2026-10-10')], ['far'], '2026-10-07')[0].reminder, '还有2天开始')
  home.activities[0].startDate = '2099-01-01'; home.activities[0].endDate = '2099-01-31'
  await page.onShow()
  page.openActivity({ currentTarget: { dataset: { index: 0 } } })
  assert.equal(wx.destination, `/pages/activity/index?id=${id}`)
  const homePage = page
  load('miniprogram/pages/activity/index.ts')
  page.onLoad({ id })
  await page.onShow()
  assert.equal(page.data.activity.id, id)
  assert.equal(page.data.status, '即将开始')
  assert.equal(page.data.reserved, false)
  page.changeReservation(event); await modal.success({ confirm: true })
  assert.equal(page.data.reserved, true)
  assert.equal((await api.callApi('activity.notice.list', {})).items.length, 2, 'new reservation generates a new success notice')
  home.activities[0].cover = 'https://example.com/activity.png'
  home.activities[0].location = '果园体验区'
  await page.onShow()
  assert.equal(page.data.activity.location, '果园体验区')
  assert.equal(page.onShareAppMessage().path, `/pages/activity/index?id=${id}`)
  assert.equal(page.onShareAppMessage().imageUrl, home.activities[0].cover)
  page.coverError({currentTarget:{dataset:{version:page.data.requestVersion-1}}})
  assert.equal(page.data.coverFailed, false, 'old cover failure ignored')
  page.coverError({currentTarget:{dataset:{version:page.data.requestVersion}}})
  assert.equal(page.onShareAppMessage().imageUrl, undefined)
  await homePage.onShow()
  assert.equal(homePage.data.activityReminders[0].id, id, 'detail reservation synchronizes to home reminder')
  page.changeReservation(event); await modal.success({ confirm: true })
  assert.equal(page.data.reserved, false)
  await homePage.onShow()
  assert.equal(homePage.data.activityReminders.length, 0, 'cancellation removes home reminder')
  home.activities[0].startDate = '2000-01-01'
  await page.onShow()
  page.openProduct()
  assert.equal(wx.destination, '/pages/product/index?id=p-shine')
  failProducts = true
  await page.onShow()
  assert.equal(page.data.error, '', 'product failure does not hide activity')
  assert.ok(page.data.productError)
  assert.equal(page.data.productAvailable, false)
  failProducts = false
  // An obsolete reservation response must not overwrite a newer detail load.
  deferList = true
  const oldLoad = page.onShow()
  while (pending.length < 3) await new Promise(resolve => setTimeout(resolve, 10))
  const newLoad = page.onShow()
  while (pending.length < 4) await new Promise(resolve => setTimeout(resolve, 10))
  pending[3]([]); await newLoad
  pending[2]([id]); await oldLoad
  assert.equal(page.data.reserved, false)
  deferList = false
  const removed = home.activities.splice(0)
  await page.onShow()
  assert.match(page.data.error, /不存在/)
  assert.equal(page.data.activity, null)
  home.activities.push(...removed)
  page.onLoad({ id: '' })
  await page.onShow()
  assert.match(page.data.error, /链接无效/)
  assert.equal(page.onShareAppMessage().path, '/pages/home/index')
  const originalTitle = (await api.callApi('activity.notice.list', {})).items[0].title
  home.activities[0].title = '新的活动标题'
  assert.equal((await api.callApi('activity.notice.list', {})).items[0].title, originalTitle, 'notice is immutable activity snapshot')
  await assert.rejects(() => api.callApi('activity.notice.list', {offset:-1}), /分页/)
  await assert.rejects(() => api.callApi('activity.notice.list', {limit:51}), /分页/)
  load('miniprogram/pages/activity-notices/index.ts')
  await page.onShow()
  assert.equal(page.data.items.length, 2)
  assert.equal(page.data.unread, 2)
  const noticeEvent = {currentTarget:{dataset:{id:page.data.items[0].id}}}
  failRead = true
  await page.openActivity(noticeEvent)
  assert.equal(page.data.unread, 2, 'failed read retains unread state')
  failRead = false
  await page.openActivity(noticeEvent)
  assert.equal(page.data.unread, 1)
  assert.equal(wx.destination, `/pages/activity/index?id=${id}`)
  await page.onShow()
  assert.equal(page.data.unread, 1, 'read survives refresh')
  await page.openActivity(noticeEvent)
  assert.equal(page.data.unread, 1, 'reopening a read notice does not decrement count')
  const noticeKey = load('miniprogram/utils/activity-notice.ts').demoActivityNoticeKey
  storage.set(noticeKey, Array.from({length:25},(_,i)=>({id:`n-${i}`,activityId:id,title:'历史预约',startDate:'2099-01-01',endDate:'2099-01-31',createdAt:'2026-10-07T00:00:00Z',read:false})))
  await page.onShow()
  assert.equal(page.data.items.length, 20)
  assert.equal(page.data.hasMore, true)
  assert.equal(page.data.items[0].timeText, '2026-10-07 08:00')
  failNotices = true
  await page.more()
  assert.equal(page.data.items.length, 20, 'append failure keeps loaded notices')
  assert.match(page.data.error, /通知断网/)
  failNotices = false
  await page.retry()
  assert.equal(page.data.items.length, 25)
  assert.equal(page.data.hasMore, false)
  // Capacity mock acceptance uses simulated other-user occupancy on this device.
  home.activities[0].capacity = 1; home.activities[0].waitlistEnabled = true
  home.activities[0].reservationEnabled = true; home.activities[0].endDate = '2099-01-31'
  await api.callApi('activity.reservation.set', { activityId: id, enabled: false })
  storage.set('chiji_demo_activity_occupancy', { [id]: 1 })
  const beforeWait = (await api.callApi('activity.notice.list', { limit: 50 })).items.length
  assert.equal((await api.callApi('activity.reservation.set', { activityId: id, enabled: true })).status, 'WAITLISTED')
  await api.callApi('activity.reservation.set', { activityId: id, enabled: true })
  assert.equal((await api.callApi('activity.reservation.list')).length, 0, 'waitlist does not masquerade as a confirmation')
  notices = await api.callApi('activity.notice.list', { limit: 50 })
  assert.equal(notices.items.length, beforeWait + 1, 'duplicate waitlist request generates one notice')
  assert.equal(notices.items[0].kind, 'WAITLISTED')
  await homePage.onShow()
  assert.equal(homePage.data.reservationActivities[0].waitlisted, true)
  assert.equal(homePage.data.reservationActivities[0].position, 1)
  assert.equal(homePage.data.activityReminders.length, 0, 'candidate gets no confirmed reminder')
  storage.set('chiji_demo_activity_occupancy', {})
  await homePage.onShow()
  assert.equal(homePage.data.reservationActivities[0].reserved, true, 'vacancy promotes mock candidate')
  assert.equal(homePage.data.reservationActivities[0].waitlisted, false)
  notices = await api.callApi('activity.notice.list', { limit: 50 })
  assert.equal(notices.items[0].kind, 'PROMOTED')
  await homePage.onShow()
  assert.equal((await api.callApi('activity.notice.list', { limit: 50 })).items.length, beforeWait + 2, 'promotion notice is idempotent')
  await api.callApi('activity.reservation.set', { activityId: id, enabled: false })
  storage.set('chiji_demo_activity_occupancy', { [id]: 1 })
  home.activities[0].waitlistEnabled = false
  await assert.rejects(() => api.callApi('activity.reservation.set', { activityId: id, enabled: true }), /名额已满/)
  home.activities[0].waitlistEnabled = true
  await api.callApi('activity.reservation.set', { activityId: id, enabled: true })
  home.activities[0].reservationEnabled = false
  storage.set('chiji_demo_activity_occupancy', {})
  assert.equal((await api.callApi('activity.reservation.status')).items[0].status, 'WAITLISTED', 'closed activity does not promote')
  const noticeCount = (await api.callApi('activity.notice.list', { limit: 50 })).items.length
  await api.callApi('activity.reservation.set', { activityId: id, enabled: false })
  assert.equal((await api.callApi('activity.reservation.status')).items[0].status, 'NONE')
  assert.equal((await api.callApi('activity.notice.list', { limit: 50 })).items.length, noticeCount, 'cancellation keeps history')
  console.log('Capacity mock passed: full/disabled admission, waitlist position, duplicate protection, vacancy promotion, notification history and closure')
  console.log('Activity additions passed: success confirmation, durable snapshots, read/retry, pagination, topic cover and sharing')
  console.log('Home features passed: reservations, cancellation, refresh, failures, stale responses, recommendation preferences, detail navigation, reminders, cross-page synchronization and opt-out')
}
run().catch(error => { console.error(error); process.exitCode = 1 })
