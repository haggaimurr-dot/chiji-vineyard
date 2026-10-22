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
    vm.runInNewContext(source, { module, exports: module.exports, console, Error, setTimeout, clearTimeout, ...globals,
      require: name => load(path.resolve(path.dirname(filename), `${name}.ts`)),
    }, { filename })
    return module.exports
  }
}

async function run() {
  const storage = new Map(), toasts = [], calls = []
  let mode = true, spec, remote
  const wx = {
    getStorageSync: key => storage.get(key), setStorageSync: (key, value) => storage.set(key, value), removeStorageSync: key => storage.delete(key),
    showToast: ({ title }) => toasts.push(title), setNavigationBarTitle() {}, stopPullDownRefresh() {},
    cloud: { callContainer: async ({ data: { action, data } }) => { calls.push({ action, data }); return { statusCode: 200, data: { ok: true, data: await remote(action, data) } } } },
  }
  const globals = { wx, getApp: () => ({ globalData: { demoMode: mode } }), Page: value => { spec = value } }
  const load = runtime(globals)
  const { callApi } = load('miniprogram/utils/api.ts')
  const { searchDemoArticles, learningTime } = load('miniprogram/utils/articles.ts')
  const { demoArticles } = load('miniprogram/data/demo.ts')
  const id = demoArticles[0].id
  assert.equal((await callApi('article.search', { view: 'history' })).articles.length, 0)
  await callApi('article.detail', { id })
  assert.equal((await callApi('article.search', { view: 'history' })).articles.length, 0, 'fetching detail alone is not learning')
  await callApi('article.favorite.set', { articleId: id, enabled: true })
  const firstTime = storage.get('chiji_demo_article_favorites')[id]
  await callApi('article.favorite.set', { articleId: id, enabled: true })
  assert.equal(storage.get('chiji_demo_article_favorites')[id], firstTime, 'duplicate favorite is idempotent')
  assert.equal((await callApi('article.search', { view: 'favorite' })).articles[0].id, id)
  assert.equal((await callApi('article.detail', { id })).favorite, true)
  const fresh = runtime(globals)('miniprogram/utils/api.ts')
  assert.equal((await fresh.callApi('article.detail', { id })).favorite, true, 'favorite survives restart')
  await callApi('article.read', { articleId: id, eventId: 'learning-1' })
  const readTime = storage.get('chiji_demo_article_reads')[id].lastReadAt
  await callApi('article.read', { articleId: id, eventId: 'learning-1' })
  assert.equal(storage.get('chiji_demo_article_reads')[id].lastReadAt, readTime)
  await callApi('article.read', { articleId: id, eventId: 'learning-2' })
  assert.equal((await callApi('article.search', { view: 'history' })).articles.length, 1, 'repeat readings merge into one article')
  assert.equal((await fresh.callApi('article.search', { view: 'history' })).articles[0].id, id)
  const fixtures = Array.from({ length: 45 }, (_, i) => ({ ...demoArticles[0], id: `fixture-${String(i).padStart(2, '0')}`, title: i % 2 ? '修剪教程' : '施肥教程', status: i === 44 ? 'DRAFT' : 'PUBLISHED' }))
  const filtered = searchDemoArticles(fixtures, { keyword: ' 修剪 ', stage: demoArticles[0].stage, variety: demoArticles[0].variety, limit: 20 })
  assert.equal(filtered.articles.length, 20); assert.equal(filtered.hasMore, true)
  const tail = searchDemoArticles(fixtures, { keyword: '修剪', offset: 20, limit: 20 })
  assert.equal(tail.articles.length, 2); assert.equal(tail.hasMore, false)
  assert.equal(searchDemoArticles(fixtures, { keyword: '%' }).articles.length, 0, 'wildcards are literal')
  assert.equal(searchDemoArticles(fixtures, { keyword: '不匹配' }).articles.length, 0)
  assert.equal(searchDemoArticles(fixtures, { stage: '不存在' }).articles.length, 0)
  assert.throws(() => searchDemoArticles(fixtures, { offset: -1 }), /参数无效/)
  assert.equal(learningTime('2026-10-04T18:03:00Z'), '2026-10-05 02:03')

  const instantiate = value => {
    const page = { ...value, data: structuredClone(value.data) }
    page.setData = (updates, cb) => { Object.assign(page.data, updates); cb?.() }
    return page
  }
  load('miniprogram/pages/article/index.ts')
  const detail = instantiate(spec)
  const loading = detail.onLoad({ id }); await detail.onShow(); await loading
  assert.equal(detail.data.article.favorite, true)
  await detail.toggleFavorite()
  assert.equal(detail.data.article.favorite, false)
  assert.equal((await callApi('article.search', { view: 'favorite' })).articles.length, 0)
  load('miniprogram/pages/classroom/index.ts')
  const page = instantiate(spec)
  await page.onShow()
  await page.toggleFavorite({ currentTarget: { dataset: { id } } })
  assert.equal(page.data.articles.find(a => a.id === id).favorite, true)
  await page.pickView({ currentTarget: { dataset: { value: 'favorite' } } })
  await page.toggleFavorite({ currentTarget: { dataset: { id } } })
  assert.equal(page.data.articles.length, 0)
  storage.set('chiji_classroom_view', 'history')
  await page.onShow()
  assert.equal(page.data.view, 'history'); assert.ok(page.data.articles[0].lastReadText)

  // Controlled responses verify race handling, pagination retries and mutation failures.
  mode = false; storage.set('chiji_mini_token', 'token')
  const waiting = []
  remote = () => new Promise((resolve, reject) => waiting.push({ resolve, reject }))
  const first = page.load()
  page.setData({ keyword: '新搜索' }); const second = page.load()
  waiting[1].resolve({ articles: [{ ...demoArticles[0], id: 'new' }], hasMore: true }); await second
  waiting[0].resolve({ articles: [{ ...demoArticles[0], id: 'old' }], hasMore: false }); await first
  assert.equal(page.data.articles[0].id, 'new'); assert.equal(page.data.hasMore, true)
  const more = page.loadMore(); await page.loadMore()
  assert.equal(waiting.length, 3, 'duplicate load more is ignored')
  waiting[2].reject(new Error('追加失败')); await more
  assert.equal(page.data.articles[0].id, 'new'); assert.equal(page.data.moreError, '追加失败')
  const retry = page.loadMore()
  assert.equal(calls.at(-1).data.offset, 1)
  waiting[3].resolve({ articles: [{ ...demoArticles[0], id: 'next' }], hasMore: false }); await retry
  assert.equal(page.data.articles.length, 2); assert.equal(page.data.moreError, '')
  remote = async () => { throw new Error('收藏失败') }
  await page.toggleFavorite({ currentTarget: { dataset: { id: 'new' } } })
  assert.equal(page.data.articles[0].favorite, undefined); assert.equal(toasts.at(-1), '收藏失败'); assert.equal(page.data.favoriteBusy, false)
  const before = calls.length
  remote = async () => ({ articles: [], hasMore: false })
  page.onSearch({ detail: { value: '修' } }); page.onSearch({ detail: { value: '修剪' } })
  await new Promise(resolve => setTimeout(resolve, 350))
  assert.equal(calls.length, before + 1); assert.equal(calls.at(-1).data.keyword, '修剪')
  page.onSearch({ detail: { value: '立即搜索' } }); await page.confirmSearch()
  const confirmedCalls = calls.length
  await new Promise(resolve => setTimeout(resolve, 350)); assert.equal(calls.length, confirmedCalls)
  page.onSearch({ detail: { value: '离开页面' } }); page.onHide()
  await new Promise(resolve => setTimeout(resolve, 350)); assert.equal(calls.length, confirmedCalls)
  console.log('Classroom learning passed: search, pagination, favorites, history, persistence, retries and stale responses')
}
run().catch(error => { console.error(error); process.exitCode = 1 })
