const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const assert = require('node:assert/strict')
const ts = require('typescript')
const root = path.resolve(__dirname, '..')
const storageKey = 'chiji_admin_demo_leads_v1'

function runtime(storage, sessions = new Map(), options = {}) {
  const cache = new Map(), logs = []
  const localStorage = { getItem: key => { if (options.failRead && key === storageKey) throw new Error('storage denied'); return storage.get(key) ?? null }, setItem: (key, value) => { if (options.failWrite && key === storageKey) throw new Error('storage full'); storage.set(key, value) } }
  const sessionStorage = { getItem: key => sessions.get(key) ?? null, setItem: (key, value) => sessions.set(key, value) }
  const load = relative => {
    let filename = path.resolve(root, relative)
    if (!fs.existsSync(filename) && filename.endsWith('.ts')) filename = filename.slice(0, -3) + '.js'
    if (cache.has(filename)) return cache.get(filename).exports
    const module = { exports: {} }; cache.set(filename, module)
    const source = ts.transpileModule(fs.readFileSync(filename, 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS } }).outputText
    vm.runInNewContext(source, { module, exports: module.exports, localStorage, sessionStorage, crypto: require('node:crypto').webcrypto, TextEncoder, structuredClone, console: { log: (...args) => logs.push(args), error: (...args) => logs.push(args) }, setTimeout: callback => callback(), require: name => load(path.resolve(path.dirname(filename), `${name}.ts`)) }, { filename })
    return module.exports
  }
  const call = load('admin/src/mock.ts').mockCall
  const login = async (username = '运营甲', password = 'demo-password') => {
    const result = await call('admin.login', { username, password })
    storage.set('chiji_admin_token', result.token); storage.set('chiji_admin_user', JSON.stringify(result.user))
    return result.user
  }
  return { call, login, logs, sessions, load }
}

async function run() {
  const seed = Array.from({ length: 35 }, (_, i) => ({
    id: `lead-${String(i).padStart(2, '0')}`, organization: `客户 ${i}`, contact: `测试联系人 ${i}`, phone: `137${i}`, region: i % 2 ? '苏州' : '杭州', channel: i % 2 ? '企业团购' : '社区团购', volume: '每周 10 箱', demand: '家庭小盒', status: 'NEW', followUpNote: '', followUps: [], createdAt: '2026-09-19T05:20:00.000Z', ownerId: '', ownerName: '', reminderAt: '', reminderDone: false, version: 0,
  }))
  seed[0].region = '北京'; seed[0].channel = '经销商'
  seed[2].followUpNote = '新的说明'; seed[2].followUps = [{ id: 'history-old', note: 'Old OFFER 蓝莓', status: 'NEW', previousStatus: 'NEW', createdAt: '2026-09-18T05:20:00.000Z', username: '旧运营' }, { id: 'history-new', note: '新的说明', status: 'NEW', previousStatus: 'NEW', createdAt: '2026-09-19T05:20:00.000Z', username: '旧运营' }]
  seed[3].status = 'CONTACTED'; seed[3].followUpNote = '已经发送报价'; delete seed[3].followUps
  seed[34].organization = 'ALPHA 葡萄'; seed[34].demand = '家庭小盒 %_ literal'
  const storage = new Map([[storageKey, JSON.stringify({ version: 1, items: seed })], ['chiji_admin_user', JSON.stringify({ username: '运营甲' })]])
  const demo = runtime(storage)
  const { call, logs } = demo
  const actor = await demo.login()
  assert.equal(actor.id, 'admin-demo')
  let result = await call('admin.lead.list')
  assert.equal(result.total, 35); assert.equal(result.items.length, 20); assert.equal(result.limit, 20); assert.equal(result.offset, 0)
  assert.equal(result.items[0].id, 'lead-34'); assert.equal(result.items.at(-1).id, 'lead-15')
  result = await call('admin.lead.list', { offset: 20, limit: 20 })
  assert.equal(result.items.length, 15); assert.equal(result.items[0].id, 'lead-14'); assert.equal(result.total, 35)
  assert.equal((await call('admin.lead.list', { offset: 40, limit: 20 })).items.length, 0)
  result = await call('admin.lead.list', { keyword: 'OLD offer' })
  assert.equal(result.total, 1); assert.equal(result.items[0].id, 'lead-02')
  assert.equal((await call('admin.lead.list', { keyword: 'alpha 13734' })).items[0].id, 'lead-34')
  assert.equal((await call('admin.lead.list', { keyword: '%_' })).total, 1)
  assert.equal((await call('admin.lead.list', { keyword: '测试联系人 每周 家庭' })).total, 35)
  assert.equal((await call('admin.lead.list', { status: 'CONTACTED', keyword: '报价' })).items[0].id, 'lead-03')
  result = await call('admin.lead.list', { channel: '社区团购', region: '杭州', status: 'NEW', limit: 3, offset: 3 })
  assert.equal(result.total, 17); assert.equal(result.items.length, 3)
  assert.deepEqual(result.channels, ['企业团购', '社区团购', '经销商'].sort()); assert.deepEqual(result.regions, ['北京', '杭州', '苏州'].sort())
  assert.equal((await call('admin.lead.list', { status: 'LOST' })).total, 0)
  assert.equal((await call('admin.lead.list', { limit: 500, offset: -5 })).limit, 100)
  assert.equal((await call('admin.lead.list', { limit: 0, offset: 0.5 })).offset, 0)

  const original = (await call('admin.lead.list', { keyword: '客户 3', status: 'CONTACTED' })).items[0]
  assert.equal(original.followUps.length, 1); assert.equal(original.followUps[0].id, 'legacy-lead-03'); assert.equal(original.followUps[0].createdAt, ''); assert.equal(original.followUps[0].username, '历史记录')
  let updated = await call('admin.lead.update', { leadId: 'lead-03', expectedVersion: 0, status: 'CONTACTED', followUpNote: '  客户希望追加礼盒  ' })
  assert.equal(updated.followUps.length, 2); assert.equal(updated.followUpNote, '客户希望追加礼盒')
  assert.equal(updated.followUps[0].note, '已经发送报价'); assert.equal(updated.followUps[0].legacy, true)
  assert.equal(updated.followUps[1].username, '运营甲'); assert.equal(updated.followUps[1].previousStatus, 'CONTACTED'); assert.ok(!Number.isNaN(Date.parse(updated.followUps[1].createdAt)))
  updated = await call('admin.lead.update', { leadId: 'lead-03', expectedVersion: 1, status: 'QUALIFIED', followUpNote: '' })
  assert.equal(updated.followUpNote, '客户希望追加礼盒'); assert.equal(updated.followUps.length, 3)
  assert.equal(updated.followUps[2].note, ''); assert.equal(updated.followUps[2].previousStatus, 'CONTACTED'); assert.equal(updated.followUps[2].status, 'QUALIFIED')
  result = await call('admin.lead.list', { keyword: '已经发送报价', status: 'QUALIFIED' })
  assert.equal(result.total, 1); assert.equal(result.items[0].id, 'lead-03')
  updated.followUps[0].note = '篡改返回值'
  assert.equal((await call('admin.lead.list', { keyword: '已经发送报价' })).total, 1)
  const beforeInvalid = storage.get(storageKey)
  const noop = await call('admin.lead.update', { leadId: 'lead-03', expectedVersion: 2, status: 'QUALIFIED', followUpNote: '  ' })
  assert.equal(noop.version, 2)
  await assert.rejects(() => call('admin.lead.update', { leadId: 'lead-03', expectedVersion: 2, status: 'INVALID', followUpNote: '拒绝' }), /无效线索状态/)
  await assert.rejects(() => call('admin.lead.update', { leadId: 'missing', expectedVersion: 0, status: 'NEW', followUpNote: '拒绝' }), /不存在/)
  await assert.rejects(() => call('admin.lead.update', { leadId: 'lead-03', expectedVersion: 2, followUpNote: '字'.repeat(2001) }), /2000/)
  await assert.rejects(() => call('admin.lead.update', { leadId: 'lead-03', expectedVersion: 2, followUpNote: 12 }), /格式无效/)
  assert.equal(storage.get(storageKey), beforeInvalid)

  const persisted = (await call('admin.lead.list', { keyword: '已经发送报价' })).items[0]
  const refreshed = runtime(storage, demo.sessions)
  result = await refreshed.call('admin.lead.list', { keyword: '已经发送报价' })
  assert.deepEqual(result.items[0], persisted)
  assert.equal(result.items[0].followUps.length, 3)
  assert.equal(logs.length, 0); assert.equal(refreshed.logs.length, 0)

  for (const broken of ['{bad json', JSON.stringify({ version: 2, items: seed }), JSON.stringify({ version: 1, items: [null] }), JSON.stringify({ version: 1, items: [{ ...seed[0], followUps: [{ note: 'incomplete' }] }] }), JSON.stringify({ version: 1, items: [seed[0], seed[0]] })]) {
    const brokenStorage = new Map([[storageKey, broken]]), brokenDemo = runtime(brokenStorage)
    await brokenDemo.login()
    await assert.rejects(() => brokenDemo.call('admin.lead.list'), /存储格式异常/)
    await assert.rejects(() => brokenDemo.call('admin.lead.get', { leadId: 'l-1' }), /存储格式异常/)
    await assert.rejects(() => brokenDemo.call('admin.lead.update', { leadId: 'l-1', expectedVersion: 0, ownerId: actor.id }), /存储格式异常/)
    assert.equal(brokenStorage.get(storageKey), broken)
  }
  await workflow(demo, storage, actor)
  await crm(demo, storage)

  console.log('Lead search, filters, pagination, assignment, authenticated history, reminder lifecycle, optimistic conflicts, migration, persistence and storage failure protection passed.')
}

async function workflow(demo, storage, actor) {
  const { call } = demo
  const owner = await call('admin.account.save', { account: { username: '渠道运营', password: 'operator-password', role: 'OPERATOR', active: true } })
  const disabled = await call('admin.account.save', { account: { username: '停用运营', password: 'disabled-password', role: 'OPERATOR', active: false } })
  let owners = await call('admin.lead.owners')
  assert.equal(owners.some(item => item.id === disabled.id), false)
  assert.equal(owners.find(item => item.id === owner.id).role, 'OPERATOR')
  assert.equal(owners.find(item => item.id === actor.id).role, 'SUPER_ADMIN')
  assert.deepEqual(Object.keys(owners[0]).sort(), ['active', 'id', 'role', 'username'])

  let lead = await call('admin.lead.get', { leadId: 'lead-01' })
  const update = patch => call('admin.lead.update', { leadId: lead.id, expectedVersion: lead.version, ...patch })
  lead = await update({ ownerId: owner.id, status: 'CONTACTED', followUpNote: '  联系采购，发送报价  ' })
  assert.equal(lead.ownerId, owner.id); assert.equal(lead.ownerName, owner.username); assert.equal(lead.version, 1)
  let history = await call('admin.lead.history', { leadId: lead.id })
  assert.equal(history.length, 1); assert.deepEqual(history[0].changes, ['owner', 'status', 'note'])
  assert.equal(history[0].actorId, actor.id); assert.equal(history[0].actorName, actor.username)
  assert.equal(history[0].note, '联系采购，发送报价'); assert.equal(history[0].ownerId, owner.id)
  assert.equal((await call('admin.lead.list', { ownerId: owner.id, keyword: '渠道运营' })).items[0].id, lead.id)
  assert.equal((await call('admin.lead.list', { ownerId: '__unassigned__' })).total, 34)

  const beforeConflict = storage.get(storageKey)
  for (const expectedVersion of [undefined, -1, 1.5, '1', null]) await assert.rejects(() => call('admin.lead.update', { leadId: lead.id, expectedVersion, status: 'QUALIFIED' }), /expectedVersion/)
  await assert.rejects(() => call('admin.lead.update', { leadId: lead.id, expectedVersion: 0, followUpNote: '旧页面' }), /已被其他人更新/)
  await assert.rejects(() => update({ ownerId: disabled.id }), /不存在或已停用/)
  await assert.rejects(() => update({ ownerId: 'missing' }), /不存在或已停用/)
  await assert.rejects(() => update({ ownerId: null }), /参数无效/)
  await assert.rejects(() => update({ followUpNote: null }), /格式无效/)
  assert.equal(storage.get(storageKey), beforeConflict)
  lead = await update({ followUpNote: lead.followUpNote })
  history = await call('admin.lead.history', { leadId: lead.id })
  assert.equal(history.length, 2); assert.deepEqual(history[0].changes, ['note']); assert.equal(lead.version, 2)
  history[0].note = '篡改返回值'; assert.equal((await call('admin.lead.history', { leadId: lead.id }))[0].note, lead.followUpNote)
  const beforeNoop = storage.get(storageKey)
  const noop = await update({ ownerId: owner.id, status: lead.status, followUpNote: '' })
  assert.equal(noop.version, lead.version); assert.equal(storage.get(storageKey), beforeNoop)

  await demo.login(owner.username, 'operator-password')
  owners = await call('admin.lead.owners')
  assert.ok(owners.some(item => item.id === owner.id))
  await assert.rejects(() => call('admin.account.list'), /超级管理员/)
  lead = await update({ followUpNote: '运营人员确认包装要求' })
  history = await call('admin.lead.history', { leadId: lead.id })
  assert.equal(history[0].actorId, owner.id); assert.equal(history[0].actorName, owner.username)
  const beforeBadReminders = storage.get(storageKey)
  for (const reminderAt of ['invalid', '2099-02-30T08:00:00Z', '2099-10-04T08:00', '2099-10-04T08:00:00+24:00', new Date(Date.now() - 1000).toISOString()]) await assert.rejects(() => update({ reminderAt }), /提醒时间/)
  await assert.rejects(() => update({ reminderDone: true }), /先设置提醒/)
  await assert.rejects(() => update({ reminderDone: 'false' }), /状态无效/)
  assert.equal(storage.get(storageKey), beforeBadReminders)
  lead = await update({ reminderAt: '2099-10-04T08:00:00+08:00' })
  assert.equal(lead.reminderAt, '2099-10-04T00:00:00.000Z'); assert.equal(lead.reminderDone, false)
  assert.equal((await call('admin.lead.list', { reminder: 'upcoming', ownerId: owner.id })).total, 1)
  assert.equal((await call('admin.lead.reminders'))[0].id, lead.id)
  lead = await update({ reminderDone: true })
  assert.equal((await call('admin.lead.reminders')).length, 0)
  assert.equal((await call('admin.lead.list', { reminder: 'done' })).total, 1)
  lead = await update({ reminderDone: false })
  lead = await update({ status: 'WON' })
  assert.equal(lead.reminderDone, true)
  assert.deepEqual((await call('admin.lead.history', { leadId: lead.id }))[0].changes, ['status', 'reminder'])
  await assert.rejects(() => update({ reminderAt: '2099-10-05T08:00:00Z' }), /关闭.*待办提醒/)
  lead = await update({ reminderAt: '' })
  assert.equal(lead.reminderAt, ''); assert.equal(lead.reminderDone, false)
  lead = await update({ status: 'NEW', ownerId: '' })
  assert.equal(lead.ownerName, ''); assert.equal(lead.followUpNote, '运营人员确认包装要求')
  await assert.rejects(() => call('admin.lead.history', { leadId: 'missing' }), /不存在/)
  await assert.rejects(() => call('admin.lead.list', { reminder: 'INVALID' }), /筛选条件/)
  const competition = await Promise.allSettled([update({ status: 'CONTACTED' }), update({ status: 'QUALIFIED' })])
  assert.equal(competition.filter(item => item.status === 'fulfilled').length, 1)
  assert.match(competition.find(item => item.status === 'rejected').reason.message, /已被其他人更新/)

  const savedHistory = await call('admin.lead.history', { leadId: lead.id })
  const refreshed = runtime(storage, demo.sessions)
  assert.deepEqual(await refreshed.call('admin.lead.history', { leadId: lead.id }), savedHistory)
  assert.ok(savedHistory.every((entry, i) => !i || entry.createdAt <= savedHistory[i - 1].createdAt))
  assert.equal(savedHistory.length, (await call('admin.lead.get', { leadId: lead.id })).followUps.length)

  const old = { id: 'old', organization: '旧保存客户', contact: '保留', phone: '13700000000', region: '杭州', channel: '团购', volume: '', demand: '', status: 'CONTACTED', followUpNote: '保留原备注', createdAt: '2026-09-19T05:20:00Z' }
  const legacyRaw = JSON.stringify({ version: 1, items: [old] }), legacyStorage = new Map([[storageKey, legacyRaw]]), legacyDemo = runtime(legacyStorage)
  const legacyActor = await legacyDemo.login('迁移运营')
  let legacy = await legacyDemo.call('admin.lead.get', { leadId: 'old' })
  assert.equal(legacy.version, 0); assert.equal(legacy.ownerId, ''); assert.equal(legacy.followUpNote, old.followUpNote)
  assert.equal(legacyStorage.get(storageKey), legacyRaw)
  legacy = await legacyDemo.call('admin.lead.update', { leadId: 'old', expectedVersion: 0, ownerId: legacyActor.id })
  assert.equal(legacy.followUps[0].note, old.followUpNote)
  const legacyHistory = await legacyDemo.call('admin.lead.history', { leadId: 'old' })
  assert.equal(legacyHistory.length, 2); assert.equal(legacyHistory[1].createdAt, '')

  const failingOptions = { failWrite: true }, failing = runtime(legacyStorage, legacyDemo.sessions, failingOptions), beforeFailedSave = legacyStorage.get(storageKey)
  await assert.rejects(() => failing.call('admin.lead.update', { leadId: 'old', expectedVersion: 1, status: 'QUALIFIED' }), /保存失败/)
  assert.equal(legacyStorage.get(storageKey), beforeFailedSave)
  assert.equal((await failing.call('admin.lead.get', { leadId: 'old' })).version, 1)
  failingOptions.failWrite = false
  assert.equal((await failing.call('admin.lead.update', { leadId: 'old', expectedVersion: 1, status: 'QUALIFIED' })).version, 2)
  const denied = runtime(legacyStorage, legacyDemo.sessions, { failRead: true })
  await assert.rejects(() => denied.call('admin.lead.list'), /无法读取/)

  const bulk = Array.from({ length: 140 }, (_, i) => ({ ...old, id: `reminder-${i}`, followUpNote: '', ownerId: '', ownerName: '', reminderAt: new Date(Date.now() - 10000 + i).toISOString(), reminderDone: false, version: 0 }))
  bulk[1].status = 'WON'; bulk[2].reminderDone = true; bulk[3].reminderAt = ''
  const bulkDemo = runtime(new Map([[storageKey, JSON.stringify({ version: 1, items: bulk })]]))
  await bulkDemo.login()
  assert.equal((await bulkDemo.call('admin.lead.list', { reminder: 'due' })).total, 137)
  const reminders = await bulkDemo.call('admin.lead.reminders')
  assert.equal(reminders.length, 137)
  assert.ok(reminders.every((item, i) => !i || item.reminderAt >= reminders[i - 1].reminderAt))
  let overdue = await bulkDemo.call('admin.lead.get', { leadId: 'reminder-0' })
  overdue = await bulkDemo.call('admin.lead.update', { leadId: overdue.id, expectedVersion: overdue.version, followUpNote: '逾期仍能记录跟进', reminderAt: overdue.reminderAt })
  overdue = await bulkDemo.call('admin.lead.update', { leadId: overdue.id, expectedVersion: overdue.version, reminderDone: true })
  assert.equal(overdue.reminderDone, true)
  await assert.rejects(() => bulkDemo.call('admin.lead.update', { leadId: overdue.id, expectedVersion: overdue.version, reminderDone: false }), /晚于当前时间/)
}

run().catch(error => { console.error(error); process.exitCode = 1 })

async function crm(demo, storage) {
  const { call } = demo
  const old = await call('admin.lead.get', { leadId: 'lead-00' })
  const profile = { email: ' buyer@example.com ', address: '客户档案唯一地址', industry: '零售', notes: '每周采购' }
  const updated = await call('admin.lead.update', { leadId: old.id, expectedVersion: old.version, customerProfile: profile, opportunityAmount: 123456 })
  assert.equal(updated.opportunityAmount, 123456)
  assert.equal(updated.customerProfile.email, 'buyer@example.com')
  assert.equal((await call('admin.lead.list', { keyword: '客户档案唯一地址' })).total, 1)
  const history = await call('admin.lead.history', { leadId: old.id })
  assert.ok(history[0].changes.includes('profile')); assert.ok(history[0].changes.includes('amount'))
  assert.equal(history[0].opportunityAmount, 123456)
  const before = storage.get(storageKey)
  for (const amount of [-1, 1.1, 100000000001, null, '100']) await assert.rejects(() => call('admin.lead.update', { leadId: old.id, expectedVersion: updated.version, opportunityAmount: amount }), /商机金额/)
  await assert.rejects(() => call('admin.lead.update', { leadId: old.id, expectedVersion: updated.version, customerProfile: { ...profile, email: 'bad' } }), /邮箱/)
  assert.equal(storage.get(storageKey), before)
  const refreshed = runtime(storage, demo.sessions)
  assert.equal((await refreshed.call('admin.lead.get', { leadId: old.id })).opportunityAmount, 123456)
  const seed = Array.from({ length: 225 }, (_, i) => ({ ...updated, id: `crm-${i}`, status: i < 125 ? 'WON' : 'NEW', opportunityAmount: 101, channel: i % 2 ? '零售' : '团购' }))
  const calc = demo.load('admin/src/leads.ts').leadFunnel
  const funnel = calc(seed, { status: 'NEW', limit: 20 })
  assert.equal(funnel.total, 225); assert.equal(funnel.amount, 22725); assert.equal(funnel.wonAmount, 12625); assert.equal(funnel.activeAmount, 10100)
  assert.equal(funnel.stages[3].count, 125); assert.equal(funnel.winRate, 125 / 225)
  assert.equal(calc(seed, { channel: '零售' }).total, 112)
  assert.equal(calc(seed, { keyword: '不存在' }).winRate, 0)
  assert.equal((await call('admin.lead.funnel')).total, 35)
  console.log('CRM profile, cents validation, history snapshots, persistence and funnel across 225 leads passed.')
}
