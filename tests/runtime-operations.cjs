const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const assert = require('node:assert/strict')
const { webcrypto } = require('node:crypto')
const ts = require('typescript')
const root = path.resolve(__dirname, '..')
const values = new Map()
const localStorage = { getItem: key => values.get(key) ?? null, setItem: (key, value) => values.set(key, value), removeItem: key => values.delete(key) }
function runtime() {
  const cache = new Map()
  function load(file) {
    const filename = path.resolve(root, file)
    if (cache.has(filename)) return cache.get(filename).exports
    const module = { exports: {} }; cache.set(filename, module)
    const code = ts.transpileModule(fs.readFileSync(filename, 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS } }).outputText
    vm.runInNewContext(code, { module, exports: module.exports, console, Error, Date, URL, crypto: webcrypto, TextEncoder, structuredClone, localStorage, sessionStorage: localStorage, setTimeout, clearTimeout,
      require: name => load(path.resolve(path.dirname(filename), `${name}.ts`)) }, { filename })
    return module.exports
  }
  return load
}
async function main() {
  let call = runtime()('admin/src/mock.ts').mockCall
  const owner = await call('admin.login', { username: 'owner', password: 'owner-password' })
  localStorage.setItem('chiji_admin_token', owner.token)
  localStorage.setItem('chiji_admin_user', JSON.stringify(owner.user))
  const op = (name, data = {}) => call(`admin.runtime.${name}`, data)
  let state = await op('overview')
  assert.ok(state.alerts.some(a => a.key === 'scheduler'))
  await assert.rejects(() => op('settings', state.settings), /缩容到零/)
  await assert.rejects(() => op('run', { task: 'expireOrders' }), /迁移/)
  state = await op('migrate'); assert.ok(state.migrations.every(m => m.status === 'APPLIED'))
  const times = state.migrations.map(m => m.appliedAt).join(',')
  state = await op('migrate'); assert.equal(state.migrations.map(m => m.appliedAt).join(','), times)
  assert.match(state.runs[0].summary, /0 项/)
  await assert.rejects(() => op('run', { task: 'expireOrders', source: 'external' }), /外部调度/)
  const settings = { ...state.settings, externalScheduler: true }
  for (const mediaBase of ['http://media.example.com', 'https://user:secret@media.example.com', 'https://media.example.com/?token=secret', 'https://localhost', 'https://192.168.1.2', 'https://media.example.com/path']) {
    await assert.rejects(() => op('settings', { ...settings, mediaBase }))
  }
  state = await op('settings', settings)
  assert.equal(state.alerts.some(a => a.key === 'scheduler'), false)
  state = await op('run', { task: 'expireOrders', source: 'external', fail: true })
  const failedId = state.runs[0].id
  assert.ok(state.alerts.some(a => a.key === 'expireOrders'))
  await assert.rejects(() => op('run', { task: 'restockNotices', retryOf: failedId }), /失败记录/)
  state = await op('run', { task: 'expireOrders', retryOf: failedId })
  assert.equal(state.runs[0].retryOf, failedId)
  assert.equal(state.runs[0].status, 'SUCCESS')
  assert.equal(state.alerts.some(a => a.key === 'expireOrders'), false)
  state = await op('mediaCheck', { fail: true }); assert.equal(state.media.passed, false)
  state = await op('mediaCheck'); assert.equal(state.media.passed, true)
  state = await op('backup'); const backup = state.backups[0]
  assert.match(backup.checksum, /^[0-9a-f]{64}$/)
  assert.equal('snapshot' in backup, false)
  state = await op('settings', { ...settings, mediaBase: 'https://other.example.com' })
  assert.equal(state.media, null)
  state = await op('drill', { backupId: backup.id }); assert.equal(state.drills[0].status, 'SUCCESS')
  assert.equal(state.settings.mediaBase, 'https://other.example.com') // Isolated restore preserves active settings.
  state = await op('drill', { backupId: backup.id, fail: true }); assert.equal(state.drills[0].status, 'FAILED')
  assert.ok(state.alerts.some(a => a.key === 'backup'))
  await assert.rejects(() => op('drill', { backupId: 'unknown' }), /备份/)
  call = runtime()('admin/src/mock.ts').mockCall
  state = await op('overview'); assert.equal(state.backups[0].id, backup.id)
  assert.equal(state.runs.some(r => r.id === failedId), true)
  const persisted = JSON.parse(localStorage.getItem('chiji_runtime_operations_demo_v1'))
  persisted.runs.find(r => r.task === 'expireOrders').finishedAt = '2020-01-01T00:00:00.000Z'
  localStorage.setItem('chiji_runtime_operations_demo_v1', JSON.stringify(persisted))
  state = await op('overview'); assert.ok(state.alerts.some(a => a.key === 'expireOrders' && a.message.includes('周期')))
  const loader = runtime(), perms = loader('admin/src/permissions.ts')
  for (const action of ['overview','settings','migrate','run','mediaCheck','backup','drill']) {
    assert.throws(() => perms.requirePermission({ username: 'operator', role: 'OPERATOR' }, `admin.runtime.${action}`), /超级管理员/)
  }
  localStorage.removeItem('chiji_admin_token')
  await assert.rejects(() => op('overview'), /登录已失效/)
  console.log('PASS runtime operations: migration, scheduler, execution/retry, alerts, media, isolated recovery, persistence and permissions')
}
main().catch(e => { console.error(e); process.exitCode = 1 })
