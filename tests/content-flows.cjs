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
    const source = ts.transpileModule(fs.readFileSync(filename, 'utf8'), {
      compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS },
    }).outputText
    vm.runInNewContext(source, {
      module, exports: module.exports, console, Error, setTimeout, clearTimeout, structuredClone, ...globals,
      require: name => load(path.resolve(path.dirname(filename), `${name}.ts`)),
    }, { filename })
    return module.exports
  }
}

async function run() {
  let page, response, apiError = '', cloudError = '', cloudStatus = 0, demoMode = false, destination, navigationTitle
  const calls = [], tempFiles = []
  const seed = {
    id: 'article-1', title: '秋季修剪', summary: '修剪与留枝', cover: 'https://cdn.example.com/cover.jpg',
    variety: '通用', stage: '采后管理', type: 'article', publishedAt: '2026-10-04',
    content: ['第一段旧正文', '第二段旧正文'],
  }
  const wx = {
    getStorageSync: () => 'test-token', setStorageSync: () => {}, removeStorageSync: () => {},
    setNavigationBarTitle: ({ title }) => { navigationTitle = title },
    switchTab: ({ url }) => { destination = url },
    cloud: {
      callContainer: async ({ data }) => {
        calls.push(data)
        assert.equal(data.action, 'article.detail')
        if (apiError) throw new Error(apiError)
        return { statusCode: 200, data: { ok: true, data: response } }
      },
      getTempFileURL: async ({ fileList }) => {
        tempFiles.push(fileList[0])
        if (cloudError) throw new Error(cloudError)
        return { fileList: [{ fileID: fileList[0], status: cloudStatus, tempFileURL: 'https://cdn.example.com/temporary.mp4' }] }
      },
    },
  }
  const globals = { wx, getApp: () => ({ globalData: { demoMode, cloudEnv: 'test', cloudService: 'test' } }),
    Page: spec => {
      page = spec
      page.setData = (updates, callback) => { Object.assign(page.data, updates); callback?.() }
    },
  }
  const load = runtime(globals)
  load('miniprogram/pages/article/index.ts')
  assert.equal(page.data.loading, true)

  // Invalid links and removed articles render an actionable error, without dereferencing null.
  await page.onLoad({})
  assert.match(page.data.error, /链接无效/)
  assert.equal(page.data.loading, false)
  assert.equal(calls.length, 0)
  assert.equal(page.onShareAppMessage().path, '/pages/classroom/index')
  page.goClassroom(); assert.equal(destination, '/pages/classroom/index')
  response = null
  await page.onLoad({ id: seed.id })
  assert.match(page.data.error, /不存在或已下架/)
  assert.equal(page.data.article, null)

  // Legacy content survives, and a successful retry removes the prior error.
  response = seed
  await page.loadArticle()
  assert.equal(page.data.error, '')
  assert.equal(page.data.contentHtml, '')
  assert.equal(page.data.paragraphs.join('|'), seed.content.join('|'))
  assert.equal(navigationTitle, seed.title)
  apiError = '网络不可用'
  await page.loadArticle()
  assert.equal(page.data.error, '网络不可用')
  assert.equal(page.data.article, null)
  apiError = ''
  await page.loadArticle()
  assert.equal(page.data.error, '')
  assert.equal(page.data.loading, false)
  page.coverError(); assert.equal(page.data.coverFailed, true)

  // Rich text replaces the old paragraphs so readers do not see duplicate content.
  const html = '<h2>结果枝</h2><p><strong>留枝建议</strong></p><ul><li>先去病弱枝</li></ul>'
  response = { ...seed, id: 'rich /果园', contentHtml: ` ${html} ` }
  await page.onLoad({ id: response.id })
  assert.equal(page.data.contentHtml, html)
  assert.equal(page.data.paragraphs.length, 0)
  assert.equal(page.data.coverFailed, false)
  assert.equal(page.onShareAppMessage().path, `/pages/article/index?id=${encodeURIComponent(response.id)}`)
  assert.equal(page.onShareAppMessage().title, seed.title)
  response = { ...seed, contentHtml: '  \n  ', cover: '' }
  await page.loadArticle()
  assert.equal(page.data.paragraphs.length, 2)
  assert.equal(page.data.coverFailed, true)

  // Article entries with an old video field are still articles; missing videos are explicit.
  response = { ...seed, videoUrl: 'https://cdn.example.com/old.mp4' }
  await page.loadArticle()
  assert.equal(page.data.videoUrl, '')
  response = { ...seed, type: 'video' }
  await page.loadArticle()
  assert.match(page.data.videoError, /暂未提供视频/)
  assert.equal(page.data.videoUrl, '')
  await page.retryVideo()
  assert.equal(page.data.videoLoading, false)
  response = { ...seed, type: 'video', videoUrl: 'javascript:alert(1)' }
  await page.loadArticle()
  assert.match(page.data.videoError, /视频地址无效/)

  // Playback failure keeps the text available and retries only after a user action.
  response = { ...seed, type: 'video', videoUrl: 'https://cdn.example.com/video.mp4' }
  await page.loadArticle()
  assert.equal(page.data.videoUrl, response.videoUrl)
  const firstAttempt = page.data.videoAttempt
  page.videoError({ currentTarget: { dataset: { attempt: firstAttempt } } })
  assert.match(page.data.videoError, /无法播放/)
  assert.equal(page.data.article.title, seed.title)
  assert.equal(page.data.paragraphs.length, 2)
  await new Promise(resolve => setTimeout(resolve, 5))
  assert.equal(page.data.videoAttempt, firstAttempt)
  await page.retryVideo()
  assert.equal(page.data.videoError, '')
  assert.ok(page.data.videoAttempt > firstAttempt)
  page.videoError({ currentTarget: { dataset: { attempt: firstAttempt } } })
  assert.equal(page.data.videoError, '') // A stale error cannot hide a successful retry.

  // Existing cloud file IDs resolve to temporary HTTPS URLs and can recover after failure.
  response = { ...seed, type: 'video', videoUrl: 'cloud://test/videos/prune.mp4' }
  cloudStatus = -1
  await page.loadArticle()
  assert.match(page.data.videoError, /地址暂时无法获取/)
  assert.equal(page.data.videoUrl, '')
  const beforeRetry = tempFiles.length
  cloudStatus = 0
  await page.retryVideo()
  assert.equal(tempFiles.length, beforeRetry + 1)
  assert.equal(page.data.videoUrl, 'https://cdn.example.com/temporary.mp4')
  cloudError = 'internal cloud service failure'
  await page.loadArticle()
  assert.match(page.data.videoError, /地址暂时无法获取/)
  assert.ok(!page.data.videoError.includes('internal'))
  cloudError = ''
  await page.retryVideo()
  assert.equal(page.data.videoError, '')

  // A slow cloud URL response must not attach a video to the next article.
  const getTempFileURL = wx.cloud.getTempFileURL
  let resolveCloud
  wx.cloud.getTempFileURL = () => new Promise(resolve => { resolveCloud = resolve })
  const slowVideo = page.loadArticle()
  await new Promise(resolve => setTimeout(resolve, 0))
  assert.equal(page.data.videoLoading, true)
  response = { ...seed, id: 'text-only' }
  await page.onLoad({ id: response.id })
  resolveCloud({ fileList: [{ status: 0, tempFileURL: 'https://cdn.example.com/stale.mp4' }] })
  await slowVideo
  assert.equal(page.data.article.id, 'text-only')
  assert.equal(page.data.videoUrl, '')
  assert.equal(page.data.videoLoading, false)
  wx.cloud.getTempFileURL = getTempFileURL

  // Slow requests must not replace a more recent article or continue after the page closes.
  const callContainer = wx.cloud.callContainer
  const pending = []
  wx.cloud.callContainer = () => new Promise(resolve => pending.push(resolve))
  const older = page.onLoad({ id: 'older' }), newer = page.onLoad({ id: 'newer' })
  pending[1]({ statusCode: 200, data: { ok: true, data: { ...seed, id: 'newer' } } })
  await newer
  pending[0]({ statusCode: 200, data: { ok: true, data: { ...seed, id: 'older' } } })
  await older
  assert.equal(page.data.article.id, 'newer')
  assert.equal(page.data.loading, false)
  const closing = page.loadArticle()
  page.onUnload()
  pending[2]({ statusCode: 200, data: { ok: true, data: seed } })
  await closing
  assert.equal(page.data.article, null)
  wx.cloud.callContainer = callContainer

  // The demonstration video intentionally has no playable media and says so.
  demoMode = true
  // A new navigation creates a fresh Page; an unloaded instance stays disposed.
  runtime(globals)('miniprogram/pages/article/index.ts')
  await page.onLoad({ id: 'a-sugar' })
  assert.match(page.data.videoError, /暂未提供视频/)
  assert.equal(page.data.videoUrl, '')
  console.log('Content flows passed: rich text, legacy content, loading recovery, video recovery, stale requests and sharing')
}

run().catch(error => { console.error(error); process.exitCode = 1 })
