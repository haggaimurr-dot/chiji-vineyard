const fs = require('node:fs'), path = require('node:path'), vm = require('node:vm'), assert = require('node:assert/strict'), ts = require('typescript')
const root = path.resolve(__dirname, '..'), moduleValue = { exports: {} }
vm.runInNewContext(ts.transpileModule(fs.readFileSync(path.join(root, 'admin/src/reminder-tasks.ts'), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText, { module: moduleValue, exports: moduleValue.exports, Date, Error, structuredClone })
const { resolutionStatus, reminderDemo } = moduleValue.exports
for (const status of ['NONE', 'READY', 'SENDING', 'SENT', 'FAILED', 'BLOCKED', 'UNCERTAIN', 'CANCELLED']) {
  for (const action of ['retry', 'sent', 'stop', 'unknown']) {
    const allowed = status === 'FAILED' && ['retry', 'stop'].includes(action) || status === 'BLOCKED' && action === 'stop' || status === 'UNCERTAIN' && ['sent', 'stop'].includes(action)
    if (allowed) assert.ok(resolutionStatus(status, action)); else assert.throws(() => resolutionStatus(status, action))
  }
}
const list = reminderDemo('admin.reminder.tasks', { status: 'issues', limit: 2 })
assert.equal(list.items.length, 2); assert.equal(list.hasMore, true); assert.equal(list.counts.SENT, 1)
const failed = list.items.find(t => t.status === 'FAILED')
assert.throws(() => reminderDemo('admin.reminder.resolve', { ...failed, action: 'retry', note: ' ' }))
assert.equal(reminderDemo('admin.reminder.resolve', { ...failed, action: 'retry', note: '已修复模板' }).status, 'READY')
assert.throws(() => reminderDemo('admin.reminder.resolve', { ...failed, action: 'retry', note: '重复请求' }))
assert.equal(reminderDemo('admin.reminder.history', { openid: failed.openid }).items[0].status, 'FAILED')
assert.equal(reminderDemo('admin.reminder.history', { openid: 'another-user' }).items.length, 0)
const uncertain = reminderDemo('admin.reminder.tasks', { status: 'UNCERTAIN' }).items[0]
assert.throws(() => reminderDemo('admin.reminder.resolve', { ...uncertain, action: 'retry', note: '重试' }))
reminderDemo('admin.reminder.resolve', { ...uncertain, action: 'sent', note: '微信记录确认成功' })
assert.equal(reminderDemo('admin.reminder.history', { openid: uncertain.openid }).items[0].status, 'SENT')
console.log('通知任务筛选、分页、处置边界、重复请求及历史保留验证通过')

async function testView() {
  const calls = [], vue = require('vue'), viewModule = { exports: {} }
  const source = fs.readFileSync(path.join(root, 'admin/src/views/ReminderTasks.vue'), 'utf8').match(/<script setup lang="ts">([\s\S]*?)<\/script>/)[1]
  const code = ts.transpileModule(source + '\nexport const state = { load, showHistory, open, resolve, data, selected, busy, note, action, error, historyUser, history, historyMore };', { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  const imports = { vue: { ...vue, onMounted() {} }, '../api': { isDemo: true, api: (action, data) => new Promise((resolve, reject) => calls.push({ action, data, resolve, reject })) }, '../permissions': { can: () => true, currentAdmin: () => ({}) }, '../reminder-tasks': moduleValue.exports }
  vm.runInNewContext(code, { module: viewModule, exports: viewModule.exports, Date, Error, require: name => imports[name] })
  const s = viewModule.exports.state
  const loading = s.load(20); await s.load(40)
  assert.equal(calls.length, 1); assert.equal(calls[0].data.offset, 20)
  calls.shift().resolve({ items: [uncertain], counts: {}, hasMore: false }); await loading; assert.equal(s.busy.value, false)
  const reading = s.showHistory('user', 20); assert.equal(calls[0].data.openid, 'user'); assert.equal(calls[0].data.offset, 20)
  calls.shift().resolve({ items: [], hasMore: false }); await reading
  s.open(failed); s.note.value = '核查记录'; const resolving = s.resolve(); await s.resolve()
  assert.equal(calls.length, 1); assert.equal(calls[0].action, 'admin.reminder.resolve'); assert.equal(calls[0].data.status, 'FAILED')
  calls.shift().reject(new Error('任务状态已变化')); await resolving
  assert.equal(s.error.value, '任务状态已变化'); assert.ok(s.selected.value); assert.equal(s.busy.value, false)
  const successful = s.resolve(); calls.shift().resolve({ status: 'READY' }); await new Promise(resolve => setImmediate(resolve))
  assert.equal(calls[0].action, 'admin.reminder.tasks'); calls.shift().resolve({ items: [], counts: {}, hasMore: false }); await successful
  assert.equal(s.selected.value, null); assert.equal(s.historyUser.value, '')
  console.log('看板页面分页、发送记录、并发点击防护、处置冲突及提交后刷新验证通过')
}
testView().catch(error => { console.error(error); process.exitCode = 1 })
