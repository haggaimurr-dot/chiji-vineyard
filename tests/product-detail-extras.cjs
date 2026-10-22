const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const assert = require('node:assert/strict')
const ts = require('typescript')
const root = path.resolve(__dirname, '..')

async function run() {
  const requests = [], navigation = [], clipboard = [], toasts = []
  let failed = false, enabled = true
  const product = { id: 'gift & 1', name: '双拼礼盒', skus: [{name:'双拼装'}], traceability: {code:'DEMO-TRACE-1'}, giftCustomization: {enabled:true, minimumQuantity:20} }
  const callApi = async (action, data) => {
    requests.push({action, data})
    if (action === 'product.detail') {
      if (failed) throw new Error('网络失败')
      return {...product, giftCustomization:{...product.giftCustomization, enabled}}
    }
    return {id:'lead-1'}
  }
  const wx = { navigateTo: v => navigation.push(v), setClipboardData: v => clipboard.push(v),
    showToast: v => toasts.push(v), setNavigationBarTitle: () => {} }
  function page(relative) {
    let spec
    const source = ts.transpileModule(fs.readFileSync(path.join(root, relative), 'utf8'), {compilerOptions:{target:ts.ScriptTarget.ES2020, module:ts.ModuleKind.CommonJS}}).outputText
    vm.runInNewContext(source, {exports:{}, wx, Page:v => {spec = v}, require:() => ({callApi}), console})
    return {...spec, data:structuredClone(spec.data), setData(updates) {
      for (const [key, value] of Object.entries(updates)) {
        const keys = key.split('.'); let target = this.data
        for (const part of keys.slice(0,-1)) target = target[part]
        target[keys.at(-1)] = value
      }
    }}
  }
  const detail = page('miniprogram/pages/product/index.ts')
  detail.data.product = structuredClone(product)
  detail.copyTraceCode()
  assert.equal(clipboard[0].data, 'DEMO-TRACE-1')
  clipboard[0].fail(); assert.match(toasts.at(-1).title, /复制失败/)
  delete detail.data.product.traceability.code
  detail.copyTraceCode(); assert.equal(clipboard.length, 1)
  detail.customizeGift()
  assert.equal(navigation[0].url, '/pages/cooperation/index?giftProductId=gift%20%26%201')
  detail.data.product.giftCustomization.enabled = false
  detail.customizeGift(); assert.equal(navigation.length, 1)

  const form = page('miniprogram/pages/cooperation/index.ts')
  await form.onLoad({giftProductId:product.id})
  assert.equal(form.data.giftProductName, product.name)
  assert.equal(form.data.form.volume, '20 盒')
  assert.ok(form.data.form.demand.includes(product.id))
  assert.equal(form.data.channelIndex, 2)
  Object.assign(form.data.form, {organization:'测试企业',contact:'测试',phone:'13800138000',region:'上海'})
  await form.submit()
  assert.equal(requests.filter(x => x.action === 'lead.submit').length, 0, 'consent required')
  form.data.agreed = true
  await form.submit()
  const lead = requests.find(x => x.action === 'lead.submit')
  assert.equal(lead.data.channel, '企业福利')
  assert.ok(lead.data.demand.includes(product.id))
  assert.equal(form.data.success, true)
  await form.again()
  assert.equal(form.data.success, false)
  assert.equal(form.data.agreed, false)
  assert.ok(form.data.form.demand.includes(product.id))
  const retry = page('miniprogram/pages/cooperation/index.ts')
  failed = true; await retry.onLoad({giftProductId:product.id})
  assert.match(retry.data.giftError, /网络失败/)
  const before = requests.length
  await retry.submit(); assert.equal(requests.length, before)
  failed = false; await retry.loadGiftProduct(); assert.equal(retry.data.giftError, '')
  enabled = false; await retry.loadGiftProduct(); assert.match(retry.data.giftError, /暂未开放/)
  const ordinary = page('miniprogram/pages/cooperation/index.ts')
  const count = requests.length
  await ordinary.onLoad({}); assert.equal(requests.length, count)
  assert.equal(ordinary.data.form.demand, '')
  console.log('Product detail extras: copy, navigation, prefill, lead submission, consent, disabled service and retry passed')
}
run().catch(error => {console.error(error); process.exitCode = 1})
