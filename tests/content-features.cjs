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
    vm.runInNewContext(source, { module, exports: module.exports, console, Error, setTimeout, clearTimeout, ...globals, require: name => load(path.resolve(path.dirname(filename), `${name}.ts`)) }, { filename })
    return module.exports
  }
}

function instantiate(spec, renderCallbacks) {
  const page = { ...spec, data: structuredClone(spec.data) }
  page.setData = (updates, callback) => {
    Object.assign(page.data, updates)
    if (callback && renderCallbacks && updates.article) renderCallbacks.push(callback)
    else callback?.()
  }
  return page
}

async function run() {
  let mode = true, spec, destination = '', readBehavior = 'success', failNextDetail = false
  const storage = new Map(), calls = [], remoteEvents = new Set()
  const remoteArticle = {
    id: 'real-article', title: '真实课堂内容', summary: '新版本', cover: 'https://example.com/cover.jpg',
    variety: '通用', stage: '采后管理', type: 'article', publishedAt: '2026-10-04',
    content: ['保留旧段落兼容'], contentHtml: '<p>已发布富文本正文</p>', version: 7, status: 'PUBLISHED',
  }
  const remoteSnapshot = () => ({ ...remoteArticle, readCount: remoteEvents.size, readerCount: remoteEvents.size ? 1 : 0 })
  const wx = {
    getStorageSync: key => structuredClone(storage.get(key)),
    setStorageSync: (key, value) => storage.set(key, structuredClone(value)),
    removeStorageSync: key => storage.delete(key),
    showToast: () => {}, setNavigationBarTitle: () => {}, stopPullDownRefresh: () => {},
    navigateTo: ({ url }) => { destination = url }, switchTab: ({ url }) => { destination = url },
    login: async () => ({ code: 'login-code' }),
    cloud: { callContainer: async ({ data: { action, data }, header }) => {
      calls.push({ action, data: structuredClone(data), header })
      if (action === 'auth.login') return { statusCode: 200, data: { ok: true, data: { token: 'renewed-token' } } }
      if (action === 'article.list') return { statusCode: 200, data: { ok: true, data: [remoteSnapshot()] } }
      if (action === 'article.search') return { statusCode: 200, data: { ok: true, data: { articles: [remoteSnapshot()], hasMore: false } } }
      if (action === 'article.detail') {
        if (failNextDetail || data.id === 'broken') { failNextDetail = false; throw new Error('详情网络失败') }
        return { statusCode: 200, data: { ok: true, data: data.id === remoteArticle.id ? remoteSnapshot() : null } }
      }
      if (action === 'article.read') {
        assert.equal(data.articleId, remoteArticle.id)
        assert.match(data.eventId, /^read-[a-z0-9]+-\d+-[a-z0-9]+$/)
        assert.ok(header.Authorization.startsWith('Bearer '))
        if (readBehavior === 'failBefore') { readBehavior = 'success'; throw new Error('阅读统计网络失败') }
        const recorded = !remoteEvents.has(data.eventId)
        remoteEvents.add(data.eventId)
        if (readBehavior === 'acceptThenFail') { readBehavior = 'success'; throw new Error('服务器已接收，但响应丢失') }
        if (readBehavior === 'authRetry') { readBehavior = 'success'; return { statusCode: 401, data: { ok: false } } }
        if (readBehavior === 'statsRefreshFail') { readBehavior = 'success'; failNextDetail = true }
        return { statusCode: 200, data: { ok: true, data: { recorded } } }
      }
      throw new Error(`Unexpected action ${action}`)
    } },
  }
  const globals = { wx, getApp: () => ({ globalData: { demoMode: mode } }), Page: value => { spec = value } }
  const load = runtime(globals)
  const { callApi } = load('miniprogram/utils/api.ts')
  const { demoArticles } = load('miniprogram/data/demo.ts')
  const demoBefore = JSON.stringify(demoArticles)
  demoArticles.forEach(article => { Object.freeze(article.content); Object.freeze(article) })
  Object.freeze(demoArticles)
  const id = demoArticles[0].id, otherId = demoArticles[1].id
  const detail = () => callApi('article.detail', { id })
  const listBefore = await callApi('article.list')
  assert.equal(listBefore[0].readCount, 0)
  await detail(); await callApi('article.list')
  assert.equal(storage.has('chiji_demo_article_reads'), false, 'list and detail fetches must not count as reading')
  listBefore[0].title = 'Changed response'; listBefore[0].content[0] = 'Changed response paragraph'
  assert.equal(JSON.stringify(demoArticles), demoBefore, 'demo responses must not mutate immutable examples')
  assert.equal((await callApi('article.read', { articleId: id, eventId: 'demo-event-1' })).recorded, true)
  assert.equal((await callApi('article.read', { articleId: id, eventId: 'demo-event-1' })).recorded, false)
  assert.equal((await callApi('article.read', { articleId: id, eventId: 'demo-event-2' })).recorded, true)
  assert.equal((await detail()).readCount, 2)
  assert.equal((await detail()).readerCount, 1)
  assert.equal((await callApi('article.detail', { id: otherId })).readCount, 0)
  await assert.rejects(callApi('article.read', { articleId: 'unknown', eventId: 'demo-event-3' }), /不存在|未发布/)
  await assert.rejects(callApi('article.read', { articleId: id, eventId: '' }), /参数无效/)
  await assert.rejects(callApi('article.read', { articleId: id, eventId: 'invalid event' }), /参数无效/)
  assert.equal((await detail()).readCount, 2)
  const freshRuntime = runtime(globals)
  assert.equal((await freshRuntime('miniprogram/utils/api.ts').callApi('article.detail', { id })).readCount, 2, 'statistics survive a new runtime')

  load('miniprogram/pages/classroom/index.ts'); const classroomSpec = spec
  const classroom = instantiate(classroomSpec)
  await classroom.onShow(); await classroom.onShow()
  assert.equal(classroom.data.articles.find(article => article.id === id).readCount, 2)
  classroom.open({ currentTarget: { dataset: { id } } })
  assert.equal(destination, `/pages/article/index?id=${id}`)
  assert.equal((await detail()).readCount, 2, 'opening the list route does not count before content display')

  load('miniprogram/pages/article/index.ts'); const articleSpec = spec
  async function visit(idToVisit, renderCallbacks) {
    const page = instantiate(articleSpec, renderCallbacks)
    const loading = page.onLoad({ id: idToVisit })
    await page.onShow()
    await loading
    return page
  }
  const missing = await visit('unknown')
  assert.equal(missing.data.article, null); assert.equal(missing.data.loading, false)
  assert.match(missing.data.error, /不存在|下架/)
  await missing.loadArticle(); assert.equal(missing.data.article, null)
  assert.equal((await detail()).readCount, 2)
  const first = await visit(id)
  assert.equal(first.data.article.readCount, 3); assert.equal(first.data.article.readerCount, 1)
  assert.equal(first.data.article.version, 1)
  await Promise.all([first.onShow(), first.onShow()]); first.onHide(); await first.onShow()
  assert.equal((await detail()).readCount, 3, 'returning to a counted page does not make a new visit')
  const second = await visit(id)
  assert.notEqual(first.readEventId, second.readEventId)
  assert.equal(second.data.article.readCount, 4); assert.equal(second.data.article.readerCount, 1)
  await classroom.onShow()
  assert.equal(classroom.data.articles.find(article => article.id === id).readCount, 4, 'classroom refreshes on return')

  const hidden = instantiate(articleSpec)
  const hiddenLoad = hidden.onLoad({ id }); await hidden.onShow(); hidden.onHide(); await hiddenLoad
  assert.equal((await detail()).readCount, 4, 'a page hidden before loading completes is not counted')
  await hidden.onShow(); assert.equal((await detail()).readCount, 5)
  const unloaded = instantiate(articleSpec)
  const unloadedLoad = unloaded.onLoad({ id }); await unloaded.onShow(); unloaded.onUnload(); await unloadedLoad
  assert.equal((await detail()).readCount, 5, 'an unloaded page is not counted after its request completes')

  mode = false; storage.set('chiji_mini_token', 'test-token')
  const realClassroom = instantiate(classroomSpec)
  await realClassroom.onShow(); await callApi('article.detail', { id: remoteArticle.id })
  assert.equal(calls.filter(call => call.action === 'article.read').length, 0, 'list and preview fetches never submit read events')
  const renderCallbacks = [], deferred = instantiate(articleSpec, renderCallbacks)
  const deferredLoad = deferred.onLoad({ id: remoteArticle.id }); await deferred.onShow()
  for (let tries = 0; !renderCallbacks.length && tries < 20; tries++) await new Promise(resolve => setTimeout(resolve, 1))
  assert.equal(renderCallbacks.length, 1)
  await deferred.onShow()
  assert.equal(remoteEvents.size, 0, 'read must wait for setData render completion')
  renderCallbacks.shift()(); await deferredLoad
  assert.equal(remoteEvents.size, 1)
  assert.equal(deferred.data.contentHtml, remoteArticle.contentHtml)
  assert.equal(deferred.data.paragraphs.length, 0, 'read statistics preserve rich content rendering')
  assert.equal(deferred.data.article.readCount, 1); assert.equal(deferred.data.article.version, 7)

  readBehavior = 'failBefore'
  const failed = await visit(remoteArticle.id)
  assert.equal(failed.data.article.id, remoteArticle.id); assert.equal(failed.data.error, ''); assert.equal(failed.data.loading, false)
  assert.equal(remoteEvents.size, 1); assert.equal(failed.readRecorded, false)
  failed.onHide(); await failed.onShow()
  const failedAttempts = calls.filter(call => call.action === 'article.read' && call.data.eventId === failed.readEventId)
  assert.equal(failedAttempts.length, 2)
  assert.equal(remoteEvents.size, 2); assert.equal(failed.data.article.readCount, 2)
  await failed.onShow(); assert.equal(remoteEvents.size, 2)

  readBehavior = 'acceptThenFail'
  const lostResponse = await visit(remoteArticle.id)
  assert.equal(lostResponse.data.article.id, remoteArticle.id); assert.equal(lostResponse.data.error, '')
  assert.equal(remoteEvents.size, 3); assert.equal(lostResponse.readRecorded, false)
  lostResponse.onHide(); await Promise.all([lostResponse.onShow(), lostResponse.onShow()])
  assert.equal(remoteEvents.size, 3, 'retry uses the previously accepted event ID')
  assert.equal(lostResponse.data.article.readCount, 3); assert.equal(lostResponse.data.article.readerCount, 1)
  assert.equal(calls.filter(call => call.action === 'article.read' && call.data.eventId === lostResponse.readEventId).length, 2)

  readBehavior = 'authRetry'
  const refreshedLogin = await visit(remoteArticle.id)
  assert.equal(remoteEvents.size, 4)
  const authAttempts = calls.filter(call => call.action === 'article.read' && call.data.eventId === refreshedLogin.readEventId)
  assert.equal(authAttempts.length, 2)
  assert.equal(authAttempts[0].header.Authorization, 'Bearer test-token')
  assert.equal(authAttempts[1].header.Authorization, 'Bearer renewed-token')
  assert.equal(refreshedLogin.data.article.readCount, 4)

  readBehavior = 'statsRefreshFail'
  const unavailableStats = await visit(remoteArticle.id)
  assert.equal(remoteEvents.size, 5); assert.equal(unavailableStats.readRecorded, true)
  assert.equal(unavailableStats.data.article.id, remoteArticle.id); assert.equal(unavailableStats.data.error, '')
  await unavailableStats.onShow(); assert.equal(remoteEvents.size, 5, 'counter refresh failure does not resubmit an accepted read')
  const broken = await visit('broken')
  assert.equal(broken.data.article, null); assert.match(broken.data.error, /网络失败/)
  const unknownRemote = await visit('unknown')
  assert.equal(unknownRemote.data.article, null); assert.match(unknownRemote.data.error, /不存在|下架/)
  assert.equal(remoteEvents.size, 5)
  const newVisit = await visit(remoteArticle.id)
  assert.equal(remoteEvents.size, 6); assert.equal(newVisit.data.article.readCount, 6)
  await realClassroom.onShow()
  assert.equal(realClassroom.data.articles[0].readCount, 6)
  assert.equal(JSON.stringify(demoArticles), demoBefore)
  console.log('Content display, persistent demo views/readers, list isolation, render timing, unique visits, deduplicated retry, lost responses, login retry and nonblocking statistics failures passed.')
}

run().catch(error => { console.error(error); process.exitCode = 1 })
