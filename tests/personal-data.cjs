const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const assert = require('node:assert/strict')
const ts = require('typescript')
const root = path.resolve(__dirname, '..')
const storage = new Map(), calls = []
let demoMode = true, spec, confirm = true, remote, modalResolve
const wx = {
  getStorageSync: key => storage.get(key), setStorageSync: (key, value) => storage.set(key, value), removeStorageSync: key => storage.delete(key),
  showToast() {}, showModal: async () => modalResolve ? new Promise(resolve => { modalResolve.resolve = resolve }) : { confirm },
  login: async () => ({ code: 'wechat-code' }),
  cloud: { callContainer: async ({ data }) => { calls.push(data); return remote(data.action, data.data) } },
}
const cache = new Map()
function load(relative) {
  const filename = path.resolve(root, relative)
  if (cache.has(filename)) return cache.get(filename).exports
  const module = { exports: {} }; cache.set(filename, module)
  const source = ts.transpileModule(fs.readFileSync(filename, 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS } }).outputText
  vm.runInNewContext(source, { module, exports: module.exports, wx, console, Error, setTimeout, clearTimeout, getApp: () => ({ globalData: { demoMode } }), Page: value => { spec = value },
    require: name => load(path.resolve(path.dirname(filename), `${name}.ts`)),
  }, { filename })
  return module.exports
}
async function run() {
  const { callApi, restartAccount } = load('miniprogram/utils/api.ts')
  storage.set('chiji_demo_orders', [])
  storage.set('chiji_demo_addresses', [{ id: 'a', name: '地址' }])
  storage.set('chiji_demo_product_history', [{ id: 'p' }])
  await assert.rejects(callApi('account.data.delete', { scope: 'all', confirm: '删除个人数据' }), /范围/)
  assert.equal(storage.get('chiji_demo_addresses').length, 1)
  await callApi('account.data.delete', { scope: 'addresses', confirm: '删除个人数据' })
  assert.equal((await callApi('address.list')).length, 0)
  assert.equal(storage.get('chiji_demo_product_history').length, 1)
  await callApi('account.data.delete', { scope: 'usage', confirm: '删除个人数据' })
  assert.equal(storage.has('chiji_demo_product_history'), false)
  storage.set('chiji_demo_orders', [{ orderStatus: 'PENDING_PAYMENT', refundStatus: 'NONE' }])
  assert.equal((await callApi('account.status')).canClose, false)
  await assert.rejects(callApi('account.close', { confirm: '注销账号' }), /未完成/)
  assert.equal(storage.has('chiji_account_closed'), false)
  storage.set('chiji_demo_orders', [])
  load('miniprogram/pages/personal-data/index.ts')
  const page = { ...spec, data: structuredClone(spec.data), setData(updates) { Object.assign(this.data, updates) } }
  await page.load()
  assert.equal(page.data.canClose, true)
  confirm = false
  storage.set('chiji_demo_addresses', [{ id: 'cancel' }])
  await page.remove({ currentTarget: { dataset: { scope: 'addresses' } } })
  assert.equal(storage.get('chiji_demo_addresses').length, 1, 'cancel must keep data')
  confirm = true; modalResolve = {}
  const deleting = page.remove({ currentTarget: { dataset: { scope: 'addresses' } } })
  assert.equal(page.data.busy, true)
  await page.remove({ currentTarget: { dataset: { scope: 'usage' } } })
  modalResolve.resolve({ confirm: true }); modalResolve = null; await deleting
  assert.equal(page.data.busy, false)
  page.data.confirmation = '注销账号'; await page.closeAccount()
  assert.equal(page.data.closed, true)
  await assert.rejects(callApi('bootstrap'), /注销/)
  assert.ok(await callApi('privacy.get'))
  await restartAccount()
  assert.equal(storage.has('chiji_account_closed'), false)
  assert.equal((await callApi('order.list')).length, 0, 'new registration must not restore orders')

  demoMode = false; storage.set('chiji_mini_token', 'token'); storage.set('chiji_live_product_history', [{ id: 'private' }])
  remote = async () => ({ statusCode: 400, data: { ok: false, error: { message: '删除失败' } } })
  await assert.rejects(callApi('account.data.delete', { scope: 'usage', confirm: '删除个人数据' }), /删除失败/)
  assert.equal(storage.get('chiji_live_product_history').length, 1, 'failed deletion must preserve local data')
  remote = async () => ({ statusCode: 200, data: { ok: true, data: { deleted: true, closed: true } } })
  await callApi('account.close', { confirm: '注销账号' })
  assert.equal(storage.has('chiji_mini_token'), false)
  const before = calls.length
  await assert.rejects(callApi('bootstrap'), /注销/)
  assert.equal(calls.length, before, 'closed account must not silently auto-login')
  remote = async (action, data) => { assert.equal(action, 'auth.login'); assert.equal(data.confirm, '重新注册'); return { statusCode: 200, data: { ok: true, data: { token: 'fresh' } } } }
  await restartAccount(); assert.equal(storage.get('chiji_mini_token'), 'fresh')
  assert.equal(storage.has('chiji_live_product_history'), false)
  console.log('Personal data scope, cancellation, duplicate submission, blocking, closure, failure preservation and explicit registration passed.')
}
run().catch(error => { console.error(error); process.exitCode = 1 })
