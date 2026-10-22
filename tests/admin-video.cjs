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
      module, exports: module.exports, console, Error, DOMException, ...globals,
      require: name => load(path.resolve(path.dirname(filename), `${name}.ts`)),
    }, { filename })
    return module.exports
  }
}

function browser() {
  const videos = [], timers = new Map(), objectUrls = []
  let timerId = 0
  class BrowserURL extends URL {}
  BrowserURL.createObjectURL = file => {
    const url = `blob:https://admin.example.com/demo-${objectUrls.length + 1}`
    objectUrls.push({ file, url })
    return url
  }
  class Video {
    duration = 0
    videoWidth = 0
    videoHeight = 0
    error = null
    src = ''
    onloadeddata = null
    onerror = null
    loads = 0
    removedSource = false
    load() { this.loads++ }
    removeAttribute(name) { assert.equal(name, 'src'); this.src = ''; this.removedSource = true }
  }
  return {
    videos, timers, objectUrls,
    globals: {
      URL: BrowserURL,
      window: { setTimeout: (callback, delay) => { const id = ++timerId; timers.set(id, { callback, delay }); return id } },
      clearTimeout: id => timers.delete(id),
      document: { createElement: tag => { assert.equal(tag, 'video'); const video = new Video(); videos.push(video); return video } },
    },
    latest() { return videos.at(-1) },
    fireTimeout() {
      assert.equal(timers.size, 1)
      const [id, timer] = [...timers][0]
      timers.delete(id)
      timer.callback()
    },
  }
}

function trackedController() {
  const controller = new AbortController(), listeners = new Set()
  return {
    listeners,
    abort: () => controller.abort(),
    signal: {
      get aborted() { return controller.signal.aborted },
      addEventListener(type, callback, options) { listeners.add(callback); controller.signal.addEventListener(type, callback, options) },
      removeEventListener(type, callback) { listeners.delete(callback); controller.signal.removeEventListener(type, callback) },
    },
  }
}

function assertCleaned(video, environment, controller) {
  assert.equal(video.src, '')
  assert.equal(video.removedSource, true)
  assert.equal(video.onloadeddata, null)
  assert.equal(video.onerror, null)
  assert.equal(environment.timers.size, 0)
  assert.equal(controller.listeners.size, 0)
}

const mp4Header = Uint8Array.from([0, 0, 0, 24, 102, 116, 121, 112, 105, 115, 111, 109])
function file({ name = 'prune.mp4', type = 'video/mp4', size = mp4Header.length, header = mp4Header } = {}) {
  // Keep size-limit cases lightweight while using a real Blob for header reads.
  const blob = new Blob([header])
  return { name, type, size, slice: (start, end) => blob.slice(start, end) }
}

async function run() {
  const environment = browser()
  const { MAX_VIDEO_BYTES, registerDemoVideo, videoAddressError, checkVideoFile, checkVideoPlayback, videoDuration } = runtime(environment.globals)('admin/src/video.ts')

  for (const address of [
    'https://cdn.example.com/video.mp4', 'HTTPS://cdn.example.com/video.mp4?token=test#t=1',
    'https://cdn.example.com/a%20b.mp4', 'http://localhost:8080/media/video.mp4',
    'http://127.0.0.1:8080/media/video.mp4', 'http://[::1]:8080/media/video.mp4',
  ]) assert.equal(videoAddressError(address), '', address)
  for (const address of [
    '', '  ', 'video.mp4', '//cdn.example.com/video.mp4', 'http://cdn.example.com/video.mp4',
    'javascript:alert(1)', 'data:video/mp4;base64,AAAA', 'ftp://cdn.example.com/video.mp4',
    'https://user:secret@cdn.example.com/video.mp4', 'https:cdn.example.com/video.mp4',
    'https://cdn.example.com/vi\ndeo.mp4', 'https://cdn.example.com/vi\tdeo.mp4',
    'https://cdn.example.com/a b.mp4', 'https://cdn.example.com\\video.mp4',
    'https://cdn.example.com/vi\u0000deo.mp4',
  ]) assert.ok(videoAddressError(address), `Address should be rejected: ${JSON.stringify(address)}`)
  assert.match(videoAddressError('cloud://env/video.mp4'), /fileID/)
  const addressPrefix = 'https://cdn.example.com/'
  assert.equal(videoAddressError(addressPrefix + 'a'.repeat(2048 - addressPrefix.length)), '')
  assert.ok(videoAddressError(addressPrefix + 'a'.repeat(2049 - addressPrefix.length)))
  const uploadedFile = file(), demoUrl = registerDemoVideo(uploadedFile)
  assert.equal(environment.objectUrls[0].file, uploadedFile)
  assert.ok(videoAddressError(demoUrl))
  assert.equal(videoAddressError(demoUrl, true), '')
  assert.ok(videoAddressError('blob:https://admin.example.com/unregistered', true))

  await checkVideoFile(file())
  await checkVideoFile(file({ name: 'PRUNE.MP4', type: '' }))
  await checkVideoFile(file({ size: MAX_VIDEO_BYTES }))
  await assert.rejects(checkVideoFile(file({ size: 0 })), /为空/)
  await assert.rejects(checkVideoFile(file({ size: MAX_VIDEO_BYTES + 1 })), /20MB/)
  await assert.rejects(checkVideoFile(file({ name: 'prune.mov' })), /MP4/)
  await assert.rejects(checkVideoFile(file({ type: 'video/webm' })), /MP4/)
  await assert.rejects(checkVideoFile(file({ header: new Uint8Array(12) })), /不是有效的 MP4/)
  await assert.rejects(checkVideoFile(file({ header: mp4Header.slice(0, 11), size: 11 })), /不是有效的 MP4/)

  // First decoded frame yields metadata and releases the media request and listeners.
  const controller = trackedController()
  const playback = checkVideoPlayback('https://cdn.example.com/video.mp4', controller.signal)
  const video = environment.latest(), staleError = video.onerror
  assert.equal(video.src, 'https://cdn.example.com/video.mp4')
  assert.equal(video.muted, true)
  assert.equal(video.playsInline, true)
  Object.assign(video, { duration: 65.8, videoWidth: 1920, videoHeight: 1080 })
  video.onloadeddata()
  assert.deepEqual({ ...await playback }, { duration: 65.8, width: 1920, height: 1080 })
  assertCleaned(video, environment, controller)
  const loadsAfterSuccess = video.loads
  video.error = { code: 2 }; staleError()
  controller.abort()
  assert.equal(video.loads, loadsAfterSuccess) // Settled or detached events cannot restart the request.

  // Audio-only, invalid-duration and zero-sized sources are not accepted as playable video.
  for (const metadata of [
    { duration: 0, videoWidth: 1920, videoHeight: 1080 },
    { duration: Infinity, videoWidth: 1920, videoHeight: 1080 },
    { duration: NaN, videoWidth: 1920, videoHeight: 1080 },
    { duration: 60, videoWidth: 0, videoHeight: 1080 },
    { duration: 60, videoWidth: 1920, videoHeight: 0 },
  ]) {
    const cancellation = trackedController()
    const result = checkVideoPlayback('https://cdn.example.com/invalid.mp4', cancellation.signal)
    const failure = assert.rejects(result, /画面或时长/)
    const invalidVideo = environment.latest()
    Object.assign(invalidVideo, metadata)
    invalidVideo.onloadeddata()
    await failure
    assertCleaned(invalidVideo, environment, cancellation)
  }

  for (const [code, message] of [[2, /网络/], [3, /解码/], [4, /格式/], [1, /无法播放/], [0, /无法播放/]]) {
    const cancellation = trackedController()
    const result = checkVideoPlayback('https://cdn.example.com/broken.mp4', cancellation.signal)
    const failure = assert.rejects(result, message)
    const failedVideo = environment.latest()
    failedVideo.error = code ? { code } : null
    failedVideo.onerror()
    await failure
    assertCleaned(failedVideo, environment, cancellation)
  }

  const timeoutController = trackedController()
  const timedOut = checkVideoPlayback('https://cdn.example.com/stalled.mp4', timeoutController.signal)
  const timeoutFailure = assert.rejects(timedOut, /加载超时/)
  const timedVideo = environment.latest()
  environment.fireTimeout()
  await timeoutFailure
  assertCleaned(timedVideo, environment, timeoutController)

  const abortController = trackedController()
  const interrupted = checkVideoPlayback('https://cdn.example.com/slow.mp4', abortController.signal)
  const abortFailure = assert.rejects(interrupted, error => error.name === 'AbortError')
  const interruptedVideo = environment.latest()
  abortController.abort()
  await abortFailure
  assertCleaned(interruptedVideo, environment, abortController)

  const alreadyAborted = trackedController()
  alreadyAborted.abort()
  await assert.rejects(checkVideoPlayback('https://cdn.example.com/unused.mp4', alreadyAborted.signal), error => error.name === 'AbortError')
  assertCleaned(environment.latest(), environment, alreadyAborted)
  assert.equal(environment.latest().loads, 1) // Only cleanup occurs; the source was never loaded.

  assert.equal(videoDuration(0), '00:00')
  assert.equal(videoDuration(65.8), '01:05')
  assert.equal(videoDuration(3605), '60:05')
  console.log('Admin video tests passed: addresses, MP4 file limits, playback metadata, media failures, timeout, cancellation and cleanup')
}

run().catch(error => { console.error(error); process.exitCode = 1 })
