const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const assert = require('node:assert/strict')
const ts = require('typescript')
const root = path.resolve(__dirname, '..')
let page, accepted = 'accept', failSave = false, requests = 0, saves = 0, resolveRequest
const wx = { requestSubscribeMessage: () => { requests++; return resolveRequest ? new Promise(resolve => { resolveRequest = resolve }) : Promise.resolve({ template: accepted }) } }
const source = ts.transpileModule(fs.readFileSync(path.join(root, 'miniprogram/pages/activity/index.ts'), 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS } }).outputText
vm.runInNewContext(source, {
  exports: {}, wx, getApp: () => ({ globalData: { demoMode: false } }),
  Page: value => { page = value; value.setData = updates => Object.assign(value.data, updates) },
  require: name => name.endsWith('/api') ? { callApi: async (action, data) => {
    if (action === 'activity.subscription.config') return { enabled: true, templateId: 'template', status: 'NONE' }
    assert.equal(action, 'activity.subscription.accept'); assert.equal(data.activityId, 'a1'); saves++
    if (failSave) throw new Error('登记失败')
    return { status: 'READY' }
  } } : name.endsWith('/home') ? { defaultHome: { activities: [] } } : { reservationActions: {}, reservationData: {} },
})
async function run() {
  Object.assign(page.data, { activityId: 'a1', reserved: true, loading: false, reservationBusy: '' })
  await page.loadSubscription()
  await page.subscribeActivity()
  assert.equal(page.data.subscription.status, 'READY'); assert.equal(saves, 1)
  accepted = 'reject'; await page.subscribeActivity()
  assert.equal(saves, 1); assert.match(page.data.subscriptionError, /预约仍然有效/)
  accepted = 'accept'; failSave = true; await page.subscribeActivity()
  assert.match(page.data.subscriptionError, /登记失败/); assert.equal(page.data.subscriptionBusy, false)
  failSave = false
  for (const status of ['SENT', 'SENDING', 'UNCERTAIN']) { page.data.subscription.status = status; const before = requests; await page.subscribeActivity(); assert.equal(requests, before) }
  page.data.subscription.status = 'NONE'; page.data.reserved = false
  const before = requests; await page.subscribeActivity(); assert.equal(requests, before)
  page.data.reserved = true; resolveRequest = true
  const pending = page.subscribeActivity(); await page.subscribeActivity(); assert.equal(requests, before + 1)
  page.data.requestVersion++; resolveRequest({ template: 'accept' }); await pending
  assert.equal(page.data.subscription.status, 'NONE', 'stale response must not change current page')
  console.log('Activity WeChat subscription passed: acceptance, refusal, save failure, busy guard, ownership input and stale responses')
}
run().catch(error => { console.error(error); process.exitCode = 1 })
