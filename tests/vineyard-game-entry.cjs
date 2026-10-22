const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const assert = require('node:assert/strict')
const ts = require('typescript')
const root = path.resolve(__dirname, '..')

async function run() {
  const cache = new Map(), navigations = [], modals = [], toasts = [], pages = []
  let apiCalls = 0, storageReads = 0, throwNavigation = false
  const app = { globalData: { demoMode: true, token: 'mall-token-private', user: { id: 'mall-user-private', nickname: '商城用户' } } }
  const wx = {
    navigateToMiniProgram(options) {
      if (throwNavigation) throw new Error('unsupported runtime')
      navigations.push(options)
    },
    showToast: options => toasts.push(options),
    showModal: options => modals.push(options),
    getStorageSync() { storageReads++; throw new Error('the game entry must not read mall credentials') },
  }
  function load(filename) {
    filename = path.resolve(root, filename)
    if (cache.has(filename)) return cache.get(filename).exports
    const module = { exports: {} }
    cache.set(filename, module)
    const source = ts.transpileModule(fs.readFileSync(filename, 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS } }).outputText
    vm.runInNewContext(source, {
      module, exports: module.exports, wx, getApp: () => app,
      Page: page => { page.setData = updates => Object.assign(page.data, updates); pages.push(page) },
      require: name => name.endsWith('/api') ? { callApi: () => { apiCalls++; throw new Error('game entry must not call the mall API') } } : load(path.resolve(path.dirname(filename), `${name}.ts`)),
    }, { filename })
    return module.exports
  }

  const config = load('miniprogram/data/vineyard-game.ts').vineyardGameConfig
  const helper = load('miniprogram/utils/vineyard-entry.ts')
  load('miniprogram/pages/home/index.ts')
  load('miniprogram/pages/profile/index.ts')
  const [home, profile] = pages
  assert.equal(config.appId, '', 'unconfigured checkout never navigates to an invented AppID')
  assert.equal(config.envVersion, 'release')
  await home.goVineyardGame()
  assert.equal(navigations.length, 0)
  assert.equal(toasts.at(-1).title, '葡萄园小游戏正在准备，暂未开放')
  assert.equal(home.data.vineyardOpening, false)
  config.appId = '   '
  assert.equal(await helper.openVineyardGame('profile'), 'unavailable')
  assert.equal(navigations.length, 0)

  // Test AppID only: the production configuration stays empty on disk.
  config.appId = 'wx1234567890abcdef'
  config.envVersion = 'trial'
  const first = home.goVineyardGame()
  assert.equal(home.data.vineyardOpening, true)
  await home.goVineyardGame()
  await profile.goVineyardGame()
  assert.equal(navigations.length, 1, 'rapid clicks and a second page share one navigation lock')
  assert.equal(profile.data.vineyardOpening, false)
  const request = navigations.at(-1)
  assert.equal(request.appId, config.appId)
  assert.equal(request.envVersion, 'trial')
  assert.deepEqual(JSON.parse(JSON.stringify(request.extraData)), { source: 'chiji-grape', entry: 'home', schemaVersion: 1 })
  assert.deepEqual(Object.keys(request).sort(), ['appId', 'envVersion', 'extraData', 'fail', 'success'])
  assert.equal(JSON.stringify(request).includes('private'), false, 'launch data cannot contain a mall token or user')
  request.success({ errMsg: 'navigateToMiniProgram:ok' })
  request.fail({ errMsg: 'duplicate callback' })
  await first
  assert.equal(home.data.vineyardOpening, false)
  assert.equal(modals.length, 0, 'late failures after success are ignored')

  config.envVersion = 'develop'
  const cancelled = profile.goVineyardGame()
  assert.equal(navigations.at(-1).envVersion, 'develop')
  assert.equal(navigations.at(-1).extraData.entry, 'profile')
  navigations.at(-1).fail({ errMsg: 'navigateToMiniProgram:fail cancel' })
  await cancelled
  assert.equal(profile.data.vineyardOpening, false)
  assert.match(toasts.at(-1).title, /已取消/)
  assert.equal(modals.length, 0, 'user cancellation does not show an error or demand a retry')

  const failed = home.goVineyardGame()
  navigations.at(-1).fail({ errMsg: 'navigateToMiniProgram:fail network unavailable' })
  assert.equal(home.data.vineyardOpening, true, 'failure confirmation still blocks duplicate navigation')
  assert.equal(modals.at(-1).confirmText, '重试')
  const beforeModalClick = navigations.length
  assert.equal(await helper.openVineyardGame('profile'), 'busy')
  assert.equal(navigations.length, beforeModalClick)
  modals.at(-1).success({ confirm: false, cancel: true })
  await failed
  assert.equal(home.data.vineyardOpening, false)

  const retry = profile.goVineyardGame()
  const oldAttempt = navigations.at(-1)
  oldAttempt.fail({ errMsg: 'navigateToMiniProgram:fail transient' })
  modals.at(-1).success({ confirm: true, cancel: false })
  assert.equal(navigations.length, beforeModalClick + 2, 'retry launches once from the confirmation action')
  assert.equal(navigations.at(-1).extraData.entry, 'profile')
  oldAttempt.success({ errMsg: 'late success' })
  assert.equal(profile.data.vineyardOpening, true, 'old attempt cannot release the new attempt')
  navigations.at(-1).success({ errMsg: 'navigateToMiniProgram:ok' })
  await retry
  assert.equal(profile.data.vineyardOpening, false)

  throwNavigation = true
  const synchronousFailure = home.goVineyardGame()
  modals.at(-1).fail({ errMsg: 'showModal:fail' })
  await synchronousFailure
  assert.equal(home.data.vineyardOpening, false, 'synchronous platform failures and dialog failure release the lock')
  throwNavigation = false
  const navigate = wx.navigateToMiniProgram
  delete wx.navigateToMiniProgram
  assert.equal(await helper.openVineyardGame('profile'), 'unavailable')
  assert.match(toasts.at(-1).title, /更新微信/)
  wx.navigateToMiniProgram = navigate
  assert.equal(apiCalls, 0, 'opening a separate game requires no mall login or backend call')
  assert.equal(storageReads, 0, 'opening a separate game reads no personal data')

  for (const pageName of ['home', 'profile']) {
    const template = fs.readFileSync(path.join(root, 'miniprogram/pages', pageName, 'index.wxml'), 'utf8')
    assert.match(template, /bindtap="goVineyardGame"/, `${pageName} exposes the game action`)
    assert.match(template, /aria-label="[^"]*独立微信小游戏"/, `${pageName} explains that it opens a separate game`)
  }
  console.log('Vineyard game entry passed: empty config, home/profile launch data, privacy, concurrent clicks, cancellation, retries, stale callbacks and platform failures')
}

run().catch(error => { console.error(error); process.exitCode = 1 })
