const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const assert = require('node:assert/strict')
const { webcrypto } = require('node:crypto')
const ts = require('typescript')
const root = path.resolve(__dirname, '..')

const store = () => {
  const values = new Map()
  return { getItem: key => values.get(key) ?? null, setItem: (key, value) => values.set(key, value), removeItem: key => values.delete(key) }
}
function runtime(globals) {
  const cache = new Map()
  return function load(relative) {
    const filename = path.resolve(root, relative)
    if (cache.has(filename)) return cache.get(filename).exports
    const module = { exports: {} }; cache.set(filename, module)
    const code = ts.transpileModule(fs.readFileSync(filename, 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS } }).outputText
    vm.runInNewContext(code, { module, exports: module.exports, console, Error, structuredClone, crypto: webcrypto, TextEncoder, setTimeout, clearTimeout, ...globals,
      require: name => load(path.resolve(path.dirname(filename), `${name}.ts`)) }, { filename })
    return module.exports
  }
}

async function run() {
  let now = Date.parse('2026-10-04T15:59:59.900Z')
  class ClockDate extends Date { constructor(...args) { if (args.length) super(...args); else super(now) } }
  const localStorage = store(), sessionStorage = store()
  const globals = { localStorage, sessionStorage, Date: ClockDate }
  let call = runtime(globals)('admin/src/mock.ts').mockCall
  const owner = await call('admin.login', { username: 'owner', password: 'owner-password' })
  localStorage.setItem('chiji_admin_token', owner.token)
  localStorage.setItem('chiji_admin_user', JSON.stringify(owner.user))
  assert.equal((await call('admin.config.versions')).total, 0)
  const original = await call('admin.config.get'), coupons = await call('admin.coupon.get')
  const save = async (key, value) => call(key === 'shipping' ? 'admin.config.save' : key === 'coupons' ? 'admin.coupon.save' : `admin.${key}.save`, key === 'coupons' ? value : { [key]: value })
  const initial = { ...original, coupons }
  for (const key of ['shipping', 'coupons', 'home', 'privacy']) {
    const changed = structuredClone(initial[key])
    if (key === 'shipping') changed.freight = 3600
    if (key === 'coupons') changed.items[0].amount = 2000
    if (key === 'home') changed.storyTitle = '历史测试果园'
    if (key === 'privacy') changed.version = '2.0'
    await save(key, changed)
    const page = await call('admin.config.versions', { key, limit: 2 })
    assert.equal(page.currentVersions[key], 2)
    assert.deepEqual(page.items.map(item => item.action), ['SAVE', 'BASELINE'])
    assert.deepEqual(page.items[1].payload, initial[key])
    const baselineId = page.items[1].id
    page.items[1].payload.injected = 'must not alter stored snapshot'
    const restored = await call('admin.config.rollback', { key, versionId: baselineId, expectedVersion: 2, reason: '恢复原有配置' })
    assert.equal(restored.version, 3)
    assert.deepEqual(restored.payload, initial[key])
    const readBack = key === 'coupons' ? await call('admin.coupon.get') : (await call('admin.config.get'))[key]
    assert.deepEqual(readBack, initial[key])
    await assert.rejects(() => call('admin.config.rollback', { key, versionId: baselineId, expectedVersion: 2, reason: '过期页面' }), /配置已变化/)
    const latest = await call('admin.config.versions', { key, limit: 2 })
    assert.equal(latest.total, 3)
    assert.equal(latest.items[0].sourceVersion, 1)
    assert.equal(latest.items[0].action, 'ROLLBACK')
  }
  const beforeInvalid = await call('admin.config.versions')
  await assert.rejects(() => save('shipping', { freight: -1, freeShippingThreshold: 0, blockedProvinces: [] }), /运费/)
  await assert.rejects(() => save('coupons', { items: [{ ...coupons.items[0], startDate: '2026-02-30' }] }), /优惠券/)
  await assert.rejects(() => call('admin.config.rollback', { key: 'shipping', versionId: beforeInvalid.items.find(item => item.key === 'shipping').id, expectedVersion: 3, reason: '' }), /原因/)
  assert.equal((await call('admin.config.versions')).total, beforeInvalid.total)
  const pageA = await call('admin.config.versions', { limit: 5, offset: 0 }), pageB = await call('admin.config.versions', { limit: 5, offset: 5 })
  assert.equal(pageA.total, 12)
  assert.equal(pageA.items.length, 5)
  assert.ok(pageB.items.every(item => !pageA.items.some(first => first.id === item.id)))
  const beforeMidnight = await call('admin.audit.list', { startDate: '2026-10-04', endDate: '2026-10-04' })
  assert.equal(beforeMidnight.total, 9) // login + four saves + four rollbacks
  now = Date.parse('2026-10-04T16:00:00.000Z')
  await save('shipping', { ...original.shipping, freight: 1500 })
  assert.equal((await call('admin.audit.list', { startDate: '2026-10-04', endDate: '2026-10-04' })).total, 9)
  const nextDay = await call('admin.audit.list', { startDate: '2026-10-05', endDate: '2026-10-05', action: 'CONFIG_SAVE', username: 'own', targetId: 'ship' })
  assert.equal(nextDay.total, 1)
  assert.equal(nextDay.items[0].username, 'owner')
  await assert.rejects(() => call('admin.audit.list', { startDate: '2026-02-30' }), /日期/)
  await assert.rejects(() => call('admin.audit.list', { startDate: '2026-10-06', endDate: '2026-10-04' }), /开始日期/)
  await assert.rejects(() => call('admin.config.versions', { key: 'credentials' }), /配置类型/)
  await assert.rejects(() => call('admin.config.versions', { limit: 101 }), /分页/)
  // Reload the module with the same browser stores: configs and history stay aligned.
  call = runtime(globals)('admin/src/mock.ts').mockCall
  assert.equal((await call('admin.config.get')).shipping.freight, 1500)
  assert.equal((await call('admin.config.versions')).currentVersions.shipping, 4)
  assert.equal((await call('admin.audit.list')).total, 10)
  const home = (await call('admin.config.get')).home
  await save('home', { ...home, productIds: ['p-shine'] })
  const target = (await call('admin.config.versions', { key: 'home' })).items[0]
  await save('home', { ...home, productIds: [] })
  const product = (await call('admin.product.list')).find(item => item.id === 'p-shine')
  await call('admin.product.save', { product: { ...product, status: 'DRAFT' } })
  await assert.rejects(() => call('admin.config.rollback', { key: 'home', versionId: target.id, expectedVersion: 5, reason: '测试下架目标' }), /未发布|未上架/)
  assert.equal((await call('admin.config.versions', { key: 'home' })).currentVersions.home, 5)
  const operator = await call('admin.account.save', { account: { username: 'operator', password: 'never-log-this-password', role: 'OPERATOR', active: true } })
  const accountLog = await call('admin.audit.list', { action: 'ADMIN_ACCOUNT_SAVE' })
  assert.equal(accountLog.items[0].targetId, operator.id)
  assert.ok(!JSON.stringify(accountLog).includes('never-log-this-password'))
  assert.ok(!JSON.stringify(accountLog).includes('password'))
  const diff = runtime(globals)('admin/src/config-diff.ts').configDiff
  const comparison = await call('admin.config.compare', { key: 'shipping', fromVersion: 1, toVersion: 4 })
  assert.equal(comparison.before.freight, original.shipping.freight)
  assert.equal(comparison.after.freight, 1500)
  assert.ok(diff(comparison.before, comparison.after).some(item => item.path === '/freight' && item.kind === 'changed'))
  const same = await call('admin.config.compare', { key: 'shipping', fromVersion: 4, toVersion: 4 })
  assert.equal(diff(same.before, same.after).length, 0)
  assert.deepEqual(JSON.parse(JSON.stringify(diff({ 'a/b': null, list: [1, 2], gone: false }, { 'a/b': false, list: [1], added: 0 }))), [
    { path: '/a~1b', kind: 'changed', before: null, after: false },
    { path: '/list/1', kind: 'removed', before: 2 },
    { path: '/gone', kind: 'removed', before: false },
    { path: '/added', kind: 'added', after: 0 },
  ])
  await assert.rejects(() => call('admin.config.compare', { key: 'shipping', fromVersion: 1, toVersion: 999 }), /不存在/)
  const exportFilter = { action: 'CONFIG_SAVE', username: 'own', startDate: '2026-10-05', endDate: '2026-10-05' }
  const expected = await call('admin.audit.list', exportFilter)
  const exported = await call('admin.audit.export', { ...exportFilter, limit: 1, offset: 999 })
  assert.equal(exported.count, expected.total)
  assert.ok(exported.content.startsWith('\uFEFF'))
  assert.ok(exported.content.includes('时间（北京时间）'))
  assert.ok(exported.content.includes('2026-10-05 00:00:00'))
  assert.equal((await call('admin.audit.list', { action: 'AUDIT_EXPORT' })).total, 1)
  const empty = await call('admin.audit.export', { action: 'NO_SUCH_ACTION' })
  assert.equal(empty.count, 0)
  assert.equal(empty.content.split('\r\n').length, 2)
  await assert.rejects(() => call('admin.audit.export', { startDate: '2026-02-30' }), /日期/)
  // Seed a separate session to exercise multi-page export and legacy redaction.
  const exportStorage = store()
  const payloads = { shipping: {}, coupons: {}, home: {}, privacy: {} }
  const seededLogs = Array.from({ length: 101 }, (_, index) => ({
    id: String(index + 1), adminId: '001', username: '=1+1', action: 'CONFIG_SAVE', targetId: 'shipping',
    detail: { nested: [{ access_token: 'never-export-token' }], passwordChanged: true, reason: '逗号,引号"与换行\n内容' }, createdAt: '2026-10-04T16:00:00Z',
  }))
  const seed = logs => exportStorage.setItem('chiji_admin_demo_history_v1', JSON.stringify({ snapshots: [], payloads, logs }))
  seed(seededLogs)
  const makeHistory = runtime({ ...globals, sessionStorage: exportStorage })('admin/src/mock-history.ts').createMockHistory
  const history = makeHistory(key => payloads[key], (key, value) => { payloads[key] = value }, (_, value) => value)
  const many = history.exportAudit({ action: 'CONFIG_SAVE', limit: 1, offset: 100 })
  assert.equal(many.count, 101)
  assert.ok(many.content.includes("'=1+1"))
  assert.ok(!many.content.includes('never-export-token'))
  assert.ok(many.content.includes('[已隐藏]'))
  assert.ok(many.content.includes('passwordChanged'))
  seed(Array.from({ length: 10001 }, (_, index) => ({ ...seededLogs[0], id: String(index + 1) })))
  const oversized = makeHistory(key => payloads[key], () => {}, (_, value) => value)
  assert.throws(() => oversized.exportAudit({}), /10000/)
  const operatorLogin = await call('admin.login', { username: 'operator', password: 'never-log-this-password' })
  localStorage.setItem('chiji_admin_token', operatorLogin.token)
  localStorage.setItem('chiji_admin_user', JSON.stringify(operatorLogin.user))
  for (const action of ['admin.config.versions', 'admin.config.rollback', 'admin.audit.list', 'admin.config.compare', 'admin.audit.export']) await assert.rejects(() => call(action), /超级管理员/)
  console.log('Configuration history, rollback, audit filters, session persistence and permissions passed.')
}
run().catch(error => { console.error(error); process.exitCode = 1 })
