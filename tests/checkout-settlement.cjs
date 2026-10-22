const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const assert = require('node:assert/strict')
const ts = require('typescript')
const root = path.resolve(__dirname, '..')
function runtime(globals) {
  const cache = new Map()
  return function load(relative) {
    const filename = path.resolve(root, relative)
    if (cache.has(filename)) return cache.get(filename).exports
    const module = { exports: {} }; cache.set(filename, module)
    const source = ts.transpileModule(fs.readFileSync(filename, 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS } }).outputText
    vm.runInNewContext(source, { module, exports: module.exports, console, Error, setTimeout, clearTimeout, structuredClone, ...globals, require: name => load(path.resolve(path.dirname(filename), `${name}.ts`)) }, { filename })
    return module.exports
  }
}
async function run() {
  const storage = new Map(), requests = []
  let page, destination, demo = true, lost = false, cancelPayment = false, remoteOrder
  const wx = {
    getStorageSync: key => structuredClone(storage.get(key)), setStorageSync: (key,v) => storage.set(key,structuredClone(v)), removeStorageSync: key => storage.delete(key),
    showToast() {}, redirectTo: ({url}) => { destination = url }, requestPayment: async () => { if (cancelPayment) throw new Error('用户取消支付') },
    cloud: { callContainer: async ({data:{action,data}}) => {
      requests.push({action,data})
      let result
      if (action === 'order.create') { remoteOrder ||= { id:'remote-order', ...data }; if (lost) throw new Error('响应丢失'); result = remoteOrder }
      else if (action === 'order.recover') result = remoteOrder || null
      else if (action === 'payment.create') result = { timeStamp:'1' }
      else if (action === 'order.get') result = remoteOrder
      else throw new Error(`unexpected action ${action}`)
      return {statusCode:200,data:{ok:true,data:result}}
    } },
  }
  const load = runtime({wx,getApp:()=>({globalData:{demoMode:demo}}),Page:spec=>{ page=spec; page.setData=(updates,callback)=>{ Object.assign(page.data,updates); callback?.() } }})
  const {callApi,demoShippingRule} = load('miniprogram/utils/api.ts')
  const {chinaDate,checkoutIntent} = load('miniprogram/utils/checkout.ts')
  const {shippingFee,validateShippingDetails} = load('miniprogram/utils/shipping.ts')
  const {validateMockConfig} = load('admin/src/mock-config-validation.ts')
  const p = (await callApi('product.list'))[0]
  const items = [{productId:p.id,skuId:p.skus[0].id,quantity:2,selected:true}]
  const address = {name:'测试',phone:'13800000000',province:'浙江省',city:'杭州市',district:'余杭区',detail:'1号'}
  const payload = {items,address,couponCode:'DEMO10',deliveryDate:chinaDate(3),idempotencyKey:'checkout_demo_0001'}
  const orders = await Promise.all(Array.from({length:8},()=>callApi('order.create',payload)))
  assert.equal(new Set(orders.map(o=>o.id)).size,1)
  assert.equal(storage.get('chiji_demo_orders').length,1)
  assert.equal((await callApi('order.create',{...payload,items:items.map(i=>({...i,product:{fake:true},sku:{price:1}}))})).id,orders[0].id)
  await assert.rejects(()=>callApi('order.create',{...payload,note:'changed'}),/其他结算/)
  await assert.rejects(()=>callApi('order.create',{...payload,idempotencyKey:'checkout_demo_0002'}),/占用/)
  assert.equal((await callApi('order.recover',{idempotencyKey:payload.idempotencyKey})).id,orders[0].id)
  await callApi('payment.create',{orderId:orders[0].id})
  assert.equal((await callApi('order.create',payload)).paymentStatus,'PAID')
  await assert.rejects(()=>callApi('order.create',{...payload,idempotencyKey:'invalid'}),/幂等键/)
  const base = {name:'省规则',province:'浙江省',city:'',district:'',blocked:false,freight:2000,freeShippingThreshold:20000,baseQuantity:1,extraFreight:500,surcharge:300}
  const city = {...base,name:'城市规则',city:'杭州市',freight:1000,baseQuantity:2,extraFreight:200,surcharge:400}
  const district = {...city,name:'暂停区域',district:'西湖区',blocked:true}
  const rule = {freight:1200,freeShippingThreshold:19900,blockedProvinces:[],regions:[base,city,district],blockedDates:[chinaDate(4)]}
  validateShippingDetails(rule)
  assert.equal(validateMockConfig('shipping',rule,[],[]).regions.length,3)
  assert.equal(shippingFee(rule,address,15000,3).freight,1600)
  assert.equal(shippingFee(rule,address,30000,3).freight,400)
  assert.match(shippingFee(rule,{...address,district:'西湖区'},15000,3).blockedReason,/不支持/)
  assert.match(shippingFee(rule,address,15000,3,chinaDate(4)).blockedReason,/暂停配送/)
  assert.throws(()=>validateShippingDetails({...rule,regions:[base,base]}),/重复/)
  assert.throws(()=>validateShippingDetails({...rule,blockedDates:['2026-02-30']}),/日期/)
  Object.assign(demoShippingRule,rule)
  const quote = await callApi('order.quote',{items,address,deliveryDate:chinaDate(4)})
  assert.match(quote.blockedReason,/暂停配送/)
  await assert.rejects(()=>callApi('order.create',{items,address,deliveryDate:chinaDate(4),idempotencyKey:'blocked_date_0001'}),/暂停配送/)
  // Preserve keys through ambiguous failures and recover before stock/coupon revalidation.
  demo=false; storage.set('chiji_mini_token','test-token')
  load('miniprogram/pages/checkout/index.ts')
  const prepare = () => page.setData({items,address,quote:{total:20000},quoteBusy:false,quoteError:'',couponCode:'',deliveryDate:'',note:'',agreed:true,createdOrderId:''})
  prepare(); lost=true
  await page.submit()
  const pending = storage.get('chiji_checkout_attempt')
  assert.ok(pending.key)
  await page.submit()
  assert.equal(requests.filter(r=>r.action==='order.create').length,2)
  assert.equal(requests[0].data.idempotencyKey,requests[1].data.idempotencyKey)
  storage.set('chiji_checkout_items',items)
  await page.onLoad()
  assert.match(destination,/remote-order/)
  assert.equal(storage.has('chiji_checkout_attempt'),false)
  assert.equal(requests.some(r=>r.action==='product.detail'),false)
  // Cancellation after a successful create goes to the same order's payment entry.
  lost=false; cancelPayment=true; destination=''; prepare(); remoteOrder=undefined
  await page.submit()
  assert.match(destination,/remote-order/)
  assert.equal(storage.has('chiji_checkout_attempt'),false)
  const count = requests.filter(r=>r.action==='order.create').length
  await page.submit()
  assert.equal(requests.filter(r=>r.action==='order.create').length,count)
  assert.equal(checkoutIntent({...payload,address:{...address,id:'other',isDefault:true}}),checkoutIntent(payload))
  console.log('Checkout settlement passed: concurrent replay, coupon occupancy, conflicts, recovery, payment cancellation, regional fees, dates and config validation.')
}
run().catch(e=>{console.error(e);process.exitCode=1})
