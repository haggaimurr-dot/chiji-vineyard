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
  const call = runtime()
  const owner = await call('admin.login', { username: 'identity-owner', password: 'owner-password' })
  localStorage.setItem('chiji_admin_token', owner.token)
  const staff = await call('admin.account.save', { account: { username: 'restricted', password: 'staff-password', role: 'OPERATOR', active: true, permissions: ['order.read'] } })
  const login = await call('admin.login', { username: 'restricted', password: 'staff-password' })
  assert.deepEqual(Array.from(login.user.permissions), ['order.read'])
  localStorage.setItem('chiji_admin_token', login.token)
  await call('admin.order.search', {})
  for (const action of ['admin.order.ship', 'admin.order.export', 'admin.refund.approve', 'admin.dashboard', 'admin.media.upload', 'admin.unknown']) {
    await assert.rejects(() => call(action, {}), /权限不足/)
  }
  localStorage.setItem('chiji_admin_token', owner.token)
  await call('admin.account.permissions', { accountId: staff.id, permissions: [] })
  localStorage.setItem('chiji_admin_token', login.token)
  await assert.rejects(() => call('admin.order.search', {}), /权限不足/)
  assert.deepEqual(Array.from((await call('admin.session')).permissions), [])
  localStorage.setItem('chiji_admin_token', owner.token)
  await assert.rejects(() => call('admin.account.permissions', { accountId: staff.id, permissions: ['super'] }), /权限标识无效/)
  await assert.rejects(() => call('admin.account.permissions', { accountId: owner.user.id, permissions: [] }), /超级管理员/)
  await call('admin.account.permissions', { accountId: staff.id, permissions: ['product.write'] })
  localStorage.setItem('chiji_admin_token', login.token)
  await assert.rejects(() => call('admin.product.save', {}), /调整库存/)
  for (let i = 0; i < 5; i++) await assert.rejects(() => call('admin.login', { username: i % 2 ? 'MISSING' : 'missing', password: 'wrong-password' }), /用户名或密码错误/)
  await assert.rejects(() => call('admin.login', { username: ' missing ', password: 'wrong-password' }), /登录尝试过于频繁/)
  console.log('Identity permissions, live revocation, restricted operations and login limiting checks passed (local demo).')
}
main().catch(error => { console.error(error); process.exitCode = 1 })
