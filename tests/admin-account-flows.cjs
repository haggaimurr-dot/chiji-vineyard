const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const assert = require('node:assert/strict')
const { webcrypto } = require('node:crypto')
const ts = require('typescript')
const root = path.resolve(__dirname, '..')
const storage = () => {
  const values = new Map()
  return { getItem: k => values.get(k) || null, setItem: (k, v) => values.set(k, String(v)), removeItem: k => values.delete(k), values }
}
const localStorage = storage(), sessionStorage = storage()
function runtime() {
  const cache = new Map()
  function load(relative) {
    const filename = path.resolve(root, relative)
    if (cache.has(filename)) return cache.get(filename).exports
    const module = { exports: {} }; cache.set(filename, module)
    const code = ts.transpileModule(fs.readFileSync(filename, 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS } }).outputText
    vm.runInNewContext(code, { module, exports: module.exports, console, structuredClone, TextEncoder, Uint8Array, crypto: webcrypto, localStorage, sessionStorage, setTimeout, clearTimeout, require: name => load(path.resolve(path.dirname(filename), `${name}.ts`)) }, { filename })
    return module.exports
  }
  return load('admin/src/mock.ts').mockCall
}
async function main() {
  let call = runtime()
  const login = await call('admin.login', { username: 'owner', password: 'demo-initial' })
  localStorage.setItem('chiji_admin_token', login.token)
  assert.equal(login.user.id, 'admin-demo')
  const created = await call('admin.account.save', { account: { username: 'operator', password: 'created-password', role: 'OPERATOR', active: true } })
  assert.equal(Object.hasOwn(created, 'password'), false)
  assert.equal(JSON.stringify(await call('admin.account.list')).includes('created-password'), false)
  assert.equal(sessionStorage.getItem('chiji_admin_demo_accounts_v1').includes('created-password'), false)
  await assert.rejects(() => call('admin.account.save', { account: { username: 'OPERATOR', password: 'another-password', role: 'OPERATOR', active: true } }), /已存在/)
  await assert.rejects(() => call('admin.account.save', { account: { username: 'short', password: '123', role: 'OPERATOR', active: true } }), /10/)
  await assert.rejects(() => call('admin.account.save', { account: { username: 'long', password: '密'.repeat(129), role: 'OPERATOR', active: true } }), /128/)
  await assert.rejects(() => call('admin.account.save', { account: { id: login.user.id, username: 'owner', role: 'SUPER_ADMIN', active: false } }), /当前账号/)
  await assert.rejects(() => call('admin.account.save', { account: { id: login.user.id, username: 'owner', role: 'OPERATOR', active: true } }), /至少一个/)
  await assert.rejects(() => call('admin.account.save', { account: { id: 'missing', username: 'missing', role: 'OPERATOR', active: true } }), /不存在/)
  await assert.rejects(() => call('admin.account.resetPassword', { accountId: 'missing', password: 'changed-password' }), /不存在/)

  const operator = await call('admin.login', { username: 'operator', password: 'created-password' })
  localStorage.setItem('chiji_admin_token', operator.token)
  await assert.rejects(() => call('admin.account.list'), /超级管理员/)
  localStorage.setItem('chiji_admin_token', login.token)
  const reset = await call('admin.account.resetPassword', { accountId: created.id, password: 'changed-password' })
  assert.equal(reset.currentSessionRevoked, false)
  localStorage.setItem('chiji_admin_token', operator.token)
  await assert.rejects(() => call('admin.product.list'), /登录已失效/)
  await assert.rejects(() => call('admin.login', { username: 'operator', password: 'created-password' }), /用户名或密码错误/)
  const changed = await call('admin.login', { username: 'operator', password: 'changed-password' })
  localStorage.setItem('chiji_admin_token', login.token)
  await call('admin.account.save', { account: { ...created, active: false } })
  await assert.rejects(() => call('admin.login', { username: 'operator', password: 'changed-password' }), /用户名或密码错误/)
  await call('admin.account.resetPassword', { accountId: created.id, password: 'inactive-password' })
  assert.equal((await call('admin.account.list')).find(a => a.id === created.id).active, false)
  await call('admin.account.save', { account: { ...created, active: true } })
  localStorage.setItem('chiji_admin_token', changed.token)
  await assert.rejects(() => call('admin.product.list'), /登录已失效/)
  localStorage.setItem('chiji_admin_token', login.token)

  const config = await call('admin.config.get')
  config.home.banners[0].title = '保存后刷新仍可预览'
  await call('admin.home.save', { home: config.home })
  const invalid = structuredClone(config.home); invalid.banners[0].target = 'product'; invalid.banners[0].targetId = 'missing'
  await assert.rejects(() => call('admin.home.save', { home: invalid }), /未发布/)
  const invalidDate = structuredClone(config.home); invalidDate.announcements = [{ title: '公告', content: '全文', publishedAt: '2026-02-30' }]
  await assert.rejects(() => call('admin.home.save', { home: invalidDate }), /日期/)
  call = runtime()
  assert.equal((await call('admin.config.get')).home.banners[0].title, '保存后刷新仍可预览')
  assert.equal((await call('admin.account.list')).find(a => a.id === created.id).username, 'operator')
  const self = await call('admin.account.resetPassword', { accountId: login.user.id, password: 'owner-new-password' })
  assert.equal(self.currentSessionRevoked, true)
  await assert.rejects(() => call('admin.config.get'), /登录已失效/)
  await assert.rejects(() => call('admin.login', { username: 'owner', password: 'demo-initial' }), /用户名或密码错误/)
  const relogin = await call('admin.login', { username: 'owner', password: 'owner-new-password' })
  localStorage.setItem('chiji_admin_token', relogin.token)
  assert.equal((await call('admin.account.list')).length, 2)
  assert.equal([...sessionStorage.values.values()].some(v => v.includes('owner-new-password') || v.includes('inactive-password')), false)
  console.log('Admin account, password reset, session revocation and saved-preview persistence checks passed (local demo).')
}
main().catch(error => { console.error(error); process.exitCode = 1 })
