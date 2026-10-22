const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const assert = require('node:assert/strict')
const crypto = require('node:crypto')
const ts = require('typescript')
const root = path.resolve(__dirname, '..')

function runtime(clock, storage) {
  const cache = new Map()
  class ClockDate extends Date {
    constructor(...args) { super(...(args.length ? args : [clock.now])) }
    static now() { return clock.now }
  }
  const localStorage = {
    getItem: key => storage.get(key) ?? null,
    setItem: (key, value) => storage.set(key, String(value)),
    removeItem: key => storage.delete(key),
  }
  return function load(relative) {
    const filename = path.resolve(root, relative)
    if (cache.has(filename)) return cache.get(filename).exports
    const module = { exports: {} }
    cache.set(filename, module)
    const source = ts.transpileModule(fs.readFileSync(filename, 'utf8'), {
      compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS },
    }).outputText
    vm.runInNewContext(source, {
      module, exports: module.exports, console, structuredClone, crypto, localStorage, sessionStorage: localStorage, TextEncoder,
      Date: ClockDate, Error, URL, setTimeout: callback => setTimeout(callback, 0), clearTimeout,
      require: name => load(path.resolve(path.dirname(filename), `${name}.ts`)),
    }, { filename })
    return module.exports
  }
}

async function run() {
  const clock = { now: Date.parse('2026-10-04T15:59:00Z') }
  const storage = new Map([['chiji_admin_user', JSON.stringify({ username: '内容验收' })]])
  const load = runtime(clock, storage)
  const { mockCall } = load('admin/src/mock.ts')
  const login = await mockCall('admin.login', { username: '内容验收', password: 'content-test-12345' })
  storage.set('chiji_admin_token', login.token)
  storage.set('chiji_admin_user', JSON.stringify(login.user))
  const blank = () => ({ id: '', title: '测试课堂', summary: '测试摘要', cover: 'https://example.com/cover.jpg', variety: '通用', stage: '采后管理', type: 'article', content: ['第一段正文'], status: 'DRAFT', publishedAt: '2026-10-04' })
  const saved = await mockCall('admin.article.save', { article: blank() })
  assert.equal(saved.version, 1)
  assert.equal(saved.status, 'DRAFT')
  assert.equal(saved.readCount, 0)
  const edited = await mockCall('admin.article.save', { article: { ...saved, title: '第二版课堂', readCount: 99999, readerCount: 999 } })
  assert.equal(edited.version, 2)
  assert.equal(edited.readCount, 0, 'client cannot overwrite reading counters')
  const versions = await mockCall('admin.article.versions', { articleId: saved.id })
  assert.equal(versions.length, 2)
  assert.equal(versions[0].version, 2)
  assert.equal(versions[1].article.title, '测试课堂')
  versions[1].article.title = '试图改写历史'
  assert.equal((await mockCall('admin.article.versions', { articleId: saved.id }))[1].article.title, '测试课堂')
  await assert.rejects(() => mockCall('admin.article.save', { article: { ...saved, title: '过期编辑' } }), /已更新|版本|变化/)
  assert.equal((await mockCall('admin.article.versions', { articleId: saved.id })).length, 2)
  await assert.rejects(() => mockCall('admin.article.restore', { articleId: saved.id, version: 1, expectedVersion: 1 }), /已更新|版本|变化/)
  await assert.rejects(() => mockCall('admin.article.restore', { articleId: saved.id, version: 99, expectedVersion: 2 }), /不存在/)
  const restored = await mockCall('admin.article.restore', { articleId: saved.id, version: 1, expectedVersion: 2 })
  assert.equal(restored.version, 3)
  assert.equal(restored.status, 'DRAFT')
  assert.equal(restored.title, '测试课堂')
  assert.equal(restored.scheduledAt, undefined)
  assert.equal((await mockCall('admin.article.versions', { articleId: saved.id })).length, 3)

  for (const scheduledAt of ['', 'invalid', '2026-10-04T23:58:00+08:00']) {
    await assert.rejects(() => mockCall('admin.article.save', { article: { ...blank(), status: 'SCHEDULED', scheduledAt } }), /时间|定时/)
  }
  const scheduled = await mockCall('admin.article.save', { article: { ...blank(), status: 'SCHEDULED', scheduledAt: '2026-10-05T00:00:00+08:00' } })
  assert.equal(Date.parse(scheduled.scheduledAt), Date.parse('2026-10-04T16:00:00Z'))
  clock.now = Date.parse('2026-10-04T15:59:59.999Z')
  assert.equal((await mockCall('admin.article.list')).find(article => article.id === scheduled.id).status, 'SCHEDULED')
  clock.now++
  const published = (await mockCall('admin.article.list')).find(article => article.id === scheduled.id)
  assert.equal(published.status, 'PUBLISHED')
  assert.equal(published.version, 2)
  assert.equal(Date.parse(published.publishedAt), clock.now)
  assert.equal(published.scheduledAt, undefined)
  assert.equal((await mockCall('admin.article.versions', { articleId: scheduled.id })).length, 2)
  await mockCall('admin.article.list')
  assert.equal((await mockCall('admin.article.versions', { articleId: scheduled.id })).length, 2, 'due publishing must run once')
  const revertedSchedule = await mockCall('admin.article.restore', { articleId: scheduled.id, version: 1, expectedVersion: 2 })
  assert.equal(revertedSchedule.status, 'DRAFT')
  assert.equal(revertedSchedule.scheduledAt, undefined, 'restore never revives an expired schedule')

  const cancellable = await mockCall('admin.article.save', { article: { ...blank(), status: 'SCHEDULED', scheduledAt: '2026-10-05T00:01:00+08:00' } })
  await mockCall('admin.article.save', { article: { ...cancellable, status: 'DRAFT' } })
  clock.now += 60000
  assert.equal((await mockCall('admin.article.list')).find(article => article.id === cancellable.id).status, 'DRAFT')

  const stats = await mockCall('admin.article.stats', { articleId: saved.id })
  assert.equal(stats.totalViews, 0)
  assert.equal(stats.totalReaders, 0)
  assert.equal(stats.days.length, 14)
  assert.equal(stats.days[13].date, '2026-10-05', 'daily buckets use Beijing midnight')
  assert.ok(stats.days.every(day => day.views === 0 && day.readers === 0))
  const sample = await mockCall('admin.article.stats', { articleId: 'a-1' })
  assert.ok(sample.totalViews >= sample.totalReaders)
  assert.equal(sample.todayViews, sample.days[13].views)
  assert.equal(sample.todayReaders, sample.days[13].readers)
  assert.ok(sample.days.every(day => day.readers <= day.views))
  const list = await mockCall('admin.article.list')
  assert.equal(list.find(article => article.id === 'a-1').readCount, sample.totalViews)
  const restoredSample = await mockCall('admin.article.restore', { articleId: 'a-1', version: 1, expectedVersion: 1 })
  assert.equal(restoredSample.readCount, sample.totalViews)
  assert.equal(restoredSample.readerCount, sample.totalReaders)

  const { mockCall: reloadedCall } = runtime(clock, storage)('admin/src/mock.ts')
  assert.equal((await reloadedCall('admin.article.list')).find(article => article.id === saved.id).version, 3)
  assert.equal((await reloadedCall('admin.article.versions', { articleId: saved.id })).length, 3)
  await assert.rejects(() => mockCall('admin.article.versions', { articleId: 'missing' }), /不存在/)
  await assert.rejects(() => mockCall('admin.article.stats', { articleId: 'missing' }), /不存在/)
  const tag = await mockCall('admin.content.resource.save', { resource: { id: '', name: '病虫害', kind: 'tag', version: 0 } })
  const image = await mockCall('admin.content.resource.save', { resource: { id: '', name: '果园封面', kind: 'image', url: 'https://example.com/orchard.jpg', version: 0 } })
  await assert.rejects(() => mockCall('admin.content.resource.save', { resource: { ...image, url: 'javascript:alert(1)' } }), /HTTPS/)
  const renamed = await mockCall('admin.content.resource.save', { resource: { ...tag, name: '葡萄病虫害' } })
  await assert.rejects(() => mockCall('admin.content.resource.save', { resource: tag }), /变化/)
  let reviewed = await mockCall('admin.article.save', { article: { ...blank(), tags: [renamed.name], cover: image.url } })
  reviewed = await mockCall('admin.article.submit', { articleId: reviewed.id, expectedVersion: reviewed.version })
  assert.equal(reviewed.reviewStatus, 'PENDING')
  await assert.rejects(() => mockCall('admin.article.save', { article: { ...reviewed, reviewStatus: 'APPROVED', status: 'PUBLISHED' } }), /审核/)
  await assert.rejects(() => mockCall('admin.article.review', { articleId: reviewed.id, expectedVersion: reviewed.version - 1, decision: 'APPROVED' }), /版本|更新/)
  await assert.rejects(() => mockCall('admin.article.review', { articleId: reviewed.id, expectedVersion: reviewed.version, decision: 'REJECTED', note: '' }), /原因/)
  reviewed = await mockCall('admin.article.review', { articleId: reviewed.id, expectedVersion: reviewed.version, decision: 'REJECTED', note: '补充用药说明' })
  assert.equal(reviewed.reviewNote, '补充用药说明')
  reviewed = await mockCall('admin.article.save', { article: { ...reviewed, title: '已补充说明' } })
  assert.equal(reviewed.reviewStatus, 'UNSUBMITTED')
  reviewed = await mockCall('admin.article.submit', { articleId: reviewed.id, expectedVersion: reviewed.version })
  reviewed = await mockCall('admin.article.review', { articleId: reviewed.id, expectedVersion: reviewed.version, decision: 'APPROVED', note: '通过' })
  assert.equal(reviewed.status, 'DRAFT')
  reviewed = await mockCall('admin.article.save', { article: { ...reviewed, status: 'PUBLISHED' } })
  assert.equal(reviewed.reviewStatus, 'APPROVED')
  await assert.rejects(() => mockCall('admin.article.save', { article: { ...reviewed, title: '擅自改动' } }), /审核/)
  reviewed = await mockCall('admin.article.restore', { articleId: reviewed.id, version: 1, expectedVersion: reviewed.version })
  assert.equal(reviewed.reviewStatus, 'UNSUBMITTED')
  await mockCall('admin.content.resource.delete', { resource: renamed })
  assert.equal((await mockCall('admin.article.list')).find(a => a.id === reviewed.id).tags[0], '葡萄病虫害')
  assert.equal((await mockCall('admin.content.library')).length, 1)
  assert.equal((await reloadedCall('admin.content.library'))[0].url, image.url)
  console.log('Content management: libraries, validation, persistence, review rejection/resubmission, conflicts, publication gating and restored approval reset passed')
  console.log('Content admin: scheduling boundaries, immutable history, conflicts, restore, statistics and persistence passed')
}

run().catch(error => { console.error(error); process.exitCode = 1 })
