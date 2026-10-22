const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const assert = require('node:assert/strict')
const { webcrypto, createHmac } = require('node:crypto')
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

function codeFor(secret) {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'
  let bits = 0, value = 0; const bytes = []
  for (const char of secret) { value = (value << 5) | alphabet.indexOf(char); bits += 5; if (bits >= 8) { bits -= 8; bytes.push((value >>> bits) & 255) } }
  const counter = Buffer.alloc(8); counter.writeBigUInt64BE(BigInt(Math.floor(Date.now() / 30000)))
  const sum = createHmac('sha1', Buffer.from(bytes)).update(counter).digest(); const offset = sum[19] & 15
  return String((sum.readUInt32BE(offset) & 0x7fffffff) % 1000000).padStart(6, '0')
}
async function main() {
  let call = runtime()
  const owner = await call('admin.login', { username: 'security-owner', password: 'original-password' })
  localStorage.setItem('chiji_admin_token', owner.token)
  const operator = await call('admin.account.save', { account: { username: 'security-operator', password: 'operator-password', role: 'OPERATOR', active: true, permissions: [] } })
  const login = await call('admin.login', { username: operator.username, password: 'operator-password' })
  localStorage.setItem('chiji_admin_token', login.token)
  assert.equal((await call('admin.security.status')).enabled, false)
  await assert.rejects(() => call('admin.account.changePassword', { currentPassword: 'wrong', password: 'new-password' }), /原密码错误/)
  await call('admin.account.changePassword', { currentPassword: 'operator-password', password: 'new-password', accountId: owner.user.id })
  await assert.rejects(() => call('admin.session'), /登录已失效/)
  const relogin = await call('admin.login', { username: operator.username, password: 'new-password' })
  localStorage.setItem('chiji_admin_token', relogin.token)
  assert.equal((await call('admin.session')).id, operator.id)
  // Start the MFA lifecycle as owner so preview rate limits are independent.
  localStorage.setItem('chiji_admin_token', owner.token)
  const setup = await call('admin.security.setup', { currentPassword: 'original-password' })
  assert.equal(setup.secret.length, 32)
  assert.equal((await call('admin.security.status')).enabled, false)
  const enrolled = await call('admin.security.enable', { currentPassword: 'original-password', code: codeFor(setup.secret) })
  assert.equal(enrolled.recoveryCodes.length, 8)
  assert.equal(enrolled.sessionRevoked, true)
  await assert.rejects(() => call('admin.session'), /登录已失效/)
  const required = await call('admin.login', { username: 'security-owner', password: 'original-password' })
  assert.equal(required.mfaRequired, true)
  assert.equal(required.token, undefined)
  await assert.rejects(() => call('admin.login', { username: 'security-owner', password: 'original-password', code: codeFor(setup.secret) }), /动态码或恢复码无效/)
  const recovered = await call('admin.login', { username: 'security-owner', password: 'original-password', code: enrolled.recoveryCodes[0] })
  localStorage.setItem('chiji_admin_token', recovered.token)
  assert.equal((await call('admin.security.status')).recoveryRemaining, 7)
  await assert.rejects(() => call('admin.login', { username: 'security-owner', password: 'original-password', code: enrolled.recoveryCodes[0] }), /动态码或恢复码无效/)
  call = runtime()
  assert.equal((await call('admin.security.status')).enabled, true)
  const result = await call('admin.security.disable', { currentPassword: 'original-password', code: enrolled.recoveryCodes[1] })
  assert.equal(result.sessionRevoked, true)
  await assert.rejects(() => call('admin.session'), /登录已失效/)
  await assert.rejects(() => call('admin.login', { username: 'security-owner', password: 'original-password' }), /登录尝试过于频繁/)
  // Advance the persisted rate-limit window before testing the next login.
  const counters = JSON.parse(sessionStorage.getItem('chiji_admin_demo_login_limits'))
  counters.forEach(([, record]) => { record.expires = Date.now() - 1 })
  sessionStorage.setItem('chiji_admin_demo_login_limits', JSON.stringify(counters))
  call = runtime()
  const withoutMFA = await call('admin.login', { username: 'security-owner', password: 'original-password' })
  localStorage.setItem('chiji_admin_token', withoutMFA.token)
  assert.equal((await call('admin.security.status')).enabled, false)
  assert.equal(JSON.stringify([...sessionStorage.values.values()]).includes('original-password'), false)
  console.log('Personal password change, operator access, MFA challenge, replay rejection, recovery and disable checks passed.')
}
main().catch(error => { console.error(error); process.exitCode = 1 })
