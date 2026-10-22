const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const assert = require('node:assert/strict')
const ts = require('typescript')
const root = path.resolve(__dirname, '..')

async function run() {
  let page, answer = 'accept', failSave = false, prompts = 0
  const calls = [], cache = new Map()
  const wx = {
    requestSubscribeMessage: async ({ tmplIds }) => { prompts++; return { [tmplIds[0]]: answer } },
    showToast: () => {},
  }
  const callApi = async (action, data) => {
    calls.push({ action, data })
    if (action === 'product.restockSubscription.accept') {
      if (failSave) throw new Error('网络异常，请重试')
      return { status: 'READY' }
    }
    if (action === 'product.attention.set') return { enabled: data.enabled }
    throw new Error(`Unexpected action ${action}`)
  }
  function load(filename) {
    filename = path.resolve(root, filename)
    if (cache.has(filename)) return cache.get(filename).exports
    const module = { exports: {} }; cache.set(filename, module)
    const source = ts.transpileModule(fs.readFileSync(filename, 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS } }).outputText
    vm.runInNewContext(source, { module, exports: module.exports, wx, Page: value => {
      page = value
      page.setData = updates => Object.assign(page.data, updates)
    }, require: name => name.endsWith('/api') ? { callApi } : load(path.resolve(path.dirname(filename), `${name}.ts`)) }, { filename })
    return module.exports
  }
  const config = { enabled: true, templateId: 'restock-template' }
  const helper = load('miniprogram/utils/restock.ts')
  assert.equal(await helper.requestRestock(null), false)
  assert.equal(await helper.requestRestock({ ...config, demo: true }), false)
  assert.equal(prompts, 0)

  load('miniprogram/pages/product/index.ts')
  page.data.product = { id: 'p1', restockReminder: true, favorite: false, restockPushStatus: 'NONE' }
  page.data.productId = 'p1'; page.data.restockConfig = config
  answer = 'reject'
  await page.subscribeRestock()
  assert.equal(calls.length, 0)
  assert.equal(page.data.product.restockReminder, true)
  assert.match(page.data.restockError, /未开启微信提醒/)
  answer = 'accept'; failSave = true
  await page.subscribeRestock()
  assert.equal(page.data.product.restockPushStatus, 'NONE')
  assert.match(page.data.restockError, /网络异常/)
  assert.equal(page.data.attentionBusy, false)
  failSave = false
  await page.subscribeRestock()
  assert.equal(page.data.product.restockPushStatus, 'READY')
  assert.equal(calls.at(-1).data.templateId, 'restock-template')
  await page.toggleAttention({ currentTarget: { dataset: { kind: 'favorite' } } })
  assert.equal(page.data.product.restockPushStatus, 'READY')
  await page.toggleAttention({ currentTarget: { dataset: { kind: 'restock' } } })
  assert.equal(page.data.product.restockReminder, false)
  assert.equal(page.data.product.restockPushStatus, 'NONE')

  load('miniprogram/pages/attention/index.ts')
  page.data.restockConfig = config
  page.data.products = [{ id: 'p1', restockReminder: true }, { id: 'p2', restockReminder: true }]
  await page.subscribeRestock({ currentTarget: { dataset: { id: 'p2' } } })
  assert.equal(page.data.products[0].restockPushStatus, undefined)
  assert.equal(page.data.products[1].restockPushStatus, 'READY')
  page.data.busy = true
  const before = prompts
  await page.subscribeRestock({ currentTarget: { dataset: { id: 'p2' } } })
  assert.equal(prompts, before)
  console.log('Restock authorization, rejection, save failure, cancellation and list checks passed')
}
run().catch(error => { console.error(error); process.exitCode = 1 })
